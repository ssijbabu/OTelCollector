# OTel Signal Data Structures

The OTel pipeline sends telemetry directly from the OTel Collector to Elasticsearch via the Elasticsearch Exporter, bypassing the APM Server. Documents use native OTel conventions: a `resource.attributes` envelope, flat `attributes.*` bags, and an OTel-native schema rather than ECS. Rollup metrics are produced by the `signaltometrics` connector inside the collector and written to APM-compatible data streams so the APM UI can consume them.

---

## Traces

**Data stream:** `traces-generic.otel-default`  
**Backing index pattern:** `.ds-traces-generic.otel-default-<date>-<seq>`

All spans land in the same data stream regardless of whether they are root spans or child spans. The distinction is made by the presence of `parent_span_id` and by APM-compatibility attributes embedded in `attributes.*`.

### Server span (transaction / root span)

```jsonc
{
  "_index": ".ds-traces-generic.otel-default-2026.07.01-000001",
  "_source": {
    // --- Timing ---
    // NOTE: @timestamp is a nanosecond float encoded as a string, not an ISO date
    "@timestamp": "1782896782679.290800",       // nanoseconds since epoch, stored as string

    // --- Trace context ---
    "trace_id": "84c2a52f3b1af9351f60f22da705ebc1",   // 128-bit hex, no hyphens
    "span_id": "d189324544c77f40",
    // parent_span_id absent — this is a root span
    "name": "Login",
    "kind": "Server",                            // OTel SpanKind: Server, Client, Internal, Producer, Consumer
    "duration": 1283700,                         // duration in nanoseconds

    // --- Data stream routing ---
    "data_stream": {
      "type": "traces",
      "dataset": "generic.otel",
      "namespace": "default"
    },

    // --- OTel span attributes (flat bag) ---
    "attributes": {
      // HTTP semconv (OTel v1 / pre-stable)
      "http.method": "GET",
      "http.scheme": "http",
      "http.target": "/login",
      "http.url": "http://10.1.1.199:80/login",
      "http.flavor": "1.1",
      "http.route": "Login",
      "http.status_code": 200,
      "net.host.name": "10.1.1.199",
      "http.user_agent": "kube-probe/1.34",

      // APM compatibility attributes injected by the collector pipeline
      "timestamp.us": 1782896782679290,         // start time in microseconds (for APM UI)
      "transaction.sampled": true,
      "transaction.id": "d189324544c77f40",
      "transaction.root": true,
      "transaction.name": "Login",
      "processor.event": "transaction",         // "transaction" marks this as a root span
      "transaction.representative_count": 1.0,
      "transaction.duration.us": 1283,
      "transaction.type": "request",
      "transaction.result": "HTTP 2xx",
      "event.outcome": "success",
      "event.success_count": 1,
      "user_agent.original": "kube-probe/1.34",
      "user_agent.name": "Other"
    },

    // --- OTel links and status ---
    "links": [],
    "status": {},                               // empty means StatusCode=Unset / OK

    // --- Resource attributes (service + SDK identity) ---
    "resource": {
      "attributes": {
        "service.name": "dotnet-login",
        "service.version": "1.0",
        "deployment.environment": "production",
        "telemetry.sdk.name": "opentelemetry",
        "telemetry.sdk.language": "dotnet",
        "telemetry.sdk.version": "1.4.0.802",
        "telemetry.auto.version": "0.7.0",
        "container.id": "4abd94d42718d56a5232b7909e712d9da414344da3b71175a251c4e36d0498de",
        // APM compatibility attributes added by the collector
        "agent.name": "opentelemetry/dotnet",
        "agent.version": "1.4.0.802",
        "service.instance.id": "4abd94d42718d56a5232b7909e712d9da414344da3b71175a251c4e36d0498de"
      }
    },

    // --- Instrumentation scope ---
    "scope": {
      "name": "OpenTelemetry.Instrumentation.AspNetCore",
      "version": "1.0.0.0",
      "attributes": {
        "service.framework.name": "OpenTelemetry.Instrumentation.AspNetCore",
        "service.framework.version": "1.0.0.0"
      }
    }
  }
}
```

### Client span (child span)

```jsonc
{
  "_index": ".ds-traces-generic.otel-default-2026.07.01-000001",
  "_source": {
    "@timestamp": "1782896790577.552835",

    "trace_id": "48ef0f851419e2e9ece300305768a1e0",
    "span_id": "3b5346ea86d03a99",
    "parent_span_id": "72a5cd5ee1e7873e",      // present on child spans
    "name": "SMEMBERS",
    "kind": "Client",                           // outbound call to a dependency
    "duration": 321875,                         // nanoseconds

    "data_stream": {
      "type": "traces",
      "dataset": "generic.otel",
      "namespace": "default"
    },

    "attributes": {
      // DB semconv
      "db.statement": "SMEMBERS ?",
      "db.system": "redis",
      "db.redis.database_index": 0,
      "net.peer.name": "redis.elastiflix.svc.cluster.local",
      "net.peer.port": "6379",
      "net.transport": "ip_tcp",

      // APM compatibility attributes
      "timestamp.us": 1782896790577552,
      "processor.event": "span",               // "span" marks this as a child span
      "span.representative_count": 1.0,
      "span.type": "db",
      "span.subtype": "redis",
      "span.duration.us": 321,
      "event.outcome": "success",
      "event.success_count": 1,

      // Dependency identification for service map
      "service.target.type": "redis",
      "service.target.name": "",
      "span.destination.service.resource": "redis"
    },

    "links": [],
    "status": {},

    "resource": {
      "attributes": {
        "service.name": "python-favorite",
        "service.version": "1.0",
        "deployment.environment": "production",
        "telemetry.sdk.name": "opentelemetry",
        "telemetry.sdk.language": "python",
        "telemetry.sdk.version": "1.19.0",
        "telemetry.auto.version": "0.40b0",
        "agent.name": "opentelemetry/python",
        "agent.version": "1.19.0"
      }
    },

    "scope": {
      "name": "opentelemetry.instrumentation.redis",
      "version": "0.40b0",
      "attributes": {
        "service.framework.name": "opentelemetry.instrumentation.redis",
        "service.framework.version": "0.40b0"
      }
    }
  }
}
```

### Key fields — Traces

| Field | Type | Description | Example value |
|---|---|---|---|
| `@timestamp` | keyword (string) | Nanoseconds since epoch as a string — not a date type | `"1782896782679.290800"` |
| `trace_id` | keyword | 128-bit trace ID, lowercase hex | `84c2a52f3b1af9351f60f22da705ebc1` |
| `span_id` | keyword | 64-bit span ID, lowercase hex | `d189324544c77f40` |
| `parent_span_id` | keyword | Parent span ID; absent on root spans | `72a5cd5ee1e7873e` |
| `name` | keyword | Span name | `Login` |
| `kind` | keyword | OTel SpanKind | `Server`, `Client`, `Internal` |
| `duration` | long | Span duration in **nanoseconds** | `1283700` |
| `status.code` | keyword | OTel status code | `Unset`, `Ok`, `Error` |
| `attributes.processor.event` | keyword | APM compat: `transaction` (root) or `span` (child) | `transaction` |
| `attributes.transaction.id` | keyword | APM compat: transaction ID for root spans | `d189324544c77f40` |
| `attributes.transaction.root` | boolean | APM compat: true when this is a root span | `true` |
| `attributes.transaction.name` | keyword | APM compat: transaction name | `Login` |
| `attributes.transaction.type` | keyword | APM compat: transaction type | `request` |
| `attributes.transaction.result` | keyword | APM compat: result bucket | `HTTP 2xx` |
| `attributes.transaction.duration.us` | long | APM compat: duration in microseconds | `1283` |
| `attributes.span.type` | keyword | APM compat: span type for child spans | `db` |
| `attributes.span.subtype` | keyword | APM compat: span sub-type | `redis` |
| `attributes.span.duration.us` | long | APM compat: child span duration in microseconds | `321` |
| `attributes.timestamp.us` | long | APM compat: start time in microseconds | `1782896782679290` |
| `attributes.event.outcome` | keyword | APM compat: `success`, `failure`, `unknown` | `success` |
| `attributes.service.target.type` | keyword | Downstream dependency protocol | `redis` |
| `attributes.span.destination.service.resource` | keyword | Dependency resource identifier | `redis` |
| `attributes.http.method` | keyword | HTTP method (OTel v1 semconv) | `GET` |
| `attributes.http.status_code` | integer | HTTP response status | `200` |
| `attributes.db.system` | keyword | Database system type | `redis` |
| `attributes.db.statement` | keyword | Sanitised query | `SMEMBERS ?` |
| `resource.attributes.service.name` | keyword | Service name | `dotnet-login` |
| `resource.attributes.deployment.environment` | keyword | Environment | `production` |
| `resource.attributes.telemetry.sdk.language` | keyword | SDK language | `dotnet` |
| `resource.attributes.agent.name` | keyword | APM compat: agent name | `opentelemetry/dotnet` |
| `scope.name` | keyword | Instrumentation library name | `OpenTelemetry.Instrumentation.AspNetCore` |

### Notes

- `@timestamp` is stored as a nanosecond float string (`"1782896782679.290800"`), not an ISO-8601 date. This differs from both the APM pipeline (ISO date) and ECS convention. Time-based queries in Kibana require mapping this field as a date with `format: epoch_nanos` or similar.
- There is no `processor.event` top-level field — the APM compat value is nested under `attributes.processor.event`. This is the key difference from the APM pipeline where it is a top-level object.
- APM compatibility attributes (`transaction.*`, `span.*`, `event.outcome`, `service.target.*`, etc.) are added by the OTel Collector pipeline so the APM UI can render traces from this data stream.
- `resource.attributes` is the OTel resource envelope. Fields like `service.name`, `deployment.environment`, and `telemetry.sdk.*` follow OTel semconv and live here rather than at the top level.
- `scope` identifies the instrumentation library, equivalent to the APM `service.framework.*` fields.
- No `observer.*` fields — documents in this pipeline are not processed by the APM Server.

---

## Metrics — SDK

**Data stream:** `metrics-generic.otel-default`  
**Backing index:** `.ds-metrics-generic.otel-default-<date>-<seq>`

SDK metrics are raw OTel metrics from the application. The Elasticsearch Exporter writes them using an OTel-native layout: a `metrics` object containing metric names as keys, and a `resource` envelope for service identity.

```jsonc
{
  "_index": ".ds-metrics-generic.otel-default-2026.07.01-000001",
  "_source": {
    "@timestamp": 1782896830111,              // milliseconds since epoch as a number
    "start_timestamp": 1782893890024,         // metric collection start time (ms)

    "data_stream": {
      "type": "metrics",
      "dataset": "generic.otel",
      "namespace": "default"
    },

    // --- Resource (service identity) ---
    "resource": {
      "attributes": {
        "service.name": "dotnet-login",
        "service.version": "1.0",
        "deployment.environment": "production",
        "telemetry.sdk.name": "opentelemetry",
        "telemetry.sdk.language": "dotnet",
        "telemetry.sdk.version": "1.4.0.802",
        "telemetry.auto.version": "0.7.0",
        "container.id": "4abd94d42718d56a5232b7909e712d9da414344da3b71175a251c4e36d0498de"
      }
    },

    // --- Instrumentation scope ---
    "scope": {
      "name": "OpenTelemetry.Instrumentation.Runtime",
      "version": "1.1.0.2"
    },

    // --- Metrics payload: object with metric names as keys ---
    "metrics": {
      "process.runtime.dotnet.assemblies.count": 142   // gauge value
    },

    "_metric_names_hash": "20b59072ce2b5bdd"           // hash for deduplication
  }
}
```

### Key fields — SDK Metrics

| Field | Type | Description | Example value |
|---|---|---|---|
| `@timestamp` | long | Metric observation time in **milliseconds** since epoch | `1782896830111` |
| `start_timestamp` | long | Cumulative metric collection start in milliseconds | `1782893890024` |
| `metrics.<name>` | long/double/object | Metric value(s); key is the OTel metric name | `metrics.process.runtime.dotnet.assemblies.count: 142` |
| `_metric_names_hash` | keyword | Hash of metric names in this document (for deduplication) | `20b59072ce2b5bdd` |
| `resource.attributes.service.name` | keyword | Service that emitted the metric | `dotnet-login` |
| `resource.attributes.deployment.environment` | keyword | Deployment environment | `production` |
| `resource.attributes.container.id` | keyword | Container ID | `4abd94d42718...` |
| `resource.attributes.telemetry.sdk.language` | keyword | SDK language | `dotnet` |
| `scope.name` | keyword | Instrumentation library | `OpenTelemetry.Instrumentation.Runtime` |
| `data_stream.dataset` | keyword | Always `generic.otel` for SDK metrics | `generic.otel` |

### Notes

- In contrast to the APM pipeline (where metric values are top-level dotted fields), here all metric values are nested under a `metrics` object. Multiple metrics sharing the same resource attributes and timestamp are colocated in one document under `metrics`.
- `@timestamp` is a long integer (milliseconds), not a string or ISO date.
- There is no `metricset.name` or `observer.*` — this is a plain OTel export.
- `_metric_names_hash` is used by the Elasticsearch Exporter to avoid writing duplicate documents during retries.

---

## Metrics — Rollups

**Data streams:** `metrics-apm.service_transaction.1m-default`, `metrics-apm.service_destination.1m-default`, `metrics-apm.service_summary.1m-default`, `metrics-apm.transaction.1m-default`

The `signaltometrics` connector inside the OTel Collector computes APM-compatible rollup metrics from the trace stream and routes them to the same APM data streams as the APM Server uses. This makes the APM UI work without needing an APM Server.

Documents are structurally similar to APM pipeline rollups but have some differences: `processor.event` is present, `signal_to_metrics.*` carries connector metadata, and some numeric types are floats instead of integers.

### service_transaction

```jsonc
{
  "_index": ".ds-metrics-apm.service_transaction.1m-default-2026.07.01-000001",
  "_source": {
    "@timestamp": "2026-07-01T09:06:06.427659838Z",
    "_doc_count": 6,

    "data_stream": {
      "type": "metrics",
      "dataset": "apm.service_transaction.1m",
      "namespace": "default"
    },
    "metricset": { "name": "service_transaction", "interval": "1m" },
    "processor": { "event": "metric" },         // present here; absent in APM pipeline rollups

    "service": {
      "name": "python-favorite",
      "environment": "production",
      "language": { "name": "python" }
    },
    "agent": { "name": "opentelemetry/python" },

    // Connector metadata (absent in APM pipeline rollups)
    "signal_to_metrics": {
      "service": {
        "instance": { "id": "2bbd660e-676c-45dc-9962-cec9f19d913d" }
      }
    },

    "transaction": {
      "type": "request",
      "root": true,                             // this rollup covers only root transactions
      "duration": {
        "histogram": {
          "values": [2351.27, 2468.70, 2677.60, 4626.76, 4702.54],
          "counts": [1, 1, 2, 1, 1]
        },
        "summary": { "sum": 19510.0, "value_count": 6 }
      }
    },

    "event": {
      "ingested": "2026-07-01T09:07:10Z",
      "success_count": { "sum": 6.0, "value_count": 6 }
    },

    // Elasticsearch mapping hint for _doc_count field
    "elasticsearch": {
      "mapping": { "hints": ["_doc_count"] }
    },

    "tags": ["_geoip_database_unavailable_GeoLite2-City.mmdb"]
  }
}
```

### service_destination

```jsonc
{
  "_index": ".ds-metrics-apm.service_destination.1m-default-2026.07.01-000001",
  "_source": {
    "@timestamp": "2026-07-01T09:06:06.427659838Z",

    "data_stream": {
      "type": "metrics",
      "dataset": "apm.service_destination.1m",
      "namespace": "default"
    },
    "metricset": { "name": "service_destination", "interval": "1m" },
    "processor": { "event": "metric" },

    "service": {
      "name": "python-favorite",
      "environment": "production",
      "language": { "name": "python" },
      "target": { "name": "", "type": "redis" }
    },
    "agent": { "name": "opentelemetry/python" },

    "signal_to_metrics": {
      "service": { "instance": { "id": "2bbd660e-676c-45dc-9962-cec9f19d913d" } }
    },

    "span": {
      "name": "SMEMBERS",
      "destination": {
        "service": {
          "resource": "redis",
          "response_time": {
            "count": 6,
            "sum": { "us": 2812.0 }   // NOTE: sum is nested object here vs sum.us in APM pipeline
          }
        }
      }
    },

    "event": {
      "ingested": "2026-07-01T09:07:10Z",
      "outcome": "success"
    }
  }
}
```

### service_summary

```jsonc
{
  "_index": ".ds-metrics-apm.service_summary.1m-default-2026.07.01-000001",
  "_source": {
    "@timestamp": "2026-07-01T09:06:06.427659838Z",

    "data_stream": {
      "type": "metrics",
      "dataset": "apm.service_summary.1m",
      "namespace": "default"
    },
    "metricset": { "name": "service_summary", "interval": "1m" },
    "processor": { "event": "metric" },

    "service": {
      "name": "python-favorite",
      "environment": "production",
      "language": { "name": "python" }
    },
    "agent": { "name": "opentelemetry/python" },

    "signal_to_metrics": {
      "service": { "instance": { "id": "2bbd660e-676c-45dc-9962-cec9f19d913d" } }
    },

    "service_summary": 12,             // span/transaction count for this service in the window

    "event": { "ingested": "2026-07-01T09:07:10Z" }
  }
}
```

### transaction

```jsonc
{
  "_index": ".ds-metrics-apm.transaction.1m-default-2026.07.01-000001",
  "_source": {
    "@timestamp": "2026-07-01T09:06:06.427659838Z",
    "_doc_count": 6,

    "data_stream": {
      "type": "metrics",
      "dataset": "apm.transaction.1m",
      "namespace": "default"
    },
    "metricset": { "name": "transaction", "interval": "1m" },
    "processor": { "event": "metric" },

    "service": {
      "name": "python-favorite",
      "version": "1.0",
      "environment": "production",
      "language": { "name": "python", "version": "1.19.0" }
    },
    "agent": { "name": "opentelemetry/python" },

    "signal_to_metrics": {
      "service": { "instance": { "id": "2bbd660e-676c-45dc-9962-cec9f19d913d" } }
    },

    "transaction": {
      "name": "/favorites",
      "type": "request",
      "result": "HTTP 2xx",
      "root": true,
      "duration": {
        "histogram": {
          "values": [2351.27, 2468.70, 2677.60, 4626.76, 4702.54],
          "counts": [1, 1, 2, 1, 1]
        },
        "summary": { "sum": 19510.0, "value_count": 6 }
      }
    },

    "event": {
      "ingested": "2026-07-01T09:07:10Z",
      "success_count": { "sum": 6.0, "value_count": 6 },
      "outcome": "success"
    },

    "elasticsearch": {
      "mapping": { "hints": ["_doc_count"] }
    }
  }
}
```

### Key fields — Rollup Metrics

| Field | Type | Description | Example value |
|---|---|---|---|
| `@timestamp` | date | Bucket timestamp (ISO-8601 with nanoseconds) | `2026-07-01T09:06:06.427659838Z` |
| `_doc_count` | integer | Source span/transaction count in this bucket | `6` |
| `metricset.name` | keyword | Rollup type | `service_transaction` |
| `metricset.interval` | keyword | Aggregation window | `1m` |
| `processor.event` | keyword | Always `metric` in OTel pipeline rollups | `metric` |
| `service.name` | keyword | Service name | `python-favorite` |
| `service.language.name` | keyword | Programming language | `python` |
| `service.target.type` | keyword | Downstream dependency protocol (service_destination) | `redis` |
| `transaction.root` | boolean | True when rollup covers only root transactions | `true` |
| `transaction.name` | keyword | Transaction name (transaction rollup only) | `/favorites` |
| `transaction.type` | keyword | Transaction category | `request` |
| `transaction.result` | keyword | Outcome bucket (transaction rollup only) | `HTTP 2xx` |
| `transaction.duration.summary.sum` | double | Total duration in microseconds | `19510.0` |
| `transaction.duration.summary.value_count` | long | Count of transactions | `6` |
| `transaction.duration.histogram` | histogram | HDR histogram of latencies | `{values:[2351,...], counts:[1,...]}` |
| `span.destination.service.resource` | keyword | Dependency resource (service_destination) | `redis` |
| `span.destination.service.response_time.count` | long | Call count to dependency | `6` |
| `span.destination.service.response_time.sum.us` | double | Total response time in microseconds | `2812.0` |
| `service_summary` | integer | Signal count for this service (service_summary) | `12` |
| `signal_to_metrics.service.instance.id` | keyword | Service instance UUID from the connector | `2bbd660e-676c-45dc-9962-cec9f19d913d` |
| `elasticsearch.mapping.hints` | keyword[] | Mapping hints; `["_doc_count"]` enables pre-agg counting | `["_doc_count"]` |

### Notes

- These rollups land in `metrics-apm.*` data streams (same as the APM pipeline), making them readable by the APM UI without modification.
- `signal_to_metrics.*` fields are injected by the `signaltometrics` connector and carry the service instance ID that produced the rollup. These are absent from APM Server-produced rollups.
- `processor.event: metric` is present here but absent from APM Server rollups — a schema difference to be aware of when querying across both pipelines.
- `elasticsearch.mapping.hints: ["_doc_count"]` tells Elasticsearch to interpret `_doc_count` as the pre-aggregation count, enabling correct statistics computation over rolled-up data.
- Duration values in `transaction.duration.summary.sum` and histogram `values` are in **microseconds** (matching APM Server convention), even though the raw trace `duration` field is in nanoseconds.
- `span.destination.service.response_time.sum` is a nested object (`{ "us": 2812.0 }`) here, whereas in the APM pipeline it is a dotted flat field (`response_time.sum.us`).

---

## Logs

**Data stream:** `logs-generic.otel-default`  
**Backing index:** `.ds-logs-generic.otel-default-<date>-<seq>`

Logs are collected by the OTel Collector's `filelog` receiver and stored in OTel log schema. The log body is in `body.text`, structured attributes are in `attributes.*`, and service identity is in `resource.attributes`.

```jsonc
{
  "_index": ".ds-logs-generic.otel-default-2026.07.01-000001",
  "_source": {
    // --- Timing ---
    "@timestamp": "1782896820578.246377",          // nanoseconds since epoch, as string
    "observed_timestamp": "1782896820724.742294",  // when the collector observed the log

    // --- Data stream routing ---
    "data_stream": {
      "type": "logs",
      "dataset": "generic.otel",
      "namespace": "default"
    },

    // --- Resource (service identity, injected by OTel Collector) ---
    "resource": {
      "attributes": {
        "service.name": "python-favorite",
        "k8s.pod.name": "python-favorite-56c97dd849-wmn6d",
        "k8s.namespace.name": "elastiflix",
        "k8s.container.name": "python-favorite"
      }
    },

    // --- Instrumentation scope (empty for filelog receiver) ---
    "scope": {},

    // --- Log attributes (parsed from structured log line by the receiver) ---
    "attributes": {
      // Application-level fields from the structured JSON log
      "message": "Getting favorites for user None",
      "log.level": "info",                         // severity from the log line
      "@timestamp": "2026-07-01T09:07:00.577Z",   // timestamp embedded in the log payload

      // Trace correlation fields extracted from the log line
      "otelTraceID": "a6896e3d6536e9732dbbf605a018eb73",
      "otelSpanID": "bcffd49de462374f",
      "otelTraceSampled": true,
      "otelServiceName": "python-favorite",

      // Structured log metadata
      "log": {
        "logger": "app",
        "original": "Getting favorites for user None",
        "origin": {
          "file": { "name": "main.py", "line": 64.0 },
          "function": "get_favorite_movies"
        }
      },
      "process": {
        "name": "MainProcess",
        "pid": 1.0,
        "thread": { "name": "Thread-892", "id": 140736255215296.0 }
      },

      // ECS-style metadata embedded in the log record by the application
      "ecs": { "version": "1.6.0" },
      "event": { "dataset": "favorite.log" },

      // File path on the host (from filelog receiver)
      "log.file.path": "/var/log/pods/elastiflix_python-favorite-56c97dd849-wmn6d_.../0.log"
    },

    // --- Log body (the raw log record text) ---
    "body": {
      "text": "{\"@timestamp\":\"2026-07-01T09:07:00.577Z\",\"log.level\":\"info\",\"message\":\"Getting favorites for user None\",...}\n"
    }
  }
}
```

### Key fields — Logs

| Field | Type | Description | Example value |
|---|---|---|---|
| `@timestamp` | keyword (string) | Log event time in nanoseconds since epoch as string | `"1782896820578.246377"` |
| `observed_timestamp` | keyword (string) | Time the OTel Collector received the log record | `"1782896820724.742294"` |
| `body.text` | text | Raw log record (the log body in OTel terms) | `"{\"log.level\":\"info\",\"message\":\"Getting favorites...\"}"` |
| `attributes.message` | keyword | Parsed log message (human-readable text) | `Getting favorites for user None` |
| `attributes.log.level` | keyword | Log severity from the application | `info` |
| `attributes.otelTraceID` | keyword | Trace ID extracted from the structured log payload | `a6896e3d6536e9732dbbf605a018eb73` |
| `attributes.otelSpanID` | keyword | Span ID extracted from the structured log payload | `bcffd49de462374f` |
| `attributes.otelTraceSampled` | boolean | Whether the trace was sampled | `true` |
| `attributes.log.file.path` | keyword | Full log file path on the Kubernetes node | `/var/log/pods/elastiflix_python-...log` |
| `attributes.log.logger` | keyword | Logger name within the application | `app` |
| `attributes.log.origin.file.name` | keyword | Source file that emitted the log | `main.py` |
| `attributes.log.origin.function` | keyword | Function that emitted the log | `get_favorite_movies` |
| `attributes.process.pid` | double | Process ID | `1.0` |
| `attributes.process.thread.name` | keyword | Thread name | `Thread-892` |
| `resource.attributes.service.name` | keyword | Service name | `python-favorite` |
| `resource.attributes.k8s.pod.name` | keyword | Kubernetes pod name | `python-favorite-56c97dd849-wmn6d` |
| `resource.attributes.k8s.namespace.name` | keyword | Kubernetes namespace | `elastiflix` |
| `resource.attributes.k8s.container.name` | keyword | Kubernetes container name | `python-favorite` |
| `data_stream.dataset` | keyword | Always `generic.otel` | `generic.otel` |

### Notes

- **Log body** is in `body.text` (OTel log semconv), not `message` (ECS). The `attributes.message` field holds the parsed human-readable message extracted from the structured JSON body.
- **Severity** is in `attributes.log.level` (extracted from the application's structured log). There is no top-level `log.level` ECS field.
- **Trace correlation** is via `attributes.otelTraceID` and `attributes.otelSpanID` — OTel SDK custom fields embedded by the Python logger adapter. These are not the standard OTel `trace_id`/`span_id` fields used in the traces data stream.
- `@timestamp` is a nanosecond float string (same encoding as the traces data stream), not an ISO date.
- `resource.attributes` carries Kubernetes pod/namespace identity added by the OTel Collector's k8s attributes processor.
- The entire raw log line is also preserved in `body.text`, making it possible to re-parse or full-text search the original JSON payload.
