package report

import (
	"fmt"
	"math"
)

func formatStat(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "—"
	}
	return fmt.Sprintf("%.2f", v)
}

func formatMetricStat(metricName string, value float64) string {
	return formatStat(chartValue(metricName, value))
}

func metricDisplayUnit(metricName, unit string) string {
	return chartUnit(metricName, unit)
}

func metricStatsLabel(metricName, vertex, unit string) string {
	label := metricName
	if vertex != "" {
		label += " - " + vertex
	}
	if displayUnit := metricDisplayUnit(metricName, unit); displayUnit != "" {
		label += " (" + displayUnit + ")"
	}
	return label
}
