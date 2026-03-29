# Guardy examples

Each subdirectory is a standalone example with its own `go.mod` (using `replace` to the parent module for local development).

Run from the example directory:

```bash
cd input_guard   # or output_guard, streaming_filter, json_streaming, reversible_redaction, multi_turn, otel_integration, custom_validator, struct_validation
go mod tidy
go run .
```

- **input_guard** — Prompt guard: validates user prompt before sending to LLM (Regex + Length). Reads from stdin; exits with code 3 on Block.
- **output_guard** — Validates and redacts PII in LLM output (`PIIValidator`). Prints either original output (pass) or `MutatedText` (redact). Pass one argument to validate that string.
- **streaming_filter** — Writes a mock token stream through GuardWriter; demonstrates Block (ErrBlocked) when a forbidden word appears in a chunk.
- **json_streaming** — Demonstrates `GuardWriter` with `WithJSONAwareSplitter()` for streamed JSON/tool-call payloads.
- **reversible_redaction** — Demonstrates `TokenVault` + `UnredactText` flow for reversible redaction.
- **multi_turn** — Demonstrates BYOT multi-message validation using `ext.MapSlice`.
- **otel_integration** — Demonstrates telemetry middleware from `github.com/skosovsky/guardy/ext/guardyotel` with payload capture disabled by default.
- **custom_validator** — Custom validator that calls a mock moderation HTTP API; integrates into a pipeline and runs two sample inputs.
- **struct_validation** — Generates JSON Schema from a Go struct, validates an LLM-style JSON response, and prints `ActionRetry` feedback when the payload violates schema rules.
