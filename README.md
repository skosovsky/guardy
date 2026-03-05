# Guardy

[![Go Reference](https://pkg.go.dev/badge/github.com/skosovsky/guardy.svg)](https://pkg.go.dev/github.com/skosovsky/guardy)
[![Build](https://img.shields.io/badge/build-go%20build-blue)](https://github.com/skosovsky/guardy)
[![Coverage](https://img.shields.io/badge/coverage-go%20test-green)](https://github.com/skosovsky/guardy)

**TL;DR** — guardy is a lightweight guardrails library for LLM applications in Go. It provides pipelines for synchronous and streaming validation of prompts and model responses, protecting against injections, toxicity, and invalid formats (JSON/regex), with easy extension via custom validators.

---

## Requirements

- Go 1.26+

## Installation

```bash
go get github.com/skosovsky/guardy
```

## AI-Friendly Quick Start

Build a basic pipeline with built-in validators (max length, blocklist, JSON schema) and validate LLM output:

```go
package main

import (
	"context"
	"fmt"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

func main() {
	lengthV := ext.NewLength(0, 2048, guardy.Block, "TOO_LONG")
	wordlistV := ext.NewWordlist([]string{"bad", "spam"}, ext.Blocklist, guardy.Block, "FORBIDDEN")
	jsonV := ext.MustJSONSchema(`{"type":"object"}`, "INVALID_JSON")

	pipeline := guardy.NewPipeline(
		guardy.WithFailFast(true),
		guardy.WithTier1(lengthV, wordlistV, jsonV),
	)

	ctx := context.Background()
	llmOutput := `{"answer": "hello"}`
	report, err := pipeline.Run(ctx, guardy.Input{Text: llmOutput})
	if err != nil {
		// system error (e.g. validator failed when failOpen=false)
		panic(err)
	}
	switch report.FinalAction {
	case guardy.Block:
		fmt.Println("blocked:", report.Results[0].Code)
	case guardy.Retry:
		r := report.Results[0]
		fmt.Println("retry:", r.Reason, r.Guidance)
	case guardy.Pass, guardy.Redact:
		fmt.Println("ok:", report.FinalText)
	default:
		fmt.Println("action:", report.FinalAction)
	}
}
```

## Key abstractions

### Validator

Any type implementing the **Validator** interface can be used in a pipeline:

```go
type Validator interface {
	Validate(ctx context.Context, input Input) (Result, error)
	Name() string
}
```

Pass your validators to `WithTier1`, `WithTier2`, or `WithTier3`.

### ConditionalValidator

Wrap a validator so it runs only when a condition holds: `ConditionalValidator{Validator: v, Predicate: func(Input) bool {...}}`. If `Predicate` is nil, the inner validator always runs. Use this for context-dependent checks (e.g. only run a heavy validator when `len(input.Text) > 100`).

### Result

Each validator returns a **Result**:

- **Passed**, **Action**, **Code** — outcome and machine-readable code (e.g. `PROMPT_INJECTION`, `PII_DETECTED`).
- **Feedback triad** (for Retry / self-correction): **Reason**, **Evidence**, **Guidance** — optional; LLM can use these to fix the text.
- **CleanText** — for Action `Redact`, the sanitized text.
- **OverrideText** — for Action `Override`, the replacement response.

### Input

**Input** passed into validators and `Pipeline.Run`:

- **Text** — current fragment (prompt or response chunk).
- **Messages** — `[]Message` (Role, Content) for conversation context (e.g. Tier 3 LLM-as-judge).
- **Metadata** — `map[string]any` for request-scoped data.
- **Documents** — `[]Document` (ID, Content, Metadata) for RAG context.

### Pipeline

- **Construction**: `NewPipeline(WithTier1(...), WithTier2(...), WithTier3(...))`.
- **Execution**: `Run(ctx, input)` returns `(Report, error)`.
- **Tiers**: Tier 1 = fast heuristics, Tier 2 = semantic, Tier 3 = e.g. LLM-as-judge. Validators inside a tier run **in parallel**; tiers run **sequentially**. After Redact in a tier, the next tier receives the cleaned `FinalText`.
- **Fail-fast**: `WithFailFast(true)` (default) — stop on first Block. `WithFailFast(false)` — run all tiers and aggregate.
- **WithFailOpen**: on validator system error — `true` = skip and continue, `false` = treat as Block.
- **WithLogger(slog.Logger)** — optional structured logging per validator.
- **WithOnResult(func(name, Result, duration))** — optional callback after each validator (for metrics).

### Report

**Report** aggregates the run: **Results**, **FinalAction**, **FinalText**, **OverrideText**. When multiple results exist, the worst action wins: **PriorityForAction** order is Block > Override > Redact > Retry > Pass.

### Stream (GuardWriter)

Use **GuardWriter** to validate streaming output (e.g. SSE) in chunks:

- Wraps an `io.Writer`; buffers until chunk size or a semantic boundary (space, newline, punctuation), then runs the pipeline on the chunk. UTF-8 safe (no mid-rune splits).
- On **Block** — returns `ErrBlocked`, ignores further writes.
- On **Redact** — writes `CleanText` for that chunk.
- On **Pass** / **Retry** — writes the original chunk.

Options: **WithChunkSize** (default 4096), **WithContext** (context factory per chunk), **WithTimeout** (per-chunk validation timeout).

```go
gw := guardy.NewGuardWriter(w, pipeline, guardy.WithChunkSize(4096))
_, _ = gw.Write(data)
_ = gw.Close()
```

### Middleware (Guard)

**Guard** wraps an HTTP handler: extractor turns `*http.Request` into `Input`, pipeline runs; on **Block** or **Retry** responds with 422 and JSON `{code, message}` (code/reason from Report); on **Override** responds 200 with OverrideText; on **Pass** or **Redact** calls next (Redact replaces request body with `FinalText`). In the next handler, use **ReportFromContext(ctx)** to get `(Report, bool)` and access Results/Reason.

```go
handler := guardy.Guard(pipeline, extractor)(yourHandler)
```

### Pipeline Middleware

**PipelineMiddleware** wraps the entire `Pipeline.Run` call at the Go/agent level (one wrap per run). Use it for cross-cutting concerns: measuring total validation time, logging by `FinalAction`/Code/Reason, audit logging of blocks, or a single OpenTelemetry span. It is strictly optional: when you do not add any pipeline middlewares, `Run` calls the core logic directly (zero overhead).

Contrast with **Guard**, which is only for HTTP: it turns a request into `Input`, runs the pipeline, and maps `Report` to HTTP responses.

Example (metrics / logging) using **report.WorstReason()** for the main reason:

```go
// imports: context, log, time, guardy
func MetricsMiddleware(next guardy.PipelineHandler) guardy.PipelineHandler {
    return func(ctx context.Context, input guardy.Input) (guardy.Report, error) {
        start := time.Now()
        report, err := next(ctx, input)
        duration := time.Since(start)
        if err == nil && report.FinalAction != guardy.Pass {
            log.Printf("Security: Action=%s Reason=%s Time=%v",
                report.FinalAction, report.WorstReason(), duration)
        }
        return report, err
    }
}
pipeline := guardy.NewPipeline(
    guardy.WithTier1(...),
    guardy.WithPipelineMiddleware(MetricsMiddleware),
)
```

## Built-in validators (ext)

| Validator | Constructor | Description |
|-----------|-------------|-------------|
| **Length** | `ext.NewLength(min, max, action, code)` | Enforce min/max rune length; use 0 to skip a bound. Option: `WithLengthName`. **MustLength** — same, panics if min > max (init-time). |
| **Wordlist** | `ext.NewWordlist(words, mode, action, code)` | **Blocklist** or **Allowlist**; option `WithWordlistLowercase`, `WithWordlistName`. |
| **Regex** | `ext.NewRegex(pattern, action, code)` | Match pattern; Block or Redact. For Redact use `WithRegexPlaceholder` (default `[REDACTED]`). Option `WithRegexName`. **MustRegex** — panics on invalid pattern. |
| **JSON** | `ext.NewJSONSchema(schemaJSON, code)` / **MustJSONSchema** | Validate against JSON Schema (draft-07 / 2020-12). On invalid JSON or schema mismatch always returns **Retry** with Reason, Evidence, Guidance. Options: `WithJSONSchemaName`, `WithJSONName`. |

## Testing with guardytest

Use **guardy/guardytest** for unit tests:

- **FakeValidator(name, *guardy.Result)** — validator that always returns the given result (nil = zero Result).
- **FailingValidator(name, err)** — validator that always returns the given error.
- **MustPass**, **MustBlock**, **MustRedact**, **MustOverride**, **MustRetry** — assert `report.FinalAction`.
- **NewInput(text)** — shorthand for `guardy.Input{Text: text}`.
- **InputBuilder** — fluent builder for Input (Text, Metadata, Documents, Messages).

Example:

```go
v := guardytest.FakeValidator("mock", &guardy.Result{Passed: false, Action: guardy.Block, Code: "TEST"})
pipeline := guardy.NewPipeline(guardy.WithTier1(v))
report, _ := pipeline.Run(ctx, guardytest.NewInput("x"))
guardytest.MustBlock(t, report)
```

## Error handling (for AI)

- **ErrBlocked** — returned by GuardWriter when the pipeline result is Block.
- **ErrOverridden** — returned by GuardWriter when the result is Override (not commonly used in stream).
- **ErrValidatorFailed** — wraps a validator’s system error; returned by `Run` when `WithFailOpen(false)` and a validator returns an error.

To know **which validator failed**: when `Run` returns `err == nil`, inspect `report.Results`. Each element corresponds to a validator (by tier order); use `Result.Code`, `Reason`, `Evidence`, `Guidance` to show a user-friendly message (e.g. “You used a forbidden word”). Use `errors.Is(err, guardy.ErrBlocked)` when handling GuardWriter or middleware errors.

## Packages

- **guardy** — core types (Action, Input, Result, Report, Report.WorstReason), Validator, ConditionalValidator, Pipeline, PipelineHandler, PipelineMiddleware, GuardWriter, Guard middleware, errors.
- **guardy/ext** — Length, Wordlist, Regex, JSONSchema.
- **guardy/guardytest** — FakeValidator, FailingValidator, MustPass/MustBlock/…, NewInput, InputBuilder.

See [.cursor/docs/TD.md](.cursor/docs/TD.md) for the full technical design.

## Migration (breaking changes)

- **ext.JSONSchema**: Use `ext.NewJSONSchema(schemaJSON, code, opts)` or `ext.MustJSONSchema(...)`. On invalid JSON or schema mismatch always returns Retry with Guidance (and Reason, Evidence).
- **Input**: New field `Messages []Message` for context-aware validation; optional.
- **GuardWriter**: Chunking is semantic and UTF-8-safe; no mid-rune splits.

## Development

```bash
make test
make lint
```

## License

See [LICENSE](LICENSE).
