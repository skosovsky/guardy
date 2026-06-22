package guardy

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
)

var errJSONArgsPipelineNil = errors.New("guardy: JSON args pipeline requires non-nil raw pipeline")

// JSONArgsSchema validates dynamic JSON argument objects without making guardy
// depend on a concrete schema generator.
type JSONArgsSchema interface {
	SchemaID() string
	Shape() any
	ValidateJSONArgs(context.Context, map[string]any) *Report
}

// JSONArgsSchemaFunc adapts a function to [JSONArgsSchema].
type JSONArgsSchemaFunc struct {
	ID       string
	Metadata any
	Validate func(context.Context, map[string]any) *Report
}

// SchemaID implements [JSONArgsSchema].
func (s JSONArgsSchemaFunc) SchemaID() string {
	return s.ID
}

// Shape implements [JSONArgsSchema].
func (s JSONArgsSchemaFunc) Shape() any {
	return s.Metadata
}

// ValidateJSONArgs implements [JSONArgsSchema].
func (s JSONArgsSchemaFunc) ValidateJSONArgs(ctx context.Context, object map[string]any) *Report {
	if s.Validate == nil {
		return nil
	}
	return s.Validate(ctx, object)
}

// GuardedJSONArgs is the dynamic JSON argument boundary returned by guardy.
type GuardedJSONArgs struct {
	Raw          string
	SanitizedRaw string
	Object       map[string]any
	SchemaID     string
	Reports      []Report
	Decision     Decision
	PayloadKind  PayloadKind
}

// JSONArgsPipeline validates raw JSON and keeps the sanitized raw payload,
// decoded object, schema identity, and decision in one boundary value.
type JSONArgsPipeline struct {
	raw    *Pipeline[string]
	schema JSONArgsSchema
}

// CompileJSONArgs builds a dynamic JSON arguments pipeline from a raw string guard.
func CompileJSONArgs(raw *Pipeline[string], schema JSONArgsSchema) (*JSONArgsPipeline, error) {
	if raw == nil {
		return nil, errJSONArgsPipelineNil
	}
	return &JSONArgsPipeline{raw: raw, schema: schema}, nil
}

// MustCompileJSONArgs is like [CompileJSONArgs] but panics on invalid configuration.
func MustCompileJSONArgs(raw *Pipeline[string], schema JSONArgsSchema) *JSONArgsPipeline {
	p, err := CompileJSONArgs(raw, schema)
	if err != nil {
		panic(err)
	}
	return p
}

// Shape returns optional schema or shape metadata attached at compile time.
func (p *JSONArgsPipeline) Shape() (any, bool) {
	if p == nil || p.schema == nil {
		return nil, false
	}
	return p.schema.Shape(), true
}

// SchemaID returns the provider-supplied schema identity, if present.
func (p *JSONArgsPipeline) SchemaID() string {
	if p == nil || p.schema == nil {
		return ""
	}
	return p.schema.SchemaID()
}

// RequiredScope returns typed scope requirements from the raw guard.
func (p *JSONArgsPipeline) RequiredScope() []ScopeRequirement {
	if p == nil || p.raw == nil {
		return nil
	}
	return p.raw.RequiredScope()
}

// RequiredScopeKeys returns scope keys from the raw guard.
func (p *JSONArgsPipeline) RequiredScopeKeys() []string {
	if p == nil || p.raw == nil {
		return nil
	}
	return p.raw.RequiredScopeKeys()
}

// Validate runs raw validation before decoding into a dynamic JSON object.
func (p *JSONArgsPipeline) Validate(ctx context.Context, scope ExecutionScope, raw string) (GuardedJSONArgs, error) {
	if p == nil || p.raw == nil {
		return GuardedJSONArgs{
			Raw:          raw,
			SanitizedRaw: raw,
			Object:       nil,
			SchemaID:     "",
			Reports:      nil,
			Decision:     DecisionFromReport(nil),
			PayloadKind:  PayloadSafeUserText,
		}, errJSONArgsPipelineNil
	}
	result, err := p.raw.Run(ctx, scope, raw)
	args := guardedJSONArgsFromRun(raw, p.SchemaID(), result)
	if err != nil {
		return args, err
	}
	if decErr := errorFromDecision(result.Decision()); decErr != nil {
		return args, decErr
	}

	var object map[string]any
	if unmarshalErr := json.Unmarshal([]byte(result.Output), &object); unmarshalErr != nil || object == nil {
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

	if p.schema == nil {
		return args, nil
	}
	if rep := p.schema.ValidateJSONArgs(ctx, copyStringAnyMap(object)); rep != nil {
		finished := FinishReport(rep.Clone(), reportControlSpec(rep))
		args.Reports = append(args.Reports, *finished)
		decisionReport := refreshGuardedJSONArgsDecision(&args)
		if decErr := errorFromDecision(decisionReport); decErr != nil {
			return args, decErr
		}
	}
	return args, nil
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

func reportControlSpec(rep *Report) ControlSpec {
	if rep == nil {
		return ControlSpec{}
	}
	spec := ControlSpec{
		Action:          rep.Action,
		Fatal:           rep.Fatal,
		SafeUserMessage: rep.SafeUserMessage,
	}
	if rep.Retryable {
		retryable := true
		spec.Retryable = &retryable
	}
	return spec
}

func guardedJSONArgsFromRun(raw string, schemaID string, result RunResult[string]) GuardedJSONArgs {
	return GuardedJSONArgs{
		Raw:          raw,
		SanitizedRaw: result.Output,
		Object:       nil,
		SchemaID:     schemaID,
		Reports:      append([]Report(nil), result.Reports...),
		Decision:     result.PolicyDecision(),
		PayloadKind:  result.OutputKind,
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
	return maps.Clone(in)
}
