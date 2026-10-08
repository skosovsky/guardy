package guardy

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"golang.org/x/sync/errgroup"
)

// ValidatorMiddleware wraps a Validator with cross-cutting logic (metrics, logging).
// Construction must be deterministic: stream compilation checks the applied
// wrapper capabilities, including scoped wrappers reconstructed per invocation.
// Unit/partial streaming requires an explicit declaration on the resulting wrapper.
type ValidatorMiddleware[T any] func(next Validator[T]) Validator[T]

// Pipeline orchestrates the execution of multiple Validators.
//
// THREAD SAFETY:
// A Pipeline is safe for concurrent use only when its validators, middleware,
// observers, scopes and caller-owned aliases are concurrency-safe. Parallel
// validators must not mutate input aliases. Configuration lists are immutable;
// objects/providers referenced by those lists remain caller-owned.
// Configuration method Use returns a new instance and never mutates the original pipeline.
type Pipeline[T any] struct {
	sequentialPath         []Validator[T]
	policyValidators       []PolicyValidator[T]
	parallelPath           []Validator[T]
	middlewares            []ValidatorMiddleware[T]
	observer               Observer
	name                   string
	requiredKeys           []string
	requiredScope          []ScopeRequirement
	userChannel            bool
	userChannelFallback    string
	userChannelFallbackSet bool

	// Wrapped chains built at Use() time (zero-overhead hot path).
	sequentialPathWrapped []Validator[T]
	parallelPathWrapped   []Validator[T]
	sequentialPathLayers  []Validator[T]
	parallelPathLayers    []Validator[T]
}

// PipelineOption configures a Pipeline.
type PipelineOption[T any] func(*Pipeline[T])

// WithObserver registers a callback for shadow block reports.
func WithObserver[T any](o Observer) PipelineOption[T] {
	return func(p *Pipeline[T]) {
		p.observer = o
	}
}

// WithPipelineName sets a stable identity included in observer events.
func WithPipelineName[T any](name string) PipelineOption[T] {
	return func(p *Pipeline[T]) {
		p.name = name
	}
}

// WithSequential adds validators that run sequentially and may return redact.
func WithSequential[T any](v ...Validator[T]) PipelineOption[T] {
	return func(p *Pipeline[T]) {
		p.sequentialPath = append(p.sequentialPath, v...)
	}
}

// WithParallel adds validators that run in parallel (read-only, no redact).
func WithParallel[T any](v ...Validator[T]) PipelineOption[T] {
	return func(p *Pipeline[T]) {
		p.parallelPath = append(p.parallelPath, v...)
	}
}

// WithPolicyValidators adds scope-aware policy validators (sequential, after sequential phase).
// Required scope is compiled once at pipeline construction; [Pipeline.Run] fails closed when keys are missing.
func WithPolicyValidators[T any](pv ...PolicyValidator[T]) PipelineOption[T] {
	return func(pipe *Pipeline[T]) {
		pipe.policyValidators = append(pipe.policyValidators, pv...)
	}
}

// WithUserChannel enables terminal filtering: non-safe [PayloadKind] becomes ActionBlock.
func WithUserChannel[T any]() PipelineOption[T] {
	return func(p *Pipeline[T]) {
		p.userChannel = true
	}
}

// WithUserChannelFallback sets the SafeUserMessage when user channel blocks technical output.
func WithUserChannelFallback[T any](msg string) PipelineOption[T] {
	return func(p *Pipeline[T]) {
		p.userChannelFallback = msg
		p.userChannelFallbackSet = true
	}
}

// Use appends middleware and returns a new immutable pipeline instance.
// The original pipeline is not modified. Validator/provider objects and their
// internal state are shared, not cloned; Use does not make them concurrency-safe.
func (p *Pipeline[T]) Use(mw ...ValidatorMiddleware[T]) (*Pipeline[T], error) {
	if p == nil {
		return nil, configurationError("pipeline", "receiver", "nil")
	}
	for i, middleware := range mw {
		if middleware == nil {
			return nil, configurationError("pipeline", fmt.Sprintf("middleware[%d]", i), "nil")
		}
	}
	if len(mw) == 0 {
		return p, nil
	}
	next := p.clone()
	next.middlewares = append(next.middlewares, mw...)
	next.sequentialPathWrapped, next.sequentialPathLayers = next.wrapAll(next.sequentialPath)
	next.parallelPathWrapped, next.parallelPathLayers = next.wrapAll(next.parallelPath)
	_, policyLayers := next.buildPolicyChain(MapScope{}, true)
	for _, layers := range [][]Validator[T]{next.sequentialPathLayers, next.parallelPathLayers, policyLayers} {
		for _, layer := range layers {
			if nilImplementation(layer) {
				return nil, configurationError("pipeline", "middleware.wrapper", "nil")
			}
		}
	}
	return next, nil
}

// MustUse appends middleware or panics on the same configuration error as Use.
func (p *Pipeline[T]) MustUse(mw ...ValidatorMiddleware[T]) *Pipeline[T] {
	next, err := p.Use(mw...)
	if err != nil {
		panic(err)
	}
	return next
}

// RequiredScopeKeys returns keys compiled at pipeline construction (immutable after clone).
func (p *Pipeline[T]) RequiredScopeKeys() []string {
	if p == nil || len(p.requiredKeys) == 0 {
		return nil
	}
	out := make([]string, len(p.requiredKeys))
	copy(out, p.requiredKeys)
	return out
}

// RequiredScope returns typed scope requirements compiled at pipeline construction.
func (p *Pipeline[T]) RequiredScope() []ScopeRequirement {
	if p == nil || len(p.requiredScope) == 0 {
		return nil
	}
	out := make([]ScopeRequirement, len(p.requiredScope))
	copy(out, p.requiredScope)
	return out
}

func (p *Pipeline[T]) sequentialChain() []Validator[T] {
	if len(p.middlewares) == 0 {
		return p.sequentialPath
	}
	return p.sequentialPathWrapped
}

func (p *Pipeline[T]) policyChain(scope ExecutionScope) []Validator[T] {
	chain, _ := p.buildPolicyChain(scope, false)
	return chain
}

func (p *Pipeline[T]) buildPolicyChain(scope ExecutionScope, collectLayers bool) ([]Validator[T], []Validator[T]) {
	if len(p.policyValidators) == 0 {
		return nil, nil
	}
	out := make([]Validator[T], len(p.policyValidators))
	var layers []Validator[T]
	for i, pv := range p.policyValidators {
		base := policyValidatorAdapter[T]{p: pv, scope: scope}
		wrapped := Validator[T](base)
		if capable, ok := pv.(streamCapable); ok {
			wrapped = streamingPolicyAdapter[T]{policyValidatorAdapter: base, capability: capable.StreamCapabilities()}
		}
		for _, mw := range slices.Backward(p.middlewares) {
			wrapped = mw(wrapped)
			if collectLayers {
				layers = append(layers, wrapped)
			}
			if nilImplementation(wrapped) {
				break
			}
		}
		out[i] = wrapped
	}
	return out, layers
}

// Preserve a declared scoped rule capability through the invocation adapter.
// Middleware still has to declare the capabilities of its resulting wrapper.
type streamingPolicyAdapter[T any] struct {
	policyValidatorAdapter[T]

	capability StreamCapabilities
}

func (v streamingPolicyAdapter[T]) StreamCapabilities() StreamCapabilities { return v.capability }

func (p *Pipeline[T]) parallelChain() []Validator[T] {
	if len(p.middlewares) == 0 {
		return p.parallelPath
	}
	return p.parallelPathWrapped
}

func (p *Pipeline[T]) wrapAll(vv []Validator[T]) ([]Validator[T], []Validator[T]) {
	if len(p.middlewares) == 0 {
		return vv, nil
	}
	out := make([]Validator[T], len(vv))
	var layers []Validator[T]
	for i, v := range vv {
		wrapped := v
		for _, mw := range slices.Backward(p.middlewares) {
			wrapped = mw(wrapped)
			layers = append(layers, wrapped)
			if nilImplementation(wrapped) {
				break
			}
		}
		out[i] = wrapped
	}
	return out, layers
}

func (p *Pipeline[T]) clone() *Pipeline[T] {
	next := &Pipeline[T]{
		sequentialPathWrapped:  nil,
		parallelPathWrapped:    nil,
		sequentialPathLayers:   nil,
		parallelPathLayers:     nil,
		observer:               p.observer,
		userChannelFallbackSet: p.userChannelFallbackSet,
		name:                   p.name,
		userChannel:            p.userChannel,
		userChannelFallback:    p.userChannelFallback,
		sequentialPath:         append([]Validator[T](nil), p.sequentialPath...),
		policyValidators:       append([]PolicyValidator[T](nil), p.policyValidators...),
		parallelPath:           append([]Validator[T](nil), p.parallelPath...),
		middlewares:            append([]ValidatorMiddleware[T](nil), p.middlewares...),
		requiredKeys:           append([]string(nil), p.requiredKeys...),
		requiredScope:          append([]ScopeRequirement(nil), p.requiredScope...),
	}
	next.sequentialPathWrapped = append([]Validator[T](nil), p.sequentialPathWrapped...)
	next.parallelPathWrapped = append([]Validator[T](nil), p.parallelPathWrapped...)
	next.sequentialPathLayers = append([]Validator[T](nil), p.sequentialPathLayers...)
	next.parallelPathLayers = append([]Validator[T](nil), p.parallelPathLayers...)
	return next
}

func (p *Pipeline[T]) notifyObserver(
	ctx context.Context,
	scope ExecutionScope,
	rep *Report,
	phase ValidationPhase,
) {
	if p == nil || p.observer == nil || rep == nil {
		return
	}
	p.observer(ctx, newGuardEvent(scope, rep, phase, p.name))
}

// NewPipeline builds a pipeline, rejecting invalid configuration before Run.
// Options and RequiredScope callbacks are trusted construction code; panics escape.
func NewPipeline[T any](opts ...PipelineOption[T]) (*Pipeline[T], error) {
	p := new(Pipeline[T])
	for i, opt := range opts {
		if opt == nil {
			return nil, configurationError("pipeline", fmt.Sprintf("options[%d]", i), "nil")
		}
		opt(p)
	}
	if p.userChannelFallbackSet && !p.userChannel {
		return nil, configurationError("pipeline", "fallback", "requires_user_channel")
	}
	for _, phase := range []struct {
		name  string
		rules []Validator[T]
	}{
		{"sequential", p.sequentialPath}, {"parallel", p.parallelPath},
	} {
		for i, rule := range phase.rules {
			if nilImplementation(rule) {
				return nil, configurationError("pipeline", fmt.Sprintf("%s[%d]", phase.name, i), "nil")
			}
		}
	}
	for i, rule := range p.policyValidators {
		if nilImplementation(rule) {
			return nil, configurationError("pipeline", fmt.Sprintf("policy[%d]", i), "nil")
		}
	}
	for _, rule := range p.policyValidators {
		requirements := rule.RequiredScope()
		if err := validateScopeDeclarations(requirements); err != nil {
			return nil, err
		}
		p.requiredScope = mergeScopeRequirements(p.requiredScope, requirements)
	}
	p.requiredKeys = scopeRequirementKeys(p.requiredScope)
	return p, nil
}

// MustNewPipeline builds a pipeline or panics on NewPipeline's configuration error.
func MustNewPipeline[T any](opts ...PipelineOption[T]) *Pipeline[T] {
	p, err := NewPipeline(opts...)
	if err != nil {
		panic(err)
	}
	return p
}

// normalizeReport copies a completed validator report and resolves its effective disposition.
// It never reapplies construction defaults or suppresses a fault.
func normalizeReport(rep *Report) Report {
	if rep == nil {
		return Report{Action: ActionPass, Disposition: DispositionNone}
	}
	cp := *rep
	cp.Disposition = cp.effectiveDisposition()
	return cp
}

func shouldShortCircuitValidator(rep *Report) bool {
	if rep == nil {
		return false
	}
	if rep.IsObservation() {
		return false
	}
	nr := normalizeReport(rep)
	return nr.IsRetryableCorrection() || nr.IsTerminalDeny() || nr.IsSystemFault()
}

func recordParallelDecision(rep *Report, block, retry **Report, cancel context.CancelFunc) {
	nr := normalizeReport(rep)
	if nr.IsRetryableCorrection() {
		if *retry == nil {
			*retry = rep
		}
		return
	}
	if (nr.IsTerminalDeny() || nr.IsSystemFault()) && *block == nil {
		*block = rep
		cancel()
	}
}

func (p *Pipeline[T]) finalizeResult(output T, reports []Report) RunResult[T] {
	kind := AggregatePayloadKind(reports)
	out := output
	if p.userChannel && kind != PayloadSafeUserText {
		blockRep := FinishReport(&Report{
			Action:          ActionBlock,
			Validator:       "user_channel",
			Code:            CodePolicyViolation,
			Reason:          "output not safe for user channel",
			SafeUserMessage: p.userChannelFallback,
			PayloadKind:     kind,
		}, ControlSpec{Action: ActionBlock})
		reports = append(reports, *blockRep)
		var zero T
		out = zero
	}
	if p.userChannel {
		partial := RunResult[T]{
			Output:     out,
			Reports:    reports,
			OutputKind: AggregatePayloadKind(reports),
		}
		if rep := partial.Decision(); rep != nil && (rep.IsTerminalDeny() || rep.IsSystemFault()) {
			var zero T
			out = zero
		}
	}
	return RunResult[T]{
		Output:     out,
		Reports:    reports,
		OutputKind: AggregatePayloadKind(reports),
	}
}

func (p *Pipeline[T]) validatorFaultResult(output T, reports []Report, cause error) (RunResult[T], error) {
	completed := make([]*Report, len(reports))
	for i := range reports {
		completed[i] = &reports[i]
	}
	cause = WithCompletedObservations(cause, completed...)
	faultRep := validatorFaultReport(cause)
	reports = append(reports, faultRep)
	result := RunResult[T]{
		Output:     output,
		Reports:    reports,
		OutputKind: AggregatePayloadKind(reports),
	}
	return result, validatorFaultErrorFromReport(policyDecisionReport(reports, result.OutputKind), cause)
}

// Run executes the pipeline. Block and Retry short-circuit immediately.
// scope supplies policy keys; fail-closed when compiled required keys are missing.
//
//nolint:funlen,gocognit,gocyclo,cyclop // single orchestration function; splitting would obscure phase flow
func (p *Pipeline[T]) Run(ctx context.Context, scope ExecutionScope, input T) (RunResult[T], error) {
	if err := checkScopeRequirements(scope, p.requiredScope); err != nil {
		return p.validatorFaultResult(input, nil, err)
	}
	if scope == nil {
		scope = MapScope{}
	}

	var reports []Report

	// Phase 1: sequential checks (cached wrapped chain when middleware applied)
	fastCtx := withValidationPhase(ctx, ValidationPhaseSequential)
	fastToRun := p.sequentialChain()
	current := input
	for _, v := range fastToRun {
		if err := fastCtx.Err(); err != nil {
			return p.validatorFaultResult(current, reports, err)
		}
		out, rep, err := validateSafely(fastCtx, v, current)
		if ctxErr := fastCtx.Err(); ctxErr != nil {
			err = errors.Join(err, ctxErr)
		}
		if err != nil {
			return p.validatorFaultResult(current, appendCompletedObservations(reports, err), err)
		}
		if rep != nil {
			reports = append(reports, normalizeReport(rep))
		}
		if shouldShortCircuitValidator(rep) {
			return p.finalizeResult(out, reports), nil
		}
		if rep.IsObservation() {
			p.notifyObserver(fastCtx, scope, rep, ValidationPhaseSequential)
			current = out
			continue
		}
		current = out
	}

	// Phase 2: sequential, scope-aware policy
	policyCtx := withValidationPhase(ctx, ValidationPhasePolicy)
	policyToRun := p.policyChain(scope)
	for _, v := range policyToRun {
		if err := policyCtx.Err(); err != nil {
			return p.validatorFaultResult(current, reports, err)
		}
		out, rep, err := validateSafely(policyCtx, v, current)
		if ctxErr := policyCtx.Err(); ctxErr != nil {
			err = errors.Join(err, ctxErr)
		}
		if err != nil {
			return p.validatorFaultResult(current, appendCompletedObservations(reports, err), err)
		}
		if rep != nil {
			reports = append(reports, normalizeReport(rep))
		}
		if shouldShortCircuitValidator(rep) {
			return p.finalizeResult(out, reports), nil
		}
		if rep.IsObservation() {
			p.notifyObserver(policyCtx, scope, rep, ValidationPhasePolicy)
			current = out
			continue
		}
		current = out
	}

	// Phase 3: parallel read-only checks (Redact forbidden)
	if len(p.parallelPath) == 0 {
		if err := ctx.Err(); err != nil {
			return p.validatorFaultResult(current, reports, err)
		}
		return p.finalizeResult(current, reports), nil
	}

	var (
		mu       sync.Mutex
		block    *Report
		retry    *Report
		slowReps []Report
		firstErr error
	)
	phase2Ctx, cancelPhase2 := context.WithCancel(ctx)
	defer cancelPhase2()
	slowCtx := withValidationPhase(phase2Ctx, ValidationPhaseParallel)
	slowToRun := p.parallelChain()
	g, gctx := errgroup.WithContext(slowCtx)
	for i := range slowToRun {
		v := slowToRun[i]
		g.Go(func() error {
			out, rep, validateErr := validateSafely(gctx, v, current)
			_ = out // parallel phase is read-only, we ignore mutations
			if ctxErr := gctx.Err(); ctxErr != nil {
				validateErr = errors.Join(validateErr, ctxErr)
			}
			if validateErr != nil {
				mu.Lock()
				slowReps = appendCompletedObservations(slowReps, validateErr)
				mu.Unlock()
				// A sibling deny cancels cooperative checks; genuine detector faults
				// still outrank deny/retry in the canonical result.
				if errors.Is(validateErr, context.Canceled) && cancellationOnly(validateErr) && ctx.Err() == nil {
					mu.Lock()
					stopped := block != nil || retry != nil
					mu.Unlock()
					if stopped {
						return nil
					}
				}
				mu.Lock()
				if firstErr == nil {
					firstErr = validateErr
				}
				mu.Unlock()
				return validateErr
			}
			if rep != nil && rep.Action == ActionRedact {
				e := fmt.Errorf("%w: parallel phase validator must not return ActionRedact", ErrValidatorFailed)
				mu.Lock()
				if firstErr == nil {
					firstErr = e
				}
				mu.Unlock()
				return e
			}
			if rep != nil {
				mu.Lock()
				slowReps = append(slowReps, normalizeReport(rep))
				mu.Unlock()
			}
			if rep != nil && shouldShortCircuitValidator(rep) {
				mu.Lock()
				recordParallelDecision(rep, &block, &retry, cancelPhase2)
				mu.Unlock()
				return nil
			}
			if rep.IsObservation() {
				p.notifyObserver(gctx, scope, rep, ValidationPhaseParallel)
			}
			return nil
		})
	}

	err := g.Wait()
	reports = append(reports, slowReps...)
	partial := p.finalizeResult(current, reports)
	if err != nil {
		return p.validatorFaultResult(current, reports, err)
	}
	if firstErr != nil {
		return p.validatorFaultResult(current, reports, firstErr)
	}
	if err := ctx.Err(); err != nil {
		return p.validatorFaultResult(current, reports, err)
	}
	return partial, nil
}
