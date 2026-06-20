package guardy

import (
	"context"
	"errors"
	"testing"
)

func TestWrapArgs_ValidatesRawBeforeHandler(t *testing.T) {
	t.Parallel()
	// Arrange.
	argsPipeline := MustCompileArgs[argsCommand](NewPipeline[string]())
	wrapped := WrapArgs(argsPipeline, nil, func(_ context.Context, req argsCommand) (string, error) {
		return "hello " + req.Name, nil
	})

	// Act.
	result, payload, err := wrapped(context.Background(), `{"name":"Ada"}`)

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello Ada" {
		t.Fatalf("result = %q", result)
	}
	if payload.Value.Name != "Ada" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestWrapGuardedOutput_ReturnsGuardedContract(t *testing.T) {
	t.Parallel()
	// Arrange.
	outputPipeline := NewPipeline(WithFastPath(ValidatorFunc[string](
		func(_ context.Context, input string) (string, *Report, error) {
			return input, FinishReport(&Report{
				Action:      ActionPass,
				Validator:   "classifier",
				PayloadKind: PayloadSafeUserText,
			}, ControlSpec{Action: ActionPass}), nil
		},
	)))
	wrapped := WrapGuardedOutput(outputPipeline, nil, func(_ context.Context, name string) (string, error) {
		return "hello " + name, nil
	})

	// Act.
	output, err := wrapped(context.Background(), "Ada")

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if !output.Deliverable {
		t.Fatalf("output = %+v", output)
	}
	if output.Value != "hello Ada" {
		t.Fatalf("Value = %q", output.Value)
	}
}

func TestWrapGuardedOutput_NextErrorDoesNotExposeResult(t *testing.T) {
	t.Parallel()
	// Arrange.
	expectedErr := errors.New("handler failed")
	outputPipeline := NewPipeline[string]()
	wrapped := WrapGuardedOutput(outputPipeline, nil, func(_ context.Context, _ string) (string, error) {
		return "raw secret", expectedErr
	})

	// Act.
	output, err := wrapped(context.Background(), "Ada")

	// Assert.
	if !errors.Is(err, expectedErr) {
		t.Fatalf("err = %v, want %v", err, expectedErr)
	}
	if output.Deliverable {
		t.Fatalf("output must not be deliverable: %+v", output)
	}
	if output.Value != "" {
		t.Fatalf("Value = %q, want zero value", output.Value)
	}
}
