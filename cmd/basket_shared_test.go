package cmd

import (
	"bufio"
	"context"
	"strings"
	"testing"
)

func TestReconcileSharedHoldings_OptionA_Default(t *testing.T) {
	ctx := context.Background()

	basketFilename := "data/aitheme.csv"
	basketKeys := []string{
		"NSE:LIQUIDCASE",
		"NSE:KAYNES",
		"NSE:E2E",
	}
	basket := map[string]float64{
		"NSE:LIQUIDCASE": 0.35,
		"NSE:KAYNES":     0.05,
		"NSE:E2E":        0.03,
	}
	quoteData := map[string]float64{
		"NSE:LIQUIDCASE": 119.60,
		"NSE:KAYNES":     3767.10,
		"NSE:E2E":        631.40,
	}
	// Simulate Kite Demat holdings where 496 LIQUIDCASE are held by ModularMicro
	currentHoldings := map[string]int{
		"LIQUIDCASE": 496,
		"KAYNES":     1,
		"E2E":        7,
	}

	// User hits Enter to accept Option A default
	reader := bufio.NewReader(strings.NewReader("\n"))

	overrides, themeName, err := reconcileSharedHoldings(
		ctx, basketFilename, basketKeys, basket, quoteData, currentHoldings, reader, "", false,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if themeName != "aitheme" {
		t.Errorf("expected themeName 'aitheme', got %q", themeName)
	}

	// In currentHoldings, LIQUIDCASE should have been reset to 0 (isolated for aitheme)
	if currentHoldings["LIQUIDCASE"] != 0 {
		t.Errorf("expected currentHoldings[LIQUIDCASE] to be 0, got %d", currentHoldings["LIQUIDCASE"])
	}

	// Non-shared holdings value: KAYNES (1*3767.10 = 3767.10) + E2E (7*631.40 = 4419.80) = 8186.90
	// Target for LIQUIDCASE (35%): 8186.90 * 0.35 = 2865.415 -> 2865.415 / 119.60 = 23.95 -> 24 shares
	expectedBuy := 24
	if buyQty, ok := overrides["LIQUIDCASE"]; !ok || buyQty != expectedBuy {
		t.Errorf("expected overrides[LIQUIDCASE] = %d, got %d (ok: %t)", expectedBuy, buyQty, ok)
	}
}

func TestReconcileSharedHoldings_OptionB_SharedPool(t *testing.T) {
	ctx := context.Background()

	basketFilename := "data/aitheme.csv"
	basketKeys := []string{
		"NSE:LIQUIDCASE",
		"NSE:KAYNES",
	}
	basket := map[string]float64{
		"NSE:LIQUIDCASE": 0.35,
		"NSE:KAYNES":     0.05,
	}
	quoteData := map[string]float64{
		"NSE:LIQUIDCASE": 119.60,
		"NSE:KAYNES":     3767.10,
	}
	currentHoldings := map[string]int{
		"LIQUIDCASE": 496,
		"KAYNES":     1,
	}

	// User types "B" for Option B (pool)
	reader := bufio.NewReader(strings.NewReader("b\n"))

	overrides, _, err := reconcileSharedHoldings(
		ctx, basketFilename, basketKeys, basket, quoteData, currentHoldings, reader, "", false,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Option B should buy 0 shares
	if buyQty, ok := overrides["LIQUIDCASE"]; !ok || buyQty != 0 {
		t.Errorf("expected overrides[LIQUIDCASE] = 0, got %d", buyQty)
	}
}

func TestReconcileSharedHoldings_CustomManualQty(t *testing.T) {
	ctx := context.Background()

	basketFilename := "data/aitheme.csv"
	basketKeys := []string{
		"NSE:LIQUIDCASE",
		"NSE:KAYNES",
	}
	basket := map[string]float64{
		"NSE:LIQUIDCASE": 0.35,
		"NSE:KAYNES":     0.05,
	}
	quoteData := map[string]float64{
		"NSE:LIQUIDCASE": 119.60,
		"NSE:KAYNES":     3767.10,
	}
	currentHoldings := map[string]int{
		"LIQUIDCASE": 496,
		"KAYNES":     1,
	}

	// User types "100" custom shares to buy
	reader := bufio.NewReader(strings.NewReader("100\n"))

	overrides, _, err := reconcileSharedHoldings(
		ctx, basketFilename, basketKeys, basket, quoteData, currentHoldings, reader, "", false,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buyQty, ok := overrides["LIQUIDCASE"]; !ok || buyQty != 100 {
		t.Errorf("expected overrides[LIQUIDCASE] = 100, got %d", buyQty)
	}
}

func TestReconcileSharedHoldings_FlagOverride(t *testing.T) {
	ctx := context.Background()

	basketFilename := "data/aitheme.csv"
	basketKeys := []string{
		"NSE:LIQUIDCASE",
	}
	basket := map[string]float64{
		"NSE:LIQUIDCASE": 0.35,
	}
	quoteData := map[string]float64{
		"NSE:LIQUIDCASE": 119.60,
	}
	currentHoldings := map[string]int{
		"LIQUIDCASE": 496,
	}

	overrides, _, err := reconcileSharedHoldings(
		ctx, basketFilename, basketKeys, basket, quoteData, currentHoldings, nil, "LIQUIDCASE:318", false,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buyQty, ok := overrides["LIQUIDCASE"]; !ok || buyQty != 318 {
		t.Errorf("expected overrides[LIQUIDCASE] = 318, got %d", buyQty)
	}
}

func TestReconcileSharedHoldings_ExitProtection_Netweb(t *testing.T) {
	ctx := context.Background()

	basketFilename := "data/aitheme.csv"
	// NETWEB has target weight 0.00 (exit), while in microsmall it has 5.1%
	basketKeys := []string{
		"NSE:NETWEB",
		"NSE:KAYNES",
	}
	basket := map[string]float64{
		"NSE:NETWEB": 0.00,
		"NSE:KAYNES": 0.05,
	}
	quoteData := map[string]float64{
		"NSE:NETWEB": 4548.50,
		"NSE:KAYNES": 3609.40,
	}
	currentHoldings := map[string]int{
		"NETWEB": 2, // 2 shares in Demat
		"KAYNES": 1,
	}

	// User presses Enter to accept default Option A (Protect Microsmall: 0 shares to sell)
	reader := bufio.NewReader(strings.NewReader("\n"))

	_, _, err := reconcileSharedHoldings(
		ctx, basketFilename, basketKeys, basket, quoteData, currentHoldings, reader, "", false,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// currentHoldings for NETWEB should be set to 0 so diff = 0 - 0 = 0 (microsmall's 2 shares protected!)
	if currentHoldings["NETWEB"] != 0 {
		t.Errorf("expected currentHoldings[NETWEB] to be 0 (protected), got %d", currentHoldings["NETWEB"])
	}
}

