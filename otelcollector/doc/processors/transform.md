# `transform` processor

Modifies telemetry with OTTL statements: set, delete, rename, truncate, parse JSON, change metric types and more.

Full example: [`transform.example.yaml`](transform.example.yaml)

## Use case on Kubernetes

Pods log in many formats, and many teams name things differently. `transform` normalises
data centrally:

- Parse JSON log lines (common for apps writing to stdout) into attributes, and derive
  severity from a `level` field.
- Replace IDs in span names (`GET /orders/12345` → `GET /orders/{id}`) to keep cardinality
  low in Kibana.
- Rename legacy metric names to semantic-convention names.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Recommended | One central set of normalisation rules. |
| Agent (DaemonSet) | ✅ Yes | For node-local work, such as parsing container log formats close to the source. |

## Configuration

```yaml
processors:
  transform:
    error_mode: ignore
    log_statements:
      # {"level":"warn","msg":"disk almost full","disk":"/data"} -> log attributes
      - merge_maps(log.attributes, ParseJSON(log.body), "upsert") where IsMatch(log.body, "^\\{")
      - set(log.severity_text, "WARN") where log.attributes["level"] == "warn"
      - set(log.severity_number, SEVERITY_NUMBER_WARN) where log.attributes["level"] == "warn"
      - set(log.severity_text, "ERROR") where log.attributes["level"] == "error"
      - set(log.severity_number, SEVERITY_NUMBER_ERROR) where log.attributes["level"] == "error"
    trace_statements:
      - truncate_all(span.attributes, 4096)
      # GET /orders/12345 -> GET /orders/{id}
      - replace_pattern(span.name, "/[0-9]+", "/{id}")
    metric_statements:
      - set(metric.name, "http.server.request.duration") where metric.name == "http_request_duration_seconds"
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, transform, batch]
      exporters: [elasticsearch]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, transform, batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, transform, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC or volumes.

## Tips and pitfalls

- Statements use context prefixes (`log.`, `span.`, `metric.`, `resource.`), so the processor infers where to run them.
- Statements run in order; later ones see the results of earlier ones.
- `error_mode: ignore` skips a failing statement instead of dropping the data.
- Changing `resource` attributes from a lower context affects every record sharing that resource. Do resource-wide changes in the `resource` processor.
- Test new statements with the debug exporter before rolling them out.

## Try it

```sh
./otelcol validate --config doc/processors/transform.example.yaml
```
