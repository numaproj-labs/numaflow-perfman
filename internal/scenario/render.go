package scenario

import (
	"fmt"
)

func renderValidation(in RenderInput) (ManifestBundle, error) {
	if _, err := LookupValidation(in.ScenarioID); err != nil {
		return ManifestBundle{}, err
	}
	if in.ScenarioID == "monovertex" {
		return ManifestBundle{MonoVertex: renderValidationMonoVertex(in)}, nil
	}
	labels := in.resourceLabels(in.ScenarioID)
	var pipeline string
	switch in.ScenarioID {
	case "map":
		pipeline = renderMapValidationPipeline(in.Namespace, in.UDFImage, in.PostgresDBName, in.ValidationNS, labels)
	case "reduce":
		pipeline = renderReduceValidationPipeline(in.Namespace, in.UDFImage, in.PostgresDBName, in.ValidationNS, false, labels)
	case "sliding-reduce":
		pipeline = renderReduceValidationPipeline(in.Namespace, in.UDFImage, in.PostgresDBName, in.ValidationNS, true, labels)
	default:
		return ManifestBundle{}, fmt.Errorf("unsupported validation scenario %q", in.ScenarioID)
	}
	isb := renderISB(in.ScenarioID, in.Namespace, labels)
	return ManifestBundle{ISB: isb, Pipeline: pipeline}, nil
}

func renderISB(name, ns, labels string) string {
	return fmt.Sprintf(`apiVersion: numaflow.numaproj.io/v1alpha1
kind: InterStepBufferService
metadata:
  name: %s
  namespace: %s
%sspec:
  jetstream:
    version: latest
    replicas: 1
    persistence:
      accessMode: ReadWriteOnce
      storageClassName: standard
      volumeSize: 1Gi
`, name, ns, labels)
}

func renderMapValidationPipeline(ns, udfImage, dbName, validationNS, labels string) string {
	dsn := postgresWrapperDSN(dbName, validationNS)
	name := "map"
	srcEnv := postgresInlineEnv(dbName, validationNS)
	return fmt.Sprintf(`apiVersion: numaflow.numaproj.io/v1alpha1
kind: Pipeline
metadata:
  name: %s
  namespace: %s
%sspec:
  interStepBufferServiceName: %s
  limits:
    readBatchSize: 500
  vertices:
    - name: source
%s      scale:
        min: 1
        max: 1
      source:
        udsource:
          container:
%s
        transformer:
          container:
%s
    - name: unary-map
%s      scale:
        min: 1
        max: 3
      udf:
        container:
%s
    - name: batch-map
%s      scale:
        min: 1
        max: 3
      udf:
        container:
%s
    - name: stream-map
%s      scale:
        min: 1
        max: 3
      udf:
        container:
%s
    - name: sink
%s      scale:
        min: 1
        max: 2
      sink:
        udsink:
          container:
%s
  edges:
    - from: source
      to: unary-map
      conditions:
        tags:
          operator: or
          values:
            - "unary-map"
    - from: source
      to: batch-map
      conditions:
        tags:
          operator: or
          values:
            - "batch-map"
    - from: source
      to: stream-map
      conditions:
        tags:
          operator: or
          values:
            - "stream-map"
    - from: unary-map
      to: sink
    - from: batch-map
      to: sink
    - from: stream-map
      to: sink
`, name, ns, labels, name,
		vertexContainerTemplateEnv(dsn, labels),
		udfContainerWithEnv(udfImage, "--source", srcEnv),
		udfContainer(udfImage, "--sourcetransformer", false),
		vertexContainerTemplateEnv(dsn, labels),
		udfContainer(udfImage, "--unarymap", false),
		vertexContainerTemplateEnv(dsn, labels),
		udfContainer(udfImage, "--batchmap", false),
		vertexContainerTemplateEnv(dsn, labels),
		udfContainer(udfImage, "--streammap", false),
		vertexContainerTemplateEnv(dsn, labels),
		udfContainerWithEnv(udfImage, "--sink", srcEnv))
}

func renderReduceValidationPipeline(ns, udfImage, dbName, validationNS string, sliding bool, labels string) string {
	dsn := postgresWrapperDSN(dbName, validationNS)
	srcEnv := postgresInlineEnv(dbName, validationNS)
	pipelineName := "reduce"
	reduceVertex := "fixed-window-reduce"
	windowYAML := `            fixed:
              length: 60s`
	processedBy := "fixed-window-reduce"
	readBatch := 500
	if sliding {
		pipelineName = "sliding-reduce"
		reduceVertex = "sliding-window-reduce"
		windowYAML = `            sliding:
              length: 60s
              slide: 10s`
		processedBy = "sliding-window-reduce"
		readBatch = 250
	}
	sinkExtraEnv := fmt.Sprintf(`              - name: PROCESSED_BY
                value: %s
`, yamlQuote(processedBy))
	sinkEnv := postgresInlineEnv(dbName, validationNS) + sinkExtraEnv
	return fmt.Sprintf(`apiVersion: numaflow.numaproj.io/v1alpha1
kind: Pipeline
metadata:
  name: %s
  namespace: %s
%sspec:
  interStepBufferServiceName: %s
  watermark:
    idleSource:
      threshold: 5s
      incrementBy: 3s
      stepInterval: 2s
      initSourceDelay: 60s
  limits:
    readBatchSize: %d
  vertices:
    - name: source
%s      scale:
        min: 1
        max: 1
      source:
        udsource:
          container:
%s
    - name: %s
      partitions: 5
%s      udf:
        container:
%s
        groupBy:
          window:
%s
          keyed: true
          storage:
            persistentVolumeClaim:
              volumeSize: 10Gi
              accessMode: ReadWriteOnce
    - name: sink
%s      scale:
        min: 1
        max: 2
      sink:
        udsink:
          container:
%s
  edges:
    - from: source
      to: %s
    - from: %s
      to: sink
`, pipelineName, ns, labels, pipelineName, readBatch,
		vertexContainerTemplateEnv(dsn, labels),
		udfContainerWithEnv(udfImage, "--reduce-source", srcEnv),
		reduceVertex,
		vertexContainerTemplateEnv(dsn, labels),
		udfContainer(udfImage, "--reduce", false),
		windowYAML,
		vertexContainerTemplateEnv(dsn, labels),
		udfContainerWithEnv(udfImage, "--reduce-sink", sinkEnv),
		reduceVertex, reduceVertex)
}

func renderValidationMonoVertex(in RenderInput) string {
	dsn := postgresWrapperDSN(in.PostgresDBName, in.ValidationNS)
	srcEnv := postgresInlineEnv(in.PostgresDBName, in.ValidationNS)
	sinkEnv := postgresInlineEnv(in.PostgresDBName, in.ValidationNS)
	name := "monovertex"
	labels := in.resourceLabels(name)
	return fmt.Sprintf(`apiVersion: numaflow.numaproj.io/v1alpha1
kind: MonoVertex
metadata:
  name: %s
  namespace: %s
%sspec:
%s
  scale:
    min: 1
    max: 1
  limits:
    readBatchSize: 500
    readTimeout: 5s
  containerTemplate:
    resources:
      requests:
        cpu: 500m
        memory: 1Gi
      limits:
        cpu: 500m
        memory: 1Gi
    env:
      - name: NUMAFLOW_WRAPPER_PG_DSN
        value: %s
%s  bypass:
    fallback:
      tags:
        operator: or
        values:
          - "bypass-to-fallback"
    onSuccess:
      tags:
        operator: or
        values:
          - "bypass-to-onsuccess"
  source:
    udsource:
      container:
%s
    transformer:
      container:
%s
  udf:
    container:
%s
  sink:
    udsink:
      container:
%s
    fallback:
      udsink:
        container:
%s
    onSuccess:
      udsink:
        container:
%s
`, name, in.Namespace, labels, workloadMetadata(labels, 2), yamlQuote(dsn), podNameEnvBlock(6),
		udfContainerWithEnv(in.UDFImage, "--monovertex-source", srcEnv),
		udfContainer(in.UDFImage, "--monovertex-transformer", false),
		udfContainer(in.UDFImage, "--monovertex-map", false),
		udfContainerWithEnv(in.UDFImage, "--monovertex-sink", sinkEnv),
		udfContainerWithEnv(in.UDFImage, "--monovertex-fallback-sink", sinkEnv),
		udfContainerWithEnv(in.UDFImage, "--monovertex-onsuccess-sink", sinkEnv))
}
