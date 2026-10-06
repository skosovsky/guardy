# T06 completed observations

R07. CompletedObservationsError has private cause/report storage, a safe fixed error string, an independent state-free snapshot getter, and original-cause Unwrap. WithCompletedObservations combines explicitly attested prior reports with nested wrapper/join evidence; CompletedReportFromError visits every joined branch and stops at a carrier containing already-composed evidence. A raw report beside failure is never evidence. No cause/no evidence does not fabricate a completed check.

Fast/policy/slow paths consume attested history; context checked immediately after calls, including slow nil-error late cancellation. Pure sibling cancellation after a policy stop preserves completed history without adding a new independent fault. validatorFaultResult attaches prior pipeline history for nested orchestration; Run/boundary/PolicyFailure kinds agree and guarded values are suppressed.

MapSlice and JSONRedact preserve original input and accumulated completed classification on error/cancel. Map/MapJSONRawMessage discard raw failed reports and forward attested nested history without injection. JSON recursive traversal/stop semantics remain unchanged; deterministic strongest aggregation is reserved for T07.

Baseline: the regression was run against archived HEAD98a52c2 with the final old-API-only regression test; direct MapSlice trust/lost-history and allphase Pipeline loss reproduced. Final tests cover technical/internal, ordinary/wrapped cancellation, all3phases, failed stronger-kind reports, cloned state-free joined evidence, firstfault defaults, parent cancellation after nil-error return, nested pipeline propagation, sibling cancellation, mapping noinject and nestedJSON priorredaction suppression. Allmodule race/lint evidence recorded separately.

Panic recovery remains the explicit T08 core task; this error/cancellation evidence mechanism does not claim to terminate callbacks or roll back aliased caller side effects. Host code owns truth of completed attestations; untrusted input cannot construct a carrier by data serialization.

Independent correctness review reproduced direct simultaneous error/cancellation cause loss in MapSlice/JSONRedact. Both now join ctx.Err immediately after leaf return; new direct regressions require both causes and retained prior classification. Final all19modules race and make lint PASS. Both reviews repeated on this corrected final revision.
