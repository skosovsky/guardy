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

func TestWrapGuardedArgs_PassesBoundaryToHandler(t *testing.T) {
	t.Parallel()
	// Arrange.
	argsPipeline := MustCompileArgs[argsCommand](NewPipeline[string]())
	wrapped := WrapGuardedArgs(
		argsPipeline,
		nil,
		func(_ context.Context, args GuardedArgs[argsCommand]) (string, error) {
			return args.SanitizedRaw + ":" + args.Value.Name, nil
		},
	)

	// Act.
	result, boundary, err := wrapped(context.Background(), `{"name":"Ada"}`)

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if result != `{"name":"Ada"}:Ada` {
		t.Fatalf("result = %q", result)
	}
	if boundary.Decision.Action != ActionPass || boundary.PayloadKind != PayloadSafeUserText {
		t.Fatalf("boundary = %+v", boundary)
	}
}

func TestWrapGuardedJSONArgs_PassesDynamicBoundaryToHandler(t *testing.T) {
	t.Parallel()
	// Arrange.
	jsonPipeline := MustCompileJSONArgs(NewPipeline[string](), JSONArgsSchemaFunc{ID: "dynamic.schema"})
	wrapped := WrapGuardedJSONArgs(jsonPipeline, nil, func(_ context.Context, args GuardedJSONArgs) (string, error) {
		return args.SchemaID + ":" + args.Object["name"].(string), nil
	})

	// Act.
	result, boundary, err := wrapped(context.Background(), `{"name":"Ada"}`)

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if result != "dynamic.schema:Ada" {
		t.Fatalf("result = %q", result)
	}
	if boundary.SanitizedRaw != `{"name":"Ada"}` || boundary.SchemaID != "dynamic.schema" {
		t.Fatalf("boundary = %+v", boundary)
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

func TestGuardedOutputUsesPostHandlerFacts(t *testing.T) {
	// Arrange.
	allowed := true
	factoryCalls := 0
	key := NewScopeKey[bool]("delivery.allowed")
	pipeline := NewPipeline(
		WithPolicyValidators(
			NewPolicyFuncWithScope(
				[]ScopeRequirement{key.Requirement()},
				func(_ context.Context, value string, scope ExecutionScope) (string, *Report, error) {
					current, _ := key.Lookup(scope)
					action := ActionPass
					if !current {
						action = ActionBlock
					}
					return value, &Report{Action: action}, nil
				},
			),
		),
	)
	factory := ScopeFactory(func(context.Context) (ExecutionScope, error) {
		factoryCalls++
		return NewScope(ScopeValue(key, allowed)), nil
	})
	wrapped := WrapGuardedOutput(pipeline, factory, func(context.Context, string) (string, error) {
		allowed = false
		return "private result", nil
	})
	// Act.
	output, err := wrapped(context.Background(), "request")
	// Assert.
	if !errors.Is(err, ErrBlocked) || output.Deliverable || factoryCalls != 1 {
		t.Fatalf("deliverable=%v calls=%d err=%v", output.Deliverable, factoryCalls, err)
	}
}

func TestScopeFactoryCancellationSuppressesHandler(t *testing.T) {
	// Arrange.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	pipeline := MustCompileArgs[string](NewPipeline[string]())
	wrapped := WrapArgs(pipeline, func(context.Context) (ExecutionScope, error) {
		cancel()
		return NewScope(), nil
	}, func(context.Context, string) (string, error) {
		calls++
		return "executed", nil
	})
	// Act.
	_, _, err := wrapped(ctx, `"request"`)
	// Assert.
	var failure *PolicyFailure
	if !errors.Is(err, context.Canceled) || !errors.As(err, &failure) || calls != 0 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
