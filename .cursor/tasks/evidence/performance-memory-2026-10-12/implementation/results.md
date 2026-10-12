# Verified implementation and performance results

F1: Run only creates contexts for nonempty phases. Policies without middleware invoke the construction-checked policy directly through the same completion-flag panic boundary, receiving invocation-local scope. Scoped middleware wrappers are still built eagerly for every Run; no shared scope cache. Existing streaming metadata adapter path remains for stream construction.

F2: a bounded preliminary grammar/key scanner rejects duplicates with decoded-key equality; standard json.Valid remains the final authority before typed encoding/json decoding. Values are not materialized into a temporary tree on valid Bind. Invalid diagnostic fallback uses Decode once; trailing values receive the same original exactly-one-value error. Number precision, BYOT/custom decoding and callback syntax admission remain unchanged.

| Workload | baseline allocs | candidate allocs | baseline B/op | candidate B/op |
|---|---:|---:|---:|---:|
| Run0 policies |4|2|128|32|
| Run1 policy |6|3|176|80|
| Run8 policies |13|3|512|80|
| Run64 policies |69|3|3328|80|
| Bind256 integers |294|21|21041|7195|
| Bind4096 integers |4155|32|475925|171053|

Bytes are medians of10 samples; JSON byte accounting varies slightly between samples. Exact raw observations are in candidate/baseline files and allocation-summary.json. These satisfy the original task thresholds, including B<=baseline across all Run sizes and strictly lower Bind allocations/bytes256/4096.

Permanent tests assert no per-policy allocation slope and64 concurrent distinct scoped requests on one pipeline, with/without invocation middleware. JSON tests/fuzz cover malformed/trailing/duplicate/escaped/Unicode replacement/whitespace keys, numbers, scalars/null, depth10000/10001 and custom binding callback/error semantics. Root uncached race gates cover all contract and deterministic stream work/capacity guards; all7 tested modules PASS. Root lint PASS0issues. Fresh strict diagnostic fuzz30s PASS, raw final-fuzz.txt; historical provisional focused logs are not attributed to revised source.

Timing: local host is noisy, so no absolute latency/SLA claim. Main samples10x300ms; increased balanced ABBA20 samples resolve disputed Pipeline construction/error, malformed/trailing and mixed Bind medians. No confirmed statistically significant degradation remains in checked valid/error workloads. All rejected round1 evidence and findings remain in round1/ and findings.md.

Final source bytes are recorded in measured-source.json. Exact final candidate SHA, SHA-bound verification and the two independent acceptance verdicts are saved beside this report after the candidate commit. A must approve26/26=100%; B must approve with no found unfixed errors. This result is review evidence, not a mathematical guarantee of all possible defects.
