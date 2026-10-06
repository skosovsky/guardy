# T07 independent correctness acceptance

Verdict: **PASS — no unresolved confirmed defects found**.

Reviewed against baseline `5dda01b` (`fix: fault evidence`): runtime diff in
`ext/jsonredact/jsonredact.go`, new `aggregation_contract_test.go`, README,
CONTRACTS and MIGRATION; original R06/R07 and all three T07 criteria. This reviewer
was not involved in implementation and did not read another reviewer's verdict.

## Findings and checks

- Sorted object keys and recursive array index order make callback visitation
  reproducible independently of source member order. Shared ComposeReports keeps
  strongest completed enforcement and the last equal-priority diagnostic, using
  its documented action-aware enforcement ranking.
- Correction/deny no longer stop siblings; system fault does. Fault stops leaf
  invocation even across nested containers. Unvisited values are not reported as
  successful. Host callbacks receive original leaf strings, not earlier rewritten
  sibling values.
- Mandatory report outcomes return the original byte-for-byte input and clear
  MutatedText. Failed callback output/report is discarded on Go error or parent
  cancellation; completed history is projected through T06 carrier, with causes
  preserved. Explicit nested completed carriers compose with prior adapter
  observations using existing payload-kind priority (technical > internal > safe).
- Shadow deny remains an observation; shadow report-only fault and invalid action
  still fault, stop traversal and suppress boundary/fallback delivery.
- Optional-module boundaries remain unchanged; only standard library `slices` was
  added. No domain dependencies entered core.

Independent checks:

1. `go test -race -count=10 ./...` from `ext/jsonredact`: PASS.
2. Separate external-package probe in `/tmp/guardy-task27/t07-review-probe_test.go`,
   race x10: PASS. Covers equal-priority terminal diagnostic ties, continuing deny
   traversal, stopping invalid report faults across nested containers, source/mirror
   suppression, and nested completed-error evidence from a failed leaf.
   Log: `/tmp/guardy-task27/t07-correctness-probe.log`.
3. `go test -run '^$' -fuzz '^FuzzRedactionPreservesValues$' -fuzztime=3s -parallel=2`:
   PASS, 89,558 executions. Checks exact decoded numbers/other values, redaction and
   original source on enforced denial.
4. `git diff --check`: PASS. Inspected task runner logs confirming all 19 module
   race suites PASS and all module lint invocations have zero issues.

A first draft of the nested evidence probe incorrectly assumed internal classification
outranked technical. That probe expectation was corrected against the existing
payload.go contract, then the probe passed. This was not an implementation defect.

## Limits

These results are bounded evidence, not proof of absence of every defect. Arbitrary
host callback side effects, non-cooperation and panic recovery are not provided by
this adapter; the first two limitations are documented and panic handling belongs
to T08. Cancellation is cooperative and cannot make an already completed return
atomic with a concurrent later cancel. Runtime cost of scanning all policy siblings
is deliberate; fault/cancel still stops detector calls. No real publish/push or
release preparation was performed in this review (final T12 gate).
