# `headers_setter` extension

Adds headers to outgoing exporter requests: static values, or values copied from the incoming request.

Full example: [`headers_setter.example.yaml`](headers_setter.example.yaml)

## Use case on Kubernetes

The cluster's gateway forwards to a **multi-tenant** backend (a central observability
platform) that needs:

- the gateway's own credentials (static `Authorization` header), and
- the original tenant on every request (`X-Scope-OrgID`), taken from the `X-Tenant-Id`
  header the sending team set.

`headers_setter` is a client authenticator: exporters reference it with `auth`, and it adds
those headers to each outgoing request.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | Where data leaves the cluster. |
| Agent (DaemonSet) | Optional | If agents export directly to an external backend. |

## Configuration

```yaml
extensions:
  headers_setter:
    headers:
      - key: Authorization
        value: Bearer ${env:UPSTREAM_TOKEN}
        action: upsert
      - key: X-Scope-OrgID
        from_context: x-tenant-id   # copied from the incoming request
        default_value: shared       # used when the sender didn't set it
        action: upsert
```

Wired into the pipelines:

```yaml
service:
  extensions: [headers_setter]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [otlp_http/upstream]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [otlp_http/upstream]
```

## Kubernetes requirements

No RBAC. The static token comes from a Secret:

```yaml
env:
  - name: UPSTREAM_TOKEN
    valueFrom: {secretKeyRef: {name: otel-upstream, key: token}}
  - name: UPSTREAM_OTLP_ENDPOINT
    valueFrom: {configMapKeyRef: {name: otel-gateway-env, key: upstream-endpoint}}
```

## Tips and pitfalls

- `from_context` only works if the receiver has `include_metadata: true` **and** `batch` lists the key in `metadata_keys`; otherwise the header is lost before export.
- `default_value` covers senders that didn't set the header.
- The [`apmconfig`](apmconfig.md) example uses `headers_setter` the same way to add an Elasticsearch API key.

## Try it

```sh
./otelcol validate --config doc/extensions/headers_setter.example.yaml
```
