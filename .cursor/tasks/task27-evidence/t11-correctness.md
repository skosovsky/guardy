# T11 independent correctness acceptance

Verdict: **PASS**. No unresolved confirmed defect found in the final T11 changes against accepted T10 `b98ede2`. Reviewed README.md, MIGRATION.md, CONTRACTS.md additions and new untracked consumer_docs_example_test.go. No implementation changes made by this reviewer. Other acceptance verdicts were not consulted.

## Checks and findings

- Literal root-only README Quick Start independently ran with `go run /tmp/guardy-task27/t11-readme-main.go`, exit 0, output `Contact [email]`. It writes only the approved projection; its literal replacement is explicitly described as an example rather than a detector.
- Independently ran `go test -race -run 'Example_guardedConsumer|TestDocumented|ExampleGuard' -count=5 ./...` with task27 Go caches: exit 0. Six string outcomes suppress deny, correction, report-only fault and Go error at actual guarded and low-level sinks, preserve causes, and consume authoritative output instead of the deliberately wrong MutatedText. Struct/lens test preserves identity and original value. Existing HTTP executable example also passes.
- Checked low-level Run recipe against canonical decision/fault behavior: it checks non-nil Go error, report-only SystemFault, terminal deny and retry before consuming output. Destination approval remains explicitly caller-owned for this low-level use.
- Checked constructor names and signatures, renamed sequential/parallel phase and OTel labels, typed prerequisites and nil semantics, explicit destination policy and classifier/fallback requirements against current source. Migration separates the unreleased candidate from earlier transitions and does not imply a published /v2 module.
- Checked HTTP cap/status/close/borrowed replacement claims against its accepted ownership contract and current implementation; streaming byte, JSON/newline framing, expanded output, partition, mutex/reentry and cooperative-cancellation claims against StreamConfig/release/scanner contracts. No unsafe fallback or rollback claim found.
- Checked optional import/module inventory, root-only dependency distinction, matcher finite-coverage statements, semantic conformance versus statistical quality, outer-slice-only alias constraints, vault and OTel setup/privacy statements against current APIs. No unsupported safety/authorization claim found.
- Independently verified all actual local Markdown links and anchors in README/MIGRATION/CONTRACTS after excluding code literals: PASS. `git diff --check` exit 0.
- Implementer reported terminal all-19-module race and plain make lint exit 0. Those global executions are supporting evidence; this reviewer independently reran focused executable documentation checks above.

## Verification limits

Acceptance covers T11 consumer documentation and the known executable fixtures. It does not prove unknown host wiring, custom serialization, detector accuracy, third-party providers, external documentation availability or release publication. No live detector/provider was called. T12 full release and final remediation verification remains required. A root test alone excludes nested modules; this review does not claim its focused rerun covers them.
