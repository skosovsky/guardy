package guardy

import "testing"

func TestFinishReport_RetryableByAction(t *testing.T) {
	t.Parallel()
	rep := &Report{Action: ActionRetry}
	FinishReport(rep, ControlSpec{Action: ActionRetry})
	if !rep.Retryable {
		t.Fatal("ActionRetry should default Retryable true")
	}
	rep2 := &Report{Action: ActionBlock}
	FinishReport(rep2, ControlSpec{Action: ActionBlock})
	if rep2.Retryable {
		t.Fatal("ActionBlock should default Retryable false")
	}
}

func TestReport_RetryDisposition(t *testing.T) {
	t.Parallel()
	if !(&Report{Action: ActionRetry, Retryable: true}).IsRetryableCorrection() {
		t.Fatal("expected ShouldRetry true for retryable correction")
	}
	if (&Report{Action: ActionRetry, Retryable: false}).IsRetryableCorrection() {
		t.Fatal("expected ShouldRetry false for terminal retry")
	}
}

func TestReport_TerminalDisposition(t *testing.T) {
	t.Parallel()
	if !(&Report{Action: ActionBlock}).IsTerminalDeny() {
		t.Fatal("block should stop")
	}
	if !(&Report{Fatal: true, Action: ActionPass}).IsTerminalDeny() {
		t.Fatal("fatal should stop")
	}
	if !(&Report{Action: ActionRetry, Retryable: false}).IsTerminalDeny() {
		t.Fatal("terminal retry should stop")
	}
}

func TestReport_PublicMessage(t *testing.T) {
	t.Parallel()
	if got := (&Report{SafeUserMessage: "safe"}).PublicMessage(); got != "safe" {
		t.Fatalf("PublicMessage = %q", got)
	}
	if got := (&Report{Reason: "internal", Feedback: "schema detail", Action: ActionRetry}).PublicMessage(); got != "validation failed" {
		t.Fatalf("PublicMessage must not leak Feedback, got %q", got)
	}
}

func TestReport_OrchestratorMessage(t *testing.T) {
	t.Parallel()
	if got := (&Report{Feedback: "fix json", Action: ActionRetry}).OrchestratorMessage(); got != "fix json" {
		t.Fatalf("OrchestratorMessage = %q", got)
	}
}
