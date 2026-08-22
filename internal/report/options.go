package report

import (
	"fmt"
	"strconv"
	"strings"
)

// CenterStatistic selects the aggregate center line per elapsed bucket.
type CenterStatistic string

const (
	CenterMean   CenterStatistic = "mean"
	CenterMedian CenterStatistic = "median"
)

// ChartOptions configures aggregation and rendering for comparison reports.
type ChartOptions struct {
	Center             CenterStatistic `json:"center"`
	PercentileBandLow  float64         `json:"percentile_band_lower"`
	PercentileBandHigh float64         `json:"percentile_band_upper"`
}

// DefaultChartOptions matches the CLI defaults.
func DefaultChartOptions() ChartOptions {
	return ChartOptions{
		Center:             CenterMedian,
		PercentileBandLow:  25,
		PercentileBandHigh: 75,
	}
}

func normalizedChartOptions(chart ChartOptions) ChartOptions {
	if chart.Center == "" {
		chart = DefaultChartOptions()
	}
	if chart.PercentileBandLow == 0 && chart.PercentileBandHigh == 0 {
		chart.PercentileBandLow = 25
		chart.PercentileBandHigh = 75
	}
	return chart
}

// ParseCenterStatistic validates a center line selector.
func ParseCenterStatistic(s string) (CenterStatistic, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", string(CenterMedian):
		return CenterMedian, nil
	case string(CenterMean):
		return CenterMean, nil
	default:
		return "", fmt.Errorf("center must be mean or median")
	}
}

// ParsePercentileBand parses "low,high" percentile bounds in [0,100] with low < high.
func ParsePercentileBand(s string) (low, high float64, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultChartOptions().PercentileBandLow, DefaultChartOptions().PercentileBandHigh, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("percentile-band must be two comma-separated values, e.g. 25,75")
	}
	low, err = parsePercentileValue(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("percentile-band lower: %w", err)
	}
	high, err = parsePercentileValue(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("percentile-band upper: %w", err)
	}
	if low >= high {
		return 0, 0, fmt.Errorf("percentile-band lower must be less than upper")
	}
	return low, high, nil
}

func parsePercentileValue(s string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", strings.TrimSpace(s))
	}
	if v < 0 || v > 100 {
		return 0, fmt.Errorf("percentile must be between 0 and 100")
	}
	return v, nil
}

// ParseOutputFormats returns a set of allowed report output formats.
func ParseOutputFormats(s string) (map[string]bool, error) {
	allowed := map[string]struct{}{
		"html": {}, "json": {},
	}
	out := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(strings.ToLower(p))
		if p == "" {
			continue
		}
		if _, ok := allowed[p]; !ok {
			return nil, fmt.Errorf("unknown format %q (allowed: html, json)", p)
		}
		out[p] = true
	}
	if len(out) == 0 {
		out["html"], out["json"] = true, true
	}
	return out, nil
}
