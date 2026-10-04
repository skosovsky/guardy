package guardy

import (
	"context"
	"errors"
	"math"
)

var errSemanticMatcherNil = errors.New("guardy: semantic matcher is nil")

var (
	errSemanticThresholdInvalid = errors.New("guardy: semantic threshold must be finite")
	errSemanticScoreInvalid     = errors.New("guardy: semantic score must be finite")
)

// Matcher returns a similarity score for the text (e.g. from a vector search).
// Higher score means more likely to block; threshold is applied by SemanticValidator.
type Matcher interface {
	Match(ctx context.Context, text string) (score float64, err error)
}

// SemanticValidator is a Slow-Path validator that blocks when score exceeds threshold.
type SemanticValidator struct {
	matcher   Matcher
	threshold float64
	shadow    bool
	name      string
}

// NewSemanticValidator builds a validator that blocks when m.Match returns score > threshold.
// If shadow is true, block reports are marked ShadowMode so the pipeline does not short-circuit.
// Validate rejects non-finite thresholds/scores as faults; the matcher defines
// its own finite score range. This does not estimate detector accuracy.
func NewSemanticValidator(m Matcher, threshold float64, shadow bool) *SemanticValidator {
	return &SemanticValidator{matcher: m, threshold: threshold, shadow: shadow, name: "semantic"}
}

// Validate runs the matcher and returns block when score > threshold.
// It checks context before/after the synchronous matcher; cancellation cannot stop
// a non-cooperative matcher but prevents its late result from becoming a report.
func (s *SemanticValidator) Validate(ctx context.Context, input string) (string, *Report, error) {
	if err := ctx.Err(); err != nil {
		return input, nil, err
	}
	if math.IsNaN(s.threshold) || math.IsInf(s.threshold, 0) {
		return input, nil, errSemanticThresholdInvalid
	}
	if s.matcher == nil {
		return "", nil, errSemanticMatcherNil
	}
	score, err := s.matcher.Match(ctx, input)
	if ctxErr := callbackCancellation(ctx, err); ctxErr != nil {
		return input, nil, ctxErr
	}
	if err != nil {
		return input, nil, err
	}
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return input, nil, errSemanticScoreInvalid
	}
	if score > s.threshold {
		return input, FinishReport(&Report{
			Action:     ActionBlock,
			Validator:  s.name,
			Reason:     "semantic match above threshold",
			Score:      score,
			ShadowMode: s.shadow,
		}, ControlSpec{Action: ActionBlock}), nil
	}
	return input, FinishReport(&Report{
		Action: ActionPass, Validator: s.name,
	}, ControlSpec{Action: ActionPass}), nil
}
