package guardy

import (
	"errors"
	"testing"
)

func TestDeriveDisposition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rep  *Report
		err  error
		want FailureDisposition
	}{
		{name: "pass", rep: &Report{Action: ActionPass}, want: DispositionNone},
		{name: "redact", rep: &Report{Action: ActionRedact}, want: DispositionNone},
		{name: "block", rep: &Report{Action: ActionBlock}, want: DispositionTerminalDeny},
		{name: "fatal pass", rep: &Report{Action: ActionPass, Fatal: true}, want: DispositionTerminalDeny},
		{name: "retryable", rep: &Report{Action: ActionRetry, Retryable: true}, want: DispositionRetryableCorrection},
		{
			name: "retry not retryable",
			rep:  &Report{Action: ActionRetry, Retryable: false},
			want: DispositionTerminalDeny,
		},
		{
			name: "fatal retry",
			rep:  &Report{Action: ActionRetry, Retryable: true, Fatal: true},
			want: DispositionTerminalDeny,
		},
		{name: "unknown action", rep: &Report{Action: Action(99)}, want: DispositionSystemFault},
		{name: "system fault", rep: nil, err: errors.New("boom"), want: DispositionSystemFault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := DeriveDisposition(tt.rep, tt.err); got != tt.want {
				t.Fatalf("DeriveDisposition() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFinishReport_SetsDisposition(t *testing.T) {
	t.Parallel()
	rep := FinishReport(&Report{Action: ActionBlock}, ControlSpec{Action: ActionBlock})
	if rep.Disposition != DispositionTerminalDeny {
		t.Fatalf("Disposition = %v, want %v", rep.Disposition, DispositionTerminalDeny)
	}
}

func TestReport_DispositionHelpers(t *testing.T) {
	t.Parallel()
	block := &Report{Action: ActionBlock, Disposition: DispositionTerminalDeny}
	if !block.IsTerminalDeny() {
		t.Fatal("expected IsTerminalDeny true")
	}
	retry := &Report{Action: ActionRetry, Disposition: DispositionRetryableCorrection}
	if !retry.IsRetryableCorrection() {
		t.Fatal("expected IsRetryableCorrection true")
	}
	fault := &Report{Disposition: DispositionSystemFault}
	if !fault.IsSystemFault() {
		t.Fatal("expected IsSystemFault true")
	}
}

func TestReport_IsTerminalDeny_DerivesWhenUnset(t *testing.T) {
	t.Parallel()
	terminalRetry := &Report{Action: ActionRetry, Retryable: false}
	if !terminalRetry.IsTerminalDeny() {
		t.Fatal("expected IsTerminalDeny true for ActionRetry with Retryable false")
	}
	if terminalRetry.IsRetryableCorrection() {
		t.Fatal("expected IsRetryableCorrection false")
	}
}
