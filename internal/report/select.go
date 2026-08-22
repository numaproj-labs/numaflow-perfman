package report

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"numa-perfman/internal/results"
)

// Builder loads runs from a results repository and constructs comparison reports.
type Builder struct {
	Repo *results.Repository
}

// Select resolves baseline and candidate runs according to sel.
func (b *Builder) Select(ctx context.Context, sel Selection) (SelectionResult, error) {
	if b == nil || b.Repo == nil {
		return SelectionResult{}, fmt.Errorf("repository is required")
	}
	if sel.Scenario == "" {
		return SelectionResult{}, fmt.Errorf("scenario is required")
	}

	out := SelectionResult{Scenario: sel.Scenario}

	var baselineRuns, candidateRuns []results.Run
	var err error

	if len(sel.BaselineRunIDs) > 0 || len(sel.CandidateRunIDs) > 0 {
		if len(sel.BaselineRunIDs) == 0 || len(sel.CandidateRunIDs) == 0 {
			return SelectionResult{}, fmt.Errorf("both baseline and candidate run ids are required")
		}
		baselineRuns, err = b.loadExplicitRuns(ctx, sel.BaselineRunIDs, sel.Scenario, GroupBaseline, &out)
		if err != nil {
			return SelectionResult{}, err
		}
		candidateRuns, err = b.loadExplicitRuns(ctx, sel.CandidateRunIDs, sel.Scenario, GroupCandidate, &out)
		if err != nil {
			return SelectionResult{}, err
		}
		baselineRuns, candidateRuns = partitionCompletedRuns(baselineRuns, candidateRuns, &out)
	} else if len(sel.RunIDs) > 0 {
		for _, id := range sel.RunIDs {
			run, err := b.Repo.GetRun(ctx, id)
			if err != nil {
				return SelectionResult{}, err
			}
			if run.Scenario != sel.Scenario {
				out.Excluded = append(out.Excluded, ExcludedRun{Run: run, Reason: "scenario mismatch"})
				continue
			}
			if run.Kind != results.KindBenchmark {
				out.Excluded = append(out.Excluded, ExcludedRun{Run: run, Reason: "not a benchmark run"})
				continue
			}
			group := classifyRunGroup(run, sel)
			if group == "" {
				out.Excluded = append(out.Excluded, ExcludedRun{Run: run, Reason: "run does not match baseline or candidate selectors"})
				continue
			}
			if group == GroupBaseline {
				baselineRuns = append(baselineRuns, run)
			} else {
				candidateRuns = append(candidateRuns, run)
			}
		}
		baselineRuns, candidateRuns = partitionCompletedRuns(baselineRuns, candidateRuns, &out)
	} else {
		baselineRuns, err = b.selectBySide(ctx, sel, GroupBaseline)
		if err != nil {
			return SelectionResult{}, err
		}
		candidateRuns, err = b.selectBySide(ctx, sel, GroupCandidate)
		if err != nil {
			return SelectionResult{}, err
		}
	}

	out.ReferenceConfig = referenceConfig(baselineRuns, candidateRuns)

	out.Baseline, err = b.attachSeries(ctx, baselineRuns, GroupBaseline)
	if err != nil {
		return SelectionResult{}, err
	}
	out.Candidate, err = b.attachSeries(ctx, candidateRuns, GroupCandidate)
	if err != nil {
		return SelectionResult{}, err
	}
	return out, nil
}

func (b *Builder) loadExplicitRuns(ctx context.Context, ids []string, scenario string, group Group, out *SelectionResult) ([]results.Run, error) {
	runs := make([]results.Run, 0, len(ids))
	for _, id := range ids {
		run, err := b.Repo.GetRun(ctx, id)
		if err != nil {
			return nil, err
		}
		switch {
		case run.Scenario != scenario:
			out.Excluded = append(out.Excluded, ExcludedRun{Run: run, Group: group, Reason: "scenario mismatch"})
		case run.Kind != results.KindBenchmark:
			out.Excluded = append(out.Excluded, ExcludedRun{Run: run, Group: group, Reason: "not a benchmark run"})
		default:
			runs = append(runs, run)
		}
	}
	return runs, nil
}

func classifyRunGroup(run results.Run, sel Selection) Group {
	if sideMatches(run, sel.BaselineImageRef, sel.BaselineImageDigest) {
		return GroupBaseline
	}
	if sideMatches(run, sel.CandidateImageRef, sel.CandidateImageDigest) {
		return GroupCandidate
	}
	return ""
}

func sideMatches(run results.Run, imageRef, digest string) bool {
	if imageRef != "" && run.ImageRef != imageRef {
		return false
	}
	if digest != "" && run.ImageDigest != digest {
		return false
	}
	return imageRef != "" || digest != ""
}

func (b *Builder) selectBySide(ctx context.Context, sel Selection, side Group) ([]results.Run, error) {
	f := results.ListRunsFilter{
		Kind:     results.KindBenchmark,
		Scenario: sel.Scenario,
		Status:   results.StatusCompleted,
		After:    sel.After,
		Before:   sel.Before,
	}
	switch side {
	case GroupBaseline:
		f.ImageRef = sel.BaselineImageRef
		f.ImageDigest = sel.BaselineImageDigest
		if f.ImageRef == "" && f.ImageDigest == "" {
			return nil, fmt.Errorf("baseline image ref or digest is required when run ids are not specified")
		}
		if sel.BaselineLatestComplete > 0 {
			f.Limit = sel.BaselineLatestComplete
		} else {
			f.Limit = 1
		}
	case GroupCandidate:
		f.ImageRef = sel.CandidateImageRef
		f.ImageDigest = sel.CandidateImageDigest
		if f.ImageRef == "" && f.ImageDigest == "" {
			return nil, fmt.Errorf("candidate image ref or digest is required when run ids are not specified")
		}
		if sel.CandidateLatestComplete > 0 {
			f.Limit = sel.CandidateLatestComplete
		} else {
			f.Limit = 1
		}
	}
	runs, err := b.Repo.ListRuns(ctx, f)
	if err != nil {
		return nil, err
	}
	return runs, nil
}

func referenceConfig(baseline, candidate []results.Run) RunConfigFingerprint {
	all := append(append([]results.Run{}, baseline...), candidate...)
	if len(all) == 0 {
		return RunConfigFingerprint{}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.Before(all[j].CreatedAt) })
	refRun := all[0]
	for _, r := range all {
		if r.Status == results.StatusCompleted {
			refRun = r
			break
		}
	}
	return fingerprint(refRun)
}

func fingerprint(run results.Run) RunConfigFingerprint {
	fp, _ := ParseRunConfig(run.ConfigJSON)
	fp.Scenario = run.Scenario
	fp.ManifestHash = run.ManifestHash
	return fp
}

func partitionCompletedRuns(baseline, candidate []results.Run, out *SelectionResult) ([]results.Run, []results.Run) {
	var keepB, keepC []results.Run
	for _, r := range baseline {
		if r.Status == results.StatusCompleted {
			keepB = append(keepB, r)
		} else {
			out.Excluded = append(out.Excluded, ExcludedRun{
				Run: r, Group: GroupBaseline, Reason: fmt.Sprintf("run not completed (status=%s)", r.Status),
			})
		}
	}
	for _, r := range candidate {
		if r.Status == results.StatusCompleted {
			keepC = append(keepC, r)
		} else {
			out.Excluded = append(out.Excluded, ExcludedRun{
				Run: r, Group: GroupCandidate, Reason: fmt.Sprintf("run not completed (status=%s)", r.Status),
			})
		}
	}
	return keepB, keepC
}

func (b *Builder) attachSeries(ctx context.Context, runs []results.Run, group Group) ([]SelectedRun, error) {
	out := make([]SelectedRun, 0, len(runs))
	for _, run := range runs {
		series, err := b.Repo.LoadMetricSeries(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, SelectedRun{Run: run, Group: group, Series: series})
	}
	return out, nil
}

// Build constructs a Report from a selection result.
func (b *Builder) Build(ctx context.Context, sel Selection) (*Report, error) {
	sr, err := b.Select(ctx, sel)
	if err != nil {
		return nil, err
	}
	if len(sr.Baseline) == 0 || len(sr.Candidate) == 0 {
		return nil, fmt.Errorf("need at least one completed baseline and candidate run (excluded=%d)", len(sr.Excluded))
	}

	chart := normalizedChartOptions(sel.Chart)

	report := &Report{
		GeneratedAt:            time.Now().UTC(),
		Scenario:               sel.Scenario,
		BaselineImageRef:       sideImageRef(sr.Baseline, sel.BaselineImageRef),
		CandidateImageRef:      sideImageRef(sr.Candidate, sel.CandidateImageRef),
		BaselineImageDigest:    sideDigest(sr.Baseline, sel.BaselineImageDigest),
		CandidateImageDigest:   sideDigest(sr.Candidate, sel.CandidateImageDigest),
		BaselineCompleteCount:  len(sr.Baseline),
		CandidateCompleteCount: len(sr.Candidate),
		ExcludedCount:          len(sr.Excluded),
		Chart:                  chart,
		Config:                 sr.ReferenceConfig,
	}
	report.Excluded = excludedSummaries(sr.Excluded)
	report.BaselineIncompleteCount, report.CandidateIncompleteCount = incompleteCounts(sr.Excluded)
	report.Warnings = buildWarnings(report)

	metricKeys := collectMetricKeys(sr)
	for _, key := range metricKeys {
		bVals := summaryValuesForMetric(sr.Baseline, key)
		cVals := summaryValuesForMetric(sr.Candidate, key)
		unit := unitForMetricKey(sr, key)
		cmp := CompareStats(key.Name, unit, ComputeStats(bVals), ComputeStats(cVals))
		cmp.Vertex = key.Vertex
		report.MetricComparisons = append(report.MetricComparisons, cmp)

		report.Series = append(report.Series,
			buildAggregated(key, unit, GroupBaseline, sr.Baseline, chart),
			buildAggregated(key, unit, GroupCandidate, sr.Candidate, chart),
		)
	}

	return report, nil
}

func sideImageRef(runs []SelectedRun, fallback string) string {
	if fallback != "" {
		return fallback
	}
	if len(runs) > 0 {
		return runs[0].Run.ImageRef
	}
	return ""
}

func sideDigest(runs []SelectedRun, fallback string) string {
	if fallback != "" {
		return fallback
	}
	for _, r := range runs {
		if r.Run.ImageDigest != "" {
			return r.Run.ImageDigest
		}
	}
	return ""
}

func excludedSummaries(in []ExcludedRun) []ExcludedRunSummary {
	out := make([]ExcludedRunSummary, 0, len(in))
	for _, e := range in {
		out = append(out, ExcludedRunSummary{
			RunID:    e.Run.ID,
			Group:    e.Group,
			Status:   e.Run.Status,
			Reason:   e.Reason,
			ImageRef: e.Run.ImageRef,
		})
	}
	return out
}

func incompleteCounts(excluded []ExcludedRun) (baseline, candidate int) {
	for _, e := range excluded {
		if !strings.HasPrefix(e.Reason, "run not completed") {
			continue
		}
		switch e.Group {
		case GroupBaseline:
			baseline++
		case GroupCandidate:
			candidate++
		}
	}
	return baseline, candidate
}

func buildWarnings(rep *Report) []string {
	var w []string
	if rep.ExcludedCount > 0 {
		w = append(w, fmt.Sprintf("%d run(s) excluded from comparison.", rep.ExcludedCount))
	}
	if rep.BaselineIncompleteCount > 0 {
		w = append(w, fmt.Sprintf("%d baseline run(s) were not completed.", rep.BaselineIncompleteCount))
	}
	if rep.CandidateIncompleteCount > 0 {
		w = append(w, fmt.Sprintf("%d candidate run(s) were not completed.", rep.CandidateIncompleteCount))
	}
	return w
}

func summaryValuesForMetric(runs []SelectedRun, key metricSeriesKey) []float64 {
	var vals []float64
	for _, r := range runs {
		for _, s := range r.Series {
			if !seriesMatchesKey(s, key) {
				continue
			}
			vals = append(vals, SeriesMeanValue(s))
		}
	}
	return vals
}

func unitForMetricKey(sr SelectionResult, key metricSeriesKey) string {
	for _, r := range sr.Baseline {
		for _, s := range r.Series {
			if seriesMatchesKey(s, key) {
				return s.Unit
			}
		}
	}
	for _, r := range sr.Candidate {
		for _, s := range r.Series {
			if seriesMatchesKey(s, key) {
				return s.Unit
			}
		}
	}
	return ""
}

func seriesMatchesKey(series results.LoadedMetricSeries, key metricSeriesKey) bool {
	if series.MetricName != key.Name {
		return false
	}
	return vertexFromLabelsJSON(series.LabelsJSON) == key.Vertex
}

func buildAggregated(key metricSeriesKey, unit string, group Group, runs []SelectedRun, chart ChartOptions) AggregatedSeries {
	agg := AggregatedSeries{MetricName: key.Name, Vertex: key.Vertex, Unit: unit, Group: group}
	for _, r := range runs {
		for _, s := range r.Series {
			if !seriesMatchesKey(s, key) {
				continue
			}
			agg.Repetitions = append(agg.Repetitions, RepetitionSeries{
				RunID:  r.Run.ID,
				Points: s.Points,
			})
		}
	}
	agg.Aggregate = AggregateElapsed(agg.Repetitions, chart)
	return agg
}
