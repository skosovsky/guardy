// Package ext provides built-in validators for the guardy pipeline.
//
// All validators implement guardy.Validator and are agnostic to business rules;
// patterns and lists are supplied at construction time.
//
// Available validators:
//   - TagSanitizer: WAF-style block on system XML tags
//   - PIIMasking: redact email, phone, credit card
//   - Regex: match or replace text using regular expressions
//   - Wordlist: blocklist or allowlist by tokens (words)
//   - Length: min/max rune length
package ext
