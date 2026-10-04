package guardy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/skosovsky/guardy/internal/jsondoc"
)

var errJSONArgsPipelineNil = errors.New("guardy: JSON args pipeline requires non-nil raw pipeline")

// JSONArgsValidator checks decoded objects using caller-owned rules.
// Metadata is supplied separately through WithJSONArgsMetadata.
type JSONArgsValidator interface {
	ValidateJSONArgs(context.Context, map[string]any) *Report
}

// JSONArgsValidatorFunc adapts an executable callback to JSONArgsValidator.
// CompileJSONArgs rejects a nil function. Callback rules remain caller-owned.
type JSONArgsValidatorFunc func(context.Context, map[string]any) *Report

// ValidateJSONArgs implements JSONArgsValidator.
func (f JSONArgsValidatorFunc) ValidateJSONArgs(ctx context.Context, object map[string]any) *Report {
	if f == nil {
		r := validatorFaultReport(ErrConfiguration)
		return &r
	}
	return f(ctx, object)
}

// JSONArgsMetadata describes a caller schema without implying enforcement.
type JSONArgsMetadata struct {
	ID    string
	Shape any
}

// GuardedJSONArgs is the dynamic JSON argument boundary returned by guardy.
type GuardedJSONArgs struct {
	Raw             string
	SanitizedRaw    string
	Object          map[string]any
	SchemaID        string
	Reports         []Report
	Decision        Decision
	PayloadKind     PayloadKind
	ConfigurationID string
}

// JSONArgsPipeline validates raw JSON and keeps the sanitized raw payload,
// decoded object, schema identity, and decision in one boundary value.
type JSONArgsPipeline struct {
	raw          *Pipeline[string]
	checker      JSONArgsValidator
	metadata     *JSONArgsMetadata
	final        *Pipeline[string]
	requireFinal bool
	identity     string
}

// JSONArgsOption declares final checking and configuration identity.
type JSONArgsOption func(*JSONArgsPipeline)

// WithJSONArgsFinalGuard requires final schema/policy checks on canonical bytes.
func WithJSONArgsFinalGuard(final *Pipeline[string]) JSONArgsOption {
	return func(p *JSONArgsPipeline) { p.final = final; p.requireFinal = true }
}

// WithJSONArgsMetadata attaches identity and shape only; it does not install a checker.
func WithJSONArgsMetadata(metadata JSONArgsMetadata) JSONArgsOption {
	return func(p *JSONArgsPipeline) { p.metadata = &metadata }
}

// WithJSONArgsConfigurationID attaches caller policy/configuration identity.
func WithJSONArgsConfigurationID(identity string) JSONArgsOption {
	return func(p *JSONArgsPipeline) { p.identity = identity }
}

// CompileJSONArgs builds a dynamic JSON arguments pipeline from a raw string guard.
func CompileJSONArgs(
	raw *Pipeline[string],
	checker JSONArgsValidator,
	opts ...JSONArgsOption,
) (*JSONArgsPipeline, error) {
	if raw == nil {
		return nil, configurationError("json_args", "raw", "required")
	}
	if checker != nil && nilImplementation(checker) {
		return nil, configurationError("json_args", "checker", "required")
	}
	p := &JSONArgsPipeline{
		raw:          raw,
		checker:      checker,
		metadata:     nil,
		final:        nil,
		requireFinal: false,
		identity:     raw.name,
	}
	for i, opt := range opts {
		if opt == nil {
			return nil, configurationError("json_args", fmt.Sprintf("options[%d]", i), "required")
		}
		opt(p)
	}
	if p.requireFinal && p.final == nil {
		return nil, configurationError("json_args", "final", "required")
	}
	return p, nil
}

// MustCompileJSONArgs is like [CompileJSONArgs] but panics on invalid configuration.
func MustCompileJSONArgs(raw *Pipeline[string], checker JSONArgsValidator, opts ...JSONArgsOption) *JSONArgsPipeline {
	p, err := CompileJSONArgs(raw, checker, opts...)
	if err != nil {
		panic(err)
	}
	return p
}

// Shape returns optional schema or shape metadata attached at compile time.
func (p *JSONArgsPipeline) Shape() (any, bool) {
	if p == nil || p.metadata == nil {
		return nil, false
	}
	return p.metadata.Shape, true
}

// SchemaID returns the provider-supplied schema identity, if present.
func (p *JSONArgsPipeline) SchemaID() string {
	if p == nil || p.metadata == nil {
		return ""
	}
	return p.metadata.ID
}

// RequiredScope returns typed scope requirements from the raw guard.
func (p *JSONArgsPipeline) RequiredScope() []ScopeRequirement {
	if p == nil || p.raw == nil {
		return nil
	}
	if p.final == nil {
		return p.raw.RequiredScope()
	}
	return mergeScopeRequirements(p.raw.RequiredScope(), p.final.RequiredScope())
}

// RequiredScopeKeys returns scope keys from the raw guard.
func (p *JSONArgsPipeline) RequiredScopeKeys() []string {
	if p == nil || p.raw == nil {
		return nil
	}
	return scopeRequirementKeys(p.RequiredScope())
}

// Validate runs raw validation before decoding into a dynamic JSON object.
// Context cancellation before/after the schema callback outranks its policy report.
func (p *JSONArgsPipeline) Validate(ctx context.Context, scope ExecutionScope, raw string) (GuardedJSONArgs, error) {
	if p == nil || p.raw == nil {
		return GuardedJSONArgs{
			ConfigurationID: "",
			Raw:             raw,
			SanitizedRaw:    raw,
			Object:          nil,
			SchemaID:        "",
			Reports:         nil,
			Decision:        DecisionFromReport(nil),
			PayloadKind:     PayloadSafeUserText,
		}, errJSONArgsPipelineNil
	}
	if err := checkScopeRequirements(scope, p.RequiredScope()); err != nil {
		var payload GuardedJSONArgs
		payload.Raw, payload.ConfigurationID = raw, p.identity
		return jsonArgsFault(payload, err)
	}
	result, err := p.raw.Run(ctx, scope, raw)
	args := guardedJSONArgsFromRun(raw, p.SchemaID(), result)
	args.ConfigurationID = p.identity
	if err != nil {
		return args, err
	}
	if decErr := errorFromDecision(result.Decision()); decErr != nil {
		return args, decErr
	}

	document, decodeErr := jsondoc.Decode(result.Output)
	object, _ := document.(map[string]any)
	if unmarshalErr := decodeErr; unmarshalErr != nil || object == nil {
		rep := FinishReport(&Report{
			Action:   ActionRetry,
			Code:     CodeJSONInvalid,
			Reason:   "invalid JSON object for dynamic args",
			Feedback: jsonObjectFeedback(unmarshalErr),
		}, ControlSpec{Action: ActionRetry})
		args.Reports = append(args.Reports, *rep)
		decisionReport := refreshGuardedJSONArgsDecision(&args)
		return args, retryErrorFromReport(decisionReport)
	}
	args.Object = object
	canonical, encodeErr := json.Marshal(object)
	if encodeErr != nil {
		return jsonArgsFault(args, encodeErr)
	}
	args.SanitizedRaw = string(canonical)

	rep, schemaErr := p.validateSchema(ctx, object)
	if schemaErr != nil {
		return jsonArgsFault(args, schemaErr)
	}
	if rep != nil {
		args.Reports = append(args.Reports, normalizeReport(rep))
		decisionReport := refreshGuardedJSONArgsDecision(&args)
		if decErr := errorFromDecision(decisionReport); decErr != nil {
			return args, decErr
		}
	}
	if p.final != nil {
		checked, finalErr := p.final.Run(ctx, scope, args.SanitizedRaw)
		args.Reports = append(args.Reports, checked.Reports...)
		rep := refreshGuardedJSONArgsDecision(&args)
		if finalErr != nil {
			return jsonArgsFault(args, finalErr)
		}
		if e := errorFromDecision(rep); e != nil {
			return args, e
		}
		if checked.Output != args.SanitizedRaw {
			return jsonArgsFault(args, errors.New("guardy: dynamic final guard mutated canonical bytes"))
		}
	}
	if err := ctx.Err(); err != nil {
		return jsonArgsFault(args, err)
	}
	return args, nil
}

func (p *JSONArgsPipeline) validateSchema(ctx context.Context, object map[string]any) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var rep *Report
	if p.checker != nil {
		rep = p.checker.ValidateJSONArgs(ctx, copyStringAnyMap(object))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return rep, nil
}

func jsonArgsFault(args GuardedJSONArgs, cause error) (GuardedJSONArgs, error) {
	args.Reports = append(args.Reports, validatorFaultReport(cause))
	rep := refreshGuardedJSONArgsDecision(&args)
	return args, validatorFaultErrorFromReport(rep, cause)
}

func refreshGuardedJSONArgsDecision(args *GuardedJSONArgs) *Report {
	if args == nil {
		return nil
	}
	args.PayloadKind = AggregatePayloadKind(args.Reports)
	decisionReport := policyDecisionReport(args.Reports, args.PayloadKind)
	args.Decision = DecisionFromReport(decisionReport)
	return decisionReport
}

func guardedJSONArgsFromRun(raw string, schemaID string, result RunResult[string]) GuardedJSONArgs {
	return GuardedJSONArgs{
		ConfigurationID: "",
		Raw:             raw,
		SanitizedRaw:    result.Output,
		Object:          nil,
		SchemaID:        schemaID,
		Reports:         append([]Report(nil), result.Reports...),
		Decision:        result.PolicyDecision(),
		PayloadKind:     result.OutputKind,
	}
}

func jsonObjectFeedback(err error) string {
	if err != nil {
		return err.Error()
	}
	return "JSON payload must be an object"
}

func copyStringAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	// JSON-decoded values contain only maps, slices and scalar JSON values.
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = copyJSONValue(v)
	}
	return out
}

func copyJSONValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return copyStringAnyMap(v)
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = copyJSONValue(child)
		}
		return out
	default:
		return v
	}
}
