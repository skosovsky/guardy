# T05 implementation

R05,D17. A single profile-aware representation predicate checks source, transformed and separately checked fallback. JSON unit mode requires exactly one valid object/array with surrounding JSON whitespace; whole-response accepts any single valid JSON value. Newline transformed/fallback output stays a single self-contained unit. Existing StreamUnsupported diagnostic for multiline text fallback candidate is retained; JSON/UTF8 malformed candidate rejects before rules, invalid transformed framing faults.

MaxUnitBytes checks fallback input with typed ErrStreamUnitLimit, and approved transformed/fallback output for unit/partial profiles before writer. Whole-response retains input/final buffer bound and overall MaxOutputBytes for expansion. Separate fallback still requires prior policy block and cannot activate after processing fault.

Baseline confirmed 12 scalar emissions across transformed/fallback/fallback-transformed JSON unit paths and 2 expanded-output budget bypasses. Regression matrix covers all6JSONclasses, spaced composites, malformed JSON/UTF8 across normal/transformed/fallback/fallback-transformed and whole/unit profiles. Budget tests cover normal+fallback expansion in unit/partial/whole modes, JSON exact-limit vsonebyteover, fallback input reject before validation, newline output framing, report-only and Goerror faults with nofallback. Existing stream partition/work/race tests remain active. Before/after adversarial/component benches saved with no performance claim.

Relevant contracts and StreamConfig Godoc are synchronized; no fallback bypass or new budget type introduced.
