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

# 3. NSE Trading Holidays (YYYY-MM-DD : Holiday Name)
typeset -A NSE_HOLIDAYS=(
    ["2026-09-14"]="Ganesh Chaturthi"
    ["2026-10-02"]="Mahatma Gandhi Jayanti"
    ["2026-10-20"]="Dussehra"
    ["2026-11-10"]="Diwali-Balipratipada"
    ["2026-11-24"]="Prakash Gurpurb Sri Guru Nanak Dev"
    ["2026-12-25"]="Christmas"
)

if [[ -n "${NSE_HOLIDAYS[$TODAY]:-}" ]]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Today ($TODAY) is an NSE Holiday: ${NSE_HOLIDAYS[$TODAY]}. Skipping run." >> "$LOG_FILE"
    exit 0
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

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Unified EOD database update completed successfully." >> "$LOG_FILE"


