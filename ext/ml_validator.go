package ext

import (
	"context"
	"errors"

	"github.com/skosovsky/guardy"
)

// TextClassifier defines a lightweight sync contract for fast-path ML checks.
type TextClassifier interface {
	Classify(text string) (ClassifierResult, error)
}

// ClassifierResult is a normalized output of local text classifiers.
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

func (m *mlValidator) Validate(_ context.Context, input string) (string, *guardy.Report, error) {
	if m.classifier == nil {
		return input, nil, errors.New("ext: ml validator classifier is nil")
	}
	result, err := m.classifier.Classify(input)
	if err != nil {
		return input, nil, err
	}
	if !result.IsViolation {
		return input, passReport(m.cfg), nil
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
