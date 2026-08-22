package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"numa-perfman/internal/report"
	"numa-perfman/internal/results"
	"numa-perfman/internal/scenario"
)

type benchmarkEntry struct {
	ID          string        `json:"id"`
	Description string        `json:"description"`
	Metrics     []metricEntry `json:"metrics,omitempty"`
}

type runEntry struct {
	ID        string `json:"id"`
	ImageRef  string `json:"image_ref"`
	CreatedAt string `json:"created_at"`
	Label     string `json:"label"`
}

func (s *Server) handleBenchmarks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ids := scenario.ListBenchmarks()
	out := make([]benchmarkEntry, 0, len(ids))
	for _, id := range ids {
		sc, err := scenario.LookupBenchmark(id)
		if err != nil {
			continue
		}
		out = append(out, benchmarkEntry{
			ID:          sc.ID,
			Description: sc.Description,
			Metrics:     enrichBenchmarkMetrics(sc),
		})
	}
	defaultID := defaultBenchmark
	if !containsBenchmarkID(out, defaultBenchmark) && len(out) > 0 {
		defaultID = out[0].ID
	}
	writeJSON(w, map[string]any{
		"benchmarks": out,
		"default":    defaultID,
	})
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scenarioID := strings.TrimSpace(r.URL.Query().Get("scenario"))
	if scenarioID == "" {
		http.Error(w, "scenario is required", http.StatusBadRequest)
		return
	}
	if _, err := scenario.LookupBenchmark(scenarioID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	runs, err := s.repo.ListRuns(r.Context(), results.ListRunsFilter{
		Kind:     results.KindBenchmark,
		Scenario: scenarioID,
		Status:   results.StatusCompleted,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]runEntry, 0, len(runs))
	for _, run := range runs {
		out = append(out, runEntry{
			ID:        run.ID,
			ImageRef:  run.ImageRef,
			CreatedAt: run.CreatedAt.UTC().Format(time.RFC3339),
			Label:     runDisplayLabel(run),
		})
	}
	writeJSON(w, map[string]any{"runs": out})
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scenarioID := strings.TrimSpace(r.URL.Query().Get("scenario"))
	runID := strings.TrimSpace(r.URL.Query().Get("run_id"))
	if scenarioID == "" || runID == "" {
		http.Error(w, "scenario and run_id are required", http.StatusBadRequest)
		return
	}
	s.renderSingleReport(w, r, scenarioID, runID)
}

func (s *Server) renderSingleReport(w http.ResponseWriter, r *http.Request, scenarioID, runID string) {
	rep, err := (&report.Builder{Repo: s.repo}).BuildSingle(r.Context(), report.SingleSelection{
		Scenario: scenarioID,
		RunID:    runID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	html, err := report.RenderSingleHTML(rep, plotlyScriptForWeb)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html)
}

func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scenarioID := strings.TrimSpace(r.URL.Query().Get("scenario"))
	baselineRunID := strings.TrimSpace(r.URL.Query().Get("baseline_run_id"))
	candidateRunID := strings.TrimSpace(r.URL.Query().Get("candidate_run_id"))
	if scenarioID == "" || baselineRunID == "" || candidateRunID == "" {
		http.Error(w, "scenario, baseline_run_id, and candidate_run_id are required", http.StatusBadRequest)
		return
	}
	if baselineRunID == candidateRunID {
		s.renderSingleReport(w, r, scenarioID, baselineRunID)
		return
	}
	rep, err := (&report.Builder{Repo: s.repo}).Build(r.Context(), report.Selection{
		Scenario:        scenarioID,
		BaselineRunIDs:  []string{baselineRunID},
		CandidateRunIDs: []string{candidateRunID},
		Chart:           report.DefaultChartOptions(),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	html, err := report.RenderCompareHTML(rep, plotlyScriptForWeb)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html)
}

func containsBenchmarkID(entries []benchmarkEntry, id string) bool {
	for _, e := range entries {
		if e.ID == id {
			return true
		}
	}
	return false
}

func runDisplayLabel(run results.Run) string {
	shortID := run.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return fmt.Sprintf("%s — %s — %s", run.ImageRef, run.CreatedAt.UTC().Format(time.RFC3339), shortID)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ListCompletedRuns is a helper for tests.
func ListCompletedRuns(ctx context.Context, repo *results.Repository, scenarioID string) ([]results.Run, error) {
	return repo.ListRuns(ctx, results.ListRunsFilter{
		Kind:     results.KindBenchmark,
		Scenario: scenarioID,
		Status:   results.StatusCompleted,
	})
}
