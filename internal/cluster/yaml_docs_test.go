package cluster_test

import (
	"strings"
	"testing"

	"numa-perfman/internal/cluster"
)

func TestFilterCRDsSkipsNonCRD(t *testing.T) {
	docs := cluster.SplitYAMLDocuments([]byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: pipelines.numaflow.numaproj.io
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: numaflow-sa
`))
	out, err := cluster.FilterCRDDocuments(docs, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || cluster.DocumentKind(out[0]) != "CustomResourceDefinition" {
		t.Fatalf("got %d docs", len(out))
	}
}

func TestFilterCRDsStrictRejectsNonCRD(t *testing.T) {
	docs := cluster.SplitYAMLDocuments([]byte(`apiVersion: v1
kind: Namespace
metadata:
  name: x
`))
	_, err := cluster.FilterCRDDocuments(docs, true)
	if err == nil || !strings.Contains(err.Error(), "reject non-CRD") {
		t.Fatalf("expected strict error, got %v", err)
	}
}
