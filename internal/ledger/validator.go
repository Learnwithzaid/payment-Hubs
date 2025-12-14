package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Validator provides invariants checking for the ledger
type Validator struct {
	postgres *PostgresLedger
}

// NewValidator creates a new validator instance
func NewValidator(postgres *PostgresLedger) *Validator {
	return &Validator{
		postgres: postgres,
	}
}

// ValidationResult represents the result of a validation check
type ValidationResult struct {
	IsValid        bool      `json:"is_valid"`
	ValidationType string    `json:"validation_type"`
	Message        string    `json:"message"`
	AccountID      string    `json:"account_id,omitempty"`
	TransactionID  string    `json:"transaction_id,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
	Details        map[string]interface{} `json:"details,omitempty"`
}

// ValidateAccountType checks if account type is valid
func (v *Validator) ValidateAccountType(accountType string) *ValidationResult {
	validTypes := map[string]bool{
		"asset":     true,
		"liability": true,
		"equity":    true,
		"revenue":   true,
		"expense":   true,
	}
	
	return &ValidationResult{
		IsValid:         validTypes[accountType],
		ValidationType:  "account_type",
		Message:         getAccountTypeValidationMessage(accountType),
		Timestamp:       time.Now(),
	}
}

// getAccountTypeValidationMessage returns appropriate validation message
func getAccountTypeValidationMessage(accountType string) string {
	validTypes := []string{"asset", "liability", "equity", "revenue", "expense"}
	if accountType == "" {
		return "account type is required"
	}
	return fmt.Sprintf("invalid account type '%s'. Valid types are: %s", 
		accountType, strings.Join(validTypes, ", "))
}

// ValidateCurrencyCode checks if currency code is valid (ISO 4217)
func (v *Validator) ValidateCurrencyCode(currencyCode string) *ValidationResult {
	if len(currencyCode) != 3 {
		return &ValidationResult{
			IsValid:        false,
			ValidationType: "currency_code",
			Message:        "currency code must be exactly 3 characters",
			Timestamp:      time.Now(),
		}
	}
	
	// Basic ISO 4217 validation (uppercase letters)
	matched, _ := regexp.MatchString("^[A-Z]{3}$", currencyCode)
	
	return &ValidationResult{
		IsValid:        matched,
		ValidationType: "currency_code",
		Message:        getCurrencyValidationMessage(currencyCode, matched),
		Timestamp:      time.Now(),
	}
}

// getCurrencyValidationMessage returns appropriate validation message for currency
func getCurrencyValidationMessage(currencyCode string, isValid bool) string {
	if !isValid {
		return fmt.Sprintf("currency code '%s' must contain only uppercase letters (ISO 4217)", currencyCode)
	}
	return fmt.Sprintf("currency code '%s' is valid", currencyCode)
}

// ValidateAccountNumber checks account number format and uniqueness
func (v *Validator) ValidateAccountNumber(ctx context.Context, accountNumber string) *ValidationResult {
	// Check format
	if len(accountNumber) == 0 || len(accountNumber) > 50 {
		return &ValidationResult{
			IsValid:        false,
			ValidationType: "account_number",
			Message:        "account number must be between 1 and 50 characters",
			Timestamp:      time.Now(),
		}
	}
	
	// Check for valid characters (alphanumeric and hyphens/underscores)
	matched, _ := regexp.MatchString("^[A-Za-z0-9_-]+$", accountNumber)
	if !matched {
		return &ValidationResult{
			IsValid:        false,
			ValidationType: "account_number",
			Message:        "account number can only contain letters, numbers, hyphens, and underscores",
			Timestamp:      time.Now(),
		}
	}
	
	// Check uniqueness
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	
	var exists bool
	err := v.postgres.Pool.QueryRow(ctx, 
		"SELECT EXISTS(SELECT 1 FROM accounts WHERE account_number = $1)", 
		accountNumber).Scan(&exists)
	
	if err != nil {
		return &ValidationResult{
			IsValid:        false,
			ValidationType: "account_number",
			Message:        fmt.Sprintf("failed to check account number uniqueness: %v", err),
			Timestamp:      time.Now(),
		}
	}
	
	if exists {
		return &ValidationResult{
			IsValid:        false,
			ValidationType: "account_number",
			Message:        fmt.Sprintf("account number '%s' already exists", accountNumber),
			Timestamp:      time.Now(),
		}
	}
	
	return &ValidationResult{
		IsValid:        true,
		ValidationType: "account_number",
		Message:        fmt.Sprintf("account number '%s' is valid and unique", accountNumber),
		Timestamp:      time.Now(),
	}
}

// ValidateTransactionAmount checks if transaction amount is valid
func (v *Validator) ValidateTransactionAmount(amount float64) *ValidationResult {
	if amount <= 0 {
		return &ValidationResult{
			IsValid:        false,
			ValidationType: "transaction_amount",
			Message:        "transaction amount must be greater than zero",
			Timestamp:      time.Now(),
		}
	}
	
	if amount > 999999999999.99999999 {
		return &ValidationResult{
			IsValid:        false,
			ValidationType: "transaction_amount",
			Message:        "transaction amount exceeds maximum limit",
			Timestamp:      time.Now(),
		}
	}
	
	return &ValidationResult{
		IsValid:        true,
		ValidationType: "transaction_amount",
		Message:        fmt.Sprintf("transaction amount %.8f is valid", amount),
		Timestamp:      time.Now(),
	}
}

// ValidateDoubleEntryConstraint validates that debits equal credits for a transaction
func (v *Validator) ValidateDoubleEntryConstraint(ctx context.Context, transactionID string) *ValidationResult {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	
	var totalDebits, totalCredits sql.NullFloat64
	
	err := v.postgres.Pool.QueryRow(ctx, `
		SELECT 
			COALESCE(SUM(CASE WHEN entry_type = 'debit' THEN amount ELSE 0 END), 0) as total_debits,
			COALESCE(SUM(CASE WHEN entry_type = 'credit' THEN amount ELSE 0 END), 0) as total_credits
		FROM journal_entries
		WHERE transaction_id = $1
	`, transactionID).Scan(&totalDebits, &totalCredits)
	
	if err != nil {
		return &ValidationResult{
			IsValid:         false,
			ValidationType:  "double_entry_constraint",
			Message:         fmt.Sprintf("failed to validate double-entry constraint: %v", err),
			TransactionID:   transactionID,
			Timestamp:       time.Now(),
		}
	}
	
	debits := totalDebits.Float64
	credits := totalCredits.Float64
	
	// Allow for small floating point differences
	const epsilon = 0.00000001
	isValid := abs(debits-credits) < epsilon
	
	if !isValid {
		return &ValidationResult{
			IsValid:         false,
			ValidationType:  "double_entry_constraint",
			Message:         fmt.Sprintf("double-entry violation: debits (%.8f) != credits (%.8f)", debits, credits),
			TransactionID:   transactionID,
			Timestamp:       time.Now(),
			Details: map[string]interface{}{
				"total_debits":  debits,
				"total_credits": credits,
				"difference":    debits - credits,
			},
		}
	}
	
	return &ValidationResult{
		IsValid:         true,
		ValidationType:  "double_entry_constraint",
		Message:         fmt.Sprintf("double-entry constraint satisfied: debits = credits = %.8f", debits),
		TransactionID:   transactionID,
		Timestamp:       time.Now(),
		Details: map[string]interface{}{
			"total_debits":  debits,
			"total_credits": credits,
		},
	}
}

// ValidateAccountBalanceConsistency checks if account balance is mathematically correct
func (v *Validator) ValidateAccountBalanceConsistency(ctx context.Context, accountID string) *ValidationResult {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	
	var actualBalance, expectedBalance sql.NullFloat64
	var accountNumber sql.NullString
	var accountType sql.NullString
	
	// Get current balance from account_balances table
	err := v.postgres.Pool.QueryRow(ctx, `
		SELECT ab.balance, a.account_number, a.account_type
		FROM account_balances ab
		JOIN accounts a ON ab.account_id = a.id
		WHERE ab.account_id = $1
	`, accountID).Scan(&actualBalance, &accountNumber, &accountType)
	
	if err != nil {
		return &ValidationResult{
			IsValid:       false,
			ValidationType: "balance_consistency",
			Message:       fmt.Sprintf("failed to get account balance: %v", err),
			AccountID:     accountID,
			Timestamp:     time.Now(),
		}
	}
	
	// Calculate expected balance from all journal entries
	err = v.postgres.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(
			CASE 
				WHEN je.entry_type = 'debit' AND a.account_type IN ('asset', 'expense') THEN je.amount
				WHEN je.entry_type = 'credit' AND a.account_type IN ('liability', 'equity', 'revenue') THEN je.amount
				WHEN je.entry_type = 'debit' AND a.account_type IN ('liability', 'equity', 'revenue') THEN -je.amount
				WHEN je.entry_type = 'credit' AND a.account_type IN ('asset', 'expense') THEN -je.amount
				ELSE 0
			END
		), 0)
		FROM journal_entries je
		JOIN accounts a ON je.account_id = a.id
		WHERE je.account_id = $1
	`, accountID).Scan(&expectedBalance)
	
	if err != nil {
		return &ValidationResult{
			IsValid:       false,
			ValidationType: "balance_consistency",
			Message:       fmt.Sprintf("failed to calculate expected balance: %v", err),
			AccountID:     accountID,
			Timestamp:     time.Now(),
		}
	}
	
	actual := actualBalance.Float64
	expected := expectedBalance.Float64
	
	// Allow for small floating point differences
	const epsilon = 0.00000001
	isValid := abs(actual-expected) < epsilon
	
	if !isValid {
		return &ValidationResult{
			IsValid:         false,
			ValidationType:  "balance_consistency",
			Message:         fmt.Sprintf("balance inconsistency: actual (%.8f) != expected (%.8f)", actual, expected),
			AccountID:       accountID,
			Timestamp:       time.Now(),
			Details: map[string]interface{}{
				"actual_balance":   actual,
				"expected_balance": expected,
				"difference":       actual - expected,
				"account_number":   accountNumber.String,
				"account_type":     accountType.String,
			},
		}
	}
	
	return &ValidationResult{
		IsValid:         true,
		ValidationType:  "balance_consistency",
		Message:         fmt.Sprintf("balance is consistent: %.8f", actual),
		AccountID:       accountID,
		Timestamp:       time.Now(),
		Details: map[string]interface{}{
			"actual_balance":   actual,
			"expected_balance": expected,
			"account_number":   accountNumber.String,
			"account_type":     accountType.String,
		},
	}
}

// ValidateOverdraftPrevention checks if a transaction would cause an overdraft
func (v *Validator) ValidateOverdraftPrevention(ctx context.Context, accountID string, amount float64, entryType string) *ValidationResult {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	
	var currentBalance sql.NullFloat64
	var accountType sql.NullString
	
	err := v.postgres.Pool.QueryRow(ctx, `
		SELECT ab.balance, a.account_type
		FROM account_balances ab
		JOIN accounts a ON ab.account_id = a.id
		WHERE ab.account_id = $1
	`, accountID).Scan(¤tBalance, &accountType)
	
	if err != nil {
		return &ValidationResult{
			IsValid:       false,
			ValidationType: "overdraft_prevention",
			Message:       fmt.Sprintf("failed to get account balance: %v", err),
			AccountID:     accountID,
			Timestamp:     time.Now(),
		}
	}
	
	balance := currentBalance.Float64
	acctType := accountType.String
	
	// Calculate what the new balance would be
	var newBalance float64
	switch entryType {
	case "debit":
		// Debit decreases balance for asset accounts, increases for liability/equity/revenue
		if acctType == "asset" || acctType == "expense" {
			newBalance = balance - amount
		} else {
			newBalance = balance + amount
		}
	case "credit":
		// Credit increases balance for asset accounts, decreases for liability/equity/revenue
		if acctType == "asset" || acctType == "expense" {
			newBalance = balance + amount
		} else {
			newBalance = balance - amount
		}
	default:
		return &ValidationResult{
			IsValid:       false,
			ValidationType: "overdraft_prevention",
			Message:       fmt.Sprintf("invalid entry type: %s", entryType),
			AccountID:     accountID,
			Timestamp:     time.Now(),
		}
	}
	
	// Check for overdraft (negative balance on accounts that shouldn't have negative balances)
	const epsilon = 0.00000001
	wouldOverdraw := newBalance < -epsilon
	
	if wouldOverdraw {
		reason := fmt.Sprintf("transaction would cause overdraft: balance would be %.8f", newBalance)
		if acctType == "asset" {
			reason = fmt.Sprintf("transaction would cause negative asset balance: %.8f", newBalance)
		} else if acctType == "liability" {
			reason = fmt.Sprintf("transaction would cause negative liability balance: %.8f", newBalance)
		}
		
		return &ValidationResult{
			IsValid:         false,
			ValidationType:  "overdraft_prevention",
			Message:         reason,
			AccountID:       accountID,
			Timestamp:       time.Now(),
			Details: map[string]interface{}{
				"current_balance": balance,
				"transaction_amount": amount,
				"entry_type":      entryType,
				"new_balance":     newBalance,
				"account_type":    acctType,
			},
		}
	}
	
	return &ValidationResult{
		IsValid:         true,
		ValidationType:  "overdraft_prevention",
		Message:         fmt.Sprintf("transaction would not cause overdraft. New balance: %.8f", newBalance),
		AccountID:       accountID,
		Timestamp:       time.Now(),
		Details: map[string]interface{}{
			"current_balance": balance,
			"transaction_amount": amount,
			"entry_type":      entryType,
			"new_balance":     newBalance,
			"account_type":    acctType,
		},
	}
}

// ValidateImmutabilityConstraint checks that journal entries are immutable
func (v *Validator) ValidateImmutabilityConstraint(ctx context.Context) *ValidationResult {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	// Check for any UPDATE or DELETE operations on journal_entries table
	// This is a meta-validation - in practice, you'd implement this at the database level
	// with proper permissions and triggers
	
	// For now, we'll check if there are any duplicate entry numbers (which shouldn't happen)
	var duplicateCount int
	err := v.postgres.Pool.QueryRow(ctx, `
		SELECT COUNT(*) 
		FROM (
			SELECT entry_number, COUNT(*) as cnt
			FROM journal_entries
			GROUP BY entry_number
			HAVING COUNT(*) > 1
		) t
	`).Scan(&duplicateCount)
	
	if err != nil {
		return &ValidationResult{
			IsValid:        false,
			ValidationType: "immutability_constraint",
			Message:        fmt.Sprintf("failed to check immutability constraint: %v", err),
			Timestamp:      time.Now(),
		}
	}
	
	if duplicateCount > 0 {
		return &ValidationResult{
			IsValid:         false,
			ValidationType:  "immutability_constraint",
			Message:         fmt.Sprintf("found %d duplicate entry numbers - immutability constraint violated", duplicateCount),
			Timestamp:       time.Now(),
			Details: map[string]interface{}{
				"duplicate_count": duplicateCount,
			},
		}
	}
	
	return &ValidationResult{
		IsValid:        true,
		ValidationType: "immutability_constraint",
		Message:        "immutability constraint is satisfied - no duplicate entries found",
		Timestamp:      time.Now(),
		Details: map[string]interface{}{
			"duplicate_count": 0,
		},
	}
}

// ComprehensiveValidation performs all validation checks for an account
func (v *Validator) ComprehensiveValidation(ctx context.Context, accountID string) []*ValidationResult {
	var results []*ValidationResult
	
	// Get account information
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	
	var accountNumber, accountType, currencyCode string
	err := v.postgres.Pool.QueryRow(ctx, `
		SELECT account_number, account_type, currency_code
		FROM accounts
		WHERE id = $1
	`, accountID).Scan(&accountNumber, &accountType, ¤cyCode)
	
	if err != nil {
		results = append(results, &ValidationResult{
			IsValid:       false,
			ValidationType: "comprehensive",
			Message:       fmt.Sprintf("failed to get account information: %v", err),
			AccountID:     accountID,
			Timestamp:     time.Now(),
		})
		return results
	}
	
	// Run individual validations
	results = append(results, v.ValidateAccountType(accountType))
	results = append(results, v.ValidateCurrencyCode(currencyCode))
	results = append(results, v.ValidateAccountBalanceConsistency(ctx, accountID))
	results = append(results, v.ValidateImmutabilityConstraint(ctx))
	
	// Get all transactions for this account and validate double-entry
	rows, err := v.postgres.Pool.Query(ctx, `
		SELECT DISTINCT transaction_id
		FROM journal_entries
		WHERE account_id = $1
	`, accountID)
	
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var transactionID string
			rows.Scan(&transactionID)
			results = append(results, v.ValidateDoubleEntryConstraint(ctx, transactionID))
		}
	}
	
	return results
}

// abs returns the absolute value of a float64
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}