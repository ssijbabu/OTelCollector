# `attributes` processor

Adds, updates, deletes, hashes or extracts attributes on individual spans, log records and metric data points.

Full example: [`attributes.example.yaml`](attributes.example.yaml)

## Use case on Kubernetes

In a cluster, many teams ship telemetry through the same collectors. Some of that data
should never reach Elasticsearch: HTTP instrumentations can record `Authorization` and
`Cookie` headers, and user identifiers are personal data. The `attributes` processor is the
central place to enforce those rules for **every** workload, without asking each team to
change their SDK configuration.

Typical Kubernetes uses:

- **Data protection:** delete credential headers and hash user IDs at the gateway, so the
  rule applies to every namespace.
- **Traceability:** stamp which gateway pod handled the data (`HOSTNAME` is the pod name).
- **Multi-tenancy:** extract a tenant ID from URL paths such as `/tenants/acme/...`.
- **Scoped rules:** apply actions only to selected services (for example, internet-facing
  ones) with `include` / `exclude`.

For attributes that describe the *source* (cluster, namespace, pod), use the
[`resource`](resource.md) or [`k8s_attributes`](k8s_attributes.md) processors instead.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Recommended | One central place to enforce data-protection rules for all namespaces. |
| Agent (DaemonSet) | Optional | Use when data must be scrubbed before it leaves the node. |

## Configuration

```yaml
processors:
  attributes/scrub:
    actions:
      # Credentials must never reach the backend.
      - key: http.request.header.authorization
        action: delete
      - key: http.request.header.cookie
        action: delete
      # Hash (SHA-256) personal identifiers.
      - key: user.email
        action: hash
      - key: enduser.id
        action: hash
      # Record which gateway pod processed the data (insert = only if missing).
      - key: otel.gateway.name
        value: ${env:HOSTNAME}
        action: insert
      # /tenants/acme/orders/42  ->  tenant.id = "acme"
      - key: url.path
        pattern: ^/tenants/(?P<tenant_id>[^/]+)/
        action: extract
      - key: tenant.id
        from_attribute: tenant_id
        action: insert
      - key: tenant_id
        action: delete

  # Only touch spans from the public API services; everything else passes through unchanged.
  attributes/public-api:
    include:
      match_type: regexp
      services: ["^public-api-.*"]
    actions:
      - key: exposure
        value: internet
        action: upsert
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, attributes/scrub, attributes/public-api, batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, attributes/scrub, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC or volumes are needed. `HOSTNAME` is set by Kubernetes to the pod name in every
container, so `${env:HOSTNAME}` identifies the gateway replica.

## Tips and pitfalls

- `insert` only sets a value when the key is missing, `update` only when it exists, `upsert` always.
- `hash` uses SHA-256: the value is unreadable but still joinable across signals.
- `extract` uses named capture groups; each group becomes a new attribute. Copy/rename it afterwards if you want a dotted name.
- Resource attributes (`k8s.*`, `service.name`) are not touched by this processor; use `resource` for them.
- Put it after `memory_limiter` and before `batch`.

## Try it

```sh
./otelcol validate --config doc/processors/attributes.example.yaml
```
