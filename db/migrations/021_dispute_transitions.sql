-- Migration 021: Create dispute_transitions table for immutable state history
-- This table maintains the hash-chained journal entries for dispute state transitions

BEGIN TRANSACTION;

-- Create dispute_transitions table (immutable - no UPDATE/DELETE)
CREATE TABLE IF NOT EXISTS dispute_transitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dispute_id TEXT NOT NULL,
    from_state TEXT CHECK (from_state IN ('PENDING', 'AUTHORIZED', 'SETTLED', 'DISPUTED', 'REVERSED')),
    to_state TEXT NOT NULL CHECK (to_state IN ('PENDING', 'AUTHORIZED', 'SETTLED', 'DISPUTED', 'REVERSED')),
    reason TEXT NOT NULL,
    transition_hash TEXT NOT NULL,
    prev_hash TEXT NOT NULL,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    
    -- Constraints
    CONSTRAINT dispute_transitions_reason_chk CHECK (length(reason) > 0),
    CONSTRAINT dispute_transitions_hash_chk CHECK (length(transition_hash) > 0),
    CONSTRAINT dispute_transitions_prev_hash_chk CHECK (length(prev_hash) > 0)
);

-- Create indices for efficient queries
CREATE INDEX idx_dispute_transitions_dispute ON dispute_transitions(dispute_id);
CREATE INDEX idx_dispute_transitions_from_state ON dispute_transitions(from_state);
CREATE INDEX idx_dispute_transitions_to_state ON dispute_transitions(to_state);
CREATE INDEX idx_dispute_transitions_created_at ON dispute_transitions(created_at);
CREATE INDEX idx_dispute_transitions_hash ON dispute_transitions(transition_hash);
CREATE INDEX idx_dispute_transitions_prev_hash ON dispute_transitions(prev_hash);

-- Function to calculate transition hash (for immutable audit trail)
CREATE OR REPLACE FUNCTION calculate_transition_hash()
RETURNS TRIGGER AS $$
DECLARE
    hash_input TEXT;
    prev_hash_val TEXT;
BEGIN
    -- Get previous transition hash for this dispute
    SELECT transition_hash INTO prev_hash_val
    FROM dispute_transitions
    WHERE dispute_id = NEW.dispute_id
    ORDER BY created_at DESC, id DESC
    LIMIT 1;
    
    -- Use empty string if no previous hash
    IF prev_hash_val IS NULL THEN
        prev_hash_val := '';
    END IF;
    
    hash_input := COALESCE(NEW.dispute_id, '') ||
                  COALESCE(NEW.from_state, '') ||
                  COALESCE(NEW.to_state, '') ||
                  NEW.reason ||
                  NEW.created_at::TEXT ||
                  NEW.created_by ||
                  prev_hash_val;
    
    NEW.transition_hash := encode(digest(hash_input, 'sha256'), 'hex');
    NEW.prev_hash := prev_hash_val;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger for transition hash calculation
DROP TRIGGER IF EXISTS trigger_calculate_transition_hash ON dispute_transitions;
CREATE TRIGGER trigger_calculate_transition_hash
    BEFORE INSERT ON dispute_transitions
    FOR EACH ROW
    EXECUTE FUNCTION calculate_transition_hash();

-- Function to validate transition sequence
CREATE OR REPLACE FUNCTION validate_transition_sequence()
RETURNS TRIGGER AS $$
DECLARE
    latest_state TEXT;
    valid BOOLEAN := FALSE;
BEGIN
    -- Get the latest state for this dispute
    SELECT to_state INTO latest_state
    FROM dispute_transitions
    WHERE dispute_id = NEW.dispute_id AND id != NEW.id
    ORDER BY created_at DESC, id DESC
    LIMIT 1;
    
    -- If no previous transition, starting state should be empty or PENDING
    IF latest_state IS NULL THEN
        valid := (NEW.from_state IS NULL OR NEW.from_state = '' OR NEW.from_state = 'PENDING');
    ELSE
        -- Validate the transition sequence
        CASE latest_state
            WHEN 'PENDING' THEN
                valid := NEW.to_state IN ('AUTHORIZED', 'REVERSED');
            WHEN 'AUTHORIZED' THEN
                valid := NEW.to_state IN ('SETTLED', 'REVERSED');
            WHEN 'SETTLED' THEN
                valid := NEW.to_state IN ('DISPUTED', 'REVERSED');
            WHEN 'DISPUTED' THEN
                valid := NEW.to_state = 'REVERSED';
            WHEN 'REVERSED' THEN
                valid := FALSE; -- Terminal state
            ELSE
                valid := FALSE;
        END CASE;
    END IF;
    
    IF NOT valid THEN
        RAISE EXCEPTION 'Invalid state transition from % to % for dispute %s',
            latest_state, NEW.to_state, NEW.dispute_id;
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger for transition sequence validation
DROP TRIGGER IF EXISTS trigger_validate_transition_sequence ON dispute_transitions;
CREATE TRIGGER trigger_validate_transition_sequence
    BEFORE INSERT ON dispute_transitions
    FOR EACH ROW
    EXECUTE FUNCTION validate_transition_sequence();

COMMIT;