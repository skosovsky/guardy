# Guardy examples

Each subdirectory is a standalone example with its own `go.mod` (using `replace` to the parent module for local development).

Run from the example directory:

```bash
cd input_guard   # or output_guard, streaming_filter, custom_validator
go mod tidy
go run .
```

- **input_guard** — Validates user prompt before sending to LLM (Regex + Length). Reads from stdin; exits with code 3 on Block.
- **output_guard** — Validates LLM output as JSON against a schema. On Retry, prints Reason/Evidence/Guidance. Pass one argument to validate that string as JSON.
- **streaming_filter** — Writes a mock token stream through GuardWriter; demonstrates Block (ErrBlocked) when a forbidden word appears in a chunk.
- **custom_validator** — Custom validator that calls a mock moderation HTTP API; integrates into a pipeline and runs two sample inputs.
