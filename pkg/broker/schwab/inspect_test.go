package schwab

import (
	"strings"
	"testing"
)

// findField returns the comparison row for a mapped field name.
func findField(t *testing.T, fields []FieldComparison, name string) FieldComparison {
	t.Helper()
	for _, f := range fields {
		if f.MappedField == name {
			return f
		}
	}
	t.Fatalf("no comparison row for %q", name)
	return FieldComparison{}
}

func hasDiagnostic(diags []string, substr string) bool {
	for _, d := range diags {
		if strings.Contains(d, substr) {
			return true
		}
	}
	return false
}

func TestInspectFundamentals_Healthy(t *testing.T) {
	body := []byte(`{"instruments":[{"symbol":"AAPL","fundamental":{
		"symbol":"AAPL","marketCap":3000000,"revenueTTM":400000000000,
		"netProfitMarginTTM":25,"sharesOutstanding":15000000000,"peRatio":30,
		"returnOnEquity":150,"beta":1.2}}]}`)

	insp, err := InspectFundamentals(body)
	if err != nil {
		t.Fatalf("InspectFundamentals err: %v", err)
	}
	if insp.InstrumentCount != 1 || !insp.HasFundamental {
		t.Fatalf("envelope = count %d, hasFund %v", insp.InstrumentCount, insp.HasFundamental)
	}
	if insp.Symbol != "AAPL" {
		t.Errorf("symbol = %q, want AAPL", insp.Symbol)
	}

	// marketCap 3,000,000 (millions) → 3e12 mapped.
	mc := findField(t, insp.Fields, "MarketCap")
	if mc.WireValue != "3e+06" {
		t.Errorf("MarketCap wire = %q, want 3e+06", mc.WireValue)
	}
	if mc.MappedValue != "3e+12" {
		t.Errorf("MarketCap mapped = %q, want 3e+12", mc.MappedValue)
	}

	// ROE: wire 150 (%) → 1.5 mapped.
	roe := findField(t, insp.Fields, "ROE")
	if roe.MappedValue != "1.5" {
		t.Errorf("ROE mapped = %q, want 1.5", roe.MappedValue)
	}

	if hasDiagnostic(insp.Diagnostics, "marketCap is 0") {
		t.Error("healthy body should not flag marketCap-zero")
	}
}

func TestInspectFundamentals_ZeroMarketCap(t *testing.T) {
	// marketCap absent → binds 0 → the 0/N bug signature.
	body := []byte(`{"instruments":[{"symbol":"AAPL","fundamental":{
		"symbol":"AAPL","revenueTTM":400000000000,"peRatio":30}}]}`)

	insp, err := InspectFundamentals(body)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	mc := findField(t, insp.Fields, "MarketCap")
	if mc.WireValue != "0" || mc.MappedValue != "0" {
		t.Errorf("MarketCap = wire %q mapped %q, want 0/0", mc.WireValue, mc.MappedValue)
	}
	if mc.Note != "bound zero" {
		t.Errorf("MarketCap note = %q, want 'bound zero'", mc.Note)
	}
	if !hasDiagnostic(insp.Diagnostics, "the 0/N bug") {
		t.Errorf("expected 0/N diagnostic, got %v", insp.Diagnostics)
	}
}

func TestInspectFundamentals_UnknownWireKey(t *testing.T) {
	// The wire carries market cap under a *different* key than the struct binds.
	// encoding/json drops it → MarketCap binds 0 despite a value on the wire.
	body := []byte(`{"instruments":[{"symbol":"AAPL","fundamental":{
		"symbol":"AAPL","marketCapFloat":3000000,"revenueTTM":400000000000}}]}`)

	insp, err := InspectFundamentals(body)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	mc := findField(t, insp.Fields, "MarketCap")
	if mc.MappedValue != "0" {
		t.Errorf("MarketCap mapped = %q, want 0 (renamed wire key dropped)", mc.MappedValue)
	}
	if !hasDiagnostic(insp.Diagnostics, "marketCapFloat") {
		t.Errorf("expected unknown-wire-key diagnostic naming marketCapFloat, got %v", insp.Diagnostics)
	}
	if !hasDiagnostic(insp.Diagnostics, "does NOT bind") {
		t.Errorf("expected unbound-keys diagnostic, got %v", insp.Diagnostics)
	}
}

func TestInspectFundamentals_NullFundamental(t *testing.T) {
	body := []byte(`{"instruments":[{"symbol":"AAPL","fundamental":null}]}`)
	insp, err := InspectFundamentals(body)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if insp.HasFundamental {
		t.Error("expected HasFundamental=false for null fundamental")
	}
	if len(insp.Fields) != 0 {
		t.Errorf("expected no field rows, got %d", len(insp.Fields))
	}
	if !hasDiagnostic(insp.Diagnostics, "fundamental is null") {
		t.Errorf("expected null-fundamental diagnostic, got %v", insp.Diagnostics)
	}
}

func TestInspectFundamentals_EmptyInstruments(t *testing.T) {
	body := []byte(`{"instruments":[]}`)
	insp, err := InspectFundamentals(body)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if insp.InstrumentCount != 0 || insp.HasFundamental {
		t.Errorf("envelope = count %d hasFund %v, want 0/false", insp.InstrumentCount, insp.HasFundamental)
	}
	if !hasDiagnostic(insp.Diagnostics, "zero instruments") {
		t.Errorf("expected zero-instruments diagnostic, got %v", insp.Diagnostics)
	}
}

func TestInspectFundamentals_InvalidJSON(t *testing.T) {
	if _, err := InspectFundamentals([]byte(`not json`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
