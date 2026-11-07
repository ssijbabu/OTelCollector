# `file_storage` extension

Persistent key/value storage on local disk that other components use to survive restarts: read offsets and persistent export queues.

Full example: [`file_storage.example.yaml`](file_storage.example.yaml)

## Use case on Kubernetes

On a node agent tailing container logs, two kinds of state are lost when the pod restarts
(rollout, OOM, node drain):

1. **Read offsets:** without them, `file_log` either re-reads files (duplicates) or starts at
   the end (gaps).
2. **The export queue:** data waiting for a slow or unavailable Elasticsearch disappears.

`file_storage` keeps both on disk. On a DaemonSet, a `hostPath` directory survives pod
restarts on the same node.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Recommended | hostPath storage per node for log offsets and queue. |
| Gateway (Deployment) | ✅ Yes | PVC-backed persistent queue (one PVC per replica, so use a StatefulSet). |

## Configuration

```yaml
extensions:
  file_storage:
    directory: /var/lib/otelcol/storage
    create_directory: true
    timeout: 5s
    compaction:
      on_start: true
      on_rebound: true
      directory: /var/lib/otelcol/storage
```

Wired into the pipelines:

```yaml
service:
  extensions: [file_storage]
  pipelines:
    logs:
      receivers: [file_log]
      processors: [memory_limiter]
      exporters: [elasticsearch]
```

## Kubernetes requirements

A writable directory that outlives the pod:

- **DaemonSet:** `hostPath` with `type: DirectoryOrCreate`.
- **Gateway:** run as a **StatefulSet** with `volumeClaimTemplates`, so each replica keeps its
  own queue.

The collector user must be able to write there (`fsGroup` or an init container that sets
ownership).

```yaml
# DaemonSet (excerpt)
securityContext:
  fsGroup: 10001
containers:
  - name: otel-agent
    volumeMounts:
      - {name: otel-storage, mountPath: /var/lib/otelcol/storage}
volumes:
  - name: otel-storage
    hostPath:
      path: /var/lib/otelcol/storage
      type: DirectoryOrCreate
```

## Tips and pitfalls

- Never share one storage directory between pods. On a Deployment with a single PVC, replicas would corrupt each other's state; use a StatefulSet.
- Size the disk for `queue_size` × batch size during the longest outage you want to ride out.
- `compaction.on_rebound` shrinks the database after a backlog drains.

## Try it

```sh
./otelcol validate --config doc/extensions/file_storage.example.yaml
```
