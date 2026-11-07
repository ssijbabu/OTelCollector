# `env` provider

Resolves `${env:NAME}` references, with optional defaults, from the container's environment variables.

Full example: [`env.example.yaml`](env.example.yaml)

## Use case on Kubernetes

The standard Kubernetes pattern is **one config, many environments**: the collector config
lives in a ConfigMap and is identical in dev, test and every production cluster. Everything
that differs is injected as environment variables:

- **ConfigMaps** (`envFrom`) for cluster name, region, sizes, log level
- **Secrets** (`secretKeyRef`) for endpoints and API keys
- **The downward API** (`fieldRef`) for pod name, node name and namespace

`${env:NAME:-default}` keeps the config working when an optional variable isn't set.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Always | Node name, cluster identity and credentials come from env. |
| Gateway (Deployment) | ✅ Always | Endpoints and credentials come from env. |

## Configuration

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:${env:OTLP_GRPC_PORT:-4317}
      http:
        endpoint: 0.0.0.0:${env:OTLP_HTTP_PORT:-4318}

processors:
  memory_limiter:
    check_interval: 1s
    limit_percentage: ${env:MEMORY_LIMIT_PERCENTAGE:-80}
    spike_limit_percentage: 20

  resource:
    attributes:
      - key: k8s.cluster.name
        value: ${env:K8S_CLUSTER_NAME:-unknown}
        action: upsert
      - key: deployment.environment.name
        value: ${env:DEPLOYMENT_ENVIRONMENT:-development}
        action: insert

  batch:
    send_batch_size: ${env:BATCH_SIZE:-8192}
    timeout: 5s

exporters:
  elasticsearch:
    endpoint: ${env:ELASTICSEARCH_ENDPOINT}
    api_key: ${env:ELASTICSEARCH_API_KEY}
    mapping:
      mode: otel

service:
  telemetry:
    logs:
      level: ${env:OTEL_LOG_LEVEL:-info}
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, resource, batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, resource, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

Define the variables in the pod spec. A typical combination:

```yaml
envFrom:
  - configMapRef: {name: otel-collector-env}      # K8S_CLUSTER_NAME, DEPLOYMENT_ENVIRONMENT, BATCH_SIZE, ...
env:
  - name: ELASTICSEARCH_ENDPOINT
    valueFrom: {secretKeyRef: {name: otel-elasticsearch, key: endpoint}}
  - name: ELASTICSEARCH_API_KEY
    valueFrom: {secretKeyRef: {name: otel-elasticsearch, key: api-key}}
  - name: K8S_NODE_NAME
    valueFrom: {fieldRef: {fieldPath: spec.nodeName}}
```

## Tips and pitfalls

- A missing variable without a default becomes an empty string. The collector may still start, so check the endpoints and keys you rely on.
- Use `$${env:NAME}` to keep a literal `${env:NAME}`, for example inside an OTTL string.
- Environment variables appear in `kubectl describe pod` (references only, not Secret values) and in the process environment. For highly sensitive values, the [`file`](file.md) provider with a mounted Secret is stricter.
- Changing a ConfigMap used via `envFrom` needs a pod restart; env vars aren't refreshed live.

## Try it

```sh
./otelcol validate --config doc/providers/env.example.yaml
```
