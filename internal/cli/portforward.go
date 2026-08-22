package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"numa-perfman/internal/central"
	"numa-perfman/internal/cluster"
	"numa-perfman/internal/validation"
)

type forwardSession struct {
	proc cluster.Process
	url  string
	kill func() error
}

func (s *forwardSession) close() {
	if s == nil {
		return
	}
	if s.kill != nil {
		_ = s.kill()
	}
}

func shouldAutoPrometheus(baseURL string) bool {
	return cluster.ShouldAutoPrometheus(baseURL)
}

func startPrometheusForward(ctx context.Context, client cluster.Client, monitoringNS string) (*forwardSession, error) {
	sess, err := client.StartPrometheusForward(ctx, monitoringNS)
	if err != nil {
		return nil, err
	}
	return &forwardSession{
		url:  sess.URL,
		kill: sess.Kill,
	}, nil
}

func startPostgresForward(ctx context.Context, client cluster.Client, validationNS string) (*forwardSession, int, error) {
	port, err := cluster.FreeTCPPort()
	if err != nil {
		return nil, 0, err
	}
	local := strconv.Itoa(port)
	resource := "svc/" + central.PostgresServiceName
	proc, err := client.PortForward(ctx, validationNS, resource, local, "5432")
	if err != nil {
		return nil, 0, fmt.Errorf("postgres port-forward: %w", err)
	}
	if err := cluster.WaitTCP(ctx, "127.0.0.1:"+local, 90*time.Second); err != nil {
		_ = proc.Kill()
		return nil, 0, err
	}
	sess := &forwardSession{proc: proc, kill: proc.Kill}
	return sess, port, nil
}

func postgresAdminDSN(ctx context.Context, client cluster.Client, validationNS string, localPort int) (validation.PostgresCredentials, error) {
	user, pass, err := readPostgresSecret(ctx, client, validationNS)
	if err != nil {
		return validation.PostgresCredentials{}, err
	}
	host := "127.0.0.1"
	port := strconv.Itoa(localPort)
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, pass),
		Host:   fmt.Sprintf("%s:%s", host, port),
		Path:   "/postgres",
	}
	return validation.PostgresCredentials{
		AdminDSN: u.String(),
		User:     user,
		Password: pass,
		Host:     host,
		Port:     port,
	}, nil
}

func readPostgresSecret(ctx context.Context, client cluster.Client, ns string) (user, password string, err error) {
	out, err := client.GetResourceJSON(ctx, ns, "secret/"+central.PostgresSecretName)
	if err != nil {
		user, password = "numaflow", "numaflow"
		return user, password, nil
	}
	var obj struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		return "", "", err
	}
	decode := func(key, def string) string {
		raw, ok := obj.Data[key]
		if !ok || raw == "" {
			return def
		}
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return def
		}
		return string(b)
	}
	return decode("POSTGRES_USER", "numaflow"), decode("POSTGRES_PASSWORD", "numaflow"), nil
}
