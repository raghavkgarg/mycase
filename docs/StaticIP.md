# Zerodha Kite Connect Static IP Setup Guide (StaticIP.in)

This document provides step-by-step instructions for configuring and using your static IPv6 address from [staticip.in](https://staticip.in) with Zerodha Kite Connect and `mycase`.

---

## 1. Credentials & Configuration Summary

| Field | Value |
| :--- | :--- |
| **Whitelisted Static IPv6** | `YOUR_STATIC_IPV6_ADDRESS` |
| **Proxy Host** | `YOUR_PROXY_HOST` (e.g. `dc-mum-601.staticip.in`) |
| **Proxy Port** | `443` |
| **Proxy User ID** | `<YOUR_PROXY_USER>` |
| **Proxy Password** | `<YOUR_PROXY_PASSWORD>` |
| **Validity Expiry** | `<EXPIRY_DATE>` |
| **Target Broker** | Zerodha Kite Connect |

---

## 2. Step 1 — Whitelist Static IP in Zerodha Console

1. Log in to the [Zerodha Developer Profile](https://developers.kite.trade/profile).
2. Go to **My Apps** $\rightarrow$ Select your active app $\rightarrow$ **App Settings**.
3. Under **IP Whitelist**, add your Static IPv6 address provided by `staticip.in`:
   ```text
   YOUR_STATIC_IPV6_ADDRESS
   ```
4. Save the changes.

---

## 3. Step 2 — Configure `config/config.json`

`mycase` supports automatic proxy routing via `config/config.json`. 

Add the `"http_proxy"` field to `config/config.json`:

```json
{
  "api_key": "YOUR_API_KEY",
  "api_secret": "YOUR_API_SECRET",
  "access_token": "YOUR_ACCESS_TOKEN",
  "http_proxy": "http://<YOUR_PROXY_USER>:<YOUR_PROXY_PASSWORD>@dc-mum-601.staticip.in:443"
}
```

When `mycase` loads `config.json`, it automatically sets the `HTTP_PROXY`, `HTTPS_PROXY`, and `ALL_PROXY` environment variables for all Zerodha API calls (`gokiteconnect`), `yfinance` queries, and IP checks.

---

## 4. Step 3 — Verification & Testing

1. **Verify Proxy Connection via Terminal**:
   Test your proxy directly using curl:
   ```bash
   curl -x http://<YOUR_PROXY_USER>:<YOUR_PROXY_PASSWORD>@dc-mum-601.staticip.in:443 https://ifconfig.me
   ```
   *Output should match your static IP:* `YOUR_STATIC_IPV6_ADDRESS`.

2. **Run Pre-Execution Check in `mycase`**:
   Run any basket command:
   ```bash
   go run main.go basket data/microsmall.csv
   ```
   `mycase` will confirm the active whitelisted IP:
   ```text
   ====================================================================
                  IP WHITELIST PRE-EXECUTION CHECK                     
   ====================================================================
   Current IPv6: YOUR_STATIC_IPV6_ADDRESS
   👉 Please make sure your IP is whitelisted under App Settings on:
      https://developers.kite.trade/profile
   ====================================================================
   ```

---

## 5. Troubleshooting

| Issue | Cause | Solution |
| :--- | :--- | :--- |
| `IP ... is not allowed` | Whitelist missing on Zerodha Console | Verify your static IPv6 address is saved in App Settings. |
| `Proxy Authentication Required` | Incorrect User ID or Password | Verify credentials in `http_proxy` string in `config/config.json`. |
| `Insufficient permission` | Zerodha Quote API add-on missing | Ensure ticker is in holdings; `mycase` automatically falls back to holdings `LastPrice` or `yfinance`. |
