package util

// TODO: use viper for configuration management, especially for password
const (
	// Namespaces
	PerfmanNamespace  = "default"
	NumaflowNamespace = "numaflow-system"

	// Service names to use for port forwarding
	PrometheusPFServiceName = "perfman-kube-prometheus-prometheus"
	GrafanaPFServiceName    = "perfman-grafana"

	GrafanaPassword = "admin"
)
