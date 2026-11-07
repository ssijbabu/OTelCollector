# `memory_limiter` processor

Refuses data when memory use is high, so senders back off and retry instead of the pod being OOM-killed.

Full example: [`memory_limiter.example.yaml`](memory_limiter.example.yaml)

## Use case on Kubernetes

Every collector pod has a memory limit. When Elasticsearch slows down or traffic bursts,
data piles up in memory. Without a limiter, the kernel OOM-kills the pod and **everything**
in memory is lost; the pod restarts and the same burst kills it again.

With `memory_limiter`, the collector starts refusing new data at the soft limit. OTLP
senders treat that as a retryable error and back off, and the pod stays alive.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Always | First processor in every pipeline. |
| Gateway (Deployment) | ✅ Always | First processor in every pipeline. |

## Configuration

```yaml
processors:
  memory_limiter:
    check_interval: 1s
    limit_percentage: 80         # hard limit: refuse data above 80% of available memory
    spike_limit_percentage: 20   # soft limit = 80% - 20% = 60%: start refusing early
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

Set a container memory **limit**. The percentages are of that limit (read from the cgroup).
Without a limit they are of the node's total memory, which is almost never what you want.
Pair it with `GOMEMLIMIT` or the [`cgroup_runtime`](../extensions/cgroup_runtime.md)
extension, so Go's garbage collector works before the limiter has to refuse data.

Container resources the percentages are based on:

```yaml
resources:
  requests: {cpu: 250m, memory: 1Gi}
  limits:   {cpu: "1",  memory: 2Gi}   # limit_percentage 80 -> refuse above ~1.6Gi
```

## Tips and pitfalls

- Always first in the processor list, so refused data is never processed.
- Soft limit = `limit_percentage` − `spike_limit_percentage`. Refusing starts there; above the hard limit, GC is also forced.
- Watch the `otelcol_processor_refused_*` metrics. Steady refusals mean the pod needs more memory or replicas.
- For refusing **before** decoding the request, also use the [`memory_limiter` extension](../extensions/memory_limiter.md).

## Try it

```sh
./otelcol validate --config doc/processors/memory_limiter.example.yaml
```
