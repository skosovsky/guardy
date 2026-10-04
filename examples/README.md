# Guardy examples

Each subdirectory is a standalone example with its own `go.mod` (using `replace` to the parent module for local development).

Run from the example directory:

```bash
cd input_guard   # or output_guard, streaming_filter, json_streaming, reversible_redaction, multi_turn, otel_integration, custom_validator, struct_validation, generic_decorator, declarative_guard, policy_attributes, agent_tool_args
go mod tidy
go run .
```

- **input_guard** — Matches example phrases and length before a host call; no trained instruction detector. Reads from stdin; exits with code 3 on Block.
- **output_guard** — Redacts documented PII formats; a tool-key JSON heuristic demonstrates `WithUserChannel`. This heuristic does not establish trusted provenance.
- **streaming_filter** — Buffers a mock producer with an explicit whole-response profile; trusted completion blocks forbidden content before release.
- **json_streaming** — Uses `CompileStream` with explicit whole-response and JSON validation; malformed/forbidden final content is never flushed.
- **reversible_redaction** — Shows host recipient authorization before `UnredactText`, then a separate final output guard before delivery. Token possession grants no disclosure permission.
- **multi_turn** — Applies an independent validator to each BYOT message using `ext.MapSlice`; no cross-message context analysis.
- **otel_integration** — Demonstrates telemetry middleware from `github.com/skosovsky/guardy/ext/guardyotel` with payload capture disabled by default; raw validator errors are never exported. Code/name labels require a static caller allowlist. Metrics and slow-path spans are not a complete decision audit.
- **custom_validator** — Custom validator that calls a mock word-matching HTTP API (no supplied model); integrates into a pipeline and runs two sample inputs.
- **struct_validation** — Validates raw JSON through `ArgsPipeline`, returns `GuardedArgs[T]`, and prints canonical retry feedback.
- **generic_decorator** — Scope-aware input policy + output user channel with technical JSON classifier; `PolicyFailure` + `GuardedOutput` demo. Separate viewer/admin flows reach policy, wordlist, and delivery checks; JSON shape detection supplies no authorization.
- **declarative_guard** — Compiles a `GuardSpec` via `guardy/build` (policy rules, user channel + output classifier); optional `build.WithJSONSchema` for schema validation. Independent scenarios continue after the expected policy denial.
- **policy_attributes** — Policy phase with typed `ScopeKey[T]`; demonstrates `PolicyDecision` and `ScopeIncompleteError` metadata.
- **agent_tool_args** — Redacts PII inside opaque `json.RawMessage` tool args via `MapJSONRawMessage` (not scope-aware policy).
