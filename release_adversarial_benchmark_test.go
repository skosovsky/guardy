package guardy

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

type adversarialStreamCase struct {
	name             string
	input            []byte
	json, unfinished bool
}

func adversarialStreamCases(size int) []adversarialStreamCase {
	prefix, suffix := `{"nested":[{"value":"`, `"}]}`
	// Each repeated fragment contains a JSON escaped quote and backslash.
	fragment := `a\"b\\c`
	body := strings.Repeat(fragment, (size-len(prefix)-len(suffix))/len(fragment))
	return []adversarialStreamCase{
		{name: "long-newline", input: []byte(strings.Repeat("x", size-1) + "\n"), json: false, unfinished: false},
		{name: "nested-json", input: []byte(prefix + body + suffix), json: true, unfinished: false},
		{name: "unfinished-json", input: []byte(prefix + body), json: true, unfinished: true},
		{name: "trailing-whitespace", input: []byte("{}" + strings.Repeat(" ", size-2)), json: true, unfinished: false},
		{name: "many-short-units", input: []byte(strings.Repeat("x\n", size/2)), json: false, unfinished: false},
	}
}

// BenchmarkStreamAdversarial isolates single-unit framing from the complete
// processor. The full path additionally pays buffering, validation, scope,
// timeout and transport costs; many-short-units exposes buffer consumption.
//
//nolint:gocognit,nestif // The benchmark matrix keeps framing and terminal assertions together.
func BenchmarkStreamAdversarial(b *testing.B) {
	for _, size := range []int{1024, 8192, 65536} {
		for _, tc := range adversarialStreamCases(size) {
			for _, split := range []int{1, 17, len(tc.input)} {
				for _, layer := range []string{"framing", "processor"} {
					if layer == "framing" && tc.name == "many-short-units" {
						continue
					}
					b.Run(fmt.Sprintf("%s/%s/size-%d/split-%d", layer, tc.name, size, split), func(b *testing.B) {
						// Arrange.
						cfg := testStreamConfig(NewPipeline[string]())
						cfg.Delivery = NewUserTextPolicy(
							"internal",
							WithDeliveryAllowedKinds(PayloadTechnicalPayload, PayloadSafeUserText),
						)
						cfg.Profile = ReleaseValidatedUnits
						cfg.JSONValues = tc.json
						cfg.MaxInputBytes = int64(len(tc.input))
						cfg.MaxOutputBytes = int64(len(tc.input))
						cfg.MaxUnitBytes = len(tc.input) + 1
						cfg.MaxPendingBytes = len(tc.input) + 2
						var peakTotal, visitedTotal, copiedTotal uint64
						b.ReportAllocs()
						b.SetBytes(int64(len(tc.input)))
						b.ResetTimer()
						for range b.N {
							stream, err := CompileStream(io.Discard, cfg)
							if err != nil {
								b.Fatal(err)
							}
							// Act.
							for offset := 0; offset < len(tc.input); offset += split {
								chunk := tc.input[offset:min(offset+split, len(tc.input))]
								if layer == "framing" {
									stream.pending.append(chunk, cfg.MaxPendingBytes)
									_, _, err = stream.nextUnit(false)
								} else {
									_, err = stream.Write(chunk)
								}
								if err != nil {
									b.Fatal(err)
								}
							}
							// Assert. An unfinished JSON must never deliver and must fail Complete.
							if layer == "framing" {
								peakTotal += uint64(len(stream.pending.data))
								n, _, finalErr := stream.nextUnit(true)
								if tc.unfinished {
									if finalErr == nil {
										b.Fatal("unfinished framing accepted")
									}
								} else if finalErr != nil || n != len(tc.input) {
									b.Fatalf("framing: n=%d err=%v", n, finalErr)
								}
							} else {
								outcome, completeErr := stream.Complete(context.Background())
								if tc.unfinished {
									if completeErr == nil || outcome.Category != StreamIncomplete ||
										outcome.ReleasedBytes != 0 {
										b.Fatalf("unfinished outcome: %+v, %v", outcome, completeErr)
									}
								} else if completeErr != nil || outcome.ReleasedBytes != int64(len(tc.input)) {
									b.Fatalf("delivery: %+v, %v", outcome, completeErr)
								}
								if outcome.PeakPendingBytes > cfg.MaxPendingBytes {
									b.Fatal("pending budget exceeded")
								}
								peakTotal += uint64(outcome.PeakPendingBytes)
							}
							visitedTotal += stream.framer.visited
							copiedTotal += stream.pending.copied
						}
						b.ReportMetric(float64(visitedTotal)/float64(b.N), "scan-byte-visits/op")
						b.ReportMetric(float64(copiedTotal)/float64(b.N), "buffer-copied-B/op")
						b.ReportMetric(float64(peakTotal)/float64(b.N), "peak-pending-B/op")
					})
				}
			}
		}
	}
}

// BenchmarkStreamComponents measures cooperative caller work independently;
// these are illustrative no-op components, not a latency bound for callbacks.
func BenchmarkStreamComponents(b *testing.B) {
	ctx := context.Background()
	value := strings.Repeat("x", 1024)
	pipeline := NewPipeline[string]()
	b.Run("scope", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := ScopeFactory(nil).scope(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("validation", func(b *testing.B) {
		scope := NewScope()
		b.ReportAllocs()
		for range b.N {
			if _, err := pipeline.GuardDelivery(ctx, scope, NewUserTextPolicy("user"), value); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("writer", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(value)))
		for range b.N {
			if _, err := io.WriteString(io.Discard, value); err != nil {
				b.Fatal(err)
			}
		}
	})
}
