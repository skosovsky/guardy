# Repository verification

Make invokes standard Go commands and golangci-lint directly. Modules are discovered
from go.mod files, excluding hidden directories and vendor. All commands use
GOWORK=off. There is no aggregate check target or tool version validation target.

| Command | Scope |
|---|---|
| `make modules` | List all discovered development modules. |
| `make test` | Fresh ordinary race tests across all modules. |
| `make test-integration` | Files with integration build tag; execute TestIntegration… functions only. |
| `make test-e2e` | Files with e2e build tag; execute TestE2E… functions only. |
| `make test-live` | Files with live build tag; execute TestLive… functions only, including paid calls. |
| `make lint` | Formatting diff and lint without rewriting files. |
| `make fix` | Go fix, formatting and lint fixes; modifies files. |
| `make fuzz` | Every discovered fuzz function separately, 30 seconds per function. |
| `make bench` / `make cover` | Benchmarks / per-module coverage. |

Build tags alone do not exclude ordinary test files. Profile targets combine the tag
with a matching test-name prefix so a module without such tests executes none.
Use the same convention for new tests; each profile runs directly through Go too:

```sh
GOWORK=off go test -race -tags=integration -run '^TestIntegration' ./...
GOWORK=off go test -race -tags=e2e -run '^TestE2E' ./...
```

Recipes use tools from PATH and explicitly propagate command failures.
Tool versions are pinned in CI, not enforced by Make. CI and source release gates
run lint, fresh unit tests, integration and e2e sequentially.

## Guardy modules and prerequisites

Core and optional modules remain independent. Development replacements point to
this repository; external toolsy/prompty dependencies stay pinned. go.work is an
editor convenience and is not required by Make or CI.

`integration` contains cross-module contracts (integration tag) and the complete
document/API reference flow (e2e tag). `integration/downstream` tests real toolsy
argument binding and prompty stream boundaries under integration; destination,
post-handler and restore recipes use e2e. Executable examples run as ordinary tests.
No live provider calls currently exist. Benchmarks and fuzz campaigns are separate.

`tooling` contains Go tests, not a runner or release executable. Unit tests exercise
Make dispatch and Bash release recovery against disposable bare repositories.
Integration tests retain exact source peer revisions and published peer selections
from downstream/conformance.json, and verify all discovered candidate modules via
a temporary module proxy plus a real downstream semantic consumer. Published peer
checks reject replacements and version drift. Internal candidate versions are
computed from the fixture, not duplicated in that peer manifest.

Required tools are Bash, Git, Make, Go and a C toolchain for race tests; lint requires
golangci-lint. Integration tooling additionally needs network access for pinned
peer Git revisions and Go dependencies. Missing prerequisites fail, never skip.
Guardy needs no PDF runtime, Python, database or Docker daemon for its test suites.
Docker can be used to reproduce Linux checks from macOS.

CI pins Go 1.27.2 and golangci-lint 2.14.0, downloads dependencies and caches sums
for every discovered module, then runs lint/unit/integration/e2e sequentially on
Linux and macOS. Make uses tools from PATH without installing or validating versions.
All modules, including examples and tooling, participate in release discovery.

The lint baseline comes from ragy commit 91e3ff2cdc87c49b5ad9d1cf0afa23d35832521e.
Ragy-specific adapters and fixture exceptions are removed. Lint enables both
integration and e2e tags so static checks include every deterministic profile. Guardy's Report,
ControlSpec, PolicyConfig and ext.RuleConfig intentionally support optional fields
with zero meaning unset; their exact types are excluded from exhaustive struct
initialization. Constructors and FinishReport normalize those fields. Other
implementation structs remain checked; shared rules and thresholds are unchanged.
