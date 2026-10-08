package guardy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type mutatedContractArgs struct {
	Mode     string `json:"mode"`
	Mutation string `json:"mutation"`
}

func (v *mutatedContractArgs) ValidatePostBind(context.Context) error {
	switch v.Mutation {
	case "remove":
		v.Mode = ""
	case "enum":
		v.Mode = "invalid"
	}
	return nil
}

func executableModeChecker(counter *int) Validator[string] {
	return ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
		*counter++
		var object map[string]any
		if err := json.Unmarshal([]byte(value), &object); err != nil {
			return value, nil, err
		}
		if object["mode"] != "allowed" {
			return value, &Report{Action: ActionRetry, Retryable: true, Code: CodeJSONSchemaInvalid}, nil
		}
		return value, nil, nil
	})
}

func TestFinalChecksRejectPostMutationRequiredAndEnum(t *testing.T) {
	for _, mutation := range []string{"remove", "enum"} {
		t.Run("typed/"+mutation, func(t *testing.T) {
			// Arrange: raw schema passes, then the post-bind hook changes the value.
			early, final, calls := 0, 0, 0
			p := MustCompileArgs[mutatedContractArgs](
				MustNewPipeline(WithSequential(executableModeChecker(&early))),
				WithArgsFinalGuard[mutatedContractArgs](
					MustNewPipeline(WithSequential(executableModeChecker(&final))),
				),
			)
			handler := WrapArgs(
				p,
				nil,
				func(context.Context, mutatedContractArgs) (string, error) { calls++; return "unsafe", nil },
			)
			// Act.
			_, boundary, err := handler(context.Background(), `{"mode":"allowed","mutation":"`+mutation+`"}`)
			// Assert.
			if early != 1 || final != 1 || calls != 0 || !errors.Is(err, ErrRetryRequested) ||
				boundary.Decision.Code != CodeJSONSchemaInvalid {
				t.Fatalf("early=%d final=%d calls=%d %+v %v", early, final, calls, boundary, err)
			}
		})
		t.Run("dynamic/"+mutation, func(t *testing.T) {
			// Arrange: early schema passes, sanitizer violates required/enum.
			early, final, calls := 0, 0, 0
			sanitizer := ValidatorFunc[string](func(_ context.Context, _ string) (string, *Report, error) {
				if mutation == "remove" {
					return `{}`, &Report{Action: ActionRedact}, nil
				}
				return `{"mode":"invalid"}`, &Report{Action: ActionRedact}, nil
			})
			p := MustCompileJSONArgs(MustNewPipeline(WithSequential(executableModeChecker(&early), sanitizer)), nil,
				WithJSONArgsFinalGuard(MustNewPipeline(WithSequential(executableModeChecker(&final)))))
			handler := WrapGuardedJSONArgs(
				p,
				nil,
				func(context.Context, GuardedJSONArgs) (string, error) { calls++; return "unsafe", nil },
			)
			// Act.
			_, boundary, err := handler(context.Background(), `{"mode":"allowed"}`)
			// Assert.
			if early != 1 || final != 1 || calls != 0 || !errors.Is(err, ErrRetryRequested) ||
				boundary.Decision.Code != CodeJSONSchemaInvalid {
				t.Fatalf("early=%d final=%d calls=%d %+v %v", early, final, calls, boundary, err)
			}
		})
	}
}

func TestComposedBoundaryRoutingMatrix(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		report  *Report
		fault   error
		budget  RemediationPolicy
		outcome GuardRouteOutcome
	}{
		{name: "deny", report: &Report{Action: ActionBlock}, fault: nil, budget: RemediationPolicy{}, outcome: GuardRouteTerminalDeny},
		{name: "correction", report: &Report{Action: ActionRetry, Retryable: true}, fault: nil, budget: RemediationPolicy{MaxRetries: 1}, outcome: GuardRouteRetryCorrection},
		{name: "zero-budget", report: &Report{Action: ActionRetry, Retryable: true}, fault: nil, budget: RemediationPolicy{}, outcome: GuardRouteTerminalDeny},
		{name: "exhausted-budget", report: &Report{Action: ActionRetry, Retryable: true}, fault: nil, budget: RemediationPolicy{RetryAttempt: 1, MaxRetries: 1}, outcome: GuardRouteTerminalDeny},
		{name: "fault", report: nil, fault: errors.New("detector unavailable"), budget: RemediationPolicy{MaxRetries: 1}, outcome: GuardRouteSystemFault},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			// Arrange.
			calls := 0
			rule := ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
				return value, fixture.report, fixture.fault
			})
			p := MustCompileArgs[argsCommand](MustNewPipeline(WithSequential(rule)))
			handler := WrapArgs(
				p,
				nil,
				func(context.Context, argsCommand) (string, error) { calls++; return "unsafe", nil },
			)
			// Act.
			_, boundary, err := handler(context.Background(), `{"name":"one"}`)
			var failure *PolicyFailure
			// Assert.
			if calls != 0 || !errors.As(err, &failure) || failure.Decision != boundary.Decision {
				t.Fatalf("calls=%d boundary=%+v err=%v", calls, boundary, err)
			}
			route, routeErr := failure.Decision.Route(fixture.budget)
			if routeErr != nil || route.Outcome != fixture.outcome {
				t.Fatalf("calls=%d boundary=%+v err=%v", calls, boundary, err)
			}
		})
	}
}

func TestIndependentConsumerPoliciesAndSerialization(t *testing.T) {
	// Arrange: the same internal result has three independent destination rules.
	value := "secret result"
	for _, channel := range []string{"context", "persistence", "export"} {
		t.Run(channel, func(t *testing.T) { checkIndependentConsumerPoliciesAndSerialization(t, channel, value) })
	}
}

func TestPreHandlerDenyVersusShadowObservation(t *testing.T) {
	for _, shadow := range []bool{false, true} {
		// Arrange.
		calls, events := 0, 0
		rule := ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
			return value, &Report{Action: ActionBlock, ShadowMode: shadow}, nil
		})
		p := MustNewPipeline(WithSequential(rule), WithObserver[string](func(context.Context, GuardEvent) { events++ }))
		handler := WrapInput(
			p,
			nil,
			func(_ context.Context, value string) (string, error) { calls++; return value, nil },
		)
		// Act.
		_, err := handler(context.Background(), "unsafe")
		// Assert.
		if shadow {
			if err != nil || calls != 1 || events != 1 {
				t.Fatalf("shadow calls=%d events=%d err=%v", calls, events, err)
			}
		} else if !errors.Is(err, ErrBlocked) || calls != 0 || events != 0 {
			t.Fatalf("enforce calls=%d events=%d err=%v", calls, events, err)
		}
	}
}

func TestResumeRechecksChangedScopePolicyAndArguments(t *testing.T) {
	for _, change := range []string{"scope", "policy", "arguments"} {
		t.Run(change, func(t *testing.T) {
			// Arrange: approval is a host event, not a scope capability granted by core.
			argsAllowed := NewScopeKey[bool]("args.allowed")
			authorization := NewScopeKey[bool]("host.approved")
			allowed, calls, scopeCalls := true, 0, 0
			identity := "args-policy-A"
			factory := ScopeFactory(func(context.Context) (ExecutionScope, error) {
				scopeCalls++
				return NewScope(ScopeValue(argsAllowed, allowed)), nil
			})
			compile := func(id string) *ArgsPipeline[mutatedContractArgs] {
				policy := MustPolicyFuncWithScope([]ScopeRequirement{argsAllowed.Requirement()},
					func(_ context.Context, value string, scope ExecutionScope) (string, *Report, error) {
						permit, _ := argsAllowed.Lookup(scope)
						if !permit || id == "args-policy-B" {
							return value, &Report{Action: ActionBlock}, nil
						}
						return value, nil, nil
					})
				checks := 0
				return MustCompileArgs[mutatedContractArgs](
					MustNewPipeline(WithPolicyValidators(policy)),
					WithArgsFinalGuard[mutatedContractArgs](
						MustNewPipeline(WithSequential(executableModeChecker(&checks))),
					),
					WithArgsConfigurationID[mutatedContractArgs](id),
				)
			}
			next := func(context.Context, mutatedContractArgs) (string, error) { calls++; return "ok", nil }
			initial := WrapArgs(compile(identity), factory, next)
			_, first, firstErr := initial(context.Background(), `{"mode":"allowed","mutation":""}`)
			// Act: approval arrived, but scope/policy/args changed before resume.
			approvedScope := NewScope(ScopeValue(authorization, true))
			raw := `{"mode":"allowed","mutation":""}`
			switch change {
			case "scope":
				allowed = false
			case "policy":
				identity = "args-policy-B"
			case "arguments":
				raw = `{"mode":"invalid","mutation":""}`
			}
			resumed := WrapArgs(compile(identity), factory, next)
			_, last, resumeErr := resumed(context.Background(), raw)
			_, substitutedErr := compile(identity).Validate(context.Background(), approvedScope, raw)
			// Assert: neither a prior verdict nor authorization facts substitute args policy.
			if firstErr != nil || first.ConfigurationID != "args-policy-A" || resumeErr == nil ||
				last.ConfigurationID != identity ||
				calls != 1 ||
				scopeCalls != 2 ||
				!errors.Is(substitutedErr, ErrScopeIncomplete) {
				t.Fatalf(
					"change=%s calls=%d scopes=%d first=%+v last=%+v errors=%v %v %v",
					change,
					calls,
					scopeCalls,
					first,
					last,
					firstErr,
					resumeErr,
					substitutedErr,
				)
			}
		})
	}
}

func TestObserverDisabledEnabledSampledParity(t *testing.T) {
	var baseline observation
	for _, mode := range []string{"disabled", "enabled", "sampled"} {
		observed, sampled := observeParity(t, mode)
		// Assert: observers affect only observed event counts.
		if mode == "disabled" {
			baseline = observed
		} else if observed != baseline || sampled == 0 {
			t.Fatalf("mode=%s observed=%+v baseline=%+v sampled=%d", mode, observed, baseline, sampled)
		}
	}
}

func TestTechnicalCallbackReplacementAndFallbackDestinationChecks(t *testing.T) {
	for _, candidate := range []string{"safe notice", "secret replacement", `{"tool_calls":[]}`} {
		t.Run(
			candidate,
			func(t *testing.T) { checkTechnicalCallbackReplacementAndFallbackDestinationChecks(t, candidate) },
		)
	}
}

func TestPublicErrorTextExcludesDiagnosticsAndCorrectionFeedback(t *testing.T) {
	// Arrange.
	cause := errors.New("private detector diagnostic SECRET")
	fault := validatorFaultError(cause)
	retry := retryErrorFromReport(
		FinishReport(
			&Report{Action: ActionRetry, Feedback: "rewrite SECRET", Retryable: true},
			ControlSpec{Action: ActionRetry},
		),
	)
	// Act / Assert: caller may explicitly inspect diagnostics, never default Error text.
	if strings.Contains(fault.Error(), "SECRET") || strings.Contains(retry.Error(), "SECRET") ||
		!errors.Is(fault, cause) {
		t.Fatalf("fault=%v retry=%v", fault, retry)
	}
	var failure *PolicyFailure
	if !errors.As(retry, &failure) || failure.Decision.RetryFeedback != "rewrite SECRET" {
		t.Fatal("correction diagnostics were lost rather than separated")
	}
}

func checkIndependentConsumerPoliciesAndSerialization(t *testing.T, channel string, value string) {
	t.Helper()
	p := MustNewPipeline(
		WithSequential(ValidatorFunc[string](func(_ context.Context, s string) (string, *Report, error) {
			switch channel {
			case "context":
				return s, nil, nil
			case "persistence":
				return strings.ReplaceAll(s, "secret", "[X]"), &Report{Action: ActionRedact}, nil
			default:
				return s, &Report{Action: ActionBlock, Reason: "internal diagnostic " + s}, nil
			}
		})),
	)
	var sink bytes.Buffer
	// Act: only the projection is serialized into an actual consumer sink.
	delivery, err := p.GuardDelivery(context.Background(), nil, NewUserTextPolicy(channel), value)
	if projection, allowed := delivery.Projection(); allowed {
		if encodeErr := json.NewEncoder(&sink).Encode(projection); encodeErr != nil {
			t.Fatal(encodeErr)
		}
	}
	// Assert: denial of export does not alter context/persistence decisions.
	if channel == "export" {
		if err == nil || sink.Len() != 0 {
			t.Fatalf("%v %q", err, sink.String())
		}
	} else if err != nil || sink.Len() == 0 {
		t.Fatalf("%v %q", err, sink.String())
	}
	if channel == "persistence" && strings.Contains(sink.String(), "secret") {
		t.Fatal("original secret reached persistence")
	}
	for _, forbidden := range []string{"Raw", "Reports", "Decision", "internal diagnostic"} {
		if strings.Contains(sink.String(), forbidden) {
			t.Fatalf("internal wrapper serialized: %q", sink.String())
		}
	}
}

func checkTechnicalCallbackReplacementAndFallbackDestinationChecks(t *testing.T, candidate string) {
	t.Helper()
	// Arrange: downstream checks apply equally to callback replacement and fallback.
	p := MustNewPipeline(
		WithSequential(ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
			if strings.Contains(value, "secret") {
				return value, &Report{Action: ActionBlock}, nil
			}
			return value, nil, nil
		})),
	)
	callback := WrapGuardedOutput(
		p,
		nil,
		func(context.Context, string) (string, error) { return candidate, nil },
	)
	var callbackSink, fallbackSink bytes.Buffer
	// Act.
	replacement, replacementErr := callback(context.Background(), "ignored")
	if value, ok := replacement.DeliverableValue(); ok {
		_, _ = callbackSink.WriteString(value)
	}
	fallback, fallbackErr := p.GuardDelivery(
		context.Background(),
		nil,
		NewUserTextPolicy("external", WithDeliveryFallback(candidate)),
		`{"tool_calls":[{"name":"internal"}]}`,
	)
	if projection, ok := fallback.Projection(); ok {
		_, _ = fallbackSink.WriteString(projection.Value)
	}
	// Assert: technical kinds cannot masquerade as externally safe text.
	if candidate == "safe notice" {
		if replacementErr != nil || fallbackErr == nil || !fallback.Fallback ||
			callbackSink.String() != candidate ||
			fallbackSink.String() != candidate {
			t.Fatalf(
				"replacement=%+v fallback=%+v errors=%v %v",
				replacement,
				fallback,
				replacementErr,
				fallbackErr,
			)
		}
	} else if replacementErr == nil || fallbackErr == nil || callbackSink.Len() != 0 || fallbackSink.Len() != 0 || fallback.Deliverable {
		t.Fatalf(
			"replacement=%+v fallback=%+v errors=%v %v",
			replacement,
			fallback,
			replacementErr,
			fallbackErr,
		)
	}
}

type observation struct {
	ArgsDecision, StreamDecision Decision
	Calls                        int
	Bytes                        string
	Released                     int64
	Sequence                     uint64
}

func observeParity(t *testing.T, mode string) (observation, int) {
	t.Helper()
	// Arrange.
	seen, sampled := 0, 0
	observer := Observer(nil)
	if mode != "disabled" {
		observer = func(context.Context, GuardEvent) {
			seen++
			if mode == "enabled" || seen%2 == 0 {
				sampled++
			}
		}
	}
	shadow := ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
		return value, &Report{Action: ActionBlock, ShadowMode: true}, nil
	})
	args := MustCompileArgs[argsCommand](MustNewPipeline(WithSequential(shadow), WithObserver[string](observer)))
	var observed observation
	handler := WrapArgs(
		args,
		nil,
		func(context.Context, argsCommand) (string, error) { observed.Calls++; return "one\ntwo\n", nil },
	)
	// Act.
	value, boundary, err := handler(context.Background(), `{"name":"benign"}`)
	if err != nil {
		t.Fatal(err)
	}
	var sink bytes.Buffer
	cfg := testStreamConfig(
		MustNewPipeline(
			WithSequential(WithStreamingCapabilities(shadow, StreamCapabilities{Unit: true})),
			WithObserver[string](observer),
		),
	)
	cfg.Profile = ReleaseValidatedUnits
	if mode != "disabled" {
		cfg.Observer = func(StreamEvent) {
			seen++
			if mode == "enabled" || seen%2 == 0 {
				sampled++
			}
		}
	}
	stream, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Write([]byte(value)); err != nil {
		t.Fatal(err)
	}
	outcome, err := stream.Complete(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	observed.ArgsDecision, observed.StreamDecision = boundary.Decision, outcome.Decision
	observed.Bytes, observed.Released, observed.Sequence = sink.String(), outcome.ReleasedBytes, outcome.Sequence
	return observed, sampled
}
