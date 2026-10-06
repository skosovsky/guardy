# T03 implementation

R03,D12–D16 implemented. All seven built-ins now separate clean pass controls from detected-hit controls; caller fatal-pass semantics unchanged. Shared fallible construction rejects nil options/detectors, unsupported actions and unused capability options. Length zero disables a side; negative/reversed enabled bounds fail. Semantic finite scales remain caller-owned; nonfinite thresholds fail at construction.

TokenVault.Store now returns (token,error). Configured-vault error/panic/empty/identity faults, returns original input/no report, and suppresses delivery; no irreversible fallback. Explicit replacement is incompatible with configured vault. Completed Store side effects remain host-owned. Zero-value in-memory vault and allowlist no-token reversible flow tested.

Regex detects matches independently of mutation, including identity/empty/zero-width. TagPatternValidator and ClassifierValidator replace misleading old names; no legacy wrappers. Core/build/integration/examples/fixture consumers migrated. Optional JSONSchema clean pass also drops violation flags and nil options fail construction.

Evidence: baseline clean-pass regression failed for all seven before changes; final root and all19modules race pass; schema regression race pass; allmodule lint evidence recorded separately. Review found ignored replacement+vault and typed-nil SemanticFixture; both fixed with regressions before repeated independent acceptance.

Remaining R/D items are reserved for T04–T12; T03 does not claim overall goal completion.
