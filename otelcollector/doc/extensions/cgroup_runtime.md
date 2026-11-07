# `cgroup_runtime` extension

Sets Go's GOMAXPROCS and GOMEMLIMIT from the container's cgroup CPU and memory limits.

Full example: [`cgroup_runtime.example.yaml`](cgroup_runtime.example.yaml)

## Use case on Kubernetes

A collector pod with `limits: {cpu: 2, memory: 2Gi}` on a 32-core node: by default Go sees
32 CPUs and runs 32 threads, which get **CPU-throttled** by the 2-CPU quota (latency spikes).
Go's garbage collector also doesn't know about the 2Gi limit, so the heap can grow until
the pod is **OOM-killed**.

`cgroup_runtime` reads the limits at startup (and periodically, for in-place pod resizes) and
sets `GOMAXPROCS=2` and `GOMEMLIMIT≈1.6Gi`.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Yes | DaemonSets often have tight limits on big nodes. |
| Gateway (Deployment) | ✅ Yes | Every collector with resource limits benefits. |

## Configuration

```yaml
extensions:
  cgroup_runtime:
    gomaxprocs:
      enabled: true
    gomemlimit:
      enabled: true
      ratio: 0.8              # GOMEMLIMIT = 80% of the container memory limit
      refresh_interval: 30s   # pick up in-place pod resizes
```

Wired into the pipelines:

```yaml
service:
  extensions: [cgroup_runtime]
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

Set CPU and memory **limits** on the container. Without limits there's nothing to read and
the extension changes nothing.

```yaml
resources:
  requests: {cpu: 500m, memory: 1Gi}
  limits:   {cpu: "2",  memory: 2Gi}
```

## Tips and pitfalls

- Keep `gomemlimit.ratio` below the `memory_limiter` hard limit, so Go collects garbage before the limiter refuses data.
- If you set `GOMAXPROCS` / `GOMEMLIMIT` env vars yourself, don't also use this extension.
- `refresh_interval` picks up in-place pod resize (Kubernetes ≥ 1.33) without restarts.

## Try it

```sh
./otelcol validate --config doc/extensions/cgroup_runtime.example.yaml
```
