package ledger

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "time"

    "github.com/google/uuid"
)

// Transaction represents a complete accounting transaction with multiple entries
type Transaction struct {
    ID            string       `json:"id"`
    TransactionID string       `json:"transaction_id"`
    Entries       []JournalEntry `json:"entries"`
    Description   string       `json:"description"`
    ReferenceType string       `json:"reference_type"`
    ReferenceID   string       `json:"reference_id"`
    CurrencyCode  string       `json:"currency_code"`
    CreatedBy     string       `json:"created_by"`
    CreatedAt     string       `json:"created_at"`
    Metadata      map[string]interface{} `json:"metadata"`
}

// LedgerService provides the high-level API for double-entry bookkeeping
type LedgerService struct {
    postgres *PostgresLedger
}

// NewLedgerService creates a new ledger service
func NewLedgerService(postgres *PostgresLedger) *LedgerService {
    return &LedgerService{
        postgres: postgres,
    }
}

// CreateAccount creates a new account with validation
func (ls *LedgerService) CreateAccount(ctx context.Context, req CreateAccountRequest) (*Account, error) {
    // Validate account type
    if !isValidAccountType(req.AccountType) {
        return nil, fmt.Errorf("invalid account type: %s", req.AccountType)
    }
    
    // Validate currency code
    if len(req.CurrencyCode) != 3 {
        return nil, fmt.Errorf("currency code must be 3 characters")
    }
    
    // Create the account
    account, err := ls.postgres.CreateAccount(ctx, req.AccountNumber, req.AccountType, req.Name, req.CurrencyCode, req.CreatedBy, req.Metadata)
    if err != nil {
        return nil, fmt.Errorf("failed to create account: %w", err)
    }
    
    return account, nil
}

// CreateAccountRequest represents the request to create an account
type CreateAccountRequest struct {
    AccountNumber string                 `json:"account_number"`
    AccountType   string                 `json:"account_type"`
    Name          string                 `json:"name"`
    CurrencyCode  string                 `json:"currency_code"`
    CreatedBy     string                 `json:"created_by"`
    Metadata      map[string]interface{} `json:"metadata"`
}

// Credit posts a credit entry to an account.
func (ls *LedgerService) Credit(ctx context.Context, req CreditRequest) error {
    return ls.postTransaction(ctx, req.TransactionRequest, req.AccountID, req.Amount, req.Description, "credit")
}

// CreditRequest represents the request to post a credit.
type CreditRequest struct {
    TransactionRequest
    AccountID   string  `json:"account_id"`
    Amount      float64 `json:"amount"`
    Description string  `json:"description"`
}

// Debit posts a debit entry to an account.
func (ls *LedgerService) Debit(ctx context.Context, req DebitRequest) error {
    return ls.postTransaction(ctx, req.TransactionRequest, req.AccountID, req.Amount, req.Description, "debit")
}

// DebitRequest represents the request to post a debit.
type DebitRequest struct {
    TransactionRequest
    AccountID   string  `json:"account_id"`
    Amount      float64 `json:"amount"`
    Description string  `json:"description"`
}

// TransactionRequest contains common fields for transactions.
type TransactionRequest struct {
    TransactionID string                 `json:"transaction_id"`
    Description   string                 `json:"description"`
    ReferenceType string                 `json:"reference_type"`
    ReferenceID   string                 `json:"reference_id"`
    CurrencyCode  string                 `json:"currency_code"`
    CreatedBy     string                 `json:"created_by"`
    Metadata      map[string]interface{} `json:"metadata"`
}

func (ls *LedgerService) postTransaction(ctx context.Context, req TransactionRequest, accountID string, amount float64, description string, entryType string) error {
    if req.TransactionID == "" {
        req.TransactionID = uuid.New().String()
    }

    if accountID == "" {
        return fmt.Errorf("account ID is required")
    }

    if amount <= 0 {
        return fmt.Errorf("amount must be positive")
    }

    if description == "" {
        description = req.Description
    }

    prefix := req.TransactionID
    if len(prefix) > 8 {
        prefix = prefix[:8]
    }

    entryNumber := fmt.Sprintf("JE-%s-%d", prefix, time.Now().UnixNano())

    entry := &JournalEntry{
        EntryNumber:   entryNumber,
        TransactionID: req.TransactionID,
        EntryType:     entryType,
        AccountID:     accountID,
        Amount:        amount,
        Description:   description,
        ReferenceType: req.ReferenceType,
        ReferenceID:   req.ReferenceID,
        CurrencyCode:  req.CurrencyCode,
        CreatedBy:     req.CreatedBy,
        Metadata:      req.Metadata,
    }

    return ls.postgres.PostJournalEntry(ctx, entry)
}

// GetBalance retrieves the current balance for an account
func (ls *LedgerService) GetBalance(ctx context.Context, accountID string) (float64, error) {
    if accountID == "" {
        return 0, fmt.Errorf("account ID is required")
    }
    
    return ls.postgres.GetBalance(ctx, accountID)
}

// Reconcile performs account reconciliation for a time period
func (ls *LedgerService) Reconcile(ctx context.Context, req ReconcileRequest) ([]BalanceSnapshot, error) {
    if req.AccountID == "" {
        return nil, fmt.Errorf("account ID is required")
    }
    
    if req.StartTime.IsZero() || req.EndTime.IsZero() {
        return nil, fmt.Errorf("start time and end time are required")
    }
    
    if req.StartTime.After(req.EndTime) {
        return nil, fmt.Errorf("start time must be before end time")
    }
    
    return ls.postgres.ReconcileTransactions(ctx, req.AccountID, req.StartTime, req.EndTime)
}

// ReconcileRequest represents the request for account reconciliation
type ReconcileRequest struct {
    AccountID string    `json:"account_id"`
    StartTime time.Time `json:"start_time"`
    EndTime   time.Time `json:"end_time"`
}

// Transfer performs a transfer between two accounts (creates balanced entries)
func (ls *LedgerService) Transfer(ctx context.Context, req TransferRequest) error {
    const maxRetries = 3
    
    for attempt := 0; attempt < maxRetries; attempt++ {
        err := ls.transferWithRetry(ctx, req)
        if err != nil {
            var pgErr interface {
                Code() string
            }
            if errors.As(err, &pgErr) && pgErr.Code() == "40001" {
                // Serialization failure, retry
                if attempt == maxRetries-1 {
                    return fmt.Errorf("failed to transfer after %d retries due to serialization failure: %w", maxRetries, err)
                }
                time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
                continue
            }
            return fmt.Errorf("failed to transfer: %w", err)
        }
        break
    }
    
    return nil
}

// TransferRequest represents the request to transfer funds between accounts
type TransferRequest struct {
    FromAccountID string                 `json:"from_account_id"`
    ToAccountID   string                 `json:"to_account_id"`
    Amount        float64                `json:"amount"`
    Description   string                 `json:"description"`
    CurrencyCode  string                 `json:"currency_code"`
    CreatedBy     string                 `json:"created_by"`
    ReferenceType string                 `json:"reference_type"`
    ReferenceID   string                 `json:"reference_id"`
    Metadata      map[string]interface{} `json:"metadata"`
}

// transferWithRetry handles the actual transfer with proper double-entry bookkeeping
func (ls *LedgerService) transferWithRetry(ctx context.Context, req TransferRequest) error {
    // Validate request
    if req.FromAccountID == "" || req.ToAccountID == "" {
        return fmt.Errorf("from_account_id and to_account_id are required")
    }
    
    if req.FromAccountID == req.ToAccountID {
        return fmt.Errorf("from_account_id and to_account_id must be different")
    }
    
    if req.Amount <= 0 {
        return fmt.Errorf("amount must be positive")
    }
    
    // Generate transaction ID if not provided
    transactionID := uuid.New().String()
    
    // Generate entry numbers
    debitEntryNumber := fmt.Sprintf("JE-%s-debit", transactionID[:8])
    creditEntryNumber := fmt.Sprintf("JE-%s-credit", transactionID[:8])
    
    // Get account information for validation
    fromAccount, err := ls.postgres.GetAccount(ctx, req.FromAccountID)
    if err != nil {
        return fmt.Errorf("failed to get from account: %w", err)
    }
    
    toAccount, err := ls.postgres.GetAccount(ctx, req.ToAccountID)
    if err != nil {
        return fmt.Errorf("failed to get to account: %w", err)
    }
    
    // Check currency compatibility
    if fromAccount.CurrencyCode != toAccount.CurrencyCode {
        return fmt.Errorf("currency mismatch: %s vs %s", fromAccount.CurrencyCode, toAccount.CurrencyCode)
    }
    
    // Check sufficient balance for debit account
    fromBalance, err := ls.postgres.GetBalance(ctx, req.FromAccountID)
    if err != nil {
        return fmt.Errorf("failed to get from account balance: %w", err)
    }
    
    if fromAccount.AccountType == "asset" && fromBalance < req.Amount {
        return fmt.Errorf("insufficient balance in account %s: have %.8f, need %.8f", req.FromAccountID, fromBalance, req.Amount)
    }
    
    // Create debit entry (money out of from account)
    debitEntry := &JournalEntry{
        EntryNumber:   debitEntryNumber,
        TransactionID: transactionID,
        EntryType:     "debit",
        AccountID:     req.FromAccountID,
        AccountType:   fromAccount.AccountType,
        Amount:        req.Amount,
        Description:   fmt.Sprintf("Transfer to %s: %s", req.ToAccountID, req.Description),
        ReferenceType: req.ReferenceType,
        ReferenceID:   req.ReferenceID,
        CurrencyCode:  req.CurrencyCode,
        CreatedBy:     req.CreatedBy,
        Metadata:      req.Metadata,
    }
    
    // Create credit entry (money into to account)
    creditEntry := &JournalEntry{
        EntryNumber:   creditEntryNumber,
        TransactionID: transactionID,
        EntryType:     "credit",
        AccountID:     req.ToAccountID,
        AccountType:   toAccount.AccountType,
        Amount:        req.Amount,
        Description:   fmt.Sprintf("Transfer from %s: %s", req.FromAccountID, req.Description),
        ReferenceType: req.ReferenceType,
        ReferenceID:   req.ReferenceID,
        CurrencyCode:  req.CurrencyCode,
        CreatedBy:     req.CreatedBy,
        Metadata:      req.Metadata,
    }
    
    // Post both entries
    err = ls.postgres.PostJournalEntry(ctx, debitEntry)
    if err != nil {
        return fmt.Errorf("failed to post debit entry: %w", err)
    }
    
    err = ls.postgres.PostJournalEntry(ctx, creditEntry)
    if err != nil {
        return fmt.Errorf("failed to post credit entry: %w", err)
    }
    
    return nil
}

// GetAccount retrieves an account by account number
func (ls *LedgerService) GetAccount(ctx context.Context, accountNumber string) (*Account, error) {
    if accountNumber == "" {
        return nil, fmt.Errorf("account number is required")
    }
    
    return ls.postgres.GetAccount(ctx, accountNumber)
}

// ValidateBalanceConsistency checks for reconciliation drift
func (ls *LedgerService) ValidateBalanceConsistency(ctx context.Context) ([]map[string]interface{}, error) {
    return ls.postgres.ValidateBalanceConsistency(ctx)
}

// isValidAccountType validates account type
func isValidAccountType(accountType string) bool {
    validTypes := map[string]bool{
        "asset":     true,
        "liability": true,
        "equity":    true,
        "revenue":   true,
        "expense":   true,
    }
    return validTypes[accountType]
}

// GetAccountByID retrieves an account by ID
func (ls *LedgerService) GetAccountByID(ctx context.Context, accountID string) (*Account, error) {
    if accountID == "" {
        return nil, fmt.Errorf("account ID is required")
    }
    
    queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    
    var account Account
    var metadataJSON sql.NullString
    
    err := ls.postgres.Pool.QueryRow(queryCtx, `
        SELECT 
            a.id, a.account_number, a.account_type, a.name, a.currency_code,
            a.is_active, a.created_at, a.created_by, a.metadata,
            COALESCE(ab.balance, 0) as current_balance
        FROM accounts a
        LEFT JOIN account_balances ab ON a.id = ab.account_id
        WHERE a.id = $1
    `, accountID).Scan(
        &account.ID, &account.AccountNumber, &account.AccountType, &account.Name,
        &account.CurrencyCode, &account.IsActive, &account.CreatedAt, &account.CreatedBy,
        &metadataJSON, &account.CurrentBalance,
    )
    
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, fmt.Errorf("account %s not found", accountID)
        }
        return nil, fmt.Errorf("failed to get account: %w", err)
    }
    
    // Parse metadata JSON
    if metadataJSON.Valid {
        account.Metadata = make(map[string]interface{})
        account.Metadata["raw"] = metadataJSON.String
    }
    
    return &account, nil
}

// ListAccounts retrieves all accounts with optional filtering
func (ls *LedgerService) ListAccounts(ctx context.Context, filter AccountFilter) ([]*Account, error) {
    queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()
    
    // Build query based on filters
    query := `
        SELECT 
            a.id, a.account_number, a.account_type, a.name, a.currency_code,
            a.is_active, a.created_at, a.created_by, a.metadata,
            COALESCE(ab.balance, 0) as current_balance
        FROM accounts a
        LEFT JOIN account_balances ab ON a.id = ab.account_id
        WHERE 1=1
    `
    args := []interface{}{}
    argCount := 1
    
    if filter.AccountType != "" {
        query += fmt.Sprintf(" AND a.account_type = $%d", argCount)
        args = append(args, filter.AccountType)
        argCount++
    }
    
    if filter.IsActive != nil {
        query += fmt.Sprintf(" AND a.is_active = $%d", argCount)
        args = append(args, *filter.IsActive)
        argCount++
    }
    
    if filter.CurrencyCode != "" {
        query += fmt.Sprintf(" AND a.currency_code = $%d", argCount)
        args = append(args, filter.CurrencyCode)
        argCount++
    }
    
    query += " ORDER BY a.account_number"
    
    if filter.Limit > 0 {
        query += fmt.Sprintf(" LIMIT $%d", argCount)
        args = append(args, filter.Limit)
        argCount++
    }
    
    if filter.Offset > 0 {
        query += fmt.Sprintf(" OFFSET $%d", argCount)
        args = append(args, filter.Offset)
    }
    
    rows, err := ls.postgres.Pool.Query(queryCtx, query, args...)
    if err != nil {
        return nil, fmt.Errorf("failed to query accounts: %w", err)
    }
    defer rows.Close()
    
    var accounts []*Account
    for rows.Next() {
        var account Account
        var metadataJSON sql.NullString
        
        err := rows.Scan(
            &account.ID, &account.AccountNumber, &account.AccountType, &account.Name,
            &account.CurrencyCode, &account.IsActive, &account.CreatedAt, &account.CreatedBy,
            &metadataJSON, &account.CurrentBalance,
        )
        if err != nil {
            return nil, fmt.Errorf("failed to scan account: %w", err)
        }
        
        // Parse metadata JSON
        if metadataJSON.Valid {
            account.Metadata = make(map[string]interface{})
            account.Metadata["raw"] = metadataJSON.String
        }
        
        accounts = append(accounts, &account)
    }
    
    return accounts, nil
}

// AccountFilter represents filtering options for listing accounts
type AccountFilter struct {
    AccountType  string
    IsActive     *bool
    CurrencyCode string
    Limit        int
    Offset       int
}