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
	result, err := pipeline.Run(ctx, nil, text)
	if err != nil {
		panic(err)
	}
	decision := result.PolicyDecision()
	switch {
	case decision.IsTerminal():
		fmt.Println("blocked:", decision.SafeMessage)
	case decision.IsRetryable():
		fmt.Println("retry:", decision.RetryFeedback)
	default:
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

For string validation: `Validator[string]`. The pipeline returns the mutated text as the first value; on **ActionRedact** the validator provides the cleaned string. **Report** holds **Action**, **Validator**, **Code**, **Severity**, **Reason**, **Feedback**, **Retryable**, **Fatal** (hard escalation), **SafeUserMessage**, **MutatedText**, **Score**, **ShadowMode**, **Disposition** (typed control flow), **PayloadKind** (output classification). Route control flow with **IsTerminalDeny()** and **IsRetryableCorrection()** — not `strings.Contains` on **Reason** or raw **Action**. **Action** remains for telemetry and redact semantics. Helpers: `ShouldStop()` / `ShouldRetry()` (aliases), `PublicMessage()` (safe UI), `OrchestratorMessage()` (LLM retry hints).

### Pipeline (two-phase)

- **Construction**: `NewPipeline[string](WithFastPath(...), WithPolicyValidators(...), WithSlowPath(...))`.
- **Execution**: `Run(ctx, scope, input)` returns `(RunResult[T], error)`. Pass `nil` or any `ExecutionScope` implementation. Use `result.PolicyDecision()` for low-level pipeline routing; use `GuardedArgs`, `GuardedJSONArgs`, `GuardDelivery`, or `GuardOutput` at host boundaries. `result.OutputKind`, `result.Decision()`, and `result.Reports` remain validator-level telemetry. Policy validators declare required scope at compile time; missing keys fail closed with `ErrScopeIncomplete` plus `ScopeIncompleteError` metadata.

`pipeline.Use()` is immutable in v2-style API: it returns a new pipeline instance and does not mutate the original.

### Struct pipelines (`Pipeline[MyDTO]`)

Use `NewPipeline[MyDTO](...)` when the payload is a struct (tool calls, agent state), not only `string`:

```go
type AgentCall struct {
    ToolArgs json.RawMessage `json:"tool_args"`
}
piiV := ext.NewPIIValidator(ext.WithAction(guardy.ActionRedact), ext.WithCode("PII"))
rawV := guardy.MapJSONRawMessage(piiV,
    func(c *AgentCall) json.RawMessage { return c.ToolArgs },
    func(c *AgentCall, raw json.RawMessage) *AgentCall { c.ToolArgs = raw; return c },
)
pipeline := guardy.NewPipeline[AgentCall](guardy.WithFastPath(rawV))
result, _ := pipeline.Run(ctx, nil, AgentCall{ToolArgs: json.RawMessage(`{"email":"a@b.com"}`)})
// result.Output.ToolArgs — redacted when ActionRedact
```

For string fields on structs use **Map**; for nested keys inside JSON text use **ext/jsonredact** on `Pipeline[string]`. Full example: [`examples/agent_tool_args`](examples/agent_tool_args/main.go). Policy rules: `PolicyValidator[MyDTO]` + explicit `ExecutionScope` in `Run`.

**Phase 1 — Fast path (sequential)**
Validators that may **redact** or **block** run one after another. The text is passed along the chain; each redact step replaces it with `MutatedText`. On **block** (and not shadow), the pipeline returns immediately. Use for: TagSanitizerValidator, PIIValidator, WordlistValidator, RegexValidator, LengthValidator.

**Phase 2 — Slow path (parallel)**
Heavy validators that only **block** or **pass** run in parallel via `errgroup` on the final text from phase 1. **Decision()** priority: `Block > Retry > last Redact > last Pass`. Context is cancelled only on **Block** (not Retry) so all reports are collected. On validator error, a **partial RunResult** with gathered reports is returned (telemetry preserved). Use for: SemanticValidator, LLMJudge.

**Recommended order in fast path:** WAF (TagSanitizerValidator) → PII (PIIValidator) → WordlistValidator → RegexValidator/LengthValidator.

### Report

**Report** holds validator telemetry and low-level rule output: **Action**, **Code**, **Reason**, **Feedback**, **Disposition**, **PayloadKind**, and related fields. Low-level `Run` callers should use `result.PolicyDecision()` or `errors.As(err, &policyFailure)` into `*PolicyFailure`; host boundaries should prefer `GuardedArgs`, `GuardedJSONArgs`, `GuardDelivery`, or `GuardOutput`. Use `result.Decision()` when you need the underlying report for telemetry or custom validators. Do not route control flow by parsing `Code` or `Reason`.

### Stream (GuardWriter)

Use **GuardWriter** to validate streaming output in chunks:

- Buffers until a semantic boundary (space, newline, punctuation) or a delimiterless hard cap is reached; runs the pipeline on each chunk. UTF-8 safe; index-based buffering; overlap prevents boundary bypass.
- On **terminal deny** or **retryable correction** — returns `*StreamError` that exposes `PolicyFailure` through `errors.As`; use `failure.Decision` for routing. `errors.Is` still works through `Unwrap` to `ErrBlocked` / `ErrRetryRequested`. Terminal retry maps to block-style `StreamError`.
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

var failure *guardy.PolicyFailure
if errors.As(err, &failure) {
    log.Println(failure.Decision.Code, failure.Decision.Disposition, failure.Decision.SafeMessage)
}
```

See `examples/streaming_filter` and `examples/json_streaming`.

### HTTP Guard (`http_guard.go`)

**Guard** wraps an HTTP handler: the request body is read once; the extractor turns it into text for the pipeline. On **terminal deny** or **retryable correction** — 422 JSON response. On **Redact** — replaces body with `MutatedText` and calls next. On **Pass** — restores the **original** request body (not the extractor’s return value) and calls next. For host-boundary routing, use `Decision`, `PolicyFailure`, typed guard events, or the generic wrapper APIs instead of request-context report state.

```go
extractor := func(r *http.Request) (string, error) {
	body, _ := io.ReadAll(r.Body)
	return string(body), nil
}
handler := guardy.Guard(pipeline, extractor, guardy.PlainTextInjector())(yourHandler)
```

### Policy validators (scope-aware)

Declare typed scope requirements with `ScopeKey[T]`, then pass any `ExecutionScope` implementation at run time. Use `NewScope(ScopeValue(...))` for static bindings, or expose host-owned structs through `ScopeFunc`. `MapScope` remains a low-level convenience, not the primary integration contract.

```go
roleKey := guardy.NewScopeKey[string]("principal.role")
pipeline := guardy.NewPipeline(
    guardy.WithPolicyValidators(
        guardy.NewTypedAttributeEquals[string, string](roleKey, "viewer"),
    ),
)

scope := guardy.NewScope(guardy.ScopeValue(roleKey, "viewer"))
result, err := pipeline.Run(ctx, scope, "hello")
decision := result.PolicyDecision()
```

Register rules with `WithPolicyValidators` (runs after fast-path, before slow-path). Built-in typed builders: `NewTypedAttributeEquals`, `NewTypedAttributePresent`. Custom rules can use `NewPolicyFuncWithScope`. Missing scope keys fail closed with `ErrScopeIncomplete`; use `errors.As` into `*ScopeIncompleteError` or `MissingScopeKeys(err)` for machine-readable missing keys. See `examples/policy_attributes`.

### Canonical boundary contracts

Use `Decision` and `PolicyFailure` at host boundaries. Guardy errors from decode, interceptors, stream, and guarded output expose `*PolicyFailure` through `errors.As`, while sentinel checks still work through `errors.Is`.

```go
payload, err := argsPipeline.Validate(ctx, scope, raw)
if err != nil {
    var failure *guardy.PolicyFailure
    if errors.As(err, &failure) && failure.Decision.IsRetryable() {
        return failure.Decision.RetryFeedback
    }
    return err
}
```

For typed arguments, compile a raw-first pipeline once and let guardy return one boundary object:

```go
type Command struct {
    Name string `json:"name"`
}

argsPipeline := guardy.MustCompileArgs[Command](rawPipeline)
args, err := argsPipeline.Validate(ctx, scope, `{"name":"Ada"}`)
// args is GuardedArgs[Command]: Value, Raw, SanitizedRaw, Reports,
// PayloadKind, and the canonical Decision stay together.
```

For dynamic JSON arguments, keep sanitized raw JSON, decoded object, schema identity, reports, and decision together:

```go
schema := guardy.JSONArgsSchemaFunc{ID: "command.schema"}
jsonArgsPipeline := guardy.MustCompileJSONArgs(rawPipeline, schema)
args, err := jsonArgsPipeline.Validate(ctx, scope, rawJSON)
// args is GuardedJSONArgs: Raw, SanitizedRaw, Object, SchemaID, Reports,
// PayloadKind, and Decision.
```

Wrap dynamic handlers with the same boundary object:

```go
handler := guardy.WrapGuardedJSONArgs(jsonArgsPipeline, scope,
    func(ctx context.Context, args guardy.GuardedJSONArgs) (string, error) {
        return args.SchemaID + ":" + args.Object["name"].(string), nil
    },
)
result, args, err := handler(ctx, rawJSON)
```

For guarded output, return a single authoritative delivery contract. `GuardOutput` uses the default external-user policy; `GuardDelivery` accepts an explicit channel policy:

```go
guarded, err := outputPipeline.GuardDelivery(
    ctx,
    scope,
    guardy.NewDeliveryPolicy("external", guardy.WithDeliveryFallback("Blocked.")),
    text,
)
if value, ok := guarded.DeliverableValue(); ok {
    send(value)
}
```

Generic adapters are available for host functions: `WrapArgs` validates raw arguments before calling a typed handler, `WrapGuardedArgs` passes the full `GuardedArgs[T]` boundary to a handler, `WrapGuardedJSONArgs` does the same for dynamic JSON, and `WrapGuardedOutput` validates handler output before returning `GuardedOutput[T]`.

Observers receive typed guard events:

```go
pipeline := guardy.NewPipeline(
    guardy.WithPipelineName[string]("reply-output"),
    guardy.WithObserver[string](func(ctx context.Context, event guardy.GuardEvent) {
        log.Println(event.PipelineName, event.Phase, event.Decision.Code)
    }),
)
```

For routing, project canonical decisions through guardy instead of reinterpreting action/disposition combinations:

```go
route := failure.Decision.Route(guardy.RemediationPolicy{
    RetryAttempt: 2,
    MaxRetries:   3,
})
switch route.Outcome {
case guardy.GuardRouteRetryCorrection:
    retry(route.RetryFeedback)
case guardy.GuardRouteTerminalDeny, guardy.GuardRouteSystemFault:
    stop(route.SafeMessage)
}
```

### User channel (`WithUserChannel`)

For output guards, enable terminal filtering so non-safe `PayloadKind` is blocked inside the library:

```go
pipeline := guardy.NewPipeline(
    guardy.WithUserChannel[string](),
    guardy.WithUserChannelFallback[string]("Sorry, I can't show that."),
    guardy.WithFastPath(classifier),
)
```

Validators may set `Report.PayloadKind` (`PayloadSafeUserText`, `PayloadInternalControlSignal`, `PayloadTechnicalPayload`). `RunResult.OutputKind` aggregates the most restrictive kind for any `T`. For delivery boundaries prefer `pipeline.GuardDelivery(ctx, scope, policy, value)` or `pipeline.GuardOutput(ctx, scope, value)`, which return guardy-owned delivery contracts and remove the need for host-side re-gating or JSON sniffing.

### Declarative guards (`guardy/build`)

Compile intent without wiring ext validators manually:

```go
import "github.com/skosovsky/guardy/build"

pipeline, err := build.CompileStringGuard(build.GuardSpec{
    WordlistBlock: []string{"bad"},
    PIIRedact:     true,
    LengthMax:     4096,
}, build.WithJSONSchema(schemaBytes))

// Output guards: user channel + technical JSON classifier
outPipeline, err := build.CompileStringGuard(build.GuardSpec{},
    build.WithUserChannel(),
    build.WithUserChannelFallback("Output blocked."),
    build.WithOutputClassifier(),
)
```

See `examples/declarative_guard`.

**Sensitivity levels** (`build.SensitivityStrict`, `SensitivityNormal`, `SensitivityPermissive`): Strict enables PII redaction and tightens `LengthMax` when set; Permissive keeps only explicit wordlist/policy rules; Normal uses spec fields as-is.

### Generic decorators (`interceptor.go`)

**WrapInput** runs a pipeline on the request value before your `func(context.Context, Req) (Res, error)`. **WrapOutput** runs after your function on the result. Both take an `ExecutionScope` argument (use `nil` when no policy keys are required). Terminal deny returns **\*BlockError**; retryable correction returns **RetryError**. Both expose **PolicyFailure** through `errors.As`. For raw typed arguments use **WrapArgs** or **WrapGuardedArgs**. For dynamic JSON handlers use **WrapGuardedJSONArgs**. For output delivery contracts use **WrapGuardedOutput**. See `examples/generic_decorator`.

## Built-in validators (ext)

| Validator                   | Description                                                                                                                                                                                             |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **TagSanitizerValidator**   | Blocks on system-tag injection (e.g. `<system>`, `</system>`). `ext.NewTagSanitizerValidator(pattern)` or `ext.MustTagSanitizerValidator("")`.                                                          |
| **PIIValidator**            | Redacts or blocks email, phone, credit card. `ext.NewPIIValidator(...)` with `ext.WithAction`, `ext.WithCode`, `ext.WithSeverity`, `ext.WithRedactionReplacement`, `ext.WithTokenVault`.                |
| **WordlistValidator**       | Blocklist or allowlist; block or redact. `ext.NewWordlistValidator(words, mode, ...)` with `ext.WithAction`, `ext.WithCode`, `ext.WithLowercase`, `ext.WithRedactionReplacement`, `ext.WithTokenVault`. |
| **RegexValidator**          | Match pattern; block or redact. `ext.NewRegexValidator(pattern, ...)` with `ext.WithAction`, `ext.WithCode`, `ext.WithSeverity`, `ext.WithRedactionReplacement`.                                        |
| **LengthValidator**         | Min/max rune length. `ext.NewLengthValidator(min, max, ...)` with `ext.WithCode`, `ext.WithSeverity`, `ext.WithName`.                                                                                   |
| **TechnicalJSONClassifier** | Classifies tool-call JSON as `PayloadTechnicalPayload` for [WithUserChannel]. `ext.NewTechnicalJSONClassifier(...)` with `ext.WithCode`.                                                                |
| **JSON Schema**             | Optional submodule `guardy/ext/jsonschema` — validates JSON strings against a schema; returns **ActionRetry** with **Feedback** on violation.                                                           |
| **JSON Redact**             | Submodule `guardy/ext/jsonredact` — recursive redact on JSON string leaves via a `Validator[string]` leaf validator.                                                                                    |

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
result, _ := guardy.NewPipeline(guardy.WithFastPath(piiV)).Run(ctx, nil, "email: a@b.com")
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

### MapJSONRawMessage (`json.RawMessage` fields)

Use **MapJSONRawMessage** when a struct field holds opaque JSON (tool calling, structured outputs). `extract` and `inject` must be non-nil (panics if nil). It skips `nil`, empty, and exact JSON `null` literals (not whitespace-padded `" null "`), runs a `Validator[string]` on the raw text, and after **ActionRedact** only calls `inject` when `json.Valid` succeeds. Broken redaction returns **ActionRetry** with **CodeJSONRedactCorrupted** (`JSON_REDACT_CORRUPTED`) and **Retryable** (pipeline contract; not `RetryError`).

```go
type AgentCall struct {
    ToolArgs json.RawMessage `json:"tool_args"`
}
piiV := ext.NewPIIValidator(ext.WithAction(guardy.ActionRedact), ext.WithCode("PII"))
v := guardy.MapJSONRawMessage(piiV,
    func(c *AgentCall) json.RawMessage { return c.ToolArgs },
    func(c *AgentCall, raw json.RawMessage) *AgentCall { c.ToolArgs = raw; return c },
)
pipeline := guardy.NewPipeline(guardy.WithFastPath(v))
```

Use `T` as a struct value (`Validator[AgentCall]`). For nested keys inside JSON, use `ext/jsonredact` instead. See `examples/agent_tool_args`.

## Core validators (guardy)

- **SemanticValidator** — wraps a `Matcher` and threshold; use for similarity/embedding checks (slow path).
- **LLMJudge** — wraps a `Judge`; use for LLM-as-judge (slow path). Both support **shadow mode** (block is logged but does not short-circuit).

## Testing with guardytest

Use **guardy/guardytest** for unit tests:

- **FakeValidator(name, *guardy.Report)** — validator that always returns the given report (nil or zero = pass).
- **FailingValidator(name, err)** — validator that always returns the given error.
- **MustPass**, **MustBlock**, **MustRedact**, **MustRetry** — assert `report.Action`.
- **MustTerminalDeny**, **MustRetryableCorrection**, **MustSystemFault** — assert validator report disposition when a test needs report-level details.
- **MustOutputKind** — assert `RunResult.OutputKind` (user channel / classifier tests).
- **MustScopeIncomplete** — assert `errors.Is(err, ErrScopeIncomplete)`.

```go
v := guardytest.FakeValidator("mock", &guardy.Report{Action: guardy.ActionBlock, Reason: "TEST"})
pipeline := guardy.NewPipeline(guardy.WithFastPath(v))
result, _ := pipeline.Run(ctx, nil, "x")
if !result.PolicyDecision().IsTerminal() {
    t.Fatal("expected terminal decision")
}
```

## Error handling

- **PolicyFailure** — canonical boundary error contract; use `errors.As(err, &failure)` and route by `failure.Decision`.
- **BlockError** — block from WrapInput or WrapOutput; unwraps to **ErrBlocked** and carries **Failure PolicyFailure**.
- **ValidatorFaultError** — validator/pipeline infrastructure failure; unwraps to **ErrValidatorFailed** and carries **Failure PolicyFailure**.
- **StreamError** — returned by GuardWriter on block/retry; unwraps to **ErrBlocked** or **ErrRetryRequested** and carries **Failure PolicyFailure**.
- **ErrBlocked** — block decisions (Guard, WrapInput, StreamError).
- **ErrRetryRequested** — retry decisions (WrapOutput, StreamError, RetryError).
- **RetryError** — structured retry from interceptors and typed argument validation; unwraps to **ErrRetryRequested** and carries **Failure PolicyFailure**.
- **ErrScopeIncomplete** — `Run` called without required policy scope keys.
- **ErrValidatorFailed** — wraps a validator’s system error from `Run`; prefer `errors.As` into **PolicyFailure** or **ValidatorFaultError**.

Use `PolicyFailure.Decision` or `RunResult.PolicyDecision()` for control flow - not string parsing on `Code` or `Reason`, and not local re-derivation from `Report`. Error report details are telemetry snapshots via `ReportSnapshot()`, not the boundary contract.

Production `ext` validators should always set **`ext.WithCode(...)`** so hosts never parse `Reason` strings.

## Packages

- **guardy** — core types (Action, Report, Decision, PolicyFailure, PayloadKind, Validator), Pipeline, typed scope, ArgsPipeline, JSONArgsPipeline, GuardedArgs, GuardedJSONArgs, GuardedOutput, GuardedDelivery, DeliveryPolicy, GuardEvent, GuardRoute, GuardWriter, Guard middleware, errors.
- **guardy/build** — declarative `GuardSpec` → `CompileStringGuard` (imports ext; core stays clean).
- **guardy/ext** — TagSanitizerValidator, PIIValidator, WordlistValidator, RegexValidator, LengthValidator, TokenVault, MapSlice, MLValidator, NewTechnicalJSONClassifier (output PayloadKind for user channel).
- **guardy/ext/jsonschema** — optional JSON Schema validator with raw-schema and struct-derived constructors.
- **guardy/ext/guardyotel** — optional OTel middleware module (metrics + tracing).
- **guardy/guardytest** — FakeValidator, FailingValidator, MustPass/MustBlock/MustRedact/MustRetry, MustTerminalDeny/MustRetryableCorrection/MustSystemFault, MustOutputKind, MustScopeIncomplete.

See [.cursor/docs/task9.md](.cursor/docs/task9.md) for the full v2 technical specification.

## Migration (task14/task16 — typed scope, boundary contracts, delivery routing)

- **Breaking:** `Run(ctx, scope, input)` — remove `WithAttributes` / `AttributesFromContext`; declare `ScopeKey[T]` requirements and pass a host `ExecutionScope`.
- **Fail-closed policy:** `RequiredScope()` compiled at pipeline construction; missing keys → `ErrScopeIncomplete` + `ScopeIncompleteError` before fast-path.
- **Decision:** route with `RunResult.PolicyDecision()` and `PolicyFailure.Decision`, not local parsing or local disposition derivation from `Report`.
- **Output contract:** use `GuardDelivery` / `GuardOutput` / `GuardedOutput[T]` for delivery boundaries, not plain strings plus `OutputKind` flags or post-guard JSON sniffing.
- **Typed arguments:** use `CompileArgs[T]` / `ArgsPipeline[T]` / `GuardedArgs[T]`; raw validation plus local decode was removed from the public path.
- **Dynamic JSON arguments:** use `CompileJSONArgs` / `JSONArgsPipeline` / `GuardedJSONArgs` when the handler cannot bind to a static Go type.
- **Observer telemetry:** `WithObserver` receives `GuardEvent` with scope, phase, decision, pipeline identity, payload kind, report, and safe telemetry metadata.
- **Decision routing:** use `Decision.Route(RemediationPolicy)` or `RouteDecision` for retry, terminal deny, system fault, and fallback projection.
- **HTTP report context:** `ReportFromContext` was removed; report context side channels are replaced by explicit decisions, policy failures, guard events, and boundary values.
- **WrapInput/WrapOutput:** add `scope` parameter (pass `nil` when unused); raw-args and output-boundary wrappers are `WrapArgs`, `WrapGuardedArgs`, `WrapGuardedJSONArgs`, and `WrapGuardedOutput`.
- **Validators:** use `FinishReport` or `ext.FinalizeRuleReport` for `ActionRetry` so `Retryable` defaults are applied; raw `ActionRetry` without defaults is treated as terminal deny.
- **Declarative guards:** `github.com/skosovsky/guardy/build` — JSON Schema via `build.WithJSONSchema`, not in core.

## Migration from v2 (task11 — Policy & Safety Engine)

Type-safe redaction patterns: see [task11-redaction.md](.cursor/docs/task11-redaction.md) (`ArgsPipeline`, `Map`, `MapJSONRawMessage`).

- **Decision control flow:** use `Decision` / `PolicyFailure`; `Report` remains validator telemetry.
- **Policy phase:** `WithPolicyValidators` + typed `ScopeKey[T]` requirements + explicit `ExecutionScope` in `Run`.
- **Typed arguments:** `ArgsPipeline` + `GuardedArgs[T]` replaces raw pipeline plus local decode.
- **Streaming:** `errors.As(err, &failure)` where `failure` is `*PolicyFailure`.
- **JSON redact:** `guardy/ext/jsonredact` (separate module; optional).
- **ext options:** `WithCode` required for production; `WithRetryable`, `WithFatal`, `WithSafeUserMessage` as needed.

## Migration (task12 — stream, policy shadow, post-bind)

- **GuardWriter:** use `errors.As(err, &failure)` into `*PolicyFailure` instead of relying only on `errors.Is(ErrBlocked)`.
- **Policy shadow:** shadow policy blocks no longer stop the pipeline; register `WithObserver` for telemetry.
- **PostBindValidator:** business rules after bind with `CodePostBindViolation` + `RetryError`.
- **jsonschema codes:** default schema violations use `CodeJSONSchemaInvalid` (`JSON_SCHEMA_INVALID`).

## Migration (task13 — `MapJSONRawMessage`)

- **Broken JSON after redact:** branch on `CodeJSONRedactCorrupted`, not `CodeJSONInvalid` (parse/bind errors).
- **Struct tool args:** `NewPipeline[MyDTO]` + `MapJSONRawMessage`; see `examples/agent_tool_args`.

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
