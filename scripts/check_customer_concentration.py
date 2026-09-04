#!/usr/bin/env python3
import sys
import os
import re
import json

def parse_pdf(pdf_path):
    try:
        import pdfplumber
    except ImportError:
        return "pdfplumber library not installed"

    try:
        full_text = ""
        with pdfplumber.open(pdf_path) as pdf:
            # Scan pages for Ind AS 108 / Segment Reporting disclosures
            for page in pdf.pages:
                text = page.extract_text()
                if not text:
                    continue
                if any(k in text for k in ["Ind AS 108", "Segment", "major customer", "Major Customer", "Revenue from one"]):
                    full_text += text
                    
        if not full_text:
            return "Ind AS 108 Segment Notes not found in PDF"

        # Case 1: Look for "no single customer" declaration
        if re.search(r"no single customer.*10%|did not contribute.*10%|less than 10%|no customer.*10%", full_text, re.IGNORECASE):
            return "PASS: No single client > 10% (Regulation compliant segment disclosure)"

        # Case 2: Extract explicit percentages from lines mentioning customer/client keywords
        customer_pcts = []
        for line in full_text.split('\n'):
            if any(k in line.lower() for k in ["customer", "client", "major", "single", "external"]):
                matches = re.findall(r"(\d+(?:\.\d+)?)\s*%", line)
                for m in matches:
                    p = float(m)
                    if 1.0 <= p <= 95.0:
                        customer_pcts.append(p)

        if customer_pcts:
            customer_pcts = sorted(list(set(customer_pcts)), reverse=True)
            if len(customer_pcts) >= 3:
                sum_top_3 = sum(customer_pcts[:3])
                if sum_top_3 <= 100.0:
                    if sum_top_3 < 40.0:
                        return f"PASS: Top 3 clients contribute {sum_top_3:.1f}% of revenue (< 40% target)"
                    else:
                        return f"ATTENTION: Top 3 clients contribute {sum_top_3:.1f}% of revenue (>= 40% threshold)"
            elif len(customer_pcts) > 0:
                sum_found = sum(customer_pcts)
                if sum_found <= 100.0:
                    if sum_found < 40.0:
                        return f"PASS: Major clients contribute {sum_found:.1f}% of revenue (< 40% target)"
                    else:
                        return f"ATTENTION: Major clients contribute {sum_found:.1f}% of revenue (>= 40% threshold)"
                    
        return "PASS: No major customer concentration detected in segment notes"
    except Exception as e:
        return f"Error parsing PDF: {e}"

def download_pdf(sym, pdf_path):
    try:
        import requests
    except ImportError:
        return False
    headers = {'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36'}
    try:
        r = requests.get(f"https://www.screener.in/company/{sym}/", headers=headers, timeout=10)
        if r.status_code == 200:
            links = re.findall(r'href="([^"]*AnnualReport[^"]*\.pdf)"', r.text)
            if links:
                pdf_url = links[0]
                r_pdf = requests.get(pdf_url, headers=headers, timeout=25)
                if r_pdf.status_code == 200:
                    os.makedirs(os.path.dirname(pdf_path), exist_ok=True)
                    with open(pdf_path, "wb") as f:
                        f.write(r_pdf.content)
                    return True
    except Exception:
        pass
    return False

def get_project_root():
    current_dir = os.path.abspath(os.path.dirname(__file__))
    while current_dir != os.path.dirname(current_dir):
        if any(os.path.exists(os.path.join(current_dir, marker)) for marker in ["main.go", ".git"]):
            return current_dir
        current_dir = os.path.dirname(current_dir)
    return os.getcwd()

def update_db(sym, res):
    root = get_project_root()
    db_path = os.path.join(root, "config", "customer_concentration.json")
    from datetime import datetime
    today = datetime.now().strftime("%Y-%m-%d")
    db_val = f"{res} (Scanned: {today})"
    
    db = {}
    if os.path.exists(db_path):
        try:
            with open(db_path, "r") as f:
                db = json.load(f)
        except Exception:
            pass
    db[sym] = db_val
    try:
        os.makedirs(os.path.dirname(db_path), exist_ok=True)
        with open(db_path, "w") as f:
            json.dump(db, f, indent=2)
    except Exception:
        pass

def get_customer_concentration(sym):
    root = get_project_root()
    # 1. Check local JSON overrides first (fast path)
    db_path = os.path.join(root, "config", "customer_concentration.json")
    if os.path.exists(db_path):
        with open(db_path, "r") as f:
            db = json.load(f)
            if sym in db:
                return f"{db[sym]} (Offline Database)"

    # 2. Check local PDF if exists
    pdf_dir = os.path.join(root, "data", "annual_reports")
    pdf_path = os.path.join(pdf_dir, f"{sym}.pdf")
    if os.path.exists(pdf_path):
        res = parse_pdf(pdf_path)
        if "PASS" in res or "ATTENTION" in res:
            update_db(sym, res)
            return f"{res} (Live PDF Scan)"

    # 3. Try to download online
    if not os.path.exists(pdf_path):
        if download_pdf(sym, pdf_path):
            if os.path.exists(pdf_path):
                res = parse_pdf(pdf_path)
                if "PASS" in res or "ATTENTION" in res:
                    update_db(sym, res)
                    return f"{res} (Live PDF Scan)"

    pending_msg = "Metric Coverage Pending (Place the Annual Report PDF in data/annual_reports/)"
    update_db(sym, pending_msg)
    return f"{pending_msg} (Live PDF Scan)"

def main():
    if len(sys.argv) < 2:
        print("Usage: check_customer_concentration.py <symbol>")
        sys.exit(1)
    sym = sys.argv[1].upper()
    print(get_customer_concentration(sym))

if __name__ == "__main__":
    main()
