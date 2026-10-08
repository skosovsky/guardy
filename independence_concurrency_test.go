package guardy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type invocationFactContextKey struct{}

func TestImmutableConfigurationAndConcurrentRequestLocalBoundaries(t *testing.T) {
	// Arrange: construction copies caller-owned requirement and validator slices.
	key := NewScopeKey[int]("request.number")
	requirements := []ScopeRequirement{key.Requirement()}
	policy := MustPolicyFuncWithScope(
		requirements,
		func(_ context.Context, raw string, scope ExecutionScope) (string, *Report, error) {
			number, ok := key.Lookup(scope)
			if !ok || !strings.Contains(raw, `"name":"`+strconv.Itoa(number)+`"`) {
				return raw, nil, errors.New("request scope mixed")
			}
			return raw, nil, nil
		},
	)
	requirements[0].Key = "mutated.original"
	exposed := policy.RequiredScope()
	exposed[0].Key = "mutated.accessor"
	validators := []PolicyValidator[string]{policy}
	p := MustNewPipeline(WithPolicyValidators(validators...), WithPipelineName[string]("immutable-config"))
	validators[0] = nil
	compiledRequirements := p.RequiredScope()
	compiledRequirements[0].Key = "mutated.pipeline-accessor"
	args := MustCompileArgs[argsCommand](p, WithArgsFinalGuard[argsCommand](p))
	var scopes atomic.Int64
	factory := ScopeFactory(func(ctx context.Context) (ExecutionScope, error) {
		scopes.Add(1)
		number, ok := ctx.Value(invocationFactContextKey{}).(int)
		if !ok {
			return nil, errors.New("missing invocation identity")
		}
		return NewScope(ScopeValue(key, number)), nil
	})
	handler := WrapArgs(
		args,
		factory,
		func(_ context.Context, value argsCommand) (string, error) { return value.Name, nil },
	)
	const invocations = 32
	results := make(chan error, invocations)
	var workers sync.WaitGroup
	// Act: one compiled boundary is shared, but scope and result are invocation-local.
	for number := range invocations {
		workers.Go(func() {
			ctx := context.WithValue(context.Background(), invocationFactContextKey{}, number)
			raw := `{"name":"` + strconv.Itoa(number) + `"}`
			value, boundary, err := handler(ctx, raw)
			if err == nil &&
				(value != strconv.Itoa(number) || boundary.SanitizedRaw != raw || boundary.ConfigurationID != "immutable-config") {
				err = fmt.Errorf("request mixed: %+v", boundary)
			}
			results <- err
		})
	}
	workers.Wait()
	close(results)
	// Assert.
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if scopes.Load() != invocations || p.RequiredScope()[0].Key != key.Name() {
		t.Fatalf("scope count=%d requirements=%+v", scopes.Load(), p.RequiredScope())
	}
}

func TestStreamAndCoverageConfigurationSnapshotOwnership(t *testing.T) {
	// Arrange.
	kinds := []PayloadKind{PayloadSafeUserText}
	cfg := testStreamConfig(MustNewPipeline[string]())
	originalIdentity := cfg.Identity
	cfg.Delivery.AllowedPayloadKinds = kinds
	var sink bytes.Buffer
	stream, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	supported := []Boundary{BoundaryArgs}
	profile, err := CompileBoundaryProfile("mapping", supported, []Boundary{BoundaryArgs})
	if err != nil {
		t.Fatal(err)
	}
	// Act: subsequent mutation of source configuration must not alter compiled config.
	kinds[0] = PayloadTechnicalPayload
	cfg.MaxInputBytes = 1
	cfg.Identity = "mutated"
	supported[0] = BoundaryDelivery
	_, writeErr := stream.Write([]byte("benign"))
	outcome, completeErr := stream.Complete(context.Background())
	// Assert.
	if writeErr != nil || completeErr != nil || sink.String() != "benign" || outcome.Identity != originalIdentity ||
		!profile.Covers(BoundaryArgs) ||
		profile.Covers(BoundaryDelivery) {
		t.Fatalf("%v %v %+v %q", writeErr, completeErr, outcome, sink.String())
	}
}

type independentPayload struct {
	Name  string
	Token string
}

func TestIndependentAPIPayloadAndBatchTransform(t *testing.T) {
	// Arrange: entirely caller-owned API and batch types, no runtime/provider wrapper.
	api := MustNewPipeline(
		WithSequential(
			ValidatorFunc[independentPayload](
				func(_ context.Context, value independentPayload) (independentPayload, *Report, error) {
					value.Name, value.Token = strings.TrimSpace(value.Name), ""
					return value, &Report{Action: ActionRedact}, nil
				},
			),
		),
	)
	apiHandler := WrapInput(
		api,
		nil,
		func(_ context.Context, value independentPayload) (independentPayload, error) { return value, nil },
	)
	batch := MustNewPipeline(
		WithSequential(
			ValidatorFunc[[]independentPayload](
				func(_ context.Context, values []independentPayload) ([]independentPayload, *Report, error) {
					out := append([]independentPayload(nil), values...)
					for i := range out {
						out[i].Name, out[i].Token = strings.TrimSpace(out[i].Name), ""
					}
					return out, &Report{Action: ActionRedact}, nil
				},
			),
		),
	)
	input := []independentPayload{{Name: " Ada ", Token: "secret"}, {Name: " Bo ", Token: "secret"}}
	// Act.
	apiValue, apiErr := apiHandler(context.Background(), input[0])
	batchValue, batchErr := batch.Run(context.Background(), nil, input)
	// Assert.
	if apiErr != nil || batchErr != nil || apiValue.Name != "Ada" || apiValue.Token != "" ||
		batchValue.Output[1].Name != "Bo" ||
		batchValue.Output[1].Token != "" ||
		input[0].Token != "secret" {
		t.Fatalf("api=%+v batch=%+v errors=%v %v", apiValue, batchValue, apiErr, batchErr)
	}
}

func TestIndependentFakeProducerConsumer(t *testing.T) {
	for _, abort := range []bool{false, true} {
		checkIndependentProducer(t, abort)
	}
}

func checkIndependentProducer(t *testing.T, abort bool) {
	t.Helper()
	// Arrange: producer has only a push callback and success/error signal.
	var sink bytes.Buffer
	stream, err := CompileStream(&sink, testStreamConfig(MustNewPipeline[string]()))
	if err != nil {
		t.Fatal(err)
	}
	producer := func(push func([]byte) error) error {
		for _, chunk := range []string{"arbitrary ", "producer"} {
			if pushErr := push([]byte(chunk)); pushErr != nil {
				return pushErr
			}
		}
		if abort {
			return errors.New("producer disconnected")
		}
		return nil
	}
	// Act.
	err = producer(func(chunk []byte) error { _, writeErr := stream.Write(chunk); return writeErr })
	if err != nil {
		_, err = stream.Abort(err)
	} else {
		_, err = stream.Complete(context.Background())
	}
	// Assert.
	if abort {
		if err == nil || sink.Len() != 0 || stream.Outcome().Category != StreamIncomplete {
			t.Fatalf("%v %q", err, sink.String())
		}
	} else if err != nil || sink.String() != "arbitrary producer" {
		t.Fatalf("%v %q", err, sink.String())
	}
}
