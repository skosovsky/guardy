# Guardy boundary and release contracts

All data APIs use caller-owned values and
canonical Decision/PolicyFailure. Guardy does not authorize execution or own storage.

## Decisions

Report control defaults are construction-only. Adapters never reapply defaults to
an existing report. Effective disposition is resolved once by the shared report
contract: unknown action/disposition/payload kind or non-finite score is a system
fault; an explicit fault wins; fatal and terminal deny cannot be downgraded by an
explicit correction. Explicit correction requires ActionRetry. Decision stores
one enforcement classification (Disposition), without redundant routing booleans.
Shadow observes only a non-fatal ActionBlock policy violation. Faults and invalid
reports always enforce, including shadow; retry and fatal escalation never shadow.
ComposeReports selects the strongest observed enforcement and joins payload kinds;
it never infers results of checks that were short-circuited. Mapping and recursive
adapters use that same contract and apply only successful redactions. Caller-owned
callbacks must not mutate aliased input while validating; generic adapters cannot
deep-copy arbitrary caller types or roll back caller side effects.

Effective disposition determines enforcement: system fault > terminal deny >
retryable correction > successful redaction > pass. Shadow block is observation.
No downstream handler or consumer runs after a mandatory fault/deny. Equal outcome
reports may have different diagnostic order in parallel validation; safety does not.
Default fault/retry error text contains the category, not raw diagnostic causes or
correction feedback. Explicit `errors.As` projections retain those fields for
host orchestration and opt-in diagnostics; do not expose the whole failure to users.

## Argument boundaries

Raw validation precedes bind. Post-bind validation runs once for value and pointer
types; nil pointer arguments fail with a correction decision. Canonical encoding
occurs after all mutations. An optional final guard validates canonical bytes and
must not change them. Integrations requiring schema enforcement must supply it;
shape metadata alone does not validate. Custom codecs own their representation
invariant. ScopeFactory runs for every invocation and host-controlled resume.
Required scope contracts are checked before any raw validator, including the
requirements of the final checker. Missing keys and incompatible typed facts
have distinct typed causes; boundary decisions remain system faults.

## Stream release

The caller explicitly selects whole-response, validated units, or best-effort.
Construction rejects invalid limits and unsupported stage/context capabilities.
Capabilities are checked on underlying rules and every applied middleware layer,
not only the outermost wrapper. An outer declaration cannot upgrade an inner rule.
For unit/partial release, wrappers must explicitly declare their actual support;
missing declarations fail construction. Scoped middleware construction must be
deterministic, because its chain is reconstructed with each invocation's scope.
Completion is a trusted method call; payload fields never control termination.
Close without Complete means abort/incomplete, not flush. All terminal operations
are sticky and writes after terminal are rejected. One processor owns state; calls
are serialized. Validation and delivery run under its lock and must not reenter it.

Whole-response buffers at most MaxPendingBytes and checks final value/channel before
release. Unit mode validates complete newline-delimited units with unit-local rules;
rules requiring cross-unit context or final-only validation are rejected. Best-effort
uses complete UTF-8 bounded chunks and permits early release, with no whole-value
guarantee. No profile silently downgrades. Context cancellation is cooperative;
validators ignoring context cannot be forcibly stopped, and are checked again before
delivery when they return.

MaxInputBytes, MaxUnitBytes, MaxPendingBytes and MaxOutputBytes count bytes; zero is
invalid. Limits are checked before copying input and before releasing expanded
redaction. Raw and output allocation inside caller validators is caller-owned.
Sequence counts attempted approved units; released bytes are actual transport bytes.
Short writes terminate as transport failure and never retry automatically. Abort
before whole-response release emits nothing. Transport failure after approval may
leave an irreversible prefix. This is reflected in the terminal outcome.

UTF-8 validity and optional JSON framing/schema checks are separate from policy.
Final-only validators are suitable for whole-response, not unit mode. Capabilities
are caller claims and cannot prove semantic detector quality.
Semantic matcher scores and thresholds must be finite. NaN/infinity means an
invalid detector outcome/configuration and yields system fault, including shadow
configurations; it is never synthesized into a benign pass. The caller supplies
detector/config identity and threshold, and evaluates statistical quality outside core.
JSON unit mode retains the last complete value and its framing whitespace until
the next value starts or Complete arrives, within configured bounds.
For JSON unit framing MaxPendingBytes must exceed MaxUnitBytes, leaving one byte
to distinguish a following value from the final unit. Framing whitespace counts
toward the preceding unit's byte limit. A value exactly at the unit limit can
complete successfully, but any following whitespace exceeds that limit before
that value is released; this is independent of transport chunking.
Validators must be read-only with respect to external side effects: validation
can occur repeatedly for units, resumed invocations and a separate fallback.
Any caller-owned side effect must have its own idempotency contract; post-handler
denial cannot undo an already performed action.

Fallback is a separate checked delivery, without recursive fallback, and cannot
mask a mandatory validator fault. Observers receive safe counters and stage metadata;
observation does not enforce. Raw payload capture is caller opt-in.
Fallback is bounded like a self-contained unit and uses the selected profile's
supported validation stage; its outcome and telemetry are separate from the
original terminal stream outcome. Correction feedback is excluded from telemetry.

## Policy facts and consumers

Source references, trust, classification and destinations belong to host-owned
scope types. Claims and confirmed facts are separate. Transformation does not
elevate trust or remove references. Missing required facts fail closed. No-provenance
mode and declassification are explicit caller policies. Evidence is bounded references,
not raw secrets. Each consumer has its own policy; only DeliveryProjection is
serialized. Fallback/replacement values go through the destination's content checks.

BoundaryProfile declares actual configured coverage, rejects unsupported mandatory
boundaries, and does not automatically intercept hosted/remote execution. External
approval binding and invocation remain the host's responsibility. Zero retry budget
means no retries; counters and execution of routes remain host-owned.

## Standard matcher and classifier limits

Wordlists use maximal Unicode letter/number/mark/underscore runs consistently for
matching and redaction, with optional strings.ToLower and no normalization. Listed
entries must be single non-empty tokens. Replacement is literal. PII matches the
finite email/+1/+7/Visa/MasterCard formats documented in README; spans are detected
on original text and overlaps merge before mutation/token storage. No generated
replacement is rescanned by the same matcher. Neither component promises global
PII or instruction-attack detection.

TextClassifier.Classify receives the execution context synchronously. Cancellation
must be cooperative; no background workers or callback termination are supplied.
The adapter checks context before/after calling it and requires finite scores on
a caller-defined scale. A late success, detector error or non-finite score cannot
be delivered. Tool-like JSON and XML-like tag matching are heuristics, not trusted
classification or authorization. GuardSpec has only explicit rule fields.

Vault tokens are lookup keys, never disclosure grants. Hosts authorize recipients
before restoration, own vault isolation/lifetime and check the final restored
payload at its destination. TokenVault contains no identity or ACL model.
