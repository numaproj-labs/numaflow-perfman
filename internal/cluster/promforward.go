package cluster

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	prom "numa-perfman/internal/prometheus"
)

// ForwardSession is a live kubectl port-forward process.
type ForwardSession struct {
	URL  string
	Kill func() error
}

// Close stops the port-forward process.
func (s *ForwardSession) Close() {
	if s == nil {
		return
	}
	if s.Kill != nil {
		_ = s.Kill()
	}
}

// ShouldAutoPrometheus reports whether Prometheus should be reached via port-forward.
func ShouldAutoPrometheus(baseURL string) bool {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return true
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return true
	}
	host := u.Hostname()
	if host == "" {
		return true
	}
	if host == "127.0.0.1" || host == "localhost" {
		return false
	}
	if strings.HasSuffix(host, ".svc") || strings.Contains(host, ".svc.cluster.local") {
		return true
	}
	return false
}

// FreeTCPPort reserves an ephemeral localhost TCP port.
func FreeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// StartPrometheusForward opens a port-forward to svc/prometheus and waits until ready.
func (c Client) StartPrometheusForward(ctx context.Context, monitoringNS string) (*ForwardSession, error) {
	port, err := FreeTCPPort()
	if err != nil {
		return nil, err
	}
	local := strconv.Itoa(port)
	proc, err := c.PortForward(ctx, monitoringNS, "svc/prometheus", local, "9090")
	if err != nil {
		return nil, fmt.Errorf("prometheus port-forward: %w", err)
	}
	base := prom.PortForwardURL(port)
	if err := WaitHTTPReady(ctx, base+"/-/ready", 60*time.Second); err != nil {
		_ = proc.Kill()
		return nil, err
	}
	return &ForwardSession{
		URL:  base,
		Kill: proc.Kill,
	}, nil
}

const (
	// NumaflowServerLocalPort is the fixed localhost port for the central Numaflow UI.
	NumaflowServerLocalPort  = 8443
	numaflowServerRemotePort = "8443"
	numaflowServerResource   = "svc/numaflow-server"
)

// NumaflowServerUIURL is the HTTPS URL for the central Numaflow UI on localhost.
func NumaflowServerUIURL() string {
	return fmt.Sprintf("https://127.0.0.1:%d", NumaflowServerLocalPort)
}

// StartNumaflowServerForward opens a port-forward to svc/numaflow-server on localhost:8443.
// If something is already listening on that port (e.g. a manual kubectl port-forward),
// the existing listener is reused and Kill is nil.
func (c Client) StartNumaflowServerForward(ctx context.Context, centralNS string) (*ForwardSession, error) {
	local := strconv.Itoa(NumaflowServerLocalPort)
	addr := "127.0.0.1:" + local
	uiURL := NumaflowServerUIURL()
	if err := WaitTCP(ctx, addr, 500*time.Millisecond); err == nil {
		return &ForwardSession{URL: uiURL}, nil
	}
	proc, err := c.PortForward(ctx, centralNS, numaflowServerResource, local, numaflowServerRemotePort)
	if err != nil {
		return nil, fmt.Errorf("numaflow-server port-forward: %w", err)
	}
	if err := WaitTCP(ctx, addr, 60*time.Second); err != nil {
		_ = proc.Kill()
		return nil, err
	}
	return &ForwardSession{
		URL:  uiURL,
		Kill: proc.Kill,
	}, nil
}

// WaitHTTPReady polls an HTTP URL until it returns a 2xx/3xx status or the timeout elapses.
func WaitHTTPReady(ctx context.Context, readyURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, readyURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				return nil
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("wait for %s: %w", readyURL, err)
			}
			return fmt.Errorf("wait for %s: status not ready", readyURL)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// WaitTCP polls until a TCP dial succeeds or the timeout elapses.
func WaitTCP(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait for tcp %s: %w", addr, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
