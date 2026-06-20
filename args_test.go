package guardy

import (
	"context"
	"errors"
	"testing"
)

type argsCommand struct {
	Name string `json:"name"`
}

type argsAgeCheck struct {
	Age int `json:"age"`
}

func (a *argsAgeCheck) ValidatePostBind(context.Context) error {
	if a.Age < 18 {
		return errors.New("age too low")
	}
	return nil
}

func TestArgsPipeline_ValidateSuccess(t *testing.T) {
	t.Parallel()
	// Arrange.
	rawPipeline := NewPipeline(WithFastPath(ValidatorFunc[string](
		func(_ context.Context, input string) (string, *Report, error) {
			return input, FinishReport(&Report{
				Action:    ActionPass,
				Validator: "pass",
			}, ControlSpec{Action: ActionPass}), nil
		},
	)))
	argsPipeline := MustCompileArgs[argsCommand](
		rawPipeline,
		WithArgsShapeProvider[argsCommand](
			ShapeProviderFunc[argsCommand](func() any { return "shape" }),
		),
	)

	// Act.
	payload, err := argsPipeline.Validate(context.Background(), nil, `{"name":"Ada"}`)

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if payload.Value.Name != "Ada" {
		t.Fatalf("Value = %+v", payload.Value)
	}
	if payload.Raw != `{"name":"Ada"}` || payload.SanitizedRaw != `{"name":"Ada"}` {
		t.Fatalf("payload raw fields = %+v", payload)
	}
	if payload.Decision.Action != ActionPass {
		t.Fatalf("Decision = %+v", payload.Decision)
	}
	if shape, ok := argsPipeline.Shape(); !ok || shape != "shape" {
		t.Fatalf("Shape = %v, %v", shape, ok)
	}
}

func TestArgsPipeline_UsesSanitizedRaw(t *testing.T) {
	t.Parallel()
	// Arrange.
	rawPipeline := NewPipeline(WithFastPath(ValidatorFunc[string](
		func(_ context.Context, _ string) (string, *Report, error) {
			return `{"name":"Redacted"}`, FinishReport(&Report{
				Action:      ActionRedact,
				Validator:   "redact",
				MutatedText: `{"name":"Redacted"}`,
			}, ControlSpec{Action: ActionRedact}), nil
		},
	)))
	argsPipeline := MustCompileArgs[argsCommand](rawPipeline)

	// Act.
	payload, err := argsPipeline.Validate(context.Background(), nil, `{"name":"Secret"}`)

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if payload.Value.Name != "Redacted" {
		t.Fatalf("Value = %+v", payload.Value)
	}
	if payload.SanitizedRaw != `{"name":"Redacted"}` {
		t.Fatalf("SanitizedRaw = %q", payload.SanitizedRaw)
	}
	if payload.Decision.Action != ActionRedact {
		t.Fatalf("Decision = %+v", payload.Decision)
	}
}

func TestArgsPipeline_InvalidJSONReturnsRetryableDecision(t *testing.T) {
	t.Parallel()
	// Arrange.
	argsPipeline := MustCompileArgs[argsCommand](NewPipeline[string]())

	// Act.
	payload, err := argsPipeline.Validate(context.Background(), nil, `not-json`)

	// Assert.
	var retryErr *RetryError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryError, got %v", err)
	}
	var failure *PolicyFailure
	if !errors.As(err, &failure) {
		t.Fatal("expected PolicyFailure")
	}
	if !failure.Decision.IsRetryable() {
		t.Fatalf("PolicyFailure = %+v", failure)
	}
	if payload.Decision.Code != CodeJSONInvalid {
		t.Fatalf("payload decision = %+v", payload.Decision)
	}
	if len(payload.Reports) == 0 || payload.Reports[len(payload.Reports)-1].Code != CodeJSONInvalid {
		t.Fatalf("Reports = %+v", payload.Reports)
	}
}

func TestArgsPipeline_PostBindViolationReturnsRetryableDecision(t *testing.T) {
	t.Parallel()
	// Arrange.
	argsPipeline := MustCompileArgs[argsAgeCheck](NewPipeline[string]())

	// Act.
	payload, err := argsPipeline.Validate(context.Background(), nil, `{"age":10}`)

	// Assert.
	var retryErr *RetryError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryError, got %v", err)
	}
	if payload.Decision.Code != CodePostBindViolation {
		t.Fatalf("payload decision = %+v", payload.Decision)
	}
	if len(payload.Reports) == 0 || payload.Reports[len(payload.Reports)-1].Code != CodePostBindViolation {
		t.Fatalf("Reports = %+v", payload.Reports)
	}
}
