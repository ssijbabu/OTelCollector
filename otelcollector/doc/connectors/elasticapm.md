# `elasticapm` connector

EDOT connector that turns traces and logs into the aggregated APM metrics Kibana's APM UI is built on: latency histograms, throughput, error rate and service summaries.

Full example: [`elasticapm.example.yaml`](elasticapm.example.yaml)

## Use case on Kubernetes

Services in the cluster send traces to the gateway, which writes them to Elasticsearch.
Kibana APM can show individual traces from that alone, but the **service overview**
(latency charts, throughput, error rate, dependencies) needs pre-aggregated metrics. This
connector computes them as data flows through, over 1-minute, 10-minute and 60-minute windows.

A connector is an **exporter** at the end of the traces/logs pipelines and a **receiver** at
the start of a metrics pipeline.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | Sees all spans of each service; the natural place for aggregation. |
| Agent (DaemonSet) | ❌ No | Each agent sees only part of every service, so the aggregates would be fragmented. |

## Configuration

```yaml
connectors:
  elasticapm:
    # Extra span attributes to keep as dimensions on the aggregated metrics.
    custom_span_attributes: [http.route]
    # Aggregation runs over 1m, 10m and 60m windows (defaults; Elastic says not to change
    # them outside testing). State is kept in memory unless a directory is set:
    # aggregation:
    #   directory: /var/lib/otelcol/apm
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
    metrics/aggregated-otel-metrics:
      receivers: [elasticapm]
      processors: []
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC. Aggregation state is kept **in memory** by default, so give the gateway memory
headroom. State is per pod; with several gateway replicas, each emits partial aggregates,
which Elasticsearch and Kibana combine at query time.

To move the state out of memory, set `aggregation.directory` (commented out in the example)
and mount a volume there. With an `emptyDir` the state survives container restarts but not
pod rescheduling:

```yaml
volumeMounts:
  - {name: apm-aggregation, mountPath: /var/lib/otelcol/apm}
volumes:
  - name: apm-aggregation
    emptyDir: {sizeLimit: 1Gi}
```

## Tips and pitfalls

- Always pair it with the [`elasticapm` processor](../processors/elasticapm.md) on the same traces and logs pipelines.
- Don't override `aggregation.intervals`; Elastic only supports the defaults (1m, 10m, 60m) outside testing.
- Keep the output pipeline's processor list empty, as in the example; the metrics are already aggregated.
- `custom_span_attributes` adds dimensions; each one multiplies cardinality, so add only what you filter on (for example `http.route`).
- Put it **before** any tail sampling, so the metrics count every request.

## Try it

```sh
./otelcol validate --config doc/connectors/elasticapm.example.yaml
```
