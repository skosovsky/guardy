# Guardy

Universal AI Guardrails — pipeline engine for validation, intervention actions (block, redact, override, retry), and tiered execution.

## Description

Guardy is an agnostic, business-logic-free pipeline for filtering and protecting AI systems. It orchestrates validators (regex, wordlists, length, JSON, or custom) in tiers, aggregates results, and applies standardized intervention strategies.

## Requirements

- Go 1.26+

## Installation

```bash
go get github.com/skosovsky/guardy
```

## Usage

### Build a pipeline

```go
import (
    "github.com/skosovsky/guardy"
    "github.com/skosovsky/guardy/ext"
)

regexV, _ := ext.NewRegex(`(?i)(ignore previous|system prompt)`, guardy.Block, "PROMPT_INJECTION")
piiV, _ := ext.NewRegex(`\d{3}-\d{3}-\d{4}`, guardy.Redact, "PII", ext.WithRegexPlaceholder("[PHONE]"))

pipeline := guardy.NewPipeline(
    guardy.WithFailFast(true),
    guardy.WithTier1(regexV, piiV),
)
```

### Run the pipeline

```go
ctx := context.Background()
input := guardy.Input{Text: userMessage, Documents: ragChunks}
report, err := pipeline.Run(ctx, input)
if err != nil {
    // system error
}
switch report.FinalAction {
case guardy.Block:
    return errors.New("blocked")
case guardy.Retry:
    return retryWithReason(report.Results[0].Reason)
case guardy.Override:
    return report.OverrideText, nil
case guardy.Pass, guardy.Redact:
    return report.FinalText, nil
}
```

### HTTP middleware

```go
extractor := func(r *http.Request) (guardy.Input, error) {
    body, _ := io.ReadAll(r.Body)
    return guardy.Input{Text: string(body)}, nil
}
handler := guardy.Guard(pipeline, extractor)(yourHandler)
```

### Streaming

```go
gw := guardy.NewGuardWriter(w, pipeline, guardy.WithChunkSize(4096))
_, _ = gw.Write(data)
_ = gw.Close()
```

## Packages

- **guardy** — core types, Validator interface, Pipeline, middleware, GuardWriter
- **guardy/ext** — built-in validators: Regex, Wordlist, Length, JSON
- **guardy/guardytest** — test helpers: FakeValidator, FailingValidator, MustPass, MustBlock

## API overview

- **Action**: Pass, Redact, Override, Retry, Block
- **Validator**: `Validate(ctx, input) (Result, error)`, `Name() string`
- **Pipeline**: `NewPipeline(opts...)`, `Run(ctx, input) (Report, error)`
- **Report**: Results, FinalAction, FinalText, OverrideText

See [.cursor/docs/TD.md](.cursor/docs/TD.md) for the full technical design.

## Development

```bash
make test
make lint
```

## License

See [LICENSE](LICENSE).
