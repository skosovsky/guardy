package guardy

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestDecisionFromReport_CarriesCanonicalFields(t *testing.T) {
	t.Parallel()
	// Arrange.
	rep := FinishReport(&Report{
		Action:          ActionRetry,
		Code:            "FIX_INPUT",
		Validator:       "shape",
		Severity:        SeverityHigh,
		Feedback:        "rewrite payload",
		SafeUserMessage: "please change the input",
		PayloadKind:     PayloadTechnicalPayload,
	}, ControlSpec{Action: ActionRetry})

	// Act.
	decision := DecisionFromReport(rep)

	// Assert.
	if !decision.IsRetryable() {
		t.Fatal("decision should be retryable")
	}
	if decision.Code != "FIX_INPUT" {
		t.Fatalf("Code = %q", decision.Code)
	}
	if decision.SafeMessage != "please change the input" {
		t.Fatalf("SafeMessage = %q", decision.SafeMessage)
	}
	if decision.RetryFeedback != "rewrite payload" {
		t.Fatalf("RetryFeedback = %q", decision.RetryFeedback)
	}
	if decision.PayloadKind != PayloadTechnicalPayload {
		t.Fatalf("PayloadKind = %v", decision.PayloadKind)
	}
}

func TestBoundaryErrors_ExposePolicyFailure(t *testing.T) {
	t.Parallel()
	// Arrange.
	rep := FinishReport(&Report{
		Action: ActionBlock,
		Code:   "DENIED",
	}, ControlSpec{Action: ActionBlock})
	err := blockErrorFromReport(rep)

	// Act.
	var failure *PolicyFailure
	ok := errors.As(err, &failure)

	// Assert.
	if !ok {
		t.Fatal("expected PolicyFailure")
	}
	if !failure.Decision.IsTerminal() {
		t.Fatalf("Decision = %+v", failure.Decision)
	}
	if failure.Decision.Code != "DENIED" {
		t.Fatalf("Code = %q", failure.Decision.Code)
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatal("expected ErrBlocked sentinel to remain available")
	}
}

func TestValidatorFaultError_ExposesPolicyFailure(t *testing.T) {
	t.Parallel()
	// Arrange.
	err := validatorFaultError(errors.New("boom"))

	// Act.
	var failure *PolicyFailure
	ok := errors.As(err, &failure)

	// Assert.
	if !ok {
		t.Fatal("expected PolicyFailure")
	}
	if !failure.Decision.IsSystemFault() {
		t.Fatalf("Decision = %+v", failure.Decision)
	}
	if failure.Decision.Code != CodeValidatorFailed {
		t.Fatalf("Code = %q", failure.Decision.Code)
	}
}

func TestWrapInput_ExposesPolicyFailure(t *testing.T) {
	t.Parallel()
	// Arrange.
	pipeline := NewPipeline(WithFastPath(blockingStringValidator("INPUT_DENIED")))
	wrapped := WrapInput(pipeline, nil, func(_ context.Context, value string) (string, error) {
		return value, nil
	})

	// Act.
	_, err := wrapped(context.Background(), "payload")

	// Assert.
	assertPolicyFailure(t, err, "INPUT_DENIED")
}

func TestWrapOutput_ExposesPolicyFailure(t *testing.T) {
	t.Parallel()
	// Arrange.
	pipeline := NewPipeline(WithFastPath(blockingStringValidator("OUTPUT_DENIED")))
	wrapped := WrapOutput(pipeline, nil, func(context.Context, string) (string, error) {
		return "payload", nil
	})

	// Act.
	_, err := wrapped(context.Background(), "request")

	// Assert.
	assertPolicyFailure(t, err, "OUTPUT_DENIED")
}

func TestGuardWriter_ExposesPolicyFailure(t *testing.T) {
	t.Parallel()
	// Arrange.
	pipeline := NewPipeline(WithFastPath(blockingStringValidator("STREAM_DENIED")))
	var out bytes.Buffer
	writer := NewGuardWriter(&out, pipeline, WithChunkSize(1))

	// Act.
	_, err := writer.Write([]byte("x"))

	// Assert.
	assertPolicyFailure(t, err, "STREAM_DENIED")
}

func blockingStringValidator(code string) ValidatorFunc[string] {
	return func(_ context.Context, input string) (string, *Report, error) {
		return input, FinishReport(&Report{
			Action:    ActionBlock,
			Validator: "blocker",
			Code:      code,
		}, ControlSpec{Action: ActionBlock}), nil
	}
}

func assertPolicyFailure(t *testing.T, err error, code string) {
	t.Helper()
	var failure *PolicyFailure
	if !errors.As(err, &failure) {
		t.Fatalf("expected PolicyFailure, got %v", err)
	}
	if failure.Decision.Code != code {
		t.Fatalf("Decision = %+v", failure.Decision)
	}
}
