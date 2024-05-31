package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/numaproj-labs/numaflow-perfman/util"
)

var pipelineGvr = schema.GroupVersionResource{
	Group:    "numaflow.numaproj.io",
	Version:  "v1alpha1",
	Resource: "pipelines",
}

// pipelineCmd represents the pipeline command
var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Apply the base numaflow pipeline",
	Long:  "Apply the base numaflow pipeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := util.CreateResource("default/pipeline.yaml", dynamicClient, pipelineGvr, util.PerfmanNamespace, log); err != nil {
			return fmt.Errorf("failed to apply base pipeline: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(pipelineCmd)

	// TODO: add path flag so that users can specify their own custom testing pipelines
}
