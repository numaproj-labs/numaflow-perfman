package util

import "time"

// TODO: use viper for configuration management

// Namespace config
const (
	PerfmanNamespace  = "default"
	NumaflowNamespace = "numaflow-system"
)

// Grafana and Prometheus config
const (
	GrafanaPassword         = "admin"
	GrafanaURL              = "http://localhost:3000"
	PrometheusURL           = "http://localhost:9090"
	GrafanaReleaseName      = "perfman-grafana"
	PrometheusReleaseName   = "perfman-kube-prometheus"
	PrometheusPFServiceName = "perfman-kube-prometheus-prometheus" // Prometheus service name to use for port forwarding
	GrafanaPFServiceName    = "perfman-grafana"                    // Grafana service name to use for port forwarding

	// RateInterval is used to determine over what period the rate function is computed.
	// It should typically be at least 4-5 times the scrape interval,
	// which is the amount of time between each "scrape" of data from the monitored targets.
	// The scrape interval can be adjusted in the yaml file for the service monitor using the `interval` key.
	// If the 'interval' key is modified make sure to adjust RateInterval accordingly
	RateInterval = 1 * time.Minute

	// Step is used when querying data, and should be tuned based on the granularity of data you want when querying.
	// It determines the time duration between two returned data points in the response, and
	// should always be equal to or larger than the scrape interval set in the service
	// monitor yaml, to prevent asking for data at a higher resolution than you have
	Step = 15 * time.Second
)
