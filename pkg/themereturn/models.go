package themereturn

import (
	"time"
)

// DatedCashFlow represents a single dated inflow or outflow.
type DatedCashFlow struct {
	Date   time.Time `json:"date"`
	Amount float64   `json:"amount"` // negative for buy/outflow, positive for sell/dividend/terminal valuation
}

// ScriptReturn captures audited metrics for an individual stock.
type ScriptReturn struct {
	Symbol           string          `json:"symbol"`
	Exchange         string          `json:"exchange"`
	IsOpen           bool            `json:"is_open"`
	Quantity         int             `json:"quantity"`
	AvgBuyPrice      float64         `json:"avg_buy_price"`
	CurrentPrice     float64         `json:"current_price"`
	InvestedValue    float64         `json:"invested_value"`
	CurrentValue     float64         `json:"current_value"`
	UnrealizedPnL    float64         `json:"unrealized_pnl"`
	UnrealizedPnLPct float64         `json:"unrealized_pnl_pct"`
	RealizedGain     float64         `json:"realized_gain"`
	Dividends        float64         `json:"dividends"`
	TotalWealth      float64         `json:"total_wealth"`
	HoldingDays      int             `json:"holding_days"`
	HPR              float64         `json:"hpr_pct"`
	MWR              float64         `json:"mwr_pct"`
	HasMWR           bool            `json:"has_mwr"`
	FirstBuyDate     time.Time       `json:"first_buy_date"`
	LastTradeDate    time.Time       `json:"last_trade_date"`
	Tranches         []TrancheDetail `json:"tranches"`
}

// TrancheDetail represents a specific fill.
type TrancheDetail struct {
	TradeDate time.Time `json:"trade_date"`
	TradeType string    `json:"trade_type"`
	Quantity  int       `json:"quantity"`
	Price     float64   `json:"price"`
	Charges   float64   `json:"charges"`
}

// ExitedPositionReturn captures a historical holding that has been fully liquidated.
type ExitedPositionReturn struct {
	Symbol       string    `json:"symbol"`
	QuantitySold int       `json:"quantity_sold"`
	BuyOutflow   float64   `json:"buy_outflow"`
	SellInflow   float64   `json:"sell_inflow"`
	RealizedGain float64   `json:"realized_gain"`
	GainPct      float64   `json:"gain_pct"`
	Dividends    float64   `json:"dividends"`
	TotalProfit  float64   `json:"total_profit"`
	FirstBuyDate time.Time `json:"first_buy_date"`
	ExitDate     time.Time `json:"exit_date"`
	HoldingDays  int       `json:"holding_days"`
}

// ThemeReturnReport encapsulates complete return intelligence for a theme.
type ThemeReturnReport struct {
	ThemeName        string    `json:"theme_name"`
	Prefix           string    `json:"prefix"`
	AccountID        string    `json:"account_id"`
	EvaluationDate   time.Time `json:"evaluation_date"`
	EarliestPurchase time.Time `json:"earliest_purchase"`
	HoldingDays      int       `json:"holding_days"`

	// Active Core Holdings View
	ActiveInvestedValue float64         `json:"active_invested_value"`
	ActiveCurrentValue  float64         `json:"active_current_value"`
	ActiveUnrealizedPnL float64         `json:"active_unrealized_pnl"`
	ActiveUnrealizedPct float64         `json:"active_unrealized_pct"`
	ActiveDividends     float64         `json:"active_dividends"`
	ActiveTotalWealth   float64         `json:"active_total_wealth"`
	ActiveHPR           float64         `json:"active_hpr_pct"`
	ActiveHPRAnn        float64         `json:"active_hpr_ann_pct"`
	ActiveMWR           float64         `json:"active_mwr_xirr_pct"`
	ActiveTWR           float64         `json:"active_twr_pct"`
	ActiveTWRAnn        float64         `json:"active_twr_ann_pct"`
	ActivePositions     []*ScriptReturn `json:"active_positions"`

	// Full Lifecycle View (Active + Exited Rebalanced)
	LifecycleGrossBuys     float64                 `json:"lifecycle_gross_buys"`
	LifecycleGrossSells    float64                 `json:"lifecycle_gross_sells"`
	LifecycleNetOutlay     float64                 `json:"lifecycle_net_outlay"`
	LifecycleRealizedGain  float64                 `json:"lifecycle_realized_gain"`
	LifecycleDividends     float64                 `json:"lifecycle_dividends"`
	LifecycleTotalWealth   float64                 `json:"lifecycle_total_wealth"`
	LifecycleHPR           float64                 `json:"lifecycle_hpr_pct"`
	LifecycleHPRAnn        float64                 `json:"lifecycle_hpr_ann_pct"`
	LifecycleMWR           float64                 `json:"lifecycle_mwr_xirr_pct"`
	LifecycleTWR           float64                 `json:"lifecycle_twr_pct"`
	LifecycleTWRAnn        float64                 `json:"lifecycle_twr_ann_pct"`
	ExitedPositions        []*ExitedPositionReturn `json:"exited_positions"`

	// Benchmark Analytics
	BenchmarkName  string  `json:"benchmark_name"`
	BenchmarkStart float64 `json:"benchmark_start"`
	BenchmarkEnd   float64 `json:"benchmark_end"`
	BenchmarkTWR   float64 `json:"benchmark_twr_pct"`
	BenchmarkAlpha float64 `json:"benchmark_alpha_pct"` // Theme TWR - Benchmark TWR
}

// ReturnOptions controls computation and filtering.
type ThemeReturnOptions struct {
	ThemeName        string
	CSVPath          string
	AccountID        string
	PortfolioDBPath  string
	Benchmark        string
	IncludeLifecycle bool
	Detail           bool
	Live             bool
}
