package guardy

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

type positiveAmount struct {
	Amount int `json:"amount"`
}

func (v *positiveAmount) ValidatePostBind(context.Context) error {
	if v.Amount < 0 {
		return errors.New("negative amount")
	}
	return nil
}

func TestArgsPointerPostBind(t *testing.T) {
	// Arrange.
	p := MustCompileArgs[*positiveAmount](NewPipeline[string]())
	for _, raw := range []string{`{"amount":-1}`, `null`} {
		// Act.
		_, err := p.Validate(context.Background(), nil, raw)
		// Assert.
		if !errors.Is(err, ErrRetryRequested) {
			t.Fatalf("%s: %v", raw, err)
		}
	}
}

func TestFatalDispositionSurvivesRedaction(t *testing.T) {
	// Arrange.
	redact := ValidatorFunc[string](func(_ context.Context, s string) (string, *Report, error) {
		return s, &Report{Action: ActionRedact}, nil
	})
	fatal := ValidatorFunc[string](func(_ context.Context, s string) (string, *Report, error) {
		return s, &Report{Action: ActionPass, Fatal: true}, nil
	})
	p := NewPipeline(WithFastPath(redact, fatal))
	called := false
	fn := WrapInput(p, nil, func(_ context.Context, s string) (string, error) { called = true; return s, nil })
	// Act.
	_, err := fn(context.Background(), "unsafe")
	// Assert.
	if called || !errors.Is(err, ErrBlocked) {
		t.Fatalf("called=%v err=%v", called, err)
	}
}

func TestStreamReportOnlyFaultDoesNotRelease(t *testing.T) {
	// Arrange.
	fault := ValidatorFunc[string](func(_ context.Context, s string) (string, *Report, error) {
		return s, &Report{Action: ActionBlock, Disposition: DispositionSystemFault}, nil
	})
	var sink bytes.Buffer
	w, err := CompileStream(&sink, testStreamConfig(NewPipeline(WithFastPath(fault))))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("SECRET"))
	// Act.
	_, err = w.Complete(context.Background())
	var failure *PolicyFailure
	// Assert.
	if sink.Len() != 0 || !errors.As(err, &failure) || !failure.Decision.IsSystemFault() {
		t.Fatalf("sink=%q err=%v", sink.String(), err)
	}
}
