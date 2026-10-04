package guardy

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

type latencySink struct {
	start time.Time
	first time.Duration
	wrote bool
}

func (s *latencySink) Write(p []byte) (int, error) {
	if !s.wrote {
		s.first, s.wrote = time.Since(s.start), true
	}
	return len(p), nil
}

// BenchmarkStreamRelease measures transport split effects without a semantic
// detector or real network. It is not a bound on live-provider latency.
//
//nolint:gocognit // Benchmark matrix keeps measurements and failure checks local to each profile/split.
func BenchmarkStreamRelease(b *testing.B) {
	input := []byte(strings.Repeat("benign 😊 unit\n", 4096))
	for _, profile := range []ReleaseProfile{ReleaseWholeResponse, ReleaseValidatedUnits, ReleaseBestEffort} {
		for _, split := range []int{1, 17, 1024, len(input)} {
			b.Run(fmt.Sprintf("%s/split-%d", profile, split), func(b *testing.B) {
				// Arrange.
				calls := 0
				rule := WithStreamingCapabilities(
					ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
						calls++
						return value, nil, nil
					}),
					StreamCapabilities{Partial: true, Unit: true, Final: true},
				)
				cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
				cfg.Profile = profile
				cfg.MaxInputBytes, cfg.MaxOutputBytes = int64(len(input)), int64(len(input))
				cfg.MaxPendingBytes = len(input)
				cfg.MaxUnitBytes = 1024
				if profile == ReleaseWholeResponse {
					cfg.MaxUnitBytes = len(input)
				}
				var firstTotal time.Duration
				b.ReportAllocs()
				b.SetBytes(int64(len(input)))
				b.ResetTimer()
				for range b.N {
					sink := &latencySink{start: time.Now(), first: 0, wrote: false}
					stream, err := CompileStream(sink, cfg)
					if err != nil {
						b.Fatal(err)
					}
					// Act.
					for offset := 0; offset < len(input); offset += split {
						if _, err = stream.Write(input[offset:min(offset+split, len(input))]); err != nil {
							b.Fatal(err)
						}
					}
					if _, err = stream.Complete(context.Background()); err != nil {
						b.Fatal(err)
					}
					// Assert / measure.
					if !sink.wrote {
						b.Fatal("missing delivery")
					}
					firstTotal += sink.first
				}
				b.ReportMetric(float64(calls)/float64(b.N), "validations/op")
				b.ReportMetric(float64(firstTotal.Nanoseconds())/float64(b.N), "first-byte-ns/op")
			})
		}
	}
}
