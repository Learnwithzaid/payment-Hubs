-- Migration 010: Create accounts table for immutable double-entry ledger
-- Each account represents a financial entity (asset, liability, equity, revenue, expense)

BEGIN TRANSACTION;

-- Create accounts table
CREATE TABLE IF NOT EXISTS accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_number TEXT UNIQUE NOT NULL,
    account_type TEXT NOT NULL CHECK (account_type IN ('asset', 'liability', 'equity', 'revenue', 'expense')),
    name TEXT NOT NULL,
    currency_code TEXT NOT NULL DEFAULT 'USD' CHECK (length(currency_code) = 3),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    metadata JSONB DEFAULT '{}',
    
    -- Constraints
    CONSTRAINT accounts_account_number_chk CHECK (length(account_number) > 0),
    CONSTRAINT accounts_name_chk CHECK (length(name) > 0)
);

-- Create indices for efficient lookups
CREATE INDEX idx_accounts_number ON accounts(account_number);
CREATE INDEX idx_accounts_type ON accounts(account_type);
CREATE INDEX idx_accounts_active ON accounts(is_active);
CREATE INDEX idx_accounts_currency ON accounts(currency_code);
CREATE INDEX idx_accounts_created_at ON accounts(created_at);

-- Create function to validate account type normal balances
CREATE OR REPLACE FUNCTION validate_account_balance()
RETURNS TRIGGER AS $$
BEGIN
    -- Asset accounts should have debit normal balance (positive balance)
    IF NEW.account_type = 'asset' AND NEW.balance < 0 THEN
        RAISE EXCEPTION 'Asset accounts cannot have negative balance';
    END IF;
    
    -- Liability, equity, and revenue accounts should have credit normal balance (positive balance)
    IF NEW.account_type IN ('liability', 'equity', 'revenue') AND NEW.balance < 0 THEN
        RAISE EXCEPTION '%s accounts cannot have negative balance', NEW.account_type;
    END IF;
    
    -- Expense accounts should have debit normal balance (can be negative/positive)
    -- No constraint needed for expenses as they can have either sign
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create balances view or table for efficient balance lookups
-- We'll maintain this via triggers
CREATE TABLE IF NOT EXISTS account_balances (
    account_id UUID PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    balance NUMERIC(20, 8) NOT NULL DEFAULT 0,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    CONSTRAINT account_balances_chk CHECK (balance >= 0)
);

CREATE INDEX idx_account_balances_updated ON account_balances(updated_at);

-- Function to update balance when entries are posted
CREATE OR REPLACE FUNCTION update_account_balance()
RETURNS TRIGGER AS $$
DECLARE
    balance_change NUMERIC(20, 8);
BEGIN
    IF TG_OP = 'INSERT' THEN
        -- Calculate balance change based on entry type
        IF NEW.entry_type = 'debit' THEN
            -- Debit increases asset/expense, decreases liability/equity/revenue
            CASE NEW.account_type
                WHEN 'asset' THEN balance_change := NEW.amount;
                WHEN 'expense' THEN balance_change := NEW.amount;
                WHEN 'liability' THEN balance_change := -NEW.amount;
                WHEN 'equity' THEN balance_change := -NEW.amount;
                WHEN 'revenue' THEN balance_change := -NEW.amount;
                ELSE RAISE EXCEPTION 'Invalid account type: %', NEW.account_type;
            END CASE;
        ELSE -- credit
            -- Credit increases liability/equity/revenue, decreases asset/expense
            CASE NEW.account_type
                WHEN 'liability' THEN balance_change := NEW.amount;
                WHEN 'equity' THEN balance_change := NEW.amount;
                WHEN 'revenue' THEN balance_change := NEW.amount;
                WHEN 'asset' THEN balance_change := -NEW.amount;
                WHEN 'expense' THEN balance_change := -NEW.amount;
                ELSE RAISE EXCEPTION 'Invalid account type: %', NEW.account_type;
            END CASE;
        END IF;
        
        -- Update the balance
        INSERT INTO account_balances (account_id, balance, updated_at)
        VALUES (NEW.account_id, balance_change, CURRENT_TIMESTAMP)
        ON CONFLICT (account_id) 
        DO UPDATE SET 
            balance = account_balances.balance + balance_change,
            updated_at = CURRENT_TIMESTAMP;
        
        RETURN NEW;
    END IF;
    
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

COMMIT;