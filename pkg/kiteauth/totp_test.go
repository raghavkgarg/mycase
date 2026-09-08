package kiteauth

import (
	"testing"
	"time"
)

func TestGenerateTOTP(t *testing.T) {
	// RFC 6238 test secret "12345678901234567890" in base32
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

	// RFC 6238 reference test vector for T0 = 59s:
	// Interval = 59 / 30 = 1
	// Expected TOTP for interval 1 with sha1 is "287082"
	testTime := time.Unix(59, 0)
	code, err := GenerateTOTPAtTime(secret, testTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "287082"
	if code != expected {
		t.Errorf("expected %s, got %s", expected, code)
	}

	// Test with current time and spaces in secret
	spacedSecret := "JBSW Y3DP EHPK 3PXP"
	liveCode, err := GenerateTOTP(spacedSecret)
	if err != nil {
		t.Fatalf("unexpected error with spaced secret: %v", err)
	}
	if len(liveCode) != 6 {
		t.Errorf("expected 6-digit code, got %s", liveCode)
	}
}
