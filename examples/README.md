# Guardy examples

Each subdirectory is a standalone example with its own `go.mod` (using `replace` to the parent module for local development).

Run from the example directory:

```bash
cd input_guard   # or output_guard, streaming_filter, custom_validator
go mod tidy
go run .
```

- **input_guard** — Prompt guard: validates user prompt before sending to LLM (Regex + Length). Reads from stdin; exits with code 3 on Block.
- **output_guard** — Validates and redacts PII in LLM output (PIIMasking). Prints either original output (pass) or `MutatedText` (redact). Pass one argument to validate that string.
- **streaming_filter** — Writes a mock token stream through GuardWriter; demonstrates Block (ErrBlocked) when a forbidden word appears in a chunk.
- **custom_validator** — Custom validator that calls a mock moderation HTTP API; integrates into a pipeline and runs two sample inputs.
