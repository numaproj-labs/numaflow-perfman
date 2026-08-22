package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"numa-perfman/internal/cluster"
	"numa-perfman/internal/config"
)

// App runs perfman commands with injectable IO and dependencies.
type App struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Args   []string
	// Version is printed by the version command.
	Version string
	// Runner executes external commands; nil uses cluster.DefaultRunner.
	Runner cluster.Runner

	lastSignal error
}

// Execute runs the CLI using os.Args and returns an exit code.
func Execute(args []string) int {
	app := App{
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Args:    args,
		Version: versionString(),
	}
	return app.Run()
}

// Run parses arguments and executes the selected command.
func (a *App) Run() int {
	if a.Stdout == nil {
		a.Stdout = os.Stdout
	}
	if a.Stderr == nil {
		a.Stderr = os.Stderr
	}
	if a.Stdin == nil {
		a.Stdin = os.Stdin
	}
	args := a.Args
	if len(args) == 0 {
		args = os.Args
	}
	if len(args) <= 1 {
		printRootUsage(a.Stdout)
		return ExitSuccess
	}
	raw := args[1:]
	if len(raw) == 1 && (raw[0] == "-h" || raw[0] == "--help" || raw[0] == "help") {
		printRootUsage(a.Stdout)
		return ExitSuccess
	}
	if len(raw) == 1 && (raw[0] == "--version" || raw[0] == "version") {
		fmt.Fprintf(a.Stdout, "perfman %s\n", a.version())
		return ExitSuccess
	}

	parsed, err := splitCLIArgs(raw)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitInvalidUsage
	}
	if len(parsed.Command) == 0 {
		printRootUsage(a.Stdout)
		return ExitInvalidUsage
	}
	if parsed.Command[0] == "help" {
		a.printHelp(parsed)
		return ExitSuccess
	}
	if parsed.Command[0] == "version" {
		fmt.Fprintf(a.Stdout, "perfman %s\n", a.version())
		return ExitSuccess
	}

	gp, err := parseGlobalFlags(parsed.GlobalArgs)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitInvalidUsage
	}
	cfg, err := resolveConfig(gp)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitInvalidUsage
	}

	ctx, cancel := a.signalContext()
	defer cancel()

	runErr := a.dispatch(ctx, cfg, parsed)
	if runErr != nil {
		code := ExitCode(runErr)
		if a.lastSignal != nil && runErr == context.Canceled {
			code = ExitCode(a.lastSignal)
		}
		if code != ExitSuccess {
			fmt.Fprintln(a.Stderr, runErr)
		}
		return code
	}
	return ExitSuccess
}

func (a *App) version() string {
	if a.Version != "" {
		return a.Version
	}
	return versionString()
}

func (a *App) printHelp(p parsedCLI) {
	switch {
	case len(p.Command) == 0:
		printRootUsage(a.Stdout)
	case p.Command[0] == "benchmark":
		printBenchmarkUsage(a.Stdout)
	case p.Command[0] == "validation":
		printValidationUsage(a.Stdout)
	case p.Command[0] == "runs":
		printRunsUsage(a.Stdout)
	case p.Command[0] == "reports":
		printReportsUsage(a.Stdout)
	case p.Command[0] == "serve":
		printServeUsage(a.Stdout)
	case p.Command[0] == "cleanup":
		printCleanupUsage(a.Stdout)
	case p.Command[0] == "setup":
		printSetupUsage(a.Stdout)
	case p.Command[0] == "config":
		printConfigUsage(a.Stdout)
	default:
		printRootUsage(a.Stdout)
	}
}

func (a *App) signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-sigCh:
			if sig == syscall.SIGTERM {
				a.lastSignal = errInterruptedSIGTERM
			} else {
				a.lastSignal = errInterruptedSIGINT
			}
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func (a *App) runner() cluster.Runner {
	if a.Runner != nil {
		return a.Runner
	}
	return cluster.DefaultRunner
}

func (a *App) clusterClient(cfg config.Config) cluster.Client {
	return cluster.Client{Runner: a.runner(), Context: cfg.Context}
}

func (a *App) dispatch(ctx context.Context, cfg config.Config, p parsedCLI) error {
	switch p.Command[0] {
	case "setup":
		return a.cmdSetup(ctx, cfg, p.Args)
	case "config":
		return a.cmdConfig(ctx, cfg, p.Args)
	case "doctor":
		return a.cmdDoctor(ctx, cfg, p.Args)
	case "benchmark":
		return a.cmdBenchmark(ctx, cfg, p.Args)
	case "validation":
		return a.cmdValidation(ctx, cfg, p.Args)
	case "runs":
		return a.cmdRuns(ctx, cfg, p.Args)
	case "reports":
		return a.cmdReports(ctx, cfg, p.Args)
	case "serve":
		return a.cmdServe(ctx, cfg, p.Args)
	case "cleanup":
		return a.cmdCleanup(ctx, cfg, p.Args)
	default:
		return fmt.Errorf("%w: unknown command %q", errUsage, p.Command[0])
	}
}
