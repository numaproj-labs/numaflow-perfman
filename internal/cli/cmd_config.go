package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strconv"

	"numa-perfman/internal/config"
)

func (a *App) cmdConfig(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: config requires show or set", errUsage)
	}
	switch args[0] {
	case "show":
		return a.configShow(ctx, cfg, args[1:])
	case "set":
		return a.configSet(ctx, cfg, args[1:])
	case "help", "-h", "--help":
		printConfigUsage(a.Stdout)
		return nil
	default:
		return fmt.Errorf("%w: unknown config subcommand %q", errUsage, args[0])
	}
}

func (a *App) configShow(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("config show", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("%w: config show takes no arguments", errUsage)
	}
	repo, err := openResultsRepo(ctx, cfg)
	if err != nil {
		return err
	}
	defer repo.Close()
	stored, found, err := repo.LoadHarnessConfig(ctx)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: configuration is not initialized; run setup or config set", errPreflight)
	}
	out := struct {
		Context                  string `json:"context"`
		Cluster                  string `json:"cluster"`
		PrometheusURL            string `json:"prometheus_url"`
		CentralNamespace         string `json:"central_namespace"`
		MonitoringNamespace      string `json:"monitoring_namespace"`
		ValidationNamespace      string `json:"validation_namespace"`
		LogFormat                string `json:"log_format"`
		Verbose                  bool   `json:"verbose"`
		UDFImage                 string `json:"udf_image"`
		Image                    string `json:"image"`
		TestedNumaflowMajorMinor string `json:"tested_numaflow_major_minor"`
	}{
		Context: stored.Context, Cluster: stored.Cluster, PrometheusURL: stored.PrometheusURL,
		CentralNamespace: stored.CentralNamespace, MonitoringNamespace: stored.MonitoringNamespace,
		ValidationNamespace: stored.ValidationNamespace,
		LogFormat:           stored.LogFormat, Verbose: stored.Verbose, UDFImage: stored.UDFImage,
		Image: stored.Image, TestedNumaflowMajorMinor: stored.TestedNumaflowMajorMinor,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func (a *App) configSet(ctx context.Context, bootstrap config.Config, args []string) error {
	fs := flag.NewFlagSet("config set", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var key, value string
	fs.StringVar(&key, "key", "", "configuration key")
	fs.StringVar(&value, "value", "", "configuration value")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if key == "" {
		return fmt.Errorf("%w: --key is required", errUsage)
	}

	repo, err := openResultsRepo(ctx, bootstrap)
	if err != nil {
		return err
	}
	defer repo.Close()
	current := config.Defaults()
	if stored, found, err := repo.LoadHarnessConfig(ctx); err != nil {
		return err
	} else if found {
		current = configFromHarness(stored)
	}
	current.ResultsDB = bootstrap.ResultsDB
	if err := setConfigValue(&current, key, value); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := current.Validate(); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := repo.UpsertHarnessConfig(ctx, harnessConfigFromConfig(current)); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "stored config %s\n", key)
	return nil
}

func setConfigValue(cfg *config.Config, key, value string) error {
	switch key {
	case "context":
		cfg.Context = value
	case "cluster":
		cfg.Cluster = value
	case "prometheus_url":
		cfg.PrometheusURL = value
	case "central_namespace":
		cfg.CentralNamespace = value
	case "monitoring_namespace":
		cfg.MonitoringNamespace = value
	case "validation_namespace":
		cfg.ValidationNamespace = value
	case "log_format":
		cfg.LogFormat = config.LogFormat(value)
	case "verbose":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%s must be true or false", key)
		}
		cfg.Verbose = v
	case "udf_image":
		cfg.UDFImage = value
	case "image":
		cfg.Image = value
	case "tested_numaflow_major_minor":
		cfg.TestedNumaflowMajorMinor = value
	case "results_db":
		return fmt.Errorf("results_db is bootstrap-only; use --results-db or PERFHARNESS_RESULTS_DB")
	default:
		return fmt.Errorf("unknown configuration key %q", key)
	}
	return nil
}
