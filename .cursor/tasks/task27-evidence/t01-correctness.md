# T01 correctness review

Verdict: **PASS**. No unresolved confirmed correctness defects found in the
staged T01 implementation. This is a scoped review, not proof of absolute
absence of errors. Reviewer did not participate in implementation and did not
read the completeness review.

## Scope and findings

- Reviewed `git diff --cached` for scope/policy runtime changes, regression tests,
  migrated callers, README, CONTRACTS and MIGRATION against T01 / R01 / D07.
- `ScopeRequirement.accepts` now matches `value.(T)` for requirements produced by
  typed keys: exact dynamic concrete type, `Implements` for interface keys.
  Named and unnamed map/slice/struct/function pairs do not silently convert.
  Nil interface is rejected, typed nil retains its type; typed nil pointer
  implementations satisfy interfaces without invoking a method.
- `Pipeline.Run` checks compiled requirements before its first raw validation
  callback. Args and JSONArgs precheck their union of raw/final requirements
  before processing as well. The scope change does not bypass these entry gates.
- The new external-package regression matrix exercises direct Lookup and Run,
  then guarded delivery independently, proving callback suppression,
  `ErrScopeIncompatible`, `ErrValidatorFailed`, `ScopeTypeError`, `PolicyFailure`
  cause/Decision agreement, canonical SystemFault and zero deliverable value.
- Scope error construction/projection is unchanged. Invalid types cannot become
  policy violations or activate successful boundary delivery/fallback.
- Deprecated untyped constructors and their private helpers are removed. A
  whole-repository name search finds only the intentional MIGRATION removal
  notes. Low-level ExecutionScope.Lookup remains available.
- The preserved public literal `ScopeRequirement{Key, Type}` path retains its
  previous string-based behavior; the exact assertion guarantee documented here
  is for requirements originating from typed ScopeKey. No new regression found
  in that unchanged compatibility path.
- Baseline log shows all five named/unnamed failing cases invoked a callback and
  returned pass before the fix, rather than merely failing compilation.

## Independent checks

All commands used `GOCACHE=/tmp/guardy-task27-gocache` and
`GOMODCACHE=/tmp/guardy-task27-modcache`.

| Command | Result |
|---|---|
| `go test -race ./...` | PASS (cached root/ext/guardytest/internal-jsondoc results) |
| `go test ./build/... ./integration/...` | PASS (cached results) |
| `go test -count=1 -race -run 'TestScope\|TestTypedScope\|TestRequiredScope\|TestCompositionPreservesAllTypedScopeRequirements\|TestStreamRequiredScopeBeforeValidation' .` | PASS, fresh execution; repeated on final formatted staged tests, 1.238s |
| Removed API/helper whole-repository search | Only MIGRATION removal notes remain |
| `git diff --cached --check` | PASS |

The independent fresh targeted race run covers the new assertion matrix, typed
scope metadata/mismatch tests, pre-policy suppression, incompatible composed
requirements and stream scope prerequisite tests. Existing submitted evidence
also records all 13 example modules and the policy example; those example runs
were not repeated by this reviewer. Full-module lint, final API migration
consolidation and release candidate verification belong to later stages and
were not validated as complete in this T01 review. No new fuzz campaign or
stateful/custom-scope concurrency proof was performed.
