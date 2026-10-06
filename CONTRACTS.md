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
invariant. ScopeFactory runs at every input/argument boundary and host-controlled resume.
Argument adapters check cancellation before and immediately after caller decode,
post-bind, encode and schema callbacks, before interpreting their result or invoking
the next stage. Cancellation/deadline errors, including wrapped callback errors with
a live parent context, are system faults with their causes retained through errors.Is.
Ordinary post-bind domain errors remain corrections. SemanticValidator and LLMJudge
also check context before/after their synchronous callback when called directly;
Pipeline projects their errors into canonical faults. Cancellation is cooperative:
these checks cannot terminate a callback or undo its side effects.
Fault projections retain aggregate payload classification from checks completed
before the fault. Pipeline/boundary decisions and PolicyFailure describe the same
canonical result, including after canonical final checking fails.
All long-lived wrappers use ScopeFactory: input/argument wrappers resolve it before
validation; output wrappers resolve it after a successful handler, immediately
before result validation. HTTP resolves it after extraction on every request.
Handler errors skip output validation; WrapOutput preserves the handler's partial
result for host orchestration, whereas WrapGuardedOutput suppresses it. A factory
error/cancellation is a fault. Low-level Run accepts an explicit caller snapshot.
A fresh lookup is not atomic execution authorization: the host must bind and
recheck approval and execute under its own consistency contract.
Required scope contracts are checked before any raw validator, including the
requirements of the final checker. Missing keys and incompatible typed facts
have distinct typed causes; boundary decisions remain system faults.
Typed prerequisites use the same dynamic type assertion contract as ScopeKey.Lookup:
concrete types must match exactly (named map/slice/struct/function values do not
implicitly convert to unnamed types); interface keys accept concrete implementations.
A typed nil retains its dynamic type and may satisfy a key; a nil interface cannot.
These checks happen before any validator callback. Low-level ExecutionScope.Lookup
remains caller-owned; custom rules declare requirements with NewPolicyFuncWithScope.
Typed attribute equality uses Go == semantics. Dynamically incomparable operands,
including interface fields holding slices/maps, fail with ErrAttributeIncomparable
and AttributeComparisonError metadata rather than panicking or becoming mismatch.
Caller-defined comparison belongs in a custom policy; equality does not imply DeepEqual.

## Delivery representation

GuardedDelivery[T] is the single canonical output boundary, including Projection.
GuardOutput and WrapGuardedOutput return that same type using the UserText recipe.
Generic GuardDelivery requires an explicit channel, allowed kinds and caller classifier;
zero/default policy is invalid. Classifiers describe the actual destination wire
representation and must terminate, avoid mutation, and be safe for concurrent sharing.
Core never invokes caller MarshalJSON/MarshalText to guess serialized safety.
Validator observations can restrict the classified kind; a classifier cannot downgrade
a previously observed more restrictive kind. Classification error, panic, invalid kind
or cancellation is SystemFault and suppresses both original content and fallback.

NewUserTextPolicy is an explicit opt-in shape recipe: text/bytes and named text
without custom marshalers are inspected for JSON object/array syntax; composite Go
shapes are technical. json.RawMessage is explicitly supported. This does not prove
serialization safety or detector quality. Unsupported scalars, custom marshalers,
nil interfaces, and pointer/interface dereference exceeding 64 steps produce
DeliveryClassificationError (ErrDeliveryClassification). Typed nil pointers are
classified by the bounded underlying type; nil byte slices remain text. Cyclic
value and type dereference always terminates, even for technical allowed kinds.
The host must send the checked representation unchanged; different canonicalization
requires a classifier bound to that host contract.

Fallback compatibility is checked before Run or any classifier/validator callback.
nil without dynamic type means absent. Typed nil is present if its dynamic type
asserts to T; otherwise configuration fails. A compatible fallback still traverses
validation and classification independently, with recursive fallback disabled.
No classification or mandatory validator fault activates fallback delivery.

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
Framing state belongs to each processor. JSON/newline framing visits each input
byte a bounded number of times across writes and releases; consumed units do not
trigger repeated scanning or shifting of a live tail. Buffer growth/consumption is
amortized linear in accepted bytes plus units. This claim excludes caller callbacks,
JSON syntax checks and per-unit policy/transport work. Retained pending allocation
capacity is bounded by MaxPendingBytes; the fixed scanner/cursor state owns no
unbounded queue or delimiter stack. PeakPendingBytes records peak reserved pending
capacity, including unused space; it does not measure total process allocation or
transient validator/output values.
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

Authenticated identity, source references, integrity/trust, confidentiality,
destinations and policy identities belong to host-owned scope types. The host
assigns these facts from authenticated transport, source adapters and its policy
configuration; payload text, tool responses and subagent claims cannot assign them.
Integrity and confidentiality are independent: a trusted source can contain secrets.
Unknown origin is not public or trusted by default. Claims and confirmed facts are separate. Transformation does not
elevate trust or remove references. Missing required facts fail closed. No-provenance
mode and declassification are explicit caller policies. Evidence is bounded references,
not raw secrets. Each consumer has its own policy; only DeliveryProjection is
serialized. Fallback/replacement values go through the destination's content checks.

BoundaryProfile declares actual configured coverage, rejects unsupported mandatory
boundaries, and does not automatically intercept hosted/remote execution. External
approval binding and invocation remain the host's responsibility. A reference host
gate binds exact canonical arguments, authenticated identity, destination, current
policy and configuration; ConfigurationID alone is metadata, not authorization.
Pause/resume rebuilds facts and checks the final arguments and gate again. Each
context, persistence, export and delivery consumer checks its own destination;
transforms, summaries, subagent responses and restored secrets retain host source
restrictions until an explicit host declassification policy changes them. Zero retry budget
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


## Observation and semantic evaluation

GuardEvent observers observe non-fatal shadow blocks; they do not receive every
allowed/denied action and do not supply an audit ledger. OTel middleware supplies
metrics/spans for validator calls, not authorization or provenance evidence.
The host exports only bounded opaque references and approved metadata/projections,
never entire Scope, Report, raw input or correction feedback by default. Third-party
validator error text can itself contain private data even when payload capture is
disabled; exporters must not attach arbitrary error messages. Explicit payload
capture remains a deliberate caller disclosure decision.

Deterministic adversarial conformance tests check exact canonical bytes, real
handler/sink calls, category routing and configuration coverage. Mock scores verify
wiring, not attack detection quality. External semantic evaluations fix detector,
model/configuration and dataset IDs; separate benign/adversarial sets and report
false positives/negatives, task success, latency and detector faults. Preserve
split/version and threshold, compare the same workload, and record unknown or
unlabelled cases separately. Live providers and paid benchmarks are outside CI.

## Compilation and declared checks

`CompileArgs`, `CompileJSONArgs` and `build.CompileStringGuard` return no pipeline
when built-in configuration is invalid. `ConfigurationError` exposes stable
Component/Field/Code and matches `ErrConfiguration`; its default text never includes
caller values or schema diagnostics. Caller option panics are not recovered.
Wordlist construction returns configuration errors; only its explicit Must helper
panics. Zero declarative LengthMax disables that rule; negatives are invalid.
An absent schema option permits schema-free flows; explicitly empty schema bytes
are invalid, while `{}` is a valid permissive schema. Fallback requires user channel.
Declarative PolicyAttributeDeepEqual uses reflect.DeepEqual without type coercion;
typed core equality uses Go == and rejects incomparable operands at execution.

Providing WithArgsFinalGuard or WithJSONArgsFinalGuard requires a non-nil read-only
final pipeline. Omitting the option permits ordinary flows without final checks.
JSONArgsValidator provides executable object checking; JSONArgsMetadata and
ShapeProvider provide metadata only. A nil built-in validator function is rejected
at compile time; custom checker internals and rule adequacy remain caller-owned.
SchemaID and ConfigurationID do not establish enforcement.

Raw schema validation constrains the original JSON property names and unknown
fields before standard encoding/json binding can discard or case-fold them. Final
validation checks canonical bytes after post-bind mutation. For exact names, the
caller schema explicitly uses required/properties/additionalProperties. Core does
not impose JSON Schema or strict decoding on typed DTOs or custom codecs. Both
validation stages use the same generic string pipeline for documents and arguments.

## Validator telemetry and framing diagnostics

OTel exports the canonical per-call disposition/outcome and execution fault,
including invalid or explicit fault reports with nil Go error. Shadow observations
export both observed and enforced dispositions; fatal/invalid results remain enforced.
Policy denial/correction is not an infrastructure span error. Middleware observes
individual rule calls, not final delivery, orchestration failures after return or
an authorization audit ledger. Labels contain bounded enums and allowlisted static
metadata; score, raw errors, scope, reason and feedback are excluded. Payload span
attributes remain explicit opt-in. All span status descriptions are static.
The OTel wrapper supports partial/unit/final calls itself; core still checks the
base rule and every wrapper separately and never upgrades delegate capabilities.

Malformed/unsupported JSON units use StreamMalformed/ErrInvalidStreamUnit,
incomplete units use StreamIncomplete/ErrIncompleteStreamUnit and budget exhaustion
uses StreamLimit/ErrStreamUnitLimit. Causes are typed categories, not parsed text.
Actual released bytes and terminal state remain sticky across transport partitions.
Finite matcher scores survive both semantic pass and block reports on the caller
scale; they are not probabilities and are not exported automatically. Caller
reports can support external calibration under the existing evaluation protocol.

## Release artifacts and independent modules

Release preparation snapshots caller-selected source files into an isolated candidate;
it never changes the source branch, index, refs or files. Candidate modules use a
single explicit version and exact internal module paths; similar external prefixes
are not internal dependencies. Local replacements and workspace overrides are absent.
Go tooling parses/edits manifests; external requirement versions are preserved.
Unsupported v2+ path transitions are rejected before candidate generation.

Prepare writes deterministic module archives, manifests and planned tags. Verify
loads the unpublished modules from the candidate's local module proxy with GOWORK=off
and readonly manifests, tests every module with race, checks lint and an independent
smoke consumer. It does not publish artifacts or tags. Integration tests have their
own module; optional engines are not dependencies of core unit tests.

Publish is a separate explicit command after successful verification of the same
candidate bytes. It creates release refs only in the isolated candidate and pushes
an explicit atomic set of tag refspecs to a supplied remote. Source refs and unrelated
tags stay unchanged. Failure/interrupt cleanup removes transient candidate refs; no
push --tags or hidden origin default is used. Publication is outside this task.

The standard make release-patch/release-break workflow runs lint and tests, then
scripts/release.sh with patch/break and the discovered modules. The script selects
the next version from origin's remote tags and asks for confirmation, then executes
prepare/verify/publish internally against origin's explicit push URL. The source
checkout must be clean and stays unchanged throughout the release train. Separate
phase commands remain available for candidate-only CI verification.

### Built-in detector construction and redaction

All seven ext constructors return `(Validator[string], error)`; Must variants
panic on the same configuration errors. Nil options, nil/typed-nil classifiers,
negative length bounds, reversed positive bounds and unsupported actions/options
are configuration errors before input processing. Length bound zero disables that
side. Semantic construction rejects nil matchers and nonfinite thresholds; any
finite score scale belongs to the caller.

Clean passes have no violation-only Fatal, Retryable or SafeUserMessage fields.
Detected hits retain these options. TechnicalJSONClassifier marks a detected hit
as technical with ActionPass; Fatal on that hit still denies delivery. Retryable
metadata alone on ActionPass does not request correction. Caller-authored fatal
pass reports retain terminal-deny semantics.

Regex redaction reports a detection on a match even when replacement leaves the
bytes unchanged, including empty and zero-width matches. Replacement suitability
belongs to the caller. TagPatternValidator blocks regex matches; it does not parse
or sanitize markup. ClassifierValidator adapts a host TextClassifier and includes
no trained model. Regex, ASCII PII patterns, token wordlists and the tool-like JSON
heuristic do not establish statistical prompt-injection protection or exhaustive
content detection.

A configured TokenVault must return a nonempty token different from the original.
Store error, panic, empty or identity token is a processing fault. The validator
returns original input with no report; guarded delivery suppresses the value.
Explicit replacement and a configured vault are mutually exclusive construction options.
No configured-vault failure silently switches to irreversible replacement. An
absent vault explicitly uses irreversible replacement. Completed Store side
effects before a later failure are not rolled back: lifetime, isolation, cleanup,
ACL and authorized restoration belong to the host. The in-memory reference is
thread-safe and supports its zero value. An allowlist with no input tokens stores
the entire rejected input when reversible redaction is configured.
