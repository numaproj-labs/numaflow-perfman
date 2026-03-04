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

// GetPodFromService returns the name of a pod that backs the given Service.
// It looks up the service's label selector, lists matching pods, and returns one—preferring Running.
// Port-forward targets a pod, not a service, so this is used to pick which pod to forward to.
func GetPodFromService(kubeClient *kubernetes.Clientset, namespace, serviceName string) (string, error) {
	svc, err := kubeClient.CoreV1().Services(namespace).Get(context.TODO(), serviceName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get service %s: %w", serviceName, err)
	}
	if len(svc.Spec.Selector) == 0 {
		return "", fmt.Errorf("service %s has no selector", serviceName)
	}

	selector := labels.SelectorFromSet(svc.Spec.Selector)
	pods, err := kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods for service %s: %w", serviceName, err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found for service %s", serviceName)
	}

	// Prefer a running pod
	for i := range pods.Items {
		if pods.Items[i].Status.Phase == v1.PodRunning {
			return pods.Items[i].Name, nil
		}
	}
	return pods.Items[0].Name, nil
}
