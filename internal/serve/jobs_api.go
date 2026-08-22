package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"numa-perfman/internal/benchmark"
	"numa-perfman/internal/cluster"
	"numa-perfman/internal/doctor"
	"numa-perfman/internal/results"
	"numa-perfman/internal/scenario"
)

type metricEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Required    bool   `json:"required"`
	Unit        string `json:"unit"`
}

func (s *Server) handleBenchmarkRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listBenchmarkRuns(w, r)
	case http.MethodPost:
		s.startBenchmarkRun(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBenchmarkRunSubpath(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/benchmark-runs/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(rest, "/")
	runID := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.getBenchmarkRun(w, r, runID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	switch parts[1] {
	case "events":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.streamBenchmarkRunEvents(w, r, runID)
	case "cancel":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.cancelBenchmarkRun(w, r, runID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) listBenchmarkRuns(w http.ResponseWriter, r *http.Request) {
	if s.jobs == nil {
		http.Error(w, "benchmark execution is not enabled", http.StatusServiceUnavailable)
		return
	}
	scenarioID := strings.TrimSpace(r.URL.Query().Get("scenario"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			http.Error(w, "limit must be a non-negative integer", http.StatusBadRequest)
			return
		}
		limit = n
	}
	if scenarioID != "" {
		if _, err := scenario.LookupBenchmark(scenarioID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	runs, err := s.jobs.List(r.Context(), scenarioID, status, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"runs": runs})
}

func (s *Server) startBenchmarkRun(w http.ResponseWriter, r *http.Request) {
	if s.jobs == nil {
		http.Error(w, "benchmark execution is not enabled", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var req StartRunRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
	}
	info, err := s.jobs.Start(r.Context(), req)
	if err != nil {
		s.writeJobError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, info)
}

func (s *Server) getBenchmarkRun(w http.ResponseWriter, r *http.Request, runID string) {
	if s.jobs == nil {
		// Fall back to repository for read-only servers.
		run, err := s.repo.GetRun(r.Context(), runID)
		if err != nil {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		writeJSON(w, jobInfoFromRun(run))
		return
	}
	info, err := s.jobs.Get(r.Context(), runID)
	if err != nil {
		s.writeJobError(w, err)
		return
	}
	writeJSON(w, info)
}

func (s *Server) cancelBenchmarkRun(w http.ResponseWriter, r *http.Request, runID string) {
	if s.jobs == nil {
		http.Error(w, "benchmark execution is not enabled", http.StatusServiceUnavailable)
		return
	}
	if err := s.jobs.Cancel(runID); err != nil {
		s.writeJobError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"id": runID, "status": "cancelling"})
}

func (s *Server) streamBenchmarkRunEvents(w http.ResponseWriter, r *http.Request, runID string) {
	afterID := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after_id")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			http.Error(w, "after_id must be a non-negative integer", http.StatusBadRequest)
			return
		}
		afterID = n
	}

	wantSSE := strings.Contains(r.Header.Get("Accept"), "text/event-stream")
	if !wantSSE {
		var events []LiveEvent
		var err error
		if s.jobs != nil {
			events, err = s.jobs.ListEvents(r.Context(), runID, afterID)
		} else {
			events, err = listRepoEvents(r, s.repo, runID, afterID)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"events": events})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var events []LiveEvent
	var err error
	if s.jobs != nil {
		events, err = s.jobs.ListEvents(r.Context(), runID, afterID)
	} else {
		events, err = listRepoEvents(r, s.repo, runID, afterID)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	lastID := afterID
	for _, ev := range events {
		writeSSE(w, ev)
		lastID = ev.ID
	}
	flusher.Flush()

	if s.jobs == nil {
		return
	}
	live, unsub, ok := s.jobs.Subscribe(runID)
	if !ok {
		// Job already finished; send a terminal ping from DB status.
		info, getErr := s.jobs.Get(r.Context(), runID)
		if getErr == nil && benchmark.Terminal(info.Status) {
			writeSSE(w, LiveEvent{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Level:     "info",
				Phase:     info.Status,
				Message:   "run " + info.Status,
			})
			flusher.Flush()
		}
		return
	}
	defer unsub()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
			info, getErr := s.jobs.Get(r.Context(), runID)
			if getErr == nil && benchmark.Terminal(info.Status) && !info.Active {
				return
			}
		case ev, open := <-live:
			if !open {
				return
			}
			if ev.ID != 0 && ev.ID <= lastID {
				continue
			}
			writeSSE(w, ev)
			if ev.ID > lastID {
				lastID = ev.ID
			}
			flusher.Flush()
			if benchmark.Terminal(ev.Phase) {
				return
			}
		}
	}
}

func listRepoEvents(r *http.Request, repo *results.Repository, runID string, afterID int64) ([]LiveEvent, error) {
	events, err := repo.ListEvents(r.Context(), runID)
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

func writeSSE(w http.ResponseWriter, ev LiveEvent) {
	payload, _ := json.Marshal(ev)
	if ev.ID > 0 {
		fmt.Fprintf(w, "id: %d\n", ev.ID)
	}
	fmt.Fprintf(w, "data: %s\n\n", payload)
}

func (s *Server) handlePreflight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.client.Runner == nil && s.jobs == nil {
		http.Error(w, "preflight is not enabled", http.StatusServiceUnavailable)
		return
	}
	client := s.client
	cfg := s.cfg
	var promFwd *cluster.ForwardSession
	if cluster.ShouldAutoPrometheus(cfg.PrometheusURL) {
		sess, err := client.StartPrometheusForward(r.Context(), cfg.MonitoringNamespace)
		if err != nil {
			http.Error(w, "prometheus port-forward: "+err.Error(), http.StatusBadGateway)
			return
		}
		promFwd = sess
		cfg.PrometheusURL = sess.URL
		defer promFwd.Close()
	}
	res := doctor.Run(r.Context(), doctor.Options{
		Config:     cfg,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		Repository: s.repo,
	}, doctor.NewInspector(client), client.Runner)
	writeJSON(w, map[string]any{
		"passed": res.Passed(),
		"checks": res.Checks,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{
		"context":              s.cfg.Context,
		"cluster":              s.cfg.Cluster,
		"results_db":           s.cfg.ResultsDB,
		"prometheus_url":       s.cfg.PrometheusURL,
		"central_namespace":    s.cfg.CentralNamespace,
		"monitoring_namespace": s.cfg.MonitoringNamespace,
		"validation_namespace": s.cfg.ValidationNamespace,
		"log_format":           s.cfg.LogFormat,
		"verbose":              s.cfg.Verbose,
		"udf_image":            s.cfg.UDFImage,
		"image":                s.cfg.Image,
		"execution_enabled":    s.jobs != nil,
	})
}

func (s *Server) writeJobError(w http.ResponseWriter, err error) {
	var conflict *JobConflictError
	if errors.As(err, &conflict) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(conflict)
		return
	}
	if errors.Is(err, errBadRequest) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if errors.Is(err, errNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if errors.Is(err, errConflict) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func enrichBenchmarkMetrics(sc scenario.Scenario) []metricEntry {
	out := make([]metricEntry, 0, len(sc.Metrics))
	for _, m := range sc.Metrics {
		out = append(out, metricEntry{
			Name:        m.Name,
			DisplayName: m.DisplayName,
			Required:    m.Required,
			Unit:        string(m.Unit),
		})
	}
	return out
}
