package guardy

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"golang.org/x/sync/errgroup"
)

// ValidatorMiddleware wraps a Validator with cross-cutting logic (metrics, logging).
type ValidatorMiddleware[T any] func(next Validator[T]) Validator[T]

// Pipeline orchestrates the execution of multiple Validators.
//
// THREAD SAFETY:
// A Pipeline is safe for concurrent use.
// Configuration method Use returns a new instance and never mutates the original pipeline.
type Pipeline[T any] struct {
	fastPath            []Validator[T]
	policyValidators    []PolicyValidator[T]
	slowPath            []Validator[T]
	middlewares         []ValidatorMiddleware[T]
	observer            Observer
	name                string
	requiredKeys        []string
	requiredScope       []ScopeRequirement
	userChannel         bool
	userChannelFallback string

	// Wrapped chains built at Use() time (zero-overhead hot path).
	fastPathWrapped []Validator[T]
	slowPathWrapped []Validator[T]
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

// WithFastPath adds validators that run sequentially and may return redact.
func WithFastPath[T any](v ...Validator[T]) PipelineOption[T] {
	return func(p *Pipeline[T]) {
		p.fastPath = append(p.fastPath, v...)
	}
}

// WithSlowPath adds validators that run in parallel (read-only, no redact).
func WithSlowPath[T any](v ...Validator[T]) PipelineOption[T] {
	return func(p *Pipeline[T]) {
		p.slowPath = append(p.slowPath, v...)
	}
}

// WithPolicyValidators adds scope-aware policy validators (sequential, after fast-path).
// Required scope is compiled once at pipeline construction; [Pipeline.Run] fails closed when keys are missing.
func WithPolicyValidators[T any](pv ...PolicyValidator[T]) PipelineOption[T] {
	return func(pipe *Pipeline[T]) {
		for _, pv := range pv {
			requirements := pv.RequiredScope()
			pipe.requiredScope = mergeScopeRequirements(pipe.requiredScope, requirements)
			pipe.requiredKeys = scopeRequirementKeys(pipe.requiredScope)
			pipe.policyValidators = append(pipe.policyValidators, pv)
		}
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
	}
}

// Use appends middleware and returns a new immutable pipeline instance.
// The original pipeline is not modified.
func (p *Pipeline[T]) Use(mw ...ValidatorMiddleware[T]) *Pipeline[T] {
	if len(mw) == 0 {
		return p
	}
	next := p.clone()
	next.middlewares = append(next.middlewares, mw...)
	next.fastPathWrapped = next.wrapAll(next.fastPath)
	next.slowPathWrapped = next.wrapAll(next.slowPath)
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

func (p *Pipeline[T]) fastChain() []Validator[T] {
	if len(p.middlewares) == 0 {
		return p.fastPath
	}
	return p.fastPathWrapped
}

func (p *Pipeline[T]) policyChain(scope ExecutionScope) []Validator[T] {
	if len(p.policyValidators) == 0 {
		return nil
	}
	out := make([]Validator[T], len(p.policyValidators))
	for i, pv := range p.policyValidators {
		base := policyValidatorAdapter[T]{p: pv, scope: scope}
		wrapped := Validator[T](base)
		for _, mw := range slices.Backward(p.middlewares) {
			wrapped = mw(wrapped)
		}
		out[i] = wrapped
	}
	return out
}

func (p *Pipeline[T]) slowChain() []Validator[T] {
	if len(p.middlewares) == 0 {
		return p.slowPath
	}
	return p.slowPathWrapped
}

func (p *Pipeline[T]) wrapAll(vv []Validator[T]) []Validator[T] {
	if len(p.middlewares) == 0 {
		return vv
	}
	out := make([]Validator[T], len(vv))
	for i, v := range vv {
		wrapped := v
		for _, mw := range slices.Backward(p.middlewares) {
			wrapped = mw(wrapped)
		}
		out[i] = wrapped
	}
	return out
}

func (p *Pipeline[T]) clone() *Pipeline[T] {
	next := &Pipeline[T]{
		observer:            p.observer,
		name:                p.name,
		userChannel:         p.userChannel,
		userChannelFallback: p.userChannelFallback,
		fastPath:            append([]Validator[T](nil), p.fastPath...),
		policyValidators:    append([]PolicyValidator[T](nil), p.policyValidators...),
		slowPath:            append([]Validator[T](nil), p.slowPath...),
		middlewares:         append([]ValidatorMiddleware[T](nil), p.middlewares...),
		requiredKeys:        append([]string(nil), p.requiredKeys...),
		requiredScope:       append([]ScopeRequirement(nil), p.requiredScope...),
	}
	next.fastPathWrapped = append([]Validator[T](nil), p.fastPathWrapped...)
	next.slowPathWrapped = append([]Validator[T](nil), p.slowPathWrapped...)
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

// NewPipeline builds a pipeline from options.
func NewPipeline[T any](opts ...PipelineOption[T]) *Pipeline[T] {
	p := &Pipeline[T]{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// normalizeReport copies a validator report and fills Disposition when unset.
// Explicit Disposition (e.g. SystemFault on ActionBlock) must be preserved —
// DeriveDisposition alone always maps ActionBlock → TerminalDeny.
func normalizeReport(rep *Report) Report {
	if rep == nil {
		return Report{Action: ActionPass, Disposition: DispositionNone}
	}
	cp := *rep
	if cp.Disposition == DispositionNone {
		cp.Disposition = DeriveDisposition(&cp, nil)
	}
	return cp
}

func shouldShortCircuitValidator(rep *Report) bool {
	if rep == nil {
		return false
	}
	if rep.ShadowMode && rep.Action == ActionBlock {
		return false
	}
	nr := normalizeReport(rep)
	return nr.IsRetryableCorrection() || nr.IsTerminalDeny() || nr.IsSystemFault()
}

func recordSlowPathDecision(rep *Report, block, retry **Report, cancel context.CancelFunc) {
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
		if rep := partial.Decision(); rep != nil && rep.IsTerminalDeny() && !rep.ShadowMode {
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
	faultRep := validatorFaultReport(cause)
	reports = append(reports, faultRep)
	return RunResult[T]{
		Output:     output,
		Reports:    reports,
		OutputKind: AggregatePayloadKind(reports),
	}, validatorFaultError(cause)
}

// Run executes the pipeline. Block and Retry short-circuit immediately.
// scope supplies policy keys; fail-closed when compiled required keys are missing.
//
//nolint:funlen,gocognit,gocyclo,cyclop // single orchestration function; splitting would obscure phase flow
func (p *Pipeline[T]) Run(ctx context.Context, scope ExecutionScope, input T) (RunResult[T], error) {
	var zero RunResult[T]
	if err := checkScopeRequirements(scope, p.requiredScope); err != nil {
		return zero, err
	}
	if scope == nil {
		scope = MapScope{}
	}

	var reports []Report

	// Phase 1: sequential Fast-Path (uses cached wrapped chain when middlewares applied)
	fastCtx := withValidationPhase(ctx, ValidationPhaseFast)
	fastToRun := p.fastChain()
	current := input
	for _, v := range fastToRun {
		if err := fastCtx.Err(); err != nil {
			return zero, err
		}
		out, rep, err := v.Validate(fastCtx, current)
		if err != nil {
			return p.validatorFaultResult(current, reports, err)
		}
		if rep != nil {
			reports = append(reports, normalizeReport(rep))
		}
		if shouldShortCircuitValidator(rep) {
			return p.finalizeResult(out, reports), nil
		}
		if rep != nil && rep.Action == ActionBlock && rep.ShadowMode {
			p.notifyObserver(fastCtx, scope, rep, ValidationPhaseFast)
			current = out
			continue
		}
		current = out
	}

	// Policy phase: sequential, scope-aware
	policyCtx := withValidationPhase(ctx, ValidationPhasePolicy)
	policyToRun := p.policyChain(scope)
	for _, v := range policyToRun {
		if err := policyCtx.Err(); err != nil {
			return zero, err
		}
		out, rep, err := v.Validate(policyCtx, current)
		if err != nil {
			return p.validatorFaultResult(current, reports, err)
		}
		if rep != nil {
			reports = append(reports, normalizeReport(rep))
		}
		if shouldShortCircuitValidator(rep) {
			return p.finalizeResult(out, reports), nil
		}
		if rep != nil && rep.Action == ActionBlock && rep.ShadowMode {
			p.notifyObserver(policyCtx, scope, rep, ValidationPhasePolicy)
			current = out
			continue
		}
		current = out
	}

	// Phase 2: parallel Slow-Path (read-only; Redact forbidden)
	if len(p.slowPath) == 0 {
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
	slowCtx := withValidationPhase(phase2Ctx, ValidationPhaseSlow)
	slowToRun := p.slowChain()
	g, gctx := errgroup.WithContext(slowCtx)
	for i := range slowToRun {
		v := slowToRun[i]
		g.Go(func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("validator panic: %v", r)
				}
			}()
			out, rep, validateErr := v.Validate(gctx, current)
			_ = out // slow-path is read-only, we ignore mutations
			if validateErr != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = validateErr
				}
				mu.Unlock()
				return validateErr
			}
			if rep != nil && rep.Action == ActionRedact {
				e := fmt.Errorf("%w: slow-path validator must not return ActionRedact", ErrValidatorFailed)
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
				recordSlowPathDecision(rep, &block, &retry, cancelPhase2)
				mu.Unlock()
				return nil
			}
			if rep != nil && rep.Action == ActionBlock && rep.ShadowMode {
				p.notifyObserver(gctx, scope, rep, ValidationPhaseSlow)
			}
			return nil
		})
	}

	err := g.Wait()
	reports = append(reports, slowReps...)
	partial := p.finalizeResult(current, reports)
	if block != nil || retry != nil {
		return partial, nil
	}
	if err != nil {
		return partial, validatorFaultError(err)
	}
	if firstErr != nil {
		return partial, validatorFaultError(firstErr)
	}
	return partial, nil
}
