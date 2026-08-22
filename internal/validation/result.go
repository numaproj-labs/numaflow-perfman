package validation

import (
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/oracle"
)

// ScenarioResult is the outcome of one scenario within a run.
type ScenarioResult struct {
	Scenario        oracle.Scenario
	Phase           Phase
	Passed          bool
	Compare         CompareResult
	Error           string
	Seed            uint64
	BaseEventTimeMs int64
	SpacingMs       int64
	SourceEvents    int64
}

// RunResult aggregates sequential scenario outcomes.
type RunResult struct {
	RunID     uuid.UUID
	Namespace string
	Database  string
	StartedAt time.Time
	EndedAt   time.Time
	Scenarios []ScenarioResult
}

func (r RunResult) AllPassed() bool {
	for _, s := range r.Scenarios {
		if !s.Passed {
			return false
		}
	}
	return len(r.Scenarios) > 0
}
