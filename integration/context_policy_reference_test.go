package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	g "github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
	"github.com/skosovsky/guardy/ext/jsonredact"
	"github.com/skosovsky/guardy/ext/jsonschema"
)

// Everything below is a single-threaded, in-memory caller harness. Guardy neither
// assigns these facts nor owns authentication, approval, policy versioning or sinks.
// A concurrent production host must bind authorization and execution atomically.
type documentAPIArgs struct {
	Text      string `json:"text"`
	Recipient string `json:"recipient"`
}

type documentAPIFacts struct {
	Sources                                                                                   []string
	Integrity, Confidentiality, Transformation, Principal, Policy, Configuration, Destination string
	Authorized                                                                                bool
}

type documentAPIApproval struct {
	Digest                                        [32]byte
	Principal, Policy, Configuration, Destination string
}

type documentAPIHost struct {
	facts         documentAPIFacts
	configuration string
	adapters      map[string]string
	permissions   map[string]bool
	typed         *g.ArgsPipeline[documentAPIArgs]
	dynamic       *g.JSONArgsPipeline
	consumers     *g.Pipeline[string]
	handlerCalls  int
	scopeCalls    int
	sinks         map[string][]byte
}

func documentAPIFactsKey() g.ScopeKey[documentAPIFacts] {
	return g.NewScopeKey[documentAPIFacts]("sample.document-api-facts")
}

var errDocumentAPIGate = errors.New("sample host: action binding changed or authorization revoked")

func makeDocumentAPIHost() (*documentAPIHost, error) {
	schema, err := jsonschema.NewJSONSchemaValidator(
		`{"type":"object","required":["text","recipient"],"additionalProperties":false,"properties":{"text":{"type":"string","minLength":1},"recipient":{"type":"string","enum":["owner"]}}}`,
	)
	if err != nil {
		return nil, err
	}
	host := &documentAPIHost{
		facts: documentAPIFacts{
			Sources:         []string{"src:document"},
			Integrity:       "untrusted",
			Confidentiality: "private",
			Transformation:  "raw",
			Principal:       "owner",
			Policy:          "policy:1",
		},
		permissions: map[string]bool{
			"arguments":   true,
			"context":     true,
			"persistence": true,
			"export":      false,
			"delivery":    true,
		},
		sinks:         make(map[string][]byte),
		configuration: "config:1",
		adapters: map[string]string{
			"context":     "context",
			"persistence": "persistence",
			"export":      "export",
			"delivery":    "delivery",
		},
	}
	policy := g.MustPolicyFuncWithScope(
		[]g.ScopeRequirement{documentAPIFactsKey().Requirement()},
		func(_ context.Context, value string, scope g.ExecutionScope) (string, *g.Report, error) {
			facts, _ := documentAPIFactsKey().Lookup(scope)
			if !documentAPISourcesKnown(facts.Sources) || facts.Principal != "owner" ||
				facts.Policy == "" ||
				facts.Destination == "" ||
				(facts.Integrity != "trusted" && facts.Integrity != "untrusted") ||
				(facts.Confidentiality != "private" && facts.Confidentiality != "public") {
				return value, &g.Report{Action: g.ActionBlock, Code: "HOST_FACTS_UNKNOWN"}, nil
			}
			if !facts.Authorized {
				return value, &g.Report{Action: g.ActionBlock, Code: "HOST_DESTINATION_DENIED"}, nil
			}
			if facts.Confidentiality == "private" &&
				(facts.Destination == "delivery" || facts.Destination == "export") &&
				strings.Contains(value, "secret") {
				return value, &g.Report{Action: g.ActionBlock, Code: "HOST_SECRET_DENIED"}, nil
			}
			return value, nil, nil
		},
	)
	raw := g.MustNewPipeline(
		g.WithSequential(schema, jsonredact.NewJSONRedactValidator(ext.MustPIIValidator(), "json-pii")),
	)
	final := g.MustNewPipeline(g.WithSequential(schema), g.WithPolicyValidators(policy))
	host.typed = g.MustCompileArgs[documentAPIArgs](
		raw,
		g.WithArgsFinalGuard[documentAPIArgs](final),
		g.WithArgsConfigurationID[documentAPIArgs]("sample-args:1"),
	)
	host.dynamic = g.MustCompileJSONArgs(
		raw,
		nil,
		g.WithJSONArgsFinalGuard(final),
		g.WithJSONArgsConfigurationID("sample-args:1"),
	)
	host.consumers = g.MustNewPipeline(g.WithPolicyValidators(policy))
	return host, nil
}

func newDocumentAPIHost(t *testing.T) *documentAPIHost {
	t.Helper()
	host, err := makeDocumentAPIHost()
	if err != nil {
		t.Fatal(err)
	}
	return host
}

// Called for every attempt, resumed action and destination. Unknown destinations
// remain unauthorized. Claims in the document never populate this projection.
func (h *documentAPIHost) scope(destination string) g.ScopeFactory {
	return func(context.Context) (g.ExecutionScope, error) {
		h.scopeCalls++
		facts := h.facts
		facts.Sources = append([]string(nil), h.facts.Sources...)
		facts.Destination = destination
		facts.Authorized = h.permissions[destination]
		return g.NewScope(g.ScopeValue(documentAPIFactsKey(), facts)), nil
	}
}

// Host lineage survives summary, subagent output or memory serialization. The
// content producer has no authority to lower confidentiality or upgrade integrity.
func (h *documentAPIHost) transform(kind, content string) string {
	h.facts.Transformation = kind
	h.facts.Sources = append([]string(nil), h.facts.Sources...)
	return kind + ": " + content
}

func (h *documentAPIHost) canonical(ctx context.Context, raw string, dynamic bool) (string, error) {
	scope, err := h.scope("arguments")(ctx)
	if err != nil {
		return "", err
	}
	if dynamic {
		value, validateErr := h.dynamic.Validate(ctx, scope, raw)
		return value.SanitizedRaw, validateErr
	}
	value, err := h.typed.Validate(ctx, scope, raw)
	return value.SanitizedRaw, err
}

func (h *documentAPIHost) propose(ctx context.Context, raw string, dynamic bool) (documentAPIApproval, string, error) {
	if err := h.configure(); err != nil {
		return documentAPIApproval{}, "", err
	}
	canonical, err := h.canonical(ctx, raw, dynamic)
	if err != nil {
		return documentAPIApproval{}, "", err
	}
	return documentAPIApproval{
		Digest:        sha256.Sum256([]byte(canonical)),
		Principal:     h.facts.Principal,
		Policy:        h.facts.Policy,
		Configuration: h.configuration,
		Destination:   "owner",
	}, canonical, nil
}

// This hook is deliberately host-owned. ConfigurationID is only pipeline
// metadata: the gate separately checks authenticated identity, current policy,
// exact canonical arguments and recipient, after revalidation with fresh facts.
func (h *documentAPIHost) execute(
	ctx context.Context,
	approval documentAPIApproval,
	raw string,
	dynamic bool,
) (string, error) {
	if err := h.configure(); err != nil {
		return "", err
	}
	canonical, err := h.canonical(ctx, raw, dynamic)
	if err != nil {
		return "", err
	}
	var args documentAPIArgs
	if err = json.Unmarshal([]byte(canonical), &args); err != nil {
		return "", err
	}
	if approval.Configuration != h.configuration || approval.Principal != h.facts.Principal ||
		approval.Policy != h.facts.Policy ||
		approval.Destination != args.Recipient ||
		approval.Digest != sha256.Sum256([]byte(canonical)) ||
		!h.permissions["arguments"] {
		return "", errDocumentAPIGate
	}
	h.handlerCalls++
	return args.Text, nil
}

func (h *documentAPIHost) consume(ctx context.Context, destination, value string) error {
	boundary := g.Boundary(destination)
	switch boundary {
	case g.BoundaryContext, g.BoundaryPersistence, g.BoundaryExport, g.BoundaryDelivery:
		if h.adapters[destination] != destination {
			return &g.BoundaryConfigurationError{Boundary: boundary, Code: "unsupported_mandatory_boundary"}
		}
	case g.BoundaryInput, g.BoundaryArgs, g.BoundaryResult:
		fallthrough
	default:
		// Empty delivery destinations fail configuration; other unknown facts reach the mandatory policy.
	}
	scope, err := h.scope(destination)(ctx)
	if err != nil {
		return err
	}
	delivery, err := h.consumers.GuardDelivery(ctx, scope, g.NewUserTextPolicy(destination), value)
	if err != nil {
		return err
	}
	projection, ok := delivery.Projection()
	if !ok {
		return errors.New("sample host: delivery projection unavailable")
	}
	bytes, err := json.Marshal(projection)
	if err != nil {
		return err
	}
	h.sinks[destination] = bytes
	return nil
}

// This profile is derived from actual registered adapters, not requested coverage.
func (h *documentAPIHost) configure() error {
	supported := []g.Boundary{g.BoundaryArgs}
	for boundary, destination := range h.adapters {
		if boundary != destination {
			return &g.BoundaryConfigurationError{Boundary: g.Boundary(boundary), Code: "incompatible_boundary"}
		}
		supported = append(supported, g.Boundary(boundary))
	}
	_, err := g.CompileBoundaryProfile(
		h.configuration,
		supported,
		[]g.Boundary{g.BoundaryArgs, g.BoundaryContext, g.BoundaryPersistence, g.BoundaryExport, g.BoundaryDelivery},
	)
	return err
}

func Example_documentAPIReference() {
	host, err := makeDocumentAPIHost()
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	approval, canonical, err := host.propose(ctx, `{"text":"Contact alice@example.com","recipient":"owner"}`, false)
	if err != nil {
		panic(err)
	}
	value, err := host.execute(ctx, approval, canonical, false)
	if err != nil {
		panic(err)
	}
	if err = host.consume(ctx, "delivery", value); err != nil {
		panic(err)
	}
	fmt.Println(host.handlerCalls, string(host.sinks["delivery"]))
	// Output:
	// 1 {"value":"Contact [REDACTED]","channel":"delivery","fallback":false}
}

func documentAPISourcesKnown(sources []string) bool {
	if len(sources) == 0 {
		return false
	}
	for _, source := range sources {
		switch source {
		case "src:document", "src:attachment":
		default:
			return false
		}
	}
	return true
}
