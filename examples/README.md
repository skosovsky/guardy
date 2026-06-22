# Guardy examples

Each subdirectory is a standalone example with its own `go.mod` (using `replace` to the parent module for local development).

Run from the example directory:

```bash
cd input_guard   # or output_guard, streaming_filter, json_streaming, reversible_redaction, multi_turn, otel_integration, custom_validator, struct_validation, generic_decorator, declarative_guard, policy_attributes, agent_tool_args
go mod tidy
go run .
```

- **input_guard** — Prompt guard: validates user prompt before sending to LLM (Regex + Length). Reads from stdin; exits with code 3 on Block.
- **output_guard** — Validates and redacts PII in LLM output; `WithUserChannel` + technical JSON classifier block unsafe payloads for end users.
- **streaming_filter** — Writes a mock token stream through GuardWriter; demonstrates Block (ErrBlocked) when a forbidden word appears in a chunk.
- **json_streaming** — Demonstrates `GuardWriter` with `WithJSONAwareSplitter()` for streamed JSON/tool-call payloads.
- **reversible_redaction** — Demonstrates `TokenVault` + `UnredactText` flow for reversible redaction.
- **multi_turn** — Demonstrates BYOT multi-message validation using `ext.MapSlice`.
- **otel_integration** — Demonstrates telemetry middleware from `github.com/skosovsky/guardy/ext/guardyotel` with payload capture disabled by default.
- **custom_validator** — Custom validator that calls a mock moderation HTTP API; integrates into a pipeline and runs two sample inputs.
- **struct_validation** — Validates raw JSON through `ArgsPipeline`, returns `GuardedArgs[T]`, and prints canonical retry feedback.
- **generic_decorator** — Scope-aware input policy + output user channel with technical JSON classifier; `PolicyFailure` + `GuardedOutput` demo.
- **declarative_guard** — Compiles a `GuardSpec` via `guardy/build` (policy rules, user channel + output classifier); optional `build.WithJSONSchema` for schema validation.
- **policy_attributes** — Policy phase with typed `ScopeKey[T]`; demonstrates `PolicyDecision` and `ScopeIncompleteError` metadata.
- **agent_tool_args** — Redacts PII inside opaque `json.RawMessage` tool args via `MapJSONRawMessage` (not scope-aware policy).
