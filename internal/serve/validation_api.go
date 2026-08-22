package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"numa-perfman/internal/benchmark"
	"numa-perfman/internal/results"
	"numa-perfman/internal/scenario"
)

type validationEntry struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

func (s *Server) handleValidations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ids := scenario.ListValidations()
	out := make([]validationEntry, 0, len(ids))
	for _, id := range ids {
		sc, err := scenario.LookupValidation(id)
		if err != nil {
			continue
		}
		out = append(out, validationEntry{ID: sc.ID, Description: sc.Description})
	}
	defaultID := ""
	if len(out) > 0 {
		defaultID = out[0].ID
	}
	writeJSON(w, map[string]any{
		"validations": out,
		"default":     defaultID,
	})
}

func (s *Server) handleValidationRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listValidationRuns(w, r)
	case http.MethodPost:
		s.startValidationRun(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleValidationRunSubpath(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/validation-runs/")
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
			s.getValidationRun(w, r, runID)
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
		s.streamValidationRunEvents(w, r, runID)
	case "cancel":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.cancelValidationRun(w, r, runID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) listValidationRuns(w http.ResponseWriter, r *http.Request) {
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
		if _, err := scenario.LookupValidation(scenarioID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	var (
		runs []ValidationRunInfo
		err  error
	)
	if s.jobs != nil {
		runs, err = s.jobs.ListValidations(r.Context(), scenarioID, status, limit)
	} else {
		runs, err = s.listValidationRunsFromRepo(r, scenarioID, status, limit)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"runs": runs})
}

func (s *Server) listValidationRunsFromRepo(r *http.Request, scenarioID, status string, limit int) ([]ValidationRunInfo, error) {
	runs, err := s.repo.ListRuns(r.Context(), results.ListRunsFilter{
		Kind:     results.KindValidation,
		Scenario: scenarioID,
		Status:   status,
		Limit:    limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ValidationRunInfo, 0, len(runs))
	for _, run := range runs {
		info, err := validationRunInfoFromRun(r.Context(), s.repo, run)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

func (s *Server) startValidationRun(w http.ResponseWriter, r *http.Request) {
	if s.jobs == nil {
		http.Error(w, "validation execution is not enabled", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var req StartValidationRunRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
	}
	info, err := s.jobs.StartValidation(r.Context(), req)
	if err != nil {
		s.writeJobError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, info)
}

func (s *Server) getValidationRun(w http.ResponseWriter, r *http.Request, runID string) {
	if s.jobs != nil {
		info, err := s.jobs.GetValidation(r.Context(), runID)
		if err != nil {
			s.writeJobError(w, err)
			return
		}
		writeJSON(w, info)
		return
	}
	run, err := s.repo.GetRun(r.Context(), runID)
	if err != nil || run.Kind != results.KindValidation {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	info, err := validationRunInfoFromRun(r.Context(), s.repo, run)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, info)
}

func (s *Server) cancelValidationRun(w http.ResponseWriter, r *http.Request, runID string) {
	if s.jobs == nil {
		http.Error(w, "validation execution is not enabled", http.StatusServiceUnavailable)
		return
	}
	if err := s.jobs.Cancel(runID); err != nil {
		s.writeJobError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"id": runID, "status": "cancelling"})
}

func (s *Server) streamValidationRunEvents(w http.ResponseWriter, r *http.Request, runID string) {
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
		events, err := validationEvents(r, s, runID, afterID)
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

	events, err := validationEvents(r, s, runID, afterID)
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
		info, getErr := s.jobs.GetValidation(r.Context(), runID)
		if getErr == nil && validationTerminal(info.Status) {
			writeSSE(w, LiveEvent{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Level:     "info",
				Phase:     info.Status,
				Message:   "validation " + info.Status,
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
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
			info, getErr := s.jobs.GetValidation(r.Context(), runID)
			if getErr == nil && validationTerminal(info.Status) && !info.Active {
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
			if validationTerminal(ev.Phase) || benchmark.Terminal(ev.Phase) {
				return
			}
		}
	}
}

func validationEvents(r *http.Request, s *Server, runID string, afterID int64) ([]LiveEvent, error) {
	if s.jobs != nil {
		return s.jobs.ListEvents(r.Context(), runID, afterID)
	}
	return listRepoEvents(r, s.repo, runID, afterID)
}
