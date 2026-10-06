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
