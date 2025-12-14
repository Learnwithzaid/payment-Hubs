package ledger

import (
    "context"
    "database/sql"
    "fmt"
    "testing"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// IntegrationTestPostgres provides integration testing with real PostgreSQL
type IntegrationTestPostgres struct {
    pool *pgxpool.Pool
    ctx  context.Context
}

// NewIntegrationTestPostgres creates a new integration test instance
func NewIntegrationTestPostgres(ctx context.Context, databaseURL string) (*IntegrationTestPostgres, error) {
    pool, err := pgxpool.New(ctx, databaseURL)
    if err != nil {
        return nil, fmt.Errorf("failed to create pool: %w", err)
    }

    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        return nil, fmt.Errorf("failed to ping database: %w", err)
    }

    return &IntegrationTestPostgres{
        pool: pool,
        ctx:  ctx,
    }, nil
}

// Close closes the database connection
func (itp *IntegrationTestPostgres) Close() {
    itp.pool.Close()
}

// SetupDatabase runs database migrations for testing
func (itp *IntegrationTestPostgres) SetupDatabase() error {
    migrations := []string{
        `CREATE TABLE IF NOT EXISTS accounts (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            account_number TEXT UNIQUE NOT NULL,
            account_type TEXT NOT NULL CHECK (account_type IN ('asset', 'liability', 'equity', 'revenue', 'expense')),
            name TEXT NOT NULL,
            currency_code TEXT NOT NULL DEFAULT 'USD' CHECK (length(currency_code) = 3),
            is_active BOOLEAN NOT NULL DEFAULT TRUE,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            created_by TEXT NOT NULL,
            metadata JSONB DEFAULT '{}'
        );`,
        
        `CREATE TABLE IF NOT EXISTS account_balances (
            account_id UUID PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
            balance NUMERIC(20, 8) NOT NULL DEFAULT 0,
            updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
        );`,
        
        `CREATE TABLE IF NOT EXISTS journal_entries (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            entry_number TEXT UNIQUE NOT NULL,
            transaction_id UUID NOT NULL,
            entry_type TEXT NOT NULL CHECK (entry_type IN ('debit', 'credit')),
            account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
            account_type TEXT NOT NULL CHECK (account_type IN ('asset', 'liability', 'equity', 'revenue', 'expense')),
            amount NUMERIC(20, 8) NOT NULL CHECK (amount > 0),
            description TEXT NOT NULL,
            reference_type TEXT,
            reference_id TEXT,
            currency_code TEXT NOT NULL DEFAULT 'USD' CHECK (length(currency_code) = 3),
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            created_by TEXT NOT NULL,
            metadata JSONB DEFAULT '{}'
        );`,
        
        `CREATE TABLE IF NOT EXISTS balance_snapshots (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
            transaction_id UUID NOT NULL,
            snapshot_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            balance_before NUMERIC(20, 8) NOT NULL,
            balance_after NUMERIC(20, 8) NOT NULL,
            balance_change NUMERIC(20, 8) NOT NULL,
            account_type TEXT NOT NULL CHECK (account_type IN ('asset', 'liability', 'equity', 'revenue', 'expense')),
            currency_code TEXT NOT NULL DEFAULT 'USD' CHECK (length(currency_code) = 3),
            entry_id UUID NOT NULL REFERENCES journal_entries(id) ON DELETE RESTRICT,
            entry_type TEXT NOT NULL CHECK (entry_type IN ('debit', 'credit')),
            amount NUMERIC(20, 8) NOT NULL,
            description TEXT NOT NULL,
            reference_type TEXT,
            reference_id TEXT,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
        );`,
    }

    for _, migration := range migrations {
        _, err := itp.pool.Exec(itp.ctx, migration)
        if err != nil {
            return fmt.Errorf("failed to execute migration: %w", err)
        }
    }

    return nil
}

// TeardownDatabase cleans up test data
func (itp *IntegrationTestPostgres) TeardownDatabase() error {
    // Clean up in reverse dependency order
    tables := []string{"balance_snapshots", "journal_entries", "account_balances", "accounts"}
    
    for _, table := range tables {
        _, err := itp.pool.Exec(itp.ctx, fmt.Sprintf("DELETE FROM %s", table))
        if err != nil {
            return fmt.Errorf("failed to clean up %s: %w", table, err)
        }
    }
    
    return nil
}

// TestFullLedgerWorkflow tests the complete ledger workflow
func TestFullLedgerWorkflow(t *testing.T) {
    ctx := context.Background()
    
    // Get database URL from environment or use default test database
    dbURL := "postgres://ledger:password@localhost:5432/ledger_test"
    if envDBURL := getenv("DATABASE_URL"); envDBURL != "" {
        dbURL = envDBURL
    }
    
    // Create integration test instance
    itp, err := NewIntegrationTestPostgres(ctx, dbURL)
    if err != nil {
        t.Skipf("skipping postgres integration test (database not available): %v", err)
    }
    defer itp.Close()
    
    // Setup database
    require.NoError(t, itp.SetupDatabase())
    defer itp.TeardownDatabase()
    
    // Create PostgreSQL ledger instance
    postgresLedger := NewPostgresLedger(itp.pool)
    ledgerService := NewLedgerService(postgresLedger)
    validator := NewValidator(postgresLedger)
    
    // Test Account Creation
    t.Run("AccountCreation", func(t *testing.T) {
        // Create test accounts
        assetAccount, err := postgresLedger.CreateAccount(ctx, "ASSET001", "asset", "Test Asset Account", "USD", "test_user", map[string]interface{}{"test": true})
        require.NoError(t, err)
        assert.NotNil(t, assetAccount)
        assert.Equal(t, "ASSET001", assetAccount.AccountNumber)
        assert.Equal(t, "asset", assetAccount.AccountType)
        
        liabilityAccount, err := postgresLedger.CreateAccount(ctx, "LIAB001", "liability", "Test Liability Account", "USD", "test_user", map[string]interface{}{"test": true})
        require.NoError(t, err)
        assert.NotNil(t, liabilityAccount)
        assert.Equal(t, "LIAB001", liabilityAccount.AccountNumber)
        assert.Equal(t, "liability", liabilityAccount.AccountType)
        
        // Test duplicate account creation
        _, err = postgresLedger.CreateAccount(ctx, "ASSET001", "asset", "Duplicate Account", "USD", "test_user", map[string]interface{}{})
        require.Error(t, err)
        assert.Contains(t, err.Error(), "already exists")
    })
    
    // Test Initial Balance
    t.Run("InitialBalance", func(t *testing.T) {
        assetAccount, err := postgresLedger.GetAccount(ctx, "ASSET001")
        require.NoError(t, err)
        assert.Equal(t, 0.0, assetAccount.CurrentBalance)
        
        liabilityAccount, err := postgresLedger.GetAccount(ctx, "LIAB001")
        require.NoError(t, err)
        assert.Equal(t, 0.0, liabilityAccount.CurrentBalance)
    })
    
    // Test Double-Entry Transaction
    t.Run("DoubleEntryTransaction", func(t *testing.T) {
        // Get account IDs
        assetAccount, err := postgresLedger.GetAccount(ctx, "ASSET001")
        require.NoError(t, err)
        
        liabilityAccount, err := postgresLedger.GetAccount(ctx, "LIAB001")
        require.NoError(t, err)
        
        transactionID := "txn-001"
        
        // Create debit entry (increase asset)
        debitEntry := &JournalEntry{
            EntryNumber:   "JE-txn-001-001",
            TransactionID: transactionID,
            EntryType:     "debit",
            AccountID:     assetAccount.ID,
            AccountType:   "asset",
            Amount:        1000.0,
            Description:   "Initial funding",
            CurrencyCode:  "USD",
            CreatedBy:     "test_user",
            Metadata:      map[string]interface{}{},
        }
        
        err = postgresLedger.PostJournalEntry(ctx, debitEntry)
        require.NoError(t, err)
        
        // Create credit entry (increase liability)
        creditEntry := &JournalEntry{
            EntryNumber:   "JE-txn-001-002",
            TransactionID: transactionID,
            EntryType:     "credit",
            AccountID:     liabilityAccount.ID,
            AccountType:   "liability",
            Amount:        1000.0,
            Description:   "Initial funding",
            CurrencyCode:  "USD",
            CreatedBy:     "test_user",
            Metadata:      map[string]interface{}{},
        }
        
        err = postgresLedger.PostJournalEntry(ctx, creditEntry)
        require.NoError(t, err)
        
        // Verify balances
        assetBalance, err := postgresLedger.GetBalance(ctx, assetAccount.ID)
        require.NoError(t, err)
        assert.Equal(t, 1000.0, assetBalance)
        
        liabilityBalance, err := postgresLedger.GetBalance(ctx, liabilityAccount.ID)
        require.NoError(t, err)
        assert.Equal(t, 1000.0, liabilityBalance)
        
        // Validate double-entry constraint
        validationResult := validator.ValidateDoubleEntryConstraint(ctx, transactionID)
        assert.True(t, validationResult.IsValid)
        assert.Equal(t, "double_entry_constraint", validationResult.ValidationType)
    })
    
    // Test Overdraft Prevention
    t.Run("OverdraftPrevention", func(t *testing.T) {
        assetAccount, err := postgresLedger.GetAccount(ctx, "ASSET001")
        require.NoError(t, err)
        
        // Try to debit more than available balance
        overdraftResult := validator.ValidateOverdraftPrevention(ctx, assetAccount.ID, 2000.0, "debit")
        assert.False(t, overdraftResult.IsValid)
        assert.Contains(t, overdraftResult.Message, "transaction would cause")
        
        // Valid debit (within balance)
        validDebitResult := validator.ValidateOverdraftPrevention(ctx, assetAccount.ID, 500.0, "debit")
        assert.True(t, validDebitResult.IsValid)
    })
    
    // Test Balance Consistency
    t.Run("BalanceConsistency", func(t *testing.T) {
        assetAccount, err := postgresLedger.GetAccount(ctx, "ASSET001")
        require.NoError(t, err)
        
        consistencyResult := validator.ValidateAccountBalanceConsistency(ctx, assetAccount.ID)
        assert.True(t, consistencyResult.IsValid)
        assert.Equal(t, "balance_consistency", consistencyResult.ValidationType)
        
        liabilityAccount, err := postgresLedger.GetAccount(ctx, "LIAB001")
        require.NoError(t, err)
        
        liabilityConsistencyResult := validator.ValidateAccountBalanceConsistency(ctx, liabilityAccount.ID)
        assert.True(t, liabilityConsistencyResult.IsValid)
    })
    
    // Test Reconcile Transactions
    t.Run("ReconcileTransactions", func(t *testing.T) {
        assetAccount, err := postgresLedger.GetAccount(ctx, "ASSET001")
        require.NoError(t, err)
        
        startTime := time.Now().Add(-10 * time.Minute)
        endTime := time.Now().Add(10 * time.Minute)
        
        snapshots, err := postgresLedger.ReconcileTransactions(ctx, assetAccount.ID, startTime, endTime)
        require.NoError(t, err)
        assert.NotNil(t, snapshots)
        assert.True(t, len(snapshots) > 0)
        
        // Check first snapshot
        if len(snapshots) > 0 {
            snapshot := snapshots[0]
            assert.NotEmpty(t, snapshot.ID)
            assert.Equal(t, assetAccount.ID, snapshot.AccountID)
            assert.Equal(t, "debit", snapshot.EntryType)
            assert.Equal(t, 1000.0, snapshot.Amount)
        }
    })
    
    // Test Comprehensive Validation
    t.Run("ComprehensiveValidation", func(t *testing.T) {
        assetAccount, err := postgresLedger.GetAccount(ctx, "ASSET001")
        require.NoError(t, err)
        
        results := validator.ComprehensiveValidation(ctx, assetAccount.ID)
        assert.NotNil(t, results)
        assert.True(t, len(results) > 0)
        
        // All validation results should be valid for our test account
        for _, result := range results {
            assert.True(t, result.IsValid, "Validation %s failed: %s", result.ValidationType, result.Message)
        }
    })
    
    // Test Transfer Operation
    t.Run("TransferOperation", func(t *testing.T) {
        assetAccount, err := postgresLedger.GetAccount(ctx, "ASSET001")
        require.NoError(t, err)
        
        liabilityAccount, err := postgresLedger.GetAccount(ctx, "LIAB001")
        require.NoError(t, err)
        
        // Get current balances
        initialAssetBalance, err := postgresLedger.GetBalance(ctx, assetAccount.ID)
        require.NoError(t, err)
        
        initialLiabilityBalance, err := postgresLedger.GetBalance(ctx, liabilityAccount.ID)
        require.NoError(t, err)
        
        // Perform transfer
        transferReq := TransferRequest{
            FromAccountID: assetAccount.ID,
            ToAccountID:   liabilityAccount.ID,
            Amount:        250.0,
            Description:   "Transfer test",
            CurrencyCode:  "USD",
            CreatedBy:     "test_user",
            ReferenceType: "test",
            ReferenceID:   "transfer-001",
            Metadata:      map[string]interface{}{},
        }
        
        err = ledgerService.Transfer(ctx, transferReq)
        require.NoError(t, err)
        
        // Verify balances changed correctly
        newAssetBalance, err := postgresLedger.GetBalance(ctx, assetAccount.ID)
        require.NoError(t, err)
        assert.Equal(t, initialAssetBalance-250.0, newAssetBalance)
        
        newLiabilityBalance, err := postgresLedger.GetBalance(ctx, liabilityAccount.ID)
        require.NoError(t, err)
        assert.Equal(t, initialLiabilityBalance+250.0, newLiabilityBalance)
    })
    
    // Test Final Balance Consistency Check
    t.Run("FinalBalanceConsistency", func(t *testing.T) {
        results, err := postgresLedger.ValidateBalanceConsistency(ctx)
        require.NoError(t, err)
        
        // Check for any inconsistencies
        for _, result := range results {
            if accountID, ok := result["account_id"].(string); ok {
                // Check if this is one of our test accounts
                if accountID != "" {
                    isConsistent, ok := result["is_consistent"].(bool)
                    if ok {
                        assert.True(t, isConsistent, "Account %s has inconsistent balance", accountID)
                    }
                }
            }
        }
    })
}

// TestAccountTypeValidationIntegration tests account type validation with real database
func TestAccountTypeValidationIntegration(t *testing.T) {
    ctx := context.Background()
    
    dbURL := "postgres://ledger:password@localhost:5432/ledger_test"
    if envDBURL := getenv("DATABASE_URL"); envDBURL != "" {
        dbURL = envDBURL
    }
    
    itp, err := NewIntegrationTestPostgres(ctx, dbURL)
    require.NoError(t, err)
    defer itp.Close()
    
    require.NoError(t, itp.SetupDatabase())
    defer itp.TeardownDatabase()
    
    postgresLedger := NewPostgresLedger(itp.pool)
    validator := NewValidator(postgresLedger)
    
    // Test valid account types
    validTypes := []string{"asset", "liability", "equity", "revenue", "expense"}
    for _, accountType := range validTypes {
        result := validator.ValidateAccountType(accountType)
        assert.True(t, result.IsValid, "Account type %s should be valid", accountType)
    }
    
    // Test invalid account types
    invalidTypes := []string{"invalid", "bank", "cash", "credit", ""}
    for _, accountType := range invalidTypes {
        result := validator.ValidateAccountType(accountType)
        assert.False(t, result.IsValid, "Account type %s should be invalid", accountType)
        assert.Contains(t, result.Message, "invalid account type")
    }
}

// TestCurrencyCodeValidationIntegration tests currency validation with real database
func TestCurrencyCodeValidationIntegration(t *testing.T) {
    ctx := context.Background()
    
    dbURL := "postgres://ledger:password@localhost:5432/ledger_test"
    if envDBURL := getenv("DATABASE_URL"); envDBURL != "" {
        dbURL = envDBURL
    }
    
    itp, err := NewIntegrationTestPostgres(ctx, dbURL)
    require.NoError(t, err)
    defer itp.Close()
    
    require.NoError(t, itp.SetupDatabase())
    defer itp.TeardownDatabase()
    
    validator := NewValidator(&PostgresLedger{Pool: itp.pool})
    
    // Test valid currency codes
    validCurrencies := []string{"USD", "EUR", "GBP", "JPY", "AUD", "CAD", "CHF", "CNY"}
    for _, currency := range validCurrencies {
        result := validator.ValidateCurrencyCode(currency)
        assert.True(t, result.IsValid, "Currency code %s should be valid", currency)
    }
    
    // Test invalid currency codes
    invalidCurrencies := []string{"US", "US Dollar", "usd", "US$", ""}
    for _, currency := range invalidCurrencies {
        result := validator.ValidateCurrencyCode(currency)
        assert.False(t, result.IsValid, "Currency code %s should be invalid", currency)
    }
}

// TestImmutabilityConstraintIntegration tests immutability constraints
func TestImmutabilityConstraintIntegration(t *testing.T) {
    ctx := context.Background()
    
    dbURL := "postgres://ledger:password@localhost:5432/ledger_test"
    if envDBURL := getenv("DATABASE_URL"); envDBURL != "" {
        dbURL = envDBURL
    }
    
    itp, err := NewIntegrationTestPostgres(ctx, dbURL)
    require.NoError(t, err)
    defer itp.Close()
    
    require.NoError(t, itp.SetupDatabase())
    defer itp.TeardownDatabase()
    
    postgresLedger := NewPostgresLedger(itp.pool)
    validator := NewValidator(postgresLedger)
    
    // Create test account
    assetAccount, err := postgresLedger.CreateAccount(ctx, "IMMUT001", "asset", "Immutability Test", "USD", "test_user", map[string]interface{}{})
    require.NoError(t, err)
    
    // Post a transaction
    debitEntry := &JournalEntry{
        EntryNumber:   "JE-IMMUT-001",
        TransactionID: "txn-immut-001",
        EntryType:     "debit",
        AccountID:     assetAccount.ID,
        AccountType:   "asset",
        Amount:        500.0,
        Description:   "Immutability test",
        CurrencyCode:  "USD",
        CreatedBy:     "test_user",
        Metadata:      map[string]interface{}{},
    }
    
    err = postgresLedger.PostJournalEntry(ctx, debitEntry)
    require.NoError(t, err)
    
    // Validate immutability constraint
    immutabilityResult := validator.ValidateImmutabilityConstraint(ctx)
    assert.True(t, immutabilityResult.IsValid)
    assert.Equal(t, "immutability_constraint", immutabilityResult.ValidationType)
}

// TestSerializationFailureHandling tests serialization failure handling
func TestSerializationFailureHandling(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping serialization test in short mode")
    }
    
    ctx := context.Background()
    
    dbURL := "postgres://ledger:password@localhost:5432/ledger_test"
    if envDBURL := getenv("DATABASE_URL"); envDBURL != "" {
        dbURL = envDBURL
    }
    
    itp, err := NewIntegrationTestPostgres(ctx, dbURL)
    require.NoError(t, err)
    defer itp.Close()
    
    require.NoError(t, itp.SetupDatabase())
    defer itp.TeardownDatabase()
    
    postgresLedger := NewPostgresLedger(itp.pool)
    
    // Create test account
    assetAccount, err := postgresLedger.CreateAccount(ctx, "SERIAL001", "asset", "Serialization Test", "USD", "test_user", map[string]interface{}{})
    require.NoError(t, err)
    
    // Test concurrent account creation (this would normally trigger serialization failures)
    const concurrentAttempts = 5
    errors := make(chan error, concurrentAttempts)
    
    for i := 0; i < concurrentAttempts; i++ {
        go func(attempt int) {
            accountNumber := fmt.Sprintf("CONCURRENT%d", attempt)
            _, err := postgresLedger.CreateAccount(ctx, accountNumber, "asset", fmt.Sprintf("Concurrent Account %d", attempt), "USD", "test_user", map[string]interface{}{})
            errors <- err
        }(i)
    }
    
    // Collect results
    var successCount, failureCount int
    for i := 0; i < concurrentAttempts; i++ {
        err := <-errors
        if err == nil {
            successCount++
        } else {
            failureCount++
            // We expect some failures due to concurrent creation attempts, but not all
            if failureCount == concurrentAttempts {
                t.Errorf("All concurrent attempts failed: %v", err)
            }
        }
    }
    
    // At least one should succeed
    assert.Greater(t, successCount, 0, "At least one concurrent account creation should succeed")
}

// Helper function to get environment variable with fallback
func getenv(key string) string {
    if value := getEnvFunc(key); value != "" {
        return value
    }
    return ""
}

// This will be overridden during testing
var getEnvFunc = func(key string) string {
    return ""
}

// Benchmark tests for performance
func BenchmarkFullWorkflow(b *testing.B) {
    ctx := context.Background()
    
    dbURL := "postgres://ledger:password@localhost:5432/ledger_test"
    if envDBURL := getenv("DATABASE_URL"); envDBURL != "" {
        dbURL = envDBURL
    }
    
    itp, err := NewIntegrationTestPostgres(ctx, dbURL)
    require.NoError(b, err)
    defer itp.Close()
    
    require.NoError(b, itp.SetupDatabase())
    defer itp.TeardownDatabase()
    
    postgresLedger := NewPostgresLedger(itp.pool)
    ledgerService := NewLedgerService(postgresLedger)
    
    b.ResetTimer()
    
    for i := 0; i < b.N; i++ {
        // Create account
        account, err := postgresLedger.CreateAccount(ctx, fmt.Sprintf("PERF%d", i), "asset", fmt.Sprintf("Performance Test %d", i), "USD", "test_user", map[string]interface{}{})
        if err != nil {
            b.Fatalf("Failed to create account: %v", err)
        }
        
        // Create transaction
        transactionID := fmt.Sprintf("txn-perf-%d", i)
        
        debitEntry := &JournalEntry{
            EntryNumber:   fmt.Sprintf("JE-perf-%d", i),
            TransactionID: transactionID,
            EntryType:     "debit",
            AccountID:     account.ID,
            AccountType:   "asset",
            Amount:        100.0,
            Description:   "Performance test",
            CurrencyCode:  "USD",
            CreatedBy:     "test_user",
            Metadata:      map[string]interface{}{},
        }
        
        err = postgresLedger.PostJournalEntry(ctx, debitEntry)
        if err != nil {
            b.Fatalf("Failed to post entry: %v", err)
        }
        
        // Get balance
        _, err = postgresLedger.GetBalance(ctx, account.ID)
        if err != nil {
            b.Fatalf("Failed to get balance: %v", err)
        }
    }
}