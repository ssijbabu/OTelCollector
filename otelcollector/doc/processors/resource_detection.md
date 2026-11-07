# `resource_detection` processor

Detects where the collector runs and adds host, OS, cloud and cluster attributes automatically.

Full example: [`resource_detection.example.yaml`](resource_detection.example.yaml)

## Use case on Kubernetes

Instead of hard-coding region, subscription and VM details, the collector asks the
environment: Azure's Instance Metadata Service (IMDS) for cloud details, AKS for the
cluster, and the OS for host name and type. On a node agent, that describes the node the
data came from.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Recommended | Describes the node where the data was produced. |
| Gateway (Deployment) | ⚠️ Careful | Describes the gateway's node, not the sender's. Use with `override: false` only. |

## Configuration

```yaml
processors:
  resource_detection:
    detectors: [env, azure, aks, system]
    timeout: 5s
    override: false
    system:
      hostname_sources: [os]
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, resource_detection, batch]
      exporters: [elasticsearch]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, resource_detection, batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, resource_detection, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

- `azure` and `aks` detectors call IMDS at `169.254.169.254`. It's reachable from AKS pods
  by default, but a NetworkPolicy with egress rules must allow it:

```yaml
# NetworkPolicy egress rule allowing Azure IMDS
egress:
  - to:
      - ipBlock: {cidr: 169.254.169.254/32}
    ports:
      - {port: 80, protocol: TCP}
```

## Tips and pitfalls

- Detectors run in order and the first one to set an attribute wins.
- `override: false` keeps values that the SDK or earlier processors already set.
- On a gateway, host attributes describe the gateway pod's node. That's misleading for data from other nodes.
- Detection runs once at startup; a failing detector delays startup by up to `timeout`.

## Try it

```sh
./otelcol validate --config doc/processors/resource_detection.example.yaml
```
