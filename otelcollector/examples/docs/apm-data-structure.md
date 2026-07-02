# APM Signal Data Structures

The APM pipeline routes telemetry through the Elastic APM Server, which normalises data into its own ECS-aligned schema. All documents land in ECS-namespaced data streams and carry APM Server enrichment fields (`observer`, `processor`, `transaction`/`span` top-level objects).

---

## Traces

**Data stream:** `traces-apm-default`  
**Backing index pattern:** `.ds-traces-apm-default-<date>-<seq>`

The APM Server writes two document types into the same data stream, distinguished by `processor.event`:

- `transaction` — the root or entry-point span for a service (inbound request)
- `span` — a child operation within a trace

### Transaction document

```jsonc
{
  "_index": ".ds-traces-apm-default-2026.06.30-000001",
  "_source": {
    // --- Timing ---
    "@timestamp": "2026-07-01T06:02:06.188Z",
    "timestamp": { "us": 1782885726188449 },   // high-resolution start time in microseconds

    // --- Trace context ---
    "trace": { "id": "73f0d100e7cc599475c139c30da4e13f" },
    "transaction": {
      "id": "d11833aac3d279f4",                 // also the root span ID
      "name": "Login",
      "type": "request",
      "result": "HTTP 2xx",
      "duration": { "us": 985 },
      "sampled": true,
      "representative_count": 1
    },
    "span": { "id": "d11833aac3d279f4" },       // span.id == transaction.id for root spans
    "parent": { "id": "80dd84d838587bda" },     // parent span from upstream service

    // --- Data stream routing ---
    "data_stream": {
      "type": "traces",
      "dataset": "apm",
      "namespace": "default"
    },

    // --- Service identity ---
    "service": {
      "name": "dotnet-login",
      "version": "1.0",
      "environment": "production",
      "language": { "name": "dotnet" },
      "framework": {
        "name": "OpenTelemetry.Instrumentation.AspNetCore",
        "version": "1.0.0.0"
      },
      "node": { "name": "09e689853e667ee0fb058dfe5f72db64b2941e8cac15817c7cf6fe868527a1e6" }
    },

    // --- Agent (added by APM Server from OTel resource attributes) ---
    "agent": {
      "name": "opentelemetry/dotnet",
      "version": "1.4.0.802"
    },

    // --- APM Server enrichment ---
    "observer": {
      "hostname": "apm-server-84496f8cf5-9zqkt",
      "type": "apm-server",
      "version": "8.17.3"
    },
    "processor": { "event": "transaction" },    // distinguishes transaction from span

    // --- HTTP context (ECS) ---
    "url": {
      "path": "/login",
      "original": "http://dotnet-login.elastiflix.svc.cluster.local/login",
      "scheme": "http",
      "domain": "dotnet-login.elastiflix.svc.cluster.local",
      "full": "http://dotnet-login.elastiflix.svc.cluster.local/login"
    },
    "http": {
      "request": { "method": "GET" },
      "response": { "status_code": 200 },
      "version": "1.1"
    },
    "user_agent": {
      "original": "axios/1.4.0",
      "name": "axios",
      "version": "1.4.0",
      "device": { "name": "Other" }
    },

    // --- Container ---
    "container": { "id": "09e689853e667ee0fb058dfe5f72db64b2941e8cac15817c7cf6fe868527a1e6" },

    // --- Outcome ---
    "event": {
      "ingested": "2026-07-01T06:02:08Z",
      "outcome": "success",
      "success_count": 1
    },

    // --- Custom attributes promoted to labels ---
    "labels": {
      "http_route": "Login",
      "telemetry_auto_version": "0.7.0"
    },
    "tags": ["_geoip_database_unavailable_GeoLite2-City.mmdb"]
  }
}
```

### Span document

```jsonc
{
  "_index": ".ds-traces-apm-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "2026-07-01T06:02:02.076Z",
    "timestamp": { "us": 1782885722076000 },

    "trace": { "id": "9c6aa2b7680962dd77a4c507f59ff5d4" },
    "span": {
      "id": "4af11960c1fae696",
      "name": "middleware - jsonParser",
      "type": "app",
      "subtype": "internal",
      "duration": { "us": 16 },
      "representative_count": 1
    },
    "parent": { "id": "5d51c1079a843d46" },    // parent transaction or span ID
    // NOTE: no "transaction" object — spans have span.* only

    "processor": { "event": "span" },           // key distinguisher

    "data_stream": {
      "type": "traces",
      "dataset": "apm",
      "namespace": "default"
    },

    "service": {
      "name": "node-server",
      "version": "1.0",
      "environment": "production",
      "language": { "name": "nodejs" },
      "framework": {
        "name": "@opentelemetry/instrumentation-express",
        "version": "0.33.1"
      }
    },
    "agent": {
      "name": "opentelemetry/nodejs",
      "version": "1.15.2"
    },
    "observer": {
      "hostname": "apm-server-84496f8cf5-9zqkt",
      "type": "apm-server",
      "version": "8.17.3"
    },

    "event": {
      "ingested": "2026-07-01T06:02:03Z",
      "outcome": "success",
      "success_count": 1
    },
    "labels": {
      "http_route": "/",
      "express_name": "jsonParser",
      "express_type": "middleware"
    }
  }
}
```

### Key fields — Traces

| Field | Type | Description | Example value |
|---|---|---|---|
| `@timestamp` | date | Wall-clock start time of the transaction/span | `2026-07-01T06:02:06.188Z` |
| `timestamp.us` | long | High-resolution start time in microseconds since epoch | `1782885726188449` |
| `trace.id` | keyword | 128-bit trace identifier (hex) | `73f0d100e7cc599475c139c30da4e13f` |
| `transaction.id` | keyword | ID of the transaction (root span) | `d11833aac3d279f4` |
| `span.id` | keyword | ID of this span (equals `transaction.id` for root spans) | `d11833aac3d279f4` |
| `parent.id` | keyword | Parent span ID; absent on true root spans | `80dd84d838587bda` |
| `processor.event` | keyword | Document type: `transaction` or `span` | `transaction` |
| `transaction.name` | keyword | Logical name of the transaction | `Login` |
| `transaction.type` | keyword | Transaction category | `request` |
| `transaction.result` | keyword | Outcome bucket | `HTTP 2xx` |
| `transaction.duration.us` | long | Duration in microseconds | `985` |
| `transaction.sampled` | boolean | Whether this trace was sampled | `true` |
| `span.name` | keyword | Logical name of the span | `middleware - jsonParser` |
| `span.type` | keyword | Span category | `app` |
| `span.subtype` | keyword | Span sub-category | `internal` |
| `span.duration.us` | long | Duration in microseconds | `16` |
| `service.name` | keyword | Service name | `dotnet-login` |
| `service.environment` | keyword | Deployment environment | `production` |
| `service.language.name` | keyword | Programming language | `dotnet` |
| `agent.name` | keyword | APM agent identifier | `opentelemetry/dotnet` |
| `observer.hostname` | keyword | APM Server pod that processed the doc | `apm-server-84496f8cf5-9zqkt` |
| `observer.type` | keyword | Always `apm-server` | `apm-server` |
| `event.outcome` | keyword | `success`, `failure`, or `unknown` | `success` |
| `event.ingested` | date | Time the APM Server indexed the document | `2026-07-01T06:02:08Z` |
| `url.full` | keyword | Full request URL (ECS) | `http://dotnet-login.elastiflix.svc.cluster.local/login` |
| `http.response.status_code` | integer | HTTP response status | `200` |
| `labels.*` | keyword | Custom OTel attributes promoted to flat key-value labels | `labels.http_route: "Login"` |
| `data_stream.type` | keyword | Always `traces` | `traces` |
| `data_stream.dataset` | keyword | Always `apm` | `apm` |

### Notes

- The APM Server translates incoming OTel spans into its own schema. OTel root spans (no parent or `transaction.root=true`) become `processor.event: transaction` documents; child spans become `processor.event: span` documents.
- `span.id` and `transaction.id` are always equal on a transaction document — APM Server sets both so APM UI queries work uniformly.
- OTel span attributes are split: HTTP/network attributes are promoted into ECS top-level objects (`url`, `http`, `user_agent`), while application-level custom attributes land in `labels.*`.
- `observer.*` fields are added by the APM Server itself — they are absent from the original OTel payload and identify which APM Server instance processed the trace.
- `tags` like `_geoip_database_unavailable_GeoLite2-City.mmdb` are processing warnings added by the APM Server pipeline when enrichment steps fail.

---

## Metrics — SDK

**Data stream:** `metrics-apm.app.<service_name>-default`  
**Example:** `.ds-metrics-apm.app.dotnet_login-default-2026.06.30-000001`

SDK metrics are raw OpenTelemetry metrics emitted by the application (e.g., .NET runtime metrics). The APM Server writes one document per metric reporting interval per service instance.

```jsonc
{
  "_index": ".ds-metrics-apm.app.dotnet_login-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "2026-07-01T06:01:39.051Z",

    // --- The metric itself: flat dotted name directly on _source ---
    "process.runtime.dotnet.jit.il_compiled.size": 754832,   // value in bytes

    "data_stream": {
      "type": "metrics",
      "dataset": "apm.app.dotnet_login",    // dataset encodes service name
      "namespace": "default"
    },

    // --- Metricset classification ---
    "metricset": { "name": "app" },

    // --- Service identity ---
    "service": {
      "name": "dotnet-login",
      "version": "1.0",
      "environment": "production",
      "language": { "name": "dotnet" },
      "framework": {
        "name": "OpenTelemetry.Instrumentation.Runtime",
        "version": "1.1.0.2"
      },
      "node": { "name": "09e689853e667ee0fb058dfe5f72db64b2941e8cac15817c7cf6fe868527a1e6" }
    },

    "agent": {
      "name": "opentelemetry/dotnet",
      "version": "1.4.0.802"
    },
    "observer": {
      "hostname": "apm-server-84496f8cf5-9zqkt",
      "type": "apm-server",
      "version": "8.17.3"
    },
    "container": { "id": "09e689853e667ee0fb058dfe5f72db64b2941e8cac15817c7cf6fe868527a1e6" },

    "event": { "ingested": "2026-07-01T06:01:40Z" },
    "labels": { "telemetry_auto_version": "0.7.0" },
    "tags": ["_geoip_database_unavailable_GeoLite2-City.mmdb"]
  }
}
```

### Key fields — SDK Metrics

| Field | Type | Description | Example value |
|---|---|---|---|
| `@timestamp` | date | Metric observation time | `2026-07-01T06:01:39.051Z` |
| `<metric.name>` | long/double | Metric value stored as a top-level dotted field | `process.runtime.dotnet.jit.il_compiled.size: 754832` |
| `metricset.name` | keyword | Always `app` for SDK metrics | `app` |
| `data_stream.dataset` | keyword | `apm.app.<service_name>` (dashes replace dots) | `apm.app.dotnet_login` |
| `service.name` | keyword | Service that emitted the metric | `dotnet-login` |
| `service.node.name` | keyword | Container ID used as node identifier | `09e689853e...` |
| `agent.name` | keyword | APM agent identifier | `opentelemetry/dotnet` |
| `observer.type` | keyword | Always `apm-server` | `apm-server` |
| `container.id` | keyword | Container runtime ID | `09e689853e...` |

### Notes

- OTel metric names (e.g., `process.runtime.dotnet.jit.il_compiled.size`) are written as-is as top-level dotted field names on the document. There is no `metrics.*` wrapper.
- One ES document can carry multiple metric names from the same OTel scope if they share the same attribute set and timestamp.
- The data stream dataset encodes the service name, so each service gets its own index.

---

## Metrics — Rollups

**Data streams:** `metrics-apm.service_transaction.1m-default`, `metrics-apm.service_destination.1m-default`, `metrics-apm.service_summary.1m-default`, `metrics-apm.transaction.1m-default`

APM Server aggregates raw trace data into four rollup metric types at 1-minute (and longer) intervals for use by the APM UI and Alerting. All rollup documents have `metricset.interval` set.

### service_transaction

Aggregated throughput and latency per service + transaction type. Lacks transaction name or result — used for service-level overviews.

```jsonc
{
  "_index": ".ds-metrics-apm.service_transaction.1m-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "2026-07-01T06:01:00.000Z",   // bucket start, floored to interval
    "_doc_count": 14,                            // number of source transactions in this bucket

    "data_stream": {
      "type": "metrics",
      "dataset": "apm.service_transaction.1m",
      "namespace": "default"
    },
    "metricset": { "name": "service_transaction", "interval": "1m" },

    "service": {
      "name": "dotnet-login",
      "environment": "production",
      "language": { "name": "dotnet" }
    },
    "agent": { "name": "opentelemetry/dotnet" },
    "observer": {
      "hostname": "apm-server-84496f8cf5-9zqkt",
      "type": "apm-server",
      "version": "8.17.3"
    },

    "transaction": {
      "type": "request",
      "duration.summary": { "sum": 12010, "value_count": 14 },   // total us, count
      "duration.histogram": {                                      // HDR histogram
        "values": [325, 407, 433, 489, 493, 515, 523, 787, 879, 987, 1095, 1143, 1183, 2751],
        "counts": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
      }
    },

    "event": {
      "ingested": "2026-07-01T06:02:01Z",
      "success_count": { "sum": 14, "value_count": 14 }
    },
    "labels": { "telemetry_auto_version": "0.7.0" }
  }
}
```

### service_destination

Aggregated throughput and latency per upstream service + downstream dependency. Used to render dependency maps.

```jsonc
{
  "_index": ".ds-metrics-apm.service_destination.1m-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "2026-07-01T06:01:00.000Z",
    "_doc_count": 18,

    "data_stream": {
      "type": "metrics",
      "dataset": "apm.service_destination.1m",
      "namespace": "default"
    },
    "metricset": { "name": "service_destination", "interval": "1m" },

    "service": {
      "name": "node-server",
      "environment": "production",
      "language": { "name": "nodejs" },
      "target": {                                         // the downstream dependency
        "name": "python-favorite.elastiflix.svc.cluster.local:5000",
        "type": "http"
      }
    },
    "agent": { "name": "opentelemetry/nodejs" },

    "span": {
      "name": "POST",
      "destination": {
        "service": {
          "resource": "python-favorite.elastiflix.svc.cluster.local:5000",
          "response_time": {
            "sum.us": 170791,   // total response time in microseconds
            "count": 18
          }
        }
      }
    },

    "event": {
      "ingested": "2026-07-01T06:02:01Z",
      "outcome": "success"
    }
  }
}
```

### service_summary

One document per service per interval. Used for service inventory and health checks.

```jsonc
{
  "_index": ".ds-metrics-apm.service_summary.1m-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "2026-07-01T06:01:00.000Z",

    "data_stream": {
      "type": "metrics",
      "dataset": "apm.service_summary.1m",
      "namespace": "default"
    },
    "metricset": { "name": "service_summary", "interval": "1m" },

    "service": {
      "name": "dotnet-login",
      "environment": "production",
      "language": { "name": "dotnet" }
    },
    "agent": { "name": "opentelemetry/dotnet" },
    "observer": {
      "hostname": "apm-server-84496f8cf5-9zqkt",
      "type": "apm-server",
      "version": "8.17.3"
    },

    // No metric payload — presence of the document itself is the signal
    "event": { "ingested": "2026-07-01T06:02:01Z" },
    "labels": { "telemetry_auto_version": "0.7.0" }
  }
}
```

### transaction

Aggregated latency per service + transaction name + result. Adds granularity below `service_transaction` (includes `transaction.name`).

```jsonc
{
  "_index": ".ds-metrics-apm.transaction.1m-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "2026-07-01T06:01:00.000Z",
    "_doc_count": 9,

    "data_stream": {
      "type": "metrics",
      "dataset": "apm.transaction.1m",
      "namespace": "default"
    },
    "metricset": { "name": "transaction", "interval": "1m" },

    "service": {
      "name": "dotnet-login",
      "version": "1.0",
      "environment": "production",
      "language": { "name": "dotnet" },
      "node": { "name": "09e689853e667ee0fb058dfe5f72db64b2941e8cac15817c7cf6fe868527a1e6" }
    },
    "agent": { "name": "opentelemetry/dotnet" },
    "container": { "id": "09e689853e667ee0fb058dfe5f72db64b2941e8cac15817c7cf6fe868527a1e6" },

    "transaction": {
      "name": "Login",            // present here but absent from service_transaction
      "type": "request",
      "result": "HTTP 2xx",       // present here but absent from service_transaction
      "duration.summary": { "sum": 7435, "value_count": 9 },
      "duration.histogram": {
        "values": [325, 407, 433, 489, 493, 515, 879, 1143, 2751],
        "counts": [1, 1, 1, 1, 1, 1, 1, 1, 1]
      }
    },

    "event": {
      "ingested": "2026-07-01T06:02:01Z",
      "outcome": "success",
      "success_count": { "sum": 9, "value_count": 9 }
    }
  }
}
```

### Key fields — Rollup Metrics

| Field | Type | Description | Example value |
|---|---|---|---|
| `@timestamp` | date | Bucket start time, floored to interval | `2026-07-01T06:01:00.000Z` |
| `_doc_count` | integer | Source document count in this bucket | `14` |
| `metricset.name` | keyword | Rollup type | `service_transaction` |
| `metricset.interval` | keyword | Aggregation window | `1m` |
| `service.name` | keyword | Service being aggregated | `dotnet-login` |
| `service.environment` | keyword | Deployment environment | `production` |
| `service.language.name` | keyword | Programming language | `dotnet` |
| `service.target.name` | keyword | Downstream dependency name (service_destination only) | `python-favorite.elastiflix.svc.cluster.local:5000` |
| `service.target.type` | keyword | Downstream dependency protocol (service_destination only) | `http` |
| `transaction.name` | keyword | Transaction name (transaction rollup only) | `Login` |
| `transaction.type` | keyword | Transaction category | `request` |
| `transaction.result` | keyword | Outcome bucket (transaction rollup only) | `HTTP 2xx` |
| `transaction.duration.summary.sum` | long | Total duration in microseconds | `12010` |
| `transaction.duration.summary.value_count` | long | Number of transactions | `14` |
| `transaction.duration.histogram` | histogram | HDR histogram of latencies (microseconds) | `{values:[325,...], counts:[1,...]}` |
| `span.destination.service.resource` | keyword | Dependency resource string (service_destination only) | `python-favorite.elastiflix.svc.cluster.local:5000` |
| `span.destination.service.response_time.sum.us` | long | Total downstream response time in us | `170791` |
| `span.destination.service.response_time.count` | long | Number of calls to dependency | `18` |
| `event.success_count.sum` | long | Count of successful transactions | `14` |
| `event.outcome` | keyword | `success`, `failure`, or `unknown` | `success` |

### Notes

- `_doc_count` is the Elasticsearch `doc_count` field used to represent pre-aggregated data. When Elasticsearch computes statistics over rollup indices it uses this field instead of counting documents.
- The `transaction` rollup adds `transaction.name` and `transaction.result` relative to `service_transaction`, enabling per-transaction-name breakdown in the APM UI.
- `service_summary` documents carry no metric payload — the APM UI queries them only to discover which services are active.
- Rollup intervals are 1m, 10m, and 60m. Only the 1m data streams are documented here; 10m and 60m follow the same schema with a longer `metricset.interval`.

---

## Logs

**Data stream:** `logs-kubernetes.container_logs-default`  
**Backing index:** `.ds-logs-kubernetes.container_logs-default-2026.07.01-000001`

Container logs are collected by Filebeat from `/var/log/containers/` and enriched with Kubernetes metadata. The pipeline does not use OTel log conventions.

```jsonc
{
  "_index": ".ds-logs-kubernetes.container_logs-default-2026.07.01-000001",
  "_source": {
    // --- Log body ---
    "message": "W0701 07:56:02.936455       1 watcher.go:331] watch chan error: etcdserver: mvcc: required revision has been compacted",

    // --- Timing ---
    "@timestamp": "2026-07-01T07:56:02.936Z",

    // --- Log file metadata ---
    "log": {
      "file": {
        "path": "/var/log/containers/kube-apiserver-docker-desktop_kube-system_kube-apiserver-14f75dca5ee1f519e4c531283cfe46063a34178dad9649138d7017f6201506fe.log"
      },
      "offset": 20420024    // byte offset within the log file
    },
    "stream": "stderr",     // stdout or stderr

    // --- Data stream routing ---
    "data_stream": {
      "type": "logs",
      "dataset": "kubernetes.container_logs",
      "namespace": "default"
    },

    // --- Kubernetes enrichment (added by Filebeat k8s metadata processor) ---
    "kubernetes": {
      "container": { "name": "kube-apiserver" },
      "pod": {
        "name": "kube-apiserver-docker-desktop",
        "uid": "d8a7fa76-d9cf-4a50-b92e-9977a1e6623d",
        "ip": "192.168.65.3"
      },
      "node": {
        "name": "docker-desktop",
        "hostname": "docker-desktop",
        "uid": "a478e5fc-c233-46cd-b911-254c189b04b8",
        "labels": {
          "kubernetes_io/hostname": "docker-desktop",
          "kubernetes_io/arch": "arm64",
          "kubernetes_io/os": "linux"
        }
      },
      "namespace": "kube-system",
      "namespace_uid": "b76c9e0b-35f1-4541-9c7b-a383e15d1777",
      "labels": {
        "component": "kube-apiserver",
        "tier": "control-plane"
      }
    },

    // --- Container image ---
    "container": {
      "id": "14f75dca5ee1f519e4c531283cfe46063a34178dad9649138d7017f6201506fe",
      "runtime": "docker",
      "image": { "name": "registry.k8s.io/kube-apiserver:v1.34.1" }
    },

    // --- Filebeat agent ---
    "agent": {
      "name": "docker-desktop",
      "id": "da4fe56c-ddef-4231-8f98-9a0f8cb2fa0a",
      "type": "filebeat",
      "version": "8.17.3"
    },

    // --- ECS ---
    "ecs": { "version": "8.0.0" },

    // --- Service identity (inferred from k8s metadata) ---
    "service": {
      "name": "kube-apiserver",
      "environment": "kube-system"
    },

    // --- Input ---
    "input": { "type": "container" },

    // --- Host ---
    "host": {
      "hostname": "docker-desktop",
      "architecture": "aarch64",
      "os": {
        "name": "Ubuntu",
        "version": "20.04.6 LTS (Focal Fossa)",
        "kernel": "6.12.54-linuxkit",
        "family": "debian",
        "platform": "ubuntu"
      }
    }
  }
}
```

### Key fields — Logs

| Field | Type | Description | Example value |
|---|---|---|---|
| `message` | text | Raw log line (the log body) | `W0701 07:56:02.936455 1 watcher.go:331] watch chan error: ...` |
| `@timestamp` | date | Log event timestamp (parsed from the log line or file mtime) | `2026-07-01T07:56:02.936Z` |
| `stream` | keyword | `stdout` or `stderr` | `stderr` |
| `log.file.path` | keyword | Path of the container log file on the host | `/var/log/containers/kube-apiserver-...log` |
| `log.offset` | long | Byte offset within the log file (Filebeat cursor) | `20420024` |
| `kubernetes.pod.name` | keyword | Pod name | `kube-apiserver-docker-desktop` |
| `kubernetes.pod.uid` | keyword | Pod UID | `d8a7fa76-d9cf-4a50-b92e-9977a1e6623d` |
| `kubernetes.namespace` | keyword | Kubernetes namespace | `kube-system` |
| `kubernetes.container.name` | keyword | Container name within the pod | `kube-apiserver` |
| `kubernetes.node.name` | keyword | Node the pod runs on | `docker-desktop` |
| `kubernetes.labels.*` | keyword | Pod labels (flattened, dots replaced with `_`) | `kubernetes.labels.component: "kube-apiserver"` |
| `container.id` | keyword | Container runtime ID | `14f75dca5ee1f51...` |
| `container.image.name` | keyword | Container image | `registry.k8s.io/kube-apiserver:v1.34.1` |
| `service.name` | keyword | Inferred from k8s metadata | `kube-apiserver` |
| `service.environment` | keyword | Set to the Kubernetes namespace | `kube-system` |
| `agent.type` | keyword | Always `filebeat` in this pipeline | `filebeat` |
| `input.type` | keyword | Always `container` | `container` |
| `data_stream.dataset` | keyword | `kubernetes.container_logs` | `kubernetes.container_logs` |

### Notes

- **No trace correlation** — APM pipeline container logs are collected by Filebeat directly and do not carry `trace.id` or `span.id`. Applications that emit structured JSON logs with OTel trace context (e.g., `otelTraceID`) will have those fields inside `message` but they are not extracted.
- **Severity** — Filebeat does not parse log severity from container log lines; the `log.level` field is absent. The raw line prefix (e.g., `W` for warning in the kube-apiserver log format) is embedded in `message`.
- **ECS-native** — these logs follow ECS conventions, not OTel log semconv. Compare to the OTel pipeline where logs use `body.text` and `attributes.*`.
- `service.environment` is set to the Kubernetes namespace by the Filebeat processor, not to an application-level environment label.
