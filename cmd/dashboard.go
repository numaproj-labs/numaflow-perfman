package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/numaproj-labs/numaflow-perfman/snapshot"
	"github.com/numaproj-labs/numaflow-perfman/util"
)

const templatePipeline = "pipeline"

// Built-in pipeline dashboard template (shipped in this repo).
const defaultPipelineTemplatePath = "./config/dashboard-pipeline-template.json"

var (
	dashboardTemplate     string // built-in template name (e.g. pipeline)
	dashboardTemplatePath string // if set, overrides --template and uses this file path
	dashboardSnapshot     bool
)

const datasourceUIDPlaceholder = "prometheus-datasource-uid-placeholder"

// resolveDatasourceUID creates the default Prometheus datasource or returns its UID if it already exists.
func resolveDatasourceUID(grafanaURL, auth string) (string, error) {
	uid, err := snapshot.CreateGrafanaDataSource(grafanaURL, auth)
	if err == nil {
		return uid, nil
	}
	if !strings.Contains(err.Error(), "data source with the same name already exists") {
		return "", fmt.Errorf("error creating data source: %w", err)
	}
	log.Warn("Prometheus data source already configured, using existing.")
	uid, err = snapshot.FetchGrafanaDataSourceUID(grafanaURL, auth)
	if err != nil {
		return "", fmt.Errorf("error fetching existing data source UID: %w", err)
	}
	return uid, nil
}

// runDashboard loads the template at templatePath, injects the datasource UID, creates/updates the
// dashboard in Grafana, and optionally creates a snapshot (returns snapshot URL) or the live dashboard URL.
func runDashboard(grafanaURL, auth, templatePath string, doSnapshot bool) (resultURL string, err error) {
	// Create the Prometheus data source (or fetch UID if it already exists).
	dsUID, err := resolveDatasourceUID(grafanaURL, auth)
	if err != nil {
		return "", err
	}

	// Read dashboard template from JSON file.
	data, err := snapshot.ReadJSONFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("error reading dashboard template: %w", err)
	}

	// Configure the dashboard template to read from the data source created above.
	data = []byte(strings.Replace(string(data), datasourceUIDPlaceholder, dsUID, -1))

	// Set overwrite so re-running updates the existing dashboard.
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("error parsing dashboard JSON: %w", err)
	}
	payload["overwrite"] = true
	if data, err = json.Marshal(payload); err != nil {
		return "", fmt.Errorf("error building dashboard payload: %w", err)
	}

	// Create dashboard in Grafana.
	resp, err := snapshot.CreateDashboard(grafanaURL, auth, data)
	if err != nil {
		return "", fmt.Errorf("error creating dashboard: %w", err)
	}

	if doSnapshot {
		// Fetch the dashboard (full model needed for snapshot API).
		data, err = snapshot.FetchDashboard(grafanaURL, auth, resp.UID)
		if err != nil {
			return "", fmt.Errorf("error fetching dashboard: %w", err)
		}
		// Create a snapshot and return the snapshot URL.
		resultURL, err = snapshot.CreateSnapshot(grafanaURL, auth, data)
		if err != nil {
			return "", fmt.Errorf("error creating snapshot: %w", err)
		}
		return resultURL, nil
	}

	// Return the live dashboard URL.
	return grafanaURL + resp.URL, nil
}

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Import or snapshot a Grafana dashboard",
	Long: `Import a dashboard into Grafana (live dashboard URL) or create a snapshot (shareable frozen URL).

  Use --template pipeline for the built-in pipeline metrics dashboard, or --template-path to load
  any dashboard JSON from a file (e.g. a MonoVertex or custom template kept outside this repo).

  Import: create/update the dashboard in Grafana and print the live dashboard URL.
  Snapshot: create the dashboard, then create a snapshot and print the snapshot URL. Note: CLI-created
  snapshots often have empty panels (no data) because Grafana's API expects pre-run query data; for
  snapshots with data, open the dashboard in Grafana and use Share → Snapshot. Snapshot URLs use your
  Grafana URL (e.g. localhost); to share externally, expose Grafana at a public URL and set root_url.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := dashboardTemplatePath
		if path == "" {
			if dashboardTemplate != templatePipeline {
				return fmt.Errorf("invalid template %q; use %q or set --template-path to a dashboard JSON file", dashboardTemplate, templatePipeline)
			}
			path = defaultPipelineTemplatePath
		}

		auth := base64.StdEncoding.EncodeToString([]byte("admin:" + util.GrafanaPassword))
		url, err := runDashboard(util.GrafanaURL, auth, path, dashboardSnapshot)
		if err != nil {
			return err
		}
		fmt.Println(url)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(dashboardCmd)
	dashboardCmd.Flags().StringVarP(&dashboardTemplate, "template", "t", templatePipeline,
		"Built-in template: pipeline (default). Ignored if --template-path is set.")
	dashboardCmd.Flags().StringVar(&dashboardTemplatePath, "template-path", "",
		"Path to a dashboard JSON file (overrides --template). Use for custom or external templates (e.g. MonoVertex).")
	dashboardCmd.Flags().BoolVar(&dashboardSnapshot, "snapshot", false,
		"If set, create a snapshot and print snapshot URL; otherwise import dashboard and print live dashboard URL")
}
