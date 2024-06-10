package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// collectCmd represents the collect command
var collectCmd = &cobra.Command{
	Use:   "collect",
	Short: "Collect Prometheus data",
	Long:  "Collect Prometheus metrics from a running pipeline, for a given time range",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("collect called")
	},
}

func init() {
	rootCmd.AddCommand(collectCmd)
}
