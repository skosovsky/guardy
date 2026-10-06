package guardy_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	g "github.com/skosovsky/guardy"
)

// All recipe types and trust rules belong to the host, not guardy's core.
type recipeFacts struct {
	Sources                                                                         []string
	ConfirmedTrust, Classification, Destination, Transformation, Contract, Identity string
	AllowNoProvenance, Declassified                                                 bool
}

type recipeMetadataError struct{ category string }

func (e *recipeMetadataError) Error() string { return "host metadata: " + e.category }

type recipeArguments struct {
	Text string `json:"text"`
}

type recipeEvidence struct {
	Code    string   `json:"code"`
	Sources []string `json:"sources"`
}

type recipeResult struct {
	Canonical  string
	Projection g.DeliveryProjection[string]
	Evidence   recipeEvidence
	Facts      recipeFacts
	Calls      int
}

func recipeKey() g.ScopeKey[recipeFacts] {
	return g.NewScopeKey[recipeFacts]("host.source-destination")
}

func checkRecipeFacts(facts recipeFacts) error {
	if facts.Contract != "source-destination" {
		return &recipeMetadataError{category: "incompatible_contract"}
	}
	if facts.Destination == "" {
		return &recipeMetadataError{category: "missing_destination"}
	}
	switch facts.Destination {
	case "context", "arguments", "external":
	default:
		return &recipeMetadataError{category: "unknown_destination"}
	}
	if facts.Classification == "" {
		return &recipeMetadataError{category: "missing_classification"}
	}
	if facts.Classification != "private" && facts.Classification != "public" {
		return &recipeMetadataError{category: "unknown_classification"}
	}
	if len(facts.Sources) == 0 && !facts.AllowNoProvenance {
		return &recipeMetadataError{category: "missing_source"}
	}
	if facts.Identity == "" || facts.ConfirmedTrust == "" {
		return &recipeMetadataError{category: "missing_policy_fact"}
	}
	if facts.ConfirmedTrust != "untrusted" && facts.ConfirmedTrust != "trusted" {
		return &recipeMetadataError{category: "unknown_trust"}
	}
	// References are opaque host-assigned IDs, never snippets or payload claims.
	for _, source := range facts.Sources {
		if !strings.HasPrefix(source, "src:") || len(source) > 64 || strings.ContainsAny(source, " \n\r\t") {
			return &recipeMetadataError{category: "invalid_source_reference"}
		}
		switch source {
		case "src:document", "src:attachment", "src:third":
		default:
			return &recipeMetadataError{category: "unknown_source"}
		}
	}
	return nil
}

func recipePipeline(identity string) *g.Pipeline[string] {
	key := recipeKey()
	rule := g.MustPolicyFuncWithScope([]g.ScopeRequirement{key.Requirement()},
		func(_ context.Context, value string, scope g.ExecutionScope) (string, *g.Report, error) {
			facts, _ := key.Lookup(scope)
			if err := checkRecipeFacts(facts); err != nil {
				return value, nil, err
			}
			if strings.Contains(value, "forbidden") {
				return value, &g.Report{
					Action:          g.ActionBlock,
					Code:            "HOST_CONTENT_DENY",
					SafeUserMessage: "Content is not permitted",
				}, nil
			}
			if facts.Destination == "external" && facts.Classification == "private" && !facts.Declassified {
				return strings.ReplaceAll(
					value,
					"secret",
					"[redacted]",
				), &g.Report{
					Action: g.ActionRedact,
					Code:   "HOST_REDACT",
				}, nil
			}
			return value, nil, nil
		})
	return g.MustNewPipeline(g.WithPolicyValidators(rule), g.WithPipelineName[string](identity))
}

func recipeScope(facts recipeFacts, destination string) g.ScopeFactory {
	// Copy host-owned mutable references when projecting into an invocation.
	facts.Sources = append([]string(nil), facts.Sources...)
	facts.Destination = destination
	return func(context.Context) (g.ExecutionScope, error) {
		current := facts
		current.Sources = append([]string(nil), facts.Sources...)
		return g.NewScope(g.ScopeValue(recipeKey(), current)), nil
	}
}

func executeRecipe(ctx context.Context, claims documentClaims, facts recipeFacts) (recipeResult, error) {
	var result recipeResult
	facts.Sources = append([]string(nil), facts.Sources...)
	facts.Transformation = "summary"
	result.Facts = facts
	if err := checkRecipeFacts(facts); err != nil {
		// Use the same scoped policy boundary for typed/canonical failures before
		// any handler, without turning missing destination into an implicit default.
		scope, _ := recipeScope(facts, facts.Destination)(ctx)
		_, err = recipePipeline(facts.Identity).Run(ctx, scope, claims.Content)
		return result, err
	}
	pipeline := recipePipeline(facts.Identity)
	contextScope, err := recipeScope(facts, "context")(ctx)
	if err != nil {
		return result, err
	}
	// Transformation does not copy claimed trust into confirmed facts.
	transformed, err := pipeline.GuardDelivery(
		ctx,
		contextScope,
		g.NewUserTextPolicy("context"),
		"summary: "+strings.TrimSpace(claims.Content),
	)
	if err != nil {
		return result, err
	}
	raw, err := json.Marshal(recipeArguments{Text: transformed.Value})
	if err != nil {
		return result, err
	}
	args := g.MustCompileArgs[recipeArguments](pipeline,
		g.WithArgsFinalGuard[recipeArguments](pipeline),
		g.WithArgsConfigurationID[recipeArguments](facts.Identity))
	handler := g.WrapArgs(
		args,
		recipeScope(facts, "arguments"),
		func(_ context.Context, value recipeArguments) (string, error) {
			result.Calls++
			return value.Text, nil
		},
	)
	value, boundary, err := handler(ctx, string(raw))
	result.Canonical = boundary.SanitizedRaw
	if err != nil {
		return result, err
	}
	deliveryScope, err := recipeScope(facts, facts.Destination)(ctx)
	if err != nil {
		return result, err
	}
	delivery, err := pipeline.GuardDelivery(ctx, deliveryScope, g.NewUserTextPolicy(facts.Destination), value)
	if err != nil {
		return result, err
	}
	var allowed bool
	result.Projection, allowed = delivery.Projection()
	if !allowed {
		return result, errors.New("host: unavailable delivery projection")
	}
	// Evidence is bounded; full references survive in host facts through every
	// phase. No findings or logs contain the original secret-bearing value.
	result.Evidence.Code = "HOST_POLICY_CHECKED"
	result.Evidence.Sources = append([]string(nil), facts.Sources[:min(2, len(facts.Sources))]...)
	return result, nil
}

func defaultRecipeFacts() recipeFacts {
	return recipeFacts{
		Sources:        []string{"src:document", "src:attachment", "src:third"},
		ConfirmedTrust: "untrusted", Classification: "private", Destination: "external",
		Transformation: "raw", Contract: "source-destination", Identity: "host-policy",
		AllowNoProvenance: false, Declassified: false,
	}
}

func ExampleScopeKey_sourceTransformArgumentsDestination() {
	result, err := executeRecipe(
		context.Background(),
		documentClaims{ClaimedTrust: "trusted", Content: "secret"},
		defaultRecipeFacts(),
	)
	fmt.Println(err == nil, result.Calls, result.Projection.Value)
	fmt.Println(result.Facts.ConfirmedTrust, result.Facts.Transformation, len(result.Facts.Sources))
	fmt.Println(result.Evidence.Code, result.Evidence.Sources)
	// Output:
	// true 1 summary: [redacted]
	// untrusted summary 3
	// HOST_POLICY_CHECKED [src:document src:attachment]
}

func TestPolicyRecipeReferencesCanonicalAndPrivacy(t *testing.T) {
	// Arrange.
	facts := defaultRecipeFacts()
	claims := documentClaims{ClaimedTrust: "trusted", Content: "secret"}
	// Act.
	result, err := executeRecipe(context.Background(), claims, facts)
	projection, marshalErr := json.Marshal(result.Projection)
	evidence, evidenceErr := json.Marshal(result.Evidence)
	// Assert.
	if err != nil || marshalErr != nil || evidenceErr != nil {
		t.Fatalf("%v %v %v", err, marshalErr, evidenceErr)
	}
	if result.Facts.ConfirmedTrust != "untrusted" || len(result.Facts.Sources) != len(facts.Sources) ||
		result.Calls != 1 {
		t.Fatalf("%+v", result)
	}
	if result.Canonical != `{"text":"summary: secret"}` || result.Projection.Value != "summary: [redacted]" ||
		len(result.Evidence.Sources) != 2 {
		t.Fatalf("%+v", result)
	}
	if strings.Contains(string(projection), "secret") || strings.Contains(string(evidence), "secret") {
		t.Fatalf("consumer/evidence leak: %s %s", projection, evidence)
	}
	for i, ref := range facts.Sources {
		if result.Facts.Sources[i] != ref {
			t.Fatal("transformation lost reference")
		}
	}
}

func TestPolicyRecipeTypedMetadataFailures(t *testing.T) {
	for _, category := range []string{"missing_source", "missing_destination", "incompatible_contract", "missing_policy_fact", "invalid_source_reference", "unknown_source", "unknown_trust", "missing_classification", "unknown_classification", "unknown_destination"} {
		t.Run(category, func(t *testing.T) {
			// Arrange.
			facts := defaultRecipeFacts()
			switch category {
			case "missing_source":
				facts.Sources = nil
			case "missing_destination":
				facts.Destination = ""
			case "incompatible_contract":
				facts.Contract = "other-contract"
			case "missing_policy_fact":
				facts.ConfirmedTrust = ""
			case "invalid_source_reference":
				facts.Sources = []string{"secret payload"}
			case "unknown_source":
				facts.Sources = []string{"src:unknown"}
			case "unknown_trust":
				facts.ConfirmedTrust = "unclassified"
			case "missing_classification":
				facts.Classification = ""
			case "unknown_classification":
				facts.Classification = "unknown-category"
			case "unknown_destination":
				facts.Destination = "exteranl"
			}
			// Act.
			result, err := executeRecipe(
				context.Background(),
				documentClaims{ClaimedTrust: "trusted", Content: "secret"},
				facts,
			)
			// Assert.
			var metadata *recipeMetadataError
			var failure *g.PolicyFailure
			if !errors.As(err, &metadata) || metadata.category != category || !errors.As(err, &failure) ||
				!failure.Decision.IsSystemFault() ||
				result.Projection.Value != "" ||
				result.Calls != 0 {
				t.Fatalf("%+v %v", result, err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(failure.Decision.SafeMessage, "secret") {
				t.Fatal("metadata diagnostic leaked payload")
			}
		})
	}
}

func TestPolicyRecipeRejectionIsNotMetadataFault(t *testing.T) {
	// Arrange / Act.
	result, err := executeRecipe(
		context.Background(),
		documentClaims{ClaimedTrust: "trusted", Content: "forbidden secret"},
		defaultRecipeFacts(),
	)
	// Assert.
	var failure *g.PolicyFailure
	var metadata *recipeMetadataError
	if !errors.As(err, &failure) || errors.As(err, &metadata) || !failure.Decision.IsTerminal() || result.Calls != 0 ||
		result.Projection.Value != "" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestPolicyRecipeExplicitHostModes(t *testing.T) {
	for _, mode := range []string{"provenance_required", "no_provenance", "declassified"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange.
			facts := defaultRecipeFacts()
			if mode != "declassified" {
				facts.Sources = nil
			}
			facts.AllowNoProvenance = mode == "no_provenance"
			facts.Declassified = mode == "declassified"
			// Act.
			result, err := executeRecipe(
				context.Background(),
				documentClaims{ClaimedTrust: "trusted", Content: "secret"},
				facts,
			)
			// Assert: claims never enable either host policy.
			if mode == "provenance_required" {
				if err == nil || result.Calls != 0 {
					t.Fatalf("%+v %v", result, err)
				}
				return
			}
			if err != nil || result.Facts.ConfirmedTrust != "untrusted" {
				t.Fatalf("%+v %v", result, err)
			}
			if mode == "no_provenance" &&
				(result.Projection.Value != "summary: [redacted]" || len(result.Evidence.Sources) != 0) {
				t.Fatalf("%+v", result)
			}
			if mode == "declassified" && result.Projection.Value != "summary: secret" {
				t.Fatalf("%+v", result)
			}
		})
	}
}
