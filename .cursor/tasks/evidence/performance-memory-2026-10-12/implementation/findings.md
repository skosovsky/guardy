# Findings ledger (all rounds retained)

- A-PERF-01 / R25, round1: Bind malformed +58.91% (p<.001, n10), 30→46 allocations, 1312→~1809 B; trailing +49.85% (p<.001), 13→23 allocations, 1000→1320 B. Confirmed double diagnostic work: json.Valid rejects with allocated syntax error, followed by original Decode diagnostics. Status: OPEN, implementer assigned fix; no safety waiver.
- A-TIMING-02 / R25, round1: pipeline cold construction0 +8.55% (p=.015), construction1 +88.15% (p<.001), sequential error +9.63% (p=.035). Large host timing spreads (up to143%) and unchanged construction source. Status: OPEN, increase samples with balanced serial order before attributing to code.
- B-OBS-01: ordinary no-middleware Run no longer queries StreamCapabilities metadata. Reviewer B checked CONTRACTS.md stream/construction requirements: no per-Run metadata callback obligation. Classified non-defect; scoped middleware factories still invocation-local. No corrective source change requested.

Independent review and performance statistics do not guarantee zero unknown bugs or production latency.

A-TIMING-02 resolution: ABBA20 samples per workload, same F1 source, serialized baseline/candidate binaries. Construction0 p=.289 (78.64→77.70ns), construction1 p=.644 (100.9→100.0ns), error p=.449 (552.0→546.3ns); no confirmed degradation. All initial logs retained; Reviewer A independently verified follow-up harness/results. Status: CLOSED (timing confound not corroborated by increased balanced sampling).

ANN-SYNTAX-03, interim R25 fix: key scanner captured token start before skipping whitespace; a comma followed by whitespace could yield the wrong unescaped key substring and miss a duplicate. Reported to implementer; implementer had concurrently corrected it before next lint. Required regression: identical raw keys with different whitespace, escaped collisions with whitespace, valid spaced/nested documents, differential fuzz. Status: FIXED IN SOURCE, awaiting final regression gates and both reviewers. This was an in-progress implementation finding; no final candidate approved with it.

A-EVIDENCE-04: implementer-reported /tmp focused log paths still held initial08:19 scanner outputs, while revised strict diagnostic fuzz was only in tool transcript. Historical files moved under round1/ and not attributed to final source. Resolution planned: fresh strict fuzz with raw stdout after frozen candidate commit, plus final SHA-bound race verification. Status: OPEN until final-fuzz.txt PASS.

A-PERF-01 resolution: round2 full matrix has original malformed30alloc/1312B and trailing2alloc/40B. Increased ABBA20 invalid samples give malformed p=.941 (1.454→1.404us), trailing -87.25% (p<.001); source fix removes invalid json.Valid error allocations. Main malformed cohort remained noisy and appeared slower; it is retained and resolved by balanced increased samples. Mixed main median was noisy +39%, p=.138; additional ABBA20 showed -26.80%, p=.038. No confirmed statistically significant degradation remains across checked workloads. Status: CLOSED, pending both frozen-SHA verdicts.

ANN-SYNTAX-03 resolution: revised whitespace key regressions, all root race contract/seed checks and fresh strict diagnostic fuzz30s PASS. Both preliminary reviewers inspected the fix. Status: CLOSED, final SHA review will repeat source association.

A-EVIDENCE-04 resolution: historical /tmp logs moved to round1/; final-fuzz.txt is fresh raw execution of revised strict diagnostic/callback admission fuzz30s,08:41:41Z,52000 executions,exit0. Final root race and lint cover revised source and measured-source.json hashes match. Status: CLOSED. Frozen-SHA verification/independent verdicts will be saved as separate sidecars after commit.
