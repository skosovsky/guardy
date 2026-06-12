// Package ext provides built-in validators for the guardy pipeline.
//
// All validators implement guardy.Validator and are agnostic to business rules;
// patterns and lists are supplied at construction time.
//
// Available validators:
//   - TagSanitizerValidator: WAF-style block on system XML tags
//   - PIIValidator: redact or block email, phone, credit card
//   - RegexValidator: match or replace text using regular expressions
//   - WordlistValidator: blocklist or allowlist by tokens (words)
//   - LengthValidator: min/max rune length
//   - MLValidator: fast-path contract for local classifiers
//   - NewTechnicalJSONClassifier: classifies tool-call JSON as PayloadTechnicalPayload for [guardy.WithUserChannel]
package ext
