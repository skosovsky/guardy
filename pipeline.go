package guardy

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"
)

// Observer is a callback invoked for non-blocking shadow block reports.
// It receives the request context and the report; intended for telemetry.
type Observer func(ctx context.Context, rep *Report)

// ValidatorMiddleware wraps a Validator with cross-cutting logic (metrics, logging).
type ValidatorMiddleware[T any] func(next Validator[T]) Validator[T]

// Pipeline orchestrates the execution of multiple Validators.
//
// THREAD SAFETY:
// A Pipeline is safe for concurrent use.
// Configuration method Use returns a new instance and never mutates the original pipeline.
type Pipeline[T any] struct {
	fastPath    []Validator[T]
	policyPath  []Validator[T]
	slowPath    []Validator[T]
	middlewares []ValidatorMiddleware[T]
	observer    Observer

	// Wrapped chains built at Use() time (zero-overhead hot path).
	fastPathWrapped   []Validator[T]
	policyPathWrapped []Validator[T]
	slowPathWrapped   []Validator[T]
}

// PipelineOption configures a Pipeline.
type PipelineOption[T any] func(*Pipeline[T])

// WithObserver registers a callback for shadow block reports.
func WithObserver[T any](o Observer) PipelineOption[T] {
	return func(p *Pipeline[T]) {
		p.observer = o
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

// WithPolicyValidators adds context-aware policy validators (sequential, after fast-path).
// Validators run only when [Attributes] were stored with [WithAttributes] (including an empty map).
// When attributes are absent from ctx, the policy phase is a no-op.
func WithPolicyValidators[T any](pv ...PolicyValidator[T]) PipelineOption[T] {
	adapters := make([]Validator[T], len(pv))
	for i, p := range pv {
		adapters[i] = policyValidatorAdapter[T]{p: p}
	}
	return func(pipe *Pipeline[T]) {
		pipe.policyPath = append(pipe.policyPath, adapters...)
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
	next.policyPathWrapped = next.wrapAll(next.policyPath)
	next.slowPathWrapped = next.wrapAll(next.slowPath)
	return next
}

// fastChain returns validators for phase 1 (cached if middlewares applied, else raw).
func (p *Pipeline[T]) fastChain() []Validator[T] {
	if len(p.middlewares) == 0 {
		return p.fastPath
	}
	return p.fastPathWrapped
}

// policyChain returns validators for the policy phase.
func (p *Pipeline[T]) policyChain() []Validator[T] {
	if len(p.middlewares) == 0 {
		return p.policyPath
	}
	return p.policyPathWrapped
}

// slowChain returns validators for phase 2 (cached if middlewares applied, else raw).
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
		for j := len(p.middlewares) - 1; j >= 0; j-- {
			wrapped = p.middlewares[j](wrapped)
		}
		out[i] = wrapped
	}
	return out
}

func (p *Pipeline[T]) clone() *Pipeline[T] {
	next := &Pipeline[T]{
		observer:    p.observer,
		fastPath:    append([]Validator[T](nil), p.fastPath...),
		policyPath:  append([]Validator[T](nil), p.policyPath...),
		slowPath:    append([]Validator[T](nil), p.slowPath...),
		middlewares: append([]ValidatorMiddleware[T](nil), p.middlewares...),
	}
	next.fastPathWrapped = append([]Validator[T](nil), p.fastPathWrapped...)
	next.policyPathWrapped = append([]Validator[T](nil), p.policyPathWrapped...)
	next.slowPathWrapped = append([]Validator[T](nil), p.slowPathWrapped...)
	return next
}

// NewPipeline builds a pipeline from options.
func NewPipeline[T any](opts ...PipelineOption[T]) *Pipeline[T] {
	p := &Pipeline[T]{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Run executes the pipeline. Block and Retry short-circuit immediately.
// Returns RunResult with Output and all Reports for telemetry.
//
//nolint:funlen,gocognit,gocyclo,cyclop // single orchestration function; splitting would obscure phase flow
func (p *Pipeline[T]) Run(ctx context.Context, input T) (RunResult[T], error) {
	var zero RunResult[T]
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
			return zero, fmt.Errorf("%w: %w", ErrValidatorFailed, err)
		}
		if rep != nil {
			reports = append(reports, *rep)
		}
		if rep != nil && (rep.Action == ActionBlock || rep.Action == ActionRetry) && !rep.ShadowMode {
			return RunResult[T]{Output: out, Reports: reports}, nil
		}
		if rep != nil && rep.Action == ActionBlock && rep.ShadowMode {
			if p.observer != nil {
				p.observer(fastCtx, rep)
			}
			current = out
			continue
		}
		current = out
	}

	// Policy phase: sequential, context-aware (no-op when Attributes absent from ctx)
	policyToRun := p.policyChain()
	policyCtx := ctx
	for _, v := range policyToRun {
		if err := policyCtx.Err(); err != nil {
			return zero, err
		}
		out, rep, err := v.Validate(policyCtx, current)
		if err != nil {
			return zero, fmt.Errorf("%w: %w", ErrValidatorFailed, err)
		}
		if rep != nil {
			reports = append(reports, *rep)
		}
		if rep != nil && (rep.Action == ActionBlock || rep.Action == ActionRetry) && !rep.ShadowMode {
			return RunResult[T]{Output: out, Reports: reports}, nil
		}
		if rep != nil && rep.Action == ActionBlock && rep.ShadowMode {
			if p.observer != nil {
				p.observer(policyCtx, rep)
			}
			current = out
			continue
		}
		current = out
	}

	// Phase 2: parallel Slow-Path (read-only; Redact forbidden)
	if len(p.slowPath) == 0 {
		return RunResult[T]{Output: current, Reports: reports}, nil
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
				slowReps = append(slowReps, *rep)
				mu.Unlock()
			}
			if rep != nil && rep.Action == ActionBlock && !rep.ShadowMode {
				mu.Lock()
				if block == nil {
					block = rep
					cancelPhase2()
				}
				mu.Unlock()
				return nil
			}
			if rep != nil && rep.Action == ActionRetry {
				mu.Lock()
				if retry == nil {
					retry = rep
				}
				mu.Unlock()
				return nil
			}
			if rep != nil && rep.Action == ActionBlock && rep.ShadowMode {
				if p.observer != nil {
					p.observer(gctx, rep)
				}
			}
			return nil
		})
	}

	err := g.Wait()
	reports = append(reports, slowReps...)
	partial := RunResult[T]{Output: current, Reports: reports}
	if block != nil || retry != nil {
		return partial, nil
	}
	if err != nil {
		return partial, fmt.Errorf("%w: %w", ErrValidatorFailed, err)
	}
	if firstErr != nil {
		return partial, fmt.Errorf("%w: %w", ErrValidatorFailed, firstErr)
	}
	return partial, nil
}
