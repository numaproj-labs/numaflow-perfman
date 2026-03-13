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

const monovertexDashboardPath = "./config/dashboard-monovertex-template.json"

var dashboardMonovertexCmd = &cobra.Command{
	Use:   "dashboard-monovertex",
	Short: "Import the MonoVertex Grafana dashboard",
	Long:  "Creates or updates the MonoVertex dashboard in Grafana. Requires Grafana and Prometheus to be reachable (e.g. port-forward with -p -g).",
	RunE: func(cmd *cobra.Command, args []string) error {
		grafanaURL := util.GrafanaURL
		username := "admin"
		password := util.GrafanaPassword

		auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

		// Get Prometheus datasource UID (create or fetch existing)
		dsId, err := snapshot.CreateGrafanaDataSource(grafanaURL, auth)
		if err != nil {
			if strings.Contains(err.Error(), "data source with the same name already exists") {
				log.Warn("Prometheus data source already configured, using existing.")
				dsId, err = snapshot.FetchGrafanaDataSourceUID(grafanaURL, auth)
				if err != nil {
					return fmt.Errorf("error fetching existing data source UID: %w", err)
				}
			} else {
				return fmt.Errorf("error creating data source: %w", err)
			}
		}

		dashboardData, err := snapshot.ReadJSONFile(monovertexDashboardPath)
		if err != nil {
			return fmt.Errorf("error reading dashboard template: %w", err)
		}

		dashboardData = []byte(strings.Replace(string(dashboardData), "prometheus-datasource-uid-placeholder", dsId, -1))

		// Set overwrite so re-running updates the dashboard
		var payload map[string]interface{}
		if err := json.Unmarshal(dashboardData, &payload); err != nil {
			return fmt.Errorf("error parsing dashboard JSON: %w", err)
		}
		payload["overwrite"] = true
		dashboardData, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("error building dashboard payload: %w", err)
		}

		resp, err := snapshot.CreateDashboard(grafanaURL, auth, dashboardData)
		if err != nil {
			return fmt.Errorf("error creating dashboard: %w", err)
		}

		fmt.Printf("MonoVertex dashboard imported successfully.\n")
		fmt.Printf("Open: %s%s\n", grafanaURL, resp.URL)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(dashboardMonovertexCmd)
}
