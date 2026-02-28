// Package ext provides built-in validators for the guardy pipeline.
//
// All validators implement guardy.Validator and are agnostic to business rules;
// patterns and lists are supplied at construction time.
//
// Available validators:
//   - Regex: match or replace text using regular expressions
//   - Wordlist: blocklist or allowlist by tokens (words)
//   - Length: min/max rune length
//   - JSONSchema: JSON Schema validation (github.com/google/jsonschema-go); always returns Retry with Guidance on mismatch
package ext
