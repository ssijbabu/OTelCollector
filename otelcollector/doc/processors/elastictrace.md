# `elastictrace` processor

**Deprecated** EDOT processor for Elastic trace enrichment. Elastic replaced it with [`elasticapm`](elasticapm.md).

Full example: [`elastictrace.example.yaml`](elastictrace.example.yaml)

## Use case on Kubernetes

You only meet this processor in older configurations. On Kubernetes, the typical case is
an existing gateway ConfigMap that still lists `elastictrace` in its traces pipeline.

**Migration:** replace `elastictrace` with `elasticapm` in the processor list, add the
`elasticapm` connector and a metrics pipeline for its output (see
[elasticapm.md](elasticapm.md)), roll out the ConfigMap and restart the Deployment.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ❌ Migrate | Use `elasticapm` instead. |
| Agent (DaemonSet) | ❌ No | Never needed on agents. |

## Configuration

```yaml
processors:
  elastictrace: {}
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, elastictrace, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC or volumes.

## Tips and pitfalls

- Kept in the build only so old configs still load. Remove it from `manifest.yaml` once nothing uses it.
- `elasticapm` covers logs and metrics as well, which `elastictrace` never did.

## Try it

```sh
./otelcol validate --config doc/processors/elastictrace.example.yaml
```
