package controller

// controllerConfigData is the numaflow-controller-config controller-config.yaml payload
// (trimmed from upstream install; stream replicas lowered for single-node kind).
const controllerConfigData = `instance: ""
defaults:
  containerResources: |
    requests:
      memory: "128Mi"
      cpu: "100m"
isbsvc:
  jetstream:
    settings: |
      max_payload: 1048576
      max_memory_store: -1
      max_file_store: 1TB
    bufferConfig: |
      stream:
        retention: 0
        maxMsgs: 100000
        maxAge: 72h
        maxBytes: -1
        storage: 0
        replicas: 1
        duplicates: 60s
      consumer:
        ackWait: 60s
        maxAckPending: 25000
      otBucket:
        maxValueSize: 0
        history: 1
        ttl: 3h
        maxBytes: 0
        storage: 0
        replicas: 1
      procBucket:
        maxValueSize: 0
        history: 1
        ttl: 72h
        maxBytes: 0
        storage: 0
        replicas: 1
    versions:
    - version: latest
      natsImage: nats:2.10.29
      metricsExporterImage: natsio/prometheus-nats-exporter:0.9.1
      configReloaderImage: natsio/nats-server-config-reloader:0.7.0
      startCommand: /nats-server
`
