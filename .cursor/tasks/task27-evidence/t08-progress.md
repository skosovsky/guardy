# T08 in progress — not accepted, not committed

Baseline HEAD f392070. The previous goal turn completed accepted T07 and its commit (progress).

Implemented so far:
- One Validate-only panic recovery for sequential/policy/parallel and runtime middleware wrappers. ValidatorPanicError stores the original value without formatting, safe Error, explicit PanicValue and error-valued Unwrap. Completed observations and cancellation still traverse T06 machinery. Removed the previous broad parallel recovery that also swallowed observer panics.
- Panic regression baseline demonstrates escaped sequential/policy/middleware panic and lost original parallel error cause. Final tests cover typed/error/string/nil/unformattable values, boundary suppression, prior classification, cancellation, observer exclusion and explicit middleware construction panic.
- Clear break WithSequential/WithParallel and ValidationPhaseSequential/ValidationPhaseParallel; labels sequential/policy/parallel. Legacy API names removed, all source consumers migrated. OTel span name guardy.validator.parallel; migration documents telemetry changes. Phase-label baseline demonstrates former fast/policy/slow mismatch.
- Three-phase package/README/contracts documentation. Direct recursive adapters retain their own callback contracts.
- Existing timeout regression depended on scheduler servicing a 1ms timer before a 5ms Sleep; a root race run showed that assumption failing. It now awaits ctx.Done then deliberately returns pass without its cancellation error, directly checking rejection of that result.

Verification so far: all 19 module race suites PASS, actual runner exit 0. Root lint fix PASS. Integration lint only required wrapping two lines after the longer new API names; formatting fixed after tests ended. Final make lint (19 modules), focused race x10 and post-format integration race all PASS; all three live handles returned exit 0. Logs saved alongside this report.

Historical pending scope at this intermediate checkpoint (now implemented; see t08-implementation.md): fallible NewPipeline/Use and explicit Must wrappers; nil/typed-nil rule/option and deterministic config validation; policy/built-in constructor validation and consumer migration as needed; negative routing counters rejected; stateless fallback proposal contract; D05 invalid report combinations/two low-level fault channels and exhaustive high-level regression matrix. All five T08 criteria still apply. Neither independent acceptance nor T08 commit has happened. No T09 work started.
