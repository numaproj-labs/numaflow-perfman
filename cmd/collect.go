package cmd

import (
	"fmt"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/numaproj-labs/numaflow-perfman/collect"
	"github.com/numaproj-labs/numaflow-perfman/metrics"
	"github.com/numaproj-labs/numaflow-perfman/util"
)

var (
	Name            string
	LookbackMinutes int
	Metrics         []string
)

var validMetrics []string

// collectCmd represents the collect command
var collectCmd = &cobra.Command{
	Use:   "collect",
	Short: "Collect Prometheus data",
	Long:  "Collect Prometheus metrics from a running pipeline, for a given time range, and output as CSV files",
	RunE: func(cmd *cobra.Command, args []string) error {
		if LookbackMinutes <= 0 {
			return fmt.Errorf("value provided to period flag must be greater than 0")
		}

		if cmd.Flags().Changed("metrics") {
			// Check that the provided metrics are valid
			for _, metric := range Metrics {
				if _, ok := metrics.MetricGroups[metric]; !ok {
					return fmt.Errorf("invalid metric: %s, valid metrics are: %v", metric, validMetrics)
				}
			}
		}

		client, err := api.NewClient(api.Config{
			Address: util.PrometheusURL,
		})
		if err != nil {
			return fmt.Errorf("error creating client: %w", err)
		}
		v1api := v1.NewAPI(client)

		for _, metric := range Metrics {
			metricObjects := metrics.MetricGroups[metric]
			if err := collect.ProcessMetrics(v1api, metric, metricObjects, Name, LookbackMinutes, log); err != nil {
				return fmt.Errorf("failed to process metrics over the last %d minutes: %w", LookbackMinutes, err)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(collectCmd)

	for m := range metrics.MetricGroups {
		validMetrics = append(validMetrics, m)
	}
	collectCmd.Flags().StringVarP(&Name, "name", "n", "", "Specify the name of the folder to output the Prometheus data files")
	collectCmd.Flags().IntVarP(&LookbackMinutes, "lookbackminutes", "l", 0, "Specify how many minutes to go back starting from the current time")
	collectCmd.Flags().StringSliceVarP(&Metrics, "metrics", "m", validMetrics, "Specify the metrics to collect Prometheus data for. Available metrics are:")
	if err := collectCmd.MarkFlagRequired("lookbackminutes"); err != nil {
		log.Fatal("Failed to mark period flag as required", zap.Error(err))
	}
	if err := collectCmd.MarkFlagRequired("name"); err != nil {
		log.Fatal("Failed to mark name flag as required", zap.Error(err))
	}

	// TODO: Add start and end flags to specify specific start and end times
	// TODO: Add pipeline flag to specify custom performance testing pipelines to collect metrics on
}
