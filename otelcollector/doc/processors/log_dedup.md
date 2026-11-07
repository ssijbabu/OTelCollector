# `log_dedup` processor

Collapses identical log records within an interval into one record with a repeat count.

Full example: [`log_dedup.example.yaml`](log_dedup.example.yaml)

## Use case on Kubernetes

A pod in `CrashLoopBackOff`, or a misconfigured client retrying in a tight loop, can print
the same line thousands of times per minute. Tailing container logs from
`/var/log/pods` ships every copy to Elasticsearch, costing storage and drowning the useful
lines. `log_dedup` keeps one record per minute with `log.repeat_count`.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Agent (DaemonSet) | ✅ Recommended | Duplicates come from one container on one node, so dedup there is exact. |
| Gateway (Deployment) | Optional | Works too, but duplicates have already crossed the network. |

## Configuration

```yaml
processors:
  log_dedup:
    interval: 60s
    log_count_attribute: log.repeat_count
    # Ignore fields that differ between otherwise identical lines.
    exclude_fields:
      - attributes.log.file.path
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    logs:
      receivers: [file_log]
      processors: [memory_limiter, log_dedup, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

The example tails container logs, so the agent DaemonSet needs:

- a read-only `hostPath` mount of `/var/log/pods`
- to run as a user that can read those files (often root, or a matching group)

DaemonSet volume for container logs:

```yaml
volumeMounts:
  - name: varlogpods
    mountPath: /var/log/pods
    readOnly: true
volumes:
  - name: varlogpods
    hostPath:
      path: /var/log/pods
```

## Tips and pitfalls

- Records are only identical if body, severity and attributes all match. Use `exclude_fields` for fields that differ, such as file path or offsets.
- Deduplicated records are emitted at the end of each `interval`, so logs are delayed by up to that interval.
- Exclude the collector's own pod logs from `file_log` to avoid feedback loops.

## Try it

```sh
./otelcol validate --config doc/processors/log_dedup.example.yaml
```
