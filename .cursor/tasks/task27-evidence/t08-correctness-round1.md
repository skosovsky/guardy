# T08 independent correctness acceptance — round 1

Base f392070; inspected final working diff, T08 five criteria, D04-D06/D10-D12, construction/panic/fault regressions and consumer migration. No implementation edits or other reviewer verdict inspected.

## Verdict: CHANGES REQUIRED

Confirmed P1: a parallel Validate panic whose error value is context.Canceled (or a wrapper of cancellation) can be suppressed as cooperative sibling cancellation after a sibling deny. `ValidatorPanicError.Unwrap` (validator_panic.go:16-18) exposes the original cancellation cause. `cancellationOnly` (errors.go:40-41) strips every ordinary wrapper, thereby losing the independent panic marker. Pipeline suppression (pipeline.go:505-510) then returns nil and leaves terminal deny rather than required SystemFault. This contradicts the all-phase panic contract, retains neither panic error nor fault and can permit remediation fallback at a host boundary.

Deterministic independent external probe `/tmp/guardy-t08-correctness/main.go` uses two parallel rules: one returns ActionBlock, second waits ctx.Done then panic(context.Canceled). Command `GOSUMDB=off GOCACHE=/tmp/guardy-task27-gocache GOMODCACHE=/tmp/guardy-task27-modcache go run -mod=mod .` exits 0 with `fault=false err=<nil> panic=false ... terminal_deny`. Need retain ValidatorPanicError as independent fault throughout cancellation classification, including nested carriers/wrappers, plus regression after sibling deny. Both acceptances must repeat on final diff.

Other inspected paths: nil/typed-nil phase rules, options, Judge/policy constructors; Must wrappers; middleware layer nil and original pipeline immutability; scoped runtime nil wrappers; phase/OTel label clear break; safe panic diagnostics and explicit cause; invalid report/shadow normalization; negative routing counters/stateless fallback proposal; report-only and Go-error boundaries. No additional confirmed defects found in these paths.

Independent targeted `go test -race -count=5 -run 'Test(Pipeline|CorePolicy|ScopedRuntime|InvalidReports|RunFaultChannels|Route)' ./...` PASS (terminal exit 0); log `/tmp/guardy-t08-correctness/independent-tests.log`. Existing tests do not cover confirmed panic/cancellation collision. Root all19 race log contains ALL 19 MODULES PASS and lint log shows 0 issues through build; authoritative terminal confirmation belongs to root.

Limits: this is finite source inspection/tests, not proof of absolute error absence. Trusted constructor/observer/host callback contracts, irreversible alias/side effects and noncooperative callback termination are excluded by documented contracts. Runtime parallel observer panic was inspected outside Validate recovery; not executed in-process because it aborts its goroutine/process. Final R08/R09 documentation execution, ownership and isolated release work remain later tasks as planned.
