package report

import (
	"testing"

	"numa-perfman/internal/results"
)

func TestParseCenterStatistic(t *testing.T) {
	c, err := ParseCenterStatistic("mean")
	if err != nil || c != CenterMean {
		t.Fatalf("%v %q", err, c)
	}
	if _, err := ParseCenterStatistic("avg"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParsePercentileBand(t *testing.T) {
	lo, hi, err := ParsePercentileBand("10,90")
	if err != nil || lo != 10 || hi != 90 {
		t.Fatalf("%v %v %v", err, lo, hi)
	}
	if _, _, err := ParsePercentileBand("90,10"); err == nil {
		t.Fatal("expected low<high error")
	}
	if _, _, err := ParsePercentileBand("x,y"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParseOutputFormats(t *testing.T) {
	f, err := ParseOutputFormats("html,json")
	if err != nil {
		t.Fatal(err)
	}
	if !f["html"] || !f["json"] || f["png"] {
		t.Fatalf("%v", f)
	}
	if _, err := ParseOutputFormats("png"); err == nil {
		t.Fatal("expected PNG format to be rejected")
	}
	if _, err := ParseOutputFormats("pdf"); err == nil {
		t.Fatal("expected unknown format error")
	}
}

func TestAggregateElapsedCenter(t *testing.T) {
	reps := []RepetitionSeries{{
		RunID: "a",
		Points: []results.MetricPoint{
			{ElapsedMilliseconds: 0, Value: 10},
			{ElapsedMilliseconds: 1000, Value: 30},
		},
	}, {
		RunID: "b",
		Points: []results.MetricPoint{
			{ElapsedMilliseconds: 0, Value: 20},
			{ElapsedMilliseconds: 1000, Value: 40},
		},
	}}
	meanPts := AggregateElapsed(reps, ChartOptions{Center: CenterMean, PercentileBandLow: 25, PercentileBandHigh: 75})
	if len(meanPts) != 2 || meanPts[0].Center != 15 {
		t.Fatalf("mean center: %#v", meanPts)
	}
	medPts := AggregateElapsed(reps, ChartOptions{Center: CenterMedian, PercentileBandLow: 25, PercentileBandHigh: 75})
	if medPts[0].Center != 15 || medPts[0].Lower != 12.5 || medPts[0].Upper != 17.5 {
		t.Fatalf("median/band: %#v", medPts[0])
	}
}
