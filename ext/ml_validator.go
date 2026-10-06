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

// ClassifierValidator adapts a caller-owned detector; it contains no trained model.
type ClassifierValidator struct {
	classifier TextClassifier
	cfg        RuleConfig
}

// Ensure ClassifierValidator implements guardy.Validator[string].
var _ guardy.Validator[string] = (*ClassifierValidator)(nil)

const defaultClassifierValidatorName = "classifier_validator"

// NewClassifierValidator adapts TextClassifier to guardy.Validator[string].
func NewClassifierValidator(classifier TextClassifier, opts ...Option) (guardy.Validator[string], error) {
	if nilComponent(classifier) {
		return nil, ruleConfigurationError("classifier", "detector", "required", nil)
	}
	cfg, err := applyOptions("classifier", RuleConfig{
		Action:   guardy.ActionBlock,
		Severity: guardy.SeverityHigh,
		Name:     defaultClassifierValidatorName,
	}, opts...)
	if err != nil {
		return nil, err
	}
	if err := validateRuleOptions("classifier", cfg); err != nil {
		return nil, err
	}

	return &ClassifierValidator{classifier: classifier, cfg: cfg}, nil
}

// MustClassifierValidator panics when NewClassifierValidator rejects configuration.
func MustClassifierValidator(classifier TextClassifier, opts ...Option) guardy.Validator[string] {
	v, err := NewClassifierValidator(classifier, opts...)
	if err != nil {
		panic(err)
	}
	return v
}

func (m *ClassifierValidator) Validate(ctx context.Context, input string) (string, *guardy.Report, error) {
	if err := ctx.Err(); err != nil {
		return input, nil, err
	}
	if m.classifier == nil {
		return input, nil, errors.New("ext: classifier validator detector is nil")
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
	reason := "classifier violation detected"
	if result.Label != "" {
		reason = "classifier violation: " + result.Label
	}
	rep := violationReport(m.cfg, guardy.ActionBlock, reason)
	rep.Score = result.Score
	if rep.Code == "" {
		rep.Code = result.Label
	}
	return input, rep, nil
}
