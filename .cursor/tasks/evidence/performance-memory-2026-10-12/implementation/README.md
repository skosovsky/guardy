# Guardy allocation optimization evidence

Scope: Pipeline.Run empty phases/direct scoped policy invocation; typed JSON Bind admission without value-tree materialization. No release, consumer pins, module dependency changes, schema cache or streaming limits changes.

Frozen acceptance: requirements.md (26 rows). Source association: measured-source.json; exact candidate commit and final independently reviewed verdicts are recorded in final-candidate.txt, reviewer-a.md, reviewer-b.md and acceptance.md after candidate freeze. Review records are local sidecars so they do not change the reviewed commit.

## Reproduce

Run from a checkout of the final candidate. Use Go1.27.1 on Darwin arm64, Apple M1 Max, GOMAXPROCS10, GOWORK=off. The scripts use a writable /tmp cache and common flock. Install benchstat under /tmp from golang.org/x/perf v0.0.0-20261009192801-be2c69fb417e; library module dependencies stay unchanged. Other host activity is not controlled; no production latency SLA is claimed.

Prepare baseline using `git archive 0a99ffbd296c281b25da7a34acaeb3a029936f0a` extracted into `/tmp/guardy-performance-baseline-20261012-final`. Scripts copy the exact same public-API benchmarks to baseline. Warm invocation includes every Bind parse/bind and every Run phase/adapter operation; fixtures exclude only construction. Cold construction is separate.

```sh
python3 .cursor/tasks/evidence/performance-memory-2026-10-12/implementation/run_checks.py
python3 .cursor/tasks/evidence/performance-memory-2026-10-12/implementation/run_benchmarks.py
python3 .cursor/tasks/evidence/performance-memory-2026-10-12/implementation/run_timing_followup.py
python3 .cursor/tasks/evidence/performance-memory-2026-10-12/implementation/run_bind_round2.py
python3 .cursor/tasks/evidence/performance-memory-2026-10-12/implementation/run_mixed_followup.py
```

`run_checks.py` runs uncached race gates for root, jsonschema, jsonredact, build, guardyotel and both integration modules. Root covers deterministic stream work/capacity, faults, cancellation, observer and scope isolation. Final-root-lint.txt checks the final root source. Final-fuzz.txt records strict admission/error-diagnostic/custom-callback differential fuzz for the revised scanner. Files under final-verification/ record fresh checks explicitly tied to the frozen candidate SHA.

Main benchmark samples:10 per workload,300ms. Disputed cold/error Pipeline and invalid/mixed JSON paths receive20 extra samples per workload with balanced ABBA order. All raw samples and benchstat outputs are retained; repeated benchmarks never run concurrently with another participating Go process. See findings.md for all issues and resolutions. round1/ preserves the rejected provisional JSON scanner measurements and historical focused logs, not final candidate acceptance. Reviewer-B preliminary fuzz is labeled preliminary/reconstructed and is not used as final raw evidence.

Counters are total transient allocations, not retained heap or a leak diagnosis. Source checks and allocation slope demonstrate invocation adapters disappear; no request scope or buffers are cached. Final reviews certify checked requirements and no found unfixed errors, not absence of all possible defects.
