# JSON-schema dependency contract

The optional module pins santhosh-tekuri/jsonschema/v6 v6.0.2 and
invopop/jsonschema v0.14.0 in go.mod. The engine validates raw schemas; invopop
provides the separate struct-schema recipe. Neither dependency moves into core.

Keep numbers as json.Number and exact rational operands. Consumers bind canonical
JSON/tool arguments with integers above 2^53 and decimal constraints; adjacent
values must remain distinct. Unsupported assertion arithmetic/count ranges fail
compilation; unsupported input numbers become correction, not a pass. Unknown
annotations remain annotations under their dialect until a reference makes them
active schemas.

numbers.go compiles a private safe-number probe to locate active assertions and
walks exported Schema/DynamicRef edges. The original documents alone produce the
runtime schema. This engine graph coupling is deliberate and must be reviewed
when the pinned engine changes; do not replace runtime operands with probe values.

For an upgrade, inspect exported graph edges and draft/vocabulary activation, then
run from this module:

```sh
go test -race -count=3 ./...
```

Required probes in contract_test.go:

- TestExactNumericConstraints, TestAdjacentExactValuesAreDistinct,
  TestLargeExactEnumsDoNotCollapseDuringCompilation: large integer/decimal bounds,
  multipleOf and adjacent enum values.
- TestUnrepresentableSchemaNumbersFailClosed, TestSchemaCountsCannotOverflow:
  assertion arithmetic and count limits, including nested properties.
- TestReferencedAnnotationsUseActualAssertions: references into object/array
  annotations and inactive draft-04 keywords.
- TestResourceURIsCannotEvadeNumericChecks, TestSchemaResourcesAndCompilationFailures:
  normalized resource IDs, unsupported vocabulary, disabled remote/file fetches.
- TestDialectContracts, TestSchemaRejectsAmbiguousDocuments: supported dialects,
  nested refs, prefixItems/unevaluatedProperties, duplicate keys and trailing JSON.

Run build and integration module tests and the repository's make lint/test gates
before accepting the version change. Add a probe for any newly exposed graph edge
or changed vocabulary; existing green probes do not certify untested new features.
The dependency pin and graph checks are retained, not upgraded in T09.
