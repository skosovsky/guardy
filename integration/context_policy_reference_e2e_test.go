//go:build e2e

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	g "github.com/skosovsky/guardy"
)

func TestE2EDocumentAPIReferenceBenignTypedAndDynamic(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		t.Run(
			strconv.FormatBool(dynamic),
			func(t *testing.T) { checkE2EDocumentAPIReferenceBenignTypedAndDynamic(t, dynamic) },
		)
	}
}

func TestE2EDocumentAPIReferenceResumeBindingCorpus(t *testing.T) {
	for _, scenario := range []string{"recipient", "arguments", "identity", "policy", "configuration", "revoked"} {
		t.Run(scenario, func(t *testing.T) { checkE2EDocumentAPIReferenceResumeBindingCorpus(t, scenario) })
	}
}

func TestE2EDocumentAPIReferenceClaimsTransformsAndIndependentConsumers(t *testing.T) {
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

func TestE2EDocumentAPIReferenceUnknownFactsAndReplacementBytes(t *testing.T) {
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

func TestE2EDocumentAPIReferenceFinalSchemaAfterRedaction(t *testing.T) {
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

func TestE2EDocumentAPIReferenceMandatoryAdapters(t *testing.T) {
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

func TestE2EDocumentAPIReferenceFaultAndExhaustedBudget(t *testing.T) {
	for _, fault := range []bool{false, true} {
		t.Run(
			strconv.FormatBool(fault),
			func(t *testing.T) { checkE2EDocumentAPIReferenceFaultAndExhaustedBudget(t, fault) },
		)
	}
}

func TestE2EDocumentAPIReferenceBoundedEvidence(t *testing.T) {
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

func TestE2EDocumentAPIReferenceAdapterRemovalOrMismatchDuringPause(t *testing.T) {
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

func checkE2EDocumentAPIReferenceBenignTypedAndDynamic(t *testing.T, dynamic bool) {
	t.Helper()
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
}

func checkE2EDocumentAPIReferenceResumeBindingCorpus(t *testing.T, scenario string) {
	t.Helper()
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
}

func checkE2EDocumentAPIReferenceFaultAndExhaustedBudget(t *testing.T, fault bool) {
	t.Helper()
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
	host.typed = g.MustCompileArgs[documentAPIArgs](g.MustNewPipeline(g.WithSequential(detector)))
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
}
