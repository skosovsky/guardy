package guardy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

// Transport partitions intentionally do not compare ReceivedBytes or the pending
// high-water mark: a failed large Write can accept more bytes than a small Write.
func releasePartitions(value string) [][]string {
	partitions := [][]string{{value}}
	for split := 0; split <= len(value); split++ {
		partitions = append(partitions, []string{value[:split], value[split:]})
	}
	for _, size := range []int{1, 3, 7} {
		var chunks []string
		for offset := 0; offset < len(value); offset += size {
			chunks = append(chunks, value[offset:min(offset+size, len(value))])
		}
		partitions = append(partitions, chunks)
	}
	return partitions
}

func writePartition(ctx context.Context, stream *StreamProcessor, chunks []string) error {
	for _, chunk := range chunks {
		if _, err := stream.WriteContext(ctx, []byte(chunk)); err != nil {
			return err
		}
	}
	_, err := stream.Complete(ctx)
	return err
}

func TestReleasePartitionFramingAndUnitLimits(t *testing.T) {
	jsonFirst := " {\"text\":\"Привет \\u263a \\\" \\\\ [}]\",\"nested\":[{}]}\r\n\t"
	for _, test := range []struct {
		name     string
		input    string
		units    []string
		json     bool
		limit    int
		category StreamCategory
	}{
		{"newline_unicode", "Привет\n😊\ntail", []string{"Привет\n", "😊\n", "tail"}, false, 64, StreamSuccess},
		{"newline_exact", "abc\n", []string{"abc\n"}, false, 4, StreamSuccess},
		{"newline_over", "abcd\n", nil, false, 4, StreamLimit},
		{"json_escapes_and_trailing", jsonFirst + "[1,{\"x\":true}]  ", []string{jsonFirst, "[1,{\"x\":true}]  "}, true, 128, StreamSuccess},
		{"json_exact", `{"x":1}`, []string{`{"x":1}`}, true, 7, StreamSuccess},
		{"json_exact_with_lookahead", "{\"x\":1}[]", []string{`{"x":1}`, "[]"}, true, 7, StreamSuccess},
		{"json_exact_with_trailing", "{\"x\":1} ", nil, true, 7, StreamLimit},
		{"json_incomplete", `{"x":`, nil, true, 16, StreamIncomplete},
		{"json_invalid_syntax", `{"x":}`, nil, true, 16, StreamMalformed},
		{"json_whitespace_over", strings.Repeat(" ", 17), nil, true, 16, StreamLimit},
	} {
		t.Run(test.name, func(t *testing.T) { checkReleasePartitionFramingAndUnitLimits(t, test) })
	}
}

func TestReleasePartitionRedactionOutputBudget(t *testing.T) {
	for _, budget := range []int64{7, 8} {
		for partition, chunks := range releasePartitions("a\nb\n") {
			// Arrange: the transformed UTF-8 bytes exceed the source byte count.
			rule := WithStreamingCapabilities(
				ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
					return strings.ReplaceAll(
						strings.ReplaceAll(value, "a", "界"),
						"b",
						"界",
					), &Report{
						Action: ActionRedact,
					}, nil
				}),
				StreamCapabilities{Unit: true},
			)
			cfg := testStreamConfig(MustNewPipeline(WithSequential(rule)))
			cfg.Profile, cfg.MaxOutputBytes = ReleaseValidatedUnits, budget
			var sink bytes.Buffer
			stream, err := CompileStream(&sink, cfg)
			if err != nil {
				t.Fatal(err)
			}
			// Act.
			err = writePartition(context.Background(), stream, chunks)
			outcome := stream.Outcome()
			// Assert: one approved prefix remains irreversible when expansion exceeds the budget.
			want, category, sequence := "界\n界\n", StreamSuccess, uint64(2)
			if budget == 7 {
				want, category, sequence = "界\n", StreamLimit, 1
			}
			if sink.String() != want || outcome.Category != category || outcome.Sequence != sequence ||
				outcome.ReleasedBytes != int64(len(want)) || (err == nil) != (category == StreamSuccess) {
				t.Fatalf(
					"budget=%d partition=%d output=%q outcome=%+v err=%v",
					budget,
					partition,
					sink.String(),
					outcome,
					err,
				)
			}
		}
	}
}

func TestReleasePartitionFaultCancelAndFallbackAfterPrefix(t *testing.T) {
	for _, mode := range []string{"fault", "cancel", "deny"} {
		for partition, chunks := range releasePartitions("ok\nbad\nlast\n") {
			checkPartitionFault(t, mode, partition, chunks)
		}
	}
}

func TestReleasePartitionAbortAndCloseAfterPrefix(t *testing.T) {
	for _, closeStream := range []bool{false, true} {
		for partition, chunks := range releasePartitions("ok\npending") {
			checkPartitionAbort(t, closeStream, partition, chunks)
		}
	}
}

func TestReleasePartitionShortTransportAfterFraming(t *testing.T) {
	for partition, chunks := range releasePartitions("ok\nnext\n") {
		// Arrange: the first framed unit encounters a short downstream write.
		cfg := testStreamConfig(MustNewPipeline[string]())
		cfg.Profile = ReleaseValidatedUnits
		stream, err := CompileStream(shortPartitionSink{}, cfg)
		if err != nil {
			t.Fatal(err)
		}
		// Act.
		err = writePartition(context.Background(), stream, chunks)
		outcome := stream.Outcome()
		_, late := stream.Write([]byte("late"))
		// Assert: attempted units and actually released bytes stay stable across transport partitions.
		if !errors.Is(err, io.ErrShortWrite) || !errors.Is(late, err) || outcome.Category != StreamTransport ||
			outcome.Sequence != 1 || outcome.ReleasedBytes != 1 || !outcome.Terminal {
			t.Fatalf("partition=%d outcome=%+v err=%v", partition, outcome, err)
		}
	}
}

type shortPartitionSink struct{}

func (shortPartitionSink) Write(value []byte) (int, error) { return len(value) / 2, nil }

func checkReleasePartitionFramingAndUnitLimits(t *testing.T, test struct {
	name     string
	input    string
	units    []string
	json     bool
	limit    int
	category StreamCategory
}) {
	t.Helper()
	for partition, chunks := range releasePartitions(test.input) {
		// Arrange: the validator records the complete original units it sees.
		var units []string
		rule := WithStreamingCapabilities(
			ValidatorFunc[string](func(ctx context.Context, value string) (string, *Report, error) {
				if StreamValidationStage(ctx) != StreamUnit {
					t.Fatal("unit validator received another stage")
				}
				units = append(units, value)
				return value, nil, nil
			}),
			StreamCapabilities{Unit: true},
		)
		cfg := testStreamConfig(MustNewPipeline(WithSequential(rule)))
		cfg.Profile, cfg.JSONValues = ReleaseValidatedUnits, test.json
		cfg.Delivery = NewUserTextPolicy(
			"internal",
			WithDeliveryAllowedKinds(PayloadTechnicalPayload, PayloadSafeUserText),
		)
		cfg.MaxUnitBytes, cfg.MaxPendingBytes = test.limit, test.limit+1
		var sink bytes.Buffer
		stream, err := CompileStream(&sink, cfg)
		if err != nil {
			t.Fatal(err)
		}
		// Act.
		err = writePartition(context.Background(), stream, chunks)
		outcome := stream.Outcome()
		// Assert: boundaries and errors are independent of transport chunks.
		if outcome.Category != test.category || (err == nil) != (test.category == StreamSuccess) ||
			!reflect.DeepEqual(units, test.units) || sink.String() != strings.Join(test.units, "") ||
			outcome.Sequence != uint64(len(test.units)) || outcome.ReleasedBytes != int64(sink.Len()) ||
			!outcome.Terminal || outcome.PeakPendingBytes > cfg.MaxPendingBytes {
			t.Fatalf(
				"partition=%d units=%q output=%q outcome=%+v err=%v",
				partition,
				units,
				sink.String(),
				outcome,
				err,
			)
		}
		if test.category == StreamSuccess && outcome.Decision.Action != ActionPass {
			t.Fatalf("partition=%d success decision=%+v", partition, outcome.Decision)
		}
	}
}

func checkPartitionFault(t *testing.T, mode string, partition int, chunks []string) {
	t.Helper()
	// Arrange: the second unit fails after an irreversible approved first unit.
	ctx, cancel := context.WithCancel(context.Background())
	var units []string
	rule := WithStreamingCapabilities(
		ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
			units = append(units, value)
			if value != "bad\n" {
				return value, nil, nil
			}
			switch mode {
			case "cancel":
				cancel()
				return value, nil, nil
			case "deny":
				return value, &Report{Action: ActionBlock, Code: "UNIT_DENY"}, nil
			default:
				return value, &Report{
					Action:      ActionPass,
					Disposition: DispositionSystemFault,
					Code:        "UNIT_FAULT",
				}, nil
			}
		}),
		StreamCapabilities{Unit: true},
	)
	cfg := testStreamConfig(MustNewPipeline(WithSequential(rule)))
	cfg.Profile = ReleaseValidatedUnits
	cfg.Delivery.Fallback = "safe\n"
	var sink bytes.Buffer
	stream, err := CompileStream(&sink, cfg)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	// Act.
	err = writePartition(ctx, stream, chunks)
	cancel()
	original := stream.Outcome()
	_, late := stream.Write([]byte("late\n"))
	again, repeated := stream.Complete(context.Background())
	fallback, fallbackErr := stream.DeliverFallback(context.Background())
	_, secondFallback := stream.DeliverFallback(context.Background())
	// Assert: terminal source delivery stays sticky; only policy deny can use a checked fallback.
	category := StreamFault
	switch mode {
	case "cancel":
		category = StreamTimeout
	case "deny":
		category = StreamBlocked
	}
	wantOutput, wantUnits := "ok\n", []string{"ok\n", "bad\n"}
	if mode == "deny" {
		wantOutput, wantUnits = "ok\nsafe\n", []string{"ok\n", "bad\n", "safe\n"}
		if fallbackErr != nil || !fallback.Fallback || fallback.Category != StreamSuccess ||
			fallback.ReleasedBytes != 5 {
			t.Fatalf("partition=%d fallback=%+v err=%v", partition, fallback, fallbackErr)
		}
	} else if fallbackErr == nil || !original.Decision.IsSystemFault() {
		t.Fatalf("partition=%d fault fallback accepted: %+v %v", partition, original, fallbackErr)
	}
	if err == nil || !errors.Is(late, err) || !errors.Is(repeated, err) || original != again ||
		stream.Outcome() != original || original.Category != category || !original.Terminal ||
		original.Sequence != 1 || original.ReleasedBytes != 3 || secondFallback == nil ||
		!reflect.DeepEqual(units, wantUnits) || sink.String() != wantOutput {
		t.Fatalf(
			"partition=%d mode=%s units=%q output=%q outcome=%+v err=%v",
			partition,
			mode,
			units,
			sink.String(),
			original,
			err,
		)
	}
}

func checkPartitionAbort(t *testing.T, closeStream bool, partition int, chunks []string) {
	t.Helper()
	// Arrange.
	cfg := testStreamConfig(MustNewPipeline[string]())
	cfg.Profile = ReleaseValidatedUnits
	var sink bytes.Buffer
	stream, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range chunks {
		if _, err = stream.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	// Act.
	if closeStream {
		err = stream.Close()
	} else {
		_, err = stream.Abort(io.ErrUnexpectedEOF)
	}
	original := stream.Outcome()
	again, completeErr := stream.Complete(context.Background())
	_, late := stream.Write([]byte("\n"))
	// Assert: abort retains only bytes that were validated before the incomplete tail.
	if err == nil || !errors.Is(completeErr, err) || !errors.Is(late, err) || original != again ||
		original.Category != StreamIncomplete || original.Sequence != 1 || original.ReleasedBytes != 3 ||
		sink.String() != "ok\n" {
		t.Fatalf(
			"partition=%d close=%v outcome=%+v output=%q err=%v",
			partition,
			closeStream,
			original,
			sink.String(),
			err,
		)
	}
}
