package report

import (
	"encoding/json"
	"sort"

	"numa-perfman/internal/results"
)

type metricSeriesKey struct {
	Name   string
	Vertex string
}

func metricKeyFromSeries(series results.LoadedMetricSeries) metricSeriesKey {
	return metricSeriesKey{
		Name:   series.MetricName,
		Vertex: vertexFromLabelsJSON(series.LabelsJSON),
	}
}

func vertexFromLabelsJSON(labelsJSON string) string {
	if labelsJSON == "" || labelsJSON == "{}" {
		return ""
	}
	var labels map[string]string
	if err := json.Unmarshal([]byte(labelsJSON), &labels); err != nil {
		return ""
	}
	return labels["vertex"]
}

func collectMetricKeys(sr SelectionResult) []metricSeriesKey {
	seen := map[metricSeriesKey]struct{}{}
	add := func(runs []SelectedRun) {
		for _, r := range runs {
			for _, s := range r.Series {
				seen[metricKeyFromSeries(s)] = struct{}{}
			}
		}
	}
	add(sr.Baseline)
	add(sr.Candidate)
	keys := make([]metricSeriesKey, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Name != keys[j].Name {
			return keys[i].Name < keys[j].Name
		}
		return keys[i].Vertex < keys[j].Vertex
	})
	return keys
}

func uniqueMetricNames(keys []metricSeriesKey) []string {
	seen := map[string]struct{}{}
	var names []string
	for _, key := range keys {
		if _, ok := seen[key.Name]; ok {
			continue
		}
		seen[key.Name] = struct{}{}
		names = append(names, key.Name)
	}
	return names
}

func verticesForMetric(keys []metricSeriesKey, metricName string) []string {
	seen := map[string]struct{}{}
	var vertices []string
	for _, key := range keys {
		if key.Name != metricName || key.Vertex == "" {
			continue
		}
		if _, ok := seen[key.Vertex]; ok {
			continue
		}
		seen[key.Vertex] = struct{}{}
		vertices = append(vertices, key.Vertex)
	}
	sort.Strings(vertices)
	return vertices
}

func metricKeysFromSingleReport(rep *SingleReport) []metricSeriesKey {
	seen := map[metricSeriesKey]struct{}{}
	for _, metric := range rep.Metrics {
		seen[metricSeriesKey{Name: metric.MetricName, Vertex: vertexFromLabelsJSON(metric.LabelsJSON)}] = struct{}{}
	}
	keys := make([]metricSeriesKey, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Name != keys[j].Name {
			return keys[i].Name < keys[j].Name
		}
		return keys[i].Vertex < keys[j].Vertex
	})
	return keys
}

func verticesForSingleMetric(metrics []MetricValues, metricName string) []string {
	keys := make([]metricSeriesKey, 0, len(metrics))
	for _, metric := range metrics {
		keys = append(keys, metricSeriesKey{Name: metric.MetricName, Vertex: vertexFromLabelsJSON(metric.LabelsJSON)})
	}
	return verticesForMetric(keys, metricName)
}

func unitForSingleMetric(metrics []MetricValues, metricName string) string {
	for _, metric := range metrics {
		if metric.MetricName == metricName {
			return metric.Unit
		}
	}
	return ""
}

func metricKeysFromReport(rep *Report) []metricSeriesKey {
	seen := map[metricSeriesKey]struct{}{}
	for _, mc := range rep.MetricComparisons {
		seen[metricSeriesKey{Name: mc.MetricName, Vertex: mc.Vertex}] = struct{}{}
	}
	keys := make([]metricSeriesKey, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Name != keys[j].Name {
			return keys[i].Name < keys[j].Name
		}
		return keys[i].Vertex < keys[j].Vertex
	})
	return keys
}

func unitForMetricName(rep *Report, metricName string) string {
	for _, mc := range rep.MetricComparisons {
		if mc.MetricName == metricName {
			return mc.Unit
		}
	}
	return ""
}
