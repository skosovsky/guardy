package guardy

import (
	"context"
	"errors"
	"testing"
)

func TestJSONArgsPipeline_ValidateKeepsBoundaryTogether(t *testing.T) {
	t.Parallel()
	// Arrange.
	rawPipeline := NewPipeline(WithFastPath(ValidatorFunc[string](
		func(_ context.Context, _ string) (string, *Report, error) {
			return `{"name":"Ada","role":"admin"}`, FinishReport(&Report{
				Action:      ActionRedact,
				Validator:   "raw",
				MutatedText: `{"name":"Ada","role":"admin"}`,
			}, ControlSpec{Action: ActionRedact}), nil
		},
	)))
	schema := JSONArgsSchemaFunc{
		ID:       "command.schema",
		Metadata: map[string]any{"required": []string{"name"}},
		Validate: func(_ context.Context, object map[string]any) *Report {
			if _, ok := object["name"]; !ok {
				return &Report{Action: ActionRetry, Code: CodeJSONSchemaInvalid}
			}
			return &Report{Action: ActionPass, Validator: "shape"}
		},
	}
	pipeline := MustCompileJSONArgs(rawPipeline, schema)

	// Act.
	args, err := pipeline.Validate(context.Background(), nil, `{"name":"secret"}`)

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if args.Raw != `{"name":"secret"}` {
		t.Fatalf("Raw = %q", args.Raw)
	}
	if args.SanitizedRaw != `{"name":"Ada","role":"admin"}` {
		t.Fatalf("SanitizedRaw = %q", args.SanitizedRaw)
	}
	if args.Object["name"] != "Ada" || args.SchemaID != "command.schema" {
		t.Fatalf("args = %+v", args)
	}
	if args.Decision.Action != ActionRedact {
		t.Fatalf("Decision = %+v", args.Decision)
	}
	if shape, ok := pipeline.Shape(); !ok || shape == nil {
		t.Fatalf("Shape = %v, %v", shape, ok)
	}
}

func TestJSONArgsPipeline_InvalidObjectReturnsRetryableDecision(t *testing.T) {
	t.Parallel()
	// Arrange.
	pipeline := MustCompileJSONArgs(NewPipeline[string](), nil)

	// Act.
	args, err := pipeline.Validate(context.Background(), nil, `["not","object"]`)

	// Assert.
	var failure *PolicyFailure
	if !errors.As(err, &failure) {
		t.Fatalf("expected PolicyFailure, got %v", err)
	}
	if !failure.Decision.IsRetryable() {
		t.Fatalf("PolicyFailure = %+v", failure)
	}
	if args.Decision.Code != CodeJSONInvalid {
		t.Fatalf("Decision = %+v", args.Decision)
	}
}

func TestJSONArgsPipeline_SchemaReportControlsDecision(t *testing.T) {
	t.Parallel()
	// Arrange.
	schema := JSONArgsSchemaFunc{
		ID: "strict.schema",
		Validate: func(_ context.Context, object map[string]any) *Report {
			if _, ok := object["name"]; !ok {
				return &Report{
					Action:    ActionRetry,
					Validator: "shape",
					Code:      CodeJSONSchemaInvalid,
					Feedback:  "name is required",
				}
			}
			return nil
		},
	}
	pipeline := MustCompileJSONArgs(NewPipeline[string](), schema)

	// Act.
	args, err := pipeline.Validate(context.Background(), nil, `{"role":"admin"}`)

	// Assert.
	var failure *PolicyFailure
	if !errors.As(err, &failure) {
		t.Fatalf("expected PolicyFailure, got %v", err)
	}
	if !failure.Decision.IsRetryable() {
		t.Fatalf("Decision = %+v", failure.Decision)
	}
	if args.SchemaID != "strict.schema" || args.Decision.Code != CodeJSONSchemaInvalid {
		t.Fatalf("args = %+v", args)
	}
}
