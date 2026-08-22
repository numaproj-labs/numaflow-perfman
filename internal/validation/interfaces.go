package validation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/oracle"
)

// ProgressSnapshot reports pipeline DB progress during sending/draining.
type ProgressSnapshot struct {
	SourceCompleted bool
	TotalEvents     int64
	AckedEvents     int64
	SinkRows        int64
	ExpectedRows    int64
	At              time.Time
}

// EventRecorder persists phase transitions for a run.
type EventRecorder interface {
	RecordPhase(ctx context.Context, runID uuid.UUID, phase Phase, message string, detail any) error
}

// RunDatabase manages per-run validation Postgres state (no pgx in this package).
type RunDatabase interface {
	Create(ctx context.Context, dbName string, bundle oracle.Bundle) error
	Drop(ctx context.Context, dbName string) error
	Progress(ctx context.Context, dbName string) (ProgressSnapshot, error)
	ListSinkDeliveries(ctx context.Context, dbName string) ([]PhysicalDelivery, error)
	Dump(ctx context.Context, dbName string) ([]byte, error)
}

// Cluster deploys namespace-scoped validation infrastructure.
type Cluster interface {
	CreateNamespace(ctx context.Context, name string) error
	DeleteNamespace(ctx context.Context, name string) error
	RetainNamespace(ctx context.Context, name string) error
	DeployController(ctx context.Context, namespace, imageRef string) error
	WaitControllerReady(ctx context.Context, namespace string, timeout time.Duration) error
	DeployScenario(ctx context.Context, namespace string, scenario oracle.Scenario, dbName, udfImage string) error
	WaitScenarioReady(ctx context.Context, namespace string, scenario oracle.Scenario, timeout time.Duration) error
	DeleteScenario(ctx context.Context, namespace string, scenario oracle.Scenario, timeout time.Duration) error
	ScaleControllerToZero(ctx context.Context, namespace string) error
}

// OracleGenerator produces deterministic bundles (defaults to oracle.Generate).
type OracleGenerator interface {
	Generate(scenario oracle.Scenario, cfg oracle.GenerationConfig) (oracle.Bundle, error)
}

type defaultOracle struct{}

func (defaultOracle) Generate(scenario oracle.Scenario, cfg oracle.GenerationConfig) (oracle.Bundle, error) {
	return oracle.Generate(scenario, cfg)
}

// Clock abstracts time for tests.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Dependencies wires infrastructure for Runner.
type Dependencies struct {
	Cluster          Cluster
	Database         RunDatabase
	Events           EventRecorder
	Oracle           OracleGenerator
	Clock            Clock
	FailureDumpStore FailureDumpStore
}

func (d *Dependencies) withDefaults() Dependencies {
	out := *d
	if out.Oracle == nil {
		out.Oracle = defaultOracle{}
	}
	if out.Clock == nil {
		out.Clock = realClock{}
	}
	return out
}
