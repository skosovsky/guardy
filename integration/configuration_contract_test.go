package integration_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	g "github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext/jsonschema"
)

func TestArgsCompilationRequirementsAndMetadata(t *testing.T) {
	for _, tc := range []struct {
		name    string
		compile func() (any, error)
		field   string
	}{
		{name: "typed raw", field: "raw", compile: func() (any, error) { p, e := g.CompileArgs[argsConfigDTO](nil); return p, e }},
		{name: "typed final", field: "final", compile: func() (any, error) {
			p, e := g.CompileArgs[argsConfigDTO](g.MustNewPipeline[string](), g.WithArgsFinalGuard[argsConfigDTO](nil), g.WithArgsShapeProvider[argsConfigDTO](g.ShapeProviderFunc[argsConfigDTO](func() any { return "shape" })), g.WithArgsConfigurationID[argsConfigDTO]("secret"))
			return p, e
		}},
		{name: "typed decode", field: "codec", compile: func() (any, error) {
			p, e := g.CompileArgs[argsConfigDTO](g.MustNewPipeline[string](), g.WithArgsCodec[argsConfigDTO](nil, func(argsConfigDTO) (string, error) { return "", nil }))
			return p, e
		}},
		{name: "typed encode", field: "codec", compile: func() (any, error) {
			p, e := g.CompileArgs[argsConfigDTO](g.MustNewPipeline[string](), g.WithArgsCodec[argsConfigDTO](func(string, *argsConfigDTO) error { return nil }, nil))
			return p, e
		}},
		{name: "typed option", field: "options[0]", compile: func() (any, error) {
			p, e := g.CompileArgs[argsConfigDTO](g.MustNewPipeline[string](), nil)
			return p, e
		}},
		{name: "dynamic raw", field: "raw", compile: func() (any, error) { p, e := g.CompileJSONArgs(nil, nil); return p, e }},
		{name: "dynamic final", field: "final", compile: func() (any, error) {
			p, e := g.CompileJSONArgs(g.MustNewPipeline[string](), nil, g.WithJSONArgsFinalGuard(nil), g.WithJSONArgsMetadata(g.JSONArgsMetadata{ID: "secret", Shape: "schema"}))
			return p, e
		}},
		{name: "dynamic checker", field: "checker", compile: func() (any, error) {
			p, e := g.CompileJSONArgs(g.MustNewPipeline[string](), g.JSONArgsValidatorFunc(nil), g.WithJSONArgsMetadata(g.JSONArgsMetadata{ID: "secret", Shape: "schema"}))
			return p, e
		}},
		{name: "dynamic typed nil", field: "checker", compile: func() (any, error) {
			var checker *nilArgsChecker
			p, e := g.CompileJSONArgs(g.MustNewPipeline[string](), checker)
			return p, e
		}},
		{name: "dynamic option", field: "options[0]", compile: func() (any, error) { p, e := g.CompileJSONArgs(g.MustNewPipeline[string](), nil, nil); return p, e }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange.
			compile := tc.compile
			// Act.
			p, err := compile()
			// Assert: typed nil pipeline inside any is checked without invoking it.
			var cfg *g.ConfigurationError
			if !errors.Is(err, g.ErrConfiguration) || !errors.As(err, &cfg) || cfg.Field != tc.field ||
				strings.Contains(err.Error(), "secret") {
				t.Fatalf("pipeline=%v err=%v", p, err)
			}
			switch v := p.(type) {
			case *g.ArgsPipeline[argsConfigDTO]:
				if v != nil {
					t.Fatal("usable typed pipeline")
				}
			case *g.JSONArgsPipeline:
				if v != nil {
					t.Fatal("usable dynamic pipeline")
				}
			default:
				t.Fatal("unexpected pipeline")
			}
		})
	}
}

type nilArgsChecker struct{}

func (*nilArgsChecker) ValidateJSONArgs(context.Context, map[string]any) *g.Report {
	panic("nil checker invoked")
}

type argsConfigDTO struct {
	Mode   string `json:"mode"`
	Change bool   `json:"change"`
}

func (v *argsConfigDTO) ValidatePostBind(context.Context) error {
	if v.Change {
		v.Mode = "forbidden"
	}
	return nil
}

func TestMetadataOnlyAndSchemaFreeFlowsRemainValid(t *testing.T) {
	// Arrange: metadata is intentionally not an executable checker.
	typed, err := g.CompileArgs[argsConfigDTO](
		g.MustNewPipeline[string](),
		g.WithArgsShapeProvider[argsConfigDTO](g.ShapeProviderFunc[argsConfigDTO](func() any { return "shape" })),
	)
	if err != nil {
		t.Fatal(err)
	}
	dynamic, err := g.CompileJSONArgs(
		g.MustNewPipeline[string](),
		nil,
		g.WithJSONArgsMetadata(g.JSONArgsMetadata{ID: "description-only", Shape: "shape"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	a, typedErr := typed.Validate(t.Context(), nil, `{"mode":"any"}`)
	b, dynamicErr := dynamic.Validate(t.Context(), nil, `{"mode":"any"}`)
	// Assert.
	if typedErr != nil || dynamicErr != nil || a.Value.Mode != "any" || b.Object["mode"] != "any" ||
		b.SchemaID != "description-only" {
		t.Fatalf("%+v %+v %v %v", a, b, typedErr, dynamicErr)
	}
}

const argsConfigSchema = `{"type":"object","required":["mode"],"additionalProperties":false,"properties":{"mode":{"type":"string","enum":["allowed"]},"change":{"type":"boolean"}}}`

func TestRawAndFinalSchemaObserveHandlerBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, raw                          string
		retry                              bool
		rawCalls, finalCalls, handlerCalls int
	}{
		{name: "unknown", raw: `{"mode":"allowed","unknown":1}`, retry: true, rawCalls: 1},
		{name: "case", raw: `{"Mode":"allowed"}`, retry: true, rawCalls: 1},
		{name: "mutation", raw: `{"mode":"allowed","change":true}`, retry: true, rawCalls: 1, finalCalls: 1},
		{name: "benign", raw: `{"mode":"allowed"}`, rawCalls: 1, finalCalls: 1, handlerCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange: schema checks original names before standard JSON bind and final mutations after it.
			rawCount, finalCount, handlerCount := 0, 0, 0
			schema, err := jsonschema.NewJSONSchemaValidator(argsConfigSchema)
			if err != nil {
				t.Fatal(err)
			}
			raw := g.MustNewPipeline(
				g.WithSequential(
					g.ValidatorFunc[string](func(ctx context.Context, s string) (string, *g.Report, error) {
						rawCount++
						return schema.Validate(ctx, s)
					}),
				),
			)
			final := g.MustNewPipeline(
				g.WithSequential(
					g.ValidatorFunc[string](func(ctx context.Context, s string) (string, *g.Report, error) {
						finalCount++
						return schema.Validate(ctx, s)
					}),
				),
			)
			args := g.MustCompileArgs[argsConfigDTO](raw, g.WithArgsFinalGuard[argsConfigDTO](final))
			handler := g.WrapArgs(
				args,
				nil,
				func(context.Context, argsConfigDTO) (string, error) { handlerCount++; return "accepted", nil },
			)
			// Act.
			_, boundary, err := handler(t.Context(), tc.raw)
			// Assert.
			if errors.Is(err, g.ErrRetryRequested) != tc.retry || rawCount != tc.rawCalls ||
				finalCount != tc.finalCalls ||
				handlerCount != tc.handlerCalls {
				t.Fatalf("%+v err=%v calls=%d/%d/%d", boundary, err, rawCount, finalCount, handlerCount)
			}
		})
	}
}

func TestDynamicRawSchemaAndReadOnlyFinalGuard(t *testing.T) {
	for _, boundary := range []string{"typed", "dynamic"} {
		for _, raw := range []string{`{"mode":"allowed","unknown":1}`, `{"Mode":"allowed"}`, `{"mode":"allowed"}`} {
			t.Run(boundary+raw, func(t *testing.T) {
				// Arrange.
				schema, err := jsonschema.NewJSONSchemaValidator(argsConfigSchema)
				if err != nil {
					t.Fatal(err)
				}
				rawGuard := g.MustNewPipeline(g.WithSequential(schema))
				finalGuard := g.MustNewPipeline(
					g.WithSequential(
						g.ValidatorFunc[string](
							func(context.Context, string) (string, *g.Report, error) { return `{"mode":"forbidden"}`, nil, nil },
						),
					),
				)
				called := 0
				var run func(context.Context, string) (string, error)
				if boundary == "typed" {
					p := g.MustCompileArgs[argsConfigDTO](rawGuard, g.WithArgsFinalGuard[argsConfigDTO](finalGuard))
					h := g.WrapArgs(
						p,
						nil,
						func(context.Context, argsConfigDTO) (string, error) { called++; return "bad", nil },
					)
					run = func(ctx context.Context, s string) (string, error) { v, _, e := h(ctx, s); return v, e }
				} else {
					p := g.MustCompileJSONArgs(rawGuard, nil, g.WithJSONArgsFinalGuard(finalGuard))
					h := g.WrapGuardedJSONArgs(
						p,
						nil,
						func(context.Context, g.GuardedJSONArgs) (string, error) { called++; return "bad", nil },
					)
					run = func(ctx context.Context, s string) (string, error) { v, _, e := h(ctx, s); return v, e }
				}
				// Act.
				out, err := run(t.Context(), raw)
				// Assert.
				category := g.ErrRetryRequested
				if raw == `{"mode":"allowed"}` {
					category = g.ErrValidatorFailed
				}
				if called != 0 || out != "" || !errors.Is(err, category) {
					t.Fatalf("calls=%d out=%q err=%v", called, out, err)
				}
			})
		}
	}
}

func TestCallerOptionPanicsAreNotConfigurationErrors(t *testing.T) {
	// Arrange.
	defer func() {
		if recover() != "caller panic" {
			t.Fatal("caller panic was recovered or changed")
		}
	}()
	// Act / Assert.
	_, _ = g.CompileArgs[argsConfigDTO](
		g.MustNewPipeline[string](),
		func(*g.ArgsPipeline[argsConfigDTO]) { panic("caller panic") },
	)
}

func Example_documentAPI() {
	// The same schema pipeline checks plain documents and typed API arguments.
	schema, err := jsonschema.NewJSONSchemaValidator(argsConfigSchema)
	if err != nil {
		panic(err)
	}
	checks := g.MustNewPipeline(g.WithSequential(schema))
	document, _ := checks.Run(context.Background(), nil, `{"mode":"allowed"}`)
	args := g.MustCompileArgs[argsConfigDTO](checks, g.WithArgsFinalGuard[argsConfigDTO](checks))
	_, invalid := args.Validate(context.Background(), nil, `{"mode":"allowed","change":true}`)
	fmt.Println(document.PolicyDecision().Action)
	fmt.Println(errors.Is(invalid, g.ErrRetryRequested))
	// Output:
	// pass
	// true
}
