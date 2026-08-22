package validation

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"numa-perfman/internal/results"
)

// ResultsRepository persists validation run rows and outcomes.
type ResultsRepository interface {
	CreateRun(ctx context.Context, p results.CreateRunParams) error
	UpdateRunLifecycle(ctx context.Context, runID string, p results.UpdateRunLifecycleParams) error
	SaveValidationResult(ctx context.Context, v results.ValidationResult) error
	ReplaceValidationFailureSamples(ctx context.Context, runID string, samples []results.ValidationFailureSample) error
	AddEvent(ctx context.Context, p results.AddEventParams) error
	ListEvents(ctx context.Context, runID string) ([]results.RunEvent, error)
}

// ResultsEventRecorder writes validation phases to run_events and lifecycle status.
type ResultsEventRecorder struct {
	Repo  ResultsRepository
	Clock Clock
}

func (r ResultsEventRecorder) clock() Clock {
	if r.Clock != nil {
		return r.Clock
	}
	return realClock{}
}

func (r ResultsEventRecorder) RecordPhase(ctx context.Context, runID uuid.UUID, phase Phase, message string, detail any) error {
	if r.Repo == nil {
		return nil
	}
	detailJSON := ""
	if detail != nil {
		b, err := json.Marshal(detail)
		if err == nil {
			detailJSON = string(b)
		}
	}
	now := r.clock().Now().UTC()
	_ = r.Repo.AddEvent(ctx, results.AddEventParams{
		RunID:      runID.String(),
		Timestamp:  now,
		Level:      "info",
		Phase:      string(phase),
		Message:    message,
		DetailJSON: detailJSON,
	})
	status := phaseToStatus(phase)
	if status == "" {
		return nil
	}
	p := results.UpdateRunLifecycleParams{Status: status}
	if phase.Terminal() {
		p.CompletedAt = &now
	}
	return r.Repo.UpdateRunLifecycle(ctx, runID.String(), p)
}

func phaseToStatus(p Phase) string {
	switch p {
	case PhaseCreated:
		return results.StatusCreated
	case PhaseGenerating:
		return results.StatusGenerating
	case PhaseNamespaceCreated:
		return results.StatusNamespaceCreated
	case PhaseControllerReady:
		return results.StatusControllerReady
	case PhasePipelineReady:
		return results.StatusPipelineReady
	case PhaseSending:
		return results.StatusSending
	case PhaseDraining:
		return results.StatusDraining
	case PhaseValidating:
		return results.StatusValidating
	case PhasePassed:
		return results.StatusPassed
	case PhaseFailed:
		return results.StatusFailed
	default:
		return ""
	}
}

// RepositoryResults adapts *results.Repository to ResultsRepository.
type RepositoryResults struct{ *results.Repository }

func (RepositoryResults) ensure(r *results.Repository) ResultsRepository {
	if r == nil {
		return nil
	}
	return RepositoryResults{r}
}

func (r RepositoryResults) CreateRun(ctx context.Context, p results.CreateRunParams) error {
	return r.Repository.CreateRun(ctx, p)
}
func (r RepositoryResults) UpdateRunLifecycle(ctx context.Context, runID string, p results.UpdateRunLifecycleParams) error {
	return r.Repository.UpdateRunLifecycle(ctx, runID, p)
}
func (r RepositoryResults) SaveValidationResult(ctx context.Context, v results.ValidationResult) error {
	return r.Repository.SaveValidationResult(ctx, v)
}
func (r RepositoryResults) ReplaceValidationFailureSamples(ctx context.Context, runID string, samples []results.ValidationFailureSample) error {
	return r.Repository.ReplaceValidationFailureSamples(ctx, runID, samples)
}
func (r RepositoryResults) AddEvent(ctx context.Context, p results.AddEventParams) error {
	return r.Repository.AddEvent(ctx, p)
}
func (r RepositoryResults) ListEvents(ctx context.Context, runID string) ([]results.RunEvent, error) {
	return r.Repository.ListEvents(ctx, runID)
}

// NewResultsEventRecorder returns a recorder backed by SQLite results storage.
func NewResultsEventRecorder(repo *results.Repository, clock Clock) EventRecorder {
	return ResultsEventRecorder{Repo: RepositoryResults{repo}, Clock: clock}
}
