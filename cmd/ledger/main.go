package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/pci-infra/internal/config"
	"github.com/example/pci-infra/internal/ledger"
	"github.com/example/pci-infra/pkg/audit"
	
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/reflection"
	
	// gRPC service implementation will be generated from proto
	ledgerpb "github.com/example/pci-infra/api/gen/ledger"
)

// LedgerGRPCService implements the gRPC service for the ledger
type LedgerGRPCService struct {
	ledgerService *ledger.LedgerService
	validator     *ledger.Validator
	auditLogger   *audit.ChainLogger
}

// NewLedgerGRPCService creates a new gRPC service instance
func NewLedgerGRPCService(ledgerService *ledger.LedgerService, validator *ledger.Validator) *LedgerGRPCService {
	return &LedgerGRPCService{
		ledgerService: ledgerService,
		validator:     validator,
		auditLogger:   audit.NewChainLogger(),
	}
}

// CreateAccount implements the CreateAccount gRPC method
func (s *LedgerGRPCService) CreateAccount(ctx context.Context, req *ledgerpb.CreateAccountRequest) (*ledgerpb.CreateAccountResponse, error) {
	// Log the request for audit
	auditEntry := s.auditLogger.Append(fmt.Sprintf("CreateAccount request: %+v", req))
	log.Printf("AUDIT: %s", auditEntry.Hash)

	// Validate the request
	if req.AccountNumber == "" {
		return nil, status.Error(codes.InvalidArgument, "account_number is required")
	}
	if req.AccountType == "" {
		return nil, status.Error(codes.InvalidArgument, "account_type is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.CurrencyCode == "" {
		return nil, status.Error(codes.InvalidArgument, "currency_code is required")
	}

	// Convert metadata
	metadata := make(map[string]interface{})
	for k, v := range req.Metadata {
		metadata[k] = v
	}

	// Create account through ledger service
	createReq := ledger.CreateAccountRequest{
		AccountNumber: req.AccountNumber,
		AccountType:   req.AccountType,
		Name:          req.Name,
		CurrencyCode:  req.CurrencyCode,
		CreatedBy:     req.CreatedBy,
		Metadata:      metadata,
	}

	account, err := s.ledgerService.CreateAccount(ctx, createReq)
	if err != nil {
		s.auditLogger.Append(fmt.Sprintf("CreateAccount failed: %v", err))
		return nil, status.Errorf(codes.Internal, "failed to create account: %v", err)
	}

	// Log successful account creation
	s.auditLogger.Append(fmt.Sprintf("Account created successfully: %s", account.AccountNumber))

	// Convert response
	response := &ledgerpb.CreateAccountResponse{
		AccountId:      account.ID,
		AccountNumber:  account.AccountNumber,
		AccountType:    account.AccountType,
		Name:           account.Name,
		CurrencyCode:   account.CurrencyCode,
		IsActive:       account.IsActive,
		CreatedAt:      account.CreatedAt,
		CreatedBy:      account.CreatedBy,
		CurrentBalance: account.CurrentBalance,
	}

	// Convert metadata
	response.Metadata = make(map[string]string)
	for k, v := range account.Metadata {
		if str, ok := v.(string); ok {
			response.Metadata[k] = str
		}
	}

	return response, nil
}

// Credit implements the Credit gRPC method
func (s *LedgerGRPCService) Credit(ctx context.Context, req *ledgerpb.CreditRequest) (*ledgerpb.CreditResponse, error) {
	// Log the request for audit
	auditEntry := s.auditLogger.Append(fmt.Sprintf("Credit request: %+v", req))
	log.Printf("AUDIT: %s", auditEntry.Hash)

	// Validate the request
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}
	if req.Amount <= 0 {
		return nil, status.Error(codes.InvalidArgument, "amount must be positive")
	}

	// Validate overdraft prevention before proceeding
	overdraftResult := s.validator.ValidateOverdraftPrevention(ctx, req.AccountId, req.Amount, "credit")
	if !overdraftResult.IsValid {
		s.auditLogger.Append(fmt.Sprintf("Credit rejected - overdraft prevention: %s", overdraftResult.Message))
		return nil, status.Errorf(codes.FailedPrecondition, "credit rejected: %s", overdraftResult.Message)
	}

	// Create transaction request
	transactionReq := ledger.TransactionRequest{
		TransactionID: req.TransactionId,
		Description:   req.Description,
		ReferenceType: req.ReferenceType,
		ReferenceID:   req.ReferenceId,
		CurrencyCode:  req.CurrencyCode,
		CreatedBy:     req.CreatedBy,
		Metadata:      make(map[string]interface{}),
	}

	// Convert metadata
	for k, v := range req.Metadata {
		transactionReq.Metadata[k] = v
	}

	// Add amount to transaction request for the posting logic
	transactionReqWithAmount := struct {
		ledger.TransactionRequest
		Amount float64 `json:"amount"`
	}{
		TransactionRequest: transactionReq,
		Amount:            req.Amount,
	}

	// For credit, we need to create a journal entry
	// This is a simplified implementation - in practice, you'd have more sophisticated logic
	err := s.ledgerService.Credit(ctx, ledger.CreditRequest{
		TransactionRequest: transactionReq,
		AccountID:         req.AccountId,
		Amount:            req.Amount,
		Description:       req.Description,
	})

	if err != nil {
		s.auditLogger.Append(fmt.Sprintf("Credit failed: %v", err))
		return nil, status.Errorf(codes.Internal, "failed to post credit: %v", err)
	}

	// Log successful credit
	s.auditLogger.Append(fmt.Sprintf("Credit posted successfully: account=%s, amount=%.8f", req.AccountId, req.Amount))

	return &ledgerpb.CreditResponse{
		Success:        true,
		AccountId:      req.AccountId,
		Amount:         req.Amount,
		Description:    req.Description,
		TransactionId:  req.TransactionId,
		CreatedAt:      time.Now().Format(time.RFC3339),
	}, nil
}

// Debit implements the Debit gRPC method
func (s *LedgerGRPCService) Debit(ctx context.Context, req *ledgerpb.DebitRequest) (*ledgerpb.DebitResponse, error) {
	// Log the request for audit
	auditEntry := s.auditLogger.Append(fmt.Sprintf("Debit request: %+v", req))
	log.Printf("AUDIT: %s", auditEntry.Hash)

	// Validate the request
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}
	if req.Amount <= 0 {
		return nil, status.Error(codes.InvalidArgument, "amount must be positive")
	}

	// Validate overdraft prevention before proceeding
	overdraftResult := s.validator.ValidateOverdraftPrevention(ctx, req.AccountId, req.Amount, "debit")
	if !overdraftResult.IsValid {
		s.auditLogger.Append(fmt.Sprintf("Debit rejected - overdraft prevention: %s", overdraftResult.Message))
		return nil, status.Errorf(codes.FailedPrecondition, "debit rejected: %s", overdraftResult.Message)
	}

	// Create transaction request
	transactionReq := ledger.TransactionRequest{
		TransactionID: req.TransactionId,
		Description:   req.Description,
		ReferenceType: req.ReferenceType,
		ReferenceID:   req.ReferenceId,
		CurrencyCode:  req.CurrencyCode,
		CreatedBy:     req.CreatedBy,
		Metadata:      make(map[string]interface{}),
	}

	// Convert metadata
	for k, v := range req.Metadata {
		transactionReq.Metadata[k] = v
	}

	// Post debit through ledger service
	err := s.ledgerService.Debit(ctx, ledger.DebitRequest{
		TransactionRequest: transactionReq,
		AccountID:         req.AccountId,
		Amount:            req.Amount,
		Description:       req.Description,
	})

	if err != nil {
		s.auditLogger.Append(fmt.Sprintf("Debit failed: %v", err))
		return nil, status.Errorf(codes.Internal, "failed to post debit: %v", err)
	}

	// Log successful debit
	s.auditLogger.Append(fmt.Sprintf("Debit posted successfully: account=%s, amount=%.8f", req.AccountId, req.Amount))

	return &ledgerpb.DebitResponse{
		Success:        true,
		AccountId:      req.AccountId,
		Amount:         req.Amount,
		Description:    req.Description,
		TransactionId:  req.TransactionId,
		CreatedAt:      time.Now().Format(time.RFC3339),
	}, nil
}

// Transfer implements the Transfer gRPC method
func (s *LedgerGRPCService) Transfer(ctx context.Context, req *ledgerpb.TransferRequest) (*ledgerpb.TransferResponse, error) {
	// Log the request for audit
	auditEntry := s.auditLogger.Append(fmt.Sprintf("Transfer request: %+v", req))
	log.Printf("AUDIT: %s", auditEntry.Hash)

	// Validate the request
	if req.FromAccountId == "" || req.ToAccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "from_account_id and to_account_id are required")
	}
	if req.FromAccountId == req.ToAccountId {
		return nil, status.Error(codes.InvalidArgument, "from_account_id and to_account_id must be different")
	}
	if req.Amount <= 0 {
		return nil, status.Error(codes.InvalidArgument, "amount must be positive")
	}

	// Convert metadata
	metadata := make(map[string]interface{})
	for k, v := range req.Metadata {
		metadata[k] = v
	}

	// Perform transfer through ledger service
	transferReq := ledger.TransferRequest{
		FromAccountID: req.FromAccountId,
		ToAccountID:   req.ToAccountId,
		Amount:        req.Amount,
		Description:   req.Description,
		CurrencyCode:  req.CurrencyCode,
		CreatedBy:     req.CreatedBy,
		ReferenceType: req.ReferenceType,
		ReferenceID:   req.ReferenceId,
		Metadata:      metadata,
	}

	err := s.ledgerService.Transfer(ctx, transferReq)
	if err != nil {
		s.auditLogger.Append(fmt.Sprintf("Transfer failed: %v", err))
		return nil, status.Errorf(codes.Internal, "failed to transfer: %v", err)
	}

	// Log successful transfer
	s.auditLogger.Append(fmt.Sprintf("Transfer completed: from=%s, to=%s, amount=%.8f", 
		req.FromAccountId, req.ToAccountId, req.Amount))

	return &ledgerpb.TransferResponse{
		Success:         true,
		FromAccountId:   req.FromAccountId,
		ToAccountId:     req.ToAccountId,
		Amount:          req.Amount,
		Description:     req.Description,
		TransactionId:   transferReq.FromAccountID + "-" + transferReq.ToAccountID, // Simplified
		CreatedAt:       time.Now().Format(time.RFC3339),
	}, nil
}

// GetBalance implements the GetBalance gRPC method
func (s *LedgerGRPCService) GetBalance(ctx context.Context, req *ledgerpb.GetBalanceRequest) (*ledgerpb.GetBalanceResponse, error) {
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}

	balance, err := s.ledgerService.GetBalance(ctx, req.AccountId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "account not found: %v", err)
	}

	// Get account info for currency code
	account, err := s.ledgerService.GetAccountByID(ctx, req.AccountId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "account not found: %v", err)
	}

	return &ledgerpb.GetBalanceResponse{
		AccountId:     req.AccountId,
		Balance:       balance,
		CurrencyCode:  account.CurrencyCode,
		UpdatedAt:     time.Now().Format(time.RFC3339),
	}, nil
}

// Reconcile implements the Reconcile gRPC method
func (s *LedgerGRPCService) Reconcile(ctx context.Context, req *ledgerpb.ReconcileRequest) (*ledgerpb.ReconcileResponse, error) {
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}

	startTime, err := time.Parse(time.RFC3339, req.StartTime)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid start_time format")
	}

	endTime, err := time.Parse(time.RFC3339, req.EndTime)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid end_time format")
	}

	reconcileReq := ledger.ReconcileRequest{
		AccountID: req.AccountId,
		StartTime: startTime,
		EndTime:   endTime,
	}

	snapshots, err := s.ledgerService.Reconcile(ctx, reconcileReq)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "reconciliation failed: %v", err)
	}

	// Convert snapshots to protobuf format
	var pbSnapshots []*ledgerpb.BalanceSnapshot
	for _, snapshot := range snapshots {
		pbSnapshot := &ledgerpb.BalanceSnapshot{
			Id:             snapshot.ID,
			AccountId:      snapshot.AccountID,
			TransactionId:  snapshot.TransactionID,
			SnapshotTime:   snapshot.SnapshotTime,
			BalanceBefore:  snapshot.BalanceBefore,
			BalanceAfter:   snapshot.BalanceAfter,
			BalanceChange:  snapshot.BalanceChange,
			AccountType:    snapshot.AccountType,
			CurrencyCode:   snapshot.CurrencyCode,
			EntryId:        snapshot.EntryID,
			EntryType:      snapshot.EntryType,
			Amount:         snapshot.Amount,
			Description:    snapshot.Description,
			ReferenceType:  snapshot.ReferenceType,
			ReferenceId:    snapshot.ReferenceID,
			CreatedAt:      snapshot.CreatedAt,
		}
		pbSnapshots = append(pbSnapshots, pbSnapshot)
	}

	// Perform drift detection
	consistencyResults, err := s.ledgerService.ValidateBalanceConsistency(ctx)
	if err != nil {
		log.Printf("Warning: failed to validate consistency: %v", err)
	}

	var hasDrift bool
	var driftAmount float64
	for _, result := range consistencyResults {
		if accountID, ok := result["account_id"].(string); ok && accountID == req.AccountId {
			if drift, ok := result["drift_amount"].(float64); ok {
				hasDrift = abs(drift) > 0.000001
				driftAmount = drift
				break
			}
		}
	}

	return &ledgerpb.ReconcileResponse{
		Snapshots:        pbSnapshots,
		HasDrift:         hasDrift,
		DriftAmount:      driftAmount,
		ReconciliationTime: time.Now().Format(time.RFC3339),
	}, nil
}

// ListAccounts implements the ListAccounts gRPC method
func (s *LedgerGRPCService) ListAccounts(ctx context.Context, req *ledgerpb.ListAccountsRequest) (*ledgerpb.ListAccountsResponse, error) {
	filter := ledger.AccountFilter{
		AccountType:  req.AccountType,
		CurrencyCode: req.CurrencyCode,
		Limit:        int(req.Limit),
		Offset:       int(req.Offset),
	}

	if req.IsActive != nil {
		filter.IsActive = &req.IsActive.Value
	}

	accounts, err := s.ledgerService.ListAccounts(ctx, filter)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list accounts: %v", err)
	}

	var pbAccounts []*ledgerpb.Account
	for _, account := range accounts {
		pbAccount := &ledgerpb.Account{
			Id:             account.ID,
			AccountNumber:  account.AccountNumber,
			AccountType:    account.AccountType,
			Name:           account.Name,
			CurrencyCode:   account.CurrencyCode,
			IsActive:       account.IsActive,
			CreatedAt:      account.CreatedAt,
			CreatedBy:      account.CreatedBy,
			CurrentBalance: account.CurrentBalance,
		}

		// Convert metadata
		pbAccount.Metadata = make(map[string]string)
		for k, v := range account.Metadata {
			if str, ok := v.(string); ok {
				pbAccount.Metadata[k] = str
			}
		}

		pbAccounts = append(pbAccounts, pbAccount)
	}

	return &ledgerpb.ListAccountsResponse{
		Accounts: pbAccounts,
		Total:    int32(len(pbAccounts)),
	}, nil
}

// GetAccount implements the GetAccount gRPC method
func (s *LedgerGRPCService) GetAccount(ctx context.Context, req *ledgerpb.GetAccountRequest) (*ledgerpb.GetAccountResponse, error) {
	var account *ledger.Account
	var err error

	if req.AccountNumber != "" {
		account, err = s.ledgerService.GetAccount(ctx, req.AccountNumber)
	} else if req.AccountId != "" {
		account, err = s.ledgerService.GetAccountByID(ctx, req.AccountId)
	} else {
		return nil, status.Error(codes.InvalidArgument, "either account_number or account_id is required")
	}

	if err != nil {
		return nil, status.Errorf(codes.NotFound, "account not found: %v", err)
	}

	pbAccount := &ledgerpb.Account{
		Id:             account.ID,
		AccountNumber:  account.AccountNumber,
		AccountType:    account.AccountType,
		Name:           account.Name,
		CurrencyCode:   account.CurrencyCode,
		IsActive:       account.IsActive,
		CreatedAt:      account.CreatedAt,
		CreatedBy:      account.CreatedBy,
		CurrentBalance: account.CurrentBalance,
	}

	// Convert metadata
	pbAccount.Metadata = make(map[string]string)
	for k, v := range account.Metadata {
		if str, ok := v.(string); ok {
			pbAccount.Metadata[k] = str
		}
	}

	return &ledgerpb.GetAccountResponse{
		Account: pbAccount,
	}, nil
}

// ValidateConsistency implements the ValidateConsistency gRPC method
func (s *LedgerGRPCService) ValidateConsistency(ctx context.Context, req *ledgerpb.ValidateConsistencyRequest) (*ledgerpb.ValidateConsistencyResponse, error) {
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}

	results := s.validator.ComprehensiveValidation(ctx, req.AccountId)

	var pbResults []*ledgerpb.ValidationResult
	var errorCount int

	for _, result := range results {
		pbResult := &ledgerpb.ValidationResult{
			IsValid:         result.IsValid,
			ValidationType:  result.ValidationType,
			Message:         result.Message,
			AccountId:       result.AccountID,
			TransactionId:   result.TransactionID,
			Timestamp:       result.Timestamp.Format(time.RFC3339),
		}

		// Convert details
		pbResult.Details = make(map[string]string)
		for k, v := range result.Details {
			pbResult.Details[k] = fmt.Sprintf("%v", v)
		}

		pbResults = append(pbResults, pbResult)

		if !result.IsValid {
			errorCount++
		}
	}

	isFullyValid := errorCount == 0

	return &ledgerpb.ValidateConsistencyResponse{
		Results:       pbResults,
		IsFullyValid:  isFullyValid,
		ErrorCount:    int32(errorCount),
	}, nil
}

// abs returns absolute value of float64
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Setup PostgreSQL connection pool
	dbURL := cfg.DatabaseURL
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("Failed to create database pool: %v", err)
	}
	defer pool.Close()

	// Test database connection
	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	// Initialize ledger components
	postgresLedger := ledger.NewPostgresLedger(pool)
	ledgerService := ledger.NewLedgerService(postgresLedger)
	validator := ledger.NewValidator(postgresLedger)
	grpcService := NewLedgerGRPCService(ledgerService, validator)

	// Setup gRPC server
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(1024*1024), // 1MB
		grpc.MaxSendMsgSize(1024*1024), // 1MB
	)

	// Register services
	ledgerpb.RegisterLedgerServiceServer(grpcServer, grpcService)
	
	// Enable reflection for debugging
	reflection.Register(grpcServer)

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-ctx.Done()
		log.Println("Shutting down gRPC server...")
		grpcServer.GracefulStop()
	}()

	// Handle OS signals for graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
	}()

	// Start server
	log.Println("Starting Ledger gRPC server on :50051")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}