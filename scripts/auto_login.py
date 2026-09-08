#!/usr/bin/env python3
"""
Automated Zerodha Kite Daily Login & Token Refresher.
Generates TOTP dynamically, authenticates with Kite Connect,
and updates config/config.json across mycase and myoption.
"""

import json
import os
import sys
import warnings
from pathlib import Path
from urllib.parse import parse_qs, urlparse

warnings.filterwarnings("ignore")

try:
    import pyotp
    import requests
    from kiteconnect import KiteConnect
except ImportError as e:
    print(
        f"Missing dependency: {e}. Please run: pip install pyotp kiteconnect requests",
        file=sys.stderr,
    )
    sys.exit(1)

# Paths to config files to keep synchronized
CONFIG_PATHS = [
    Path(__file__).resolve().parent.parent / "config" / "config.json",
    Path("/Users/raghavgarg/Projects/myGo/myoption/config/config.json"),
]


def load_config():
    """Load configuration from the primary config.json or environment variables."""
    primary_config_file = CONFIG_PATHS[0]
    file_cfg = {}
    if primary_config_file.exists():
        with open(primary_config_file, "r", encoding="utf-8") as f:
            try:
                file_cfg = json.load(f)
            except Exception as e:
                print(f"[WARN] Failed to parse {primary_config_file}: {e}")

    api_key = os.getenv("KITE_API_KEY") or file_cfg.get("api_key")
    api_secret = os.getenv("KITE_API_SECRET") or file_cfg.get("api_secret")
    user_id = os.getenv("KITE_USER_ID") or file_cfg.get("user_id")
    password = os.getenv("KITE_PASSWORD") or file_cfg.get("password")
    totp_secret = os.getenv("KITE_TOTP_SECRET") or file_cfg.get("totp_secret")
    http_proxy = os.getenv("HTTP_PROXY") or file_cfg.get("http_proxy")

    missing = []
    if not api_key:
        missing.append("api_key / KITE_API_KEY")
    if not api_secret:
        missing.append("api_secret / KITE_API_SECRET")
    if not user_id:
        missing.append("user_id / KITE_USER_ID")
    if not password:
        missing.append("password / KITE_PASSWORD")
    if not totp_secret:
        missing.append("totp_secret / KITE_TOTP_SECRET")

    if missing:
        raise ValueError(
            f"Missing required credentials: {', '.join(missing)}. "
            f"Please configure them in {primary_config_file} or set environment variables."
        )

    return {
        "api_key": api_key.strip(),
        "api_secret": api_secret.strip(),
        "user_id": user_id.strip(),
        "password": password.strip(),
        "totp_secret": totp_secret.strip().replace(" ", ""),
        "http_proxy": http_proxy.strip() if http_proxy else None,
    }


def update_access_token_in_configs(new_token: str):
    """Write the updated access_token to all configured config.json files."""
    for cfg_path in CONFIG_PATHS:
        if not cfg_path.exists():
            continue
        try:
            with open(cfg_path, "r", encoding="utf-8") as f:
                data = json.load(f)
            data["access_token"] = new_token
            with open(cfg_path, "w", encoding="utf-8") as f:
                json.dump(data, f, indent=2)
            print(f"🔄 Updated access_token in: {cfg_path}")
        except Exception as e:
            print(f"[ERROR] Failed to update {cfg_path}: {e}")


def get_automated_access_token(creds: dict) -> str:
    """Execute headless authentication chain and return fresh access_token."""
    session = requests.Session()

    # Route through proxy if configured (necessary if static IP whitelisting is active)
    if creds["http_proxy"]:
        session.proxies = {
            "http": creds["http_proxy"],
            "https": creds["http_proxy"],
        }
        print("🌐 Using configured HTTP proxy for authentication requests.")

    # Step 1: Handshake with Kite login page to establish session context & cookies
    login_url = f"https://kite.trade/connect/login?v=3&api_key={creds['api_key']}"
    print("➡️  Step 1: Initiating login handshake...")
    session.get(login_url, timeout=15)

    # Step 2: Submit User ID and Password to the backend login endpoint
    print("➡️  Step 2: Authenticating User ID & Password...")
    login_payload = {
        "user_id": creds["user_id"],
        "password": creds["password"],
    }
    login_resp = session.post(
        "https://kite.zerodha.com/api/login",
        data=login_payload,
        timeout=15,
    )
    login_data = login_resp.json()
    if login_data.get("status") != "success":
        msg = login_data.get("message", "Unknown error")
        raise RuntimeError(f"Login failed: {msg}")

    request_id = login_data["data"]["request_id"]

    # Step 3: Generate TOTP code
    totp = pyotp.TOTP(creds["totp_secret"])
    totp_token = totp.now()
    print(f"➡️  Step 3: Generated live TOTP token: {totp_token[:2]}****")

    # Step 4: Submit TOTP to 2FA validation endpoint
    print("➡️  Step 4: Submitting Two-Factor Authentication (2FA)...")
    twofa_payload = {
        "user_id": creds["user_id"],
        "request_id": request_id,
        "twofa_value": totp_token,
    }
    twofa_resp = session.post(
        "https://kite.zerodha.com/api/twofa",
        data=twofa_payload,
        timeout=15,
    )
    twofa_data = twofa_resp.json()
    if twofa_data.get("status") != "success":
        msg = twofa_data.get("message", "Unknown error")
        raise RuntimeError(f"2FA/TOTP validation failed: {msg}")

    # Step 5: Follow redirect to capture request_token
    print("➡️  Step 5: Catching redirect for request_token...")
    final_auth_url = f"{login_url}&skip_session=true"
    redirect_resp = session.get(final_auth_url, allow_redirects=True, timeout=15)

    parsed_url = urlparse(redirect_resp.url)
    query_params = parse_qs(parsed_url.query)

    if "request_token" not in query_params:
        raise RuntimeError(
            f"Failed to capture request_token. Final URL was: {redirect_resp.url}"
        )

    request_token = query_params["request_token"][0]
    print(f"✅ Captured request_token: {request_token[:6]}...")

    # Step 6: Exchange request_token for daily access_token using KiteConnect
    print("➡️  Step 6: Exchanging request_token for Kite access_token...")
    kite = KiteConnect(api_key=creds["api_key"])
    if creds["http_proxy"]:
        kite.set_proxy({
            "http": creds["http_proxy"],
            "https": creds["http_proxy"],
        })

    session_data = kite.generate_session(
        request_token=request_token,
        api_secret=creds["api_secret"],
    )
    return session_data["access_token"]


def main():
    print("====================================================================")
    print("            Zerodha Kite Automated Headless Login Refresher         ")
    print("====================================================================")
    try:
        creds = load_config()
        access_token = get_automated_access_token(creds)
        print(f"🎉 Success! Generated Access Token: {access_token[:8]}...")
        update_access_token_in_configs(access_token)
        print("✅ Daily Zerodha authentication complete.")
    except Exception as e:
        print(f"❌ Automation Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
