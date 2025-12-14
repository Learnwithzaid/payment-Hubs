package ledger

type CreateAccountRequest struct {
	AccountNumber string            `protobuf:"bytes,1,opt,name=account_number"`
	AccountType   string            `protobuf:"bytes,2,opt,name=account_type"`
	Name          string            `protobuf:"bytes,3,opt,name=name"`
	CurrencyCode  string            `protobuf:"bytes,4,opt,name=currency_code"`
	CreatedBy     string            `protobuf:"bytes,5,opt,name=created_by"`
	Metadata      map[string]string `protobuf:"bytes,6,rep,name=metadata"`
}

type CreateAccountResponse struct {
	AccountID       string            `protobuf:"bytes,1,opt,name=account_id"`
	AccountNumber   string            `protobuf:"bytes,2,opt,name=account_number"`
	AccountType     string            `protobuf:"bytes,3,opt,name=account_type"`
	Name            string            `protobuf:"bytes,4,opt,name=name"`
	CurrencyCode    string            `protobuf:"bytes,5,opt,name=currency_code"`
	IsActive        bool              `protobuf:"bytes,6,opt,name=is_active"`
	CreatedAt       string            `protobuf:"bytes,7,opt,name=created_at"`
	CreatedBy       string            `protobuf:"bytes,8,opt,name=created_by"`
	Metadata        map[string]string `protobuf:"bytes,9,rep,name=metadata"`
	CurrentBalance  float64           `protobuf:"fixed64,10,opt,name=current_balance"`
}

type CreditRequest struct {
	TransactionID string            `protobuf:"bytes,1,opt,name=transaction_id"`
	AccountID     string            `protobuf:"bytes,2,opt,name=account_id"`
	Amount        float64           `protobuf:"fixed64,3,opt,name=amount"`
	Description   string            `protobuf:"bytes,4,opt,name=description"`
	ReferenceType string            `protobuf:"bytes,5,opt,name=reference_type"`
	ReferenceID   string            `protobuf:"bytes,6,opt,name=reference_id"`
	CurrencyCode  string            `protobuf:"bytes,7,opt,name=currency_code"`
	CreatedBy     string            `protobuf:"bytes,8,opt,name=created_by"`
	Metadata      map[string]string `protobuf:"bytes,9,rep,name=metadata"`
}

type CreditResponse struct {
	Success       bool   `protobuf:"bool,1,opt,name=success"`
	EntryID       string `protobuf:"bytes,2,opt,name=entry_id"`
	EntryNumber   string `protobuf:"bytes,3,opt,name=entry_number"`
	TransactionID string `protobuf:"bytes,4,opt,name=transaction_id"`
	AccountID     string `protobuf:"bytes,5,opt,name=account_id"`
	Amount        float64 `protobuf:"fixed64,6,opt,name=amount"`
	Description   string  `protobuf:"bytes,7,opt,name=description"`
	CreatedAt     string  `protobuf:"bytes,8,opt,name=created_at"`
}

type DebitRequest struct {
	TransactionID string            `protobuf:"bytes,1,opt,name=transaction_id"`
	AccountID     string            `protobuf:"bytes,2,opt,name=account_id"`
	Amount        float64           `protobuf:"fixed64,3,opt,name=amount"`
	Description   string            `protobuf:"bytes,4,opt,name=description"`
	ReferenceType string            `protobuf:"bytes,5,opt,name=reference_type"`
	ReferenceID   string            `protobuf:"bytes,6,opt,name=reference_id"`
	CurrencyCode  string            `protobuf:"bytes,7,opt,name=currency_code"`
	CreatedBy     string            `protobuf:"bytes,8,opt,name=created_by"`
	Metadata      map[string]string `protobuf:"bytes,9,rep,name=metadata"`
}

type DebitResponse struct {
	Success       bool   `protobuf:"bool,1,opt,name=success"`
	EntryID       string `protobuf:"bytes,2,opt,name=entry_id"`
	EntryNumber   string `protobuf:"bytes,3,opt,name=entry_number"`
	TransactionID string `protobuf:"bytes,4,opt,name=transaction_id"`
	AccountID     string `protobuf:"bytes,5,opt,name=account_id"`
	Amount        float64 `protobuf:"fixed64,6,opt,name=amount"`
	Description   string  `protobuf:"bytes,7,opt,name=description"`
	CreatedAt     string  `protobuf:"bytes,8,opt,name=created_at"`
}

type TransferRequest struct {
	FromAccountID string            `protobuf:"bytes,1,opt,name=from_account_id"`
	ToAccountID   string            `protobuf:"bytes,2,opt,name=to_account_id"`
	Amount        float64           `protobuf:"fixed64,3,opt,name=amount"`
	Description   string            `protobuf:"bytes,4,opt,name=description"`
	CurrencyCode  string            `protobuf:"bytes,5,opt,name=currency_code"`
	CreatedBy     string            `protobuf:"bytes,6,opt,name=created_by"`
	ReferenceType string            `protobuf:"bytes,7,opt,name=reference_type"`
	ReferenceID   string            `protobuf:"bytes,8,opt,name=reference_id"`
	Metadata      map[string]string `protobuf:"bytes,9,rep,name=metadata"`
}

type TransferResponse struct {
	Success         bool   `protobuf:"bool,1,opt,name=success"`
	TransactionID   string `protobuf:"bytes,2,opt,name=transaction_id"`
	FromAccountID   string `protobuf:"bytes,3,opt,name=from_account_id"`
	ToAccountID     string `protobuf:"bytes,4,opt,name=to_account_id"`
	Amount          float64 `protobuf:"fixed64,5,opt,name=amount"`
	Description     string  `protobuf:"bytes,6,opt,name=description"`
	CreatedAt       string  `protobuf:"bytes,7,opt,name=created_at"`
}

type GetBalanceRequest struct {
	AccountID string `protobuf:"bytes,1,opt,name=account_id"`
}

type GetBalanceResponse struct {
	AccountID    string  `protobuf:"bytes,1,opt,name=account_id"`
	Balance      float64 `protobuf:"fixed64,2,opt,name=balance"`
	CurrencyCode string  `protobuf:"bytes,3,opt,name=currency_code"`
	UpdatedAt    string  `protobuf:"bytes,4,opt,name=updated_at"`
}

type ReconcileRequest struct {
	AccountID string `protobuf:"bytes,1,opt,name=account_id"`
	StartTime string `protobuf:"bytes,2,opt,name=start_time"`
	EndTime   string `protobuf:"bytes,3,opt,name=end_time"`
}

type ReconcileResponse struct {
	Snapshots          []*BalanceSnapshot `protobuf:"bytes,1,rep,name=snapshots"`
	HasDrift           bool               `protobuf:"bool,2,opt,name=has_drift"`
	DriftAmount        float64            `protobuf:"fixed64,3,opt,name=drift_amount"`
	ReconciliationTime string             `protobuf:"bytes,4,opt,name=reconciliation_time"`
}

type BalanceSnapshot struct {
	ID             string  `protobuf:"bytes,1,opt,name=id"`
	AccountID      string  `protobuf:"bytes,2,opt,name=account_id"`
	TransactionID  string  `protobuf:"bytes,3,opt,name=transaction_id"`
	SnapshotTime   string  `protobuf:"bytes,4,opt,name=snapshot_time"`
	BalanceBefore  float64 `protobuf:"fixed64,5,opt,name=balance_before"`
	BalanceAfter   float64 `protobuf:"fixed64,6,opt,name=balance_after"`
	BalanceChange  float64 `protobuf:"fixed64,7,opt,name=balance_change"`
	AccountType    string  `protobuf:"bytes,8,opt,name=account_type"`
	CurrencyCode   string  `protobuf:"bytes,9,opt,name=currency_code"`
	EntryID        string  `protobuf:"bytes,10,opt,name=entry_id"`
	EntryType      string  `protobuf:"bytes,11,opt,name=entry_type"`
	Amount         float64 `protobuf:"fixed64,12,opt,name=amount"`
	Description    string  `protobuf:"bytes,13,opt,name=description"`
	ReferenceType  string  `protobuf:"bytes,14,opt,name=reference_type"`
	ReferenceID    string  `protobuf:"bytes,15,opt,name=reference_id"`
	CreatedAt      string  `protobuf:"bytes,16,opt,name=created_at"`
}

type ListAccountsRequest struct {
	AccountType  string `protobuf:"bytes,1,opt,name=account_type"`
	IsActive     *bool  `protobuf:"varint,2,opt,name=is_active"`
	CurrencyCode string `protobuf:"bytes,3,opt,name=currency_code"`
	Limit        int32  `protobuf:"varint,4,opt,name=limit"`
	Offset       int32  `protobuf:"varint,5,opt,name=offset"`
}

type ListAccountsResponse struct {
	Accounts []*Account `protobuf:"bytes,1,rep,name=accounts"`
	Total    int32      `protobuf:"varint,2,opt,name=total"`
}

type Account struct {
	ID             string            `protobuf:"bytes,1,opt,name=id"`
	AccountNumber  string            `protobuf:"bytes,2,opt,name=account_number"`
	AccountType    string            `protobuf:"bytes,3,opt,name=account_type"`
	Name           string            `protobuf:"bytes,4,opt,name=name"`
	CurrencyCode   string            `protobuf:"bytes,5,opt,name=currency_code"`
	IsActive       bool              `protobuf:"bytes,6,opt,name=is_active"`
	CreatedAt      string            `protobuf:"bytes,7,opt,name=created_at"`
	CreatedBy      string            `protobuf:"bytes,8,opt,name=created_by"`
	Metadata       map[string]string `protobuf:"bytes,9,rep,name=metadata"`
	CurrentBalance float64           `protobuf:"fixed64,10,opt,name=current_balance"`
}

type GetAccountRequest struct {
	AccountNumber string `protobuf:"bytes,1,opt,name=account_number"`
	AccountID     string `protobuf:"bytes,2,opt,name=account_id"`
}

type GetAccountResponse struct {
	Account *Account `protobuf:"bytes,1,opt,name=account"`
}

type ValidateConsistencyRequest struct {
	AccountID string `protobuf:"bytes,1,opt,name=account_id"`
}

type ValidationResult struct {
	IsValid         bool              `protobuf:"bool,1,opt,name=is_valid"`
	ValidationType  string            `protobuf:"bytes,2,opt,name=validation_type"`
	Message         string            `protobuf:"bytes,3,opt,name=message"`
	AccountID       string            `protobuf:"bytes,4,opt,name=account_id"`
	TransactionID   string            `protobuf:"bytes,5,opt,name=transaction_id"`
	Timestamp       string            `protobuf:"bytes,6,opt,name=timestamp"`
	Details         map[string]string `protobuf:"bytes,7,rep,name=details"`
}

type ValidateConsistencyResponse struct {
	Results       []*ValidationResult `protobuf:"bytes,1,rep,name=results"`
	IsFullyValid  bool                `protobuf:"bool,2,opt,name=is_fully_valid"`
	ErrorCount    int32               `protobuf:"varint,3,opt,name=error_count"`
}