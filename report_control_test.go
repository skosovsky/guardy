package guardy

import "testing"

func TestApplyControlDefaults_RetryableByAction(t *testing.T) {
	t.Parallel()
	rep := &Report{Action: ActionRetry}
	ApplyControlDefaults(rep, ControlSpec{Action: ActionRetry})
	if !rep.Retryable {
		t.Fatal("ActionRetry should default Retryable true")
	}
	rep2 := &Report{Action: ActionBlock}
	ApplyControlDefaults(rep2, ControlSpec{Action: ActionBlock})
	if rep2.Retryable {
		t.Fatal("ActionBlock should default Retryable false")
	}
}

func TestReport_ShouldRetry(t *testing.T) {
	t.Parallel()
	if (&Report{Action: ActionRetry, Retryable: true}).ShouldRetry() != true {
		t.Fatal("expected ShouldRetry true")
	}
	if (&Report{Action: ActionRetry, Retryable: false}).ShouldRetry() != false {
		t.Fatal("expected ShouldRetry false when Retryable false")
	}
}

func TestReport_ShouldStop(t *testing.T) {
	t.Parallel()
	if !(&Report{Action: ActionBlock}).ShouldStop() {
		t.Fatal("block should stop")
	}
	if !(&Report{Fatal: true, Action: ActionPass}).ShouldStop() {
		t.Fatal("fatal should stop")
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
