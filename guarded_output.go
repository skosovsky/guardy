package guardy

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
)

const defaultDeliveryPolicyValidator = "delivery_policy"

// DeliveryPolicy defines the generic channel boundary used by guardy to decide
// whether a guarded value may be delivered externally.
type DeliveryPolicy struct {
	Channel             string
	AllowedPayloadKinds []PayloadKind
	Fallback            any
}

// DeliveryPolicyOption configures [DeliveryPolicy].
type DeliveryPolicyOption func(*DeliveryPolicy)

// NewDeliveryPolicy creates a channel-aware delivery policy.
func NewDeliveryPolicy(channel string, opts ...DeliveryPolicyOption) DeliveryPolicy {
	policy := DeliveryPolicy{
		Channel:             channel,
		AllowedPayloadKinds: nil,
		Fallback:            nil,
	}
	for _, opt := range opts {
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

// WithDeliveryFallback configures safe fallback content for blocked delivery.
func WithDeliveryFallback(fallback any) DeliveryPolicyOption {
	return func(policy *DeliveryPolicy) {
		policy.Fallback = fallback
	}
}

// GuardedOutput is the canonical guarded output value returned by guardy.
// Host transports should format this value, not re-classify its safety.
type GuardedOutput[T any] struct {
	Value       T
	Kind        PayloadKind
	Decision    Decision
	Reports     []Report
	Deliverable bool
	Channel     string
	Fallback    bool
}

// GuardedDelivery is the channel-aware guarded output contract.
type GuardedDelivery[T any] struct {
	Value       T
	Kind        PayloadKind
	Decision    Decision
	Reports     []Report
	Deliverable bool
	Channel     string
	Fallback    bool
}

// DeliverableValue returns the guarded value only when guardy marked it deliverable.
func (o GuardedOutput[T]) DeliverableValue() (T, bool) {
	if !o.Deliverable {
		var zero T
		return zero, false
	}
	return o.Value, true
}

// DeliverableValue returns the guarded value only when guardy marked it deliverable.
func (o GuardedDelivery[T]) DeliverableValue() (T, bool) {
	if !o.Deliverable {
		var zero T
		return zero, false
	}
	return o.Value, true
}

// GuardOutput runs the pipeline and returns one authoritative output contract.
func (p *Pipeline[T]) GuardOutput(ctx context.Context, scope ExecutionScope, output T) (GuardedOutput[T], error) {
	delivery, err := p.GuardDelivery(ctx, scope, NewDeliveryPolicy("user"), output)
	return GuardedOutput[T](delivery), err
}

// GuardDelivery runs the pipeline and applies a channel-aware delivery policy.
func (p *Pipeline[T]) GuardDelivery(
	ctx context.Context,
	scope ExecutionScope,
	policy DeliveryPolicy,
	output T,
) (GuardedDelivery[T], error) {
	policy = normalizeDeliveryPolicy(policy)
	result, err := p.Run(ctx, scope, output)
	guarded := guardedOutputFromRun(result, policy)
	if err != nil {
		return suppressDelivery(guarded), err
	}
	if decErr := errorFromDecision(result.Decision()); decErr != nil {
		guarded = applyDeliveryFallback(guarded, policy)
		return guarded, decErr
	}
	guarded, err = applyDeliveryPolicy(guarded, policy)
	if err != nil {
		return guarded, err
	}
	return guarded, nil
}

func guardedOutputFromRun[T any](result RunResult[T], policy DeliveryPolicy) GuardedDelivery[T] {
	decision := result.PolicyDecision()
	return GuardedDelivery[T]{
		Value:       result.Output,
		Kind:        result.OutputKind,
		Decision:    decision,
		Reports:     append([]Report(nil), result.Reports...),
		Deliverable: decision.Disposition == DispositionNone,
		Channel:     policy.Channel,
		Fallback:    false,
	}
}

func normalizeDeliveryPolicy(policy DeliveryPolicy) DeliveryPolicy {
	if policy.Channel == "" {
		policy.Channel = "user"
	}
	if len(policy.AllowedPayloadKinds) == 0 {
		policy.AllowedPayloadKinds = []PayloadKind{PayloadSafeUserText}
	} else {
		policy.AllowedPayloadKinds = append([]PayloadKind(nil), policy.AllowedPayloadKinds...)
	}
	return policy
}

func applyDeliveryPolicy[T any](
	guarded GuardedDelivery[T],
	policy DeliveryPolicy,
) (GuardedDelivery[T], error) {
	kind := classifyDeliveryKind(guarded.Kind, guarded.Value)
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
	guarded = applyDeliveryFallback(guarded, policy)
	return guarded, blockErrorFromReport(rep)
}

func suppressDelivery[T any](guarded GuardedDelivery[T]) GuardedDelivery[T] {
	var zero T
	guarded.Value = zero
	guarded.Deliverable = false
	guarded.Fallback = false
	return guarded
}

func applyDeliveryFallback[T any](guarded GuardedDelivery[T], policy DeliveryPolicy) GuardedDelivery[T] {
	if fallback, ok := policy.Fallback.(T); ok {
		kind := classifyDeliveryKind(PayloadSafeUserText, fallback)
		if !deliveryPolicyAllows(policy, kind) {
			return suppressDelivery(guarded)
		}
		guarded.Value = fallback
		guarded.Kind = kind
		guarded.Decision.PayloadKind = kind
		guarded.Deliverable = true
		guarded.Fallback = true
		return guarded
	}
	var zero T
	guarded.Value = zero
	guarded.Deliverable = false
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

func classifyDeliveryKind(kind PayloadKind, value any) PayloadKind {
	if kind == PayloadSafeUserText && isStructuredDeliveryPayload(value) {
		return PayloadTechnicalPayload
	}
	return kind
}

func isStructuredDeliveryPayload(value any) bool {
	switch v := value.(type) {
	case string:
		return isStructuredJSONString(v)
	case []byte:
		return isStructuredJSONString(string(v))
	case json.RawMessage:
		return isStructuredJSONString(string(v))
	default:
		return isStructuredValue(reflect.ValueOf(value))
	}
}

func isStructuredValue(value reflect.Value) bool {
	if !value.IsValid() {
		return false
	}
	for value.Kind() == reflect.Interface {
		if value.IsNil() {
			return false
		}
		value = value.Elem()
	}
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return isStructuredType(value.Type().Elem())
		}
		value = value.Elem()
		for value.Kind() == reflect.Interface {
			if value.IsNil() {
				return false
			}
			value = value.Elem()
		}
	}
	switch value.Kind() {
	case reflect.String:
		return isStructuredJSONString(value.String())
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
		return true
	default:
		return false
	}
}

func isStructuredType(valueType reflect.Type) bool {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	switch valueType.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
		return true
	default:
		return false
	}
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
