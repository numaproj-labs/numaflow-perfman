package validation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/oracle"
)

// Runner executes validation scenarios sequentially with a finite state machine.
type Runner struct {
	deps Dependencies
}

func NewRunner(deps Dependencies) *Runner {
	d := deps.withDefaults()
	return &Runner{deps: d}
}

// SetFailureDumpStore configures where failed scenario database dumps are persisted.
func (r *Runner) SetFailureDumpStore(store FailureDumpStore) {
	r.deps.FailureDumpStore = store
}

// Run executes each scenario in opts.Scenarios sequentially.
func (r *Runner) Run(ctx context.Context, runID uuid.UUID, opts Options) (RunResult, error) {
	opts = opts.withDefaults()
	if runID == uuid.Nil {
		return RunResult{}, fmt.Errorf("run ID is required")
	}
	if opts.ImageRef == "" {
		return RunResult{}, fmt.Errorf("image reference is required")
	}
	opts.ResolvedSeed()

	result := RunResult{
		RunID:     runID,
		Namespace: ScenarioNamespace(opts, runID, opts.Scenarios[0]),
		Database:  DatabaseNameFromRunID(runID),
		StartedAt: r.deps.Clock.Now(),
	}
	deadline, hasDeadline := opts.TotalTimeout, opts.TotalTimeout > 0
	var runCtx context.Context
	var cancel context.CancelFunc
	if hasDeadline {
		runCtx, cancel = context.WithTimeout(ctx, deadline)
	} else {
		runCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	for _, scenario := range opts.Scenarios {
		if err := runCtx.Err(); err != nil {
			result.EndedAt = r.deps.Clock.Now()
			return result, err
		}
		sr, err := r.runScenario(runCtx, runID, scenario, opts)
		result.Scenarios = append(result.Scenarios, sr)
		ns := ScenarioNamespace(opts, runID, scenario)
		dbName := DatabaseNameForScenario(runID, scenario)
		if err != nil {
			result.EndedAt = r.deps.Clock.Now()
			r.recordCleanupErrors(runID, r.cleanupScenario(ctx, runID, scenario, ns, dbName, opts, true))
			return result, err
		}
		if !sr.Passed {
			result.EndedAt = r.deps.Clock.Now()
			r.recordCleanupErrors(runID, r.cleanupScenario(ctx, runID, scenario, ns, dbName, opts, true))
			return result, ErrCorrectness
		}
		if err := r.cleanupScenario(ctx, runID, scenario, ns, dbName, opts, false); err != nil {
			result.EndedAt = r.deps.Clock.Now()
			r.recordCleanupErrors(runID, err)
			return result, err
		}
	}
	result.EndedAt = r.deps.Clock.Now()
	return result, nil
}

func (r *Runner) cleanupScenario(parentCtx context.Context, runID uuid.UUID, scenario oracle.Scenario, ns, dbName string, opts Options, failed bool) error {
	ctx := parentCtx
	var cancel context.CancelFunc
	if parentCtx.Err() != nil {
		ctx, cancel = context.WithTimeout(context.Background(), opts.CleanupTimeout)
		defer cancel()
	}

	var errs []error
	if failed && opts.DumpFailureDB && r.deps.Database != nil {
		content, err := r.deps.Database.Dump(ctx, dbName)
		if err != nil {
			errs = append(errs, fmt.Errorf("dump database %s: %w", dbName, err))
		} else if r.deps.FailureDumpStore != nil {
			if err := r.deps.FailureDumpStore.PutFailureDump(ctx, runID, content); err != nil {
				errs = append(errs, fmt.Errorf("persist database dump for %s: %w", dbName, err))
			}
		}
	}
	if failed {
		if err := r.deps.Cluster.RetainNamespace(ctx, ns); err != nil {
			errs = append(errs, fmt.Errorf("retain namespace %s: %w", ns, err))
		}
		return errors.Join(errs...)
	}
	if err := r.deps.Cluster.DeleteScenario(ctx, ns, scenario, opts.CleanupTimeout); err != nil {
		errs = append(errs, fmt.Errorf("delete scenario resources in %s: %w", ns, err))
		return errors.Join(errs...)
	}
	if err := r.deps.Cluster.DeleteNamespace(ctx, ns); err != nil {
		errs = append(errs, fmt.Errorf("delete namespace %s: %w", ns, err))
		return errors.Join(errs...)
	}
	if r.deps.Database != nil {
		if err := r.deps.Database.Drop(ctx, dbName); err != nil {
			errs = append(errs, fmt.Errorf("drop database %s: %w", dbName, err))
		}
	}
	return errors.Join(errs...)
}

func (r *Runner) recordCleanupErrors(runID uuid.UUID, err error) {
	if err == nil || r.deps.Events == nil {
		return
	}
	_ = r.deps.Events.RecordPhase(context.Background(), runID, PhaseFailed, "cleanup failed", map[string]string{
		"error": err.Error(),
	})
}

func (r *Runner) runScenario(ctx context.Context, runID uuid.UUID, scenario oracle.Scenario, opts Options) (ScenarioResult, error) {
	sr := ScenarioResult{Scenario: scenario, Phase: PhaseCreated}
	seed := opts.ResolvedSeed()
	sr.Seed = seed
	cfg := opts.GenerationConfig(seed)
	sr.BaseEventTimeMs = cfg.BaseEventTimeMs
	sr.SpacingMs = cfg.SpacingMs

	transition := func(to Phase, msg string) error {
		if !ValidTransition(sr.Phase, to) {
			return fmt.Errorf("invalid transition %s -> %s", sr.Phase, to)
		}
		sr.Phase = to
		if r.deps.Events != nil {
			_ = r.deps.Events.RecordPhase(ctx, runID, to, msg, nil)
		}
		return nil
	}

	if err := transition(PhaseGenerating, "generating oracle bundle"); err != nil {
		return sr, err
	}
	bundle, err := r.deps.Oracle.Generate(scenario, cfg)
	if err != nil {
		sr.Phase = PhaseFailed
		sr.Error = err.Error()
		return sr, err
	}
	switch scenario {
	case oracle.ScenarioReduce, oracle.ScenarioSlidingReduce:
		sr.SourceEvents = int64(len(bundle.ReduceSources))
	default:
		sr.SourceEvents = int64(len(bundle.MapSources))
	}

	dbName := DatabaseNameForScenario(runID, scenario)
	ns := ScenarioNamespace(opts, runID, scenario)

	if err := r.deps.Cluster.CreateNamespace(ctx, ns); err != nil {
		sr.Phase = PhaseFailed
		return sr, err
	}
	if err := transition(PhaseNamespaceCreated, "namespace created"); err != nil {
		return sr, err
	}

	err = WithBoundedRetry(ctx, opts.DBRetryAttempts, time.Second, func(ctx context.Context) error {
		if err := r.deps.Database.Create(ctx, dbName, bundle); err != nil {
			return WrapInfrastructure(err)
		}
		return nil
	})
	if err != nil {
		sr.Phase = PhaseFailed
		return sr, err
	}

	if err := r.deps.Cluster.DeployController(ctx, ns, opts.ImageRef); err != nil {
		sr.Phase = PhaseFailed
		return sr, err
	}
	if err := r.deps.Cluster.WaitControllerReady(ctx, ns, opts.TotalTimeout); err != nil {
		sr.Phase = PhaseFailed
		return sr, err
	}
	if err := transition(PhaseControllerReady, "controller ready"); err != nil {
		return sr, err
	}

	if err := r.deps.Cluster.DeployScenario(ctx, ns, scenario, dbName, opts.UDFImage); err != nil {
		sr.Phase = PhaseFailed
		return sr, err
	}
	if err := r.deps.Cluster.WaitScenarioReady(ctx, ns, scenario, opts.TotalTimeout); err != nil {
		sr.Phase = PhaseFailed
		return sr, err
	}
	if err := transition(PhasePipelineReady, "pipeline ready"); err != nil {
		return sr, err
	}

	if err := transition(PhaseSending, "sending"); err != nil {
		return sr, err
	}
	if err := r.waitSending(ctx, dbName, opts); err != nil {
		sr.Phase = PhaseFailed
		sr.Error = err.Error()
		return sr, err
	}

	if err := transition(PhaseDraining, "draining"); err != nil {
		return sr, err
	}
	if err := r.waitDraining(ctx, dbName, opts); err != nil {
		sr.Phase = PhaseFailed
		if IsInfrastructure(err) {
			var cmp CompareResult
			err, cmp = r.addDrainFailureDetails(ctx, dbName, bundle.Expected, opts, err)
			sr.Compare = cmp
		}
		sr.Error = err.Error()
		return sr, err
	}

	if err := transition(PhaseValidating, "validating"); err != nil {
		return sr, err
	}
	var actual []PhysicalDelivery
	err = WithBoundedRetry(ctx, opts.DBRetryAttempts, time.Second, func(ctx context.Context) error {
		var err error
		actual, err = r.deps.Database.ListSinkDeliveries(ctx, dbName)
		if err != nil {
			return WrapInfrastructure(err)
		}
		return nil
	})
	if err != nil {
		sr.Phase = PhaseFailed
		return sr, err
	}

	cmp := CompareExpected(bundle.Expected, actual, CompareOptions{MaxSamples: opts.MaxFailureSamples})
	sr.Compare = cmp
	if !cmp.Passed {
		sr.Phase = PhaseFailed
		sr.Passed = false
		return sr, nil
	}

	if err := transition(PhasePassed, "passed"); err != nil {
		return sr, err
	}
	sr.Passed = true
	return sr, nil
}

func (r *Runner) waitSending(ctx context.Context, dbName string, opts Options) error {
	progressDeadline := time.Now().Add(opts.ProgressTimeout)
	var lastAcked, lastSink int64 = -1, -1
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var snap ProgressSnapshot
		err := WithBoundedRetry(ctx, opts.DBRetryAttempts, time.Second, func(ctx context.Context) error {
			var err error
			snap, err = r.deps.Database.Progress(ctx, dbName)
			if err != nil {
				return WrapInfrastructure(err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if snap.SourceCompleted {
			return nil
		}
		if snap.AckedEvents > lastAcked || snap.SinkRows > lastSink {
			lastAcked = snap.AckedEvents
			lastSink = snap.SinkRows
			progressDeadline = time.Now().Add(opts.ProgressTimeout)
		}
		if time.Now().After(progressDeadline) {
			return fmt.Errorf("%w: no sending progress for %v; %s", ErrInfrastructure, opts.ProgressTimeout, formatProgressSnapshot(snap))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (r *Runner) waitDraining(ctx context.Context, dbName string, opts Options) error {
	progressDeadline := time.Now().Add(opts.ProgressTimeout)
	var lastSink int64 = -1
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var snap ProgressSnapshot
		err := WithBoundedRetry(ctx, opts.DBRetryAttempts, time.Second, func(ctx context.Context) error {
			var err error
			snap, err = r.deps.Database.Progress(ctx, dbName)
			if err != nil {
				return WrapInfrastructure(err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if snap.ExpectedRows > 0 && snap.SinkRows >= snap.ExpectedRows {
			if snap.SinkRows > lastSink {
				lastSink = snap.SinkRows
				progressDeadline = time.Now().Add(opts.ProgressTimeout)
			} else if time.Now().After(progressDeadline) {
				return nil
			}
		} else if snap.SinkRows > lastSink {
			lastSink = snap.SinkRows
			progressDeadline = time.Now().Add(opts.ProgressTimeout)
		} else if time.Now().After(progressDeadline) {
			return fmt.Errorf("%w: all source data was sent, then waited %v with no sink progress; %s; failing validation benchmark", ErrInfrastructure, opts.ProgressTimeout, formatProgressSnapshot(snap))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func formatProgressSnapshot(snap ProgressSnapshot) string {
	pendingSource := snap.TotalEvents - snap.AckedEvents
	if pendingSource < 0 {
		pendingSource = 0
	}
	missingSink := snap.ExpectedRows - snap.SinkRows
	if missingSink < 0 {
		missingSink = 0
	}
	return fmt.Sprintf(
		"source_completed=%t source_acked=%d/%d source_pending=%d sink_received=%d/%d sink_missing=%d",
		snap.SourceCompleted,
		snap.AckedEvents,
		snap.TotalEvents,
		pendingSource,
		snap.SinkRows,
		snap.ExpectedRows,
		missingSink,
	)
}

func (r *Runner) addDrainFailureDetails(ctx context.Context, dbName string, expected []oracle.ExpectedLogicalOutput, opts Options, base error) (error, CompareResult) {
	var actual []PhysicalDelivery
	err := WithBoundedRetry(ctx, opts.DBRetryAttempts, time.Second, func(ctx context.Context) error {
		var err error
		actual, err = r.deps.Database.ListSinkDeliveries(ctx, dbName)
		if err != nil {
			return WrapInfrastructure(err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%w; missing-output details unavailable: %v", base, err), CompareResult{}
	}

	cmp := CompareExpected(expected, actual, CompareOptions{MaxSamples: opts.MaxFailureSamples})
	summary := formatMissingOutputSummary(expected, actual, 5)
	if summary == "" {
		return base, cmp
	}
	return fmt.Errorf("%w; %s", base, summary), cmp
}

type missingPathStat struct {
	path     string
	expected int64
	missing  int64
}

func formatMissingOutputSummary(expected []oracle.ExpectedLogicalOutput, actual []PhysicalDelivery, maxSamples int) string {
	if maxSamples <= 0 {
		maxSamples = 5
	}
	actualByKey := collapsePhysical(actual)
	statsByPath := make(map[string]*missingPathStat)
	var samples []string
	var totalMissing int64
	for _, exp := range expected {
		path := expectedOutputPath(exp)
		stat, ok := statsByPath[path]
		if !ok {
			stat = &missingPathStat{path: path}
			statsByPath[path] = stat
		}
		stat.expected++
		if _, ok := actualByKey[exp.LogicalKey]; ok {
			continue
		}
		stat.missing++
		totalMissing++
		if len(samples) < maxSamples {
			samples = append(samples, formatMissingOutputSample(exp))
		}
	}
	if totalMissing == 0 {
		return ""
	}

	stats := make([]*missingPathStat, 0, len(statsByPath))
	for _, stat := range statsByPath {
		if stat.missing > 0 {
			stats = append(stats, stat)
		}
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].missing == stats[j].missing {
			return stats[i].path < stats[j].path
		}
		return stats[i].missing > stats[j].missing
	})

	pathParts := make([]string, 0, len(stats))
	for i, stat := range stats {
		if i == 8 {
			pathParts = append(pathParts, fmt.Sprintf("...+%d paths", len(stats)-i))
			break
		}
		pathParts = append(pathParts, fmt.Sprintf("%s=%d/%d", stat.path, stat.missing, stat.expected))
	}
	summary := "missing_by_path=" + strings.Join(pathParts, ",")
	if len(samples) > 0 {
		summary += "; missing_samples=[" + strings.Join(samples, ";") + "]"
	}
	return summary
}

func formatMissingOutputSample(exp oracle.ExpectedLogicalOutput) string {
	return fmt.Sprintf(
		"path=%s event=%s child=%s/%s key=%s",
		expectedOutputPath(exp),
		exp.EventID,
		exp.ChildIndex,
		exp.TotalChildren,
		exp.LogicalKey,
	)
}
