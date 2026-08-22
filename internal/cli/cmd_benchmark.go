package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"numa-perfman/internal/artifacts"
	"numa-perfman/internal/benchmark"
	"numa-perfman/internal/config"
	"numa-perfman/internal/controller"
	"numa-perfman/internal/diagnostics"
	"numa-perfman/internal/namespace"
	prom "numa-perfman/internal/prometheus"
	"numa-perfman/internal/report"
	"numa-perfman/internal/results"
	"numa-perfman/internal/runmeta"
	"numa-perfman/internal/scenario"
)

type benchmarkProgressWriter struct {
	writer io.Writer
}

func (w benchmarkProgressWriter) Report(message string) {
	if w.writer != nil {
		fmt.Fprintln(w.writer, message)
	}
}

func (a *App) cmdBenchmark(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: benchmark requires list, run, report, or compare", errUsage)
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "list":
		return a.benchmarkList(rest)
	case "run":
		return a.benchmarkRun(ctx, cfg, rest)
	case "report":
		return a.benchmarkReport(ctx, cfg, rest)
	case "compare":
		return a.benchmarkCompare(ctx, cfg, rest)
	case "help", "-h", "--help":
		printBenchmarkUsage(a.Stdout)
		return nil
	default:
		return fmt.Errorf("%w: unknown benchmark subcommand %q", errUsage, sub)
	}
}

func (a *App) benchmarkList(args []string) error {
	fs := flag.NewFlagSet("benchmark list", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	for _, id := range scenario.ListBenchmarks() {
		sc, err := scenario.LookupBenchmark(id)
		if err != nil {
			continue
		}
		fmt.Fprintf(a.Stdout, "%s\t%s\n", sc.ID, sc.Description)
	}
	return nil
}

func (a *App) benchmarkRun(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("benchmark run", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var scenarioID, image string
	var duration string
	var yes bool
	fs.StringVar(&scenarioID, "scenario", "", "benchmark scenario ID")
	fs.StringVar(&image, "image", "", "complete Numaflow OCI image reference")
	fs.StringVar(&duration, "duration", "15m", "measurement duration")
	fs.BoolVar(&yes, "yes", false, "overwrite an existing scenario/image result without prompting")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if scenarioID == "" || image == "" {
		return fmt.Errorf("%w: --scenario and --image are required", errUsage)
	}
	if err := controller.ValidateImageReference(image); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	warnMutableImage(a.Stderr, image)
	mDur, err := time.ParseDuration(duration)
	if err != nil {
		return fmt.Errorf("%w: duration: %v", errUsage, err)
	}
	if cfg.UDFImage == "" {
		return fmt.Errorf("%w: udf image is required (global --udf-image or stored config)", errUsage)
	}
	if err := cfg.EnsureResultsLayout(); err != nil {
		return fmt.Errorf("%w: %v", errPreflight, err)
	}

	repo, err := openResultsRepo(ctx, cfg)
	if err != nil {
		return err
	}
	defer repo.Close()

	artWriter, err := artifacts.NewWriter(repo)
	if err != nil {
		return err
	}

	client := a.clusterClient(cfg)
	var promFwd *forwardSession
	promURL := cfg.PrometheusURL
	if shouldAutoPrometheus(promURL) {
		sess, err := startPrometheusForward(ctx, client, cfg.MonitoringNamespace)
		if err != nil {
			return fmt.Errorf("%w: %v", errPreflight, err)
		}
		promFwd = sess
		promURL = sess.url
		defer promFwd.close()
		fmt.Fprintf(a.Stdout, "prometheus port-forward: %s\n", promURL)
	}

	nsMgr := namespace.Manager{Cluster: client}
	clusterAdp := &benchmark.ClusterAdapter{
		Cluster: client, Namespaces: nsMgr, KindCluster: cfg.Cluster,
	}
	promClient := prom.Client{BaseURL: promURL, HTTPClient: httpClient()}
	runner := benchmark.NewRunner(benchmark.Dependencies{
		Preflight: benchmark.PreflightAdapter{Cluster: client, KindCluster: cfg.Cluster},
		Cluster:   clusterAdp,
		Metrics:   benchmark.MetricsAdapter{Client: promClient},
		Results:   repo,
		Artifacts: benchmark.ArtifactsFromWriter(artWriter),
		Diagnostics: benchmark.DiagnosticsAdapter{
			Collector: diagnostics.Collector{Cluster: client},
		},
		Locker:      benchmark.DatabaseLocker{Repo: repo},
		Events:      benchEventRecorder{repo: repo},
		Progress:    benchmarkProgressWriter{writer: a.Stdout},
		Overwrite:   benchmarkOverwriteConfirmer{input: a.Stdin, output: a.Stdout, yes: yes},
		Environment: runmeta.StaticProvider{Snap: runmeta.FromConfig(cfg, versionString(), runmeta.DefaultBenchmarkResources)},
	})

	opts := benchmark.Options{
		Scenario: scenarioID,
		ImageRef: image,
		UDFImage: cfg.UDFImage,
		Duration: mDur,
		Command:  "benchmark run",
	}

	result, err := runner.Run(ctx, opts)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "run %s namespace=%s status=%s\n", result.RunID, result.Namespace, result.FinalState)
	if result.FinalState != benchmark.StateCompleted {
		return errBenchmarkFailed
	}
	return nil
}

func (a *App) benchmarkReport(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("benchmark report", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var scenarioID, runID, image, imageDigest string
	var after, before string
	fs.StringVar(&scenarioID, "scenario", "", "benchmark scenario")
	fs.StringVar(&runID, "run-id", "", "completed benchmark run UUID")
	fs.StringVar(&image, "image", "", "image reference selector")
	fs.StringVar(&imageDigest, "image-digest", "", "image digest selector")
	fs.StringVar(&after, "after", "", "RFC3339 lower bound for image/digest selection")
	fs.StringVar(&before, "before", "", "RFC3339 upper bound for image/digest selection")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if scenarioID == "" {
		return fmt.Errorf("%w: --scenario is required", errUsage)
	}
	hasSelectors := image != "" || imageDigest != ""
	if runID == "" && !hasSelectors {
		return fmt.Errorf("%w: --run-id, --image, or --image-digest is required", errUsage)
	}
	if runID != "" && hasSelectors {
		return fmt.Errorf("%w: --run-id cannot be combined with image or digest selectors", errUsage)
	}
	if runID != "" && (after != "" || before != "") {
		return fmt.Errorf("%w: --run-id cannot be combined with time selectors", errUsage)
	}
	var afterTime, beforeTime *time.Time
	if after != "" {
		t, err := time.Parse(time.RFC3339, after)
		if err != nil {
			return fmt.Errorf("%w: after: %v", errUsage, err)
		}
		afterTime = &t
	}
	if before != "" {
		t, err := time.Parse(time.RFC3339, before)
		if err != nil {
			return fmt.Errorf("%w: before: %v", errUsage, err)
		}
		beforeTime = &t
	}

	repo, err := openResultsRepo(ctx, cfg)
	if err != nil {
		return err
	}
	defer repo.Close()

	sel := report.SingleSelection{
		Scenario:    scenarioID,
		RunID:       runID,
		ImageRef:    image,
		ImageDigest: imageDigest,
		After:       afterTime,
		Before:      beforeTime,
	}
	rep, err := (&report.Builder{Repo: repo}).BuildSingle(ctx, sel)
	if err != nil {
		return fmt.Errorf("%w: %v", errReport, err)
	}
	reportID, err := report.PersistSingle(ctx, repo, sel, rep)
	if err != nil {
		return fmt.Errorf("%w: %v", errReport, err)
	}
	fmt.Fprintf(a.Stdout, "stored report %s\n", reportID)
	return nil
}

func (a *App) benchmarkCompare(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("benchmark compare", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var scenarioID, baseline, candidate string
	var baselineDigest, candidateDigest string
	var runIDs, baselineRunIDs, candidateRunIDs string
	var after, before, center, percentileBand string
	fs.StringVar(&scenarioID, "scenario", "", "benchmark scenario")
	fs.StringVar(&baseline, "baseline", "", "baseline image reference")
	fs.StringVar(&candidate, "candidate", "", "candidate image reference")
	fs.StringVar(&baselineDigest, "baseline-digest", "", "baseline image digest")
	fs.StringVar(&candidateDigest, "candidate-digest", "", "candidate image digest")
	fs.StringVar(&runIDs, "run-id", "", "comma-separated run UUIDs")
	fs.StringVar(&baselineRunIDs, "baseline-run-id", "", "comma-separated baseline run UUIDs")
	fs.StringVar(&candidateRunIDs, "candidate-run-id", "", "comma-separated candidate run UUIDs")
	fs.StringVar(&after, "after", "", "RFC3339 lower bound")
	fs.StringVar(&before, "before", "", "RFC3339 upper bound")
	fs.StringVar(&center, "center", "median", "aggregate center line: mean or median")
	fs.StringVar(&percentileBand, "percentile-band", "25,75", "percentile band lower,upper (0-100)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if scenarioID == "" {
		return fmt.Errorf("%w: --scenario is required", errUsage)
	}
	hasGenericRunIDs := strings.TrimSpace(runIDs) != ""
	hasSideRunIDs := strings.TrimSpace(baselineRunIDs) != "" || strings.TrimSpace(candidateRunIDs) != ""
	if hasSideRunIDs && (strings.TrimSpace(baselineRunIDs) == "" || strings.TrimSpace(candidateRunIDs) == "") {
		return fmt.Errorf("%w: both --baseline-run-id and --candidate-run-id are required", errUsage)
	}
	if !hasGenericRunIDs && !hasSideRunIDs {
		if baseline == "" && baselineDigest == "" {
			return fmt.Errorf("%w: --baseline (or --baseline-digest) is required without --run-id", errUsage)
		}
		if candidate == "" && candidateDigest == "" {
			return fmt.Errorf("%w: --candidate (or --candidate-digest) is required without --run-id", errUsage)
		}
	}
	centerStat, err := report.ParseCenterStatistic(center)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	bandLow, bandHigh, err := report.ParsePercentileBand(percentileBand)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}

	repo, err := openResultsRepo(ctx, cfg)
	if err != nil {
		return err
	}
	defer repo.Close()

	sel := report.Selection{
		Scenario:             scenarioID,
		BaselineImageRef:     baseline,
		CandidateImageRef:    candidate,
		BaselineImageDigest:  baselineDigest,
		CandidateImageDigest: candidateDigest,
		Chart: report.ChartOptions{
			Center:             centerStat,
			PercentileBandLow:  bandLow,
			PercentileBandHigh: bandHigh,
		},
	}
	if runIDs != "" {
		sel.RunIDs = splitCommaSeparated(runIDs)
	}
	sel.BaselineRunIDs = splitCommaSeparated(baselineRunIDs)
	sel.CandidateRunIDs = splitCommaSeparated(candidateRunIDs)
	if after != "" {
		t, err := time.Parse(time.RFC3339, after)
		if err != nil {
			return fmt.Errorf("%w: after: %v", errUsage, err)
		}
		sel.After = &t
	}
	if before != "" {
		t, err := time.Parse(time.RFC3339, before)
		if err != nil {
			return fmt.Errorf("%w: before: %v", errUsage, err)
		}
		sel.Before = &t
	}

	builder := report.Builder{Repo: repo}
	rep, err := builder.Build(ctx, sel)
	if err != nil {
		return fmt.Errorf("%w: %v", errReport, err)
	}
	reportID, err := report.PersistComparison(ctx, repo, sel, rep)
	if err != nil {
		return fmt.Errorf("%w: %v", errReport, err)
	}
	fmt.Fprintf(a.Stdout, "stored report %s\n", reportID)
	return nil
}

func splitCommaSeparated(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

type benchEventRecorder struct {
	repo  *results.Repository
	clock func() time.Time
}

func (b benchEventRecorder) now() time.Time {
	if b.clock != nil {
		return b.clock().UTC()
	}
	return time.Now().UTC()
}

func (b benchEventRecorder) RecordPhase(ctx context.Context, runID string, state benchmark.State, message string, detail any) error {
	if b.repo == nil {
		return nil
	}
	ts := b.now()
	return b.repo.AddEvent(ctx, results.AddEventParams{
		RunID: runID, Timestamp: ts,
		Level: "info", Phase: string(state), Message: message,
	})
}

func warnMutableImage(w io.Writer, imageRef string) {
	if runmeta.UsesMutableTag(imageRef) {
		fmt.Fprintf(w, "warning: image %q uses a mutable tag; record digests for reliable comparisons\n", imageRef)
	}
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}
