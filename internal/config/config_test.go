package config_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"numa-perfman/internal/config"
)

func storedConfig() config.StoredConfig {
	return config.StoredConfig{
		Cluster:                  "stored-cluster",
		CentralNamespace:         "numaflow-system",
		MonitoringNamespace:      "monitoring",
		ValidationNamespace:      "validation-system",
		Verbose:                  true,
		LogFormat:                config.LogFormatJSON,
		TestedNumaflowMajorMinor: "1.7",
		Image:                    config.DefaultNumaflowImage,
	}
}

func resolveWithStored(t *testing.T, stored config.StoredConfig, env map[string]string, args []string) config.Config {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var fv config.FlagValues
	config.RegisterGlobalFlags(fs, &fv)
	if len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
	}
	o := config.OverridesFromFlags(fs, fv)
	resultsDB, err := config.ResolveResultsDB(o)
	if err != nil {
		t.Fatal(err)
	}
	base := config.ConfigFromStored(stored)
	cfg, err := config.Resolve(base, resultsDB, o)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// precedenceCase verifies flag > env > stored > default for one field at a time.
func TestConfigPrecedenceMatrix(t *testing.T) {
	stored := storedConfig()

	type fieldCase struct {
		name      string
		env       map[string]string
		args      []string
		useStored bool
		want      func(config.Config) bool
		msg       string
	}
	cases := []fieldCase{
		{
			name: "cluster-default",
			want: func(c config.Config) bool { return c.Cluster == "numaflow" },
			msg:  "default cluster",
		},
		{
			name:      "cluster-stored",
			useStored: true,
			want:      func(c config.Config) bool { return c.Cluster == "stored-cluster" },
			msg:       "stored cluster",
		},
		{
			name:      "cluster-env",
			useStored: true,
			env:       map[string]string{config.EnvPrefix + "CLUSTER": "env-cluster"},
			want:      func(c config.Config) bool { return c.Cluster == "env-cluster" },
			msg:       "env beats stored",
		},
		{
			name:      "cluster-flag",
			useStored: true,
			env:       map[string]string{config.EnvPrefix + "CLUSTER": "env-cluster"},
			args:      []string{"--cluster", "flag-cluster"},
			want:      func(c config.Config) bool { return c.Cluster == "flag-cluster" },
			msg:       "flag beats env",
		},
		{
			name: "verbose-default",
			want: func(c config.Config) bool { return !c.Verbose },
			msg:  "default verbose false",
		},
		{
			name:      "verbose-stored",
			useStored: true,
			want:      func(c config.Config) bool { return c.Verbose },
			msg:       "stored verbose true",
		},
		{
			name:      "verbose-env",
			useStored: true,
			env:       map[string]string{config.EnvPrefix + "VERBOSE": "0"},
			want:      func(c config.Config) bool { return !c.Verbose },
			msg:       "env false beats stored true",
		},
		{
			name:      "verbose-flag",
			useStored: true,
			env:       map[string]string{config.EnvPrefix + "VERBOSE": "0"},
			args:      []string{"--verbose"},
			want:      func(c config.Config) bool { return c.Verbose },
			msg:       "flag true beats env false",
		},
		{
			name: "log-format-default",
			want: func(c config.Config) bool { return c.LogFormat == config.LogFormatText },
			msg:  "default log text",
		},
		{
			name:      "log-format-stored",
			useStored: true,
			want:      func(c config.Config) bool { return c.LogFormat == config.LogFormatJSON },
			msg:       "stored log json",
		},
		{
			name:      "log-format-env",
			useStored: true,
			env:       map[string]string{config.EnvPrefix + "LOG_FORMAT": "text"},
			want:      func(c config.Config) bool { return c.LogFormat == config.LogFormatText },
			msg:       "env text beats stored json",
		},
		{
			name:      "log-format-flag",
			useStored: true,
			env:       map[string]string{config.EnvPrefix + "LOG_FORMAT": "text"},
			args:      []string{"--log-format", "json"},
			want:      func(c config.Config) bool { return c.LogFormat == config.LogFormatJSON },
			msg:       "flag json beats env text",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := config.Defaults()
			if tc.useStored {
				base = config.ConfigFromStored(stored)
			}
			cfg := resolveWithStored(t, base.StoredConfig(), tc.env, tc.args)
			if !tc.want(cfg) {
				t.Fatalf("%s: %+v", tc.msg, cfg)
			}
		})
	}
}

func TestResolvePrecedence(t *testing.T) {
	stored := storedConfig()
	stored.Cluster = "from-stored"
	t.Setenv(config.EnvPrefix+"CLUSTER", "from-env")

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var fv config.FlagValues
	config.RegisterGlobalFlags(fs, &fv)
	if err := fs.Parse([]string{"--cluster", "from-flag"}); err != nil {
		t.Fatal(err)
	}
	o := config.OverridesFromFlags(fs, fv)
	resultsDB, err := config.ResolveResultsDB(o)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Resolve(config.ConfigFromStored(stored), resultsDB, o)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cluster != "from-flag" {
		t.Fatalf("cluster = %q, want from-flag", cfg.Cluster)
	}
}

func TestDefaultsAndValidate(t *testing.T) {
	resultsDB, err := config.ResolveResultsDB(config.Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Resolve(config.Defaults(), resultsDB, config.Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CentralNamespace != "numaflow-system" {
		t.Fatalf("central namespace default: %q", cfg.CentralNamespace)
	}
	if cfg.Cluster != "numaflow" {
		t.Fatalf("cluster default: %q", cfg.Cluster)
	}
	if cfg.TestedNumaflowMajorMinor != "1.8" {
		t.Fatalf("tested range default: %q", cfg.TestedNumaflowMajorMinor)
	}
	if cfg.Image != config.DefaultNumaflowImage {
		t.Fatalf("image default: %q", cfg.Image)
	}
}

func TestStoredEmptyValuesPreserved(t *testing.T) {
	stored := config.StoredConfig{
		Cluster:             "stored-cluster",
		Context:             "",
		PrometheusURL:       "",
		UDFImage:            "",
		CentralNamespace:    "numaflow-system",
		MonitoringNamespace: "monitoring",
		ValidationNamespace: "validation-system",
		LogFormat:           config.LogFormatText,
	}
	resultsDB, err := config.ResolveResultsDB(config.Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Resolve(config.ConfigFromStored(stored), resultsDB, config.Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Context != "" {
		t.Fatalf("context = %q, want empty stored value", cfg.Context)
	}
	if cfg.PrometheusURL != "" {
		t.Fatalf("prometheus_url = %q, want empty stored value", cfg.PrometheusURL)
	}
	if cfg.UDFImage != "" {
		t.Fatalf("udf_image = %q, want empty stored value", cfg.UDFImage)
	}
}

func TestResolveResultsDBPrecedence(t *testing.T) {
	t.Setenv(config.EnvPrefix+"RESULTS_DB", "/env/db.sqlite")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var fv config.FlagValues
	config.RegisterGlobalFlags(fs, &fv)
	if err := fs.Parse([]string{"--results-db", "/flag/db.sqlite"}); err != nil {
		t.Fatal(err)
	}
	o := config.OverridesFromFlags(fs, fv)
	got, err := config.ResolveResultsDB(o)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/flag/db.sqlite" {
		t.Fatalf("results db = %q, want /flag/db.sqlite", got)
	}
}

func TestEnsureResultsLayout(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.ResultsDB = filepath.Join(dir, "nested", "db.sqlite")
	if err := cfg.EnsureResultsLayout(); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(cfg.ResultsDB)
	if st, err := os.Stat(parent); err != nil || !st.IsDir() {
		t.Fatalf("missing dir %q: %v", parent, err)
	}
}
