package guardy

import (
	"context"
	"errors"
	"testing"
)

func TestEqualityScopeFailures(t *testing.T) {
	for _, fixture := range []struct {
		name  string
		scope ExecutionScope
		cause error
	}{
		{name: "missing", scope: MapScope{}, cause: ErrScopeIncomplete},
		{name: "incompatible", scope: MapScope{"fact": "wrong"}, cause: ErrScopeIncompatible},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			// Arrange.
			key := NewScopeKey[int]("fact")
			calls := 0
			p := NewPipeline(WithPolicyValidators(NewTypedAttributeEquals[string](key, 1)),
				WithFastPath(ValidatorFunc[string](func(_ context.Context, raw string) (string, *Report, error) {
					calls++
					return raw, nil, nil
				})))
			// Act.
			result, err := p.Run(t.Context(), fixture.scope, "input")
			// Assert.
			assertCallbackFault(t, result.PolicyDecision(), err, fixture.cause)
			if calls != 0 {
				t.Fatal("validator invoked with unusable facts")
			}
		})
	}
}

func TestSiblingCancellationOfNestedValidation(t *testing.T) {
	// Arrange: a cooperative check delegates to another pipeline after its sibling stops it.
	started := make(chan struct{})
	nested := NewPipeline[string]()
	check := ValidatorFunc[string](func(ctx context.Context, raw string) (string, *Report, error) {
		close(started)
		<-ctx.Done()
		_, err := nested.Run(ctx, nil, raw)
		return raw, nil, err
	})
	deny := ValidatorFunc[string](func(_ context.Context, raw string) (string, *Report, error) {
		<-started
		return raw, &Report{Action: ActionBlock}, nil
	})
	p := NewPipeline(WithSlowPath(check, deny))
	// Act.
	result, err := p.Run(t.Context(), nil, "input")
	// Assert.
	if err != nil || !result.PolicyDecision().IsTerminal() {
		t.Fatalf("decision=%+v err=%v", result.PolicyDecision(), err)
	}
}

func TestCallbackFaultRetainsClassification(t *testing.T) {
	for _, kind := range []PayloadKind{PayloadTechnicalPayload, PayloadInternalControlSignal} {
		for _, boundary := range []string{"typed", "dynamic"} {
			for _, stage := range []string{"raw", "callback", "final"} {
				t.Run(kind.String()+"/"+boundary+"/"+stage, func(t *testing.T) {
					testCallbackClassification(t, kind, boundary, stage)
				})
			}
		}
	}
}

func testCallbackClassification(t *testing.T, kind PayloadKind, boundary, stage string) {
	t.Helper()
	// Arrange.
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	probe := &callbackProbe{stage: "none", stop: cancel, err: context.Canceled}
	if stage == "callback" {
		probe.stage = "bind"
	}
	ctx = context.WithValue(ctx, callbackProbeKey{}, probe)
	classify := ValidatorFunc[string](func(_ context.Context, raw string) (string, *Report, error) {
		return raw, &Report{Action: ActionPass, PayloadKind: kind}, nil
	})
	abort := ValidatorFunc[string](func(_ context.Context, raw string) (string, *Report, error) {
		cancel()
		return raw, nil, nil
	})
	raw := NewPipeline(WithFastPath(classify))
	if stage == "raw" {
		raw = NewPipeline(WithFastPath(classify, abort))
	}
	final := NewPipeline[string]()
	if stage == "final" {
		final = NewPipeline(WithFastPath(abort))
	}
	calls := 0
	var decision Decision
	var payloadKind PayloadKind
	var err error
	// Act.
	if boundary == "typed" {
		p := MustCompileArgs[callbackArgs](raw, WithArgsFinalGuard[callbackArgs](final))
		wrapped := WrapArgs(
			p,
			nil,
			func(context.Context, callbackArgs) (string, error) { calls++; return "delivered", nil },
		)
		_, args, callErr := wrapped(ctx, `{"amount":1}`)
		decision, payloadKind, err = args.Decision, args.PayloadKind, callErr
	} else {
		schema := JSONArgsValidatorFunc(func(context.Context, map[string]any) *Report {
			if stage == "callback" {
				cancel()
			}
			return nil
		})

		p := MustCompileJSONArgs(
			raw,
			schema,
			WithJSONArgsFinalGuard(final),
			WithJSONArgsMetadata(JSONArgsMetadata{ID: "schema", Shape: nil}),
		)
		wrapped := WrapGuardedJSONArgs(
			p,
			nil,
			func(context.Context, GuardedJSONArgs) (string, error) { calls++; return "delivered", nil },
		)
		_, args, callErr := wrapped(ctx, `{"amount":1}`)
		decision, payloadKind, err = args.Decision, args.PayloadKind, callErr
	}
	// Assert.
	assertCallbackFault(t, decision, err, context.Canceled)
	if decision.PayloadKind != kind || payloadKind != kind || calls != 0 {
		t.Fatalf("decision=%+v kind=%v calls=%d", decision, payloadKind, calls)
	}
	if !errors.Is(err, ErrValidatorFailed) {
		t.Fatal("missing fault sentinel")
	}
}
