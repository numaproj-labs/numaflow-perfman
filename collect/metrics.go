package collect

import (
	"fmt"
	"time"

	"github.com/numaproj-labs/numaflow-perfman/util"
)

const (
	minutes = int(util.RateInterval / time.Minute)
	seconds = int((util.RateInterval % time.Minute) / time.Second)
)

type MetricObject struct {
	Query string
	Title string
	XAxis string
	YAxis string
}

// Data forward metrics
var InboundMessages = MetricObject{
	Query: fmt.Sprintf("rate(forwarder_read_total{pipeline=\"perfman-base-pipeline\"}[%dm%ds])", minutes, seconds),
	Title: "Inbound Messages (TPS)",
	XAxis: "Unix Timestamp",
	YAxis: "Number of Messages Per Second",
}

// Latency metrics
var ForwarderE2EP90 = MetricObject{
	Query: fmt.Sprintf("histogram_quantile(0.9, rate(forwarder_forward_chunk_processing_time_bucket{pipeline=\"perfman-base-pipeline\"}[%dm%ds])) / 1000000", minutes, seconds),
	Title: "Forwarder E2E - Batch Processing Time (P90)",
	XAxis: "Unix Timestamp",
	YAxis: "Seconds",
}

var ForwarderE2EP95 = MetricObject{
	Query: fmt.Sprintf("histogram_quantile(0.95, rate(forwarder_forward_chunk_processing_time_bucket{pipeline=\"perfman-base-pipeline\"}[%dm%ds])) / 1000000", minutes, seconds),
	Title: "Forwarder E2E - Batch Processing Time (P95)",
	XAxis: "Unix Timestamp",
	YAxis: "Seconds",
}

var ForwarderE2EP99 = MetricObject{
	Query: fmt.Sprintf("histogram_quantile(0.99, rate(forwarder_forward_chunk_processing_time_bucket{pipeline=\"perfman-base-pipeline\"}[%dm%ds])) / 1000000", minutes, seconds),
	Title: "Forwarder E2E - Batch Processing Time (P99)",
	XAxis: "Unix Timestamp",
	YAxis: "Seconds",
}
