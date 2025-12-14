package api

import (
    "encoding/json"
    "net/http"
    "strconv"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/google/uuid"

    "github.com/example/pci-infra/internal/disputes"
    "github.com/example/pci-infra/internal/ledger"
    "github.com/example/pci-infra/internal/security"
)

type listAccountsResponse struct {
    CorrelationID string           `json:"correlation_id"`
    Accounts      []*ledger.Account `json:"accounts"`
}

type createAccountRequest struct {
    AccountNumber string                 `json:"account_number"`
    AccountType   string                 `json:"account_type"`
    Name          string                 `json:"name"`
    CurrencyCode  string                 `json:"currency_code"`
    CreatedBy     string                 `json:"created_by"`
    Metadata      map[string]interface{} `json:"metadata"`
}

type createAccountResponse struct {
    CorrelationID string         `json:"correlation_id"`
    Account       *ledger.Account `json:"account"`
}

type postEntryRequest struct {
    TransactionID string                 `json:"transaction_id"`
    AccountID     string                 `json:"account_id"`
    Amount        float64                `json:"amount"`
    Description   string                 `json:"description"`
    ReferenceType string                 `json:"reference_type"`
    ReferenceID   string                 `json:"reference_id"`
    CurrencyCode  string                 `json:"currency_code"`
    CreatedBy     string                 `json:"created_by"`
    Metadata      map[string]interface{} `json:"metadata"`
}

type postEntryResponse struct {
    CorrelationID string  `json:"correlation_id"`
    Success       bool    `json:"success"`
    Type          string  `json:"type"`
    TransactionID string  `json:"transaction_id"`
    AccountID     string  `json:"account_id"`
    Amount        float64 `json:"amount"`
}

type balanceResponse struct {
    CorrelationID string  `json:"correlation_id"`
    AccountID     string  `json:"account_id"`
    Balance       float64 `json:"balance"`
}

func handleListAccounts(deps Dependencies) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps.LedgerReader == nil {
            security.WriteJSONError(w, r, http.StatusServiceUnavailable, "ledger_unavailable")
            return
        }

        filter := ledger.AccountFilter{}
        filter.AccountType = r.URL.Query().Get("account_type")
        filter.CurrencyCode = r.URL.Query().Get("currency_code")

        if v := r.URL.Query().Get("limit"); v != "" {
            if i, err := strconv.Atoi(v); err == nil {
                filter.Limit = i
            }
        }
        if v := r.URL.Query().Get("offset"); v != "" {
            if i, err := strconv.Atoi(v); err == nil {
                filter.Offset = i
            }
        }
        if v := r.URL.Query().Get("is_active"); v != "" {
            b, err := strconv.ParseBool(v)
            if err == nil {
                filter.IsActive = &b
            }
        }

        accounts, err := deps.LedgerReader.ListAccounts(r.Context(), filter)
        if err != nil {
            security.WriteJSONError(w, r, http.StatusInternalServerError, "internal_error")
            return
        }

        writeJSON(w, r, http.StatusOK, listAccountsResponse{
            CorrelationID: security.CorrelationIDFromContext(r.Context()),
            Accounts:      accounts,
        })
    }
}

func handleCreateAccount(deps Dependencies) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps.LedgerWriter == nil {
            security.WriteJSONError(w, r, http.StatusServiceUnavailable, "ledger_unavailable")
            return
        }

        var req createAccountRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_json")
            return
        }

        account, err := deps.LedgerWriter.CreateAccount(r.Context(), ledger.CreateAccountRequest{
            AccountNumber: req.AccountNumber,
            AccountType:   req.AccountType,
            Name:          req.Name,
            CurrencyCode:  req.CurrencyCode,
            CreatedBy:     req.CreatedBy,
            Metadata:      req.Metadata,
        })
        if err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_request")
            return
        }

        writeJSON(w, r, http.StatusCreated, createAccountResponse{
            CorrelationID: security.CorrelationIDFromContext(r.Context()),
            Account:       account,
        })
    }
}

func handleDebit(deps Dependencies) http.HandlerFunc {
    return handlePostEntry(deps, "debit")
}

func handleCredit(deps Dependencies) http.HandlerFunc {
    return handlePostEntry(deps, "credit")
}

func handlePostEntry(deps Dependencies, entryType string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps.LedgerWriter == nil {
            security.WriteJSONError(w, r, http.StatusServiceUnavailable, "ledger_unavailable")
            return
        }

        var req postEntryRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_json")
            return
        }
        if req.TransactionID == "" {
            req.TransactionID = uuid.NewString()
        }

        switch entryType {
        case "debit":
            err := deps.LedgerWriter.Debit(r.Context(), ledger.DebitRequest{
                TransactionRequest: ledger.TransactionRequest{
                    TransactionID: req.TransactionID,
                    Description:   req.Description,
                    ReferenceType: req.ReferenceType,
                    ReferenceID:   req.ReferenceID,
                    CurrencyCode:  req.CurrencyCode,
                    CreatedBy:     req.CreatedBy,
                    Metadata:      req.Metadata,
                },
                AccountID:   req.AccountID,
                Amount:      req.Amount,
                Description: req.Description,
            })
            if err != nil {
                security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_request")
                return
            }
        case "credit":
            err := deps.LedgerWriter.Credit(r.Context(), ledger.CreditRequest{
                TransactionRequest: ledger.TransactionRequest{
                    TransactionID: req.TransactionID,
                    Description:   req.Description,
                    ReferenceType: req.ReferenceType,
                    ReferenceID:   req.ReferenceID,
                    CurrencyCode:  req.CurrencyCode,
                    CreatedBy:     req.CreatedBy,
                    Metadata:      req.Metadata,
                },
                AccountID:   req.AccountID,
                Amount:      req.Amount,
                Description: req.Description,
            })
            if err != nil {
                security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_request")
                return
            }
        default:
            security.WriteJSONError(w, r, http.StatusInternalServerError, "internal_error")
            return
        }

        writeJSON(w, r, http.StatusOK, postEntryResponse{
            CorrelationID: security.CorrelationIDFromContext(r.Context()),
            Success:       true,
            Type:          entryType,
            TransactionID: req.TransactionID,
            AccountID:     req.AccountID,
            Amount:        req.Amount,
        })
    }
}

func handleBalance(deps Dependencies) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps.LedgerReader == nil {
            security.WriteJSONError(w, r, http.StatusServiceUnavailable, "ledger_unavailable")
            return
        }

        accountID := r.URL.Query().Get("account_id")
        if accountID == "" {
            security.WriteJSONError(w, r, http.StatusBadRequest, "validation_error")
            return
        }

        bal, err := deps.LedgerReader.GetBalance(r.Context(), accountID)
        if err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_request")
            return
        }

        writeJSON(w, r, http.StatusOK, balanceResponse{
            CorrelationID: security.CorrelationIDFromContext(r.Context()),
            AccountID:     accountID,
            Balance:       bal,
        })
    }
}

// Dispute-related request/response types
type createDisputeRequest struct {
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

type createDisputeResponse struct {
    CorrelationID string         `json:"correlation_id"`
    Dispute       *disputes.Dispute `json:"dispute"`
}

type authorizeDisputeRequest struct {
    DisputeID      string `json:"dispute_id"`
    AuthorizedBy   string `json:"authorized_by"`
}

type settleTransactionRequest struct {
    JournalEntryID string `json:"journal_entry_id"`
    SettledBy      string `json:"settled_by"`
}

type initiateDisputeRequest struct {
    DisputeID    string `json:"dispute_id"`
    InitiatedBy  string `json:"initiated_by"`
}

type reverseDisputeRequest struct {
    DisputeID   string `json:"dispute_id"`
    ReversedBy  string `json:"reversed_by"`
    Reason      string `json:"reason"`
}

type getDisputeResponse struct {
    CorrelationID string         `json:"correlation_id"`
    Dispute       *disputes.Dispute `json:"dispute"`
}

type listDisputesResponse struct {
    CorrelationID string           `json:"correlation_id"`
    Disputes      []*disputes.Dispute `json:"disputes"`
    Total         int              `json:"total"`
}

type getDisputeHistoryResponse struct {
    CorrelationID string                       `json:"correlation_id"`
    Transitions   []*disputes.StateTransition `json:"transitions"`
}

type calculateReserveRequest struct {
    MerchantID        string  `json:"merchant_id"`
    TransactionVolume float64 `json:"transaction_volume"`
    CurrencyCode      string  `json:"currency_code"`
}

type calculateReserveResponse struct {
    CorrelationID     string  `json:"correlation_id"`
    RequiredReserve   float64 `json:"required_reserve"`
    CurrencyCode      string  `json:"currency_code"`
    ReservePercentage float64 `json:"reserve_percentage"`
    MinimumReserve    float64 `json:"minimum_reserve"`
    CurrentReserve    float64 `json:"current_reserve"`
}

type actionResponse struct {
    CorrelationID string `json:"correlation_id"`
    Success       bool   `json:"success"`
    Status        string `json:"status"`
}

// Dispute handlers
func handleCreateDispute(deps Dependencies) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps.DisputesService == nil {
            security.WriteJSONError(w, r, http.StatusServiceUnavailable, "disputes_unavailable")
            return
        }

        var req createDisputeRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_json")
            return
        }

        dispute, err := deps.DisputesService.CreateDispute(r.Context(), disputes.CreateDisputeRequest{
            JournalEntryID: req.JournalEntryID,
            MerchantID:     req.MerchantID,
            DisputedAmount: req.DisputedAmount,
            CurrencyCode:   req.CurrencyCode,
            ReasonCode:     req.ReasonCode,
            ReasonText:     req.ReasonText,
            ReferenceType:  req.ReferenceType,
            ReferenceID:    req.ReferenceID,
            CreatedBy:      req.CreatedBy,
            Metadata:       disputes.MaskPII(req.Metadata),
        })
        if err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_request")
            return
        }

        writeJSON(w, r, http.StatusCreated, createDisputeResponse{
            CorrelationID: security.CorrelationIDFromContext(r.Context()),
            Dispute:       dispute,
        })
    }
}

func handleAuthorizeDispute(deps Dependencies) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps.DisputesService == nil {
            security.WriteJSONError(w, r, http.StatusServiceUnavailable, "disputes_unavailable")
            return
        }

        disputeID := chi.URLParam(r, "dispute_id")
        if disputeID == "" {
            security.WriteJSONError(w, r, http.StatusBadRequest, "validation_error")
            return
        }

        var req authorizeDisputeRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_json")
            return
        }

        err := deps.DisputesService.AuthorizeDispute(r.Context(), disputeID, req.AuthorizedBy)
        if err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_request")
            return
        }

        writeJSON(w, r, http.StatusOK, actionResponse{
            CorrelationID: security.CorrelationIDFromContext(r.Context()),
            Success:       true,
            Status:        "AUTHORIZED",
        })
    }
}

func handleSettleTransaction(deps Dependencies) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps.DisputesService == nil {
            security.WriteJSONError(w, r, http.StatusServiceUnavailable, "disputes_unavailable")
            return
        }

        var req settleTransactionRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_json")
            return
        }

        err := deps.DisputesService.SettleTransaction(r.Context(), req.JournalEntryID, req.SettledBy)
        if err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_request")
            return
        }

        writeJSON(w, r, http.StatusOK, actionResponse{
            CorrelationID: security.CorrelationIDFromContext(r.Context()),
            Success:       true,
            Status:        "SETTLED",
        })
    }
}

func handleInitiateDispute(deps Dependencies) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps.DisputesService == nil {
            security.WriteJSONError(w, r, http.StatusServiceUnavailable, "disputes_unavailable")
            return
        }

        disputeID := chi.URLParam(r, "dispute_id")
        if disputeID == "" {
            security.WriteJSONError(w, r, http.StatusBadRequest, "validation_error")
            return
        }

        var req initiateDisputeRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_json")
            return
        }

        err := deps.DisputesService.InitiateDispute(r.Context(), disputeID, req.InitiatedBy)
        if err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_request")
            return
        }

        writeJSON(w, r, http.StatusOK, actionResponse{
            CorrelationID: security.CorrelationIDFromContext(r.Context()),
            Success:       true,
            Status:        "DISPUTED",
        })
    }
}

func handleReverseDispute(deps Dependencies) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps.DisputesService == nil {
            security.WriteJSONError(w, r, http.StatusServiceUnavailable, "disputes_unavailable")
            return
        }

        disputeID := chi.URLParam(r, "dispute_id")
        if disputeID == "" {
            security.WriteJSONError(w, r, http.StatusBadRequest, "validation_error")
            return
        }

        var req reverseDisputeRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_json")
            return
        }

        err := deps.DisputesService.ReverseDispute(r.Context(), disputeID, req.ReversedBy, req.Reason)
        if err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_request")
            return
        }

        writeJSON(w, r, http.StatusOK, actionResponse{
            CorrelationID: security.CorrelationIDFromContext(r.Context()),
            Success:       true,
            Status:        "REVERSED",
        })
    }
}

func handleGetDispute(deps Dependencies) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps.DisputesService == nil {
            security.WriteJSONError(w, r, http.StatusServiceUnavailable, "disputes_unavailable")
            return
        }

        disputeID := r.URL.Query().Get("dispute_id")
        if disputeID == "" {
            disputeID = chi.URLParam(r, "dispute_id")
        }

        if disputeID == "" {
            security.WriteJSONError(w, r, http.StatusBadRequest, "validation_error")
            return
        }

        dispute, err := deps.DisputesService.GetDispute(r.Context(), disputeID)
        if err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_request")
            return
        }

        if dispute == nil {
            security.WriteJSONError(w, r, http.StatusNotFound, "dispute_not_found")
            return
        }

        writeJSON(w, r, http.StatusOK, getDisputeResponse{
            CorrelationID: security.CorrelationIDFromContext(r.Context()),
            Dispute:       dispute,
        })
    }
}

func handleListDisputes(deps Dependencies) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps.DisputesService == nil {
            security.WriteJSONError(w, r, http.StatusServiceUnavailable, "disputes_unavailable")
            return
        }

        filter := disputes.DisputeFilter{}
        filter.MerchantID = r.URL.Query().Get("merchant_id")
        filter.Status = r.URL.Query().Get("status")

        if v := r.URL.Query().Get("is_fraud"); v != "" {
            b, err := strconv.ParseBool(v)
            if err == nil {
                filter.IsFraud = &b
            }
        }

        if v := r.URL.Query().Get("created_after"); v != "" {
            if t, err := time.Parse(time.RFC3339, v); err == nil {
                filter.CreatedAfter = t
            }
        }

        if v := r.URL.Query().Get("limit"); v != "" {
            if i, err := strconv.Atoi(v); err == nil {
                filter.Limit = i
            }
        }
        if v := r.URL.Query().Get("offset"); v != "" {
            if i, err := strconv.Atoi(v); err == nil {
                filter.Offset = i
            }
        }

        disputes, err := deps.DisputesService.ListDisputes(r.Context(), filter)
        if err != nil {
            security.WriteJSONError(w, r, http.StatusInternalServerError, "internal_error")
            return
        }

        writeJSON(w, r, http.StatusOK, listDisputesResponse{
            CorrelationID: security.CorrelationIDFromContext(r.Context()),
            Disputes:      disputes,
            Total:         len(disputes),
        })
    }
}

func handleGetDisputeHistory(deps Dependencies) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps.DisputesService == nil {
            security.WriteJSONError(w, r, http.StatusServiceUnavailable, "disputes_unavailable")
            return
        }

        disputeID := r.URL.Query().Get("dispute_id")
        if disputeID == "" {
            disputeID = chi.URLParam(r, "dispute_id")
        }

        if disputeID == "" {
            security.WriteJSONError(w, r, http.StatusBadRequest, "validation_error")
            return
        }

        // Get state machine from disputes service
        // Note: This would require extending the interface to get the state machine
        // For now, we'll implement a basic version
        writeJSON(w, r, http.StatusOK, getDisputeHistoryResponse{
            CorrelationID: security.CorrelationIDFromContext(r.Context()),
            Transitions:   []*disputes.StateTransition{}, // TODO: Implement state machine access
        })
    }
}

func handleCalculateReserve(deps Dependencies) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps.DisputesService == nil {
            security.WriteJSONError(w, r, http.StatusServiceUnavailable, "disputes_unavailable")
            return
        }

        merchantID := r.URL.Query().Get("merchant_id")
        if merchantID == "" {
            security.WriteJSONError(w, r, http.StatusBadRequest, "validation_error")
            return
        }

        transactionVolumeStr := r.URL.Query().Get("transaction_volume")
        transactionVolume := 0.0
        if v := transactionVolumeStr; v != "" {
            if f, err := strconv.ParseFloat(v, 64); err == nil {
                transactionVolume = f
            }
        }

        currencyCode := r.URL.Query().Get("currency_code")
        if currencyCode == "" {
            currencyCode = "USD"
        }

        requiredReserve, err := deps.DisputesService.CalculateMerchantReserve(r.Context(), merchantID, transactionVolume)
        if err != nil {
            security.WriteJSONError(w, r, http.StatusBadRequest, "invalid_request")
            return
        }

        writeJSON(w, r, http.StatusOK, calculateReserveResponse{
            CorrelationID:     security.CorrelationIDFromContext(r.Context()),
            RequiredReserve:   requiredReserve,
            CurrencyCode:      currencyCode,
            ReservePercentage: 0.05, // Default 5% - would come from config
            MinimumReserve:    0.0,
            CurrentReserve:    0.0,  // Would come from actual merchant data
        })
    }
}
