package validation_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/oracle"
	"numa-perfman/internal/validation"
)

func TestPhaseTransitions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from, to validation.Phase
		ok       bool
	}{
		{validation.PhaseCreated, validation.PhaseGenerating, true},
		{validation.PhaseSending, validation.PhaseDraining, true},
		{validation.PhaseValidating, validation.PhasePassed, true},
		{validation.PhasePassed, validation.PhaseFailed, false},
		{validation.PhaseCreated, validation.PhasePassed, false},
	}
	for _, c := range cases {
		if got := validation.ValidTransition(c.from, c.to); got != c.ok {
			t.Fatalf("%s -> %s = %v want %v", c.from, c.to, got, c.ok)
		}
	}
}

func TestCompareDuplicatesPass(t *testing.T) {
	t.Parallel()
	bundle, err := oracle.Generate(oracle.ScenarioMap, oracle.GenerationConfig{Seed: 1, EventCount: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Expected) == 0 {
		t.Fatal("no expected")
	}
	exp := bundle.Expected[0]
	actual := []validation.PhysicalDelivery{
		{
			LogicalKey: exp.LogicalKey, EventID: exp.EventID,
			ProcessedBy: exp.ProcessedBy, ChildIndex: exp.ChildIndex, TotalChildren: exp.TotalChildren,
			Payload: exp.Payload, ReceiveCount: 3,
		},
	}
	res := validation.CompareExpected(bundle.Expected, actual, validation.CompareOptions{MaxSamples: 5})
	if res.Passed {
		t.Fatal("expected failure due to missing keys")
	}
	if res.DuplicateDeliveryCount != 2 {
		t.Fatalf("dup count = %d want 2", res.DuplicateDeliveryCount)
	}

	fullActual := deliveriesFromExpected(bundle.Expected)
	fullActual[0].ReceiveCount = 4
	res2 := validation.CompareExpected(bundle.Expected, fullActual, validation.CompareOptions{})
	if !res2.Passed {
		t.Fatalf("duplicates should pass: %+v", res2)
	}
	if res2.DuplicateDeliveryCount < 1 {
		t.Fatalf("expected duplicate reporting")
	}
}

func TestCompareDefectsFail(t *testing.T) {
	t.Parallel()
	bundle, _ := oracle.Generate(oracle.ScenarioMap, oracle.GenerationConfig{Seed: 2, EventCount: 3})
	actual := deliveriesFromExpected(bundle.Expected)
	actual[0].Payload = []byte(`{"corrupted":true}`)
	if validation.CompareExpected(bundle.Expected, actual, validation.CompareOptions{}).Passed {
		t.Fatal("corruption should fail")
	}
	actual = deliveriesFromExpected(bundle.Expected)
	actual = actual[1:]
	if validation.CompareExpected(bundle.Expected, actual, validation.CompareOptions{}).Passed {
		t.Fatal("missing should fail")
	}
	extra := deliveriesFromExpected(bundle.Expected)
	extra = append(extra, validation.PhysicalDelivery{LogicalKey: "unexpected_key", ReceiveCount: 1})
	if validation.CompareExpected(bundle.Expected, extra, validation.CompareOptions{}).Passed {
		t.Fatal("unexpected should fail")
	}
}

func TestBoundedRetry(t *testing.T) {
	t.Parallel()
	attempts := 0
	err := validation.WithBoundedRetry(context.Background(), 3, time.Millisecond, func(ctx context.Context) error {
		attempts++
		return validation.WrapInfrastructure(context.Canceled)
	})
	if !validation.IsInfrastructure(err) {
		t.Fatalf("want infrastructure error, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestRunnerSequentialFake(t *testing.T) {
	t.Parallel()
	runID := uuid.New()
	fake := newFakeInfra()
	runner := validation.NewRunner(validation.Dependencies{
		Cluster:  fake,
		Database: fake,
		Events:   fake,
		Clock:    fakeClock{t: time.Unix(0, 0)},
	})
	opts := validation.Options{
		Scenarios:       []oracle.Scenario{oracle.ScenarioMap},
		ImageRef:        "quay.io/numaproj/numaflow:v1.0.0",
		Events:          10,
		TotalTimeout:    time.Minute,
		ProgressTimeout: time.Second,
	}
	seed := uint64(7)
	opts.Seed = &seed

	res, err := runner.Run(context.Background(), runID, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !res.AllPassed() {
		t.Fatalf("run failed: %+v", res)
	}
	if fake.lastPhase != validation.PhasePassed {
		t.Fatalf("last phase %s", fake.lastPhase)
	}
	if len(fake.ops) < 2 || fake.ops[0] != "namespace-create" || fake.ops[1] != "database-create" {
		t.Fatalf("operation order = %v, want namespace-create before database-create", fake.ops)
	}
}

func deliveriesFromExpected(exp []oracle.ExpectedLogicalOutput) []validation.PhysicalDelivery {
	out := make([]validation.PhysicalDelivery, len(exp))
	for i, e := range exp {
		out[i] = validation.PhysicalDelivery{
			LogicalKey: e.LogicalKey, EventID: e.EventID,
			ProcessedBy: e.ProcessedBy, ChildIndex: e.ChildIndex, TotalChildren: e.TotalChildren,
			Payload: e.Payload, ReceiveCount: 1,
		}
	}
	return out
}

type fakeClock struct {
	t time.Time
}

func (f fakeClock) Now() time.Time { return f.t }

type fakeInfra struct {
	mu        sync.Mutex
	bundle    oracle.Bundle
	complete  bool
	lastPhase validation.Phase
	ops       []string
}

func newFakeInfra() *fakeInfra { return &fakeInfra{} }

func (f *fakeInfra) Create(_ context.Context, _ string, bundle oracle.Bundle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, "database-create")
	f.bundle = bundle
	f.complete = false
	return nil
}
func (f *fakeInfra) Drop(context.Context, string) error { return nil }
func (f *fakeInfra) Progress(_ context.Context, _ string) (validation.ProgressSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.complete {
		f.complete = true
	}
	return validation.ProgressSnapshot{
		SourceCompleted: f.complete,
		TotalEvents:     int64(len(f.bundle.MapSources)),
		AckedEvents:     int64(len(f.bundle.MapSources)),
		SinkRows:        int64(len(f.bundle.Expected)),
		ExpectedRows:    int64(len(f.bundle.Expected)),
		At:              time.Unix(0, 0),
	}, nil
}
func (f *fakeInfra) ListSinkDeliveries(_ context.Context, _ string) ([]validation.PhysicalDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return deliveriesFromExpected(f.bundle.Expected), nil
}
func (f *fakeInfra) Dump(context.Context, string) ([]byte, error) { return nil, nil }

func (f *fakeInfra) CreateNamespace(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, "namespace-create")
	return nil
}
func (f *fakeInfra) DeleteNamespace(context.Context, string) error                    { return nil }
func (f *fakeInfra) RetainNamespace(context.Context, string) error                    { return nil }
func (f *fakeInfra) DeployController(context.Context, string, string) error           { return nil }
func (f *fakeInfra) WaitControllerReady(context.Context, string, time.Duration) error { return nil }
func (f *fakeInfra) DeployScenario(context.Context, string, oracle.Scenario, string, string) error {
	return nil
}
func (f *fakeInfra) WaitScenarioReady(context.Context, string, oracle.Scenario, time.Duration) error {
	return nil
}
func (f *fakeInfra) DeleteScenario(context.Context, string, oracle.Scenario, time.Duration) error {
	return nil
}
func (f *fakeInfra) ScaleControllerToZero(context.Context, string) error { return nil }

func (f *fakeInfra) RecordPhase(_ context.Context, _ uuid.UUID, phase validation.Phase, _ string, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastPhase = phase
	return nil
}

func TestDatabaseNameFromRunID(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	got := validation.DatabaseNameFromRunID(id)
	want := "validation_550e8400e29b41d4a716446655440000"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
