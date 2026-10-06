package guardy_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	g "github.com/skosovsky/guardy"
)

type scopeTags map[string]string
type scopeNames []string
type scopeRecord struct{ Name string }
type scopeCallback func() string
type scopeLabel string

func (v scopeLabel) String() string { return string(v) }

type scopePointerLabel struct{}

func (*scopePointerLabel) String() string { return "label" }

type scopeAssertionCase struct {
	name        string
	value       any
	requirement g.ScopeRequirement
	lookup      func(g.ExecutionScope) bool
	accepted    bool
}

func assertionCase[T any](name string, value any, accepted bool) scopeAssertionCase {
	key := g.NewScopeKey[T]("fact")
	return scopeAssertionCase{
		name: name, value: value, requirement: key.Requirement(), accepted: accepted,
		lookup: func(scope g.ExecutionScope) bool {
			_, ok := key.Lookup(scope)
			return ok
		},
	}
}

func TestScopePrecheckMatchesTypedAssertion(t *testing.T) {
	t.Parallel()
	for _, tc := range []scopeAssertionCase{
		assertionCase[map[string]string]("named_map", scopeTags{"name": "ann"}, false),
		assertionCase[[]string]("named_slice", scopeNames{"ann"}, false),
		assertionCase[struct{ Name string }]("named_struct", scopeRecord{Name: "ann"}, false),
		assertionCase[func() string]("named_function", scopeCallback(func() string { return "ann" }), false),
		assertionCase[scopeTags]("unnamed_map_for_named_key", map[string]string{}, false),
		assertionCase[map[string]string]("exact_map", map[string]string{}, true),
		assertionCase[[]string]("exact_slice", []string{}, true),
		assertionCase[struct{ Name string }]("exact_struct", struct{ Name string }{Name: "ann"}, true),
		assertionCase[func() string]("exact_function", func() string { return "ann" }, true),
		assertionCase[scopeTags]("exact_named_map", scopeTags{}, true),
		assertionCase[fmt.Stringer]("interface_implementation", scopeLabel("ann"), true),
		assertionCase[any]("any_implementation", scopeRecord{}, true),
		assertionCase[fmt.Stringer]("interface_incompatible", "ann", false),
		assertionCase[*scopePointerLabel]("typed_nil_pointer", (*scopePointerLabel)(nil), true),
		assertionCase[fmt.Stringer]("typed_nil_interface_implementation", (*scopePointerLabel)(nil), true),
		assertionCase[map[string]string]("typed_nil_map", map[string]string(nil), true),
		assertionCase[func() string]("typed_nil_function", (func() string)(nil), true),
		assertionCase[any]("nil_interface", nil, false),
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange.
			scope := g.MapScope{"fact": tc.value}
			calls := 0
			policy := g.NewPolicyFuncWithScope([]g.ScopeRequirement{tc.requirement},
				func(_ context.Context, value string, _ g.ExecutionScope) (string, *g.Report, error) {
					calls++
					return value, nil, nil
				})
			pipeline := g.NewPipeline(g.WithPolicyValidators(policy))
			// Act.
			lookupOK := tc.lookup(scope)
			result, err := pipeline.Run(context.Background(), scope, "payload")
			// Assert.
			if lookupOK != tc.accepted {
				t.Fatalf("Lookup = %v, want %v", lookupOK, tc.accepted)
			}
			if tc.accepted {
				if err != nil || calls != 1 || result.PolicyDecision().Disposition != g.DispositionNone {
					t.Fatalf("accepted value: calls=%d decision=%+v error=%v", calls, result.PolicyDecision(), err)
				}
			} else {
				assertScopeTypeFault(t, err, result.PolicyDecision(), calls)
			}

			// Arrange: exercise the boundary independently, using the same typed contract.
			calls = 0
			// Act.
			delivery, boundaryErr := pipeline.GuardDelivery(
				context.Background(), scope, g.NewUserTextPolicy("user"), "payload",
			)
			value, deliverable := delivery.DeliverableValue()
			// Assert.
			if tc.accepted {
				if boundaryErr != nil || calls != 1 || !deliverable || value != "payload" {
					t.Fatalf("accepted boundary: calls=%d delivery=%+v error=%v", calls, delivery, boundaryErr)
				}
			} else {
				assertScopeTypeFault(t, boundaryErr, delivery.Decision, calls)
				if deliverable || value != "" || delivery.Value != "" {
					t.Fatalf("fault delivered payload: %+v", delivery)
				}
			}
		})
	}
}

func assertScopeTypeFault(t *testing.T, err error, decision g.Decision, calls int) {
	t.Helper()
	var typed *g.ScopeTypeError
	var failure *g.PolicyFailure
	if calls != 0 || !errors.Is(err, g.ErrScopeIncompatible) || !errors.Is(err, g.ErrValidatorFailed) ||
		!errors.As(err, &typed) || typed.Requirement.Key != "fact" ||
		!errors.As(err, &failure) || !errors.Is(failure.Cause, g.ErrScopeIncompatible) ||
		!decision.IsSystemFault() || failure.Decision != decision {
		t.Fatalf("calls=%d decision=%+v failure=%+v error=%v", calls, decision, failure, err)
	}
}
