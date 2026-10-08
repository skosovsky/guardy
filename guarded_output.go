package guardy

import (
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
)

const (
	defaultDeliveryPolicyValidator = "delivery_policy"
	userTextTraversalLimit         = 64
	deliveryDereferenceLimitCode   = "dereference_limit"
)

// DeliveryClassifier classifies the actual destination representation of a value.
// The caller owns canonicalization/serialization, termination and thread safety.
// It must not mark unknown representations safe or mutate the value.
type DeliveryClassifier func(any) (PayloadKind, error)

// DeliveryPolicy requires an explicit destination, allowed kinds and classifier.
// Generic delivery makes no inference from Go shape or MarshalJSON. UserText
// shape classification is available separately through NewUserTextPolicy.
type DeliveryPolicy struct {
	Channel             string
	AllowedPayloadKinds []PayloadKind
	Fallback            any
	Classifier          DeliveryClassifier
	configurationErr    error
}

// DeliveryPolicyOption configures [DeliveryPolicy].
type DeliveryPolicyOption func(*DeliveryPolicy)

// NewDeliveryPolicy creates a channel-aware delivery policy.
func NewDeliveryPolicy(channel string, opts ...DeliveryPolicyOption) DeliveryPolicy {
	policy := DeliveryPolicy{
		Channel:             channel,
		AllowedPayloadKinds: nil,
		Fallback:            nil,
		Classifier:          nil,
		configurationErr:    nil,
	}
	for _, opt := range opts {
		if opt == nil {
			policy.configurationErr = configurationError("delivery", "options", "nil_option")
			continue
		}
		opt(&policy)
	}
	return policy
}

// WithDeliveryAllowedKinds allows the listed payload kinds for this delivery policy.
func WithDeliveryAllowedKinds(kinds ...PayloadKind) DeliveryPolicyOption {
	return func(policy *DeliveryPolicy) {
		policy.AllowedPayloadKinds = append([]PayloadKind(nil), kinds...)
	}
}

// WithDeliveryFallback configures a separately checked fallback for blocked delivery.
// nil means absent; a typed nil is present and must assert to the pipeline T.
// Compatibility is checked before processing; eligibility is never assumed.
func WithDeliveryFallback(fallback any) DeliveryPolicyOption {
	return func(policy *DeliveryPolicy) {
		policy.Fallback = fallback
	}
}

// GuardedDelivery is the channel-aware guarded output contract.
type GuardedDelivery[T any] struct {
	Value           T
	Kind            PayloadKind
	Decision        Decision
	Reports         []Report
	Deliverable     bool
	Channel         string
	Fallback        bool
	ConfigurationID string
}

// DeliverableValue returns the guarded value only when guardy marked it deliverable.
func (o GuardedDelivery[T]) DeliverableValue() (T, bool) {
	if !o.Deliverable {
		var zero T
		return zero, false
	}
	return o.Value, true
}

// GuardOutput applies the explicit user-text recipe and returns GuardedDelivery.
// Use GuardDelivery with a caller classifier for other wire representations.
func (p *Pipeline[T]) GuardOutput(ctx context.Context, scope ExecutionScope, output T) (GuardedDelivery[T], error) {
	return p.GuardDelivery(ctx, scope, NewUserTextPolicy("user"), output)
}

// GuardDelivery runs the pipeline and applies a channel-aware delivery policy.
func (p *Pipeline[T]) GuardDelivery(
	ctx context.Context,
	scope ExecutionScope,
	policy DeliveryPolicy,
	output T,
) (GuardedDelivery[T], error) {
	policy = normalizeDeliveryPolicy(policy)
	if err := validateDeliveryPolicy[T](policy); err != nil {
		var empty RunResult[T]
		guarded := guardedOutputFromRun(empty, policy)
		guarded.ConfigurationID = p.name
		return deliveryFault(guarded, err)
	}
	result, err := p.Run(ctx, scope, output)
	guarded := guardedOutputFromRun(result, policy)
	guarded.ConfigurationID = p.name
	if err != nil {
		return suppressDelivery(guarded), err
	}
	if decErr := errorFromDecision(result.Decision()); decErr != nil {
		if result.PolicyDecision().IsSystemFault() {
			return suppressDelivery(guarded), decErr
		}
		var fallbackErr error
		guarded, fallbackErr = p.checkedDeliveryFallback(ctx, scope, guarded, policy)
		if fallbackErr != nil {
			return guarded, fallbackErr
		}
		return guarded, decErr
	}
	fallback := policy.Fallback
	policy.Fallback = nil
	guarded, err = applyDeliveryPolicy(ctx, guarded, policy)
	if err != nil {
		if guarded.Decision.IsSystemFault() {
			return guarded, err
		}
		policy.Fallback = fallback
		var fallbackErr error
		guarded, fallbackErr = p.checkedDeliveryFallback(ctx, scope, guarded, policy)
		if fallbackErr != nil {
			return guarded, fallbackErr
		}
		return guarded, err
	}
	return guarded, nil
}

func (p *Pipeline[T]) checkedDeliveryFallback(
	ctx context.Context,
	scope ExecutionScope,
	blocked GuardedDelivery[T],
	policy DeliveryPolicy,
) (GuardedDelivery[T], error) {
	candidate, ok := policy.Fallback.(T)
	if !ok {
		return suppressDelivery(blocked), nil
	}
	policy.Fallback = nil
	checked, err := p.GuardDelivery(ctx, scope, policy, candidate)
	if err != nil || !checked.Deliverable {
		if checked.Decision.IsSystemFault() {
			return suppressDelivery(checked), err
		}
		return suppressDelivery(blocked), nil
	}
	blocked.Value = checked.Value
	blocked.Kind = checked.Kind
	blocked.Deliverable = true
	blocked.Fallback = true
	blocked.Reports = append(blocked.Reports, checked.Reports...)
	return blocked, nil
}

func guardedOutputFromRun[T any](result RunResult[T], policy DeliveryPolicy) GuardedDelivery[T] {
	decision := result.PolicyDecision()
	return GuardedDelivery[T]{
		ConfigurationID: "",
		Value:           result.Output,
		Kind:            result.OutputKind,
		Decision:        decision,
		Reports:         append([]Report(nil), result.Reports...),
		Deliverable:     decision.Disposition == DispositionNone,
		Channel:         policy.Channel,
		Fallback:        false,
	}
}

func normalizeDeliveryPolicy(policy DeliveryPolicy) DeliveryPolicy {
	policy.AllowedPayloadKinds = append([]PayloadKind(nil), policy.AllowedPayloadKinds...)
	return policy
}

func validateDeliveryPolicy[T any](policy DeliveryPolicy) error {
	if policy.configurationErr != nil {
		return policy.configurationErr
	}
	if strings.TrimSpace(policy.Channel) == "" {
		return configurationError("delivery", "channel", "missing_channel")
	}
	if len(policy.AllowedPayloadKinds) == 0 {
		return configurationError("delivery", "allowed_kinds", "missing_kinds")
	}
	for _, kind := range policy.AllowedPayloadKinds {
		if payloadKindPriority(kind) == 0 {
			return configurationError("delivery", "allowed_kinds", "invalid_kind")
		}
	}
	if policy.Classifier == nil {
		return configurationError("delivery", "classifier", "missing_classifier")
	}
	if policy.Fallback != nil {
		if _, ok := policy.Fallback.(T); !ok {
			return configurationError("delivery", "fallback", "incompatible_type")
		}
	}
	return nil
}

func deliveryFault[T any](guarded GuardedDelivery[T], cause error) (GuardedDelivery[T], error) {
	report := validatorFaultReport(cause)
	report.Validator = defaultDeliveryPolicyValidator
	report.PayloadKind = guarded.Kind
	guarded.Reports = append(guarded.Reports, report)
	guarded.Decision = DecisionFromReport(&report)
	return suppressDelivery(guarded), validatorFaultErrorFromReport(&report, cause)
}

func applyDeliveryPolicy[T any](
	ctx context.Context,
	guarded GuardedDelivery[T],
	policy DeliveryPolicy,
) (GuardedDelivery[T], error) {
	kind, err := runDeliveryClassifier(policy.Classifier, guarded.Value)
	if cancelErr := callbackCancellation(ctx, err); cancelErr != nil {
		return deliveryFault(guarded, cancelErr)
	}
	if err != nil {
		return deliveryFault(guarded, err)
	}
	if payloadKindPriority(kind) == 0 {
		return deliveryFault(guarded, &DeliveryClassificationError{Code: "invalid_kind"})
	}
	if payloadKindPriority(guarded.Kind) > payloadKindPriority(kind) {
		kind = guarded.Kind
	}
	if deliveryPolicyAllows(policy, kind) {
		guarded.Kind = kind
		guarded.Decision.PayloadKind = kind
		guarded.Deliverable = guarded.Decision.Disposition == DispositionNone
		if !guarded.Deliverable {
			var zero T
			guarded.Value = zero
		}
		return guarded, nil
	}

	rep := FinishReport(&Report{
		Action:          ActionBlock,
		Validator:       defaultDeliveryPolicyValidator,
		Code:            CodePolicyViolation,
		Reason:          "payload kind is not deliverable on channel",
		SafeUserMessage: fallbackSafeMessage(policy),
		PayloadKind:     kind,
	}, ControlSpec{
		Action:          ActionBlock,
		SafeUserMessage: fallbackSafeMessage(policy),
	})
	guarded.Kind = kind
	guarded.Reports = append(guarded.Reports, *rep)
	guarded.Decision = DecisionFromReport(rep)
	guarded = suppressDelivery(guarded)
	return guarded, blockErrorFromReport(rep)
}

func suppressDelivery[T any](guarded GuardedDelivery[T]) GuardedDelivery[T] {
	var zero T
	guarded.Value = zero
	guarded.Deliverable = false
	guarded.Fallback = false
	return guarded
}

func deliveryPolicyAllows(policy DeliveryPolicy, kind PayloadKind) bool {
	return slices.Contains(policy.AllowedPayloadKinds, kind)
}

func fallbackSafeMessage(policy DeliveryPolicy) string {
	if fallback, ok := policy.Fallback.(string); ok && !isStructuredJSONString(fallback) {
		return fallback
	}
	return ""
}

// WithDeliveryClassifier binds the caller's destination representation contract.
func WithDeliveryClassifier(classifier DeliveryClassifier) DeliveryPolicyOption {
	return func(policy *DeliveryPolicy) { policy.Classifier = classifier }
}

// NewUserTextPolicy explicitly selects UserTextClassifier and safe-text eligibility.
// Other allowed kinds can be selected explicitly for technical destinations.
func NewUserTextPolicy(channel string, opts ...DeliveryPolicyOption) DeliveryPolicy {
	base := []DeliveryPolicyOption{
		WithDeliveryAllowedKinds(PayloadSafeUserText),
		WithDeliveryClassifier(UserTextClassifier),
	}
	return NewDeliveryPolicy(channel, append(base, opts...)...)
}

// ErrDeliveryClassification identifies unsupported or cyclic representations.
var ErrDeliveryClassification = errors.New("guardy: unsupported delivery representation")

// DeliveryClassificationError contains only a library-owned diagnostic code.
type DeliveryClassificationError struct{ Code string }

func (e *DeliveryClassificationError) Error() string { return ErrDeliveryClassification.Error() }
func (e *DeliveryClassificationError) Unwrap() error { return ErrDeliveryClassification }

// runDeliveryClassifier never promotes a failed classifier to successful evidence.
//
//nolint:nonamedreturns // recovery sets both result fields before returning a fault.
func runDeliveryClassifier(classifier DeliveryClassifier, value any) (kind PayloadKind, err error) {
	defer func() {
		if recover() != nil {
			kind = PayloadSafeUserText
			err = &DeliveryClassificationError{Code: "classifier_panic"}
		}
	}()
	return classifier(value)
}

// UserTextClassifier is an opt-in shape recipe, not a proof of serialized safety.
// Text/bytes (including named types without custom marshalers) are checked for
// JSON object/array syntax. Maps/slices/arrays/structs are technical. Pointer and
// interface dereference is bounded to 64 steps. Nil typed pointers use their
// underlying type; nil interface, unsupported scalars and custom marshalers fault.
// [json.RawMessage] is supported explicitly; arbitrary MarshalJSON/MarshalText is
// never invoked. The host must deliver the checked representation unchanged.
func UserTextClassifier(value any) (PayloadKind, error) {
	v := reflect.ValueOf(value)
	for step := range userTextTraversalLimit + 1 {
		if !v.IsValid() {
			return PayloadSafeUserText, &DeliveryClassificationError{Code: "nil_interface"}
		}
		if customDeliveryMarshaler(v.Type()) {
			return PayloadSafeUserText, &DeliveryClassificationError{Code: "custom_marshaler"}
		}
		switch v.Kind() {
		case reflect.Interface, reflect.Pointer:
			if step == userTextTraversalLimit {
				return PayloadSafeUserText, &DeliveryClassificationError{Code: deliveryDereferenceLimitCode}
			}
			if v.IsNil() {
				return userTextTypeKind(v.Type(), userTextTraversalLimit-step)
			}
			v = v.Elem()
		case reflect.String:
			return userTextStringKind(v.String()), nil
		case reflect.Slice:
			if v.Type().Elem().Kind() == reflect.Uint8 {
				return userTextStringKind(string(v.Bytes())), nil
			}
			return PayloadTechnicalPayload, nil
		case reflect.Map, reflect.Array, reflect.Struct:
			return PayloadTechnicalPayload, nil
		case reflect.Invalid,
			reflect.Bool,
			reflect.Int,
			reflect.Int8,
			reflect.Int16,
			reflect.Int32,
			reflect.Int64,
			reflect.Uint,
			reflect.Uint8,
			reflect.Uint16,
			reflect.Uint32,
			reflect.Uint64,
			reflect.Uintptr,
			reflect.Float32,
			reflect.Float64,
			reflect.Complex64,
			reflect.Complex128,
			reflect.Chan,
			reflect.Func,
			reflect.UnsafePointer:
			fallthrough
		default:
			return PayloadSafeUserText, &DeliveryClassificationError{Code: "unsupported_type"}
		}
	}
	return PayloadSafeUserText, &DeliveryClassificationError{Code: deliveryDereferenceLimitCode}
}

func userTextTypeKind(t reflect.Type, budget int) (PayloadKind, error) {
	for step := range budget + 1 {
		if customDeliveryMarshaler(t) {
			return PayloadSafeUserText, &DeliveryClassificationError{Code: "custom_marshaler"}
		}
		switch t.Kind() {
		case reflect.Pointer:
			if step == budget {
				return PayloadSafeUserText, &DeliveryClassificationError{Code: deliveryDereferenceLimitCode}
			}
			t = t.Elem()
		case reflect.String:
			return PayloadSafeUserText, nil
		case reflect.Slice:
			if t.Elem().Kind() == reflect.Uint8 {
				return PayloadSafeUserText, nil
			}
			return PayloadTechnicalPayload, nil
		case reflect.Map, reflect.Array, reflect.Struct:
			return PayloadTechnicalPayload, nil
		case reflect.Invalid,
			reflect.Bool,
			reflect.Int,
			reflect.Int8,
			reflect.Int16,
			reflect.Int32,
			reflect.Int64,
			reflect.Uint,
			reflect.Uint8,
			reflect.Uint16,
			reflect.Uint32,
			reflect.Uint64,
			reflect.Uintptr,
			reflect.Float32,
			reflect.Float64,
			reflect.Complex64,
			reflect.Complex128,
			reflect.Chan,
			reflect.Func,
			reflect.UnsafePointer,
			reflect.Interface:
			fallthrough
		default:
			return PayloadSafeUserText, &DeliveryClassificationError{Code: "unsupported_type"}
		}
	}
	return PayloadSafeUserText, &DeliveryClassificationError{Code: deliveryDereferenceLimitCode}
}

func customDeliveryMarshaler(t reflect.Type) bool {
	if t == reflect.TypeFor[json.RawMessage]() || t == reflect.TypeFor[*json.RawMessage]() {
		return false
	}
	return t.Implements(reflect.TypeFor[json.Marshaler]()) ||
		t.Implements(reflect.TypeFor[encoding.TextMarshaler]())
}

func userTextStringKind(value string) PayloadKind {
	if isStructuredJSONString(value) {
		return PayloadTechnicalPayload
	}
	return PayloadSafeUserText
}

func isStructuredJSONString(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	switch trimmed[0] {
	case '{', '[':
	default:
		return false
	}
	var decoded any
	return json.Unmarshal([]byte(trimmed), &decoded) == nil
}
