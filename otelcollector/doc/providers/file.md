# `file` provider

Loads configuration from files: the main `--config` file, and individual values or sections via `${file:/path}`.

Full example: [`file.example.yaml`](file.example.yaml)

## Use case on Kubernetes

On Kubernetes, the collector config itself is a file: a ConfigMap mounted at
`/etc/otelcol/config.yaml` and passed with `--config`. That is the `file` provider.

It also reads **individual values** from files. The example reads the Elasticsearch API key
from a Secret **mounted as a file** instead of an environment variable, so it doesn't show
up in the process environment, and rotated Secret values reach the file after a short delay.

Several `--config` files are merged in order, which lets you keep a shared base config and a
small per-cluster override in separate ConfigMaps.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Always | The config file itself is loaded by this provider. |
| Gateway (Deployment) | ✅ Always | The config file itself is loaded by this provider. |

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
    api_key: ${file:/var/run/secrets/elasticsearch/api-key}
    mapping:
      mode: otel

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
```

## Kubernetes requirements

Mount the ConfigMap (config) and the Secret (API key) as volumes:

```yaml
containers:
  - name: otel-gateway
    args:
      - --config=/etc/otelcol/config.yaml
      - --config=/etc/otelcol/overrides/cluster.yaml   # optional, merged on top
    volumeMounts:
      - {name: config,    mountPath: /etc/otelcol, readOnly: true}
      - {name: overrides, mountPath: /etc/otelcol/overrides, readOnly: true}
      - {name: es-secret, mountPath: /var/run/secrets/elasticsearch, readOnly: true}
volumes:
  - name: config
    configMap: {name: otel-gateway-config}
  - name: overrides
    configMap: {name: otel-gateway-overrides}
  - name: es-secret
    secret:
      secretName: otel-elasticsearch     # key "api-key" -> file api-key
      defaultMode: 0400
```

## Tips and pitfalls

- File contents are parsed as YAML; a single-line API key stays a string.
- The collector reads files at startup. A rotated Secret updates the mounted file, but the collector only picks it up after a restart.
- Mount Secrets with `defaultMode: 0400` and run the collector as the file's owner (or use `fsGroup`).
- With `subPath` mounts, Kubernetes never updates the file; mount the whole directory instead.

## Try it

```sh
./otelcol validate --config doc/providers/file.example.yaml
```
