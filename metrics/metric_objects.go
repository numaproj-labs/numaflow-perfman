package metrics

import (
	"fmt"
	"time"

	"github.com/numaproj-labs/numaflow-perfman/util"
)

const (
	minutes = int(util.RateInterval / time.Minute)
	seconds = int((util.RateInterval % time.Minute) / time.Second)
)

const (
	LabelVertexName         = "vertex"
	LabelVertexType         = "vertex_type"
	LabelPipelineName       = "pipeline"
	LabelVertexReplicaIndex = "replica"
	LabelPodName            = "pod"
)

// MetricObject is the unit used for processing metrics, and is part of a metric group
type MetricObject struct {
	Query    string   // the PromQL query used in call to Prometheus API
	Filename string   // the name of the CSV file that will be produced for this metric
	XAxis    string   // The column name in the CSV file for the independent variable
	YAxis    string   // the column name in the CSV file for the dependent variable
	Labels   []string // the labels supported by the metric, used for the remaining columns of the CSV file
}

// TODO: Configure Query strings to pass in pipeline dynamically in order to support customized pipelines

// Throughput metrics
var InboundMessages = MetricObject{
	Query:    fmt.Sprintf(`rate(forwarder_read_total{pipeline="perfman-base-pipeline"}[%dm%ds])`, minutes, seconds),
	Filename: "inbound-messages",
	XAxis:    "unix_timestamp",
	YAxis:    "number_of_messages_per_second",
	Labels:   []string{LabelVertexName, LabelVertexType, LabelPipelineName, LabelVertexReplicaIndex},
}

// Latency metrics
var ForwarderProcessingTimeP90 = MetricObject{
	Query:    fmt.Sprintf(`histogram_quantile(0.9, rate(forwarder_forward_chunk_processing_time_bucket{pipeline="perfman-base-pipeline"}[%dm%ds])) / 1000000`, minutes, seconds),
	Filename: "forwarder-e2e-batch-processing-time-p90",
	XAxis:    "unix_timestamp",
	YAxis:    "seconds",
	Labels:   []string{LabelVertexName, LabelVertexType, LabelPipelineName, LabelVertexReplicaIndex},
}

var ForwarderProcessingTimeP95 = MetricObject{
	Query:    fmt.Sprintf(`histogram_quantile(0.95, rate(forwarder_forward_chunk_processing_time_bucket{pipeline="perfman-base-pipeline"}[%dm%ds])) / 1000000`, minutes, seconds),
	Filename: "forwarder-e2e-batch-processing-time-p95",
	XAxis:    "unix_timestamp",
	YAxis:    "seconds",
	Labels:   []string{LabelVertexName, LabelVertexType, LabelPipelineName, LabelVertexReplicaIndex},
}

var ForwarderProcessingTimeP99 = MetricObject{
	Query:    fmt.Sprintf(`histogram_quantile(0.99, rate(forwarder_forward_chunk_processing_time_bucket{pipeline="perfman-base-pipeline"}[%dm%ds])) / 1000000`, minutes, seconds),
	Filename: "forwarder-e2e-batch-processing-time-p99",
	XAxis:    "unix_timestamp",
	YAxis:    "seconds",
	Labels:   []string{LabelVertexName, LabelVertexType, LabelPipelineName, LabelVertexReplicaIndex},
}

// Resource metrics
var Memory = MetricObject{
	Query:    `sum(container_memory_usage_bytes{pod=~"perfman-base-pipeline.*", pod!~"perfman-base-pipeline.*-(daemon).*"} / 1024 / 1024) by (pod)`,
	Filename: "memory-usage",
	XAxis:    "unix_timestamp",
	YAxis:    "bytes",
	Labels:   []string{LabelPodName},
}

var CPU = MetricObject{
	Query:    fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{pod=~"perfman-base-pipeline.*", pod!~"perfman-base-pipeline.*-(daemon).*"}[%dm%ds]) * 1000) by (pod)`, minutes, seconds),
	Filename: "cpu-usage",
	XAxis:    "unix_timestamp",
	YAxis:    "milliCPUs",
	Labels:   []string{LabelPodName},
}
