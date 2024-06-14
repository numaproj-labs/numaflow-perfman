package util

import "time"

// TODO: use viper for configuration management
const (
	// Namespace config
	PerfmanNamespace  = "default"
	NumaflowNamespace = "numaflow-system"

	// Grafana and Prometheus config
	GrafanaPassword         = "admin"
	GrafanaURL              = "http://localhost:3000"
	PrometheusURL           = "http://localhost:9090"
	GrafanaReleaseName      = "perfman-grafana"
	PrometheusReleaseName   = "perfman-kube-prometheus"
	PrometheusPFServiceName = "perfman-kube-prometheus-prometheus" // Prometheus service name to use for port forwarding
	GrafanaPFServiceName    = "perfman-grafana"                    // Grafana service name to use for port forwarding

	// Promethues HTTP API config
	Step         = 15 * time.Second // defines how often a new value is produced
	RateInterval = Step * 4         // used to determine over what period the rate function is computed
)
