# `tail_sampling` processor

Waits for whole traces, then keeps or drops each trace based on policies (errors, latency, attributes, probability).

Full example: [`tail_sampling.example.yaml`](tail_sampling.example.yaml)

## Use case on Kubernetes

Busy clusters produce more traces than are worth storing. Head sampling in SDKs decides
before knowing whether a request will fail. Tail sampling decides after seeing the whole
trace: keep **all** errors and slow requests, keep everything from critical services, and
sample a small share of normal traffic.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | Needs all spans of a trace in one place. |
| Agent (DaemonSet) | ❌ No | A trace's spans come from many nodes; an agent only sees part of each trace. |

## Configuration

```yaml
processors:
  tail_sampling:
    decision_wait: 10s        # how long to wait for late spans of a trace
    num_traces: 100000        # traces kept in memory while waiting
    expected_new_traces_per_sec: 2000
    policies:
      - name: keep-errors
        type: status_code
        status_code:
          status_codes: [ERROR]
      - name: keep-slow
        type: latency
        latency:
          threshold_ms: 1000
      - name: keep-checkout
        type: string_attribute
        string_attribute:
          key: service.name
          values: [checkout]
      - name: sample-rest
        type: probabilistic
        probabilistic:
          sampling_percentage: 5
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, tail_sampling, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

**All spans of a trace must reach the same gateway replica.** With several replicas behind
a normal Service, spans are spread across pods and each pod sees fragments. Options:

1. A first gateway tier with the `load_balancing` exporter (`routing_key: traceID`) sending
   to a headless Service of sampling pods. `load_balancing` is in `manifest-old.yaml`; add
   it to `manifest.yaml` if you use this.
2. A single sampling replica, which is simple but has no high availability.

Memory: `num_traces` × average spans per trace are held for `decision_wait`; size limits
accordingly.

```yaml
# Headless Service for trace-ID-based routing to sampling pods
apiVersion: v1
kind: Service
metadata: {name: otel-sampler, namespace: otel-gateway}
spec:
  clusterIP: None
  selector: {app: otel-sampler}
  ports:
    - {name: otlp-grpc, port: 4317}
```

## Tips and pitfalls

- Generate span metrics **before** sampling (see [`span_metrics`](../connectors/span_metrics.md)), so rates and latencies count every request.
- `decision_wait` must exceed your longest normal trace duration, or late spans are dropped.
- Policies are ORed: a trace is kept if any policy says keep.
- Watch `otelcol_processor_tail_sampling_*` metrics for traces dropped because `num_traces` was too small.

## Try it

```sh
./otelcol validate --config doc/processors/tail_sampling.example.yaml
```
