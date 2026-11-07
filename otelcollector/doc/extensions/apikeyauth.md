# `apikeyauth` extension

EDOT extension that authenticates incoming requests with Elasticsearch API keys, checked against Elasticsearch and cached.

Full example: [`apikeyauth.example.yaml`](apikeyauth.example.yaml)

## Use case on Kubernetes

The gateway is exposed outside the cluster (through an Ingress or a `LoadBalancer`
Service) so that applications in other clusters, VMs or serverless functions can send
telemetry. Anything reachable from outside must be authenticated.

`apikeyauth` reuses Elasticsearch's own API keys: senders put
`Authorization: ApiKey <base64>` on their requests, the extension asks Elasticsearch whether
that key has the required application privileges, and caches the answer. Key management
stays in Kibana; no separate credential store is needed.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | Protects the externally reachable entry point. |
| Agent (DaemonSet) | ❌ No | In-cluster pods talk to agents over the node network; protect that with NetworkPolicies. |

## Configuration

```yaml
extensions:
  apikeyauth:
    endpoint: ${env:ELASTICSEARCH_ENDPOINT}
    application_privileges:
      - application: otel
        privileges: [ingest]
        resources: ["*"]
    cache:
      capacity: 1000
      ttl: 5m
```

Wired into the pipelines:

```yaml
service:
  extensions: [apikeyauth]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

- Egress from the gateway to Elasticsearch.
- API keys for senders created in Kibana or via the API with an application privilege that
  matches the config (`application: otel`, privilege `ingest`):

Example key for a sending service:

```json
# POST _security/api_key  (run in Kibana Dev Tools)
{
  "name": "orders-service-ingest",
  "role_descriptors": {
    "otel-ingest": {
      "applications": [
        {"application": "otel", "privileges": ["ingest"], "resources": ["*"]}
      ]
    }
  }
}
```

## Tips and pitfalls

- Terminate TLS at the Ingress or on the receiver; API keys must never travel in plain text.
- The cache (`capacity`, `ttl`) avoids one Elasticsearch call per request. Revoked keys keep working until their cache entry expires.
- Use separate keys per sender, so one can be revoked without affecting others.
- Combine with [`ratelimit`](../processors/ratelimit.md) to give each authenticated sender a budget.

## Try it

```sh
./otelcol validate --config doc/extensions/apikeyauth.example.yaml
```
