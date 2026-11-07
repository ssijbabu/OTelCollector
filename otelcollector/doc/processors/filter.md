# `filter` processor

Drops spans, span events, log records, metrics or data points that match OTTL conditions.

Full example: [`filter.example.yaml`](filter.example.yaml)

## Use case on Kubernetes

A Kubernetes cluster generates a lot of telemetry nobody looks at:

- **Probe noise:** kubelet calls `/healthz` and `/readyz` on every pod every few seconds.
  Instrumented services record each call as a span.
- **Debug logs:** left on in production by mistake.
- **Runtime metrics:** not used on any dashboard.

Dropping them at the collector is the cheapest way to cut Elasticsearch storage and
ingest cost, without redeploying any application.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Recommended | Drop noise as early as possible, before it crosses the network. |
| Gateway (Deployment) | ✅ Yes | Central rules that must apply to every source. |

## Configuration

```yaml
processors:
  filter/noise:
    # ignore = log OTTL errors and keep the data, instead of failing the request
    error_mode: ignore
    trace_conditions:
      - span.attributes["url.path"] == "/healthz"
      - span.attributes["url.path"] == "/readyz"
      - IsMatch(span.attributes["user_agent.original"], "kube-probe/.*")
    log_conditions:
      - log.severity_number < SEVERITY_NUMBER_INFO and resource.attributes["deployment.environment.name"] == "production"
    metric_conditions:
      - IsMatch(metric.name, "^process\\.runtime\\.jvm\\.buffer\\..*")
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, filter/noise, batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, filter/noise, batch]
      exporters: [elasticsearch]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, filter/noise, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC or volumes.

## Tips and pitfalls

- Conditions use context prefixes (`span.`, `log.`, `metric.`, `resource.`). This is the documented style since v0.146; the older `traces.span` keys still work.
- If **any** condition matches, the item is dropped.
- `error_mode: ignore` keeps data when a condition errors (for example, a missing attribute), instead of failing the request.
- Use `resource.attributes["k8s.namespace.name"]` to scope rules to a namespace (needs `k8s_attributes` upstream).
- Test new rules with the debug exporter before rolling them out; dropped data can't be recovered.

## Try it

```sh
./otelcol validate --config doc/processors/filter.example.yaml
```
