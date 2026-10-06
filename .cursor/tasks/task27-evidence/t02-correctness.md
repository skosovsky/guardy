# T02 independent correctness review

Reviewer did not implement T02 or read another acceptance report. Scope: staged
T02 diff, T02 criteria and original R02 / D01–D03.

Status: PASS on the final staged diff after repair. No unresolved confirmed errors found within this review scope.

## Confirmed finding, repaired

The documented bound describes pointer/interface dereference exceeding 64 steps
as a fault (CONTRACTS.md; README.md; guarded_output.go:334), but the value loop at
guarded_output.go:340 counts the final supported leaf as a traversal step.
Exactly 64 pointers to string therefore fault; 63 pass. No unsafe delivery occurs,
but the supported representation contract and runtime boundary differ.

Independent repro: `/tmp/guardy-task27/t02-independent/depth.go`, constructing
nested pointer values with reflect.New and calling exported UserTextClassifier:

```
depth=63 kind=safe_user_text err=<nil>
depth=64 kind=safe_user_text err=guardy: unsupported delivery representation
depth=65 kind=safe_user_text err=guardy: unsupported delivery representation
```

The final staged implementation permits the supported leaf after exactly 64
dereferences; both value and typed-nil type walkers share the remaining budget.
Independent rerun now prints depth 63 PASS, depth 64 PASS, depth 65 typed fault.
TestUserTextDereferenceLimit covers 63/64/65 value depth and 64/65 typed-nil depth.
The repaired contract is consistent across runtime/Godoc/README/CONTRACTS.

## Verified paths

- Generic policy validates channel, kinds, classifier, nil options and fallback
  assertion before Run; type mismatch preserves ConfigurationError cause and
  canonical SystemFault, and suppresses Projection.
- Value/type cycles have finite 64-step bounds independent of context and allowed
  technical kinds; subprocess watchdog regressions assert typed fault and zero
  delivery. Classifier error/panic/invalid kind/cancellation cannot start fallback.
- UserText does not call custom MarshalJSON/MarshalText. Exact RawMessage is an
  explicit supported representation; custom marshalers and unknown scalar kinds
  fault. Direct independent probes also checked named-byte slice, typed nil map/
  slice/function, byte array and pointer custom marshaler.
- Independently probed a safe caller classification following a validator's
  technical observation: kind remains technical, denied on safe-only channel.
- Checked fallback disables recursion, validates with the same pipeline/classifier,
  suppresses failed fallback; mandatory classifier/pipeline fault never becomes
  deliverable. Checked fallback Kind represents its value, original Decision/error
  retain blocked policy evidence as the existing contract specifies.
- Single GuardedDelivery is returned by both helpers/adapters and provides Projection.
  Remaining GuardedOutput references are function name or explicit migration text.
  Stream compilation validates policy and string fallback before execution.

## Checks and limits

Executed independently with GOCACHE=/tmp/guardy-task27-gocache and
GOMODCACHE=/tmp/guardy-task27-modcache:

```
go test -race ./... -run 'Test(GenericDelivery|UserText|TypedNilFallback|ClassifierFailure|DeliveryCycles|GuardOutput|GuardDelivery|Delivery|StreamConfiguration)' -count=1
```

PASS on final staged runtime: core (5.316s), ext, guardytest, internal/jsondoc (latter three no selected
tests). Independent public API depth repro PASS after repair; edge probes PASS.
Final diff including repair was re-inspected; no additional confirmed errors found. `git diff --cached --check` PASS.

19-module execution logs are provided by implementation evidence; this reviewer
has not independently rerun all 19 modules or root lint. No full make/release/
benchmark campaign or arbitrary custom-classifier termination proof attempted;
custom classifier termination and actual host serialization remain caller contracts.
Existing pending R03–R09 defects are handled by their later planned stages.
