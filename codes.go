package guardy

// Neutral machine-readable report codes (host apps may define their own strings).
const (
	CodeJSONInvalid         = "JSON_INVALID"
	CodeJSONRedactCorrupted = "JSON_REDACT_CORRUPTED"
	CodeJSONSchemaInvalid   = "JSON_SCHEMA_INVALID"
	CodePolicyViolation     = "POLICY_VIOLATION"
	CodeAttributeMissing    = "ATTRIBUTE_MISSING"
	CodeAttributeMismatch   = "ATTRIBUTE_MISMATCH"
	CodePostBindViolation   = "POST_BIND_VIOLATION"
)
