package downstream

import (
	"context"
	"errors"
	"io"

	"github.com/skosovsky/prompty"

	"github.com/skosovsky/guardy"
)

// TextRoute tells the host why this operation ended. It grants no retry or resume.
type TextRoute string

const (
	TextCompleted   TextRoute = "completed"
	TextIncomplete  TextRoute = "incomplete"
	TextRefusal     TextRoute = "refusal"
	TextPaused      TextRoute = "paused"
	TextToolCalls   TextRoute = "tool_calls"
	TextCanceled    TextRoute = "canceled"
	TextProducer    TextRoute = "producer_failure"
	TextUnsupported TextRoute = "unsupported"
	TextDelivery    TextRoute = "delivery_failure"
)

// TextRelease contains only route and safe processor counters, never raw frames.
type TextRelease struct {
	Route   TextRoute
	Outcome guardy.StreamOutcome
}

// ConsumeTextStream consumes one real producer handle into a fresh guardy release
// processor. Only a successful completed terminal lifecycle calls Complete.
// Non-text content is unsupported. Progressive profiles are explicit in cfg and
// can leave irreversible prefixes. No sink error or terminal state causes replay.
func ConsumeTextStream(
	ctx context.Context,
	stream *prompty.Stream,
	sink io.Writer,
	cfg guardy.StreamConfig,
) (TextRelease, error) {
	if stream != nil {
		defer stream.Close() // Close ownership includes invalid configuration and early stop.
	}
	processor, err := guardy.CompileStream(sink, cfg)
	if err != nil {
		var zero guardy.StreamOutcome
		return TextRelease{Route: TextDelivery, Outcome: zero}, err
	}
	if stream == nil {
		return abortText(processor, TextUnsupported, errors.New("downstream: nil producer"))
	}
	terminal := false
	for chunk, sourceErr := range stream.Events() {
		if sourceErr != nil {
			return abortText(processor, producerRoute(sourceErr), sourceErr)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return abortText(processor, TextCanceled, ctxErr)
		}
		if chunk == nil {
			return abortText(processor, TextUnsupported, prompty.ErrStreamProtocol)
		}
		if chunk.Kind == prompty.StreamTerminal {
			terminal = true
			continue
		}
		for _, part := range chunk.Content {
			text, ok := part.(prompty.TextPart)
			if !ok {
				return abortText(processor, TextUnsupported, errors.New("downstream: text content required"))
			}
			if _, writeErr := processor.WriteContext(ctx, []byte(text.Text)); writeErr != nil {
				return TextRelease{Route: TextDelivery, Outcome: processor.Outcome()}, writeErr
			}
		}
	}
	status := stream.Status()
	if !terminal || status.State != prompty.StreamCompleted || status.Err != nil || status.Result == nil {
		return abortText(processor, producerRoute(status.Err), errors.Join(prompty.ErrStreamProtocol, status.Err))
	}
	route := outcomeRoute(status.Result.Outcome)
	if route != TextCompleted {
		return abortText(processor, route, &prompty.OutcomeError{Outcome: status.Result.Outcome, FinishReason: ""})
	}
	outcome, err := processor.Complete(ctx)
	if err != nil {
		return TextRelease{Route: TextDelivery, Outcome: outcome}, err
	}
	return TextRelease{Route: TextCompleted, Outcome: outcome}, nil
}

func abortText(processor *guardy.StreamProcessor, route TextRoute, cause error) (TextRelease, error) {
	outcome, err := processor.Abort(cause)
	return TextRelease{Route: route, Outcome: outcome}, err
}

func producerRoute(err error) TextRoute {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return TextCanceled
	}
	return TextProducer
}

func outcomeRoute(outcome prompty.Outcome) TextRoute {
	switch outcome {
	case prompty.OutcomeCompleted:
		return TextCompleted
	case prompty.OutcomeIncomplete:
		return TextIncomplete
	case prompty.OutcomeRefusal:
		return TextRefusal
	case prompty.OutcomePaused:
		return TextPaused
	case prompty.OutcomeToolCalls:
		return TextToolCalls
	default:
		return TextUnsupported
	}
}
