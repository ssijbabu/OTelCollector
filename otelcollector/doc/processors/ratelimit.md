# `ratelimit` processor

EDOT processor that limits how much data flows to exporters, per client, with an in-memory token bucket.

Full example: [`ratelimit.example.yaml`](ratelimit.example.yaml)

## Use case on Kubernetes

A shared gateway serves many teams or tenants. Without limits, one team's runaway logging
can saturate Elasticsearch for everyone. `ratelimit` gives each tenant (keyed on a header
such as `X-Tenant-Id`) its own budget; requests over budget are rejected and retried later
by the sender.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | Central enforcement at the shared entry point. |
| Agent (DaemonSet) | Rarely | Per-node budgets are hard to reason about. |

## Configuration

```yaml
processors:
  ratelimit:
    metadata_keys: [x-tenant-id]   # one bucket per tenant
    strategy: requests             # count requests (alternatives: records, bytes)
    rate: 500                      # sustained requests per second per tenant
    burst: 2000                    # short bursts allowed above the rate
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, ratelimit, batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, ratelimit, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC or volumes. `include_metadata: true` on the receiver, and the same key in `batch`
`metadata_keys`.

The limiter is **in memory per pod**: with 3 gateway replicas, a tenant's effective limit is
about 3 × `rate` (depending on how the Service balances connections).

## Tips and pitfalls

- `strategy: requests` counts requests; `records` counts spans/logs/data points; `bytes` counts payload size.
- Divide the intended cluster-wide limit by the replica count, or route each tenant consistently to one replica.
- Requests without the metadata key share one bucket. Give unauthenticated traffic a small budget, or reject it with [`apikeyauth`](../extensions/apikeyauth.md).

## Try it

```sh
./otelcol validate --config doc/processors/ratelimit.example.yaml
```
