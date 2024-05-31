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

func CreateResource(filename string, dynamicClient *dynamic.DynamicClient, gvr schema.GroupVersionResource, namespace string, log *zap.Logger) error {
	obj, err := readYamlFile(filename)
	if err != nil {
		return fmt.Errorf("failed to retrieve configuration information: %w", err)
	}

	// Check if resource already exists
	existing, err := dynamicClient.Resource(gvr).Namespace(namespace).Get(context.TODO(), obj.GetName(), metav1.GetOptions{})
	if err == nil {
		log.Info("Resource already exists, skipping creation", zap.String("resource-name", existing.GetName()))
		return nil
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check if resource exists: %w", err)
	}

	result, err := dynamicClient.Resource(gvr).Namespace(namespace).Create(context.TODO(), obj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	log.Info("Applied resource", zap.String("resource-name", result.GetName()))
	return nil
}

func DeleteResourcesWithLabel(dynamicClient *dynamic.DynamicClient, gvr schema.GroupVersionResource, namespace string, labelKey string, labelVal string, log *zap.Logger) error {
	labelSelector := labelKey + "=" + labelVal

	resources, err := dynamicClient.Resource(gvr).Namespace(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return fmt.Errorf("could not list resources: %w", err)
	}

	for _, r := range resources.Items {
		err = dynamicClient.Resource(gvr).Namespace(r.GetNamespace()).Delete(context.TODO(), r.GetName(), metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("could not delete resource %s: %w", r.GetName(), err)
		}

		log.Info("Successfully deleted resource", zap.String("resource-name", r.GetName()))
	}

	return nil
}
