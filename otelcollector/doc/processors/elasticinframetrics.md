# `elasticinframetrics` processor

EDOT processor that derives Elastic infrastructure metrics from kubelet / host metrics for Kibana's curated Kubernetes and Hosts UIs.

Full example: [`elasticinframetrics.example.yaml`](elasticinframetrics.example.yaml)

## Use case on Kubernetes

Kibana's **Infrastructure** and **Kubernetes** inventory pages expect Elastic-format metrics.
The `kubelet_stats` receiver produces OpenTelemetry metrics (`k8s.pod.cpu.usage`, ...).
`elasticinframetrics` adds the Elastic-compatible versions next to them, so both the OTel
dashboards and the Elastic curated UIs work from one collection.

On Kubernetes this runs on the node agents that scrape each node's kubelet.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Yes | Runs next to `kubelet_stats`, which must scrape its own node. |
| Gateway (Deployment) | Optional | Possible if agents forward raw kubelet metrics, but keeps more data on the wire. |

## Configuration

```yaml
processors:
  elasticinframetrics:
    add_k8s_metrics: true      # derive Elastic Kubernetes metrics from kubelet_stats
    add_system_metrics: false  # no host_metrics receiver in this pipeline
    drop_original: false       # keep the OTel metrics as well
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    metrics:
      receivers: [kubelet_stats]
      processors: [memory_limiter, elasticinframetrics, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

The `kubelet_stats` receiver in the example needs:

- RBAC: `get` on `nodes/stats` and `nodes/proxy`, `get`/`list` on `nodes`.
- `K8S_NODE_NAME` from the downward API, so each agent scrapes its own node.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: otel-collector-kubeletstats
rules:
  - apiGroups: [""]
    resources: [nodes, nodes/stats, nodes/proxy]
    verbs: [get, list]
---
# In the DaemonSet container spec:
env:
  - name: K8S_NODE_NAME
    valueFrom:
      fieldRef:
        fieldPath: spec.nodeName
```

## Tips and pitfalls

- Only use it with `kubelet_stats` or `host_metrics` data; other metrics pass through unchanged.
- `drop_original: true` saves storage if you only use Elastic's curated UIs.
- On AKS, `insecure_skip_verify: true` is common because kubelet serving certificates are self-signed.

## Try it

```sh
./otelcol validate --config doc/processors/elasticinframetrics.example.yaml
```
