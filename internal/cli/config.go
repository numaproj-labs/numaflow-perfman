package cli

import (
	"context"
	"flag"
	"fmt"
	"time"

	"numa-perfman/internal/config"
	"numa-perfman/internal/results"
)

type globalParse struct {
	Values config.FlagValues
	FS     *flag.FlagSet
}

func parseGlobalFlags(args []string) (globalParse, error) {
	fs := flag.NewFlagSet("perfman", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})
	var gp globalParse
	gv := &gp.Values
	config.RegisterGlobalFlags(fs, gv)
	if err := fs.Parse(args); err != nil {
		return gp, fmt.Errorf("%w: %v", errUsage, err)
	}
	gp.FS = fs
	return gp, nil
}

func resolveConfig(gp globalParse) (config.Config, error) {
	overrides := config.OverridesFromFlags(gp.FS, gp.Values)
	dbPath, err := config.ResolveResultsDB(overrides)
	if err != nil {
		return config.Config{}, fmt.Errorf("%w: %v", errUsage, err)
	}

	ctx := context.Background()
	repo, err := results.Open(ctx, dbPath)
	if err != nil {
		return config.Config{}, fmt.Errorf("%w: open results db: %v", errUsage, err)
	}
	defer repo.Close()

	base := config.Defaults()
	if stored, found, err := repo.LoadHarnessConfig(ctx); err != nil {
		return config.Config{}, fmt.Errorf("%w: load harness config: %v", errUsage, err)
	} else if found {
		base = configFromHarness(stored)
	}

	cfg, err := config.Resolve(base, dbPath, overrides)
	if err != nil {
		return cfg, fmt.Errorf("%w: %v", errUsage, err)
	}
	return cfg, nil
}

func configFromHarness(h results.HarnessConfig) config.Config {
	return config.ConfigFromStored(config.StoredConfig{
		Context:                  h.Context,
		Cluster:                  h.Cluster,
		PrometheusURL:            h.PrometheusURL,
		CentralNamespace:         h.CentralNamespace,
		MonitoringNamespace:      h.MonitoringNamespace,
		ValidationNamespace:      h.ValidationNamespace,
		LogFormat:                config.LogFormat(h.LogFormat),
		Verbose:                  h.Verbose,
		UDFImage:                 h.UDFImage,
		Image:                    h.Image,
		TestedNumaflowMajorMinor: h.TestedNumaflowMajorMinor,
	})
}

func harnessConfigFromConfig(c config.Config) results.HarnessConfig {
	stored := c.StoredConfig()
	return results.HarnessConfig{
		Context:                  stored.Context,
		Cluster:                  stored.Cluster,
		PrometheusURL:            stored.PrometheusURL,
		CentralNamespace:         stored.CentralNamespace,
		MonitoringNamespace:      stored.MonitoringNamespace,
		ValidationNamespace:      stored.ValidationNamespace,
		LogFormat:                string(stored.LogFormat),
		Verbose:                  stored.Verbose,
		UDFImage:                 stored.UDFImage,
		Image:                    stored.Image,
		TestedNumaflowMajorMinor: stored.TestedNumaflowMajorMinor,
		UpdatedAt:                time.Now().UTC(),
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
