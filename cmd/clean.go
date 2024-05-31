package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/numaproj-labs/numaflow-perfman/util"
)

// cleanCmd represents the clean command
var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove all perfman resources",
	Long:  "Delete all resources from K8s clusters that perfman created during setup ",
	RunE: func(cmd *cobra.Command, args []string) error {

		// Delete Prometheus Operator
		if err := util.UninstallRelease(util.PrometheusReleaseName, util.PerfmanNamespace, log); err != nil {
			return fmt.Errorf("failed to delete Prometheus Operator: %w", err)
		}

		// Delete Grafana
		if err := util.UninstallRelease(util.GrafanaReleaseName, util.PerfmanNamespace, log); err != nil {
			return fmt.Errorf("failed to delete Grafana: %w", err)
		}

		labels := map[string]string{
			"app.kubernetes.io/part-of": "perfman",
		}
		// Delete metrics service monitors
		if err := util.DeleteResourcesWithLabel(dynamicClient, svGvr, util.PerfmanNamespace, labels, log); err != nil {
			return fmt.Errorf("failed to delete metrics service monitors: %w", err)
		}

		// Delete pipeline
		if err := util.DeleteResourcesWithLabel(dynamicClient, pipelineGvr, util.PerfmanNamespace, labels, log); err != nil {
			return fmt.Errorf("failed to delete base pipeline: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)
}
