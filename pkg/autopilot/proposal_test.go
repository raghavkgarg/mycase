package autopilot

import (
	"math"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/tax"
)

func TestFinalStageProposals(t *testing.T) {
	tests := []struct {
		name    string
		p       *Proposal
		want    map[string]float64 // ticker -> weight (empty => expect nil result)
		wantLen int
	}{
		{
			name: "buys only, weighted by executed value",
			p: &Proposal{
				Orders: []ProposedOrder{
					{Ticker: "AAPL", Action: "BUY", Value: 6000},
					{Ticker: "MSFT", Action: "BUY", Value: 4000},
				},
				ExecutionLog: []OrderResult{
					{Ticker: "AAPL", Action: "BUY", Success: true},
					{Ticker: "MSFT", Action: "BUY", Success: true},
				},
			},
			want:    map[string]float64{"AAPL": 0.6, "MSFT": 0.4},
			wantLen: 2,
		},
		{
			name: "failed order excluded from final basket",
			p: &Proposal{
				Orders: []ProposedOrder{
					{Ticker: "AAPL", Action: "BUY", Value: 6000},
					{Ticker: "MSFT", Action: "BUY", Value: 4000},
				},
				ExecutionLog: []OrderResult{
					{Ticker: "AAPL", Action: "BUY", Success: true},
					{Ticker: "MSFT", Action: "BUY", Success: false, Error: "rejected"},
				},
			},
			// MSFT dropped; AAPL is 100% of the executed BUY value.
			want:    map[string]float64{"AAPL": 1.0},
			wantLen: 1,
		},
		{
			name: "sell orders carry no weight",
			p: &Proposal{
				Orders: []ProposedOrder{
					{Ticker: "AAPL", Action: "BUY", Value: 5000},
					{Ticker: "OLD", Action: "SELL", Value: 3000},
				},
				ExecutionLog: []OrderResult{
					{Ticker: "AAPL", Action: "BUY", Success: true},
					{Ticker: "OLD", Action: "SELL", Success: true},
				},
			},
			want:    map[string]float64{"AAPL": 1.0},
			wantLen: 1,
		},
		{
			name: "no successful buys returns nil",
			p: &Proposal{
				Orders: []ProposedOrder{
					{Ticker: "AAPL", Action: "BUY", Value: 5000},
				},
				ExecutionLog: []OrderResult{
					{Ticker: "AAPL", Action: "BUY", Success: false},
				},
			},
			wantLen: 0,
		},
		{
			name:    "nil proposal returns nil",
			p:       nil,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FinalStageProposals(tt.p)
			if len(got) != tt.wantLen {
				t.Fatalf("FinalStageProposals() len = %d, want %d (%+v)", len(got), tt.wantLen, got)
			}
			if tt.wantLen == 0 {
				return
			}
			var sum float64
			for i, p := range got {
				if p.Rank != i+1 {
					t.Errorf("proposal %d rank = %d, want %d", i, p.Rank, i+1)
				}
				want, ok := tt.want[p.Ticker]
				if !ok {
					t.Errorf("unexpected ticker %q in result", p.Ticker)
					continue
				}
				if math.Abs(p.Weight-want) > 1e-9 {
					t.Errorf("%s weight = %.6f, want %.6f", p.Ticker, p.Weight, want)
				}
				sum += p.Weight
			}
			if math.Abs(sum-1.0) > 1e-9 {
				t.Errorf("weights sum = %.6f, want 1.0", sum)
			}
		})
	}
}

func TestRealizedStageProposals(t *testing.T) {
	base := time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)
	from := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC)

	txn := func(ticker string, typ tax.TxnType, qty, price float64, at time.Time) tax.Transaction {
		return tax.Transaction{Ticker: ticker, Type: typ, Quantity: qty, Price: price, TradedAt: at}
	}

	tests := []struct {
		name    string
		txns    []tax.Transaction
		want    map[string]float64
		wantLen int
		// wantOrder asserts the descending-weight rank ordering.
		wantOrder []string
	}{
		{
			name: "buys weighted by realized value, ranked descending",
			txns: []tax.Transaction{
				txn("US:AAPL", tax.TxnBuy, 10, 200, base), // 2000
				txn("US:MSFT", tax.TxnBuy, 10, 300, base), // 3000
			},
			want:      map[string]float64{"US:AAPL": 0.4, "US:MSFT": 0.6},
			wantLen:   2,
			wantOrder: []string{"US:MSFT", "US:AAPL"},
		},
		{
			name: "sells contribute no weight",
			txns: []tax.Transaction{
				txn("US:AAPL", tax.TxnBuy, 10, 500, base), // 5000
				txn("US:OLD", tax.TxnSell, 10, 300, base), // ignored
			},
			want:      map[string]float64{"US:AAPL": 1.0},
			wantLen:   1,
			wantOrder: []string{"US:AAPL"},
		},
		{
			name: "multiple fills for same ticker aggregate",
			txns: []tax.Transaction{
				txn("US:AAPL", tax.TxnBuy, 5, 200, base),                // 1000
				txn("US:AAPL", tax.TxnBuy, 5, 200, base.Add(time.Hour)), // 1000
				txn("US:MSFT", tax.TxnBuy, 10, 200, base),               // 2000
			},
			want:      map[string]float64{"US:AAPL": 0.5, "US:MSFT": 0.5},
			wantLen:   2,
			wantOrder: []string{"US:AAPL", "US:MSFT"}, // tie → ticker asc
		},
		{
			name: "fills outside the window are excluded",
			txns: []tax.Transaction{
				txn("US:AAPL", tax.TxnBuy, 10, 200, base),                  // in window
				txn("US:OLD", tax.TxnBuy, 10, 999, from.AddDate(0, 0, -1)), // before from
				txn("US:NEW", tax.TxnBuy, 10, 999, to),                     // == to (exclusive)
			},
			want:      map[string]float64{"US:AAPL": 1.0},
			wantLen:   1,
			wantOrder: []string{"US:AAPL"},
		},
		{
			name:    "no buys in window returns nil",
			txns:    []tax.Transaction{txn("US:OLD", tax.TxnSell, 10, 200, base)},
			wantLen: 0,
		},
		{
			name:    "empty input returns nil",
			txns:    nil,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RealizedStageProposals(tt.txns, from, to)
			if len(got) != tt.wantLen {
				t.Fatalf("RealizedStageProposals() len = %d, want %d (%+v)", len(got), tt.wantLen, got)
			}
			if tt.wantLen == 0 {
				return
			}
			var sum float64
			for i, p := range got {
				if p.Rank != i+1 {
					t.Errorf("proposal %d rank = %d, want %d", i, p.Rank, i+1)
				}
				if tt.wantOrder != nil && p.Ticker != tt.wantOrder[i] {
					t.Errorf("rank %d ticker = %q, want %q", i+1, p.Ticker, tt.wantOrder[i])
				}
				if w, ok := tt.want[p.Ticker]; ok {
					if math.Abs(p.Weight-w) > 1e-9 {
						t.Errorf("%s weight = %.6f, want %.6f", p.Ticker, p.Weight, w)
					}
				} else {
					t.Errorf("unexpected ticker %q", p.Ticker)
				}
				sum += p.Weight
			}
			if math.Abs(sum-1.0) > 1e-9 {
				t.Errorf("weights sum = %.6f, want 1.0", sum)
			}
		})
	}
}
