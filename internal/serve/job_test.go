package serve_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"numa-perfman/internal/cluster"
	"numa-perfman/internal/config"
	"numa-perfman/internal/results"
	"numa-perfman/internal/serve"
)

func TestAPIConfigAndBenchmarkMetrics(t *testing.T) {
	repo := openTestRepo(t)
	cfg := config.Defaults()
	cfg.UDFImage = "udf:local"
	cfg.Context = "kind-numaflow"
	srv := httptest.NewServer(serve.NewWithOptions(serve.Options{
		Repo:       repo,
		Config:     cfg,
		Client:     cluster.Client{Runner: cluster.DefaultRunner},
		Version:    "test",
		EnableJobs: true,
	}).Handler())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("config status=%d", res.StatusCode)
	}
	var cfgPayload map[string]any
	if err := json.NewDecoder(res.Body).Decode(&cfgPayload); err != nil {
		t.Fatal(err)
	}
	if cfgPayload["udf_image"] != "udf:local" {
		t.Fatalf("udf_image=%v", cfgPayload["udf_image"])
	}
	if cfgPayload["execution_enabled"] != true {
		t.Fatalf("execution_enabled=%v", cfgPayload["execution_enabled"])
	}

	res, err = http.Get(srv.URL + "/api/benchmarks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var benchPayload struct {
		Benchmarks []struct {
			ID      string `json:"id"`
			Metrics []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"metrics"`
		} `json:"benchmarks"`
	}
	if err := json.NewDecoder(res.Body).Decode(&benchPayload); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range benchPayload.Benchmarks {
		if b.ID == "single-map" {
			found = true
			if len(b.Metrics) == 0 {
				t.Fatal("expected metrics on single-map")
			}
		}
	}
	if !found {
		t.Fatal("single-map missing")
	}
}

func TestAPIStartRunValidationAndOverwrite(t *testing.T) {
	repo := openTestRepo(t)
	cfg := config.Defaults()
	cfg.UDFImage = "udf:local"
	srv := httptest.NewServer(serve.NewWithOptions(serve.Options{
		Repo:       repo,
		Config:     cfg,
		Client:     cluster.Client{Runner: cluster.DefaultRunner},
		EnableJobs: true,
	}).Handler())
	t.Cleanup(srv.Close)

	// Missing image -> 400
	res, err := http.Post(srv.URL+"/api/benchmark-runs", "application/json", strings.NewReader(`{"scenario":"single-map"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := readAll(res)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing image status=%d body=%s", res.StatusCode, body)
	}

	existing := seedCompletedRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.8.2")
	res, err = http.Post(srv.URL+"/api/benchmark-runs", "application/json", bytes.NewReader([]byte(
		`{"scenario":"single-map","image":"quay.io/numaproj/numaflow:v1.8.2","duration":"1m"}`,
	)))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = readAll(res)
	res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("overwrite status=%d body=%s", res.StatusCode, body)
	}
	var conflict struct {
		Code        string `json:"code"`
		ExistingRun struct {
			ID string `json:"id"`
		} `json:"existing_run"`
	}
	if err := json.Unmarshal(body, &conflict); err != nil {
		t.Fatal(err)
	}
	if conflict.Code != "overwrite_required" {
		t.Fatalf("code=%q", conflict.Code)
	}
	if conflict.ExistingRun.ID != existing {
		t.Fatalf("existing=%q want=%q", conflict.ExistingRun.ID, existing)
	}
}

func TestAPIListBenchmarkRunsAllStatuses(t *testing.T) {
	repo := openTestRepo(t)
	completed := seedCompletedRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.8.0")
	ctx := t.Context()
	failedID := "11111111-1111-1111-1111-111111111111"
	if err := repo.CreateRun(ctx, results.CreateRunParams{
		ID: failedID, Kind: results.KindBenchmark, Scenario: "single-map",
		ImageRef: "quay.io/numaproj/numaflow:v1.7.0", Status: results.StatusCreated,
		ManifestHash: "h", ConfigJSON: `{}`, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := repo.UpdateRunLifecycle(ctx, failedID, results.UpdateRunLifecycleParams{
		Status: results.StatusFailed, CompletedAt: &now, ErrorMessage: "boom",
	}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(serve.NewWithOptions(serve.Options{
		Repo:       repo,
		Config:     config.Defaults(),
		EnableJobs: true,
	}).Handler())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/api/benchmark-runs?scenario=single-map")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var payload struct {
		Runs []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"runs"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Runs) < 2 {
		t.Fatalf("runs=%d", len(payload.Runs))
	}
	seen := map[string]string{}
	for _, r := range payload.Runs {
		seen[r.ID] = r.Status
	}
	if seen[completed] != results.StatusCompleted {
		t.Fatalf("completed status=%q", seen[completed])
	}
	if seen[failedID] != results.StatusFailed {
		t.Fatalf("failed status=%q", seen[failedID])
	}
}

func TestAPIBenchmarkRunEventsJSON(t *testing.T) {
	repo := openTestRepo(t)
	id := seedCompletedRun(t, repo, "single-map", "quay.io/numaproj/numaflow:v1.8.0")
	ctx := t.Context()
	if err := repo.AddEvent(ctx, results.AddEventParams{
		RunID: id, Timestamp: time.Now().UTC(), Level: "info", Phase: "measuring", Message: "measuring",
	}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(serve.NewWithOptions(serve.Options{
		Repo:       repo,
		Config:     config.Defaults(),
		EnableJobs: true,
	}).Handler())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/api/benchmark-runs/" + id + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var payload struct {
		Events []struct {
			Phase   string `json:"phase"`
			Message string `json:"message"`
		} `json:"events"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Events) == 0 || payload.Events[0].Phase != "measuring" {
		t.Fatalf("events=%v", payload.Events)
	}
}

func readAll(res *http.Response) ([]byte, error) {
	buf := &bytes.Buffer{}
	_, err := buf.ReadFrom(res.Body)
	return buf.Bytes(), err
}
