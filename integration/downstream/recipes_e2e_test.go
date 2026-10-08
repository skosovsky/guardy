//go:build e2e

package downstream_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/skosovsky/toolsy"

	g "github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
	d "github.com/skosovsky/guardy/integration/downstream"
)

func TestE2EPostHandlerFailureNeverRepairsArguments(t *testing.T) {
	// Arrange.
	effects, yields := 0, 0
	cause := errors.New("private result denied")
	binder, err := d.NewArgsBinder[sampleArgs](g.MustNewPipeline[string](), g.MustNewPipeline[string](), nil)
	if err != nil {
		t.Fatal(err)
	}
	guard := g.MustNewPipeline(
		g.WithSequential(g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
			return value, &g.Report{Action: g.ActionBlock}, nil
		})),
	)
	tool := newTool(t, binder, func(toolsy.ValidatedArgs[sampleArgs]) (toolsy.ToolResult[string, string], error) {
		effects++
		return toolsy.NewToolResult[string, string]("private"), nil
	}, func(value string) error {
		_, guardErr := guard.GuardOutput(context.Background(), nil, value)
		return errors.Join(cause, guardErr)
	})
	// Act.
	err = tool.Execute(
		context.Background(),
		nil,
		toolsy.ToolInput{ArgsJSON: []byte(`{"value":"safe"}`)},
		func(toolsy.Chunk) error { yields++; return nil },
	)
	// Assert.
	var resultFailure *toolsy.ResultContractError
	te, ok := toolsy.AsToolError(err)
	if effects != 1 || yields != 0 || !ok || te.Code != toolsy.CodeInternal || te.Retryable ||
		toolsy.ClientCorrectable(te.Code) ||
		!errors.As(err, &resultFailure) ||
		!errors.Is(err, cause) {
		t.Fatalf("post-handler: effects=%d yields=%d err=%v", effects, yields, err)
	}
}

func TestE2EDeliverOnlyGuardedValueNotEnvelope(t *testing.T) {
	// Arrange.
	effects := 0
	var wire string
	binder, err := d.NewArgsBinder[sampleArgs](g.MustNewPipeline[string](), g.MustNewPipeline[string](), nil)
	if err != nil {
		t.Fatal(err)
	}
	output := g.MustNewPipeline(
		g.WithSequential(g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
			return strings.ReplaceAll(
				value,
				"secret",
				"safe",
			), &g.Report{
				Action: g.ActionRedact,
				Reason: "internal-report",
			}, nil
		})),
	)
	tool := newTool(t, binder, func(toolsy.ValidatedArgs[sampleArgs]) (toolsy.ToolResult[string, string], error) {
		effects++
		delivery, deliveryErr := output.GuardDelivery(context.Background(), nil, g.NewUserTextPolicy("model"), "secret")
		if deliveryErr != nil {
			return toolsy.ToolResult[string, string]{}, deliveryErr
		}
		projection, ok := delivery.Projection()
		if !ok {
			return toolsy.ToolResult[string, string]{}, errors.New("no projection")
		}
		result := toolsy.NewToolResult[string, string](projection.Value)
		result.Effects = []string{"private-effect"}
		result.Audience = toolsy.ToolAudience("private-audience")
		result.EnvelopeMetadata = map[string]any{"control": "private-control"}
		return result, nil
	}, nil)
	// Act: host emits Data only, never json.Marshal(chunk) or result envelope.
	err = tool.Execute(
		context.Background(),
		nil,
		toolsy.ToolInput{ArgsJSON: []byte(`{"value":"safe"}`)},
		func(chunk toolsy.Chunk) error {
			wire += string(chunk.Data)
			if len(chunk.Effects) != 1 {
				t.Fatal("operator effect evidence lost")
			}
			return nil
		},
	)
	// Assert.
	if err != nil || effects != 1 || wire != `"safe"` || strings.Contains(wire, "private") ||
		strings.Contains(wire, "internal-report") {
		t.Fatalf("wire=%s effects=%d err=%v", wire, effects, err)
	}
}

//nolint:cyclop,gocognit,gocyclo // Outcome matrix asserts distinct real boundary failures and observable effects.
func TestE2EAuthorizedRestoreAndProvenanceRecipe(t *testing.T) {
	for _, mode := range []string{"allow", "recipient", "session", "expired", "destination", "missing-facts", "summary-claim", "nested-claim"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange.
			now := time.Unix(100, 0)
			expires := now.Add(time.Minute)
			vault := ext.NewInMemoryTokenVault()
			token, err := vault.Store(ext.TokenNamespacePII, "secret")
			if err != nil {
				t.Fatal(err)
			}
			key := g.NewScopeKey[disclosureFacts]("host.disclosure")
			facts := disclosureFacts{Recipient: "owner", Session: "session-a", Trust: "trusted", Authorized: true}
			if mode == "recipient" {
				facts.Recipient = "other"
			}
			if mode == "session" {
				facts.Session = "session-b"
			}
			if mode == "expired" {
				expires = now.Add(-time.Second)
			}
			if mode == "summary-claim" || mode == "nested-claim" {
				facts.Trust = "untrusted"
			}
			lookups, effects, finalChecks := 0, 0, 0
			var wire string
			policy := g.MustPolicyFuncWithScope(
				[]g.ScopeRequirement{key.Requirement()},
				func(_ context.Context, value string, scope g.ExecutionScope) (string, *g.Report, error) {
					trusted, _ := key.Lookup(scope)
					if trusted.Recipient != "owner" || trusted.Session != "session-a" || trusted.Trust != "trusted" ||
						!trusted.Authorized ||
						!now.Before(expires) {
						return value, &g.Report{Action: g.ActionBlock}, nil
					}
					return value, nil, nil
				},
			)
			scope := g.ScopeFactory(func(context.Context) (g.ExecutionScope, error) {
				if mode == "missing-facts" {
					return g.NewScope(), nil
				}
				return g.NewScope(g.ScopeValue(key, facts)), nil
			})
			binder, err := d.NewArgsBinder[restoreArgs](
				g.MustNewPipeline[string](),
				g.MustNewPipeline(g.WithPolicyValidators(policy)),
				scope,
			)
			if err != nil {
				t.Fatal(err)
			}
			final := g.MustNewPipeline(
				g.WithPolicyValidators(
					g.MustPolicyFuncWithScope(
						[]g.ScopeRequirement{key.Requirement()},
						func(_ context.Context, value string, scope g.ExecutionScope) (string, *g.Report, error) {
							finalChecks++
							trusted, _ := key.Lookup(scope)
							if mode == "destination" || trusted.Recipient != "owner" ||
								trusted.Session != "session-a" ||
								!trusted.Authorized {
								return value, &g.Report{Action: g.ActionBlock}, nil
							}
							return value, nil, nil
						},
					),
				),
			)
			tool := newTool(
				t,
				binder,
				func(args toolsy.ValidatedArgs[restoreArgs]) (toolsy.ToolResult[string, string], error) {
					effects++
					lookups++
					restored, ok := vault.Restore(args.Value.Token)
					if !ok {
						return toolsy.ToolResult[string, string]{}, toolsy.NewInternalError(
							errors.New("token unavailable"),
						)
					}
					fresh, scopeErr := scope(context.Background())
					if scopeErr != nil {
						return toolsy.ToolResult[string, string]{}, scopeErr
					}
					delivery, outputErr := final.GuardDelivery(
						context.Background(),
						fresh,
						g.NewUserTextPolicy("owner"),
						restored,
					)
					if outputErr != nil {
						return toolsy.ToolResult[string, string]{}, toolsy.NewInternalError(
							&toolsy.ResultContractError{Kind: "restored_delivery", Cause: outputErr},
						)
					}
					projection, ok := delivery.Projection()
					if !ok {
						return toolsy.ToolResult[string, string]{}, errors.New("no projection")
					}
					return toolsy.NewToolResult[string, string](projection.Value), nil
				},
				nil,
			)
			// Act: model-controlled Claim is intentionally ignored by the scope factory.
			err = tool.Execute(
				context.Background(),
				nil,
				toolsy.ToolInput{ArgsJSON: []byte(`{"token":"` + token + `","claim":"trusted"}`)},
				func(chunk toolsy.Chunk) error { wire += string(chunk.Data); return nil },
			)
			// Assert.
			if mode == "allow" {
				if err != nil || wire != `"secret"` || lookups != 1 || finalChecks != 1 {
					t.Fatalf("authorized restore: %v wire=%q", err, wire)
				}
				return
			}
			if err == nil || wire != "" {
				t.Fatalf("restore leaked: %v wire=%q", err, wire)
			}
			if mode == "destination" {
				if effects != 1 || lookups != 1 || finalChecks != 1 {
					t.Fatal("restored output not rechecked")
				}
			} else if effects != 0 || lookups != 0 || finalChecks != 0 {
				t.Fatal("unauthorized lookup occurred")
			}
		})
	}
}
