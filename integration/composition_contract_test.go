package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	g "github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
	"github.com/skosovsky/guardy/ext/jsonredact"
)

type contractJudge struct{ report g.Report }

func (j contractJudge) Evaluate(context.Context, string) (g.Report, error) { return j.report, nil }

//nolint:gocognit // Matrix checks each independent phase against actual arguments, delivery and release boundaries.
func TestCompositionControlBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		report g.Report
		want   g.FailureDisposition
	}{
		{"fatal_pass", g.Report{Action: g.ActionPass, Fatal: true}, g.DispositionTerminalDeny},
		{"fatal_redact", g.Report{Action: g.ActionRedact, Fatal: true}, g.DispositionTerminalDeny},
		{"terminal_retry", g.Report{Action: g.ActionRetry}, g.DispositionTerminalDeny},
		{"correction", g.Report{Action: g.ActionRetry, Retryable: true}, g.DispositionRetryableCorrection},
		{
			"shadow_fault",
			g.Report{Action: g.ActionBlock, ShadowMode: true, Disposition: g.DispositionSystemFault},
			g.DispositionSystemFault,
		},
		{"shadow_fatal", g.Report{Action: g.ActionBlock, ShadowMode: true, Fatal: true}, g.DispositionTerminalDeny},
		{"unknown_action", g.Report{Action: g.Action(99)}, g.DispositionSystemFault},
		{"unknown_disposition", g.Report{Disposition: g.FailureDisposition(99)}, g.DispositionSystemFault},
		{"unknown_kind", g.Report{PayloadKind: g.PayloadKind(99)}, g.DispositionSystemFault},
		{"invalid_score", g.Report{Score: math.NaN(), ShadowMode: true}, g.DispositionSystemFault},
		{
			"conflict",
			g.Report{Action: g.ActionBlock, Disposition: g.DispositionRetryableCorrection},
			g.DispositionSystemFault,
		},
		{
			"fatal_correction",
			g.Report{
				Action:      g.ActionRetry,
				Retryable:   true,
				Fatal:       true,
				Disposition: g.DispositionRetryableCorrection,
			},
			g.DispositionTerminalDeny,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, phase := range []string{"fast", "policy", "slow", "judge"} {
				t.Run(phase, func(t *testing.T) {
					// Arrange.
					report := tc.report
					rule := g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
						return value, report.Clone(), nil
					})
					var option g.PipelineOption[string]
					switch phase {
					case "fast":
						option = g.WithFastPath(rule)
					case "policy":
						option = g.WithPolicyValidators(g.NewPolicyFuncWithScope(nil,
							func(ctx context.Context, value string, _ g.ExecutionScope) (string, *g.Report, error) {
								return rule.Validate(ctx, value)
							}))
					case "slow":
						if report.Action == g.ActionRedact {
							return
						}
						option = g.WithSlowPath(rule)
					case "judge":
						if report.Action == g.ActionRedact {
							return
						}
						option = g.WithSlowPath[string](g.NewLLMJudge(contractJudge{report}, true))
					}
					p := g.NewPipeline(option).Use(func(next g.Validator[string]) g.Validator[string] {
						return g.ValidatorFunc[string](
							func(ctx context.Context, value string) (string, *g.Report, error) {
								return next.Validate(ctx, value)
							},
						)
					})
					calls := 0
					wrapped := g.WrapArgs(
						g.MustCompileArgs[map[string]string](p),
						nil,
						func(_ context.Context, _ map[string]string) (string, error) { calls++; return "executed", nil },
					)
					// Act.
					_, _, err := wrapped(context.Background(), `{"value":"secret"}`)
					delivery, deliveryErr := p.GuardDelivery(context.Background(), nil,
						g.NewUserTextPolicy("external", g.WithDeliveryFallback("fallback")), "secret")
					var sink bytes.Buffer
					s, compileErr := g.CompileStream(&sink, streamConfig(p))
					if compileErr != nil {
						t.Fatal(compileErr)
					}
					if _, writeErr := s.Write([]byte("secret")); writeErr != nil {
						t.Fatal(writeErr)
					}
					_, streamErr := s.Complete(context.Background())
					// Assert.
					for _, boundaryErr := range []error{err, deliveryErr, streamErr} {
						var failure *g.PolicyFailure
						if !errors.As(boundaryErr, &failure) || failure.Decision.Disposition != tc.want {
							t.Fatalf("want %v, error=%v failure=%+v", tc.want, boundaryErr, failure)
						}
					}
					if calls != 0 || delivery.Deliverable || sink.Len() != 0 {
						t.Fatal("mandatory boundary leaked")
					}
				})
			}
		})
	}
}

func TestCompositeAdaptersPreserveControl(t *testing.T) {
	for _, report := range []g.Report{
		{Action: g.ActionPass, Fatal: true},
		{Action: g.ActionRedact, Disposition: g.DispositionSystemFault, ShadowMode: true},
		{Action: g.ActionPass, Disposition: g.FailureDisposition(99)},
	} {
		// Arrange.
		leaf := g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
			if value == "secret" {
				return "clean", report.Clone(), nil
			}
			return value, &g.Report{Action: g.ActionPass}, nil
		})
		v := ext.MapSlice(func(v string) string { return v }, func(_ string, v string) string { return v }, leaf)
		j := jsonredact.NewJSONRedactValidator(leaf, "nested")
		_, sliceReport, sliceErr := v.Validate(context.Background(), []string{"secret", "safe"})
		_, jsonReport, jsonErr := j.Validate(context.Background(), `["secret","safe"]`)
		// Assert.
		want := g.DecisionFromReport(&report).Disposition
		if sliceErr != nil || jsonErr != nil || g.DecisionFromReport(sliceReport).Disposition != want ||
			g.DecisionFromReport(jsonReport).Disposition != want {
			t.Fatal("composite lost enforcement")
		}
	}
}

func TestCompositePayloadClassification(t *testing.T) {
	// Arrange.
	leaf := g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
		if value == "internal" {
			return value, &g.Report{PayloadKind: g.PayloadTechnicalPayload}, nil
		}
		return "clean", &g.Report{Action: g.ActionRedact}, nil
	})
	v := ext.MapSlice(func(v string) string { return v }, func(_ string, v string) string { return v }, leaf)
	j := jsonredact.NewJSONRedactValidator(leaf, "nested")
	// Act.
	_, sliceReport, sliceErr := v.Validate(context.Background(), []string{"internal", "secret"})
	_, jsonReport, jsonErr := j.Validate(context.Background(), `["internal","secret"]`)
	// Assert.
	if sliceErr != nil || jsonErr != nil || sliceReport.PayloadKind != g.PayloadTechnicalPayload ||
		jsonReport.PayloadKind != g.PayloadTechnicalPayload {
		t.Fatal("composite lost classification")
	}
}

func TestReportConstructionAndComposition(t *testing.T) {
	for _, raw := range []g.Report{
		{Action: g.ActionPass, Fatal: true},
		{Action: g.ActionRetry},
		{Action: g.ActionRetry, Retryable: true},
		{Action: g.ActionBlock, Disposition: g.DispositionSystemFault, ShadowMode: true},
	} {
		// Arrange.
		canRetry := raw.Retryable
		built := g.FinishReport(raw.Clone(), g.ControlSpec{Action: raw.Action, Retryable: &canRetry, Fatal: raw.Fatal})
		pass := &g.Report{Action: g.ActionPass}
		redact := &g.Report{Action: g.ActionRedact, PayloadKind: g.PayloadTechnicalPayload}
		want := g.DecisionFromReport(&raw).Disposition
		_, judged, err := g.NewLLMJudge(contractJudge{raw}, false).Validate(context.Background(), "value")
		first := g.ComposeReports(pass, built, redact)
		last := g.ComposeReports(redact, pass, judged)
		// Assert.
		if err != nil || g.DecisionFromReport(built).Disposition != want ||
			g.DecisionFromReport(first).Disposition != want || g.DecisionFromReport(last).Disposition != want {
			t.Fatal("report construction or permutation changed enforcement")
		}
		if first.PayloadKind != g.PayloadTechnicalPayload || last.PayloadKind != g.PayloadTechnicalPayload {
			t.Fatal("composition downgraded payload")
		}
	}
}

type contractDocument struct {
	Text string
	Raw  json.RawMessage
}

func TestMappedControlAndCancellation(t *testing.T) {
	for _, cancelDuringCheck := range []bool{false, true} {
		// Arrange.
		ctx, cancel := context.WithCancel(context.Background())
		injections := 0
		leaf := g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
			if cancelDuringCheck {
				cancel()
			}
			return value, &g.Report{Action: g.ActionRedact, Fatal: !cancelDuringCheck}, nil
		})
		mapped := g.Map(leaf, func(v contractDocument) string { return v.Text },
			func(v contractDocument, value string) contractDocument { injections++; v.Text = value; return v })
		raw := g.MapJSONRawMessage(leaf, func(v *contractDocument) json.RawMessage { return v.Raw },
			func(v *contractDocument, value json.RawMessage) *contractDocument {
				injections++
				v.Raw = value
				return v
			})
		_, rep, err := mapped.Validate(ctx, contractDocument{Text: "secret", Raw: json.RawMessage(`{}`)})
		_, rawRep, rawErr := raw.Validate(ctx, contractDocument{Text: "secret", Raw: json.RawMessage(`{}`)})
		cancel()
		// Assert.
		if injections != 0 {
			t.Fatal("failed/cancelled transformation invoked inject")
		}
		if cancelDuringCheck {
			if !errors.Is(err, context.Canceled) || !errors.Is(rawErr, context.Canceled) {
				t.Fatal("cancellation lost")
			}
		} else if err != nil || rawErr != nil || !g.DecisionFromReport(rep).IsTerminal() ||
			!g.DecisionFromReport(rawRep).IsTerminal() {
			t.Fatal("mapped escalation lost")
		}
	}
}

func TestJSONSchemaCallbackDoesNotReinitializeReport(t *testing.T) {
	// Arrange.
	schema := g.JSONArgsValidatorFunc(func(context.Context, map[string]any) *g.Report {
		return &g.Report{Action: g.ActionRetry, Retryable: false}
	})
	p := g.MustCompileJSONArgs(g.NewPipeline[string](), schema, g.WithJSONArgsMetadata(
		g.JSONArgsMetadata{ID: "callback"}),
	)

	// Act.
	_, err := p.Validate(context.Background(), nil, `{}`)
	// Assert.
	var failure *g.PolicyFailure
	if !errors.As(err, &failure) || !failure.Decision.IsTerminal() || !errors.Is(err, g.ErrBlocked) {
		t.Fatal("callback non-retryability was lost")
	}
}

func TestMappedCorruptionPreservesClassification(t *testing.T) {
	// Arrange.
	leaf := g.ValidatorFunc[string](func(context.Context, string) (string, *g.Report, error) {
		return "broken", &g.Report{Action: g.ActionRedact, PayloadKind: g.PayloadTechnicalPayload}, nil
	})
	mapped := g.MapJSONRawMessage(leaf, func(v *contractDocument) json.RawMessage { return v.Raw },
		func(v *contractDocument, value json.RawMessage) *contractDocument { v.Raw = value; return v })
	// Act.
	_, rep, err := mapped.Validate(context.Background(), contractDocument{Raw: json.RawMessage(`{}`)})
	// Assert.
	if err != nil || !g.DecisionFromReport(rep).IsRetryable() || rep.PayloadKind != g.PayloadTechnicalPayload {
		t.Fatal("corruption correction lost classification")
	}
}

func TestRecursiveCancellationAfterLeaf(t *testing.T) {
	// Arrange.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leaf := g.ValidatorFunc[string](func(context.Context, string) (string, *g.Report, error) {
		cancel()
		return "clean", &g.Report{Action: g.ActionRedact}, nil
	})
	v := jsonredact.NewJSONRedactValidator(leaf, "cancel")
	// Act.
	output, _, err := v.Validate(ctx, `"secret"`)
	// Assert.
	if !errors.Is(err, context.Canceled) || output != `"secret"` {
		t.Fatal("cancelled recursive transformation escaped")
	}
}

func TestHTTPReportFaultStopsHandler(t *testing.T) {
	// Arrange.
	leaf := g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
		return value, &g.Report{Action: g.ActionBlock, ShadowMode: true, Disposition: g.DispositionSystemFault}, nil
	})
	p := g.NewPipeline(g.WithFastPath(leaf))
	calls := 0
	handler := g.Guard(p, func(*http.Request) (string, error) { return "secret", nil }, g.PlainTextInjector())(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }),
	)
	response := httptest.NewRecorder()
	// Act.
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", nil))
	// Assert.
	if calls != 0 || response.Code != http.StatusInternalServerError {
		t.Fatal("HTTP report fault passed handler")
	}
}
