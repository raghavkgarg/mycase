package schwab

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/marketdata"
)

// This file is the Schwab half of the raw-response field inspector
// (docs/roadmap.md Phase 11, R-store-5). The generic triage spine (cmd/raw.go +
// pkg/rawstore) is schema-blind — it can list and print any archived body but
// cannot say what a body *meant* to the production mapper. This inspector closes
// that gap for the one endpoint that motivated the whole feature: the Schwab
// /instruments?projection=fundamental response whose mis-parse left
// Fundamentals.MarketCap == 0 for every US ticker despite HTTP 200s (the
// `pick 0/N` bug).
//
// It lives here, with its source, because it must import the production mapper
// (mapSchwabFundamentals) and the wire structs (InstrumentResponse/Fundamental):
// the value of an inspector is showing the *raw wire field* next to *what
// production actually mapped it to*, using the real parser so the comparison is
// truthful. cmd/raw.go (the composition root, above all of pkg/) wires this to
// the archive; nothing in pkg/ imports both the L4 store and this L2 source.

// FieldComparison pairs one raw wire field with the mapped output it feeds, so a
// reader can see at a glance whether a zero (or wrong) mapped value originated on
// the wire or in the mapper. It is a plain data row the schema-blind spine
// renders without interpreting.
type FieldComparison struct {
	// MappedField is the marketdata.Fundamentals field name.
	MappedField string
	// WireKey is the JSON key on the Schwab fundamental object that feeds it
	// ("—" for purely-derived outputs like NetIncome/RegularPrice).
	WireKey string
	// WireValue is the value the struct actually bound from the wire (the value
	// mapSchwabFundamentals saw). Rendered as a string for schema-blindness.
	WireValue string
	// MappedValue is the resulting marketdata.Fundamentals value.
	MappedValue string
	// Note flags anything notable (e.g. a bound-zero, or a wire key present in
	// the JSON but under a different name than the struct expects).
	Note string
}

// FundamentalsInspection is the full result of inspecting one archived
// /instruments?projection=fundamental body. Everything is stringly-typed /
// primitive so the spine can render it without importing Schwab types.
type FundamentalsInspection struct {
	// Symbol is the instrument symbol from the wire (best-effort).
	Symbol string
	// InstrumentCount is len(instruments) in the envelope.
	InstrumentCount int
	// HasFundamental reports whether instruments[0].fundamental was present
	// (non-null). When false, FetchFundamentals skips the ticker entirely.
	HasFundamental bool
	// Diagnostics holds human-readable notes about the envelope / parse that
	// explain a downstream zero (empty instruments, null fundamental, unknown
	// wire keys, etc.).
	Diagnostics []string
	// Fields is the raw-vs-mapped comparison, one row per mapped output. Empty
	// when HasFundamental is false.
	Fields []FieldComparison
}

// InspectFundamentals parses an archived Schwab /instruments body exactly as
// production does (encoding/json → InstrumentResponse), reproduces the mapping
// via mapSchwabFundamentals, and reports the raw-vs-mapped comparison plus
// envelope diagnostics. It never mutates state and makes no network calls — it
// is meant to run against a recorded body offline.
//
// A parse error is returned only when the bytes are not valid JSON at all; an
// empty-instruments or null-fundamental envelope is a *successful* inspection
// that reports the condition via InstrumentCount/HasFundamental/Diagnostics
// (those are exactly the cases where FetchFundamentals silently skips a ticker,
// so surfacing them is the point).
func InspectFundamentals(body []byte) (*FundamentalsInspection, error) {
	var resp InstrumentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding schwab instruments body: %w", err)
	}

	insp := &FundamentalsInspection{InstrumentCount: len(resp.Instruments)}

	if len(resp.Instruments) == 0 {
		insp.Diagnostics = append(insp.Diagnostics,
			"envelope has zero instruments — FetchFundamentals skips this ticker (contributes to 0/N)")
		return insp, nil
	}

	inst := resp.Instruments[0]
	insp.Symbol = inst.Symbol
	if inst.Fundamental == nil {
		insp.HasFundamental = false
		insp.Diagnostics = append(insp.Diagnostics,
			"instruments[0].fundamental is null — FetchFundamentals skips this ticker (contributes to 0/N)")
		return insp, nil
	}
	insp.HasFundamental = true
	f := inst.Fundamental
	if insp.Symbol == "" {
		insp.Symbol = f.Symbol
	}

	// Detect the "wire key the struct didn't expect" failure mode: unmarshal the
	// same fundamental object into a free-form map and compare its keys to the
	// keys our struct knows. A key present on the wire but absent from the
	// struct means encoding/json silently dropped a value the mapper needed —
	// exactly how MarketCap can arrive nonzero yet bind to 0.
	unknown := detectUnknownWireKeys(body)
	if len(unknown) > 0 {
		insp.Diagnostics = append(insp.Diagnostics, fmt.Sprintf(
			"wire keys present on the fundamental object that our struct does NOT bind: %s "+
				"(encoding/json drops these silently — a likely cause of bound-zero fields)",
			strings.Join(unknown, ", ")))
	}

	mapped := mapSchwabFundamentals(f)
	insp.Fields = compareFields(f, mapped)

	// Headline diagnostic for the motivating bug.
	if f.MarketCap == 0 {
		insp.Diagnostics = append(insp.Diagnostics,
			"wire marketCap is 0 (or absent) → mapped MarketCap is 0 → size filter eliminates the ticker (the 0/N bug)")
	}

	return insp, nil
}

// compareFields builds the raw-vs-mapped rows for the 18 outputs the Schwab
// mapper populates. WireKey mirrors the json tag on Fundamental; keep this in
// sync with mapSchwabFundamentals.
func compareFields(f *Fundamental, m marketdata.Fundamentals) []FieldComparison {
	num := func(v float64) string { return fmt.Sprintf("%g", v) }
	rows := []FieldComparison{
		{MappedField: "MarketCap", WireKey: "marketCap", WireValue: num(f.MarketCap), MappedValue: num(m.MarketCap)},
		{MappedField: "TTMRevenue", WireKey: "revenueTTM", WireValue: num(f.RevenueTTM), MappedValue: num(m.TTMRevenue)},
		{MappedField: "PEGRatio", WireKey: "pegRatio", WireValue: num(f.PegRatio), MappedValue: num(m.PEGRatio)},
		{MappedField: "ForwardPE", WireKey: "peRatio", WireValue: num(f.PeRatio), MappedValue: num(m.ForwardPE)},
		{MappedField: "PBRatio", WireKey: "pbRatio", WireValue: num(f.PbRatio), MappedValue: num(m.PBRatio)},
		{MappedField: "ROE", WireKey: "returnOnEquity", WireValue: num(f.ReturnOnEquity), MappedValue: num(m.ROE)},
		{MappedField: "ReturnOnAssets", WireKey: "returnOnAssets", WireValue: num(f.ReturnOnAssets), MappedValue: num(m.ReturnOnAssets)},
		{MappedField: "OperatingMargins", WireKey: "operatingMarginTTM", WireValue: num(f.OperatingMarginTTM), MappedValue: num(m.OperatingMargins)},
		{MappedField: "NetProfitMargin", WireKey: "netProfitMarginTTM", WireValue: num(f.NetProfitMarginTTM), MappedValue: num(m.NetProfitMargin)},
		{MappedField: "GrossMarginTTM", WireKey: "grossMarginTTM", WireValue: num(f.GrossMarginTTM), MappedValue: num(m.GrossMarginTTM)},
		{MappedField: "DividendYield", WireKey: "divYield", WireValue: num(f.DivYield), MappedValue: num(m.DividendYield)},
		{MappedField: "DebtToEquity", WireKey: "totalDebtToEquity", WireValue: num(f.TotalDebtToEquity), MappedValue: num(m.DebtToEquity)},
		{MappedField: "Beta", WireKey: "beta", WireValue: num(f.Beta), MappedValue: num(m.Beta)},
		{MappedField: "AverageVolume", WireKey: "avg3MonthVolume", WireValue: num(f.Avg3MonthVolume), MappedValue: num(m.AverageVolume)},
		{MappedField: "FreeCashflow", WireKey: "freeCashFlowPerShare×sharesOutstanding", WireValue: num(f.FreeCashFlowPerShare), MappedValue: num(m.FreeCashflow)},
		{MappedField: "NetIncome", WireKey: "— (netProfitMarginTTM×revenueTTM)", WireValue: "—", MappedValue: num(m.NetIncome)},
		{MappedField: "RegularPrice", WireKey: "— (marketCap÷sharesOutstanding)", WireValue: num(f.SharesOutstanding), MappedValue: num(m.RegularPrice)},
		{MappedField: "Sector", WireKey: "— (injected from CSV downstream)", WireValue: "—", MappedValue: quoteOrDash(m.Sector)},
	}
	for i := range rows {
		if rows[i].WireValue == "0" && rows[i].MappedValue == "0" {
			rows[i].Note = "bound zero"
		}
	}
	return rows
}

// detectUnknownWireKeys returns the JSON keys on instruments[0].fundamental that
// the Fundamental struct does not declare, sorted for stable output. This is how
// a renamed/extra wire key (which encoding/json drops) is surfaced.
func detectUnknownWireKeys(body []byte) []string {
	var envelope struct {
		Instruments []struct {
			Fundamental map[string]json.RawMessage `json:"fundamental"`
		} `json:"instruments"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}
	if len(envelope.Instruments) == 0 || envelope.Instruments[0].Fundamental == nil {
		return nil
	}
	known := knownFundamentalKeys()
	var unknown []string
	for k := range envelope.Instruments[0].Fundamental {
		if _, ok := known[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// knownFundamentalKeys is the set of json tags the Fundamental struct binds.
// Kept beside the struct so it stays in sync.
func knownFundamentalKeys() map[string]struct{} {
	keys := []string{
		"symbol", "marketCap", "peRatio", "pegRatio", "pbRatio", "divYield",
		"returnOnEquity", "returnOnAssets", "operatingMarginTTM", "netProfitMarginTTM",
		"grossMarginTTM", "quickRatio", "currentRatio", "debtToCapital",
		"totalDebtToEquity", "epsTTM", "revenueTTM", "vol10DayAvg", "vol3MonthAvg",
		"avg3MonthVolume",
		"beta", "sharesOutstanding", "bookValuePerShare", "freeCashFlowPerShare",
	}
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}

func quoteOrDash(s string) string {
	if s == "" {
		return `"" (empty)`
	}
	return s
}
