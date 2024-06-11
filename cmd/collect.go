package cmd

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"

	"github.com/numaproj-labs/numaflow-perfman/collect"
	"github.com/numaproj-labs/numaflow-perfman/util"
)

var (
	Name    string
	Period  int
	Metrics []string
)

var validMetrics = []string{"latency", "tps"}

// RateInterval is used to determine over what period the rate function is computed
const RateInterval = collect.DefaultStep * 4

// collectCmd represents the collect command
var collectCmd = &cobra.Command{
	Use:   "collect",
	Short: "Collect Prometheus data",
	Long:  "Collect Prometheus metrics from a running pipeline, for a given time range",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Period <= 0 {
			return fmt.Errorf("value provided to period flag must be greater than 0")
		}

		// Check that the metrics provided are valid
		for _, metric := range Metrics {
			if !slices.Contains(validMetrics, metric) {
				return fmt.Errorf("invalid metric: %s. Valid metrics are: %v", metric, validMetrics)
			}
		}

		client, err := api.NewClient(api.Config{
			Address: util.PrometheusURL,
		})
		if err != nil {
			return fmt.Errorf("error creating client: %w", err)
		}

		v1api := v1.NewAPI(client)

		queryRange := v1.Range{
			Start: time.Now().Add(-5 * time.Minute),
			End:   time.Now(),
			Step:  collect.DefaultStep,
		}

		result, _, err := v1api.QueryRange(context.TODO(), "rate(forwarder_read_total[1m])", queryRange)
		if err != nil {
			return fmt.Errorf("error querying Prometheus: %w", err)
		}

		matrix := result.(model.Matrix)
		for _, v := range matrix {
			fmt.Printf("%v =>\n", v.Metric)
			for _, val := range v.Values {
				fmt.Printf("%v, %v\n", val.Value, val.Timestamp)
			}
		}

		minutes := int(RateInterval / time.Minute)
		seconds := int((RateInterval % time.Minute) / time.Second)
		fmt.Printf("%dm%ds\n", minutes, seconds)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(collectCmd)

	collectCmd.Flags().StringVarP(&Name, "name", "n", "", "Specify the name of the folder under which you would like to output the Prometheus data files")
	collectCmd.Flags().IntVarP(&Period, "period", "p", 0, "Specify the time period for which you want to collect Prometheus data")
	collectCmd.Flags().StringSliceVarP(&Metrics, "metrics", "m", validMetrics, "Specify the metrics you would like to collect data for")
	collectCmd.MarkFlagRequired("period")
	collectCmd.MarkFlagRequired("name")
}
