package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"numa-perfman/internal/artifacts"
	"numa-perfman/internal/benchmark"
	"numa-perfman/internal/cluster"
	"numa-perfman/internal/config"
	"numa-perfman/internal/controller"
	"numa-perfman/internal/diagnostics"
	"numa-perfman/internal/namespace"
	prom "numa-perfman/internal/prometheus"
	"numa-perfman/internal/results"
	"numa-perfman/internal/runmeta"
)

// StartRunRequest is the JSON body for POST /api/benchmark-runs.
type StartRunRequest struct {
	Scenario  string `json:"scenario"`
	Image     string `json:"image"`
	Duration  string `json:"duration"`
	Overwrite bool   `json:"overwrite"`
}

// JobConflictError is returned when a run cannot start due to lock or overwrite policy.
type JobConflictError struct {
	Code        string    `json:"code"`
	Message     string    `json:"message"`
	ExistingRun *JobInfo  `json:"existing_run,omitempty"`
	Lock        *lockInfo `json:"lock,omitempty"`
}

type lockInfo struct {
	LockKey   string `json:"lock_key"`
	PID       int    `json:"pid"`
	Host      string `json:"host"`
	Command   string `json:"command"`
	RunID     string `json:"run_id"`
	Namespace string `json:"namespace"`
	Scenario  string `json:"scenario"`
	ImageRef  string `json:"image_ref"`
	ImageTag  string `json:"image_tag"`
	StartedAt string `json:"started_at"`
}

func lockInfoFrom(lock results.ActiveLock) *lockInfo {
	return &lockInfo{
		LockKey:   lock.LockKey,
		PID:       lock.PID,
		Host:      lock.Host,
		Command:   lock.Command,
		RunID:     lock.RunID,
		Namespace: lock.Namespace,
		Scenario:  lock.Scenario,
		ImageRef:  lock.ImageRef,
		ImageTag:  lock.ImageTag,
		StartedAt: lock.StartedAt.UTC().Format(time.RFC3339),
	}
}

func (e *JobConflictError) Error() string { return e.Message }

// JobInfo is the API view of a benchmark job.
type JobInfo struct {
	ID           string `json:"id"`
	Scenario     string `json:"scenario"`
	ImageRef     string `json:"image_ref"`
	ImageDigest  string `json:"image_digest,omitempty"`
	Status       string `json:"status"`
	Namespace    string `json:"namespace,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	CompletedAt  string `json:"completed_at,omitempty"`
	MutableWarn  bool   `json:"mutable_tag_warning,omitempty"`
	Active       bool   `json:"active"`
	DisplayLabel string `json:"display_label,omitempty"`
}

// LiveEvent is a progress/phase update for SSE or polling.
type LiveEvent struct {
	ID        int64  `json:"id"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Phase     string `json:"phase"`
	Message   string `json:"message"`
}

type activeJob struct {
	cancel   context.CancelFunc
	runID    string
	scenario string
	image    string
	lockKey  string
	ns       string
	hub      *eventHub
	mu       sync.Mutex
	earlyErr error
	done     chan struct{}
}

func (j *activeJob) isRunning() bool {
	if j == nil {
		return false
	}
	select {
	case <-j.done:
		return false
	default:
		return true
	}
}

type eventHub struct {
	mu   sync.Mutex
	next int64
	subs map[chan LiveEvent]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subs: make(map[chan LiveEvent]struct{})}
}

func (h *eventHub) Publish(ev LiveEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ev.ID == 0 {
		h.next++
		ev.ID = h.next
	} else if ev.ID > h.next {
		h.next = ev.ID
	}
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (h *eventHub) Subscribe() chan LiveEvent {
	ch := make(chan LiveEvent, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *eventHub) Unsubscribe(ch chan LiveEvent) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
	close(ch)
}

// JobManager runs UI-triggered benchmarks inside serve.
// Multiple jobs may run in parallel when their (scenario, image-tag) lock keys differ,
// matching CLI concurrency. The same scenario+tag combination is serialized.
type JobManager struct {
	repo    *results.Repository
	cfg     config.Config
	client  cluster.Client
	version string

	mu       sync.Mutex
	jobs     map[string]*activeJob
	shutdown bool
}

// NewJobManager constructs a job manager backed by the shared results repository.
func NewJobManager(repo *results.Repository, cfg config.Config, client cluster.Client, version string) *JobManager {
	return &JobManager{
		repo:    repo,
		cfg:     cfg,
		client:  client,
		version: version,
		jobs:    make(map[string]*activeJob),
	}
}

// Start validates and asynchronously starts a benchmark run.
func (m *JobManager) Start(ctx context.Context, req StartRunRequest) (JobInfo, error) {
	if m == nil {
		return JobInfo{}, fmt.Errorf("benchmark execution is not enabled")
	}
	opts, warnMutable, err := m.validateStart(req)
	if err != nil {
		return JobInfo{}, err
	}
	key, err := namespace.ParseBenchmarkKey(opts.Scenario, opts.ImageRef)
	if err != nil {
		return JobInfo{}, fmt.Errorf("%w: %v", errBadRequest, err)
	}
	lockKey := benchmark.BenchmarkLockKey(key)

	existing, found, err := m.repo.FindBenchmarkRun(ctx, opts.Scenario, opts.ImageRef)
	if err != nil {
		return JobInfo{}, err
	}
	if found && !req.Overwrite {
		info := jobInfoFromRun(existing)
		return JobInfo{}, &JobConflictError{
			Code:        "overwrite_required",
			Message:     fmt.Sprintf("benchmark result already exists for scenario %q and image %q", opts.Scenario, opts.ImageRef),
			ExistingRun: &info,
		}
	}

	if lock, held, err := m.repo.ReadActiveLock(ctx, lockKey); err != nil {
		return JobInfo{}, err
	} else if held {
		return JobInfo{}, &JobConflictError{
			Code:    "lock_held",
			Message: "another benchmark is active for this scenario and image tag",
			Lock:    lockInfoFrom(lock),
		}
	}

	m.mu.Lock()
	if m.shutdown {
		m.mu.Unlock()
		return JobInfo{}, fmt.Errorf("server is shutting down")
	}
	if busy := m.findRunningJobLocked(lockKey); busy != nil {
		info := &JobInfo{
			ID:       busy.runID,
			Scenario: busy.scenario,
			ImageRef: busy.image,
			Status:   results.StatusMeasuring,
			Active:   true,
		}
		m.mu.Unlock()
		return JobInfo{}, &JobConflictError{
			Code:        "lock_held",
			Message:     "another benchmark is active for this scenario and image tag",
			ExistingRun: info,
		}
	}

	runID := uuid.New()
	jobCtx, cancel := context.WithCancel(context.Background())
	job := &activeJob{
		cancel:   cancel,
		runID:    runID.String(),
		scenario: opts.Scenario,
		image:    opts.ImageRef,
		lockKey:  lockKey,
		ns:       key.Namespace,
		hub:      newEventHub(),
		done:     make(chan struct{}),
	}
	m.jobs[runID.String()] = job
	m.mu.Unlock()

	go m.execute(jobCtx, job, opts, req.Overwrite, fixedID{id: runID})

	info := JobInfo{
		ID:           runID.String(),
		Scenario:     opts.Scenario,
		ImageRef:     opts.ImageRef,
		Status:       results.StatusCreated,
		Namespace:    key.Namespace,
		Active:       true,
		MutableWarn:  warnMutable,
		DisplayLabel: fmt.Sprintf("%s — starting — %s", opts.ImageRef, runID.String()[:8]),
	}
	return info, nil
}

func (m *JobManager) validateStart(req StartRunRequest) (benchmark.Options, bool, error) {
	scenarioID := trim(req.Scenario)
	image := trim(req.Image)
	if scenarioID == "" || image == "" {
		return benchmark.Options{}, false, fmt.Errorf("%w: scenario and image are required", errBadRequest)
	}
	if err := controller.ValidateImageReference(image); err != nil {
		return benchmark.Options{}, false, fmt.Errorf("%w: %v", errBadRequest, err)
	}
	duration := trim(req.Duration)
	if duration == "" {
		duration = "15m"
	}
	mDur, err := time.ParseDuration(duration)
	if err != nil {
		return benchmark.Options{}, false, fmt.Errorf("%w: duration: %v", errBadRequest, err)
	}
	if m.cfg.UDFImage == "" {
		return benchmark.Options{}, false, fmt.Errorf("%w: udf image is required (configure via perfman config set --key udf_image)", errBadRequest)
	}
	opts := benchmark.Options{
		Scenario: scenarioID,
		ImageRef: image,
		UDFImage: m.cfg.UDFImage,
		Duration: mDur,
		Command:  "serve benchmark run",
	}
	if err := opts.Validate(); err != nil {
		return benchmark.Options{}, false, fmt.Errorf("%w: %v", errBadRequest, err)
	}
	return opts, runmeta.UsesMutableTag(image), nil
}

func (m *JobManager) findRunningJobLocked(lockKey string) *activeJob {
	for _, job := range m.jobs {
		if job.lockKey == lockKey && job.isRunning() {
			return job
		}
	}
	return nil
}

func (m *JobManager) execute(ctx context.Context, job *activeJob, opts benchmark.Options, overwrite bool, ids fixedID) {
	defer func() {
		job.cancel()
		close(job.done)
	}()

	var promFwd *cluster.ForwardSession
	promURL := m.cfg.PrometheusURL
	if cluster.ShouldAutoPrometheus(promURL) {
		sess, err := m.client.StartPrometheusForward(ctx, m.cfg.MonitoringNamespace)
		if err != nil {
			m.failEarly(job, fmt.Errorf("prometheus port-forward: %w", err))
			return
		}
		promFwd = sess
		promURL = sess.URL
		defer promFwd.Close()
		job.hub.Publish(LiveEvent{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Level:     "info",
			Phase:     "progress",
			Message:   "prometheus port-forward: " + promURL,
		})
	}

	artWriter, err := artifacts.NewWriter(m.repo)
	if err != nil {
		m.failEarly(job, err)
		return
	}
	nsMgr := namespace.Manager{Cluster: m.client}
	clusterAdp := &benchmark.ClusterAdapter{
		Cluster: m.client, Namespaces: nsMgr, KindCluster: m.cfg.Cluster,
	}
	promClient := prom.Client{BaseURL: promURL, HTTPClient: &http.Client{Timeout: 30 * time.Second}}
	progress := &jobProgress{hub: job.hub, repo: m.repo, runID: job.runID}
	events := &jobEventRecorder{repo: m.repo, hub: job.hub}
	runner := benchmark.NewRunner(benchmark.Dependencies{
		Preflight: benchmark.PreflightAdapter{Cluster: m.client, KindCluster: m.cfg.Cluster},
		Cluster:   clusterAdp,
		Metrics:   benchmark.MetricsAdapter{Client: promClient},
		Results:   m.repo,
		Artifacts: benchmark.ArtifactsFromWriter(artWriter),
		Diagnostics: benchmark.DiagnosticsAdapter{
			Collector: diagnostics.Collector{Cluster: m.client},
		},
		Locker:      benchmark.DatabaseLocker{Repo: m.repo},
		Events:      events,
		Progress:    progress,
		Overwrite:   apiOverwrite{allow: overwrite},
		Environment: runmeta.StaticProvider{Snap: runmeta.FromConfig(m.cfg, m.version, runmeta.DefaultBenchmarkResources)},
		IDs:         ids,
	})

	result, err := runner.Run(ctx, opts)
	if err != nil {
		if errors.Is(err, benchmark.ErrLockHeld) {
			m.failEarly(job, err)
			return
		}
		if errors.Is(err, benchmark.ErrOverwriteDeclined) {
			m.failEarly(job, err)
			return
		}
		// Runner persists terminal state for most failures after CreateRun.
		job.hub.Publish(LiveEvent{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Level:     "error",
			Phase:     string(result.FinalState),
			Message:   err.Error(),
		})
		return
	}
	job.hub.Publish(LiveEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     "info",
		Phase:     string(result.FinalState),
		Message:   fmt.Sprintf("run %s namespace=%s status=%s", result.RunID, result.Namespace, result.FinalState),
	})
}

func (m *JobManager) failEarly(job *activeJob, err error) {
	job.mu.Lock()
	job.earlyErr = err
	job.mu.Unlock()
	job.hub.Publish(LiveEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     "error",
		Phase:     results.StatusFailed,
		Message:   err.Error(),
	})
}

// Cancel requests graceful cancellation of an in-flight UI job.
func (m *JobManager) Cancel(runID string) error {
	m.mu.Lock()
	job := m.jobs[runID]
	m.mu.Unlock()
	if job == nil {
		run, err := m.repo.GetRun(context.Background(), runID)
		if err != nil {
			return fmt.Errorf("%w: run not found", errNotFound)
		}
		if benchmark.Terminal(run.Status) {
			return fmt.Errorf("%w: run already terminal (%s)", errConflict, run.Status)
		}
		return fmt.Errorf("%w: run is not managed by this serve process", errConflict)
	}
	job.cancel()
	return nil
}

// Get returns the current job info from the repository (or early failure state).
func (m *JobManager) Get(ctx context.Context, runID string) (JobInfo, error) {
	m.mu.Lock()
	job := m.jobs[runID]
	active := job.isRunning()
	m.mu.Unlock()

	run, err := m.repo.GetRun(ctx, runID)
	if err != nil {
		if job != nil {
			job.mu.Lock()
			early := job.earlyErr
			job.mu.Unlock()
			if early != nil {
				return JobInfo{
					ID:           runID,
					Scenario:     job.scenario,
					ImageRef:     job.image,
					Namespace:    job.ns,
					Status:       results.StatusFailed,
					ErrorMessage: early.Error(),
					Active:       false,
				}, nil
			}
			return JobInfo{
				ID:        runID,
				Scenario:  job.scenario,
				ImageRef:  job.image,
				Namespace: job.ns,
				Status:    results.StatusCreated,
				Active:    active,
			}, nil
		}
		return JobInfo{}, fmt.Errorf("%w: run not found", errNotFound)
	}
	info := jobInfoFromRun(run)
	info.Active = active || (job != nil && !benchmark.Terminal(run.Status))
	info.MutableWarn = runmeta.UsesMutableTag(run.ImageRef)
	return info, nil
}

// List returns benchmark runs, optionally filtered by scenario and status.
func (m *JobManager) List(ctx context.Context, scenarioID, status string, limit int) ([]JobInfo, error) {
	filter := results.ListRunsFilter{
		Kind:     results.KindBenchmark,
		Scenario: scenarioID,
		Status:   status,
		Limit:    limit,
	}
	runs, err := m.repo.ListRuns(ctx, filter)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	activeIDs := make(map[string]struct{})
	for id, job := range m.jobs {
		if job.isRunning() {
			activeIDs[id] = struct{}{}
		}
	}
	m.mu.Unlock()
	out := make([]JobInfo, 0, len(runs))
	for _, run := range runs {
		info := jobInfoFromRun(run)
		_, info.Active = activeIDs[run.ID]
		out = append(out, info)
	}
	return out, nil
}

// ListEvents returns persisted events for a run, optionally after a given event id.
func (m *JobManager) ListEvents(ctx context.Context, runID string, afterID int64) ([]LiveEvent, error) {
	events, err := m.repo.ListEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	out := make([]LiveEvent, 0, len(events))
	for _, ev := range events {
		if ev.ID <= afterID {
			continue
		}
		out = append(out, LiveEvent{
			ID:        ev.ID,
			Timestamp: ev.Timestamp.UTC().Format(time.RFC3339),
			Level:     ev.Level,
			Phase:     ev.Phase,
			Message:   ev.Message,
		})
	}
	return out, nil
}

// Subscribe returns a live event channel for an active job, or nil when not active.
func (m *JobManager) Subscribe(runID string) (<-chan LiveEvent, func(), bool) {
	m.mu.Lock()
	job := m.jobs[runID]
	m.mu.Unlock()
	if job == nil {
		return nil, func() {}, false
	}
	ch := job.hub.Subscribe()
	unsub := func() { job.hub.Unsubscribe(ch) }
	return ch, unsub, true
}

// WaitDone waits until the job finishes or the context is cancelled.
func (m *JobManager) WaitDone(ctx context.Context, runID string) error {
	m.mu.Lock()
	job := m.jobs[runID]
	m.mu.Unlock()
	if job == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-job.done:
		return nil
	}
}

// Shutdown cancels active jobs and waits up to timeout for cleanup.
func (m *JobManager) Shutdown(timeout time.Duration) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.shutdown = true
	var jobs []*activeJob
	for _, job := range m.jobs {
		jobs = append(jobs, job)
		job.cancel()
	}
	m.mu.Unlock()
	deadline := time.After(timeout)
	for _, job := range jobs {
		select {
		case <-job.done:
		case <-deadline:
			return
		}
	}
}

func jobInfoFromRun(run results.Run) JobInfo {
	info := JobInfo{
		ID:           run.ID,
		Scenario:     run.Scenario,
		ImageRef:     run.ImageRef,
		ImageDigest:  run.ImageDigest,
		Status:       run.Status,
		Namespace:    run.Namespace,
		ErrorMessage: run.ErrorMessage,
		DisplayLabel: runDisplayLabel(run),
	}
	if !run.CreatedAt.IsZero() {
		info.CreatedAt = run.CreatedAt.UTC().Format(time.RFC3339)
	}
	if run.CompletedAt != nil {
		info.CompletedAt = run.CompletedAt.UTC().Format(time.RFC3339)
	}
	return info
}

type fixedID struct{ id uuid.UUID }

func (f fixedID) New() uuid.UUID { return f.id }

type apiOverwrite struct{ allow bool }

func (a apiOverwrite) ConfirmOverwrite(_ context.Context, _ results.Run) (bool, error) {
	return a.allow, nil
}

type jobProgress struct {
	hub   *eventHub
	repo  *results.Repository
	runID string
}

func (p *jobProgress) Report(message string) {
	ts := time.Now().UTC()
	p.hub.Publish(LiveEvent{
		Timestamp: ts.Format(time.RFC3339),
		Level:     "info",
		Phase:     "progress",
		Message:   message,
	})
	if p.repo != nil && p.runID != "" {
		_ = p.repo.AddEvent(context.Background(), results.AddEventParams{
			RunID: p.runID, Timestamp: ts, Level: "info", Phase: "progress", Message: message,
		})
	}
}

type jobEventRecorder struct {
	repo *results.Repository
	hub  *eventHub
}

func (r *jobEventRecorder) RecordPhase(ctx context.Context, runID string, state benchmark.State, message string, _ any) error {
	ts := time.Now().UTC()
	if r.hub != nil {
		r.hub.Publish(LiveEvent{
			Timestamp: ts.Format(time.RFC3339),
			Level:     "info",
			Phase:     string(state),
			Message:   message,
		})
	}
	if r.repo == nil {
		return nil
	}
	return r.repo.AddEvent(ctx, results.AddEventParams{
		RunID: runID, Timestamp: ts, Level: "info", Phase: string(state), Message: message,
	})
}

func trim(s string) string { return strings.TrimSpace(s) }
