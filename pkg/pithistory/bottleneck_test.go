package pithistory

import (
	"strings"
	"testing"
)

func TestClassifyCashFlow(t *testing.T) {
	// 1. Loss
	b1 := classifyCashFlow(-173.5e7, 302.7e7, -69.5e7, "Consumer Cyclical")
	if b1.Code != "CF-LOSS" {
		t.Errorf("expected CF-LOSS, got %s", b1.Code)
	}
	if !strings.Contains(b1.Detail, "NI -₹173.5Cr") {
		t.Errorf("expected NI in detail, got %s", b1.Detail)
	}

	// 2. Lag
	b2 := classifyCashFlow(572.6e7, -378.1e7, -723.4e7, "Technology")
	if b2.Code != "CF-LAG" {
		t.Errorf("expected CF-LAG, got %s", b2.Code)
	}
	if !strings.Contains(b2.Detail, "PAT ₹572.6Cr") {
		t.Errorf("expected PAT in detail, got %s", b2.Detail)
	}

	// 3. Normal for Lender/Developer
	b3 := classifyCashFlow(1127.6e7, -8727.8e7, -8787.4e7, "Financial Services")
	if b3.Code != "CF-NORM" {
		t.Errorf("expected CF-NORM, got %s", b3.Code)
	}
	if !strings.Contains(b3.Detail, "Financial Serv-norm") {
		t.Errorf("expected norm tag in detail, got %s", b3.Detail)
	}

	// 4. No data
	b4 := classifyCashFlow(668.5e7, 0, 1795.3e7, "Financial Services")
	if b4.Code != "CF-NODATA" {
		t.Errorf("expected CF-NODATA, got %s", b4.Code)
	}
}

func TestClassifyDSO(t *testing.T) {
	cases := []struct {
		val  float64
		code string
	}{
		{16.8, "DSO-MILD"},
		{27.3, "DSO-SEVERE"},
		{45.0, "DSO-CRIT"},
	}
	for _, c := range cases {
		b := classifyDSO(c.val)
		if b.Code != c.code {
			t.Errorf("classifyDSO(%f) = %s; want %s", c.val, b.Code, c.code)
		}
	}
}

func TestClassify52WHigh(t *testing.T) {
	cases := []struct {
		val  float64
		code string
	}{
		{84.7, "52W-NEAR"},
		{75.0, "52W-BASE"},
		{55.0, "52W-DEEP"},
	}
	for _, c := range cases {
		b := classify52WHigh(c.val)
		if b.Code != c.code {
			t.Errorf("classify52WHigh(%f) = %s; want %s", c.val, b.Code, c.code)
		}
	}
}

func TestClassifyROCE(t *testing.T) {
	cases := []struct {
		val  float64
		code string
	}{
		{10.6, "ROCE-BORDER"},
		{8.4, "ROCE-WEAK"},
		{2.3, "ROCE-POOR"},
		{-1.0, "ROCE-NEG"},
	}
	for _, c := range cases {
		b := classifyROCE(c.val)
		if b.Code != c.code {
			t.Errorf("classifyROCE(%f) = %s; want %s", c.val, b.Code, c.code)
		}
	}
}

func TestClassifySMA(t *testing.T) {
	b1 := classifySMA(0.9496, 0.2, false)
	if b1.Code != "SMA-BORDER" {
		t.Errorf("expected SMA-BORDER, got %s", b1.Code)
	}
	if !strings.Contains(b1.Detail, "0.9496") || !strings.Contains(b1.Detail, "0.0004 short") {
		t.Errorf("expected 4-decimal precision, got %s", b1.Detail)
	}

	b2 := classifySMA(0.95, -0.6, true)
	if b2.Code != "SMA-DECLINE" {
		t.Errorf("expected SMA-DECLINE, got %s", b2.Code)
	}

	b3 := classifySMA(0.89, -1.0, false)
	if b3.Code != "SMA-BREAK" {
		t.Errorf("expected SMA-BREAK, got %s", b3.Code)
	}
}

func TestClassifyBottleneckGate_Dispatcher(t *testing.T) {
	bCleared := ClassifyBottleneckGate("Stage-1 Qualified", "Healthcare", 0, 0, 0, "")
	if bCleared.Code != "CLEARED" {
		t.Errorf("expected CLEARED, got %s", bCleared.Code)
	}

	bDSO := ClassifyBottleneckGate("DSO Deterioration limit exceeded (+16.8% > 15.0% threshold)", "Healthcare", 0, 0, 0, "")
	if bDSO.Code != "DSO-MILD" {
		t.Errorf("expected DSO-MILD, got %s", bDSO.Code)
	}

	bDebt := ClassifyBottleneckGate("High Debt/Equity (2.92 >= 1.50 cap)", "Consumer Cyclical", 0, 0, 0, "")
	if bDebt.Code != "DEBT-HIGH" {
		t.Errorf("expected DEBT-HIGH, got %s", bDebt.Code)
	}

	b52W := ClassifyBottleneckGate("Far from 52-Week High (84.7% of 52W high < 85.0% floor)", "Basic Materials", 0, 0, 0, "")
	if b52W.Code != "52W-NEAR" {
		t.Errorf("expected 52W-NEAR, got %s", b52W.Code)
	}
}

func TestColorizeGateCode(t *testing.T) {
	green := ColorizeGateCode("CLEARED", 12)
	if !strings.Contains(green, "\033[1;32m") {
		t.Errorf("expected green ANSI code, got %q", green)
	}

	red := ColorizeGateCode("CF-LOSS", 12)
	if !strings.Contains(red, "\033[1;31m") {
		t.Errorf("expected red ANSI code, got %q", red)
	}

	yellow := ColorizeGateCode("CF-LAG", 12)
	if !strings.Contains(yellow, "\033[1;33m") {
		t.Errorf("expected yellow ANSI code, got %q", yellow)
	}
}

func TestClassifyPreBreakoutSignature(t *testing.T) {
	// 1. Stealth High
	s1 := classifyPreBreakoutSignature("Stealth Institutional Accum", 0.03, 17.3)
	if s1.Code != "STEALTH-HIGH" {
		t.Errorf("expected STEALTH-HIGH, got %s", s1.Code)
	}
	if !strings.Contains(s1.Detail, "Score CV 0.03") {
		t.Errorf("expected detail with Score CV, got %s", s1.Detail)
	}

	// 2. Stealth Low
	s2 := classifyPreBreakoutSignature("Stealth Institutional Accum", 0.19, 6.9)
	if s2.Code != "STEALTH-LOW" {
		t.Errorf("expected STEALTH-LOW, got %s", s2.Code)
	}

	// 3. Base Strong
	s3 := classifyPreBreakoutSignature("Base Consolidating", 0.10, 172.1)
	if s3.Code != "BASE-STRONG" {
		t.Errorf("expected BASE-STRONG, got %s", s3.Code)
	}
	if !strings.Contains(s3.Detail, "+172.1%") {
		t.Errorf("expected detail with +172.1%%, got %s", s3.Detail)
	}

	// 4. Base Weak
	s4 := classifyPreBreakoutSignature("Base Consolidating", 0.31, 2.0)
	if s4.Code != "BASE-WEAK" {
		t.Errorf("expected BASE-WEAK, got %s", s4.Code)
	}

	// 5. Coil Heavy
	s5 := classifyPreBreakoutSignature("Tight VCP Coil + Heavy Deliv", 0.07, 57.2)
	if s5.Code != "COIL-HEAVY" {
		t.Errorf("expected COIL-HEAVY, got %s", s5.Code)
	}

	// 6. Coil Extreme
	s6 := classifyPreBreakoutSignature("Extreme Volatility Coil", 0.04, 31.1)
	if s6.Code != "COIL-EXTREME" {
		t.Errorf("expected COIL-EXTREME, got %s", s6.Code)
	}
}

func TestClassifyAccumulationTrajectory(t *testing.T) {
	// 1. Velocity Breakout (TITAGARH: 11.2 -> 10.8 -> 17.3)
	t1 := ClassifyAccumulationTrajectory(11.2, 10.8, 17.3)
	if t1.Code != "VELOCITY-BREAKOUT" {
		t.Errorf("expected VELOCITY-BREAKOUT, got %s", t1.Code)
	}

	// 2. Dipped then recovered (MOTHERSON: 29.2 -> 24.2 -> 28.9)
	t2 := ClassifyAccumulationTrajectory(29.2, 24.2, 28.9)
	if t2.Code != "RECOVERY-ACCUM" {
		t.Errorf("expected RECOVERY-ACCUM, got %s", t2.Code)
	}
	if t2.Note != "Dipped, then recovered" {
		t.Errorf("expected Dipped, then recovered, got %s", t2.Note)
	}

	// 3. Flat then jumped (PNB: 20.3 -> 20.3 -> 23.7)
	t3 := ClassifyAccumulationTrajectory(20.3, 20.3, 23.7)
	if t3.Code != "RECOVERY-ACCUM" {
		t.Errorf("expected RECOVERY-ACCUM, got %s", t3.Code)
	}
	if t3.Note != "Flat, then jumped" {
		t.Errorf("expected Flat, then jumped, got %s", t3.Note)
	}

	// 4. Consecutive Surge (BALRAMCHIN: 30.7 -> 38.4 -> 42.7)
	t4 := ClassifyAccumulationTrajectory(30.7, 38.4, 42.7)
	if t4.Code != "CONSEC-SURGE" {
		t.Errorf("expected CONSEC-SURGE, got %s", t4.Code)
	}

	// 5. Steady Accum (APLLTD: 8.6 -> 8.4 -> 12.9)
	t5 := ClassifyAccumulationTrajectory(8.6, 8.4, 12.9)
	if t5.Code != "STEADY-ACCUM" {
		t.Errorf("expected STEADY-ACCUM, got %s", t5.Code)
	}
}

func TestColorizeLaunchpadState(t *testing.T) {
	armed := ColorizeLaunchpadState("LAUNCHPAD-ARMED", 15)
	if !strings.Contains(armed, "\033[1;32m") {
		t.Errorf("expected green ANSI code for LAUNCHPAD-ARMED, got %q", armed)
	}

	coil := ColorizeLaunchpadState("COIL-COMPRESS", 15)
	if !strings.Contains(coil, "\033[1;36m") {
		t.Errorf("expected cyan ANSI code for COIL-COMPRESS, got %q", coil)
	}

	stealthHigh := ColorizeLaunchpadState("STEALTH-HIGH", 15)
	if !strings.Contains(stealthHigh, "\033[1;32m") {
		t.Errorf("expected bold green ANSI code for STEALTH-HIGH, got %q", stealthHigh)
	}

	baseAccum := ColorizeLaunchpadState("BASE-ACCUM", 15)
	if !strings.Contains(baseAccum, "\033[0;37m") {
		t.Errorf("expected light gray ANSI code for BASE-ACCUM, got %q", baseAccum)
	}
}

func TestClassifyVelocityPattern(t *testing.T) {
	d1_4_3 := 4.3
	d3_12_0 := 12.0
	// 1. Consec surge (BALRAMCHIN)
	p1 := ClassifyVelocityPattern(42.7, 38.4, 30.7, &d1_4_3, &d3_12_0, 0.69, false)
	if p1 != "CONSEC-SURGE" {
		t.Errorf("expected CONSEC-SURGE, got %s", p1)
	}

	// 2. Severe Decliner (GLAXO in Section 5)
	d1_neg5_6 := -5.6
	d3_pos5_8 := 5.8
	p2 := ClassifyVelocityPattern(36.9, 42.5, 31.1, &d1_neg5_6, &d3_pos5_8, 0.66, true)
	if p2 != "FADE-SHARP" {
		t.Errorf("expected FADE-SHARP for Section 5 decliner, got %s", p2)
	}

	// 3. Severe multi-day bleed (DCBBANK: 1D -0.9, 3D -12.8)
	d1_neg0_9 := -0.9
	d3_neg12_8 := -12.8
	p3 := ClassifyVelocityPattern(30.9, 31.8, 43.6, &d1_neg0_9, &d3_neg12_8, 0.71, false)
	if p3 != "FADE-SHARP" {
		t.Errorf("expected FADE-SHARP for multi-session bleed, got %s", p3)
	}

	// 4. FADE-MILD (AVALON multi-day drift: 1D -1.6, 3D -7.8)
	d1_neg1_6 := -1.6
	d3_neg7_8 := -7.8
	p4 := ClassifyVelocityPattern(32.1, 33.7, 39.9, &d1_neg1_6, &d3_neg7_8, 1.10, false)
	if p4 != "FADE-MILD" {
		t.Errorf("expected FADE-MILD, got %s", p4)
	}

	// 5. Extreme Coil holding through mild drift (TIPSMUSIC)
	d1_neg2_1 := -2.1
	d3_neg3_8 := -3.8
	p5 := ClassifyVelocityPattern(40.9, 43.0, 44.7, &d1_neg2_1, &d3_neg3_8, 0.36, false)
	if p5 != "COILING" {
		t.Errorf("expected COILING for extreme VCP, got %s", p5)
	}
}

func TestFormatDiagnosticFootprint(t *testing.T) {
	d1_pos4_3 := 4.3
	d3_pos12_0 := 12.0
	d1 := FormatDiagnosticFootprint("LAUNCHPAD-ARMED", "CONSEC-SURGE", 0.69, 0.118, 0.276, 0.15, false, &d1_pos4_3, &d3_pos12_0)
	if d1 != "Tight coil + heavy inflows" {
		t.Errorf("expected 'Tight coil + heavy inflows', got %q", d1)
	}

	d1_pos1_5 := 1.5
	d3_pos1_5 := 1.5
	d2 := FormatDiagnosticFootprint("COIL-COMPRESS", "COILING", 0.38, -0.015, 0.311, 0.04, false, &d1_pos1_5, &d3_pos1_5)
	if d2 != "Extreme volatility squeeze" {
		t.Errorf("expected 'Extreme volatility squeeze', got %q", d2)
	}

	d1_neg2_1 := -2.1
	d3_neg3_8 := -3.8
	d3 := FormatDiagnosticFootprint("COIL-COMPRESS", "COILING", 0.36, 0.008, 0.113, 0.04, false, &d1_neg2_1, &d3_neg3_8)
	if d3 != "Energy coil awaiting volume" {
		t.Errorf("expected 'Energy coil awaiting volume', got %q", d3)
	}

	// Decliner / Breakdown
	d1_neg5_6 := -5.6
	d3_pos5_8 := 5.8
	d4 := FormatDiagnosticFootprint("BASE-ACCUM", "FADE-SHARP", 0.66, 0.103, 0.069, 0.19, true, &d1_neg5_6, &d3_pos5_8)
	if d4 != "⚠ Confirmed drop (-5.6pt)" {
		t.Errorf("expected '⚠ Confirmed drop (-5.6pt)', got %q", d4)
	}

	// Multi-day bleed
	d1_neg0_9 := -0.9
	d3_neg12_8 := -12.8
	d5 := FormatDiagnosticFootprint("BASE-STRONG", "FADE-SHARP", 0.71, 0.037, 0.301, 0.17, false, &d1_neg0_9, &d3_neg12_8)
	if d5 != "⚠ Multi-day bleed (-12.8pt)" {
		t.Errorf("expected '⚠ Multi-day bleed (-12.8pt)', got %q", d5)
	}

	// Score CV stealth high
	d1_pos1_3 := 1.3
	d3_neg0_6 := -0.6
	d6 := FormatDiagnosticFootprint("STEALTH-HIGH", "RECOVERY-ACCUM", 0.86, 0.117, 0.173, 0.03, false, &d1_pos1_3, &d3_neg0_6)
	if d6 != "CV 0.03: High conviction" {
		t.Errorf("expected 'CV 0.03: High conviction', got %q", d6)
	}
}

func TestPadVisible(t *testing.T) {
	s := "NSE:GLAXO ⚠"
	padded := PadVisible(s, 15)
	if len([]rune(padded)) != 15 {
		t.Errorf("expected 15 runes, got %d (string %q)", len([]rune(padded)), padded)
	}
}
