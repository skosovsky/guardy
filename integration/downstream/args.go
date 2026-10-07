package downstream

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/skosovsky/toolsy"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext/jsonschema"
)

// MapArgsError maps only pre-handler failures. Public copy is static and bounded
// to 128 bytes regardless of diagnostic length; Err retains the original chain.
// Post-handler failures must retain their noncorrectable result-contract wrapper.
func MapArgsError(err error) *toolsy.ToolError {
	if err == nil {
		return nil
	}
	code, retry, message := toolsy.CodeInternal, false, "argument validation failed"
	var failure *guardy.PolicyFailure
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		code, message = toolsy.CodeTimeout, "argument validation timed out"
	case errors.Is(err, context.Canceled):
		message = "argument validation canceled"
	case errors.As(err, &failure):
		switch {
		case failure.Decision.IsSystemFault():
		case failure.Decision.IsTerminal():
			code, message = toolsy.CodePolicyDenied, "arguments denied"
		case failure.Decision.IsRetryable():
			code, retry, message = toolsy.CodeValidationFailed, true, "arguments require correction"
		}
	}
	return &toolsy.ToolError{
		Code: code, Retryable: retry, Reason: message, SafeMessage: message,
		FixableArgs: nil, Err: err,
	}
}

// NewArgsBinder builds a typed tool binder. Both pipelines are required; final
// must be read-only. Options configure binding/codecs, but cannot remove final.
// The actual tool manifest schema is also enforced on final canonical bytes.
// Scope is refreshed per invocation. Host policy/ArgValidator must not mutate
// approved arguments; approval/resume requires revalidation by the host.
func NewArgsBinder[T any](
	raw, final *guardy.Pipeline[string],
	facts guardy.ScopeFactory,
	opts ...guardy.ArgsOption[T],
) (toolsy.ArgsBinder[T], error) {
	options := append([]guardy.ArgsOption[T](nil), opts...)
	options = append(options, guardy.WithArgsFinalGuard[T](final))
	pipeline, err := guardy.CompileArgs[T](raw, options...)
	if err != nil {
		return nil, MapArgsError(err)
	}
	return func(ctx context.Context, req toolsy.ArgsBindRequest) (toolsy.ValidatedArgs[T], error) {
		var zero toolsy.ValidatedArgs[T]
		scope, scopeErr := resolveScope(ctx, facts)
		if scopeErr != nil {
			return zero, MapArgsError(scopeErr)
		}
		args, validateErr := pipeline.Validate(ctx, scope, string(req.Input.ArgsJSON))
		if validateErr != nil {
			return zero, MapArgsError(validateErr)
		}
		if schemaErr := checkManifest(ctx, scope, req.Manifest, args.SanitizedRaw); schemaErr != nil {
			return zero, MapArgsError(schemaErr)
		}
		return toolsy.ValidatedArgs[T]{Value: args.Value, Raw: []byte(args.SanitizedRaw), Metadata: nil}, nil
	}, nil
}

// NewJSONArgsBinder provides the same boundary for dynamic JSON objects. Checker
// is optional; final and the actual manifest schema are always enforced.
func NewJSONArgsBinder(
	raw, final *guardy.Pipeline[string],
	facts guardy.ScopeFactory,
	checker guardy.JSONArgsValidator,
) (toolsy.ArgsBinder[map[string]any], error) {
	pipeline, err := guardy.CompileJSONArgs(raw, checker, guardy.WithJSONArgsFinalGuard(final))
	if err != nil {
		return nil, MapArgsError(err)
	}
	return func(ctx context.Context, req toolsy.ArgsBindRequest) (toolsy.ValidatedArgs[map[string]any], error) {
		var zero toolsy.ValidatedArgs[map[string]any]
		scope, scopeErr := resolveScope(ctx, facts)
		if scopeErr != nil {
			return zero, MapArgsError(scopeErr)
		}
		args, validateErr := pipeline.Validate(ctx, scope, string(req.Input.ArgsJSON))
		if validateErr != nil {
			return zero, MapArgsError(validateErr)
		}
		if schemaErr := checkManifest(ctx, scope, req.Manifest, args.SanitizedRaw); schemaErr != nil {
			return zero, MapArgsError(schemaErr)
		}
		return toolsy.ValidatedArgs[map[string]any]{
			Value: args.Object, Raw: []byte(args.SanitizedRaw), Metadata: nil,
		}, nil
	}, nil
}

func resolveScope(ctx context.Context, facts guardy.ScopeFactory) (guardy.ExecutionScope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if facts == nil {
		return guardy.NewScope(), nil
	}
	scope, err := facts(ctx)
	if interrupted := ctx.Err(); interrupted != nil {
		return nil, errors.Join(err, interrupted)
	}
	return scope, err
}

func checkManifest(ctx context.Context, scope guardy.ExecutionScope, manifest toolsy.ToolManifest, raw string) error {
	if len(manifest.Parameters) == 0 {
		return errors.New("downstream: missing argument schema")
	}
	schema, err := json.Marshal(manifest.Parameters)
	if err != nil {
		return err
	}
	validator, err := jsonschema.NewJSONSchemaValidator(string(schema))
	if err != nil {
		return err
	}
	pipeline, err := guardy.NewPipeline(guardy.WithSequential(validator))
	if err != nil {
		return err
	}
	boundary, err := guardy.CompileArgs[json.RawMessage](pipeline)
	if err != nil {
		return err
	}
	_, err = boundary.Validate(ctx, scope, raw)
	return err
}
