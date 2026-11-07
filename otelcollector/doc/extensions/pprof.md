# `pprof` extension

Exposes Go's pprof endpoint to profile the collector itself: CPU, heap, goroutines and contention.

Full example: [`pprof.example.yaml`](pprof.example.yaml)

## Use case on Kubernetes

A gateway pod uses more CPU than expected, or its memory keeps growing. Profiles show which
component is responsible: a regex in a `filter` rule, a high-cardinality `span_metrics`
dimension, a growing queue.

On Kubernetes, you reach the endpoint with `kubectl port-forward`, without exposing it.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | Optional | Enable when investigating a node agent. |
| Gateway (Deployment) | ✅ Recommended | Cheap when idle; invaluable when a gateway misbehaves. |

## Configuration

```yaml
extensions:
  pprof:
    endpoint: localhost:1777
    block_profile_fraction: 0     # set >0 temporarily to investigate blocking
    mutex_profile_fraction: 0     # set >0 temporarily to investigate lock contention
```

Wired into the pipelines:

```yaml
service:
  extensions: [pprof]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

Nothing to expose. Bind to `localhost` and use port-forwarding:

Capture profiles from your workstation:

```sh
kubectl port-forward -n otel-gateway pod/<gateway-pod> 1777:1777
go tool pprof http://localhost:1777/debug/pprof/heap
go tool pprof "http://localhost:1777/debug/pprof/profile?seconds=30"
curl http://localhost:1777/debug/pprof/goroutine?debug=1 | head
```

## Tips and pitfalls

- Bind to `localhost:1777`; never put pprof behind a Service or Ingress.
- `block_profile_fraction` and `mutex_profile_fraction` add overhead; enable them temporarily.
- `kubectl port-forward` reaches localhost-bound ports inside the pod, so nothing else is needed.

## Try it

```sh
./otelcol validate --config doc/extensions/pprof.example.yaml
```
