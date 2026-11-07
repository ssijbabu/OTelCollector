# `otlp_json` connector

Reads log lines that contain OTLP JSON and turns them back into real traces, metrics and logs.

Full example: [`otlp_json.example.yaml`](otlp_json.example.yaml)

## Use case on Kubernetes

Some workloads can't send OTLP over the network: batch Jobs in restricted namespaces,
air-gapped clusters, or systems that write telemetry to files with a file exporter. Those
files can be shipped (or written to a shared volume) and picked up by a collector, which
uses `file_log` + `otlp_json` to restore the original signals and send them to
Elasticsearch as if they had arrived over OTLP.

On Kubernetes this typically looks like a Job writing OTLP JSON to a PVC, and a collector
Deployment mounting the same PVC.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | Reads files from a shared volume. |
| Agent (DaemonSet) | Optional | If the files are written to node-local paths. |

## Configuration

```yaml
connectors:
  otlp_json: {}
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    logs/raw:
      receivers: [file_log/otlp]
      processors: [memory_limiter]
      exporters: [otlp_json]
    traces:
      receivers: [otlp_json]
      processors: [batch]
      exporters: [elasticsearch]
    metrics:
      receivers: [otlp_json]
      processors: [batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp_json]
      processors: [batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

A volume that contains the OTLP JSON files, mounted into the collector. With a
`ReadWriteMany` PVC shared with the producing Job:

```yaml
volumeMounts:
  - {name: otlp-files, mountPath: /data/otlp, readOnly: true}
volumes:
  - name: otlp-files
    persistentVolumeClaim:
      claimName: otlp-export   # ReadWriteMany, e.g. Azure Files
```

## Tips and pitfalls

- Each line must be one complete OTLP JSON export request (as written by the file exporter).
- Add `storage: file_storage` to the `file_log` receiver (see [`file_storage`](../extensions/file_storage.md)) so files aren't re-read after a restart.
- Lines that aren't valid OTLP JSON are dropped and logged.

## Try it

```sh
./otelcol validate --config doc/connectors/otlp_json.example.yaml
```
