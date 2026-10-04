# Streaming release measurements

Local run on Apple M1 Max, darwin/arm64. Input: 69,632 bytes of benign UTF-8,
4,096 newline-delimited units. No network or semantic detector. Unit rules are
read-only pass; whole-response limit is input size; best-effort units are 1,024
bytes. Figures are a baseline, not acceptance thresholds or provider guarantees.

Reproduce:

```sh
go test -run '^$' -bench '^BenchmarkStreamRelease$' -benchmem -benchtime=100ms .
```

| Profile | Split bytes | Total µs/op | First byte µs/op | Validations/op | B/op | Allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| Whole | 1 | 1,329 | 1,328 | 1 | 434,272 | 41 |
| Whole | 17 | 269 | 268 | 1 | 432,855 | 39 |
| Whole | 1,024 | 166 | 165 | 1 | 425,772 | 31 |
| Whole | 69,632 | 238 | 236 | 1 | 222,498 | 20 |
| Units | 1 | 6,495 | 9.16 | 4,096 | 3,637,811 | 65,542 |
| Units | 17 | 3,799 | 4.41 | 4,096 | 3,637,766 | 65,540 |
| Units | 1,024 | 3,981 | 4.55 | 4,096 | 3,640,330 | 65,541 |
| Units | 69,632 | 6,784 | 13.93 | 4,096 | 3,711,479 | 65,540 |
| Best effort | 1 | 1,684 | 30.91 | 69 | 201,112 | 1,116 |
| Best effort | 17 | 361 | 7.42 | 69 | 200,394 | 1,114 |
| Best effort | 1,024 | 274 | 4.85 | 69 | 200,352 | 1,109 |
| Best effort | 69,632 | 354 | 14.03 | 69 | 271,522 | 1,108 |

Validation count depends on release units, not transport split count. Tiny
transport writes still add processing overhead. Whole-response first-byte latency
includes all buffering and final checks by design. Unit mode pays per-unit scope,
timeout and policy overhead; this run uses deliberately small units. Best-effort
reduces first-byte latency but provides no global secret-safety guarantee.

Allocation figures include pipeline results, scope creation, timeouts and pending
growth. They are not peak retained memory: bounded pending state is measured by
`PeakPendingBytes` and tested separately. Timing is noisy under concurrent local
work; compare multiple runs before drawing performance conclusions.

## Task 22 adversarial matrix

Measured 2026-10-04 on Apple M1 Max, darwin/arm64, Go 1.27.1;
benchmark process default GOMAXPROCS=10. Baseline source: `8b524be`.
The matrix uses sizes 1,024 / 8,192 / 65,536 bytes and transport splits
1 / 17 / full input. Escaped nested JSON rounds its body down to whole
escape fragments, so actual input bytes can be a few bytes below the named size.
Cases cover a newline arriving only at the end, nested JSON with escaped quotes
and backslashes, an unfinished JSON string, a complete value followed by long
whitespace, and many two-byte newline units in one transport write.

Reproduce the bounded matrix (one measured operation per case):

```sh
go test -run '^$' -bench '^BenchmarkStream(Adversarial|Components)$' -benchmem -benchtime=1x .
```

`framing` measures framing **and pending accumulation**, including buffer growth
and construction, while bypassing scopes, policy validation and writer calls.
`processor` exercises the actual Write/Complete path, with an empty pipeline,
explicit internal delivery permissions for text and technical JSON, and
`io.Discard` as transport. Its JSON path still checks syntax before and after
validation. Component sub-benchmarks separately measure scope construction,
empty-pipeline GuardDelivery, and discard writing. Subtracting timings is not a
reliable way to price individual callbacks: construction, cancellation timeouts,
JSON syntax checks and result allocations remain in the full processor path.

Every successful processor case checks actual released bytes. Unfinished JSON
checks a terminal `StreamIncomplete` outcome with zero released bytes, rather
than benchmarking an incorrectly accepted truncated value. The matrix also
checks observed pending bytes against its configured budget. Scope, validator
and writer remain caller-owned cooperative work; these figures cannot bound
latency of a network, semantic detector, or callback that ignores cancellation.

The baseline and after runs are single samples under concurrent local work;
they establish an algorithmic regression example, not stable throughput or CI
wall-clock thresholds. The deterministic scanner/buffer operation tests are the
acceptance check for linear work. Allocation totals include all temporary
objects and are distinct from retained pending capacity. In baseline isolated
unfinished framing, the terminal failure cleared the pending slice before its
metric was recorded; use the full processor's peak metric for that case.

Selected processor results, 64 KiB nominal size (ms/op; B/op and allocations
include validation and delivery). Full input means one Write.

| Case | Split | Before ms | After ms | Before B/op | After B/op | Before allocs | After allocs |
|---|---:|---:|---:|---:|---:|---:|---:|
| long-newline | 1 | 722.533 | 1.514 | 352,304 | 198,000 | 40 | 35 |
| long-newline | full | 0.046 | 0.155 | 132,400 | 132,464 | 19 | 19 |
| nested-json | 1 | 2688.124 | 2.935 | 671,160 | 516,856 | 72 | 67 |
| nested-json | full | 0.678 | 0.751 | 451,256 | 451,320 | 51 | 51 |
| unfinished-json | 1 | 2667.463 | 1.906 | 286,240 | 131,936 | 26 | 21 |
| unfinished-json | full | 0.190 | 0.719 | 66,336 | 66,400 | 5 | 5 |
| trailing-whitespace | 1 | 1437.211 | 1.746 | 485,232 | 330,944 | 53 | 48 |
| trailing-whitespace | full | 0.252 | 0.332 | 265,344 | 265,408 | 32 | 32 |
| many-short-units | 1 | 28.061 | 47.931 | 27,853,536 | 28,051,600 | 491,526 | 491,542 |
| many-short-units | full | 41.949 | 70.262 | 27,919,088 | 28,116,816 | 491,525 | 491,535 |

The one-byte large-unit cases remove repeated prefix scanning. The many-short-unit
case still pays scope, timeout, pipeline and delivery costs for every two-byte unit;
this sample does not show an overall throughput improvement for every partition.
The after-run operation counters record one framing visit per input byte in these
single-unit cases, and 65,536 visits for the 32,768 short units. Ring accumulation,
growth and copying into validation strings stay below four input lengths in this
matrix; these counters exclude callback allocations and JSON syntax validation.

The full processor reports peak **retained ring allocation**, including unused
capacity, bounded by MaxPendingBytes. At nominal 64 KiB with a one-byte split, long
newline and trailing whitespace retain 65,536 bytes; nested JSON retains 65,533
bytes, and unfinished JSON 65,529 bytes. With split=17, newline/trailing whitespace
growth reserves 65,538 bytes, exactly their configured pending budget. Many-short-unit peaks depend on transport
split (tiny writes retain a tiny ring, a full Write can retain 65,536 bytes). Terminal
completion or failure clears the retained allocation. The isolated framing metric
also samples retained allocation before terminal failure; baseline metrics counted
logical slice bytes and do not establish an equivalent capacity bound.

Framing plus accumulation only, split=1 (ms/op):

| Case | Nominal bytes | Before ms | After ms |
|---|---:|---:|---:|
| long-newline | 1,024 | 0.249 | 0.070 |
| long-newline | 8,192 | 11.382 | 0.170 |
| long-newline | 65,536 | 714.411 | 0.989 |
| nested-json | 1,024 | 0.682 | 0.040 |
| nested-json | 8,192 | 41.420 | 0.246 |
| nested-json | 65,536 | 2675.530 | 1.207 |

Component after-run sample: scope 2.208 µs / 48 B / 1 allocation; empty-pipeline
validation 15.541 µs / 512 B / 8 allocations; discard writer 0.625 µs / 0 B /
0 allocations. One-operation measurements include cold effects and are illustrative.
The full raw before/after matrix is retained with the task acceptance logs.
