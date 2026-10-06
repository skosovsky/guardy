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
