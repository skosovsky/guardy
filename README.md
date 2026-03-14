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
	report, err := pipeline.Run(ctx, text)
	if err != nil {
		panic(err)
	}
	switch report.Action {
	case guardy.ActionBlock:
		fmt.Println("blocked:", report.Reason)
	case guardy.ActionRedact:
		fmt.Println("ok (redacted):", report.MutatedText)
	case guardy.ActionPass:
		fmt.Println("ok:", report.MutatedText)
	}
}
```

## Key abstractions

### Validator

Any type implementing the **Validator** interface can be used in a pipeline:

```go
type Validator interface {
	Validate(ctx context.Context, text string) (Report, error)
	Name() string
}
```

Validators return a **Report** with **Action** (`pass`, `block`, `redact`), optional **MutatedText** (for redaction), **Validator** name, **Reason**, **Score**, and **ShadowMode**.

### Pipeline (two-phase)

- **Construction**: `NewPipeline(WithFastPath(...), WithSlowPath(...))`.
- **Execution**: `Run(ctx, text string)` returns `(Report, error)`.

**Phase 1 — Fast path (sequential)**  
Validators that may **redact** or **block** run one after another. The text is passed along the chain; each redact step replaces it with `MutatedText`. On **block** (and not shadow), the pipeline returns immediately. Use for: TagSanitizer, PIIMasking, Wordlist, Regex, Length.

**Phase 2 — Slow path (parallel)**  
Heavy validators that only **block** or **pass** run in parallel via `errgroup` on the final text from phase 1. Results are collected; a **block** (non-shadow) takes priority over infrastructure errors. Context is cancelled when a block is stored so other validators can exit early. Use for: SemanticValidator, LLMJudge.

**Recommended order in fast path:** WAF (TagSanitizer) → PII (PIIMasking) → Wordlist → Regex/Length.

### Report

**Report** holds: **Action** (`ActionPass`, `ActionBlock`, `ActionRedact`), **Validator**, **Reason**, **Score**, **ShadowMode**, **MutatedText**. After `Run`, use `report.Action` and `report.MutatedText` (safe text when redactions were applied).

### Stream (GuardWriter)

Use **GuardWriter** to validate streaming output in chunks:

- Buffers until chunk size or a semantic boundary (space, newline, punctuation); runs the pipeline on each chunk. UTF-8 safe.
- On **Block** — returns `ErrBlocked`.
- On **Redact** — writes `report.MutatedText` for that chunk.
- On **Pass** — writes the original chunk.

Options: **WithChunkSize** (default 4096), **WithContext**, **WithTimeout**.

```go
gw := guardy.NewGuardWriter(w, pipeline, guardy.WithChunkSize(4096))
_, _ = gw.Write(data)
_ = gw.Close()
```

### Middleware (Guard)

**Guard** wraps an HTTP handler: the request body is read once; the extractor turns it into text for the pipeline. On **Block** — 422 JSON response. On **Redact** — replaces body with `MutatedText` and calls next. On **Pass** — restores the **original** request body (not the extractor’s return value) and calls next. Use **ReportFromContext(ctx)** in the next handler to get the report.

```go
extractor := func(r *http.Request) (string, error) {
	body, _ := io.ReadAll(r.Body)
	return string(body), nil
}
handler := guardy.Guard(pipeline, extractor)(yourHandler)
```

## Built-in validators (ext)

| Validator       | Description |
|----------------|-------------|
| **TagSanitizer** | Blocks on system-tag injection (e.g. `<system>`, `</system>`). `ext.NewTagSanitizer(pattern)` or `ext.MustTagSanitizer("")` for default pattern. |
| **PIIMasking**   | Redacts email, phone, credit card. `ext.NewPIIMasking()`, options: `WithPIIReplacement`, `WithPIIName`. |
| **Wordlist**     | Blocklist or allowlist; block or redact. `ext.NewWordlist(words, mode, action, code)`, options: `WithWordlistRedaction`, `WithWordlistLowercase`, `WithWordlistName`. |
| **Regex**        | Match pattern; block or redact. `ext.NewRegex(pattern, action, code)`, options: `WithRegexRedaction` / `WithRegexPlaceholder`, `WithRegexName`. |
| **Length**       | Min/max rune length. `ext.NewLength(min, max, action, code)`, option: `WithLengthName`. |

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
report, _ := pipeline.Run(ctx, "x")
guardytest.MustBlock(t, &report)
```

## Error handling

- **ErrBlocked** — returned by GuardWriter when report.Action == ActionBlock.
- **ErrValidatorFailed** — wraps a validator’s system error from `Run`.

## Packages

- **guardy** — core types (Action, Report, Validator), Pipeline, SemanticValidator, LLMJudge, GuardWriter, Guard middleware, errors.
- **guardy/ext** — TagSanitizer, PIIMasking, Wordlist, Regex, Length.
- **guardy/guardytest** — FakeValidator, FailingValidator, MustPass/MustBlock/MustRedact.

See [.cursor/docs/TD.md](.cursor/docs/TD.md) for the full technical design.

## Development

```bash
make test
make lint
```

## License

See [LICENSE](LICENSE).
