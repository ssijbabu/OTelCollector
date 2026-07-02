# Infrastructure Metrics: EDOT vs Custom OTel Collector

This document explains how the EDOT kube-stack and APM Server collected infrastructure
and host metrics, and how the custom OTel Collector DaemonSet in this repo replaces that
role for the Kibana Infrastructure UI.

---

## TL;DR

| Component | Role | Kibana Infra UI? |
|---|---|---|
| **APM Server** | Receives OTLP from instrumented apps only | No |
| **EDOT kube-stack daemon** | Collected host + kubelet metrics in OTel-native format | No (wrong format) |
| **Custom OTel Collector DaemonSet** | Collects host + kubelet metrics in ECS format | **Yes** |

The Kibana Infrastructure inventory and host detail pages query `metrics-system.*` data streams.
Only the custom OTel Collector with the `elasticinframetrics` processor produces this format.

---

## APM Server

**Config** ([apm-server/configmap.yaml](apm-server/configmap.yaml)):
```yaml
apm-server:
  host: "0.0.0.0:8200"

output.elasticsearch:
  hosts: ["http://elasticsearch.elastic.svc.cluster.local:9200"]
  username: "elastic"
  password: "elastic123"
```

APM Server is a pure **application telemetry receiver**. It accepts OTLP/HTTP from
instrumented services and writes:

| Data stream | Content |
|---|---|
| `traces-apm-default` | Distributed traces from instrumented apps |
| `logs-apm.app.*` | Structured logs emitted by apps |
| `metrics-apm.app.*` | Custom metrics from SDKs |
| `metrics-apm.internal-default` | JVM/runtime metrics (thread count, GC) from EDOT Java agents |
| `metrics-apm.service_transaction.*` | Pre-aggregated latency histograms (APM rollups) |

**APM Server never touches host, OS, or Kubernetes infrastructure metrics.** It only
knows what instrumented applications send to it via OTLP.

---

## EDOT kube-stack Daemon

The [kube-stack](kube-stack/values.yaml) is an opinionated Helm chart that deploys three
collectors via the OTel Operator:

```
daemon (DaemonSet)
  receivers:  hostmetrics + kubeletstats + filelog + otlp
  processors: (default)
  → gateway (Deployment)
       processors: elastictrace, signaltometrics
       → Elasticsearch

clusterStats (Deployment)
  receivers:  k8s_cluster + k8sobjects
  → gateway
```

The daemon wrote infrastructure metrics in **OTel-native format**, producing:

| Data stream | Content |
|---|---|
| `metrics-hostmetricsreceiver-default` | Host CPU, memory, disk, network in OTel field names |
| `metrics-kubeletstatsreceiver-default` | Pod/container CPU, memory in OTel field names |
| `metrics-k8sclusterreceiver-default` | Deployment replicas, node conditions, pod phases |

**These OTel-native indices are not read by Kibana Infrastructure.** Kibana's inventory and
host detail pages (`/app/metrics/detail/host/*`) query `metrics-system.*` — the ECS-formatted
data streams — via the `metrics-*,metricbeat-*` index pattern. The kube-stack's OTel-native
output is queryable in Discover or custom dashboards but does not power the Infra UI.

The kube-stack gateway _can_ also write ECS format via an optional elasticinframetrics
step, but that requires explicit configuration that was not enabled in this repo's values.yaml.

---

## Custom OTel Collector DaemonSet (current setup)

**Config**: [collector/daemonset-configmap.yaml](collector/daemonset-configmap.yaml)

This DaemonSet is the direct replacement for the kube-stack daemon's infrastructure path.
It collects host and kubelet metrics and converts them to ECS format so Kibana
Infrastructure reads them correctly.

### Receiver: hostmetrics

Scrapes the host filesystem at `/hostfs` (mounted from the node root):

| Scraper | OTel metrics produced | Maps to ECS |
|---|---|---|
| `cpu` | `system.cpu.time`, `system.cpu.utilization` | `system.cpu.total.pct`, `system.cpu.cores` |
| `load` | `system.cpu.load_average.{1m,5m,15m}` | `system.load.{1,5,15}`, `system.load.cores` |
| `memory` | `system.memory.usage`, `system.memory.utilization` | `system.memory.actual.used.bytes`, `system.memory.actual.used.pct` |
| `disk` | `system.disk.io`, `system.disk.operations` | `system.diskio.*` |
| `filesystem` | `system.filesystem.usage`, `system.filesystem.utilization` | `system.filesystem.*` |
| `network` | `system.network.io`, `system.network.packets` | `system.network.*` |
| `paging` | `system.paging.usage`, `system.paging.operations` | `system.memory.swap.*` |
| `processes` | `system.processes.count` | `system.process.summary.*` (counts by state) |
| `process` | Per-process CPU, memory, FDs, threads | `system.process.*` per PID |

The `process` scraper (singular) is critical for Kibana's process list. It attaches
`process.pid`, `process.executable.name`, `process.command`, `process.command_line`, and
`process.owner` attributes to each data point.

### Receiver: kubeletstats

Queries the node's kubelet API at `https://${K8S_NODE_IP}:10250`:

| Metric group | OTel metrics | Maps to ECS |
|---|---|---|
| `node` | `k8s.node.cpu.usage`, `k8s.node.memory.usage` | `kubernetes.node.*` |
| `pod` | `k8s.pod.cpu.usage`, `k8s.pod.memory.usage` | `kubernetes.pod.*` |
| `container` | `k8s.container.cpu.usage`, `k8s.container.memory.usage` | `kubernetes.container.*` |

### Processor chain

```
memory_limiter → resourcedetection → resource → transform/infra_ecs_mode
  → elasticinframetrics → filter/drop_load_cores_from_cpu
  → transform/fix_process_command_line → batch
```

| Processor | Purpose |
|---|---|
| `resourcedetection` | Adds `host.name`, `os.type`, `host.os.platform` from the node OS |
| `resource` | Overrides `host.name` and `k8s.node.name` with `K8S_NODE_NAME` env var (pod hostname is not the node name) |
| `transform/infra_ecs_mode` | Sets `elastic.mapping.mode = "ecs"` on every scope so the ES exporter writes ECS-format documents |
| `elasticinframetrics` | Converts OTel metric names to ECS field names; computes derived fields (e.g., `system.memory.actual.used.pct` from usage/total); drops OTel originals |
| `filter/drop_load_cores_from_cpu` | Removes `system.load.cores` from the `system.cpu` dataset (it ends up there due to same scope/timestamp) — it's emitted correctly in `system.load` |
| `transform/fix_process_command_line` | Copies `system.process.cmdline` back to `process.command_line` — Kibana's process list groups by `process.command_line` but `elasticinframetrics` drops the original OTel attribute |

### Data streams produced

| Data stream | Kibana feature |
|---|---|
| `metrics-system.cpu-default` | Infrastructure host CPU gauge, per-core breakdown |
| `metrics-system.load-default` | Load average 1m/5m/15m |
| `metrics-system.memory-default` | Memory used %, swap |
| `metrics-system.diskio-default` | Disk read/write bytes and ops |
| `metrics-system.filesystem-default` | Filesystem used % per mount point |
| `metrics-system.network-default` | Network in/out bytes and packets |
| `metrics-system.process.summary-default` | Process counts by state (running/sleeping/zombie) |
| `metrics-system.process-default` | Per-process CPU, memory, command line, owner — drives the Processes tab |
| `metrics-kubernetes.node-default` | Kubernetes node CPU/memory |
| `metrics-kubernetes.pod-default` | Kubernetes pod CPU/memory, status |
| `metrics-kubernetes.container-default` | Kubernetes container CPU/memory, resource limits |

---

## Process List Specifics

Kibana's **Processes** tab (`/app/metrics/detail/host/{hostname}`) is powered by the
`POST /api/infra/host/{hostname}/processes` endpoint. The handler
(`@kbn/infra-plugin/server/lib/host_details/process_list.js`) queries `metrics-*,metricbeat-*`
and groups by `process.command_line` using a `terms` aggregation, sorted by CPU or memory.

Two requirements must both be met for the list to appear:

1. **The `process` scraper must be enabled** (distinct from `processes`). The `processes`
   scraper only produces aggregate counts; the `process` scraper produces per-PID data points
   with `process.command_line` as a dimension attribute.

2. **`process.command_line` must be present in the ES document.** `elasticinframetrics`
   maps `process.command_line` → `system.process.cmdline` and drops the original OTel
   attribute. Without the `transform/fix_process_command_line` step, the field is absent
   and the terms aggregation returns 0 buckets.

**Process state limitation**: The OTel `process` scraper never calls gopsutil `Status()`.
All processes show `system.process.state: "undefined"` → Kibana displays "Unknown" in the
State column. This is a hard limitation; Metricbeat is required for accurate process state.

---

## What Each Approach Does NOT Cover

| Gap | EDOT kube-stack | Custom OTel Collector | Workaround in this repo |
|---|---|---|---|
| Kibana Infra UI (metrics-system.*) | ✗ (OTel-native format) | ✓ | — |
| Container logs (Logs tab) | ✓ (filelog receiver in daemon) | ✗ | Filebeat DaemonSet |
| K8s cluster-level metrics | ✓ (clusterStats collector) | ✗ | Not deployed |
| Process running state | ✗ | ✗ | None (limitation) |
| App traces / APM metrics | ✗ | ✗ | APM Server |

---

## Equivalent Component Map

| EDOT kube-stack | This repo | Notes |
|---|---|---|
| daemon → hostmetrics | [collector/daemonset-configmap.yaml](collector/daemonset-configmap.yaml) hostmetrics | ECS mode adds `elasticinframetrics` |
| daemon → kubeletstats | [collector/daemonset-configmap.yaml](collector/daemonset-configmap.yaml) kubeletstats | Same receiver, ECS mode |
| daemon → filelog (container logs) | [filebeat/](filebeat/) DaemonSet | Different agent, same end result |
| gateway → elastictrace | Not deployed | Not needed without app OTLP traffic |
| gateway → signaltometrics | Not deployed | APM rollups via APM Server instead |
| clusterStats → k8s_cluster | Not deployed | No Kubernetes Inventory UI needed |
| APM Server | [apm-server/](apm-server/) | Unchanged |
