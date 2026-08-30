#!/usr/bin/env python3
"""
scripts/fetch_nse_data.py
Fetch official equity data, earnings dates, quarterly financial results,
corporate actions, and deliverable position data from NSE using nselib.
Outputs JSON formatted results to stdout. Supports single or multiple comma-separated symbols.
"""

import sys
import os
import json
import re
import argparse
from datetime import datetime
from html.parser import HTMLParser

try:
    import pandas as pd
    from nselib import capital_market
except ImportError as e:
    print(json.dumps({"error": f"Failed to import nselib or pandas: {e}"}))
    sys.exit(1)


def clean_symbol(symbol: str) -> str:
    s = symbol.strip().upper()
    for prefix in ["NSE:", "BSE:"]:
        if s.startswith(prefix):
            s = s[len(prefix):]
    for suffix in [".NS", ".BO"]:
        if s.endswith(suffix):
            s = s[:-len(suffix)]
    return s


def parse_nse_date(d_str: str):
    if not d_str or pd.isna(d_str) or str(d_str).strip() in ["-", "", "None", "nan"]:
        return None
    d_clean = str(d_str).strip()
    formats = [
        "%d-%b-%Y",  # 28-Jul-2026
        "%Y-%m-%d",  # 2026-07-28
        "%d-%m-%Y",  # 28-07-2026
        "%d/%m/%Y",  # 28/07/2026
        "%d-%b-%y",  # 28-Jul-26
    ]
    for fmt in formats:
        try:
            return datetime.strptime(d_clean, fmt)
        except ValueError:
            continue
    return None


def sanitize_val(val):
    if val is None or pd.isna(val):
        return None
    if isinstance(val, (int, float)):
        return val
    val_str = str(val).strip().replace(",", "")
    try:
        if "." in val_str:
            return float(val_str)
        return int(val_str)
    except ValueError:
        return str(val)


def fetch_earnings_dates(symbols: list):
    symbols_clean = [clean_symbol(s) for s in symbols if clean_symbol(s)]
    sym_set = set(symbols_clean)
    dates_map = {s: [] for s in symbols_clean}

    # 1. Event Calendar (Board Meetings & Announcement Dates)
    for period in ["1M", "3M"]:
        try:
            df_evt = capital_market.event_calendar_for_equity(period=period)
            if df_evt is not None and hasattr(df_evt, 'iterrows') and len(df_evt) > 0:
                if 'symbol' in df_evt.columns:
                    matched = df_evt[df_evt['symbol'].astype(str).str.upper().isin(sym_set)]
                    for _, row in matched.iterrows():
                        sym = str(row.get('symbol', '')).strip().upper()
                        d_str = str(row.get('date', '')).strip()
                        purpose = str(row.get('purpose', '')).strip()
                        bm_desc = str(row.get('bm_desc', '')).strip()
                        p_dt = parse_nse_date(d_str)
                        if p_dt and sym in dates_map:
                            dates_map[sym].append({
                                "date": p_dt.strftime("%Y-%m-%d"),
                                "purpose": purpose,
                                "description": bm_desc,
                                "source": "NSE Event Calendar"
                            })
        except Exception:
            pass

    # 2. Financial Results intimations
    try:
        df_fin = capital_market.financial_results_for_equity(period="3M")
        if df_fin is not None and hasattr(df_fin, 'iterrows') and len(df_fin) > 0:
            sym_col = 'Symbol' if 'Symbol' in df_fin.columns else ('symbol' if 'symbol' in df_fin.columns else None)
            if sym_col:
                matched = df_fin[df_fin[sym_col].astype(str).str.upper().isin(sym_set)]
                for _, row in matched.iterrows():
                    sym = str(row.get(sym_col, '')).strip().upper()
                    bm_date = str(row.get('DateOfBoardMeetingWhenFinancialResultsWereApproved', '')).strip()
                    prior_date = str(row.get('DateOnWhichPriorIntimationOfTheMeetingForConsideringFinancialResultsWasInformedToTheExchange', '')).strip()
                    qtr = str(row.get('ReportingQuarter', '')).strip()

                    for d_raw, label in [(bm_date, f"Board Meeting Qtr {qtr}"), (prior_date, f"Prior Intimation Qtr {qtr}")]:
                        p_dt = parse_nse_date(d_raw)
                        if p_dt and sym in dates_map:
                            dates_map[sym].append({
                                "date": p_dt.strftime("%Y-%m-%d"),
                                "purpose": "Financial Results",
                                "description": label,
                                "source": "NSE Financial Results"
                            })
    except Exception:
        pass

    results = {}
    for sym in symbols_clean:
        items = dates_map.get(sym, [])
        unique_items = []
        seen = set()
        for item in items:
            key = (item["date"], item["purpose"])
            if key not in seen:
                seen.add(key)
                unique_items.append(item)
        unique_items.sort(key=lambda x: x["date"])
        dates_only = sorted(list(set([x["date"] for x in unique_items])))
        results[sym] = {
            "symbol": sym,
            "earnings_dates": unique_items,
            "dates_only": dates_only
        }

    return results if len(symbols_clean) > 1 else results[symbols_clean[0]]


def fetch_financial_results(symbols: list, period: str = "3M"):
    symbols_clean = [clean_symbol(s) for s in symbols if clean_symbol(s)]
    sym_set = set(symbols_clean)
    res_map = {s: [] for s in symbols_clean}

    try:
        df_fin = capital_market.financial_results_for_equity(period=period)
        if df_fin is not None and hasattr(df_fin, 'iterrows') and len(df_fin) > 0:
            sym_col = 'Symbol' if 'Symbol' in df_fin.columns else ('symbol' if 'symbol' in df_fin.columns else None)
            if sym_col:
                matched = df_fin[df_fin[sym_col].astype(str).str.upper().isin(sym_set)]
                for _, row in matched.iterrows():
                    sym = str(row.get(sym_col, '')).strip().upper()
                    item = {}
                    for col in df_fin.columns:
                        item[col] = sanitize_val(row.get(col))
                    if sym in res_map:
                        res_map[sym].append(item)
    except Exception as e:
        return {"error": f"Failed to fetch financial results: {e}"}

    results = {}
    for sym in symbols_clean:
        r_list = res_map.get(sym, [])
        results[sym] = {
            "symbol": sym,
            "results_count": len(r_list),
            "results": r_list
        }

    return results if len(symbols_clean) > 1 else results[symbols_clean[0]]


def fetch_corporate_actions(symbols: list, period: str = "3M"):
    symbols_clean = [clean_symbol(s) for s in symbols if clean_symbol(s)]
    sym_set = set(symbols_clean)
    act_map = {s: [] for s in symbols_clean}

    try:
        df_ca = capital_market.corporate_actions_for_equity(period=period)
        if df_ca is not None and hasattr(df_ca, 'iterrows') and len(df_ca) > 0:
            if 'symbol' in df_ca.columns:
                matched = df_ca[df_ca['symbol'].astype(str).str.upper().isin(sym_set)]
                for _, row in matched.iterrows():
                    sym = str(row.get('symbol', '')).strip().upper()
                    if sym in act_map:
                        act_map[sym].append({
                            "subject": sanitize_val(row.get('subject')),
                            "ex_date": sanitize_val(row.get('exDate')),
                            "rec_date": sanitize_val(row.get('recDate')),
                            "broadcast_date": sanitize_val(row.get('caBroadcastDate')),
                            "face_val": sanitize_val(row.get('faceVal')),
                        })
    except Exception as e:
        return {"error": f"Failed to fetch corporate actions: {e}"}

    results = {}
    for sym in symbols_clean:
        a_list = act_map.get(sym, [])
        results[sym] = {
            "symbol": sym,
            "actions_count": len(a_list),
            "actions": a_list
        }

    return results if len(symbols_clean) > 1 else results[symbols_clean[0]]


def fetch_delivery_data(symbols: list, period: str = "1M"):
    symbols_clean = [clean_symbol(s) for s in symbols if clean_symbol(s)]
    results = {}

    for sym in symbols_clean:
        records = []
        try:
            df_pv = capital_market.price_volume_and_deliverable_position_data(symbol=sym, period=period)
            if df_pv is not None and hasattr(df_pv, 'iterrows') and len(df_pv) > 0:
                for _, row in df_pv.iterrows():
                    d_str = str(row.get('Date', '')).strip()
                    p_dt = parse_nse_date(d_str)
                    records.append({
                        "date": p_dt.strftime("%Y-%m-%d") if p_dt else d_str,
                        "close_price": sanitize_val(row.get('ClosePrice')),
                        "prev_close": sanitize_val(row.get('PrevClose')),
                        "open_price": sanitize_val(row.get('OpenPrice')),
                        "high_price": sanitize_val(row.get('HighPrice')),
                        "low_price": sanitize_val(row.get('LowPrice')),
                        "total_traded_qty": sanitize_val(row.get('TotalTradedQuantity')),
                        "deliverable_qty": sanitize_val(row.get('DeliverableQty')),
                        "delivery_pct": sanitize_val(row.get('%DlyQttoTradedQty')),
                        "turnover_rs": sanitize_val(row.get('TurnoverInRs')),
                    })
        except Exception as e:
            results[sym] = {"error": f"Failed to fetch delivery data for {sym}: {e}"}
            continue

        results[sym] = {
            "symbol": sym,
            "records_count": len(records),
            "records": records
        }

    return results if len(symbols_clean) > 1 else results[symbols_clean[0]]


class ScreenerAnnParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.announcements = []
        self.in_li = False
        self.in_div = False
        self.current_title = []
        self.current_desc = []

    def handle_starttag(self, tag, attrs):
        attrs_dict = dict(attrs)
        if tag == 'li' and 'overflow-wrap-anywhere' in attrs_dict.get('class', ''):
            self.in_li = True
            self.current_title = []
            self.current_desc = []
        elif tag == 'div' and self.in_li and 'ink-600' in attrs_dict.get('class', ''):
            self.in_div = True

    def handle_endtag(self, tag):
        if tag == 'li' and self.in_li:
            title = ''.join(self.current_title).strip()
            desc = ''.join(self.current_desc).strip()
            if desc and title.endswith(desc):
                title = title[:-len(desc)].strip()
            title = re.sub(r'\s+', ' ', title)
            desc = re.sub(r'\s+', ' ', desc)
            self.announcements.append({'title': title, 'desc': desc})
            self.in_li = False
        elif tag == 'div' and self.in_div:
            self.in_div = False

    def handle_data(self, data):
        if self.in_div:
            self.current_desc.append(data)
        elif self.in_li:
            self.current_title.append(data)


def is_within_last_12_months(date_str: str) -> bool:
    if not date_str:
        return True
    date_str = date_str.strip().lower()
    if " - " in date_str:
        date_str = date_str.split(" - ")[0].strip()

    now = datetime.now()
    if date_str.endswith("h"):
        return True
    if date_str.endswith("d"):
        try:
            return int(date_str[:-1]) <= 365
        except ValueError:
            return True
    if date_str.endswith("w"):
        try:
            return int(date_str[:-1]) * 7 <= 365
        except ValueError:
            return True

    months = ["jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"]
    matched_month = next((m for m in months if m in date_str), None)

    if matched_month:
        try:
            clean_str = re.sub(r'[^a-z0-9 ]', '', date_str)
            tokens = [t for t in clean_str.split() if t]
            day = 1
            year = now.year
            for t in tokens:
                if t.isdigit():
                    val = int(t)
                    if val <= 31:
                        day = val
                    elif val > 2000:
                        year = val
                    elif val > 20:
                        year = 2000 + val
            month_num = months.index(matched_month) + 1
            ann_date = datetime(year, month_num, day)
            return (now - ann_date).days <= 365
        except Exception:
            return True
    return True


def fetch_qualitative_data(symbols_raw: list) -> dict:
    import requests
    symbols_clean = [clean_symbol(s) for s in symbols_raw if clean_symbol(s)]
    results = {}
    headers = {'User-Agent': 'Mozilla/5.0'}

    for sym in symbols_clean:
        auditor_status = "Metric Coverage Pending (Failed to retrieve Screener.in data)"
        transcript_summary = "Metric Coverage Pending (Failed to retrieve Screener.in data)"
        management_stability = "Metric Coverage Pending (Failed to retrieve Screener.in data)"
        rpt_status = "Metric Coverage Pending (Failed to retrieve Screener.in data)"

        try:
            r = requests.get(f'https://www.screener.in/company/{sym}/', headers=headers, timeout=10)
            if r.status_code == 200:
                m = re.search(r'/announcements/recent/(\d+)/', r.text)
                if m:
                    comp_id = m.group(1)
                    r_ann = requests.get(f'https://www.screener.in/announcements/recent/{comp_id}/', headers=headers, timeout=10)
                    if r_ann.status_code == 200:
                        auditor_status = "No auditor qualifications detected in recent announcements"
                        transcript_summary = "Metric Coverage Pending (No order book updates found in recent announcements)"
                        management_stability = "Stable (No CFO/Auditor/KMP resignations in recent announcements)"
                        rpt_status = "Clean (No RPT alerts in recent announcements)"

                        parser = ScreenerAnnParser()
                        parser.feed(r_ann.text)
                        for ann in parser.announcements:
                            # Verify if announcement is within 12 months
                            if not is_within_last_12_months(ann['desc']):
                                continue

                            full_txt = (ann['title'] + " " + ann['desc']).lower()
                            if any(q in full_txt for q in ["qualification", "adverse remark", "disclaimer of opinion"]):
                                auditor_status = f"Attention Required: Potential Qualification ({ann['title']})"
                            if "order book" in full_txt:
                                transcript_summary = f"Order Book Update: {ann['title']}"
                            if any(k in full_txt for k in ["cfo", "chief financial officer", "auditor", "company secretary", "kmp"]):
                                if any(r in full_txt for r in ["resignation", "cessation", "resigned", "stepped down"]):
                                    management_stability = f"Warning: Cessation/Resignation detected: {ann['title']}"
                            if any(rp in full_txt for rp in ["related party", "rpt", "material transaction"]):
                                rpt_status = f"Disclosed: Related Party Transactions filed ({ann['title']})"
        except Exception:
            pass

        # Check local overrides / database for KMP alerts
        try:
            alerts_path = os.path.join("config", "management_alerts.json")
            if os.path.exists(alerts_path):
                with open(alerts_path, "r") as f_alerts:
                    alerts_db = json.load(f_alerts)
                    if sym in alerts_db:
                        for entry in alerts_db[sym]:
                            if is_within_last_12_months(entry["date"]):
                                management_stability = f"Warning: Cessation/Resignation detected: {entry['event']} (Date: {entry['date']})"
        except Exception:
            pass

        results[sym] = {
            "symbol": sym,
            "auditor_status": auditor_status,
            "transcript_summary": transcript_summary,
            "management_stability": management_stability,
            "rpt_status": rpt_status
        }

    return results if len(symbols_clean) > 1 else results[symbols_clean[0]]


def main():
    parser = argparse.ArgumentParser(description="Fetch NSE equity data using nselib")
    parser.add_argument("--symbol", type=str, help="Single stock ticker or comma-separated tickers (e.g. TATAMOTORS,NETWEB)")
    parser.add_argument(
        "--mode",
        type=str,
        default="earnings_dates",
        choices=["earnings_dates", "financial_results", "corporate_actions", "delivery_data", "qualitative_data"],
        help="Data fetching mode",
    )
    parser.add_argument("--period", type=str, default="1M", help="Time period (1W, 1M, 3M, 1Y)")

    args = parser.parse_args()

    if not args.symbol:
        print(json.dumps({"error": "--symbol argument is required"}))
        sys.exit(1)

    symbols = [s.strip() for s in args.symbol.split(",") if s.strip()]

    if args.mode == "earnings_dates":
        res = fetch_earnings_dates(symbols)
    elif args.mode == "financial_results":
        res = fetch_financial_results(symbols, period=args.period)
    elif args.mode == "corporate_actions":
        res = fetch_corporate_actions(symbols, period=args.period)
    elif args.mode == "delivery_data":
        res = fetch_delivery_data(symbols, period=args.period)
    elif args.mode == "qualitative_data":
        res = fetch_qualitative_data(symbols)
    else:
        res = {"error": f"Unknown mode {args.mode}"}

    print(json.dumps(res))


if __name__ == "__main__":
    main()
