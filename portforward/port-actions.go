package portforward

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type APodRequest struct {
	RestConfig *rest.Config
	Pod        v1.Pod
	LocalPort  int
	PodPort    int
	Streams    genericiooptions.IOStreams
	StopCh     <-chan struct{}
	ReadyCh    chan struct{}
}

func (req *APodRequest) PortForwardAPod() error {
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward",
		req.Pod.Namespace, req.Pod.Name)

	var scheme string
	var host string
	if strings.HasPrefix(req.RestConfig.Host, "https://") {
		scheme = "https"
		host = strings.TrimPrefix(req.RestConfig.Host, "https://")
	} else {
		scheme = "http"
		host = strings.TrimPrefix(req.RestConfig.Host, "http://")
	}

	transport, upgrader, err := spdy.RoundTripperFor(req.RestConfig)
	if err != nil {
		return err
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, &url.URL{Scheme: scheme, Path: path, Host: host})
	fw, err := portforward.New(dialer, []string{fmt.Sprintf("%d:%d", req.LocalPort, req.PodPort)}, req.StopCh, req.ReadyCh, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}

	return fw.ForwardPorts()
}

func WaitForTermination(stopCh chan struct{}, wg *sync.WaitGroup) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Println("Terminating connection...")
		close(stopCh)
		wg.Done()
	}()
}

func GetPodFromService(kubeClient *kubernetes.Clientset, namespace string, serviceName string) (string, error) {
	pods, err := kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/instance=" + serviceName,
	})
	if err != nil {
		return "", fmt.Errorf("failed to fetch pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no matching pods found for %s", serviceName)
	}

	firstPod := pods.Items[0].Name
	return firstPod, nil
}
