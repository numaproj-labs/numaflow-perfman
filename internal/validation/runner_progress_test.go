package validation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/oracle"
)

type progressStubDB struct {
	snap   ProgressSnapshot
	actual []PhysicalDelivery
}

func (p *progressStubDB) Create(context.Context, string, oracle.Bundle) error { return nil }
func (p *progressStubDB) Drop(context.Context, string) error                  { return nil }
func (p *progressStubDB) ListSinkDeliveries(context.Context, string) ([]PhysicalDelivery, error) {
	return p.actual, nil
}
func (p *progressStubDB) Dump(context.Context, string) ([]byte, error) { return nil, nil }
func (p *progressStubDB) Progress(context.Context, string) (ProgressSnapshot, error) {
	return p.snap, nil
}

type progressStubCluster struct {
	retained        bool
	scaledToZero    bool
	scenarioDeleted bool
	deleteCalled    bool
}

func (p *progressStubCluster) CreateNamespace(context.Context, string) error { return nil }
func (p *progressStubCluster) RetainNamespace(context.Context, string) error {
	p.retained = true
	return nil
}
func (p *progressStubCluster) DeployController(context.Context, string, string) error {
	return nil
}
func (p *progressStubCluster) WaitControllerReady(context.Context, string, time.Duration) error {
	return nil
}
func (p *progressStubCluster) DeployScenario(context.Context, string, oracle.Scenario, string, string) error {
	return nil
}
func (p *progressStubCluster) WaitScenarioReady(context.Context, string, oracle.Scenario, time.Duration) error {
	return nil
}
func (p *progressStubCluster) DeleteScenario(context.Context, string, oracle.Scenario, time.Duration) error {
	p.scenarioDeleted = true
	return nil
}
func (p *progressStubCluster) ScaleControllerToZero(context.Context, string) error {
	p.scaledToZero = true
	return nil
}
func (p *progressStubCluster) DeleteNamespace(context.Context, string) error {
	p.deleteCalled = true
	return nil
}

func TestWaitSendingStallsWhenCountersFlat(t *testing.T) {
	t.Parallel()
	r := NewRunner(Dependencies{
		Database: &progressStubDB{snap: ProgressSnapshot{TotalEvents: 10, AckedEvents: 5, SinkRows: 2, ExpectedRows: 7}},
	})
	opts := Options{ProgressTimeout: 200 * time.Millisecond, DBRetryAttempts: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := r.waitSending(ctx, "db", opts)
	if err == nil {
		t.Fatal("expected progress timeout")
	}
	if !errors.Is(err, ErrInfrastructure) {
		t.Fatalf("expected infrastructure error, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{
		"no sending progress for 200ms",
		"source_acked=5/10",
		"source_pending=5",
		"sink_received=2/7",
		"sink_missing=5",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

func TestWaitDrainingStallsReportsProgressSnapshot(t *testing.T) {
	t.Parallel()
	r := NewRunner(Dependencies{
		Database: &progressStubDB{snap: ProgressSnapshot{
			SourceCompleted: true,
			TotalEvents:     10,
			AckedEvents:     10,
			SinkRows:        6,
			ExpectedRows:    8,
		}},
	})
	opts := Options{ProgressTimeout: 200 * time.Millisecond, DBRetryAttempts: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := r.waitDraining(ctx, "db", opts)
	if err == nil {
		t.Fatal("expected progress timeout")
	}
	if !errors.Is(err, ErrInfrastructure) {
		t.Fatalf("expected infrastructure error, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{
		"all source data was sent",
		"waited 200ms with no sink progress",
		"source_completed=true",
		"source_acked=10/10",
		"sink_received=6/8",
		"sink_missing=2",
		"failing validation benchmark",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

func TestFormatMissingOutputSummaryGroupsByPath(t *testing.T) {
	t.Parallel()
	expected := []oracle.ExpectedLogicalOutput{
		{LogicalKey: "evt-1_unary-map_0", EventID: "evt-1", ProcessedBy: "unary-map", ChildIndex: "0", TotalChildren: "1"},
		{LogicalKey: "evt-2_batch-map_0", EventID: "evt-2", ProcessedBy: "batch-map", ChildIndex: "0", TotalChildren: "2"},
		{LogicalKey: "evt-2_batch-map_1", EventID: "evt-2", ProcessedBy: "batch-map", ChildIndex: "1", TotalChildren: "2"},
		{LogicalKey: "evt-3_stream-map_0", EventID: "evt-3", ProcessedBy: "stream-map", ChildIndex: "0", TotalChildren: "1"},
	}
	actual := []PhysicalDelivery{{LogicalKey: "evt-1_unary-map_0"}}

	got := formatMissingOutputSummary(expected, actual, 2)
	for _, want := range []string{
		"missing_by_path=batch-map=2/2,stream-map=1/1",
		"path=batch-map event=evt-2 child=0/2 key=evt-2_batch-map_0",
		"path=batch-map event=evt-2 child=1/2 key=evt-2_batch-map_1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "evt-3_stream-map_0") {
		t.Fatalf("summary exceeded sample limit: %q", got)
	}
}

func TestDrainFailureDetailsIncludeMissingPaths(t *testing.T) {
	t.Parallel()
	expected := []oracle.ExpectedLogicalOutput{
		{LogicalKey: "evt-1_unary-map_0", EventID: "evt-1", ProcessedBy: "unary-map", ChildIndex: "0", TotalChildren: "1"},
		{LogicalKey: "evt-2_batch-map_0", EventID: "evt-2", ProcessedBy: "batch-map", ChildIndex: "0", TotalChildren: "1"},
	}
	r := NewRunner(Dependencies{
		Database: &progressStubDB{actual: []PhysicalDelivery{
			{LogicalKey: "evt-1_unary-map_0", ProcessedBy: "unary-map", ChildIndex: "0", TotalChildren: "1"},
		}},
	})
	base := errors.New("validation infrastructure error: all source data was sent")

	err, cmp := r.addDrainFailureDetails(context.Background(), "db", expected, Options{DBRetryAttempts: 1, MaxFailureSamples: 5}, base)
	if err == nil {
		t.Fatal("expected annotated error")
	}
	if cmp.MissingCount != 1 || len(cmp.Samples) != 1 || cmp.Samples[0].Expected != "batch-map" {
		t.Fatalf("unexpected compare result: %+v", cmp)
	}
	for _, want := range []string{
		"missing_by_path=batch-map=1/1",
		"path=batch-map event=evt-2 child=0/1 key=evt-2_batch-map_0",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestCleanupScenarioKeepsControllerRunningUntilNamespaceDeletion(t *testing.T) {
	t.Parallel()
	cluster := &progressStubCluster{}
	r := NewRunner(Dependencies{Cluster: cluster})
	if err := r.cleanupScenario(context.Background(), uuid.New(), oracle.ScenarioMap, "ns", "db", Options{}, false); err != nil {
		t.Fatal(err)
	}
	if cluster.scaledToZero || !cluster.scenarioDeleted || !cluster.deleteCalled {
		t.Fatalf("scaled=%v scenarioDeleted=%v delete=%v", cluster.scaledToZero, cluster.scenarioDeleted, cluster.deleteCalled)
	}
}

func TestCleanupScenarioRetainsFailureNamespace(t *testing.T) {
	t.Parallel()
	cluster := &progressStubCluster{}
	r := NewRunner(Dependencies{Cluster: cluster})
	if err := r.cleanupScenario(context.Background(), uuid.New(), oracle.ScenarioMap, "ns", "db", Options{}, true); err != nil {
		t.Fatal(err)
	}
	if !cluster.retained || cluster.scenarioDeleted || cluster.deleteCalled {
		t.Fatalf("retained=%v scenarioDeleted=%v delete=%v", cluster.retained, cluster.scenarioDeleted, cluster.deleteCalled)
	}
}

func TestResolvedSeedStableAcrossCopies(t *testing.T) {
	t.Parallel()
	opts := Options{}
	first := opts.ResolvedSeed()
	copy := opts
	second := copy.ResolvedSeed()
	if first != second {
		t.Fatalf("seed drift %d vs %d", first, second)
	}
}

func TestScenarioNamespaceUsesOptionsOverride(t *testing.T) {
	t.Parallel()
	parent := uuid.New()
	scenarioRun := ScenarioRunID(parent, oracle.ScenarioMap)
	legacy := NamespaceForScenario(scenarioRun, oracle.ScenarioMap)
	opts := Options{Namespace: "validation-map-v1-8-0"}
	if ScenarioNamespace(opts, scenarioRun, oracle.ScenarioMap) != "validation-map-v1-8-0" {
		t.Fatal("expected options namespace override")
	}
	if ScenarioNamespace(Options{}, scenarioRun, oracle.ScenarioMap) != legacy {
		t.Fatal("expected legacy UUID namespace when unset")
	}
}

func TestDatabaseNameForScenarioSanitizesHyphens(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	got := DatabaseNameForScenario(id, oracle.ScenarioSlidingReduce)
	want := "validation_550e8400e29b41d4a716446655440000_sliding_reduce"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
