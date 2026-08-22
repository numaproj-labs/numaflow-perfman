package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"time"
)

// RenderCompareHTML returns comparison report HTML bytes.
func RenderCompareHTML(rep *Report, plotlyScriptSrc string) ([]byte, error) {
	if rep == nil {
		return nil, fmt.Errorf("report is nil")
	}
	var buf bytes.Buffer
	buf.WriteString("<!DOCTYPE html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	buf.WriteString("<title>Benchmark comparison — ")
	buf.WriteString(html.EscapeString(rep.Scenario))
	buf.WriteString("</title><style>")
	buf.WriteString(`body{font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:2rem;max-width:1200px;color:#d8d9da;background:#0b0e11}
h1,h2{margin-top:1.5rem;color:#f5f6f7}table{border-collapse:collapse;width:100%;font-size:0.9rem;background:#181b1f}
th,td{border:1px solid #2f3542;padding:0.35rem 0.5rem;text-align:right}
th{background:#1f2329;color:#f5f6f7}
th:first-child,td:first-child{text-align:left}
thead tr + tr th,thead tr + tr th:first-child{text-align:right}
th.group-header{text-align:center}
.warn{background:#3a2b14;color:#f9d589;padding:0.75rem;border:1px solid #80651f;margin:1rem 0}
.meta{color:#a9b0ba;font-size:0.95rem;line-height:1.5}
.chart{margin:1.5rem 0;border:1px solid #2f3542;border-radius:4px;padding:0.5rem;background:#181b1f;box-shadow:0 1px 2px #0006}
.chart-controls{display:flex;align-items:center;gap:0.5rem;margin:0.25rem 0 0.75rem;color:#a9b0ba;font-size:0.9rem}
.chart-controls select{background:#111217;color:#d8d9da;border:1px solid #4a525e;border-radius:4px;padding:0.25rem 0.5rem}
.metric-chart{min-height:340px;width:100%;color:#a9b0ba}
.metric-chart .plot-container,.metric-chart .svg-container{width:100%!important}
.metric-chart--error{color:#f2495c}
.chart-stats{margin-top:0.75rem}`)
	buf.WriteString("</style></head><body>")

	buf.WriteString("<h1>Benchmark comparison</h1>")
	writeHTMLMeta(&buf, rep)

	if len(rep.Warnings) > 0 {
		buf.WriteString(`<div class="warn">`)
		for _, w := range rep.Warnings {
			buf.WriteString(html.EscapeString(w))
			buf.WriteString(" ")
		}
		buf.WriteString("</div>")
	}

	if len(rep.Excluded) > 0 {
		buf.WriteString("<h2>Excluded runs</h2><table><thead><tr>")
		for _, h := range []string{"Run ID", "Side", "Status", "Reason", "Image"} {
			buf.WriteString("<th>")
			buf.WriteString(html.EscapeString(h))
			buf.WriteString("</th>")
		}
		buf.WriteString("</tr></thead><tbody>")
		for _, e := range rep.Excluded {
			buf.WriteString("<tr><td>")
			buf.WriteString(html.EscapeString(e.RunID))
			buf.WriteString("</td><td>")
			buf.WriteString(html.EscapeString(string(e.Group)))
			buf.WriteString("</td><td>")
			buf.WriteString(html.EscapeString(e.Status))
			buf.WriteString("</td><td>")
			buf.WriteString(html.EscapeString(e.Reason))
			buf.WriteString("</td><td>")
			buf.WriteString(html.EscapeString(e.ImageRef))
			buf.WriteString("</td></tr>")
		}
		buf.WriteString("</tbody></table>")
	}

	buf.WriteString("<h2>Elapsed-time charts</h2>")
	metricKeys := metricKeysFromReport(rep)
	for i, metricName := range uniqueMetricNames(metricKeys) {
		buf.WriteString(`<div class="chart">`)
		if err := writeMetricPlotly(&buf, rep, metricName, unitForMetricName(rep, metricName), verticesForMetric(metricKeys, metricName), fmt.Sprintf("metric-chart-%d", i)); err != nil {
			return nil, err
		}
		buf.WriteString(`<div class="chart-stats">`)
		writeHTMLStatsTable(&buf, comparisonsForMetric(rep, metricName))
		buf.WriteString(`</div></div>`)
	}

	writePlotlyChartScript(&buf, plotlyScriptSrc)
	buf.WriteString("</body></html>\n")
	return buf.Bytes(), nil
}

// MarshalComparisonJSON returns indented JSON for a comparison report model.
func MarshalComparisonJSON(rep *Report) ([]byte, error) {
	if rep == nil {
		return nil, fmt.Errorf("report is nil")
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

func writeHTMLMeta(buf *bytes.Buffer, rep *Report) {
	buf.WriteString("<p class=\"meta\">Scenario: <strong>")
	buf.WriteString(html.EscapeString(rep.Scenario))
	buf.WriteString("</strong><br>Baseline: ")
	buf.WriteString(html.EscapeString(rep.BaselineImageRef))
	if rep.BaselineImageDigest != "" {
		buf.WriteString(" <code>")
		buf.WriteString(html.EscapeString(rep.BaselineImageDigest))
		buf.WriteString("</code>")
	}
	buf.WriteString("<br>Candidate: ")
	buf.WriteString(html.EscapeString(rep.CandidateImageRef))
	if rep.CandidateImageDigest != "" {
		buf.WriteString(" <code>")
		buf.WriteString(html.EscapeString(rep.CandidateImageDigest))
		buf.WriteString("</code>")
	}
	buf.WriteString("<br>Complete repetitions: baseline ")
	fmt.Fprintf(buf, "%d", rep.BaselineCompleteCount)
	buf.WriteString(", candidate ")
	fmt.Fprintf(buf, "%d", rep.CandidateCompleteCount)
	buf.WriteString("; excluded ")
	fmt.Fprintf(buf, "%d", rep.ExcludedCount)
	if rep.BaselineIncompleteCount+rep.CandidateIncompleteCount > 0 {
		buf.WriteString("; incomplete baseline/candidate ")
		fmt.Fprintf(buf, "%d/%d", rep.BaselineIncompleteCount, rep.CandidateIncompleteCount)
	}
	buf.WriteString("</p>")
}

func comparisonsForMetric(rep *Report, metricName string) []MetricComparison {
	var out []MetricComparison
	for _, m := range rep.MetricComparisons {
		if m.MetricName == metricName {
			out = append(out, m)
		}
	}
	return out
}

func writeHTMLStatsTable(buf *bytes.Buffer, comparisons []MetricComparison) {
	if len(comparisons) == 0 {
		return
	}
	buf.WriteString(`<table><thead><tr><th rowspan="2">Metric</th>`)
	buf.WriteString(`<th colspan="2" class="group-header">Baseline</th><th colspan="2" class="group-header">Candidate</th>`)
	buf.WriteString(`<th rowspan="2">Δ abs</th><th rowspan="2">Δ %</th></tr><tr>`)
	buf.WriteString(`<th>Avg</th><th>p99</th><th>Avg</th><th>p99</th></tr></thead><tbody>`)

	for _, m := range comparisons {
		writeStatsRow(buf, m)
	}
	buf.WriteString("</tbody></table>")
}

func writeStatsRow(buf *bytes.Buffer, m MetricComparison) {
	buf.WriteString("<tr><td>")
	buf.WriteString(html.EscapeString(metricStatsLabel(m.MetricName, m.Vertex, m.Unit)))
	buf.WriteString("</td><td>")
	buf.WriteString(formatMetricStat(m.MetricName, m.Baseline.Mean))
	buf.WriteString("</td><td>")
	buf.WriteString(formatMetricStat(m.MetricName, m.Baseline.P99))
	buf.WriteString("</td><td>")
	buf.WriteString(formatMetricStat(m.MetricName, m.Candidate.Mean))
	buf.WriteString("</td><td>")
	buf.WriteString(formatMetricStat(m.MetricName, m.Candidate.P99))
	buf.WriteString("</td><td>")
	buf.WriteString(formatMetricStat(m.MetricName, m.AbsoluteChange))
	buf.WriteString("</td><td>")
	fmt.Fprintf(buf, "%.2f%%", m.PercentChange)
	buf.WriteString("</td></tr>")
}
