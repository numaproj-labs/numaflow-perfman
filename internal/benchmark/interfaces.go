package benchmark

import (
	"context"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/artifacts"
	"numa-perfman/internal/diagnostics"
	"numa-perfman/internal/prometheus"
	"numa-perfman/internal/results"
	"numa-perfman/internal/runmeta"
	"numa-perfman/internal/scenario"
)

// Clock abstracts wall time for tests.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Sleeper waits for a duration or until context cancellation (virtual in tests).
type Sleeper interface {
	Sleep(ctx context.Context, d time.Duration) error
}

type realSleeper struct{}

func (realSleeper) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// IDGenerator supplies run UUIDs (fresh per repetition in production).
type IDGenerator interface {
	New() uuid.UUID
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID { return uuid.New() }

// Preflight verifies setup preconditions before namespace creation.
type Preflight interface {
	VerifyUDFImage(ctx context.Context, imageRef string) error
	ResolveImageDigest(ctx context.Context, imageRef string) (string, error)
}

// Cluster deploys benchmark infrastructure in a temporary namespace.
type Cluster interface {
	BindRunContext(runID, scenarioID, numaflowImage string)
	CreateRunNamespace(ctx context.Context, name, runID, scenarioID, imageTag, imageRef string) error
	DeleteRunNamespace(ctx context.Context, name string, wait time.Duration) error
	ScaleControllerToZero(ctx context.Context, namespace string) error
	RetainFailureNamespace(ctx context.Context, name string) error
	DeployController(ctx context.Context, namespace, imageRef string) error
	WaitControllerReady(ctx context.Context, namespace string, timeout time.Duration) error
	DeployScenario(ctx context.Context, namespace string, sc scenario.Scenario, udfImage string) (scenario.ManifestBundle, error)
	WaitScenarioReady(ctx context.Context, namespace string, sc scenario.Scenario, timeout time.Duration) error
	DeleteScenario(ctx context.Context, namespace string, sc scenario.Scenario, timeout time.Duration) error
	CheckMeasurementHealth(ctx context.Context, namespace string) error
}

// MetricsCollector performs Prometheus preflight and range collection.
type MetricsCollector interface {
	PreflightRequired(ctx context.Context, defs []scenario.MetricDefinition, namespace string) error
	CollectRange(ctx context.Context, defs []scenario.MetricDefinition, namespace string, start, end time.Time, step time.Duration) ([]prometheus.SeriesResult, error)
}

// ResultsRepository persists run rows, lifecycle, and metrics.
type ResultsRepository interface {
	CreateRun(ctx context.Context, p results.CreateRunParams) error
	UpdateRunLifecycle(ctx context.Context, runID string, p results.UpdateRunLifecycleParams) error
	SaveMetrics(ctx context.Context, p results.SaveMetricsParams) error
	AddEvent(ctx context.Context, p results.AddEventParams) error
	GetRun(ctx context.Context, runID string) (results.Run, error)
	FindBenchmarkRun(ctx context.Context, scenario, imageRef string) (results.Run, bool, error)
	FindBenchmarkRunByNamespace(ctx context.Context, scenario, namespace string) (results.Run, bool, error)
	DeleteRun(ctx context.Context, runID string) error
}

// ArtifactsWriter stores opaque run artifacts in SQLite.
type ArtifactsWriter interface {
	WriteResolvedManifest(runID, filename string, content []byte) error
	WriteDiagnostic(runID, name string, content []byte) error
}

// OverwriteConfirmer asks the caller whether an existing result may be replaced.
type OverwriteConfirmer interface {
	ConfirmOverwrite(ctx context.Context, existing results.Run) (bool, error)
}

// DiagnosticsCollector captures failure diagnostics.
type DiagnosticsCollector interface {
	Collect(ctx context.Context, ns string) (diagnostics.Bundle, error)
}

// EventRecorder persists phase transitions.
type EventRecorder interface {
	RecordPhase(ctx context.Context, runID string, state State, message string, detail any) error
}

// ProgressReporter emits human-readable lifecycle updates while a benchmark runs.
type ProgressReporter interface {
	Report(message string)
}

// Dependencies wires infrastructure for Runner.
type Dependencies struct {
	Preflight   Preflight
	Cluster     Cluster
	Metrics     MetricsCollector
	Results     ResultsRepository
	Artifacts   ArtifactsWriter
	Diagnostics DiagnosticsCollector
	Locker      RunLocker
	Events      EventRecorder
	Progress    ProgressReporter
	Overwrite   OverwriteConfirmer
	Environment runmeta.Provider
	Clock       Clock
	Sleeper     Sleeper
	IDs         IDGenerator
}

func (d Dependencies) withDefaults() Dependencies {
	out := d
	if out.Clock == nil {
		out.Clock = realClock{}
	}
	if out.Sleeper == nil {
		out.Sleeper = realSleeper{}
	}
	if out.IDs == nil {
		out.IDs = uuidGenerator{}
	}
	return out
}

type artifactsWriterAdapter struct{ w *artifacts.Writer }

// ArtifactsFromWriter adapts *artifacts.Writer to ArtifactsWriter.
func ArtifactsFromWriter(w *artifacts.Writer) ArtifactsWriter {
	if w == nil {
		return nil
	}
	return artifactsWriterAdapter{w: w}
}

func (a artifactsWriterAdapter) WriteResolvedManifest(runID, filename string, content []byte) error {
	return a.w.WriteResolvedManifest(runID, filename, content)
}

func (a artifactsWriterAdapter) WriteDiagnostic(runID, name string, content []byte) error {
	return a.w.WriteTextLog(runID, name, string(content))
}
