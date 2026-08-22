package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"time"

	"numa-perfman/internal/config"
	"numa-perfman/internal/doctor"
	"numa-perfman/internal/results"
	"numa-perfman/internal/setup"
)

func (a *App) cmdSetup(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var createCluster, withValidation bool
	var image, caCert, crdsPath, kindConfig, kindNodeImage string
	fs.BoolVar(&createCluster, "create-cluster", false, "create kind cluster when missing")
	fs.BoolVar(&withValidation, "with-validation", false, "install validation Postgres")
	fs.StringVar(&image, "image", "", "central Numaflow server image reference")
	fs.StringVar(&caCert, "ca-cert", "", "corporate CA certificate path")
	fs.StringVar(&crdsPath, "crds", "", "Numaflow CRD manifest path")
	fs.StringVar(&kindConfig, "kind-config", "", "kind cluster config path")
	fs.StringVar(&kindNodeImage, "kind-node-image", "", "kindest/node image reference (digest-pinned by default)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected arguments to setup", errUsage)
	}

	if image != "" {
		cfg.Image = image
	}

	client := a.clusterClient(cfg)
	opts := setup.Options{
		Config:         cfg,
		CreateCluster:  createCluster,
		WithValidation: withValidation,
		CACertPath:     caCert,
		CRDsPath:       crdsPath,
		KindConfigPath: kindConfig,
		KindNodeImage:  kindNodeImage,
		Client:         client,
		Runner:         a.runner(),
		Progress:       a.Stderr,
	}
	outCfg, err := setup.Run(ctx, opts)
	if err != nil {
		return fmt.Errorf("%w: %v", errPreflight, err)
	}
	fmt.Fprintf(a.Stdout, "setup complete (cluster=%s image=%s)\n", outCfg.Cluster, outCfg.Image)
	if outCfg.UDFImage != "" {
		fmt.Fprintf(a.Stdout, "udf image: %s\n", outCfg.UDFImage)
	}
	if err := outCfg.EnsureResultsLayout(); err != nil {
		return err
	}
	repo, err := openResultsRepo(ctx, outCfg)
	if err != nil {
		return err
	}
	defer repo.Close()
	if err := repo.UpsertHarnessConfig(ctx, harnessConfigFromConfig(outCfg)); err != nil {
		return fmt.Errorf("%w: persist harness config: %v", errPreflight, err)
	}
	fmt.Fprintf(a.Stdout, "persisted harness config to %s\n", outCfg.ResultsDB)
	return nil
}

func (a *App) cmdDoctor(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var withValidation bool
	fs.BoolVar(&withValidation, "with-validation", false, "check validation Postgres")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	client := a.clusterClient(cfg)
	doctorCfg := cfg
	var promForward *forwardSession
	if shouldAutoPrometheus(cfg.PrometheusURL) {
		sess, err := startPrometheusForward(ctx, client, cfg.MonitoringNamespace)
		if err != nil {
			return fmt.Errorf("%w: prometheus port-forward: %v", errPreflight, err)
		}
		promForward = sess
		doctorCfg.PrometheusURL = sess.url
		defer promForward.close()
	}

	var repo *results.Repository
	if err := cfg.EnsureResultsLayout(); err == nil {
		if opened, openErr := openResultsRepo(ctx, cfg); openErr == nil {
			repo = opened
			defer repo.Close()
		}
	}

	res := doctor.Run(ctx, doctor.Options{
		Config:         doctorCfg,
		WithValidation: withValidation,
		HTTPClient:     &http.Client{Timeout: 10 * time.Second},
		Repository:     repo,
	}, doctor.NewInspector(client), a.runner())
	return printDoctorResult(a, res)
}

func printDoctorResult(a *App, res doctor.Result) error {
	for _, c := range res.Checks {
		line := fmt.Sprintf("[%s] %s: %s", c.Status, c.Name, c.Message)
		if c.Blocker && c.Status == doctor.StatusFail {
			fmt.Fprintln(a.Stderr, line)
		} else {
			fmt.Fprintln(a.Stdout, line)
		}
	}
	if !res.Passed() {
		return fmt.Errorf("%w: %v", errPreflight, res.Error())
	}
	return nil
}
