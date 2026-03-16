package guardy

import (
	"context"
	"errors"
)

var errSemanticMatcherNil = errors.New("guardy: semantic matcher is nil")

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
func NewSemanticValidator(m Matcher, threshold float64, shadow bool) *SemanticValidator {
	return &SemanticValidator{matcher: m, threshold: threshold, shadow: shadow, name: "semantic"}
}

// Validate runs the matcher and returns block when score > threshold.
func (s *SemanticValidator) Validate(ctx context.Context, input string) (string, *Report, error) {
	if s.matcher == nil {
		return "", nil, errSemanticMatcherNil
	}
	score, err := s.matcher.Match(ctx, input)
	if err != nil {
		return input, nil, err
	}
	if score > s.threshold {
		return input, &Report{
			Action:     ActionBlock,
			Validator:  s.name,
			Reason:     "semantic match above threshold",
			Score:      score,
			ShadowMode: s.shadow,
		}, nil
	}
	return input, &Report{Action: ActionPass, Validator: s.name}, nil
}
