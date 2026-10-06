# T04 independent correctness acceptance

Verdict: PASS. No unresolved confirmed defects in T04 (R04, D18, D19).
Reviewed source diff against f221001, final untracked stream_unit_boundary_test.go, and the four updated contract/measurement documents. No implementation changes were made by this reviewer; no other reviewer's conclusions were used.

The newline admission branch restricts both accepted slices and ring allocation to min(MaxPendingBytes, MaxUnitBytes). Exactly MaxUnitBytes without a delimiter stays pending, and Complete invokes validation/delivery once. An additional byte or newline faults before appending it. Complete newline units drain before subsequent input admission, so the bound does not prevent legitimate multi-unit Writes. The JSON branch and its separate bounded lookahead are untouched. A smaller pending budget remains its own limit, not an implicit change of the unit budget.

Independent validation:
- Fresh race run, count=1: new exact-tail and overbudget-partition tests, existing newline/JSON partition matrix, ring wraparound and framing work bounds, all-phase cancellation, and cancellation after an ignoring validator: PASS (1.533s). Raw log: /tmp/guardy-t04-reviewer/checks.log.
- External executable exercised 35,012 admissible newline configuration/partition cases: all binary a/newline strings through length 7, unit limits 1–5, pending limits 1–8 (including pending < unit), and every two-part split. Complete reproduced the entire input and reported capacity <= min(unit,pending): PASS. Source/log: /tmp/guardy-t04-reviewer/main.go and exhaustive.log.
- Baseline log confirms original exact-tail regression. Author's root race, targeted race, lint and before/after benchmark logs inspected: PASS, lint 0 issues. git diff --check: PASS.

D18 accurately describes atomic whole-Write input-budget admission and irreversible earlier prefixes; the regression demonstrates both single-Write rejection and a previously released prefix. D19 reflects the actual mutex ownership: ScopeFactory, validators, writer and observer are invoked while holding it; Abort/Outcome also acquire it. Cancellation remains cooperative and no detached timeout workers were introduced.

Limits: liveness for non-cooperative host callbacks cannot be guaranteed; reentry is prohibited by contract, not detected at runtime. This review checked lock/callback placement statically and ran the existing cooperative cancellation checks; it did not leave a deliberately deadlocked callback running. Benchmarks are one-operation samples and are not throughput claims. Transformed output/fallback framing remains T05 scope, not accepted by this T04 verdict.

## Repeated final-diff acceptance after stronger tests

Final verdict: PASS; no unresolved confirmed defects. Re-read final source and updated test file after the added all-composition boundary matrix and real-validator Complete-once test. The temporary test-helper syntax defect (`range 1 << {`) was confirmed by an independent failing compile, reported, and corrected to a named `partitionCount := 1 << (len(input)-1)` before this verdict. It is resolved, not waived. Runtime source remains identical to the reviewed T04 implementation.

Fresh independent targeted race run (`-count=1`, including the new Complete-once test, boundary compositions and the previous JSON/work/cancellation regressions) passed in 1.880s: /tmp/guardy-t04-reviewer/final-checks.log. Final author root race passed in 5.772s and final lint log contains 0 issues without formatter warnings. git diff --check passed. The new test explicitly observes zero validation calls and zero delivered bytes before Complete for every exact-tail composition, then exactly one call and one delivery after Complete and repeated Complete. The composition helper enumerates all 2^(n-1) nonempty compositions for its short nonempty boundary inputs, in addition to existing empty-write partitions. This repeated verdict applies to the corrected final diff.

After final formatter-only line wraps, another independent fresh targeted race run passed in 1.610s; the latest complete root race log passed in 6.085s and lint remained 0 issues. No semantic differences or new defects. Final PASS is reaffirmed on this formatted diff.
