package cli

import (
	"context"
	"flag"
	"fmt"

	"numa-perfman/internal/config"
	"numa-perfman/internal/results"
	"numa-perfman/internal/serve"
)

func (a *App) cmdServe(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var port int
	var listen string
	fs.IntVar(&port, "port", 8080, "HTTP listen port")
	fs.StringVar(&listen, "listen", "127.0.0.1", "HTTP listen address")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("%w: port must be between 1 and 65535", errUsage)
	}

	dbPath, err := cfg.AbsResultsDB()
	if err != nil {
		return err
	}
	repo, err := results.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("%w: open results db: %v", errPreflight, err)
	}
	defer repo.Close()

	addr := fmt.Sprintf("%s:%d", listen, port)
	client := a.clusterClient(cfg)

	numaFwd, err := client.StartNumaflowServerForward(ctx, cfg.CentralNamespace)
	if err != nil {
		fmt.Fprintf(a.Stderr, "warning: numaflow UI port-forward unavailable: %v\n", err)
	} else {
		defer numaFwd.Close()
		fmt.Fprintf(a.Stdout, "numaflow UI at %s\n", numaFwd.URL)
	}
	fmt.Fprintf(a.Stdout, "perfman web UI at http://%s\n", addr)

	return serve.RunWithOptions(ctx, addr, serve.Options{
		Repo:       repo,
		Config:     cfg,
		Client:     client,
		Version:    a.version(),
		EnableJobs: true,
	})
}
