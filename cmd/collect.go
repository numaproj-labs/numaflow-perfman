package cmd

import (
	"fmt"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/numaproj-labs/numaflow-perfman/collect"
	"github.com/numaproj-labs/numaflow-perfman/util"
)

var (
	Name    string
	Last    int
	Metrics []string
)

var validMetrics []string
var metrics = map[string][]collect.MetricObject{
	"data-forward": {
		collect.InboundMessages,
	},
	"latency": {
		collect.ForwarderE2EP90,
		collect.ForwarderE2EP95,
		collect.ForwarderE2EP99,
	},
}

// collectCmd represents the collect command
var collectCmd = &cobra.Command{
	Use:   "collect",
	Short: "Collect Prometheus data",
	Long:  "Collect Prometheus metrics from a running pipeline, for a given time range",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Last <= 0 {
			return fmt.Errorf("value provided to period flag must be greater than 0")
		}

		if cmd.Flags().Changed("metrics") {
			// Check that the provided metrics are valid
			for _, metric := range Metrics {
				if _, ok := metrics[metric]; !ok {
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
			metricObjects := metrics[metric]
			if err := collect.ProcessMetrics(v1api, metricObjects, Last, log); err != nil {
				return fmt.Errorf("failed to process metrics over the last %d minutes: %w", Last, err)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(collectCmd)

	for m := range metrics {
		validMetrics = append(validMetrics, m)
	}
	collectCmd.Flags().StringVarP(&Name, "name", "n", "", "Specify the name of the folder to output the Prometheus data files")
	collectCmd.Flags().IntVarP(&Last, "last", "l", 0, "Specify how many minutes to go back starting from the current time")
	collectCmd.Flags().StringSliceVarP(&Metrics, "metrics", "m", validMetrics, "Specify the metrics to collect Prometheus data for")
	if err := collectCmd.MarkFlagRequired("last"); err != nil {
		log.Fatal("Failed to mark period flag as required", zap.Error(err))
	}
	if err := collectCmd.MarkFlagRequired("name"); err != nil {
		log.Fatal("Failed to mark name flag as required", zap.Error(err))
	}

	// TODO: Add start and end flags to specify specific start and end times
}
