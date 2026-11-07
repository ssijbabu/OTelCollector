# `yaml` provider

Accepts configuration inline on the command line with the `yaml:` scheme, merged on top of the other configs.

Full example: [`yaml.example.yaml`](yaml.example.yaml)

## Use case on Kubernetes

During an incident you want **one** pod to log at debug level and print traces with the
debug exporter, without editing the shared ConfigMap (which would change every replica).
Adding `--config yaml:...` arguments to a single pod's spec does exactly that, and removing
them restores normal behaviour.

It's also handy for small per-environment differences that don't justify a separate config
file.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | Situational | Targeted debugging or small overrides. |
| Gateway (Deployment) | Situational | Targeted debugging or small overrides. |

## Configuration

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  memory_limiter:
    check_interval: 1s
    limit_percentage: 80
    spike_limit_percentage: 20
  batch: {}

exporters:
  elasticsearch:
    endpoint: ${env:ELASTICSEARCH_ENDPOINT}
    api_key: ${env:ELASTICSEARCH_API_KEY}
    mapping:
      mode: otel

service:
  telemetry:
    logs:
      level: info
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

Add the overrides to the container args. `::` separates levels of the config path:

```yaml
args:
  - --config=/etc/otelcol/config.yaml
  - "--config=yaml:service::telemetry::logs::level: debug"
  - "--config=yaml:exporters::debug::verbosity: detailed"
  - "--config=yaml:service::pipelines::traces::exporters: [elasticsearch, debug]"
```

## Tips and pitfalls

- Overrides are merged in order; later ones win. Lists are **replaced**, not appended, so repeat every exporter you want to keep.
- Quote the whole argument in YAML (the value contains `:` and spaces).
- To debug a single replica of a Deployment, run a temporary copy of the pod with the extra args rather than editing the Deployment.
- `otelcol print-config --config ... --config yaml:...` shows the merged result before you apply it.

## Try it

```sh
./otelcol validate --config doc/providers/yaml.example.yaml
```
