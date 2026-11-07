# `elasticmetadata` processor

EDOT processor that copies request (client) metadata, such as HTTP headers, into fields of structured log bodies.

Full example: [`elasticmetadata.example.yaml`](elasticmetadata.example.yaml)

## Use case on Kubernetes

A shared log intake in the cluster receives logs from many senders: other clusters,
serverless functions, edge devices. Each sender identifies itself with headers
(`X-Tenant-Id`, `X-Source-Region`). Those values only exist on the HTTP request; this
processor copies them into every log document, so you can filter by tenant or region in
Kibana.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | The gateway is where external senders connect and headers are available. |
| Agent (DaemonSet) | Rarely | In-cluster pods usually carry identity in resource attributes instead. |

## Configuration

```yaml
processors:
  elasticmetadata:
    # client metadata key  ->  field in the log body
    log_body_fields:
      x-tenant-id: tenant.id
      x-source-region: cloud.region
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    logs:
      receivers: [otlp]
      processors: [memory_limiter, elasticmetadata, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC or volumes. The receiver must keep headers (`include_metadata: true`), and `batch`
must list the same keys in `metadata_keys`, otherwise the headers are lost before the
processor runs.

If senders reach the gateway through an Ingress, make sure the Ingress forwards those headers.

## Tips and pitfalls

- Only structured log bodies (maps) are enriched; plain-string bodies pass through unchanged.
- Header names are case-insensitive but are matched in lower case: use `x-tenant-id`.
- Never trust identity headers from the internet without authentication; combine with [`apikeyauth`](../extensions/apikeyauth.md).

## Try it

```sh
./otelcol validate --config doc/processors/elasticmetadata.example.yaml
```
