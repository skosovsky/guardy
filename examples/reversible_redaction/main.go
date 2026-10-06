// Reversible redaction: the host authorizes a recipient before restoring vault mappings.
// A separate final output guard checks the restored payload before delivery.
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

const maxRecipientOutputLength = 500

func main() {
	vault := ext.NewInMemoryTokenVault()

	piiValidator := ext.MustPIIValidator(
		ext.WithAction(guardy.ActionRedact),
		ext.WithTokenVault(vault),
		ext.WithCode("PII_DETECTED"),
		ext.WithSeverity(guardy.SeverityHigh),
	)
	wordlistValidator := ext.MustWordlistValidator(
		[]string{"acme"},
		ext.Blocklist,
		ext.WithAction(guardy.ActionRedact),
		ext.WithLowercase(true),
		ext.WithTokenVault(vault),
		ext.WithCode("CONFIDENTIAL_TERM"),
		ext.WithSeverity(guardy.SeverityMedium),
	)

	pipeline := guardy.NewPipeline(guardy.WithFastPath(piiValidator, wordlistValidator))
	input := "Contact alice@example.com. Internal customer: ACME."
	result, err := pipeline.Run(context.Background(), nil, input)
	if err != nil {
		panic(err)
	}

	redacted := result.Output
	llmAnswer := "Approved summary: " + redacted
	// Caller-owned facts/decision: a token never grants permission to disclose.
	const ownerID = "customer-42"
	final := guardy.NewPipeline(guardy.WithFastPath(ext.MustLengthValidator(0, maxRecipientOutputLength)))

	for _, recipientID := range []string{"external-reader", ownerID} {
		checked, err := restoreForRecipient(context.Background(), recipientID, ownerID, llmAnswer, vault, final)
		if err != nil {
			fmt.Println("recipient output rejected:", recipientID, err)
			continue
		}
		if deliverable, ok := checked.DeliverableValue(); ok {
			fmt.Println("authorized recipient:", recipientID)
			fmt.Println("delivered:", deliverable)
		}
	}

	fmt.Println("redacted:", redacted)
}

// restoreForRecipient is example host code. The authenticated IDs must come from
// trusted host facts; guardy tokens and model output cannot supply them.
func restoreForRecipient(
	ctx context.Context,
	recipientID, ownerID, answer string,
	vault ext.TokenVault,
	final *guardy.Pipeline[string],
) (guardy.GuardedDelivery[string], error) {
	if err := ctx.Err(); err != nil {
		return guardy.GuardedDelivery[string]{}, err
	}
	if ownerID == "" || recipientID != ownerID {
		return guardy.GuardedDelivery[string]{}, errors.New("recipient not authorized")
	}
	if final == nil {
		return guardy.GuardedDelivery[string]{}, errors.New("final output guard required")
	}
	restored := ext.UnredactText(answer, vault)
	return final.GuardOutput(ctx, nil, restored)
}
