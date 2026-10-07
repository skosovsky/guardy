# Optional downstream adapters

This module composes guardy with the real tool execution and producer stream APIs.
Install it separately:

```sh
go get github.com/skosovsky/guardy/integration/downstream
```

Core has no dependency on this module or its external runtimes. Sources and
published dependencies are selected in `conformance.json` and `go.mod`.

## Arguments

```go
binder, err := downstream.NewArgsBinder[Request](rawGuard, finalGuard, scopeFactory)
if err != nil { return err }
spec.ArgsBinder = binder
```

Use `NewJSONArgsBinder(rawGuard, finalGuard, scopeFactory, checker)` for dynamic
objects. Final guard is required and must not transform bytes. It runs after all
binding hooks and canonical encoding. The adapter additionally checks the actual
`req.Manifest.Parameters`, because custom binding bypasses default schema checks.
Schema resources must be embedded; fetching remote references is unsupported.
Compilation of the manifest schema is currently per invocation, with no shared
mutable cache. Custom codecs must encode the bound value faithfully.

Before, a raw return from a custom binder:

```go
args, err := pipeline.Validate(ctx, scope, string(req.Input.ArgsJSON))
if err != nil { return toolsy.ValidatedArgs[Request]{}, err }
```

could turn execution faults into argument validation failures and discard their
cause. Use the binder above, or `MapArgsError(err)` in custom pre-handler glue.
Deny maps to `POLICY_DENIED`, correction to retryable `VALIDATION_FAILED`, fault
and cancellation to `INTERNAL`, deadline to `TIMEOUT`. Cancellation remains
identifiable with `errors.Is`; original `PolicyFailure` remains available with
`errors.As`. Public copy is static, bounded to 128 bytes, and omits diagnostic
payload, feedback and causes. Host controls correction budgets and scheduling.

Downstream `ValidatedArgs.Value` and `Raw` are canonical sanitized data.
Guardy's original `Raw` is diagnostic input; manual glue must copy `SanitizedRaw`.
Do not mutate args through a later `ArgValidator`/policy callback. On resume invoke
again with current facts and exact args; the adapter issues no approval grants.
The [executable example](example_test.go) builds a real tool and emits `"safe"`.

## Text streams

```go
// stream was created by the producer with the invocation context.
release, err := downstream.ConsumeTextStream(ctx, stream, sink, streamConfig)
// Route distinguishes incomplete/refusal/paused/tool_calls from producer,
// cancellation and delivery failures. Outcome counts actual released bytes.
```

Use explicit `ReleaseWholeResponse` for whole-value approval. `Complete` is called
only after a completed lifecycle and provider outcome, including trailing errors
and final validation. `StreamFinish`, EOF, observer or finalizer copy do not grant
release or sanitize content. Only processor writes reach the sink. Handles are
single use; paused work requires a new producer invocation and fresh host checks.

Progressive profiles are explicit. They can leave an irreversible prefix on a
later refusal/error. Sink short writes/errors preserve released-byte counters and
never replay. Only text content is supported; reasoning, media and tool-call
content fail closed. The tool-call terminal route is for host handling, not tool
execution. No automatic fallback, multimodal projection or provider invocation is
installed. Sources/callbacks must cooperate with cancellation; use the same
invocation context when constructing and consuming the producer handle.

## Results, restore and host facts

`ResultValidator` is reject-only and runs after the effect. Preserve its
noncorrectable `ResultContractError`; never remap it as an argument correction or
rerun the handler. To transform an output, guard its value before returning a tool
result and deliver the approved destination projection. Carry effect/control
metadata separately for the host; serialize only the approved wire payload,
never the result/chunk/guard wrapper as a whole. `recipes_test.go` exercises this
through real tool execution with effect counters and sink assertions.

The restore recipe in `recipes_test.go` uses host-owned recipient/session/expiry
checks before vault lookup, then fresh facts and destination policy after restore.
Tokens are keys, not permissions. Vault lifecycle/isolation, authorization and
atomic action binding belong to the host. Claims in model input, summaries and
nested outputs never populate trusted scope facts. The fixture is deterministic,
in-memory host wiring, not a production IAM/storage implementation.

## Conformance

```sh
python3 scripts/downstream_conformance.py sources --output /tmp/guardy-sources
python3 scripts/downstream_conformance.py published --output /tmp/guardy-published
# Optional local source snapshots, copied into an isolated workspace:
python3 scripts/downstream_conformance.py sources --toolsy /path/to/executor \
  --prompty /path/to/producer --output /tmp/guardy-local
```

Run these from the repository root. Source mode clones exact selected revisions
or copies explicit local paths; it uses its own temporary workspace. Published
mode copies only this consumer, uses `GOWORK=off`, checks selections and rejects
replacements. Both retain the actual dependency graph, revision selection and
verbose `go test -mod=readonly -race -count=1 ./...` log. No semantic fixture is
skipped for missing dependencies. CI runs both modes and uploads evidence.
`make test`/`make lint` also discover this nested module; candidate release checks
run its suite against staged guardy artifacts and published external runtimes.
Live providers and production authorization/storage are outside these claims.
