package api

import (
    "encoding/json"
    "net/http"
    "strconv"

    "github.com/google/uuid"

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
