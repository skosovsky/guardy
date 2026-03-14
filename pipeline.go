package guardy

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"
)

// Pipeline runs validators in two phases: sequential Fast-Path (mutations),
// then parallel Slow-Path (block/pass only) via errgroup.
type Pipeline struct {
	fastPath []Validator
	slowPath []Validator
	observer func(Report)
}

// PipelineOption configures a Pipeline.
type PipelineOption func(*Pipeline)

// WithObserver registers a callback invoked for non-blocking shadow block reports.
// It is intended for telemetry (e.g. shadow-mode detections) without changing the public Report shape.
func WithObserver(fn func(Report)) PipelineOption {
	return func(p *Pipeline) {
		p.observer = fn
	}
}

// WithFastPath adds validators that run sequentially and may return redact.
func WithFastPath(v ...Validator) PipelineOption {
	return func(p *Pipeline) {
		p.fastPath = append(p.fastPath, v...)
	}
}

// WithSlowPath adds validators that run in parallel (errgroup) on the cleaned text.
func WithSlowPath(v ...Validator) PipelineOption {
	return func(p *Pipeline) {
		p.slowPath = append(p.slowPath, v...)
	}
}

// NewPipeline builds a pipeline from options.
func NewPipeline(opts ...PipelineOption) *Pipeline {
	p := &Pipeline{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Run executes the pipeline on text. Phase 1 runs fastPath sequentially,
// accumulating MutatedText; Phase 2 runs slowPath in parallel. On block (non-shadow)
// returns immediately with that Report.
func (p *Pipeline) Run(ctx context.Context, text string) (Report, error) {
	finalReport := Report{
		Action:      ActionPass,
		MutatedText: text,
	}

	// Phase 1: sequential Fast-Path
	for _, v := range p.fastPath {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		rep, err := v.Validate(ctx, finalReport.MutatedText)
		if err != nil {
			return Report{}, fmt.Errorf("%w: %w", ErrValidatorFailed, err)
		}
		if rep.Action == ActionBlock && !rep.ShadowMode {
			return rep, nil
		}
		if rep.Action == ActionBlock && rep.ShadowMode {
			if p.observer != nil {
				p.observer(rep)
			}
			continue
		}
		if rep.Action == ActionRedact {
			finalReport.MutatedText = rep.MutatedText
			finalReport.Action = ActionRedact
			finalReport.Validator = rep.Validator
			finalReport.Reason = rep.Reason
		}
		if rep.Action == ActionPass && finalReport.Action == ActionPass {
			finalReport.Validator = rep.Validator
		}
	}

	// Phase 2: parallel Slow-Path on finalReport.MutatedText.
	// Block has priority over infrastructure errors: we collect results and then prefer block.
	if len(p.slowPath) == 0 {
		return finalReport, nil
	}

	var (
		mu       sync.Mutex
		block    *Report
		firstErr error
	)
	phase2Ctx, cancelPhase2 := context.WithCancel(ctx)
	defer cancelPhase2()
	g, gctx := errgroup.WithContext(phase2Ctx)
	phase2Text := finalReport.MutatedText
	for i := range p.slowPath {
		v := p.slowPath[i]
		g.Go(func() error {
			rep, err := v.Validate(gctx, phase2Text)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return nil
			}
			if rep.Action != ActionBlock && rep.Action != ActionPass {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("%w: slow-path validator returned unexpected action %s", ErrValidatorFailed, rep.Action)
				}
				mu.Unlock()
				return nil
			}
			if rep.Action == ActionBlock && !rep.ShadowMode {
				mu.Lock()
				if block == nil {
					block = &rep
					cancelPhase2()
				}
				mu.Unlock()
				return nil
			}
			if rep.Action == ActionBlock && rep.ShadowMode {
				if p.observer != nil {
					p.observer(rep)
				}
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return Report{}, fmt.Errorf("%w: %w", ErrValidatorFailed, err)
	}
	if block != nil {
		return *block, nil
	}
	if firstErr != nil {
		return Report{}, fmt.Errorf("%w: %w", ErrValidatorFailed, firstErr)
	}
	return finalReport, nil
}
