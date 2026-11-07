# `elasticapm` processor

EDOT processor that enriches traces, logs and metrics with the fields Elastic's APM UI needs.

Full example: [`elasticapm.example.yaml`](elasticapm.example.yaml)

## Use case on Kubernetes

Applications in the cluster are instrumented with OpenTelemetry SDKs or EDOT SDKs. To get
the full Kibana APM experience (service map, transactions, dependencies, error groups), the
data needs Elastic-specific fields such as transaction type, event outcome and service
metadata. This processor adds them.

It is used together with the [`elasticapm` connector](../connectors/elasticapm.md), which
computes the aggregated APM metrics (latency, throughput, error rate).

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Recommended | Enrich once, centrally, right before writing to Elasticsearch. |
| Agent (DaemonSet) | ❌ No | Keep agents light; let the gateway do Elastic-specific work. |

## Configuration

```yaml
processors:
  elasticapm:
    # Copy host IP into the fields the APM UI shows.
    host_ip_enabled: true
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, elasticapm, batch]
      exporters: [elasticapm, elasticsearch]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, elasticapm, batch]
      exporters: [elasticapm, elasticsearch]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
    metrics/aggregated-otel-metrics:
      receivers: [elasticapm]
      processors: []
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC or volumes. For the APM UI to show Kubernetes metadata per service, add
[`k8s_attributes`](k8s_attributes.md) on the agents so pod and namespace names are present.

## Tips and pitfalls

- Put it after `memory_limiter` and enrichment processors, before `batch`.
- Always pair it with the `elasticapm` connector; without the connector Kibana APM has traces but no service charts.
- Use it on traces **and** logs pipelines, so log correlation works in the APM UI.

## Try it

```sh
./otelcol validate --config doc/processors/elasticapm.example.yaml
```
