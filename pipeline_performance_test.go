package guardy

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// Benchmarks deliberately use only public contracts so the same workloads can
// be copied to the pre-optimization revision for a toolchain-matched comparison.
func BenchmarkPipelineRunPolicies(b *testing.B) {
	for _, n := range []int{0, 1, 8, 64} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			// Arrange: construction and caller-owned scope are outside warm Run.
			p := performancePolicyPipeline(n)
			ctx, scope := context.Background(), MapScope{}
			b.ReportAllocs()
			b.ResetTimer()
			// Act and Assert.
			for range b.N {
				out, err := p.Run(ctx, scope, "ok")
				if err != nil || out.Output != "ok" || len(out.Reports) != 0 {
					b.Fatalf("result=%+v err=%v", out, err)
				}
			}
		})
	}
}

func BenchmarkPipelineConstruction(b *testing.B) {
	for _, n := range []int{0, 1, 8, 64} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			// Arrange: policy providers are host objects, pipeline construction is measured.
			rules := performancePolicies(n)
			b.ReportAllocs()
			b.ResetTimer()
			// Act and Assert.
			for range b.N {
				p, err := NewPipeline(WithPolicyValidators(rules...))
				if err != nil || p == nil {
					b.Fatal(err)
				}
			}
		})
	}
}

//nolint:gocognit // One table compares distinct execution/error paths with assertions inside the measured invocation.
func BenchmarkPipelineRunAdjacent(b *testing.B) {
	for _, name := range []string{"sequential", "parallel", "middleware", "observation", "block", "error"} {
		b.Run(name, func(b *testing.B) {
			// Arrange.
			failure := errors.New("detector unavailable")
			var options []PipelineOption[string]
			policy := MustPolicyFuncWithScope[string](
				nil,
				func(_ context.Context, input string, _ ExecutionScope) (string, *Report, error) {
					return input, nil, nil
				},
			)
			rule := ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
				switch name {
				case "observation":
					return input, FinishReport(
						&Report{Action: ActionBlock, ShadowMode: true},
						ControlSpec{Action: ActionBlock},
					), nil
				case "block":
					return input, FinishReport(&Report{Action: ActionBlock}, ControlSpec{Action: ActionBlock}), nil
				case "error":
					return input, nil, failure
				default:
					return input, nil, nil
				}
			})
			switch name {
			case "parallel":
				options = append(options, WithParallel(rule))
			case "middleware":
				options = append(options, WithPolicyValidators(policy))
			default:
				options = append(options, WithSequential(rule))
			}
			if name == "observation" {
				options = append(options, WithObserver[string](func(context.Context, GuardEvent) {}))
			}
			p := MustNewPipeline(options...)
			if name == "middleware" {
				p = p.MustUse(func(next Validator[string]) Validator[string] { return next })
			}
			ctx, scope := context.Background(), MapScope{}
			b.ReportAllocs()
			b.ResetTimer()
			// Act and Assert.
			for range b.N {
				out, err := p.Run(ctx, scope, "ok")
				if out.Output != "ok" || (name == "error") != errors.Is(err, failure) {
					b.Fatalf("result=%+v err=%v", out, err)
				}
				if name != "error" && err != nil {
					b.Fatal(err)
				}
				if name == "block" && !out.PolicyDecision().IsTerminal() {
					b.Fatal("block decision lost")
				}
				if name == "observation" && (len(out.Reports) != 1 || !out.Reports[0].IsObservation()) {
					b.Fatal("observation lost")
				}
			}
		})
	}
}

func performancePolicies(n int) []PolicyValidator[string] {
	rules := make([]PolicyValidator[string], n)
	for i := range rules {
		rules[i] = MustPolicyFuncWithScope[string](
			nil,
			func(_ context.Context, input string, _ ExecutionScope) (string, *Report, error) {
				return input, nil, nil
			},
		)
	}
	return rules
}

func performancePolicyPipeline(n int) *Pipeline[string] {
	return MustNewPipeline(WithPolicyValidators(performancePolicies(n)...))
}

func TestPipelineRunAllocationDoesNotScaleWithPolicies(t *testing.T) {
	// Arrange: input and scope are borrowed across repeated invocations.
	ctx, scope := context.Background(), MapScope{}
	var measured [4]float64
	// Act: capture allocations inside Run, with construction outside the measurement.
	for i, n := range []int{0, 1, 8, 64} {
		p := performancePolicyPipeline(n)
		measured[i] = testing.AllocsPerRun(100, func() {
			result, err := p.Run(ctx, scope, "ok")
			if err != nil || result.Output != "ok" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
	// Assert: empty phases do not allocate contexts; policy adapters no longer
	// produce a per-rule allocation slope. Budgets are the task's acceptance gates.
	if measured[0] >= 4 || measured[3] >= 69 {
		t.Fatalf("allocation gates exceeded: 0/1/8/64=%v", measured)
	}
	if measured[1] != measured[2] || measured[2] != measured[3] {
		t.Fatalf("per-policy allocations remain: 0/1/8/64=%v", measured)
	}
}

//nolint:gocognit // Exercise both scoped execution paths and independent output/factory assertions in one race fixture.
func TestPipelineConcurrentScopedReuseWithAndWithoutMiddleware(t *testing.T) {
	for _, middleware := range []bool{false, true} {
		t.Run("middleware="+strconv.FormatBool(middleware), func(t *testing.T) {
			// Arrange: each request binds a distinct scope to the same immutable pipeline.
			const requests = 64
			var factories atomic.Int64
			rule := MustPolicyFuncWithScope[string](
				nil,
				func(ctx context.Context, input string, scope ExecutionScope) (string, *Report, error) {
					phase, ok := ValidationPhaseFromContext(ctx)
					value, found := scope.Lookup("request")
					if !ok || phase != ValidationPhasePolicy || !found || value != input {
						return "", nil, errors.New("request scope or phase crossed invocations")
					}
					return input, nil, nil
				},
			)
			p := MustNewPipeline(WithPolicyValidators(rule))
			if middleware {
				p = p.MustUse(func(next Validator[string]) Validator[string] {
					factories.Add(1)
					return next
				})
				factories.Store(0) // Use checks construction separately from invocations.
			}
			failures := make(chan error, requests)
			var wg sync.WaitGroup
			// Act.
			for i := range requests {
				wg.Go(func() {
					input := strconv.Itoa(i)
					result, err := p.Run(context.Background(), MapScope{"request": input}, input)
					if err != nil {
						failures <- err
					} else if result.Output != input {
						failures <- errors.New("wrong request output")
					}
				})
			}
			wg.Wait()
			close(failures)
			// Assert.
			for err := range failures {
				t.Error(err)
			}
			if middleware && factories.Load() != requests {
				t.Fatalf("scoped factory calls=%d want=%d", factories.Load(), requests)
			}
		})
	}
}
