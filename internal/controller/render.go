package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	DeploymentName      = "numaflow-controller"
	ServiceAccountName  = "numaflow-sa"
	RoleName            = "numaflow-role"
	RoleBindingName     = "numaflow-role-binding"
	ConfigMapName       = "numaflow-cmd-params-config"
	ControllerConfigMap = "numaflow-controller-config"
	ImagePullPolicy     = "IfNotPresent"
)

var (
	ErrInvalidImageRef = errors.New("invalid OCI image reference: must be a complete reference, not a bare tag")
)

var (
	bareTagPattern    = regexp.MustCompile(`^v[0-9][a-zA-Z0-9._-]*$`)
	localImagePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*:[a-zA-Z0-9][a-zA-Z0-9._-]+$`)
)

// ValidateImageReference ensures ref is a complete OCI reference without rewriting it.
func ValidateImageReference(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("%w: empty", ErrInvalidImageRef)
	}
	if bareTagPattern.MatchString(ref) {
		return fmt.Errorf("%w: %q", ErrInvalidImageRef, ref)
	}
	if strings.Contains(ref, "@") {
		return nil
	}
	if strings.Contains(ref, "/") {
		return nil
	}
	if localImagePattern.MatchString(ref) {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrInvalidImageRef, ref)
}

// RenderInput carries namespace-scoped controller parameters.
type RenderInput struct {
	Namespace      string
	ImageReference string
	RunID          string
	ScenarioID     string
}

// ManifestSet contains YAML documents for one controller install.
type ManifestSet struct {
	ServiceAccount   string
	Role             string
	RoleBinding      string
	CmdParamsConfig  string
	ControllerConfig string
	Deployment       string
}

// Render produces YAML-safe manifests for namespace-scoped controller installation.
func Render(in RenderInput) (ManifestSet, error) {
	if err := ValidateImageReference(in.ImageReference); err != nil {
		return ManifestSet{}, err
	}
	if strings.TrimSpace(in.Namespace) == "" {
		return ManifestSet{}, errors.New("namespace is required")
	}
	sa := map[string]any{
		"apiVersion": "v1",
		"kind":       "ServiceAccount",
		"metadata": map[string]any{
			"name":      ServiceAccountName,
			"namespace": in.Namespace,
		},
	}
	role := map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "Role",
		"metadata": map[string]any{
			"name":      RoleName,
			"namespace": in.Namespace,
		},
		"rules": []any{
			map[string]any{"apiGroups": []any{"*"}, "resources": []any{"*"}, "verbs": []any{"*"}},
		},
	}
	roleBinding := map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "RoleBinding",
		"metadata": map[string]any{
			"name":      RoleBindingName,
			"namespace": in.Namespace,
		},
		"roleRef": map[string]any{
			"apiGroup": "rbac.authorization.k8s.io",
			"kind":     "Role",
			"name":     RoleName,
		},
		"subjects": []any{
			map[string]any{"kind": "ServiceAccount", "name": ServiceAccountName, "namespace": in.Namespace},
		},
	}
	cmdParams := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      ConfigMapName,
			"namespace": in.Namespace,
		},
		"data": map[string]any{
			"namespaced":                          "true",
			"managed.namespace":                   in.Namespace,
			"controller.leader.election.disabled": "true",
		},
	}
	controllerCfg := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      ControllerConfigMap,
			"namespace": in.Namespace,
		},
		"data": map[string]any{
			"controller-config.yaml": controllerConfigData,
		},
	}
	podLabels := map[string]any{
		"app.kubernetes.io/component": "controller-manager",
		"app.kubernetes.io/name":      "controller-manager",
		"app.kubernetes.io/part-of":   "numaflow",
	}
	if id := strings.TrimSpace(in.RunID); id != "" {
		podLabels["perfman.numaproj.io/run-id"] = id
	}
	if sc := strings.TrimSpace(in.ScenarioID); sc != "" {
		podLabels["perfman.numaproj.io/scenario"] = sc
	}
	if img := strings.TrimSpace(in.ImageReference); img != "" {
		digest := sha256.Sum256([]byte(img))
		podLabels["perfman.numaproj.io/numaflow-image"] = fmt.Sprintf("ref-%x", digest[:8])
	}
	podAnnotations := map[string]any{}
	if img := strings.TrimSpace(in.ImageReference); img != "" {
		podAnnotations["perfman.numaproj.io/numaflow-image-ref"] = img
	}
	deployment := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      DeploymentName,
			"namespace": in.Namespace,
		},
		"spec": map[string]any{
			"replicas": 1,
			"selector": map[string]any{
				"matchLabels": map[string]any{
					"app.kubernetes.io/component": "controller-manager",
					"app.kubernetes.io/name":      "controller-manager",
					"app.kubernetes.io/part-of":   "numaflow",
				},
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": podAnnotations,
					"labels":      podLabels,
				},
				"spec": map[string]any{
					"serviceAccountName": ServiceAccountName,
					"securityContext": map[string]any{
						"runAsNonRoot": true,
						"runAsUser":    9737,
					},
					"volumes": []any{
						map[string]any{
							"name": "controller-config-volume",
							"configMap": map[string]any{
								"name": ControllerConfigMap,
							},
						},
					},
					"containers": []any{
						map[string]any{
							"name":            "controller-manager",
							"image":           in.ImageReference,
							"args":            []any{"controller"},
							"imagePullPolicy": ImagePullPolicy,
							"env": []any{
								map[string]any{"name": "NUMAFLOW_IMAGE", "value": in.ImageReference},
								map[string]any{
									"name": "NAMESPACE",
									"valueFrom": map[string]any{
										"fieldRef": map[string]any{"fieldPath": "metadata.namespace"},
									},
								},
								map[string]any{
									"name": "NUMAFLOW_CONTROLLER_NAMESPACED",
									"valueFrom": map[string]any{
										"configMapKeyRef": map[string]any{
											"name": ConfigMapName, "key": "namespaced", "optional": true,
										},
									},
								},
								map[string]any{
									"name": "NUMAFLOW_CONTROLLER_MANAGED_NAMESPACE",
									"valueFrom": map[string]any{
										"configMapKeyRef": map[string]any{
											"name": ConfigMapName, "key": "managed.namespace", "optional": true,
										},
									},
								},
								map[string]any{
									"name": "NUMAFLOW_LEADER_ELECTION_DISABLED",
									"valueFrom": map[string]any{
										"configMapKeyRef": map[string]any{
											"name": ConfigMapName, "key": "controller.leader.election.disabled", "optional": true,
										},
									},
								},
							},
							"ports": []any{
								map[string]any{"name": "metrics", "containerPort": 9090},
							},
							"livenessProbe": map[string]any{
								"httpGet":             map[string]any{"path": "/healthz", "port": 8081},
								"initialDelaySeconds": 3,
								"periodSeconds":       3,
							},
							"readinessProbe": map[string]any{
								"httpGet":             map[string]any{"path": "/readyz", "port": 8081},
								"initialDelaySeconds": 3,
								"periodSeconds":       3,
							},
							"volumeMounts": []any{
								map[string]any{"name": "controller-config-volume", "mountPath": "/etc/numaflow"},
							},
							"resources": map[string]any{
								"requests": map[string]any{"cpu": "100m", "memory": "200Mi"},
								"limits":   map[string]any{"cpu": "500m", "memory": "1024Mi"},
							},
						},
					},
				},
			},
		},
	}
	return ManifestSet{
		ServiceAccount:   mustYAML(sa),
		Role:             mustYAML(role),
		RoleBinding:      mustYAML(roleBinding),
		CmdParamsConfig:  mustYAML(cmdParams),
		ControllerConfig: mustYAML(controllerCfg),
		Deployment:       mustYAML(deployment),
	}, nil
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
	// JSON structure is YAML-compatible for these manifests.
	return pretty.String()
}

// ApplyDocuments returns manifests in the order required for kubectl apply.
func (m ManifestSet) ApplyDocuments() []string {
	return []string{
		m.ServiceAccount,
		m.Role,
		m.RoleBinding,
		m.CmdParamsConfig,
		m.ControllerConfig,
		m.Deployment,
	}
}

// Concat returns all manifests separated by YAML document markers.
func (m ManifestSet) Concat() string {
	documents := m.ApplyDocuments()
	if len(documents) == 0 {
		return ""
	}
	return strings.Join(documents, "\n---\n") + "\n"
}
