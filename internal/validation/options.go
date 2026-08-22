package validation

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/oracle"
)

// Tier selects default event counts for validation scenarios.
type Tier string

const (
	TierSmoke    Tier = "smoke"
	TierStandard Tier = "standard"
	TierSoak     Tier = "soak"
)

// Options configures a single finite validation scenario run.
type Options struct {
	Scenarios []oracle.Scenario
	ImageRef  string
	UDFImage  string
	Events    int64
	Tier      Tier

	// Namespace is the deterministic run namespace (validation-<scenario>-<tag>).
	// When empty, NamespaceForScenario(runID, scenario) is used (tests / legacy).
	Namespace string

	// ValidationPostgresNS is the namespace hosting data-validation-postgres (default validation-system).
	ValidationPostgresNS string
	NamespaceDeleteWait  time.Duration

	Seed            *uint64
	BaseEventTimeMs int64
	SpacingMs       int64

	TotalTimeout    time.Duration
	ProgressTimeout time.Duration

	DumpFailureDB bool

	CleanupTimeout time.Duration

	MaxFailureSamples int
	DBRetryAttempts   int
}

func (o Options) withDefaults() Options {
	out := o
	if out.TotalTimeout == 0 {
		out.TotalTimeout = 90 * time.Minute
	}
	if out.ProgressTimeout == 0 {
		out.ProgressTimeout = 5 * time.Minute
	}
	if out.MaxFailureSamples == 0 {
		out.MaxFailureSamples = 20
	}
	if out.DBRetryAttempts == 0 {
		out.DBRetryAttempts = 5
	}
	if out.CleanupTimeout == 0 {
		out.CleanupTimeout = 5 * time.Minute
	}
	if out.ValidationPostgresNS == "" {
		out.ValidationPostgresNS = "validation-system"
	}
	if out.Events == 0 {
		out.Events = defaultEventsForTier(out.Tier)
	}
	if len(out.Scenarios) == 0 {
		out.Scenarios = []oracle.Scenario{
			oracle.ScenarioMap,
			oracle.ScenarioReduce,
			oracle.ScenarioSlidingReduce,
			oracle.ScenarioMonoVertex,
		}
	}
	return out
}

func defaultEventsForTier(t Tier) int64 {
	switch t {
	case TierSmoke:
		return 1_000
	case TierSoak:
		return 1_000_000
	default:
		return 50_000
	}
}

// ResolvedSeed returns the configured seed or generates one once when unset.
func (o *Options) ResolvedSeed() uint64 {
	if o.Seed != nil {
		return *o.Seed
	}
	s := generateSeedOnce()
	o.Seed = &s
	return s
}

var seedCounter uint64 = 0xdeadbeeff00d

func generateSeedOnce() uint64 {
	seedCounter++
	return seedCounter ^ uint64(time.Now().UnixNano())
}

// GenerationConfig builds oracle generation settings from options.
func (o Options) GenerationConfig(seed uint64) oracle.GenerationConfig {
	cfg := oracle.GenerationConfig{
		Seed:            seed,
		EventCount:      o.Events,
		BaseEventTimeMs: oracle.DefaultBaseEventTimeMs,
		SpacingMs:       oracle.DefaultEventSpacingMs,
	}
	if o.BaseEventTimeMs != 0 {
		cfg.BaseEventTimeMs = o.BaseEventTimeMs
	}
	if o.SpacingMs != 0 {
		cfg.SpacingMs = o.SpacingMs
	}
	return cfg
}

// DatabaseNameFromRunID derives a unique Postgres database name from a run UUID.
func DatabaseNameFromRunID(id uuid.UUID) string {
	s := strings.ReplaceAll(id.String(), "-", "")
	return "validation_" + s
}

// NamespaceName derives the legacy UUID-based namespace prefix for a run UUID.
func NamespaceName(id uuid.UUID) string {
	short := strings.ReplaceAll(id.String(), "-", "")[:12]
	return fmt.Sprintf("numaflow-validation-%s", short)
}

// DatabaseNameForScenario derives a unique Postgres database for a run and scenario.
func DatabaseNameForScenario(id uuid.UUID, scenario oracle.Scenario) string {
	safe := strings.ReplaceAll(string(scenario), "-", "_")
	return DatabaseNameFromRunID(id) + "_" + safe
}

// NamespaceForScenario derives the legacy UUID-based namespace for one scenario execution.
func NamespaceForScenario(id uuid.UUID, scenario oracle.Scenario) string {
	return fmt.Sprintf("%s-%s", NamespaceName(id), scenario)
}

// ScenarioNamespace returns the run namespace: Options.Namespace when set, otherwise UUID-based.
func ScenarioNamespace(opts Options, runID uuid.UUID, scenario oracle.Scenario) string {
	if opts.Namespace != "" {
		return opts.Namespace
	}
	return NamespaceForScenario(runID, scenario)
}
