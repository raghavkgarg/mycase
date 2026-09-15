#!/bin/zsh
set -euo pipefail

# 1. Ensure working directory and environment PATH
PROJECT_DIR="/Users/raghavgarg/Projects/myGo/mycase"
cd "$PROJECT_DIR"

export PATH="/Users/raghavgarg/go/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"

LOG_DIR="$PROJECT_DIR/logs"
mkdir -p "$LOG_DIR"
LOG_FILE="$LOG_DIR/pit_update.log"

TODAY=$(date "+%Y-%m-%d")
DAY_OF_WEEK=$(date "+%u") # 1 = Monday, 7 = Sunday

echo "==================================================" >> "$LOG_FILE"
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Triggered daily PIT update check" >> "$LOG_FILE"

# 2. Skip weekends (safety check even if launchd is configured for weekdays)
if [ "$DAY_OF_WEEK" -ge 6 ]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Today is weekend ($TODAY). Skipping." >> "$LOG_FILE"
    exit 0
fi

# 3. NSE Trading Holidays (from config/nse_holidays.json)
HOLIDAYS_FILE="$PROJECT_DIR/config/nse_holidays.json"
if [ -f "$HOLIDAYS_FILE" ]; then
    HOLIDAY_NAME=""
    if command -v jq >/dev/null 2>&1; then
        HOLIDAY_NAME=$(jq -r --arg d "$TODAY" '.[$d] // empty' "$HOLIDAYS_FILE" 2>/dev/null || true)
    elif command -v python3 >/dev/null 2>&1; then
        HOLIDAY_NAME=$(python3 -c "import json; d=json.load(open('$HOLIDAYS_FILE')); print(d.get('$TODAY', ''))" 2>/dev/null || true)
    fi

    if [[ -n "$HOLIDAY_NAME" ]]; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Today ($TODAY) is an NSE Holiday: $HOLIDAY_NAME. Skipping run." >> "$LOG_FILE"
        exit 0
    fi
fi

# 4. Authenticate with broker (Zerodha Kite)
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Authenticating broker session (mycase auth)..." >> "$LOG_FILE"
cd "$PROJECT_DIR"
./mycase auth --no-browser >> "$LOG_FILE" 2>&1

# 5. Fetch today's executed trades into myportfolio
PORTFOLIO_DIR="/Users/raghavgarg/Projects/myGo/myportfolio"
if [ -d "$PORTFOLIO_DIR" ] && [ -x "$PORTFOLIO_DIR/dist/myportfolio" ]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Fetching today's trades into myportfolio..." >> "$LOG_FILE"
    cd "$PORTFOLIO_DIR"
    ./dist/myportfolio fetch-trades >> "$LOG_FILE" 2>&1
else
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] WARNING: myportfolio directory or binary not found. Skipping trade fetch." >> "$LOG_FILE"
fi

# 6. Run the unified EOD database update (Market Data Cache, PIT Quant Research, and Theme Lifecycle)
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Starting unified EOD database update for data/mycase.db..." >> "$LOG_FILE"
cd "$PROJECT_DIR"
./mycase db update --all --index niftytotalmarket --method earlymb --top 10 >> "$LOG_FILE" 2>&1

# 7. Synchronize Multibagger PIT factor scores on warmed Nifty Total Market cache
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Synchronizing Multibagger PIT factor scores on warmed Nifty Total Market cache..." >> "$LOG_FILE"
./mycase pit update --index niftytotalmarket --method multibagger --top 20 >> "$LOG_FILE" 2>&1

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Unified EOD database update completed successfully." >> "$LOG_FILE"



