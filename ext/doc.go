// Package ext provides built-in validators for the guardy pipeline.
//
// All validators implement guardy.Validator and are agnostic to business rules;
// patterns and lists are supplied at construction time.
//
// Available validators:
//   - Regex: match or replace text using regular expressions
//   - Wordlist: blocklist or allowlist by tokens (words)
//   - Length: min/max rune length
//   - JSON: valid JSON and optional required keys
package ext
