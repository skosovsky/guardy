package guardy

import (
	"bytes"
	"errors"
	"strconv"
	"testing"
)

func TestJSONFramingCategoriesAndStickyPrefixAcrossPartitions(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		limit       int
		category    StreamCategory
		cause       error
		prefix      string
	}{
		{name: "scalar", input: "42", limit: 16, category: StreamMalformed, cause: ErrInvalidStreamUnit},
		{name: "invalid first", input: "!", limit: 16, category: StreamMalformed, cause: ErrInvalidStreamUnit},
		{name: "malformed", input: `{"x":}`, limit: 16, category: StreamMalformed, cause: ErrInvalidStreamUnit},
		{name: "invalid brackets", input: `{]`, limit: 16, category: StreamMalformed, cause: ErrInvalidStreamUnit},
		{name: "unfinished", input: `{"x":`, limit: 16, category: StreamIncomplete, cause: ErrIncompleteStreamUnit},
		{name: "exact", input: `{"x":1}`, limit: 7, category: StreamSuccess, prefix: `{"x":1}`},
		{name: "over", input: `{"x":1} `, limit: 7, category: StreamLimit, cause: ErrStreamUnitLimit},
		{name: "malformed beyond budget", input: `{"xxxx":1]`, limit: 7, category: StreamLimit, cause: ErrStreamUnitLimit},
		{name: "malformed at budget", input: `{"x":]`, limit: 6, category: StreamMalformed, cause: ErrInvalidStreamUnit},
		{name: "invalid after leading space budget", input: "       !", limit: 7, category: StreamLimit, cause: ErrStreamUnitLimit},
		{name: "incomplete over", input: `{"x":123`, limit: 7, category: StreamLimit, cause: ErrStreamUnitLimit},
		{name: "released then scalar", input: "{}42", limit: 16, category: StreamMalformed, cause: ErrInvalidStreamUnit, prefix: "{}"},
		{name: "released then malformed", input: `{}{"x":}`, limit: 16, category: StreamMalformed, cause: ErrInvalidStreamUnit, prefix: "{}"},
		{name: "released then unfinished", input: `{}{"x":`, limit: 16, category: StreamIncomplete, cause: ErrIncompleteStreamUnit, prefix: "{}"},
	} {
		for i, partition := range releasePartitions(tc.input) {
			t.Run(tc.name+"/"+strconv.Itoa(i), func(t *testing.T) {
				// Arrange.
				var sink bytes.Buffer
				cfg := testStreamConfig(NewPipeline[string]())
				cfg.Delivery = NewDeliveryPolicy(
					"internal",
					WithDeliveryAllowedKinds(PayloadTechnicalPayload, PayloadSafeUserText),
				)
				cfg.Profile, cfg.JSONValues, cfg.MaxUnitBytes = ReleaseValidatedUnits, true, tc.limit
				var last StreamEvent
				cfg.Observer = func(e StreamEvent) { last = e }
				s, err := CompileStream(&sink, cfg)
				if err != nil {
					t.Fatal(err)
				}
				// Act.
				err = writePartition(t.Context(), s, partition)
				outcome := s.Outcome()
				_, secondErr := s.Write([]byte("{}"))
				final, completeErr := s.Complete(t.Context())
				// Assert: category, cause and actual prefix do not depend on transport partitioning.
				if outcome.Category != tc.category || sink.String() != tc.prefix ||
					outcome.ReleasedBytes != int64(len(tc.prefix)) ||
					outcome != final ||
					last.Outcome != outcome {
					t.Fatalf(
						"partition=%v out=%+v final=%+v last=%+v sink=%q err=%v",
						partition,
						outcome,
						final,
						last,
						sink.String(),
						err,
					)
				}
				if tc.cause != nil {
					if !errors.Is(err, tc.cause) || !errors.Is(secondErr, err) || !errors.Is(completeErr, err) ||
						!outcome.Decision.IsSystemFault() {
						t.Fatalf("unstable errors %v %v %v", err, secondErr, completeErr)
					}
				} else if err != nil || completeErr != nil {
					t.Fatalf("unexpected error %v/%v", err, completeErr)
				}
			})
		}
	}
}
