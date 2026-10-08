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
are serialized. Scope factory, validation, writer and observer run under its lock.
They must return and must not reenter the processor. Abort/Outcome and subsequent
calls wait for a running callback; context cancellation cannot interrupt mutex
acquisition or forcibly stop a non-cooperative writer/observer/validator.

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

BoundaryProfile declares caller-reported configured coverage, rejects unsupported mandatory
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
For admissible inputs, framing and delivered units are independent of transport
partitions. Each Write is admitted atomically against the remaining MaxInputBytes;
an oversized Write is rejected before accepting any of its bytes. Overbudget input
can release different prefixes depending on partitioning: prior delivery is
irreversible. Released-byte accounting and terminal state remain sticky.
Newline delimiters count toward MaxUnitBytes. An unterminated tail exactly at the
limit waits for Complete, which validates it once; the next byte/newline overflows
before buffering it. Newline pending capacity never exceeds min(MaxPendingBytes,
MaxUnitBytes). JSON keeps its separate bounded lookahead capacity.
Finite matcher scores survive both semantic pass and block reports on the caller
scale; they are not probabilities and are not exported automatically. Caller
reports can support external calibration under the existing evaluation protocol.

## Release artifacts and independent modules

Make discovers all go.mod files outside hidden/vendor directories and runs with
GOWORK=off. Development source retains relative replacements for its own modules.
The release script checks that nested module paths equal the root path plus their
directory; every discovered module participates in the release.

Release selects committed source from local main and runs lint, unit, integration
and e2e gates in an isolated checkout. Only candidate go.mod/go.sum may change:
internal requirements use the release version, internal replacements are removed.
The caller's checkout, index, local tags and development manifests are unchanged.

After confirmation, a single atomic push sends source to remote main and candidate
to exact module tags. Remote main must fast-forward; there is no force, broad tag
push or non-atomic fallback. Failed gates publish nothing. Candidate state survives
cancellation or failed publication. Inspect/resume/finish retain exact identity and
ref checks; conflicting tags are never overwritten. Completion verifies remote
refs, not public proxy availability. Artifact/consumer conformance is part of the
prepublication Go test gate. See [runbook](docs/release/runbook.md).

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


### Stream wire units and transformed budgets

ReleaseValidatedUnits with JSONValues frames exactly one object or array per unit,
including surrounding framing whitespace. Scalars are unsupported as unit input,
transformed output and fallback candidate/output; all are checked before the writer.
Whole-response JSONValues accepts any single valid JSON value, including scalars.
Newline transformed/fallback output must remain one self-contained newline unit or
unterminated final tail. Invalid transformed framing is a processing fault and
cannot activate fallback. Invalid fallback candidate is rejected before its rules.

MaxUnitBytes bounds source units and each approved transformed/fallback unit for
validated-unit and best-effort partial release. UTF-8 bytes and JSON whitespace
count toward the bound. Expansion beyond it faults with StreamLimit and
ErrStreamUnitLimit before writing that unit, even when total output budget permits
it. Whole-response retains its bounded source/final-buffer contract and uses
MaxOutputBytes for approved expanded output, with no artificial unit boundary.
MaxOutputBytes also bounds cumulative actual release plus any separate fallback.
Previously released prefixes remain irreversible; no failing unit is partially
written for framing or budget errors. A mandatory processing fault makes separate
fallback unavailable; fallback cannot turn the original terminal outcome to success.

### Completed observations on processing errors

A report/output returned beside a callback error is untrusted and is never treated
as a completed check. This also applies when context cancellation is detected
immediately after return. Compound adapters explicitly attach prior completed
checks with WithCompletedObservations; CompletedObservationsError privately stores
a snapshot and preserves the original cause through Unwrap. Snapshot projections
clear MutatedText and return copies. CompletedReportFromError traverses wrappers
and every joined branch, joining only these attested observations. A nested carrier
already contains its nested evidence and is not counted twice. No cause/evidence
fabricates a successful callback or a deliverable value.

Pipeline consumes this separate evidence in fast, policy and slow phases, retains
payload classification in RunResult, boundary Decision and PolicyFailure, and
attaches its own completed history to nested pipeline faults. MapSlice/jsonredact
return original input on error/cancellation and expose only completed snapshots,
including nested carrier evidence. Map/MapJSONRawMessage forward attested inner
history and discard raw failed reports. Failed callback output is never injected.

Slow sibling-only cancellation after a policy stop is still not an independent
fault, but completed evidence from that sibling is retained. Genuine faults and
parent cancellation enforce. Low-level Run may retain prior diagnostic output;
guarded boundaries suppress values on faults. The carrier is a trusted-code
attestation, not proof that arbitrary host detector claims are true. It exposes no
transformed value, grants no declassification and never activates fallback. This
error/cancellation contract does not add callback termination. Pipeline Validate
panic recovery is specified under Pipeline callback faults; direct adapters retain
their own callback contracts.

### Recursive JSON aggregation

JSONRedact visits object keys in lexicographic Go string order and arrays in index
order, recursively; JSON source key order does not alter traversal. Leaf checks
are independent and receive original string values. Traversal continues after
correction and terminal deny to discover stronger outcomes. Completed reports
compose with the shared enforcement priority and aggregate payload classification.
The last visited report wins equal-priority diagnostics, matching ComposeReports;
the first report-only system fault stops further leaf checks, as do Go error and
cancellation. Unvisited siblings are never inferred to have passed.

Any enforced correction/deny/fault returns the exact original document, including
original formatting; partial redaction remains private to the traversal. Successful
output is re-encoded JSON. Shadow policy block remains an observation and cannot
hide a subsequent fault, including invalid reports. Go error/cancellation preserves
only prior completed classification through the T06 evidence carrier; failed leaf
report/output is discarded. Host leaf callbacks must be independent, cooperative,
and free of input alias mutation or irreversible validation side effects.

## Pipeline callback faults (T08)

Every Validate invocation in sequential, policy and parallel phases, including
middleware's Validate wrapper, has the same panic boundary. A panic is a validator
system fault, never pass, retry or shadow observation. The failed callback's report
and returned value are discarded; completed earlier observations and original
error/cancellation causes are retained. Public error text and fault diagnostics do
not format the panic value. `ValidatorPanicError.PanicValue` explicitly exposes the
original value for trusted operator inspection; an error-valued panic also remains
in the cause chain. High-level delivery suppresses payload on this fault.

Recovery surrounds Validate only. Pipeline option evaluation, RequiredScope and
middleware construction callbacks are trusted construction code: their panics
remain construction panics. Framework scope prechecks, runtime observer and host
sink callbacks must not panic; their invocations sit outside Validate recovery.
A scope lookup (or another nested callback) invoked inside Validate is covered by
that enclosing Validate boundary, not by an independent scope/host recovery layer. Recovery is not
rollback for mutations of caller-owned aliases or external callback side effects.

## Pipeline phase identities (T08)

`WithSequential` validators execute in registration order and may return a new
value. Policy validators then execute sequentially with the supplied scope.
`WithParallel` validators read the final sequential/policy value concurrently;
returned values are ignored and ActionRedact is a system fault. Middleware and
observer context/event phases use the stable serialized identities `sequential`,
`policy`, `parallel`. There are three phases; names describe execution semantics,
not estimated latency. Fast/Slow APIs and labels are removed in this clear break.

## Pipeline construction and routing configuration (T08)

NewPipeline returns (*Pipeline[T], error); MustNewPipeline panics on that same
error for static configuration. Use returns a new pipeline and an error;
MustUse is the explicit static-configuration wrapper. Nil options, nil/typed-nil
rules in every phase, nil middleware and nil middleware wrapper layers are
configuration errors. WithObserver(nil) explicitly disables the optional observer.
WithUserChannelFallback requires WithUserChannel, including an empty fallback.
Empty pipelines and empty phase lists are valid. RequiredScope is evaluated only
after rules are checked. Options, RequiredScope and middleware factories are
trusted deterministic construction callbacks; their panics are not intercepted.
Middleware factories for scoped policies are checked during Use and reconstructed
per invocation; they must return valid wrappers for every supplied scope.

Core policy/Judge constructors are fallible with explicit Must wrappers. Nil policy
functions/options, zero scope keys and nil/typed-nil Judge implementations are
configuration errors. Scope requirements need a nonempty key; typed declarations
come from ScopeKey.Requirement. Name-only declarations retain their presence-only
contract. Caller-owned equality operands remain borrowed; construction does not
introduce a universal copier.

Report remains the validator diagnostic/observation DTO: Action describes the
requested intervention, while the normalized disposition drives enforcement.
Fatal is escalation; Retryable describes retry metadata; ShadowMode suppresses
only a valid, nonfatal block. Explicit terminal/fault dispositions may strengthen
an action. Explicit retryable disposition requires ActionRetry. Invalid actions,
dispositions, payload kinds and nonfinite scores fault even in shadow mode. This
keeps existing consumers and avoids a second outcome DTO; it does not authorize
routing by diagnostic strings or MutatedText.

Low-level Run intentionally has two system-fault channels: a Validate Go error
(including panic/cancellation) returns a safe typed ValidatorFaultError; a completed
report-only system fault returns a fault decision with nil Go error. Always inspect
PolicyDecision after checking error. High-level boundaries project both into a
PolicyFailure and suppress payload delivery. Returned T is the authoritative value.

RouteDecision and Decision.Route return (GuardRoute, error). Negative RetryAttempt
or MaxRetries is a configuration error for every decision. Zero budget permits no
retries. The helper is a stateless projection: it never increments counters, calls
callbacks, schedules retries or delivers messages. FallbackDelivery is a host
proposal, not approval of SafeMessage/FallbackMessage; the chosen fallback must
pass a separate GuardDelivery check for its destination. System faults never
propose fallback. Repeated calls with identical inputs return identical routes.

## Borrowed ownership and reload (T09)

Build PolicyRuleSpec.Value/DeepEqual operands and StaticScope values are borrowed,
not recursively frozen or copied. NewScope owns its key map; each bound value
retains its ordinary Go aliases. Pipeline.Use copies configuration lists but shares
validator objects and their middleware/provider state. Sharing is safe only when
validators, middleware, scope lookups/values, observers and host callbacks are
concurrency-safe, and borrowed operands/inputs are not mutated during their lifetime.
Parallel validators are read-only with respect to all aliases, not only returned T.
Custom equality operands are immutable after compile; mutating a map can change
future decisions and concurrent mutation is a caller data race, not snapshot reload.

Reload by constructing and validating a fresh pipeline and fresh immutable values,
then atomically publish one bundle containing both pipeline and scope/config version.
Each request loads that bundle once. In-flight requests may keep the old bundle;
its operands remain immutable until all users release it. The executable build
reload example/regression demonstrates this contract without a reflective copier.
ScopeFactory supplies current transient facts at a boundary; it does not make an
aliased value a coherent snapshot, bind approval or authorize subsequent execution.
The host owns consistency, authorization, provider/vault lifecycle and side effects.

MapSlice copies the outer slice only. Getters/validators must not mutate input;
setters for pointer/map/slice elements must use copy-on-write for reachable aliases.
Returning original input after deny/error discards private transformations; it
cannot undo alias mutations or external effects. This same precondition applies
to Map and other BYOT adapters. No generic rollback or deep copy is promised.

BoundaryProfile remains a declaration of caller-reported adapter coverage. Compile
checks names and mandatory membership; Covers does not inspect wiring, intercept
calls or prove enforcement. Retain it for host setup validation; prove actual
handler calls and delivered bytes through integration fixtures such as
`guardytest.CheckBoundaryCases`. A compiled profile alone cannot authorize a sink.

The optional JSON-schema module retains its pinned engine and numeric graph probe:
consumers need distinct adjacent integers beyond float64 precision, decimal
multipleOf/bounds and fail-closed unsupported numeric/count ranges. Only the
original schema becomes runtime validation; numericProbe's safe compilation copy
locates active assertions/references rather than silently weakening them. Inactive
annotations must not be mistaken for assertions. Core has no schema-engine import.
Dependency upgrades require the probes documented in ext/jsonschema/UPGRADE.md;
a shorter graph walk or a pin update without these checks is not equivalent safety.

## OTel setup errors (T10)

Optional guardyotel.NewMiddleware returns (middleware, error). Counter/histogram
creation errors fail setup with nil middleware and a safe ConfigurationError;
errors.Is/As retain the provider cause for explicit operator diagnostics. Nil options,
absent/typed-nil instruments and typed-nil providers are configuration errors. MustMiddleware is the explicit static setup wrapper.
WithMeter(nil)/WithTracer(nil) intentionally disable their telemetry channels.
Provider construction panics remain caller/provider failures, not Validate faults.

A failed setup never silently installs partially working metrics or changes a
business validation outcome. The host chooses whether setup failure blocks startup
or explicitly disables a channel and retries construction. Runtime middleware
still preserves delegated reports/errors and existing payload/metadata privacy.
Instrument/provider lifecycle and any already created instrument stay provider-owned;
setup cannot roll back provider effects. Per-call telemetry is not delivery approval.

## HTTP request limits and body ownership (T10)

Guard returns (middleware, error); MustGuard is the explicit static wrapper. Nil
pipeline/extractor/injector/options and nonpositive WithGuardMaxBodyBytes values
are configuration errors. DefaultMaxBodyBytes is 1 MiB; the positive configurable
cap applies to consumed input bytes, not declared ContentLength or injected output.
Exactly the cap is allowed; one extra byte is 413. Read/extraction errors are 400,
missing scope remains 400, policy deny/retry is 422; validation, cancellation,
consumed-body Close errors and injection faults are 500 with safe public text.
Callbacks remain cooperative; no goroutine timeout or side-effect rollback.

| Body | Owner and close point |
| --- | --- |
| Incoming body/custom wrapper | Guard consumes under cap and calls Close once before replacement, including read/size/cancel errors; net/http retains its own server lifecycle obligations. |
| Buffered extraction body | Borrowed by extractor. Guard closes it and the callback's currently installed replacement after extraction on success/error/cancel. |
| Buffered injection body | Borrowed by injector; if replaced, Guard closes the previous body before handoff. On injection error/cancel Guard also closes current replacement. |
| Body handed to next | Borrowed by handler. Guard closes the captured handed-off body when handler returns. Close failure after a response cannot rewrite already delivered bytes/status. |
| Bodies removed/replaced inside a callback or handler | The replacing code owns any intermediate bodies Guard cannot observe. Do not close borrowed bodies; callback-installed current bodies transfer to Guard. Handler-created replacements remain handler-owned. |
| GetBody replay | Independent body owned/closed by its caller; no shared cursor. |

Pass restores original bytes with synchronized length/header/GetBody. Redaction
injects authoritative returned T; the format-aware injector owns representation
correctness. Framework Close calls are idempotent per owned wrapper. A callback's
replacement does not authorize bypassing the cap on original input or invoking next
on fault. This contract concerns wrappers/resources, not a universal socket leak.

## Consumer recipes (T11)

Quick Start must deliver only an approved GuardedDelivery.Projection to its sink.
A low-level Run recipe must check both a non-nil Go error and canonical
PolicyDecision().IsSystemFault(), then terminal deny/retry, before consuming
RunResult.Output. Report-only faults can have nil Go error. Errors, raw reports,
original values and correction feedback are not consumer payloads.

The returned T and RunResult.Output are authoritative transformations for string,
struct/lens and HTTP consumers; MutatedText is optional diagnostic text and must
never replace the actual typed output. Runnable examples and sink matrices exercise
pass, redact, deny, retry, report-only fault and Go error separately. Docs tests
certify the example recipes and known fixture behavior, not unknown host wiring,
custom serialization, detector quality or execution authorization.

README is the current consumer guide. MIGRATION.md records the unreleased candidate
and earlier API transitions separately, without implying a published major version.
Root-only consumers need no optional JSON/schema/OTel engine; nested modules require
their own go get/import. Precise matcher, ownership, stream and telemetry limits
remain part of the public contract even where the guide links to detailed evidence.

## Stream fault evidence after validation (T12)

After GuardDelivery completes, stream timeout and output-budget failures retain
its canonical Decision and completed payload classification. When context expires
alongside an independent validator error, ReleaseError/PolicyFailure retain both
causes through errors.Is/As; StreamTimeout remains the stream category and always
projects SystemFault, even if cancellation raced with a completed deny/correction. Checked
fallback follows the same cause rule and remains separate from the original
terminal outcome. No normal/fallback bytes are written on these faults. These
projections do not attest a failed callback's arbitrary report or output.
# Optional downstream composition

`integration/downstream` is an independently installed module. Core imports no
execution or producer runtime. It supplies a tool argument binder and a text stream
consumer, not an agent, permission store, retry scheduler or rollback facility.

`NewArgsBinder` and `NewJSONArgsBinder` require both a raw pipeline and a non-nil
read-only final pipeline. The custom tool binder replaces default parsing/schema
validation. These adapters run fresh host scope → raw guard → binding/hooks →
canonical encoding → final guard → the actual tool manifest schema. Missing or
unsupported schema faults before policy/approval/handler. Schema compilation uses
only embedded resources and never fetches references. The host must not install
mutating `ArgValidator` or policy callbacks after binding; a mutation requires
another validation boundary. Custom codecs must faithfully encode their value.
Approval and resume binding remain host responsibilities; invoke the binder again
with refreshed facts and exact arguments after a pause.

The tool's `ValidatedArgs.Raw` receives `SanitizedRaw`, never guardy's diagnostic
original `Raw`. No reports or original payload are copied into metadata.

| Guard outcome | Tool error code | Retryable |
| --- | --- | --- |
| Terminal deny | POLICY_DENIED | false |
| Correction | VALIDATION_FAILED | true (host owns budget) |
| Fault, report-only fault, unknown error | INTERNAL | false |
| Cancellation | INTERNAL, detected by errors.Is | false |
| Deadline | TIMEOUT, detected by errors.Is | false |

The original error is retained in `ToolError.Err`, including `PolicyFailure` and
sentinel causes. Public `Reason`, `SafeMessage` and `Error()` use static safe copy;
each message is at most 128 bytes. Arbitrarily long diagnostic input is replaced,
not truncated into public output. Final schema mismatch uses the checker's
correction disposition. Post-handler result checks retain the runtime's
noncorrectable result-contract failure; the adapter does not redispatch.

`ConsumeTextStream` accepts only text content. It creates a fresh bounded guardy
processor and consumes the real producer handle once. Only successful lifecycle
termination with `OutcomeCompleted` calls `Complete`; finish/EOF alone do not.
Incomplete, refusal, paused and tool-call outcomes have distinct host routes;
unknown/unsupported content, protocol failure, producer error and cancellation
abort without flushing. Observers/finalizers validate isolated snapshots and
cannot sanitize delivery. The sink sees only processor writes. Progressive
profiles are explicit and cannot revoke already delivered prefixes. Sink failures
preserve actual `ReleasedBytes` and causes, never replay. Reusing a consumed
producer fails before new delivery. Resume requires a new handle and new checks.

Host recipes deliver only destination projections, never serialized result/control
envelopes. Token restore requires recipient authorization before lookup and a fresh
destination guard afterward. Vault isolation/expiry and provenance attestations
are host-owned; untrusted claims, summaries and nested results grant no trust.
Live providers, multimodal/tool-call stream delivery, automatic fallback, arbitrary
schema resource fetching and atomic authorization/effect transactions are unsupported.
