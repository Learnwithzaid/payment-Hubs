package ledger

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
    "github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQL connection and transaction management
type PostgresLedger struct {
    Pool *pgxpool.Pool
}

// Account represents a ledger account
type Account struct {
    ID             string  `json:"id"`
    AccountNumber  string  `json:"account_number"`
    AccountType    string  `json:"account_type"`
    Name           string  `json:"name"`
    CurrencyCode   string  `json:"currency_code"`
    IsActive       bool    `json:"is_active"`
    CreatedAt      string  `json:"created_at"`
    CreatedBy      string  `json:"created_by"`
    Metadata       map[string]interface{} `json:"metadata"`
    CurrentBalance float64 `json:"current_balance"`
}

// JournalEntry represents a single debit/credit entry
type JournalEntry struct {
    ID            string  `json:"id"`
    EntryNumber   string  `json:"entry_number"`
    TransactionID string  `json:"transaction_id"`
    EntryType     string  `json:"entry_type"`
    AccountID     string  `json:"account_id"`
    AccountType   string  `json:"account_type"`
    Amount        float64 `json:"amount"`
    Description   string  `json:"description"`
    ReferenceType string  `json:"reference_type"`
    ReferenceID   string  `json:"reference_id"`
    CurrencyCode  string  `json:"currency_code"`
    CreatedAt     string  `json:"created_at"`
    CreatedBy     string  `json:"created_by"`
    Metadata      map[string]interface{} `json:"metadata"`
}

// BalanceSnapshot represents a balance change snapshot
type BalanceSnapshot struct {
    ID              string  `json:"id"`
    AccountID       string  `json:"account_id"`
    TransactionID   string  `json:"transaction_id"`
    SnapshotTime    string  `json:"snapshot_time"`
    BalanceBefore   float64 `json:"balance_before"`
    BalanceAfter    float64 `json:"balance_after"`
    BalanceChange   float64 `json:"balance_change"`
    AccountType     string  `json:"account_type"`
    CurrencyCode    string  `json:"currency_code"`
    EntryID         string  `json:"entry_id"`
    EntryType       string  `json:"entry_type"`
    Amount          float64 `json:"amount"`
    Description     string  `json:"description"`
    ReferenceType   string  `json:"reference_type"`
    ReferenceID     string  `json:"reference_id"`
    CreatedAt       string  `json:"created_at"`
}

// NewPostgresLedger creates a new PostgreSQL ledger instance
func NewPostgresLedger(pool *pgxpool.Pool) *PostgresLedger {
    return &PostgresLedger{Pool: pool}
}

// CreateAccount creates a new account with transaction safety
func (pl *PostgresLedger) CreateAccount(ctx context.Context, accountNumber, accountType, name, currencyCode, createdBy string, metadata map[string]interface{}) (*Account, error) {
    const maxRetries = 3
    
    for attempt := 0; attempt < maxRetries; attempt++ {
        err := pl.createAccountWithRetry(ctx, accountNumber, accountType, name, currencyCode, createdBy, metadata)
        if err != nil {
            var pgErr *pgconn.PgError
            if errors.As(err, &pgErr) && pgErr.Code == "40001" {
                // Serialization failure, retry
                if attempt == maxRetries-1 {
                    return nil, fmt.Errorf("failed to create account after %d retries due to serialization failure: %w", maxRetries, err)
                }
                time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
                continue
            }
            return nil, fmt.Errorf("failed to create account: %w", err)
        }
        break
    }
    
    return pl.GetAccount(ctx, accountNumber)
}

// createAccountWithRetry handles the actual account creation with SERIALIZABLE isolation
func (pl *PostgresLedger) createAccountWithRetry(ctx context.Context, accountNumber, accountType, name, currencyCode, createdBy string, metadata map[string]interface{}) error {
    // Set deadline for the operation
    queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    
    // Use SERIALIZABLE isolation level for safety
    conn, err := pl.Pool.Acquire(queryCtx)
    if err != nil {
        return fmt.Errorf("failed to acquire connection: %w", err)
    }
    defer conn.Release()
    
    // Begin transaction with SERIALIZABLE isolation
    tx, err := conn.BeginTx(queryCtx, pgx.TxOptions{
        IsoLevel:   pgx.Serializable,
        AccessMode: pgx.ReadWrite,
    })
    if err != nil {
        return fmt.Errorf("failed to begin transaction: %w", err)
    }
    defer tx.Rollback(queryCtx)
    
    // Verify account number doesn't exist
    var exists bool
    err = tx.QueryRow(queryCtx, 
        "SELECT EXISTS(SELECT 1 FROM accounts WHERE account_number = $1 FOR UPDATE)",
        accountNumber).Scan(&exists)
    if err != nil {
        return fmt.Errorf("failed to check account existence: %w", err)
    }
    if exists {
        return fmt.Errorf("account number %s already exists", accountNumber)
    }
    
    // Insert new account
    _, err = tx.Exec(queryCtx, `
        INSERT INTO accounts (account_number, account_type, name, currency_code, created_by, metadata)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, accountNumber, accountType, name, currencyCode, createdBy, metadata)
    if err != nil {
        return fmt.Errorf("failed to insert account: %w", err)
    }
    
    // Initialize account balance
    _, err = tx.Exec(queryCtx, `
        INSERT INTO account_balances (account_id, balance)
        SELECT id, 0 FROM accounts WHERE account_number = $1
    `, accountNumber)
    if err != nil {
        return fmt.Errorf("failed to initialize account balance: %w", err)
    }
    
    // Commit transaction
    err = tx.Commit(queryCtx)
    if err != nil {
        return fmt.Errorf("failed to commit transaction: %w", err)
    }
    
    return nil
}

// GetAccount retrieves an account by account number
func (pl *PostgresLedger) GetAccount(ctx context.Context, accountNumber string) (*Account, error) {
    queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    
    var account Account
    var metadataJSON sql.NullString
    
    err := pl.Pool.QueryRow(queryCtx, `
        SELECT 
            a.id,
            a.account_number,
            a.account_type,
            a.name,
            a.currency_code,
            a.is_active,
            a.created_at,
            a.created_by,
            a.metadata,
            COALESCE(ab.balance, 0) as current_balance
        FROM accounts a
        LEFT JOIN account_balances ab ON a.id = ab.account_id
        WHERE a.account_number = $1
    `, accountNumber).Scan(
        &account.ID,
        &account.AccountNumber,
        &account.AccountType,
        &account.Name,
        &account.CurrencyCode,
        &account.IsActive,
        &account.CreatedAt,
        &account.CreatedBy,
        &metadataJSON,
        &account.CurrentBalance,
    )
    
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, fmt.Errorf("account %s not found", accountNumber)
        }
        return nil, fmt.Errorf("failed to get account: %w", err)
    }
    
    // Parse metadata JSON
    if metadataJSON.Valid {
        account.Metadata = make(map[string]interface{})
        // For simplicity, we'll store metadata as string representation
        account.Metadata["raw"] = metadataJSON.String
    }
    
    return &account, nil
}

// PostJournalEntry posts a single debit or credit entry
func (pl *PostgresLedger) PostJournalEntry(ctx context.Context, entry *JournalEntry) error {
    const maxRetries = 3
    
    for attempt := 0; attempt < maxRetries; attempt++ {
        err := pl.postJournalEntryWithRetry(ctx, entry)
        if err != nil {
            var pgErr *pgconn.PgError
            if errors.As(err, &pgErr) && pgErr.Code == "40001" {
                // Serialization failure, retry
                if attempt == maxRetries-1 {
                    return fmt.Errorf("failed to post journal entry after %d retries due to serialization failure: %w", maxRetries, err)
                }
                time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
                continue
            }
            return fmt.Errorf("failed to post journal entry: %w", err)
        }
        break
    }
    
    return nil
}

// postJournalEntryWithRetry handles the actual journal entry posting with SERIALIZABLE isolation
func (pl *PostgresLedger) postJournalEntryWithRetry(ctx context.Context, entry *JournalEntry) error {
    queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    
    conn, err := pl.Pool.Acquire(queryCtx)
    if err != nil {
        return fmt.Errorf("failed to acquire connection: %w", err)
    }
    defer conn.Release()
    
    // Begin transaction with SERIALIZABLE isolation
    tx, err := conn.BeginTx(queryCtx, pgx.TxOptions{
        IsoLevel:   pgx.Serializable,
        AccessMode: pgx.ReadWrite,
    })
    if err != nil {
        return fmt.Errorf("failed to begin transaction: %w", err)
    }
    defer tx.Rollback(queryCtx)
    
    // Lock the account for update
    var accountType string
    var accountActive bool
    err = tx.QueryRow(queryCtx, `
        SELECT account_type, is_active 
        FROM accounts 
        WHERE id = $1 
        FOR UPDATE
    `, entry.AccountID).Scan(&accountType, &accountActive)
    
    if err != nil {
        return fmt.Errorf("failed to lock account: %w", err)
    }
    
    if !accountActive {
        return fmt.Errorf("account %s is not active", entry.AccountID)
    }
    
    // Insert journal entry
    _, err = tx.Exec(queryCtx, `
        INSERT INTO journal_entries (
            entry_number, transaction_id, entry_type, account_id, account_type,
            amount, description, reference_type, reference_id, currency_code, created_by, metadata
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
    `, entry.EntryNumber, entry.TransactionID, entry.EntryType, entry.AccountID, accountType,
        entry.Amount, entry.Description, entry.ReferenceType, entry.ReferenceID, 
        entry.CurrencyCode, entry.CreatedBy, entry.Metadata)
    
    if err != nil {
        return fmt.Errorf("failed to insert journal entry: %w", err)
    }
    
    // Commit transaction
    err = tx.Commit(queryCtx)
    if err != nil {
        return fmt.Errorf("failed to commit transaction: %w", err)
    }
    
    return nil
}

// GetBalance retrieves the current balance for an account
func (pl *PostgresLedger) GetBalance(ctx context.Context, accountID string) (float64, error) {
    queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    
    var balance sql.NullFloat64
    err := pl.Pool.QueryRow(queryCtx, `
        SELECT COALESCE(balance, 0)
        FROM account_balances
        WHERE account_id = $1
    `, accountID).Scan(&balance)
    
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return 0, fmt.Errorf("account %s not found", accountID)
        }
        return 0, fmt.Errorf("failed to get balance: %w", err)
    }
    
    return balance.Float64, nil
}

// ReconcileTransactions performs reconciliation and drift detection
func (pl *PostgresLedger) ReconcileTransactions(ctx context.Context, accountID string, startTime, endTime time.Time) ([]BalanceSnapshot, error) {
    queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    
    rows, err := pl.Pool.Query(queryCtx, `
        SELECT 
            bs.id, bs.account_id, bs.transaction_id, bs.snapshot_time,
            bs.balance_before, bs.balance_after, bs.balance_change,
            bs.account_type, bs.currency_code, bs.entry_id, bs.entry_type,
            bs.amount, bs.description, bs.reference_type, bs.reference_id, bs.created_at
        FROM balance_snapshots bs
        WHERE bs.account_id = $1
        AND bs.snapshot_time >= $2
        AND bs.snapshot_time <= $3
        ORDER BY bs.snapshot_time ASC
    `, accountID, startTime, endTime)
    
    if err != nil {
        return nil, fmt.Errorf("failed to query balance snapshots: %w", err)
    }
    defer rows.Close()
    
    var snapshots []BalanceSnapshot
    for rows.Next() {
        var snapshot BalanceSnapshot
        err := rows.Scan(
            &snapshot.ID, &snapshot.AccountID, &snapshot.TransactionID, &snapshot.SnapshotTime,
            &snapshot.BalanceBefore, &snapshot.BalanceAfter, &snapshot.BalanceChange,
            &snapshot.AccountType, &snapshot.CurrencyCode, &snapshot.EntryID, &snapshot.EntryType,
            &snapshot.Amount, &snapshot.Description, &snapshot.ReferenceType, &snapshot.ReferenceID, &snapshot.CreatedAt,
        )
        if err != nil {
            return nil, fmt.Errorf("failed to scan balance snapshot: %w", err)
        }
        snapshots = append(snapshots, snapshot)
    }
    
    return snapshots, nil
}

// ValidateBalanceConsistency checks for reconciliation drift
func (pl *PostgresLedger) ValidateBalanceConsistency(ctx context.Context) ([]map[string]interface{}, error) {
    queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    
    rows, err := pl.Pool.Query(queryCtx, `
        SELECT 
            account_id, account_number, expected_balance, actual_balance, is_consistent
        FROM validate_balance_consistency()
    `)
    
    if err != nil {
        return nil, fmt.Errorf("failed to validate balance consistency: %w", err)
    }
    defer rows.Close()
    
    var results []map[string]interface{}
    for rows.Next() {
        var accountID, accountNumber string
        var expected, actual float64
        var isConsistent bool
        
        err := rows.Scan(&accountID, &accountNumber, &expected, &actual, &isConsistent)
        if err != nil {
            return nil, fmt.Errorf("failed to scan consistency result: %w", err)
        }
        
        result := map[string]interface{}{
            "account_id":        accountID,
            "account_number":    accountNumber,
            "expected_balance":  expected,
            "actual_balance":    actual,
            "is_consistent":     isConsistent,
            "drift_amount":      actual - expected,
        }
        results = append(results, result)
    }
    
    return results, nil
}

// Close closes the PostgreSQL pool
func (pl *PostgresLedger) Close() {
    pl.Pool.Close()
}