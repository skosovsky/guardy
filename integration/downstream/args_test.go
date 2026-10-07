package downstream_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/skosovsky/toolsy"

	g "github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext/jsonschema"
	d "github.com/skosovsky/guardy/integration/downstream"
)

type sampleArgs struct {
	Value string `json:"value"`
}

func newTool[T any](
	t *testing.T,
	binder toolsy.ArgsBinder[T],
	handler func(toolsy.ValidatedArgs[T]) (toolsy.ToolResult[string, string], error),
	resultCheck toolsy.ResultValidator[string],
) toolsy.Tool {
	t.Helper()
	tool, err := toolsy.NewTypedTool(toolsy.TypedToolSpec[toolsy.NoSubject, toolsy.NoScope, T, string, string]{
		Name:            "sample",
		Description:     "sample",
		ArgsBinder:      binder,
		ResultValidator: resultCheck,
		Handler: func(_ context.Context, _ toolsy.TypedCallContext[toolsy.NoSubject, toolsy.NoScope], _ *toolsy.RunEnv, args toolsy.ValidatedArgs[T]) (toolsy.ToolResult[string, string], error) {
			return handler(args)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func finalSchema(t *testing.T, schema string) *g.Pipeline[string] {
	t.Helper()
	checker, err := jsonschema.NewJSONSchemaValidator(schema)
	if err != nil {
		t.Fatal(err)
	}
	return g.MustNewPipeline(g.WithSequential(checker))
}

//nolint:cyclop,gocognit,gocyclo // Outcome matrix asserts distinct real boundary failures and observable effects.
func TestRealToolOutcomeMatrix(t *testing.T) {
	for _, mode := range []string{"pass", "redact", "deny", "correction", "fault", "report-fault", "cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange.
			cause := errors.New(strings.Repeat("private-secret", 1000))
			var sentinel error
			leaf := g.ValidatorFunc[string](func(_ context.Context, raw string) (string, *g.Report, error) {
				switch mode {
				case "redact":
					return strings.ReplaceAll(raw, "secret", "safe"), &g.Report{Action: g.ActionRedact}, nil
				case "deny":
					return raw, &g.Report{Action: g.ActionBlock, Reason: cause.Error()}, nil
				case "correction":
					return raw, &g.Report{Action: g.ActionRetry, Retryable: true, Feedback: cause.Error()}, nil
				case "fault":
					sentinel = cause
					return raw, nil, cause
				case "report-fault":
					return raw, &g.Report{Disposition: g.DispositionSystemFault}, nil
				case "cancel":
					sentinel = context.Canceled
					return raw, nil, sentinel
				case "deadline":
					sentinel = context.DeadlineExceeded
					return raw, nil, sentinel
				default:
					return raw, nil, nil
				}
			})
			binder, err := d.NewArgsBinder[sampleArgs](
				g.MustNewPipeline(g.WithSequential(leaf)),
				g.MustNewPipeline[string](),
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			var seen toolsy.ValidatedArgs[sampleArgs]
			var chunks []toolsy.Chunk
			tool := newTool(
				t,
				binder,
				func(args toolsy.ValidatedArgs[sampleArgs]) (toolsy.ToolResult[string, string], error) {
					calls++
					seen = args
					return toolsy.NewToolResult[string, string](args.Value.Value), nil
				},
				nil,
			)
			// Act.
			err = tool.Execute(
				context.Background(),
				nil,
				toolsy.ToolInput{ArgsJSON: []byte(`{"value":"secret"}`)},
				func(c toolsy.Chunk) error { chunks = append(chunks, c); return nil },
			)
			// Assert.
			if mode == "pass" || mode == "redact" {
				expected := "secret"
				if mode == "redact" {
					expected = "safe"
				}
				var decoded sampleArgs
				if err != nil || calls != 1 || len(chunks) != 1 || json.Unmarshal(seen.Raw, &decoded) != nil ||
					decoded != seen.Value ||
					seen.Value.Value != expected ||
					seen.Metadata != nil {
					t.Fatalf("canonical boundary: calls=%d seen=%+v err=%v", calls, seen, err)
				}
				return
			}
			te, ok := toolsy.AsToolError(err)
			if !ok || calls != 0 || len(chunks) != 0 {
				t.Fatalf("dispatch escaped: calls=%d err=%v", calls, err)
			}
			code := toolsy.CodeInternal
			retry := false
			if mode == "deny" {
				code = toolsy.CodePolicyDenied
			}
			if mode == "correction" {
				code = toolsy.CodeValidationFailed
				retry = true
			}
			if mode == "deadline" {
				code = toolsy.CodeTimeout
			}
			if te.Code != code || te.Retryable != retry || strings.Contains(te.Error(), "private-secret") ||
				len(te.Reason) > 128 ||
				len(te.SafeMessage) > 128 {
				t.Fatalf("wrong public mapping: %+v", te)
			}
			if _, ok := errors.AsType[*g.PolicyFailure](err); !ok {
				t.Fatal("PolicyFailure lost")
			}
			if sentinel != nil && !errors.Is(err, sentinel) {
				t.Fatal("cause lost")
			}
		})
	}
}

func TestRawFaultWithoutMappingLosesCause(t *testing.T) {
	// Arrange: retain the reproducer, beside the supported adapter tests above.
	cause := errors.New("private cause")
	calls := 0
	pipeline := g.MustCompileArgs[sampleArgs](
		g.MustNewPipeline(
			g.WithSequential(
				g.ValidatorFunc[string](
					func(_ context.Context, s string) (string, *g.Report, error) { return s, nil, cause },
				),
			),
		),
	)
	binder := func(ctx context.Context, req toolsy.ArgsBindRequest) (toolsy.ValidatedArgs[sampleArgs], error) {
		_, err := pipeline.Validate(ctx, nil, string(req.Input.ArgsJSON))
		return toolsy.ValidatedArgs[sampleArgs]{}, err
	}
	tool := newTool(t, binder, func(toolsy.ValidatedArgs[sampleArgs]) (toolsy.ToolResult[string, string], error) {
		calls++
		return toolsy.NewToolResult[string, string]("bad"), nil
	}, nil)
	// Act.
	err := tool.Execute(
		context.Background(),
		nil,
		toolsy.ToolInput{ArgsJSON: []byte(`{}`)},
		func(toolsy.Chunk) error { return nil },
	)
	// Assert.
	te, ok := toolsy.AsToolError(err)
	if !ok || te.Code != toolsy.CodeValidationFailed || errors.Is(err, cause) || calls != 0 {
		t.Fatalf("reproducer changed: %v calls=%d", err, calls)
	}
}

type numericArgs struct {
	Amount int    `json:"amount"`
	Role   string `json:"role"`
}
type hookedArgs struct {
	Value string `json:"value"`
}

func (a *hookedArgs) ValidatePostBind(context.Context) error { a.Value = "changed"; return nil }

func TestFinalChecksAfterMutations(t *testing.T) {
	for _, kind := range []string{"required", "enum", "minimum"} {
		t.Run(kind, func(t *testing.T) {
			// Arrange.
			schema := `{"type":"object","required":["amount","role"],"properties":{"amount":{"type":"integer","minimum":1},"role":{"enum":["admin"]}}}`
			invalid := `{"amount":1}`
			if kind == "enum" {
				invalid = `{"amount":1,"role":"other"}`
			}
			if kind == "minimum" {
				invalid = `{"amount":0,"role":"admin"}`
			}
			raw := g.MustNewPipeline(
				g.WithSequential(g.ValidatorFunc[string](func(context.Context, string) (string, *g.Report, error) {
					return invalid, &g.Report{Action: g.ActionRedact}, nil
				})),
			)
			final := finalSchema(t, schema)
			typed, err := d.NewArgsBinder[numericArgs](raw, final, nil)
			if err != nil {
				t.Fatal(err)
			}
			dynamic, err := d.NewJSONArgsBinder(raw, final, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			calls, approvals := 0, 0
			handler := func() toolsy.ToolResult[string, string] {
				calls++
				return toolsy.NewToolResult[string, string]("bad")
			}
			tools := []toolsy.Tool{
				newTool(
					t,
					typed,
					func(toolsy.ValidatedArgs[numericArgs]) (toolsy.ToolResult[string, string], error) {
						return handler(), nil
					},
					nil,
				),
				newTool(
					t,
					dynamic,
					func(toolsy.ValidatedArgs[map[string]any]) (toolsy.ToolResult[string, string], error) {
						return handler(), nil
					},
					nil,
				),
			}
			env := toolsy.NewRunEnv(
				nil,
				toolsy.WithRunExecutionProfile(
					profileFunc(
						func(_ context.Context, _ toolsy.PreparedCall, next toolsy.InvocationHandler, yield func(toolsy.Chunk) error) error {
							approvals++
							return next(yield)
						},
					),
				),
			)
			// Act / Assert.
			for _, tool := range tools {
				err := tool.Execute(
					context.Background(),
					env,
					toolsy.ToolInput{ArgsJSON: []byte(`{"amount":1,"role":"admin"}`)},
					func(toolsy.Chunk) error { return nil },
				)
				te, ok := toolsy.AsToolError(err)
				if !ok || te.Code != toolsy.CodeValidationFailed || !errors.Is(err, g.ErrRetryRequested) {
					t.Fatalf("schema outcome: %v", err)
				}
			}
			if calls != 0 || approvals != 0 {
				t.Fatalf("calls=%d approvals=%d", calls, approvals)
			}
		})
	}
}

func TestPostBindAndFinalMutationBlocked(t *testing.T) {
	for _, mode := range []string{"hook", "final-mutation"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange.
			final := finalSchema(t, `{"type":"object","properties":{"value":{"enum":["original"]}}}`)
			if mode == "final-mutation" {
				final = g.MustNewPipeline(
					g.WithSequential(
						g.ValidatorFunc[string](
							func(context.Context, string) (string, *g.Report, error) { return `{"value":"original"}`, nil, nil },
						),
					),
				)
			}
			binder, err := d.NewArgsBinder[hookedArgs](g.MustNewPipeline[string](), final, nil)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			tool := newTool(
				t,
				binder,
				func(toolsy.ValidatedArgs[hookedArgs]) (toolsy.ToolResult[string, string], error) {
					calls++
					return toolsy.NewToolResult[string, string]("bad"), nil
				},
				nil,
			)
			// Act.
			err = tool.Execute(
				context.Background(),
				nil,
				toolsy.ToolInput{ArgsJSON: []byte(`{"value":"original"}`)},
				func(toolsy.Chunk) error { return nil },
			)
			// Assert.
			if err == nil || calls != 0 {
				t.Fatalf("mutation escaped: %v", err)
			}
			if mode == "final-mutation" {
				te, _ := toolsy.AsToolError(err)
				if te.Code != toolsy.CodeInternal {
					t.Fatal("final mutation is a fault")
				}
			}
		})
	}
}

func TestDynamicCanonicalAndManifestEnforcement(t *testing.T) {
	// Arrange.
	raw := g.MustNewPipeline(
		g.WithSequential(g.ValidatorFunc[string](func(_ context.Context, s string) (string, *g.Report, error) {
			return strings.ReplaceAll(s, "secret", "safe"), &g.Report{Action: g.ActionRedact}, nil
		})),
	)
	binder, err := d.NewJSONArgsBinder(raw, g.MustNewPipeline[string](), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	tool := newTool(t, binder, func(a toolsy.ValidatedArgs[map[string]any]) (toolsy.ToolResult[string, string], error) {
		calls++
		var value map[string]any
		if json.Unmarshal(a.Raw, &value) != nil || value["value"] != a.Value["value"] || a.Value["value"] != "safe" {
			t.Fatal("dynamic canonical mismatch")
		}
		return toolsy.NewToolResult[string, string]("safe"), nil
	}, nil)
	// Act.
	err = tool.Execute(
		context.Background(),
		nil,
		toolsy.ToolInput{ArgsJSON: []byte(`{"value":"secret"}`)},
		func(toolsy.Chunk) error { return nil },
	)
	// Assert.
	if err != nil || calls != 1 {
		t.Fatalf("dynamic: %v calls=%d", err, calls)
	}
	// Arrange: a custom codec binds a string to a declared integer. Its own final guard passes.
	typed, err := d.NewArgsBinder[int](
		g.MustNewPipeline[string](),
		g.MustNewPipeline[string](),
		nil,
		g.WithArgsCodec[int](
			func(_ string, v *int) error { *v = 1; return nil },
			func(int) (string, error) { return `"unsafe"`, nil },
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	intTool := newTool(t, typed, func(toolsy.ValidatedArgs[int]) (toolsy.ToolResult[string, string], error) {
		calls++
		return toolsy.NewToolResult[string, string]("bad"), nil
	}, nil)
	// Act.
	err = intTool.Execute(
		context.Background(),
		nil,
		toolsy.ToolInput{ArgsJSON: []byte(`1`)},
		func(toolsy.Chunk) error { return nil },
	)
	// Assert.
	if !errors.Is(err, g.ErrRetryRequested) || calls != 1 {
		t.Fatalf("manifest validation bypass: %v", err)
	}
}

type profileFunc func(context.Context, toolsy.PreparedCall, toolsy.InvocationHandler, func(toolsy.Chunk) error) error

func (f profileFunc) ExecutePrepared(
	ctx context.Context,
	c toolsy.PreparedCall,
	next toolsy.InvocationHandler,
	y func(toolsy.Chunk) error,
) error {
	return f(ctx, c, next, y)
}

func TestResumeRefreshesFactsAndArguments(t *testing.T) {
	// Arrange: host deliberately pauses before its external effect.
	allowed := true
	factsCalls, calls, approvals := 0, 0, 0
	pause := errors.New("host paused")
	key := g.NewScopeKey[bool]("host.allowed")
	final := g.MustNewPipeline(g.WithPolicyValidators(g.MustTypedAttributeEquals[string](key, true)))
	binder, err := d.NewArgsBinder[sampleArgs](
		g.MustNewPipeline[string](),
		final,
		func(context.Context) (g.ExecutionScope, error) {
			factsCalls++
			return g.NewScope(g.ScopeValue(key, allowed)), nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	tool := newTool(t, binder, func(toolsy.ValidatedArgs[sampleArgs]) (toolsy.ToolResult[string, string], error) {
		calls++
		return toolsy.NewToolResult[string, string]("safe"), nil
	}, nil)
	env := toolsy.NewRunEnv(
		nil,
		toolsy.WithRunExecutionProfile(
			profileFunc(
				func(_ context.Context, c toolsy.PreparedCall, _ toolsy.InvocationHandler, _ func(toolsy.Chunk) error) error {
					approvals++
					if string(c.Input.ArgsJSON) != `{"value":"first"}` {
						t.Fatal("approval args mismatch")
					}
					return pause
				},
			),
		),
	)
	// Act: first operation pauses; authorization is revoked before a fresh invocation.
	first := tool.Execute(
		context.Background(),
		env,
		toolsy.ToolInput{ArgsJSON: []byte(`{"value":"first"}`)},
		func(toolsy.Chunk) error { return nil },
	)
	allowed = false
	resumed := tool.Execute(
		context.Background(),
		env,
		toolsy.ToolInput{ArgsJSON: []byte(`{"value":"changed"}`)},
		func(toolsy.Chunk) error { return nil },
	)
	// Assert.
	if !errors.Is(first, pause) || resumed == nil || calls != 0 || approvals != 1 || factsCalls != 2 {
		t.Fatalf("resume: %v %v facts=%d calls=%d approvals=%d", first, resumed, factsCalls, calls, approvals)
	}
}

func TestConfigurationAndSafeMapping(t *testing.T) {
	// Arrange / Act.
	_, typedErr := d.NewArgsBinder[sampleArgs](g.MustNewPipeline[string](), nil, nil)
	_, dynamicErr := d.NewJSONArgsBinder(g.MustNewPipeline[string](), nil, nil, nil)
	cause := errors.New(strings.Repeat("private-secret", 1000))
	mapped := d.MapArgsError(cause)
	// Assert.
	if typedErr == nil || dynamicErr == nil || d.MapArgsError(nil) != nil || !errors.Is(mapped, cause) ||
		strings.Contains(mapped.Error(), "private-secret") ||
		len(mapped.Reason) > 128 {
		t.Fatal("configuration or safe copy failed")
	}
	// Arrange: schema missing or unsupported references must fault, not pass.
	binder, err := d.NewArgsBinder[sampleArgs](g.MustNewPipeline[string](), g.MustNewPipeline[string](), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []map[string]any{nil, {"$ref": "https://unavailable.invalid/schema"}} {
		// Act.
		_, err := binder(
			context.Background(),
			toolsy.ArgsBindRequest{
				Manifest: toolsy.ToolManifest{Parameters: schema},
				Input:    toolsy.ToolInput{ArgsJSON: []byte(`{"value":"safe"}`)},
			},
		)
		// Assert.
		te, ok := toolsy.AsToolError(err)
		if !ok || te.Code != toolsy.CodeInternal {
			t.Fatalf("unsupported schema passed: %v", err)
		}
	}
}

type postBindSchemaArgs struct {
	Amount   int    `json:"amount"`
	Role     string `json:"role,omitempty"`
	Mutation string `json:"mutation"`
}

func (a *postBindSchemaArgs) ValidatePostBind(context.Context) error {
	switch a.Mutation {
	case "required":
		a.Role = ""
	case "enum":
		a.Role = "other"
	case "minimum":
		a.Amount = 0
	}
	return nil
}

func TestPostBindRequiredEnumMinimumBeforeApproval(t *testing.T) {
	for _, mutation := range []string{"required", "enum", "minimum"} {
		t.Run(mutation, func(t *testing.T) {
			// Arrange.
			final := finalSchema(
				t,
				`{"type":"object","required":["amount","role"],"properties":{"amount":{"minimum":1},"role":{"enum":["admin"]}}}`,
			)
			binder, err := d.NewArgsBinder[postBindSchemaArgs](g.MustNewPipeline[string](), final, nil)
			if err != nil {
				t.Fatal(err)
			}
			calls, approvals := 0, 0
			tool := newTool(
				t,
				binder,
				func(toolsy.ValidatedArgs[postBindSchemaArgs]) (toolsy.ToolResult[string, string], error) {
					calls++
					return toolsy.NewToolResult[string, string]("bad"), nil
				},
				nil,
			)
			env := toolsy.NewRunEnv(
				nil,
				toolsy.WithRunExecutionProfile(
					profileFunc(
						func(_ context.Context, _ toolsy.PreparedCall, next toolsy.InvocationHandler, y func(toolsy.Chunk) error) error {
							approvals++
							return next(y)
						},
					),
				),
			)
			// Act.
			err = tool.Execute(
				context.Background(),
				env,
				toolsy.ToolInput{ArgsJSON: []byte(`{"amount":1,"role":"admin","mutation":"` + mutation + `"}`)},
				func(toolsy.Chunk) error { return nil },
			)
			// Assert.
			if !errors.Is(err, g.ErrRetryRequested) || calls != 0 || approvals != 0 {
				t.Fatalf("post-bind mutation=%s calls=%d approvals=%d err=%v", mutation, calls, approvals, err)
			}
		})
	}
}

func TestResumeChangedArgumentsAreRechecked(t *testing.T) {
	// Arrange: permission stays granted, but resumed arguments violate the schema.
	key := g.NewScopeKey[bool]("host.allowed")
	factCalls, calls, approvals := 0, 0, 0
	pause := errors.New("host pause")
	schema, err := jsonschema.NewJSONSchemaValidator(`{"type":"object","properties":{"value":{"enum":["first"]}}}`)
	if err != nil {
		t.Fatal(err)
	}
	final := g.MustNewPipeline(
		g.WithSequential(schema),
		g.WithPolicyValidators(g.MustTypedAttributeEquals[string](key, true)),
	)
	binder, err := d.NewArgsBinder[sampleArgs](
		g.MustNewPipeline[string](),
		final,
		func(context.Context) (g.ExecutionScope, error) {
			factCalls++
			return g.NewScope(g.ScopeValue(key, true)), nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	tool := newTool(t, binder, func(toolsy.ValidatedArgs[sampleArgs]) (toolsy.ToolResult[string, string], error) {
		calls++
		return toolsy.NewToolResult[string, string]("bad"), nil
	}, nil)
	env := toolsy.NewRunEnv(
		nil,
		toolsy.WithRunExecutionProfile(
			profileFunc(
				func(_ context.Context, _ toolsy.PreparedCall, _ toolsy.InvocationHandler, _ func(toolsy.Chunk) error) error {
					approvals++
					return pause
				},
			),
		),
	)
	// Act.
	first := tool.Execute(
		context.Background(),
		env,
		toolsy.ToolInput{ArgsJSON: []byte(`{"value":"first"}`)},
		func(toolsy.Chunk) error { return nil },
	)
	resumed := tool.Execute(
		context.Background(),
		env,
		toolsy.ToolInput{ArgsJSON: []byte(`{"value":"changed"}`)},
		func(toolsy.Chunk) error { return nil },
	)
	// Assert.
	if !errors.Is(first, pause) || !errors.Is(resumed, g.ErrRetryRequested) || factCalls != 2 || calls != 0 ||
		approvals != 1 {
		t.Fatalf(
			"resumed args not validated: %v %v facts=%d approvals=%d effects=%d",
			first,
			resumed,
			factCalls,
			approvals,
			calls,
		)
	}
}
