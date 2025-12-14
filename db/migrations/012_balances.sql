-- Migration 012: Create balance snapshots table for immutable double-entry ledger
-- Provides historical balance tracking for auditing and reporting

BEGIN TRANSACTION;

-- Create balance_snapshots table (immutable - only INSERTs)
CREATE TABLE IF NOT EXISTS balance_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    transaction_id UUID NOT NULL,
    snapshot_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    balance_before NUMERIC(20, 8) NOT NULL,
    balance_after NUMERIC(20, 8) NOT NULL,
    balance_change NUMERIC(20, 8) NOT NULL,
    account_type TEXT NOT NULL CHECK (account_type IN ('asset', 'liability', 'equity', 'revenue', 'expense')),
    currency_code TEXT NOT NULL DEFAULT 'USD' CHECK (length(currency_code) = 3),
    entry_id UUID NOT NULL REFERENCES journal_entries(id) ON DELETE RESTRICT,
    entry_type TEXT NOT NULL CHECK (entry_type IN ('debit', 'credit')),
    amount NUMERIC(20, 8) NOT NULL,
    description TEXT NOT NULL,
    reference_type TEXT,
    reference_id TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Constraints
    CONSTRAINT balance_snapshots_balance_change_chk CHECK (balance_after = balance_before + balance_change),
    CONSTRAINT balance_snapshots_amount_chk CHECK (amount > 0),
    CONSTRAINT balance_snapshots_description_chk CHECK (length(description) > 0)
);

-- Create indices for efficient historical queries
CREATE INDEX idx_balance_snapshots_account ON balance_snapshots(account_id);
CREATE INDEX idx_balance_snapshots_time ON balance_snapshots(snapshot_time);
CREATE INDEX idx_balance_snapshots_transaction ON balance_snapshots(transaction_id);
CREATE INDEX idx_balance_snapshots_entry ON balance_snapshots(entry_id);
CREATE INDEX idx_balance_snapshots_account_time ON balance_snapshots(account_id, snapshot_time);
CREATE INDEX idx_balance_snapshots_currency ON balance_snapshots(currency_code);

-- Create function to automatically create balance snapshots
CREATE OR REPLACE FUNCTION create_balance_snapshot()
RETURNS TRIGGER AS $$
DECLARE
    current_balance NUMERIC(20, 8);
    balance_before NUMERIC(20, 8);
    balance_change NUMERIC(20, 8);
BEGIN
    -- Get current balance before applying this entry
    SELECT COALESCE(balance, 0) INTO balance_before
    FROM account_balances
    WHERE account_id = NEW.account_id;
    
    -- Calculate balance change based on entry type and account type
    IF NEW.entry_type = 'debit' THEN
        CASE NEW.account_type
            WHEN 'asset' THEN balance_change := NEW.amount;
            WHEN 'expense' THEN balance_change := NEW.amount;
            WHEN 'liability' THEN balance_change := -NEW.amount;
            WHEN 'equity' THEN balance_change := -NEW.amount;
            WHEN 'revenue' THEN balance_change := -NEW.amount;
            ELSE RAISE EXCEPTION 'Invalid account type: %', NEW.account_type;
        END CASE;
    ELSE -- credit
        CASE NEW.account_type
            WHEN 'liability' THEN balance_change := NEW.amount;
            WHEN 'equity' THEN balance_change := NEW.amount;
            WHEN 'revenue' THEN balance_change := NEW.amount;
            WHEN 'asset' THEN balance_change := -NEW.amount;
            WHEN 'expense' THEN balance_change := -NEW.amount;
            ELSE RAISE EXCEPTION 'Invalid account type: %', NEW.account_type;
        END CASE;
    END IF;
    
    -- Insert balance snapshot
    INSERT INTO balance_snapshots (
        account_id,
        transaction_id,
        snapshot_time,
        balance_before,
        balance_after,
        balance_change,
        account_type,
        currency_code,
        entry_id,
        entry_type,
        amount,
        description,
        reference_type,
        reference_id
    ) VALUES (
        NEW.account_id,
        NEW.transaction_id,
        CURRENT_TIMESTAMP,
        balance_before,
        balance_before + balance_change,
        balance_change,
        NEW.account_type,
        NEW.currency_code,
        NEW.id,
        NEW.entry_type,
        NEW.amount,
        NEW.description,
        NEW.reference_type,
        NEW.reference_id
    );
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger to automatically create balance snapshots
DROP TRIGGER IF EXISTS trigger_create_balance_snapshot ON journal_entries;
CREATE TRIGGER trigger_create_balance_snapshot
    AFTER INSERT ON journal_entries
    FOR EACH ROW
    EXECUTE FUNCTION create_balance_snapshot();

-- Create view for current account balances
CREATE OR REPLACE VIEW account_balance_view AS
SELECT 
    a.id,
    a.account_number,
    a.account_type,
    a.name,
    a.currency_code,
    COALESCE(ab.balance, 0) as current_balance,
    ab.updated_at as last_updated
FROM accounts a
LEFT JOIN account_balances ab ON a.id = ab.account_id
WHERE a.is_active = TRUE;

-- Create function to get account balance at specific time
CREATE OR REPLACE FUNCTION get_account_balance_at_time(
    p_account_id UUID,
    p_timestamp TIMESTAMP
)
RETURNS NUMERIC(20, 8) AS $$
DECLARE
    balance_at_time NUMERIC(20, 8);
BEGIN
    SELECT COALESCE(
        SUM(balance_change), 
        0
    ) INTO balance_at_time
    FROM balance_snapshots
    WHERE account_id = p_account_id
    AND snapshot_time <= p_timestamp;
    
    RETURN COALESCE(balance_at_time, 0);
END;
$$ LANGUAGE plpgsql;

-- Create function for reconciliation drift detection
CREATE OR REPLACE FUNCTION detect_reconciliation_drift(
    p_account_id UUID,
    p_start_time TIMESTAMP,
    p_end_time TIMESTAMP
)
RETURNS TABLE (
    transaction_id UUID,
    expected_balance NUMERIC(20, 8),
    actual_balance NUMERIC(20, 8),
    drift_amount NUMERIC(20, 8)
) AS $$
DECLARE
    expected_balance NUMERIC(20, 8);
    actual_balance NUMERIC(20, 8);
BEGIN
    -- Calculate expected balance from all entries
    SELECT COALESCE(SUM(
        CASE 
            WHEN bs.entry_type = 'debit' AND a.account_type IN ('asset', 'expense') THEN bs.amount
            WHEN bs.entry_type = 'credit' AND a.account_type IN ('liability', 'equity', 'revenue') THEN bs.amount
            WHEN bs.entry_type = 'debit' AND a.account_type IN ('liability', 'equity', 'revenue') THEN -bs.amount
            WHEN bs.entry_type = 'credit' AND a.account_type IN ('asset', 'expense') THEN -bs.amount
            ELSE 0
        END
    ), 0) INTO expected_balance
    FROM balance_snapshots bs
    JOIN accounts a ON bs.account_id = a.id
    WHERE bs.account_id = p_account_id
    AND bs.snapshot_time >= p_start_time
    AND bs.snapshot_time <= p_end_time;
    
    -- Calculate actual balance from snapshots
    SELECT COALESCE(MAX(balance_after), 0) INTO actual_balance
    FROM balance_snapshots
    WHERE account_id = p_account_id
    AND snapshot_time <= p_end_time;
    
    -- Return result if there's a drift
    IF ABS(expected_balance - actual_balance) > 0.0001 THEN
        RETURN QUERY
        SELECT 
            p_account_id::UUID,
            expected_balance,
            actual_balance,
            actual_balance - expected_balance;
    END IF;
    
    RETURN;
END;
$$ LANGUAGE plpgsql;

-- Create function to validate balance consistency
CREATE OR REPLACE FUNCTION validate_balance_consistency()
RETURNS TABLE (
    account_id UUID,
    account_number TEXT,
    expected_balance NUMERIC(20, 8),
    actual_balance NUMERIC(20, 8),
    is_consistent BOOLEAN
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        abv.id,
        abv.account_number,
        COALESCE(SUM(
            CASE 
                WHEN bs.entry_type = 'debit' AND abv.account_type IN ('asset', 'expense') THEN bs.amount
                WHEN bs.entry_type = 'credit' AND abv.account_type IN ('liability', 'equity', 'revenue') THEN bs.amount
                WHEN bs.entry_type = 'debit' AND abv.account_type IN ('liability', 'equity', 'revenue') THEN -bs.amount
                WHEN bs.entry_type = 'credit' AND abv.account_type IN ('asset', 'expense') THEN -bs.amount
                ELSE 0
            END
        ), 0),
        abv.current_balance,
        (COALESCE(SUM(
            CASE 
                WHEN bs.entry_type = 'debit' AND abv.account_type IN ('asset', 'expense') THEN bs.amount
                WHEN bs.entry_type = 'credit' AND abv.account_type IN ('liability', 'equity', 'revenue') THEN bs.amount
                WHEN bs.entry_type = 'debit' AND abv.account_type IN ('liability', 'equity', 'revenue') THEN -bs.amount
                WHEN bs.entry_type = 'credit' AND abv.account_type IN ('asset', 'expense') THEN -bs.amount
                ELSE 0
            END
        ), 0) = abv.current_balance)
    FROM account_balance_view abv
    LEFT JOIN balance_snapshots bs ON abv.id = bs.account_id
    GROUP BY abv.id, abv.account_number, abv.account_type, abv.current_balance;
END;
$$ LANGUAGE plpgsql;

COMMIT;