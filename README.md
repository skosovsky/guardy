# Guardy

[![Go Reference](https://pkg.go.dev/badge/github.com/skosovsky/guardy.svg)](https://pkg.go.dev/github.com/skosovsky/guardy)
[![Build](https://img.shields.io/badge/build-go%20build-blue)](https://github.com/skosovsky/guardy)
[![Coverage](https://img.shields.io/badge/coverage-go%20test-green)](https://github.com/skosovsky/guardy)

Guardy validates, transforms and controls delivery of caller-owned data through
`Pipeline[T]`. It supports sequential fast checks, caller policies and parallel
read-only slow checks. Optional matchers and detector adapters can be used in LLM
applications; the library supplies no trained model or agent runtime.

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
	wordlistV := ext.MustWordlistValidator([]string{"bad", "spam"}, ext.Blocklist, ext.WithCode("FORBIDDEN"))
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

For string validation: `Validator[string]`. The pipeline returns the mutated text as the first value; on **ActionRedact** the validator provides the cleaned string. **Report** holds **Action**, **Validator**, **Code**, **Severity**, **Reason**, **Feedback**, **Retryable**, **Fatal** (hard escalation), **SafeUserMessage**, **MutatedText**, **Score**, **ShadowMode**, **Disposition** (typed control flow), **PayloadKind** (output classification). Route control flow with **IsTerminalDeny()** and **IsRetryableCorrection()** — not `strings.Contains` on **Reason** or raw **Action**. **Action** remains for telemetry and redact semantics. Helpers: `PublicMessage()` (safe UI), `OrchestratorMessage()` (LLM retry hints).

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
Heavy validators that only **block** or **pass** run in parallel via `errgroup` on the final text from phase 1. **Decision()** priority: `system fault > terminal deny > retryable correction > redact > pass`. Terminal deny or fault cancels sibling checks; correction does not. Only non-fatal shadow policy blocks are observations; shadow never suppresses faults. On validator error, a **partial RunResult** with gathered reports is returned (telemetry preserved). Use for: SemanticValidator, LLMJudge.

**Example fast-path order:** tag pattern matcher → PII matcher → wordlist → regex/length. Choose and test order for your rules; this ordering is not a measured protection level.

### Report

**Report** holds validator telemetry and low-level rule output: **Action**, **Code**, **Reason**, **Feedback**, **Disposition**, **PayloadKind**, and related fields. Low-level `Run` callers should use `result.PolicyDecision()` or `errors.As(err, &policyFailure)` into `*PolicyFailure`; host boundaries should prefer `GuardedArgs`, `GuardedJSONArgs`, `GuardDelivery`, or `GuardOutput`. Use `result.Decision()` when you need the underlying report for telemetry or custom validators. Do not route control flow by parsing `Code` or `Reason`.

### Stream release

Use `CompileStream` with an explicit profile and byte limits. `ReleaseWholeResponse`
checks the final value before the first release. `ReleaseValidatedUnits` emits
complete newline-delimited units (or JSON objects/arrays with `JSONValues`) and
requires declared unit-local rule capabilities. `ReleaseBestEffort` allows early
release of bounded UTF-8 chunks; it has no whole-value safety guarantee.

```go
stream, err := guardy.CompileStream(w, guardy.StreamConfig{
    Identity: "response",
    Profile: guardy.ReleaseWholeResponse,
    Pipeline: pipeline,
    Delivery: guardy.NewDeliveryPolicy("external"),
    MaxInputBytes: 1 << 20,
    MaxPendingBytes: 1 << 20,
    MaxUnitBytes: 1 << 20,
    MaxOutputBytes: 1 << 20,
    ValidationTimeout: time.Second,
})
if err != nil { return err }
if _, err := stream.WriteContext(ctx, data); err != nil { return err }
outcome, err := stream.Complete(ctx) // trusted producer success
// On disconnect/error: stream.Abort(cause). Close without Complete aborts.
```

Limits count bytes and include redaction expansion. Pending memory stays bounded;
validator-owned allocations are caller-owned. Unsupported mandatory capabilities
fail at construction. Completion is a trusted method call, never a payload flag.
The initial unit profile supports unit-local rules with zero cross-unit lookaround;
final-only or unknown rules require whole-response. JSON framing does not replace
schema/policy validation. No profile silently downgrades.

`ReleaseError` carries a terminal category and exposes `PolicyFailure` via
`errors.As`. Outcomes include actual received/released bytes, sequence and peak
pending bytes. Partial transport writes are irreversible and terminate; no automatic
replay occurs. `DeliverFallback` is a separate at-most-once checked delivery after
policy deny, never after validator fault. Observers see counters/stages, not payload.
Validators must honor context; non-cooperative validators cannot be forcibly stopped.

See `examples/streaming_filter`, `examples/json_streaming`, and [CONTRACTS.md](CONTRACTS.md).

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

`NewTypedAttributeEquals` uses Go equality. Interface values that contain slices,
maps or other incomparable operands fail with `ErrAttributeIncomparable` and
`AttributeComparisonError`, projected as a system fault by the pipeline. Use a custom
policy for domain-specific comparison. The untyped `NewAttributeEquals` API was removed.

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
// Metadata describes the schema; it does not validate the object.
metadata := guardy.JSONArgsMetadata{ID: "command.schema", Shape: callerShape}
checker := guardy.JSONArgsValidatorFunc(validateObject) // caller executable rules
jsonArgsPipeline := guardy.MustCompileJSONArgs(rawPipeline, checker,
    guardy.WithJSONArgsMetadata(metadata),
)
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

`CompileStringGuard` returns `ConfigurationError` (matching `ErrConfiguration`)
with a component, field path and code; its default text excludes schema and policy
values. Empty schema bytes explicitly supplied through `WithJSONSchema` are invalid;
`{}` is permitted. Omitting that option permits flows without schemas. Negative
`LengthMax` is invalid; zero disables the rule. Fallback requires `WithUserChannel`.
`PolicyAttributeDeepEqual` compares values using `reflect.DeepEqual`, preserving
types and supporting maps/slices. Core typed equality uses Go `==`.

`GuardSpec` selects rules explicitly: `PIIRedact`, `WordlistBlock`, `LengthMax`, and `PolicyRules`. There are no sensitivity/security presets or implicit rule changes. Fast rules run in the documented order; their composition does not establish detector accuracy.

### Generic decorators (`interceptor.go`)

**WrapInput** runs a pipeline on the request value before your `func(context.Context, Req) (Res, error)`. **WrapOutput** runs after your function on the result. Both take a `ScopeFactory` (use `nil` when no policy keys are required). Input facts are resolved before validation; output facts are resolved after a successful handler. The host owns coherent snapshots and execution authorization. Terminal deny returns **\*BlockError**; retryable correction returns **RetryError**. Both expose **PolicyFailure** through `errors.As`. For raw typed arguments use **WrapArgs** or **WrapGuardedArgs**. For dynamic JSON handlers use **WrapGuardedJSONArgs**. For output delivery contracts use **WrapGuardedOutput**. See `examples/generic_decorator`.

## Built-in validators (ext)

| Validator                   | Description                                                                                                                                                                                             |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **TagSanitizerValidator**   | Matches XML-like system tags (e.g. `<system>`, `</system>`). `ext.NewTagSanitizerValidator(pattern)` or `ext.MustTagSanitizerValidator("")`.                                                          |
| **PIIValidator**            | Redacts or blocks email, phone, credit card. `ext.NewPIIValidator(...)` with `ext.WithAction`, `ext.WithCode`, `ext.WithSeverity`, `ext.WithRedactionReplacement`, `ext.WithTokenVault`.                |
| **WordlistValidator**       | Blocklist or allowlist; block or redact. `ext.NewWordlistValidator(words, mode, ...)` returns `(Validator, error)`; `ext.MustWordlistValidator` is for static config. Use `ext.WithAction`, `ext.WithCode`, `ext.WithLowercase`, `ext.WithRedactionReplacement`, `ext.WithTokenVault`. |
| **RegexValidator**          | Match pattern; block or redact. `ext.NewRegexValidator(pattern, ...)` with `ext.WithAction`, `ext.WithCode`, `ext.WithSeverity`, `ext.WithRedactionReplacement`.                                        |
| **LengthValidator**         | Min/max rune length. `ext.NewLengthValidator(min, max, ...)` with `ext.WithCode`, `ext.WithSeverity`, `ext.WithName`.                                                                                   |
| **TechnicalJSONClassifier** | Heuristically marks tool-like JSON as `PayloadTechnicalPayload` for [WithUserChannel]. `ext.NewTechnicalJSONClassifier(...)` with `ext.WithCode`.                                                                |
| **JSON Schema**             | Optional submodule `guardy/ext/jsonschema` — validates JSON strings against a schema; returns **ActionRetry** with **Feedback** on violation.                                                           |
| **JSON Redact**             | Submodule `guardy/ext/jsonredact` — recursive redact on JSON string leaves via a `Validator[string]` leaf validator.                                                                                    |

### Matcher and detector contracts

Wordlist tokens are maximal runs of Unicode letters, numbers, combining marks
and underscore. Punctuation/whitespace delimit tokens. Every listed entry must be
one non-empty token. `NewWordlistValidator` returns an error for invalid entries/modes;
`MustWordlistValidator` panics and is intended for static configuration. `WithLowercase` applies Go
`strings.ToLower` to both entries and matched tokens; it is not full Unicode case
folding. There is no Unicode normalization, transliteration or confusable matching.
Detection and replacement use the same original spans. Wordlist, regex and PII
replacement strings are literal, including `$` and backslash.

PII matching has a finite format contract:

- ASCII unquoted email shapes with a dotted alphabetic TLD; no internationalized
  local parts, quoted mailboxes or address literals.
- NANP 10-digit numbers with optional `1`/`+1`; `+7` numbers with 3-3-2-2 grouping.
  Spaces, dots and hyphens are supported, with optional area-code parentheses.
  Other country codes and extensions are outside the matched span contract.
- Visa contiguous 13/16/19 digits and MasterCard 51–55 with 16 digits. A 16-digit
  card may use four groups with consistent spaces or hyphens. Other prefixes,
  families, lengths, mixed separators and grouped 13/19-digit forms are unsupported.

Matches require non-word boundaries (Unicode letters/numbers/marks/underscore
are word characters). These are shape checks, not issuance, Luhn, mailbox or
telephone validity checks. IDs that look like supported numbers can be false
positives. All spans are detected on original text; overlaps merge before
replacement or vault storage, and generated replacements/tokens are not rescanned
by that validator. An unsupported format can still contain a supported substring
which is replaced, leaving other parts unchanged (for example, a 16-digit group
inside a longer grouped number, or a base phone before an extension). Unsupported
formats have no whole-value redaction guarantee. This is not global PII coverage
or a DLP product.

`TextClassifier.Classify(context.Context, string)` is synchronous and caller
supplied. Cancellation is cooperative; the adapter starts no background workers
and cannot stop a callback that ignores context. It checks cancellation before
and after the call, so a late success cannot be delivered. `IsViolation` determines
the verdict; `Score` may use any finite detector-defined scale. Error, cancellation,
NaN or infinity yields a pipeline fault, even for an alleged pass. No model SDK
or classifier accuracy claim is supplied by the adapter.

The tag matcher recognizes configured text patterns, not arbitrary instruction
attacks. Tool-key JSON classification is a heuristic with false positives and
negatives. Host adapters must supply trusted `PayloadKind` independently of text
when a delivery boundary requires provenance; a heuristic cannot establish trust
or authorize execution.

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

JSON decoding in `ext/jsonschema`, `ext/jsonredact`, dynamic args and the default typed
args codec accepts exactly one document and rejects duplicate keys at every depth
(including equivalent escaped keys). Numbers use `json.Number`, preserving integers,
decimals and exponents without a float64 round trip. Typed numeric fields follow
`encoding/json`: choose `int64`, `uint64` or `json.Number` for exact values; explicit
floating point fields and custom codecs are caller-owned choices. Schema/redaction
accept null and scalar top-level documents; dynamic args require a non-null object.

The schema engine supports Draft 4, 6, 7, 2019-09 and 2020-12; absent `$schema` means
2020-12, matching the struct generator. Unsupported dialects and required custom
vocabularies fail compilation. Unknown annotation keywords remain allowed. Format
assertions follow the selected dialect (annotation by default in 2019-09/2020-12);
content assertions are disabled. Patterns use Go's regular expression syntax.
Exact numeric assertions use `math/big.Rat`; operands outside its representable
range fail schema compilation, and instance numbers outside that range receive
`ActionRetry` instead of a panic or an unchecked pass. This engine limit does not
apply to syntax decoding/redaction or to numbers inside schema annotations.
Length/item/property count assertions must fit a platform integer; compilation
rejects overflow rather than truncating the count.
Compilation never loads network or filesystem resources. Local `$ref` works directly;
use `NewJSONSchemaValidatorWithResources` with caller-owned JSON strings keyed by
absolute URI for external references or custom metaschemas. Custom metaschemas must
ultimately identify a supported dialect and may require only supported vocabularies.

The JSON redactor applies its leaf validator to string **values**, preserving keys
and all other JSON values. A nil or typed-nil leaf panics at construction. A mandatory
leaf decision or error returns the original document; partial redaction is never
released as an allowed result. Output is re-encoded as valid JSON; formatting is not
preserved. Cancellation is checked throughout traversal and after leaf callbacks.

Syntax checks, schema validation and business policy are separate obligations.
`build.WithJSONSchema` uses the same optional schema validator. Shape metadata is
not validation. After transformation/binding, attach `WithArgsFinalGuard`
or `WithJSONArgsFinalGuard` to check schema and policy against canonical bytes;
final guards must not mutate them. No schema dependency is required in core.
To reject unknown properties and wrong-case names, enforce an explicit schema
with `required`, `properties` and `additionalProperties: false` in the raw guard
before binding. The final guard checks post-bind mutations; it cannot recover
fields that standard `encoding/json` discarded. Both are ordinary `Pipeline[string]`
checks. See executable `ExampleCompileArgs_documentAPI` and
`ExampleScopeFactory_documentAPIReference` for document/API composition.
Providing a final guard option with nil fails compilation; omit the option for
flows intentionally without final checks. The library cannot prove the adequacy
of rules inside an arbitrary caller pipeline.


### Token Vault (Reversible Redaction)

Use `TokenVault` when you need reversible redaction (`[GUARDY_TOKEN_...]`) and later restoration in model output:

```go
vault := ext.NewInMemoryTokenVault()
piiV := ext.NewPIIValidator(
	ext.WithAction(guardy.ActionRedact),
	ext.WithTokenVault(vault),
)
result, _ := guardy.NewPipeline(guardy.WithFastPath(piiV)).Run(ctx, nil, "email: a@b.com")
// After host recipient authorization: restore, then validate final output.
// See examples/reversible_redaction for the complete delivery flow.
```

Built-in validators write namespaced canonical tokens such as `[GUARDY_TOKEN_PII_1]` and `[GUARDY_TOKEN_WORDLIST_1]` through `TokenVault.Store(namespace, original)`.
A token is a lookup key, not permission to disclose. The host owns recipient
authorization, vault isolation/lifetime and a separate final delivery check after
restoration. Do not share a request vault across recipients by default.

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

Mandatory deny/retry/fault from an item prevents release of partial transformations. `MapSlice` checks each item independently and aggregates reports; it does not analyze relationships or instructions across messages.

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
Arbitrary validator/code labels are omitted by default; `WithAllowedMetadata` permits
explicit static identifiers of at most 128 bytes. Raw validator errors and report
text are never exported. This middleware does not observe every policy decision.

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
- **ReleaseError** — terminal stream outcome with distinct guard/limit/incomplete/transport category, actual released bytes, and canonical **PolicyFailure**.
- **ErrBlocked** — block decisions (Guard, WrapInput, ReleaseError).
- **ErrRetryRequested** — retry decisions (WrapOutput, ReleaseError, RetryError).
- **RetryError** — structured retry from interceptors and typed argument validation; unwraps to **ErrRetryRequested** and carries **Failure PolicyFailure**.
- **ErrScopeIncomplete** — `Run` called without required policy scope keys.
- **ErrValidatorFailed** — wraps a validator’s system error from `Run`; prefer `errors.As` into **PolicyFailure** or **ValidatorFaultError**.

Use `PolicyFailure.Decision` or `RunResult.PolicyDecision()` for control flow - not string parsing on `Code` or `Reason`, and not local re-derivation from `Report`. Error report details are telemetry snapshots via `ReportSnapshot()`, not the boundary contract.

Production `ext` validators should always set **`ext.WithCode(...)`** so hosts never parse `Reason` strings.

## Packages

- **guardy** — core types (Action, Report, Decision, PolicyFailure, PayloadKind, Validator), Pipeline, typed scope, ArgsPipeline, JSONArgsPipeline, GuardedArgs, GuardedJSONArgs, GuardedOutput, GuardedDelivery, DeliveryPolicy, GuardEvent, GuardRoute, StreamProcessor, BoundaryProfile, Guard middleware, errors.
- **guardy/build** — declarative `GuardSpec` → `CompileStringGuard` (imports ext; core stays clean).
- **guardy/ext** — TagSanitizerValidator, PIIValidator, WordlistValidator, RegexValidator, LengthValidator, TokenVault, MapSlice, MLValidator, NewTechnicalJSONClassifier (output PayloadKind for user channel).
- **guardy/ext/jsonschema** — optional JSON Schema validator with raw-schema and struct-derived constructors.
- **guardy/ext/guardyotel** — optional OTel middleware module (metrics + tracing).
- **guardy/guardytest** — FakeValidator, FailingValidator, MustPass/MustBlock/MustRedact/MustRetry, MustTerminalDeny/MustRetryableCorrection/MustSystemFault, MustOutputKind, MustScopeIncomplete.

See [CONTRACTS.md](CONTRACTS.md) for boundary and release invariants.

## Migration: streaming release and canonical boundaries

- Removed `NewGuardWriter`, `GuardWriterOption`, chunk/timeout/scope options and
  `StreamError`. Use `CompileStream`, an explicit profile, bounds and
  `ReleaseError`. Replace successful `Close` flushes with `Complete(ctx)`;
  disconnects use `Abort`. Whole-response PII guarding no longer exposes a prefix.
- `WrapInput`, `WrapOutput`, `WrapArgs`, `WrapGuardedArgs`,
  `WrapGuardedJSONArgs`, `WrapGuardedOutput` take `ScopeFactory`, called on every invocation/resume. Use
  `func(ctx context.Context) (guardy.ExecutionScope, error)` to project current
  policy facts. Low-level `Run` still accepts an explicit scope.
- Typed args are canonically encoded after post-bind hooks, including pointer
  types. Add `WithArgsFinalGuard[T](schemaAndPolicyPipeline)` to require final
  schema/policy checking after mutations; `ShapeProvider` is metadata only.
  Final checks are read-only. `WithArgsCodec` accepts caller-owned bind/encode.
- Dynamic schema callbacks get deeply isolated JSON. They check the sanitized
  decoded object, not the original payload. Use `WithJSONArgsFinalGuard` for
  mandatory policy checks on canonical JSON after all transformations.
  `JSONArgsValidator` and `JSONArgsValidatorFunc` supply checks; `JSONArgsMetadata`
  supplies only ID/shape. Nil built-in checker functions and explicitly nil final
  guards fail compilation with `ErrConfiguration`. A custom checker’s rules remain
  caller-owned. Ordinary flows can omit schema and final options.
- Dispatch only guarded sanitized arguments. For every consumer use a separate
  destination policy and serialize `GuardedDelivery.Projection()`, never the
  wrapper with original raw values or reports. Replacement/fallback content is
  checked by its destination pipeline; a forbidden fallback is suppressed.
- Decision aggregation prioritizes effective disposition. Fatal pass/redact can
  no longer disappear behind an earlier mutation. Report-only system faults block
  stream release. Map errors using `errors.As`, not text.
- `MaxRetries == 0` means no retries. Caller owns retry counters and external
  approval binding; resume must rebuild current scope and revalidate arguments.
- Fault/retry `Error()` text no longer includes diagnostic causes/correction
  feedback. Read these explicitly through `PolicyFailure`; do not expose them
  as external response text. Semantic scores and thresholds must be finite;
  NaN/infinity is a system fault, not a benign detector result.
- `CompileBoundaryProfile` declares actual supported/mandatory coverage; it does
  not intercept remote backends automatically. Use reusable
  `guardytest.CheckBoundaryCases` in optional integrations.

Caller-owned facts examples in `policy_facts_example_test.go` show source linkage,
separate confirmed/claimed trust, missing destination rejection and redaction.
The executable recipe in `policy_recipe_test.go` connects untrusted source,
transformation, typed arguments and destination. Its caller-owned facts preserve
all references and confirmed trust; returned evidence is at most two opaque
references, each at most 64 bytes. Unknown/missing sources fail closed. Host
declassification and the restricted no-provenance profile require explicit choices.
Guardy creates no permission grant, trust registry, provenance store or workflow.

`guardytest.ReferenceBoundaryFixtures` and `StringBoundaryCases` provide fresh
benign/adversarial cases for real handler/consumer wiring; configure their stated
synthetic test policy. `ReferenceSemanticFixtures` exercises real threshold/shadow
behavior with deterministic mock scores, detector identity, errors and cooperative
timeouts. Keep deterministic and semantic suites separate: mock conformance does
not measure detector false positives/negatives or live-provider safety.

## Migration: typed scope, boundary contracts and delivery routing

- **Breaking:** `Run(ctx, scope, input)` — remove `WithAttributes` / `AttributesFromContext`; declare `ScopeKey[T]` requirements and pass a host `ExecutionScope`.
- **Fail-closed policy:** `RequiredScope()` compiled at pipeline construction; missing keys → `ErrScopeIncomplete` + `ScopeIncompleteError` before fast-path.
- **Decision:** route with `RunResult.PolicyDecision()` and `PolicyFailure.Decision`, not local parsing or local disposition derivation from `Report`.
- **Output contract:** use `GuardDelivery` / `GuardOutput` / `GuardedOutput[T]` for delivery boundaries, not plain strings plus `OutputKind` flags or post-guard JSON sniffing.
- **Typed arguments:** use `CompileArgs[T]` / `ArgsPipeline[T]` / `GuardedArgs[T]`; raw validation plus local decode was removed from the public path.
- **Dynamic JSON arguments:** use `CompileJSONArgs` / `JSONArgsPipeline` / `GuardedJSONArgs` when the handler cannot bind to a static Go type.
- **Observer telemetry:** `WithObserver` receives `GuardEvent` for non-fatal shadow blocks, with scope, phase, decision, pipeline identity, payload kind, report, and safe telemetry metadata. It is not an all-events audit ledger.
- **Decision routing:** use `Decision.Route(RemediationPolicy)` or `RouteDecision` for retry, terminal deny, system fault, and fallback projection.
- **HTTP report context:** `ReportFromContext` was removed; report context side channels are replaced by explicit decisions, policy failures, guard events, and boundary values.
- **WrapInput/WrapOutput:** take `ScopeFactory` (pass `nil` when unused); raw-args and output-boundary wrappers are `WrapArgs`, `WrapGuardedArgs`, `WrapGuardedJSONArgs`, and `WrapGuardedOutput`.
- **Validators:** use `FinishReport` or `ext.FinalizeRuleReport` for `ActionRetry` so `Retryable` defaults are applied; raw `ActionRetry` without defaults is treated as terminal deny.
- **Declarative guards:** `github.com/skosovsky/guardy/build` — JSON Schema via `build.WithJSONSchema`, not in core.

## Migration: policy and safety decisions

Use `ArgsPipeline`, `Map` and `MapJSONRawMessage` for type-safe argument validation and redaction.

- **Decision control flow:** use `Decision` / `PolicyFailure`; `Report` remains validator telemetry.
- **Policy phase:** `WithPolicyValidators` + typed `ScopeKey[T]` requirements + explicit `ExecutionScope` in `Run`.
- **Typed arguments:** `ArgsPipeline` + `GuardedArgs[T]` replaces raw pipeline plus local decode.
- **Streaming:** `errors.As(err, &failure)` where `failure` is `*PolicyFailure`.
- **JSON redact:** `guardy/ext/jsonredact` (separate module; optional).
- **ext options:** `WithCode` required for production; `WithRetryable`, `WithFatal`, `WithSafeUserMessage` as needed.

## Migration: streaming, policy shadow and post-bind validation

- **Stream migration:** use explicit `CompileStream` profiles and trusted `Complete`; `Close` aborts.
- **Policy shadow:** shadow policy blocks no longer stop the pipeline; register `WithObserver` for telemetry.
- **PostBindValidator:** business rules after bind with `CodePostBindViolation` + `RetryError`;
  cancellation/deadline errors are system faults with their cause preserved through `errors.Is`.
- **jsonschema codes:** default schema violations use `CodeJSONSchemaInvalid` (`JSON_SCHEMA_INVALID`).

## Migration: `MapJSONRawMessage`

- **Broken JSON after redact:** branch on `CodeJSONRedactCorrupted`, not `CodeJSONInvalid` (parse/bind errors).
- **Struct tool args:** `NewPipeline[MyDTO]` + `MapJSONRawMessage`; see `examples/agent_tool_args`.

## v2 Migration Highlights

- `Pipeline.Use(...)` is immutable and returns a new pipeline.
- `Report` includes `Code` and typed `Severity`.
- Legacy streaming constructors/options are removed; use `StreamConfig`.
- `PIIMasking` APIs were renamed to `PIIValidator` / `NewPIIValidator`.
- `ext/jsonschema.NewValidatorFromStruct` was renamed to `NewJSONSchemaValidatorFromStruct`.
- Built-in `ext` validators use options for common rule metadata (`WithAction`, `WithCode`, `WithSeverity`, `WithReason`, ...).

## Wordlist benchmark

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

Use Go 1.27.1 or newer and golangci-lint 2.14.0 or newer. CI pins the current
tooling releases. An explicit linter binary can be selected with
`make lint GOLANGCI_LINT=/path/to/golangci-lint`.

```bash
make test
make lint
```

## License

See [LICENSE](LICENSE).

## Migration: report composition

Decision routing now uses Disposition alone: use IsTerminal, IsRetryable and
IsSystemFault instead of the removed Terminal, Retryable, SystemFault and
UserCorrectable fields. Report flags remain construction inputs and telemetry.
FinishReport initializes new reports; adapters must preserve completed reports,
including explicit non-retryability. JSONArgsValidator callbacks follow the same
contract as Validator; raw ActionRetry is terminal unless retryability is explicit.
ComposeReports preserves the strongest enforcement and aggregated PayloadKind.
Unknown enums and non-finite report scores are faults; fatal escalation wins over
correction. Shadow observes only non-fatal policy blocks. The shadow observer's
Decision describes the violation without suppression; its Report retains ShadowMode.
ShouldStop/ShouldRetry, ApplyControlDefaults and ext.ValidatorOption were removed;
use disposition methods, FinishReport and ext.Option.


## Current host facts and context policies

Long-lived adapters resolve `ScopeFactory` at each boundary. Input/argument checks
obtain facts before validation; output checks obtain facts after a successful
handler. HTTP uses `WithGuardScopeFactory` after extraction. Low-level `Run` accepts
an explicit snapshot. Fact freshness and atomic authorization at execution remain
host responsibilities. `WrapOutput` preserves a partial handler result on error;
`WrapGuardedOutput` suppresses it. Neither can undo handler side effects.

The host assigns authenticated identity, source references, trust/integrity,
confidentiality, destination and policy identity. These are separate facts:
authenticated content can still be confidential. Untrusted text claiming to be
trusted does not change facts. Summaries, memory records and subagent results must
retain host source restrictions; unknown provenance fails a configured mandatory
policy. Each consumer checks its own destination and serializes only an approved
`DeliveryProjection`. A host gate binds approval to exact canonical args, identity,
destination and current policy/configuration; `ConfigurationID` is metadata.
After pause/resume, rebuild facts and revalidate. The same contracts apply to a
plain document/API workflow with no model or agent runtime.

Telemetry must export bounded opaque references and explicitly permitted metadata.
Do not serialize complete Scope/Report/raw data or feedback. Third-party validator
errors may contain secrets even with payload capture disabled. OTel provides call
metrics/spans, not a provenance ledger or complete decision audit.

For external semantic evaluation fix detector/model/configuration and dataset IDs,
threshold and train/evaluation split. Keep benign and adversarial sets separate;
report false positives/negatives, task success, latency and fault rates. Deterministic
mock conformance verifies integration only. Live-provider benchmarks are optional
and are not required by CI.


`context_policy_reference_test.go` contains the executable document/API reference
harness and deterministic adversarial corpus. It validates typed and dynamic
canonical arguments with JSON redaction and final schema/policy, binds a host
approval, resumes against fresh facts, and observes real handler calls and bytes
at independent context, persistence, export and delivery sinks. All harness types
and approvals are caller-owned; no Agent/Message/Session type is required.


JSON/newline streaming uses processor-local incremental framing and bounded pending
storage. Already scanned prefixes are retained as scanner state rather than parsed
again on each Write; consuming a unit does not repeatedly shift the live tail.
`PeakPendingBytes` records reserved pending capacity, which stays within
`MaxPendingBytes`. Fixed scanner state and caller-owned validation/output allocations
are separate. Deterministic operation tests check framing/buffer work; local
before/after benchmarks in `STREAM_MEASUREMENTS.md` do not promise callback latency.
