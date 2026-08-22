package serve_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"numa-perfman/internal/results"
	"numa-perfman/internal/serve"
)

func seedCompletedRun(t *testing.T, repo *results.Repository, scenario, image string) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	start := time.Now().UTC().Add(-time.Hour)
	if err := repo.CreateRun(ctx, results.CreateRunParams{
		ID: id, Kind: results.KindBenchmark, Scenario: scenario, ImageRef: image,
		Status: results.StatusCreated, ManifestHash: "hash", ConfigJSON: `{"duration":"15m"}`,
		CreatedAt: start,
	}); err != nil {
		t.Fatal(err)
	}
	end := start.Add(20 * time.Minute)
	if err := repo.UpdateRunLifecycle(ctx, id, results.UpdateRunLifecycleParams{
		Status: results.StatusCompleted, CompletedAt: &end,
		MeasurementStartedAt: &start,
		MeasurementEndedAt:   &end,
	}); err != nil {
		t.Fatal(err)
	}
	measure := start
	if err := repo.SaveMetrics(ctx, results.SaveMetricsParams{
		RunID: id, RequiredMetrics: []string{"throughput"},
		Series: []results.MetricSeriesInput{{
			MetricName: "throughput", Unit: "events/s",
			Points: []results.MetricPoint{
				{Timestamp: measure, ElapsedMilliseconds: 0, Value: 100},
				{Timestamp: measure.Add(10 * time.Second), ElapsedMilliseconds: 10000, Value: 110},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedFailedValidationRun(t *testing.T, repo *results.Repository, scenario, image string) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	start := time.Now().UTC().Add(-30 * time.Minute)
	events := int64(1000)
	seed := int64(42)
	if err := repo.CreateRun(ctx, results.CreateRunParams{
		ID: id, Kind: results.KindValidation, Scenario: scenario, ImageRef: image,
		Status: results.StatusCreated, ManifestHash: "hash", ConfigJSON: `{"tier":"smoke"}`,
		CreatedAt: start, Seed: &seed, EventCount: &events,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveValidationResult(ctx, results.ValidationResult{
		RunID:                 id,
		Passed:                false,
		SourceCount:           1000,
		ExpectedCount:         1000,
		LogicalOutputCount:    998,
		PhysicalDeliveryCount: 998,
		MissingCount:          2,
		DetailJSON:            `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceValidationFailureSamples(ctx, id, []results.ValidationFailureSample{{
		Ordinal:    1,
		Kind:       "missing",
		LogicalKey: "event-1",
		Expected:   `{"event_id":"event-1"}`,
		Detail:     "path=map output missing",
	}}); err != nil {
		t.Fatal(err)
	}
	end := start.Add(5 * time.Minute)
	if err := repo.UpdateRunLifecycle(ctx, id, results.UpdateRunLifecycleParams{
		Status: results.StatusFailed, CompletedAt: &end, ErrorMessage: "validation correctness failure",
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func openTestRepo(t *testing.T) *results.Repository {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	repo, err := results.Open(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func TestAPIValidationsAndFailureDetails(t *testing.T) {
	repo := openTestRepo(t)
	id := seedFailedValidationRun(t, repo, "map", "quay.io/numaproj/numaflow:v1.8.0")

	srv := httptest.NewServer(serve.New(repo).Handler())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/api/validations")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("validations status=%d", res.StatusCode)
	}
	var validationsPayload struct {
		Validations []struct {
			ID string `json:"id"`
		} `json:"validations"`
	}
	if err := json.NewDecoder(res.Body).Decode(&validationsPayload); err != nil {
		t.Fatal(err)
	}
	foundMap := false
	for _, v := range validationsPayload.Validations {
		if v.ID == "map" {
			foundMap = true
		}
	}
	if !foundMap {
		t.Fatal("map validation missing")
	}

	res, err = http.Get(srv.URL + "/api/validation-runs/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("validation run status=%d", res.StatusCode)
	}
	var detail struct {
		Status     string `json:"status"`
		Validation struct {
			MissingCount int64 `json:"missing_count"`
		} `json:"validation"`
		FailureSamples []struct {
			Kind       string `json:"kind"`
			LogicalKey string `json:"logical_key"`
			Detail     string `json:"detail"`
		} `json:"failure_samples"`
	}
	if err := json.NewDecoder(res.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail.Status != results.StatusFailed {
		t.Fatalf("status=%q", detail.Status)
	}
	if detail.Validation.MissingCount != 2 {
		t.Fatalf("missing_count=%d", detail.Validation.MissingCount)
	}
	if len(detail.FailureSamples) != 1 || detail.FailureSamples[0].Kind != "missing" ||
		detail.FailureSamples[0].LogicalKey != "event-1" {
		t.Fatalf("failure samples=%v", detail.FailureSamples)
	}
}

func TestAPIBenchmarksAndRuns(t *testing.T) {
	repo := openTestRepo(t)
	seedCompletedRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.8.0")

	srv := httptest.NewServer(serve.New(repo).Handler())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/api/benchmarks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("benchmarks status=%d", res.StatusCode)
	}
	var benchPayload struct {
		Default    string `json:"default"`
		Benchmarks []struct {
			ID string `json:"id"`
		} `json:"benchmarks"`
	}
	if err := json.NewDecoder(res.Body).Decode(&benchPayload); err != nil {
		t.Fatal(err)
	}
	if benchPayload.Default != "single-map" {
		t.Fatalf("default=%q", benchPayload.Default)
	}

	res, err = http.Get(srv.URL + "/api/runs?scenario=single-map")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("runs status=%d", res.StatusCode)
	}
	var runsPayload struct {
		Runs []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"runs"`
	}
	if err := json.NewDecoder(res.Body).Decode(&runsPayload); err != nil {
		t.Fatal(err)
	}
	if len(runsPayload.Runs) != 1 {
		t.Fatalf("runs=%d", len(runsPayload.Runs))
	}
	if !strings.Contains(runsPayload.Runs[0].Label, "quay.io/numaproj/numaflow:v1.8.0") {
		t.Fatalf("label=%q", runsPayload.Runs[0].Label)
	}
}

func TestAPISingleAndCompareReport(t *testing.T) {
	repo := openTestRepo(t)
	baselineID := seedCompletedRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.7.0")
	candidateID := seedCompletedRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.8.0")

	srv := httptest.NewServer(serve.New(repo).Handler())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/api/report?scenario=single-map&run_id=" + baselineID)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("report status=%d body=%s", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "Benchmark result") || !strings.Contains(string(body), "/static/plotly.min.js") {
		t.Fatalf("report html missing expected content")
	}

	res, err = http.Get(srv.URL + "/api/compare?scenario=single-map&baseline_run_id=" + baselineID + "&candidate_run_id=" + candidateID)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("compare status=%d body=%s", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "Benchmark comparison") {
		t.Fatalf("compare html missing title")
	}

	res, err = http.Get(srv.URL + "/api/compare?scenario=single-map&baseline_run_id=" + baselineID + "&candidate_run_id=" + baselineID)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("same-run compare status=%d body=%s", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "Benchmark result") || strings.Contains(string(body), "Benchmark comparison") {
		t.Fatalf("same-run compare should render single report")
	}
}

func TestPlotlyStaticServed(t *testing.T) {
	repo := openTestRepo(t)
	srv := httptest.NewServer(serve.New(repo).Handler())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/static/plotly.min.js")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 1000 {
		t.Fatalf("plotly asset too small: %d bytes", len(body))
	}
}
