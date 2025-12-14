package vault

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Tokenizer provides format-preserving tokenization for payment cards.
type Tokenizer struct{}

// NewTokenizer creates a new tokenizer.
func NewTokenizer() *Tokenizer {
	return &Tokenizer{}
}

// CardData holds validated card information.
type CardData struct {
	PAN        string
	CVV        string
	Expiry     string
	Cardholder string
}

// ValidateAndTokenize validates card data and generates a unique token.
func (t *Tokenizer) ValidateAndTokenize(pan, cvv, expiry, cardholder string) (token string, first6, last4 string, err error) {
	// Validate inputs
	if err := t.validatePAN(pan); err != nil {
		return "", "", "", fmt.Errorf("invalid PAN: %w", err)
	}

	if err := t.validateCVV(cvv); err != nil {
		return "", "", "", fmt.Errorf("invalid CVV: %w", err)
	}

	if err := t.validateExpiry(expiry); err != nil {
		return "", "", "", fmt.Errorf("invalid expiry: %w", err)
	}

	if err := t.validateCardholder(cardholder); err != nil {
		return "", "", "", fmt.Errorf("invalid cardholder: %w", err)
	}

	// Extract first 6 and last 4
	first6 = pan[:6]
	last4 = pan[len(pan)-4:]

	// Generate unique token (UUID-style)
	token, err = t.generateToken()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate token: %w", err)
	}

	return token, first6, last4, nil
}

// validatePAN validates a Primary Account Number using Luhn algorithm.
func (t *Tokenizer) validatePAN(pan string) error {
	// Remove spaces and dashes
	pan = strings.ReplaceAll(pan, " ", "")
	pan = strings.ReplaceAll(pan, "-", "")

	// Check length (13-19 digits for valid card numbers)
	if len(pan) < 13 || len(pan) > 19 {
		return errors.New("PAN must be 13-19 digits")
	}

	// Check if it's all digits
	if !regexp.MustCompile(`^\d+$`).MatchString(pan) {
		return errors.New("PAN must contain only digits")
	}

	// Luhn algorithm validation
	if !t.luhnCheck(pan) {
		return errors.New("PAN failed Luhn check")
	}

	return nil
}

// validateCVV validates a Card Verification Value.
func (t *Tokenizer) validateCVV(cvv string) error {
	cvv = strings.TrimSpace(cvv)

	// CVV should be 3-4 digits
	if len(cvv) < 3 || len(cvv) > 4 {
		return errors.New("CVV must be 3-4 digits")
	}

	// Check if it's all digits
	if !regexp.MustCompile(`^\d+$`).MatchString(cvv) {
		return errors.New("CVV must contain only digits")
	}

	return nil
}

// validateExpiry validates card expiry date in MM/YY format.
func (t *Tokenizer) validateExpiry(expiry string) error {
	expiry = strings.TrimSpace(expiry)

	// Check format MM/YY or MM/YYYY
	if !regexp.MustCompile(`^\d{2}/\d{2,4}$`).MatchString(expiry) {
		return errors.New("expiry must be in MM/YY or MM/YYYY format")
	}

	parts := strings.Split(expiry, "/")
	month, err := strconv.Atoi(parts[0])
	if err != nil {
		return errors.New("invalid month in expiry")
	}

	if month < 1 || month > 12 {
		return errors.New("month must be between 01 and 12")
	}

	return nil
}

// validateCardholder validates cardholder name.
func (t *Tokenizer) validateCardholder(name string) error {
	name = strings.TrimSpace(name)

	if len(name) == 0 {
		return errors.New("cardholder name must not be empty")
	}

	if len(name) > 255 {
		return errors.New("cardholder name must not exceed 255 characters")
	}

	// Allow letters, spaces, hyphens, and apostrophes
	if !regexp.MustCompile(`^[a-zA-Z\s\-']+$`).MatchString(name) {
		return errors.New("cardholder name contains invalid characters")
	}

	return nil
}

// luhnCheck performs the Luhn algorithm validation.
func (t *Tokenizer) luhnCheck(pan string) bool {
	sum := 0
	parity := len(pan) % 2

	for i, digit := range pan {
		d, err := strconv.Atoi(string(digit))
		if err != nil {
			return false
		}

		if i%2 == parity {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}

		sum += d
	}

	return sum%10 == 0
}

// generateToken generates a unique token using random bytes.
func (t *Tokenizer) generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "tok_" + hex.EncodeToString(b), nil
}
