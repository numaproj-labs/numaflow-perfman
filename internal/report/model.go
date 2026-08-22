package report

import (
	"encoding/json"
	"fmt"
	"time"

	"numa-perfman/internal/results"
)

// Group identifies baseline or candidate runs in a comparison.
type Group string

const (
	GroupBaseline  Group = "baseline"
	GroupCandidate Group = "candidate"
	GroupResult    Group = "result"
)

// Selection chooses runs for a benchmark comparison report.
type Selection struct {
	Scenario string

	// RunIDs applies the side selectors below to an explicit set of runs.
	RunIDs []string
	// BaselineRunIDs and CandidateRunIDs select explicit runs without requiring
	// image or digest selectors.
	BaselineRunIDs  []string
	CandidateRunIDs []string

	BaselineImageRef     string
	CandidateImageRef    string
	BaselineImageDigest  string
	CandidateImageDigest string

	// LatestComplete selects the N most recent completed benchmark runs per side when RunIDs is empty.
	BaselineLatestComplete  int
	CandidateLatestComplete int

	After  *time.Time
	Before *time.Time

	Chart ChartOptions
}

// SelectedRun is one run included or excluded from comparison with a reason.
type SelectedRun struct {
	Run    results.Run
	Group  Group
	Series []results.LoadedMetricSeries
}

// ExcludedRun records a run omitted from comparison.
type ExcludedRun struct {
	Run    results.Run
	Group  Group
	Reason string
}

// SelectionResult is the output of run selection.
type SelectionResult struct {
	Scenario        string
	Baseline        []SelectedRun
	Candidate       []SelectedRun
	Excluded        []ExcludedRun
	ReferenceConfig RunConfigFingerprint
}

// RunConfigFingerprint summarizes benchmark config from a reference run for the report.
type RunConfigFingerprint struct {
	Scenario     string
	ManifestHash string
	Duration     string
	Resources    string
}

// MetricStats holds descriptive statistics for one scalar sample set.
type MetricStats struct {
	Count  int     `json:"count"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	StdDev float64 `json:"std_dev"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	P50    float64 `json:"p50"`
	P95    float64 `json:"p95"`
	P99    float64 `json:"p99"`
}

// MetricComparison compares baseline and candidate summary stats for one metric.
type MetricComparison struct {
	MetricName     string      `json:"metric_name"`
	Vertex         string      `json:"vertex,omitempty"`
	Unit           string      `json:"unit"`
	Baseline       MetricStats `json:"baseline"`
	Candidate      MetricStats `json:"candidate"`
	AbsoluteChange float64     `json:"absolute_change"`
	PercentChange  float64     `json:"percent_change"`
}

// TimeSeriesPoint is one aggregated sample on the elapsed-time axis.
type TimeSeriesPoint struct {
	ElapsedMilliseconds int64   `json:"elapsed_ms"`
	Center              float64 `json:"center"`
	Lower               float64 `json:"lower"`
	Upper               float64 `json:"upper"`
}

// ExcludedRunSummary is a run omitted from comparison (included in summary JSON).
type ExcludedRunSummary struct {
	RunID    string `json:"run_id"`
	Group    Group  `json:"group,omitempty"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	ImageRef string `json:"image_ref,omitempty"`
}

// RepetitionSeries is one run's metric samples aligned by elapsed time.
type RepetitionSeries struct {
	RunID  string                `json:"run_id"`
	Points []results.MetricPoint `json:"points"`
}

// AggregatedSeries combines repetitions for charting.
type AggregatedSeries struct {
	MetricName  string             `json:"metric_name"`
	Vertex      string             `json:"vertex,omitempty"`
	Unit        string             `json:"unit"`
	Group       Group              `json:"group"`
	Repetitions []RepetitionSeries `json:"repetitions"`
	Aggregate   []TimeSeriesPoint  `json:"aggregate"`
}

// Report is the in-memory comparison report model.
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Scenario    string    `json:"scenario"`

	BaselineImageRef     string `json:"baseline_image_ref"`
	CandidateImageRef    string `json:"candidate_image_ref"`
	BaselineImageDigest  string `json:"baseline_image_digest"`
	CandidateImageDigest string `json:"candidate_image_digest"`

	BaselineCompleteCount    int `json:"baseline_complete_count"`
	CandidateCompleteCount   int `json:"candidate_complete_count"`
	BaselineIncompleteCount  int `json:"baseline_incomplete_count"`
	CandidateIncompleteCount int `json:"candidate_incomplete_count"`
	ExcludedCount            int `json:"excluded_count"`

	Warnings []string             `json:"warnings,omitempty"`
	Excluded []ExcludedRunSummary `json:"excluded_runs,omitempty"`

	Chart ChartOptions `json:"chart"`

	Config RunConfigFingerprint `json:"config"`

	MetricComparisons []MetricComparison `json:"metric_comparisons"`
	Series            []AggregatedSeries `json:"series"`
}

// SingleSelection chooses one completed benchmark run for a result report.
type SingleSelection struct {
	Scenario string
	RunID    string

	ImageRef    string
	ImageDigest string

	After  *time.Time
	Before *time.Time
}

// SingleReport is the in-memory model for one completed benchmark result.
type SingleReport struct {
	GeneratedAt time.Time `json:"generated_at"`
	Scenario    string    `json:"scenario"`

	RunID       string               `json:"run_id"`
	ImageRef    string               `json:"image_ref"`
	ImageDigest string               `json:"image_digest,omitempty"`
	CompletedAt *time.Time           `json:"completed_at,omitempty"`
	Config      RunConfigFingerprint `json:"config"`

	Metrics []MetricValues `json:"metrics"`
}

// MetricValue is an observed metric value at one timestamp.
type MetricValue struct {
	Timestamp           time.Time `json:"timestamp"`
	ElapsedMilliseconds int64     `json:"elapsed_ms"`
	Value               float64   `json:"value"`
}

// MetricValues stores the unaggregated observations for one metric series.
type MetricValues struct {
	MetricName  string        `json:"metric_name"`
	DisplayName string        `json:"display_name,omitempty"`
	Unit        string        `json:"unit"`
	LabelsJSON  string        `json:"labels_json,omitempty"`
	Values      []MetricValue `json:"values"`
}

// ParseRunConfig extracts compatibility fields from runs.config_json.
func ParseRunConfig(configJSON string) (RunConfigFingerprint, error) {
	var raw map[string]json.RawMessage
	if configJSON == "" {
		configJSON = "{}"
	}
	if err := json.Unmarshal([]byte(configJSON), &raw); err != nil {
		return RunConfigFingerprint{}, fmt.Errorf("parse config_json: %w", err)
	}
	fp := RunConfigFingerprint{}
	fp.Duration = stringField(raw, "duration")
	if b, ok := raw["resources"]; ok {
		fp.Resources = string(b)
	}
	return fp, nil
}

func stringField(raw map[string]json.RawMessage, key string) string {
	if b, ok := raw[key]; ok {
		var s string
		if err := json.Unmarshal(b, &s); err == nil {
			return s
		}
		return string(b)
	}
	return ""
}
