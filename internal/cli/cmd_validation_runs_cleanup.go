package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/benchmark"
	"numa-perfman/internal/cluster"
	"numa-perfman/internal/config"
	"numa-perfman/internal/diagnostics"
	"numa-perfman/internal/namespace"
	"numa-perfman/internal/oracle"
	"numa-perfman/internal/results"
	"numa-perfman/internal/runmeta"
	"numa-perfman/internal/scenario"
	"numa-perfman/internal/validation"
)

func (a *App) cmdValidation(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: validation requires list or run", errUsage)
	}
	switch args[0] {
	case "list":
		return a.validationList(args[1:])
	case "run":
		return a.validationRun(ctx, cfg, args[1:])
	case "help", "-h", "--help":
		printValidationUsage(a.Stdout)
		return nil
	default:
		return fmt.Errorf("%w: unknown validation subcommand %q", errUsage, args[0])
	}
}

func (a *App) validationList(args []string) error {
	fs := flag.NewFlagSet("validation list", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	for _, id := range scenario.ListValidations() {
		sc, err := scenario.LookupValidation(id)
		if err != nil {
			continue
		}
		fmt.Fprintf(a.Stdout, "%s\t%s\n", sc.ID, sc.Description)
	}
	return nil
}

func (a *App) validationRun(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("validation run", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var image, scenarioID string
	var events int64
	var tier, timeout, baseEventTime string
	var seed uint64
	var seedProvided bool
	var dumpFail bool
	fs.StringVar(&image, "image", "", "complete Numaflow image reference")
	fs.StringVar(&scenarioID, "scenario", "", "validation scenario ID")
	fs.Int64Var(&events, "events", 0, "event count")
	fs.StringVar(&tier, "tier", "standard", "smoke|standard|soak")
	fs.Uint64Var(&seed, "seed", 0, "deterministic seed")
	fs.StringVar(&baseEventTime, "base-event-time", "", "RFC3339 base event time")
	fs.StringVar(&timeout, "timeout", "90m", "total timeout")
	fs.BoolVar(&dumpFail, "dump-failure-db", false, "dump failed scenario database")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "seed" {
			seedProvided = true
		}
	})
	if image == "" || scenarioID == "" {
		return fmt.Errorf("%w: --scenario and --image are required", errUsage)
	}
	warnMutableImage(a.Stderr, image)
	if cfg.UDFImage == "" {
		return fmt.Errorf("%w: udf image is required", errUsage)
	}
	tDur, err := time.ParseDuration(timeout)
	if err != nil {
		return fmt.Errorf("%w: timeout: %v", errUsage, err)
	}
	key, err := namespace.ParseValidationKey(scenarioID, image)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	oracleScenario := oracle.Scenario(scenarioID)

	if err := cfg.EnsureResultsLayout(); err != nil {
		return fmt.Errorf("%w: %v", errPreflight, err)
	}

	repo, err := openResultsRepo(ctx, cfg)
	if err != nil {
		return err
	}
	defer repo.Close()

	invocationID := uuid.New()
	scenarioRunID := validation.ScenarioRunID(invocationID, oracleScenario)
	locker := benchmark.DatabaseLocker{Repo: repo}
	lockKey := benchmark.ValidationLockKey(key)
	lockMeta := benchmark.LockMetadata{
		PID:       os.Getpid(),
		Host:      cluster.Hostname(),
		Command:   "validation run",
		RunID:     scenarioRunID.String(),
		Start:     time.Now().UTC(),
		Namespace: key.Namespace,
		Scenario:  key.Scenario,
		ImageTag:  key.ImageTag,
		ImageRef:  image,
	}
	runLock, err := locker.Acquire(ctx, lockKey, lockMeta)
	if err != nil {
		return err
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = locker.Release(releaseCtx, runLock)
	}()

	client := a.clusterClient(cfg)
	var pgFwd *forwardSession
	creds := validation.PostgresCredentials{}
	if cfg.ValidationNamespace != "" {
		sess, port, err := startPostgresForward(ctx, client, cfg.ValidationNamespace)
		if err != nil {
			return fmt.Errorf("%w: %v", errPreflight, err)
		}
		pgFwd = sess
		defer pgFwd.close()
		creds, err = postgresAdminDSN(ctx, client, cfg.ValidationNamespace, port)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Stdout, "postgres port-forward: 127.0.0.1:%s (user=%s)\n", creds.Port, creds.User)
	}

	opts := validation.Options{
		Scenarios:            []oracle.Scenario{oracleScenario},
		ImageRef:             image,
		UDFImage:             cfg.UDFImage,
		Events:               events,
		Tier:                 validation.Tier(tier),
		Namespace:            key.Namespace,
		TotalTimeout:         tDur,
		DumpFailureDB:        dumpFail,
		ValidationPostgresNS: cfg.ValidationNamespace,
	}
	if seedProvided {
		opts.Seed = &seed
	}
	if baseEventTime != "" {
		t, err := time.Parse(time.RFC3339, baseEventTime)
		if err != nil {
			return fmt.Errorf("%w: base-event-time: %v", errUsage, err)
		}
		opts.BaseEventTimeMs = t.UTC().UnixMilli()
	}

	resolvedSeed := opts.ResolvedSeed()
	fmt.Fprintf(a.Stdout, "validation seed: %d namespace=%s\n", resolvedSeed, key.Namespace)

	nsMgr := namespace.Manager{Cluster: client}
	clusterAdp := &validation.ClusterAdapter{
		Cluster: client, Namespaces: nsMgr, ValidationPostgresNS: cfg.ValidationNamespace,
	}
	runner := validation.NewRunner(validation.Dependencies{
		Cluster:  clusterAdp,
		Database: &validation.PostgresAdapter{Creds: creds},
		Events:   validation.NewResultsEventRecorder(repo, nil),
	})
	persist := &validation.PersistingRunner{
		Runner:      runner,
		Results:     validation.RepositoryResults{Repository: repo},
		Artifacts:   validation.ArtifactsFromRepository(repo),
		Cluster:     clusterAdp,
		Diagnostics: &diagnostics.Collector{Cluster: client},
		Digest:      benchmark.PreflightAdapter{Cluster: client, KindCluster: cfg.Cluster},
		Environment: runmeta.StaticProvider{Snap: runmeta.FromConfig(cfg, versionString(), runmeta.DefaultBenchmarkResources)},
		Database:    &validation.PostgresAdapter{Creds: creds},
		Invocation:  invocationID,
		OnScenarioStart: func(runID uuid.UUID, namespaceName string) error {
			lockMeta.RunID = runID.String()
			lockMeta.Namespace = namespaceName
			return locker.UpdateMetadata(ctx, runLock, lockMeta)
		},
	}

	out, err := persist.Run(ctx, invocationID, opts)
	for _, sr := range out.RunResult.Scenarios {
		fmt.Fprintf(a.Stdout, "scenario=%s passed=%v phase=%s\n", sr.Scenario, sr.Passed, sr.Phase)
	}
	return err
}

func (a *App) cmdRuns(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: runs requires list, show, delete, or artifacts", errUsage)
	}
	switch args[0] {
	case "list":
		return a.runsList(ctx, cfg, args[1:])
	case "show":
		return a.runsShow(ctx, cfg, args[1:])
	case "delete":
		return a.runsDelete(ctx, cfg, args[1:])
	case "artifacts":
		return a.runsArtifacts(ctx, cfg, args[1:])
	case "help", "-h", "--help":
		printRunsUsage(a.Stdout)
		return nil
	default:
		return fmt.Errorf("%w: unknown runs subcommand %q", errUsage, args[0])
	}
}

func (a *App) runsList(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("runs list", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var kind, scenarioID, status string
	var limit int
	fs.StringVar(&kind, "kind", "", "benchmark or validation")
	fs.StringVar(&scenarioID, "scenario", "", "scenario filter")
	fs.StringVar(&status, "status", "", "status filter")
	fs.IntVar(&limit, "limit", 50, "max rows")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	repo, err := openResultsRepo(ctx, cfg)
	if err != nil {
		return err
	}
	defer repo.Close()
	runs, err := repo.ListRuns(ctx, results.ListRunsFilter{
		Kind: kind, Scenario: scenarioID, Status: status, Limit: limit,
	})
	if err != nil {
		return err
	}
	for _, r := range runs {
		fmt.Fprintf(a.Stdout, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.ID, r.Kind, r.Scenario, r.Status, r.ImageRef, r.CreatedAt.Format(time.RFC3339))
	}
	return nil
}

func (a *App) runsShow(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("runs show", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var id string
	fs.StringVar(&id, "id", "", "run UUID")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if id == "" {
		return fmt.Errorf("%w: --id is required", errUsage)
	}
	repo, err := openResultsRepo(ctx, cfg)
	if err != nil {
		return err
	}
	defer repo.Close()
	run, err := repo.GetRun(ctx, id)
	if err != nil {
		return err
	}
	series, err := repo.LoadMetricSeries(ctx, id)
	if err != nil {
		return err
	}
	vr, hasVR, err := repo.GetValidationResult(ctx, id)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"run": run, "metrics": series,
	}
	if hasVR {
		payload["validation"] = vr
		samples, err := repo.LoadValidationFailureSamples(ctx, id)
		if err != nil {
			return err
		}
		if len(samples) > 0 {
			payload["validation_failure_samples"] = samples
		}
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a *App) runsDelete(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("runs delete", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var id string
	fs.StringVar(&id, "id", "", "run UUID")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if id == "" {
		return fmt.Errorf("%w: --id is required", errUsage)
	}
	repo, err := openResultsRepo(ctx, cfg)
	if err != nil {
		return err
	}
	defer repo.Close()
	if err := repo.DeleteRun(ctx, id); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "deleted run %s\n", id)
	return nil
}

func (a *App) runsArtifacts(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("runs artifacts", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var id, kind, name string
	fs.StringVar(&id, "id", "", "run UUID")
	fs.StringVar(&kind, "kind", "", "artifact kind")
	fs.StringVar(&name, "name", "", "artifact name")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if id == "" {
		return fmt.Errorf("%w: --id is required", errUsage)
	}
	repo, err := openResultsRepo(ctx, cfg)
	if err != nil {
		return err
	}
	defer repo.Close()

	if kind == "" && name == "" {
		artifacts, err := repo.ListRunArtifacts(ctx, id)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(artifacts)
	}
	if kind == "" || name == "" {
		return fmt.Errorf("%w: --kind and --name are required to fetch artifact content", errUsage)
	}
	art, err := repo.GetRunArtifact(ctx, id, kind, name)
	if err != nil {
		return err
	}
	_, err = a.Stdout.Write(art.Content)
	return err
}

func (a *App) cmdCleanup(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var retainedOnly, removeStaleLock bool
	fs.BoolVar(&retainedOnly, "retained-only", false, "only delete retained failure namespaces")
	fs.BoolVar(&removeStaleLock, "remove-stale-lock", false, "remove stale active lock when safe")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	client := a.clusterClient(cfg)
	mgr := namespace.Manager{Cluster: client}
	names, err := mgr.ListActiveRunNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", benchmark.ErrInfrastructure, err)
	}
	for _, ns := range names {
		replicas, err := client.DeploymentReplicas(ctx, ns, "numaflow-controller")
		if err != nil {
			fmt.Fprintf(a.Stderr, "warn: %s controller replicas: %v\n", ns, err)
			replicas = 1
		}
		if retainedOnly {
			if replicas > 0 {
				continue
			}
			if err := mgr.Delete(ctx, ns, 2*time.Minute); err != nil {
				fmt.Fprintf(a.Stderr, "warn: delete %s: %v\n", ns, err)
				continue
			}
			fmt.Fprintf(a.Stdout, "deleted retained namespace %s\n", ns)
			continue
		}
		if replicas > 0 {
			if err := mgr.RetainFailure(ctx, ns); err != nil {
				fmt.Fprintf(a.Stderr, "warn: scale %s: %v\n", ns, err)
				continue
			}
			fmt.Fprintf(a.Stdout, "scaled controller to zero in %s\n", ns)
		}
		if err := mgr.Delete(ctx, ns, 2*time.Minute); err != nil {
			fmt.Fprintf(a.Stderr, "warn: delete %s: %v\n", ns, err)
			continue
		}
		fmt.Fprintf(a.Stdout, "deleted namespace %s\n", ns)
	}
	if removeStaleLock {
		repo, err := openResultsRepo(ctx, cfg)
		if err != nil {
			return err
		}
		defer repo.Close()
		nsCheck := func(ns string) (bool, error) {
			if !namespace.IsActiveRunNamespace(ns) {
				return false, nil
			}
			return client.NamespaceExists(ctx, ns)
		}
		if err := removeStaleActiveLocks(ctx, repo, nsCheck, pidAlive); err != nil {
			return err
		}
	}
	return nil
}

func removeStaleActiveLocks(ctx context.Context, repo *results.Repository, nsCheck func(string) (bool, error), pidCheck func(int) bool) error {
	locks, err := repo.ListActiveLocks(ctx)
	if err != nil {
		return err
	}
	for _, lock := range locks {
		if pidCheck(lock.PID) {
			continue
		}
		if lock.Namespace != "" {
			active, err := nsCheck(lock.Namespace)
			if err != nil {
				fmt.Fprintf(os.Stderr, "stale lock %s namespace check: %v\n", lock.LockKey, err)
				continue
			}
			if active {
				continue
			}
		}
		if err := repo.DeleteActiveLock(ctx, lock.LockKey, lock.OwnerToken); err != nil {
			fmt.Fprintf(os.Stderr, "stale lock %s: %v\n", lock.LockKey, err)
			continue
		}
		fmt.Fprintf(os.Stdout, "removed stale lock %s\n", lock.LockKey)
	}
	return nil
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}
