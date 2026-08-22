package report

import "math"

func plotBounds(baseline, candidate AggregatedSeries) (maxElapsed int64, minVal, maxVal float64) {
	minVal = math.MaxFloat64
	update := func(pts []TimeSeriesPoint) {
		for _, p := range pts {
			if p.ElapsedMilliseconds > maxElapsed {
				maxElapsed = p.ElapsedMilliseconds
			}
			for _, v := range []float64{p.Center, p.Lower, p.Upper} {
				if v < minVal {
					minVal = v
				}
				if v > maxVal {
					maxVal = v
				}
			}
		}
	}
	for _, repetition := range append(baseline.Repetitions, candidate.Repetitions...) {
		for _, p := range repetition.Points {
			if p.ElapsedMilliseconds > maxElapsed {
				maxElapsed = p.ElapsedMilliseconds
			}
			if p.Value < minVal {
				minVal = p.Value
			}
			if p.Value > maxVal {
				maxVal = p.Value
			}
		}
	}
	update(baseline.Aggregate)
	update(candidate.Aggregate)
	if minVal == math.MaxFloat64 {
		minVal, maxVal = 0, 1
	}
	return maxElapsed, minVal, maxVal
}
