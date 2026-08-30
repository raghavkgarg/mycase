#!/usr/bin/env python3
import sys
import os
import re
import json
import urllib.request
import urllib.parse
from html.parser import HTMLParser

class DDGSnippetParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.snippets = []
        self.in_snippet = False
        self.current_snippet = []

    def handle_starttag(self, tag, attrs):
        attrs_dict = dict(attrs)
        if tag == 'a' and 'result__snippet' in attrs_dict.get('class', ''):
            self.in_snippet = True
            self.current_snippet = []

    def handle_endtag(self, tag):
        if tag == 'a' and self.in_snippet:
            self.snippets.append(" ".join(self.current_snippet).strip())
            self.in_snippet = False

    def handle_data(self, data):
        if self.in_snippet:
            self.current_snippet.append(data)

def fetch_latest_cagr(sector):
    query = f"{sector} industry market size CAGR growth forecast"
    url = "https://html.duckduckgo.com/html/?" + urllib.parse.urlencode({'q': query})
    req = urllib.request.Request(
        url,
        headers={'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36'}
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            html = response.read().decode('utf-8')
            parser = DDGSnippetParser()
            parser.feed(html)
            
            cagr_candidates = []
            drivers = []
            
            # Pattern to match percentages like 15.4% or 8% near words like CAGR, grow, market
            pct_pattern = re.compile(r'(\d+(?:\.\d+)?)\s*%')
            
            for snip in parser.snippets:
                snip_clean = re.sub(r'\s+', ' ', snip)
                match = pct_pattern.search(snip_clean)
                if match:
                    try:
                        cagr_val = float(match.group(1))
                        # Filter for realistic CAGR bounds (2% to 45%)
                        if 2.0 <= cagr_val <= 45.0:
                            cagr_candidates.append(cagr_val)
                            # Get a short driver sentence
                            words = snip_clean.split()
                            short_driver = " ".join(words[:12]) + "..."
                            drivers.append((cagr_val, short_driver))
                    except ValueError:
                        continue
            
            if cagr_candidates:
                # Find the maximum or average CAGR found to be optimistic/realistic
                avg_cagr = sum(cagr_candidates) / len(cagr_candidates)
                best_cagr, best_driver = drivers[0]
                
                # Check if average passes threshold
                pass_label = "(> 15% Pass)" if avg_cagr >= 15.0 else ""
                
                return f"TAM Growth: ~{avg_cagr:.1f}% CAGR {pass_label} | {best_driver}"
    except Exception as e:
        print(f"Error fetching data for {sector}: {e}", file=sys.stderr)
    return None

def main():
    config_path = os.path.join("config", "sector_tam.json")
    if not os.path.exists(config_path):
        print(f"Error: {config_path} not found.", file=sys.stderr)
        sys.exit(1)
        
    with open(config_path, "r") as f:
        tam_db = json.load(f)
        
    updated = False
    for sector in list(tam_db.keys()):
        print(f"Updating CAGR forecast for sector: {sector}...")
        res = fetch_latest_cagr(sector)
        if res:
            tam_db[sector] = res
            updated = True
            print(f"  Result: {res}")
            
    if updated:
        with open(config_path, "w") as f:
            json.dump(tam_db, f, indent=2)
        print("Successfully updated sector_tam.json with latest CAGR forecasts.")
    else:
        print("No updates made.")

if __name__ == "__main__":
    main()
