package disputes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// DisputesService provides dispute management functionality
type DisputesService struct {
	pool            *pgxpool.Pool
	ledger          *LedgerService
	stateMachine    *StateMachine
	reservePercentage float64
}

// NewDisputesService creates a new disputes service
func NewDisputesService(pool *pgxpool.Pool, ledger *LedgerService, reservePercentage float64) *DisputesService {
	// Create transition store implementation
	transitionStore := &PostgresTransitionStore{Pool: pool}
	stateMachine := NewStateMachine(transitionStore)

	return &DisputesService{
		pool:             pool,
		ledger:           ledger,
		stateMachine:     stateMachine,
		reservePercentage: reservePercentage,
	}
}

// CreateDisputeRequest represents a request to create a new dispute
type CreateDisputeRequest struct {
	JournalEntryID  string                 `json:"journal_entry_id"`
	MerchantID      string                 `json:"merchant_id"`
	DisputedAmount  float64                `json:"disputed_amount"`
	CurrencyCode    string                 `json:"currency_code"`
	ReasonCode      string                 `json:"reason_code"`
	ReasonText      string                 `json:"reason_text"`
	ReferenceType   string                 `json:"reference_type"`
	ReferenceID     string                 `json:"reference_id"`
	CreatedBy       string                 `json:"created_by"`
	Metadata        map[string]interface{} `json:"metadata"`
}

// Dispute represents a dispute record
type Dispute struct {
	ID               string                 `json:"id"`
	DisputeID        string                 `json:"dispute_id"`
	JournalEntryID   string                 `json:"journal_entry_id"`
	MerchantID       string                 `json:"merchant_id"`
	OriginalAmount   float64                `json:"original_amount"`
	DisputedAmount   float64                `json:"disputed_amount"`
	CurrencyCode     string                 `json:"currency_code"`
	ReasonCode       string                 `json:"reason_code"`
	ReasonText       string                 `json:"reason_text"`
	Status           DisputeState           `json:"status"`
	IsFraud          bool                   `json:"is_fraud"`
	ChargebackFee    float64                `json:"chargeback_fee"`
	CreatedAt        time.Time              `json:"created_at"`
	CreatedBy        string                 `json:"created_by"`
	ResolvedAt       *time.Time             `json:"resolved_at,omitempty"`
	ResolvedBy       string                 `json:"resolved_by,omitempty"`
	Metadata         map[string]interface{} `json:"metadata"`
}

// Hold represents a funds hold record
type Hold struct {
	ID            string                 `json:"id"`
	HoldID        string                 `json:"hold_id"`
	DisputeID     string                 `json:"dispute_id"`
	AccountID     string                 `json:"account_id"`
	HeldAmount    float64                `json:"held_amount"`
	CurrencyCode  string                 `json:"currency_code"`
	Status        string                 `json:"status"`
	ExpiresAt     time.Time              `json:"expires_at"`
	CreatedAt     time.Time              `json:"created_at"`
	CreatedBy     string                 `json:"created_by"`
	ReleasedAt    *time.Time             `json:"released_at,omitempty"`
	ReleasedBy    string                 `json:"released_by,omitempty"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// FraudReserve represents a merchant fraud reserve record
type FraudReserve struct {
	ID                   string  `json:"id"`
	MerchantID           string  `json:"merchant_id"`
	ReserveAccountID     string  `json:"reserve_account_id"`
	ReservePercentage    float64 `json:"reserve_percentage"`
	MinimumReserveAmount float64 `json:"minimum_reserve_amount"`
	CurrentReserveAmount float64 `json:"current_reserve_amount"`
	CurrencyCode         string  `json:"currency_code"`
	IsActive             bool    `json:"is_active"`
	CreatedAt            time.Time `json:"created_at"`
	CreatedBy            string   `json:"created_by"`
	UpdatedAt            time.Time `json:"updated_at"`
	UpdatedBy            string   `json:"updated_by"`
}

// CreateDispute creates a new dispute with atomic ledger operations
func (ds *DisputesService) CreateDispute(ctx context.Context, req CreateDisputeRequest) (*Dispute, error) {
	// Start transaction for ACID compliance
	tx, err := ds.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Validate dispute request
	validationReq := DisputeValidationRequest{
		DisputeID:      uuid.New().String(),
		JournalEntryID: req.JournalEntryID,
		MerchantID:     req.MerchantID,
		OriginalAmount: req.DisputedAmount, // We'll get this from journal entry
		DisputedAmount: req.DisputedAmount,
		CurrencyCode:   req.CurrencyCode,
		ReasonCode:     req.ReasonCode,
		ReasonText:     req.ReasonText,
		ReferenceType:  req.ReferenceType,
		ReferenceID:    req.ReferenceID,
		CreatedBy:      req.CreatedBy,
		Metadata:       req.Metadata,
	}

	if err := ValidateDisputeRequest(validationReq); err != nil {
		return nil, fmt.Errorf("dispute validation failed: %w", err)
	}

	// Get journal entry to validate and get original amount
	var originalAmount float64
	var accountID, accountType string
	err = tx.QueryRow(ctx, `
		SELECT amount, account_id, account_type, currency_code, reference_type, reference_id
		FROM journal_entries
		WHERE id = $1
	`, req.JournalEntryID).Scan(&originalAmount, &accountID, &accountType, &validationReq.CurrencyCode, &validationReq.ReferenceType, &validationReq.ReferenceID)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("journal entry not found: %s", req.JournalEntryID)
		}
		return nil, fmt.Errorf("failed to get journal entry: %w", err)
	}

	validationReq.OriginalAmount = originalAmount

	if err := ValidateDisputeRequest(validationReq); err != nil {
		return nil, fmt.Errorf("dispute validation failed: %w", err)
	}

	// Validate reason code and determine if it's fraud
	reasonCode, err := ValidateReasonCode(req.ReasonCode)
	if err != nil {
		return nil, fmt.Errorf("invalid reason code: %w", err)
	}

	// Calculate chargeback fee based on reason code
	chargebackFee := 0.0
	if reasonCode.ChargebackFee {
		chargebackFee = calculateChargebackFee(req.DisputedAmount, reasonCode.Brand)
	}

	// Create dispute record
	disputeID := fmt.Sprintf("DSP-%s", time.Now().Format("20060102-")) + uuid.New().String()[:8]
	
	dispute := &Dispute{
		ID:               uuid.New().String(),
		DisputeID:        disputeID,
		JournalEntryID:   req.JournalEntryID,
		MerchantID:       req.MerchantID,
		OriginalAmount:   originalAmount,
		DisputedAmount:   req.DisputedAmount,
		CurrencyCode:     req.CurrencyCode,
		ReasonCode:       req.ReasonCode,
		ReasonText:       req.ReasonText,
		Status:          StatePending,
		IsFraud:         reasonCode.Fraud,
		ChargebackFee:   chargebackFee,
		CreatedAt:       time.Now(),
		CreatedBy:       req.CreatedBy,
		Metadata:        MaskPII(req.Metadata),
	}

	// Insert dispute record
	_, err = tx.Exec(ctx, `
		INSERT INTO disputes (
			dispute_id, journal_entry_id, merchant_id, original_amount, disputed_amount,
			currency_code, reason_code, reason_text, status, is_fraud, chargeback_fee,
			prev_dispute_hash, reference_type, reference_id, metadata, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, '', $12, $13, $14, $15)
	`, dispute.DisputeID, dispute.JournalEntryID, dispute.MerchantID, dispute.OriginalAmount,
		dispute.DisputedAmount, dispute.CurrencyCode, dispute.ReasonCode, dispute.ReasonText,
		dispute.Status, dispute.IsFraud, dispute.ChargebackFee, dispute.ReferenceType,
		dispute.ReferenceID, json.RawMessage{}, dispute.CreatedBy)

	if err != nil {
		return nil, fmt.Errorf("failed to insert dispute: %w", err)
	}

	// Create initial state transition
	transitionReq := TransitionRequest{
		DisputeID: dispute.DisputeID,
		ToState:   StatePending,
		Reason:    "Dispute created",
		CreatedBy: req.CreatedBy,
		Metadata:  dispute.Metadata,
	}

	result := ds.stateMachine.Transition(ctx, transitionReq)
	if !result.Success {
		return nil, fmt.Errorf("failed to create state transition: %w", result.Error)
	}

	// Apply holds and reserves if dispute is immediately authorized
	if req.DisputedAmount > 0 {
		err = ds.applyFundsHold(ctx, tx, dispute)
		if err != nil {
			return nil, fmt.Errorf("failed to apply funds hold: %w", err)
		}

		err = ds.updateFraudReserve(ctx, tx, dispute.MerchantID, req.DisputedAmount)
		if err != nil {
			return nil, fmt.Errorf("failed to update fraud reserve: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return dispute, nil
}

// AuthorizeDispute authorizes a dispute and applies holds
func (ds *DisputesService) AuthorizeDispute(ctx context.Context, disputeID, authorizedBy string) error {
	tx, err := ds.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Get dispute
	dispute, err := ds.getDisputeByDisputeID(ctx, tx, disputeID)
	if err != nil {
		return fmt.Errorf("failed to get dispute: %w", err)
	}

	if dispute == nil {
		return fmt.Errorf("dispute not found: %s", disputeID)
	}

	if dispute.Status != StatePending {
		return fmt.Errorf("dispute must be in PENDING state to authorize, current state: %s", dispute.Status)
	}

	// Apply state transition
	transitionReq := TransitionRequest{
		DisputeID: disputeID,
		ToState:   StateAuthorized,
		Reason:    "Dispute authorized",
		CreatedBy: authorizedBy,
		Metadata:  map[string]interface{}{},
	}

	result := ds.stateMachine.Transition(ctx, transitionReq)
	if !result.Success {
		return fmt.Errorf("failed to transition dispute: %w", result.Error)
	}

	// Apply funds hold
	err = ds.applyFundsHold(ctx, tx, dispute)
	if err != nil {
		return fmt.Errorf("failed to apply funds hold: %w", err)
	}

	// Update fraud reserve
	err = ds.updateFraudReserve(ctx, tx, dispute.MerchantID, dispute.DisputedAmount)
	if err != nil {
		return fmt.Errorf("failed to update fraud reserve: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// SettleTransaction marks a transaction as settled (making it eligible for dispute)
func (ds *DisputesService) SettleTransaction(ctx context.Context, journalEntryID, settledBy string) error {
	// This would typically be called during normal transaction processing
	// For now, we'll implement it as a simple state transition for any disputes on this entry
	
	tx, err := ds.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Get any disputes for this journal entry
	disputes, err := ds.getDisputesByJournalEntry(ctx, tx, journalEntryID)
	if err != nil {
		return fmt.Errorf("failed to get disputes: %w", err)
	}

	// Transition any pending disputes to SETTLED state
	for _, dispute := range disputes {
		if dispute.Status == StateAuthorized {
			transitionReq := TransitionRequest{
				DisputeID: dispute.DisputeID,
				ToState:   StateSettled,
				Reason:    "Transaction settled",
				CreatedBy: settledBy,
				Metadata:  map[string]interface{}{},
			}

			result := ds.stateMachine.Transition(ctx, transitionReq)
			if !result.Success {
				return fmt.Errorf("failed to transition dispute %s: %w", dispute.DisputeID, result.Error)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// InitiateDispute initiates a formal dispute/chargeback
func (ds *DisputesService) InitiateDispute(ctx context.Context, disputeID, initiatedBy string) error {
	// For now, this is a simple state transition
	// In a real implementation, this would involve network communication with card networks
	
	dispute, err := ds.getDispute(ctx, disputeID)
	if err != nil {
		return fmt.Errorf("failed to get dispute: %w", err)
	}

	if dispute == nil {
		return fmt.Errorf("dispute not found: %s", disputeID)
	}

	if dispute.Status != StateSettled {
		return fmt.Errorf("dispute must be in SETTLED state to initiate, current state: %s", dispute.Status)
	}

	transitionReq := TransitionRequest{
		DisputeID: disputeID,
		ToState:   StateDisputed,
		Reason:    "Dispute initiated",
		CreatedBy: initiatedBy,
		Metadata:  map[string]interface{}{},
	}

	result := ds.stateMachine.Transition(ctx, transitionReq)
	if !result.Success {
		return fmt.Errorf("failed to transition dispute: %w", result.Error)
	}

	return nil
}

// ReverseDispute reverses a dispute and releases any holds
func (ds *DisputesService) ReverseDispute(ctx context.Context, disputeID, reversedBy, reason string) error {
	tx, err := ds.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Get dispute
	dispute, err := ds.getDisputeByDisputeID(ctx, tx, disputeID)
	if err != nil {
		return fmt.Errorf("failed to get dispute: %w", err)
	}

	if dispute == nil {
		return fmt.Errorf("dispute not found: %s", disputeID)
	}

	// Apply state transition
	transitionReq := TransitionRequest{
		DisputeID: disputeID,
		ToState:   StateReversed,
		Reason:    reason,
		CreatedBy: reversedBy,
		Metadata:  map[string]interface{}{},
	}

	result := ds.stateMachine.Transition(ctx, transitionReq)
	if !result.Success {
		return fmt.Errorf("failed to transition dispute: %w", result.Error)
	}

	// Update dispute resolution details
	_, err = tx.Exec(ctx, `
		UPDATE disputes 
		SET resolved_at = CURRENT_TIMESTAMP, resolved_by = $2
		WHERE dispute_id = $1
	`, disputeID, reversedBy)

	if err != nil {
		return fmt.Errorf("failed to update dispute resolution: %w", err)
	}

	// Release holds and adjust reserves will be handled by database triggers
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetDispute retrieves a dispute by ID
func (ds *DisputesService) GetDispute(ctx context.Context, disputeID string) (*Dispute, error) {
	return ds.getDispute(ctx, disputeID)
}

// ListDisputes lists disputes with optional filtering
func (ds *DisputesService) ListDisputes(ctx context.Context, filter DisputeFilter) ([]*Dispute, error) {
	return ds.listDisputes(ctx, filter)
}

// CalculateMerchantReserve calculates the required reserve for a merchant
func (ds *DisputesService) CalculateMerchantReserve(ctx context.Context, merchantID string, transactionVolume float64) (float64, error) {
	// Get merchant's current reserve configuration
	_, err := ds.getFraudReserve(ctx, merchantID)
	if err != nil {
		return 0, fmt.Errorf("failed to get fraud reserve: %w", err)
	}

	// Calculate reserve based on volume and percentage
	requiredReserve := transactionVolume * ds.reservePercentage
	
	return requiredReserve, nil
}

// applyFundsHold applies funds hold for a dispute
func (ds *DisputesService) applyFundsHold(ctx context.Context, tx pgx.Tx, dispute *Dispute) error {
	// Get the account associated with the journal entry
	var accountID string
	err := tx.QueryRow(ctx, `
		SELECT account_id FROM journal_entries WHERE id = $1
	`, dispute.JournalEntryID).Scan(&accountID)

	if err != nil {
		return fmt.Errorf("failed to get account ID: %w", err)
	}

	// Create hold ID
	holdID := fmt.Sprintf("HLD-%s", time.Now().Format("20060102-")) + uuid.New().String()[:8]
	
	// Calculate hold amount (disputed amount plus any fees)
	holdAmount := dispute.DisputedAmount + dispute.ChargebackFee
	
	// Create hold with 30-day expiry
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	_, err = tx.Exec(ctx, `
		INSERT INTO holds (
			hold_id, dispute_id, account_id, held_amount, currency_code,
			status, expires_at, created_by, prev_hold_hash
		) VALUES ($1, $2, $3, $4, $5, 'ACTIVE', $6, $7, '')
	`, holdID, dispute.ID, accountID, holdAmount, dispute.CurrencyCode, expiresAt, dispute.CreatedBy)

	if err != nil {
		return fmt.Errorf("failed to create hold: %w", err)
	}

	// The database trigger will handle the journal entries automatically

	return nil
}

// updateFraudReserve updates the merchant's fraud reserve
func (ds *DisputesService) updateFraudReserve(ctx context.Context, tx pgx.Tx, merchantID string, amount float64) error {
	// Get or create fraud reserve
	reserve, err := ds.getFraudReserve(ctx, tx, merchantID)
	if err != nil {
		return fmt.Errorf("failed to get fraud reserve: %w", err)
	}

	if reserve == nil {
		// Create new reserve
		reserveID := uuid.New().String()
		reserveAccountID := uuid.New().String()
		
		_, err = tx.Exec(ctx, `
			INSERT INTO fraud_reserves (
				id, merchant_id, reserve_account_id, reserve_percentage,
				minimum_reserve_amount, current_reserve_amount, currency_code,
				created_by, updated_by
			) VALUES ($1, $2, $3, $4, $5, $6, 'USD', $7, $7)
		`, reserveID, merchantID, reserveAccountID, ds.reservePercentage, 0.0, 0.0, "system")

		if err != nil {
			return fmt.Errorf("failed to create fraud reserve: %w", err)
		}
		
		reserve = &FraudReserve{
			ID:                   reserveID,
			MerchantID:           merchantID,
			ReserveAccountID:     reserveAccountID,
			ReservePercentage:    ds.reservePercentage,
			MinimumReserveAmount: 0.0,
			CurrentReserveAmount: 0.0,
			CurrencyCode:         "USD",
			IsActive:             true,
			CreatedBy:           "system",
			UpdatedBy:           "system",
		}
	}

	// Update reserve amount
	newReserveAmount := reserve.CurrentReserveAmount + (amount * reserve.ReservePercentage)
	
	_, err = tx.Exec(ctx, `
		UPDATE fraud_reserves 
		SET current_reserve_amount = $1, updated_at = CURRENT_TIMESTAMP
		WHERE merchant_id = $2
	`, newReserveAmount, merchantID)

	if err != nil {
		return fmt.Errorf("failed to update fraud reserve: %w", err)
	}

	return nil
}

// getDispute gets a dispute by dispute_id
func (ds *DisputesService) getDispute(ctx context.Context, disputeID string) (*Dispute, error) {
	return ds.getDisputeByDisputeID(ctx, ds.pool, disputeID)
}

// getDisputeByDisputeID gets a dispute by dispute_id
func (ds *DisputesService) getDisputeByDisputeID(ctx context.Context, tx interface {
	QueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row
}, disputeID string) (*Dispute, error) {
	dispute := &Dispute{}
	
	err := tx.QueryRow(ctx, `
		SELECT id, dispute_id, journal_entry_id, merchant_id, original_amount,
		       disputed_amount, currency_code, reason_code, reason_text, status,
		       is_fraud, chargeback_fee, created_at, created_by, resolved_at,
		       resolved_by, metadata
		FROM disputes
		WHERE dispute_id = $1
	`, disputeID).Scan(
		&dispute.ID, &dispute.DisputeID, &dispute.JournalEntryID, &dispute.MerchantID,
		&dispute.OriginalAmount, &dispute.DisputedAmount, &dispute.CurrencyCode,
		&dispute.ReasonCode, &dispute.ReasonText, &dispute.Status, &dispute.IsFraud,
		&dispute.ChargebackFee, &dispute.CreatedAt, &dispute.CreatedBy,
		&dispute.ResolvedAt, &dispute.ResolvedBy,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query dispute: %w", err)
	}

	// Parse metadata
	var metadataJSON json.RawMessage
	if err := tx.QueryRow(ctx, `SELECT metadata FROM disputes WHERE dispute_id = $1`, disputeID).Scan(&metadataJSON); err == nil {
		dispute.Metadata = make(map[string]interface{})
		json.Unmarshal(metadataJSON, &dispute.Metadata)
	}

	return dispute, nil
}

// getDisputesByJournalEntry gets all disputes for a journal entry
func (ds *DisputesService) getDisputesByJournalEntry(ctx context.Context, tx interface {
	Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error)
}, journalEntryID string) ([]*Dispute, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, dispute_id, journal_entry_id, merchant_id, original_amount,
		       disputed_amount, currency_code, reason_code, reason_text, status,
		       is_fraud, chargeback_fee, created_at, created_by, resolved_at,
		       resolved_by
		FROM disputes
		WHERE journal_entry_id = $1
	`, journalEntryID)

	if err != nil {
		return nil, fmt.Errorf("failed to query disputes: %w", err)
	}
	defer rows.Close()

	var disputes []*Dispute
	for rows.Next() {
		dispute := &Dispute{}
		err := rows.Scan(
			&dispute.ID, &dispute.DisputeID, &dispute.JournalEntryID, &dispute.MerchantID,
			&dispute.OriginalAmount, &dispute.DisputedAmount, &dispute.CurrencyCode,
			&dispute.ReasonCode, &dispute.ReasonText, &dispute.Status, &dispute.IsFraud,
			&dispute.ChargebackFee, &dispute.CreatedAt, &dispute.CreatedBy,
			&dispute.ResolvedAt, &dispute.ResolvedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan dispute: %w", err)
		}
		disputes = append(disputes, dispute)
	}

	return disputes, nil
}

// getFraudReserve gets fraud reserve for a merchant
func (ds *DisputesService) getFraudReserve(ctx context.Context, merchantID string) (*FraudReserve, error) {
	return ds.getFraudReserve(ctx, ds.pool, merchantID)
}

// getFraudReserve gets fraud reserve for a merchant with transaction support
func (ds *DisputesService) getFraudReserve(ctx context.Context, tx interface {
	QueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row
}, merchantID string) (*FraudReserve, error) {
	reserve := &FraudReserve{}
	
	err := tx.QueryRow(ctx, `
		SELECT id, merchant_id, reserve_account_id, reserve_percentage,
		       minimum_reserve_amount, current_reserve_amount, currency_code,
		       is_active, created_at, created_by, updated_at, updated_by
		FROM fraud_reserves
		WHERE merchant_id = $1
	`, merchantID).Scan(
		&reserve.ID, &reserve.MerchantID, &reserve.ReserveAccountID, &reserve.ReservePercentage,
		&reserve.MinimumReserveAmount, &reserve.CurrentReserveAmount, &reserve.CurrencyCode,
		&reserve.IsActive, &reserve.CreatedAt, &reserve.CreatedBy, &reserve.UpdatedAt, &reserve.UpdatedBy,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query fraud reserve: %w", err)
	}

	return reserve, nil
}

// listDisputes lists disputes with filtering
func (ds *DisputesService) listDisputes(ctx context.Context, filter DisputeFilter) ([]*Dispute, error) {
	query := `
		SELECT id, dispute_id, journal_entry_id, merchant_id, original_amount,
		       disputed_amount, currency_code, reason_code, reason_text, status,
		       is_fraud, chargeback_fee, created_at, created_by, resolved_at,
		       resolved_by
		FROM disputes
		WHERE 1=1
	`
	args := []interface{}{}
	argCount := 1

	if filter.MerchantID != "" {
		query += fmt.Sprintf(" AND merchant_id = $%d", argCount)
		args = append(args, filter.MerchantID)
		argCount++
	}

	if filter.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argCount)
		args = append(args, filter.Status)
		argCount++
	}

	if filter.IsFraud != nil {
		query += fmt.Sprintf(" AND is_fraud = $%d", argCount)
		args = append(args, *filter.IsFraud)
		argCount++
	}

	if !filter.CreatedAfter.IsZero() {
		query += fmt.Sprintf(" AND created_at >= $%d", argCount)
		args = append(args, filter.CreatedAfter)
		argCount++
	}

	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argCount)
		args = append(args, filter.Limit)
		argCount++
	}

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argCount)
		args = append(args, filter.Offset)
	}

	rows, err := ds.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query disputes: %w", err)
	}
	defer rows.Close()

	var disputes []*Dispute
	for rows.Next() {
		dispute := &Dispute{}
		err := rows.Scan(
			&dispute.ID, &dispute.DisputeID, &dispute.JournalEntryID, &dispute.MerchantID,
			&dispute.OriginalAmount, &dispute.DisputedAmount, &dispute.CurrencyCode,
			&dispute.ReasonCode, &dispute.ReasonText, &dispute.Status, &dispute.IsFraud,
			&dispute.ChargebackFee, &dispute.CreatedAt, &dispute.CreatedBy,
			&dispute.ResolvedAt, &dispute.ResolvedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan dispute: %w", err)
		}
		disputes = append(disputes, dispute)
	}

	return disputes, nil
}

// DisputeFilter represents filtering options for listing disputes
type DisputeFilter struct {
	MerchantID    string
	Status        string
	IsFraud       *bool
	CreatedAfter  time.Time
	Limit         int
	Offset        int
}

// calculateChargebackFee calculates chargeback fee based on amount and brand
func calculateChargebackFee(amount float64, brand CardBrand) float64 {
	// Typical chargeback fees range from $5 to $25 depending on card network
	switch brand {
	case BrandVisa:
		return min(15.0, max(5.0, amount*0.02)) // 2% capped at $15
	case BrandMastercard:
		return min(25.0, max(8.0, amount*0.025)) // 2.5% capped at $25
	default:
		return 10.0 // Default fee
	}
}

// min and max helper functions
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// PostgresTransitionStore implements TransitionStore interface for PostgreSQL
type PostgresTransitionStore struct {
	Pool *pgxpool.Pool
}

// CreateTransition creates a new state transition
func (pts *PostgresTransitionStore) CreateTransition(ctx context.Context, transition *StateTransition) error {
	_, err := pts.Pool.Exec(ctx, `
		INSERT INTO dispute_transitions (
			id, dispute_id, from_state, to_state, reason, transition_hash,
			prev_hash, created_at, created_by, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, transition.ID, transition.DisputeID, transition.FromState, transition.ToState,
		transition.Reason, transition.TransitionHash, transition.PrevHash,
		transition.CreatedAt, transition.CreatedBy, json.RawMessage{})

	if err != nil {
		return fmt.Errorf("failed to insert transition: %w", err)
	}

	return nil
}

// GetLatestTransition gets the latest transition for a dispute
func (pts *PostgresTransitionStore) GetLatestTransition(ctx context.Context, disputeID string) (*StateTransition, error) {
	transition := &StateTransition{}
	
	err := pts.Pool.QueryRow(ctx, `
		SELECT id, dispute_id, from_state, to_state, reason, transition_hash,
		       prev_hash, created_at, created_by
		FROM dispute_transitions
		WHERE dispute_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, disputeID).Scan(
		&transition.ID, &transition.DisputeID, &transition.FromState, &transition.ToState,
		&transition.Reason, &transition.TransitionHash, &transition.PrevHash,
		&transition.CreatedAt, &transition.CreatedBy,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query transition: %w", err)
	}

	return transition, nil
}

// GetTransitionHistory gets the complete transition history for a dispute
func (pts *PostgresTransitionStore) GetTransitionHistory(ctx context.Context, disputeID string) ([]*StateTransition, error) {
	rows, err := pts.Pool.Query(ctx, `
		SELECT id, dispute_id, from_state, to_state, reason, transition_hash,
		       prev_hash, created_at, created_by
		FROM dispute_transitions
		WHERE dispute_id = $1
		ORDER BY created_at ASC
	`, disputeID)

	if err != nil {
		return nil, fmt.Errorf("failed to query transitions: %w", err)
	}
	defer rows.Close()

	var transitions []*StateTransition
	for rows.Next() {
		transition := &StateTransition{}
		err := rows.Scan(
			&transition.ID, &transition.DisputeID, &transition.FromState, &transition.ToState,
			&transition.Reason, &transition.TransitionHash, &transition.PrevHash,
			&transition.CreatedAt, &transition.CreatedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan transition: %w", err)
		}
		transitions = append(transitions, transition)
	}

	return transitions, nil
}

// GetTransitionHash gets the hash of the latest transition for a dispute
func (pts *PostgresTransitionStore) GetTransitionHash(ctx context.Context, disputeID string) (string, error) {
	var hash string
	err := pts.Pool.QueryRow(ctx, `
		SELECT transition_hash FROM dispute_transitions
		WHERE dispute_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, disputeID).Scan(&hash)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("failed to query transition hash: %w", err)
	}

	return hash, nil
}