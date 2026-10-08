package integration_test

import (
	"context"
	"encoding/json"

	g "github.com/skosovsky/guardy"
)

type contractJudge struct{ report g.Report }

func (j contractJudge) Evaluate(context.Context, string) (g.Report, error) { return j.report, nil }

type contractDocument struct {
	Text string
	Raw  json.RawMessage
}
