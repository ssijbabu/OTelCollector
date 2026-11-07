# `routing` connector

Sends data to different pipelines based on OTTL conditions, for example by namespace, tenant or attribute.

Full example: [`routing.example.yaml`](routing.example.yaml)

## Use case on Kubernetes

One shared gateway serves the whole cluster, but data must not all go to the same place:

- The **payments** namespace must go to a separate, access-restricted Elasticsearch cluster
  (compliance).
- **Audit** log records must go to their own data stream with longer retention.
- Everything else takes the default route.

`routing` evaluates conditions on resource attributes (such as `k8s.namespace.name`) or on
individual records, and hands data to the matching pipeline, each with its own exporter.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | Central routing decisions before export. |
| Agent (DaemonSet) | Optional | Per-node routing is rarely needed. |

## Configuration

```yaml
connectors:
  routing:
    default_pipelines: [logs/default]
    error_mode: ignore
    table:
      # Everything from the payments namespace -> dedicated cluster.
      - context: resource
        condition: attributes["k8s.namespace.name"] == "payments"
        pipelines: [logs/payments]
      # Individual audit log records -> audit data stream.
      - context: log
        condition: attributes["event.category"] == "audit"
        pipelines: [logs/audit]
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    logs/in:
      receivers: [otlp]
      processors: [memory_limiter]
      exporters: [routing]
    logs/default:
      receivers: [routing]
      processors: [batch]
      exporters: [elasticsearch]
    logs/audit:
      receivers: [routing]
      processors: [batch]
      exporters: [elasticsearch/audit]
    logs/payments:
      receivers: [routing]
      processors: [batch]
      exporters: [elasticsearch/payments]
```

## Kubernetes requirements

No RBAC for the connector. Routing on `k8s.namespace.name` requires the attribute to be
present: run [`k8s_attributes`](../processors/k8s_attributes.md) on the agents. Each
destination's credentials come from its own Secret.

```yaml
env:
  - name: PAYMENTS_ELASTICSEARCH_ENDPOINT
    valueFrom: {secretKeyRef: {name: otel-es-payments, key: endpoint}}
  - name: PAYMENTS_ELASTICSEARCH_API_KEY
    valueFrom: {secretKeyRef: {name: otel-es-payments, key: api-key}}
```

## Tips and pitfalls

- Data matching no route goes to `default_pipelines`; without it, unmatched data is dropped.
- `context: resource` routes whole resources (fast); `context: log` / `span` routes individual records.
- A record matching several routes goes to all of them; order routes from most to least specific.
- Keep `batch` in the destination pipelines, after routing, so batches don't mix destinations.

## Try it

```sh
./otelcol validate --config doc/connectors/routing.example.yaml
```
