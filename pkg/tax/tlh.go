package tax

import (
	"sort"
	"time"
)

// HarvestCandidate describes a position (or lot) whose unrealized loss can be
// harvested for a tax deduction. Harvesting means selling to realize the loss,
// then optionally replacing with a correlated (but not "substantially
// identical") substitute to maintain market exposure without triggering the
// wash-sale rule.
type HarvestCandidate struct {
	Ticker         string
	Sector         string
	Quantity       float64   // shares available to harvest at a loss
	CostBasis      float64   // total cost basis of the loss-making shares
	MarketValue    float64   // current market value of those shares
	UnrealizedLoss float64   // negative number: MarketValue - CostBasis
	LongTerm       bool      // true if the loss-making lots are long-term
	OldestAcquired time.Time // earliest acquisition among the loss lots
	WashSaleRisk   bool      // true if a buy occurred within 30 days of today
	EstTaxSaving   float64   // positive: estimated federal tax reduction
	Substitute     string    // suggested replacement ticker (same sector), if any
	Note           string
}

// HarvestParams configures loss-harvesting analysis.
type HarvestParams struct {
	// AsOf is the valuation date (defaults to time.Now() if zero).
	AsOf time.Time
	// MinLoss is the minimum absolute unrealized loss (USD) to bother
	// harvesting. Small losses aren't worth the transaction friction.
	MinLoss float64
	// STCGRate / LTCGRate are the marginal tax rates used to estimate savings.
	STCGRate float64
	// LTCGRate is the long-term capital gains rate.
	LTCGRate float64
	// RecentBuys maps ticker → most recent BUY date, used for wash-sale detection.
	RecentBuys map[string]time.Time
	// Sectors maps ticker → sector, used to suggest substitutes.
	Sectors map[string]string
}

// DefaultHarvestParams returns sensible defaults matching the US cost model.
func DefaultHarvestParams() HarvestParams {
	return HarvestParams{
		AsOf:     time.Now(),
		MinLoss:  50.0,
		STCGRate: 0.37,
		LTCGRate: 0.20,
	}
}

// FindHarvestCandidates scans open lots and current prices to identify
// loss-making positions worth harvesting. openLots is typically
// FIFOResult.OpenLots; prices maps ticker → current price. Universe is the set
// of tickers eligible as substitutes (e.g. the current index constituents),
// used to suggest a same-sector replacement that avoids the wash-sale rule.
func FindHarvestCandidates(openLots map[string][]Lot, prices map[string]float64, universe []string, p HarvestParams) []HarvestCandidate {
	if p.AsOf.IsZero() {
		p.AsOf = time.Now()
	}
	if p.STCGRate == 0 {
		p.STCGRate = 0.37
	}
	if p.LTCGRate == 0 {
		p.LTCGRate = 0.20
	}

	var candidates []HarvestCandidate

	for ticker, lots := range openLots {
		price, ok := prices[ticker]
		if !ok || price <= 0 {
			continue // no price → cannot value the loss
		}

		// Aggregate only the loss-making shares. A position can hold both
		// winning and losing lots; we only harvest the losers.
		var lossQty, lossCost, lossValue float64
		var oldest time.Time
		anyLongTerm := false
		for _, lot := range lots {
			lotValue := lot.Quantity * price
			lotCost := lot.CostBasis()
			if lotValue >= lotCost {
				continue // this lot is at a gain — skip
			}
			lossQty += lot.Quantity
			lossCost += lotCost
			lossValue += lotValue
			if oldest.IsZero() || lot.AcquiredAt.Before(oldest) {
				oldest = lot.AcquiredAt
			}
			if lot.IsLongTerm(p.AsOf) {
				anyLongTerm = true
			}
		}

		if lossQty <= epsilon {
			continue
		}
		unrealizedLoss := lossValue - lossCost // negative
		if -unrealizedLoss < p.MinLoss {
			continue // loss too small to bother
		}

		washSale := false
		if buyDate, exists := p.RecentBuys[ticker]; exists && !buyDate.IsZero() {
			if daysBetween(buyDate, p.AsOf) <= washSaleDays {
				washSale = true
			}
		}

		rate := p.STCGRate
		if anyLongTerm {
			rate = p.LTCGRate
		}
		// Tax saving = magnitude of loss × applicable rate.
		estSaving := -unrealizedLoss * rate

		sector := ""
		if p.Sectors != nil {
			sector = p.Sectors[ticker]
		}

		c := HarvestCandidate{
			Ticker:         ticker,
			Sector:         sector,
			Quantity:       lossQty,
			CostBasis:      lossCost,
			MarketValue:    lossValue,
			UnrealizedLoss: unrealizedLoss,
			LongTerm:       anyLongTerm,
			OldestAcquired: oldest,
			WashSaleRisk:   washSale,
			EstTaxSaving:   estSaving,
			Substitute:     suggestSubstitute(ticker, sector, universe, p.Sectors, openLots),
		}
		c.Note = harvestNote(c)
		candidates = append(candidates, c)
	}

	// Largest tax saving first.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].EstTaxSaving > candidates[j].EstTaxSaving
	})
	return candidates
}

// suggestSubstitute picks a same-sector ticker from the universe that is not the
// harvested ticker and not already held, to preserve factor exposure without a
// wash sale. Returns "" if no suitable substitute is found.
func suggestSubstitute(ticker, sector string, universe []string, sectors map[string]string, held map[string][]Lot) string {
	if sector == "" || sectors == nil {
		return ""
	}
	for _, cand := range universe {
		if cand == ticker {
			continue
		}
		if sectors[cand] != sector {
			continue
		}
		// Avoid suggesting something already held (would concentrate, and if
		// recently bought could itself be a wash-sale concern).
		if _, isHeld := held[cand]; isHeld {
			continue
		}
		return cand
	}
	return ""
}

// WashSaleViolation describes a detected or potential wash-sale rule breach: a
// loss-generating sell paired with a buy of the same security within ±30 days.
type WashSaleViolation struct {
	Ticker    string
	SellDate  time.Time
	BuyDate   time.Time
	DaysApart int
	Note      string
}

// DetectWashSales scans realized losses against buy transactions and flags any
// where a buy of the same ticker occurred within 30 days before or after the
// loss sale. This catches violations in already-executed history (for
// reporting) as well as planned buys.
func DetectWashSales(realized []RealizedGain, buys []Transaction) []WashSaleViolation {
	// Index buy dates per ticker.
	buyDates := make(map[string][]time.Time)
	for _, b := range buys {
		if b.Type != TxnBuy {
			continue
		}
		buyDates[b.Ticker] = append(buyDates[b.Ticker], b.TradedAt)
	}

	var violations []WashSaleViolation
	for _, r := range realized {
		if r.Gain >= 0 {
			continue // only losses trigger wash-sale disallowance
		}
		for _, bd := range buyDates[r.Ticker] {
			d := daysBetween(bd, r.SoldAt)
			if d <= washSaleDays {
				violations = append(violations, WashSaleViolation{
					Ticker:    r.Ticker,
					SellDate:  r.SoldAt,
					BuyDate:   bd,
					DaysApart: d,
					Note:      "loss may be disallowed: bought within 30 days of loss sale",
				})
				break
			}
		}
	}
	return violations
}

// RealizedSummary aggregates realized gains for a period (e.g. YTD).
type RealizedSummary struct {
	ShortTermGain float64
	LongTermGain  float64
	TotalGain     float64
	ShortTermLoss float64 // negative
	LongTermLoss  float64 // negative
	NetShortTerm  float64
	NetLongTerm   float64
	NetTotal      float64
	Count         int
}

// SummarizeRealized aggregates realized gains that occurred on/after `since`
// (zero `since` includes everything).
func SummarizeRealized(realized []RealizedGain, since time.Time) RealizedSummary {
	var s RealizedSummary
	for _, r := range realized {
		if !since.IsZero() && r.SoldAt.Before(since) {
			continue
		}
		s.Count++
		switch {
		case r.LongTerm && r.Gain >= 0:
			s.LongTermGain += r.Gain
		case r.LongTerm && r.Gain < 0:
			s.LongTermLoss += r.Gain
		case !r.LongTerm && r.Gain >= 0:
			s.ShortTermGain += r.Gain
		default:
			s.ShortTermLoss += r.Gain
		}
	}
	s.NetShortTerm = s.ShortTermGain + s.ShortTermLoss
	s.NetLongTerm = s.LongTermGain + s.LongTermLoss
	s.TotalGain = s.ShortTermGain + s.LongTermGain
	s.NetTotal = s.NetShortTerm + s.NetLongTerm
	return s
}

func daysBetween(a, b time.Time) int {
	d := b.Sub(a)
	if d < 0 {
		d = -d
	}
	return int(d.Hours() / 24)
}

func harvestNote(c HarvestCandidate) string {
	term := "short-term"
	if c.LongTerm {
		term = "long-term"
	}
	note := c.Ticker + ": " + term + " loss of $" + fmtUSD(-c.UnrealizedLoss) +
		" — est. tax saving $" + fmtUSD(c.EstTaxSaving)
	if c.Substitute != "" {
		note += "; substitute → " + c.Substitute
	}
	if c.WashSaleRisk {
		note += " ⚠️ WASH SALE RISK: bought within 30 days"
	}
	return note
}
