# T04 implementation

R04,D18,D19. Exact-limit newline tail now waits for trusted Complete. Processor newline admission appends no more than remaining unit capacity, so further byte/newline faults with ErrStreamUnitLimit before append and reserved capacity is bounded by min(unit,pending). Newline delimiter counts against unit budget. JSON lookahead/framing remains separate and existing exact-limit/partition tests apply.

AAA baseline failed exact_tail before implementation. New matrix tests every short-input composition plus every two-way split and 1/3/7-byte partitions, smaller/exact tail/exact delimiter/extra byte/extra delimiter, pending==unit and pending>unit, one validation on success and zero output on overflow. An explicit overbudget input regression demonstrates atomic Write rejection versus prior irreversible prefixes. Core race suite, framing/partition/work targeted race and before/after component/adversarial benchmarks saved.

StreamConfig/Processor Godoc, README, CONTRACTS, STREAM_MEASUREMENTS document admissible-input partition guarantee and mutex/callback/reentry/cooperative cancellation boundaries. No worker timeout framework or perf improvement claim added. Output/framing R05,D17 remains T05.

Acceptance strengthened proof with a real validator counter: no validation or delivery before trusted Complete, one call after Complete plus repeated Complete. Formatter intrange autofix damaged a shift loop in the new test helper; reviewers rejected that diff. Fixed by a named partitionCount, then repeated lint and race sequentially on the final valid source.
