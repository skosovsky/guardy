# Frozen acceptance checklist

Frozen before implementation acceptance. Each row is one required PASS; FAIL, NOT RUN and partial results count zero. Reviewer A: 100 × PASS / 26. Reviewer B independently examines the same final candidate commit. Findings and rounds must remain visible. No release, consumer pin or dependency changes.

| ID | Mandatory requirement |
|---|---|
| R01 | Permanent library benchmarks/probes follow AAA and assert outcomes. |
| R02 | Cold construction measured separately from warm Run/Bind; no moved invocation cost in fixtures. |
| R03 | Empty Run same-run allocations <4; 64 no-middleware policies <69. |
| R04 | Pipeline B/op <= same-toolchain baseline for 0/1/8/64 policies. |
| R05 | Structural/allocation regression proves removal of per-rule invocation adapters. |
| R06 | Required scope checked before validation; missing scope fails closed. |
| R07 | Middleware policy factories reconstructed per invocation; deterministic capability contract preserved. |
| R08 | Phase contexts, callback/observer order and redaction propagation preserved. |
| R09 | Short circuit, cancellation and panic recovery preserved. |
| R10 | Completed observations and canonical decisions preserved on faults. |
| R11 | Same immutable pipeline concurrently reuses distinct request scopes without cross-talk. |
| R12 | Bind 256/4096 allocations strictly below 294/4155. |
| R13 | Bind 256/4096 B/op strictly below 21042/~475936. |
| R14 | Duplicate keys, including escaped-key collisions and Unicode replacement collisions, rejected. |
| R15 | Exactly one value, malformed input and trailing values rejected. |
| R16 | Number precision, null, scalars and interface UseNumber semantics preserved. |
| R17 | Deep nesting acceptance/rejection equivalent to existing encoding/json syntax contract. |
| R18 | Typed/custom UnmarshalJSON semantics preserved; no callbacks before syntax admission. |
| R19 | Object, mixed and nested payload benchmarks included, with assertions. |
| R20 | No unbounded cache, retained request buffers/scope or new shared mutable state. |
| R21 | Root go test -race -count=1 ./... PASS, including contract tests. |
| R22 | ext/jsonschema, ext/jsonredact and build go test -race -count=1 ./... PASS. |
| R23 | Deterministic stream work/capacity and cancellation/isolation regression guards PASS. |
| R24 | Baseline/candidate exact SHAs, environment, serial count>=5 benchmarks, raw outputs and benchstat preserved. |
| R25 | Adjacent valid/error workloads have no confirmed statistically significant degradation; investigate disputed timing with more samples. |
| R26 | Reviewer A 26/26=100% APPROVE and independent B APPROVE with no found unfixed errors on same final candidate; relevant changes re-reviewed by both. |

Limits: performance counters measure allocations, not retained heap. Timing is local, not SLA. Two reviews do not mathematically guarantee zero defects. R26 includes final independent verdicts, rather than allowing A to waive missing B.
