# T10 independent correctness acceptance

Verdict: **PASS**. No unresolved confirmed defect in the reviewed T10 change.
Reviewed the working tree against `be2d8c7` (`docs: ownership`), including newly added HTTP, OTel and integration tests. This review covers T10 / D20–D22 and the T10 enforcement-fixture portion of D24. It does not accept T11 or T12. No other acceptance report was consulted and no implementation was changed.

## Correctness findings

- OTel construction now fails before returning usable middleware on counter/histogram errors, missing instruments, typed-nil instruments/providers and nil options. ConfigurationError public text is safe while errors.Is/As retain the original provider cause. MustMiddleware preserves that same cause. Plain nil meter/tracer is an explicit disabled channel. The runtime validate/reportAttrs/recordMetrics/payload paths have no semantic changes in this diff; existing real SDK privacy/decision/stream tests also pass.
- HTTP consumed-input admission uses MaxBytesReader rather than ContentLength. Positive configuration is enforced; exact cap succeeds and an extra byte yields 413. Original consumed wrappers close before replacement on success, oversize/read errors and cancellation. Extraction borrowed/current replacement bodies close before validation; failed injection bodies close before next; captured handoff body closes on handler return. The owned wrapper makes framework repeated close idempotent without relying on comparable concrete ReadCloser values.
- Cancellation is checked at admission, after extraction, through scope/Run, after injection and before next. Wrapped callback cancellation remains a fault with a live parent. Read/extraction errors, scope incompleteness, deny/retry and faults retain the documented HTTP status distinction; private callback/close details are not rendered publicly.
- Pass restores original bytes and replay metadata; redaction passes authoritative returned T to the injector. Intermediate callback bodies and handler-created replacements are explicitly caller-owned. A late body Close failure cannot retroactively rewrite a response. Cooperative callback execution and no rollback are stated rather than promised as resource/time isolation.
- Actual guarded-output integration fixtures gate an actual sink on approved Projection, suppress partial/Go-error/report-only-fault/cancellation/deny/retry outcomes, and prove the handler side effect still occurs exactly once. Low-level WrapOutput preservation of unvalidated partial handler output is explicitly named and tested. BoundaryProfile is not turned into an enforcement registry.
- Go consumers of changed Guard/NewMiddleware signatures are migrated; README/MIGRATION/CONTRACTS show the fallible APIs or explicit Must wrappers and consistent cap/status/ownership semantics.

## Independent verification

All processes completed with exit 0, using GOCACHE=/tmp/guardy-task27-gocache and GOMODCACHE=/tmp/guardy-task27-modcache:

| Command / working directory | Result | Log |
| --- | --- | --- |
| `go test -race -count=5 -run 'TestHTTP' ./...` / root | PASS, HTTP tests repeated five times | `/tmp/guardy-task27/t10-correctness-http.log` |
| `go test -race -count=5 ./...` / integration | PASS, full integration module repeated five times | `/tmp/guardy-task27/t10-correctness-integration.log` |
| `go test -race -count=5 ./...` / ext/guardyotel | PASS, full optional module repeated five times | `/tmp/guardy-task27/t10-correctness-otel.log` |

Reviewed the producer's baseline reproduction logs and source/contract diff. Final recheck confirms the directive-only corrections: serveGuard suppresses funlen; the HTTP failure matrix suppresses gocognit/gocyclo/cyclop. No functional source changes were reported after independent focused tests. Reviewed t10-implementation.md and the saved t10-all-lint.txt (all module sections report 0 issues); the lead agent confirmed the actual all19 make lint process exit 0. The lead agent subsequently confirmed actual final all19 race process 63079 exit 0, saved in t10-all-race.txt, and diff --check exit 0. Reviewed the saved race log; these overall gates are separately attributed and not inferred from the independent focused commands. Correctness PASS applies to the final runtime diff.

## Verification limits

No external OTel exporter/backend or live net/http socket test was executed here. Ownership verification uses tracking ReadCloser and httptest; it proves framework wrapper handling, not universal server socket lifecycle. Callbacks/providers are trusted to respect documented borrowing, synchronous cooperative execution and no-panic preconditions outside Validate. Injector representation correctness and caller-owned intermediate replacements cannot be proven generically. These stated limits are consistent with the T10 contract and do not leave a confirmed defect.
