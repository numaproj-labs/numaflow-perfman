package report

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"numa-perfman/internal/results"
)

func seedBenchmarkRun(t *testing.T, repo *results.Repository, scenario, image, hash, cfg string) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	start := time.Now().UTC().Add(-time.Hour)
	if err := repo.CreateRun(ctx, results.CreateRunParams{
		ID: id, Kind: results.KindBenchmark, Scenario: scenario, ImageRef: image,
		Status: results.StatusCreated, ManifestHash: hash, ConfigJSON: cfg,
		CreatedAt: start,
	}); err != nil {
		t.Fatal(err)
	}
	measure := start.Add(5 * time.Minute)
	end := measure.Add(15 * time.Minute)
	if err := repo.UpdateRunLifecycle(ctx, id, results.UpdateRunLifecycleParams{
		Status:               results.StatusCompleted,
		MeasurementStartedAt: &measure,
		MeasurementEndedAt:   &end,
		CompletedAt:          &end,
	}); err != nil {
		t.Fatal(err)
	}
	pts := []results.MetricPoint{
		{Timestamp: measure, ElapsedMilliseconds: 0, Value: 100},
		{Timestamp: measure.Add(10 * time.Second), ElapsedMilliseconds: 10000, Value: 110},
	}
	if err := repo.SaveMetrics(ctx, results.SaveMetricsParams{
		RunID: id, RequiredMetrics: []string{"throughput"},
		Series: []results.MetricSeriesInput{
			{MetricName: "throughput", Unit: "events/s", Points: pts},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestBuildAndWriteDeterministicSummary(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	repo, err := results.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	cfg := `{"duration":"15m","resources":{"cpu":"1"}}`
	hash := "manifest-v1"
	seedBenchmarkRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.7.0", hash, cfg)
	seedBenchmarkRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.8.0", hash, cfg)

	b := &Builder{Repo: repo}
	rep, err := b.Build(ctx, Selection{
		Scenario:                "single-map",
		BaselineImageRef:        "quay.io/numaproj/numaflow:v1.7.0",
		CandidateImageRef:       "quay.io/numaproj/numaflow:v1.8.0",
		BaselineLatestComplete:  1,
		CandidateLatestComplete: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.MetricComparisons) != 1 {
		t.Fatalf("comparisons: %#v", rep.MetricComparisons)
	}
	if rep.MetricComparisons[0].PercentChange == 0 && rep.MetricComparisons[0].Baseline.Mean == rep.MetricComparisons[0].Candidate.Mean {
		// both runs have same points — ok
	}

	reportID, err := PersistComparison(ctx, repo, Selection{
		Scenario:                "single-map",
		BaselineImageRef:        "quay.io/numaproj/numaflow:v1.7.0",
		CandidateImageRef:       "quay.io/numaproj/numaflow:v1.8.0",
		BaselineLatestComplete:  1,
		CandidateLatestComplete: 1,
	}, rep)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetReport(ctx, reportID)
	if err != nil {
		t.Fatal(err)
	}
	sumBytes, err := RenderStoredJSON(stored)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(sumBytes, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Scenario != "single-map" || decoded.ExcludedCount != 0 {
		t.Fatalf("summary: %#v", decoded)
	}
	if decoded.Chart.Center != CenterMedian || decoded.Chart.PercentileBandLow != 25 {
		t.Fatalf("chart options: %#v", decoded.Chart)
	}
	if decoded.BaselineImageRef == "" || decoded.CandidateImageRef == "" {
		t.Fatalf("image refs missing: %#v", decoded)
	}
	decoded.GeneratedAt = time.Time{}
	rep.GeneratedAt = time.Time{}
	sumAgain, _ := json.MarshalIndent(decoded, "", "  ")
	repAgain, _ := json.MarshalIndent(rep, "", "  ")
	if string(sumAgain) != string(repAgain) {
		t.Fatal("summary JSON should match in-memory report when generated_at cleared")
	}

	htmlBytes, err := RenderStoredHTML(stored, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(htmlBytes) == 0 || !contains(string(htmlBytes), "Benchmark comparison") {
		t.Fatal("invalid html")
	}
	if !contains(string(htmlBytes), `<th colspan="2" class="group-header">Baseline</th><th colspan="2" class="group-header">Candidate</th>`) || !contains(string(htmlBytes), "<th>Avg</th><th>p99</th><th>Avg</th><th>p99</th>") || !contains(string(htmlBytes), `class="metric-chart"`) {
		t.Fatal("html missing stats table or interactive chart")
	}
	if !contains(string(htmlBytes), "thead tr + tr th,thead tr + tr th:first-child{text-align:right}") {
		t.Fatalf("comparison stats sub-headers should be right-aligned: %s", htmlBytes)
	}
	if contains(string(htmlBytes), "<h2>Metric statistics</h2>") {
		t.Fatalf("comparison report still has top-level metric statistics: %s", htmlBytes)
	}
	if !contains(string(htmlBytes), `class="chart-stats"`) {
		t.Fatalf("comparison report missing per-chart stats: %s", htmlBytes)
	}
	chartIdx := stringIndex(string(htmlBytes), `class="metric-chart"`)
	statsIdx := stringIndex(string(htmlBytes), `class="chart-stats"`)
	if chartIdx < 0 || statsIdx < 0 || statsIdx < chartIdx {
		t.Fatalf("comparison stats should appear under chart: chart=%d stats=%d", chartIdx, statsIdx)
	}
	if contains(string(htmlBytes), "<th>Unit</th>") || contains(string(htmlBytes), "<th>Side</th>") || contains(string(htmlBytes), ">baseline</td>") || contains(string(htmlBytes), `class="metric-info"`) {
		t.Fatalf("comparison stats table uses legacy layout: %s", htmlBytes)
	}
	if !contains(string(htmlBytes), "throughput (events/s)") {
		t.Fatalf("comparison stats table missing unit in metric label: %s", htmlBytes)
	}
	if contains(string(htmlBytes), "std_dev") || contains(string(htmlBytes), "<th>median</th>") || contains(string(htmlBytes), "<th>count</th>") {
		t.Fatalf("comparison report includes extra metric stats columns: %s", htmlBytes)
	}
	if !contains(string(htmlBytes), "v1.7.0") || !contains(string(htmlBytes), "v1.8.0") {
		t.Fatalf("html missing image tag legends: %s", htmlBytes)
	}
	if !contains(string(htmlBytes), plotlyCDN) || !contains(string(htmlBytes), "Plotly.newPlot") {
		t.Fatalf("html missing Plotly CDN chart: %s", htmlBytes)
	}
	if !contains(string(htmlBytes), `"mode":"lines+markers"`) || !contains(string(htmlBytes), `"marker":{"color":"#5794F2","size":7}`) {
		t.Fatalf("html chart is missing visible Plotly data points: %s", htmlBytes)
	}
	if contains(string(htmlBytes), "<svg") || contains(string(htmlBytes), `class="chart-tooltip"`) {
		t.Fatalf("html retains legacy SVG chart markup: %s", htmlBytes)
	}
	if contains(string(htmlBytes), "— baseline") || contains(string(htmlBytes), "— candidate") {
		t.Fatalf("html chart still uses side labels: %s", htmlBytes)
	}
	if !contains(string(htmlBytes), "Elapsed time (min)") || contains(string(htmlBytes), "Elapsed time (ms)") {
		t.Fatalf("comparison chart elapsed-time unit: %s", htmlBytes)
	}
	if contains(string(htmlBytes), `data-run-id=`) || contains(string(htmlBytes), `addDetail(tooltip, "Run"`) {
		t.Fatalf("comparison chart exposes run ids in tooltips: %s", htmlBytes)
	}
	if contains(string(htmlBytes), "Elapsed:") {
		t.Fatalf("comparison chart hover includes elapsed time: %s", htmlBytes)
	}
	if !contains(string(htmlBytes), `"hovermode":"x unified"`) {
		t.Fatalf("comparison chart missing unified hover mode: %s", htmlBytes)
	}
	if !contains(string(htmlBytes), `"showspikes":false`) {
		t.Fatalf("comparison chart still enables axis hover spikes: %s", htmlBytes)
	}
	if contains(string(htmlBytes), "Static chart") {
		t.Fatalf("comparison report includes removed static chart output: %s", htmlBytes)
	}
}

func TestDifferentConfigsComparable(t *testing.T) {
	ctx := context.Background()
	repo, err := results.Open(ctx, filepath.Join(t.TempDir(), "mismatch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	cfgA := `{"duration":"15m","resources":{"cpu":"1"}}`
	cfgB := `{"duration":"20m","resources":{"cpu":"1"}}`
	seedBenchmarkRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.7.0", "hash-a", cfgA)
	seedBenchmarkRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.8.0", "hash-b", cfgB)

	b := &Builder{Repo: repo}
	rep, err := b.Build(ctx, Selection{
		Scenario:          "single-map",
		BaselineImageRef:  "quay.io/numaproj/numaflow:v1.7.0",
		CandidateImageRef: "quay.io/numaproj/numaflow:v1.8.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.BaselineCompleteCount != 1 || rep.CandidateCompleteCount != 1 {
		t.Fatalf("expected both sides in report: baseline=%d candidate=%d excluded=%d",
			rep.BaselineCompleteCount, rep.CandidateCompleteCount, rep.ExcludedCount)
	}
}

func TestExplicitRunIncompleteExcluded(t *testing.T) {
	ctx := context.Background()
	repo, err := results.Open(ctx, filepath.Join(t.TempDir(), "incomplete.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	cfg := `{"duration":"15m"}`
	hash := "h1"
	bComplete := seedBenchmarkRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.7.0", hash, cfg)
	cComplete := seedBenchmarkRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.8.0", hash, cfg)

	failID := uuid.NewString()
	if err := repo.CreateRun(ctx, results.CreateRunParams{
		ID: failID, Kind: results.KindBenchmark, Scenario: "single-map",
		ImageRef: "quay.io/numaproj/numaflow:v1.6.0", Status: results.StatusFailed,
		ManifestHash: hash, ConfigJSON: cfg,
	}); err != nil {
		t.Fatal(err)
	}

	b := &Builder{Repo: repo}
	rep, err := b.Build(ctx, Selection{
		Scenario:        "single-map",
		BaselineRunIDs:  []string{bComplete, failID},
		CandidateRunIDs: []string{cComplete},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.ExcludedCount != 1 || rep.BaselineIncompleteCount != 1 {
		t.Fatalf("excluded=%d incomplete baseline=%d", rep.ExcludedCount, rep.BaselineIncompleteCount)
	}
}

func TestSideSpecificRunIDsNeedNoImageSelectors(t *testing.T) {
	ctx := context.Background()
	repo, err := results.Open(ctx, filepath.Join(t.TempDir(), "explicit-sides.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	cfg := `{"duration":"15m"}`
	hash := "h1"
	baselineID := seedBenchmarkRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.7.0", hash, cfg)
	candidateID := seedBenchmarkRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.8.0", hash, cfg)

	rep, err := (&Builder{Repo: repo}).Build(ctx, Selection{
		Scenario:        "single-map",
		BaselineRunIDs:  []string{baselineID},
		CandidateRunIDs: []string{candidateID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.BaselineImageRef != "quay.io/numaproj/numaflow:v1.7.0" ||
		rep.CandidateImageRef != "quay.io/numaproj/numaflow:v1.8.0" {
		t.Fatalf("unexpected image refs: baseline=%q candidate=%q", rep.BaselineImageRef, rep.CandidateImageRef)
	}
}

func TestBuildAndWriteSingleReport(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	repo, err := results.Open(ctx, filepath.Join(dir, "single.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	runID := seedBenchmarkRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.8.0", "manifest-v1",
		`{"duration":"15m","resources":{"cpu":"1"}}`)
	builder := &Builder{Repo: repo}
	rep, err := builder.BuildSingle(ctx, SingleSelection{
		Scenario: "single-map",
		RunID:    runID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.RunID != runID || rep.ImageRef != "quay.io/numaproj/numaflow:v1.8.0" {
		t.Fatalf("single report metadata: %#v", rep)
	}
	if len(rep.Metrics) != 1 {
		t.Fatalf("metrics: %#v", rep.Metrics)
	}
	metric := rep.Metrics[0]
	if metric.MetricName != "throughput" || metric.Unit != "events/s" || len(metric.Values) != 2 ||
		metric.Values[0].Value != 100 || metric.Values[1].Value != 110 {
		t.Fatalf("single metric values: %#v", metric)
	}
	byImage, err := builder.BuildSingle(ctx, SingleSelection{
		Scenario: "single-map",
		ImageRef: "quay.io/numaproj/numaflow:v1.8.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if byImage.RunID != runID {
		t.Fatalf("image selector chose run %q, want %q", byImage.RunID, runID)
	}

	sel := SingleSelection{Scenario: "single-map", RunID: runID}
	reportID, err := PersistSingle(ctx, repo, sel, rep)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetReport(ctx, reportID)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := RenderStoredJSON(stored)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SingleReport
	if err := json.Unmarshal(summary, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RunID != runID || len(decoded.Metrics) != 1 || len(decoded.Metrics[0].Values) != 2 {
		t.Fatalf("decoded single report: %#v", decoded)
	}
	htmlBytes, err := RenderStoredHTML(stored, "")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	if !contains(html, "Benchmark result") || !contains(html, `class="metric-chart"`) || !contains(html, "v1.8.0") {
		t.Fatalf("invalid single report HTML: %s", html)
	}
	if !contains(html, plotlyCDN) || !contains(html, "Plotly.newPlot") || !contains(html, `"mode":"lines+markers"`) {
		t.Fatalf("single report missing Plotly chart: %s", html)
	}
	if contains(html, "<svg") || contains(html, `class="chart-tooltip"`) {
		t.Fatalf("single report retains legacy SVG chart markup: %s", html)
	}
	if contains(html, "Benchmark comparison") || contains(html, ">Candidate<") {
		t.Fatalf("single report includes comparison UI: %s", html)
	}
	if !contains(html, "<th>Avg</th><th>p99</th>") || !contains(html, "105.00") || !contains(html, "109.90") {
		t.Fatalf("single report missing average or p99: %s", html)
	}
	if contains(html, "<h2>Metric values</h2>") {
		t.Fatalf("single report still has top-level metric values: %s", html)
	}
	if !contains(html, `class="chart-stats"`) {
		t.Fatalf("single report missing per-chart stats: %s", html)
	}
	chartIdx := stringIndex(html, `class="metric-chart"`)
	statsIdx := stringIndex(html, `class="chart-stats"`)
	if chartIdx < 0 || statsIdx < 0 || statsIdx < chartIdx {
		t.Fatalf("single report stats should appear under chart: chart=%d stats=%d", chartIdx, statsIdx)
	}
	if contains(html, "<th>Unit</th>") || contains(html, "<th>Vertex</th>") || contains(html, `class="metric-info"`) {
		t.Fatalf("single report uses legacy metric table layout: %s", html)
	}
	if !contains(html, "throughput (events/s)") {
		t.Fatalf("single report missing unit in metric label: %s", html)
	}
	if contains(html, "Elapsed:") {
		t.Fatalf("single report hover includes elapsed time: %s", html)
	}
	if !contains(html, `"showspikes":false`) {
		t.Fatalf("single report still enables axis hover spikes: %s", html)
	}
	if contains(html, "Timestamp") || contains(html, "std_dev") || contains(html, "p95") {
		t.Fatalf("single report includes unwanted metric values: %s", html)
	}
	if contains(html, "Each line shows the measurements collected for that metric.") || contains(html, "Loading interactive chart") {
		t.Fatalf("single report includes removed chart copy: %s", html)
	}
	if contains(html, "Static chart") {
		t.Fatalf("single report includes removed static chart output: %s", html)
	}
	if !contains(html, "Elapsed time (min)") || contains(html, "Elapsed time (ms)") {
		t.Fatalf("single report elapsed-time unit: %s", html)
	}
	if contains(html, `data-run-id=`) || contains(html, `addDetail(tooltip, "Run"`) {
		t.Fatalf("single report exposes run ids in tooltips: %s", html)
	}
}

func TestBuildAndWritePerVertexCharts(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	repo, err := results.Open(ctx, filepath.Join(dir, "vertex.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	cfg := `{"duration":"15m"}`
	hash := "manifest-v1"
	baselineID := seedBenchmarkRunWithVertexMetrics(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.7.0", hash, cfg)
	candidateID := seedBenchmarkRunWithVertexMetrics(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.8.0", hash, cfg)

	b := &Builder{Repo: repo}
	rep, err := b.Build(ctx, Selection{
		Scenario:        "single-map",
		BaselineRunIDs:  []string{baselineID},
		CandidateRunIDs: []string{candidateID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.MetricComparisons) != 2 {
		t.Fatalf("expected per-vertex comparisons, got %#v", rep.MetricComparisons)
	}

	reportID, err := PersistComparison(ctx, repo, Selection{
		Scenario:        "single-map",
		BaselineRunIDs:  []string{baselineID},
		CandidateRunIDs: []string{candidateID},
	}, rep)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetReport(ctx, reportID)
	if err != nil {
		t.Fatal(err)
	}
	htmlBytes, err := RenderStoredHTML(stored, "")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	if !contains(html, `class="vertex-select"`) || !contains(html, `<option value="in"`) || !contains(html, `<option value="map"`) {
		t.Fatalf("missing vertex selector: %s", html)
	}
	if !contains(html, "throughput - in (events/s)") || !contains(html, "throughput - map (events/s)") {
		t.Fatalf("missing combined metric-vertex labels: %s", html)
	}
	if !contains(html, "plotly-vertex-chart-data") {
		t.Fatalf("missing vertex chart bundle: %s", html)
	}
}

func seedBenchmarkRunWithVertexMetrics(t *testing.T, repo *results.Repository, scenario, image, hash, cfg string) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	start := time.Now().UTC().Add(-time.Hour)
	if err := repo.CreateRun(ctx, results.CreateRunParams{
		ID: id, Kind: results.KindBenchmark, Scenario: scenario, ImageRef: image,
		Status: results.StatusCreated, ManifestHash: hash, ConfigJSON: cfg,
		CreatedAt: start,
	}); err != nil {
		t.Fatal(err)
	}
	measure := start.Add(5 * time.Minute)
	end := measure.Add(15 * time.Minute)
	if err := repo.UpdateRunLifecycle(ctx, id, results.UpdateRunLifecycleParams{
		Status:               results.StatusCompleted,
		MeasurementStartedAt: &measure,
		MeasurementEndedAt:   &end,
		CompletedAt:          &end,
	}); err != nil {
		t.Fatal(err)
	}
	pts := []results.MetricPoint{
		{Timestamp: measure, ElapsedMilliseconds: 0, Value: 100},
		{Timestamp: measure.Add(10 * time.Second), ElapsedMilliseconds: 10000, Value: 110},
	}
	if err := repo.SaveMetrics(ctx, results.SaveMetricsParams{
		RunID: id, RequiredMetrics: []string{"throughput"},
		Series: []results.MetricSeriesInput{
			{MetricName: "throughput", Unit: "events/s", LabelsJSON: `{"vertex":"in"}`, Points: pts},
			{MetricName: "throughput", Unit: "events/s", LabelsJSON: `{"vertex":"map"}`, Points: pts},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestChartValueConvertsVertexMemoryToMB(t *testing.T) {
	const bytes = 5 * 1024 * 1024
	got := chartValue(vertexMemory, bytes)
	if got != 5 {
		t.Fatalf("chartValue(vertex_memory) = %v, want 5", got)
	}
	if chartUnit(vertexMemory, "bytes") != "MB" {
		t.Fatalf("chartUnit(vertex_memory) = %q, want MB", chartUnit(vertexMemory, "bytes"))
	}
	if metricDisplayUnit(vertexMemory, "bytes") != "MB" {
		t.Fatalf("metricDisplayUnit(vertex_memory) = %q, want MB", metricDisplayUnit(vertexMemory, "bytes"))
	}
	if formatMetricStat(vertexMemory, bytes) != "5.00" {
		t.Fatalf("formatMetricStat(vertex_memory) = %q, want 5.00", formatMetricStat(vertexMemory, bytes))
	}
	if formatMetricStat("throughput", 123.456) != "123.46" {
		t.Fatalf("formatMetricStat(throughput) = %q, want 123.46", formatMetricStat("throughput", 123.456))
	}
	if got := metricStatsLabel("forwarder_latency", "in", "milliseconds"); got != "forwarder_latency - in (milliseconds)" {
		t.Fatalf("metricStatsLabel = %q", got)
	}
	if got := metricStatsLabel(vertexMemory, "map", "bytes"); got != "vertex_memory - map (MB)" {
		t.Fatalf("metricStatsLabel(vertex_memory) = %q", got)
	}
}

func TestComparisonHoverTemplate(t *testing.T) {
	got := comparisonHoverTemplate("v1.8.2")
	if contains(got, "Elapsed:") {
		t.Fatalf("comparison hover includes elapsed: %q", got)
	}
	if !contains(got, "v1.8.2") || !contains(got, "%{y:.2f}") {
		t.Fatalf("comparison hover template: %q", got)
	}
}

func TestComputeStatsPercentiles(t *testing.T) {
	st := ComputeStats([]float64{1, 2, 3, 4, 100})
	if st.Min != 1 || st.Max != 100 || st.Count != 5 {
		t.Fatalf("%#v", st)
	}
	if st.Median != st.P50 {
		t.Fatalf("median/p50 mismatch")
	}
}

func TestMetricPointCoordinatesExcludeInvalidValues(t *testing.T) {
	x, y := metricPointCoordinates("throughput", []results.MetricPoint{
		{ElapsedMilliseconds: 0, Value: 10},
		{ElapsedMilliseconds: -1, Value: 20},
		{ElapsedMilliseconds: 60_000, Value: math.NaN()},
		{ElapsedMilliseconds: 120_000, Value: math.Inf(1)},
	})
	if len(x) != 1 || len(y) != 1 || x[0] != 0 || y[0] != 10 {
		t.Fatalf("Plotly coordinates included invalid points: x=%v y=%v", x, y)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
