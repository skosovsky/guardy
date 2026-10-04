package guardy

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestEffectiveDispositionReportPermutations(t *testing.T) {
	// Arrange: aggregation must not confuse Action with the effective disposition.
	redaction := Report{Action: ActionRedact}
	for _, fixture := range []struct {
		name   string
		report Report
		want   FailureDisposition
	}{
		{name: "fatal-pass", report: Report{Action: ActionPass, Fatal: true}, want: DispositionTerminalDeny},
		{name: "fatal-redact", report: Report{Action: ActionRedact, Fatal: true}, want: DispositionTerminalDeny},
		{name: "fatal-retry", report: Report{Action: ActionRetry, Fatal: true}, want: DispositionTerminalDeny},
		{name: "explicit-deny", report: Report{Action: ActionPass, Disposition: DispositionTerminalDeny}, want: DispositionTerminalDeny},
		{name: "explicit-fault", report: Report{Action: ActionPass, Disposition: DispositionSystemFault}, want: DispositionSystemFault},
		{name: "shadow", report: Report{Action: ActionBlock, ShadowMode: true}, want: DispositionNone},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, reports := range [][]Report{{redaction, fixture.report}, {fixture.report, redaction}} {
				// Act.
				result := RunResult[string]{Output: "payload", Reports: reports, OutputKind: PayloadSafeUserText}
				decision := result.PolicyDecision()
				// Assert.
				if decision.Disposition != fixture.want {
					t.Fatalf("reports=%+v decision=%+v", reports, decision)
				}
			}
		})
	}
	for _, reports := range [][]Report{
		{{Action: ActionBlock}, {Action: ActionPass, Disposition: DispositionSystemFault}},
		{{Action: ActionPass, Disposition: DispositionSystemFault}, {Action: ActionBlock}},
	} {
		result := RunResult[string]{Output: "payload", Reports: reports, OutputKind: PayloadSafeUserText}
		if !result.PolicyDecision().IsSystemFault() {
			t.Fatalf("fault masked: %+v", result.PolicyDecision())
		}
	}
}

func assertBoundaryFailure(t *testing.T, decision Decision, err error, expected FailureDisposition) {
	t.Helper()
	if decision.Disposition != expected {
		t.Fatalf("decision=%+v expected=%s err=%v", decision, expected, err)
	}
	if expected == DispositionNone {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var failure *PolicyFailure
	if !errors.As(err, &failure) || failure.Decision.Disposition != expected {
		t.Fatalf("canonical failure lost: decision=%+v err=%v", decision, err)
	}
	wantRoute := GuardRouteTerminalDeny
	if expected == DispositionSystemFault {
		wantRoute = GuardRouteSystemFault
	}
	if failure.Decision.Route(RemediationPolicy{MaxRetries: 1}).Outcome != wantRoute {
		t.Fatal("boundary route disagrees")
	}
}

func checkAllEnforcementBoundaries(t *testing.T, p *Pipeline[string], expected FailureDisposition) {
	t.Helper()
	// Arrange.
	ctx := context.Background()
	raw := `{"name":"ok"}`
	calls := 0
	wrapped := WrapInput(p, nil, func(_ context.Context, value string) (string, error) { calls++; return value, nil })
	// Act / Assert: raw result may have nil infrastructure error for a policy deny.
	run, runErr := p.Run(ctx, nil, raw)
	if runErr == nil {
		runErr = errorFromDecision(run.Decision())
	}
	assertBoundaryFailure(t, run.PolicyDecision(), runErr, expected)
	_, inputErr := wrapped(ctx, raw)
	assertBoundaryFailure(t, run.PolicyDecision(), inputErr, expected)
	args := MustCompileArgs[argsCommand](p)
	argsHandler := WrapArgs(
		args,
		nil,
		func(_ context.Context, value argsCommand) (string, error) { calls++; return value.Name, nil },
	)
	_, typed, typedErr := argsHandler(ctx, raw)
	assertBoundaryFailure(t, typed.Decision, typedErr, expected)
	dynamic := MustCompileJSONArgs(p, nil)
	dynamicHandler := WrapGuardedJSONArgs(
		dynamic,
		nil,
		func(context.Context, GuardedJSONArgs) (string, error) { calls++; return "ok", nil },
	)
	_, object, objectErr := dynamicHandler(ctx, raw)
	assertBoundaryFailure(t, object.Decision, objectErr, expected)
	policy := NewDeliveryPolicy("internal", WithDeliveryAllowedKinds(PayloadSafeUserText, PayloadTechnicalPayload))
	delivery, deliveryErr := p.GuardDelivery(ctx, nil, policy, raw)
	assertBoundaryFailure(t, delivery.Decision, deliveryErr, expected)
	var sink bytes.Buffer
	cfg := testStreamConfig(p)
	cfg.Delivery = policy
	stream, compileErr := CompileStream(&sink, cfg)
	if compileErr != nil {
		t.Fatal(compileErr)
	}
	_, _ = stream.Write([]byte(raw))
	outcome, streamErr := stream.Complete(ctx)
	assertBoundaryFailure(t, outcome.Decision, streamErr, expected)
	if expected == DispositionNone {
		if calls != 3 || sink.String() != raw || !delivery.Deliverable {
			t.Fatalf("allow mismatch: calls=%d sink=%q delivery=%+v", calls, sink.String(), delivery)
		}
	} else if calls != 0 || sink.Len() != 0 || delivery.Deliverable {
		t.Fatalf("enforcement bypass: calls=%d sink=%q delivery=%+v", calls, sink.String(), delivery)
	}
}

func TestFatalExplicitShadowAndErrorAllBoundariesAgree(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		report *Report
		err    error
		want   FailureDisposition
	}{
		{name: "fatal-pass", report: &Report{Action: ActionPass, Fatal: true}, err: nil, want: DispositionTerminalDeny},
		{name: "fatal-redact", report: &Report{Action: ActionRedact, Fatal: true}, err: nil, want: DispositionTerminalDeny},
		{name: "fatal-retry", report: &Report{Action: ActionRetry, Fatal: true}, err: nil, want: DispositionTerminalDeny},
		{name: "explicit-deny", report: &Report{Action: ActionPass, Disposition: DispositionTerminalDeny}, err: nil, want: DispositionTerminalDeny},
		{name: "explicit-fault", report: &Report{Action: ActionPass, Disposition: DispositionSystemFault}, err: nil, want: DispositionSystemFault},
		{name: "shadow", report: &Report{Action: ActionBlock, ShadowMode: true}, err: nil, want: DispositionNone},
		{name: "validator-error", report: nil, err: errors.New("detector unavailable"), want: DispositionSystemFault},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			// Arrange: successful redaction precedes the terminal/fault/shadow rule.
			redactor := ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
				return value, &Report{Action: ActionRedact}, nil
			})
			rule := ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
				return value, fixture.report, fixture.err
			})
			p := NewPipeline(WithFastPath(redactor, rule))
			// Act / Assert.
			checkAllEnforcementBoundaries(t, p, fixture.want)
		})
	}
}

func TestSlowFaultDenyRetryOrderCannotChangeSafety(t *testing.T) {
	for _, order := range [][]Action{{ActionBlock, ActionRetry}, {ActionRetry, ActionBlock}} {
		// Arrange: validators ignore sibling cancellation so both reports and the
		// genuine detector fault reach aggregation independent of scheduling.
		var validators []Validator[string]
		for _, action := range order {
			validators = append(
				validators,
				ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
					return value, &Report{Action: action, Retryable: action == ActionRetry}, nil
				}),
			)
		}
		validators = append(
			validators,
			ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
				return value, nil, errors.New("real detector fault")
			}),
		)
		p := NewPipeline(WithSlowPath(validators...))
		// Act / Assert: all boundary invocations reschedule the parallel validators.
		checkAllEnforcementBoundaries(t, p, DispositionSystemFault)
	}
}
