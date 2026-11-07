# Falco alerts → OpenTelemetry Collector → Elasticsearch

Falco has no OTLP output, and its gRPC output is a **server** that clients pull from (and is
deprecated in recent releases). The simplest reliable path uses what this build already has:

```
Falco pod (DaemonSet)                         otel-collector agent (DaemonSet, same node)
  json_output: true                             file_log/falco
  stdout  ──► container runtime ──► /var/log/pods/falco_falco-*/falco/*.log ──► parse JSON ──► gateway ──► Elasticsearch
```

1. Falco writes each alert to **stdout as one JSON line**. Its own logs go to stderr.
2. The container runtime stores stdout in the pod's log file on the node.
3. The agent on each node reads that file, keeps only JSON alerts on stdout, parses them,
   and forwards them to the gateway like any other log.

Nothing extra to deploy, and no network path from Falco to the collector.

---

## 1. Configure Falco for JSON output

### Helm (`falcosecurity/falco` chart)

The chart's `falco:` values map one-to-one to `falco.yaml`:

```yaml
# falco-values.yaml
falco:
  json_output: true                    # one JSON object per alert
  json_include_output_property: true   # keep the human-readable "output" string
  json_include_tags_property: true     # keep rule tags (MITRE technique IDs etc.)
  stdout_output:
    enabled: true                      # alerts to stdout -> container log
  log_stderr: true                     # Falco's own logs to stderr (filtered out by the collector)
  buffered_outputs: false              # write each alert immediately
```

```sh
helm upgrade --install falco falcosecurity/falco -n falco --create-namespace \
  -f falco-values.yaml --reuse-values
```

Or with `--set` only:

```sh
helm upgrade falco falcosecurity/falco -n falco --reuse-values \
  --set falco.json_output=true \
  --set falco.json_include_output_property=true \
  --set falco.json_include_tags_property=true \
  --set falco.stdout_output.enabled=true
```

### Plain `falco.yaml`

```yaml
json_output: true
json_include_output_property: true
json_include_tags_property: true
stdout_output:
  enabled: true
log_stderr: true
buffered_outputs: false
```

Each option can also be overridden on the command line, e.g. `falco -o json_output=true`.

### Check it

```sh
kubectl -n falco logs ds/falco -c falco --tail=5

# Trigger test alerts with default rules: "Terminal shell in container" (interactive exec)
# and "Read sensitive file untrusted" (/etc/shadow).
kubectl run falco-test --image=busybox --restart=Never -- sleep 300
kubectl wait --for=condition=Ready pod/falco-test
kubectl exec -it falco-test -- sh -c 'cat /etc/shadow > /dev/null'
kubectl delete pod falco-test

# Alerts are logged by the Falco pod on the node where falco-test ran
kubectl -n falco logs -l app.kubernetes.io/name=falco -c falco --tail=50 | grep '^{'
```

An alert looks like this (one line, wrapped here):

```json
{"hostname":"aks-nodepool1-123-vmss000000",
 "output":"10:00:04.987654321: Notice A shell was spawned in a container with an attached terminal (...)",
 "output_fields":{"container.id":"3f1e2d","k8s.ns.name":"default","k8s.pod.name":"web-5d9c","proc.cmdline":"bash","user.name":"root"},
 "priority":"Notice",
 "rule":"Terminal shell in container",
 "source":"syscall",
 "tags":["T1059","container","maturity_stable","mitre_execution","shell"],
 "time":"2026-11-07T10:00:04.987654321Z"}
```

---

## 2. Configure the collector agent

Add this to the agent DaemonSet config ([`k8s/collector/configmap.yaml`](otelcollector/examples/k8s/collector/configmap.yaml)).
It reuses the agent's existing processors and `otlp_grpc/gateway` exporter.

```yaml
extensions:
  file_storage:
    directory: /var/lib/otelcol/file_storage   # file offsets survive agent restarts

receivers:
  file_log/falco:
    include:
      - /var/log/pods/falco_falco-*/falco/*.log   # <namespace>_<pod>_<uid>/<container>/
    start_at: end
    include_file_path: true                       # needed for the k8s.* attributes below
    storage: file_storage
    operators:
      # CRI log line -> body, log.iostream, k8s.namespace/pod/container resource attributes
      - type: container
        id: container
      # Keep only alerts: JSON lines on stdout. Falco's own logs (stderr) are dropped.
      - type: filter
        expr: 'attributes["log.iostream"] != "stdout" or not (body matches "^\\s*\\{")'
      - type: json_parser
        parse_from: body
        parse_to: attributes["falco"]
      # Timestamp = when Falco saw the event, not when the line was written
      - type: time_parser
        parse_from: attributes["falco"]["time"]
        layout_type: gotime
        layout: "2006-01-02T15:04:05.999999999Z07:00"
      - type: severity_parser
        parse_from: attributes["falco"]["priority"]
        mapping:
          fatal: [emergency, alert, critical]
          error: error
          warn: warning
          info: [notice, informational]
          debug: debug
      # Human-readable alert as the body; structured fields stay under attributes.falco
      - type: move
        from: attributes["falco"]["output"]
        to: body
      - type: add
        field: attributes["event.name"]
        value: falco.alert

service:
  extensions: [health_check, file_storage]      # keep the agent's existing extensions
  pipelines:
    logs/falco:
      receivers: [file_log/falco]
      processors: [memory_limiter, resource/cluster, resource_detection, batch]
      exporters: [otlp_grpc/gateway]
```

Adjust the `include` path if Falco runs in a different namespace, the pod name doesn't start
with `falco-`, or the container isn't named `falco`. The path pattern is
`/var/log/pods/<namespace>_<pod-name>_<pod-uid>/<container-name>/*.log`.

### DaemonSet changes ([`k8s/collector/deployment.yaml`](otelcollector/examples/k8s/collector/deployment.yaml))

The agent needs the node's pod logs (read-only) and a directory for offsets. Container log
files are owned by root on AKS nodes, so the agent must run as **root** to read them;
privilege escalation stays off and all capabilities stay dropped.

```yaml
spec:
  template:
    spec:
      securityContext:
        runAsUser: 0
        runAsGroup: 0
        seccompProfile: {type: RuntimeDefault}
      containers:
        - name: otel-collector
          securityContext:
            runAsNonRoot: false
            runAsUser: 0
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities: {drop: [ALL]}
          volumeMounts:
            - {name: varlogpods, mountPath: /var/log/pods, readOnly: true}
            - {name: otelcol-state, mountPath: /var/lib/otelcol}
      volumes:
        - name: varlogpods
          hostPath: {path: /var/log/pods, type: Directory}
        - name: otelcol-state
          hostPath: {path: /var/lib/otelcol, type: DirectoryOrCreate}
```

---

## 3. What arrives in Elasticsearch

| Field | Value |
|---|---|
| `@timestamp` | Falco's event time (`time`) |
| `severity_text` / `severity_number` | Falco priority (`Notice` → INFO, `Warning` → WARN, `Critical` → FATAL …) |
| `body.text` | the human-readable `output` line |
| `attributes.falco.rule`, `.priority`, `.source`, `.tags`, `.hostname` | rule metadata |
| `attributes.falco.output_fields.*` | event fields: `k8s.ns.name`, `k8s.pod.name`, `proc.cmdline`, `user.name`, `fd.name` … |
| `attributes.event.name` | `falco.alert` |
| `resource.attributes.k8s.*` | the **Falco** pod that reported it (from the log path), plus cluster/cloud attributes |

The pod that *triggered* the alert is in `attributes.falco.output_fields.k8s.ns.name` /
`.k8s.pod.name`, not in the resource attributes (those describe the Falco pod).

Example query in Kibana (KQL): `attributes.event.name : "falco.alert" and severity_number >= 13`.

---

## Notes

- **Volume:** alert volume depends on the rules. Tune noisy rules in Falco
  (`customRules`, rule exceptions) rather than filtering in the collector, so Falco does less work.
- **`evt.time` precision:** integer nanosecond fields in `output_fields` (such as `evt.time`)
  exceed JSON number precision and lose their last digits. Use the record timestamp instead.
- **Falcosidekick alternative:** to send alerts as OTLP over the network instead of through
  log files (for example, with routing to several destinations), run Falcosidekick and point
  its OTLP output at the gateway's `otlp` receiver. Check which OTLP signals your
  Falcosidekick version supports.
- **Verified:** the collector configuration above was tested with this repo's `otelcol`
  binary against sample Falco container logs (startup lines on stderr, JSON alerts and a
  non-JSON line on stdout): only the alerts were emitted, with correct timestamps and severities.
