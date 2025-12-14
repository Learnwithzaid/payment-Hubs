package disputes

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockTransitionStore implements TransitionStore for testing
type MockTransitionStore struct {
	transitions []*StateTransition
}

func (m *MockTransitionStore) CreateTransition(ctx context.Context, transition *StateTransition) error {
	m.transitions = append(m.transitions, transition)
	return nil
}

func (m *MockTransitionStore) GetLatestTransition(ctx context.Context, disputeID string) (*StateTransition, error) {
	for i := len(m.transitions) - 1; i >= 0; i-- {
		if m.transitions[i].DisputeID == disputeID {
			return m.transitions[i], nil
		}
	}
	return nil, nil
}

func (m *MockTransitionStore) GetTransitionHistory(ctx context.Context, disputeID string) ([]*StateTransition, error) {
	var history []*StateTransition
	for _, t := range m.transitions {
		if t.DisputeID == disputeID {
			history = append(history, t)
		}
	}
	return history, nil
}

func (m *MockTransitionStore) GetTransitionHash(ctx context.Context, disputeID string) (string, error) {
	latest, err := m.GetLatestTransition(ctx, disputeID)
	if err != nil {
		return "", err
	}
	if latest == nil {
		return "", nil
	}
	return latest.TransitionHash, nil
}

func TestStateMachine_ValidTransitions(t *testing.T) {
	store := &MockTransitionStore{}
	sm := NewStateMachine(store)

	// Test allowed transitions
	allowed := AllowedTransitions()
	
	// PENDING can go to AUTHORIZED or REVERSED
	assert.Contains(t, allowed[StatePending], StateAuthorized)
	assert.Contains(t, allowed[StatePending], StateReversed)
	assert.Equal(t, 2, len(allowed[StatePending]))
	
	// AUTHORIZED can go to SETTLED or REVERSED
	assert.Contains(t, allowed[StateAuthorized], StateSettled)
	assert.Contains(t, allowed[StateAuthorized], StateReversed)
	assert.Equal(t, 2, len(allowed[StateAuthorized]))
	
	// SETTLED can go to DISPUTED or REVERSED
	assert.Contains(t, allowed[StateSettled], StateDisputed)
	assert.Contains(t, allowed[StateSettled], StateReversed)
	assert.Equal(t, 2, len(allowed[StateSettled]))
	
	// DISPUTED can only go to REVERSED
	assert.Contains(t, allowed[StateDisputed], StateReversed)
	assert.Equal(t, 1, len(allowed[StateDisputed]))
	
	// REVERSED is terminal
	assert.Equal(t, 0, len(allowed[StateReversed]))
}

func TestStateMachine_TransitionValidation(t *testing.T) {
	store := &MockTransitionStore{}
	sm := NewStateMachine(store)

	// Test invalid transition
	result := sm.Transition(context.Background(), TransitionRequest{
		DisputeID: "test-dispute",
		ToState:   StateAuthorized, // Starting from empty state, this should fail
		Reason:    "test",
		CreatedBy: "test-user",
	})
	
	assert.False(t, result.Success)
	assert.Error(t, result.Error)

	// Test valid initial transition (PENDING)
	result = sm.Transition(context.Background(), TransitionRequest{
		DisputeID: "test-dispute",
		ToState:   StatePending,
		Reason:    "Initial state",
		CreatedBy: "test-user",
	})
	
	assert.True(t, result.Success)
	assert.NotNil(t, result.Transition)
	
	// Test valid transition from PENDING to AUTHORIZED
	result = sm.Transition(context.Background(), TransitionRequest{
		DisputeID: "test-dispute",
		ToState:   StateAuthorized,
		Reason:    "Authorized for processing",
		CreatedBy: "test-user",
	})
	
	assert.True(t, result.Success)
	assert.NotNil(t, result.Transition)
	assert.Equal(t, StateAuthorized, result.Transition.ToState)
}

func TestStateMachine_InvalidTransitions(t *testing.T) {
	store := &MockTransitionStore{}
	sm := NewStateMachine(store)

	// Set up dispute in PENDING state
	result := sm.Transition(context.Background(), TransitionRequest{
		DisputeID: "test-dispute",
		ToState:   StatePending,
		Reason:    "Initial state",
		CreatedBy: "test-user",
	})
	require.True(t, result.Success)

	// Try invalid transition from PENDING to SETTLED
	result = sm.Transition(context.Background(), TransitionRequest{
		DisputeID: "test-dispute",
		ToState:   StateSettled,
		Reason:    "Invalid transition",
		CreatedBy: "test-user",
	})
	
	assert.False(t, result.Success)
	assert.Error(t, result.Error)

	// Try invalid transition from PENDING to DISPUTED
	result = sm.Transition(context.Background(), TransitionRequest{
		DisputeID: "test-dispute",
		ToState:   StateDisputed,
		Reason:    "Invalid transition",
		CreatedBy: "test-user",
	})
	
	assert.False(t, result.Success)
	assert.Error(t, result.Error)
}

func TestStateMachine_ChainIntegrity(t *testing.T) {
	store := &MockTransitionStore{}
	sm := NewStateMachine(store)

	disputeID := "test-dispute-chain"
	
	// Create a chain of transitions
	transitions := []TransitionRequest{
		{DisputeID: disputeID, ToState: StatePending, Reason: "Initial", CreatedBy: "user1"},
		{DisputeID: disputeID, ToState: StateAuthorized, Reason: "Authorized", CreatedBy: "user2"},
		{DisputeID: disputeID, ToState: StateSettled, Reason: "Settled", CreatedBy: "user3"},
		{DisputeID: disputeID, ToState: StateReversed, Reason: "Reversed", CreatedBy: "user4"},
	}

	for _, req := range transitions {
		result := sm.Transition(context.Background(), req)
		assert.True(t, result.Success, "Failed at transition: %v", req)
	}

	// Verify chain integrity
	valid, err := sm.VerifyChainIntegrity(context.Background(), disputeID)
	assert.NoError(t, err)
	assert.True(t, valid, "Chain integrity should be valid")

	// Verify history
	history, err := sm.GetStateHistory(context.Background(), disputeID)
	assert.NoError(t, err)
	assert.Equal(t, 4, len(history))
	
	// Verify hash chain
	for i, transition := range history {
		if i == 0 {
			assert.Empty(t, transition.PrevHash, "First transition should have empty prev_hash")
		} else {
			assert.Equal(t, history[i-1].TransitionHash, transition.PrevHash, "Hash chain should be continuous")
		}
	}
}

func TestStateMachine_OperationValidation(t *testing.T) {
	store := &MockTransitionStore{}
	sm := NewStateMachine(store)

	// Test operation validation for different states
	tests := []struct {
		state      DisputeState
		operation  string
		valid      bool
	}{
		{StatePending, "authorize", true},
		{StatePending, "settle", false},
		{StatePending, "dispute", false},
		{StatePending, "reverse", false},
		
		{StateAuthorized, "authorize", false},
		{StateAuthorized, "settle", true},
		{StateAuthorized, "dispute", false},
		{StateAuthorized, "reverse", true},
		
		{StateSettled, "authorize", false},
		{StateSettled, "settle", false},
		{StateSettled, "dispute", true},
		{StateSettled, "reverse", true},
		
		{StateDisputed, "authorize", false},
		{StateDisputed, "settle", false},
		{StateDisputed, "dispute", false},
		{StateDisputed, "reverse", true},
		
		{StateReversed, "authorize", false},
		{StateReversed, "settle", false},
		{StateReversed, "dispute", false},
		{StateReversed, "reverse", false},
	}

	for _, test := range tests {
		err := sm.ValidateOperation(test.state, test.operation)
		if test.valid {
			assert.NoError(t, err, "Operation %s should be valid for state %s", test.operation, test.state)
		} else {
			assert.Error(t, err, "Operation %s should be invalid for state %s", test.operation, test.state)
		}
	}
}

func TestStateMachine_GetCurrentState(t *testing.T) {
	store := &MockTransitionStore{}
	sm := NewStateMachine(store)

	// Test with no transitions
	state, err := sm.GetCurrentState(context.Background(), "nonexistent")
	assert.NoError(t, err)
	assert.Equal(t, StatePending, state)

	// Create a dispute and verify states
	disputeID := "test-state"
	
	result := sm.Transition(context.Background(), TransitionRequest{
		DisputeID: disputeID,
		ToState:   StatePending,
		Reason:    "Initial",
		CreatedBy: "user",
	})
	require.True(t, result.Success)

	state, err = sm.GetCurrentState(context.Background(), disputeID)
	assert.NoError(t, err)
	assert.Equal(t, StatePending, state)

	result = sm.Transition(context.Background(), TransitionRequest{
		DisputeID: disputeID,
		ToState:   StateAuthorized,
		Reason:    "Authorized",
		CreatedBy: "user",
	})
	require.True(t, result.Success)

	state, err = sm.GetCurrentState(context.Background(), disputeID)
	assert.NoError(t, err)
	assert.Equal(t, StateAuthorized, state)
}

func TestReasonCodeValidation(t *testing.T) {
	tests := []struct {
		code       string
		valid      bool
		expected   string
		isFraud    bool
	}{
		{"10.1", true, "10.1", false}, // Visa Authorization
		{"14.1", true, "14.1", true},  // Visa Cardholder Dispute - Fraud
		{"4807", true, "4807", false}, // Mastercard Authorization
		{"4840", true, "4840", true},  // Mastercard Fraud
		{"999.9", false, "", false},   // Invalid code
		{"", false, "", false},        // Empty code
	}

	for _, test := range tests {
		reasonCode, err := ValidateReasonCode(test.code)
		if test.valid {
			assert.NoError(t, err)
			assert.Equal(t, test.expected, reasonCode.Code)
			assert.Equal(t, test.isFraud, reasonCode.Fraud)
		} else {
			assert.Error(t, err)
			assert.Nil(t, reasonCode)
		}
	}
}

func TestPIIMasking(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name: "Card number masking",
			input: map[string]interface{}{
				"card_number": "4111111111111111",
				"normal_field": "visible_value",
			},
			expected: map[string]interface{}{
				"card_number": "****1111",
				"normal_field": "visible_value",
			},
		},
		{
			name: "Email masking",
			input: map[string]interface{}{
				"email": "user@example.com",
				"name": "John Doe",
			},
			expected: map[string]interface{}{
				"email": "u***@example.com",
				"name": "John Doe",
			},
		},
		{
			name: "PAN masking",
			input: map[string]interface{}{
				"pan": "5555555555554444",
				"cvv": "123",
			},
			expected: map[string]interface{}{
				"pan": "****4444",
				"cvv": "***",
			},
		},
		{
			name: "Phone masking",
			input: map[string]interface{}{
				"phone": "+1-555-123-4567",
			},
			expected: map[string]interface{}{
				"phone": "***-***-4567",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := MaskPII(test.input)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestDisputeValidation(t *testing.T) {
	tests := []struct {
		name    string
		request DisputeValidationRequest
		valid   bool
	}{
		{
			name: "Valid dispute request",
			request: DisputeValidationRequest{
				DisputeID:      uuid.NewString(),
				JournalEntryID: uuid.NewString(),
				MerchantID:     uuid.NewString(),
				OriginalAmount: 100.0,
				DisputedAmount: 50.0,
				CurrencyCode:   "USD",
				ReasonCode:     "14.1",
				CreatedBy:      "test-user",
			},
			valid: true,
		},
		{
			name: "Invalid reason code",
			request: DisputeValidationRequest{
				DisputeID:      uuid.NewString(),
				JournalEntryID: uuid.NewString(),
				MerchantID:     uuid.NewString(),
				OriginalAmount: 100.0,
				DisputedAmount: 50.0,
				CurrencyCode:   "USD",
				ReasonCode:     "999.9",
				CreatedBy:      "test-user",
			},
			valid: false,
		},
		{
			name: "Disputed amount exceeds original",
			request: DisputeValidationRequest{
				DisputeID:      uuid.NewString(),
				JournalEntryID: uuid.NewString(),
				MerchantID:     uuid.NewString(),
				OriginalAmount: 50.0,
				DisputedAmount: 100.0,
				CurrencyCode:   "USD",
				ReasonCode:     "14.1",
				CreatedBy:      "test-user",
			},
			valid: false,
		},
		{
			name: "Invalid currency code",
			request: DisputeValidationRequest{
				DisputeID:      uuid.NewString(),
				JournalEntryID: uuid.NewString(),
				MerchantID:     uuid.NewString(),
				OriginalAmount: 100.0,
				DisputedAmount: 50.0,
				CurrencyCode:   "INVALID",
				ReasonCode:     "14.1",
				CreatedBy:      "test-user",
			},
			valid: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDisputeRequest(test.request)
			if test.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestStateDescriptions(t *testing.T) {
	tests := []struct {
		state       DisputeState
		description string
	}{
		{StatePending, "Dispute has been created but not yet authorized"},
		{StateAuthorized, "Dispute has been authorized and is pending settlement"},
		{StateSettled, "Transaction has been settled and is eligible for dispute"},
		{StateDisputed, "Transaction is currently under dispute with chargeback initiated"},
		{StateReversed, "Dispute has been resolved with funds reversed"},
		{"UNKNOWN", "Unknown state"},
	}

	for _, test := range tests {
		t.Run(string(test.state), func(t *testing.T) {
			assert.Equal(t, test.description, StateDescription(test.state))
		})
	}
}

func TestOperationDescriptions(t *testing.T) {
	tests := []struct {
		operation  string
		description string
	}{
		{"authorize", "Authorize the dispute for processing"},
		{"settle", "Settle the underlying transaction"},
		{"dispute", "Initiate a formal dispute/chargeback"},
		{"reverse", "Reverse the dispute and release any holds"},
		{"create", "Create a new dispute"},
		{"unknown", "Unknown operation"},
	}

	for _, test := range tests {
		t.Run(test.operation, func(t *testing.T) {
			assert.Equal(t, test.description, OperationDescription(test.operation))
		})
	}
}

func TestGetAllowedStatesForState(t *testing.T) {
	store := &MockTransitionStore{}
	sm := NewStateMachine(store)

	// Test getting allowed states
	allowed := sm.GetAllowedStatesForState(StatePending)
	assert.Contains(t, allowed, StateAuthorized)
	assert.Contains(t, allowed, StateReversed)
	assert.Equal(t, 2, len(allowed))

	allowed = sm.GetAllowedStatesForState(StateReversed)
	assert.Equal(t, 0, len(allowed))
}

func TestCalculateTransitionHash(t *testing.T) {
	store := &MockTransitionStore{}
	sm := NewStateMachine(store)

	disputeID := "test-hash"
	fromState := StatePending
	toState := StateAuthorized
	reason := "Test reason"
	createdBy := "test-user"
	prevHash := "previous-hash"

	hash1, err := sm.calculateTransitionHash(disputeID, fromState, toState, prevHash, reason, createdBy)
	assert.NoError(t, err)
	assert.NotEmpty(t, hash1)
	assert.Equal(t, 64, len(hash1)) // SHA256 hex string length

	// Same input should produce same hash
	hash2, err := sm.calculateTransitionHash(disputeID, fromState, toState, prevHash, reason, createdBy)
	assert.NoError(t, err)
	assert.Equal(t, hash1, hash2)

	// Different input should produce different hash
	hash3, err := sm.calculateTransitionHash("different-id", fromState, toState, prevHash, reason, createdBy)
	assert.NoError(t, err)
	assert.NotEqual(t, hash1, hash3)
}