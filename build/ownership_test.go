package build_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/build"
)

type guardVersion struct {
	pipeline *guardy.Pipeline[string]
	scope    guardy.StaticScope
}

func compileVersion(tenant string) (*guardVersion, error) {
	// Separate immutable values for policy and facts; never mutate a published version.
	want := map[string]string{"tenant": tenant}
	p, err := build.CompileStringGuard(build.GuardSpec{PolicyRules: []build.PolicyRuleSpec{
		{Kind: build.PolicyAttributeDeepEqual, Key: "tenant", Value: want},
	}})
	if err != nil {
		return nil, err
	}
	key := guardy.NewScopeKey[map[string]string]("tenant")
	scope := guardy.NewScope(guardy.ScopeValue(key, map[string]string{"tenant": tenant}))
	return &guardVersion{pipeline: p, scope: scope}, nil
}

func ExampleCompileStringGuard_reload() {
	first, err := compileVersion("one")
	if err != nil {
		panic(err)
	}
	var current atomic.Pointer[guardVersion]
	current.Store(first)
	// An in-flight request retains one coherent version; reload publishes a fresh one.
	inFlight := current.Load()
	replacement, err := compileVersion("two")
	if err != nil {
		panic(err)
	}
	current.Store(replacement)
	next := current.Load()
	oldResult, oldErr := inFlight.pipeline.Run(context.Background(), inFlight.scope, "request")
	nextResult, nextErr := next.pipeline.Run(context.Background(), next.scope, "request")
	fmt.Println(oldErr == nil && oldResult.PolicyDecision().Disposition == guardy.DispositionNone)
	fmt.Println(nextErr == nil && nextResult.PolicyDecision().Disposition == guardy.DispositionNone)
	// Output:
	// true
	// true
}

func TestDeepEqualOperandIsBorrowedNotSnapshot(t *testing.T) {
	// Arrange: intentional serial alias mutation probes the ownership boundary;
	// this is NOT a permitted reload recipe or a concurrent mutation example.
	want := map[string]string{"tenant": "one"}
	p, err := build.CompileStringGuard(build.GuardSpec{PolicyRules: []build.PolicyRuleSpec{
		{Kind: build.PolicyAttributeDeepEqual, Key: "tenant", Value: want},
	}})
	if err != nil {
		t.Fatal(err)
	}
	scope := guardy.MapScope{"tenant": map[string]string{"tenant": "two"}}
	before, err := p.Run(context.Background(), scope, "request")
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	want["tenant"] = "two"
	after, err := p.Run(context.Background(), scope, "request")
	// Assert: freeze/snapshot was never promised for the borrowed operand.
	if err != nil || !before.PolicyDecision().IsTerminal() ||
		after.PolicyDecision().Disposition != guardy.DispositionNone {
		t.Fatal("borrowed policy operand unexpectedly behaved as a snapshot")
	}
}

func TestWholeVersionReloadKeepsBorrowedValuesImmutable(t *testing.T) {
	// Arrange.
	first, err := compileVersion("one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := compileVersion("two")
	if err != nil {
		t.Fatal(err)
	}
	var current atomic.Pointer[guardVersion]
	current.Store(first)
	retired := current.Load()
	mismatch, err := first.pipeline.Run(context.Background(), second.scope, "request")
	if err != nil || !mismatch.PolicyDecision().IsTerminal() {
		t.Fatal("different versions must not mix facts and policy")
	}
	var workers sync.WaitGroup
	failures := make(chan struct{}, 1)
	// Act: each reader loads the pair once; both published versions remain immutable.
	for range 4 {
		workers.Go(func() {
			readGuardVersions(&current, failures)
		})
	}
	for range 100 {
		current.Store(second)
		current.Store(first)
	}
	current.Store(second)
	workers.Wait()
	oldResult, oldErr := retired.pipeline.Run(context.Background(), retired.scope, "request")
	// Assert.
	if len(failures) != 0 || oldErr != nil || oldResult.PolicyDecision().Disposition != guardy.DispositionNone ||
		current.Load() != second {
		t.Fatal("atomic whole-version reload changed retired data or mixed versions")
	}
}

func readGuardVersions(current *atomic.Pointer[guardVersion], failures chan<- struct{}) {
	for range 200 {
		version := current.Load()
		result, runErr := version.pipeline.Run(context.Background(), version.scope, "request")
		if runErr != nil || result.PolicyDecision().Disposition != guardy.DispositionNone {
			select {
			case failures <- struct{}{}:
			default:
			}
		}
	}
}
