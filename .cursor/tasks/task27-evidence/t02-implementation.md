# T02 implementation evidence

Baseline: `0bd3903` (T01 accepted); T01 commit: `0bd3903`.

R02 baseline regression was added before implementation. Each of value-interface
cycle and nil recursive pointer type was exercised in an independent watchdog
process, through GuardOutput and directly through technical-allowed GuardDelivery.
`go test -run '^TestDeliveryCyclesTerminate$' .` exited 1: four processes killed
at 3s deadlines (t02-baseline.txt). After explicit recipe migration and fix,
`go test -count=1 -race -run 'TestDeliveryCyclesTerminate|TestGenericDelivery|TestUserText|TestTypedNilFallback|TestClassifierFailure' .`
exited 0 (t02-regression.txt); cycle assertions additionally require the new typed
DeliveryClassificationError and category after its API was introduced.

D01: generic policy requires explicit channel/kinds/caller classifier; no shape
inference. NewUserTextPolicy is an explicit bounded shape recipe, never calls
arbitrary marshalers, rejects unknown/custom representations as typed faults.
Classifier error/panic/invalid kind/cancel cannot enable fallback or delivery.
More restrictive validator kind cannot be downgraded. Caller owns actual wire
representation, cooperative termination and sharing of custom callbacks.
D02: only GuardedDelivery[T] remains; GuardOutput and WrapGuardedOutput return it,
including Projection. Existing text-oriented consumers use NewUserTextPolicy.
D03: policy/fallback compatibility checked before Run, nil options invalid;
nil fallback absent vs compatible typed nil present/checkable. CompileStream
also rejects invalid string delivery policies before processing.

Config acceptance matrix, generic custom representation, UserText pointer/nil/
struct/bytes/raw-message/custom-marshaler matrix, typed nil fallback check and
classifier failure matrix are in delivery_policy_contract_test.go. Existing
checked-fallback/Decision/PolicyFailure and stream regression tests remain.

Migration adjustments: an integration reference's missing destination now expects
ErrConfiguration/SystemFault before policy callbacks, matching the explicit new
contract; all other host-policy denies retain their expected classification.
OTel stream fixtures/example now specify their formerly implicit UserText policy.

Checks with GOCACHE=/tmp/guardy-task27-gocache,
GOMODCACHE=/tmp/guardy-task27-modcache:
- 19 modules from `rg --files -g go.mod`, per-module `go test -race ./...`: PASS,
  t02-modules.txt. All 13 example modules compile, test-bearing examples execute.
- Fresh targeted race/watchdog: PASS, t02-regression.txt.
- root golangci-lint with /tmp/guardy-task27-lintcache: evidence added after completion.
- git diff --check: PASS. Full final make/lint/release verification remains T12.

README/Godoc/CONTRACTS/MIGRATION record supported representation, explicit policy,
nil fallback and canonical delivery. Fallback Kind describes its checked value;
Decision/error preserve the original blocked policy decision (existing contract).

Review correction: boundary tests establish 64 dereferences allowed, 65 fault
for both concrete pointer values and typed nil pointer chains. Initial 64-depth
failures recorded in t02-depth-before.txt. Implementation now counts only
dereference operations, including a shared budget for nil type traversal.
Fresh `go test -count=1 -race ./...` PASS on corrected runtime (t02-regression.txt).
All root/ext/guardytest/internal packages executed, not just targeted tests.
Root lint evidence follows after its final check; both reviewers repeat acceptance.

Final root golangci-lint: exit0, 0issues; t02-lint.txt.
