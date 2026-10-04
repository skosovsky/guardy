package ext

import (
	"context"
	"errors"
	"math"

	"github.com/skosovsky/guardy"
)

// TextClassifier is a synchronous, caller-supplied detector contract.
// Implementations must cooperate with context cancellation. The adapter does not
// start background goroutines or impose a provider/model SDK.
type TextClassifier interface {
	Classify(ctx context.Context, text string) (ClassifierResult, error)
}

// ClassifierResult contains a detector verdict and a score on its own finite scale.
// IsViolation determines the decision; Score is not assumed to be a probability.
// NaN and infinities are errors even when IsViolation is false.
type ClassifierResult struct {
	IsViolation bool
	Score       float64
	Label       string
}

type mlValidator struct {
	classifier TextClassifier
	cfg        RuleConfig
}

// Ensure ML validator implements guardy.Validator[string].
var _ guardy.Validator[string] = (*mlValidator)(nil)

const defaultMLValidatorName = "ml_validator"

// NewMLValidator adapts TextClassifier to guardy.Validator[string].
func NewMLValidator(classifier TextClassifier, opts ...Option) guardy.Validator[string] {
	cfg := applyOptions(RuleConfig{
		Action:   guardy.ActionBlock,
		Severity: guardy.SeverityHigh,
		Name:     defaultMLValidatorName,
	}, opts...)
	cfg.Action = guardy.ActionBlock
	return &mlValidator{classifier: classifier, cfg: cfg}
}

func (m *mlValidator) Validate(ctx context.Context, input string) (string, *guardy.Report, error) {
	if err := ctx.Err(); err != nil {
		return input, nil, err
	}
	if m.classifier == nil {
		return input, nil, errors.New("ext: ml validator classifier is nil")
	}
	result, err := m.classifier.Classify(ctx, input)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return input, nil, ctxErr
	}
	if err != nil {
		return input, nil, err
	}
	if math.IsNaN(result.Score) || math.IsInf(result.Score, 0) {
		return input, nil, errors.New("ext: classifier score must be finite")
	}
	if !result.IsViolation {
		rep := passReport(m.cfg)
		rep.Score = result.Score
		return input, rep, nil
	}
	reason := "ml violation detected"
	if result.Label != "" {
		reason = "ml violation: " + result.Label
	}
	rep := violationReport(m.cfg, guardy.ActionBlock, reason)
	rep.Score = result.Score
	if rep.Code == "" {
		rep.Code = result.Label
	}
	return input, rep, nil
}
