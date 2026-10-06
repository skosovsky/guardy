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
