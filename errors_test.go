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
	if blockErr.Report.Disposition != DispositionTerminalDeny {
		t.Fatalf("disposition = %v", blockErr.Report.Disposition)
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
	if fault.Report.Disposition != DispositionSystemFault {
		t.Fatalf("disposition = %v", fault.Report.Disposition)
	}
	if fault.Report.Code != CodeValidatorFailed {
		t.Fatalf("code = %q", fault.Report.Code)
	}
}

func TestStreamErrorFromDecision_RetryNotRetryable(t *testing.T) {
	t.Parallel()
	rep := FinishReport(&Report{
		Action:    ActionRetry,
		Retryable: false,
		Reason:    "no retry",
	}, ControlSpec{Action: ActionRetry, Retryable: new(false)})
	err := streamErrorFromDecision(rep)
	var streamErr *StreamError
	if !errors.As(err, &streamErr) {
		t.Fatal("expected StreamError")
	}
	if streamErr.Report.Retryable {
		t.Fatal("Retryable should stay false")
	}
	if streamErr.Report.Disposition != DispositionTerminalDeny {
		t.Fatalf("disposition = %v", streamErr.Report.Disposition)
	}
	if streamErr.Action != ActionBlock {
		t.Fatalf("Action = %v, want ActionBlock for terminal retry", streamErr.Action)
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatal("expected ErrBlocked for terminal retry")
	}
}

func TestStreamErrorFromDecision_Block(t *testing.T) {
	t.Parallel()
	rep := FinishReport(&Report{
		Action: ActionBlock,
		Code:   "DENIED",
	}, ControlSpec{Action: ActionBlock})
	err := streamErrorFromDecision(rep)
	var streamErr *StreamError
	if !errors.As(err, &streamErr) {
		t.Fatal("expected StreamError")
	}
	if streamErr.Report.Retryable {
		t.Fatal("block should not be Retryable")
	}
	if streamErr.Report.Disposition != DispositionTerminalDeny {
		t.Fatalf("disposition = %v", streamErr.Report.Disposition)
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatal("expected ErrBlocked")
	}
}

func TestStreamErrorFromDecision_NilReport(t *testing.T) {
	t.Parallel()
	err := streamErrorFromDecision(nil)
	var streamErr *StreamError
	if !errors.As(err, &streamErr) {
		t.Fatal("expected StreamError")
	}
	if streamErr.Action != ActionBlock {
		t.Fatalf("action = %v", streamErr.Action)
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatal("expected ErrBlocked")
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
	err := &RetryError{Feedback: rep.Feedback, Report: *rep}
	if !errors.Is(err, ErrRetryRequested) {
		t.Fatal("expected ErrRetryRequested")
	}
	if err.Report.Disposition != DispositionRetryableCorrection {
		t.Fatalf("disposition = %v", err.Report.Disposition)
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
	if blockErr.Report.Disposition != DispositionTerminalDeny {
		t.Fatalf("disposition = %v", blockErr.Report.Disposition)
	}
}

func TestErrorFromDecision_RetryableReturnsRetryError(t *testing.T) {
	t.Parallel()
	rep := FinishReport(&Report{
		Action:   ActionRetry,
		Feedback: "fix",
	}, ControlSpec{Action: ActionRetry})
	err := errorFromDecision(rep)
	var retryErr *RetryError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryError, got %v", err)
	}
}

func TestStreamErrorFromDecision_PassReturnsValidatorFailed(t *testing.T) {
	t.Parallel()
	rep := FinishReport(&Report{Action: ActionPass}, ControlSpec{Action: ActionPass})
	err := streamErrorFromDecision(rep)
	if !errors.Is(err, ErrValidatorFailed) {
		t.Fatalf("err = %v", err)
	}
}
