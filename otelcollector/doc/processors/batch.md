# `batch` processor

Groups spans, log records and data points into batches before export.

Full example: [`batch.example.yaml`](batch.example.yaml)

## Use case on Kubernetes

Elasticsearch performs best with a few large bulk requests rather than many small ones.
On Kubernetes, hundreds of pods each send small OTLP requests to the collector; without
batching, each of those becomes a separate bulk request.

`batch` also lets you keep tenants apart: with `metadata_keys`, data that arrived with
different `X-Tenant-Id` headers is batched separately, so one bulk request never mixes
tenants. That matters when a later exporter or extension uses the header (for example
[`headers_setter`](../extensions/headers_setter.md)).

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | Larger batches before Elasticsearch; the main reason the processor exists. |
| Agent (DaemonSet) | ✅ Yes | Fewer, larger OTLP requests from each node to the gateway. |

## Configuration

```yaml
processors:
  batch:
    send_batch_size: 8192        # flush when this many items are queued...
    send_batch_max_size: 10000   # ...never send more than this in one request
    timeout: 5s                  # ...or after 5s, whichever comes first
    metadata_keys: [x-tenant-id] # one independent batcher per tenant
    metadata_cardinality_limit: 100

  # Metrics are small and frequent: flush faster so dashboards stay fresh.
  batch/metrics:
    send_batch_size: 2048
    timeout: 1s
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch/metrics]
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC or volumes. For `metadata_keys`, the receiver must keep request headers:
`include_metadata: true` on the OTLP receiver protocols, as in the example.

Memory: a batch is held in memory until it is sent. Size the pod's memory limit for
`send_batch_max_size` × average item size × number of tenants.

## Tips and pitfalls

- Place `batch` **last** in the processor list, after filtering and enrichment.
- `send_batch_size` triggers a send; `send_batch_max_size` caps one request. Set the cap to keep bulk requests below Elasticsearch's `http.max_content_length`.
- Use a separate `batch/metrics` with a shorter `timeout` if dashboards need fresh data.
- `metadata_cardinality_limit` protects against a flood of distinct tenant values.
- The Elasticsearch exporter can also batch inside its sending queue; one layer of batching is usually enough.

## Try it

```sh
./otelcol validate --config doc/processors/batch.example.yaml
```
