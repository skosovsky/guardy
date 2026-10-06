package guardy

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestStreamNewlineTailBudgetAcrossPartitions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, input string
		success     bool
	}{
		{"smaller_tail", "abc", true},
		{"exact_tail", "abcd", true},
		{"exact_newline", "abc\n", true},
		{"extra_byte", "abcde", false},
		{"extra_newline", "abcd\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, pending := range []int{4, 32} {
				for _, chunks := range boundaryPartitions(tc.input) {
					// Arrange.
					cfg := testStreamConfig(MustNewPipeline[string]())
					cfg.Profile = ReleaseValidatedUnits
					cfg.MaxUnitBytes = 4
					cfg.MaxPendingBytes = pending
					var sink bytes.Buffer
					stream, err := CompileStream(&sink, cfg)
					if err != nil {
						t.Fatal(err)
					}
					// Act.
					err = writePartition(context.Background(), stream, chunks)
					outcome := stream.Outcome()
					// Assert.
					if tc.success {
						if err != nil || outcome.Category != StreamSuccess ||
							sink.String() != tc.input || outcome.Sequence != 1 {
							t.Fatalf("chunks=%q outcome=%+v error=%v sink=%q", chunks, outcome, err, sink.String())
						}
					} else if !errors.Is(err, ErrStreamUnitLimit) || outcome.Category != StreamLimit || sink.Len() != 0 {
						t.Fatalf("chunks=%q outcome=%+v error=%v sink=%q", chunks, outcome, err, sink.String())
					}
					if outcome.PeakPendingBytes > 4 {
						t.Fatalf("pending=%d", outcome.PeakPendingBytes)
					}
				}
			}
		})
	}
}

func TestStreamOverBudgetPartitionsMayReleaseDifferentPrefixes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		chunks []string
		output string
	}{
		{"single_write", []string{"a\nb\n"}, ""},
		{"two_writes", []string{"a\n", "b\n"}, "a\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange.
			cfg := testStreamConfig(MustNewPipeline[string]())
			cfg.Profile = ReleaseValidatedUnits
			cfg.MaxInputBytes = 3
			cfg.MaxUnitBytes = 2
			cfg.MaxPendingBytes = 2
			var sink bytes.Buffer
			stream, err := CompileStream(&sink, cfg)
			if err != nil {
				t.Fatal(err)
			}
			// Act.
			err = writePartition(t.Context(), stream, tc.chunks)
			outcome := stream.Outcome()
			// Assert: oversized Write is rejected atomically, but prior delivery is irreversible.
			if err == nil || outcome.Category != StreamLimit || sink.String() != tc.output ||
				outcome.ReleasedBytes != int64(len(tc.output)) {
				t.Fatalf("outcome=%+v error=%v sink=%q", outcome, err, sink.String())
			}
		})
	}
}

// boundaryPartitions adds every nonempty composition of these short boundary inputs.
func boundaryPartitions(input string) [][]string {
	partitions := releasePartitions(input)
	partitionCount := 1 << (len(input) - 1)
	for mask := range partitionCount {
		start := 0
		var chunks []string
		for cut := 1; cut < len(input); cut++ {
			if mask&(1<<(cut-1)) != 0 {
				chunks = append(chunks, input[start:cut])
				start = cut
			}
		}
		chunks = append(chunks, input[start:])
		partitions = append(partitions, chunks)
	}
	return partitions
}

func TestStreamExactTailWaitsForCompleteOnce(t *testing.T) {
	t.Parallel()
	for _, chunks := range boundaryPartitions("abcd") {
		// Arrange.
		calls := 0
		rule := WithStreamingCapabilities(
			ValidatorFunc[string](
				func(_ context.Context, input string) (string, *Report, error) { calls++; return input, nil, nil },
			),
			StreamCapabilities{Unit: true},
		)
		cfg := testStreamConfig(MustNewPipeline(WithSequential(rule)))
		cfg.Profile = ReleaseValidatedUnits
		cfg.MaxUnitBytes = 4
		cfg.MaxPendingBytes = 4
		var sink bytes.Buffer
		stream, err := CompileStream(&sink, cfg)
		if err != nil {
			t.Fatal(err)
		}
		// Act / Assert: filling the exact-limit tail is not trusted completion.
		for _, chunk := range chunks {
			_, err = stream.WriteContext(t.Context(), []byte(chunk))
			if err != nil || calls != 0 || sink.Len() != 0 {
				t.Fatalf("before Complete chunks=%q calls=%d sink=%q err=%v", chunks, calls, sink.String(), err)
			}
		}
		// Act.
		_, firstErr := stream.Complete(t.Context())
		_, againErr := stream.Complete(t.Context())
		// Assert: completion is sticky and does not validate/deliver the tail twice.
		if firstErr != nil || againErr != nil || calls != 1 || sink.String() != "abcd" {
			t.Fatalf("chunks=%q calls=%d sink=%q errors=%v/%v", chunks, calls, sink.String(), firstErr, againErr)
		}
	}
}
