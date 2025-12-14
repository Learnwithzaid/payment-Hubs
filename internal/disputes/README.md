# Dispute Workflow Module

This document describes the dispute workflow module implementation, which provides comprehensive dispute/chargeback logic atop the ledger system with ACID safety, immutable audit trails, and compliance with card network regulations.

## Overview

The dispute workflow module implements a complete dispute management system with the following key features:

- **State Machine**: Enforces valid dispute state transitions with immutable audit trail
- **ACID Safety**: All dispute operations are atomic and maintain database consistency  
- **Ledger Integration**: Automatically applies holds and fraud reserves to merchant accounts
- **Card Network Compliance**: Supports Visa and Mastercard reason codes with proper validation
- **PII Protection**: Masks sensitive information in audit logs
- **API Endpoints**: RESTful API for all dispute operations with compliance-only access control

## Architecture

### Core Components

1. **State Machine** (`state_machine.go`)
   - Manages dispute state transitions: PENDING → AUTHORIZED → SETTLED → DISPUTED → REVERSED
   - Enforces guard rails against invalid sequences
   - Creates hash-chained journal entries for immutable audit trail
   - Validates operations for each state

2. **Disputes Service** (`service.go`)
   - Main business logic for dispute operations
   - Atomic ledger operations for holds and reserves
   - Integration with existing ledger system
   - Transaction safety with proper locking

3. **Reason Codes** (`reasons.go`)
   - Visa and Mastercard reason codes validation
   - Fraud vs non-fraud classification
   - PII masking for audit logs

4. **Database Schema** (`migrations/020_disputes.sql`, `migrations/021_dispute_transitions.sql`)
   - Immutable dispute records with cryptographic hashing
   - Holds table for fund reservations
   - Fraud reserves tracking
   - State transition audit trail

5. **API Layer** (`api/proto/disputes.proto`, `internal/api/handlers.go`)
   - RESTful endpoints for all dispute operations
   - Compliance-only access control
   - Integration with audit logger

## State Machine

### Valid State Transitions

```
PENDING → AUTHORIZED | REVERSED
AUTHORIZED → SETTLED | REVERSED  
SETTLED → DISPUTED | REVERSED
DISPUTED → REVERSED
REVERSED → [TERMINAL]
```

### Operations by State

| State | Allowed Operations | Description |
|-------|-------------------|-------------|
| PENDING | authorize, reverse | Initial creation, awaiting authorization |
| AUTHORIZED | settle, reverse | Authorized and pending settlement |
| SETTLED | dispute, reverse | Transaction settled, eligible for dispute |
| DISPUTED | reverse | Under formal dispute/chargeback process |
| REVERSED | [none] | Terminal state - dispute resolved |

### Immutable Audit Trail

Each state transition creates a hash-chained record:

```go
type StateTransition struct {
    ID                string       // UUID
    DisputeID         string       // Reference to dispute
    FromState         DisputeState // Previous state
    ToState           DisputeState // New state  
    Reason            string       // Transition reason
    TransitionHash    string       // Cryptographic hash
    PrevHash          string       // Previous transition hash
    CreatedAt         time.Time    // Timestamp
    CreatedBy         string       // User who performed action
    Metadata          map[string]interface{} // Additional data
}
```

Hash chain ensures integrity: `transition_hash = SHA256(dispute_id + from_state + to_state + reason + created_at + created_by + prev_hash)`

## Database Schema

### Disputes Table

```sql
CREATE TABLE disputes (
    id UUID PRIMARY KEY,
    dispute_id TEXT UNIQUE NOT NULL,
    journal_entry_id UUID REFERENCES journal_entries(id),
    merchant_id UUID NOT NULL,
    original_amount NUMERIC(20,8) NOT NULL,
    disputed_amount NUMERIC(20,8) NOT NULL,
    currency_code TEXT NOT NULL,
    reason_code TEXT NOT NULL,
    reason_text TEXT NOT NULL,
    status TEXT NOT NULL, -- PENDING, AUTHORIZED, SETTLED, DISPUTED, REVERSED
    is_fraud BOOLEAN NOT NULL,
    chargeback_fee NUMERIC(20,8) NOT NULL DEFAULT 0,
    dispute_hash TEXT NOT NULL, -- Cryptographic hash
    prev_dispute_hash TEXT NOT NULL, -- Chain linkage
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    resolved_at TIMESTAMP,
    resolved_by TEXT
);
```

### Holds Table

```sql  
CREATE TABLE holds (
    id UUID PRIMARY KEY,
    hold_id TEXT UNIQUE NOT NULL,
    dispute_id UUID REFERENCES disputes(id),
    account_id UUID REFERENCES accounts(id),
    held_amount NUMERIC(20,8) NOT NULL,
    currency_code TEXT NOT NULL,
    status TEXT NOT NULL, -- ACTIVE, RELEASED, CONVERTED
    expires_at TIMESTAMP NOT NULL,
    hold_hash TEXT NOT NULL, -- Cryptographic hash
    prev_hold_hash TEXT NOT NULL, -- Chain linkage
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    released_at TIMESTAMP,
    released_by TEXT
);
```

### Fraud Reserves Table

```sql
CREATE TABLE fraud_reserves (
    id UUID PRIMARY KEY,
    merchant_id UUID NOT NULL UNIQUE,
    reserve_account_id UUID REFERENCES accounts(id),
    reserve_percentage NUMERIC(5,4) NOT NULL, -- 0.0000 to 1.0000
    minimum_reserve_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    current_reserve_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    currency_code TEXT NOT NULL DEFAULT 'USD',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_by TEXT NOT NULL
);
```

### Dispute Transitions Table

```sql
CREATE TABLE dispute_transitions (
    id UUID PRIMARY KEY,
    dispute_id TEXT NOT NULL,
    from_state TEXT, -- Previous state
    to_state TEXT NOT NULL, -- New state
    reason TEXT NOT NULL,
    transition_hash TEXT NOT NULL, -- Cryptographic hash
    prev_hash TEXT NOT NULL, -- Previous transition hash
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL
);
```

## API Endpoints

### Authentication

All dispute endpoints require compliance-only OAuth scopes:
- `disputes:write` - Create disputes, authorize, settle, initiate, reverse
- `disputes:read` - View disputes, history, calculate reserves

### Create Dispute

```http
POST /v1/disputes
Content-Type: application/json

{
  "journal_entry_id": "uuid-of-journal-entry",
  "merchant_id": "uuid-of-merchant",
  "disputed_amount": 100.00,
  "currency_code": "USD",
  "reason_code": "14.1",
  "reason_text": "Cardholder does not recognize transaction",
  "reference_type": "transaction",
  "reference_id": "txn-123",
  "created_by": "compliance-officer",
  "metadata": {
    "card_last_four": "1234"
  }
}
```

### Authorize Dispute

```http
POST /v1/disputes/{dispute_id}/authorize
Content-Type: application/json

{
  "authorized_by": "compliance-officer"
}
```

### Settle Transaction

```http
POST /v1/disputes/settle
Content-Type: application/json

{
  "journal_entry_id": "uuid-of-journal-entry",
  "settled_by": "settlement-system"
}
```

### Initiate Dispute

```http
POST /v1/disputes/{dispute_id}/dispute
Content-Type: application/json

{
  "initiated_by": "dispute-processor"
}
```

### Reverse Dispute

```http
POST /v1/disputes/{dispute_id}/reverse
Content-Type: application/json

{
  "reversed_by": "compliance-manager",
  "reason": "Evidence provided by merchant"
}
```

### Get Dispute

```http
GET /v1/disputes/{dispute_id}
```

### List Disputes

```http
GET /v1/disputes?merchant_id={merchant_id}&status={status}&is_fraud=true&limit=10&offset=0
```

### Get Dispute History

```http
GET /v1/disputes/{dispute_id}/history
```

### Calculate Reserve

```http
GET /v1/disputes/reserve/calculate?merchant_id={merchant_id}&transaction_volume={volume}¤cy_code=USD
```

## Reason Codes

The module supports Visa and Mastercard reason codes with proper classification:

### Visa Codes
- `10.1` - Authorization - Cardholder Dispute (Non-fraud)
- `11.1` - Card Recovery Bulletin (Fraud)
- `12.1` - Counterfeit Transaction (Fraud)
- `13.1` - Non-Counterfeit Fraud - Card-Present (Fraud)
- `14.1` - Cardholder Dispute - Fraud (Fraud)
- `14.2` - Cardholder Dispute - Authorization-Related (Non-fraud)
- `14.3` - Cardholder Dispute - Processing Error (Non-fraud)

### Mastercard Codes
- `4807` - Authorization - Warning Notice File (Non-fraud)
- `4837` - Cardholder Dispute - No Cardholder Authorization (Non-fraud)
- `4840` - Fraud - Fraudulent Processing of Transactions (Fraud)
- `4853` - Cardholder Dispute - Cardholder Dispute (Non-fraud)
- `4859` - Cardholder Dispute - Addendum, No-Show, ATM (Non-fraud)
- `4863` - Cardholder Dispute - Cardholder Does Not Recognize (Fraud)

### PII Masking

All PII is automatically masked in audit logs:

```go
// Input
{
  "card_number": "4111111111111111",
  "cvv": "123", 
  "email": "user@example.com",
  "phone": "555-123-4567"
}

// Masked Output
{
  "card_number": "****1111",
  "cvv": "***",
  "email": "u***@example.com", 
  "phone": "***-***-4567"
}
```

## Usage Examples

### Complete Dispute Workflow

```go
// Initialize service
disputesService := disputes.NewDisputesService(pool, ledger, 0.05) // 5% reserve

// 1. Create dispute
dispute, err := disputesService.CreateDispute(ctx, disputes.CreateDisputeRequest{
    JournalEntryID:  "journal-entry-uuid",
    MerchantID:      "merchant-uuid", 
    DisputedAmount:  100.00,
    CurrencyCode:    "USD",
    ReasonCode:      "14.1",
    ReasonText:      "Cardholder does not recognize transaction",
    CreatedBy:       "compliance-officer",
})

// 2. Authorize dispute
err = disputesService.AuthorizeDispute(ctx, dispute.DisputeID, "compliance-officer")

// 3. Settle transaction (when ready)
err = disputesService.SettleTransaction(ctx, journalEntryID, "settlement-system")

// 4. Initiate dispute (chargeback process)
err = disputesService.InitiateDispute(ctx, dispute.DisputeID, "dispute-processor")

// 5. Eventually reverse dispute
err = disputesService.ReverseDispute(ctx, dispute.DisputeID, "compliance-manager", "Evidence provided")

// Check dispute state
currentState, err := stateMachine.GetCurrentState(ctx, dispute.DisputeID)
```

### Concurrent Dispute Handling

```go
// Multiple disputes can be created concurrently
var wg sync.WaitGroup
for i := 0; i < 5; i++ {
    wg.Add(1)
    go func(index int) {
        defer wg.Done()
        
        req := disputes.CreateDisputeRequest{
            JournalEntryID:  "shared-journal-entry-uuid",
            MerchantID:      "merchant-uuid",
            DisputedAmount:  50.0 + float64(index)*10.0,
            CurrencyCode:    "USD", 
            ReasonCode:      "14.1",
            ReasonText:      "Concurrent dispute",
            CreatedBy:       fmt.Sprintf("user-%d", index),
        }
        
        dispute, err := disputesService.CreateDispute(ctx, req)
        if err != nil {
            log.Printf("Dispute %d failed: %v", index, err)
            return
        }
        
        log.Printf("Created dispute: %s", dispute.DisputeID)
    }(i)
}
wg.Wait()
```

### Reserve Calculation

```go
// Calculate required fraud reserve for merchant
requiredReserve, err := disputesService.CalculateMerchantReserve(ctx, "merchant-uuid", 10000.0)
if err != nil {
    log.Fatalf("Reserve calculation failed: %v", err)
}

log.Printf("Required reserve: %.2f", requiredReserve)
```

## Security & Compliance

### ACID Safety
- All dispute operations use database transactions
- Proper row locking to prevent race conditions
- Atomic state transitions with rollback on failure

### Audit Trail
- Immutable records with cryptographic hashing
- Hash chain prevents tampering
- Complete state history preserved

### Access Control
- Compliance-only OAuth scopes required
- All actions logged with user attribution
- PII automatically masked in logs

### Data Validation
- Reason code validation against card network specifications
- State transition validation with guard rails
- Amount validation (disputed ≤ original)
- Currency code validation (ISO 4217)

## Error Handling

The module provides detailed error types:

```go
// Invalid state transition
type InvalidStateTransitionError struct {
    FromState DisputeState
    ToState   DisputeState
    DisputeID string
}

// Invalid operation for current state  
type InvalidOperationError struct {
    State     DisputeState
    Operation string
    DisputeID string
}
```

### Common Error Scenarios

1. **Invalid Transition**: Attempting PENDING → SETTLED
2. **Invalid State**: Authorizing already settled dispute  
3. **Unauthorized Role**: User lacks compliance scope
4. **Invalid Reason Code**: Unknown or expired reason code
5. **Amount Exceeded**: Disputed amount > original amount
6. **Contradictory State**: Concurrent state changes

## Testing

The module includes comprehensive unit and integration tests:

```bash
# Run unit tests
go test ./internal/disputes -v

# Run with coverage
go test ./internal/disputes -cover

# Run integration tests  
go test ./internal/disputes -run IntegrationTest -v

# Run benchmarks
go test ./internal/disputes -bench=.
```

### Test Coverage

- State machine validation and transitions
- Reason code validation and classification  
- PII masking functionality
- ACID compliance for concurrent operations
- Complete workflow integration tests
- Performance benchmarks

## Performance Considerations

### Optimizations
- Database indices on dispute_id, merchant_id, status, created_at
- Efficient hash chain validation
- Batch reserve calculations
- Connection pooling for high concurrency

### Monitoring
- State transition timing
- Reserve calculation performance  
- Dispute creation throughput
- Database query performance

### Scalability
- Horizontal scaling via database partitioning
- Read replicas for dispute history queries
- Caching for reason code validation
- Async processing for large volumes

## Configuration

### Service Configuration

```go
type DisputesServiceConfig struct {
    ReservePercentage    float64 // Default: 5% (0.05)
    MaxConcurrentDisputes int     // Default: 100
    HoldDurationDays     int      // Default: 30
    ChargebackFeeConfig  ChargebackFeeConfig
}
```

### Database Configuration

```sql
-- Indexes for performance
CREATE INDEX idx_disputes_merchant_status ON disputes(merchant_id, status);
CREATE INDEX idx_disputes_created_at ON disputes(created_at);
CREATE INDEX idx_holds_dispute_status ON holds(dispute_id, status);
CREATE INDEX idx_fraud_reserves_merchant ON fraud_reserves(merchant_id);

-- Partitioning for large volumes (optional)
CREATE TABLE disputes_y2024m01 PARTITION OF disputes 
FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
```

## Integration with Existing Systems

### Ledger Integration
- Automatic journal entries for holds and reserves
- Balance validation before holds applied
- Integration with existing account management

### Audit System
- All operations logged via audit middleware
- Correlation IDs for tracing
- PII masking in all audit records

### Authentication/Authorization  
- OAuth scopes for access control
- JWT validation for all endpoints
- Role-based permissions for operations

### Monitoring & Alerting
- State transition metrics
- Fraud rate monitoring
- Reserve requirement tracking
- API response time monitoring

This dispute workflow module provides enterprise-grade dispute management capabilities with strong security, compliance, and audit requirements suitable for payment processing environments.