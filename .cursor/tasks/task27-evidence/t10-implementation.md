# T10 integrations/setup — implementation evidence

Base HEAD: be2d8c7 (`docs: ownership`). Scope: all four T10 criteria, D20–22 and HTTP/OTel actual fixture portion of D24. No push/publish. T11/T12 remain required.

Contract-first: CONTRACTS.md specifies OTel construction error/cause/privacy/lifecycle and HTTP fallible construction, consumed-input cap/default/status table/body ownership/cancellation before implementation. MIGRATION.md and README consumers use new fallible APIs or explicit static Must wrappers. No compatibility aliases.

D20: optional guardyotel.NewMiddleware returns (middleware,error); safe ConfigurationError retains counter/histogram creation cause through errors.Is/As; MustMiddleware mirrors failure. Nil options, typed-nil providers, absent/typed-nil instruments reject. Plain nil Meter/Tracer intentionally disable. No partial middleware installed, no hidden business decision changes, provider effects remain host-owned. Fake instrument/provider regressions plus unchanged SDK privacy/runtime/exporter/stream tests exercise the optional module directly.

D22: Guard returns (middleware,error), MustGuard static convenience; positive WithGuardMaxBodyBytes validated, default1MiB, exact cap succeeds, overflow413 even with false ContentLength. HTTP consumes and closes original ReadCloser before replacement; callback borrowed/current replacement and captured handoff bodies have explicit ownership. Tracking-body matrix covers read/close/extraction/replacement/errors/cancellation/deadline/scope/rule/report fault/deny/retry/injection; actual handler count proves suppression. Independent GetBody readers retain separate cursors; pass restores original bytes, redaction uses authoritative T. Late close cannot rewrite committed response. No forced cancellation/host side-effect rollback promise.

D21/D24: integration/output_adapter_contract_test.go executes an actual bytes.Buffer sink only through an approved Projection across pass/redact/partial-handler/scope/Go-error/report-fault/cancellation/deny/retry. Output effects occur once and remain after denial. Low-level WrapOutput preserves unvalidated partial result with cause and skips validation/retry, explicitly documented; WrapGuardedOutput is preferred delivery recipe. HTTP real-handler matrix and existing composition/OTel fixtures prove enforcement rather than BoundaryProfile declarations.

Baselines: old-head OTel module reproduces two ignored creation sentinel errors (exit1 assertions), preserved in t10-otel-baseline*. Old-head HTTP tracking ReadCloser probe reproduces missing consumed-wrapper Close and overflow400 (exit1 assertions), preserved in t10-http-baseline*. They use the pre-change one-return APIs to avoid compile-only failures.

Final gate/reviewer results will be recorded in t10-acceptance.md only after terminal checks and both independent reviewers accept the final diff. Historical t10-progress.md and *-progress.txt describe an earlier OTel-only checkpoint, not full acceptance.
