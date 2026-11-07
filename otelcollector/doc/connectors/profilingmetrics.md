# `profilingmetrics` connector

EDOT connector that turns continuous-profiling data (stack-trace samples) into metrics, such as CPU samples per runtime.

Full example: [`profilingmetrics.example.yaml`](profilingmetrics.example.yaml)

## Use case on Kubernetes

Continuous profiling shows where CPU time goes inside each pod. Raw profiles are large and
expensive to keep for long. `profilingmetrics` derives compact metrics (CPU samples per
runtime: Go, JVM, Python, .NET, native, kernel) that are cheap to store with long retention,
while raw profiles are kept for a shorter time.

On Kubernetes, profiles usually come from an eBPF profiler on every node, or from SDK
profilers inside pods.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | Receives profiles from agents and SDKs. |
| Agent (DaemonSet) | Optional | Next to an eBPF profiler on each node. |

## Configuration

```yaml
connectors:
  profilingmetrics:
    flush_interval: 30s     # aggregate in memory for 30s before emitting metrics
    metrics:
      samples.frame_type:   # extra breakdown, off by default
        enabled: true
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    profiles:
      receivers: [otlp]
      processors: [memory_limiter]
      exporters: [profilingmetrics, elasticsearch]
    metrics/profiling:
      receivers: [profilingmetrics]
      processors: [batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

No RBAC. Profiles are still **experimental** in OpenTelemetry: start the collector with
the feature gate, for example in the container args:

```yaml
args:
  - --config=/etc/otelcol/config.yaml
  - --feature-gates=service.profilesSupport
```

## Tips and pitfalls

- Without the feature gate, the `profiles` pipeline fails to load.
- The eBPF profiling receiver isn't in this build (it's in `manifest-old.yaml`); add it to `manifest.yaml` to profile nodes without changing apps. It needs a privileged DaemonSet.
- `flush_interval` trades freshness for fewer, larger metric writes.

## Try it

```sh
./otelcol validate --feature-gates=service.profilesSupport \
  --config doc/connectors/profilingmetrics.example.yaml
```
