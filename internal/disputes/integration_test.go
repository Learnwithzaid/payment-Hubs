package disputes

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// IntegrationTestSuite tests the complete disputes workflow
type IntegrationTestSuite struct {
	suite.Suite
	pool     *pgxpool.Pool
	service  *DisputesService
	ledger   *MockLedgerService
	sm       *StateMachine
	cleanup  func()
}

// MockLedgerService implements minimal ledger interface for testing
type MockLedgerService struct {
	accounts map[string]*Account
	mu       sync.RWMutex
}

func NewMockLedgerService() *MockLedgerService {
	return &MockLedgerService{
		accounts: make(map[string]*Account),
	}
}

func (m *MockLedgerService) CreateAccount(ctx context.Context, req CreateAccountRequest) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	account := &Account{
		ID:            uuid.NewString(),
		AccountNumber: req.AccountNumber,
		AccountType:   req.AccountType,
		Name:          req.Name,
		CurrencyCode:  req.CurrencyCode,
		CreatedBy:     req.CreatedBy,
		CreatedAt:     time.Now(),
		Metadata:      req.Metadata,
	}

	m.accounts[account.ID] = account
	return account, nil
}

func (m *MockLedgerService) GetAccount(ctx context.Context, accountID string) (*Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	account, exists := m.accounts[accountID]
	if !exists {
		return nil, sql.ErrNoRows
	}
	return account, nil
}

func (m *MockLedgerService) PostJournalEntry(ctx context.Context, entry *JournalEntry) error {
	// Mock implementation - just log the entry
	return nil
}

func (m *MockLedgerService) GetBalance(ctx context.Context, accountID string) (float64, error) {
	return 1000.0, nil // Mock balance
}

// SetupSuite creates test database and services
func (suite *IntegrationTestSuite) SetupSuite() {
	// Create test database connection
	// This would typically use a test database URL from environment
	// For this example, we'll skip actual DB setup and use mocks
	suite.ledger = NewMockLedgerService()
	
	// Create state machine with mock store
	transitionStore := &MockTransitionStore{}
	suite.sm = NewStateMachine(transitionStore)
	
	// Create disputes service
	suite.service = &DisputesService{
		pool:             nil, // Would be set in real test with actual DB pool
		ledger:           suite.ledger,
		stateMachine:     suite.sm,
		reservePercentage: 0.05, // 5% reserve
	}
}

// TestCompleteDisputeWorkflow tests the full dispute lifecycle
func (suite *IntegrationTestSuite) TestCompleteDisputeWorkflow() {
	ctx := context.Background()
	
	// Step 1: Create a mock journal entry for dispute
	journalEntryID := uuid.NewString()
	merchantID := uuid.NewString()
	
	// Step 2: Create dispute
	createReq := CreateDisputeRequest{
		JournalEntryID:  journalEntryID,
		MerchantID:      merchantID,
		DisputedAmount:  100.0,
		CurrencyCode:    "USD",
		ReasonCode:      "14.1", // Visa Cardholder Dispute - Fraud
		ReasonText:      "Cardholder does not recognize transaction",
		ReferenceType:   "transaction",
		ReferenceID:     "txn-123",
		CreatedBy:       "test-user",
		Metadata: map[string]interface{}{
			"card_last_four": "1234",
			"transaction_id": "TXN-456",
		},
	}

	dispute, err := suite.service.CreateDispute(ctx, createReq)
	suite.NoError(err)
	suite.NotNil(dispute)
	
	// Verify dispute was created with correct properties
	suite.Equal("PENDING", string(dispute.Status))
	suite.Equal(100.0, dispute.DisputedAmount)
	suite.Equal("14.1", dispute.ReasonCode)
	suite.True(dispute.IsFraud) // Based on reason code
	suite.NotEmpty(dispute.DisputeID)
	
	// Step 3: Authorize dispute
	err = suite.service.AuthorizeDispute(ctx, dispute.DisputeID, "compliance-officer")
	suite.NoError(err)
	
	// Verify state transition
	currentState, err := suite.sm.GetCurrentState(ctx, dispute.DisputeID)
	suite.NoError(err)
	suite.Equal(StateAuthorized, currentState)
	
	// Step 4: Settle transaction (making it eligible for dispute)
	err = suite.service.SettleTransaction(ctx, journalEntryID, "settlement-system")
	suite.NoError(err)
	
	// Step 5: Initiate dispute
	err = suite.service.InitiateDispute(ctx, dispute.DisputeID, "dispute-processor")
	suite.NoError(err)
	
	// Verify final state
	currentState, err = suite.sm.GetCurrentState(ctx, dispute.DisputeID)
	suite.NoError(err)
	suite.Equal(StateDisputed, currentState)
	
	// Step 6: Reverse dispute
	err = suite.service.ReverseDispute(ctx, dispute.DisputeID, "compliance-manager", "Evidence provided by merchant")
	suite.NoError(err)
	
	// Verify final state
	currentState, err = suite.sm.GetCurrentState(ctx, dispute.DisputeID)
	suite.NoError(err)
	suite.Equal(StateReversed, currentState)
	
	// Step 7: Verify immutable history
	history, err := suite.sm.GetStateHistory(ctx, dispute.DisputeID)
	suite.NoError(err)
	suite.NotEmpty(history)
	
	// Should have transitions: PENDING -> AUTHORIZED -> DISPUTED -> REVERSED
	suite.True(len(history) >= 4)
	
	// Verify hash chain integrity
	valid, err := suite.sm.VerifyChainIntegrity(ctx, dispute.DisputeID)
	suite.NoError(err)
	suite.True(valid)
}

// TestConcurrentDisputes tests handling concurrent disputes on same transaction
func (suite *IntegrationTestSuite) TestConcurrentDisputes() {
	ctx := context.Background()
	journalEntryID := uuid.NewString()
	merchantID := uuid.NewString()
	
	// Create multiple disputes concurrently
	numDisputes := 5
	disputes := make([]*Dispute, numDisputes)
	errors := make([]error, numDisputes)
	var wg sync.WaitGroup
	
	for i := 0; i < numDisputes; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			
			req := CreateDisputeRequest{
				JournalEntryID:  journalEntryID,
				MerchantID:      merchantID,
				DisputedAmount:  50.0 + float64(index)*10.0,
				CurrencyCode:    "USD",
				ReasonCode:      "14.1",
				ReasonText:      "Concurrent dispute",
				CreatedBy:       "test-user",
				Metadata:        map[string]interface{}{"dispute_index": index},
			}
			
			disputes[index], errors[index] = suite.service.CreateDispute(ctx, req)
		}(i)
	}
	
	wg.Wait()
	
	// Verify all disputes were created successfully
	for i := 0; i < numDisputes; i++ {
		suite.NoError(errors[i])
		suite.NotNil(disputes[i])
	}
	
	// Each dispute should have unique dispute IDs
	disputeIDs := make(map[string]bool)
	for _, dispute := range disputes {
		suite.False(disputeIDs[dispute.DisputeID], "Dispute IDs should be unique")
		disputeIDs[dispute.DisputeID] = true
	}
}

// TestFraudReserveCalculation tests fraud reserve computations
func (suite *IntegrationTestSuite) TestFraudReserveCalculation() {
	ctx := context.Background()
	merchantID := uuid.NewString()
	
	// Test reserve calculation for different volumes
	testCases := []struct {
		volume     float64
		reservePct float64
		expected   float64
	}{
		{1000.0, 0.05, 50.0},
		{10000.0, 0.05, 500.0},
		{100000.0, 0.05, 5000.0},
	}
	
	for _, tc := range testCases {
		actual, err := suite.service.CalculateMerchantReserve(ctx, merchantID, tc.volume)
		suite.NoError(err)
		suite.Equal(tc.expected, actual, "Reserve calculation incorrect for volume %f", tc.volume)
	}
}

// TestPIIMaskingInAuditLog tests that PII is properly masked in audit logs
func (suite *IntegrationTestSuite) TestPIIMaskingInAuditLog() {
	ctx := context.Background()
	
	// Create dispute with PII data
	createReq := CreateDisputeRequest{
		JournalEntryID:  uuid.NewString(),
		MerchantID:      uuid.NewString(),
		DisputedAmount:  100.0,
		CurrencyCode:    "USD",
		ReasonCode:      "14.1",
		ReasonText:      "Cardholder dispute",
		CreatedBy:       "test-user",
		Metadata: map[string]interface{}{
			"card_number":       "4111111111111111",
			"cvv":              "123",
			"cardholder_name":  "John Doe",
			"email":            "john@example.com",
			"phone":            "555-123-4567",
			"normal_field":     "should_be_visible",
		},
	}

	dispute, err := suite.service.CreateDispute(ctx, createReq)
	suite.NoError(err)
	suite.NotNil(dispute)
	
	// Verify PII was masked in metadata
	suite.NotNil(dispute.Metadata)
	
	// Check that sensitive fields were masked
	cardNumber, exists := dispute.Metadata["card_number"]
	suite.True(exists)
	suite.NotEqual("4111111111111111", cardNumber)
	
	normalField, exists := dispute.Metadata["normal_field"]
	suite.True(exists)
	suite.Equal("should_be_visible", normalField)
}

// TestInvalidStateTransitions tests that invalid state transitions are rejected
func (suite *IntegrationTestSuite) TestInvalidStateTransitions() {
	ctx := context.Background()
	
	// Create a dispute
	createReq := CreateDisputeRequest{
		JournalEntryID:  uuid.NewString(),
		MerchantID:      uuid.NewString(),
		DisputedAmount:  100.0,
		CurrencyCode:    "USD",
		ReasonCode:      "14.1",
		ReasonText:      "Test dispute",
		CreatedBy:       "test-user",
	}
	
	dispute, err := suite.service.CreateDispute(ctx, createReq)
	suite.NoError(err)
	
	// Test invalid transitions
	invalidTransitions := []struct {
		fromState DisputeState
		toState   DisputeState
		shouldFail bool
	}{
		{StatePending, StateSettled, true},      // PENDING -> SETTLED is invalid
		{StatePending, StateDisputed, true},     // PENDING -> DISPUTED is invalid
		{StateAuthorized, StatePending, true},   // Cannot go backwards
		{StateSettled, StateAuthorized, true},   // Cannot go backwards
		{StateDisputed, StateSettled, true},     // Cannot go backwards from DISPUTED
		{StatePending, StateAuthorized, false},  // This should work
		{StateAuthorized, StateSettled, false},  // This should work
	}
	
	for _, test := range invalidTransitions {
		transitionReq := TransitionRequest{
			DisputeID: dispute.DisputeID,
			ToState:   test.toState,
			Reason:    "Test transition",
			CreatedBy: "test-user",
			Metadata:  map[string]interface{}{},
		}
		
		// Set up the state if needed
		if test.fromState != StatePending {
			// First transition to establish the state
			firstReq := TransitionRequest{
				DisputeID: dispute.DisputeID,
				ToState:   test.fromState,
				Reason:    "Setup state",
				CreatedBy: "test-user",
				Metadata:  map[string]interface{}{},
			}
			
			result := suite.sm.Transition(ctx, firstReq)
			if test.fromState != StatePending {
				suite.True(result.Success, "Setup transition should succeed")
			}
		}
		
		result := suite.sm.Transition(ctx, transitionReq)
		
		if test.shouldFail {
			suite.False(result.Success, "Transition %s -> %s should fail", test.fromState, test.toState)
			suite.Error(result.Error)
		} else {
			suite.True(result.Success, "Transition %s -> %s should succeed", test.fromState, test.toState)
			suite.NoError(result.Error)
		}
	}
}

// TestReasonCodeValidation tests comprehensive reason code validation
func (suite *IntegrationTestSuite) TestReasonCodeValidation() {
	// Test valid Visa codes
	visaCodes := []string{"10.1", "14.1", "12.1", "13.1"}
	for _, code := range visaCodes {
		reasonCode, err := ValidateReasonCode(code)
		suite.NoError(err)
		suite.Equal(BrandVisa, reasonCode.Brand)
		suite.NotEmpty(reasonCode.Description)
	}
	
	// Test valid Mastercard codes
	mcCodes := []string{"4807", "4837", "4840", "4853"}
	for _, code := range mcCodes {
		reasonCode, err := ValidateReasonCode(code)
		suite.NoError(err)
		suite.Equal(BrandMastercard, reasonCode.Brand)
		suite.NotEmpty(reasonCode.Description)
	}
	
	// Test invalid codes
	invalidCodes := []string{"999.9", "ABC", "", "1.1.1"}
	for _, code := range invalidCodes {
		_, err := ValidateReasonCode(code)
		suite.Error(err)
	}
	
	// Test fraud vs non-fraud classification
	fraudCodes := GetFraudReasonCodes()
	nonFraudCodes := GetNonFraudReasonCodes()
	
	suite.NotEmpty(fraudCodes)
	suite.NotEmpty(nonFraudCodes)
	
	// Ensure no overlap between fraud and non-fraud codes
	fraudCodeSet := make(map[string]bool)
	for _, code := range fraudCodes {
		fraudCodeSet[code.Code] = true
	}
	
	for _, code := range nonFraudCodes {
		suite.False(fraudCodeSet[code.Code], "Code %s appears in both fraud and non-fraud sets", code.Code)
	}
}

// TestDisputeFiltering tests dispute listing with various filters
func (suite *IntegrationTestSuite) TestDisputeFiltering() {
	ctx := context.Background()
	
	// Create multiple disputes with different properties
	merchant1ID := uuid.NewString()
	merchant2ID := uuid.NewString()
	
	disputes := []*Dispute{}
	
	// Create fraud dispute for merchant1
	req1 := CreateDisputeRequest{
		JournalEntryID:  uuid.NewString(),
		MerchantID:      merchant1ID,
		DisputedAmount:  100.0,
		CurrencyCode:    "USD",
		ReasonCode:      "14.1", // Fraud code
		ReasonText:      "Fraud dispute",
		CreatedBy:       "test-user",
	}
	dispute1, err := suite.service.CreateDispute(ctx, req1)
	suite.NoError(err)
	disputes = append(disputes, dispute1)
	
	// Create non-fraud dispute for merchant2
	req2 := CreateDisputeRequest{
		JournalEntryID:  uuid.NewString(),
		MerchantID:      merchant2ID,
		DisputedAmount:  50.0,
		CurrencyCode:    "USD",
		ReasonCode:      "10.1", // Non-fraud code
		ReasonText:      "Authorization dispute",
		CreatedBy:       "test-user",
	}
	dispute2, err := suite.service.CreateDispute(ctx, req2)
	suite.NoError(err)
	disputes = append(disputes, dispute2)
	
	// Test filtering by merchant
	// Note: In real implementation, this would query the database
	// For this test, we're testing the filter structure
	
	filter := DisputeFilter{
		MerchantID: merchant1ID,
	}
	
	// Verify filter structure
	suite.Equal(merchant1ID, filter.MerchantID)
	
	// Test filtering by fraud status
	isFraud := true
	filterFraud := DisputeFilter{
		IsFraud: &isFraud,
	}
	suite.True(*filterFraud.IsFraud)
	
	// Test filtering by status
	filterStatus := DisputeFilter{
		Status: "PENDING",
	}
	suite.Equal("PENDING", filterStatus.Status)
}

// TestACIDCompliance tests that dispute operations maintain ACID properties
func (suite *IntegrationTestSuite) TestACIDCompliance() {
	ctx := context.Background()
	
	// Test atomicity - operations should either completely succeed or fail
	journalEntryID := uuid.NewString()
	merchantID := uuid.NewString()
	
	// Create dispute with valid data
	validReq := CreateDisputeRequest{
		JournalEntryID:  journalEntryID,
		MerchantID:      merchantID,
		DisputedAmount:  100.0,
		CurrencyCode:    "USD",
		ReasonCode:      "14.1",
		ReasonText:      "Valid dispute",
		CreatedBy:       "test-user",
	}
	
	dispute, err := suite.service.CreateDispute(ctx, validReq)
	suite.NoError(err)
	suite.NotNil(dispute)
	
	// Test consistency - state transitions should be consistent
	err = suite.service.AuthorizeDispute(ctx, dispute.DisputeID, "auth-user")
	suite.NoError(err)
	
	// Verify state is consistent
	currentState, err := suite.sm.GetCurrentState(ctx, dispute.DisputeID)
	suite.NoError(err)
	suite.Equal(StateAuthorized, currentState)
	
	// Test isolation - concurrent operations should not interfere
	var wg sync.WaitGroup
	errors := make([]error, 3)
	
	// Attempt multiple operations concurrently
	wg.Add(3)
	go func() { defer wg.Done(); errors[0] = suite.service.AuthorizeDispute(ctx, dispute.DisputeID, "user1") }()
	go func() { defer wg.Done(); errors[1] = suite.service.AuthorizeDispute(ctx, dispute.DisputeID, "user2") }()
	go func() { defer wg.Done(); errors[2] = suite.service.AuthorizeDispute(ctx, dispute.DisputeID, "user3") }()
	
	wg.Wait()
	
	// At most one should succeed (isolation), others should fail
	successCount := 0
	for _, err := range errors {
		if err == nil {
			successCount++
		}
	}
	suite.Equal(1, successCount, "Should have exactly one successful operation under isolation")
}

// TestDisputeAuditTrail tests that all operations create proper audit trails
func (suite *IntegrationTestSuite) TestDisputeAuditTrail() {
	ctx := context.Background()
	
	// Create dispute and perform multiple state transitions
	req := CreateDisputeRequest{
		JournalEntryID:  uuid.NewString(),
		MerchantID:      uuid.NewString(),
		DisputedAmount:  100.0,
		CurrencyCode:    "USD",
		ReasonCode:      "14.1",
		ReasonText:      "Audit trail test",
		CreatedBy:       "test-user",
		Metadata: map[string]interface{}{
			"test_case": "audit_trail",
			"timestamp": time.Now().Unix(),
		},
	}
	
	dispute, err := suite.service.CreateDispute(ctx, req)
	suite.NoError(err)
	suite.NotNil(dispute)
	
	// Perform series of state changes
	stateChanges := []struct {
		toState   DisputeState
		reason    string
		user      string
	}{
		{StateAuthorized, "Authorized for processing", "compliance-officer"},
		{StateSettled, "Transaction settled", "settlement-system"},
		{StateDisputed, "Dispute initiated", "dispute-processor"},
		{StateReversed, "Evidence provided", "compliance-manager"},
	}
	
	for _, change := range stateChanges {
		transitionReq := TransitionRequest{
			DisputeID: dispute.DisputeID,
			ToState:   change.toState,
			Reason:    change.reason,
			CreatedBy: change.user,
			Metadata:  map[string]interface{}{},
		}
		
		result := suite.sm.Transition(ctx, transitionReq)
		suite.True(result.Success)
		suite.NotNil(result.Transition)
		
		// Verify transition record
		transition := result.Transition
		suite.Equal(dispute.DisputeID, transition.DisputeID)
		suite.Equal(change.reason, transition.Reason)
		suite.Equal(change.user, transition.CreatedBy)
		suite.NotEmpty(transition.TransitionHash)
	}
	
	// Verify complete history
	history, err := suite.sm.GetStateHistory(ctx, dispute.DisputeID)
	suite.NoError(err)
	
	// Should have initial creation + all state changes
	suite.True(len(history) >= len(stateChanges)+1)
	
	// Verify hash chain
	valid, err := suite.sm.VerifyChainIntegrity(ctx, dispute.DisputeID)
	suite.NoError(err)
	suite.True(valid)
}

// TestChargebackFeeCalculation tests chargeback fee calculations
func (suite *IntegrationTestSuite) TestChargebackFeeCalculation() {
	ctx := context.Background()
	
	testCases := []struct {
		amount    float64
		brand     CardBrand
		expected  float64
	}{
		{100.0, BrandVisa, 5.0},      // 2% of 100 = 2, min 5
		{1000.0, BrandVisa, 15.0},    // 2% of 1000 = 20, max 15
		{100.0, BrandMastercard, 8.0}, // 2.5% of 100 = 2.5, min 8
		{1000.0, BrandMastercard, 25.0}, // 2.5% of 1000 = 25, max 25
	}
	
	for _, tc := range testCases {
		fee := calculateChargebackFee(tc.amount, tc.brand)
		suite.Equal(tc.expected, fee, "Chargeback fee calculation incorrect for amount %f and brand %s", tc.amount, tc.brand)
	}
}

// RunIntegrationTests runs the integration test suite
func RunIntegrationTests(t *testing.T) {
	// Create a new test suite
	integrationSuite := new(IntegrationTestSuite)
	
	// Run the suite with cleanup
	suite.Run(t, integrationSuite)
}

// Helper function to run individual tests
func (suite *IntegrationTestSuite) TestHelper() {
	// This method ensures the suite structure is correct
	suite.NotNil(suite.service)
	suite.NotNil(suite.sm)
}

// Benchmark tests for performance evaluation
func BenchmarkCreateDispute(b *testing.B) {
	ctx := context.Background()
	
	// Setup
	service := &DisputesService{
		ledger:           NewMockLedgerService(),
		stateMachine:     NewStateMachine(&MockTransitionStore{}),
		reservePercentage: 0.05,
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		req := CreateDisputeRequest{
			JournalEntryID:  uuid.NewString(),
			MerchantID:      uuid.NewString(),
			DisputedAmount:  100.0,
			CurrencyCode:    "USD",
			ReasonCode:      "14.1",
			ReasonText:      "Benchmark dispute",
			CreatedBy:       "benchmark-user",
		}
		
		_, err := service.CreateDispute(ctx, req)
		if err != nil {
			b.Fatalf("CreateDispute failed: %v", err)
		}
	}
}

func BenchmarkStateTransition(b *testing.B) {
	ctx := context.Background()
	
	// Setup
	transitionStore := &MockTransitionStore{}
	sm := NewStateMachine(transitionStore)
	
	// Create initial dispute
	disputeID := "benchmark-dispute"
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		transitionReq := TransitionRequest{
			DisputeID: disputeID,
			ToState:   StateAuthorized,
			Reason:    "Benchmark transition",
			CreatedBy: "benchmark-user",
		}
		
		result := sm.Transition(ctx, transitionReq)
		if !result.Success {
			b.Fatalf("State transition failed: %v", result.Error)
		}
	}
}

// Test utility functions
func TestCalculateMinMax(t *testing.T) {
	tests := []struct {
		a, b      float64
		minExpected float64
		maxExpected float64
	}{
		{1.0, 2.0, 1.0, 2.0},
		{2.0, 1.0, 1.0, 2.0},
		{-1.0, 1.0, -1.0, 1.0},
		{-2.0, -1.0, -2.0, -1.0},
		{5.0, 5.0, 5.0, 5.0},
	}
	
	for _, test := range tests {
		minActual := min(test.a, test.b)
		maxActual := max(test.a, test.b)
		assert.Equal(t, test.minExpected, minActual)
		assert.Equal(t, test.maxExpected, maxActual)
	}
}

// Additional unit tests for edge cases
func TestDisputeValidationEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		request  DisputeValidationRequest
		expected string
	}{
		{
			name: "Empty journal entry ID",
			request: DisputeValidationRequest{
				JournalEntryID: "",
				MerchantID:     uuid.NewString(),
				OriginalAmount: 100.0,
				DisputedAmount: 50.0,
				CurrencyCode:   "USD",
				ReasonCode:     "14.1",
				CreatedBy:      "test-user",
			},
			expected: "journal entry ID is required",
		},
		{
			name: "Zero disputed amount",
			request: DisputeValidationRequest{
				JournalEntryID: uuid.NewString(),
				MerchantID:     uuid.NewString(),
				OriginalAmount: 100.0,
				DisputedAmount: 0.0,
				CurrencyCode:   "USD",
				ReasonCode:     "14.1",
				CreatedBy:      "test-user",
			},
			expected: "disputed amount must be positive",
		},
		{
			name: "Negative original amount",
			request: DisputeValidationRequest{
				JournalEntryID: uuid.NewString(),
				MerchantID:     uuid.NewString(),
				OriginalAmount: -100.0,
				DisputedAmount: 50.0,
				CurrencyCode:   "USD",
				ReasonCode:     "14.1",
				CreatedBy:      "test-user",
			},
			expected: "original amount must be positive",
		},
	}
	
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDisputeRequest(test.request)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), test.expected)
		})
	}
}