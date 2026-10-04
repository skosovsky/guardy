package guardy

import (
	"errors"
	"testing"
)

func TestBlockError_UnwrapAndDisposition(t *testing.T) {
	t.Parallel()
	rep := FinishReport(&Report{
		Action: ActionBlock,
		Code:   "DENIED",
		Reason: "internal",
	}, ControlSpec{Action: ActionBlock})
	err := blockErrorFromReport(rep)
	if !errors.Is(err, ErrBlocked) {
		t.Fatal("expected ErrBlocked")
	}
	var blockErr *BlockError
	if !errors.As(err, &blockErr) {
		t.Fatal("expected BlockError")
	}
	if blockErr.Failure.Decision.Disposition != DispositionTerminalDeny {
		t.Fatalf("disposition = %v", blockErr.Failure.Decision.Disposition)
	}
}

func TestValidatorFaultError_SystemFault(t *testing.T) {
	t.Parallel()
	cause := errors.New("boom")
	err := validatorFaultError(cause)
	if !errors.Is(err, ErrValidatorFailed) {
		t.Fatal("expected ErrValidatorFailed")
	}
	var fault *ValidatorFaultError
	if !errors.As(err, &fault) {
		t.Fatal("expected ValidatorFaultError")
	}
	if fault.Failure.Decision.Disposition != DispositionSystemFault {
		t.Fatalf("disposition = %v", fault.Failure.Decision.Disposition)
	}
	if fault.Failure.Decision.Code != CodeValidatorFailed {
		t.Fatalf("code = %q", fault.Failure.Decision.Code)
	}
}

func TestRetryError_UnwrapAndDisposition(t *testing.T) {
	t.Parallel()
	rep := FinishReport(&Report{
		Action:    ActionRetry,
		Code:      "RETRY",
		Feedback:  "fix it",
		Retryable: true,
	}, ControlSpec{Action: ActionRetry})
	err := retryErrorFromReport(rep)
	if !errors.Is(err, ErrRetryRequested) {
		t.Fatal("expected ErrRetryRequested")
	}
	var retryErr *RetryError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryError, got %v", err)
	}
	if retryErr.Failure.Decision.Disposition != DispositionRetryableCorrection {
		t.Fatalf("disposition = %v", retryErr.Failure.Decision.Disposition)
	}
}

func TestErrorFromDecision_TerminalRetryReturnsBlockError(t *testing.T) {
	t.Parallel()
	rep := FinishReport(&Report{
		Action:    ActionRetry,
		Retryable: false,
		Reason:    "no retry",
	}, ControlSpec{Action: ActionRetry, Retryable: new(false)})
	err := errorFromDecision(rep)
	var blockErr *BlockError
	if !errors.As(err, &blockErr) {
		t.Fatalf("expected BlockError, got %v", err)
	}
	if blockErr.Failure.Decision.Disposition != DispositionTerminalDeny {
		t.Fatalf("disposition = %v", blockErr.Failure.Decision.Disposition)
	}
}

func TestErrorFromDecision_RetryableReturnsRetryError(t *testing.T) {
	t.Parallel()
	rep := FinishReport(&Report{
		Action:   ActionRetry,
		Feedback: "fix",
	}, ControlSpec{Action: ActionRetry})
	err := errorFromDecision(rep)
	if _, ok := errors.AsType[*RetryError](err); !ok {
		t.Fatalf("expected RetryError, got %v", err)
	}
}

func TestCanonicalErrorGate(t *testing.T) {
	// Arrange / Act / Assert: all stream failures now reuse the canonical gate.
	for _, rep := range []Report{{Action: ActionBlock}, {Action: ActionRetry, Retryable: false}, {Action: ActionPass, Fatal: true}} {
		err := errorFromDecision(&rep)
		var pf *PolicyFailure
		if !errors.Is(err, ErrBlocked) || !errors.As(err, &pf) || !pf.Decision.IsTerminal() {
			t.Fatalf("%+v %v", rep, err)
		}
	}
	if err := errorFromDecision(&Report{Action: ActionPass}); err != nil {
		t.Fatal(err)
	}
	if err := errorFromDecision(nil); !errors.Is(err, ErrBlocked) {
		t.Fatal(err)
	}
}
