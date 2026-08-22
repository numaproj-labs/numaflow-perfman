package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"math"

	"numa-perfman/internal/results"
)

const (
	plotlyCDN    = "https://cdn.plot.ly/plotly-2.35.2.min.js"
	bytesPerMB   = 1024 * 1024
	vertexMemory = "vertex_memory"
)

// PlotlyScriptSrc is the script URL embedded in generated report HTML.
// Serve overrides this to a local path when rendering reports for the web UI.
var PlotlyScriptSrc = plotlyCDN

type plotlyChart struct {
	Data   []plotlyTrace  `json:"data"`
	Layout plotlyLayout   `json:"layout"`
	Config map[string]any `json:"config"`
}

type plotlyTrace struct {
	Type          string         `json:"type"`
	Mode          string         `json:"mode,omitempty"`
	Name          string         `json:"name,omitempty"`
	LegendGroup   string         `json:"legendgroup,omitempty"`
	ShowLegend    bool           `json:"showlegend"`
	X             []float64      `json:"x"`
	Y             []float64      `json:"y"`
	CustomData    [][]float64    `json:"customdata,omitempty"`
	Line          map[string]any `json:"line,omitempty"`
	Marker        map[string]any `json:"marker,omitempty"`
	Fill          string         `json:"fill,omitempty"`
	FillColor     string         `json:"fillcolor,omitempty"`
	HoverTemplate string         `json:"hovertemplate,omitempty"`
	HoverInfo     string         `json:"hoverinfo,omitempty"`
	ConnectGaps   bool           `json:"connectgaps"`
}

type plotlyLayout struct {
	Title           map[string]any `json:"title"`
	Height          int            `json:"height"`
	PaperBackground string         `json:"paper_bgcolor"`
	PlotBackground  string         `json:"plot_bgcolor"`
	Font            map[string]any `json:"font"`
	Margin          map[string]int `json:"margin"`
	HoverMode       string         `json:"hovermode"`
	HoverLabel      map[string]any `json:"hoverlabel"`
	Legend          map[string]any `json:"legend"`
	XAxis           map[string]any `json:"xaxis"`
	YAxis           map[string]any `json:"yaxis"`
}

type vertexChartBundle struct {
	MetricName string                 `json:"metric_name"`
	Unit       string                 `json:"unit"`
	Default    string                 `json:"default"`
	Vertices   []string               `json:"vertices"`
	Charts     map[string]plotlyChart `json:"charts"`
}

func writeMetricPlotly(buf *bytes.Buffer, rep *Report, metricName, unit string, vertices []string, id string) error {
	bundle := vertexChartBundle{
		MetricName: metricName,
		Unit:       unit,
		Vertices:   vertices,
		Charts:     make(map[string]plotlyChart, len(vertices)),
	}
	if len(vertices) == 0 {
		vertices = []string{""}
	}
	bundle.Default = vertices[0]
	baselineSource := sourceDisplayLabel(rep.BaselineImageRef)
	candidateSource := sourceDisplayLabel(rep.CandidateImageRef)
	for _, vertex := range vertices {
		baseline, candidate := seriesForMetricVertex(rep, metricName, vertex)
		chart := newPlotlyChart(metricName, unit, true)
		appendRepetitionTraces(&chart.Data, metricName, baseline.Repetitions, baselineSource, "#5794F2", "rgba(87,148,242,0.32)")
		appendRepetitionTraces(&chart.Data, metricName, candidate.Repetitions, candidateSource, "#E24D42", "rgba(226,77,66,0.32)")
		appendAggregateTraces(&chart.Data, metricName, baseline.Aggregate, baselineSource, "#5794F2", "rgba(87,148,242,0.18)")
		appendAggregateTraces(&chart.Data, metricName, candidate.Aggregate, candidateSource, "#E24D42", "rgba(226,77,66,0.18)")
		bundle.Charts[vertexChartKey(vertex)] = chart
	}
	return writeVertexChartBundle(buf, id, bundle)
}

func seriesForMetricVertex(rep *Report, metric, vertex string) (baseline, candidate AggregatedSeries) {
	for _, series := range rep.Series {
		if series.MetricName != metric || series.Vertex != vertex {
			continue
		}
		switch series.Group {
		case GroupBaseline:
			baseline = series
		case GroupCandidate:
			candidate = series
		}
	}
	return baseline, candidate
}

func seriesForMetric(rep *Report, metric string) (baseline, candidate AggregatedSeries) {
	return seriesForMetricVertex(rep, metric, "")
}

func writeSingleMetricPlotly(buf *bytes.Buffer, metrics []MetricValues, metricName, unit, source, id string, vertices []string) error {
	bundle := vertexChartBundle{
		MetricName: metricName,
		Unit:       unit,
		Vertices:   vertices,
		Charts:     make(map[string]plotlyChart, len(vertices)),
	}
	if len(vertices) == 0 {
		vertices = []string{""}
	}
	bundle.Default = vertices[0]
	for _, vertex := range vertices {
		metric := metricValuesForVertex(metrics, metricName, vertex)
		x, y := metricValueCoordinates(metricName, metric.Values)
		chart := newPlotlyChart(metricName, unit, false)
		chart.Data = append(chart.Data, plotlyTrace{
			Type:        "scatter",
			Mode:        "lines+markers",
			Name:        plotlyText(source),
			LegendGroup: plotlyText(source),
			ShowLegend:  true,
			X:           x,
			Y:           y,
			Line: map[string]any{
				"color": "#5794F2",
				"width": 2.5,
			},
			Marker: map[string]any{
				"color": "#5794F2",
				"size":  7,
			},
			HoverTemplate: valueHoverTemplate(metricName, source, unit),
			ConnectGaps:   false,
		})
		bundle.Charts[vertexChartKey(vertex)] = chart
	}
	return writeVertexChartBundle(buf, id, bundle)
}

func metricValuesForVertex(metrics []MetricValues, metricName, vertex string) MetricValues {
	for _, metric := range metrics {
		if metric.MetricName != metricName {
			continue
		}
		if vertexFromLabelsJSON(metric.LabelsJSON) == vertex {
			return metric
		}
	}
	return MetricValues{MetricName: metricName}
}

func chartUnit(metricName, unit string) string {
	if metricName == vertexMemory {
		return "MB"
	}
	return unit
}

func chartValue(metricName string, value float64) float64 {
	if metricName == vertexMemory {
		return value / bytesPerMB
	}
	return value
}

func newPlotlyChart(metricName, unit string, comparison bool) plotlyChart {
	displayUnit := chartUnit(metricName, unit)
	hoverMode := "closest"
	if comparison {
		hoverMode = "x unified"
	}
	return plotlyChart{
		Layout: plotlyLayout{
			Title: map[string]any{
				"text":    fmt.Sprintf("%s <span style=\"color:#9FA7B3;font-size:12px\">(%s)</span>", plotlyText(metricName), plotlyText(displayUnit)),
				"x":       0,
				"xanchor": "left",
				"font": map[string]any{
					"size": 14,
				},
			},
			Height:          340,
			PaperBackground: "#181B1F",
			PlotBackground:  "#181B1F",
			Font: map[string]any{
				"color":  "#D8D9DA",
				"family": "system-ui, -apple-system, BlinkMacSystemFont, \"Segoe UI\", sans-serif",
				"size":   12,
			},
			Margin: map[string]int{
				"l": 68,
				"r": 24,
				"t": 48,
				"b": 68,
			},
			HoverMode: hoverMode,
			HoverLabel: map[string]any{
				"bgcolor":     "#111217",
				"bordercolor": "#4A525E",
				"font": map[string]any{
					"color": "#F5F6F7",
				},
			},
			Legend: map[string]any{
				"orientation": "h",
				"x":           0,
				"y":           -0.28,
				"font": map[string]any{
					"size": 11,
				},
			},
			XAxis: plotlyXAxis(comparison),
			YAxis: plotlyYAxis(displayUnit),
		},
		Config: map[string]any{
			"displaylogo": false,
			"responsive":  true,
			"scrollZoom":  true,
		},
	}
}

func plotlyXAxis(comparison bool) map[string]any {
	axis := plotlyAxis("Elapsed time (min)")
	axis["showspikes"] = false
	if comparison {
		axis["unifiedhovertitle"] = map[string]any{
			"text": "",
		}
	}
	return axis
}

func plotlyYAxis(title string) map[string]any {
	axis := plotlyAxis(title)
	axis["showspikes"] = false
	return axis
}

func plotlyAxis(title string) map[string]any {
	return map[string]any{
		"title": map[string]any{
			"text":     title,
			"standoff": 12,
		},
		"automargin": true,
		"gridcolor":  "#2F3542",
		"gridwidth":  1,
		"linecolor":  "#4A525E",
		"linewidth":  1,
		"showline":   true,
		"ticks":      "outside",
		"tickcolor":  "#6B7280",
		"ticklen":    4,
		"zeroline":   false,
	}
}

func appendRepetitionTraces(traces *[]plotlyTrace, metricName string, repetitions []RepetitionSeries, source, color, faintColor string) {
	for _, repetition := range repetitions {
		x, y := metricPointCoordinates(metricName, repetition.Points)
		if len(x) == 0 {
			continue
		}
		*traces = append(*traces, plotlyTrace{
			Type:        "scatter",
			Mode:        "lines+markers",
			Name:        plotlyText(source) + " repetition",
			LegendGroup: plotlyText(source),
			ShowLegend:  false,
			X:           x,
			Y:           y,
			Line: map[string]any{
				"color": faintColor,
				"width": 1,
			},
			Marker: map[string]any{
				"color": faintColor,
				"size":  5,
			},
			HoverInfo:   "skip",
			ConnectGaps: false,
		})
	}
}

func appendAggregateTraces(traces *[]plotlyTrace, metricName string, points []TimeSeriesPoint, source, color, bandColor string) {
	x, lower, upper, center := aggregateCoordinates(metricName, points)
	if len(x) == 0 {
		return
	}

	*traces = append(*traces,
		plotlyTrace{
			Type:        "scatter",
			Mode:        "lines",
			Name:        plotlyText(source) + " percentile band",
			LegendGroup: plotlyText(source),
			ShowLegend:  false,
			X:           x,
			Y:           lower,
			Line: map[string]any{
				"color": bandColor,
				"width": 0,
			},
			HoverInfo:   "skip",
			ConnectGaps: false,
		},
		plotlyTrace{
			Type:        "scatter",
			Mode:        "lines",
			Name:        plotlyText(source) + " percentile band",
			LegendGroup: plotlyText(source),
			ShowLegend:  false,
			X:           x,
			Y:           upper,
			Line: map[string]any{
				"color": bandColor,
				"width": 0,
			},
			Fill:        "tonexty",
			FillColor:   bandColor,
			HoverInfo:   "skip",
			ConnectGaps: false,
		},
	)

	*traces = append(*traces, plotlyTrace{
		Type:        "scatter",
		Mode:        "lines+markers",
		Name:        plotlyText(source),
		LegendGroup: plotlyText(source),
		ShowLegend:  true,
		X:           x,
		Y:           center,
		Line: map[string]any{
			"color": color,
			"width": 2.5,
		},
		Marker: map[string]any{
			"color": color,
			"size":  7,
		},
		HoverTemplate: comparisonHoverTemplate(source),
		ConnectGaps:   false,
	})
}

func metricPointCoordinates(metricName string, points []results.MetricPoint) ([]float64, []float64) {
	x := make([]float64, 0, len(points))
	y := make([]float64, 0, len(points))
	for _, point := range points {
		if !validPlotlyValue(point.ElapsedMilliseconds, point.Value) {
			continue
		}
		x = append(x, elapsedMinutes(point.ElapsedMilliseconds))
		y = append(y, chartValue(metricName, point.Value))
	}
	return x, y
}

func metricValueCoordinates(metricName string, values []MetricValue) ([]float64, []float64) {
	x := make([]float64, 0, len(values))
	y := make([]float64, 0, len(values))
	for _, value := range values {
		if !validPlotlyValue(value.ElapsedMilliseconds, value.Value) {
			continue
		}
		x = append(x, elapsedMinutes(value.ElapsedMilliseconds))
		y = append(y, chartValue(metricName, value.Value))
	}
	return x, y
}

func aggregateCoordinates(metricName string, points []TimeSeriesPoint) (x, lower, upper, center []float64) {
	x = make([]float64, 0, len(points))
	lower = make([]float64, 0, len(points))
	upper = make([]float64, 0, len(points))
	center = make([]float64, 0, len(points))
	for _, point := range points {
		if !validPlotlyValue(point.ElapsedMilliseconds, point.Center) ||
			math.IsNaN(point.Lower) || math.IsInf(point.Lower, 0) ||
			math.IsNaN(point.Upper) || math.IsInf(point.Upper, 0) {
			continue
		}
		x = append(x, elapsedMinutes(point.ElapsedMilliseconds))
		lower = append(lower, chartValue(metricName, point.Lower))
		upper = append(upper, chartValue(metricName, point.Upper))
		center = append(center, chartValue(metricName, point.Center))
	}
	return x, lower, upper, center
}

func validPlotlyValue(elapsedMilliseconds int64, value float64) bool {
	return elapsedMilliseconds >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func elapsedMinutes(elapsedMilliseconds int64) float64 {
	return float64(elapsedMilliseconds) / 60000
}

func valueHoverTemplate(metricName, source, unit string) string {
	displayUnit := chartUnit(metricName, unit)
	if displayUnit == "" {
		displayUnit = "value"
	}
	return fmt.Sprintf("<b>%s</b><br>%%{y:.2f} %s<extra></extra>", plotlyText(source), plotlyText(displayUnit))
}

func comparisonHoverTemplate(source string) string {
	return fmt.Sprintf("%s : %%{y:.2f}<extra></extra>", plotlyText(source))
}

func plotlyText(value string) string {
	return html.EscapeString(value)
}

func writePlotlyChart(buf *bytes.Buffer, id, metricName string, chart plotlyChart) error {
	data, err := json.Marshal(chart)
	if err != nil {
		return fmt.Errorf("marshal Plotly chart %q: %w", metricName, err)
	}
	fmt.Fprintf(buf, `<div id="%s" class="metric-chart" role="img" aria-label="%s chart. Hover a data point for values." aria-busy="true"></div>`,
		html.EscapeString(id), html.EscapeString(metricName))
	buf.WriteString(`<script type="application/json" class="plotly-chart-data">`)
	buf.Write(data)
	buf.WriteString(`</script>`)
	return nil
}

func writeVertexChartBundle(buf *bytes.Buffer, id string, bundle vertexChartBundle) error {
	data, err := json.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("marshal vertex chart bundle %q: %w", bundle.MetricName, err)
	}
	fmt.Fprintf(buf, `<div class="chart-controls">`)
	if len(bundle.Vertices) > 0 {
		fmt.Fprintf(buf, `<label for="%s-vertex">Vertex</label>`, html.EscapeString(id))
		fmt.Fprintf(buf, `<select id="%s-vertex" class="vertex-select" data-chart-id="%s">`, html.EscapeString(id), html.EscapeString(id))
		for _, vertex := range bundle.Vertices {
			selected := ""
			if vertex == bundle.Default {
				selected = ` selected`
			}
			fmt.Fprintf(buf, `<option value="%s"%s>%s</option>`, html.EscapeString(vertexChartKey(vertex)), selected, html.EscapeString(vertex))
		}
		buf.WriteString(`</select>`)
	}
	buf.WriteString(`</div>`)
	fmt.Fprintf(buf, `<div id="%s" class="metric-chart" role="img" aria-label="%s chart. Hover a data point for values." aria-busy="true"></div>`,
		html.EscapeString(id), html.EscapeString(bundle.MetricName))
	fmt.Fprintf(buf, `<script type="application/json" class="plotly-vertex-chart-data" data-chart-id="%s">`, html.EscapeString(id))
	buf.Write(data)
	buf.WriteString(`</script>`)
	return nil
}

func vertexChartKey(vertex string) string {
	if vertex == "" {
		return "_default"
	}
	return vertex
}

func plotlyScriptURL(src string) string {
	if src != "" {
		return src
	}
	return PlotlyScriptSrc
}

func writePlotlyChartScript(buf *bytes.Buffer, plotlyScriptSrc string) {
	fmt.Fprintf(buf, `<style>.js-plotly-plot .hoverlayer .axistext{display:none!important}</style>
<script src="%s"></script>
<script>
(() => {
  function vertexChartKey(vertex) {
    return vertex || "_default";
  }
  const hideHoverAxisText = (chart) => {
    chart.querySelectorAll(".hoverlayer .axistext").forEach((node) => {
      node.style.display = "none";
    });
  };
  const renderChart = (chart, spec) => {
    chart.replaceChildren();
    Plotly.newPlot(chart, spec.data, spec.layout, spec.config).then(() => {
      hideHoverAxisText(chart);
      chart.on("plotly_hover", () => hideHoverAxisText(chart));
    });
    chart.setAttribute("aria-busy", "false");
  };
  const renderVertexBundle = (chart, bundle) => {
    const vertex = chart.dataset.vertex || vertexChartKey(bundle.default) || "_default";
    const spec = bundle.charts[vertex] || bundle.charts[vertexChartKey(bundle.default)] || Object.values(bundle.charts)[0];
    if (!spec) throw new Error("missing vertex chart spec");
    renderChart(chart, spec);
  };
  document.querySelectorAll(".plotly-vertex-chart-data").forEach((node) => {
    const bundle = JSON.parse(node.textContent);
    const chartID = node.getAttribute("data-chart-id");
    const chart = chartID ? document.getElementById(chartID) : null;
    if (!chart) return;
    const select = document.querySelector('.vertex-select[data-chart-id="' + chart.id + '"]');
    chart.dataset.vertex = select ? select.value : vertexChartKey(bundle.default);
    try {
      if (!window.Plotly) throw new Error("Plotly did not load");
      renderVertexBundle(chart, bundle);
      if (select) {
        select.addEventListener("change", () => {
          chart.dataset.vertex = select.value;
          try {
            renderVertexBundle(chart, bundle);
          } catch (error) {
            console.error("Unable to render benchmark chart", error);
          }
        });
      }
    } catch (error) {
      chart.classList.add("metric-chart--error");
      chart.setAttribute("aria-busy", "false");
      chart.textContent = "Unable to render interactive chart.";
      console.error("Unable to render benchmark chart", error);
    }
  });
  const charts = document.querySelectorAll(".metric-chart");
  const data = document.querySelectorAll(".plotly-chart-data");
  charts.forEach((chart, index) => {
    if (chart.dataset.vertex !== undefined) return;
    try {
      const spec = JSON.parse(data[index].textContent);
      if (!window.Plotly) throw new Error("Plotly did not load");
      renderChart(chart, spec);
    } catch (error) {
      chart.classList.add("metric-chart--error");
      chart.setAttribute("aria-busy", "false");
      chart.textContent = "Unable to render interactive chart.";
      console.error("Unable to render benchmark chart", error);
    }
  });
})();
</script>`, html.EscapeString(plotlyScriptURL(plotlyScriptSrc)))
}
