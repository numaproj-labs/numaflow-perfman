package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/numaproj-labs/numaflow-perfman/util"
)

var Numaflow bool
var Jetstream bool

var svGvr = schema.GroupVersionResource{
	Group:    "monitoring.coreos.com",
	Version:  "v1",
	Resource: "servicemonitors",
}

// setupCmd represents the setup command
var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Deploy necessary services",
	Long:  "The setup command deploys Prometheus Operator, Grafana, and Service Monitors onto the cluster",
	Args: func(cmd *cobra.Command, args []string) error {
		nonFlagArgs := cmd.Flags().Args()
		if len(nonFlagArgs) > 0 {
			return errors.New("this command doesn't accept args")
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Optionally install numaflow
		if cmd.Flag("numaflow").Changed {
			numaflowChart := util.ChartRelease{
				ChartName:   "numaflow",
				ReleaseName: "perfman-numaflow",
				RepoUrl:     "https://numaproj.io/helm-charts",
				Namespace:   util.NumaflowNamespace,
				Values:      nil,
			}
			if err := numaflowChart.InstallOrUpgradeRelease(kubeClient, log); err != nil {
				return fmt.Errorf("unable to install numaflow: %w", err)
			}
		}

		// Optionally install ISB service
		if cmd.Flag("jetstream").Changed {
			isbGvr := schema.GroupVersionResource{
				Group:    "numaflow.numaproj.io",
				Version:  "v1alpha1",
				Resource: "interstepbufferservices",
			}
			if err := util.CreateResource("default/isbvc.yaml", dynamicClient, isbGvr, util.PerfmanNamespace, log); err != nil {
				return fmt.Errorf("failed to create jetsream-isbvc: %w", err)
			}
		}

		// Install Prometheus Operator
		kubePrometheusChart := util.ChartRelease{
			ChartName:   "kube-prometheus",
			ReleaseName: util.PrometheusReleaseName,
			RepoUrl:     "https://charts.bitnami.com/bitnami",
			Namespace:   util.PerfmanNamespace,
			Values:      nil,
		}
		if err := kubePrometheusChart.InstallOrUpgradeRelease(kubeClient, log); err != nil {
			return fmt.Errorf("failed to install prometheus operator: %w", err)
		}

		// Install Grafana
		// TODO: figure out how to sync k8s secret with updated password
		grafanaChart := util.ChartRelease{
			ChartName:   "grafana",
			ReleaseName: util.GrafanaReleaseName,
			RepoUrl:     "https://grafana.github.io/helm-charts",
			Namespace:   util.PerfmanNamespace,
			Values: map[string]interface{}{
				"adminPassword": util.GrafanaPassword,
			},
		}
		if err := grafanaChart.InstallOrUpgradeRelease(kubeClient, log); err != nil {
			return fmt.Errorf("unable to install grafana: %w", err)
		}

		// Install metrics service monitors
		if err := util.CreateResource("default/pipeline-metrics.yaml", dynamicClient, svGvr, util.PerfmanNamespace, log); err != nil {
			return fmt.Errorf("failed to create service monitor for pipeline metrics: %w", err)
		}

		if err := util.CreateResource("default/isbvc-jetstream-metrics.yaml", dynamicClient, svGvr, util.PerfmanNamespace, log); err != nil {
			return fmt.Errorf("failed to create service monitor for jetstream metrics: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)

	setupCmd.Flags().BoolVarP(&Numaflow, "numaflow", "n", false, "Install/upgrade the numaflow system")
	setupCmd.Flags().BoolVarP(&Jetstream, "jetstream", "j", false, "Install jetsream as the InterStepBuffer service")
}
