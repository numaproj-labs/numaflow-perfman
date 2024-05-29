package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/numaproj-labs/numaflow-perfman/util"
)

// pipelineCmd represents the pipeline command
var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Apply the base numaflow pipeline",
	Long:  "Apply the base numaflow pipeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		pipelineGvro := util.GVRObject{
			Group:     "numaflow.numaproj.io",
			Version:   "v1alpha1",
			Resource:  "pipelines",
			Namespace: util.PerfmanNamespace,
		}

		if err := pipelineGvro.CreateResource("default/pipeline.yaml", dynamicClient, log); err != nil {
			return fmt.Errorf("failed to apply base pipeline: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(pipelineCmd)

	// TODO: add path flag so that users can specify their own custom testing pipelines
}
