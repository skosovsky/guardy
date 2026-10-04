package guardy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCanonicalArgsFinalGuardAfterPostBind(t *testing.T) {
	// Arrange: final schema requires a positive amount after binding.
	final := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, s string) (string, *Report, error) {
		var v positiveAmount
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return s, nil, err
		}
		if v.Amount == 0 {
			return s, FinishReport(
				&Report{Action: ActionRetry, Code: CodeJSONSchemaInvalid},
				ControlSpec{Action: ActionRetry},
			), nil
		}
		return s, &Report{Action: ActionPass}, nil
	})))
	p := MustCompileArgs[positiveAmount](NewPipeline[string](), WithArgsFinalGuard[positiveAmount](final))
	calls := 0
	wrapped := WrapArgs(
		p,
		nil,
		func(_ context.Context, v positiveAmount) (int, error) { calls++; return v.Amount, nil },
	)
	// Act.
	_, _, err := wrapped(context.Background(), `{"amount":0}`)
	// Assert.
	if !errors.Is(err, ErrRetryRequested) || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestFreshScopeOnEveryInvocation(t *testing.T) {
	// Arrange: caller policy factory changes between host-controlled invocations.
	key := NewScopeKey[bool]("allowed")
	policy := NewPolicyFuncWithScope(
		[]ScopeRequirement{key.Requirement()},
		func(_ context.Context, s string, scope ExecutionScope) (string, *Report, error) {
			allow, _ := key.Lookup(scope)
			if !allow {
				return s, &Report{Action: ActionBlock}, nil
			}
			return s, &Report{Action: ActionPass}, nil
		},
	)
	p := MustCompileArgs[argsCommand](NewPipeline(WithPolicyValidators(policy)))
	allowed := true
	factory := ScopeFactory(
		func(context.Context) (ExecutionScope, error) { return NewScope(ScopeValue(key, allowed)), nil },
	)
	var observed []bool
	wrapped := WrapGuardedArgs(p, factory, func(_ context.Context, _ GuardedArgs[argsCommand]) (string, error) {
		observed = append(observed, allowed)
		return "ok", nil
	})
	// Act.
	_, _, e1 := wrapped(context.Background(), `{"name":"one"}`)
	allowed = false
	_, _, e2 := wrapped(context.Background(), `{"name":"two"}`)
	// Assert: resumed invocation cannot reuse the first policy verdict.
	if e1 != nil || !errors.Is(e2, ErrBlocked) || len(observed) != 1 {
		t.Fatalf("%v %v %v", observed, e1, e2)
	}
}

func TestFallbackMustPassContentPolicy(t *testing.T) {
	// Arrange.
	p := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, s string) (string, *Report, error) {
		if strings.Contains(s, "secret") {
			return s, &Report{Action: ActionBlock}, nil
		}
		return s, &Report{Action: ActionPass}, nil
	})))
	// Act.
	result, err := p.GuardDelivery(
		context.Background(),
		nil,
		NewDeliveryPolicy("external", WithDeliveryFallback("secret fallback")),
		"secret original",
	)
	// Assert.
	if err == nil || result.Deliverable || result.Value != "" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestDynamicSchemaCannotMutateNestedArguments(t *testing.T) {
	// Arrange: schema evaluation receives an isolated deep projection.
	schema := JSONArgsSchemaFunc{ID: "nested", Validate: func(_ context.Context, obj map[string]any) *Report {
		obj["nested"].(map[string]any)["name"] = "injected"
		return &Report{Action: ActionPass}
	}}
	p := MustCompileJSONArgs(NewPipeline[string](), schema)
	// Act.
	result, err := p.Validate(context.Background(), nil, `{"nested":{"name":"approved"}}`)
	// Assert.
	if err != nil || result.Object["nested"].(map[string]any)["name"] != "approved" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestCoverageRejectsUnsupportedMandatoryBoundary(t *testing.T) {
	// Arrange / Act.
	_, err := CompileBoundaryProfile("local", []Boundary{BoundaryArgs}, []Boundary{BoundaryArgs, BoundaryDelivery})
	// Assert.
	if err == nil {
		t.Fatal("remote delivery without adapter was declared covered")
	}
}
