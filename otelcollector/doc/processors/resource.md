# `resource` processor

Inserts, updates or deletes resource attributes, which describe the source of the data.

Full example: [`resource.example.yaml`](resource.example.yaml)

## Use case on Kubernetes

Telemetry leaving a cluster must say which cluster, region and environment it came from.
SDKs inside pods don't know that. The `resource` processor stamps it centrally, using values
from a ConfigMap, with OTel semantic-convention names (`k8s.cluster.name`, `cloud.region`,
`deployment.environment.name`).

It also cleans up: renaming or deleting legacy attributes that old services still send.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Yes | Stamp cluster identity where the data enters (`upsert`). |
| Gateway (Deployment) | ✅ Yes | Fill gaps for data that bypassed agents (`insert` = only if missing). |

## Configuration

```yaml
processors:
  resource/cluster:
    attributes:
      - key: k8s.cluster.name
        value: ${env:K8S_CLUSTER_NAME}
        action: upsert
      - key: cloud.provider
        value: azure
        action: upsert
      - key: cloud.platform
        value: azure_aks
        action: upsert
      - key: cloud.region
        value: ${env:CLOUD_REGION}
        action: upsert
      - key: deployment.environment.name
        value: ${env:DEPLOYMENT_ENVIRONMENT}
        action: insert
      # Old, non-standard attribute some services still send.
      - key: env
        action: delete
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, resource/cluster, batch]
      exporters: [elasticsearch]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, resource/cluster, batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, resource/cluster, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC. Values come from environment variables, typically a ConfigMap referenced with
`envFrom`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata: {name: otel-collector-env, namespace: otel-collector}
data:
  K8S_CLUSTER_NAME: aks-prod-westeurope
  CLOUD_REGION: westeurope
  DEPLOYMENT_ENVIRONMENT: production
---
# In the container spec:
envFrom:
  - configMapRef: {name: otel-collector-env}
```

## Tips and pitfalls

- `upsert` on agents (authoritative), `insert` on gateways (don't overwrite what agents set).
- Use semantic-convention names so Kibana's built-in views pick them up.
- For per-pod metadata, use [`k8s_attributes`](k8s_attributes.md); `resource` is for values that are the same for everything the collector handles.

## Try it

```sh
./otelcol validate --config doc/processors/resource.example.yaml
```
