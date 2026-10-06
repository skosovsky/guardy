# T01 implementation evidence

Baseline code: `a581a1c` (runtime equals review `e7e1c8c`). T00 commit: `a581a1c`.

Contract: precheck matches type assertion (exact concrete type, implementing
interface type; typed nil preserved, nil interface rejected). Error metadata and
boundary suppression use existing canonical SystemFault projection.

Regression added before code change: `scope_assertion_contract_test.go`.
Baseline command (exit 1):
`GOCACHE=/tmp/guardy-task27-gocache GOMODCACHE=/tmp/guardy-task27-modcache go test -run '^TestScopePrecheckMatchesTypedAssertion$' .`
Baseline failures: named map/slice/struct/function and unnamed map for named key;
all invoke callback and produce pass despite failed typed Lookup. See t01-baseline.txt.
Same command after fix: exit 0, t01-regression.txt.

Implementation uses reflect.Type equality for concrete types, Implements for
interfaces. Deprecated untyped policy constructors and private helpers removed;
two test consumers migrated to NewPolicyFuncWithScope. Low-level Lookup retained.
README/CONTRACTS/Godoc and MIGRATION updated; migration consolidation remains T11.

Checks (all exit 0):
- Core/root packages: `go test -race ./...` with above cache variables, t01-core.txt.
- `go test ./build/... ./integration/...`, t01-consumers.txt. Initial combined
  command also included `./examples/...`, which matched no packages across nested
  modules; the separate inventory loop below tested all 13 example modules.
- `rg --files examples -g go.mod` and per-directory `go test ./...` with above
  caches, t01-examples.txt: all 13 example modules compile; existing tests pass.
- `go run ./examples/policy_attributes`, t01-policy-example.txt: mismatch deny and
  missing scope typed metadata demonstrated.
- `git diff --check` PASS; search of Go source has no removed APIs/helpers.

Initial test attempts using host cache paths were blocked by the filesystem
sandbox before tests executed. Writable /tmp caches enabled actual checks;
those initialization failures are not regression evidence.

Additional gate: root `golangci-lint run --allow-serial-runners ./...` with
`GOLANGCI_LINT_CACHE=/tmp/guardy-task27-lintcache` and above Go caches: exit 0,
0 issues (t01-lint.txt). Initial golines findings were fixed before final review.
