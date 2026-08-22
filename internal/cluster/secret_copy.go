package cluster

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// SecretStringData reads a Secret's data map as decoded string values.
func SecretStringData(resourceJSON string) (map[string]string, error) {
	var obj struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(resourceJSON), &obj); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(obj.Data))
	for k, v := range obj.Data {
		if v == "" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, fmt.Errorf("decode secret key %q: %w", k, err)
		}
		out[k] = string(b)
	}
	return out, nil
}

// RenderOpaqueSecretYAML builds a namespaced Secret manifest using stringData.
func RenderOpaqueSecretYAML(name, ns string, stringData map[string]string) ([]byte, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(ns) == "" {
		return nil, fmt.Errorf("secret name and namespace required")
	}
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      name,
			"namespace": ns,
		},
		"type":       "Opaque",
		"stringData": stringData,
	}
	return jsonSecretYAML(obj)
}

func jsonSecretYAML(obj map[string]any) ([]byte, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	// JSON structure is YAML-compatible for these manifests.
	return data, nil
}

// CopySecretToNamespace reads secret/name from srcNS and applies equivalent stringData to dstNS.
func (c Client) CopySecretToNamespace(ctx context.Context, srcNS, dstNS, name string) error {
	raw, err := c.GetResourceJSON(ctx, srcNS, "secret/"+name)
	if err != nil {
		return fmt.Errorf("read secret %q from %s: %w", name, srcNS, err)
	}
	stringData, err := SecretStringData(raw)
	if err != nil {
		return err
	}
	if len(stringData) == 0 {
		return fmt.Errorf("secret %q in %s has no data", name, srcNS)
	}
	yaml, err := RenderOpaqueSecretYAML(name, dstNS, stringData)
	if err != nil {
		return err
	}
	if _, err := c.ApplyYAML(ctx, dstNS, yaml); err != nil {
		return fmt.Errorf("apply secret %q to %s: %w", name, dstNS, err)
	}
	return nil
}
