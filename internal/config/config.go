package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LogFormat is the structured log encoding.
type LogFormat string

const (
	LogFormatText LogFormat = "text"
	LogFormatJSON LogFormat = "json"
)

// DefaultNumaflowImage is the central Numaflow server image used when no image is configured.
const DefaultNumaflowImage = "quay.io/numaproj/numaflow:v1.8.2"

// Config holds global perfman settings resolved from stored config, env, and flags.
type Config struct {
	Context             string    `json:"context"`
	Cluster             string    `json:"cluster"`
	ResultsDB           string    `json:"results_db"`
	PrometheusURL       string    `json:"prometheus_url"`
	CentralNamespace    string    `json:"central_namespace"`
	MonitoringNamespace string    `json:"monitoring_namespace"`
	ValidationNamespace string    `json:"validation_namespace"`
	LogFormat           LogFormat `json:"log_format"`
	Verbose             bool      `json:"verbose"`
	UDFImage            string    `json:"udf_image"`
	Image               string    `json:"image"`
	// TestedNumaflowMajorMinor documents the major.minor tag exercised in repo docs (guidance only).
	TestedNumaflowMajorMinor string `json:"tested_numaflow_major_minor"`
}

// StoredConfig is harness configuration persisted in SQLite (excluding ResultsDB).
type StoredConfig struct {
	Context                  string
	Cluster                  string
	PrometheusURL            string
	CentralNamespace         string
	MonitoringNamespace      string
	ValidationNamespace      string
	LogFormat                LogFormat
	Verbose                  bool
	UDFImage                 string
	Image                    string
	TestedNumaflowMajorMinor string
}

// Defaults returns documented baseline configuration before stored config/env/flags.
func Defaults() Config {
	return Config{
		Cluster:                  "numaflow",
		ResultsDB:                "./results/perfman.db",
		PrometheusURL:            "",
		CentralNamespace:         "numaflow-system",
		MonitoringNamespace:      "monitoring",
		ValidationNamespace:      "validation-system",
		LogFormat:                LogFormatText,
		Verbose:                  false,
		Image:                    DefaultNumaflowImage,
		TestedNumaflowMajorMinor: "1.8",
	}
}

// ConfigFromStored builds Config from a database row. Empty strings are preserved as explicit values.
func ConfigFromStored(s StoredConfig) Config {
	return Config{
		Context:                  s.Context,
		Cluster:                  s.Cluster,
		PrometheusURL:            s.PrometheusURL,
		CentralNamespace:         s.CentralNamespace,
		MonitoringNamespace:      s.MonitoringNamespace,
		ValidationNamespace:      s.ValidationNamespace,
		LogFormat:                s.LogFormat,
		Verbose:                  s.Verbose,
		UDFImage:                 s.UDFImage,
		Image:                    s.Image,
		TestedNumaflowMajorMinor: s.TestedNumaflowMajorMinor,
	}
}

// StoredConfig extracts fields persisted in harness_config (excludes ResultsDB).
func (c Config) StoredConfig() StoredConfig {
	return StoredConfig{
		Context:                  c.Context,
		Cluster:                  c.Cluster,
		PrometheusURL:            c.PrometheusURL,
		CentralNamespace:         c.CentralNamespace,
		MonitoringNamespace:      c.MonitoringNamespace,
		ValidationNamespace:      c.ValidationNamespace,
		LogFormat:                c.LogFormat,
		Verbose:                  c.Verbose,
		UDFImage:                 c.UDFImage,
		Image:                    c.Image,
		TestedNumaflowMajorMinor: c.TestedNumaflowMajorMinor,
	}
}

// EnvPrefix is prepended to environment variable names (e.g. PERFHARNESS_CLUSTER).
const EnvPrefix = "PERFHARNESS_"

func envKey(field string) string {
	return EnvPrefix + strings.ToUpper(strings.ReplaceAll(field, "_", "_"))
}

// ApplyEnv overlays recognized environment variables onto cfg.
func ApplyEnv(cfg *Config) {
	if v := os.Getenv(envKey("CONTEXT")); v != "" {
		cfg.Context = v
	}
	if v := os.Getenv(envKey("CLUSTER")); v != "" {
		cfg.Cluster = v
	}
	if v := os.Getenv(envKey("RESULTS_DB")); v != "" {
		cfg.ResultsDB = v
	}
	if v := os.Getenv(envKey("PROMETHEUS_URL")); v != "" {
		cfg.PrometheusURL = v
	}
	if v := os.Getenv(envKey("CENTRAL_NAMESPACE")); v != "" {
		cfg.CentralNamespace = v
	}
	if v := os.Getenv(envKey("MONITORING_NAMESPACE")); v != "" {
		cfg.MonitoringNamespace = v
	}
	if v := os.Getenv(envKey("VALIDATION_NAMESPACE")); v != "" {
		cfg.ValidationNamespace = v
	}
	if v := os.Getenv(envKey("LOG_FORMAT")); v != "" {
		cfg.LogFormat = LogFormat(v)
	}
	if v := os.Getenv(envKey("VERBOSE")); v != "" {
		cfg.Verbose = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv(envKey("UDF_IMAGE")); v != "" {
		cfg.UDFImage = v
	}
	if v := os.Getenv(envKey("IMAGE")); v != "" {
		cfg.Image = v
	} else if v := os.Getenv("IMAGE"); v != "" {
		cfg.Image = v
	}
	if v := os.Getenv(envKey("TESTED_NUMAFLOW_MAJOR_MINOR")); v != "" {
		cfg.TestedNumaflowMajorMinor = v
	}
}

// Overrides carries optional command-line values. Empty string or nil pointer means no override.
type Overrides struct {
	Context             *string
	Cluster             *string
	ResultsDB           *string
	PrometheusURL       *string
	CentralNamespace    *string
	MonitoringNamespace *string
	ValidationNamespace *string
	LogFormat           *LogFormat
	Verbose             *bool
	UDFImage            *string
	Image               *string
}

// ResolveResultsDB determines the SQLite database path from defaults, environment, and flag overrides.
// Call this before opening the results database.
func ResolveResultsDB(overrides Overrides) (string, error) {
	db := Defaults().ResultsDB
	if v := os.Getenv(envKey("RESULTS_DB")); v != "" {
		db = v
	}
	if overrides.ResultsDB != nil {
		db = *overrides.ResultsDB
	}
	if db == "" {
		return "", errors.New("results-db path must not be empty")
	}
	return db, nil
}

// Resolve merges stored configuration, environment, and flag overrides in documented priority.
// resultsDB is taken from ResolveResultsDB and is not overridden by stored values.
func Resolve(base Config, resultsDB string, overrides Overrides) (Config, error) {
	cfg := base
	ApplyEnv(&cfg)
	applyOverrides(&cfg, overrides)
	cfg.ResultsDB = resultsDB
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func applyOverrides(cfg *Config, o Overrides) {
	if o.Context != nil {
		cfg.Context = *o.Context
	}
	if o.Cluster != nil {
		cfg.Cluster = *o.Cluster
	}
	if o.ResultsDB != nil {
		cfg.ResultsDB = *o.ResultsDB
	}
	if o.PrometheusURL != nil {
		cfg.PrometheusURL = *o.PrometheusURL
	}
	if o.CentralNamespace != nil {
		cfg.CentralNamespace = *o.CentralNamespace
	}
	if o.MonitoringNamespace != nil {
		cfg.MonitoringNamespace = *o.MonitoringNamespace
	}
	if o.ValidationNamespace != nil {
		cfg.ValidationNamespace = *o.ValidationNamespace
	}
	if o.LogFormat != nil {
		cfg.LogFormat = *o.LogFormat
	}
	if o.Verbose != nil {
		cfg.Verbose = *o.Verbose
	}
	if o.UDFImage != nil {
		cfg.UDFImage = *o.UDFImage
	}
	if o.Image != nil {
		cfg.Image = *o.Image
	}
}

var (
	errInvalidLogFormat = errors.New("log-format must be text or json")
	errEmptyCluster     = errors.New("cluster name must not be empty")
	errEmptyNamespace   = errors.New("namespace names must not be empty")
)

// Validate checks resolved configuration.
func (c Config) Validate() error {
	if c.Cluster == "" {
		return errEmptyCluster
	}
	if c.CentralNamespace == "" || c.MonitoringNamespace == "" || c.ValidationNamespace == "" {
		return errEmptyNamespace
	}
	switch c.LogFormat {
	case LogFormatText, LogFormatJSON:
	default:
		return errInvalidLogFormat
	}
	if c.ResultsDB == "" {
		return errors.New("results-db path must not be empty")
	}
	return nil
}

// AbsResultsDB returns an absolute path to the SQLite database file.
func (c Config) AbsResultsDB() (string, error) {
	return filepath.Abs(c.ResultsDB)
}

// EnsureResultsLayout creates the parent directory for the results database file.
func (c Config) EnsureResultsLayout() error {
	dbPath, err := c.AbsResultsDB()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("create results db directory: %w", err)
	}
	return nil
}

// BenchmarkRunOptions are common benchmark CLI settings (not part of global config by default).
type BenchmarkRunOptions struct {
	Scenario string
	Image    string
	Duration time.Duration
}

// ValidationRunOptions are validation CLI settings.
type ValidationRunOptions struct {
	Scenario      string
	Image         string
	Events        int
	Tier          string
	Seed          int64
	BaseEventTime time.Time
	Timeout       time.Duration
	DumpFailureDB bool
}
