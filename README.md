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
	lengthV := ext.NewLength(0, 2048, guardy.ActionBlock, "TOO_LONG")
	wordlistV := ext.NewWordlist([]string{"bad", "spam"}, ext.Blocklist, guardy.ActionBlock, "FORBIDDEN")
	piiV := ext.NewPIIMasking()

	pipeline := guardy.NewPipeline(
		guardy.WithFastPath(ext.MustTagSanitizer(""), piiV, wordlistV, lengthV),
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

For string validation: `Validator[string]`. The pipeline returns the mutated text as the first value; on **ActionRedact** the validator provides the cleaned string. Report holds **Action** (`ActionPass`, `ActionBlock`, `ActionRedact`, `ActionRetry`), **Validator** name, **Reason**, **Feedback** (for LLM on Retry), **MutatedText**, **Score**, **ShadowMode**.

### Pipeline (two-phase)

- **Construction**: `NewPipeline[string](WithFastPath(...), WithSlowPath(...))`.
- **Execution**: `Run(ctx, text)` returns `(RunResult[string], error)`. Use `result.Output` for mutated text and `result.Decision()` for the outcome Report. `result.Reports` holds all validator reports for telemetry.

> Warning
> `pipeline.Use()` mutates internal cached state and is not safe to call concurrently with `Run()` or `GuardWriter`.
> Configure the pipeline during initialization, then share the fully configured instance across goroutines.

**Phase 1 — Fast path (sequential)**  
Validators that may **redact** or **block** run one after another. The text is passed along the chain; each redact step replaces it with `MutatedText`. On **block** (and not shadow), the pipeline returns immediately. Use for: TagSanitizer, PIIMasking, Wordlist, Regex, Length.

**Phase 2 — Slow path (parallel)**  
Heavy validators that only **block** or **pass** run in parallel via `errgroup` on the final text from phase 1. **Decision()** priority: `Block > Retry > last Redact > last Pass`. Context is cancelled only on **Block** (not Retry) so all reports are collected. On validator error, a **partial RunResult** with gathered reports is returned (telemetry preserved). Use for: SemanticValidator, LLMJudge.

**Recommended order in fast path:** WAF (TagSanitizer) → PII (PIIMasking) → Wordlist → Regex/Length.

### Report

**Report** holds: **Action** (`ActionPass`, `ActionBlock`, `ActionRedact`, `ActionRetry`), **Validator**, **Reason**, **Feedback** (for LLM on Retry), **Score**, **ShadowMode**, **MutatedText**. After `Run`, use the first return value (mutated text) and `report.Action`. **ActionRetry** with **Feedback** supports validation loops where the LLM can retry with the feedback message.

### Stream (GuardWriter)

Use **GuardWriter** to validate streaming output in chunks:

- Buffers until a semantic boundary (space, newline, punctuation) or a delimiterless hard cap is reached; runs the pipeline on each chunk. UTF-8 safe; index-based buffering; overlap prevents boundary bypass.
- On **Block** — returns `ErrBlocked`.
- On **Retry** — returns `ErrRetryRequested` (orchestrator should retry with Feedback).
- On **Redact** — writes the mutated text for that chunk.
- On **Pass** — writes the original chunk.

Options: **WithChunkSize** (preferred natural boundary target, default 4096), **WithMaxChunkSize** (delimiterless hard cap, default 2048), **WithContext**, **WithTimeout**.

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

### Middleware (Guard)

**Guard** wraps an HTTP handler: the request body is read once; the extractor turns it into text for the pipeline. On **Block** or **Retry** — 422 JSON response. On **Redact** — replaces body with `MutatedText` and calls next. On **Pass** — restores the **original** request body (not the extractor’s return value) and calls next. Use **ReportFromContext(ctx)** in the next handler to get the report.

```go
extractor := func(r *http.Request) (string, error) {
	body, _ := io.ReadAll(r.Body)
	return string(body), nil
}
handler := guardy.Guard(pipeline, extractor, guardy.PlainTextInjector())(yourHandler)
```

## Built-in validators (ext)

| Validator       | Description |
|----------------|-------------|
| **TagSanitizer** | Blocks on system-tag injection (e.g. `<system>`, `</system>`). `ext.NewTagSanitizer(pattern)` or `ext.MustTagSanitizer("")` for default pattern. |
| **PIIMasking**   | Redacts email, phone, credit card. `ext.NewPIIMasking()`, options: `WithPIIReplacement`, `WithPIIName`. |
| **Wordlist**     | Blocklist or allowlist; block or redact. `ext.NewWordlist(words, mode, action, code)`, options: `WithWordlistRedaction`, `WithWordlistLowercase`, `WithWordlistName`. |
| **Regex**        | Match pattern; block or redact. `ext.NewRegex(pattern, action, code)`, options: `WithRegexRedaction`, `WithRegexName`. |
| **Length**       | Min/max rune length. `ext.NewLength(min, max, action, code)`, option: `WithLengthName`. |
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

For the common case, prefer generating the schema from a Go struct:

```go
package main

import jsonschemaext "github.com/skosovsky/guardy/ext/jsonschema"

type User struct {
	Name string `json:"name" jsonschema:"required"`
	Age  int    `json:"age" jsonschema:"minimum=18"`
}

validator, _ := jsonschemaext.NewValidatorFromStruct(&User{})
```

`NewValidatorFromStruct` keeps the schema in sync with your Go type and still returns `ActionRetry` with detailed `Feedback` for invalid JSON or schema violations.

### Map (Lens adapter)

Use **Map[T,U]** to adapt `Validator[U]` to `Validator[T]` for domain structs:

```go
type AgentState struct { Text string }
regexV, _ := ext.NewRegex(`(?i)bad`, guardy.ActionRedact, "X", ext.WithRegexRedaction("[REDACTED]"))
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
- **guardy/ext** — TagSanitizer, PIIMasking, Wordlist, Regex, Length.
- **guardy/ext/jsonschema** — optional JSON Schema validator with raw-schema and struct-derived constructors.
- **guardy/guardytest** — FakeValidator, FailingValidator, MustPass/MustBlock/MustRedact.

See [.cursor/docs/task7.md](.cursor/docs/task7.md) for the full technical specification.

## Development

```bash
make test
make lint
```

## License

See [LICENSE](LICENSE).
