# T05 correctness acceptance

Reviewed independently on 2026-10-06 against HEAD `f07f82b`. Reviewer did not implement changes or inspect another reviewer's conclusions. Scope: R05, D17, four T05 acceptance criteria; release.go diff, new stream_output_contract_test.go, contracts, README and measurements.

**Verdict: PASS. No unresolved confirmed defects found in the reviewed scope.** This is bounded review evidence, not proof of absolute absence of bugs.

## Contract and code inspection

- `validRepresentation` requires UTF-8 and exactly one valid JSON value. Validated JSON units additionally require object/array top level, including surrounding JSON whitespace. Whole-response permits all six standard JSON classes. Best-effort JSON remains rejected by CompileStream; non-JSON partial release has no newline unit boundary requirement.
- Source units, approved transformed values, fallback candidates and approved fallback values share that profile-aware check. Invalid candidate is rejected before validation. Invalid transformed output is a processing fault before transport. Newline unit transformation cannot inject multiple release units.
- Source/fallback input retains MaxUnitBytes bounds. Approved unit and partial expansion checks MaxUnitBytes before any writer call. Whole-response output expansion intentionally uses MaxOutputBytes. Cumulative source release plus separate fallback cannot exceed output budget. JSON framing whitespace and UTF-8 bytes count by byte length.
- New failure calls retain the checked delivery Decision, including earlier payload classification, then promote DispositionNone to SystemFault. Limit and framing terminal categories cannot activate fallback. Separately checked fallback leaves original terminal source outcome unchanged and is at most once.
- Existing cancellation check follows GuardDelivery and precedes writer. Writer short counts/errors still use transport accounting; prior approved prefixes cannot be recalled. No stream framing state/admission mutation was introduced by T05.

## Checks

| Evidence | Result |
| --- | --- |
| Independent `go test -race -count=1 -run 'Test(Stream\|Transport\|Cancellation\|Release\|JSON)' ./...`, caches in /tmp/guardy-task27; `t05-correctness-check.log` | PASS, root and three root subpackages |
| New matrix inspected and independently run above: normal/transformed/fallback/fallback-transformed × number/bool/null/string/object/array, whitespace, malformed/invalid UTF-8 × unit/whole | PASS; unsupported values zero writes |
| Exact/expanded JSON output and unit/partial/whole expanded output; candidate limit before rule invocation | PASS |
| Existing partition redaction budget, fault/cancel/fallback after prefix, short transport, all-phase faults and cancellation, bounded scanner work tests | PASS in independent run |
| Independent public API probe `go run -race /tmp/guardy-task27/t05-correctness-probe.go`; `t05-correctness-probe.log` | PASS: irreversible `{}` prefix, later unit expansion limit retains InternalControlSignal with host classifier, limit forbids fallback, cancelled fallback has zero writes, short fallback writer called once with correct released-byte count and sticky original outcome |
| Baseline `t05-baseline.log` inspection | Reproduces scalar fallback/transformed release and expanded unit delivery defects before fix |
| Implementation verification logs inspected: `t05-root.log`, `t05-otel.log`, `t05-lint.log` | Root race and optional OTel race PASS; lint 0 issues |
| `t05-bench-before.log`, `t05-bench-after.log` inspected | Both benchmark matrices PASS; single-sample timing has no performance claim |
| `git diff --check` | PASS |

The independent probe initially expected InternalControlSignal despite the selected UserTextClassifier correctly upgrading JSON to TechnicalPayload. The expectation was corrected by supplying an explicit host classifier returning safe text; the report then demonstrates that the existing InternalControlSignal is preserved on the new post-validation limit fault. This was a probe assumption, not a library defect.

## Limits

Full all-module lint/release packaging verification is reserved for T12. This review independently exercised the root stream paths, not every nested example/module. Runtime forced termination of non-cooperative callbacks is outside the documented stream contract. No absolute no-bug claim or statistical performance conclusion is made.
