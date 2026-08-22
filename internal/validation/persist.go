package validation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/diagnostics"
	"numa-perfman/internal/oracle"
	"numa-perfman/internal/results"
	"numa-perfman/internal/runmeta"
	"numa-perfman/internal/scenario"
)

// ScenarioRunID derives a deterministic UUID for one scenario under a parent invocation.
func ScenarioRunID(invocation uuid.UUID, sc oracle.Scenario) uuid.UUID {
	return uuid.NewSHA1(invocation, []byte("validation-scenario:"+string(sc)))
}

// PersistingRunner wraps Runner to persist one results row per scenario and write artifacts.
type PersistingRunner struct {
	Runner          *Runner
	Results         ResultsRepository
	Artifacts       ArtifactsWriter
	Cluster         *ClusterAdapter
	Diagnostics     *diagnostics.Collector
	Digest          runmeta.DigestResolver
	Environment     runmeta.Provider
	Database        RunDatabase
	Invocation      uuid.UUID
	OnScenarioStart func(runID uuid.UUID, namespace string) error
}

// PersistedRunResult aggregates scenario outcomes under one invocation id.
type PersistedRunResult struct {
	InvocationID uuid.UUID
	RunResult    RunResult
}

// Run executes all scenarios sequentially, persisting run rows keyed by ScenarioRunID.
func (p *PersistingRunner) Run(ctx context.Context, invocationID uuid.UUID, opts Options) (PersistedRunResult, error) {
	opts = opts.withDefaults()
	if invocationID == uuid.Nil {
		invocationID = uuid.New()
	}
	if p.Invocation != uuid.Nil {
		invocationID = p.Invocation
	}
	if opts.ImageRef == "" {
		return PersistedRunResult{}, fmt.Errorf("image reference is required")
	}
	if opts.UDFImage == "" {
		return PersistedRunResult{}, fmt.Errorf("udf image is required")
	}

	if p.Runner != nil && p.Artifacts != nil {
		p.Runner.SetFailureDumpStore(newArtifactsFailureDumpStore(p.Artifacts))
	}

	out := PersistedRunResult{InvocationID: invocationID}
	out.RunResult.RunID = invocationID
	if opts.Namespace != "" {
		out.RunResult.Namespace = opts.Namespace
	} else {
		out.RunResult.Namespace = NamespaceName(invocationID)
	}
	out.RunResult.Database = DatabaseNameFromRunID(invocationID)
	out.RunResult.StartedAt = time.Now().UTC()

	opts.ResolvedSeed()

	if opts.Namespace != "" && len(opts.Scenarios) != 1 {
		return PersistedRunResult{}, fmt.Errorf("deterministic namespace requires exactly one scenario")
	}

	scenarios := opts.Scenarios
	for _, sc := range scenarios {
		if err := ctx.Err(); err != nil {
			out.RunResult.EndedAt = time.Now().UTC()
			return out, err
		}
		scenarioRunID := ScenarioRunID(invocationID, sc)
		ns := ScenarioNamespace(opts, scenarioRunID, sc)
		if p.OnScenarioStart != nil {
			if err := p.OnScenarioStart(scenarioRunID, ns); err != nil {
				return out, fmt.Errorf("update scenario metadata: %w", err)
			}
		}
		if p.Cluster != nil {
			p.Cluster.BindRunID(scenarioRunID, opts.ImageRef, string(sc))
		}

		manifestHash, manifestDocs, err := validationManifestMeta(opts, sc, scenarioRunID)
		if err != nil {
			return out, err
		}
		manifestDocs["image_ref"] = opts.ImageRef

		seed := opts.ResolvedSeed()
		seed64 := int64(seed)
		events := opts.Events
		digest, digestWarn := runmeta.ResolveDigest(ctx, opts.ImageRef, p.Digest)
		envJSON := "{}"
		if p.Environment != nil {
			envJSON = p.Environment.Snapshot().JSON()
		}

		if p.Results != nil {
			cfgJSON, _ := json.Marshal(map[string]any{
				"invocation_id":      invocationID.String(),
				"udf_image":          opts.UDFImage,
				"tier":               opts.Tier,
				"events":             opts.Events,
				"base_event_time_ms": opts.GenerationConfig(seed).BaseEventTimeMs,
				"spacing_ms":         opts.GenerationConfig(seed).SpacingMs,
				"resources":          runmeta.DefaultBenchmarkResources,
			})
			if err := p.Results.CreateRun(ctx, results.CreateRunParams{
				ID:              scenarioRunID.String(),
				Kind:            results.KindValidation,
				Scenario:        string(sc),
				ImageRef:        opts.ImageRef,
				ImageDigest:     digest,
				Status:          results.StatusCreated,
				Namespace:       ns,
				CreatedAt:       time.Now().UTC(),
				Seed:            &seed64,
				EventCount:      &events,
				ManifestHash:    manifestHash,
				ConfigJSON:      string(cfgJSON),
				EnvironmentJSON: envJSON,
			}); err != nil {
				return out, fmt.Errorf("create run %s: %w", sc, err)
			}
			if digestWarn != "" {
				_ = p.Results.AddEvent(ctx, results.AddEventParams{
					RunID: scenarioRunID.String(), Timestamp: time.Now().UTC(),
					Level: "warning", Phase: string(PhaseCreated), Message: digestWarn,
				})
			}
		}

		scenarioOpts := opts
		scenarioOpts.Scenarios = []oracle.Scenario{sc}

		res, err := p.Runner.Run(ctx, scenarioRunID, scenarioOpts)
		out.RunResult.Scenarios = append(out.RunResult.Scenarios, res.Scenarios...)
		if len(res.Scenarios) > 0 {
			sr := res.Scenarios[len(res.Scenarios)-1]
			if pErr := p.persistScenarioOutcome(ctx, scenarioRunID, sr, manifestDocs, ns, err); pErr != nil && err == nil {
				err = fmt.Errorf("persist scenario outcome: %w", pErr)
			}
		} else if err != nil {
			sr := ScenarioResult{Scenario: sc, Phase: PhaseFailed, Error: err.Error()}
			if pErr := p.persistScenarioOutcome(ctx, scenarioRunID, sr, manifestDocs, ns, err); pErr != nil {
				err = errors.Join(err, fmt.Errorf("persist scenario outcome: %w", pErr))
			}
		}
		if err != nil {
			out.RunResult.EndedAt = time.Now().UTC()
			return out, err
		}
		if len(res.Scenarios) > 0 && !res.Scenarios[0].Passed {
			out.RunResult.EndedAt = time.Now().UTC()
			return out, ErrCorrectness
		}
	}

	out.RunResult.EndedAt = time.Now().UTC()
	return out, nil
}

func validationManifestMeta(opts Options, sc oracle.Scenario, scenarioRunID uuid.UUID) (string, map[string]string, error) {
	bundle, docs, err := renderValidationManifests(opts, sc, scenarioRunID)
	if err != nil {
		return "", nil, err
	}
	return bundle.Hash, docs, nil
}

func renderValidationManifests(opts Options, sc oracle.Scenario, scenarioRunID uuid.UUID) (scenario.ManifestBundle, map[string]string, error) {
	ns := ScenarioNamespace(opts, scenarioRunID, sc)
	dbName := DatabaseNameForScenario(scenarioRunID, sc)
	validationNS := opts.ValidationPostgresNS
	if validationNS == "" {
		validationNS = "validation-system"
	}
	bundle, err := scenario.RenderManifests(scenario.RenderInput{
		ScenarioID:     string(sc),
		Kind:           scenario.RunKindValidation,
		Namespace:      ns,
		UDFImage:       opts.UDFImage,
		ValidationNS:   validationNS,
		PostgresDBName: dbName,
		RunID:          scenarioRunID.String(),
		NumaflowImage:  opts.ImageRef,
	})
	if err != nil {
		return scenario.ManifestBundle{}, nil, err
	}
	docs := map[string]string{}
	idx := 0
	for _, doc := range bundle.AllDocuments() {
		idx++
		docs[fmt.Sprintf("manifest-%d.yaml", idx)] = doc
	}
	return bundle, docs, nil
}

func (p *PersistingRunner) persistScenarioOutcome(ctx context.Context, scenarioRunID uuid.UUID, sr ScenarioResult, manifestDocs map[string]string, ns string, runErr error) error {
	runID := scenarioRunID.String()
	var persistErrs []error
	if p.Diagnostics != nil && p.Artifacts != nil && ns != "" {
		if bundle, err := p.Diagnostics.Collect(ctx, ns); err == nil {
			if err := p.Artifacts.WriteTextLog(runID, "pods.txt", bundle.Pods); err != nil {
				persistErrs = append(persistErrs, err)
			}
			if err := p.Artifacts.WriteTextLog(runID, "controller.log", bundle.ControllerLog); err != nil {
				persistErrs = append(persistErrs, err)
			}
			if err := p.Artifacts.WriteTextLog(runID, "pipeline.log", bundle.PipelineLog); err != nil {
				persistErrs = append(persistErrs, err)
			}
			if bundle.Events != "" {
				if err := p.Artifacts.WriteTextLog(runID, "k8s-events.log", bundle.Events); err != nil {
					persistErrs = append(persistErrs, err)
				}
			}
		} else {
			persistErrs = append(persistErrs, fmt.Errorf("collect diagnostics: %w", err))
		}
	}
	if p.Artifacts != nil {
		if err := WriteScenarioManifests(p.Artifacts, runID, manifestDocs); err != nil {
			persistErrs = append(persistErrs, err)
		}
	}
	if p.Results == nil {
		return errors.Join(persistErrs...)
	}
	now := time.Now().UTC()
	sourceCount := sr.SourceEvents
	vr := ValidationResultFromCompare(runID, sr.Compare, sourceCount)
	if runErr != nil && sr.Error == "" {
		sr.Error = runErr.Error()
	}
	status := results.StatusPassed
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		status = results.StatusInterrupted
	} else if !sr.Passed || sr.Phase == PhaseFailed {
		status = results.StatusFailed
	}
	if runErr != nil && status == results.StatusPassed {
		status = results.StatusFailed
	}
	if err := p.Results.SaveValidationResult(ctx, vr); err != nil {
		persistErrs = append(persistErrs, err)
	}
	if len(sr.Compare.Samples) > 0 {
		if err := p.Results.ReplaceValidationFailureSamples(ctx, runID, FailureSamplesToResults(sr.Compare.Samples)); err != nil {
			persistErrs = append(persistErrs, err)
		}
	}
	if len(persistErrs) > 0 {
		status = results.StatusFailed
		if sr.Error == "" {
			sr.Error = errors.Join(persistErrs...).Error()
		}
	}
	if err := p.Results.UpdateRunLifecycle(ctx, runID, results.UpdateRunLifecycleParams{
		Status:       status,
		CompletedAt:  &now,
		ErrorMessage: sr.Error,
	}); err != nil {
		persistErrs = append(persistErrs, err)
	}
	return errors.Join(persistErrs...)
}
