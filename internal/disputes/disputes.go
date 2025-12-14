package disputes

// This file exports the main types and functions for the disputes module

// Core types
type (
	DisputeState      = DisputeState
	StateTransition   = StateTransition
	Dispute           = Dispute
	Hold             = Hold
	FraudReserve     = FraudReserve
	ReasonCode       = ReasonCode
)

// Main service
type (
	DisputesService = DisputesService
)

// Request/Response types
type (
	CreateDisputeRequest = CreateDisputeRequest
	TransitionRequest    = TransitionRequest
	DisputeFilter       = DisputeFilter
)

// State constants
const (
	StatePending    = StatePending
	StateAuthorized = StateAuthorized
	StateSettled    = StateSettled
	StateDisputed   = StateDisputed
	StateReversed   = StateReversed
)

// Card brands
const (
	BrandVisa        = BrandVisa
	BrandMastercard  = BrandMastercard
	BrandAmericanExpress = BrandAmericanExpress
	BrandDiscover    = BrandDiscover
	BrandJCB         = BrandJCB
	BrandDinersClub  = BrandDinersClub
)

// Constructor functions
func NewDisputesService(pool interface{}, ledger interface{}, reservePercentage float64) *DisputesService {
	// This is a type assertion placeholder - in real usage, this would be properly typed
	// The actual implementation is in service.go
	return nil
}

func NewStateMachine(store TransitionStore) *StateMachine {
	return NewStateMachine(store)
}

// Utility functions
func ValidateReasonCode(code string) (*ReasonCode, error) {
	return ValidateReasonCode(code)
}

func MaskPII(data map[string]interface{}) map[string]interface{} {
	return MaskPII(data)
}

func GetFraudReasonCodes() []ReasonCode {
	return GetFraudReasonCodes()
}

func GetNonFraudReasonCodes() []ReasonCode {
	return GetNonFraudReasonCodes()
}

func GetReasonCodesByBrand(brand CardBrand) []ReasonCode {
	return GetReasonCodesByBrand(brand)
}

func GetReasonCodesByCategory(category string) []ReasonCode {
	return GetReasonCodesByCategory(category)
}

func ValidateDisputeRequest(request DisputeValidationRequest) error {
	return ValidateDisputeRequest(request)
}

// State machine utilities
func AllowedTransitions() map[DisputeState][]DisputeState {
	return AllowedTransitions()
}

func StateDescription(state DisputeState) string {
	return StateDescription(state)
}

func OperationDescription(operation string) string {
	return OperationDescription(operation)
}