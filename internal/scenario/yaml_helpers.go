package scenario

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"numa-perfman/internal/central"
)

func yamlQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Sprintf("%q", s)
	}
	return string(b)
}

func postgresServiceHost(validationNS string) string {
	return fmt.Sprintf("%s.%s.svc", central.PostgresServiceName, validationNS)
}

func postgresWrapperDSN(dbName, validationNS string) string {
	host := postgresServiceHost(validationNS)
	return fmt.Sprintf("postgres://numaflow:numaflow@%s:5432/%s", host, dbName)
}

const (
	labelManagedBy     = "perfman.numaproj.io/managed-by"
	labelScenario      = "perfman.numaproj.io/scenario"
	labelRunID         = "perfman.numaproj.io/run-id"
	labelNumaflowImage = "perfman.numaproj.io/numaflow-image"
	annotationImageRef = "perfman.numaproj.io/numaflow-image-ref"
	managedByValue     = "numaflow-perfman"
)

func (in RenderInput) resourceLabels(scenarioID string) string {
	return resourceLabels(scenarioID, in.RunID, in.NumaflowImage)
}

func resourceLabels(scenarioID, runID, numaflowImage string) string {
	var b strings.Builder
	b.WriteString("  labels:\n")
	b.WriteString(fmt.Sprintf("    %s: %s\n", labelManagedBy, managedByValue))
	b.WriteString(fmt.Sprintf("    %s: %q\n", labelScenario, scenarioID))
	if runID != "" {
		b.WriteString(fmt.Sprintf("    %s: %q\n", labelRunID, runID))
	}
	if numaflowImage != "" {
		digest := sha256.Sum256([]byte(numaflowImage))
		b.WriteString(fmt.Sprintf("    %s: %q\n", labelNumaflowImage, fmt.Sprintf("ref-%x", digest[:8])))
		b.WriteString("  annotations:\n")
		b.WriteString(fmt.Sprintf("    %s: %s\n", annotationImageRef, yamlQuote(numaflowImage)))
	}
	return b.String()
}

// workloadMetadata copies run labels into Numaflow workload metadata so the
// controller propagates them to data-plane pods. Full image references remain
// annotations on the parent resource because they are not valid label values.
func workloadMetadata(resourceMetadata string, indent int) string {
	labelBlock := resourceMetadata
	if idx := strings.Index(labelBlock, "  annotations:\n"); idx >= 0 {
		labelBlock = labelBlock[:idx]
	}
	prefix := strings.Repeat(" ", indent)
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString("metadata:\n")
	for _, line := range strings.Split(strings.TrimSuffix(labelBlock, "\n"), "\n") {
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func udfResourcesBlock() string {
	return `            resources:
              requests:
                cpu: 300m
                memory: 500Mi
`
}

func benchmarkMapResourcesBlock() string {
	return `            resources:
              requests:
                cpu: 1
                memory: 500Mi
              limits:
                cpu: 1
                memory: 500Mi
`
}

func podNameEnvBlock(indent int) string {
	prefix := strings.Repeat(" ", indent)
	return fmt.Sprintf(`%s- name: POD_NAME
%s  valueFrom:
%s    fieldRef:
%s      fieldPath: metadata.name
`, prefix, prefix, prefix, prefix)
}

func postgresInlineEnv(dbName, validationNS string) string {
	host := yamlQuote(postgresServiceHost(validationNS))
	db := yamlQuote(dbName)
	return fmt.Sprintf(`            env:
              - name: POSTGRES_HOST
                value: %s
              - name: POSTGRES_PORT
                value: "5432"
              - name: POSTGRES_DB
                value: %s
              - name: POSTGRES_USER
                valueFrom:
                  secretKeyRef:
                    name: %s
                    key: POSTGRES_USER
              - name: POSTGRES_PASSWORD
                valueFrom:
                  secretKeyRef:
                    name: %s
                    key: POSTGRES_PASSWORD
`, host, db, central.PostgresSecretName, central.PostgresSecretName)
}

func vertexContainerTemplateEnv(dsn, labels string) string {
	return fmt.Sprintf(`%s      containerTemplate:
        env:
          - name: NUMAFLOW_WRAPPER_PG_DSN
            value: %s
%s`, workloadMetadata(labels, 6), yamlQuote(dsn), podNameEnvBlock(10))
}

func udfContainer(image, arg string, bench bool) string {
	resources := udfResourcesBlock()
	if bench {
		resources = benchmarkMapResourcesBlock()
	}
	return fmt.Sprintf(`            image: %s
            imagePullPolicy: Never
%s            args:
              - %s
`, image, resources, yamlQuote(arg))
}

func udfContainerWithEnv(image, arg string, extraEnv string) string {
	return fmt.Sprintf(`            image: %s
            imagePullPolicy: Never
%s            args:
              - %s
%s`, image, udfResourcesBlock(), yamlQuote(arg), extraEnv)
}
