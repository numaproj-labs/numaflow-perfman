package benchmark

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/namespace"
	"numa-perfman/internal/results"
	"numa-perfman/internal/runmeta"
	"numa-perfman/internal/scenario"
)

// Runner executes the synchronous benchmark state machine.
type Runner struct {
	deps Dependencies
}

// NewRunner constructs a runner with default dependencies filled in.
func NewRunner(deps Dependencies) *Runner {
	d := deps.withDefaults()
	return &Runner{deps: d}
}

func (r *Runner) report(format string, args ...any) {
	if r.deps.Progress != nil {
		r.deps.Progress.Report(fmt.Sprintf(format, args...))
	}
}

// RunResult is the outcome of one benchmark run.
type RunResult struct {
	RunID       uuid.UUID
	Namespace   string
	FinalState  State
	Error       string
	CompletedAt time.Time
}

// Run executes a benchmark under a per-(scenario,tag) lock when configured.
func (r *Runner) Run(ctx context.Context, opts Options) (RunResult, error) {
	opts = opts.withDefaults()
	if err := opts.Validate(); err != nil {
		return RunResult{}, err
	}

	key, err := namespace.ParseBenchmarkKey(opts.Scenario, opts.ImageRef)
	if err != nil {
		return RunResult{}, fmt.Errorf("%w: %v", ErrConfiguration, err)
	}
	lockKey := BenchmarkLockKey(key)

	var hostLock *RunLock
	if r.deps.Locker != nil {
		meta := LockMetadata{
			PID:       os.Getpid(),
			Host:      lockHost(),
			Command:   opts.Command,
			Start:     r.deps.Clock.Now(),
			Namespace: key.Namespace,
			Scenario:  key.Scenario,
			ImageTag:  key.ImageTag,
			ImageRef:  opts.ImageRef,
		}
		hostLock, err = r.deps.Locker.Acquire(ctx, lockKey, meta)
		if err != nil {
			return RunResult{}, err
		}
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = r.deps.Locker.Release(releaseCtx, hostLock)
		}()
	}

	if err := ctx.Err(); err != nil {
		return RunResult{}, err
	}
	if err := r.prepareOverwrite(ctx, opts); err != nil {
		return RunResult{}, err
	}
	result, err := r.run(ctx, opts, key, hostLock)
	if hostLock != nil && r.deps.Locker != nil {
		_ = r.deps.Locker.UpdateMetadata(ctx, hostLock, LockMetadata{
			PID:       os.Getpid(),
			Host:      lockHost(),
			RunID:     result.RunID.String(),
			Namespace: result.Namespace,
			Command:   opts.Command,
			Start:     r.deps.Clock.Now(),
			Scenario:  key.Scenario,
			ImageTag:  key.ImageTag,
			ImageRef:  opts.ImageRef,
		})
	}
	return result, err
}

func (r *Runner) prepareOverwrite(ctx context.Context, opts Options) error {
	if r.deps.Results == nil {
		return nil
	}
	existing, found, err := r.deps.Results.FindBenchmarkRun(ctx, opts.Scenario, opts.ImageRef)
	if err != nil {
		return fmt.Errorf("find existing benchmark result: %w", err)
	}
	if !found {
		return nil
	}
	if r.deps.Overwrite == nil {
		return fmt.Errorf("%w: scenario=%q image=%q", ErrOverwriteDeclined, opts.Scenario, opts.ImageRef)
	}
	confirmed, err := r.deps.Overwrite.ConfirmOverwrite(ctx, existing)
	if err != nil {
		return fmt.Errorf("confirm benchmark overwrite: %w", err)
	}
	if !confirmed {
		r.report("benchmark: existing result was not overwritten")
		return fmt.Errorf("%w: scenario=%q image=%q", ErrOverwriteDeclined, opts.Scenario, opts.ImageRef)
	}
	if err := r.deps.Results.DeleteRun(ctx, existing.ID); err != nil {
		return fmt.Errorf("delete existing benchmark result: %w", err)
	}
	r.report("benchmark: overwrote run=%s scenario=%s image=%s", existing.ID, existing.Scenario, existing.ImageRef)
	return nil
}

func (r *Runner) run(ctx context.Context, opts Options, key namespace.BenchmarkKey, hostLock *RunLock) (RunResult, error) {
	runID := r.deps.IDs.New()
	ns := key.Namespace
	r.report("benchmark: run=%s namespace=%s scenario=%s", runID, ns, opts.Scenario)

	if hostLock != nil && r.deps.Locker != nil {
		_ = r.deps.Locker.UpdateMetadata(ctx, hostLock, LockMetadata{
			PID:       os.Getpid(),
			Host:      lockHost(),
			RunID:     runID.String(),
			Namespace: ns,
			Command:   opts.Command,
			Start:     r.deps.Clock.Now(),
			Scenario:  key.Scenario,
			ImageTag:  key.ImageTag,
			ImageRef:  opts.ImageRef,
		})
	}

	sc, _ := scenario.LookupBenchmark(opts.Scenario)
	metricDefs := sc.Metrics
	requiredNames := RequiredMetricNames(metricDefs)

	manifestBundle, err := scenario.RenderManifests(scenario.RenderInput{
		ScenarioID:    sc.ID,
		Kind:          scenario.RunKindBenchmark,
		Namespace:     ns,
		UDFImage:      opts.UDFImage,
		RunID:         runID.String(),
		NumaflowImage: opts.ImageRef,
	})
	if err != nil {
		return RunResult{RunID: runID, Namespace: ns}, err
	}

	rep := RunResult{RunID: runID, Namespace: ns, FinalState: StateCreated}
	state := StateCreated
	runPersisted := false
	nsCreated := false
	scenarioCleanupRequired := false
	var deployedBundle scenario.ManifestBundle

	r.report("benchmark: resolving image metadata")
	digest, digestWarn := runmeta.ResolveDigest(ctx, opts.ImageRef, r.deps.Preflight)
	envJSON := "{}"
	if r.deps.Environment != nil {
		envJSON = r.deps.Environment.Snapshot().JSON()
	}

	fail := func(to State, runErr error) (RunResult, error) {
		cleanupCtx := ctx
		cleanupCancel := func() {}
		if ctx.Err() != nil {
			cleanupCtx, cleanupCancel = context.WithTimeout(context.Background(), opts.NamespaceDeleteWait)
		}
		defer cleanupCancel()

		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			to = StateInterrupted
		} else if to != StateInterrupted {
			to = StateFailed
		}
		msg := ""
		if runErr != nil {
			msg = runErr.Error()
		}
		rep.FinalState = to
		rep.Error = msg
		r.report("benchmark: %s: %s", to, msg)
		now := r.deps.Clock.Now()
		if runPersisted {
			_ = r.updateStatus(cleanupCtx, runID.String(), to, results.UpdateRunLifecycleParams{
				Status:       string(to),
				ErrorMessage: msg,
				CompletedAt:  &now,
			})
		}
		if diagErr := r.captureDiagnostics(cleanupCtx, runID.String(), ns); diagErr != nil {
			r.recordRunWarning(cleanupCtx, runID.String(), string(to), "persist diagnostics: "+diagErr.Error())
		}
		if nsCreated {
			if to == StateFailed {
				r.report("benchmark: cleanup: retaining namespace %s and scaling down controller", ns)
				_ = r.deps.Cluster.RetainFailureNamespace(cleanupCtx, ns)
			} else {
				if scenarioCleanupRequired {
					r.report("benchmark: cleanup: deleting scenario resources")
					if err := r.deps.Cluster.DeleteScenario(cleanupCtx, ns, sc, opts.NamespaceDeleteWait); err != nil {
						r.report("benchmark: cleanup: scenario deletion failed; retaining namespace %s: %s", ns, err)
						return rep, runErr
					}
				}
				r.report("benchmark: cleanup: deleting namespace %s", ns)
				_ = r.deps.Cluster.DeleteRunNamespace(cleanupCtx, ns, opts.NamespaceDeleteWait)
			}
		}
		return rep, runErr
	}

	transition := func(to State, msg string) error {
		if !ValidTransition(state, to) {
			return fmt.Errorf("invalid transition %s -> %s", state, to)
		}
		state = to
		rep.FinalState = state
		if r.deps.Events != nil {
			_ = r.deps.Events.RecordPhase(ctx, runID.String(), state, msg, nil)
		}
		return r.updateStatus(ctx, runID.String(), state, results.UpdateRunLifecycleParams{Status: string(state)})
	}

	if err := r.deps.Results.CreateRun(ctx, results.CreateRunParams{
		ID: runID.String(), Kind: results.KindBenchmark, Scenario: opts.Scenario,
		ImageRef: opts.ImageRef, ImageDigest: digest,
		Status: string(StateCreated), Namespace: ns, CreatedAt: r.deps.Clock.Now(),
		ManifestHash: manifestBundle.Hash, ConfigJSON: opts.ConfigJSON(),
		EnvironmentJSON: envJSON,
	}); err != nil {
		return fail(StateFailed, err)
	}
	runPersisted = true
	if digestWarn != "" {
		r.recordRunWarning(ctx, runID.String(), string(StateCreated), digestWarn)
	}
	r.warnSameTagDifferentImage(ctx, opts, key, runID.String())

	if err := transition(StatePreflight, "preflight"); err != nil {
		return fail(StateFailed, err)
	}
	r.report("benchmark: preflight: checking UDF image on Kind nodes")
	if err := r.deps.Preflight.VerifyUDFImage(ctx, opts.UDFImage); err != nil {
		return fail(StateFailed, WrapInfrastructure(err))
	}

	r.report("benchmark: creating namespace %s", ns)
	if err := r.deps.Cluster.CreateRunNamespace(ctx, ns, runID.String(), key.Scenario, key.ImageTagSafe, opts.ImageRef); err != nil {
		return fail(StateFailed, WrapInfrastructure(err))
	}
	r.deps.Cluster.BindRunContext(runID.String(), sc.ID, opts.ImageRef)
	nsCreated = true
	if err := transition(StateNamespaceCreated, "namespace created"); err != nil {
		return fail(StateFailed, err)
	}

	r.report("benchmark: deploying Numaflow controller")
	if err := r.deps.Cluster.DeployController(ctx, ns, opts.ImageRef); err != nil {
		return fail(StateFailed, WrapInfrastructure(err))
	}
	r.report("benchmark: waiting up to %s for controller readiness", opts.DeployTimeout)
	if err := r.deps.Cluster.WaitControllerReady(ctx, ns, opts.DeployTimeout); err != nil {
		return fail(StateFailed, WrapInfrastructure(err))
	}
	if err := transition(StateControllerReady, "controller ready"); err != nil {
		return fail(StateFailed, err)
	}

	r.report("benchmark: deploying scenario resources")
	scenarioCleanupRequired = true
	deployedBundle, err = r.deps.Cluster.DeployScenario(ctx, ns, sc, opts.UDFImage)
	if err != nil {
		return fail(StateFailed, WrapInfrastructure(err))
	}
	if deployedBundle.Hash == "" {
		deployedBundle = manifestBundle
	}
	r.report("benchmark: waiting up to %s for scenario readiness", opts.DeployTimeout)
	if err := r.deps.Cluster.WaitScenarioReady(ctx, ns, sc, opts.DeployTimeout); err != nil {
		return fail(StateFailed, WrapInfrastructure(err))
	}
	if err := transition(StatePipelineReady, "pipeline ready"); err != nil {
		return fail(StateFailed, err)
	}
	if err := r.writeManifestArtifacts(runID.String(), deployedBundle); err != nil {
		return fail(StateFailed, fmt.Errorf("persist resolved manifests: %w", err))
	}

	r.report("benchmark: checking required Prometheus metrics")
	if err := r.waitForMetricPreflight(ctx, opts, metricDefs, ns); err != nil {
		return fail(StateFailed, WrapMetricsIncomplete(err))
	}

	if err := transition(StateMeasuring, "measuring"); err != nil {
		return fail(StateFailed, err)
	}
	r.report("benchmark: measuring for %s; health check every %s", opts.Duration, opts.HealthPollInterval)
	measureStart := r.deps.Clock.Now()
	if err := r.updateStatus(ctx, runID.String(), StateMeasuring, results.UpdateRunLifecycleParams{
		Status: string(StateMeasuring), MeasurementStartedAt: &measureStart,
	}); err != nil {
		return fail(StateFailed, err)
	}
	if err := r.waitMeasurement(ctx, opts, ns); err != nil {
		return fail(StateInterrupted, err)
	}
	measureEnd := r.deps.Clock.Now()
	if err := r.updateStatus(ctx, runID.String(), StateMeasuring, results.UpdateRunLifecycleParams{
		Status: string(StateMeasuring), MeasurementEndedAt: &measureEnd,
	}); err != nil {
		return fail(StateFailed, err)
	}

	r.report("benchmark: collecting metrics")
	if err := transition(StateCollecting, "collect metrics"); err != nil {
		return fail(StateFailed, err)
	}
	series, err := r.deps.Metrics.CollectRange(ctx, metricDefs, ns, measureStart, measureEnd, opts.PrometheusStep)
	if err != nil {
		return fail(StateFailed, WrapMetricsIncomplete(err))
	}
	for _, metricSeries := range series {
		if metricSeries.Warning != "" {
			r.recordRunWarning(ctx, runID.String(), string(StateCollecting),
				fmt.Sprintf("%s: %s", metricSeries.Metric.Name, metricSeries.Warning))
		}
	}
	if err := ValidateCollectedMetrics(metricDefs, series); err != nil {
		return fail(StateFailed, err)
	}
	saveParams, err := SeriesToSaveParams(runID.String(), measureStart, requiredNames, series)
	if err != nil {
		return fail(StateFailed, err)
	}
	if err := r.deps.Results.SaveMetrics(ctx, saveParams); err != nil {
		return fail(StateFailed, WrapMetricsIncomplete(err))
	}

	if err := r.captureDiagnostics(ctx, runID.String(), ns); err != nil {
		return fail(StateFailed, fmt.Errorf("persist diagnostics: %w", err))
	}

	r.report("benchmark: deleting scenario resources")
	if err := r.deps.Cluster.DeleteScenario(ctx, ns, sc, opts.NamespaceDeleteWait); err != nil {
		return fail(StateFailed, WrapInfrastructure(err))
	}
	scenarioCleanupRequired = false

	r.report("benchmark: deleting namespace %s", ns)
	if err := r.deps.Cluster.DeleteRunNamespace(ctx, ns, opts.NamespaceDeleteWait); err != nil {
		// Scenario resource finalizers have completed, so the namespace can now
		// be left for Kubernetes to finish deleting without the run controller.
		nsCreated = false
		return fail(StateFailed, WrapInfrastructure(err))
	}
	nsCreated = false

	completedAt := r.deps.Clock.Now()
	if err := transition(StateCompleted, "completed"); err != nil {
		return fail(StateFailed, err)
	}
	if err := r.updateStatus(ctx, runID.String(), StateCompleted, results.UpdateRunLifecycleParams{
		Status: string(StateCompleted), CompletedAt: &completedAt,
	}); err != nil {
		return fail(StateFailed, err)
	}
	rep.FinalState = StateCompleted
	rep.CompletedAt = completedAt
	r.report("benchmark: completed")
	return rep, nil
}

func (r *Runner) waitForMetricPreflight(ctx context.Context, opts Options, defs []scenario.MetricDefinition, ns string) error {
	deadline := r.deps.Clock.Now().Add(opts.DeployTimeout)
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.deps.Metrics.PreflightRequired(ctx, defs, ns); err == nil {
			return nil
		} else {
			lastErr = err
		}
		now := r.deps.Clock.Now()
		if !now.Before(deadline) {
			return lastErr
		}
		remaining := deadline.Sub(now)
		wait := opts.HealthPollInterval
		if wait > remaining {
			wait = remaining
		}
		r.report("benchmark: required metrics not ready; retrying in %s", wait)
		if err := r.deps.Sleeper.Sleep(ctx, wait); err != nil {
			return err
		}
	}
}

func (r *Runner) waitMeasurement(ctx context.Context, opts Options, ns string) error {
	deadline := r.deps.Clock.Now().Add(opts.Duration)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := r.deps.Clock.Now()
		if !now.Before(deadline) {
			return nil
		}
		remaining := deadline.Sub(now)
		sleepFor := opts.HealthPollInterval
		if sleepFor > remaining {
			sleepFor = remaining
		}
		if err := r.deps.Cluster.CheckMeasurementHealth(ctx, ns); err != nil {
			return WrapInfrastructure(err)
		}
		if err := r.deps.Sleeper.Sleep(ctx, sleepFor); err != nil {
			return err
		}
	}
}

func (r *Runner) updateStatus(ctx context.Context, runID string, state State, p results.UpdateRunLifecycleParams) error {
	if p.Status == "" {
		p.Status = string(state)
	}
	return r.deps.Results.UpdateRunLifecycle(ctx, runID, p)
}

func (r *Runner) recordRunWarning(ctx context.Context, runID, phase, msg string) {
	if r.deps.Results == nil || msg == "" {
		return
	}
	_ = r.deps.Results.AddEvent(ctx, results.AddEventParams{
		RunID: runID, Timestamp: r.deps.Clock.Now().UTC(),
		Level: "warning", Phase: phase, Message: msg,
	})
}

func (r *Runner) writeManifestArtifacts(runID string, bundle scenario.ManifestBundle) error {
	if r.deps.Artifacts == nil {
		return nil
	}
	if bundle.ISB != "" {
		if err := r.deps.Artifacts.WriteResolvedManifest(runID, "isb.yaml", []byte(bundle.ISB)); err != nil {
			return err
		}
	}
	if bundle.Pipeline != "" {
		if err := r.deps.Artifacts.WriteResolvedManifest(runID, "pipeline.yaml", []byte(bundle.Pipeline)); err != nil {
			return err
		}
	}
	if bundle.MonoVertex != "" {
		if err := r.deps.Artifacts.WriteResolvedManifest(runID, "monovertex.yaml", []byte(bundle.MonoVertex)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) captureDiagnostics(ctx context.Context, runID, ns string) error {
	if r.deps.Diagnostics == nil || r.deps.Artifacts == nil {
		return nil
	}
	bundle, err := r.deps.Diagnostics.Collect(ctx, ns)
	if err != nil {
		return err
	}
	var errs []error
	if err := r.deps.Artifacts.WriteDiagnostic(runID, "pods.txt", []byte(bundle.Pods)); err != nil {
		errs = append(errs, err)
	}
	if err := r.deps.Artifacts.WriteDiagnostic(runID, "controller.log", []byte(bundle.ControllerLog)); err != nil {
		errs = append(errs, err)
	}
	if err := r.deps.Artifacts.WriteDiagnostic(runID, "pipeline.log", []byte(bundle.PipelineLog)); err != nil {
		errs = append(errs, err)
	}
	if bundle.Events != "" {
		if err := r.deps.Artifacts.WriteDiagnostic(runID, "k8s-events.log", []byte(bundle.Events)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func lockHost() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "unknown"
	}
	return host
}

func (r *Runner) warnSameTagDifferentImage(ctx context.Context, opts Options, key namespace.BenchmarkKey, runID string) {
	if r.deps.Results == nil {
		return
	}
	existing, found, err := r.deps.Results.FindBenchmarkRunByNamespace(ctx, opts.Scenario, key.Namespace)
	if err != nil || !found || existing.ImageRef == opts.ImageRef {
		return
	}
	msg := fmt.Sprintf("namespace %q was previously used for image %q; this run uses %q (same tag segment %q)",
		key.Namespace, existing.ImageRef, opts.ImageRef, key.ImageTag)
	r.report("benchmark: warning: %s", msg)
	if runID != "" {
		r.recordRunWarning(ctx, runID, string(StateCreated), msg)
	}
}
