package central

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"numa-perfman/internal/controller"
)

const (
	ServerDeploymentName     = "numaflow-server"
	ServerServiceName        = "numaflow-server"
	ServerServiceAccountName = "numaflow-server-sa"
	ServerClusterRoleName    = "numaflow-server-role"
	PrometheusDeploymentName = "prometheus"
	PostgresStatefulSetName  = "data-validation-postgres"
	PostgresServiceName      = "data-validation-postgres"
	PostgresSecretName       = "data-validation-postgres-credentials"
	ImagePullPolicy          = "IfNotPresent"
)

var readOnlyVerbs = []any{"get", "list", "watch"}

// ServerRenderInput parameters for the central Numaflow UI server.
type ServerRenderInput struct {
	Namespace           string
	MonitoringNamespace string
	ImageReference      string
}

// PrometheusRenderInput parameters for in-cluster Prometheus.
type PrometheusRenderInput struct {
	Namespace string
	PVCSize   string
}

// PostgresRenderInput parameters for validation Postgres.
type PostgresRenderInput struct {
	Namespace        string
	StorageClassName string
	PVCSize          string
	PostgresUser     string
	PostgresPassword string
	PostgresDatabase string
}

// ManifestSet is a concatenatable bundle of YAML documents.
type ManifestSet struct {
	Documents []string
}

// Concat joins documents with YAML document separators.
func (m ManifestSet) Concat() string {
	if len(m.Documents) == 0 {
		return ""
	}
	return strings.Join(m.Documents, "\n---\n") + "\n"
}

// RenderServer produces central read-only Numaflow server manifests (no Dex).
func RenderServer(in ServerRenderInput) (ManifestSet, error) {
	if err := controller.ValidateImageReference(in.ImageReference); err != nil {
		return ManifestSet{}, err
	}
	ns := strings.TrimSpace(in.Namespace)
	if ns == "" {
		return ManifestSet{}, errors.New("namespace is required")
	}
	img := in.ImageReference

	sa := namespacedObject("v1", "ServiceAccount", ServerServiceAccountName, ns, nil)
	clusterRole := map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRole",
		"metadata": map[string]any{
			"name": ServerClusterRoleName,
			"labels": map[string]any{
				"app.kubernetes.io/component": "numaflow-ux",
				"app.kubernetes.io/name":      "numaflow-ux",
				"app.kubernetes.io/part-of":   "numaflow",
			},
		},
		"rules": []any{
			map[string]any{
				"apiGroups": []any{"numaflow.numaproj.io"},
				"resources": []any{
					"interstepbufferservices", "interstepbufferservices/status",
					"pipelines", "pipelines/status",
					"vertices", "vertices/status", "vertices/scale",
					"monovertices", "monovertices/status", "monovertices/scale",
					"servingpipelines", "servingpipelines/status",
				},
				"verbs": readOnlyVerbs,
			},
			map[string]any{
				"apiGroups": []any{""},
				"resources": []any{"pods", "pods/log", "configmaps", "services", "persistentvolumeclaims", "namespaces"},
				"verbs":     readOnlyVerbs,
			},
			map[string]any{
				"apiGroups": []any{"", "events.k8s.io"},
				"resources": []any{"events"},
				"verbs":     readOnlyVerbs,
			},
			map[string]any{
				"apiGroups": []any{"apps"},
				"resources": []any{"deployments", "statefulsets"},
				"verbs":     readOnlyVerbs,
			},
			map[string]any{
				"apiGroups": []any{"metrics.k8s.io"},
				"resources": []any{"pods"},
				"verbs":     readOnlyVerbs,
			},
		},
	}
	role := map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "Role",
		"metadata": map[string]any{
			"name":      "numaflow-server-secrets-role",
			"namespace": ns,
		},
		"rules": []any{
			map[string]any{
				"apiGroups":     []any{""},
				"resources":     []any{"secrets"},
				"resourceNames": []any{"numaflow-server-secrets"},
				"verbs":         []any{"get", "list", "watch", "create", "update", "patch"},
			},
		},
	}
	crb := map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRoleBinding",
		"metadata": map[string]any{
			"name": "numaflow-server-binding",
		},
		"roleRef": map[string]any{
			"apiGroup": "rbac.authorization.k8s.io",
			"kind":     "ClusterRole",
			"name":     ServerClusterRoleName,
		},
		"subjects": []any{
			map[string]any{"kind": "ServiceAccount", "name": ServerServiceAccountName, "namespace": ns},
		},
	}
	rb := map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "RoleBinding",
		"metadata": map[string]any{
			"name":      "numaflow-server-secrets-binding",
			"namespace": ns,
		},
		"roleRef": map[string]any{
			"apiGroup": "rbac.authorization.k8s.io",
			"kind":     "Role",
			"name":     "numaflow-server-secrets-role",
		},
		"subjects": []any{
			map[string]any{"kind": "ServiceAccount", "name": ServerServiceAccountName, "namespace": ns},
		},
	}
	cmdParams := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "numaflow-cmd-params-config",
			"namespace": ns,
		},
		"data": map[string]any{
			"server.disable.auth": "true",
			"server.readonly":     "true",
			"server.insecure":     "false",
		},
	}
	rbacConfig := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "numaflow-server-rbac-config",
			"namespace": ns,
		},
		"data": map[string]any{
			"rbac-conf.yaml":  "policy.default: role:readonly\npolicy.scopes: groups,email,username\n",
			"rbac-policy.csv": "p, role:admin, *, *, *\np, role:readonly, *, *, GET\n",
		},
	}
	monNS := strings.TrimSpace(in.MonitoringNamespace)
	if monNS == "" {
		monNS = "monitoring"
	}
	metricsProxy := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "numaflow-server-metrics-proxy-config",
			"namespace": ns,
		},
		"data": map[string]any{
			"config.yaml": fmt.Sprintf("url: http://prometheus.%s.svc.cluster.local:9090\npatterns: []\n", monNS),
		},
	}
	secret := namespacedObject("v1", "Secret", "numaflow-server-secrets", ns, map[string]any{"type": "Opaque"})
	svc := map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      ServerServiceName,
			"namespace": ns,
		},
		"spec": map[string]any{
			"type": "ClusterIP",
			"ports": []any{
				map[string]any{"port": 8443, "targetPort": 8443},
			},
			"selector": uxSelector(),
		},
	}
	deployment := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      ServerDeploymentName,
			"namespace": ns,
		},
		"spec": map[string]any{
			"replicas": 1,
			"selector": map[string]any{"matchLabels": uxSelector()},
			"template": map[string]any{
				"metadata": map[string]any{"labels": uxSelector()},
				"spec": map[string]any{
					"serviceAccountName": ServerServiceAccountName,
					"securityContext": map[string]any{
						"runAsNonRoot": true,
						"runAsUser":    9737,
					},
					"volumes": []any{
						map[string]any{"name": "env-volume", "emptyDir": map[string]any{}},
						map[string]any{"name": "rbac-config", "configMap": map[string]any{"name": "numaflow-server-rbac-config"}},
						map[string]any{"name": "metrics-proxy-config", "configMap": map[string]any{"name": "numaflow-server-metrics-proxy-config"}},
					},
					"initContainers": []any{
						serverInitContainer(img, ns),
						serverSecretsInitContainer(img, ns),
					},
					"containers": []any{
						serverMainContainer(img, ns),
					},
				},
			},
		},
	}
	return ManifestSet{Documents: []string{
		mustYAML(sa),
		mustYAML(clusterRole),
		mustYAML(role),
		mustYAML(crb),
		mustYAML(rb),
		mustYAML(cmdParams),
		mustYAML(rbacConfig),
		mustYAML(metricsProxy),
		mustYAML(secret),
		mustYAML(svc),
		mustYAML(deployment),
	}}, nil
}

func serverInitContainer(img, ns string) map[string]any {
	return map[string]any{
		"name": "server-init", "image": img, "imagePullPolicy": ImagePullPolicy,
		"args": []any{"server-init"},
		"env": []any{
			envFromConfig("NUMAFLOW_SERVER_BASE_HREF", "server.base.href", ns),
		},
		"volumeMounts": []any{
			map[string]any{"name": "env-volume", "mountPath": "/opt/numaflow"},
		},
	}
}

func serverSecretsInitContainer(img, ns string) map[string]any {
	return map[string]any{
		"name": "server-secrets-init", "image": img, "imagePullPolicy": ImagePullPolicy,
		"args": []any{"server-secrets-init"},
		"env": []any{
			map[string]any{"name": "NAMESPACE", "valueFrom": map[string]any{"fieldRef": map[string]any{"fieldPath": "metadata.namespace"}}},
			envFromConfig("NUMAFLOW_SERVER_DISABLE_AUTH", "server.disable.auth", ns),
		},
	}
}

func serverMainContainer(img, ns string) map[string]any {
	env := []any{
		map[string]any{"name": "NAMESPACE", "valueFrom": map[string]any{"fieldRef": map[string]any{"fieldPath": "metadata.namespace"}}},
		envFromConfig("NUMAFLOW_SERVER_INSECURE", "server.insecure", ns),
		envFromConfig("NUMAFLOW_SERVER_READONLY", "server.readonly", ns),
		envFromConfig("NUMAFLOW_SERVER_DISABLE_AUTH", "server.disable.auth", ns),
	}
	return map[string]any{
		"name": "main", "image": img, "imagePullPolicy": ImagePullPolicy,
		"args": []any{"server"},
		"env":  env,
		"ports": []any{
			map[string]any{"containerPort": 8443},
		},
		"livenessProbe": map[string]any{
			"httpGet":             map[string]any{"path": "/livez", "port": 8443, "scheme": "HTTPS"},
			"initialDelaySeconds": 3, "periodSeconds": 3,
		},
		"readinessProbe": map[string]any{
			"httpGet":             map[string]any{"path": "/livez", "port": 8443, "scheme": "HTTPS"},
			"initialDelaySeconds": 3, "periodSeconds": 3,
		},
		"volumeMounts": []any{
			map[string]any{"name": "env-volume", "mountPath": "/ui/build/runtime-env.js", "subPath": "runtime-env.js"},
			map[string]any{"name": "env-volume", "mountPath": "/ui/build/index.html", "subPath": "index.html"},
			map[string]any{"name": "rbac-config", "mountPath": "/etc/numaflow"},
			map[string]any{"name": "metrics-proxy-config", "mountPath": "/etc/numaflow/metrics-proxy"},
		},
		"resources": map[string]any{
			"requests": map[string]any{"cpu": "100m", "memory": "200Mi"},
			"limits":   map[string]any{"cpu": "500m", "memory": "1024Mi"},
		},
	}
}

func envFromConfig(envName, key, ns string) map[string]any {
	return map[string]any{
		"name": envName,
		"valueFrom": map[string]any{
			"configMapKeyRef": map[string]any{
				"name": "numaflow-cmd-params-config", "key": key, "optional": true,
			},
		},
	}
}

func uxSelector() map[string]any {
	return map[string]any{
		"app.kubernetes.io/component": "numaflow-ux",
		"app.kubernetes.io/name":      "numaflow-ux",
		"app.kubernetes.io/part-of":   "numaflow",
	}
}

// RenderPrometheus produces monitoring-namespace Prometheus with PVC and cAdvisor/numaflow scrapes.
func RenderPrometheus(in PrometheusRenderInput) (ManifestSet, error) {
	ns := strings.TrimSpace(in.Namespace)
	if ns == "" {
		return ManifestSet{}, errors.New("namespace is required")
	}
	pvcSize := in.PVCSize
	if pvcSize == "" {
		pvcSize = "10Gi"
	}
	promConfig := fmt.Sprintf(`global:
  scrape_interval: 10s
scrape_configs:
  - job_name: cadvisor
    scheme: https
    tls_config:
      insecure_skip_verify: true
    bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
    kubernetes_sd_configs:
      - role: node
    relabel_configs:
      - action: labelmap
        regex: __meta_kubernetes_node_label_(.+)
      - target_label: __address__
        replacement: kubernetes.default.svc:443
      - source_labels: [__meta_kubernetes_node_name]
        regex: (.+)
        target_label: __metrics_path__
        replacement: /api/v1/nodes/${1}/proxy/metrics/cadvisor
  - job_name: numaflow-pods
    scheme: https
    tls_config:
      insecure_skip_verify: true
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_numaflow_numaproj_io_pipeline_name, __meta_kubernetes_pod_label_numaflow_numaproj_io_mono_vertex_name]
        regex: (.+)|(.+)
        action: keep
      - source_labels: [__meta_kubernetes_pod_container_port_name]
        regex: metrics
        action: keep
      - source_labels: [__meta_kubernetes_namespace]
        target_label: namespace
      - source_labels: [__meta_kubernetes_pod_label_perfman_numaproj_io_run_id]
        target_label: run_id
      - source_labels: [__meta_kubernetes_pod_label_perfman_numaproj_io_scenario]
        target_label: scenario
      - source_labels: [__meta_kubernetes_pod_label_perfman_numaproj_io_numaflow_image]
        target_label: numaflow_image
      - source_labels: [__meta_kubernetes_pod_name]
        target_label: pod
`)
	sa := namespacedObject("v1", "ServiceAccount", "prometheus", ns, nil)
	clusterRole := map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRole",
		"metadata":   map[string]any{"name": "prometheus"},
		"rules": []any{
			map[string]any{
				"apiGroups": []any{""},
				"resources": []any{"nodes", "nodes/metrics", "nodes/proxy", "services", "endpoints", "pods"},
				"verbs":     readOnlyVerbs,
			},
			map[string]any{
				"nonResourceURLs": []any{"/metrics", "/metrics/cadvisor"},
				"verbs":           []any{"get"},
			},
		},
	}
	crb := map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRoleBinding",
		"metadata":   map[string]any{"name": "prometheus"},
		"roleRef": map[string]any{
			"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "prometheus",
		},
		"subjects": []any{
			map[string]any{"kind": "ServiceAccount", "name": "prometheus", "namespace": ns},
		},
	}
	cm := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "prometheus-config", "namespace": ns},
		"data":       map[string]any{"prometheus.yml": promConfig},
	}
	pvc := map[string]any{
		"apiVersion": "v1",
		"kind":       "PersistentVolumeClaim",
		"metadata":   map[string]any{"name": "prometheus-data", "namespace": ns},
		"spec": map[string]any{
			"accessModes": []any{"ReadWriteOnce"},
			"resources":   map[string]any{"requests": map[string]any{"storage": pvcSize}},
		},
	}
	deployment := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": PrometheusDeploymentName, "namespace": ns},
		"spec": map[string]any{
			"replicas": 1,
			"selector": map[string]any{"matchLabels": map[string]any{"app": "prometheus"}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": "prometheus"}},
				"spec": map[string]any{
					"serviceAccountName": "prometheus",
					"containers": []any{
						map[string]any{
							"name":  "prometheus",
							"image": "prom/prometheus:v2.54.1",
							"args":  []any{"--config.file=/etc/prometheus/prometheus.yml", "--storage.tsdb.path=/prometheus"},
							"ports": []any{map[string]any{"containerPort": 9090}},
							"volumeMounts": []any{
								map[string]any{"name": "config", "mountPath": "/etc/prometheus"},
								map[string]any{"name": "data", "mountPath": "/prometheus"},
							},
						},
					},
					"volumes": []any{
						map[string]any{"name": "config", "configMap": map[string]any{"name": "prometheus-config"}},
						map[string]any{"name": "data", "persistentVolumeClaim": map[string]any{"claimName": "prometheus-data"}},
					},
				},
			},
		},
	}
	svc := map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]any{"name": "prometheus", "namespace": ns},
		"spec": map[string]any{
			"type":     "ClusterIP",
			"selector": map[string]any{"app": "prometheus"},
			"ports":    []any{map[string]any{"port": 9090, "targetPort": 9090}},
		},
	}
	return ManifestSet{Documents: []string{
		mustYAML(sa), mustYAML(clusterRole), mustYAML(crb), mustYAML(cm), mustYAML(pvc), mustYAML(deployment), mustYAML(svc),
	}}, nil
}

// RenderValidationPostgres produces a StatefulSet-backed Postgres for validation runs.
func RenderValidationPostgres(in PostgresRenderInput) (ManifestSet, error) {
	ns := strings.TrimSpace(in.Namespace)
	if ns == "" {
		return ManifestSet{}, errors.New("namespace is required")
	}
	user, pass, db := in.PostgresUser, in.PostgresPassword, in.PostgresDatabase
	if user == "" {
		user = "numaflow"
	}
	if pass == "" {
		pass = "numaflow"
	}
	if db == "" {
		db = "postgres"
	}
	pvcSize := in.PVCSize
	if pvcSize == "" {
		pvcSize = "5Gi"
	}
	secret := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": PostgresSecretName, "namespace": ns},
		"type":       "Opaque",
		"stringData": map[string]any{
			"POSTGRES_USER":     user,
			"POSTGRES_PASSWORD": pass,
			"POSTGRES_DB":       db,
		},
	}
	svc := map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]any{"name": PostgresServiceName, "namespace": ns},
		"spec": map[string]any{
			"type":     "ClusterIP",
			"selector": map[string]any{"app": PostgresStatefulSetName},
			"ports":    []any{map[string]any{"port": 5432, "targetPort": 5432}},
		},
	}
	volumeClaim := map[string]any{
		"metadata": map[string]any{"name": "pg-data"},
		"spec": map[string]any{
			"accessModes": []any{"ReadWriteOnce"},
			"resources":   map[string]any{"requests": map[string]any{"storage": pvcSize}},
		},
	}
	if sc := strings.TrimSpace(in.StorageClassName); sc != "" {
		volumeClaim["spec"].(map[string]any)["storageClassName"] = sc
	}
	sts := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "StatefulSet",
		"metadata":   map[string]any{"name": PostgresStatefulSetName, "namespace": ns},
		"spec": map[string]any{
			"serviceName": PostgresServiceName,
			"replicas":    1,
			"selector":    map[string]any{"matchLabels": map[string]any{"app": PostgresStatefulSetName}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": PostgresStatefulSetName}},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":  "postgres",
							"image": "postgres:18",
							"ports": []any{map[string]any{"containerPort": 5432}},
							"env": []any{
								map[string]any{"name": "POSTGRES_USER", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": PostgresSecretName, "key": "POSTGRES_USER"}}},
								map[string]any{"name": "POSTGRES_PASSWORD", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": PostgresSecretName, "key": "POSTGRES_PASSWORD"}}},
								map[string]any{"name": "POSTGRES_DB", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": PostgresSecretName, "key": "POSTGRES_DB"}}},
								map[string]any{"name": "PGDATA", "value": "/var/lib/postgresql/data/pgdata"},
							},
							"volumeMounts": []any{
								map[string]any{"name": "pg-data", "mountPath": "/var/lib/postgresql/data"},
							},
							"livenessProbe": map[string]any{
								"exec":                map[string]any{"command": []any{"pg_isready", "-U", user}},
								"initialDelaySeconds": 30, "periodSeconds": 10,
							},
							"readinessProbe": map[string]any{
								"exec":                map[string]any{"command": []any{"pg_isready", "-U", user}},
								"initialDelaySeconds": 5, "periodSeconds": 5,
							},
							"resources": map[string]any{
								"requests": map[string]any{"cpu": "250m", "memory": "1Gi"},
							},
						},
					},
				},
			},
			"volumeClaimTemplates": []any{volumeClaim},
		},
	}
	return ManifestSet{Documents: []string{mustYAML(secret), mustYAML(svc), mustYAML(sts)}}, nil
}

func referencesDex(yaml string) bool {
	lower := strings.ToLower(yaml)
	return strings.Contains(lower, "numaflow-dex") ||
		strings.Contains(lower, "server.dex") ||
		strings.Contains(lower, "dex.server") ||
		strings.Contains(lower, "dex_server")
}

// AssertBundlePolicy validates rendered central manifests for harness policy.
func AssertBundlePolicy(name, yaml string, uiImage string) error {
	if strings.Contains(yaml, "gp3") {
		return fmt.Errorf("%s: must not reference gp3 storage class", name)
	}
	if strings.Contains(yaml, "numa-perfman") {
		return fmt.Errorf("%s: must not deploy perfman workload", name)
	}
	if name != "prometheus" && strings.Contains(yaml, "perfman") && strings.Contains(yaml, "kind: Deployment") {
		return fmt.Errorf("%s: must not deploy perfman workload", name)
	}
	if referencesDex(yaml) {
		return fmt.Errorf("%s: must not reference Dex", name)
	}
	if uiImage != "" && name == "server" && !strings.Contains(yaml, uiImage) {
		return fmt.Errorf("server: missing exact UI image %q", uiImage)
	}
	return nil
}

func namespacedObject(apiVersion, kind, name, ns string, extra map[string]any) map[string]any {
	obj := map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]any{"name": name, "namespace": ns},
	}
	for k, v := range extra {
		obj[k] = v
	}
	return obj
}

func mustYAML(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, data, "", "  "); err != nil {
		panic(err)
	}
	return pretty.String()
}
