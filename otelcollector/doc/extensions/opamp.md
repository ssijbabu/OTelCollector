# `opamp` extension

Connects the collector to an OpAMP server for fleet management: identity, health and effective configuration.

Full example: [`opamp.example.yaml`](opamp.example.yaml)

## Use case on Kubernetes

With collectors in many clusters (an agent DaemonSet plus a gateway per cluster), it's hard
to answer "which version runs where, with which config, and is it healthy?". The `opamp`
extension registers each collector pod with a central OpAMP server, which shows all of them
in one place.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Yes | Every node's agent appears in the fleet view. |
| Gateway (Deployment) | ✅ Yes | Every gateway replica appears in the fleet view. |

## Configuration

```yaml
extensions:
  opamp:
    server:
      ws:
        endpoint: ${env:OPAMP_ENDPOINT}
        headers:
          Authorization: Bearer ${env:OPAMP_API_KEY}
    capabilities:
      reports_effective_config: true
      reports_health: true
    agent_description:
      non_identifying_attributes:
        k8s.cluster.name: ${env:K8S_CLUSTER_NAME}
        k8s.pod.name: ${env:POD_NAME}
        deployment.environment.name: production
```

Wired into the pipelines:

```yaml
service:
  extensions: [opamp]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

- Egress to the OpAMP server (WebSocket over 443 in the example).
- The server API key from a Secret.
- `POD_NAME` and `K8S_CLUSTER_NAME`, so `agent_description` labels each pod in the fleet
  view (see the example config):

```yaml
env:
  - name: OPAMP_ENDPOINT
    value: wss://opamp.example.com/v1/opamp
  - name: OPAMP_API_KEY
    valueFrom: {secretKeyRef: {name: otel-opamp, key: api-key}}
  - name: POD_NAME
    valueFrom: {fieldRef: {fieldPath: metadata.name}}
  - name: K8S_CLUSTER_NAME
    valueFrom: {configMapKeyRef: {name: otel-collector-env, key: K8S_CLUSTER_NAME}}
```

## Tips and pitfalls

- The extension only **reports**. Remote configuration changes need the OpAMP Supervisor, which manages the collector process.
- `reports_effective_config` sends the running config to the server; make sure the server is trusted and secrets in the config are acceptable to share.
- Each pod gets its own instance ID, so replicas show up separately.

## Try it

```sh
./otelcol validate --config doc/extensions/opamp.example.yaml
```
