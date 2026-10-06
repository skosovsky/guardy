package guardy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"
)

type callbackProbeKey struct{}

type callbackProbe struct {
	stage                                string
	stop                                 func()
	err                                  error
	decode, bind, encode, final, handler int
}

type callbackArgs struct {
	Amount int `json:"amount"`
}

func (a *callbackArgs) ValidatePostBind(ctx context.Context) error {
	probe, _ := ctx.Value(callbackProbeKey{}).(*callbackProbe)
	probe.bind++
	a.Amount++
	if probe.stage == "bind" {
		probe.stop()
		return probe.err
	}
	return nil
}

func cancellationContext(t *testing.T, kind string) (context.Context, func(), error) {
	t.Helper()
	if kind == "deadline" {
		ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
		t.Cleanup(cancel)
		return ctx, func() { <-ctx.Done() }, context.DeadlineExceeded
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	return ctx, cancel, context.Canceled
}

func assertCallbackFault(t *testing.T, decision Decision, err, cause error) {
	t.Helper()
	var failure *PolicyFailure
	if !decision.IsSystemFault() || !errors.As(err, &failure) || !failure.Decision.IsSystemFault() ||
		!errors.Is(err, cause) || errors.Is(err, ErrRetryRequested) || err.Error() != ErrValidatorFailed.Error() {
		t.Fatalf("decision=%+v err=%v cause=%v", decision, err, cause)
	}
	if decision != failure.Decision {
		t.Fatalf("boundary=%+v failure=%+v", decision, failure.Decision)
	}
}

func testTypedCallbackCancellation[T any](t *testing.T) {
	t.Helper()
	for _, stage := range []string{"before", "decode", "bind", "encode"} {
		for _, kind := range []string{"cancel", "deadline"} {
			for _, outcome := range []string{"success", "domain", "wrapped"} {
				t.Run(stage+"/"+kind+"/"+outcome, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						testTypedCallbackCase[T](t, stage, kind, outcome)
					})
				})
			}
		}
	}
}

func testTypedCallbackCase[T any](t *testing.T, stage, kind, outcome string) {
	t.Helper()
	// Arrange.
	ctx, stop, cause := cancellationContext(t, kind)
	probe := &callbackProbe{stage: stage, stop: stop}
	if outcome == "domain" {
		probe.err = errors.New("private domain failure")
	}
	if outcome == "wrapped" {
		probe.err = fmt.Errorf("private callback: %w", cause)
	}
	ctx = context.WithValue(ctx, callbackProbeKey{}, probe)
	p := MustCompileArgs[T](NewPipeline[string](), WithArgsCodec(
		func(raw string, value *T) error {
			probe.decode++
			if stage == "decode" {
				stop()
				return probe.err
			}
			return json.Unmarshal([]byte(raw), value)
		},
		func(value T) (string, error) {
			probe.encode++
			if stage == "encode" {
				stop()
				return "{}", probe.err
			}
			bytes, err := json.Marshal(value)
			return string(bytes), err
		}), WithArgsFinalGuard[T](NewPipeline(WithFastPath(
		ValidatorFunc[string](func(_ context.Context, raw string) (string, *Report, error) {
			probe.final++
			return raw, nil, nil
		})))))
	wrapped := WrapArgs(p, nil, func(context.Context, T) (string, error) {
		probe.handler++
		return "delivered", nil
	})
	if stage == "before" {
		stop()
	}
	// Act.
	output, boundary, err := wrapped(ctx, `{"amount":1}`)
	// Assert.
	assertCallbackFault(t, boundary.Decision, err, cause)
	wantDecode, wantBind, wantEncode := 1, 0, 0
	switch stage {
	case "before":
		wantDecode = 0
	case "bind":
		wantBind = 1
	case "encode":
		wantBind, wantEncode = 1, 1
	case "decode":
	default:
		t.Fatal("unknown stage")
	}
	if probe.decode != wantDecode || probe.bind != wantBind || probe.encode != wantEncode ||
		probe.final != 0 || probe.handler != 0 || output != "" {
		t.Fatalf("probe=%+v output=%q", probe, output)
	}
}

func TestTypedCallbackCancellation(t *testing.T) {
	t.Run("value", testTypedCallbackCancellation[callbackArgs])
	t.Run("pointer", testTypedCallbackCancellation[*callbackArgs])
}

func TestCallbackWrappedCancellationWithLiveContext(t *testing.T) {
	for _, stage := range []string{"decode", "bind", "encode"} {
		for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(stage+"/"+cause.Error(), func(t *testing.T) {
				// Arrange.
				probe := &callbackProbe{stage: stage, stop: func() {}, err: fmt.Errorf("callback: %w", cause)}
				ctx := context.WithValue(t.Context(), callbackProbeKey{}, probe)
				p := MustCompileArgs[callbackArgs](NewPipeline[string](), WithArgsCodec(
					func(_ string, v *callbackArgs) error {
						v.Amount = 1
						if stage == "decode" {
							return probe.err
						}
						return nil
					}, func(callbackArgs) (string, error) {
						if stage == "encode" {
							return "", probe.err
						}
						return "{}", nil
					}))
				// Act.
				boundary, err := p.Validate(ctx, nil, `{}`)
				// Assert.
				assertCallbackFault(t, boundary.Decision, err, cause)
				if ctx.Err() != nil {
					t.Fatal("parent unexpectedly canceled")
				}
			})
		}
	}
}

func TestDynamicSchemaCancellation(t *testing.T) {
	for _, kind := range []string{"cancel", "deadline"} {
		for _, outcome := range []Action{ActionPass, ActionBlock, ActionRetry} {
			t.Run(kind+"/"+outcome.String(), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					// Arrange.
					ctx, stop, cause := cancellationContext(t, kind)
					final, handler := 0, 0
					p := MustCompileJSONArgs(
						NewPipeline[string](),
						JSONArgsValidatorFunc(func(context.Context, map[string]any) *Report {
							stop()
							return FinishReport(&Report{Action: outcome}, ControlSpec{Action: outcome})
						}),
						WithJSONArgsFinalGuard(NewPipeline(WithFastPath(ValidatorFunc[string](
							func(_ context.Context, s string) (string, *Report, error) { final++; return s, nil, nil },
						)))),
						WithJSONArgsMetadata(JSONArgsMetadata{ID: "schema", Shape: nil}),
					)
					wrapped := WrapGuardedJSONArgs(p, nil, func(context.Context, GuardedJSONArgs) (string, error) {
						handler++
						return "delivered", nil
					})
					// Act.
					output, boundary, err := wrapped(ctx, `{}`)
					// Assert.
					assertCallbackFault(t, boundary.Decision, err, cause)
					if final != 0 || handler != 0 || output != "" {
						t.Fatal("downstream work after cancel")
					}
				})
			})
		}
	}
}

func TestDynamicSchemaPreCanceled(t *testing.T) {
	for _, kind := range []string{"cancel", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// Arrange.
				ctx, stop, cause := cancellationContext(t, kind)
				calls := 0
				p := MustCompileJSONArgs(
					NewPipeline[string](),
					JSONArgsValidatorFunc(func(context.Context, map[string]any) *Report { calls++; return nil }),
					WithJSONArgsMetadata(JSONArgsMetadata{ID: "schema", Shape: nil}),
				)
				stop()
				// Act.
				boundary, err := p.Validate(ctx, nil, `{}`)
				// Assert.
				assertCallbackFault(t, boundary.Decision, err, cause)
				if calls != 0 {
					t.Fatal("schema ran after cancellation")
				}
			})
		})
	}
}

func testTypedCallbackBenign[T any](t *testing.T) {
	t.Helper()
	// Arrange.
	probe := &callbackProbe{stop: func() {}}
	ctx := context.WithValue(t.Context(), callbackProbeKey{}, probe)
	finalRule := ValidatorFunc[string](func(_ context.Context, raw string) (string, *Report, error) {
		probe.final++
		if raw != `{"amount":2}` {
			return raw, nil, errors.New("unexpected canonical argument")
		}
		return raw, nil, nil
	})
	final := NewPipeline(WithFastPath(finalRule))
	p := MustCompileArgs[T](NewPipeline[string](), WithArgsCodec(
		func(raw string, v *T) error { probe.decode++; return json.Unmarshal([]byte(raw), v) },
		func(v T) (string, error) { probe.encode++; bytes, err := json.Marshal(v); return string(bytes), err },
	), WithArgsFinalGuard[T](final))
	wrapped := WrapArgs(p, nil, func(context.Context, T) (string, error) { probe.handler++; return "delivered", nil })
	// Act.
	output, boundary, err := wrapped(ctx, `{"amount":1}`)
	// Assert.
	if err != nil || boundary.Decision.Disposition != DispositionNone || boundary.SanitizedRaw != `{"amount":2}` ||
		output != "delivered" || probe.decode != 1 || probe.bind != 1 || probe.encode != 1 || probe.final != 1 || probe.handler != 1 {
		t.Fatalf("boundary=%+v err=%v probe=%+v output=%q", boundary, err, probe, output)
	}
}

func TestTypedCallbackBenignMutation(t *testing.T) {
	t.Run("value", testTypedCallbackBenign[callbackArgs])
	t.Run("pointer", testTypedCallbackBenign[*callbackArgs])
}

func TestDetectorDirectAndPipelineCancellation(t *testing.T) {
	for _, detector := range []string{"semantic", "judge"} {
		for _, kind := range []string{"cancel", "deadline"} {
			for _, before := range []bool{true, false} {
				for _, pipeline := range []bool{true, false} {
					name := fmt.Sprintf("%s/%s/before=%t/pipeline=%t", detector, kind, before, pipeline)
					t.Run(name, func(t *testing.T) {
						synctest.Test(t, func(t *testing.T) {
							testDetectorCallbackCase(t, detector, kind, before, pipeline)
						})
					})
				}
			}
		}
	}
}

func testDetectorCallbackCase(t *testing.T, detector, kind string, before, pipeline bool) {
	t.Helper()
	// Arrange.
	ctx, stop, cause := cancellationContext(t, kind)
	calls := 0
	var validator Validator[string]
	if detector == "semantic" {
		validator = MustSemanticValidator(fakeMatcher{match: func(context.Context, string) (float64, error) {
			calls++
			stop()
			return 0, nil
		}}, 0.5, true)
	} else {
		validator = NewLLMJudge(fakeJudge{eval: func(context.Context, string) (Report, error) {
			calls++
			stop()
			return Report{Action: ActionRetry}, nil
		}}, true)
	}
	if before {
		stop()
	}
	// Act.
	var err error
	if pipeline {
		result, runErr := NewPipeline(WithSlowPath(validator)).Run(ctx, nil, "input")
		err = runErr
		assertCallbackFault(t, result.PolicyDecision(), err, cause)
	} else {
		_, rep, validateErr := validator.Validate(ctx, "input")
		err = validateErr
		if rep != nil {
			t.Fatalf("late report=%+v", rep)
		}
	}
	// Assert.
	wantCalls := 1
	if before {
		wantCalls = 0
	}
	if !errors.Is(err, cause) || calls != wantCalls {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestTypedEqualityDynamicComparability(t *testing.T) {
	for _, fixture := range []struct {
		name        string
		got, want   any
		fault, deny bool
	}{
		{name: "string", got: "allowed", want: "allowed"},
		{name: "int", got: 1, want: 1},
		{name: "mismatch", got: "other", want: "allowed", deny: true},
		{name: "slice", got: []string{"secret"}, want: []string{"secret"}, fault: true},
		{name: "map", got: map[string]int{"secret": 1}, want: map[string]int{"secret": 1}, fault: true},
		{name: "want_only", got: "allowed", want: []string{"secret"}, fault: true},
		{name: "struct", got: struct{ Value any }{[]string{"secret"}}, want: struct{ Value any }{1}, fault: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			// Arrange.
			key := NewScopeKey[any]("fact")
			p := NewPipeline(WithPolicyValidators(NewTypedAttributeEquals[string](key, fixture.want)))
			// Act.
			result, err := p.Run(t.Context(), NewScope(ScopeValue(key, fixture.got)), "input")
			// Assert.
			if fixture.fault {
				assertCallbackFault(t, result.PolicyDecision(), err, ErrAttributeIncomparable)
				var comparison *AttributeComparisonError
				if !errors.As(err, &comparison) || comparison.Key != "fact" {
					t.Fatalf("comparison=%+v", comparison)
				}
			} else if err != nil || result.PolicyDecision().IsTerminal() != fixture.deny {
				t.Fatalf("decision=%+v err=%v", result.PolicyDecision(), err)
			}
		})
	}
}

func TestDetectorFaultSurvivesSiblingCancellation(t *testing.T) {
	for _, detector := range []string{"semantic", "judge"} {
		t.Run(detector, func(t *testing.T) {
			// Arrange: the provider starts before the deny, then fails after sibling cancellation.
			started := make(chan struct{})
			providerErr := errors.New("provider failure")
			var check Validator[string]
			if detector == "semantic" {
				check = MustSemanticValidator(fakeMatcher{match: func(ctx context.Context, _ string) (float64, error) {
					close(started)
					<-ctx.Done()
					return 0, providerErr
				}}, 0.5, false)
			} else {
				check = NewLLMJudge(fakeJudge{eval: func(ctx context.Context, _ string) (Report, error) {
					close(started)
					<-ctx.Done()
					return Report{}, providerErr
				}}, false)
			}
			deny := ValidatorFunc[string](func(_ context.Context, raw string) (string, *Report, error) {
				<-started
				return raw, &Report{Action: ActionBlock}, nil
			})
			p := NewPipeline(WithSlowPath(check, deny))
			// Act.
			result, err := p.Run(t.Context(), nil, "input")
			// Assert.
			assertCallbackFault(t, result.PolicyDecision(), err, providerErr)
			if !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation cause")
			}
		})
	}
}
