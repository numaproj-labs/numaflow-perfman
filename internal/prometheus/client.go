package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"numa-perfman/internal/scenario"
)

// Sample is a single time-series point in canonical units.
type Sample struct {
	Timestamp time.Time
	Value     float64
}

// SeriesResult is a parsed query result for one metric definition.
type SeriesResult struct {
	Metric  scenario.MetricDefinition
	Labels  map[string]string
	Samples []Sample
	Warning string
}

// LabeledSamples is one Prometheus matrix series with its label set.
type LabeledSamples struct {
	Labels  map[string]string
	Samples []Sample
}

// Client queries Prometheus HTTP API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// QueryRange executes a Prometheus range query and parses a single aggregated series.
func (c Client) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]Sample, error) {
	series, err := c.QueryRangeLabeled(ctx, query, start, end, step)
	if err != nil {
		return nil, err
	}
	if len(series) != 1 {
		return nil, fmt.Errorf("expected one aggregated prometheus series, got %d", len(series))
	}
	return series[0].Samples, nil
}

// QueryRangeLabeled executes a Prometheus range query and parses all matrix series.
func (c Client) QueryRangeLabeled(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]LabeledSamples, error) {
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = "/api/v1/query_range"
	q := u.Query()
	q.Set("query", query)
	q.Set("start", fmt.Sprintf("%f", float64(start.UnixNano())/1e9))
	q.Set("end", fmt.Sprintf("%f", float64(end.UnixNano())/1e9))
	q.Set("step", step.String())
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus query failed: %s: %s", resp.Status, string(body))
	}
	return ParseQueryRangeResponseLabeled(body)
}

type apiResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][]any           `json:"values"`
		} `json:"result"`
	} `json:"data"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
}

// ParseQueryRangeResponse parses Prometheus /api/v1/query_range JSON into one series.
func ParseQueryRangeResponse(body []byte) ([]Sample, error) {
	series, err := ParseQueryRangeResponseLabeled(body)
	if err != nil {
		return nil, err
	}
	if len(series) != 1 {
		return nil, fmt.Errorf("expected one aggregated prometheus series, got %d", len(series))
	}
	return series[0].Samples, nil
}

// ParseQueryRangeResponseLabeled parses Prometheus /api/v1/query_range JSON.
func ParseQueryRangeResponseLabeled(body []byte) ([]LabeledSamples, error) {
	var parsed apiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode prometheus response: %w", err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("prometheus error: %s %s", parsed.ErrorType, parsed.Error)
	}
	if parsed.Data.ResultType != "matrix" {
		return nil, errors.New("expected matrix result type")
	}
	if len(parsed.Data.Result) == 0 {
		return nil, errors.New("empty prometheus result")
	}
	var out []LabeledSamples
	for _, result := range parsed.Data.Result {
		var samples []Sample
		for _, pair := range result.Values {
			if len(pair) != 2 {
				continue
			}
			ts, err := parseTimestamp(pair[0])
			if err != nil {
				return nil, err
			}
			val, err := parseValue(pair[1])
			if err != nil {
				return nil, err
			}
			samples = append(samples, Sample{Timestamp: ts, Value: val})
		}
		out = append(out, LabeledSamples{Labels: result.Metric, Samples: samples})
	}
	return out, nil
}

func parseTimestamp(v any) (time.Time, error) {
	switch t := v.(type) {
	case float64:
		sec, frac := modf(t)
		return time.Unix(int64(sec), int64(frac*1e9)).UTC(), nil
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return time.Time{}, err
		}
		sec, frac := modf(f)
		return time.Unix(int64(sec), int64(frac*1e9)).UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("unexpected timestamp type %T", v)
	}
}

func parseValue(v any) (float64, error) {
	switch x := v.(type) {
	case string:
		return strconv.ParseFloat(x, 64)
	case float64:
		return x, nil
	case json.Number:
		return x.Float64()
	default:
		return 0, fmt.Errorf("unexpected value type %T", v)
	}
}

func modf(f float64) (float64, float64) {
	iv := float64(int64(f))
	return iv, f - iv
}

// CollectScenarioMetrics queries all definitions, enforcing required metrics and min samples.
func (c Client) CollectScenarioMetrics(ctx context.Context, defs []scenario.MetricDefinition, namespace string, start, end time.Time, step time.Duration) ([]SeriesResult, error) {
	var results []SeriesResult
	var errs []error
	for _, def := range defs {
		query := replaceNamespace(def.Query, namespace)
		if len(def.GroupBy) > 0 {
			labeled, err := c.QueryRangeLabeled(ctx, query, start, end, step)
			if err != nil {
				res := SeriesResult{Metric: def, Labels: groupLabels(nil, def.GroupBy)}
				if def.Required {
					errs = append(errs, fmt.Errorf("%s: %w", def.Name, err))
				} else {
					res.Warning = err.Error()
				}
				results = append(results, res)
				continue
			}
			for _, ls := range labeled {
				labels := groupLabels(ls.Labels, def.GroupBy)
				canon, canonErr := ToCanonicalUnit(def.Unit, ls.Samples)
				res := SeriesResult{Metric: def, Labels: labels, Samples: ls.Samples}
				if canonErr != nil {
					if def.Required {
						errs = append(errs, fmt.Errorf("%s: %w", def.Name, canonErr))
					} else {
						res.Warning = canonErr.Error()
					}
				} else {
					res.Samples = canon
				}
				results = append(results, res)
			}
			if def.Required && !hasSufficientGroupedSamples(def, results) {
				errs = append(errs, fmt.Errorf("%s: insufficient grouped samples", def.Name))
			}
			continue
		}

		samples, err := c.QueryRange(ctx, query, start, end, step)
		res := SeriesResult{Metric: def, Samples: samples}
		if err != nil {
			if def.Required {
				errs = append(errs, fmt.Errorf("%s: %w", def.Name, err))
			} else {
				res.Warning = err.Error()
			}
			results = append(results, res)
			continue
		}
		canon, err := ToCanonicalUnit(def.Unit, samples)
		if err != nil {
			if def.Required {
				errs = append(errs, fmt.Errorf("%s: %w", def.Name, err))
			} else {
				res.Warning = err.Error()
			}
		} else {
			res.Samples = canon
		}
		if def.Required && len(res.Samples) < def.MinSamples {
			errs = append(errs, fmt.Errorf("%s: insufficient samples (%d < %d)", def.Name, len(res.Samples), def.MinSamples))
		}
		results = append(results, res)
	}
	return results, errors.Join(errs...)
}

func groupLabels(source map[string]string, keys []string) map[string]string {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if source == nil {
			continue
		}
		if v := source[key]; v != "" {
			out[key] = v
		}
	}
	return out
}

func hasSufficientGroupedSamples(def scenario.MetricDefinition, results []SeriesResult) bool {
	for _, res := range results {
		if res.Metric.Name != def.Name {
			continue
		}
		if len(res.Samples) >= def.MinSamples {
			return true
		}
	}
	return false
}

func replaceNamespace(query, namespace string) string {
	return strings.ReplaceAll(query, "$namespace", namespace)
}

// ToCanonicalUnit converts sample values to scenario canonical units.
func ToCanonicalUnit(unit scenario.Unit, samples []Sample) ([]Sample, error) {
	out := make([]Sample, len(samples))
	copy(out, samples)
	switch unit {
	case scenario.UnitCores, scenario.UnitBytes, scenario.UnitSeconds, scenario.UnitEventsPerSec:
		return out, nil
	case scenario.UnitMilliseconds:
		for i := range out {
			out[i].Value /= 1000
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown unit %q", unit)
	}
}

// NormalizeForStorage applies canonical conversion rules (e.g. millicores -> cores).
func NormalizeForStorage(unit scenario.Unit, value float64, sourceUnit string) (float64, error) {
	switch unit {
	case scenario.UnitCores:
		if sourceUnit == "millicores" {
			return value / 1000.0, nil
		}
		return value, nil
	case scenario.UnitBytes:
		if sourceUnit == "mib" {
			return value * 1024 * 1024, nil
		}
		return value, nil
	case scenario.UnitSeconds:
		if sourceUnit == "milliseconds" {
			return value / 1000.0, nil
		}
		return value, nil
	case scenario.UnitMilliseconds:
		switch sourceUnit {
		case "seconds":
			return value * 1000, nil
		case "microseconds":
			return value / 1000, nil
		}
		return value, nil
	default:
		return value, nil
	}
}

// PortForwardURL builds a localhost Prometheus URL from a local port.
func PortForwardURL(localPort int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", localPort)
}
