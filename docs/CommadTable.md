# Mycase Command Reference & Configuration Mapping Table

This document provides a complete lookup reference mapping individual standalone CLI commands to their Go automated pipeline runner equivalents. 

It highlights the exact parameters that must be aligned to ensure standalone command executions yield identical portfolio selections and weights as the pipeline runner.

---

## 1. Parameter Configuration Source
The automated pipeline runner (`cmd/pipeline`) reads parameters from [config/pipeline.yaml](file:///Users/raghavgarg/Projects/myGo/mycase/config/pipeline.yaml). If a parameter is missing from the YAML file, it falls back to predefined default values inside the code. 

When executing standalone CLI commands, ensure the parameters match these configurations.

| pipeline.yaml Key | Description | Standalone CLI Flag | Code Default Fallback |
| :--- | :--- | :--- | :--- |
| `indices` | List of stock indices to scan | `-index [index_name]` | `["smallcap250"]` |
| `strategy` | Strategy scoring preset | `-method [strategy_name]` | `"balanced"` |
| `top_n` | Target number of top stocks to select | `-top [count]` | `20` |
| `golden_copy_path` | Target CSV portfolio file | `-golden [file_path]` | `"data/microsmall.csv"` |
| `rebalance_tolerance_pct` | Rebalancing weight tolerance drift percentage | `-rebalance-tolerance [value]` | `0.10` (0.10%) |
| `hysteresis_rank_buffer` | Ranks allowed to drift before exit | `-hysteresis-buffer [count]` | `5` |
| `capital` | Investment capital for simulation | `-capital [value]` | `100000.0` |
| `purchase_date` | Backtest/buy start date | `-date [YYYY-MM-DD]` | `"2026-01-01"` |

---

## 2. Step-by-Step Command Mapping Table

| Step | Goal / Action | Standalone CLI Command | Pipeline runner Behavior / Equivalence | Mapped pipeline.yaml Key | Required Alignment Parameter |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **1** | **Scan Microcap Index** | `./dist/mycase pick -index microcap250 -method balanced -top 20 -skip-scuttlebutt -golden data/microsmall.csv -rebalance-tolerance 0.10 -hysteresis-buffer 2` | Runs automatically for the configured index | `indices`, `strategy`, `top_n`, `golden_copy_path`, `rebalance_tolerance_pct`, `hysteresis_rank_buffer` | `-index`, `-method`, `-top`, `-golden`, `-rebalance-tolerance`, `-hysteresis-buffer`, `-skip-scuttlebutt` (skipped dynamically if multiple indices are run) |
| **2** | **Scan Smallcap Index** | `./dist/mycase pick -index small250 -method balanced -top 20 -skip-scuttlebutt -golden data/microsmall.csv -rebalance-tolerance 0.10 -hysteresis-buffer 2` | Runs automatically for the configured index | `indices`, `strategy`, `top_n`, `golden_copy_path`, `rebalance_tolerance_pct`, `hysteresis_rank_buffer` | `-index`, `-method`, `-top`, `-golden`, `-rebalance-tolerance`, `-hysteresis-buffer`, `-skip-scuttlebutt` (skipped dynamically if multiple indices are run) |
| **3** | **Merge Candidates** | `./dist/mycase merge combine data/candidates/temp/combine_microsmall.csv data/candidates/index_picks/microcap250_balanced.csv data/candidates/index_picks/small250_balanced.csv` | Automatically combines constituent CSV outputs | None (Implicit helper) | Source and target files |
| **4** | **Draft N+5 Proposal** | `./dist/mycase pick -file data/candidates/temp/combine_microsmall.csv -method balanced -top 25 -golden data/microsmall.csv -name microsmall -rebalance-tolerance 0.10 -hysteresis-buffer 2` | Automatically generates `YYYYMMDD_microsmall_balanced.csv` | `strategy`, `golden_copy_path`, `rebalance_tolerance_pct`, `hysteresis_rank_buffer` | `-file`, `-method`, `-top` (set to `top_n + 5`), `-golden`, `-name` |
| **5** | **Curation (Manual)** | Manually open the generated CSV and delete unwanted stock rows; save file. | Pauses with interactive prompt: `Would you like to manually remove shares...` | None (Interactive choice) | Interactive user selection |
| **6** | **Prune and Optimize** | `./dist/mycase pick -file data/candidates/proposals/YYYYMMDD_microsmall_balanced.csv -method balanced -top 20 -golden data/microsmall.csv -name microsmall -rebalance-tolerance 0.10 -hysteresis-buffer 2 -out data/candidates/proposals/YYYYMMDD_microsmall_balanced_optim.csv` | Automatically prunes the curated list and optimizes weights | `strategy`, `top_n`, `golden_copy_path`, `rebalance_tolerance_pct`, `hysteresis_rank_buffer` | `-file`, `-method`, `-top` (restored to target `top_n`), `-golden`, `-name`, `-out` |
| **7** | **Update Golden Copy** | `./dist/mycase merge data/candidates/proposals/YYYYMMDD_microsmall_balanced_optim.csv data/microsmall.csv` | Interactive prompt: `Would you like to update the golden copy...` | `golden_copy_path` | Source path and destination path |
| **8** | **Generate Reports** | `./dist/mycase report -file data/microsmall.csv -method balanced` | Automatically runs the report tool | `golden_copy_path`, `strategy` | `-file`, `-method` |
| **9** | **Historical Backtest** | `./dist/mycase performance -file data/microsmall.csv -capital 100000 -date 2026-01-01 -time 09:30` | Interactive prompt: `Enter capital... Enter purchase date...` | `golden_copy_path`, `capital`, `purchase_date` | `-file`, `-capital`, `-date`, `-time` |
| **10**| **Monitor & Alert Drift** | `./dist/mycase monitor -file data/microsmall.csv -interactive -strategy balanced -date 2026-01-01` | Automatically runs monitoring tool (prompts timeframe match) | `golden_copy_path`, `strategy`, `purchase_date` | `-file`, `-strategy`, `-date` |
| **11**| **Broker Login** | `./dist/mycase auth` | Interactive prompt: `Would you like to setup authentication now?` | None (Implicit helper) | kiteconnect auth session setup |
| **12**| **Rebalance Order** | `./dist/mycase basket --live -- microsmall` | Interactive prompt: `Would you like to execute basket orders?` | `golden_copy_path` (Used to determine the name key) | `--live` (live rebalance trigger), `-- [name]` |
| **13**| **Holdings Check** | `./dist/mycase holdings --live` | (Optional manual step outside the pipeline runner flow) | None (Implicit helper) | `--live` (live holdings print) |

