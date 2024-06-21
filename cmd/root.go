package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/numaproj-labs/numaflow-perfman/util"
)

var (
	config        *rest.Config
	kubeClient    *kubernetes.Clientset
	dynamicClient *dynamic.DynamicClient
	log           *zap.Logger
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:          "perfman",
	Short:        "Numaflow performance testing framework",
	Long:         "Perfman is a command line utility for performance testing changes to the numaflow platform",
	SilenceUsage: true,
}

// Execute adds all child commands to the root command and sets flags appropriately
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	var err error

	config, err = util.K8sRestConfig()
	if err != nil {
		panic(err)
	}

	kubeClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	dynamicClient, err = dynamic.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	log = util.CreateLogger()
}
