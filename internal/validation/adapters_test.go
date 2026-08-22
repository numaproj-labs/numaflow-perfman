package validation_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/oracle"
	"numa-perfman/internal/results"
	"numa-perfman/internal/validation"
)

func TestScenarioRunIDDeterministic(t *testing.T) {
	t.Parallel()
	parent := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	a := validation.ScenarioRunID(parent, oracle.ScenarioMap)
	b := validation.ScenarioRunID(parent, oracle.ScenarioMap)
	if a != b {
		t.Fatalf("expected stable id")
	}
	c := validation.ScenarioRunID(parent, oracle.ScenarioReduce)
	if a == c {
		t.Fatal("different scenarios should differ")
	}
}

func TestValidationResultFromCompare(t *testing.T) {
	t.Parallel()
	cmp := validation.CompareResult{
		Passed:                 false,
		ExpectedLogicalCount:   2,
		LogicalActualCount:     1,
		MissingCount:           1,
		DuplicateDeliveryCount: 3,
		DuplicateRate:          0.5,
		RoutingMismatch:        1,
		ChildMismatch:          2,
		Samples:                []validation.FailureSample{{Kind: "missing", LogicalKey: "k1"}},
	}
	vr := validation.ValidationResultFromCompare("550e8400-e29b-41d4-a716-446655440000", cmp, 10)
	if vr.Passed || vr.SourceCount != 10 || vr.MissingCount != 1 {
		t.Fatalf("unexpected vr: %+v", vr)
	}
	if vr.DuplicateRate != cmp.DuplicateRate ||
		vr.RoutingMismatchCount != cmp.RoutingMismatch ||
		vr.ChildMismatchCount != cmp.ChildMismatch {
		t.Fatalf("structured validation fields not persisted: %+v", vr)
	}
	if vr.DetailJSON == "" {
		t.Fatal("expected detail json")
	}
	if strings.Contains(vr.DetailJSON, "samples") {
		t.Fatalf("samples must not be in detail_json: %s", vr.DetailJSON)
	}
	samples := validation.FailureSamplesToResults(cmp.Samples)
	if len(samples) != 1 || samples[0].Kind != "missing" || samples[0].LogicalKey != "k1" {
		t.Fatalf("unexpected samples: %+v", samples)
	}
}

func TestPostgresValidateDBName(t *testing.T) {
	t.Parallel()
	a := &validation.PostgresAdapter{}
	err := a.Create(context.Background(), "bad-name", oracle.Bundle{Scenario: oracle.ScenarioMap})
	if err == nil {
		t.Fatal("expected invalid name error")
	}
}

func TestPostgresDumpUsesArgv(t *testing.T) {
	t.Parallel()
	runner := &recordingCommandRunner{stdout: "-- PostgreSQL dump\n"}
	a := &validation.PostgresAdapter{
		Creds: validation.PostgresCredentials{
			AdminDSN: "postgres://numaflow:numaflow@127.0.0.1:5432/postgres",
			User:     "numaflow",
			Password: "numaflow",
			Host:     "127.0.0.1",
			Port:     "5432",
		},
		Runner: runner,
	}
	dbName := validation.DatabaseNameFromRunID(uuid.New())
	got, err := a.Dump(context.Background(), dbName)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "-- PostgreSQL dump\n" {
		t.Fatalf("dump = %q", got)
	}
	if len(runner.calls) != 1 || runner.calls[0].name != "pg_dump" {
		t.Fatalf("calls: %+v", runner.calls)
	}
	args := runner.calls[0].args
	if len(args) < 2 || args[0] != "--no-owner" {
		t.Fatalf("args: %v", args)
	}
	for _, arg := range args {
		if arg == "-f" {
			t.Fatalf("pg_dump must capture stdout, not write to file: %v", args)
		}
	}
}

func TestPersistingRunnerFake(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, err := results.Open(ctx, filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	invocation := uuid.New()
	fake := newFakeInfra()
	runner := validation.NewRunner(validation.Dependencies{
		Cluster:  fake,
		Database: fake,
		Events:   validation.NewResultsEventRecorder(repo, fakeClock{t: time.Unix(0, 0)}),
		Clock:    fakeClock{t: time.Unix(0, 0)},
	})
	pr := &validation.PersistingRunner{
		Runner:  runner,
		Results: validation.RepositoryResults{Repository: repo},
	}
	opts := validation.Options{
		Scenarios:       []oracle.Scenario{oracle.ScenarioMap},
		ImageRef:        "quay.io/numaproj/numaflow:v1.0.0",
		UDFImage:        "numaflow-perfman-udfs:local",
		Events:          5,
		TotalTimeout:    time.Minute,
		ProgressTimeout: time.Second,
	}
	seed := uint64(3)
	opts.Seed = &seed

	out, err := pr.Run(ctx, invocation, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !out.RunResult.AllPassed() {
		t.Fatalf("run failed: %+v", out.RunResult)
	}
	scenarioRunID := validation.ScenarioRunID(invocation, oracle.ScenarioMap)
	run, err := repo.GetRun(ctx, scenarioRunID.String())
	if err != nil {
		t.Fatal(err)
	}
	if run.Scenario != "map" || run.ManifestHash == "" {
		t.Fatalf("run row: %+v", run)
	}
	vr, ok, err := repo.GetValidationResult(ctx, scenarioRunID.String())
	if err != nil || !ok || !vr.Passed {
		t.Fatalf("validation result: ok=%v err=%v vr=%+v", ok, err, vr)
	}
}

type recordingCommandRunner struct {
	calls  []commandCall
	stdout string
}

type commandCall struct {
	name string
	args []string
}

func (r *recordingCommandRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	r.calls = append(r.calls, commandCall{name: name, args: append([]string(nil), args...)})
	return r.stdout, "", nil
}

func TestSchemaDDLForScenario(t *testing.T) {
	t.Parallel()
	if validation.SchemaDDLForTest(oracle.ScenarioMap) == "" {
		t.Fatal("map schema")
	}
	if validation.SchemaDDLForTest(oracle.ScenarioReduce) == validation.SchemaDDLForTest(oracle.ScenarioMap) {
		t.Fatal("reduce schema should differ")
	}
}
