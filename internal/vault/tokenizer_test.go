package vault

import (
	"strings"
	"testing"
)

func TestValidatePANValid(t *testing.T) {
	tokenizer := NewTokenizer()

	testCases := []string{
		"4532015112830366", // Valid Visa
		"5425233010103442", // Valid Mastercard
		"378282246310005",  // Valid Amex
		"4532-0151-1283-0366", // With dashes
		"4532 0151 1283 0366", // With spaces
	}

	for _, pan := range testCases {
		if err := tokenizer.validatePAN(pan); err != nil {
			t.Errorf("Valid PAN rejected: %s - %v", pan, err)
		}
	}
}

func TestValidatePANInvalid(t *testing.T) {
	tokenizer := NewTokenizer()

	testCases := []struct {
		pan    string
		reason string
	}{
		{"", "empty PAN"},
		{"123", "too short"},
		{"12345678901234567890", "too long"},
		{"123456789012345a", "contains letter"},
		{"1234567890123", "fails Luhn check"},
	}

	for _, tc := range testCases {
		if err := tokenizer.validatePAN(tc.pan); err == nil {
			t.Errorf("Invalid PAN accepted: %s (%s)", tc.pan, tc.reason)
		}
	}
}

func TestValidateCVVValid(t *testing.T) {
	tokenizer := NewTokenizer()

	testCases := []string{"123", "1234", "999", "000"}

	for _, cvv := range testCases {
		if err := tokenizer.validateCVV(cvv); err != nil {
			t.Errorf("Valid CVV rejected: %s - %v", cvv, err)
		}
	}
}

func TestValidateCVVInvalid(t *testing.T) {
	tokenizer := NewTokenizer()

	testCases := []string{"", "12", "12345", "abc", "1a3"}

	for _, cvv := range testCases {
		if err := tokenizer.validateCVV(cvv); err == nil {
			t.Errorf("Invalid CVV accepted: %s", cvv)
		}
	}
}

func TestValidateExpiryValid(t *testing.T) {
	tokenizer := NewTokenizer()

	testCases := []string{"12/25", "01/26", "12/2026", "06/2025"}

	for _, expiry := range testCases {
		if err := tokenizer.validateExpiry(expiry); err != nil {
			t.Errorf("Valid expiry rejected: %s - %v", expiry, err)
		}
	}
}

func TestValidateExpiryInvalid(t *testing.T) {
	tokenizer := NewTokenizer()

	testCases := []string{"", "13/25", "00/25", "12/2", "12-25", "2025-12"}

	for _, expiry := range testCases {
		if err := tokenizer.validateExpiry(expiry); err == nil {
			t.Errorf("Invalid expiry accepted: %s", expiry)
		}
	}
}

func TestValidateCardholderValid(t *testing.T) {
	tokenizer := NewTokenizer()

	testCases := []string{"John Doe", "Mary-Jane Smith", "Patrick O'Connor", "A"}

	for _, name := range testCases {
		if err := tokenizer.validateCardholder(name); err != nil {
			t.Errorf("Valid cardholder rejected: %s - %v", name, err)
		}
	}
}

func TestValidateCardholderInvalid(t *testing.T) {
	tokenizer := NewTokenizer()

	testCases := []struct {
		name   string
		reason string
	}{
		{"", "empty"},
		{strings.Repeat("A", 256), "too long"},
		{"John123", "contains digits"},
		{"John@Doe", "contains special characters"},
	}

	for _, tc := range testCases {
		if err := tokenizer.validateCardholder(tc.name); err == nil {
			t.Errorf("Invalid cardholder accepted: %s (%s)", tc.name, tc.reason)
		}
	}
}

func TestLuhnCheck(t *testing.T) {
	tokenizer := NewTokenizer()

	testCases := []struct {
		pan   string
		valid bool
	}{
		{"4532015112830366", true},   // Valid Visa
		{"5425233010103442", true},   // Valid Mastercard
		{"378282246310005", true},    // Valid Amex
		{"1234567890123456", false},  // Invalid
	}

	for _, tc := range testCases {
		result := tokenizer.luhnCheck(tc.pan)
		if result != tc.valid {
			t.Errorf("Luhn check for %s: expected %v, got %v", tc.pan, tc.valid, result)
		}
	}
}

func TestValidateAndTokenize(t *testing.T) {
	tokenizer := NewTokenizer()

	token, first6, last4, err := tokenizer.ValidateAndTokenize(
		"4532015112830366",
		"123",
		"12/25",
		"John Doe",
	)

	if err != nil {
		t.Fatalf("ValidateAndTokenize failed: %v", err)
	}

	if !strings.HasPrefix(token, "tok_") {
		t.Errorf("Token should start with 'tok_': %s", token)
	}

	if first6 != "453201" {
		t.Errorf("Expected first6 '453201', got '%s'", first6)
	}

	if last4 != "0366" {
		t.Errorf("Expected last4 '0366', got '%s'", last4)
	}
}

func TestValidateAndTokenizeInvalidPAN(t *testing.T) {
	tokenizer := NewTokenizer()

	_, _, _, err := tokenizer.ValidateAndTokenize(
		"1234567890123456",
		"123",
		"12/25",
		"John Doe",
	)

	if err == nil {
		t.Error("Expected error for invalid PAN")
	}
}

func TestValidateAndTokenizeUniqueness(t *testing.T) {
	tokenizer := NewTokenizer()

	token1, _, _, err1 := tokenizer.ValidateAndTokenize(
		"4532015112830366",
		"123",
		"12/25",
		"John Doe",
	)

	token2, _, _, err2 := tokenizer.ValidateAndTokenize(
		"4532015112830366",
		"123",
		"12/25",
		"John Doe",
	)

	if err1 != nil || err2 != nil {
		t.Fatalf("ValidateAndTokenize failed: %v, %v", err1, err2)
	}

	if token1 == token2 {
		t.Error("Two tokenizations should produce different tokens")
	}
}
