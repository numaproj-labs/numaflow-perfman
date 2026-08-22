package report

import (
	"context"
	"encoding/json"
	"fmt"

	"numa-perfman/internal/results"
)

// PersistComparison marshals rep as JSON and stores it in the results database.
func PersistComparison(ctx context.Context, repo *results.Repository, sel Selection, rep *Report) (string, error) {
	if repo == nil {
		return "", fmt.Errorf("repository is required")
	}
	if rep == nil {
		return "", fmt.Errorf("report is nil")
	}
	payload, err := json.Marshal(rep)
	if err != nil {
		return "", err
	}
	selectionJSON, err := json.Marshal(sel)
	if err != nil {
		return "", err
	}
	stored, err := repo.SaveReport(ctx, results.SaveReportParams{
		Kind:              results.ReportKindComparison,
		Scenario:          rep.Scenario,
		GeneratedAt:       rep.GeneratedAt,
		SelectionJSON:     string(selectionJSON),
		Payload:           payload,
		BaselineImageRef:  rep.BaselineImageRef,
		CandidateImageRef: rep.CandidateImageRef,
		RunRefs:           comparisonRunRefs(rep),
	})
	if err != nil {
		return "", err
	}
	return stored.ID, nil
}

// PersistSingle marshals rep as JSON and stores it in the results database.
func PersistSingle(ctx context.Context, repo *results.Repository, sel SingleSelection, rep *SingleReport) (string, error) {
	if repo == nil {
		return "", fmt.Errorf("repository is required")
	}
	if rep == nil {
		return "", fmt.Errorf("single report is nil")
	}
	payload, err := json.Marshal(rep)
	if err != nil {
		return "", err
	}
	selectionJSON, err := json.Marshal(sel)
	if err != nil {
		return "", err
	}
	stored, err := repo.SaveReport(ctx, results.SaveReportParams{
		Kind:             results.ReportKindSingle,
		Scenario:         rep.Scenario,
		GeneratedAt:      rep.GeneratedAt,
		SelectionJSON:    string(selectionJSON),
		Payload:          payload,
		BaselineImageRef: rep.ImageRef,
		RunRefs: []results.ReportRunRef{{
			RunID: rep.RunID,
			Group: string(GroupResult),
		}},
	})
	if err != nil {
		return "", err
	}
	return stored.ID, nil
}

// RenderStoredJSON returns the canonical report payload bytes.
func RenderStoredJSON(stored results.StoredReport) ([]byte, error) {
	if len(stored.Payload) == 0 {
		return nil, fmt.Errorf("report %q has empty payload", stored.ID)
	}
	return append([]byte(nil), stored.Payload...), nil
}

// RenderStoredHTML renders HTML from the stored canonical report model.
func RenderStoredHTML(stored results.StoredReport, plotlyScriptSrc string) ([]byte, error) {
	switch stored.Kind {
	case results.ReportKindComparison:
		var rep Report
		if err := json.Unmarshal(stored.Payload, &rep); err != nil {
			return nil, fmt.Errorf("decode comparison report: %w", err)
		}
		return RenderCompareHTML(&rep, plotlyScriptSrc)
	case results.ReportKindSingle:
		var rep SingleReport
		if err := json.Unmarshal(stored.Payload, &rep); err != nil {
			return nil, fmt.Errorf("decode single report: %w", err)
		}
		return RenderSingleHTML(&rep, plotlyScriptSrc)
	default:
		return nil, fmt.Errorf("unsupported report kind %q", stored.Kind)
	}
}

func comparisonRunRefs(rep *Report) []results.ReportRunRef {
	seen := make(map[string]string)
	for _, series := range rep.Series {
		group := string(series.Group)
		for _, rep := range series.Repetitions {
			if rep.RunID == "" {
				continue
			}
			if _, ok := seen[rep.RunID]; !ok {
				seen[rep.RunID] = group
			}
		}
	}
	out := make([]results.ReportRunRef, 0, len(seen))
	for runID, group := range seen {
		out = append(out, results.ReportRunRef{RunID: runID, Group: group})
	}
	return out
}
