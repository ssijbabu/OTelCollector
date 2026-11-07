# `k8s_observer` extension

Watches the Kubernetes API for pods, ports, nodes, services and ingresses. With `receiver_creator`, it starts a receiver for each matching endpoint.

Full example: [`k8s_observer.example.yaml`](k8s_observer.example.yaml)

## Use case on Kubernetes

**Dynamic discovery:** instead of listing scrape targets by hand, the collector reacts to
what's in the cluster. The example collects kubelet stats from **every node** from a single
Deployment: the observer reports each node, and `receiver_creator` starts one
`kubelet_stats` receiver per node, and stops it when the node leaves.

The same pattern works for pods: start a receiver for every pod with a given label or port
(for example, every Redis pod on port 6379), as soon as it's scheduled.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | Cluster-wide discovery from one Deployment (node example). |
| Agent (DaemonSet) | ✅ Yes | Pod discovery on the agent's own node (`observe_pods` + node filter). |

## Configuration

```yaml
extensions:
  k8s_observer:
    auth_type: serviceAccount
    observe_pods: false
    observe_nodes: true
```

Wired into the pipelines:

```yaml
service:
  extensions: [k8s_observer]
  pipelines:
    metrics:
      receivers: [receiver_creator]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

RBAC to `list`/`watch` the observed resources, plus whatever the created receivers need
(here: kubelet stats).

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: otel-k8s-observer}
rules:
  - apiGroups: [""]
    resources: [nodes, pods]
    verbs: [get, list, watch]
  - apiGroups: [""]
    resources: [nodes/stats, nodes/proxy]
    verbs: [get]
```

## Tips and pitfalls

- Backtick expressions (`` `endpoint` ``, `` `kubelet_endpoint_port` ``) are filled in per discovered endpoint.
- On a DaemonSet with `observe_pods`, set `node: ${env:K8S_NODE_NAME}` so each agent only discovers its own node's pods.
- Write tight `rule` expressions; a loose rule can start hundreds of receivers.
- Only receivers included in the build can be created. This build has `kubelet_stats`, `kafka_metrics` and the Kafka receiver, but not database receivers.

## Try it

```sh
./otelcol validate --config doc/extensions/k8s_observer.example.yaml
```
