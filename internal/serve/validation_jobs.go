package serve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"numa-perfman/internal/artifacts"
	"numa-perfman/internal/benchmark"
	"numa-perfman/internal/central"
	"numa-perfman/internal/cluster"
	"numa-perfman/internal/controller"
	"numa-perfman/internal/diagnostics"
	"numa-perfman/internal/namespace"
	"numa-perfman/internal/oracle"
	"numa-perfman/internal/results"
	"numa-perfman/internal/runmeta"
	"numa-perfman/internal/validation"
)

// StartValidationRunRequest is the JSON body for POST /api/validation-runs.
type StartValidationRunRequest struct {
	Scenario      string `json:"scenario"`
	Image         string `json:"image"`
	Events        int64  `json:"events"`
	Tier          string `json:"tier"`
	Seed          string `json:"seed"`
	BaseEventTime string `json:"base_event_time"`
	Timeout       string `json:"timeout"`
	DumpFailureDB bool   `json:"dump_failure_db"`
}

// ValidationRunInfo is the API view of a validation job and its correctness details.
type ValidationRunInfo struct {
	ID             string                            `json:"id"`
	Scenario       string                            `json:"scenario"`
	ImageRef       string                            `json:"image_ref"`
	ImageDigest    string                            `json:"image_digest,omitempty"`
	Status         string                            `json:"status"`
	Namespace      string                            `json:"namespace,omitempty"`
	ErrorMessage   string                            `json:"error_message,omitempty"`
	CreatedAt      string                            `json:"created_at,omitempty"`
	CompletedAt    string                            `json:"completed_at,omitempty"`
	Seed           *int64                            `json:"seed,omitempty"`
	EventCount     *int64                            `json:"event_count,omitempty"`
	Active         bool                              `json:"active"`
	DisplayLabel   string                            `json:"display_label,omitempty"`
	MutableWarn    bool                              `json:"mutable_tag_warning,omitempty"`
	Validation     *validationResultInfo             `json:"validation,omitempty"`
	FailureSamples []results.ValidationFailureSample `json:"failure_samples,omitempty"`
}

type validationResultInfo struct {
	Passed                 bool    `json:"passed"`
	SourceCount            int64   `json:"source_count"`
	ExpectedCount          int64   `json:"expected_count"`
	LogicalOutputCount     int64   `json:"logical_output_count"`
	PhysicalDeliveryCount  int64   `json:"physical_delivery_count"`
	DuplicateDeliveryCount int64   `json:"duplicate_delivery_count"`
	DuplicateRate          float64 `json:"duplicate_rate"`
	MissingCount           int64   `json:"missing_count"`
	UnexpectedCount        int64   `json:"unexpected_count"`
	CorruptedCount         int64   `json:"corrupted_count"`
	RoutingMismatchCount   int64   `json:"routing_mismatch_count"`
	ChildMismatchCount     int64   `json:"child_mismatch_count"`
	DetailJSON             string  `json:"detail_json,omitempty"`
}

// StartValidation validates and asynchronously starts a data validation run.
func (m *JobManager) StartValidation(ctx context.Context, req StartValidationRunRequest) (ValidationRunInfo, error) {
	if m == nil {
		return ValidationRunInfo{}, fmt.Errorf("validation execution is not enabled")
	}
	opts, key, warnMutable, err := m.validateValidationStart(req)
	if err != nil {
		return ValidationRunInfo{}, err
	}
	lockKey := benchmark.ValidationLockKey(key)

	if lock, held, err := m.repo.ReadActiveLock(ctx, lockKey); err != nil {
		return ValidationRunInfo{}, err
	} else if held {
		return ValidationRunInfo{}, &JobConflictError{
			Code:    "lock_held",
			Message: "another validation is active for this scenario and image tag",
			Lock:    lockInfoFrom(lock),
		}
	}

	m.mu.Lock()
	if m.shutdown {
		m.mu.Unlock()
		return ValidationRunInfo{}, fmt.Errorf("server is shutting down")
	}
	if busy := m.findRunningJobLocked(lockKey); busy != nil {
		info := &JobInfo{
			ID:        busy.runID,
			Scenario:  busy.scenario,
			ImageRef:  busy.image,
			Status:    results.StatusValidating,
			Namespace: busy.ns,
			Active:    true,
		}
		m.mu.Unlock()
		return ValidationRunInfo{}, &JobConflictError{
			Code:        "lock_held",
			Message:     "another validation is active for this scenario and image tag",
			ExistingRun: info,
		}
	}

	invocationID := uuid.New()
	sc := opts.Scenarios[0]
	runID := validation.ScenarioRunID(invocationID, sc)
	jobCtx, cancel := context.WithCancel(context.Background())
	job := &activeJob{
		cancel:   cancel,
		runID:    runID.String(),
		scenario: string(sc),
		image:    opts.ImageRef,
		lockKey:  lockKey,
		ns:       key.Namespace,
		hub:      newEventHub(),
		done:     make(chan struct{}),
	}
	m.jobs[runID.String()] = job
	m.mu.Unlock()

	go m.executeValidation(jobCtx, job, invocationID, opts, key)

	return ValidationRunInfo{
		ID:           runID.String(),
		Scenario:     string(sc),
		ImageRef:     opts.ImageRef,
		Status:       results.StatusCreated,
		Namespace:    key.Namespace,
		Active:       true,
		MutableWarn:  warnMutable,
		DisplayLabel: fmt.Sprintf("%s - starting - %s", opts.ImageRef, runID.String()[:8]),
	}, nil
}

func (m *JobManager) validateValidationStart(req StartValidationRunRequest) (validation.Options, namespace.ValidationKey, bool, error) {
	scenarioID := trim(req.Scenario)
	image := trim(req.Image)
	if scenarioID == "" || image == "" {
		return validation.Options{}, namespace.ValidationKey{}, false, fmt.Errorf("%w: scenario and image are required", errBadRequest)
	}
	if err := controller.ValidateImageReference(image); err != nil {
		return validation.Options{}, namespace.ValidationKey{}, false, fmt.Errorf("%w: %v", errBadRequest, err)
	}
	if m.cfg.UDFImage == "" {
		return validation.Options{}, namespace.ValidationKey{}, false, fmt.Errorf("%w: udf image is required (configure via perfman config set --key udf_image)", errBadRequest)
	}
	if req.Events < 0 {
		return validation.Options{}, namespace.ValidationKey{}, false, fmt.Errorf("%w: events must be non-negative", errBadRequest)
	}
	tier := validation.Tier(trim(req.Tier))
	if tier == "" {
		tier = validation.TierStandard
	}
	switch tier {
	case validation.TierSmoke, validation.TierStandard, validation.TierSoak:
	default:
		return validation.Options{}, namespace.ValidationKey{}, false, fmt.Errorf("%w: tier must be smoke, standard, or soak", errBadRequest)
	}
	timeout := trim(req.Timeout)
	if timeout == "" {
		timeout = "90m"
	}
	tDur, err := time.ParseDuration(timeout)
	if err != nil {
		return validation.Options{}, namespace.ValidationKey{}, false, fmt.Errorf("%w: timeout: %v", errBadRequest, err)
	}
	key, err := namespace.ParseValidationKey(scenarioID, image)
	if err != nil {
		return validation.Options{}, namespace.ValidationKey{}, false, fmt.Errorf("%w: %v", errBadRequest, err)
	}
	opts := validation.Options{
		Scenarios:            []oracle.Scenario{oracle.Scenario(scenarioID)},
		ImageRef:             image,
		UDFImage:             m.cfg.UDFImage,
		Events:               req.Events,
		Tier:                 tier,
		Namespace:            key.Namespace,
		TotalTimeout:         tDur,
		DumpFailureDB:        req.DumpFailureDB,
		ValidationPostgresNS: m.cfg.ValidationNamespace,
	}
	if raw := trim(req.Seed); raw != "" {
		seed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return validation.Options{}, namespace.ValidationKey{}, false, fmt.Errorf("%w: seed must be an unsigned integer", errBadRequest)
		}
		opts.Seed = &seed
	}
	if raw := trim(req.BaseEventTime); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return validation.Options{}, namespace.ValidationKey{}, false, fmt.Errorf("%w: base-event-time: %v", errBadRequest, err)
		}
		opts.BaseEventTimeMs = t.UTC().UnixMilli()
	}
	return opts, key, runmeta.UsesMutableTag(image), nil
}

func (m *JobManager) executeValidation(ctx context.Context, job *activeJob, invocationID uuid.UUID, opts validation.Options, key namespace.ValidationKey) {
	defer func() {
		job.cancel()
		close(job.done)
	}()

	locker := benchmark.DatabaseLocker{Repo: m.repo}
	lockMeta := benchmark.LockMetadata{
		PID:       os.Getpid(),
		Host:      cluster.Hostname(),
		Command:   "serve validation run",
		RunID:     job.runID,
		Start:     time.Now().UTC(),
		Namespace: key.Namespace,
		Scenario:  key.Scenario,
		ImageTag:  key.ImageTag,
		ImageRef:  opts.ImageRef,
	}
	runLock, err := locker.Acquire(ctx, job.lockKey, lockMeta)
	if err != nil {
		m.failEarly(job, err)
		return
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = locker.Release(releaseCtx, runLock)
	}()

	pgFwd, port, err := startValidationPostgresForward(ctx, m.client, m.cfg.ValidationNamespace)
	if err != nil {
		m.failEarly(job, fmt.Errorf("postgres port-forward: %w", err))
		return
	}
	defer pgFwd.close()
	job.hub.Publish(LiveEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     "info",
		Phase:     "progress",
		Message:   fmt.Sprintf("postgres port-forward: 127.0.0.1:%d", port),
	})
	creds, err := validationPostgresAdminDSN(ctx, m.client, m.cfg.ValidationNamespace, port)
	if err != nil {
		m.failEarly(job, err)
		return
	}

	artWriter, err := artifacts.NewWriter(m.repo)
	if err != nil {
		m.failEarly(job, err)
		return
	}
	nsMgr := namespace.Manager{Cluster: m.client}
	clusterAdp := &validation.ClusterAdapter{
		Cluster: m.client, Namespaces: nsMgr, ValidationPostgresNS: m.cfg.ValidationNamespace,
	}
	db := &validation.PostgresAdapter{Creds: creds}
	runner := validation.NewRunner(validation.Dependencies{
		Cluster:  clusterAdp,
		Database: db,
		Events:   validationJobEventRecorder{repo: m.repo, hub: job.hub},
	})
	persist := &validation.PersistingRunner{
		Runner:      runner,
		Results:     validation.RepositoryResults{Repository: m.repo},
		Artifacts:   validation.ArtifactsFromWriter(artWriter, m.repo),
		Cluster:     clusterAdp,
		Diagnostics: &diagnostics.Collector{Cluster: m.client},
		Digest:      benchmark.PreflightAdapter{Cluster: m.client, KindCluster: m.cfg.Cluster},
		Environment: runmeta.StaticProvider{Snap: runmeta.FromConfig(m.cfg, m.version, runmeta.DefaultBenchmarkResources)},
		Database:    db,
		Invocation:  invocationID,
		OnScenarioStart: func(runID uuid.UUID, namespaceName string) error {
			lockMeta.RunID = runID.String()
			lockMeta.Namespace = namespaceName
			return locker.UpdateMetadata(ctx, runLock, lockMeta)
		},
	}

	_, err = persist.Run(ctx, invocationID, opts)
	info, getErr := m.GetValidation(context.Background(), job.runID)
	if getErr != nil && err == nil {
		err = getErr
	}
	if err != nil {
		phase := results.StatusFailed
		if getErr == nil && info.Status != "" {
			phase = info.Status
		}
		job.hub.Publish(LiveEvent{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Level:     "error",
			Phase:     phase,
			Message:   validationFailureMessage(info, err.Error()),
		})
		return
	}
	job.hub.Publish(LiveEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     "info",
		Phase:     results.StatusPassed,
		Message:   fmt.Sprintf("validation %s scenario=%s passed", job.runID, job.scenario),
	})
}

// GetValidation returns a validation run plus stored correctness details.
func (m *JobManager) GetValidation(ctx context.Context, runID string) (ValidationRunInfo, error) {
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
				return ValidationRunInfo{
					ID:           runID,
					Scenario:     job.scenario,
					ImageRef:     job.image,
					Namespace:    job.ns,
					Status:       results.StatusFailed,
					ErrorMessage: early.Error(),
					Active:       false,
				}, nil
			}
			return ValidationRunInfo{
				ID:        runID,
				Scenario:  job.scenario,
				ImageRef:  job.image,
				Namespace: job.ns,
				Status:    results.StatusCreated,
				Active:    active,
			}, nil
		}
		return ValidationRunInfo{}, fmt.Errorf("%w: run not found", errNotFound)
	}
	info, err := validationRunInfoFromRun(ctx, m.repo, run)
	if err != nil {
		return ValidationRunInfo{}, err
	}
	info.Active = active || (job != nil && !validationTerminal(run.Status))
	info.MutableWarn = runmeta.UsesMutableTag(run.ImageRef)
	return info, nil
}

// ListValidations returns validation runs, optionally filtered by scenario and status.
func (m *JobManager) ListValidations(ctx context.Context, scenarioID, status string, limit int) ([]ValidationRunInfo, error) {
	runs, err := m.repo.ListRuns(ctx, results.ListRunsFilter{
		Kind:     results.KindValidation,
		Scenario: scenarioID,
		Status:   status,
		Limit:    limit,
	})
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
	out := make([]ValidationRunInfo, 0, len(runs))
	for _, run := range runs {
		info, err := validationRunInfoFromRun(ctx, m.repo, run)
		if err != nil {
			return nil, err
		}
		_, info.Active = activeIDs[run.ID]
		out = append(out, info)
	}
	return out, nil
}

func validationRunInfoFromRun(ctx context.Context, repo *results.Repository, run results.Run) (ValidationRunInfo, error) {
	info := ValidationRunInfo{
		ID:           run.ID,
		Scenario:     run.Scenario,
		ImageRef:     run.ImageRef,
		ImageDigest:  run.ImageDigest,
		Status:       run.Status,
		Namespace:    run.Namespace,
		ErrorMessage: run.ErrorMessage,
		Seed:         run.Seed,
		EventCount:   run.EventCount,
		DisplayLabel: runDisplayLabel(run),
	}
	if !run.CreatedAt.IsZero() {
		info.CreatedAt = run.CreatedAt.UTC().Format(time.RFC3339)
	}
	if run.CompletedAt != nil {
		info.CompletedAt = run.CompletedAt.UTC().Format(time.RFC3339)
	}
	if repo == nil {
		return info, nil
	}
	if vr, ok, err := repo.GetValidationResult(ctx, run.ID); err != nil {
		return ValidationRunInfo{}, err
	} else if ok {
		info.Validation = validationResultInfoFrom(vr)
		samples, err := repo.LoadValidationFailureSamples(ctx, run.ID)
		if err != nil {
			return ValidationRunInfo{}, err
		}
		info.FailureSamples = samples
	}
	return info, nil
}

func validationResultInfoFrom(v results.ValidationResult) *validationResultInfo {
	return &validationResultInfo{
		Passed:                 v.Passed,
		SourceCount:            v.SourceCount,
		ExpectedCount:          v.ExpectedCount,
		LogicalOutputCount:     v.LogicalOutputCount,
		PhysicalDeliveryCount:  v.PhysicalDeliveryCount,
		DuplicateDeliveryCount: v.DuplicateDeliveryCount,
		DuplicateRate:          v.DuplicateRate,
		MissingCount:           v.MissingCount,
		UnexpectedCount:        v.UnexpectedCount,
		CorruptedCount:         v.CorruptedCount,
		RoutingMismatchCount:   v.RoutingMismatchCount,
		ChildMismatchCount:     v.ChildMismatchCount,
		DetailJSON:             v.DetailJSON,
	}
}

func validationTerminal(status string) bool {
	switch status {
	case results.StatusPassed, results.StatusFailed, results.StatusInterrupted:
		return true
	default:
		return false
	}
}

func validationFailureMessage(info ValidationRunInfo, fallback string) string {
	var parts []string
	if info.Validation != nil {
		add := func(label string, n int64) {
			if n > 0 {
				parts = append(parts, fmt.Sprintf("%s=%d", label, n))
			}
		}
		add("missing", info.Validation.MissingCount)
		add("unexpected", info.Validation.UnexpectedCount)
		add("corrupted", info.Validation.CorruptedCount)
		add("routing_mismatch", info.Validation.RoutingMismatchCount)
		add("child_mismatch", info.Validation.ChildMismatchCount)
		add("duplicates", info.Validation.DuplicateDeliveryCount)
	}
	if len(parts) > 0 {
		msg := "validation failed: " + strings.Join(parts, ", ")
		if len(info.FailureSamples) > 0 {
			s := info.FailureSamples[0]
			msg += fmt.Sprintf("; sample kind=%s key=%s", s.Kind, s.LogicalKey)
		}
		return msg
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return "validation failed"
}

type validationJobEventRecorder struct {
	repo *results.Repository
	hub  *eventHub
}

func (r validationJobEventRecorder) RecordPhase(ctx context.Context, runID uuid.UUID, phase validation.Phase, message string, detail any) error {
	ts := time.Now().UTC()
	detailJSON := ""
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			detailJSON = string(b)
		}
	}
	level := "info"
	if phase == validation.PhaseFailed {
		level = "error"
	}
	if r.hub != nil {
		r.hub.Publish(LiveEvent{
			Timestamp: ts.Format(time.RFC3339),
			Level:     level,
			Phase:     string(phase),
			Message:   message,
		})
	}
	if r.repo == nil {
		return nil
	}
	if err := r.repo.AddEvent(ctx, results.AddEventParams{
		RunID: runID.String(), Timestamp: ts, Level: level, Phase: string(phase), Message: message, DetailJSON: detailJSON,
	}); err != nil {
		return err
	}
	status := validationPhaseStatus(phase)
	if status == "" {
		return nil
	}
	p := results.UpdateRunLifecycleParams{Status: status}
	if validationTerminal(status) {
		p.CompletedAt = &ts
	}
	return r.repo.UpdateRunLifecycle(ctx, runID.String(), p)
}

func validationPhaseStatus(phase validation.Phase) string {
	switch phase {
	case validation.PhaseCreated:
		return results.StatusCreated
	case validation.PhaseGenerating:
		return results.StatusGenerating
	case validation.PhaseNamespaceCreated:
		return results.StatusNamespaceCreated
	case validation.PhaseControllerReady:
		return results.StatusControllerReady
	case validation.PhasePipelineReady:
		return results.StatusPipelineReady
	case validation.PhaseSending:
		return results.StatusSending
	case validation.PhaseDraining:
		return results.StatusDraining
	case validation.PhaseValidating:
		return results.StatusValidating
	case validation.PhasePassed:
		return results.StatusPassed
	case validation.PhaseFailed:
		return results.StatusFailed
	default:
		return ""
	}
}

type validationForwardSession struct {
	proc cluster.Process
	kill func() error
}

func (s *validationForwardSession) close() {
	if s == nil {
		return
	}
	if s.kill != nil {
		_ = s.kill()
	}
}

func startValidationPostgresForward(ctx context.Context, client cluster.Client, validationNS string) (*validationForwardSession, int, error) {
	port, err := cluster.FreeTCPPort()
	if err != nil {
		return nil, 0, err
	}
	local := strconv.Itoa(port)
	proc, err := client.PortForward(ctx, validationNS, "svc/"+central.PostgresServiceName, local, "5432")
	if err != nil {
		return nil, 0, err
	}
	if err := cluster.WaitTCP(ctx, "127.0.0.1:"+local, 90*time.Second); err != nil {
		_ = proc.Kill()
		return nil, 0, err
	}
	return &validationForwardSession{proc: proc, kill: proc.Kill}, port, nil
}

func validationPostgresAdminDSN(ctx context.Context, client cluster.Client, validationNS string, localPort int) (validation.PostgresCredentials, error) {
	user, pass, err := readValidationPostgresSecret(ctx, client, validationNS)
	if err != nil {
		return validation.PostgresCredentials{}, err
	}
	host := "127.0.0.1"
	port := strconv.Itoa(localPort)
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, pass),
		Host:   fmt.Sprintf("%s:%s", host, port),
		Path:   "/postgres",
	}
	return validation.PostgresCredentials{
		AdminDSN: u.String(),
		User:     user,
		Password: pass,
		Host:     host,
		Port:     port,
	}, nil
}

func readValidationPostgresSecret(ctx context.Context, client cluster.Client, ns string) (user, password string, err error) {
	out, err := client.GetResourceJSON(ctx, ns, "secret/"+central.PostgresSecretName)
	if err != nil {
		return "numaflow", "numaflow", nil
	}
	var obj struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		return "", "", err
	}
	decode := func(key, def string) string {
		raw, ok := obj.Data[key]
		if !ok || raw == "" {
			return def
		}
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return def
		}
		return string(b)
	}
	return decode("POSTGRES_USER", "numaflow"), decode("POSTGRES_PASSWORD", "numaflow"), nil
}
