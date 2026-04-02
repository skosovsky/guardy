# Guardy

[![Go Reference](https://pkg.go.dev/badge/github.com/skosovsky/guardy.svg)](https://pkg.go.dev/github.com/skosovsky/guardy)
[![Build](https://img.shields.io/badge/build-go%20build-blue)](https://github.com/skosovsky/guardy)
[![Coverage](https://img.shields.io/badge/coverage-go%20test-green)](https://github.com/skosovsky/guardy)

**TL;DR** — guardy is a lightweight guardrails library for LLM applications in Go. It provides a two-phase pipeline for validating prompts and model responses: a sequential **fast path** (WAF, PII redaction, wordlist) and a parallel **slow path** (semantic/LLM checks), with easy extension via custom validators.

---

## Requirements

- Go 1.26+

## Installation

```bash
go get github.com/skosovsky/guardy
```

## Quick Start

Build a pipeline with fast-path validators and validate text:

```go
package main

import (
	"context"
	"fmt"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

func main() {
	lengthV := ext.NewLengthValidator(0, 2048, ext.WithCode("TOO_LONG"))
	wordlistV := ext.NewWordlistValidator([]string{"bad", "spam"}, ext.Blocklist, ext.WithCode("FORBIDDEN"))
	piiV := ext.NewPIIValidator()

	pipeline := guardy.NewPipeline(
		guardy.WithFastPath(ext.MustTagSanitizerValidator(""), piiV, wordlistV, lengthV),
	)

	ctx := context.Background()
	text := "Contact me at user@example.com"
	result, err := pipeline.Run(ctx, text)
	if err != nil {
		panic(err)
	}
	report := result.Decision()
	switch report.Action {
	case guardy.ActionBlock:
		fmt.Println("blocked:", report.Reason)
	case guardy.ActionRedact:
		fmt.Println("ok (redacted):", result.Output)
	case guardy.ActionPass:
		fmt.Println("ok:", result.Output)
	}
}
```

## Key abstractions

### Validator

Validators implement the generic **Validator[T]** interface:

```go
type Validator[T any] interface {
	Validate(ctx context.Context, input T) (T, *Report, error)
}
```

For string validation: `Validator[string]`. The pipeline returns the mutated text as the first value; on **ActionRedact** the validator provides the cleaned string. Report holds **Action** (`ActionPass`, `ActionBlock`, `ActionRedact`, `ActionRetry`), **Validator**, **Code**, **Severity**, **Reason**, **Feedback**, **MutatedText**, **Score**, **ShadowMode**.

### Pipeline (two-phase)

- **Construction**: `NewPipeline[string](WithFastPath(...), WithSlowPath(...))`.
- **Execution**: `Run(ctx, text)` returns `(RunResult[string], error)`. Use `result.Output` for mutated text and `result.Decision()` for the outcome Report. `result.Reports` holds all validator reports for telemetry.

`pipeline.Use()` is immutable in v2-style API: it returns a new pipeline instance and does not mutate the original.

**Phase 1 — Fast path (sequential)**  
Validators that may **redact** or **block** run one after another. The text is passed along the chain; each redact step replaces it with `MutatedText`. On **block** (and not shadow), the pipeline returns immediately. Use for: TagSanitizerValidator, PIIValidator, WordlistValidator, RegexValidator, LengthValidator.

**Phase 2 — Slow path (parallel)**  
Heavy validators that only **block** or **pass** run in parallel via `errgroup` on the final text from phase 1. **Decision()** priority: `Block > Retry > last Redact > last Pass`. Context is cancelled only on **Block** (not Retry) so all reports are collected. On validator error, a **partial RunResult** with gathered reports is returned (telemetry preserved). Use for: SemanticValidator, LLMJudge.

**Recommended order in fast path:** WAF (TagSanitizerValidator) → PII (PIIValidator) → WordlistValidator → RegexValidator/LengthValidator.

### Report

**Report** holds: **Action**, **Validator**, **Code**, **Severity**, **Reason**, **Feedback**, **Score**, **ShadowMode**, **MutatedText**. After `Run`, use the first return value (mutated text) and `report.Action`.

### Stream (GuardWriter)

Use **GuardWriter** to validate streaming output in chunks:

- Buffers until a semantic boundary (space, newline, punctuation) or a delimiterless hard cap is reached; runs the pipeline on each chunk. UTF-8 safe; index-based buffering; overlap prevents boundary bypass.
- On **Block** — returns `ErrBlocked`.
- On **Retry** — returns `ErrRetryRequested` (orchestrator should retry with Feedback).
- On **Redact** — writes the mutated text for that chunk.
- On **Pass** — writes the original chunk.
- In JSON-aware mode, incomplete JSON is never validated/written; `Close()` on incomplete JSON returns `ErrValidatorFailed`.

Options: **WithChunkSize** (preferred natural boundary target, default 4096), **WithMaxChunkSize** (delimiterless hard cap, default 2048), **WithJSONAwareSplitter** (for streamed JSON/tool-call payloads), **WithContext**, **WithTimeout**.

```go
gw := guardy.NewGuardWriter(
	w,
	pipeline,
	guardy.WithChunkSize(4096),
	guardy.WithMaxChunkSize(2048),
)
_, _ = gw.Write(data)
_ = gw.Close()
```

### HTTP Guard (`http_guard.go`)

**Guard** wraps an HTTP handler: the request body is read once; the extractor turns it into text for the pipeline. On **Block** or **Retry** — 422 JSON response. On **Redact** — replaces body with `MutatedText` and calls next. On **Pass** — restores the **original** request body (not the extractor’s return value) and calls next. Use **ReportFromContext(ctx)** in the next handler to get the report.

```go
extractor := func(r *http.Request) (string, error) {
	body, _ := io.ReadAll(r.Body)
	return string(body), nil
}
handler := guardy.Guard(pipeline, extractor, guardy.PlainTextInjector())(yourHandler)
```

### Generic decorators (`interceptor.go`)

**WrapInput** runs a pipeline on the request value before your `func(context.Context, Req) (Res, error)`. **WrapOutput** runs after your function on the result. Block maps to an error wrapping **ErrBlocked**; retry maps to **RetryError** (unwraps to **ErrRetryRequested**). See `examples/generic_decorator`.

## Built-in validators (ext)

| Validator       | Description |
|----------------|-------------|
| **TagSanitizerValidator** | Blocks on system-tag injection (e.g. `<system>`, `</system>`). `ext.NewTagSanitizerValidator(pattern)` or `ext.MustTagSanitizerValidator("")`. |
| **PIIValidator**   | Redacts or blocks email, phone, credit card. `ext.NewPIIValidator(...)` with `ext.WithAction`, `ext.WithCode`, `ext.WithSeverity`, `ext.WithRedactionReplacement`, `ext.WithTokenVault`. |
| **WordlistValidator**     | Blocklist or allowlist; block or redact. `ext.NewWordlistValidator(words, mode, ...)` with `ext.WithAction`, `ext.WithCode`, `ext.WithLowercase`, `ext.WithRedactionReplacement`, `ext.WithTokenVault`. |
| **RegexValidator**        | Match pattern; block or redact. `ext.NewRegexValidator(pattern, ...)` with `ext.WithAction`, `ext.WithCode`, `ext.WithSeverity`, `ext.WithRedactionReplacement`. |
| **LengthValidator**       | Min/max rune length. `ext.NewLengthValidator(min, max, ...)` with `ext.WithCode`, `ext.WithSeverity`, `ext.WithName`. |
| **JSON Schema**  | Optional submodule `guardy/ext/jsonschema` — validates JSON strings against a schema; returns **ActionRetry** with **Feedback** on violation. |

### Structured Output / JSON Schema

For low-level control you can still provide a raw JSON Schema string:

```go
validator, _ := jsonschemaext.NewJSONSchemaValidator(`{
  "type": "object",
  "properties": {
    "name": {"type": "string"}
  },
  "required": ["name"]
}`)
```

`jsonschema` also accepts shared v2 rule options (`ext.WithCode`, `ext.WithSeverity`, `ext.WithReason`, `ext.WithName`). It is an intrinsically `ActionRetry` validator; mutation options (`WithTokenVault`, `WithRedactionReplacement`, `WithLowercase`) are rejected at construction time.

For the common case, prefer generating the schema from a Go struct:

```go
package main

import jsonschemaext "github.com/skosovsky/guardy/ext/jsonschema"

type User struct {
	Name string `json:"name" jsonschema:"required"`
	Age  int    `json:"age" jsonschema:"minimum=18"`
}

validator, _ := jsonschemaext.NewJSONSchemaValidatorFromStruct(&User{})
```

`NewJSONSchemaValidatorFromStruct` keeps the schema in sync with your Go type and still returns `ActionRetry` with detailed `Feedback` for invalid JSON or schema violations.

### Token Vault (Reversible Redaction)

Use `TokenVault` when you need reversible redaction (`[GUARDY_TOKEN_...]`) and later restoration in model output:

```go
vault := ext.NewInMemoryTokenVault()
piiV := ext.NewPIIValidator(
	ext.WithAction(guardy.ActionRedact),
	ext.WithTokenVault(vault),
)
result, _ := guardy.NewPipeline(guardy.WithFastPath(piiV)).Run(ctx, "email: a@b.com")
restored := ext.UnredactText("model: "+result.Output, vault)
```

Built-in validators write namespaced canonical tokens such as `[GUARDY_TOKEN_PII_1]` and `[GUARDY_TOKEN_WORDLIST_1]` through `TokenVault.Store(namespace, original)`.

### Multi-turn Adapter (MapSlice)

Use `MapSlice` for BYOT message slices (`[]T`) without introducing framework-specific message types into guardy:

```go
type Msg struct{ Content string }
base, _ := ext.NewRegexValidator(`(?i)secret`, ext.WithAction(guardy.ActionRedact))
multi := ext.MapSlice(
	func(m Msg) string { return m.Content },
	func(m Msg, s string) Msg { m.Content = s; return m },
	base,
)
```

If any item returns `ActionBlock` or `ActionRetry`, `MapSlice` blocks the whole collection by default.

### Telemetry (Optional `ext/guardyotel`)

For OpenTelemetry integration without adding heavy deps to root module, use `github.com/skosovsky/guardy/ext/guardyotel`:

```go
import "github.com/skosovsky/guardy/ext/guardyotel"

pipeline := guardy.NewPipeline(guardy.WithFastPath(v))
pipeline = pipeline.Use(guardyotel.NewMiddleware[string](
	guardyotel.WithIncludePayloads(false), // default secure mode
))
```

Fast path exports counters/histograms; slow path emits spans. Raw payload capture is opt-in.

### Map (Lens adapter)

Use **Map[T,U]** to adapt `Validator[U]` to `Validator[T]` for domain structs:

```go
type AgentState struct { Text string }
regexV, _ := ext.NewRegexValidator(`(?i)bad`, ext.WithAction(guardy.ActionRedact), ext.WithCode("X"), ext.WithRedactionReplacement("[REDACTED]"))
v := guardy.Map(regexV, func(s *AgentState) string { return s.Text },
	func(s *AgentState, t string) *AgentState { s.Text = t; return s })
```

## Core validators (guardy)

- **SemanticValidator** — wraps a `Matcher` and threshold; use for similarity/embedding checks (slow path).
- **LLMJudge** — wraps a `Judge`; use for LLM-as-judge (slow path). Both support **shadow mode** (block is logged but does not short-circuit).

## Testing with guardytest

Use **guardy/guardytest** for unit tests:

- **FakeValidator(name, *guardy.Report)** — validator that always returns the given report (nil or zero = pass).
- **FailingValidator(name, err)** — validator that always returns the given error.
- **MustPass**, **MustBlock**, **MustRedact** — assert `report.Action`.

```go
v := guardytest.FakeValidator("mock", &guardy.Report{Action: guardy.ActionBlock, Reason: "TEST"})
pipeline := guardy.NewPipeline(guardy.WithFastPath(v))
result, _ := pipeline.Run(ctx, "x")
guardytest.MustBlock(t, result.Decision())
```

## Error handling

- **ErrBlocked** — returned by GuardWriter when report.Action == ActionBlock.
- **ErrRetryRequested** — returned by GuardWriter when report.Action == ActionRetry.
- **ErrValidatorFailed** — wraps a validator’s system error from `Run`.

## Packages

- **guardy** — core types (Action, Report, Validator), Pipeline, SemanticValidator, LLMJudge, GuardWriter, Guard middleware, errors.
- **guardy/ext** — TagSanitizerValidator, PIIValidator, WordlistValidator, RegexValidator, LengthValidator, TokenVault, MapSlice, MLValidator.
- **guardy/ext/jsonschema** — optional JSON Schema validator with raw-schema and struct-derived constructors.
- **guardy/ext/guardyotel** — optional OTel middleware module (metrics + tracing).
- **guardy/guardytest** — FakeValidator, FailingValidator, MustPass/MustBlock/MustRedact.

See [.cursor/docs/task9.md](.cursor/docs/task9.md) for the full v2 technical specification.

## v2 Migration Highlights

- `Pipeline.Use(...)` is immutable and returns a new pipeline.
- `Report` includes `Code` and typed `Severity`.
- `StreamOption` was renamed to `GuardWriterOption`.
- `PIIMasking` APIs were renamed to `PIIValidator` / `NewPIIValidator`.
- `ext/jsonschema.NewValidatorFromStruct` was renamed to `NewJSONSchemaValidatorFromStruct`.
- Built-in `ext` validators use options for common rule metadata (`WithAction`, `WithCode`, `WithSeverity`, `WithReason`, ...).

## Wordlist Benchmark Evidence (Task9 DoD #3)

Run the reproducible redact comparison benchmark:

```bash
go test -bench '^BenchmarkWordlist_Blocklist_Redact_BaselineComparison$' -benchmem ./ext -run '^$'
```

This benchmark includes:
- `baseline_a16279e_runtime_compile`: frozen pre-v2 redact path copied from commit `a16279e` (runtime regex compile on each hit).
- `v2_precompiled`: current v2 validator with precompiled matchers from constructor.

Latest local run on this workspace (March 28, 2026):
- `baseline_a16279e_runtime_compile`: `~197k ns/op`, `~234k B/op`, `~1678 allocs/op`
- `v2_precompiled`: `~3.2k ns/op`, `~587 B/op`, `~15 allocs/op`
- Throughput ratio: `~61x` faster for v2 on redact path.

## Development

```bash
make test
make lint
```

## License

See [LICENSE](LICENSE).
