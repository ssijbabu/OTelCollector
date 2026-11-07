# `healthcheckv2` extension

Health check that reports per-pipeline and per-component status over HTTP and gRPC.

Full example: [`healthcheckv2.example.yaml`](healthcheckv2.example.yaml)

## Use case on Kubernetes

A plain liveness check says the process is up, but a gateway whose Elasticsearch exporter
has been failing for 10 minutes is effectively broken. `healthcheckv2` tracks each
component's status, so:

- **readiness** can fail when exports keep failing (after `recovery_duration`), and the
  Service stops sending traffic to that pod;
- **troubleshooting** is easier: `GET /status` shows which pipeline and component is
  unhealthy, and `GET /config` shows the running configuration.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Recommended | Readiness reflects whether data really reaches Elasticsearch. |
| Agent (DaemonSet) | Optional | Useful for troubleshooting; plain `health_check` is often enough. |

## Configuration

```yaml
extensions:
  healthcheckv2:
    use_v2: true
    component_health:
      include_permanent_errors: true
      include_recoverable_errors: true
      recovery_duration: 5m     # recoverable errors turn unhealthy after 5 minutes
    http:
      endpoint: 0.0.0.0:13133
      status:
        enabled: true
        path: /status
      config:
        enabled: true
        path: /config
    grpc:
      endpoint: 0.0.0.0:13132
```

Wired into the pipelines:

```yaml
service:
  extensions: [healthcheckv2]
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

Point probes at `/status`. Expose the gRPC port only if a load balancer uses
`grpc.health.v1`.

```yaml
ports:
  - {name: health, containerPort: 13133}
livenessProbe:
  httpGet: {path: /status, port: health}
  periodSeconds: 20
readinessProbe:
  httpGet: {path: /status, port: health}
  periodSeconds: 10
```

## Tips and pitfalls

- `use_v2: true` is required to get the per-component behaviour.
- Be careful using component health for **liveness**: if Elasticsearch is down, restarting every gateway pod won't fix it, and they would restart in a loop. Prefer it for readiness.
- `/config` shows configuration; keep the port internal to the cluster.

## Try it

```sh
./otelcol validate --config doc/extensions/healthcheckv2.example.yaml
```
