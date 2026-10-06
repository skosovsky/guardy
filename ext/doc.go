// Package ext provides built-in validators for the guardy pipeline.
//
// All validators implement guardy.Validator and are agnostic to business rules;
// patterns and lists are supplied at construction time.
//
// Available validators:
//   - TagPatternValidator: text matcher for XML-like system tags
//   - PIIValidator: redact or block documented ASCII email/phone/card formats
//   - RegexValidator: match or replace text using regular expressions
//   - WordlistValidator: blocklist or allowlist by tokens (words)
//   - LengthValidator: min/max rune length
//   - ClassifierValidator: context-aware adapter for caller-supplied classifiers
//   - NewTechnicalJSONClassifier: heuristically marks tool-like JSON as PayloadTechnicalPayload for [guardy.WithUserChannel]
package ext
