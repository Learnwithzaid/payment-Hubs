package disputes

import (
	"fmt"
	"regexp"
	"strings"
)

// CardBrand represents the card network brand
type CardBrand string

const (
	BrandVisa       CardBrand = "VISA"
	BrandMastercard CardBrand = "MASTERCARD"
	BrandAmericanExpress CardBrand = "AMERICAN_EXPRESS"
	BrandDiscover   CardBrand = "DISCOVER"
	BrandJCB        CardBrand = "JCB"
	BrandDinersClub CardBrand = "DINERS_CLUB"
)

// ReasonCode represents a dispute reason code
type ReasonCode struct {
	Code        string    `json:"code"`
	Description string    `json:"description"`
	Brand       CardBrand `json:"brand"`
	Category    string    `json:"category"`
	Fraud       bool      `json:"fraud"`
	ChargebackFee bool    `json:"chargeback_fee"`
	ValidFrom   string    `json:"valid_from"`
	ValidTo     string    `json:"valid_to"`
}

// Reason codes based on Visa and Mastercard specifications
var ReasonCodes = map[string]ReasonCode{
	// Visa Reason Codes
	"10.1": {Code: "10.1", Description: "Authorization - Cardholder Dispute", Brand: BrandVisa, Category: "Authorization", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"10.2": {Code: "10.2", Description: "Authorization - Authorization Processing Error", Brand: BrandVisa, Category: "Authorization", Fraud: false, ChargebackFee: false, ValidFrom: "2019-04-15"},
	"11.1": {Code: "11.1", Description: "Card Recovery Bulletin - Card Recovery Bulletin", Brand: BrandVisa, Category: "Card Recovery", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"11.2": {Code: "11.2", Description: "Card Recovery Bulletin - Declined by Card Issuer", Brand: BrandVisa, Category: "Card Recovery", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"11.3": {Code: "11.3", Description: "Card Recovery Bulletin - Declined by Card Issuer", Brand: BrandVisa, Category: "Card Recovery", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"12.1": {Code: "12.1", Description: "Counterfeit Transaction - EMV Liability Shift Counterfeit", Brand: BrandVisa, Category: "Fraud", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"12.2": {Code: "12.2", Description: "Counterfeit Transaction - Contact Chip Counterfeit", Brand: BrandVisa, Category: "Fraud", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"12.5": {Code: "12.5", Description: "Counterfeit Transaction - Other Counterfeit", Brand: BrandVisa, Category: "Fraud", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"13.1": {Code: "13.1", Description: "Non-Counterfeit Fraud - Card-Present Fraud", Brand: BrandVisa, Category: "Fraud", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"13.2": {Code: "13.2", Description: "Non-Counterfeit Fraud - Card-Absent Fraud", Brand: BrandVisa, Category: "Fraud", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"13.7": {Code: "13.7", Description: "Non-Counterfeit Fraud - Card-Absent Environment", Brand: BrandVisa, Category: "Fraud", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"13.8": {Code: "13.8", Description: "Non-Counterfeit Fraud - Authentication Failed", Brand: BrandVisa, Category: "Fraud", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"13.9": {Code: "13.9", Description: "Non-Counterfeit Fraud - Magnetic Stripe Fallback", Brand: BrandVisa, Category: "Fraud", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"14.1": {Code: "14.1", Description: "Cardholder Dispute - Fraud", Brand: BrandVisa, Category: "Cardholder Dispute", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"14.2": {Code: "14.2", Description: "Cardholder Dispute - Authorization-Related", Brand: BrandVisa, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"14.3": {Code: "14.3", Description: "Cardholder Dispute - Processing Error", Brand: BrandVisa, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"14.4": {Code: "14.4", Description: "Cardholder Dispute - Cardholder Dispute", Brand: BrandVisa, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"14.5": {Code: "14.5", Description: "Cardholder Dispute - Credit Processed as Charge", Brand: BrandVisa, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"14.6": {Code: "14.6", Description: "Cardholder Dispute - Incorrect Transaction Code", Brand: BrandVisa, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"14.7": {Code: "14.7", Description: "Cardholder Dispute - Duplicate Processing", Brand: BrandVisa, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"14.8": {Code: "14.8", Description: "Cardholder Dispute - Invalid Data", Brand: BrandVisa, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},

	// Mastercard Reason Codes
	"4807": {Code: "4807", Description: "Authorization - Warning Notice File", Brand: BrandMastercard, Category: "Authorization", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4808": {Code: "4808", Description: "Authorization - Authorization-Related", Brand: BrandMastercard, Category: "Authorization", Fraud: false, ChargebackFee: false, ValidFrom: "2019-04-15"},
	"4837": {Code: "4837", Description: "Cardholder Dispute - No Cardholder Authorization", Brand: BrandMastercard, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4840": {Code: "4840", Description: "Fraud - Fraudulent Processing of Transactions", Brand: BrandMastercard, Category: "Fraud", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4841": {Code: "4841", Description: "Fraud - Canceled Recurring or Digital Transaction", Brand: BrandMastercard, Category: "Fraud", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4842": {Code: "4842", Description: "Fraud - Late Presentation", Brand: BrandMastercard, Category: "Fraud", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4849": {Code: "4849", Description: "Cardholder Dispute - Credit Not Processed", Brand: BrandMastercard, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4850": {Code: "4850", Description: "Cardholder Dispute - Cardholder Dispute", Brand: BrandMastercard, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4851": {Code: "4851", Description: "Cardholder Dispute - Credit Processed as Charge", Brand: BrandMastercard, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4852": {Code: "4852", Description: "Cardholder Dispute - Goods or Services Not Provided", Brand: BrandMastercard, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4853": {Code: "4853", Description: "Cardholder Dispute - Cardholder Dispute", Brand: BrandMastercard, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4854": {Code: "4854", Description: "Cardholder Dispute - Cardholder Dispute", Brand: BrandMastercard, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4855": {Code: "4855", Description: "Cardholder Dispute - Goods or Services Returned", Brand: BrandMastercard, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4859": {Code: "4859", Description: "Cardholder Dispute - Addendum, No-Show, or ATM Dispute", Brand: BrandMastercard, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4860": {Code: "4860", Description: "Cardholder Dispute - Cardholder Dispute", Brand: BrandMastercard, Category: "Cardholder Dispute", Fraud: false, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4863": {Code: "4863", Description: "Cardholder Dispute - Cardholder Does Not Recognize", Brand: BrandMastercard, Category: "Cardholder Dispute", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4870": {Code: "4870", Description: "Chip Liability Shift - Chip Liability Shift", Brand: BrandMastercard, Category: "Chip Liability", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
	"4871": {Code: "4871", Description: "Chip Liability Shift - Chip Transaction Failure", Brand: BrandMastercard, Category: "Chip Liability", Fraud: true, ChargebackFee: true, ValidFrom: "2019-04-15"},
}

// ValidateReasonCode validates a dispute reason code
func ValidateReasonCode(code string) (*ReasonCode, error) {
	if code == "" {
		return nil, fmt.Errorf("reason code cannot be empty")
	}

	// Clean up the code (remove spaces, normalize format)
	cleanCode := strings.TrimSpace(code)
	cleanCode = strings.ToUpper(cleanCode)
	cleanCode = regexp.MustCompile(`\s+`).ReplaceAllString(cleanCode, " ")

	reasonCode, exists := ReasonCodes[cleanCode]
	if !exists {
		return nil, fmt.Errorf("invalid reason code: %s", code)
	}

	return &reasonCode, nil
}

// GetReasonCodesByBrand returns all reason codes for a specific brand
func GetReasonCodesByBrand(brand CardBrand) []ReasonCode {
	var codes []ReasonCode
	for _, code := range ReasonCodes {
		if code.Brand == brand {
			codes = append(codes, code)
		}
	}
	return codes
}

// GetFraudReasonCodes returns all fraud-related reason codes
func GetFraudReasonCodes() []ReasonCode {
	var codes []ReasonCode
	for _, code := range ReasonCodes {
		if code.Fraud {
			codes = append(codes, code)
		}
	}
	return codes
}

// GetNonFraudReasonCodes returns all non-fraud reason codes
func GetNonFraudReasonCodes() []ReasonCode {
	var codes []ReasonCode
	for _, code := range ReasonCodes {
		if !code.Fraud {
			codes = append(codes, code)
		}
	}
	return codes
}

// GetReasonCodesByCategory returns all reason codes for a specific category
func GetReasonCodesByCategory(category string) []ReasonCode {
	var codes []ReasonCode
	for _, code := range ReasonCodes {
		if code.Category == category {
			codes = append(codes, code)
		}
	}
	return codes
}

// MaskPII masks personally identifiable information in audit logs
func MaskPII(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}

	masked := make(map[string]interface{})
	for key, value := range data {
		switch key {
		case "card_number", "pan", "primary_account_number":
			if str, ok := value.(string); ok && len(str) > 4 {
				masked[key] = "****" + str[len(str)-4:]
			} else {
				masked[key] = "****"
			}
		case "cvv", "cvc", "security_code":
			masked[key] = "***"
		case "expiry_date", "expiration_date":
			if str, ok := value.(string); ok && len(str) > 4 {
				masked[key] = "**/" + str[len(str)-2:]
			} else {
				masked[key] = "**/**"
			}
		case "cardholder_name", "cardholder":
			if str, ok := value.(string); ok && len(str) > 0 {
				parts := strings.Fields(str)
				if len(parts) >= 2 {
					masked[key] = parts[0] + " " + strings.Repeat("*", len(str)-len(parts[0])-len(parts[len(parts)-1])-1) + " " + parts[len(parts)-1]
				} else {
					masked[key] = strings.Repeat("*", len(str))
				}
			} else {
				masked[key] = "********"
			}
		case "email":
			if str, ok := value.(string); ok {
				parts := strings.Split(str, "@")
				if len(parts) == 2 {
					localPart := parts[0]
					domain := parts[1]
					if len(localPart) > 0 {
						masked[key] = localPart[0:1] + "***@" + domain
					} else {
						masked[key] = "***@" + domain
					}
				} else {
					masked[key] = "***@***"
				}
			} else {
				masked[key] = "***@***"
			}
		case "phone", "phone_number":
			if str, ok := value.(string); ok {
				digits := regexp.MustCompile(`\d`).FindAllString(str, -1)
				if len(digits) >= 4 {
					masked[key] = "***-***-" + strings.Join(digits[len(digits)-4:], "")
				} else {
					masked[key] = "***-***-****"
				}
			} else {
				masked[key] = "***-***-****"
			}
		case "address", "billing_address", "shipping_address":
			if str, ok := value.(string); ok {
				// Basic address masking - show only city and last 3 digits of postal code
				parts := strings.Split(str, ",")
				if len(parts) >= 2 {
					city := strings.TrimSpace(parts[len(parts)-2])
					postalPart := strings.TrimSpace(parts[len(parts)-1])
					digits := regexp.MustCompile(`\d`).FindAllString(postalPart, -1)
					if len(digits) >= 3 {
						masked[key] = city + ", ***" + strings.Join(digits[len(digits)-3:], "")
					} else {
						masked[key] = city + ", ***"
					}
				} else {
					masked[key] = "***, ***"
				}
			} else {
				masked[key] = "***, ***"
			}
		case "ip_address":
			if str, ok := value.(string); ok {
				parts := strings.Split(str, ".")
				if len(parts) == 4 {
					masked[key] = parts[0] + "." + parts[1] + "." + parts[2] + ".*"
				} else {
					masked[key] = "***.***.***.***"
				}
			} else {
				masked[key] = "***.***.***.***"
			}
		case "device_id", "device_fingerprint":
			if str, ok := value.(string); ok && len(str) > 8 {
				masked[key] = str[:8] + "***"
			} else {
				masked[key] = "***"
			}
		case "session_id", "transaction_id":
			if str, ok := value.(string); ok && len(str) > 8 {
				masked[key] = str[:8] + "***"
			} else {
				masked[key] = "***"
			}
		default:
			// For other fields, include them as-is
			masked[key] = value
		}
	}
	return masked
}

// ValidateDisputeRequest validates a dispute request before state changes
func ValidateDisputeRequest(request DisputeValidationRequest) error {
	if request.DisputeID == "" {
		return fmt.Errorf("dispute ID is required")
	}

	if request.JournalEntryID == "" {
		return fmt.Errorf("journal entry ID is required")
	}

	if request.MerchantID == "" {
		return fmt.Errorf("merchant ID is required")
	}

	if request.OriginalAmount <= 0 {
		return fmt.Errorf("original amount must be positive")
	}

	if request.DisputedAmount <= 0 {
		return fmt.Errorf("disputed amount must be positive")
	}

	if request.DisputedAmount > request.OriginalAmount {
		return fmt.Errorf("disputed amount cannot exceed original amount")
	}

	if len(request.CurrencyCode) != 3 {
		return fmt.Errorf("currency code must be 3 characters")
	}

	if request.ReasonCode == "" {
		return fmt.Errorf("reason code is required")
	}

	if request.CreatedBy == "" {
		return fmt.Errorf("created_by is required")
	}

	// Validate reason code
	reasonCode, err := ValidateReasonCode(request.ReasonCode)
	if err != nil {
		return fmt.Errorf("invalid reason code: %w", err)
	}

	// Additional business rule validations based on reason code
	if reasonCode.Fraud {
		if request.RequiresFraudInvestigation == nil {
			return fmt.Errorf("fraud disputes require investigation flag")
		}
	}

	if reasonCode.Category == "Authorization" {
		if request.AuthorizationCode == "" {
			return fmt.Errorf("authorization disputes require authorization code")
		}
	}

	return nil
}

// DisputeValidationRequest represents a request to validate dispute data
type DisputeValidationRequest struct {
	DisputeID                string                 `json:"dispute_id"`
	JournalEntryID           string                 `json:"journal_entry_id"`
	MerchantID               string                 `json:"merchant_id"`
	OriginalAmount           float64                `json:"original_amount"`
	DisputedAmount           float64                `json:"disputed_amount"`
	CurrencyCode             string                 `json:"currency_code"`
	ReasonCode               string                 `json:"reason_code"`
	ReasonText               string                 `json:"reason_text"`
	AuthorizationCode        string                 `json:"authorization_code"`
	RequiresFraudInvestigation *bool                 `json:"requires_fraud_investigation"`
	ReferenceType            string                 `json:"reference_type"`
	ReferenceID              string                 `json:"reference_id"`
	CreatedBy                string                 `json:"created_by"`
	Metadata                 map[string]interface{} `json:"metadata"`
}