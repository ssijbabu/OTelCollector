# `health_check` extension

Serves an HTTP endpoint that reports whether the collector is running, for Kubernetes liveness and readiness probes.

Full example: [`health_check.example.yaml`](health_check.example.yaml)

## Use case on Kubernetes

Kubernetes needs to know when a collector pod is alive (restart it if not) and ready (send
it traffic only once pipelines have started). `health_check` answers both on port 13133.
Every collector Deployment and DaemonSet in this repo uses it for its probes.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Always | Probes for every DaemonSet pod. |
| Gateway (Deployment) | ✅ Always | Probes, and readiness gates Service traffic. |

## Configuration

```yaml
extensions:
  health_check:
    endpoint: 0.0.0.0:13133
    path: /health
    response_body:
      healthy: OK
      unhealthy: NOT READY
```

Wired into the pipelines:

```yaml
service:
  extensions: [health_check]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

Probes on port 13133. If you use NetworkPolicies, allow the kubelet (node IPs) to reach
13133.

```yaml
ports:
  - {name: health, containerPort: 13133}
livenessProbe:
  httpGet: {path: /health, port: health}
  initialDelaySeconds: 10
  periodSeconds: 20
readinessProbe:
  httpGet: {path: /health, port: health}
  periodSeconds: 10
startupProbe:
  httpGet: {path: /health, port: health}
  failureThreshold: 12
  periodSeconds: 5
```

## Tips and pitfalls

- The `path` in the config must match the probes' `path`.
- It reports whether the collector is up, not whether exporters succeed. For that, use [`healthcheckv2`](healthcheckv2.md).
- Don't expose 13133 through a public Service.

## Try it

```sh
./otelcol validate --config doc/extensions/health_check.example.yaml
```
