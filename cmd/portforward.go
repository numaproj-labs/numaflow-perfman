package cmd

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"

	"github.com/numaproj-labs/numaflow-perfman/portforward"
	"github.com/numaproj-labs/numaflow-perfman/util"
)

var PfPrometheus bool
var PfGrafana bool

// portforwardCmd represents the pf command
var portforwardCmd = &cobra.Command{
	Use:   "portforward",
	Short: "Port forward services",
	Long:  "Port forward services",
	Args: func(cmd *cobra.Command, args []string) error {
		nonFlagArgs := cmd.Flags().Args()
		if len(nonFlagArgs) > 0 {
			return errors.New("this command doesn't accept args")
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {

		// stream is used to tell the port forwarder where to place its output, and where to expect input if needed
		stream := genericiooptions.IOStreams{
			In:     os.Stdin,
			Out:    os.Stdout,
			ErrOut: os.Stderr,
		}

		if cmd.Flag("prometheus").Changed && cmd.Flag("grafana").Changed {
			return errors.New("only one service can be port forwarded at a time")
		}

		// Port forward prometheus operator so that it can be used as a source in the Grafana dashboard
		if cmd.Flag("prometheus").Changed {
			serviceName := util.PrometheusPFServiceName
			podName, err := portforward.GetPodFromService(kubeClient, util.PerfmanNamespace, serviceName)
			if err != nil {
				return fmt.Errorf("unable to find a pod for the service: %w", err)
			}

			var prometheusWg sync.WaitGroup
			prometheusWg.Add(1)
			prometheusStopCh := make(chan struct{}, 1)
			prometheusReadyCh := make(chan struct{})

			prometheusPf := portforward.APodRequest{
				RestConfig: config,
				Pod: v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName,
						Namespace: util.PerfmanNamespace,
					},
				},
				LocalPort: 9090,
				PodPort:   9090,
				Streams:   stream,
				StopCh:    prometheusStopCh,
				ReadyCh:   prometheusReadyCh,
			}

			portforward.WaitForTermination(prometheusStopCh, &prometheusWg)

			go func() {
				err := prometheusPf.PortForwardAPod()
				if err != nil {
					panic(err)
				}
			}()

			<-prometheusPf.ReadyCh

			prometheusWg.Wait()
		}

		// Port forward Grafana
		if cmd.Flag("grafana").Changed {
			serviceName := util.GrafanaPFServiceName
			podName, err := portforward.GetPodFromService(kubeClient, util.PerfmanNamespace, serviceName)
			if err != nil {
				return fmt.Errorf("unable to find a pod for the service: %w", err)
			}

			var grafanaWg sync.WaitGroup
			grafanaWg.Add(1)
			grafanaStopCh := make(chan struct{}, 1)
			grafanaReadyCh := make(chan struct{})

			grafanaPf := portforward.APodRequest{
				RestConfig: config,
				Pod: v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName,
						Namespace: util.PerfmanNamespace,
					},
				},
				LocalPort: 3000,
				PodPort:   3000,
				Streams:   stream,
				StopCh:    grafanaStopCh,
				ReadyCh:   grafanaReadyCh,
			}

			portforward.WaitForTermination(grafanaStopCh, &grafanaWg)

			go func() {
				err := grafanaPf.PortForwardAPod()
				if err != nil {
					panic(err)
				}
			}()

			<-grafanaPf.ReadyCh

			grafanaWg.Wait()
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(portforwardCmd)

	portforwardCmd.Flags().BoolVarP(&PfPrometheus, "prometheus", "p", false, "Port forward prometheus operator to localhost:9090")
	portforwardCmd.Flags().BoolVarP(&PfGrafana, "grafana", "g", false, "Port forward grafana to localhost:3000")

}
