# Migration: task27 remediation (unreleased)

This guide records the current remediation API changes. It will be consolidated
with the previous migration notes before the candidate is verified.

## Typed scope

- `NewPolicyFunc` is removed. Use `NewPolicyFuncWithScope` and requirements from
  `ScopeKey[T].Requirement()`. Rules without prerequisites pass nil requirements.
- `NewAttributePresent` is removed. Use `NewTypedAttributePresent` with a host-owned
  typed key. Low-level `ExecutionScope.Lookup` remains available for BYOT scopes.
- Prerequisite checks now match `ScopeKey.Lookup` type assertion semantics:
  concrete keys require the exact dynamic type, while interface keys accept
  implementing concrete types. Previously assignable named/unnamed map, slice,
  struct and function pairs can now fail before any callback with
  `ErrScopeIncompatible` and a canonical system-fault decision. Convert explicitly
  at the host boundary when conversion is intended.
- Typed nil pointers/maps/functions keep their dynamic type; an untyped nil or
  nil interface cannot satisfy a typed prerequisite, including `ScopeKey[any]`.

## Delivery

- GuardedOutput[T] is removed; GuardOutput and WrapGuardedOutput return the single
  GuardedDelivery[T], which provides DeliverableValue and Projection.
- NewDeliveryPolicy no longer supplies implicit safe-text eligibility or shape
  classification. Supply channel, WithDeliveryAllowedKinds and a caller classifier
  with WithDeliveryClassifier; zero policies fail before callbacks. Classifiers
  must represent actual destination serialization/canonicalization and never infer
  unknown content safe. Existing text/shape recipes use NewUserTextPolicy explicitly.
- GuardOutput uses NewUserTextPolicy("user"). UserText dereference is limited to
  64 steps; unsupported/nil-interface/custom-marshaler representations fault.
  Core does not invoke arbitrary MarshalJSON/MarshalText. Generic BYOT consumers
  can bind their own classifier instead of inheriting shape rules.
- Fallback type mismatch is a configuration error before Run. Untyped nil means
  no fallback, typed nil is present and must assert to T; it still receives the
  same content/classification checks. Classifier faults never activate fallback.
- Stream compilation validates its delivery policy and string fallback type.

## Built-ins and semantic construction

- NewLengthValidator, NewPIIValidator, NewTechnicalJSONClassifier and
  NewSemanticValidator now return `(validator, error)`. Handle configuration errors
  during setup; Must variants are available for known-valid static configuration.
  SemanticFixture.Validator also returns `(Validator[string], error)`.
- TagSanitizerValidator/NewTagSanitizerValidator/MustTagSanitizerValidator are
  replaced by TagPatternValidator/NewTagPatternValidator/MustTagPatternValidator.
  MLValidator/NewMLValidator are replaced by ClassifierValidator/NewClassifierValidator
  with MustClassifierValidator for static setup. Old names are removed.
- Built-ins reject nil options, unsupported option combinations and invalid actions
  instead of coercing or ignoring them. Length bounds are nonnegative, zero disables
  a side, and reversed enabled bounds fail. Nil detectors and nonfinite semantic
  thresholds fail at construction; finite caller scales remain valid.
- TokenVault.Store returns `(string, error)`; migrate host vaults and handle errors
  on direct Store calls. Configured-vault failures, panic and empty/identity tokens
  fault instead of falling back to irreversible text. Only an absent vault selects
  irreversible replacement. Host lifecycle/ACL responsibilities remain unchanged.
- Clean passes clear violation-only flags. Detected technical JSON still receives
  configured hit flags. Regex identity and zero-width matches remain detections.

## Completed observations

Compound validators returning an error must attach only earlier completed checks
with WithCompletedObservations(error, reports...). Pipeline ignores the report
returned beside an error, including late cancellation. Inspect attested history
with CompletedReportFromError or errors.As into *CompletedObservationsError.
Snapshots clear MutatedText and retain causes through errors.Is/errors.As. MapSlice
and JSONRedact now preserve prior classification on faults; Map/MapJSONRawMessage
error reports project only attested inner history. Fault delivery remains suppressed.


## Recursive JSON decisions

JSONRedact uses sorted object keys and array index order. It continues after
correction/deny to find stronger outcomes and stops on fault/error/cancellation.
Equal-priority diagnostics select the last visited report; source key order no
longer changes decisions. Any mandatory outcome returns the original document,
with MutatedText cleared; success remains re-encoded JSON. Leaf checks must be
independent and cooperative: later checks now run after earlier corrections/denies.

### Pipeline phases and panic handling

Replace `WithFastPath` with `WithSequential`, `WithSlowPath` with `WithParallel`,
`ValidationPhaseFast` with `ValidationPhaseSequential`, and `ValidationPhaseSlow`
with `ValidationPhaseParallel`. Execution order remains sequential → policy →
parallel. Serialized phase labels change from `fast`/`slow` to
`sequential`/`parallel`; update dashboard filters, telemetry queries and saved
fixtures. OTel parallel spans are now named `guardy.validator.parallel` instead of
`guardy.validator.slow`. Policy keeps its `policy` label. No compatibility aliases remain.

Validate panics now become system faults in every phase, including middleware
Validate wrappers. Public error text does not include the panic value. Inspect
`ValidatorPanicError` through errors.As and call PanicValue only in trusted
operator diagnostics. Error-valued panics retain their cause through errors.Is/As.
Construction and observer/host callbacks have separate contracts in CONTRACTS.md.

### Fallible core construction and host routing

`NewPipeline` and `Pipeline.Use` now return a pipeline and configuration error.
Use `MustNewPipeline`/`MustUse` only for static trusted configuration. Nil/typed-nil
rules, nil options/middleware/wrapper layers and fallback without user-channel
configuration are rejected before processing. `WithObserver(nil)` still disables
observation. Middleware factories for policy adapters are checked by Use and
reconstructed per invocation; deterministic construction and valid wrappers for
all scopes remain caller obligations. Construction callback panics still escape.

`NewPolicyFuncWithScope`, `NewTypedAttributeEquals`, `NewTypedAttributePresent`
and `NewLLMJudge` are fallible. Their static counterparts are
`MustPolicyFuncWithScope`, `MustTypedAttributeEquals`, `MustTypedAttributePresent`
and `MustLLMJudge`. Reject nil callbacks/options/Judge, zero scope keys and malformed
requirements during construction. Name-only scope declarations are presence checks;
typed declarations must use ScopeKey.Requirement rather than fabricated type names.

`RouteDecision` and `Decision.Route` return `(GuardRoute, error)` and reject negative
retry counters for every decision. A zero retry budget remains exhausted. The host
owns counter updates and scheduling. `GuardRouteFallbackDelivery` proposes a value;
check the proposed value for its destination with GuardDelivery before sending it.
System faults never use this fallback route.

### OTel setup errors

`guardyotel.NewMiddleware` now returns `(ValidatorMiddleware[T], error)`. Handle
counter/histogram creation failures before installing it; use `MustMiddleware` for
static configuration. Provider causes remain in the ConfigurationError chain, not
its public text. Nil options, typed-nil providers and absent/typed-nil instruments are rejected;
plain `WithMeter(nil)` and `WithTracer(nil)` intentionally disable their channels.
Setup no longer silently degrades telemetry. Runtime delegated decisions/errors
and metadata/payload privacy stay unchanged. Provider lifecycle remains host-owned.

### HTTP middleware and output adapters

`Guard` now returns `(func(http.Handler) http.Handler, error)`; handle setup errors,
or use `MustGuard` for static configuration. Nil dependencies/options and
nonpositive `WithGuardMaxBodyBytes` are rejected. The consumed-input cap defaults
to 1 MiB; oversized input now returns 413 instead of 400. Pass restores original
bytes; redaction sends the authoritative returned `T` to the injector.

Guard closes the original consumed wrapper before replacement. Extractors and
injectors borrow replay bodies; their current replacement transfers ownership to
Guard. Next borrows its handed-off body, which Guard closes on return. Callbacks
own any intermediate replacements they remove; handlers own their own replacements.
Independent GetBody readers are caller-owned. Close errors before next suppress
execution with 500; post-handler close errors cannot rewrite a committed response.
See CONTRACTS.md for the full ownership/status matrix and cooperative cancellation.

Prefer `WrapGuardedOutput` and deliver only its approved `Projection`. It suppresses
partial handler results, validation faults and cancellation. `WrapOutput` remains
a low-level API that returns the unvalidated partial result alongside a handler
error. Neither wrapper retries execution or undoes completed host side effects.
