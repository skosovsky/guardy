package guardy

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Pipeline runs validators in tiers and aggregates results.
// It is immutable after construction and safe for concurrent use.
type Pipeline struct {
	tier1    []Validator
	tier2    []Validator
	tier3    []Validator
	failFast bool
	failOpen bool
	logger   *slog.Logger
	onResult func(name string, r Result, d time.Duration)
}

// PipelineOption configures a Pipeline.
type PipelineOption func(*Pipeline)

// WithTier1 adds validators to tier 1 (fast heuristics).
func WithTier1(v ...Validator) PipelineOption {
	return func(p *Pipeline) {
		p.tier1 = append(p.tier1, v...)
	}
}

// WithTier2 adds validators to tier 2 (semantic, e.g. embeddings).
func WithTier2(v ...Validator) PipelineOption {
	return func(p *Pipeline) {
		p.tier2 = append(p.tier2, v...)
	}
}

// WithTier3 adds validators to tier 3 (e.g. LLM-as-judge).
func WithTier3(v ...Validator) PipelineOption {
	return func(p *Pipeline) {
		p.tier3 = append(p.tier3, v...)
	}
}

// WithFailFast stops the pipeline on the first Block (default: true when not set explicitly).
func WithFailFast(failFast bool) PipelineOption {
	return func(p *Pipeline) {
		p.failFast = failFast
	}
}

// WithFailOpen sets policy for validator system errors: true = skip and continue, false = treat as Block.
func WithFailOpen(failOpen bool) PipelineOption {
	return func(p *Pipeline) {
		p.failOpen = failOpen
	}
}

// WithLogger sets an optional logger for pipeline execution.
func WithLogger(logger *slog.Logger) PipelineOption {
	return func(p *Pipeline) {
		p.logger = logger
	}
}

// WithOnResult sets an optional callback invoked after each validator run (for metrics).
func WithOnResult(fn func(name string, r Result, d time.Duration)) PipelineOption {
	return func(p *Pipeline) {
		p.onResult = fn
	}
}

// NewPipeline builds an immutable pipeline from options.
func NewPipeline(opts ...PipelineOption) *Pipeline {
	p := &Pipeline{
		failFast: true,
		failOpen: true,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Run executes the pipeline on the given input and returns an aggregated report.
// It checks ctx.Err() before each tier and returns immediately if context is cancelled.
func (p *Pipeline) Run(ctx context.Context, input Input) (Report, error) {
	var allResults []Result
	text := input.Text

	tiers := [][]Validator{p.tier1, p.tier2, p.tier3}
	for _, tier := range tiers {
		if err := ctx.Err(); err != nil {
			return Report{
				Results:     allResults,
				FinalAction: Pass,
				FinalText:   text,
			}, err
		}
		if len(tier) == 0 {
			continue
		}

		results, tierErr := p.runTier(ctx, tier, input)
		if tierErr != nil && !p.failOpen {
			return Report{
				Results:     allResults,
				FinalAction: Block,
				FinalText:   text,
			}, tierErr
		}
		if len(results) == 0 && tierErr != nil {
			continue
		}

		allResults = append(allResults, results...)

		// Apply Redact results: each validator in the tier saw the same (original or tier-input) text
		// because they run in parallel; we apply CleanText in result order (last non-empty wins for this tier).
		// For a true sequential redact chain (output of one as input to next), use separate tiers.
		for i := range results {
			r := &results[i]
			if r.Action == Redact && r.CleanText != "" {
				text = r.CleanText
			}
		}

		// Aggregate tier outcome (use allResults so Block from earlier tier wins over Override in later tier)
		bestAction, overrideText := aggregateActions(allResults)
		if bestAction == Block && p.failFast {
			return Report{
				Results:     allResults,
				FinalAction: Block,
				FinalText:   text,
			}, nil
		}
		if bestAction == Override && overrideText != "" {
			return Report{
				Results:      allResults,
				FinalAction:  Override,
				FinalText:    text,
				OverrideText: overrideText,
			}, nil
		}
		// Update input.Text for next tier (after Redacts)
		input.Text = text
	}

	bestAction, overrideText := aggregateActions(allResults)
	return Report{
		Results:      allResults,
		FinalAction:  bestAction,
		FinalText:    text,
		OverrideText: overrideText,
	}, nil
}

// runTier runs all validators in the tier in parallel and collects results.
// If a validator returns an error, it is recorded; when failOpen is false the first error is returned.
func (p *Pipeline) runTier(ctx context.Context, tier []Validator, input Input) ([]Result, error) {
	type pair struct {
		result Result
		err    error
	}
	results := make([]pair, len(tier))
	var wg sync.WaitGroup
	for i, v := range tier {
		wg.Add(1)
		go func(i int, v Validator) {
			defer wg.Done()
			name := v.Name()
			defer func() {
				if rec := recover(); rec != nil {
					results[i] = pair{err: fmt.Errorf("%w: validator %q panicked: %v", ErrValidatorFailed, name, rec)}
				}
			}()
			start := time.Now()
			r, err := v.Validate(ctx, input)
			d := time.Since(start)
			if err != nil {
				err = fmt.Errorf("%w: %w", ErrValidatorFailed, err)
			}
			if p.onResult != nil {
				p.onResult(name, r, d)
			}
			if p.logger != nil {
				if err != nil {
					p.logger.ErrorContext(ctx, "validator failed", "validator", name, "error", err)
				} else {
					p.logger.InfoContext(ctx, "validator completed", "validator", name, "action", r.Action, "code", r.Code, "duration_ms", d.Milliseconds())
				}
			}
			results[i] = pair{result: r, err: err}
		}(i, v)
	}
	wg.Wait()

	var out []Result
	var firstErr error
	for i := range results {
		pr := &results[i]
		if pr.err != nil {
			if firstErr == nil {
				firstErr = pr.err
			}
			if !p.failOpen {
				return nil, firstErr
			}
			continue
		}
		out = append(out, pr.result)
	}
	return out, firstErr
}

// aggregateActions returns the highest-priority action and the first OverrideText if any.
func aggregateActions(results []Result) (bestAction Action, overrideText string) {
	var bestPri int
	for i := range results {
		r := &results[i]
		pri := PriorityForAction(r.Action)
		if pri > bestPri {
			bestPri = pri
			bestAction = r.Action
			if r.Action == Override && r.OverrideText != "" {
				overrideText = r.OverrideText
			} else {
				overrideText = ""
			}
		}
	}
	if bestAction == "" {
		bestAction = Pass
	}
	return bestAction, overrideText
}
