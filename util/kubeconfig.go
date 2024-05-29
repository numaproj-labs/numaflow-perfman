package util

import (
	"os"
	"path/filepath"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// K8sRestConfig returns a rest config for the kubernetes cluster
func K8sRestConfig() (*rest.Config, error) {
	var restConfig *rest.Config
	var err error
	kubeconfig := os.Getenv("KUBECONFIG")

	if kubeconfig == "" {
		home := homedir.HomeDir()
		kubeconfig = filepath.Join(home, ".kube", "config")
		if _, err := os.Stat(kubeconfig); err != nil && os.IsNotExist(err) {
			kubeconfig = ""
		}
	}

	if kubeconfig != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		restConfig, err = rest.InClusterConfig()
	}

	return restConfig, err
}
