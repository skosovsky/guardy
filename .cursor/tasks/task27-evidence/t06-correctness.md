# T06 independent correctness review — initial rejection

Reviewer: separate correctness subagent, not implementation participant. Base: 98a52c2; reviewed working diff and new completed_observations*.go / optional jsonredact test. No access to completeness review conclusions.

## Confirmed defect

**P2: direct MapSlice and JSONRedact lose simultaneous parent cancellation cause.** Both adapters branch on a non-nil leaf error before checking ctx.Err. If the leaf cancels its parent context and returns an independent error (including a completed-observations carrier), direct adapter error preserves the detector fault but errors.Is(err, context.Canceled) is false. Pipeline and Map adapters already join these two causes, so projections differ by entry point. Relevant code: ext/map_slice.go leaf invocation/error branch; ext/jsonredact/jsonredact.go walk string branch.

Independent probe module at /tmp/guardy-t06-review, with local module replacements and a leaf that calls cancel() then returns WithCompletedObservations(detectorError, internalReport), printed:

```
slice fault=true cancellation=false kind=internal_control_signal
json fault=true cancellation=false kind=internal_control_signal
```

Required correction: join ctx.Err with the leaf error before interpreting success/error; test direct adapters, original output suppression, attested prior evidence, and errors.Is for both causes. Both independent acceptance reviews must be repeated after the correction.

## Other examined paths

Private snapshot state, clear MutatedText, wrapped/joined evidence traversal, raw failed report rejection, nested pipelines, all three pipeline phases, slow sibling-only cancellation, parent cancellation, mapped injection suppression, boundary decision and PolicyFailure projections were inspected. Focused repeated root race checks are running; no other confirmed defect found at this point.

Not a claim of absence of all defects. JSON strongest/deterministic aggregation belongs to T07; central panic recovery belongs to T08 and is not accepted by this review.

Verdict: REJECT until the confirmed defect above is corrected and both reviews repeated.

## Repeated final-diff acceptance after correction

The final diff was re-read after both leaf adapters were changed to join their returned error with ctx.Err immediately after Validate, before interpreting the failed callback report. New direct AAA regressions exercise a previously completed internal observation, simultaneous independent error + parent cancellation, original input preservation, and rejection of the failed technical report.

The same independent external probe now prints:

```
slice fault=true cancellation=true kind=internal_control_signal
json fault=true cancellation=true kind=internal_control_signal
```

Independent checks completed successfully:

- Root `go test -race -count=10 -run 'Completed|FailedReports|NestedPipelineFault|SlowSiblingCancellation|MappingFault|ConcurrentErrorAndCancellation' ./...`.
- Optional jsonredact `go test -race -count=10 -run 'Completed|FaultPreserves' ./...` (the Completed matrix), plus its new simultaneous cancellation regression inspected and covered by the final all-module race run.
- Final `/tmp/guardy-task27/t06-modules.log` shows `ALL 19 MODULES PASS`, including jsonredact, root, integration, build, and examples.
- `git diff --check` passes. The all-module lint run is still finishing at this review snapshot; this reviewer does not claim its terminal result.

Final inspection confirms the separate attestation channel carries only completed checks; raw failed reports and transformed failed output are ignored in all three pipeline phases. Snapshots clear MutatedText, preserve original typed/wrapped/joined causes, and keep nested carrier projections independent of later report mutation. Map/MapJSONRawMessage do not inject failed values. MapSlice and recursive JSON errors preserve original input and prior technical/internal classification; RunResult, boundary Decision, and PolicyFailure agree and faults remain non-deliverable. Slow sibling-only cancellation preserves attested evidence without inventing an independent fault; genuine independent errors and parent cancellation enforce.

The previously confirmed defect is resolved. No unresolved confirmed correctness defects were found in the final T06 diff. This is not proof that all defects are absent. Deterministic strongest JSON aggregation (T07), centralized panic recovery (T08), malicious cyclic custom error graphs, and arbitrary host detector truthfulness are not proven by these checks.

**Final verdict: PASS — no unresolved confirmed defects in T06.**
