package kiteauth

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// GenerateTOTP generates a 6-digit Time-based One-Time Password (RFC 6238)
// using the provided Base32-encoded secret string.
func GenerateTOTP(secret string) (string, error) {
	return GenerateTOTPAtTime(secret, time.Now())
}

// GenerateTOTPAtTime generates the TOTP code for a specific time point.
func GenerateTOTPAtTime(secret string, t time.Time) (string, error) {
	cleaned := strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(cleaned)
	if err != nil {
		// Fallback to standard padding if NoPadding fails
		key, err = base32.StdEncoding.DecodeString(cleaned)
		if err != nil {
			return "", fmt.Errorf("invalid base32 secret: %w", err)
		}
	}

	interval := t.Unix() / 30
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(interval))

	h := hmac.New(sha1.New, key)
	h.Write(counter[:])
	hash := h.Sum(nil)

	offset := hash[len(hash)-1] & 0x0f
	binaryCode := binary.BigEndian.Uint32(hash[offset:offset+4]) & 0x7fffffff
	otp := binaryCode % 1000000

	return fmt.Sprintf("%06d", otp), nil
}
