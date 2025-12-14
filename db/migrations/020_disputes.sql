-- Migration 020: Create dispute workflow tables with immutable history
-- Implements dispute/chargeback logic atop the ledger with proper audit trails

BEGIN TRANSACTION;

-- Create disputes table (immutable - no UPDATE/DELETE)
CREATE TABLE IF NOT EXISTS disputes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dispute_id TEXT UNIQUE NOT NULL,
    journal_entry_id UUID NOT NULL REFERENCES journal_entries(id) ON DELETE RESTRICT,
    merchant_id UUID NOT NULL,
    original_amount NUMERIC(20, 8) NOT NULL CHECK (original_amount > 0),
    disputed_amount NUMERIC(20, 8) NOT NULL CHECK (disputed_amount > 0),
    currency_code TEXT NOT NULL DEFAULT 'USD' CHECK (length(currency_code) = 3),
    reason_code TEXT NOT NULL,
    reason_text TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'AUTHORIZED', 'SETTLED', 'DISPUTED', 'REVERSED')),
    is_fraud BOOLEAN NOT NULL DEFAULT FALSE,
    chargeback_fee NUMERIC(20, 8) NOT NULL DEFAULT 0 CHECK (chargeback_fee >= 0),
    prev_dispute_hash TEXT NOT NULL,
    dispute_hash TEXT NOT NULL,
    reference_type TEXT,
    reference_id TEXT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    resolved_at TIMESTAMP,
    resolved_by TEXT,
    
    -- Constraints
    CONSTRAINT disputes_disputed_amount_chk CHECK (disputed_amount <= original_amount),
    CONSTRAINT disputes_chargeback_fee_chk CHECK (chargeback_fee >= 0)
);

-- Create holds table to track funds held due to disputes
CREATE TABLE IF NOT EXISTS holds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hold_id TEXT UNIQUE NOT NULL,
    dispute_id UUID NOT NULL REFERENCES disputes(id) ON DELETE RESTRICT,
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    held_amount NUMERIC(20, 8) NOT NULL CHECK (held_amount > 0),
    currency_code TEXT NOT NULL DEFAULT 'USD' CHECK (length(currency_code) = 3),
    status TEXT NOT NULL CHECK (status IN ('ACTIVE', 'RELEASED', 'CONVERTED')),
    expires_at TIMESTAMP NOT NULL,
    prev_hold_hash TEXT NOT NULL,
    hold_hash TEXT NOT NULL,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    released_at TIMESTAMP,
    released_by TEXT,
    
    -- Constraints
    CONSTRAINT holds_amount_chk CHECK (held_amount > 0),
    CONSTRAINT holds_expiry_chk CHECK (expires_at > created_at)
);

-- Create fraud_reserves table to track merchant-specific fraud reserves
CREATE TABLE IF NOT EXISTS fraud_reserves (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL,
    reserve_account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    reserve_percentage NUMERIC(5, 4) NOT NULL CHECK (reserve_percentage >= 0 AND reserve_percentage <= 1.0000),
    minimum_reserve_amount NUMERIC(20, 8) NOT NULL DEFAULT 0 CHECK (minimum_reserve_amount >= 0),
    current_reserve_amount NUMERIC(20, 8) NOT NULL DEFAULT 0 CHECK (current_reserve_amount >= 0),
    currency_code TEXT NOT NULL DEFAULT 'USD' CHECK (length(currency_code) = 3),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_by TEXT NOT NULL,
    
    -- Constraints
    CONSTRAINT fraud_reserves_percentage_chk CHECK (reserve_percentage >= 0 AND reserve_percentage <= 1.0000),
    CONSTRAINT fraud_reserves_min_amount_chk CHECK (minimum_reserve_amount >= 0),
    CONSTRAINT fraud_reserves_current_amount_chk CHECK (current_reserve_amount >= 0),
    
    UNIQUE(merchant_id)
);

-- Create indices for efficient queries
CREATE INDEX idx_disputes_merchant ON disputes(merchant_id);
CREATE INDEX idx_disputes_status ON disputes(status);
CREATE INDEX idx_disputes_journal_entry ON disputes(journal_entry_id);
CREATE INDEX idx_disputes_created_at ON disputes(created_at);
CREATE INDEX idx_disputes_reference ON disputes(reference_type, reference_id);
CREATE INDEX idx_disputes_hash ON disputes(dispute_hash);
CREATE INDEX idx_disputes_prev_hash ON disputes(prev_dispute_hash);

CREATE INDEX idx_holds_dispute ON holds(dispute_id);
CREATE INDEX idx_holds_account ON holds(account_id);
CREATE INDEX idx_holds_status ON holds(status);
CREATE INDEX idx_holds_expires_at ON holds(expires_at);
CREATE INDEX idx_holds_hash ON holds(hold_hash);
CREATE INDEX idx_holds_prev_hash ON holds(prev_hold_hash);

CREATE INDEX idx_fraud_reserves_merchant ON fraud_reserves(merchant_id);
CREATE INDEX idx_fraud_reserves_active ON fraud_reserves(is_active);

-- Function to calculate hash for dispute (for immutable audit trail)
CREATE OR REPLACE FUNCTION calculate_dispute_hash()
RETURNS TRIGGER AS $$
DECLARE
    hash_input TEXT;
BEGIN
    hash_input := COALESCE(NEW.dispute_id, '') ||
                  COALESCE(NEW.journal_entry_id::TEXT, '') ||
                  COALESCE(NEW.merchant_id::TEXT, '') ||
                  NEW.original_amount::TEXT ||
                  NEW.disputed_amount::TEXT ||
                  NEW.currency_code ||
                  NEW.reason_code ||
                  NEW.reason_text ||
                  NEW.status ||
                  NEW.created_at::TEXT ||
                  COALESCE(NEW.prev_dispute_hash, '');
    
    NEW.dispute_hash := encode(digest(hash_input, 'sha256'), 'hex');
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Function to calculate hash for hold (for immutable audit trail)
CREATE OR REPLACE FUNCTION calculate_hold_hash()
RETURNS TRIGGER AS $$
DECLARE
    hash_input TEXT;
BEGIN
    hash_input := COALESCE(NEW.hold_id, '') ||
                  COALESCE(NEW.dispute_id::TEXT, '') ||
                  COALESCE(NEW.account_id::TEXT, '') ||
                  NEW.held_amount::TEXT ||
                  NEW.currency_code ||
                  NEW.status ||
                  NEW.expires_at::TEXT ||
                  NEW.created_at::TEXT ||
                  COALESCE(NEW.prev_hold_hash, '');
    
    NEW.hold_hash := encode(digest(hash_input, 'sha256'), 'hex');
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create triggers for hash calculation
DROP TRIGGER IF EXISTS trigger_calculate_dispute_hash ON disputes;
CREATE TRIGGER trigger_calculate_dispute_hash
    BEFORE INSERT ON disputes
    FOR EACH ROW
    EXECUTE FUNCTION calculate_dispute_hash();

DROP TRIGGER IF EXISTS trigger_calculate_hold_hash ON holds;
CREATE TRIGGER trigger_calculate_hold_hash
    BEFORE INSERT ON holds
    FOR EACH ROW
    EXECUTE FUNCTION calculate_hold_hash();

-- Function to validate dispute state transitions
CREATE OR REPLACE FUNCTION validate_dispute_transition()
RETURNS TRIGGER AS $$
DECLARE
    current_status TEXT;
BEGIN
    -- Get current status if updating
    IF TG_OP = 'UPDATE' THEN
        SELECT status INTO current_status
        FROM disputes
        WHERE id = NEW.id;
        
        -- Validate state transition
        IF NOT is_valid_dispute_transition(current_status, NEW.status) THEN
            RAISE EXCEPTION 'Invalid dispute state transition from % to % for dispute %s',
                current_status, NEW.status, NEW.dispute_id;
        END IF;
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Function to check if state transition is valid
CREATE OR REPLACE FUNCTION is_valid_dispute_transition(from_status TEXT, to_status TEXT)
RETURNS BOOLEAN AS $$
BEGIN
    -- Define valid transitions
    CASE from_status
        WHEN 'PENDING' THEN
            RETURN to_status IN ('AUTHORIZED', 'REVERSED');
        WHEN 'AUTHORIZED' THEN
            RETURN to_status IN ('SETTLED', 'REVERSED');
        WHEN 'SETTLED' THEN
            RETURN to_status IN ('DISPUTED', 'REVERSED');
        WHEN 'DISPUTED' THEN
            RETURN to_status IN ('REVERSED');
        ELSE
            RETURN FALSE;
    END CASE;
END;
$$ LANGUAGE plpgsql;

-- Create trigger for state transition validation
DROP TRIGGER IF EXISTS trigger_validate_dispute_transition ON disputes;
CREATE TRIGGER trigger_validate_dispute_transition
    BEFORE UPDATE ON disputes
    FOR EACH ROW
    EXECUTE FUNCTION validate_dispute_transition();

-- Function to automatically release holds when dispute is resolved
CREATE OR REPLACE FUNCTION handle_dispute_resolution()
RETURNS TRIGGER AS $$
DECLARE
    hold_record RECORD;
BEGIN
    -- If dispute is being resolved (REVERSED), release associated holds
    IF NEW.status = 'REVERSED' AND OLD.status != 'REVERSED' THEN
        FOR hold_record IN 
            SELECT * FROM holds 
            WHERE dispute_id = NEW.id AND status = 'ACTIVE'
        LOOP
            -- Release the hold by updating its status
            UPDATE holds 
            SET status = 'RELEASED', 
                released_at = CURRENT_TIMESTAMP, 
                released_by = NEW.resolved_by
            WHERE id = hold_record.id;
            
            -- Post journal entry to release the held funds
            INSERT INTO journal_entries (
                entry_number, transaction_id, entry_type, account_id, account_type,
                amount, description, reference_type, reference_id, currency_code,
                created_by, metadata
            ) VALUES (
                'JE-HOLD-RELEASE-' || hold_record.hold_id,
                gen_random_uuid(),
                'credit',
                hold_record.account_id,
                (SELECT account_type FROM accounts WHERE id = hold_record.account_id),
                hold_record.held_amount,
                'Hold released - dispute reversed: ' || NEW.dispute_id,
                'dispute',
                NEW.dispute_id,
                hold_record.currency_code,
                NEW.resolved_by,
                jsonb_build_object('dispute_id', NEW.id, 'hold_id', hold_record.id)
            );
        END LOOP;
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger for dispute resolution handling
DROP TRIGGER IF EXISTS trigger_handle_dispute_resolution ON disputes;
CREATE TRIGGER trigger_handle_dispute_resolution
    AFTER UPDATE ON disputes
    FOR EACH ROW
    EXECUTE FUNCTION handle_dispute_resolution();

-- Function to check if account has sufficient available balance (excluding holds)
CREATE OR REPLACE FUNCTION check_available_balance(account_uuid UUID, required_amount NUMERIC)
RETURNS BOOLEAN AS $$
DECLARE
    current_balance NUMERIC;
    held_amount NUMERIC;
BEGIN
    -- Get current account balance
    SELECT COALESCE(balance, 0) INTO current_balance
    FROM account_balances
    WHERE account_id = account_uuid;
    
    -- Get total held amount for this account
    SELECT COALESCE(SUM(held_amount), 0) INTO held_amount
    FROM holds
    WHERE account_id = account_uuid AND status = 'ACTIVE';
    
    -- Check if available balance is sufficient
    RETURN (current_balance - held_amount) >= required_amount;
END;
$$ LANGUAGE plpgsql;

-- Function to create hold with proper double-entry accounting
CREATE OR REPLACE FUNCTION create_hold_with_entries(
    p_hold_id TEXT,
    p_dispute_id UUID,
    p_account_id UUID,
    p_held_amount NUMERIC,
    p_currency_code TEXT,
    p_expires_at TIMESTAMP,
    p_created_by TEXT
) RETURNS VOID AS $$
DECLARE
    transaction_id UUID := gen_random_uuid();
    account_type_val TEXT;
BEGIN
    -- Get account type
    SELECT account_type INTO account_type_val
    FROM accounts
    WHERE id = p_account_id;
    
    -- Insert hold record
    INSERT INTO holds (
        hold_id, dispute_id, account_id, held_amount, currency_code,
        status, expires_at, created_by
    ) VALUES (
        p_hold_id, p_dispute_id, p_account_id, p_held_amount, p_currency_code,
        'ACTIVE', p_expires_at, p_created_by
    );
    
    -- Post journal entry to hold funds
    INSERT INTO journal_entries (
        entry_number, transaction_id, entry_type, account_id, account_type,
        amount, description, reference_type, reference_id, currency_code,
        created_by, metadata
    ) VALUES (
        'JE-HOLD-' || p_hold_id,
        transaction_id,
        'debit',
        p_account_id,
        account_type_val,
        p_held_amount,
        'Funds held for dispute: ' || p_dispute_id,
        'dispute',
        p_dispute_id,
        p_currency_code,
        p_created_by,
        jsonb_build_object('hold_id', p_hold_id, 'dispute_id', p_dispute_id)
    );
END;
$$ LANGUAGE plpgsql;

-- Create view for dispute summary with balance information
CREATE OR REPLACE VIEW dispute_summary AS
SELECT 
    d.id,
    d.dispute_id,
    d.journal_entry_id,
    d.merchant_id,
    d.original_amount,
    d.disputed_amount,
    d.disputed_amount - d.chargeback_fee as net_disputed_amount,
    d.currency_code,
    d.reason_code,
    d.reason_text,
    d.status,
    d.is_fraud,
    d.chargeback_fee,
    d.created_at,
    d.resolved_at,
    COUNT(h.id) as hold_count,
    COALESCE(SUM(h.held_amount), 0) as total_held_amount,
    CASE 
        WHEN h.status = 'ACTIVE' THEN SUM(h.held_amount)
        ELSE 0 
    END as active_hold_amount
FROM disputes d
LEFT JOIN holds h ON d.id = h.dispute_id
GROUP BY d.id, d.dispute_id, d.journal_entry_id, d.merchant_id,
         d.original_amount, d.disputed_amount, d.currency_code,
         d.reason_code, d.reason_text, d.status, d.is_fraud,
         d.chargeback_fee, d.created_at, d.resolved_at;

-- Create view for merchant reserve summary
CREATE OR REPLACE VIEW merchant_reserve_summary AS
SELECT 
    fr.merchant_id,
    fr.reserve_account_id,
    fr.reserve_percentage,
    fr.minimum_reserve_amount,
    fr.current_reserve_amount,
    fr.currency_code,
    fr.is_active,
    (SELECT COUNT(*) FROM disputes d WHERE d.merchant_id = fr.merchant_id AND d.status = 'DISPUTED') as active_disputes,
    (SELECT COALESCE(SUM(d.disputed_amount), 0) FROM disputes d WHERE d.merchant_id = fr.merchant_id AND d.status = 'DISPUTED') as total_disputed_amount
FROM fraud_reserves fr;

COMMIT;