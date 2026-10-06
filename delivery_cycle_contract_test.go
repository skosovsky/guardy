package guardy_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	g "github.com/skosovsky/guardy"
)

func TestDeliveryCyclesTerminate(t *testing.T) {
	if mode := os.Getenv("GUARDY_TASK27_CYCLE"); mode != "" {
		checkDeliveryCycleChild(t, mode)
		return
	}
	for _, mode := range []string{"value/user", "type/user", "value/technical", "type/technical"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange.
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDeliveryCyclesTerminate$")
			cmd.Env = append(os.Environ(), "GUARDY_TASK27_CYCLE="+mode)
			// Act.
			output, err := cmd.CombinedOutput()
			// Assert: watchdog kills a looping child, without leaking goroutines.
			if ctx.Err() != nil || err != nil {
				t.Fatalf("mode=%s watchdog=%v child=%v output=%s", mode, ctx.Err(), err, output)
			}
		})
	}
}

func checkDeliveryCycleChild(t *testing.T, mode string) {
	t.Helper()
	// Arrange: each potentially looping classifier runs in its own process.
	var value any
	if strings.HasPrefix(mode, "value") {
		value = &value
	} else {
		type recursivePointer *recursivePointer
		var pointer recursivePointer
		value = pointer
	}
	pipeline := g.NewPipeline[any]()
	// Act.
	var decision g.Decision
	var err error
	var deliverable, leaked bool
	if strings.HasSuffix(mode, "technical") {
		policy := g.NewUserTextPolicy("internal",
			g.WithDeliveryAllowedKinds(g.PayloadSafeUserText, g.PayloadTechnicalPayload))
		delivery, fault := pipeline.GuardDelivery(context.Background(), nil, policy, value)
		decision, err = delivery.Decision, fault
		deliverable, leaked = delivery.Deliverable, delivery.Value != nil
	} else {
		delivery, fault := pipeline.GuardOutput(context.Background(), nil, value)
		decision, err = delivery.Decision, fault
		deliverable, leaked = delivery.Deliverable, delivery.Value != nil
	}
	// Assert.
	var failure *g.PolicyFailure
	var classification *g.DeliveryClassificationError
	if !errors.Is(err, g.ErrDeliveryClassification) || !errors.As(err, &classification) ||
		!errors.Is(err, g.ErrValidatorFailed) || !errors.As(err, &failure) ||
		!failure.Decision.IsSystemFault() || !decision.IsSystemFault() || deliverable || leaked {
		t.Fatalf("cycle not classified as fault: error=%v deliverable=%v", err, deliverable)
	}
}
