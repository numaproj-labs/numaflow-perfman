package config

import "flag"

// FlagValues holds backing storage for global flags before Resolve.
type FlagValues struct {
	Context             string
	Cluster             string
	ResultsDB           string
	PrometheusURL       string
	CentralNamespace    string
	MonitoringNamespace string
	ValidationNamespace string
	LogFormat           string
	Verbose             bool
	UDFImage            string
}

// RegisterGlobalFlags binds global CLI options to v.
func RegisterGlobalFlags(fs *flag.FlagSet, v *FlagValues) {
	fs.StringVar(&v.Context, "context", "", "Kubernetes context (current context when empty)")
	fs.StringVar(&v.Cluster, "cluster", "", "kind cluster name")
	fs.StringVar(&v.ResultsDB, "results-db", "", "SQLite results database path")
	fs.StringVar(&v.PrometheusURL, "prometheus-url", "", "Prometheus base URL (auto port-forward when empty)")
	fs.StringVar(&v.CentralNamespace, "central-namespace", "", "Central Numaflow server namespace")
	fs.StringVar(&v.MonitoringNamespace, "monitoring-namespace", "", "Prometheus namespace")
	fs.StringVar(&v.ValidationNamespace, "validation-namespace", "", "Validation Postgres namespace")
	fs.StringVar(&v.LogFormat, "log-format", "", "Log format: text or json")
	fs.BoolVar(&v.Verbose, "verbose", false, "Verbose logging")
	fs.StringVar(&v.UDFImage, "udf-image", "", "Repository UDF image reference")
}

// OverridesFromFlags builds Overrides from parsed FlagValues, honoring only flags explicitly set on the command line.
func OverridesFromFlags(fs *flag.FlagSet, v FlagValues) Overrides {
	var o Overrides
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "context":
			val := v.Context
			o.Context = &val
		case "cluster":
			val := v.Cluster
			o.Cluster = &val
		case "results-db":
			val := v.ResultsDB
			o.ResultsDB = &val
		case "prometheus-url":
			val := v.PrometheusURL
			o.PrometheusURL = &val
		case "central-namespace":
			val := v.CentralNamespace
			o.CentralNamespace = &val
		case "monitoring-namespace":
			val := v.MonitoringNamespace
			o.MonitoringNamespace = &val
		case "validation-namespace":
			val := v.ValidationNamespace
			o.ValidationNamespace = &val
		case "log-format":
			lf := LogFormat(v.LogFormat)
			o.LogFormat = &lf
		case "verbose":
			val := v.Verbose
			o.Verbose = &val
		case "udf-image":
			val := v.UDFImage
			o.UDFImage = &val
		}
	})
	return o
}

// ApplyFlagSet is an alias for OverridesFromFlags for callers that already hold FlagValues.
func ApplyFlagSet(fs *flag.FlagSet, v FlagValues) Overrides {
	return OverridesFromFlags(fs, v)
}
