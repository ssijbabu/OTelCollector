# `k8s_leader_elector` extension

Elects one leader among collector replicas with a Kubernetes Lease, so cluster-wide receivers run in exactly one pod.

Full example: [`k8s_leader_elector.example.yaml`](k8s_leader_elector.example.yaml)

## Use case on Kubernetes

Some receivers report **cluster-wide** state: `k8s_cluster` (deployments, nodes, pod
phases), `k8s_events`, `k8s_objects`. Running them on every replica duplicates every data
point; running a single replica means no high availability.

With `k8s_leader_elector`, run 2 replicas: only the Lease holder runs the receiver. If that
pod dies, the other takes over within the lease duration (about 15 s in the example).

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | A Deployment with 2+ replicas running cluster-wide receivers. |
| Agent (DaemonSet) | ❌ No | DaemonSets run node-local receivers that should run everywhere. |

## Configuration

```yaml
extensions:
  k8s_leader_elector:
    auth_type: serviceAccount
    lease_name: otel-cluster-receiver
    lease_namespace: ${env:K8S_NAMESPACE}
    lease_duration: 15s
    renew_deadline: 10s
    retry_period: 2s
```

Wired into the pipelines:

```yaml
service:
  extensions: [k8s_leader_elector]
  pipelines:
    metrics:
      receivers: [k8s_cluster]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

- RBAC on Leases in the collector's namespace, plus the read permissions `k8s_cluster` needs.
- `K8S_NAMESPACE` from the downward API.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: {name: otel-leader-election, namespace: otel-gateway}
rules:
  - apiGroups: [coordination.k8s.io]
    resources: [leases]
    verbs: [get, create, update]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: otel-k8s-cluster}
rules:
  - apiGroups: [""]
    resources: [events, namespaces, namespaces/status, nodes, nodes/spec, pods, pods/status,
                replicationcontrollers, replicationcontrollers/status, resourcequotas, services]
    verbs: [get, list, watch]
  - apiGroups: [apps]
    resources: [daemonsets, deployments, replicasets, statefulsets]
    verbs: [get, list, watch]
  - apiGroups: [batch]
    resources: [jobs, cronjobs]
    verbs: [get, list, watch]
  - apiGroups: [autoscaling]
    resources: [horizontalpodautoscalers]
    verbs: [get, list, watch]
---
# Deployment container env
env:
  - name: K8S_NAMESPACE
    valueFrom: {fieldRef: {fieldPath: metadata.namespace}}
```

## Tips and pitfalls

- Only receivers that support it (they take a `k8s_leader_elector` setting) are gated; other receivers keep running on all replicas.
- Use a distinct `lease_name` per group of receivers that should fail over together.
- `kubectl get lease -n <namespace>` shows which pod currently holds the lease.

## Try it

```sh
./otelcol validate --config doc/extensions/k8s_leader_elector.example.yaml
```
