package prometheus_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"numa-perfman/internal/prometheus"
	"numa-perfman/internal/scenario"
)

func TestParseQueryRangeResponse(t *testing.T) {
	body := []byte(`{"status":"success","data":{"resultType":"matrix","result":[{"values":[[1700000000,"1.5"],[1700000010,"2"]]}]}}`)
	samples, err := prometheus.ParseQueryRangeResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || samples[0].Value != 1.5 {
		t.Fatalf("samples: %+v", samples)
	}
}

func TestParseQueryRangeResponseRejectsMultipleSeries(t *testing.T) {
	body := []byte(`{"status":"success","data":{"resultType":"matrix","result":[{"values":[[1700000000,"1.5"]]},{"values":[[1700000000,"2.5"]]}]}}`)
	if _, err := prometheus.ParseQueryRangeResponse(body); err == nil {
		t.Fatal("expected multiple unaggregated series to be rejected")
	}
}

func TestParseQueryRangeResponseLabeled(t *testing.T) {
	body := []byte(`{"status":"success","data":{"resultType":"matrix","result":[
		{"metric":{"vertex":"in"},"values":[[1700000000,"1.5"]]},
		{"metric":{"vertex":"map"},"values":[[1700000000,"2.5"]]}
	]}}`)
	series, err := prometheus.ParseQueryRangeResponseLabeled(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 || series[0].Labels["vertex"] != "in" || series[1].Samples[0].Value != 2.5 {
		t.Fatalf("series: %#v", series)
	}
}

func TestCollectScenarioMetricsEnforcement(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"values":[[1,"1"]]}]}}`))
	}))
	defer srv.Close()

	client := prometheus.Client{BaseURL: srv.URL}
	defs := []scenario.MetricDefinition{
		{Name: "m1", Required: true, MinSamples: 3, Unit: scenario.UnitCores, Query: `up{namespace="$namespace"}`},
		{Name: "m2", Required: false, MinSamples: 1, Unit: scenario.UnitCores, Query: `up{namespace="$namespace"}`},
	}
	_, err := client.CollectScenarioMetrics(context.Background(), defs, "ns1", time.Unix(0, 0), time.Unix(60, 0), 10*time.Second)
	if err == nil {
		t.Fatal("expected required metric sample enforcement error")
	}
}

func TestPortForwardURL(t *testing.T) {
	if prometheus.PortForwardURL(9090) != "http://127.0.0.1:9090" {
		t.Fatal("unexpected url")
	}
}

func TestNormalizeForStorage(t *testing.T) {
	v, err := prometheus.NormalizeForStorage(scenario.UnitCores, 500, "millicores")
	if err != nil || v != 0.5 {
		t.Fatalf("got %v %v", v, err)
	}
	v, err = prometheus.NormalizeForStorage(scenario.UnitMilliseconds, 535652.3698793, "microseconds")
	if err != nil || v != 535.6523698793 {
		t.Fatalf("microseconds to milliseconds: got %v %v", v, err)
	}
}

func TestToCanonicalUnitMilliseconds(t *testing.T) {
	samples, err := prometheus.ToCanonicalUnit(scenario.UnitMilliseconds, []prometheus.Sample{{Value: 535652.3698793}})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || samples[0].Value != 535.6523698793 {
		t.Fatalf("samples: %#v", samples)
	}
}
