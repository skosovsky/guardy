package guardy

import (
	"bytes"
	"context"
	"errors"
	htmltemplate "html/template"
	"strings"
	"testing"
	texttemplate "text/template"
)

func TestBestEffortEveryUTF8Split(t *testing.T) {
	// Arrange.
	input := "abc€😊Z"
	for split := 0; split <= len(input); split++ {
		cfg := testStreamConfig(NewPipeline[string]())
		cfg.Profile, cfg.MaxUnitBytes = ReleaseBestEffort, 4
		var sink bytes.Buffer
		s, err := CompileStream(&sink, cfg)
		if err != nil {
			t.Fatal(err)
		}
		// Act.
		_, first := s.Write([]byte(input[:split]))
		_, second := s.Write([]byte(input[split:]))
		_, final := s.Complete(context.Background())
		// Assert.
		if first != nil || second != nil || final != nil || sink.String() != input {
			t.Fatalf("split=%d: %v %v %v output=%q", split, first, second, final, sink.String())
		}
	}
}

func TestJSONFramingWhitespaceEverySplit(t *testing.T) {
	// Arrange.
	input := "{}\n [] \t\r\n"
	for split := 0; split <= len(input); split++ {
		cfg := testStreamConfig(NewPipeline[string]())
		cfg.Profile, cfg.JSONValues = ReleaseValidatedUnits, true
		cfg.MaxPendingBytes = cfg.MaxUnitBytes + 1
		cfg.Delivery = NewDeliveryPolicy("internal", WithDeliveryAllowedKinds(PayloadTechnicalPayload))
		var sink bytes.Buffer
		s, err := CompileStream(&sink, cfg)
		if err != nil {
			t.Fatal(err)
		}
		// Act.
		_, first := s.Write([]byte(input[:split]))
		_, second := s.Write([]byte(input[split:]))
		_, final := s.Complete(context.Background())
		// Assert.
		if first != nil || second != nil || final != nil || sink.String() != input {
			t.Fatalf("split=%d: %v %v %v output=%q", split, first, second, final, sink.String())
		}
	}
}

func TestFallbackUsesSupportedReleaseStage(t *testing.T) {
	// Arrange: final deliberately passes; this rule supports only unit checks.
	rule := WithStreamingCapabilities(
		ValidatorFunc[string](func(ctx context.Context, value string) (string, *Report, error) {
			if StreamValidationStage(ctx) == StreamUnit && strings.Contains(value, "SECRET") {
				return value, &Report{Action: ActionBlock}, nil
			}
			return value, nil, nil
		}),
		StreamCapabilities{Unit: true},
	)
	cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
	cfg.Profile = ReleaseValidatedUnits
	cfg.Delivery.Fallback = "SECRET"
	var sink bytes.Buffer
	s, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	_, denied := s.Write([]byte("SECRET\n"))
	_, fallback := s.DeliverFallback(context.Background())
	// Assert.
	if denied == nil || fallback == nil || sink.Len() != 0 {
		t.Fatalf("%v %v output=%q", denied, fallback, sink.String())
	}
}

func TestJSONExactUnitLimitIndependentOfTransport(t *testing.T) {
	for _, input := range []string{"{}", "{}\n", "{}[]"} {
		for split := 0; split <= len(input); split++ {
			// Arrange.
			cfg := testStreamConfig(NewPipeline[string]())
			cfg.Profile, cfg.JSONValues = ReleaseValidatedUnits, true
			cfg.MaxUnitBytes, cfg.MaxPendingBytes = 2, 3
			cfg.Delivery = NewDeliveryPolicy("internal", WithDeliveryAllowedKinds(PayloadTechnicalPayload))
			var sink bytes.Buffer
			s, err := CompileStream(&sink, cfg)
			if err != nil {
				t.Fatal(err)
			}
			// Act.
			_, first := s.Write([]byte(input[:split]))
			_, second := s.Write([]byte(input[split:]))
			outcome, final := s.Complete(context.Background())
			// Assert: whitespace exceeds the previous unit budget, not a new unit.
			if input == "{}\n" {
				if outcome.Category != StreamLimit || final == nil || sink.Len() != 0 {
					t.Fatalf("split %d: %+v %v output=%q", split, outcome, final, sink.String())
				}
			} else if first != nil || second != nil || final != nil || sink.String() != input {
				t.Fatalf("%q split %d: %v %v %v output=%q", input, split, first, second, final, sink.String())
			}
		}
	}
}

func TestStreamObserverDoesNotExposeCorrectionFeedback(t *testing.T) {
	// Arrange.
	rule := ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
		return value, &Report{Action: ActionRetry, Feedback: "diagnostic=" + value}, nil
	})
	cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
	var event StreamEvent
	cfg.Observer = func(e StreamEvent) { event = e }
	var sink bytes.Buffer
	s, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	_, _ = s.Write([]byte("SECRET"))
	_, err = s.Complete(context.Background())
	// Assert.
	if err == nil || event.Outcome.Decision.RetryFeedback != "" || sink.Len() != 0 {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

func TestUnitFallbackCannotBypassFraming(t *testing.T) {
	// Arrange.
	rule := WithStreamingCapabilities(
		ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
			if value == "SECRET\n" {
				return value, &Report{Action: ActionBlock}, nil
			}
			return value, nil, nil
		}),
		StreamCapabilities{Unit: true},
	)
	cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
	cfg.Profile = ReleaseValidatedUnits
	cfg.Delivery.Fallback = "benign\nSECRET\n"
	var sink bytes.Buffer
	s, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	_, denied := s.Write([]byte("SECRET\n"))
	fallback, err := s.DeliverFallback(context.Background())
	// Assert.
	if denied == nil || err == nil || fallback.Category != StreamUnsupported || sink.Len() != 0 {
		t.Fatalf("%+v %v output=%q", fallback, err, sink.String())
	}
}

func TestRequiredScopeTypeRejectedBeforePolicy(t *testing.T) {
	// Arrange.
	key := NewScopeKey[bool]("required.flag")
	calls := 0
	rule := NewPolicyFuncWithScope[string](
		[]ScopeRequirement{key.Requirement()},
		func(_ context.Context, value string, _ ExecutionScope) (string, *Report, error) {
			calls++
			return value, nil, nil
		},
	)
	p := NewPipeline(WithPolicyValidators(rule))
	// Act.
	_, err := p.Run(context.Background(), MapScope{"required.flag": "wrong"}, "SECRET")
	// Assert.
	var mismatch *ScopeTypeError
	if !errors.As(err, &mismatch) || calls != 0 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestArgsEncoderFaultCanonicalDecision(t *testing.T) {
	// Arrange.
	p := MustCompileArgs[positiveAmount](NewPipeline[string](), WithArgsCodec[positiveAmount](
		func(_ string, value *positiveAmount) error { value.Amount = 1; return nil },
		func(positiveAmount) (string, error) { return "", errors.New("encoder fault") },
	))
	// Act.
	payload, err := p.Validate(context.Background(), nil, `{}`)
	// Assert.
	var failure *PolicyFailure
	if !errors.As(err, &failure) || !payload.Decision.IsSystemFault() || payload.Decision != failure.Decision {
		t.Fatalf("payload=%+v err=%v", payload, err)
	}
}

func TestCompositionPreservesAllTypedScopeRequirements(t *testing.T) {
	// Arrange.
	boolKey := NewScopeKey[bool]("shared.fact")
	stringKey := NewScopeKey[string]("shared.fact")
	calls := 0
	callback := func(_ context.Context, value string, _ ExecutionScope) (string, *Report, error) {
		calls++
		return value, nil, nil
	}
	p := NewPipeline(WithPolicyValidators(
		NewPolicyFuncWithScope([]ScopeRequirement{boolKey.Requirement()}, callback),
		NewPolicyFuncWithScope([]ScopeRequirement{stringKey.Requirement()}, callback),
	))
	// Act.
	result, err := p.Run(context.Background(), NewScope(ScopeValue(boolKey, false)), "SECRET")
	// Assert.
	if !errors.Is(err, ErrScopeIncompatible) || calls != 0 || !result.PolicyDecision().IsSystemFault() {
		t.Fatalf("calls=%d decision=%+v err=%v", calls, result.PolicyDecision(), err)
	}
}

func TestBoundaryConfigurationTypedFailures(t *testing.T) {
	for _, test := range []struct {
		identity             string
		supported, mandatory []Boundary
		code                 string
	}{
		{identity: "", supported: nil, mandatory: nil, code: "missing_identity"},
		{identity: "mapping", supported: []Boundary{"invalid.boundary"}, mandatory: nil, code: "incompatible_boundary"},
		{identity: "mapping", supported: []Boundary{BoundaryArgs}, mandatory: []Boundary{BoundaryDelivery}, code: "unsupported_mandatory_boundary"},
	} {
		// Arrange / Act.
		_, err := CompileBoundaryProfile(test.identity, test.supported, test.mandatory)
		// Assert.
		var failure *BoundaryConfigurationError
		if !errors.As(err, &failure) || failure.Code != test.code || !errors.Is(err, ErrBoundaryConfiguration) {
			t.Fatalf("%s: %v", test.code, err)
		}
	}
}

func TestScopeContractsUseGoTypeIdentityNotDisplayName(t *testing.T) {
	// Arrange: these distinct standard-library types have identical display names.
	firstKey := NewScopeKey[*htmltemplate.Template]("shared.template")
	secondKey := NewScopeKey[*texttemplate.Template]("shared.template")
	if firstKey.Requirement().Type != secondKey.Requirement().Type {
		t.Fatal("fixture must reproduce display-name collision")
	}
	calls := 0
	callback := func(_ context.Context, value string, _ ExecutionScope) (string, *Report, error) {
		calls++
		return value, nil, nil
	}
	p := NewPipeline(WithPolicyValidators(
		NewPolicyFuncWithScope([]ScopeRequirement{firstKey.Requirement()}, callback),
		NewPolicyFuncWithScope([]ScopeRequirement{secondKey.Requirement()}, callback),
	))
	// Act.
	result, err := p.Run(context.Background(), NewScope(ScopeValue(firstKey, htmltemplate.New("first"))), "SECRET")
	// Assert.
	if calls != 0 || !errors.Is(err, ErrScopeIncompatible) || !result.PolicyDecision().IsSystemFault() {
		t.Fatalf("calls=%d err=%v result=%+v", calls, err, result)
	}
}
