# Guardy

[Go reference](https://pkg.go.dev/github.com/skosovsky/guardy)

Guardy validates, transforms and controls delivery of caller-owned values through
`Pipeline[T]`. It runs sequential transformations, caller policies and parallel
read-only checks. Bring your own Go types, facts and detectors. Core supplies no
agent runtime, trained model, permissions store or retry scheduler.

Requires Go 1.27.1+. Install core alone:

```sh
go get github.com/skosovsky/guardy
```

## Quick Start

This root-only example redacts a value and writes only its approved projection.
Static configuration uses `MustNewPipeline`; dynamic configuration should handle
`NewPipeline`'s error during setup.

```go
package main

import (
    "context"
    "fmt"
    "strings"

    "github.com/skosovsky/guardy"
)

func main() {
    redact := guardy.ValidatorFunc[string](func(_ context.Context, value string) (string, *guardy.Report, error) {
        clean := strings.ReplaceAll(value, "user@example.com", "[email]")
        return clean, &guardy.Report{Action: guardy.ActionRedact}, nil
    })
    pipeline := guardy.MustNewPipeline(guardy.WithSequential(redact))
    delivery, err := pipeline.GuardOutput(context.Background(), nil, "Contact user@example.com")
    if err != nil {
        fmt.Println("delivery rejected")
        return
    }
    projection, ok := delivery.Projection()
    if !ok {
        return
    }
    fmt.Println(projection.Value)
}
```

Output: `Contact [email]`. This literal replacement is an example transformation,
not a general email detector. The executable counterpart and outcome matrix are
in [consumer_docs_example_test.go](consumer_docs_example_test.go).

`GuardOutput` uses the explicit `NewUserTextPolicy("user")` recipe. Its bounded
shape classifier supports known text representations; it does not certify custom
serialization or content provenance. For another destination use `GuardDelivery`
with `NewDeliveryPolicy(channel, WithDeliveryAllowedKinds(...),
WithDeliveryClassifier(...))`. Bind the classifier to the actual host wire format.
Unknown kinds and unsupported/cyclic representations fault. Core never invokes
arbitrary `MarshalJSON`/`MarshalText` to infer approval.

Deliver only `DeliveryProjection`, not the entire `GuardedDelivery`, original
payload, raw reports or correction feedback. Fallback is a separate checked
value, allowed after policy rejection only by an explicit contract; faults cannot
activate it. A routing proposal is not delivery approval.

## Values, decisions and faults

A validator implements `Validate(context.Context, T) (T, *Report, error)`.
Returned `T` and `RunResult.Output` are authoritative; `MutatedText` is only an
optional diagnostic mirror. Use `Map` for a struct field and `MapJSONRawMessage`
for a raw JSON field. Setters must copy reachable aliases before mutation: guardy
cannot undo external mutations, tool execution or other host effects.

`Report` is a validator DTO, retained for third-party integration and telemetry.
`Decision` is canonical control flow. Fault outranks deny, which outranks correction.
Fatal escalation cannot be hidden by redaction or nonfatal shadow observation.
Route with `PolicyDecision`/`PolicyFailure.Decision`, not `Reason`, `Code` text or
locally re-derived report flags. `FinishReport` applies construction defaults;
a raw `ActionRetry` without explicit retryability is a terminal deny.

Low-level callers must handle both fault channels before using output:

```go
result, err := pipeline.Run(ctx, scope, input)
if err != nil {
    return err // Validate error/panic, cancellation or prerequisite failure
}
decision := result.PolicyDecision()
switch {
case decision.IsSystemFault():
    return errors.New("validation fault") // a report-only fault can have nil err
case decision.IsTerminal():
    return errors.New("blocked")
case decision.IsRetryable():
    return errors.New("correction required")
default:
    consume(result.Output) // host owns destination approval for this low-level use
}
```

High-level argument, delivery and stream boundaries suppress fault output and
expose `PolicyFailure` through `errors.As`. Inspect `ValidatorFaultError`,
`ValidatorPanicError`, `CompletedObservationsError` and their causes only in trusted
operator diagnostics. Default public errors omit raw causes and retry feedback.
`WithCompletedObservations` attests only earlier finished checks; arbitrary reports
beside errors are discarded. See [CONTRACTS.md](CONTRACTS.md).

## Pipeline, typed facts and ownership

Phases run sequential → policy → parallel. `WithSequential` callbacks transform
values in order; `WithPolicyValidators` use typed host facts; `WithParallel` checks
must be read-only. Equal-priority parallel diagnostics may vary; canonical outcome
priority remains fixed. `Validate` panics become safe faults in every phase.
Construction, observers and host callbacks outside Validate must not panic.

`NewPipeline` and `Pipeline.Use` return `(pipeline, error)` and reject invalid
configuration before processing. `MustNewPipeline`/`MustUse` are static wrappers.
Nil rules/options/middleware/wrapper layers are errors; `WithObserver(nil)` explicitly
disables observation. Use copies registration slices and shares rule/provider objects.

Declare requirements with `ScopeKey[T].Requirement()` and supply an `ExecutionScope`
through `NewScope(ScopeValue(key, value))` or your own Lookup implementation.
Concrete prerequisites require exact dynamic types; interface keys accept implementing
types. A nil interface cannot satisfy a typed key. Name-only declarations check presence.
Missing/incompatible prerequisites fail before sequential callbacks.

`ScopeFactory` resolves fresh caller facts at each adapter invocation; output
adapters resolve them after a successful handler. Facts are transient borrowed
values. Build policy operands, nested scope values and callback/provider objects
must stay immutable or have caller synchronization. Publish a fresh pipeline and
matching facts as one version for reload; do not mutate retained aliases. The
executable atomic reload recipe is in [build/ownership_test.go](build/ownership_test.go).
Fact lookup does not atomically authorize execution or bind an approval.

Identity, source references, confirmed trust, confidentiality, destination and
policy identity are separate host facts. Untrusted claims do not upgrade trust;
summaries and subagent outputs retain source restrictions. After a pause, refresh
facts and revalidate exact canonical arguments. Approval, scheduling, retry counters,
rollback, declassification, persistence and vault lifetime remain host-owned.
See [integration/context_policy_reference_test.go](integration/context_policy_reference_test.go)
for actual argument/handler/context/persistence/export/delivery fixtures.

## Adapters and declared configuration

- `WrapInput` checks before execution; `WrapGuardedOutput` checks the result and
  supplies a guarded projection. Handler errors suppress partial results. Neither
  wrapper retries or undoes side effects. `WrapOutput` is low-level: it returns an
  unvalidated partial result alongside a handler error; never deliver that value.
- `CompileArgs[T]` and `CompileJSONArgs` produce typed/dynamic canonical argument
  boundaries. Dispatch approved sanitized arguments, not a local re-decode of raw
  input. Final guards check canonical output after mutation; `ShapeProvider` and
  JSON metadata describe shapes, not enforcement. See [examples/agent_tool_args](examples/agent_tool_args/main.go).
- Optional `build.CompileStringGuard` compiles a `GuardSpec`. JSON-schema injection
  uses `build.WithJSONSchema`; it does not put an engine in core.
- `CompileBoundaryProfile` records declared coverage only. It cannot inspect or
  install host wiring. Real handler/sink fixtures prove enforcement.
- `Decision.Route(RemediationPolicy)` returns `(GuardRoute, error)`; negative
  counters reject configuration, zero retries means exhausted. The host schedules
  retries and separately checks proposed fallback delivery.

## HTTP

`Guard(pipeline, extractor, injector, options...)` returns `(middleware, error)`;
`MustGuard` is the static alternative. `WithGuardScopeFactory` refreshes facts after
extraction. `WithGuardMaxBodyBytes` requires a positive consumed-input cap, default
1 MiB; declared ContentLength cannot bypass it. Exact cap is allowed, excess is 413.
Read/extraction and missing scope are 400; deny/correction 422; validation faults,
cancellation, pre-handoff body-close and injection errors 500 with safe public text.

Pass restores original bytes, length/header and independent GetBody replay.
Redaction sends returned `T` to the format-aware injector, never `MutatedText`.
`PlainTextInjector` supports string bodies. Guard closes the consumed original
wrapper before replacement; extractors/injectors borrow their replay body and
transfer current replacements to Guard. Next borrows the captured handed-off body,
closed on return. Callbacks own intermediate replacements they remove; handlers own
their replacements. GetBody readers are caller-owned. A late close cannot rewrite
an already committed response; this is not a universal socket lifecycle promise.
The full ownership table is in [CONTRACTS.md](CONTRACTS.md#http-request-limits-and-body-ownership-t10).
See [ExampleGuard](example_test.go) and [http_guard_ownership_test.go](http_guard_ownership_test.go).

## Streaming

Use `CompileStream(writer, StreamConfig)` with explicit profile, framing and positive
byte budgets. `Complete(ctx)` is a trusted final boundary; `Close` aborts. Whole-response
buffers and validates the complete value; unit mode requires declared unit-local rules;
best-effort releases bounded UTF-8 chunks and makes no whole-value guarantee.
No profile silently downgrades. `DeliverFallback` performs separate checked delivery,
never recursive fallback or masking a fault.

Input/output/pending/unit limits count bytes, including UTF-8 and delimiters.
Newline exact-limit tails wait for Complete; an extra byte/newline faults before
buffering. Unit JSON requires object/array for original, transformed and fallback
values; whole-response JSON accepts one valid value, including scalars. Expanded
unit output is bounded by MaxUnitBytes and whole output by MaxOutputBytes. JSON
framing whitespace counts toward the preceding unit; pending must exceed unit cap
by a byte. Transformed/fallback newline output must remain one self-contained unit.

Pending allocation capacity stays within MaxPendingBytes; PeakPendingBytes measures
reserved pending capacity, not all process/validator allocations. Incremental framing
avoids repeated prefix scans/tail shifts, excluding caller callbacks, JSON syntax
checks and transport work. Partition guarantees apply to admissible inputs. Each
Write's input-budget admission is atomic; rejected partitions can have different
already delivered prefixes. Released bytes are actual transport bytes; short writes
terminate without replay, and earlier prefixes cannot be undone.

Callbacks run under the processor mutex and must return without reentry. Abort,
Outcome and subsequent calls can wait for them; context cannot interrupt mutex
acquisition or forcibly stop a non-cooperative callback. No detached timeout workers.
Use ReleaseError categories and errors.Is for malformed/incomplete/limit/transport
faults, not diagnostic strings. See [CONTRACTS.md](CONTRACTS.md#stream-release),
[STREAM_MEASUREMENTS.md](STREAM_MEASUREMENTS.md), [examples/streaming_filter](examples/streaming_filter/main.go)
and [examples/json_streaming](examples/json_streaming/main.go).

## Packages and optional installation

| Import | Purpose |
| --- | --- |
| `github.com/skosovsky/guardy` | Core BYOT pipeline, typed facts, argument/delivery/HTTP/stream boundaries |
| `github.com/skosovsky/guardy/ext` | Built-in matchers, classifier/semantic adapter, vault, MapSlice (root module) |
| `github.com/skosovsky/guardy/guardytest` | Synthetic outcome, boundary and semantic fixtures (root module) |
| `github.com/skosovsky/guardy/build` | Declarative compiler, separate module |
| `github.com/skosovsky/guardy/ext/jsonredact` | Recursive JSON leaf redaction, separate module |
| `github.com/skosovsky/guardy/ext/jsonschema` | Raw/struct-derived JSON Schema validation, separate module |
| `github.com/skosovsky/guardy/ext/guardyotel` | OTel middleware, separate module |

Install and import only the optional module you use:

```sh
go get github.com/skosovsky/guardy/build
go get github.com/skosovsky/guardy/ext/jsonredact
go get github.com/skosovsky/guardy/ext/jsonschema
go get github.com/skosovsky/guardy/ext/guardyotel
```

For example, a host using optional JSON redaction and schema validation imports:

```go
import (
    "github.com/skosovsky/guardy/ext/jsonredact"
    "github.com/skosovsky/guardy/ext/jsonschema"
)
```

OTel and declarative hosts import `"github.com/skosovsky/guardy/ext/guardyotel"`
and `"github.com/skosovsky/guardy/build"` respectively.

The corresponding Go imports are exactly the paths in the table. Core imports none
of the optional engines. See [ext/jsonschema/UPGRADE.md](ext/jsonschema/UPGRADE.md)
for pinned exact-number, overflow, dialect and reference upgrade probes.

## Detector, vault and telemetry limits

Built-in detector and semantic rule constructors return configuration errors;
Must variants serve static setup.
Regex/wordlist compile at construction. Set bounded stable WithCode identifiers.
PII recognizes a finite set of ASCII email, NANP/+7 phone and Visa/MasterCard shapes;
it does not certify mailbox/phone issuance, Luhn validity or global PII coverage.
Unsupported formats can contain a supported substring and be only partly redacted.
Word boundaries, overlaps and generated replacements follow the explicit
[matcher contract](CONTRACTS.md#standard-matcher-and-classifier-limits).
TagPattern matches configured text patterns, not arbitrary instruction attacks;
TechnicalJSONClassifier is a heuristic with false positives/negatives, not trust evidence.

Caller-supplied synchronous TextClassifier/SemanticDetector callbacks are cooperative.
Scores/thresholds must be finite; NaN/infinity/error/cancellation faults even for an
alleged pass. MapSlice checks independent items, not relationships across messages;
its copy is outer-slice only, so setters must use copy-on-write for reachable aliases.
JSONRedact visits sorted object keys and array indices, continues after deny/correction
to find stronger outcomes, and stops at fault/cancellation. Mandatory outcomes retain
the original document and clear MutatedText.

TokenVault.Store returns `(token, error)`; storage error/panic/empty/identity token
faults without degrading to irreversible replacement. A token is a lookup key, not
a permission grant. Host owns isolation, lifetime, recipient authorization and a
final delivery check after restoration; see [examples/reversible_redaction](examples/reversible_redaction/main.go).

Optional guardyotel.NewMiddleware returns `(middleware, error)` on instrument setup
failure, with safe ConfigurationError and inspectable original cause. MustMiddleware
is static setup; plain nil Meter/Tracer explicitly disables that channel. Runtime
metadata allowlists are opt-in, bounded to 128-byte static identifiers. Raw payload
capture is opt-in; raw errors/report text are never exported. Canonical disposition,
outcome and execution-fault status do not constitute delivery approval or a full
audit ledger. Provider lifecycle is host-owned. See [examples/otel_integration](examples/otel_integration/main.go).

Deterministic fixtures measure conformance, not detector accuracy. Evaluate fixed
model/config/dataset identities, benign/adversarial splits, false positives/negatives,
task success, latency and faults separately. There is no live-provider safety claim.

## Development, measurements and release

Use golangci-lint 2.14.0+; CI pins its exact version. `make test` and `make lint` cover
all nested modules and examples; make test also runs release tooling fixtures.
A root `go test ./...` alone excludes nested modules. `make bench` measures local
workloads. Historical wordlist comparison names `baseline_a16279e_runtime_compile`
and `v2_precompiled` are benchmark identifiers, not a module major version:

```sh
go test -bench '^BenchmarkWordlist_Blocklist_Redact_BaselineComparison$' -benchmem ./ext -run '^$'
```

Results depend on machine/workload and do not measure arbitrary callback latency,
semantic accuracy or production throughput. Streaming before/after boundaries and
partition/work limitations are recorded in [STREAM_MEASUREMENTS.md](STREAM_MEASUREMENTS.md).

Release preparation requires Python3.9+, Git and Go. `make release-prepare VERSION=...
CANDIDATE=...` stages an isolated candidate; `make release-verify CANDIDATE=...` uses
its private exact-module proxy/cache, readonly graph/race/lint and independent
consumer. Neither publishes. Candidate generation preserves module boundaries,
removes workspace/local replacements and rejects unsupported v2+ path transitions.
The standard confirmed release workflow is `make release-patch`/`make release-break`;
it prepares/verifies and publishes candidate tags only to the configured origin.
Low-level publish requires explicit REMOTE and verified candidate bytes. See
[CONTRACTS.md](CONTRACTS.md#release-artifacts-and-independent-modules).

All API changes and earlier transitions are in [MIGRATION.md](MIGRATION.md).
This guide describes the current candidate, without claiming it has been published.

[License](LICENSE).
