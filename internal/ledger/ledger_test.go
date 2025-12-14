package ledger

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SimpleMockPool provides a simplified mock for testing
type SimpleMockPool struct {
	execFunc func(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
	queryFunc func(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	queryRowFunc func(ctx context.Context, sql string, args ...interface{}) pgx.Row
}

func (m *SimpleMockPool) Acquire(ctx context.Context) (*pgx.Conn, error) {
	return &pgx.Conn{}, nil
}

func (m *SimpleMockPool) Close() {}

func (m *SimpleMockPool) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (m *SimpleMockPool) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, sql, args...)
	}
	return &mockRows{}, nil
}

func (m *SimpleMockPool) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if m.queryRowFunc != nil {
		return m.queryRowFunc(ctx, sql, args...)
	}
	return &mockRow{}
}

type mockRow struct {
	scanned bool
}

func (r *mockRow) Scan(dest ...interface{}) error {
	for _, d := range dest {
		if sqlVar, ok := d.(*sql.NullString); ok {
			*sqlVar = sql.NullString{String: "", Valid: false}
		} else if floatVar, ok := d.(*sql.NullFloat64); ok {
			*floatVar = sql.NullFloat64{Float64: 0, Valid: false}
		} else if boolVar, ok := d.(*sql.NullBool); ok {
			*boolVar = sql.NullBool{Bool: false, Valid: false}
		}
	}
	return pgx.ErrNoRows
}

type mockRows struct {
	closed bool
}

func (r *mockRows) Close() {
	r.closed = true
}

func (r *mockRows) Err() error {
	return nil
}

func (r *mockRows) Next() bool {
	return false
}

func (r *mockRows) Scan(dest ...interface{}) error {
	return nil
}

// TestCreateAccount tests the CreateAccount functionality
func TestCreateAccount(t *testing.T) {
	ctx := context.Background()
	
	mockPool := &SimpleMockPool{
		queryRowFunc: func(ctx context.Context, sql string, args ...interface{}) pgx.Row {
			switch {
			case contains(sql, "EXISTS"):
				return &mockRowWithValues{values: []interface{}{false}}
			case contains(sql, "SELECT") && contains(sql, "accounts"):
				return &mockRowWithValues{values: []interface{}{
					"acc-123", "ACC001", "asset", "Test Account", "USD", true, 
					"2024-01-01T00:00:00Z", "test_user", nil, 0.0,
				}}
			}
			return &mockRow{}
		},
	}
	
	postgresLedger := &PostgresLedger{Pool: mockPool}
	
	account, err := postgresLedger.CreateAccount(ctx, "ACC001", "asset", "Test Account", "USD", "test_user", map[string]interface{}{})
	
	require.NoError(t, err)
	assert.NotNil(t, account)
	assert.Equal(t, "ACC001", account.AccountNumber)
	assert.Equal(t, "asset", account.AccountType)
	assert.Equal(t, "Test Account", account.Name)
	assert.Equal(t, "USD", account.CurrencyCode)
}

// TestPostJournalEntry tests posting journal entries
func TestPostJournalEntry(t *testing.T) {
	ctx := context.Background()
	
	mockPool := &SimpleMockPool{
		queryRowFunc: func(ctx context.Context, sql string, args ...interface{}) pgx.Row {
			if contains(sql, "SELECT account_type, is_active") {
				return &mockRowWithValues{values: []interface{}{"asset", true}}
			}
			return &mockRow{}
		},
	}
	
	postgresLedger := &PostgresLedger{Pool: mockPool}
	
	entry := &JournalEntry{
		EntryNumber:   "JE-12345678-1234567890",
		TransactionID: "txn-123",
		EntryType:     "debit",
		AccountID:     "acc-123",
		Amount:        100.0,
		Description:   "Test debit",
		CurrencyCode:  "USD",
		CreatedBy:     "test_user",
		Metadata:      map[string]interface{}{},
	}
	
	err := postgresLedger.PostJournalEntry(ctx, entry)
	
	require.NoError(t, err)
}

// TestGetBalance tests getting account balance
func TestGetBalance(t *testing.T) {
	ctx := context.Background()
	
	mockPool := &SimpleMockPool{
		queryRowFunc: func(ctx context.Context, sql string, args ...interface{}) pgx.Row {
			if contains(sql, "SELECT COALESCE(balance") {
				return &mockRowWithValues{values: []interface{}{150.75}}
			}
			return &mockRow{}
		},
	}
	
	postgresLedger := &PostgresLedger{Pool: mockPool}
	
	balance, err := postgresLedger.GetBalance(ctx, "acc-123")
	
	require.NoError(t, err)
	assert.Equal(t, 150.75, balance)
}

// TestValidateAccountType tests account type validation
func TestValidateAccountType(t *testing.T) {
	validator := &Validator{}
	
	// Valid account types
	assetResult := validator.ValidateAccountType("asset")
	assert.True(t, assetResult.IsValid)
	assert.Equal(t, "account_type", assetResult.ValidationType)
	
	liabilityResult := validator.ValidateAccountType("liability")
	assert.True(t, liabilityResult.IsValid)
	
	// Invalid account type
	invalidResult := validator.ValidateAccountType("invalid")
	assert.False(t, invalidResult.IsValid)
	assert.Contains(t, invalidResult.Message, "invalid account type")
}

// TestValidateCurrencyCode tests currency code validation
func TestValidateCurrencyCode(t *testing.T) {
	validator := &Validator{}
	
	// Valid currency codes
	usdResult := validator.ValidateCurrencyCode("USD")
	assert.True(t, usdResult.IsValid)
	
	eurResult := validator.ValidateCurrencyCode("EUR")
	assert.True(t, eurResult.IsValid)
	
	// Invalid currency codes
	invalidLengthResult := validator.ValidateCurrencyCode("US")
	assert.False(t, invalidLengthResult.IsValid)
	assert.Contains(t, invalidLengthResult.Message, "must be exactly 3 characters")
	
	invalidFormatResult := validator.ValidateCurrencyCode("usd")
	assert.False(t, invalidFormatResult.IsValid)
	assert.Contains(t, invalidFormatResult.Message, "must contain only uppercase letters")
}

// TestValidateTransactionAmount tests transaction amount validation
func TestValidateTransactionAmount(t *testing.T) {
	validator := &Validator{}
	
	// Valid amounts
	validResult := validator.ValidateTransactionAmount(100.50)
	assert.True(t, validResult.IsValid)
	assert.Contains(t, validResult.Message, "is valid")
	
	// Invalid amounts
	zeroAmountResult := validator.ValidateTransactionAmount(0)
	assert.False(t, zeroAmountResult.IsValid)
	assert.Contains(t, zeroAmountResult.Message, "must be greater than zero")
	
	negativeAmountResult := validator.ValidateTransactionAmount(-50.0)
	assert.False(t, negativeAmountResult.IsValid)
	assert.Contains(t, negativeAmountResult.Message, "must be greater than zero")
}

// TestLedgerService_CreateAccount tests the ledger service CreateAccount method
func TestLedgerService_CreateAccount(t *testing.T) {
	ctx := context.Background()
	
	mockPostgres := &PostgresLedger{Pool: &SimpleMockPool{}}
	ledgerService := NewLedgerService(mockPostgres)
	
	// Test invalid account type
	invalidReq := CreateAccountRequest{
		AccountNumber: "ACC001",
		AccountType:   "invalid",
		Name:          "Test Account",
		CurrencyCode:  "USD",
		CreatedBy:     "test_user",
		Metadata:      map[string]interface{}{},
	}
	
	_, err := ledgerService.CreateAccount(ctx, invalidReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid account type")
	
	// Test invalid currency code
	invalidCurrencyReq := CreateAccountRequest{
		AccountNumber: "ACC002",
		AccountType:   "asset",
		Name:          "Test Account",
		CurrencyCode:  "US", // Too short
		CreatedBy:     "test_user",
		Metadata:      map[string]interface{}{},
	}
	
	_, err = ledgerService.CreateAccount(ctx, invalidCurrencyReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "currency code must be 3 characters")
}

// TestLedgerService_CreditDebit tests credit and debit operations
func TestLedgerService_CreditDebit(t *testing.T) {
	ctx := context.Background()
	
	mockPostgres := &PostgresLedger{Pool: &SimpleMockPool{}}
	ledgerService := NewLedgerService(mockPostgres)
	
	// Test invalid amount in credit
	creditReq := CreditRequest{
		AccountID:   "acc-123",
		Amount:      0, // Invalid amount
		Description: "Test credit",
		TransactionRequest: TransactionRequest{
			TransactionID: "txn-123",
			CurrencyCode:  "USD",
			CreatedBy:     "test_user",
		},
	}
	
	err := ledgerService.Credit(ctx, creditReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "amount must be positive")
	
	// Test invalid amount in debit
	debitReq := DebitRequest{
		AccountID:   "acc-123",
		Amount:      -50.0, // Invalid amount
		Description: "Test debit",
		TransactionRequest: TransactionRequest{
			TransactionID: "txn-123",
			CurrencyCode:  "USD",
			CreatedBy:     "test_user",
		},
	}
	
	err = ledgerService.Debit(ctx, debitReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "amount must be positive")
}

// TestLedgerService_Transfer tests transfer operations
func TestLedgerService_Transfer(t *testing.T) {
	ctx := context.Background()
	
	mockPostgres := &PostgresLedger{Pool: &SimpleMockPool{}}
	ledgerService := NewLedgerService(mockPostgres)
	
	// Test invalid transfer - same account
	sameAccountReq := TransferRequest{
		FromAccountID: "acc-123",
		ToAccountID:   "acc-123", // Same as from
		Amount:        100.0,
		Description:   "Test transfer",
		CurrencyCode:  "USD",
		CreatedBy:     "test_user",
	}
	
	err := ledgerService.Transfer(ctx, sameAccountReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be different")
	
	// Test invalid transfer - negative amount
	negativeAmountReq := TransferRequest{
		FromAccountID: "acc-123",
		ToAccountID:   "acc-456",
		Amount:        -100.0, // Invalid amount
		Description:   "Test transfer",
		CurrencyCode:  "USD",
		CreatedBy:     "test_user",
	}
	
	err = ledgerService.Transfer(ctx, negativeAmountReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "amount must be positive")
}

// TestReconcileRequest_Validation tests reconcile request validation
func TestReconcileRequest_Validation(t *testing.T) {
	ctx := context.Background()
	
	mockPostgres := &PostgresLedger{Pool: &SimpleMockPool{}}
	ledgerService := NewLedgerService(mockPostgres)
	
	// Test missing account ID
	reconcileReq := ReconcileRequest{
		AccountID: "", // Missing
		StartTime: time.Now().Add(-time.Hour),
		EndTime:   time.Now(),
	}
	
	_, err := ledgerService.Reconcile(ctx, reconcileReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account ID is required")
	
	// Test invalid time range
	invalidTimeReq := ReconcileRequest{
		AccountID: "acc-123",
		StartTime: time.Now(),       // After end time
		EndTime:   time.Now().Add(-time.Hour),
	}
	
	_, err = ledgerService.Reconcile(ctx, invalidTimeReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start time must be before end time")
}

// TestAbsFunction tests the absolute value function
func TestAbsFunction(t *testing.T) {
	testCases := []struct {
		input    float64
		expected float64
	}{
		{5.5, 5.5},
		{-5.5, 5.5},
		{0.0, 0.0},
		{-0.0, 0.0},
		{123.456, 123.456},
		{-123.456, 123.456},
	}
	
	for _, tc := range testCases {
		result := abs(tc.input)
		assert.Equal(t, tc.expected, result)
	}
}

// Helper structs for testing
type mockRowWithValues struct {
	values []interface{}
}

func (r *mockRowWithValues) Scan(dest ...interface{}) error {
	for i, d := range dest {
		if i < len(r.values) {
			switch v := dest[i].(type) {
			case *string:
				if str, ok := r.values[i].(string); ok {
					*v = str
				}
			case *sql.NullString:
				if str, ok := r.values[i].(string); ok {
					*v = sql.NullString{String: str, Valid: true}
				}
			case *sql.NullFloat64:
				if f, ok := r.values[i].(float64); ok {
					*v = sql.NullFloat64{Float64: f, Valid: true}
				}
			case *sql.NullBool:
				if b, ok := r.values[i].(bool); ok {
					*v = sql.NullBool{Bool: b, Valid: true}
				}
			}
		}
	}
	return nil
}

// Benchmark tests for performance
func BenchmarkCreateAccount(b *testing.B) {
	ctx := context.Background()
	
	mockPool := &SimpleMockPool{}
	postgresLedger := &PostgresLedger{Pool: mockPool}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := postgresLedger.CreateAccount(ctx, "ACC001", "asset", "Test Account", "USD", "test_user", map[string]interface{}{})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateAccountType(b *testing.B) {
	validator := &Validator{}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := validator.ValidateAccountType("asset")
		if !result.IsValid {
			b.Fatal("Expected valid result")
		}
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && 
		   (s == substr || (len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || findSubstring(s, substr))))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}