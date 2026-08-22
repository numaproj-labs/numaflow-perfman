package report

import (
	"math"
	"sort"

	"numa-perfman/internal/results"
)

// ComputeStats returns descriptive statistics for values (empty input yields zero values).
func ComputeStats(values []float64) MetricStats {
	if len(values) == 0 {
		return MetricStats{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	variance := 0.0
	for _, v := range sorted {
		d := v - mean
		variance += d * d
	}
	if len(sorted) > 1 {
		variance /= float64(len(sorted) - 1)
	}
	std := math.Sqrt(variance)

	return MetricStats{
		Count:  len(sorted),
		Mean:   mean,
		Median: percentile(sorted, 50),
		StdDev: std,
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
		P50:    percentile(sorted, 50),
		P95:    percentile(sorted, 95),
		P99:    percentile(sorted, 99),
	}
}

func metricAverageAndP99(values []MetricValue) (average, p99 float64) {
	if len(values) == 0 {
		return 0, 0
	}
	sorted := make([]float64, len(values))
	for i, value := range values {
		sorted[i] = value.Value
		average += value.Value
	}
	average /= float64(len(sorted))
	sort.Float64s(sorted)
	return average, percentile(sorted, 99)
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := (p / 100) * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// CompareStats computes candidate minus baseline and percent change relative to baseline mean.
func CompareStats(metricName, unit string, baseline, candidate MetricStats) MetricComparison {
	abs := candidate.Mean - baseline.Mean
	pct := 0.0
	if baseline.Mean != 0 {
		pct = (abs / baseline.Mean) * 100
	}
	return MetricComparison{
		MetricName:     metricName,
		Unit:           unit,
		Baseline:       baseline,
		Candidate:      candidate,
		AbsoluteChange: abs,
		PercentChange:  pct,
	}
}

// SeriesMeanValue uses the mean of all points in a series as a repetition summary scalar.
func SeriesMeanValue(series results.LoadedMetricSeries) float64 {
	if len(series.Points) == 0 {
		return 0
	}
	sum := 0.0
	for _, p := range series.Points {
		sum += p.Value
	}
	return sum / float64(len(series.Points))
}

// AggregateElapsed aligns repetitions by elapsed milliseconds and computes center and percentile band per bucket.
func AggregateElapsed(repetitions []RepetitionSeries, opts ChartOptions) []TimeSeriesPoint {
	if opts.Center == "" {
		opts = DefaultChartOptions()
	}
	if opts.PercentileBandLow == 0 && opts.PercentileBandHigh == 0 {
		opts.PercentileBandLow = 25
		opts.PercentileBandHigh = 75
	}

	buckets := map[int64][]float64{}
	for _, rep := range repetitions {
		for _, p := range rep.Points {
			buckets[p.ElapsedMilliseconds] = append(buckets[p.ElapsedMilliseconds], p.Value)
		}
	}
	if len(buckets) == 0 {
		return nil
	}
	keys := make([]int64, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	out := make([]TimeSeriesPoint, 0, len(keys))
	for _, k := range keys {
		vals := buckets[k]
		sorted := append([]float64(nil), vals...)
		sort.Float64s(sorted)
		center := percentile(sorted, 50)
		if opts.Center == CenterMean {
			sum := 0.0
			for _, v := range sorted {
				sum += v
			}
			center = sum / float64(len(sorted))
		}
		out = append(out, TimeSeriesPoint{
			ElapsedMilliseconds: k,
			Center:              center,
			Lower:               percentile(sorted, opts.PercentileBandLow),
			Upper:               percentile(sorted, opts.PercentileBandHigh),
		})
	}
	return out
}

// AggregateElapsedMean is deprecated; use AggregateElapsed with CenterMean.
func AggregateElapsedMean(repetitions []RepetitionSeries) []TimeSeriesPoint {
	return AggregateElapsed(repetitions, ChartOptions{
		Center: CenterMean, PercentileBandLow: 25, PercentileBandHigh: 75,
	})
}
