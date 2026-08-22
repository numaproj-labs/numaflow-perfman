package benchmark

import (
	"encoding/json"
	"fmt"
	"time"

	"numa-perfman/internal/prometheus"
	"numa-perfman/internal/results"
	"numa-perfman/internal/scenario"
)

// SeriesToSaveParams converts Prometheus query results into repository metric inputs.
func SeriesToSaveParams(runID string, measureStart time.Time, required []string, series []prometheus.SeriesResult) (results.SaveMetricsParams, error) {
	if measureStart.IsZero() {
		return results.SaveMetricsParams{}, fmt.Errorf("measurement start time is required")
	}
	var inputs []results.MetricSeriesInput
	for _, sr := range series {
		if len(sr.Samples) == 0 && sr.Metric.Required {
			return results.SaveMetricsParams{}, fmt.Errorf("%w: %s has no samples", ErrMetricsIncomplete, metricSeriesName(sr))
		}
		if len(sr.Samples) == 0 {
			continue
		}
		points := make([]results.MetricPoint, len(sr.Samples))
		for i, s := range sr.Samples {
			elapsed := s.Timestamp.Sub(measureStart)
			if elapsed < 0 {
				elapsed = 0
			}
			points[i] = results.MetricPoint{
				Timestamp:           s.Timestamp,
				ElapsedMilliseconds: elapsed.Milliseconds(),
				Value:               s.Value,
			}
		}
		labelsJSON, err := marshalLabelsJSON(sr.Labels)
		if err != nil {
			return results.SaveMetricsParams{}, fmt.Errorf("labels for %s: %w", metricSeriesName(sr), err)
		}
		inputs = append(inputs, results.MetricSeriesInput{
			MetricName:      sr.Metric.Name,
			DisplayName:     sr.Metric.DisplayName,
			Unit:            string(sr.Metric.Unit),
			PrometheusQuery: sr.Metric.Query,
			LabelsJSON:      labelsJSON,
			Points:          points,
		})
	}
	return results.SaveMetricsParams{
		RunID:           runID,
		RequiredMetrics: required,
		Series:          inputs,
	}, nil
}

// ValidateCollectedMetrics enforces required definitions after collection.
func ValidateCollectedMetrics(defs []scenario.MetricDefinition, series []prometheus.SeriesResult) error {
	var errs []error
	for _, def := range defs {
		if !def.Required {
			continue
		}
		if len(def.GroupBy) > 0 {
			if !hasRequiredGroupedSeries(def, series) {
				errs = append(errs, fmt.Errorf("%s: no grouped series with sufficient samples", def.Name))
			}
			continue
		}
		sr, ok := singleSeriesByName(series, def.Name)
		if !ok || len(sr.Samples) == 0 {
			errs = append(errs, fmt.Errorf("%s: empty result", def.Name))
			continue
		}
		if len(sr.Samples) < def.MinSamples {
			errs = append(errs, fmt.Errorf("%s: insufficient samples (%d < %d)", def.Name, len(sr.Samples), def.MinSamples))
		}
	}
	if len(errs) > 0 {
		return WrapMetricsIncomplete(fmt.Errorf("%v", errs))
	}
	return nil
}

func hasRequiredGroupedSeries(def scenario.MetricDefinition, series []prometheus.SeriesResult) bool {
	for _, sr := range series {
		if sr.Metric.Name != def.Name {
			continue
		}
		if len(sr.Samples) >= def.MinSamples {
			return true
		}
	}
	return false
}

func singleSeriesByName(series []prometheus.SeriesResult, name string) (prometheus.SeriesResult, bool) {
	for _, sr := range series {
		if sr.Metric.Name == name && len(sr.Labels) == 0 {
			return sr, true
		}
	}
	return prometheus.SeriesResult{}, false
}

func metricSeriesName(sr prometheus.SeriesResult) string {
	if v := sr.Labels["vertex"]; v != "" {
		return fmt.Sprintf("%s[%s]", sr.Metric.Name, v)
	}
	return sr.Metric.Name
}

func marshalLabelsJSON(labels map[string]string) (string, error) {
	if len(labels) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
