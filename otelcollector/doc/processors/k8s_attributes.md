# `k8s_attributes` processor

Finds the Kubernetes pod that sent the data and adds pod, namespace, workload and node metadata, plus selected labels and annotations.

Full example: [`k8s_attributes.example.yaml`](k8s_attributes.example.yaml)

## Use case on Kubernetes

This is **the** Kubernetes processor. Without it, telemetry only carries what the SDK knows,
usually just `service.name`. With it, every span, log and metric gets `k8s.namespace.name`,
`k8s.deployment.name`, `k8s.pod.name`, `k8s.node.name`, container image and selected labels
(`app`, `team`), so in Kibana you can:

- filter everything by namespace or deployment
- jump from a trace to the pod's logs and metrics
- group by team using a namespace label

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Recommended | Pod IP association works because pods connect directly to their node's agent. |
| Gateway (Deployment) | ⚠️ Careful | The connection IP is the agent's, not the app's. Only works if the agent already set `k8s.pod.ip` / `k8s.pod.uid`. |

## Configuration

```yaml
processors:
  k8s_attributes:
    auth_type: serviceAccount
    passthrough: false
    filter:
      node_from_env_var: K8S_NODE_NAME
    extract:
      metadata:
        - k8s.namespace.name
        - k8s.deployment.name
        - k8s.statefulset.name
        - k8s.daemonset.name
        - k8s.pod.name
        - k8s.pod.uid
        - k8s.node.name
        - k8s.container.name
        - container.image.name
        - container.image.tag
      labels:
        - tag_name: app
          key: app.kubernetes.io/name
          from: pod
        - tag_name: team
          key: team
          from: namespace
      annotations:
        - tag_name: owner
          key: example.com/owner
          from: pod
    # How to match incoming data to a pod: SDK-provided IP/UID first, then the connection IP.
    pod_association:
      - sources:
          - from: resource_attribute
            name: k8s.pod.ip
      - sources:
          - from: resource_attribute
            name: k8s.pod.uid
      - sources:
          - from: connection
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, k8s_attributes, batch]
      exporters: [elasticsearch]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, k8s_attributes, batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, k8s_attributes, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

- **RBAC:** `get`/`watch`/`list` on pods, namespaces and nodes, plus replicasets in `apps`
  to resolve deployment names.
- **DaemonSet:** set `K8S_NODE_NAME` from the downward API and use `filter.node_from_env_var`,
  so each agent only watches pods on its own node. Without it, every agent caches every pod
  in the cluster.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: otel-collector-k8sattributes
rules:
  - apiGroups: [""]
    resources: [pods, namespaces, nodes]
    verbs: [get, watch, list]
  - apiGroups: [apps]
    resources: [replicasets]
    verbs: [get, watch, list]
---
# In the DaemonSet container spec:
env:
  - name: K8S_NODE_NAME
    valueFrom:
      fieldRef:
        fieldPath: spec.nodeName
```

## Tips and pitfalls

- Pod association order matters: SDK-provided `k8s.pod.ip` / `k8s.pod.uid` first, then the connection IP.
- Apps should send to the agent on their own node (`status.hostIP` via the downward API); otherwise the connection IP isn't the pod's.
- Pods using `hostNetwork: true` share the node IP and can't be told apart by IP.
- Pulling all labels (`key_regex: (.*)`) causes high cardinality in Elasticsearch. Pick the labels you need.
- `passthrough: true` on agents only adds the pod IP, leaving full lookups to a gateway; useful for very large clusters.

## Try it

```sh
./otelcol validate --config doc/processors/k8s_attributes.example.yaml
```
