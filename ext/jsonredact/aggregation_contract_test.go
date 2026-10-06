package jsonredact

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	g "github.com/skosovsky/guardy"
)

func aggregationLeaf(_ context.Context, input string) (string, *g.Report, error) {
	switch input {
	case "retry":
		return input, g.FinishReport(
			&g.Report{Action: g.ActionRetry, Code: "RETRY"},
			g.ControlSpec{Action: g.ActionRetry},
		), nil
	case "deny":
		return input, g.FinishReport(
			&g.Report{Action: g.ActionBlock, Code: "DENY"},
			g.ControlSpec{Action: g.ActionBlock},
		), nil
	case "fault":
		return input, &g.Report{Code: "FAULT", Disposition: g.DispositionSystemFault}, nil
	default:
		return input, &g.Report{Action: g.ActionPass}, nil
	}
}

func TestJSONAggregationFindsStrongestCompletedOutcome(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		documents []string
		fault     bool
	}{
		{"retry_deny", []string{`{"a":"retry","b":"deny"}`, `{"b":"deny","a":"retry"}`}, false},
		{"nested", []string{`{"a":["retry",{"x":"deny"}],"z":"pass"}`, `{"z":"pass","a":["retry",{"x":"deny"}]}`}, false},
		{"retry_deny_fault", []string{`{"a":"retry","b":"deny","c":"fault"}`, `{"c":"fault","b":"deny","a":"retry"}`}, true},
		{"nested_fault", []string{`{"a":["retry",{"x":"deny","z":"fault"}],"z":"pass"}`}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, input := range tc.documents {
				for range 100 {
					// Arrange.
					adapter := NewJSONRedactValidator(g.ValidatorFunc[string](aggregationLeaf), "")
					// Act.
					output, report, err := adapter.Validate(t.Context(), input)
					// Assert: later deny/fault is not hidden by an earlier correction.
					disposition, code := g.DispositionTerminalDeny, "DENY"
					if tc.fault {
						disposition, code = g.DispositionSystemFault, "FAULT"
					}
					decision := g.DecisionFromReport(report)
					if err != nil || output != input || decision.Disposition != disposition || decision.Code != code {
						t.Fatalf("input=%s output=%s report=%+v error=%v", input, output, report, err)
					}
				}
			}
		})
	}
}

func TestJSONAggregationTraversalAndDiagnosticTieOrder(t *testing.T) {
	t.Parallel()
	for _, input := range []string{`{"z":["z0","z1"],"a":{"y":"ay","x":"ax"}}`, `{"a":{"x":"ax","y":"ay"},"z":["z0","z1"]}`} {
		// Arrange.
		var visited []string
		adapter := NewJSONRedactValidator(
			g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
				visited = append(visited, value)
				return value, g.FinishReport(
					&g.Report{Action: g.ActionRetry, Code: value},
					g.ControlSpec{Action: g.ActionRetry},
				), nil
			}),
			"",
		)
		// Act.
		output, report, err := adapter.Validate(t.Context(), input)
		// Assert: source ordering is irrelevant; shared equal-priority composition chooses the last visited.
		if err != nil || output != input || !reflect.DeepEqual(visited, []string{"ax", "ay", "z0", "z1"}) ||
			report.Code != "z1" {
			t.Fatalf("visited=%v report=%+v error=%v", visited, report, err)
		}
	}
}

func TestJSONPriorKindSurvivesDenyThenGoFault(t *testing.T) {
	t.Parallel()
	// Arrange.
	cause := errors.New("late fault")
	var visited []string
	adapter := NewJSONRedactValidator(
		g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
			visited = append(visited, value)
			switch value {
			case "classified":
				return "redacted", &g.Report{Action: g.ActionRedact, PayloadKind: g.PayloadTechnicalPayload}, nil
			case "deny":
				return value, g.FinishReport(
					&g.Report{Action: g.ActionBlock, Code: "DENY"},
					g.ControlSpec{Action: g.ActionBlock},
				), nil
			default:
				return "failed output", &g.Report{PayloadKind: g.PayloadInternalControlSignal}, cause
			}
		}),
		"",
	)
	input := `{"c":"fault","b":"deny","a":"classified","z":"unvisited"}`
	// Act.
	output, report, err := adapter.Validate(t.Context(), input)
	// Assert.
	if !errors.Is(err, cause) || output != input || report == nil || report.PayloadKind != g.PayloadTechnicalPayload ||
		report.MutatedText != "" ||
		!reflect.DeepEqual(visited, []string{"classified", "deny", "fault"}) {
		t.Fatalf("output=%s report=%+v visited=%v error=%v", output, report, visited, err)
	}
	// Act: the completed deny remains evidence, while the late Go fault determines enforcement.
	pipeline := g.MustNewPipeline(g.WithSequential[string](adapter))
	result, runErr := pipeline.Run(t.Context(), nil, input)
	policy := g.NewUserTextPolicy("internal", g.WithDeliveryAllowedKinds(g.PayloadTechnicalPayload))
	delivery, boundaryErr := pipeline.GuardDelivery(t.Context(), nil, policy, input)
	// Assert.
	var failure *g.PolicyFailure
	if !errors.Is(runErr, cause) || result.OutputKind != g.PayloadTechnicalPayload ||
		!result.PolicyDecision().IsSystemFault() {
		t.Fatalf("result=%+v error=%v", result, runErr)
	}
	if !errors.Is(boundaryErr, cause) || !errors.As(boundaryErr, &failure) {
		t.Fatalf("error=%v", boundaryErr)
	}
	if !failure.Decision.IsSystemFault() || failure.Decision.PayloadKind != g.PayloadTechnicalPayload ||
		delivery.Deliverable ||
		delivery.Value != "" {
		t.Fatalf("delivery=%+v failure=%+v", delivery, failure)
	}
}

func TestJSONShadowCannotHideFaultAndFaultStopsTraversal(t *testing.T) {
	t.Parallel()
	// Arrange.
	var visited []string
	leaf := g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
		visited = append(visited, value)
		if value == "shadow" {
			return value, g.FinishReport(
				&g.Report{Action: g.ActionBlock, ShadowMode: true, PayloadKind: g.PayloadTechnicalPayload},
				g.ControlSpec{Action: g.ActionBlock},
			), nil
		}
		if value == "fault" {
			return "failed", &g.Report{
				Action:      g.ActionBlock,
				ShadowMode:  true,
				Disposition: g.DispositionSystemFault,
				Code:        "FIRST_FAULT",
				MutatedText: "failed mirror",
			}, nil
		}
		return value, &g.Report{Action: g.ActionPass}, nil
	})
	adapter := NewJSONRedactValidator(leaf, "")
	input := `{"z":"unvisited","b":"fault","a":"shadow"}`
	pipeline := g.MustNewPipeline(g.WithSequential[string](adapter))
	policy := g.NewUserTextPolicy(
		"internal",
		g.WithDeliveryAllowedKinds(g.PayloadTechnicalPayload),
		g.WithDeliveryFallback("fallback"),
	)
	// Act.
	output, report, err := adapter.Validate(t.Context(), input)
	firstVisits := append([]string(nil), visited...)
	result, runErr := pipeline.Run(t.Context(), nil, input)
	delivery, boundaryErr := pipeline.GuardDelivery(t.Context(), nil, policy, input)
	// Assert.
	var failure *g.PolicyFailure
	if err != nil || output != input || !g.DecisionFromReport(report).IsSystemFault() || report.Code != "FIRST_FAULT" ||
		report.MutatedText != "" ||
		!reflect.DeepEqual(firstVisits, []string{"shadow", "fault"}) {
		t.Fatalf("report=%+v visited=%v err=%v", report, firstVisits, err)
	}
	if runErr != nil || result.OutputKind != g.PayloadTechnicalPayload || !result.PolicyDecision().IsSystemFault() {
		t.Fatalf("result=%+v err=%v", result, runErr)
	}
	if !errors.As(boundaryErr, &failure) || failure.Decision.PayloadKind != g.PayloadTechnicalPayload ||
		!failure.Decision.IsSystemFault() ||
		delivery.Deliverable ||
		delivery.Value != "" ||
		delivery.Fallback {
		t.Fatalf("delivery=%+v failure=%+v error=%v", delivery, failure, boundaryErr)
	}
}

func TestJSONLaterCancellationAfterCorrectionAndDeny(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"wrapped", "parent", "invalid_report"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			assertJSONLaterCancellation(t, mode)
		})
	}
}

func assertJSONLaterCancellation(t *testing.T, mode string) {
	t.Helper()
	// Arrange.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cause := errors.New("independent failure")
	if mode == "wrapped" {
		cause = fmt.Errorf("child: %w", context.DeadlineExceeded)
	}
	var visited []string
	adapter := NewJSONRedactValidator(
		g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
			visited = append(visited, value)
			switch value {
			case "classified":
				return "redacted", &g.Report{Action: g.ActionRedact, PayloadKind: g.PayloadInternalControlSignal}, nil
			case "retry", "deny":
				return aggregationLeaf(ctx, value)
			default:
				if mode == "invalid_report" {
					return "failed", &g.Report{Action: g.Action(255), ShadowMode: true, Code: "INVALID"}, nil
				}
				if mode == "parent" {
					cancel()
				}
				return "failed", &g.Report{PayloadKind: g.PayloadTechnicalPayload}, cause
			}
		}),
		"",
	)
	input := `{"d":"fault","b":"retry","c":"deny","a":"classified","z":"unvisited"}`
	// Act.
	output, report, err := adapter.Validate(ctx, input)
	// Assert: prior classification survives; cancellation/fault beats both correction and deny.
	if output != input || report == nil || report.PayloadKind != g.PayloadInternalControlSignal ||
		report.MutatedText != "" ||
		!reflect.DeepEqual(visited, []string{"classified", "retry", "deny", "fault"}) {
		t.Fatalf("output=%s report=%+v visited=%v error=%v", output, report, visited, err)
	}
	if mode == "invalid_report" {
		if err != nil || !g.DecisionFromReport(report).IsSystemFault() || report.Code != "INVALID" {
			t.Fatalf("report=%+v error=%v", report, err)
		}
	} else if !errors.Is(err, cause) || (mode == "parent" && !errors.Is(err, context.Canceled)) {
		t.Fatalf("lost cause: %v", err)
	}
}
