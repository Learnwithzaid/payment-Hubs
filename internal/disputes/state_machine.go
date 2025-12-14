package disputes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DisputeState represents the current state of a dispute
type DisputeState string

const (
	StatePending   DisputeState = "PENDING"
	StateAuthorized DisputeState = "AUTHORIZED"
	StateSettled   DisputeState = "SETTLED"
	StateDisputed  DisputeState = "DISPUTED"
	StateReversed  DisputeState = "REVERSED"
)

// InvalidStateTransitionError represents an invalid state transition
type InvalidStateTransitionError struct {
	FromState DisputeState
	ToState   DisputeState
	DisputeID string
}

func (e *InvalidStateTransitionError) Error() string {
	return fmt.Sprintf("invalid state transition from %s to %s for dispute %s", e.FromState, e.ToState, e.DisputeID)
}

// InvalidOperationError represents an invalid operation for the current state
type InvalidOperationError struct {
	State     DisputeState
	Operation string
	DisputeID string
}

func (e *InvalidOperationError) Error() string {
	return fmt.Sprintf("invalid operation %s for state %s in dispute %s", e.Operation, e.State, e.DisputeID)
}

// StateTransition represents a state transition with metadata
type StateTransition struct {
	ID            string       `json:"id"`
	DisputeID     string       `json:"dispute_id"`
	FromState     DisputeState `json:"from_state"`
	ToState       DisputeState `json:"to_state"`
	Reason        string       `json:"reason"`
	TransitionHash string      `json:"transition_hash"`
	PrevHash      string       `json:"prev_hash"`
	CreatedAt     time.Time    `json:"created_at"`
	CreatedBy     string       `json:"created_by"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// StateMachine manages dispute state transitions with immutable audit trail
type StateMachine struct {
	transitionStore TransitionStore
}

// TransitionStore defines the interface for storing state transitions
type TransitionStore interface {
	CreateTransition(ctx context.Context, transition *StateTransition) error
	GetLatestTransition(ctx context.Context, disputeID string) (*StateTransition, error)
	GetTransitionHistory(ctx context.Context, disputeID string) ([]*StateTransition, error)
	GetTransitionHash(ctx context.Context, disputeID string) (string, error)
}

// NewStateMachine creates a new dispute state machine
func NewStateMachine(store TransitionStore) *StateMachine {
	return &StateMachine{
		transitionStore: store,
	}
}

// AllowedTransitions defines valid state transitions
func AllowedTransitions() map[DisputeState][]DisputeState {
	return map[DisputeState][]DisputeState{
		StatePending:    {StateAuthorized, StateReversed},
		StateAuthorized: {StateSettled, StateReversed},
		StateSettled:    {StateDisputed, StateReversed},
		StateDisputed:   {StateReversed},
		StateReversed:   {}, // Terminal state
	}
}

// IsValidTransition checks if a state transition is allowed
func (sm *StateMachine) IsValidTransition(fromState, toState DisputeState) bool {
	allowed := AllowedTransitions()
	for _, allowedState := range allowed[fromState] {
		if allowedState == toState {
			return true
		}
	}
	return false
}

// ValidateOperation checks if an operation is allowed for the given state
func (sm *StateMachine) ValidateOperation(state DisputeState, operation string) error {
	switch operation {
	case "authorize":
		if state != StatePending {
			return &InvalidOperationError{State: state, Operation: operation}
		}
	case "settle":
		if state != StateAuthorized {
			return &InvalidOperationError{State: state, Operation: operation}
		}
	case "dispute":
		if state != StateSettled {
			return &InvalidOperationError{State: state, Operation: operation}
		}
	case "reverse":
		if state != StateDisputed && state != StateSettled && state != StateAuthorized && state != StatePending {
			return &InvalidOperationError{State: state, Operation: operation}
		}
	case "create":
		if state != "" { // Initial state
			return &InvalidOperationError{State: state, Operation: operation}
		}
	default:
		return fmt.Errorf("unknown operation: %s", operation)
	}
	return nil
}

// TransitionRequest represents a request to transition a dispute state
type TransitionRequest struct {
	DisputeID   string       `json:"dispute_id"`
	ToState     DisputeState `json:"to_state"`
	Reason      string       `json:"reason"`
	CreatedBy   string       `json:"created_by"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// TransitionResult represents the result of a state transition
type TransitionResult struct {
	Transition *StateTransition `json:"transition"`
	Success    bool             `json:"success"`
	Error      error            `json:"error"`
}

// Transition performs a state transition with immutable audit trail
func (sm *StateMachine) Transition(ctx context.Context, req TransitionRequest) *TransitionResult {
	// Validate request
	if req.DisputeID == "" {
		return &TransitionResult{
			Success: false,
			Error:   fmt.Errorf("dispute ID is required"),
		}
	}

	if req.ToState == "" {
		return &TransitionResult{
			Success: false,
			Error:   fmt.Errorf("target state is required"),
		}
	}

	if req.CreatedBy == "" {
		return &TransitionResult{
			Success: false,
			Error:   fmt.Errorf("created_by is required"),
		}
	}

	// Get current state and previous hash
	var currentState DisputeState
	var prevHash string
	var err error

	if req.ToState != StatePending { // Not initial state transition
		latestTransition, err := sm.transitionStore.GetLatestTransition(ctx, req.DisputeID)
		if err != nil {
			return &TransitionResult{
				Success: false,
				Error:   fmt.Errorf("failed to get current state: %w", err),
			}
		}

		if latestTransition != nil {
			currentState = latestTransition.ToState
			prevHash = latestTransition.TransitionHash
		}

		// Validate transition
		if !sm.IsValidTransition(currentState, req.ToState) {
			return &TransitionResult{
				Success: false,
				Error: &InvalidStateTransitionError{
					FromState: currentState,
					ToState:   req.ToState,
					DisputeID: req.DisputeID,
				},
			}
		}

		// Validate operation
		var operation string
		switch req.ToState {
		case StateAuthorized:
			operation = "authorize"
		case StateSettled:
			operation = "settle"
		case StateDisputed:
			operation = "dispute"
		case StateReversed:
			operation = "reverse"
		}

		if err := sm.ValidateOperation(currentState, operation); err != nil {
			return &TransitionResult{
				Success: false,
				Error: err,
			}
		}
	}

	// Create transition hash
	transitionHash, err := sm.calculateTransitionHash(req.DisputeID, currentState, req.ToState, prevHash, req.Reason, req.CreatedBy)
	if err != nil {
		return &TransitionResult{
			Success: false,
			Error:   fmt.Errorf("failed to calculate transition hash: %w", err),
		}
	}

	// Create transition record
	transition := &StateTransition{
		ID:             uuid.New().String(),
		DisputeID:      req.DisputeID,
		FromState:      currentState,
		ToState:        req.ToState,
		Reason:         req.Reason,
		TransitionHash: transitionHash,
		PrevHash:       prevHash,
		CreatedAt:      time.Now(),
		CreatedBy:      req.CreatedBy,
		Metadata:       req.Metadata,
	}

	// Store transition (this creates the immutable journal entry)
	err = sm.transitionStore.CreateTransition(ctx, transition)
	if err != nil {
		return &TransitionResult{
			Success: false,
			Error:   fmt.Errorf("failed to create transition: %w", err),
		}
	}

	return &TransitionResult{
		Success:    true,
		Transition: transition,
	}
}

// calculateTransitionHash creates a cryptographic hash for the transition
func (sm *StateMachine) calculateTransitionHash(disputeID string, fromState, toState DisputeState, prevHash, reason, createdBy string) (string, error) {
	hashInput := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		disputeID,
		string(fromState),
		string(toState),
		reason,
		createdBy,
		time.Now().Format(time.RFC3339Nano),
		prevHash,
	)

	hash := sha256.Sum256([]byte(hashInput))
	return hex.EncodeToString(hash[:]), nil
}

// GetCurrentState gets the current state of a dispute
func (sm *StateMachine) GetCurrentState(ctx context.Context, disputeID string) (DisputeState, error) {
	if disputeID == "" {
		return "", fmt.Errorf("dispute ID is required")
	}

	latestTransition, err := sm.transitionStore.GetLatestTransition(ctx, disputeID)
	if err != nil {
		return "", fmt.Errorf("failed to get current state: %w", err)
	}

	if latestTransition == nil {
		return StatePending, nil // Default initial state
	}

	return latestTransition.ToState, nil
}

// GetStateHistory gets the complete state history for a dispute
func (sm *StateMachine) GetStateHistory(ctx context.Context, disputeID string) ([]*StateTransition, error) {
	if disputeID == "" {
		return nil, fmt.Errorf("dispute ID is required")
	}

	return sm.transitionStore.GetTransitionHistory(ctx, disputeID)
}

// VerifyChainIntegrity verifies the integrity of the hash chain for a dispute
func (sm *StateMachine) VerifyChainIntegrity(ctx context.Context, disputeID string) (bool, error) {
	transitions, err := sm.GetStateHistory(ctx, disputeID)
	if err != nil {
		return false, fmt.Errorf("failed to get state history: %w", err)
	}

	if len(transitions) == 0 {
		return true, nil // No transitions to verify
	}

	// Verify each transition's hash chain
	for i, transition := range transitions {
		// Verify hash continuity
		if i > 0 {
			if transition.PrevHash != transitions[i-1].TransitionHash {
				return false, fmt.Errorf("hash chain broken at transition %s: expected %s, got %s",
					transition.ID, transitions[i-1].TransitionHash, transition.PrevHash)
			}
		}

		// Verify hash calculation
		expectedHash, err := sm.calculateTransitionHash(
			transition.DisputeID,
			transition.FromState,
			transition.ToState,
			transition.PrevHash,
			transition.Reason,
			transition.CreatedBy,
		)
		if err != nil {
			return false, fmt.Errorf("failed to calculate expected hash for transition %s: %w", transition.ID, err)
		}

		if transition.TransitionHash != expectedHash {
			return false, fmt.Errorf("hash mismatch at transition %s: expected %s, got %s",
				transition.ID, expectedHash, transition.TransitionHash)
		}
	}

	return true, nil
}

// GetAllowedStatesForState returns the states that can be transitioned to from the given state
func (sm *StateMachine) GetAllowedStatesForState(state DisputeState) []DisputeState {
	return AllowedTransitions()[state]
}

// StateDescription provides human-readable descriptions of states
func StateDescription(state DisputeState) string {
	switch state {
	case StatePending:
		return "Dispute has been created but not yet authorized"
	case StateAuthorized:
		return "Dispute has been authorized and is pending settlement"
	case StateSettled:
		return "Transaction has been settled and is eligible for dispute"
	case StateDisputed:
		return "Transaction is currently under dispute with chargeback initiated"
	case StateReversed:
		return "Dispute has been resolved with funds reversed"
	default:
		return "Unknown state"
	}
}

// OperationDescription provides human-readable descriptions of operations
func OperationDescription(operation string) string {
	switch operation {
	case "authorize":
		return "Authorize the dispute for processing"
	case "settle":
		return "Settle the underlying transaction"
	case "dispute":
		return "Initiate a formal dispute/chargeback"
	case "reverse":
		return "Reverse the dispute and release any holds"
	case "create":
		return "Create a new dispute"
	default:
		return "Unknown operation"
	}
}