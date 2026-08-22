package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"numa-perfman/internal/config"
	"numa-perfman/internal/report"
	"numa-perfman/internal/results"
)

func (a *App) cmdReports(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: reports requires list or show", errUsage)
	}
	switch args[0] {
	case "list":
		return a.reportsList(ctx, cfg, args[1:])
	case "show":
		return a.reportsShow(ctx, cfg, args[1:])
	case "help", "-h", "--help":
		printReportsUsage(a.Stdout)
		return nil
	default:
		return fmt.Errorf("%w: unknown reports subcommand %q", errUsage, args[0])
	}
}

func (a *App) reportsList(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("reports list", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var kind, scenarioID string
	var limit int
	fs.StringVar(&kind, "kind", "", "comparison or single")
	fs.StringVar(&scenarioID, "scenario", "", "scenario filter")
	fs.IntVar(&limit, "limit", 50, "max rows")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	repo, err := openResultsRepo(ctx, cfg)
	if err != nil {
		return err
	}
	defer repo.Close()

	reports, err := repo.ListReports(ctx, results.ListReportsFilter{
		Kind: kind, Scenario: scenarioID, Limit: limit,
	})
	if err != nil {
		return err
	}
	for _, r := range reports {
		fmt.Fprintf(a.Stdout, "%s\t%s\t%s\t%s\t%s\n",
			r.ID, r.Kind, r.Scenario, r.GeneratedAt.Format(time.RFC3339), r.SHA256)
	}
	return nil
}

func (a *App) reportsShow(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("reports show", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var id, format string
	fs.StringVar(&id, "id", "", "report UUID")
	fs.StringVar(&format, "format", "json", "json or html")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if id == "" {
		return fmt.Errorf("%w: --id is required", errUsage)
	}
	switch format {
	case "json", "html":
	default:
		return fmt.Errorf("%w: format must be json or html", errUsage)
	}

	repo, err := openResultsRepo(ctx, cfg)
	if err != nil {
		return err
	}
	defer repo.Close()

	stored, err := repo.GetReport(ctx, id)
	if err != nil {
		return fmt.Errorf("%w: %v", errReport, err)
	}
	switch format {
	case "json":
		data, err := report.RenderStoredJSON(stored)
		if err != nil {
			return fmt.Errorf("%w: %v", errReport, err)
		}
		var pretty json.RawMessage
		if err := json.Unmarshal(data, &pretty); err != nil {
			return fmt.Errorf("%w: %v", errReport, err)
		}
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(pretty)
	case "html":
		data, err := report.RenderStoredHTML(stored, "")
		if err != nil {
			return fmt.Errorf("%w: %v", errReport, err)
		}
		_, err = a.Stdout.Write(data)
		return err
	default:
		return nil
	}
}

func openResultsRepo(ctx context.Context, cfg config.Config) (*results.Repository, error) {
	dbPath, err := cfg.AbsResultsDB()
	if err != nil {
		return nil, err
	}
	repo, err := results.Open(ctx, dbPath)
	if err != nil {
		return nil, fmt.Errorf("%w: open results db: %v", errPreflight, err)
	}
	return repo, nil
}
