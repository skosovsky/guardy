# T07 JSON aggregation

R06 and recursive R07. Object keys are sorted lexicographically with stdlib slices.Sort; arrays retain index order. Independent leaf validation continues after correction and terminal deny, composing strongest completed enforcement and payload kind through shared ComposeReports. Report-only system fault/error/cancellation stops leaf checks. Equal-priority diagnostics use the last visited completed report; the first canonical report-only fault stops before later siblings. No outcome is inferred for unvisited siblings.

Any mandatory outcome returns the exact source document and clears MutatedText. Successful redactions affect only the private decoded tree; output is re-encoded only after all checks allow it. Failed callback reports/output stay discarded. Go faults/cancellation after earlier retry/deny retain prior kind via T06 snapshots, while pipeline/boundary/PolicyFailure fault beats those earlier policy outcomes.

Baseline archived HEAD5dda01b with valid old-API regression fixtures reproduces earlystop/source-key-order losses, incorrect retry vsdeny/fault, traversal/diagnostic order, and missed late Go error. The final matrix covers nested arrays/objects, reversed source key orders, 100 repeated evaluations per document, exact original failure output, retry diagnostic ties, shadow followed by explicit fault, invalid shadow reports, wrappeddeadline/parentcancel after correction+deny, successful priorredaction classification, and suppressed boundaries/no fallback.

CONTRACTS, README, migration and Godoc agree. Standard library sorting adds no domain or optional-module dependency. Optional/example/integration allmodule race/lint results recorded separately. Panic recovery remains T08; arbitrary host detector quality and cancellation of non-cooperative callbacks are not claimed.

Final verification: all 19 module race suites PASS and make lint PASS; both process handles returned exit 0. Both independent acceptance reports accept the final implementation: completeness 100% (3/3), correctness PASS with explicit limits.
