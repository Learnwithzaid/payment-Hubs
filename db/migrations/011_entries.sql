-- Migration 011: Create journal entries table for immutable double-entry ledger
-- This table ensures every transaction maintains the double-entry principle

BEGIN TRANSACTION;

-- Create journal_entries table (immutable - no UPDATE/DELETE)
CREATE TABLE IF NOT EXISTS journal_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entry_number TEXT UNIQUE NOT NULL,
    transaction_id UUID NOT NULL,
    entry_type TEXT NOT NULL CHECK (entry_type IN ('debit', 'credit')),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    account_type TEXT NOT NULL CHECK (account_type IN ('asset', 'liability', 'equity', 'revenue', 'expense')),
    amount NUMERIC(20, 8) NOT NULL CHECK (amount > 0),
    description TEXT NOT NULL,
    reference_type TEXT,
    reference_id TEXT,
    currency_code TEXT NOT NULL DEFAULT 'USD' CHECK (length(currency_code) = 3),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    metadata JSONB DEFAULT '{}',
    
    -- Constraints
    CONSTRAINT journal_entries_amount_chk CHECK (amount > 0),
    CONSTRAINT journal_entries_description_chk CHECK (length(description) > 0)
);

-- Create indices for efficient queries
CREATE INDEX idx_journal_entries_number ON journal_entries(entry_number);
CREATE INDEX idx_journal_entries_transaction ON journal_entries(transaction_id);
CREATE INDEX idx_journal_entries_account ON journal_entries(account_id);
CREATE INDEX idx_journal_entries_type ON journal_entries(entry_type);
CREATE INDEX idx_journal_entries_created_at ON journal_entries(created_at);
CREATE INDEX idx_journal_entries_reference ON journal_entries(reference_type, reference_id);

-- Create function to validate double-entry constraint
CREATE OR REPLACE FUNCTION validate_double_entry()
RETURNS TRIGGER AS $$
DECLARE
    transaction_debits NUMERIC(20, 8);
    transaction_credits NUMERIC(20, 8);
BEGIN
    -- Calculate total debits and credits for this transaction
    SELECT 
        COALESCE(SUM(CASE WHEN entry_type = 'debit' THEN amount ELSE 0 END), 0),
        COALESCE(SUM(CASE WHEN entry_type = 'credit' THEN amount ELSE 0 END), 0)
    INTO transaction_debits, transaction_credits
    FROM journal_entries
    WHERE transaction_id = NEW.transaction_id;
    
    -- Add the new entry to the totals
    IF NEW.entry_type = 'debit' THEN
        transaction_debits := transaction_debits + NEW.amount;
    ELSE
        transaction_credits := transaction_credits + NEW.amount;
    END IF;
    
    -- Validate that debits equal credits for the transaction
    IF transaction_debits != transaction_credits THEN
        RAISE EXCEPTION 'Double-entry violation: debits (%.8f) must equal credits (%.8f) for transaction %s',
            transaction_debits, transaction_credits, NEW.transaction_id;
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger to enforce double-entry constraint
DROP TRIGGER IF EXISTS trigger_validate_double_entry ON journal_entries;
CREATE TRIGGER trigger_validate_double_entry
    BEFORE INSERT ON journal_entries
    FOR EACH ROW
    EXECUTE FUNCTION validate_double_entry();

-- Function to ensure every transaction has at least 2 entries
CREATE OR REPLACE FUNCTION validate_transaction_entries()
RETURNS TRIGGER AS $$
DECLARE
    entry_count INTEGER;
BEGIN
    -- Count total entries for this transaction
    SELECT COUNT(*) INTO entry_count
    FROM journal_entries
    WHERE transaction_id = NEW.transaction_id;
    
    -- Every transaction must have at least 2 entries
    IF entry_count < 2 THEN
        RAISE EXCEPTION 'Transaction %s must have at least 2 entries (current: %s)', 
            NEW.transaction_id, entry_count;
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger to validate transaction completeness
DROP TRIGGER IF EXISTS trigger_validate_transaction_entries ON journal_entries;
CREATE TRIGGER trigger_validate_transaction_entries
    AFTER INSERT ON journal_entries
    FOR EACH ROW
    EXECUTE FUNCTION validate_transaction_entries();

-- Create function to update account balance when entries are posted
CREATE OR REPLACE FUNCTION update_account_balance_from_entry()
RETURNS TRIGGER AS $$
DECLARE
    balance_change NUMERIC(20, 8);
    current_balance NUMERIC(20, 8);
BEGIN
    -- Calculate balance change based on entry type and account type
    IF NEW.entry_type = 'debit' THEN
        -- Debits increase asset/expense, decrease liability/equity/revenue
        CASE NEW.account_type
            WHEN 'asset' THEN balance_change := NEW.amount;
            WHEN 'expense' THEN balance_change := NEW.amount;
            WHEN 'liability' THEN balance_change := -NEW.amount;
            WHEN 'equity' THEN balance_change := -NEW.amount;
            WHEN 'revenue' THEN balance_change := -NEW.amount;
            ELSE RAISE EXCEPTION 'Invalid account type: %', NEW.account_type;
        END CASE;
    ELSE -- credit
        -- Credits increase liability/equity/revenue, decrease asset/expense
        CASE NEW.account_type
            WHEN 'liability' THEN balance_change := NEW.amount;
            WHEN 'equity' THEN balance_change := NEW.amount;
            WHEN 'revenue' THEN balance_change := NEW.amount;
            WHEN 'asset' THEN balance_change := -NEW.amount;
            WHEN 'expense' THEN balance_change := -NEW.amount;
            ELSE RAISE EXCEPTION 'Invalid account type: %', NEW.account_type;
        END CASE;
    END IF;
    
    -- Update account balance
    INSERT INTO account_balances (account_id, balance, updated_at)
    VALUES (NEW.account_id, balance_change, CURRENT_TIMESTAMP)
    ON CONFLICT (account_id) 
    DO UPDATE SET 
        balance = account_balances.balance + balance_change,
        updated_at = CURRENT_TIMESTAMP;
    
    -- Validate the new balance doesn't violate account type constraints
    SELECT balance INTO current_balance
    FROM account_balances
    WHERE account_id = NEW.account_id;
    
    -- Asset accounts should have non-negative balance
    IF NEW.account_type = 'asset' AND current_balance < 0 THEN
        RAISE EXCEPTION 'Asset account balance cannot be negative: %.8f', current_balance;
    END IF;
    
    -- Liability accounts should have non-negative balance  
    IF NEW.account_type = 'liability' AND current_balance < 0 THEN
        RAISE EXCEPTION 'Liability account balance cannot be negative: %.8f', current_balance;
    END IF;
    
    -- Equity accounts should have non-negative balance
    IF NEW.account_type = 'equity' AND current_balance < 0 THEN
        RAISE EXCEPTION 'Equity account balance cannot be negative: %.8f', current_balance;
    END IF;
    
    -- Revenue accounts should have non-negative balance
    IF NEW.account_type = 'revenue' AND current_balance < 0 THEN
        RAISE EXCEPTION 'Revenue account balance cannot be negative: %.8f', current_balance;
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger to update account balances
DROP TRIGGER IF EXISTS trigger_update_account_balance ON journal_entries;
CREATE TRIGGER trigger_update_account_balance
    AFTER INSERT ON journal_entries
    FOR EACH ROW
    EXECUTE FUNCTION update_account_balance_from_entry();

COMMIT;