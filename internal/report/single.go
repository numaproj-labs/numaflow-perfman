package report

import (
	"context"
	"fmt"
	"sort"
	"time"

	"numa-perfman/internal/results"
)

// BuildSingle constructs a report for one completed benchmark run.
func (b *Builder) BuildSingle(ctx context.Context, sel SingleSelection) (*SingleReport, error) {
	if b == nil || b.Repo == nil {
		return nil, fmt.Errorf("repository is required")
	}
	if sel.Scenario == "" {
		return nil, fmt.Errorf("scenario is required")
	}

	run, err := b.selectSingleRun(ctx, sel)
	if err != nil {
		return nil, err
	}
	series, err := b.Repo.LoadMetricSeries(ctx, run.ID)
	if err != nil {
		return nil, err
	}

	rep := &SingleReport{
		GeneratedAt: time.Now().UTC(),
		Scenario:    run.Scenario,
		RunID:       run.ID,
		ImageRef:    run.ImageRef,
		ImageDigest: run.ImageDigest,
		CompletedAt: run.CompletedAt,
		Config:      fingerprint(run),
	}

	for _, s := range series {
		rep.Metrics = append(rep.Metrics, metricValues(s))
	}
	sort.Slice(rep.Metrics, func(i, j int) bool {
		if rep.Metrics[i].MetricName != rep.Metrics[j].MetricName {
			return rep.Metrics[i].MetricName < rep.Metrics[j].MetricName
		}
		return rep.Metrics[i].LabelsJSON < rep.Metrics[j].LabelsJSON
	})
	return rep, nil
}

func (b *Builder) selectSingleRun(ctx context.Context, sel SingleSelection) (results.Run, error) {
	if sel.RunID != "" {
		if sel.ImageRef != "" || sel.ImageDigest != "" || sel.After != nil || sel.Before != nil {
			return results.Run{}, fmt.Errorf("run-id cannot be combined with image, digest, or time selectors")
		}
		run, err := b.Repo.GetRun(ctx, sel.RunID)
		if err != nil {
			return results.Run{}, err
		}
		if run.Scenario != sel.Scenario {
			return results.Run{}, fmt.Errorf("run %q has scenario %q, not %q", run.ID, run.Scenario, sel.Scenario)
		}
		if run.Kind != results.KindBenchmark {
			return results.Run{}, fmt.Errorf("run %q is not a benchmark run", run.ID)
		}
		if run.Status != results.StatusCompleted {
			return results.Run{}, fmt.Errorf("run %q is not completed (status=%s)", run.ID, run.Status)
		}
		return run, nil
	}

	if sel.ImageRef == "" && sel.ImageDigest == "" {
		return results.Run{}, fmt.Errorf("run-id, image, or image digest is required")
	}
	runs, err := b.Repo.ListRuns(ctx, results.ListRunsFilter{
		Kind:        results.KindBenchmark,
		Scenario:    sel.Scenario,
		ImageRef:    sel.ImageRef,
		ImageDigest: sel.ImageDigest,
		Status:      results.StatusCompleted,
		After:       sel.After,
		Before:      sel.Before,
		Limit:       1,
	})
	if err != nil {
		return results.Run{}, err
	}
	if len(runs) == 0 {
		return results.Run{}, fmt.Errorf("no completed benchmark run matches the selectors")
	}
	return runs[0], nil
}

func metricValues(series results.LoadedMetricSeries) MetricValues {
	values := make([]MetricValue, len(series.Points))
	for i, point := range series.Points {
		values[i] = MetricValue{
			Timestamp:           point.Timestamp,
			ElapsedMilliseconds: point.ElapsedMilliseconds,
			Value:               point.Value,
		}
	}
	return MetricValues{
		MetricName:  series.MetricName,
		DisplayName: series.DisplayName,
		Unit:        series.Unit,
		LabelsJSON:  series.LabelsJSON,
		Values:      values,
	}
}
