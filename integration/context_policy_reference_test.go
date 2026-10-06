package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	policy := g.NewPolicyFuncWithScope(
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
	raw := g.NewPipeline(g.WithFastPath(schema, jsonredact.NewJSONRedactValidator(ext.NewPIIValidator(), "json-pii")))
	final := g.NewPipeline(g.WithFastPath(schema), g.WithPolicyValidators(policy))
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
	host.consumers = g.NewPipeline(g.WithPolicyValidators(policy))
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

func TestDocumentAPIReferenceBenignTypedAndDynamic(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		t.Run(strconv.FormatBool(dynamic), func(t *testing.T) {
			// Arrange: ordinary document/API flow, with no model or agent runtime.
			host := newDocumentAPIHost(t)
			ctx := context.Background()
			raw := `{"text":"Contact alice@example.com","recipient":"owner"}`
			// Act.
			approval, canonical, err := host.propose(ctx, raw, dynamic)
			if err != nil {
				t.Fatal(err)
			}
			value, err := host.execute(ctx, approval, canonical, dynamic)
			if err != nil {
				t.Fatal(err)
			}
			for _, destination := range []string{"context", "persistence", "delivery"} {
				if err = host.consume(ctx, destination, value); err != nil {
					t.Fatal(err)
				}
			}
			// Assert: actual handler and independently serialized consumer bytes.
			if host.handlerCalls != 1 || host.scopeCalls != 5 || strings.Contains(canonical, "alice@") ||
				value != "Contact [REDACTED]" {
				t.Fatalf("host=%+v canonical=%s value=%s", host, canonical, value)
			}
			for _, destination := range []string{"context", "persistence", "delivery"} {
				var projection g.DeliveryProjection[string]
				if err = json.Unmarshal(host.sinks[destination], &projection); err != nil {
					t.Fatal(err)
				}
				if projection.Value != value || projection.Channel != destination {
					t.Fatalf("%s: %+v", destination, projection)
				}
			}
		})
	}
}

func TestDocumentAPIReferenceResumeBindingCorpus(t *testing.T) {
	for _, scenario := range []string{"recipient", "arguments", "identity", "policy", "configuration", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange: a proposal is paused before any handler execution.
			host := newDocumentAPIHost(t)
			ctx := context.Background()
			raw := `{"text":"ordinary","recipient":"owner"}`
			approval, canonical, err := host.propose(ctx, raw, false)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "recipient":
				canonical = `{"text":"ordinary","recipient":"outsider"}`
			case "arguments":
				canonical = `{"text":"changed","recipient":"owner"}`
			case "identity":
				host.facts.Principal = "other"
			case "policy":
				host.facts.Policy = "policy:2"
			case "configuration":
				host.configuration = "config:2"
			case "revoked":
				host.permissions["arguments"] = false
			}
			// Act: resume always rebuilds facts and reruns schema/policy.
			value, err := host.execute(ctx, approval, canonical, false)
			// Assert.
			if err == nil || value != "" || host.handlerCalls != 0 || host.scopeCalls != 2 || len(host.sinks) != 0 {
				t.Fatalf("host=%+v value=%q err=%v", host, value, err)
			}
			if scenario == "arguments" || scenario == "policy" || scenario == "configuration" {
				if !errors.Is(err, errDocumentAPIGate) {
					t.Fatalf("expected host binding error: %v", err)
				}
			} else {
				if _, ok := errors.AsType[*g.PolicyFailure](err); !ok {
					t.Fatalf("expected schema/policy failure: %v", err)
				}
			}
		})
	}
}

func TestDocumentAPIReferenceClaimsTransformsAndIndependentConsumers(t *testing.T) {
	for _, kind := range []string{"summary", "subagent-response", "memory-record"} {
		t.Run(kind, func(t *testing.T) {
			// Arrange: external content's self-declared trust is merely payload.
			host := newDocumentAPIHost(t)
			claims := documentClaims{ClaimedTrust: "trusted", Content: "ordinary"}
			value := host.transform(kind, claims.Content)
			// Act.
			contextErr := host.consume(context.Background(), "context", value)
			memoryErr := host.consume(context.Background(), "persistence", value)
			exportErr := host.consume(context.Background(), "export", value)
			deliveryErr := host.consume(context.Background(), "delivery", value)
			// Assert.
			if contextErr != nil || memoryErr != nil || deliveryErr != nil || exportErr == nil ||
				host.sinks["export"] != nil ||
				len(host.sinks) != 3 ||
				host.facts.Integrity != "untrusted" ||
				host.facts.Confidentiality != "private" ||
				host.facts.Sources[0] != "src:document" ||
				host.facts.Transformation != kind {
				t.Fatalf("host=%+v errors=%v/%v/%v/%v", host, contextErr, memoryErr, exportErr, deliveryErr)
			}
		})
	}
}

func TestDocumentAPIReferenceUnknownFactsAndReplacementBytes(t *testing.T) {
	for _, scenario := range []string{"source", "unknown-source", "unknown-secondary-source", "integrity", "confidentiality", "destination", "fallback", "restored-secret", "trusted-secret"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange.
			host := newDocumentAPIHost(t)
			destination, value := "delivery", "ordinary"
			switch scenario {
			case "source":
				host.facts.Sources = nil
			case "unknown-source":
				host.facts.Sources = []string{"src:unknown"}
			case "unknown-secondary-source":
				host.facts.Sources = []string{"src:document", "raw secret"}
			case "integrity":
				host.facts.Integrity = ""
			case "confidentiality":
				host.facts.Confidentiality = "unknown"
			case "destination":
				destination = ""
			case "fallback":
				value = "fallback contains secret"
			case "restored-secret":
				value = strings.ReplaceAll("restored [TOKEN]", "[TOKEN]", "secret")
			case "trusted-secret":
				host.facts.Integrity = "trusted"
				value = "secret"
			}
			// Act: fallback/restored bytes enter the same final destination check.
			err := host.consume(context.Background(), destination, value)
			// Assert: trust/integrity does not declassify a secret.
			var failure *g.PolicyFailure
			if !errors.As(err, &failure) || len(host.sinks) != 0 {
				t.Fatalf("host=%+v err=%v", host, err)
			}
			if scenario == "destination" {
				if !failure.Decision.IsSystemFault() || !errors.Is(err, g.ErrConfiguration) {
					t.Fatalf("missing delivery destination must fail configuration: %v", err)
				}
			} else if !failure.Decision.IsTerminal() {
				t.Fatalf("host policy rejection must deny: %v", err)
			}
		})
	}
}

func TestDocumentAPIReferenceFinalSchemaAfterRedaction(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		t.Run(strconv.FormatBool(dynamic), func(t *testing.T) {
			// Arrange: redaction is allowed, but cannot make an invalid recipient valid.
			host := newDocumentAPIHost(t)
			raw := `{"text":"alice@example.com","recipient":"outsider"}`
			// Act.
			_, canonical, err := host.propose(context.Background(), raw, dynamic)
			// Assert.
			var failure *g.PolicyFailure
			if !errors.As(err, &failure) || !failure.Decision.IsRetryable() || host.handlerCalls != 0 ||
				canonical != "" ||
				len(host.sinks) != 0 {
				t.Fatalf("host=%+v canonical=%q err=%v", host, canonical, err)
			}
		})
	}
}

func TestDocumentAPIReferenceMandatoryAdapters(t *testing.T) {
	for _, missing := range []g.Boundary{g.BoundaryPersistence, g.BoundaryExport} {
		t.Run(string(missing), func(t *testing.T) {
			// Arrange: only registered sink adapters may claim coverage.
			host := newDocumentAPIHost(t)
			delete(host.adapters, string(missing))
			// Act.
			_, _, err := host.propose(context.Background(), `{"text":"ordinary","recipient":"owner"}`, false)
			// Assert: fail configuration before any handler/remote sink.
			var configuration *g.BoundaryConfigurationError
			if !errors.As(err, &configuration) || configuration.Boundary != missing || host.handlerCalls != 0 ||
				len(host.sinks) != 0 {
				t.Fatalf("host=%+v err=%v", host, err)
			}
		})
	}
}

func TestDocumentAPIReferenceFaultAndExhaustedBudget(t *testing.T) {
	for _, fault := range []bool{false, true} {
		t.Run(strconv.FormatBool(fault), func(t *testing.T) {
			// Arrange: host owns a finite attempt budget; Guardy returns categories only.
			host := newDocumentAPIHost(t)
			calls := 0
			detectorErr := errors.New("third-party detector failed")
			detector := g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
				calls++
				if fault {
					return value, nil, detectorErr
				}
				return value, &g.Report{Action: g.ActionRetry, Retryable: true, Code: "HOST_CORRECTION"}, nil
			})
			host.typed = g.MustCompileArgs[documentAPIArgs](g.NewPipeline(g.WithFastPath(detector)))
			var err error
			// Act: retries stop on fault and never bypass validation into execution.
			for range 2 {
				_, _, err = host.propose(context.Background(), `{"text":"ordinary","recipient":"owner"}`, false)
				var failure *g.PolicyFailure
				if !errors.As(err, &failure) || !failure.Decision.IsRetryable() {
					break
				}
			}
			// Assert.
			var failure *g.PolicyFailure
			if !errors.As(err, &failure) || host.handlerCalls != 0 || len(host.sinks) != 0 {
				t.Fatalf("host=%+v err=%v", host, err)
			}
			if fault {
				if calls != 1 || !failure.Decision.IsSystemFault() || !errors.Is(err, detectorErr) {
					t.Fatalf("calls=%d err=%v", calls, err)
				}
			} else if calls != 2 || !failure.Decision.IsRetryable() {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
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

func TestDocumentAPIReferenceBoundedEvidence(t *testing.T) {
	// Arrange: raw content and third-party diagnostic text are never log fields.
	host := newDocumentAPIHost(t)
	raw := `{"text":"Contact alice@example.com","recipient":"owner"}`
	// Act.
	approval, canonical, err := host.propose(context.Background(), raw, false)
	if err != nil {
		t.Fatal(err)
	}
	value, err := host.execute(context.Background(), approval, canonical, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = host.consume(context.Background(), "delivery", value); err != nil {
		t.Fatal(err)
	}
	evidence := struct {
		Code    string   `json:"code"`
		Sources []string `json:"sources"`
		Policy  string   `json:"policy"`
	}{Code: "HOST_CHECKED", Sources: append([]string(nil), host.facts.Sources[:min(2, len(host.facts.Sources))]...), Policy: "policy:1"}
	bytes, err := json.Marshal(evidence)
	// Assert: only bounded known refs, allowed metadata and consumer projection.
	if err != nil || string(bytes) != `{"code":"HOST_CHECKED","sources":["src:document"],"policy":"policy:1"}` ||
		strings.Contains(string(host.sinks["delivery"]), "alice@") {
		t.Fatalf("evidence=%s sinks=%v err=%v", bytes, host.sinks, err)
	}
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

func TestDocumentAPIReferenceAdapterRemovalOrMismatchDuringPause(t *testing.T) {
	for _, scenario := range []string{"removed", "mismapped", "mismapped-proposal"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange: adapters are real host registrations, including after a pause.
			host := newDocumentAPIHost(t)
			ctx := context.Background()
			raw := `{"text":"ordinary","recipient":"owner"}`
			var approval documentAPIApproval
			canonical := raw
			if scenario != "mismapped-proposal" {
				var err error
				approval, canonical, err = host.propose(ctx, raw, false)
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "removed" {
				delete(host.adapters, "persistence")
			} else {
				host.adapters["persistence"] = "wrong"
			}
			// Act: configuration is checked before proposal and again before execution.
			var err error
			if scenario == "mismapped-proposal" {
				_, _, err = host.propose(ctx, raw, false)
			} else {
				_, err = host.execute(ctx, approval, canonical, false)
			}
			// Assert: a declaration cannot substitute for a valid destination adapter.
			var configuration *g.BoundaryConfigurationError
			if !errors.As(err, &configuration) || configuration.Boundary != g.BoundaryPersistence ||
				host.handlerCalls != 0 ||
				len(host.sinks) != 0 {
				t.Fatalf("host=%+v err=%v", host, err)
			}
		})
	}
}
