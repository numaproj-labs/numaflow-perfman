package report

import (
	"bytes"
	"fmt"
	"html"
	"strings"
)

func writeMetricSVG(buf *bytes.Buffer, rep *Report, metricName, unit string, width, height int) {
	bSeries, cSeries := seriesForMetric(rep, metricName)
	maxElapsed, minVal, maxVal := plotBounds(bSeries, cSeries)
	if maxElapsed == 0 {
		maxElapsed = 1
	}
	if maxVal <= minVal {
		maxVal = minVal + 1
	}
	baselineSource := sourceDisplayLabel(rep.BaselineImageRef)
	candidateSource := sourceDisplayLabel(rep.CandidateImageRef)
	marginL, marginT, marginB := 58, 24, 62
	plotW := width - marginL - 8
	plotH := height - marginT - marginB

	fmt.Fprintf(buf, `<svg class="metric-chart" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="100%%" role="img" aria-label="%s chart. Hover or focus a point for values." data-plot-top="%d" data-plot-bottom="%d">`,
		width, height, html.EscapeString(metricName), marginT, marginT+plotH)
	fmt.Fprintf(buf, `<text x="%d" y="16" font-size="12" fill="#111">%s (%s)</text>`,
		marginL, html.EscapeString(metricName), html.EscapeString(unit))
	fmt.Fprintf(buf, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#1e1e1e"/>`,
		marginL, marginT+plotH, marginL+plotW, marginT+plotH)
	fmt.Fprintf(buf, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#1e1e1e"/>`,
		marginL, marginT, marginL, marginT+plotH)
	fmt.Fprintf(buf, `<text x="%d" y="%d" text-anchor="middle" font-size="10" fill="#444">Elapsed time (min)</text>`,
		marginL+plotW/2, height-8)
	fmt.Fprintf(buf, `<text x="14" y="%d" text-anchor="middle" font-size="10" fill="#444" transform="rotate(-90 14 %d)">%s</text>`,
		marginT+plotH/2, marginT+plotH/2, html.EscapeString(unit))

	writeSVGReps(buf, bSeries.Repetitions, marginL, marginT, plotW, plotH, maxElapsed, minVal, maxVal, svgBaselineFaint)
	writeSVGReps(buf, cSeries.Repetitions, marginL, marginT, plotW, plotH, maxElapsed, minVal, maxVal, svgCandidateFaint)
	writeSVGBand(buf, bSeries.Aggregate, marginL, marginT, plotW, plotH, maxElapsed, minVal, maxVal, svgBaselineBand)
	writeSVGBand(buf, cSeries.Aggregate, marginL, marginT, plotW, plotH, maxElapsed, minVal, maxVal, svgCandidateBand)
	writeSVGRepetitionPoints(buf, bSeries.Repetitions, marginL, marginT, plotW, plotH, maxElapsed, minVal, maxVal, baselineSource, unit)
	writeSVGRepetitionPoints(buf, cSeries.Repetitions, marginL, marginT, plotW, plotH, maxElapsed, minVal, maxVal, candidateSource, unit)
	writeSVGAggregate(buf, bSeries.Aggregate, marginL, marginT, plotW, plotH, maxElapsed, minVal, maxVal, svgBaseline, baselineSource, unit, rep.Chart)
	writeSVGAggregate(buf, cSeries.Aggregate, marginL, marginT, plotW, plotH, maxElapsed, minVal, maxVal, svgCandidate, candidateSource, unit, rep.Chart)

	writeSVGLegend(buf, marginL, height-30, baselineSource, svgBaseline)
	writeSVGLegend(buf, width/2, height-30, candidateSource, svgCandidate)
	buf.WriteString(`</svg>`)
}

func writeSVGReps(buf *bytes.Buffer, reps []RepetitionSeries, plotX, plotY, plotW, plotH int, maxElapsed int64, minVal, maxVal float64, stroke string) {
	for _, rep := range reps {
		var b strings.Builder
		for i, p := range rep.Points {
			x := svgX(plotX, plotW, p.ElapsedMilliseconds, maxElapsed)
			y := svgY(plotY, plotH, p.Value, minVal, maxVal)
			if i == 0 {
				fmt.Fprintf(&b, "M%d,%d", x, y)
			} else {
				fmt.Fprintf(&b, " L%d,%d", x, y)
			}
		}
		if len(rep.Points) >= 2 {
			fmt.Fprintf(buf, `<path d="%s" fill="none" stroke="%s" stroke-width="1" pointer-events="none"/>`, b.String(), stroke)
		}
	}
}

func writeSVGRepetitionPoints(buf *bytes.Buffer, reps []RepetitionSeries, plotX, plotY, plotW, plotH int, maxElapsed int64, minVal, maxVal float64, source, unit string) {
	for _, rep := range reps {
		for _, p := range rep.Points {
			x := svgX(plotX, plotW, p.ElapsedMilliseconds, maxElapsed)
			y := svgY(plotY, plotH, p.Value, minVal, maxVal)
			title := fmt.Sprintf("%s measurement: elapsed=%s min value=%s %s",
				source, formatElapsedMinutes(p.ElapsedMilliseconds), formatStat(p.Value), unit)
			fmt.Fprintf(buf, `<circle class="chart-point chart-point--repetition" cx="%d" cy="%d" r="7" fill="transparent" stroke="none" pointer-events="all" tabindex="0" data-source="%s" data-kind="repetition" data-elapsed-ms="%d" data-value="%s" data-unit="%s" aria-label="%s"></circle>`,
				x, y, html.EscapeString(source), p.ElapsedMilliseconds,
				html.EscapeString(formatStat(p.Value)), html.EscapeString(unit), html.EscapeString(title))
		}
	}
}

func writeSVGBand(buf *bytes.Buffer, agg []TimeSeriesPoint, plotX, plotY, plotW, plotH int, maxElapsed int64, minVal, maxVal float64, fill string) {
	if len(agg) < 2 {
		return
	}
	var upper, lower strings.Builder
	for i, p := range agg {
		x := svgX(plotX, plotW, p.ElapsedMilliseconds, maxElapsed)
		yU := svgY(plotY, plotH, p.Upper, minVal, maxVal)
		if i == 0 {
			fmt.Fprintf(&upper, "M%d,%d", x, yU)
		} else {
			fmt.Fprintf(&upper, " L%d,%d", x, yU)
		}
	}
	for i := len(agg) - 1; i >= 0; i-- {
		p := agg[i]
		x := svgX(plotX, plotW, p.ElapsedMilliseconds, maxElapsed)
		yL := svgY(plotY, plotH, p.Lower, minVal, maxVal)
		if i == len(agg)-1 {
			fmt.Fprintf(&lower, "L%d,%d", x, yL)
		} else {
			fmt.Fprintf(&lower, " L%d,%d", x, yL)
		}
	}
	path := upper.String() + " " + lower.String() + " Z"
	fmt.Fprintf(buf, `<path d="%s" fill="%s" stroke="none" pointer-events="none"/>`, path, fill)
}

func writeSVGAggregate(buf *bytes.Buffer, agg []TimeSeriesPoint, plotX, plotY, plotW, plotH int, maxElapsed int64, minVal, maxVal float64, stroke, source, unit string, chart ChartOptions) {
	if len(agg) == 0 {
		return
	}
	var b strings.Builder
	for i, p := range agg {
		x := svgX(plotX, plotW, p.ElapsedMilliseconds, maxElapsed)
		y := svgY(plotY, plotH, p.Center, minVal, maxVal)
		if i == 0 {
			fmt.Fprintf(&b, "M%d,%d", x, y)
		} else {
			fmt.Fprintf(&b, " L%d,%d", x, y)
		}
	}
	if len(agg) >= 2 {
		fmt.Fprintf(buf, `<path d="%s" fill="none" stroke="%s" stroke-width="2.5" pointer-events="none"/>`, b.String(), stroke)
	}
	band := fmt.Sprintf("p%.0f–p%.0f", chart.PercentileBandLow, chart.PercentileBandHigh)
	for _, p := range agg {
		x := svgX(plotX, plotW, p.ElapsedMilliseconds, maxElapsed)
		y := svgY(plotY, plotH, p.Center, minVal, maxVal)
		title := fmt.Sprintf("%s aggregate: elapsed=%s min %s=%s %s=%s–%s %s",
			source, formatElapsedMinutes(p.ElapsedMilliseconds), chart.Center, formatStat(p.Center), band,
			formatStat(p.Lower), formatStat(p.Upper), unit)
		fmt.Fprintf(buf, `<circle class="chart-point chart-point--aggregate" cx="%d" cy="%d" r="5" fill="%s" tabindex="0" data-source="%s" data-kind="aggregate" data-elapsed-ms="%d" data-center-name="%s" data-center="%s" data-lower="%s" data-upper="%s" data-band="%s" data-unit="%s" aria-label="%s"></circle>`,
			x, y, stroke, html.EscapeString(source), p.ElapsedMilliseconds,
			html.EscapeString(string(chart.Center)), html.EscapeString(formatStat(p.Center)),
			html.EscapeString(formatStat(p.Lower)), html.EscapeString(formatStat(p.Upper)),
			html.EscapeString(band), html.EscapeString(unit), html.EscapeString(title))
	}
}

func writeSVGLegend(buf *bytes.Buffer, x, y int, label, stroke string) {
	fmt.Fprintf(buf, `<g><line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="2.5"/><text x="%d" y="%d" font-size="11" fill="%s">%s</text></g>`,
		x, y-3, x+16, y-3, stroke, x+21, y, stroke, html.EscapeString(label))
}

func svgX(plotX, plotW int, elapsed, maxElapsed int64) int {
	return plotX + int(float64(plotW)*float64(elapsed)/float64(maxElapsed))
}

func svgY(plotY, plotH int, v, minVal, maxVal float64) int {
	if maxVal <= minVal {
		return plotY + plotH/2
	}
	return plotY + plotH - int(float64(plotH)*(v-minVal)/(maxVal-minVal))
}

func formatElapsedMinutes(elapsedMilliseconds int64) string {
	return fmt.Sprintf("%.2f", float64(elapsedMilliseconds)/60000)
}
