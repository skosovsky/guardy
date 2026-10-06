# Final corrected stream benchmark comparison

Corrected T12 release.go source based on e5038d2; last test-only assertion reformat does not change measured production source. Apple M1 Max/darwin arm64, benchtime1x count3; concurrent checks make ns/op unsuitable for performance claims. Compare deterministic work/capacity/validation counters with accepted saved baselines/postop. No statistical latency/throughput certification.

## .cursor/tasks/task27-evidence/t04-bench-before.txt
Common cases: 84. Work/capacity/validation counter differences: 3.
BenchmarkStreamAdversarial/processor/long-newline/size-1024/split-17 peak-pending-B/op: 1026.0 -> {1025.0}
BenchmarkStreamAdversarial/processor/long-newline/size-65536/split-17 peak-pending-B/op: 65538.0 -> {65537.0}
BenchmarkStreamAdversarial/processor/long-newline/size-8192/split-17 peak-pending-B/op: 8194.0 -> {8193.0}

## .cursor/tasks/task27-evidence/t04-bench-after.txt
Common cases: 84. Work/capacity/validation counter differences: 0.

## .cursor/tasks/task27-evidence/t05-bench-before.txt
Common cases: 84. Work/capacity/validation counter differences: 0.

## .cursor/tasks/task27-evidence/t05-bench-after.txt
Common cases: 84. Work/capacity/validation counter differences: 0.
