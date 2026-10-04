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
