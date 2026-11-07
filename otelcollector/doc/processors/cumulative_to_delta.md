# `cumulative_to_delta` processor

Converts cumulative monotonic sums and histograms into delta (per-interval) values.

Full example: [`cumulative_to_delta.example.yaml`](cumulative_to_delta.example.yaml)

## Use case on Kubernetes

Most OpenTelemetry SDKs export counters as **cumulative** totals ("12 345 requests since the
pod started"). When a pod restarts, the total drops back to zero. Dashboards that plot
rates per interval are simpler and survive restarts better with **delta** values
("120 requests in the last 10s").

On Kubernetes this matters because pods restart and scale all the time. Converting at the
collector gives consistent per-interval values, whatever temporality each SDK uses.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Recommended | Each series comes from a pod on the same node, so state stays on one instance. |
| Gateway (Deployment) | ⚠️ Careful | Only if every series always reaches the same replica (sticky routing); otherwise deltas are wrong. |

## Configuration

```yaml
processors:
  cumulative_to_delta:
    include:
      match_type: regexp
      metrics:
        - ^http\.server\..*
        - ^rpc\.server\..*
    # Forget series that have not been seen for 10 minutes (bounds memory use).
    max_staleness: 10m
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, cumulative_to_delta, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC or volumes. The processor keeps the last value of every series in memory, so give
the pod enough memory for the number of series it sees, and use `max_staleness` to forget
series from pods that no longer exist.

## Tips and pitfalls

- State is per collector instance. If one series is spread across replicas, each replica computes its own (wrong) delta.
- Use `include` to convert only the metrics your dashboards need.
- The first data point of a new series produces no delta; that's expected.
- Pod restarts reset the cumulative counter; the processor handles the reset and starts a new delta series.

## Try it

```sh
./otelcol validate --config doc/processors/cumulative_to_delta.example.yaml
```
