package util

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

type GVRObject struct {
	Group     string
	Version   string
	Resource  string
	Namespace string
}

func readYamlFile(filename string) (*unstructured.Unstructured, error) {
	yamlFile, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read yaml file: %w", err)
	}

	var obj unstructured.Unstructured
	if err := yaml.Unmarshal(yamlFile, &obj.Object); err != nil {
		return nil, fmt.Errorf("failed to unmasrhsal into object: %w", err)
	}

	return &obj, nil
}

func (gvro *GVRObject) CreateResource(filename string, dynamicClient *dynamic.DynamicClient, logger *zap.Logger) error {
	obj, err := readYamlFile(filename)
	if err != nil {
		return fmt.Errorf("failed to retrieve configuration information: %w", err)
	}

	gvr := schema.GroupVersionResource{Group: gvro.Group, Version: gvro.Version, Resource: gvro.Resource}
	resourceInterface := dynamicClient.Resource(gvr).Namespace(gvro.Namespace)

	// Check if resource already exists
	existing, err := resourceInterface.Get(context.TODO(), obj.GetName(), metav1.GetOptions{})
	if err == nil {
		logger.Info("Resource already exists, skipping creation", zap.String("resource-name", existing.GetName()))
		return nil
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check if resource exists: %w", err)
	}

	result, err := dynamicClient.Resource(gvr).Namespace(gvro.Namespace).Create(context.TODO(), obj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	logger.Info("Applied resource", zap.String("resource-name", result.GetName()))
	return nil
}
