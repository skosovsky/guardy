package guardy

import (
	"context"
	"errors"
	"testing"
)

func TestPipelineConstructionRejectsAbsentRulesAndOptions(t *testing.T) {
	var pointer *fakeValidator
	var fn ValidatorFunc[string]
	var policy *constructionPolicy
	for name, options := range map[string][]PipelineOption[string]{
		"option":                  {nil},
		"sequential nil":          {WithSequential[string](nil)},
		"sequential typed nil":    {WithSequential[string](pointer)},
		"sequential nil function": {WithSequential[string](fn)},
		"parallel nil":            {WithParallel[string](nil)},
		"parallel typed nil":      {WithParallel[string](pointer)},
		"policy nil":              {WithPolicyValidators[string](nil)},
		"policy typed nil":        {WithPolicyValidators[string](policy)},
		"unused fallback":         {WithUserChannelFallback[string]("unused")},
		"unused empty fallback":   {WithUserChannelFallback[string]("")},
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange/Act.
			pipeline, err := NewPipeline(options...)
			// Assert.
			if pipeline != nil || !errors.Is(err, ErrConfiguration) {
				t.Fatalf("invalid config admitted: %v", err)
			}
			var escaped any
			func() { defer func() { escaped = recover() }(); MustNewPipeline(options...) }()
			config, ok := escaped.(error)
			if !ok || !errors.Is(config, ErrConfiguration) {
				t.Fatal("Must must panic with same configuration category")
			}
		})
	}
}

type constructionPolicy struct{}

func (*constructionPolicy) RequiredScope() []ScopeRequirement {
	panic("nil policy must be checked before RequiredScope")
}
func (*constructionPolicy) Validate(context.Context, string, ExecutionScope) (string, *Report, error) {
	panic("not invoked")
}

func TestPipelineUseRejectsNilMiddlewareLayersWithoutChangingOriginal(t *testing.T) {
	for _, phase := range []string{"sequential", "policy", "parallel"} {
		t.Run(phase, func(t *testing.T) {
			// Arrange.
			rule := &fakeValidator{}
			option := WithSequential[string](rule)
			if phase == "parallel" {
				option = WithParallel[string](rule)
			}
			if phase == "policy" {
				option = WithPolicyValidators(MustPolicyFuncWithScope[string](nil,
					func(ctx context.Context, text string, _ ExecutionScope) (string, *Report, error) {
						return rule.Validate(ctx, text)
					}))
			}
			original := MustNewPipeline(option)
			var absent *fakeValidator
			for _, middleware := range []ValidatorMiddleware[string]{nil,
				func(Validator[string]) Validator[string] { return nil },
				func(Validator[string]) Validator[string] { return absent },
			} {
				// Act.
				derived, err := original.Use(middleware)
				result, runErr := original.Run(context.Background(), nil, "input")
				// Assert.
				if derived != nil || !errors.Is(err, ErrConfiguration) || runErr != nil || result.Output != "input" {
					t.Fatalf("invalid middleware changed/admitted config: %v %v", err, runErr)
				}
			}
		})
	}
	// Arrange/Act/Assert: a nil receiver is a deterministic configuration error.
	var absent *Pipeline[string]
	if p, err := absent.Use(); p != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatal("nil Use receiver admitted")
	}
}

func TestCorePolicyAndJudgeConstruction(t *testing.T) {
	// Arrange.
	key := NewScopeKey[string]("tenant")
	var missing ScopeKey[string]
	var judge *fakeJudge
	validFn := func(_ context.Context, text string, _ ExecutionScope) (string, *Report, error) { return text, nil, nil }
	constructors := []func() error{
		func() error { _, err := NewPolicyFuncWithScope[string](nil, nil); return err },
		func() error { _, err := NewPolicyFuncWithScope([]ScopeRequirement{{Key: ""}}, validFn); return err },
		func() error {
			_, err := NewPolicyFuncWithScope([]ScopeRequirement{{Key: "tenant", Type: "invented"}}, validFn)
			return err
		},
		func() error { _, err := NewTypedAttributePresent[string](missing); return err },
		func() error { _, err := NewTypedAttributeEquals[string](missing, "tenant"); return err },
		func() error { _, err := NewTypedAttributePresent[string](key, nil); return err },
		func() error { _, err := NewTypedAttributeEquals[string](key, "tenant", nil); return err },
		func() error { _, err := NewLLMJudge(nil, false); return err },
		func() error { _, err := NewLLMJudge(judge, false); return err },
	}
	for _, construct := range constructors {
		// Act.
		err := construct()
		// Assert.
		if !errors.Is(err, ErrConfiguration) {
			t.Fatalf("invalid core config admitted: %v", err)
		}
	}
	// Empty pipeline/lists and explicitly disabled optional observer are valid.
	p, err := NewPipeline(
		WithSequential[string](),
		WithParallel[string](),
		WithPolicyValidators[string](),
		WithObserver[string](nil),
	)
	if err != nil || p == nil {
		t.Fatal(err)
	}
	if _, err := NewPipeline(WithUserChannelFallback[string](""), WithUserChannel[string]()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPolicyFuncWithScope([]ScopeRequirement{{Key: "present"}}, validFn); err != nil {
		t.Fatal(err)
	}
}

func TestScopedRuntimeNilWrapperCannotBecomePass(t *testing.T) {
	// Arrange: a factory violates its contract only for a real invocation scope.
	// A typed-nil receiver can implement Validate without panicking; reject it anyway.
	rule := MustPolicyFuncWithScope[string](
		nil,
		func(_ context.Context, value string, _ ExecutionScope) (string, *Report, error) {
			return value, nil, nil
		},
	)
	p := MustNewPipeline(WithPolicyValidators(rule)).MustUse(func(next Validator[string]) Validator[string] {
		scoped := next.(policyValidatorAdapter[string])
		if _, invalid := scoped.scope.Lookup("bad-wrapper"); invalid {
			return (*nilReceiverPass)(nil)
		}
		return next
	})
	// Act.
	guarded, err := p.GuardOutput(context.Background(), MapScope{"bad-wrapper": true}, "secret")
	// Assert.
	if err == nil || !guarded.Decision.IsSystemFault() || guarded.Deliverable || guarded.Value != "" {
		t.Fatal("typed nil runtime wrapper allowed delivery")
	}
}

type nilReceiverPass struct{}

func (*nilReceiverPass) Validate(_ context.Context, value string) (string, *Report, error) {
	return value, nil, nil
}
