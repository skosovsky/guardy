package guardy_test

import (
	"context"
	"fmt"
	"strings"

	g "github.com/skosovsky/guardy"
)

// These types belong to the caller. Guardy has no trust taxonomy or provenance store.
type boundaryFacts struct {
	Sources        []string
	ConfirmedTrust string
	Classification string
	Destination    string
	Transformation string
	Contract       string
	PolicyIdentity string
}

type documentClaims struct {
	ClaimedTrust string
	Content      string
}

func ExampleScopeFactory_policyFacts() {
	ctx := context.Background()
	factsKey := g.NewScopeKey[boundaryFacts]("policy.facts")
	policy := g.NewPolicyFuncWithScope(
		[]g.ScopeRequirement{factsKey.Requirement()},
		func(_ context.Context, s string, scope g.ExecutionScope) (string, *g.Report, error) {
			facts, _ := factsKey.Lookup(scope)
			if facts.Contract != "source-destination" {
				return s, &g.Report{
					Action:      g.ActionBlock,
					Disposition: g.DispositionSystemFault,
					Code:        "METADATA_CONTRACT_INCOMPATIBLE",
				}, nil
			}
			if len(facts.Sources) == 0 || facts.Destination == "" || facts.PolicyIdentity == "" {
				return s, &g.Report{Action: g.ActionBlock, Code: "MISSING_POLICY_FACT"}, nil
			}
			// Caller explicitly permits redaction for this destination. There is no
			// implicit declassification and validation does not authorize an action.
			if facts.Destination == "external" && facts.Classification == "private" {
				return strings.ReplaceAll(s, "secret", "[redacted]"), &g.Report{Action: g.ActionRedact}, nil
			}
			return s, &g.Report{Action: g.ActionPass}, nil
		},
	)
	p := g.NewPipeline(g.WithPolicyValidators(policy))
	claims := documentClaims{ClaimedTrust: "trusted", Content: "summary: secret"}
	// Projection comes from host evidence, never from documentClaims. Summarizing
	// changes transformation identity, but keeps the source and confirmed trust.
	facts := boundaryFacts{
		Sources:        []string{"source:document"},
		ConfirmedTrust: "untrusted",
		Classification: "private",
		Destination:    "external",
		Transformation: "summary",
		Contract:       "source-destination",
		PolicyIdentity: "host-policy",
	}
	factory := g.ScopeFactory(
		func(context.Context) (g.ExecutionScope, error) { return g.NewScope(g.ScopeValue(factsKey, facts)), nil },
	)
	scope, _ := factory(ctx)
	delivery, err := p.GuardDelivery(ctx, scope, g.NewUserTextPolicy("external"), claims.Content)
	projection, allowed := delivery.Projection()
	fmt.Println(projection.Value, allowed, err == nil)
	fmt.Println(facts.Sources[0], facts.ConfirmedTrust)
	fmt.Println(claims.ClaimedTrust != facts.ConfirmedTrust)
	// Each additional consumer needs its own destination facts and policy check.
	facts.Destination = ""
	scope, _ = factory(ctx)
	blocked, err := p.GuardDelivery(ctx, scope, g.NewUserTextPolicy("external"), claims.Content)
	fmt.Println(blocked.Deliverable, err != nil)
	// Output:
	// summary: [redacted] true true
	// source:document untrusted
	// true
	// false true
}
