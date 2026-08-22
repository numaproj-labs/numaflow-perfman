package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"time"
)

// RenderSingleHTML returns single-run report HTML bytes.
func RenderSingleHTML(rep *SingleReport, plotlyScriptSrc string) ([]byte, error) {
	if rep == nil {
		return nil, fmt.Errorf("single report is nil")
	}
	var buf bytes.Buffer
	buf.WriteString("<!DOCTYPE html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	buf.WriteString("<title>Benchmark result — ")
	buf.WriteString(html.EscapeString(rep.Scenario))
	buf.WriteString("</title><style>")
	buf.WriteString(`body{font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:2rem;max-width:1200px;color:#d8d9da;background:#0b0e11}
h1,h2{margin-top:1.5rem;color:#f5f6f7}table{border-collapse:collapse;width:100%;font-size:0.9rem;background:#181b1f}
th,td{border:1px solid #2f3542;padding:0.35rem 0.5rem;text-align:right}
th{background:#1f2329;color:#f5f6f7}
th:first-child,td:first-child{text-align:left}
.meta{color:#a9b0ba;font-size:0.95rem;line-height:1.5}
.chart{margin:1.5rem 0;border:1px solid #2f3542;border-radius:4px;padding:0.5rem;background:#181b1f;box-shadow:0 1px 2px #0006}
.chart-controls{display:flex;align-items:center;gap:0.5rem;margin:0.25rem 0 0.75rem;color:#a9b0ba;font-size:0.9rem}
.chart-controls select{background:#111217;color:#d8d9da;border:1px solid #4a525e;border-radius:4px;padding:0.25rem 0.5rem}
.metric-chart{min-height:340px;width:100%;color:#a9b0ba}
.metric-chart .plot-container,.metric-chart .svg-container{width:100%!important}
.metric-chart--error{color:#f2495c}
.chart-stats{margin-top:0.75rem}`)
	buf.WriteString("</style></head><body>")

	buf.WriteString("<h1>Benchmark result</h1>")
	writeSingleHTMLMeta(&buf, rep)

	buf.WriteString("<h2>Elapsed-time charts</h2>")
	source := sourceDisplayLabel(rep.ImageRef)
	metricNames := uniqueMetricNames(metricKeysFromSingleReport(rep))
	for i, metricName := range metricNames {
		buf.WriteString(`<div class="chart">`)
		vertices := verticesForSingleMetric(rep.Metrics, metricName)
		unit := unitForSingleMetric(rep.Metrics, metricName)
		if err := writeSingleMetricPlotly(&buf, rep.Metrics, metricName, unit, source, fmt.Sprintf("metric-chart-%d", i), vertices); err != nil {
			return nil, err
		}
		buf.WriteString(`<div class="chart-stats">`)
		writeSingleHTMLValuesTable(&buf, metricsForName(rep.Metrics, metricName))
		buf.WriteString(`</div></div>`)
	}

	writePlotlyChartScript(&buf, plotlyScriptSrc)
	buf.WriteString("</body></html>\n")
	return buf.Bytes(), nil
}

// MarshalSingleJSON returns indented JSON for a single-run report model.
func MarshalSingleJSON(rep *SingleReport) ([]byte, error) {
	if rep == nil {
		return nil, fmt.Errorf("single report is nil")
	}
	if rep.GeneratedAt.IsZero() {
		rep.GeneratedAt = time.Now().UTC()
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func writeSingleHTMLMeta(buf *bytes.Buffer, rep *SingleReport) {
	buf.WriteString("<p class=\"meta\">Scenario: <strong>")
	buf.WriteString(html.EscapeString(rep.Scenario))
	buf.WriteString("</strong><br>Run: <code>")
	buf.WriteString(html.EscapeString(rep.RunID))
	buf.WriteString("</code><br>Image: ")
	buf.WriteString(html.EscapeString(rep.ImageRef))
	if rep.ImageDigest != "" {
		buf.WriteString(" <code>")
		buf.WriteString(html.EscapeString(rep.ImageDigest))
		buf.WriteString("</code>")
	}
	if rep.CompletedAt != nil {
		buf.WriteString("<br>Completed: ")
		buf.WriteString(html.EscapeString(rep.CompletedAt.UTC().Format(time.RFC3339)))
	}
	buf.WriteString("</p>")
}

func metricsForName(metrics []MetricValues, metricName string) []MetricValues {
	var out []MetricValues
	for _, metric := range metrics {
		if metric.MetricName == metricName {
			out = append(out, metric)
		}
	}
	return out
}

func writeSingleHTMLValuesTable(buf *bytes.Buffer, metrics []MetricValues) {
	if len(metrics) == 0 {
		return
	}
	buf.WriteString("<table><thead><tr><th>Metric</th><th>Avg</th><th>p99</th></tr></thead><tbody>")
	for _, metric := range metrics {
		average, p99 := metricAverageAndP99(metric.Values)
		vertex := vertexFromLabelsJSON(metric.LabelsJSON)
		buf.WriteString("<tr><td>")
		buf.WriteString(html.EscapeString(metricStatsLabel(metric.MetricName, vertex, metric.Unit)))
		buf.WriteString("</td><td>")
		buf.WriteString(formatMetricStat(metric.MetricName, average))
		buf.WriteString("</td><td>")
		buf.WriteString(formatMetricStat(metric.MetricName, p99))
		buf.WriteString("</td></tr>")
	}
	buf.WriteString("</tbody></table>")
}
