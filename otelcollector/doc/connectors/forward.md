# `forward` connector

Passes data unchanged from one pipeline to another, so several pipelines can feed one shared pipeline.

Full example: [`forward.example.yaml`](forward.example.yaml)

## Use case on Kubernetes

On a node agent, logs arrive from two very different sources:

- **Container logs** tailed from `/var/log/pods`, which need parsing.
- **Application logs** sent over OTLP by instrumented pods, which are already structured
  but may need noise filtering.

Each source gets its own pipeline for source-specific work, and `forward` merges them into
one `logs/export` pipeline. Cluster enrichment, batching and the Elasticsearch settings then
exist in exactly one place.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Yes | Merge node-local log sources into one export path. |
| Gateway (Deployment) | ✅ Yes | Split or merge pipelines with different processing. |

## Configuration

```yaml
connectors:
  forward: {}
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    logs/containers:
      receivers: [file_log]
      processors: [memory_limiter]
      exporters: [forward]
    logs/apps:
      receivers: [otlp]
      processors: [memory_limiter, filter/drop-debug]
      exporters: [forward]
    logs/export:
      receivers: [forward]
      processors: [resource/cluster, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC for the connector itself. The example's `file_log` receiver needs the
`/var/log/pods` hostPath mount on the DaemonSet:

```yaml
volumeMounts:
  - name: varlogpods
    mountPath: /var/log/pods
    readOnly: true
volumes:
  - name: varlogpods
    hostPath:
      path: /var/log/pods
```

## Tips and pitfalls

- Data passes through unchanged; all processing happens in the pipelines on either side.
- It also works the other way: one input pipeline exporting to `forward` can fan out to several pipelines that each receive from it.
- Use it with [`span_metrics`](span_metrics.md) to compute metrics from all spans while only a sampled share is stored.

## Try it

```sh
./otelcol validate --config doc/connectors/forward.example.yaml
```
