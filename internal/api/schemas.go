package api

const createAccountSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["account_number", "account_type", "name", "currency_code", "created_by"],
  "properties": {
    "account_number": {"type": "string", "minLength": 1, "maxLength": 50},
    "account_type": {"type": "string", "enum": ["asset", "liability", "equity", "revenue", "expense"]},
    "name": {"type": "string", "minLength": 1, "maxLength": 255},
    "currency_code": {"type": "string", "pattern": "^[A-Z]{3}$"},
    "created_by": {"type": "string", "minLength": 1, "maxLength": 255},
    "metadata": {"type": "object"}
  }
}`

const debitSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["account_id", "amount", "currency_code", "created_by"],
  "properties": {
    "transaction_id": {"type": "string"},
    "account_id": {"type": "string", "minLength": 1},
    "amount": {"type": "number", "exclusiveMinimum": 0},
    "description": {"type": "string"},
    "reference_type": {"type": "string"},
    "reference_id": {"type": "string"},
    "currency_code": {"type": "string", "pattern": "^[A-Z]{3}$"},
    "created_by": {"type": "string", "minLength": 1},
    "metadata": {"type": "object"}
  }
}`

const creditSchema = debitSchema

const disputesSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["journal_entry_id", "merchant_id", "disputed_amount", "currency_code", "reason_code", "reason_text", "created_by"],
  "properties": {
    "journal_entry_id": {"type": "string", "minLength": 1},
    "merchant_id": {"type": "string", "minLength": 1},
    "disputed_amount": {"type": "number", "exclusiveMinimum": 0},
    "currency_code": {"type": "string", "pattern": "^[A-Z]{3}$"},
    "reason_code": {"type": "string", "minLength": 1},
    "reason_text": {"type": "string", "minLength": 1},
    "reference_type": {"type": "string"},
    "reference_id": {"type": "string"},
    "created_by": {"type": "string", "minLength": 1},
    "metadata": {"type": "object"}
  }
}`
