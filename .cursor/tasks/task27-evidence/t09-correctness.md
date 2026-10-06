# T09 independent correctness acceptance

Verdict: **PASS — no unresolved confirmed defect found** in the final T09 working diff against `6e79068` (D08, D09, D23, D24). Reviewer did not implement changes or consult the other acceptance verdict.

## Inspection

- `build/compile.go` captures the DeepEqual operand as `any`; mutable reachable maps remain aliased. The new serial probe explicitly demonstrates this behavior without claiming it is a permitted reload strategy. Godoc and CONTRACTS require immutable borrowed values. The reload example compiles fresh policy/facts and publishes their pair using `atomic.Pointer`; each reader loads once and retired values remain unchanged. A deliberate mismatched version produces denial, so the fixture checks coherence rather than merely proving both guards individually pass.
- `scope.go` copies key bindings into a private map but retains value aliases. The new scope probe replaces a caller binding and separately mutates the original map, demonstrating precisely those two ownership boundaries. No recursive snapshot or general mutation safety is promised.
- `pipeline.go` clone copies configuration slices while retaining validator, observer and middleware references. Revised concurrency and Use comments correctly condition safety on shared caller state and prohibit parallel alias mutation. No unconditional concurrent safety or state cloning claim remains in the changed passages.
- `ext/map_slice.go` allocates an outer slice and applies setters to original element values. Its documented copy-on-write precondition matches implementation; returning original input on deny does not undo mutated aliases. The pointer fixture clones the first element, then denies the second and verifies the original pointer/value survives. Authorization, coherent host facts, providers/vault lifecycle and irreversible side effects are explicitly host-owned.
- `BoundaryProfile` checks declared names/membership only. The fixture intentionally delivers through an unwired sink, then actually guards the second delivery and verifies no denied bytes reach it. Revised README/Godoc distinguish those cases and do not claim automatic interception. `guardytest.CheckBoundaryCases` separately checks actual dispatch/projection behavior; a declaration is not evidence of enforcement.
- `ext/jsonschema/UPGRADE.md` names the exact versions pinned in that module's go.mod. The optional module's existing tests cover large exact bounds/enums, decimal multipleOf, unsupported numbers/counts, active referenced annotations, normalized resources, dialects and ambiguous documents. `numbers.go` traverses exported Schema/DynamicRef edges from a private probe; runtime compilation uses originals. No engine dependency is added to core, no pin or numeric behavior changes in T09, and the document explicitly requires new probes for future graph/vocabulary changes.

## Verification

Independent reviewer command completed with exit 0: `go test -race -count=3 ./...` in root (including ext, guardytest and internal/jsondoc), build and ext/jsonschema, using dedicated task Go caches. Root: 14.344s; build: 1.243s; schema: 1.304s; ext: 1.256s.

Inspected author logs: `/tmp/guardy-task27/t09-modules.log` records root/build/jsonschema/integration race success and `ALL 4 RELEVANT MODULES PASS`; `/tmp/guardy-task27/t09-all-lint.log` records all 19 module lint results with zero issues. Parent confirmed both final process handles terminated with exit 0 (14835 and 92042), as well as the isolated HEAD preservation baseline (65083). These are relevant four-module race checks, not a claimed all-19 race run for T09.

## Limits

No absolute absence-of-errors claim. No proof of arbitrary custom callback safety, external authorization, aliased mutation rollback or universal concurrent scheduling. The atomic reload regression exercises immutable whole-version publication but does not exhaust every scheduler interleaving. Dependency upgrade tests protect the current pinned graph and documented cases; untested future engine edges still require review and added probes. T10 remains responsible for broader adapter/setup work; T12 remains responsible for the full final repository gates.
