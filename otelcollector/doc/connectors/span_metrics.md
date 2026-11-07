# `span_metrics` connector

Builds RED metrics (rate, errors, duration) from spans: call counts and latency histograms per service, operation and chosen attributes.

Full example: [`span_metrics.example.yaml`](span_metrics.example.yaml)

## Use case on Kubernetes

Dashboards and alerts such as "p95 latency of `POST /orders` > 500 ms" need metrics that
count **every** request, but storing every trace is too expensive. `span_metrics` computes
those metrics from all spans first; then tail sampling decides which traces to keep.

The example uses [`forward`](forward.md) to split the flow: one copy of all spans goes to
`span_metrics`, the other through `tail_sampling` to storage.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | Sees all spans; place it before sampling. |
| Agent (DaemonSet) | Optional | Works, but each agent only sees its node's spans. |

## Configuration

```yaml
connectors:
  span_metrics:
    namespace: traces.span.metrics
    histogram:
      unit: ms
      explicit:
        buckets: [5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s]
    dimensions:
      - name: http.request.method
      - name: http.route
      - name: http.response.status_code
    exemplars:
      enabled: true
    metrics_flush_interval: 15s
    aggregation_temporality: AGGREGATION_TEMPORALITY_DELTA
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    # All spans -> span_metrics (unsampled), then sample what gets stored.
    traces/in:
      receivers: [otlp]
      processors: [memory_limiter]
      exporters: [span_metrics, forward]
    traces/store:
      receivers: [forward]
      processors: [tail_sampling, batch]
      exporters: [elasticsearch]
    metrics/red:
      receivers: [span_metrics]
      processors: [batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC. Metrics state is in memory, per pod. Cardinality is the main risk: every
combination of service × span name × dimension values becomes a series. Keep span names
low-cardinality (see the [`transform`](../processors/transform.md) example that replaces
IDs in span names).

## Tips and pitfalls

- Compute span metrics **before** `tail_sampling`, otherwise the metrics only count sampled traces.
- Each extra dimension multiplies the number of series; use route templates (`http.route`), never raw URLs.
- `aggregation_temporality: AGGREGATION_TEMPORALITY_DELTA` gives per-interval values and copes well with pod restarts.
- If you use Kibana APM, the [`elasticapm` connector](elasticapm.md) gives similar metrics in the format the APM UI expects.

## Try it

```sh
./otelcol validate --config doc/connectors/span_metrics.example.yaml
```
