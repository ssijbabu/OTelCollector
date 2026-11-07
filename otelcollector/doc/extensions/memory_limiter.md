# `memory_limiter` extension

The memory limiter as a receiver middleware: refuses requests at the HTTP/gRPC layer, before the payload is decoded.

Full example: [`memory_limiter.example.yaml`](memory_limiter.example.yaml)

## Use case on Kubernetes

The [`memory_limiter` processor](../processors/memory_limiter.md) only sees data **after**
the receiver has decoded it, so a burst of large requests can still exhaust memory during
decoding. As a middleware on the OTLP receiver, the extension rejects requests as soon as
memory is high, with HTTP 429 or gRPC `RESOURCE_EXHAUSTED`, which OTLP clients retry with
backoff.

On Kubernetes, that's most valuable on gateways exposed to many senders, where request size
and volume are outside your control.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Recommended | Protects internet- or cluster-facing receivers. |
| Agent (DaemonSet) | Optional | Agents usually see smaller, steadier traffic. |

## Configuration

```yaml
extensions:
  memory_limiter:
    check_interval: 1s
    limit_percentage: 80
    spike_limit_percentage: 20
```

Wired into the pipelines:

```yaml
service:
  extensions: [memory_limiter]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

A container memory **limit**; percentages are of that limit. The same sizing rules as the
processor apply.

## Tips and pitfalls

- Add it to every HTTP and gRPC protocol of every receiver you want protected, under `middlewares`.
- Using both the extension and the processor is fine: the extension guards decoding, the processor guards pipelines.
- Watch `otelcol_memorylimiter_refused_requests` to see how often requests are refused.

## Try it

```sh
./otelcol validate --config doc/extensions/memory_limiter.example.yaml
```
