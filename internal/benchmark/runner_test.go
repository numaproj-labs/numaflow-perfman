package benchmark_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"path/filepath"

	"github.com/google/uuid"
	"numa-perfman/internal/benchmark"
	"numa-perfman/internal/diagnostics"
	"numa-perfman/internal/namespace"
	prom "numa-perfman/internal/prometheus"
	"numa-perfman/internal/results"
	"numa-perfman/internal/scenario"
)

func TestStateTransitions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from, to benchmark.State
		ok       bool
	}{
		{benchmark.StateCreated, benchmark.StatePreflight, true},
		{benchmark.StatePipelineReady, benchmark.StateMeasuring, true},
		{benchmark.StateMeasuring, benchmark.StateCollecting, true},
		{benchmark.StateCollecting, benchmark.StateCompleted, true},
		{benchmark.StateCompleted, benchmark.StateFailed, false},
		{benchmark.StateCreated, benchmark.StateCompleted, false},
	}
	for _, c := range cases {
		if got := benchmark.ValidTransition(c.from, c.to); got != c.ok {
			t.Fatalf("%s -> %s = %v want %v", c.from, c.to, got, c.ok)
		}
	}
}

func TestRunnerSuccessNoCompleteBeforeMetricsCommit(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	runner := benchmark.NewRunner(fake.deps())
	opts := baseOpts()
	opts.Duration = 2 * time.Second
	opts.HealthPollInterval = time.Millisecond

	res, err := runner.Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalState != benchmark.StateCompleted {
		t.Fatalf("result: %+v", res)
	}
	if fake.completedBeforeMetrics {
		t.Fatal("completed status persisted before SaveMetrics")
	}
	if fake.metricsSavedAt.IsZero() {
		t.Fatal("metrics not saved")
	}
	if fake.completedAt.Before(fake.metricsSavedAt) {
		t.Fatal("completed_at before metrics commit")
	}
	if fake.completedBeforeDelete {
		t.Fatal("completed status before namespace deletion")
	}
}

func TestRunnerReportsLifecycleProgress(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	var progress recordingProgressReporter
	deps := fake.deps()
	deps.Progress = &progress
	runner := benchmark.NewRunner(deps)
	opts := baseOpts()
	opts.Duration = time.Millisecond

	if _, err := runner.Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	output := strings.Join(progress.messages, "\n")
	for _, want := range []string{
		"run=",
		"resolving image metadata",
		"preflight: checking UDF image",
		"creating namespace",
		"deploying Numaflow controller",
		"waiting up to",
		"deploying scenario resources",
		"checking required Prometheus metrics",
		"measuring for",
		"collecting metrics",
		"deleting namespace",
		"completed",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("progress output missing %q:\n%s", want, output)
		}
	}
}

func TestRunnerCreatesOneRun(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	ids := &seqIDs{}
	fake.ids = ids
	runner := benchmark.NewRunner(fake.deps())
	opts := baseOpts()
	opts.Duration = time.Millisecond

	res, err := runner.Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids.sequence) != 1 {
		t.Fatalf("runs = %d", len(ids.sequence))
	}
	if res.RunID != ids.sequence[0] || res.Namespace != "perf-single-map-v1-8-0" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestRunnerDeclinesExistingResultOverwrite(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	oldID := uuid.NewString()
	fake.store.runs = map[string]results.Run{
		oldID: {
			ID: oldID, Kind: results.KindBenchmark, Scenario: "single-map",
			ImageRef: "quay.io/numaproj/numaflow:v1.8.0", Status: results.StatusCompleted,
		},
	}
	deps := fake.deps()
	deps.Overwrite = overwriteConfirmer{confirmed: false}

	_, err := benchmark.NewRunner(deps).Run(context.Background(), baseOpts())
	if !errors.Is(err, benchmark.ErrOverwriteDeclined) {
		t.Fatalf("err = %v, want overwrite declined", err)
	}
	if _, ok := fake.store.runs[oldID]; !ok {
		t.Fatal("existing run was removed")
	}
	if fake.lastRunID != "" {
		t.Fatalf("created run %q after declined overwrite", fake.lastRunID)
	}
}

func TestRunnerReplacesExistingResultAfterConfirmation(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	oldID := uuid.NewString()
	fake.store.runs = map[string]results.Run{
		oldID: {
			ID: oldID, Kind: results.KindBenchmark, Scenario: "single-map",
			ImageRef: "quay.io/numaproj/numaflow:v1.8.0", Status: results.StatusCompleted,
		},
	}
	deps := fake.deps()
	deps.Overwrite = overwriteConfirmer{confirmed: true}
	opts := baseOpts()
	opts.Duration = time.Millisecond

	result, err := benchmark.NewRunner(deps).Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.store.runs[oldID]; ok {
		t.Fatal("existing run was not removed")
	}
	if _, ok := fake.store.runs[result.RunID.String()]; !ok {
		t.Fatal("replacement run was not stored")
	}
}

func TestRunnerMissingRequiredMetrics(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	fake.metrics.collectErr = benchmark.WrapMetricsIncomplete(errors.New("missing forwarder_rate"))
	runner := benchmark.NewRunner(fake.deps())
	opts := baseOpts()
	opts.Duration = time.Millisecond

	_, err := runner.Run(context.Background(), opts)
	if !benchmark.IsMetricsIncomplete(err) {
		t.Fatalf("want metrics incomplete, got %v", err)
	}
	run, _ := fake.store.GetRun(context.Background(), fake.lastRunID)
	if run.Status != results.StatusFailed {
		t.Fatalf("status = %q", run.Status)
	}
}

func TestRunnerWaitsForRequiredMetrics(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	fake.metrics.preflightErrs = []error{errors.New("metric series not ready")}
	runner := benchmark.NewRunner(fake.deps())
	opts := baseOpts()
	opts.Duration = time.Millisecond
	opts.HealthPollInterval = time.Second

	if _, err := runner.Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if fake.metrics.preflightCalls != 2 {
		t.Fatalf("metric preflight calls = %d, want 2", fake.metrics.preflightCalls)
	}
}

func TestRunnerRetainFailureScalesController(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	fake.cluster.deployScenarioErr = benchmark.WrapInfrastructure(errors.New("deploy failed"))
	runner := benchmark.NewRunner(fake.deps())
	opts := baseOpts()

	_, err := runner.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error")
	}
	if !fake.cluster.scaledZero {
		t.Fatal("expected controller scaled to zero")
	}
	if !fake.cluster.retained {
		t.Fatal("expected namespace retained")
	}
	if fake.cluster.deleted {
		t.Fatal("expected namespace retained (not deleted)")
	}
}

func TestRunnerInterrupt(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	ctx, cancel := context.WithCancel(context.Background())
	fake.sleeper.onSleep = func(d time.Duration) {
		if d >= time.Second {
			cancel()
		}
	}
	runner := benchmark.NewRunner(fake.deps())
	opts := baseOpts()
	opts.Duration = 2 * time.Second

	result, err := runner.Run(ctx, opts)
	if err == nil {
		t.Fatal("expected cancel error")
	}
	if result.FinalState != benchmark.StateInterrupted {
		t.Fatalf("state %s", result.FinalState)
	}
	run, _ := fake.store.GetRun(context.Background(), fake.lastRunID)
	if run.Status != results.StatusInterrupted {
		t.Fatalf("status %q", run.Status)
	}
}

func TestRunnerCleanupOnSuccess(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	runner := benchmark.NewRunner(fake.deps())
	opts := baseOpts()
	opts.Duration = time.Millisecond

	_, err := runner.Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !fake.cluster.deleted {
		t.Fatal("namespace not deleted")
	}
	if !fake.cluster.scenarioDeleted {
		t.Fatal("scenario resources not deleted")
	}
}

func TestExactControllerImage(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	runner := benchmark.NewRunner(fake.deps())
	opts := baseOpts()
	opts.Duration = time.Millisecond
	_, err := runner.Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if fake.cluster.lastControllerImage != opts.ImageRef {
		t.Fatalf("controller image %q want %q", fake.cluster.lastControllerImage, opts.ImageRef)
	}
}

func TestDiagnosticPersistenceFailurePreventsCompletedStatus(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	fake.artifacts.diagnosticErr = errors.New("artifact database write failed")
	runner := benchmark.NewRunner(fake.deps())
	opts := baseOpts()
	opts.Duration = time.Millisecond

	result, err := runner.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected artifact persistence error")
	}
	if result.FinalState == benchmark.StateCompleted {
		t.Fatal("run completed despite missing diagnostic artifacts")
	}
}

func TestGlobalLockMetadataUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, err := results.Open(ctx, filepath.Join(t.TempDir(), "perfman.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	var updates int
	locker := trackingLocker{
		DatabaseLocker: benchmark.DatabaseLocker{Repo: repo},
		onUpdate:       func() { updates++ },
	}

	fake := newFakeDeps()
	deps := fake.deps()
	deps.Locker = locker
	runner := benchmark.NewRunner(deps)
	opts := baseOpts()
	opts.Duration = time.Millisecond

	_, err = runner.Run(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if updates < 2 {
		t.Fatalf("lock metadata updates = %d", updates)
	}
	lockKey := benchmark.BenchmarkLockKey(mustBenchmarkKey(t, opts))
	if _, found, err := repo.ReadActiveLock(ctx, lockKey); err != nil || found {
		t.Fatalf("lock still held after run: found=%v err=%v", found, err)
	}
}

func baseOpts() benchmark.Options {
	return benchmark.Options{
		Scenario: "single-map",
		ImageRef: "quay.io/numaproj/numaflow:v1.8.0",
		UDFImage: "numaflow-perfman-udfs:abc",
	}
}

type seqIDs struct {
	sequence []uuid.UUID
}

func (s *seqIDs) New() uuid.UUID {
	id := uuid.New()
	s.sequence = append(s.sequence, id)
	return id
}

type fakeDeps struct {
	clock     *fakeClock
	sleeper   fakeSleeper
	store     *memResults
	cluster   *fakeCluster
	metrics   *fakeMetrics
	preflight fakePreflight
	artifacts *memArtifacts
	events    *memEvents
	ids       benchmark.IDGenerator

	lastRunID              string
	metricsSavedAt         time.Time
	completedAt            time.Time
	completedBeforeMetrics bool
	completedBeforeDelete  bool
}

func newFakeDeps() *fakeDeps {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	f := &fakeDeps{
		clock:     clk,
		sleeper:   fakeSleeper{clock: clk},
		store:     &memResults{},
		cluster:   &fakeCluster{},
		metrics:   &fakeMetrics{},
		preflight: fakePreflight{},
		artifacts: &memArtifacts{},
		events:    &memEvents{},
	}
	f.store.bind(f)
	f.cluster.bind(f)
	return f
}

func (f *fakeDeps) deps() benchmark.Dependencies {
	ids := f.ids
	if ids == nil {
		ids = seqUUIDGen{}
	}
	return benchmark.Dependencies{
		Preflight:   f.preflight,
		Cluster:     f.cluster,
		Metrics:     f.metrics,
		Results:     f.store,
		Artifacts:   f.artifacts,
		Diagnostics: fakeDiagnostics{},
		Events:      f.events,
		Clock:       f.clock,
		Sleeper:     f.sleeper,
		IDs:         ids,
	}
}

type seqUUIDGen struct{}

func (seqUUIDGen) New() uuid.UUID { return uuid.New() }

type fakeClock struct {
	t time.Time
}

func (f *fakeClock) Now() time.Time { return f.t }

type fakeSleeper struct {
	clock   *fakeClock
	onSleep func(time.Duration)
}

func (f fakeSleeper) Sleep(ctx context.Context, d time.Duration) error {
	if f.onSleep != nil {
		f.onSleep(d)
	}
	if f.clock != nil {
		f.clock.t = f.clock.t.Add(d)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

type fakePreflight struct {
	verifyErr error
}

func (f fakePreflight) VerifyUDFImage(context.Context, string) error { return f.verifyErr }
func (fakePreflight) ResolveImageDigest(context.Context, string) (string, error) {
	return "sha256:deadbeef", nil
}

type recordingProgressReporter struct {
	messages []string
}

func (r *recordingProgressReporter) Report(message string) {
	r.messages = append(r.messages, message)
}

type overwriteConfirmer struct {
	confirmed bool
}

func (o overwriteConfirmer) ConfirmOverwrite(context.Context, results.Run) (bool, error) {
	return o.confirmed, nil
}

type fakeCluster struct {
	mu                    sync.Mutex
	deleted               bool
	retained              bool
	scaledZero            bool
	deleteContextCanceled bool
	scenarioDeleted       bool
	deployScenarioErr     error
	lastControllerImage   string
	external              *fakeDeps
}

func (f *fakeCluster) bind(d *fakeDeps) { f.external = d }

func (f *fakeCluster) BindRunContext(string, string, string) {}
func (f *fakeCluster) CreateRunNamespace(context.Context, string, string, string, string, string) error {
	return nil
}
func (f *fakeCluster) DeleteRunNamespace(ctx context.Context, _ string, _ time.Duration) error {
	f.deleted = true
	f.deleteContextCanceled = ctx.Err() != nil
	return nil
}
func (f *fakeCluster) ScaleControllerToZero(context.Context, string) error {
	f.scaledZero = true
	return nil
}
func (f *fakeCluster) RetainFailureNamespace(context.Context, string) error {
	f.retained = true
	f.scaledZero = true
	return nil
}
func (f *fakeCluster) DeployController(_ context.Context, _, imageRef string) error {
	f.lastControllerImage = imageRef
	return nil
}
func (f *fakeCluster) WaitControllerReady(context.Context, string, time.Duration) error { return nil }
func (f *fakeCluster) DeployScenario(context.Context, string, scenario.Scenario, string) (scenario.ManifestBundle, error) {
	if f.deployScenarioErr != nil {
		return scenario.ManifestBundle{}, f.deployScenarioErr
	}
	return scenario.ManifestBundle{Pipeline: "pipeline: true", Hash: "abc"}, nil
}
func (f *fakeCluster) WaitScenarioReady(context.Context, string, scenario.Scenario, time.Duration) error {
	return nil
}
func (f *fakeCluster) DeleteScenario(context.Context, string, scenario.Scenario, time.Duration) error {
	f.scenarioDeleted = true
	return nil
}
func (f *fakeCluster) CheckMeasurementHealth(context.Context, string) error { return nil }

type fakeMetrics struct {
	collectErr     error
	preflightErrs  []error
	preflightCalls int
}

func (f *fakeMetrics) PreflightRequired(context.Context, []scenario.MetricDefinition, string) error {
	f.preflightCalls++
	if len(f.preflightErrs) > 0 {
		err := f.preflightErrs[0]
		f.preflightErrs = f.preflightErrs[1:]
		return err
	}
	return nil
}
func (f *fakeMetrics) CollectRange(ctx context.Context, defs []scenario.MetricDefinition, _ string, start, end time.Time, _ time.Duration) ([]prom.SeriesResult, error) {
	if f.collectErr != nil {
		return nil, f.collectErr
	}
	var out []prom.SeriesResult
	for _, d := range defs {
		if !d.Required {
			continue
		}
		samples := []prom.Sample{
			{Timestamp: start, Value: 1},
			{Timestamp: start.Add(time.Second), Value: 2},
			{Timestamp: end, Value: 3},
		}
		out = append(out, prom.SeriesResult{Metric: d, Samples: samples})
	}
	return out, nil
}

type fakeDiagnostics struct{}

func (fakeDiagnostics) Collect(context.Context, string) (diagnostics.Bundle, error) {
	return diagnostics.Bundle{Pods: "pod-a"}, nil
}

type memResults struct {
	mu       sync.Mutex
	runs     map[string]results.Run
	metrics  map[string]bool
	external *fakeDeps

	lastStatus string
}

func (m *memResults) bind(f *fakeDeps) { m.external = f }

func (m *memResults) CreateRun(_ context.Context, p results.CreateRunParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runs == nil {
		m.runs = map[string]results.Run{}
	}
	m.runs[p.ID] = results.Run{
		ID: p.ID, Kind: p.Kind, Scenario: p.Scenario, ImageRef: p.ImageRef,
		Status: p.Status, Namespace: p.Namespace, CreatedAt: p.CreatedAt,
		ManifestHash: p.ManifestHash, ImageDigest: p.ImageDigest,
	}
	if m.external != nil {
		m.external.lastRunID = p.ID
	}
	return nil
}

func (m *memResults) UpdateRunLifecycle(_ context.Context, runID string, p results.UpdateRunLifecycleParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runs[runID]
	if p.Status == results.StatusCompleted {
		if m.external != nil && !m.metrics[runID] {
			m.external.completedBeforeMetrics = true
		}
		if m.external != nil && !m.external.cluster.deleted {
			m.external.completedBeforeDelete = true
		}
		if p.CompletedAt != nil && m.external != nil {
			m.external.completedAt = *p.CompletedAt
		}
	}
	r.Status = p.Status
	r.ErrorMessage = p.ErrorMessage
	m.lastStatus = p.Status
	if p.CompletedAt != nil {
		r.CompletedAt = p.CompletedAt
	}
	m.runs[runID] = r
	return nil
}

func (m *memResults) SaveMetrics(_ context.Context, p results.SaveMetricsParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.metrics == nil {
		m.metrics = map[string]bool{}
	}
	m.metrics[p.RunID] = true
	if m.external != nil {
		m.external.metricsSavedAt = time.Unix(1000, 0)
	}
	return nil
}

func (m *memResults) AddEvent(context.Context, results.AddEventParams) error { return nil }

func (m *memResults) ListEvents(context.Context, string) ([]results.RunEvent, error) {
	return nil, nil
}

func (m *memResults) GetRun(_ context.Context, runID string) (results.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runs[runID], nil
}

func (m *memResults) FindBenchmarkRun(_ context.Context, scenarioID, imageRef string) (results.Run, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, run := range m.runs {
		if run.Kind == results.KindBenchmark && run.Scenario == scenarioID && run.ImageRef == imageRef {
			return run, true, nil
		}
	}
	return results.Run{}, false, nil
}

func (m *memResults) FindBenchmarkRunByNamespace(_ context.Context, scenarioID, namespaceName string) (results.Run, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, run := range m.runs {
		if run.Kind == results.KindBenchmark && run.Scenario == scenarioID && run.Namespace == namespaceName {
			return run, true, nil
		}
	}
	return results.Run{}, false, nil
}

func (m *memResults) DeleteRun(_ context.Context, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runs, runID)
	delete(m.metrics, runID)
	return nil
}

type memArtifacts struct {
	diagnosticErr error
}

func (memArtifacts) WriteResolvedManifest(string, string, []byte) error { return nil }
func (m memArtifacts) WriteDiagnostic(string, string, []byte) error {
	return m.diagnosticErr
}

type memEvents struct{}

func (memEvents) RecordPhase(context.Context, string, benchmark.State, string, any) error {
	return nil
}

type trackingLocker struct {
	benchmark.DatabaseLocker
	onUpdate func()
}

func (t trackingLocker) Acquire(ctx context.Context, lockKey string, meta benchmark.LockMetadata) (*benchmark.RunLock, error) {
	return t.DatabaseLocker.Acquire(ctx, lockKey, meta)
}

func (t trackingLocker) UpdateMetadata(ctx context.Context, l *benchmark.RunLock, meta benchmark.LockMetadata) error {
	if t.onUpdate != nil {
		t.onUpdate()
	}
	if meta.RunID == "" {
		return fmt.Errorf("lock metadata missing run id")
	}
	if meta.PID <= 0 || meta.Host == "" {
		return fmt.Errorf("lock metadata missing pid or host")
	}
	return t.DatabaseLocker.UpdateMetadata(ctx, l, meta)
}

func mustBenchmarkKey(t *testing.T, opts benchmark.Options) namespace.BenchmarkKey {
	t.Helper()
	key, err := namespace.ParseBenchmarkKey(opts.Scenario, opts.ImageRef)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestRunnerPreflightFailurePersistsRun(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	fake.preflight = fakePreflight{verifyErr: benchmark.WrapInfrastructure(errors.New("missing udf"))}
	runner := benchmark.NewRunner(fake.deps())
	opts := baseOpts()
	_, err := runner.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error")
	}
	run, ok := fake.store.runs[fake.lastRunID]
	if !ok {
		t.Fatal("run row not persisted")
	}
	if run.Status != results.StatusFailed {
		t.Fatalf("status %q", run.Status)
	}
}

func TestRunnerManifestHashIsRenderedDigest(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	runner := benchmark.NewRunner(fake.deps())
	opts := baseOpts()
	opts.Duration = time.Millisecond
	_, err := runner.Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	run, _ := fake.store.GetRun(context.Background(), fake.lastRunID)
	if run.ManifestHash == "" || run.ManifestHash == opts.Scenario {
		t.Fatalf("manifest hash %q should be rendered digest", run.ManifestHash)
	}
	if run.ImageDigest != "sha256:deadbeef" {
		t.Fatalf("image digest %q", run.ImageDigest)
	}
}

func TestRunnerInterruptDeletes(t *testing.T) {
	t.Parallel()
	fake := newFakeDeps()
	ctx, cancel := context.WithCancel(context.Background())
	fake.sleeper.onSleep = func(d time.Duration) {
		if d >= time.Second {
			cancel()
		}
	}
	runner := benchmark.NewRunner(fake.deps())
	opts := baseOpts()
	opts.Duration = 2 * time.Second

	_, err := runner.Run(ctx, opts)
	if err == nil {
		t.Fatal("expected cancel")
	}
	if fake.cluster.scaledZero {
		t.Fatal("controller must remain running while deleting the namespace")
	}
	if !fake.cluster.deleted {
		t.Fatal("expected namespace deleted on interrupt")
	}
	if !fake.cluster.scenarioDeleted {
		t.Fatal("expected scenario resources deleted before namespace")
	}
	if fake.cluster.deleteContextCanceled {
		t.Fatal("expected cleanup to use a live background context")
	}
}
