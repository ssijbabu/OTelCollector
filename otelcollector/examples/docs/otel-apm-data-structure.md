# OTel-APM Signal Data Structures

The OTel-APM pipeline is a superset of the OTel pipeline that adds APM-compatible rollup metrics computed by the `signaltometrics` connector **and** infrastructure metrics collected by OTel receivers (k8s cluster receiver, kubelet stats, host metrics). Trace and log documents are identical in schema to the OTel pipeline but land in the same data streams. Rollup metrics go to new OTel-native data streams (e.g., `metrics-service_transaction.1m.otel-default`) rather than the APM-namespaced streams used by the OTel pipeline.

---

## Traces

**Data stream:** `traces-generic.otel-default`  
**Backing index pattern:** `.ds-traces-generic.otel-default-<date>-<seq>`

The trace schema is identical to the OTel pipeline. All spans (root and child) land in the same data stream, distinguished by `attributes.processor.event` (`transaction` vs `span`) and the presence of `parent_span_id`.

### Server span (root transaction)

```jsonc
{
  "_index": ".ds-traces-generic.otel-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "1782830605846.128",            // nanoseconds since epoch, string-encoded

    "trace_id": "deb9be1451783898cd8a5a84da4a728d",
    "span_id": "1724debfc38653c8",
    // parent_span_id absent — root span
    "name": "POST /api/favorites",
    "kind": "Server",
    "duration": 15403008,                         // nanoseconds

    "data_stream": {
      "type": "traces",
      "dataset": "generic.otel",
      "namespace": "default"
    },

    "attributes": {
      // HTTP semconv (OTel v1)
      "http.method": "POST",
      "http.scheme": "http",
      "http.target": "/api/favorites",
      "http.url": "http://node-server.elastiflix.svc.cluster.local:3001/api/favorites",
      "http.host": "node-server.elastiflix.svc.cluster.local:3001",
      "http.flavor": "1.1",
      "http.route": "/api/favorites",
      "http.status_code": 200,
      "http.status_text": "OK",
      "http.user_agent": "curl/8.21.0",
      "net.host.name": "node-server.elastiflix.svc.cluster.local",
      "net.host.ip": "::ffff:10.1.1.175",
      "net.host.port": 3001,
      "net.peer.ip": "::ffff:10.1.1.160",
      "net.peer.port": 48378,
      "net.transport": "ip_tcp",

      // APM compatibility attributes (injected by collector)
      "timestamp.us": 1782830605846000,
      "processor.event": "transaction",
      "transaction.sampled": true,
      "transaction.id": "1724debfc38653c8",
      "transaction.root": true,
      "transaction.name": "POST /api/favorites",
      "transaction.type": "request",
      "transaction.result": "HTTP 2xx",
      "transaction.representative_count": 1.0,
      "transaction.duration.us": 15403,
      "event.outcome": "success",
      "event.success_count": 1
    },

    "links": [],
    "status": { "code": "Unset" },

    "resource": {
      "attributes": {
        "service.name": "node-server",
        "service.version": "1.0",
        "deployment.environment": "production",
        "telemetry.sdk.name": "opentelemetry",
        "telemetry.sdk.language": "nodejs",
        "telemetry.sdk.version": "1.15.2",
        "agent.name": "opentelemetry/nodejs",
        "agent.version": "1.15.2"
      }
    },

    "scope": {
      "name": "@opentelemetry/instrumentation-http",
      "version": "0.41.2",
      "attributes": {
        "service.framework.name": "@opentelemetry/instrumentation-http",
        "service.framework.version": "0.41.2"
      }
    }
  }
}
```

### Client span (child span)

```jsonc
{
  "_index": ".ds-traces-generic.otel-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "1782830606944.999936",

    "trace_id": "cfecace37de52cea374e430a0930f687",
    "span_id": "76bd4402c0f64b52",
    "parent_span_id": "79760c8cc7344430",        // links this to its parent span
    "name": "GET",
    "kind": "Client",                             // outbound HTTP call
    "duration": 6144000,                          // nanoseconds

    "data_stream": {
      "type": "traces",
      "dataset": "generic.otel",
      "namespace": "default"
    },

    "attributes": {
      "http.url": "http://dotnet-login.elastiflix.svc.cluster.local/login",
      "http.method": "GET",
      "http.target": "/login",
      "http.host": "dotnet-login.elastiflix.svc.cluster.local:80",
      "http.status_code": 200,
      "http.flavor": "1.1",
      "net.peer.name": "dotnet-login.elastiflix.svc.cluster.local",
      "net.peer.ip": "10.110.34.65",
      "net.peer.port": 80,
      "net.transport": "ip_tcp",

      // APM compatibility attributes
      "timestamp.us": 1782830606944999,
      "processor.event": "span",                 // child span
      "span.name": "GET",
      "span.type": "external",
      "span.subtype": "http",
      "span.representative_count": 1.0,
      "span.duration.us": 6144,
      "event.outcome": "success",
      "event.success_count": 1,

      // Dependency identification for service map
      "service.target.type": "http",
      "service.target.name": "dotnet-login.elastiflix.svc.cluster.local",
      "span.destination.service.resource": "dotnet-login.elastiflix.svc.cluster.local"
    },

    "links": [],
    "status": { "code": "Unset" },

    "resource": {
      "attributes": {
        "service.name": "node-server",
        "service.version": "1.0",
        "deployment.environment": "production",
        "telemetry.sdk.name": "opentelemetry",
        "telemetry.sdk.language": "nodejs",
        "telemetry.sdk.version": "1.15.2",
        "agent.name": "opentelemetry/nodejs",
        "agent.version": "1.15.2"
      }
    },

    "scope": {
      "name": "@opentelemetry/instrumentation-http",
      "version": "0.41.2"
    }
  }
}
```

### Key fields — Traces

| Field | Type | Description | Example value |
|---|---|---|---|
| `@timestamp` | keyword (string) | Span start time in nanoseconds since epoch | `"1782830605846.128"` |
| `trace_id` | keyword | 128-bit trace ID, lowercase hex | `deb9be1451783898cd8a5a84da4a728d` |
| `span_id` | keyword | 64-bit span ID, lowercase hex | `1724debfc38653c8` |
| `parent_span_id` | keyword | Parent span ID; absent on root spans | `79760c8cc7344430` |
| `name` | keyword | Span name | `POST /api/favorites` |
| `kind` | keyword | OTel SpanKind | `Server`, `Client`, `Internal` |
| `duration` | long | Span duration in **nanoseconds** | `15403008` |
| `status.code` | keyword | OTel status code | `Unset`, `Ok`, `Error` |
| `attributes.processor.event` | keyword | APM compat: `transaction` or `span` | `transaction` |
| `attributes.transaction.id` | keyword | APM compat: ID of the root transaction | `1724debfc38653c8` |
| `attributes.transaction.root` | boolean | APM compat: true on root spans | `true` |
| `attributes.transaction.name` | keyword | APM compat: transaction name | `POST /api/favorites` |
| `attributes.transaction.type` | keyword | APM compat: transaction category | `request` |
| `attributes.transaction.result` | keyword | APM compat: result bucket | `HTTP 2xx` |
| `attributes.transaction.duration.us` | long | APM compat: duration in microseconds | `15403` |
| `attributes.span.type` | keyword | APM compat: span type | `external` |
| `attributes.span.subtype` | keyword | APM compat: span sub-type | `http` |
| `attributes.span.duration.us` | long | APM compat: child span duration in microseconds | `6144` |
| `attributes.timestamp.us` | long | APM compat: start time in microseconds | `1782830605846000` |
| `attributes.event.outcome` | keyword | `success`, `failure`, or `unknown` | `success` |
| `attributes.service.target.type` | keyword | Downstream dependency protocol | `http` |
| `attributes.service.target.name` | keyword | Downstream dependency hostname | `dotnet-login.elastiflix.svc.cluster.local` |
| `attributes.span.destination.service.resource` | keyword | Dependency resource identifier | `dotnet-login.elastiflix.svc.cluster.local` |
| `resource.attributes.service.name` | keyword | Service name | `node-server` |
| `resource.attributes.deployment.environment` | keyword | Deployment environment | `production` |
| `resource.attributes.telemetry.sdk.language` | keyword | SDK language | `nodejs` |
| `resource.attributes.agent.name` | keyword | APM compat: agent name | `opentelemetry/nodejs` |
| `scope.name` | keyword | Instrumentation library | `@opentelemetry/instrumentation-http` |

### Notes

- Trace documents are schema-identical to the OTel pipeline. Both pipelines write to the same `traces-generic.otel-default` data stream — the only difference is how rollup metrics are produced.
- APM compatibility attributes (`transaction.*`, `span.*`, `service.target.*`, etc.) are injected by the OTel Collector, not the APM Server.
- `@timestamp` is a nanosecond float stored as a string — not an ISO-8601 date.
- No `observer.*` fields — there is no APM Server in this pipeline.

---

## Metrics — SDK

**Data stream:** `metrics-generic.otel-default`  
**Backing index:** `.ds-metrics-generic.otel-default-<date>-<seq>`

SDK metrics are raw OTel metrics emitted by the application runtime. Schema is identical to the OTel pipeline.

```jsonc
{
  "_index": ".ds-metrics-generic.otel-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "1782830553174.660400",        // nanoseconds since epoch, string-encoded
    "start_timestamp": "1782825016318.570300",   // cumulative window start

    "data_stream": {
      "type": "metrics",
      "dataset": "generic.otel",
      "namespace": "default"
    },

    "resource": {
      "attributes": {
        "service.name": "dotnet-login",
        "service.version": "1.0",
        "deployment.environment": "production",
        "telemetry.sdk.name": "opentelemetry",
        "telemetry.sdk.language": "dotnet",
        "telemetry.sdk.version": "1.4.0.802",
        "telemetry.auto.version": "0.7.0",
        "container.id": "8413a850a2354bfd779643b4c245668f729532d2476c3e4359e55aa53974bc51"
      }
    },

    "scope": {
      "name": "OpenTelemetry.Instrumentation.Runtime",
      "version": "1.1.0.2"
    },

    // Metric values nested under "metrics" object
    "metrics": {
      "process.runtime.dotnet.thread_pool.queue.length": 0
    }
  }
}
```

### Key fields — SDK Metrics

| Field | Type | Description | Example value |
|---|---|---|---|
| `@timestamp` | keyword (string) | Observation time in nanoseconds since epoch | `"1782830553174.660400"` |
| `start_timestamp` | keyword (string) | Cumulative metric window start in nanoseconds | `"1782825016318.570300"` |
| `metrics.<name>` | long/double/object | Metric value; key is the OTel metric name | `metrics.process.runtime.dotnet.thread_pool.queue.length: 0` |
| `resource.attributes.service.name` | keyword | Service that emitted the metric | `dotnet-login` |
| `resource.attributes.container.id` | keyword | Container runtime ID | `8413a850a235...` |
| `resource.attributes.telemetry.sdk.language` | keyword | SDK language | `dotnet` |
| `scope.name` | keyword | Instrumentation library | `OpenTelemetry.Instrumentation.Runtime` |
| `data_stream.dataset` | keyword | Always `generic.otel` | `generic.otel` |

### Notes

- SDK metrics here are schema-identical to the OTel pipeline. All metric values live under `metrics.*`, not as top-level dotted fields (unlike the APM pipeline).
- `@timestamp` and `start_timestamp` are both nanosecond strings in this pipeline (vs millisecond integers in the OTel pipeline for some documents).

---

## Metrics — APM Rollups

**Data streams:** `metrics-service_transaction.1m.otel-default`, `metrics-service_destination.1m.otel-default`, `metrics-service_summary.1m.otel-default`, `metrics-transaction.1m.otel-default`

These rollup data streams use an OTel-native naming convention (e.g., `service_transaction.1m.otel`) rather than the `apm.*` prefix used by the OTel pipeline's rollups. The `signaltometrics` connector produces them; document structure keeps metric values inside a `metrics` object and uses OTel `resource`/`attributes`/`scope` envelopes throughout.

### service_transaction

```jsonc
{
  "_index": ".ds-metrics-service_transaction.1m.otel-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "1782830522286.234087",        // nanoseconds since epoch, string-encoded
    "unit": "us",                                // unit for the histogram values
    "_doc_count": 44,

    "data_stream": {
      "type": "metrics",
      "dataset": "service_transaction.1m.otel",  // OTel-native dataset name
      "namespace": "default"
    },

    // Span/transaction attributes used as rollup dimensions
    "attributes": {
      "transaction.root": false,
      "transaction.type": "request",
      "metricset.name": "service_transaction",
      "processor.event": "metric"
    },

    // Service identity
    "resource": {
      "attributes": {
        "service.name": "python-favorite",
        "deployment.environment": "production",
        "telemetry.sdk.language": "python",
        "agent.name": "opentelemetry/python",
        // Connector metadata
        "signaltometrics.service.instance.id": "f0ea6abd-9bdd-4379-b40d-8ad1718d3ef5",
        "signaltometrics.service.name": "elastic-agent",
        "metricset.interval": "1m"              // interval in resource attributes (not metricset object)
      }
    },

    "scope": {
      "name": "github.com/elastic/opentelemetry-collector-components/connector/signaltometricsconnector"
    },

    // Metric values
    "metrics": {
      "transaction.duration.histogram": {
        "counts": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 1, 1, 2, 1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1],
        "values": [2502.38, 2641.62, 2728.86, 2758.58, 2788.62, 2880.71, 3107.60, 3141.44, 3175.64, 3316.24, 3352.35, 3463.06, 3538.90, 3577.43, 3655.77, 3695.58, 3776.50, 3817.62, 3901.22, 4030.05, 4163.15, 4254.31, 4347.46, 4442.66, 4639.36, 4740.94, 4897.51, 5226.33, 5283.25, 5824.16, 5887.58, 6215.19, 6351.29, 6632.48, 6704.71, 7154.86, 7311.54, 7887.40, 7973.29, 8236.60, 10340.07]
      },
      "transaction.duration.summary": { "sum": 206740.0, "value_count": 44 },
      "event.success_count": { "sum": 44.0, "value_count": 44 }
    }
  }
}
```

### service_destination

```jsonc
{
  "_index": ".ds-metrics-service_destination.1m.otel-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "1782830462014.445545",

    "data_stream": {
      "type": "metrics",
      "dataset": "service_destination.1m.otel",
      "namespace": "default"
    },

    "attributes": {
      "span.name": "GET",
      "event.outcome": "success",
      "service.target.type": "http",
      "service.target.name": "python-favorite.elastiflix.svc.cluster.local:5000",
      "span.destination.service.resource": "python-favorite.elastiflix.svc.cluster.local:5000",
      "metricset.name": "service_destination",
      "processor.event": "metric"
    },

    "resource": {
      "attributes": {
        "service.name": "node-server",
        "deployment.environment": "production",
        "telemetry.sdk.language": "nodejs",
        "agent.name": "opentelemetry/nodejs",
        "signaltometrics.service.instance.id": "f0ea6abd-9bdd-4379-b40d-8ad1718d3ef5",
        "signaltometrics.service.name": "elastic-agent",
        "metricset.interval": "1m"
      }
    },

    "scope": {
      "name": "github.com/elastic/opentelemetry-collector-components/connector/signaltometricsconnector"
    },

    "metrics": {
      "span.destination.service.response_time.count": 24
      // Note: only the count metric here; sum is tracked separately in other documents
    }
  }
}
```

### service_summary

```jsonc
{
  "_index": ".ds-metrics-service_summary.1m.otel-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "1782830525774.656047",

    "data_stream": {
      "type": "metrics",
      "dataset": "service_summary.1m.otel",
      "namespace": "default"
    },

    "attributes": {
      "metricset.name": "service_summary",
      "processor.event": "metric"
    },

    "resource": {
      "attributes": {
        "service.name": "dotnet-login",
        "agent.name": "unknown",               // agent unknown when not injected by collector
        "signaltometrics.service.instance.id": "fac42570-8702-4e0d-afe7-45efb793bd85",
        "signaltometrics.service.name": "elastic-agent",
        "metricset.interval": "1m"
      }
    },

    "scope": {
      "name": "github.com/elastic/opentelemetry-collector-components/connector/signaltometricsconnector"
    },

    "metrics": {
      "service_summary": 304                   // count of spans/transactions for this service
    }
  }
}
```

### transaction

```jsonc
{
  "_index": ".ds-metrics-transaction.1m.otel-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "1782830522286.234087",
    "unit": "us",
    "_doc_count": 44,

    "data_stream": {
      "type": "metrics",
      "dataset": "transaction.1m.otel",
      "namespace": "default"
    },

    "attributes": {
      "transaction.root": false,
      "transaction.name": "/favorites",          // present here; absent from service_transaction
      "transaction.type": "request",
      "transaction.result": "HTTP 2xx",           // present here; absent from service_transaction
      "event.outcome": "success",
      "metricset.name": "transaction",
      "processor.event": "metric"
    },

    "resource": {
      "attributes": {
        "service.name": "python-favorite",
        "service.version": "1.0",
        "deployment.environment": "production",
        "telemetry.sdk.language": "python",
        "telemetry.sdk.version": "1.19.0",
        "agent.name": "opentelemetry/python",
        "signaltometrics.service.instance.id": "f0ea6abd-9bdd-4379-b40d-8ad1718d3ef5",
        "signaltometrics.service.name": "elastic-agent",
        "metricset.interval": "1m"
      }
    },

    "scope": {
      "name": "github.com/elastic/opentelemetry-collector-components/connector/signaltometricsconnector"
    },

    "metrics": {
      "transaction.duration.histogram": {
        "counts": [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 1, 1, 2, 1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1],
        "values": [2502.38, 2641.62, 2728.86, 2758.58, 2788.62, 2880.71, 3107.60, 3141.44, 3175.64, 3316.24, 3352.35, 3463.06, 3538.90, 3577.43, 3655.77, 3695.58, 3776.50, 3817.62, 3901.22, 4030.05, 4163.15, 4254.31, 4347.46, 4442.66, 4639.36, 4740.94, 4897.51, 5226.33, 5283.25, 5824.16, 5887.58, 6215.19, 6351.29, 6632.48, 6704.71, 7154.86, 7311.54, 7887.40, 7973.29, 8236.60, 10340.07]
      },
      "transaction.duration.summary": { "sum": 206740.0, "value_count": 44 }
    }
  }
}
```

### Key fields — APM Rollups

| Field | Type | Description | Example value |
|---|---|---|---|
| `@timestamp` | keyword (string) | Bucket timestamp in nanoseconds since epoch | `"1782830522286.234087"` |
| `_doc_count` | integer | Source span/transaction count in this bucket | `44` |
| `unit` | keyword | Unit for histogram values (`us` = microseconds) | `us` |
| `data_stream.dataset` | keyword | OTel-native rollup dataset name | `service_transaction.1m.otel` |
| `attributes.metricset.name` | keyword | Rollup type (inside attributes, not top-level) | `service_transaction` |
| `attributes.processor.event` | keyword | Always `metric` | `metric` |
| `attributes.transaction.type` | keyword | Transaction category dimension | `request` |
| `attributes.transaction.name` | keyword | Transaction name dimension (transaction rollup only) | `/favorites` |
| `attributes.transaction.result` | keyword | Outcome bucket (transaction rollup only) | `HTTP 2xx` |
| `attributes.transaction.root` | boolean | Whether this covers only root transactions | `false` |
| `attributes.event.outcome` | keyword | Outcome dimension (service_destination) | `success` |
| `attributes.service.target.type` | keyword | Downstream dependency protocol (service_destination) | `http` |
| `attributes.service.target.name` | keyword | Downstream dependency hostname (service_destination) | `python-favorite.elastiflix.svc.cluster.local:5000` |
| `attributes.span.destination.service.resource` | keyword | Dependency resource ID (service_destination) | `python-favorite.elastiflix.svc.cluster.local:5000` |
| `metrics.transaction.duration.histogram` | histogram | HDR histogram, values in microseconds | `{values:[2502,...], counts:[1,...]}` |
| `metrics.transaction.duration.summary.sum` | double | Total duration in microseconds | `206740.0` |
| `metrics.transaction.duration.summary.value_count` | long | Transaction count | `44` |
| `metrics.event.success_count.sum` | double | Successful transaction count | `44.0` |
| `metrics.span.destination.service.response_time.count` | long | Call count to dependency | `24` |
| `metrics.service_summary` | integer | Span/transaction count (service_summary only) | `304` |
| `resource.attributes.service.name` | keyword | Service name | `python-favorite` |
| `resource.attributes.signaltometrics.service.instance.id` | keyword | Service instance UUID from connector | `f0ea6abd-9bdd-4379-b40d-8ad1718d3ef5` |
| `resource.attributes.metricset.interval` | keyword | Aggregation window (in resource, not top-level) | `1m` |
| `scope.name` | keyword | Always the signaltometrics connector path | `github.com/elastic/opentelemetry-collector-components/connector/signaltometricsconnector` |

### Notes

- Data stream dataset names use OTel-native naming (`service_transaction.1m.otel`) unlike the OTel pipeline which writes to `apm.service_transaction.1m`. This means OTel-APM rollups and OTel rollups land in **different** data streams.
- `metricset.name` and `metricset.interval` are in `attributes.*` and `resource.attributes.*` respectively, not top-level objects as in the APM pipeline.
- All metric values are nested under `metrics.*` (OTel convention), unlike APM pipeline rollups where `transaction.duration.summary` is a top-level field.
- `@timestamp` is a nanosecond float string, not an ISO date — same encoding as SDK metrics and traces in this pipeline.
- `transaction.root: false` can appear here (unlike OTel pipeline rollups where `transaction.root: true` is the norm), indicating this connector rollup may include non-root spans too.
- `scope.name` always identifies the `signaltometrics` connector, making it easy to filter connector-produced metrics from application-emitted metrics.

---

## Metrics — Infrastructure

**Data stream:** `metrics-k8sclusterreceiver.otel-default`  
**Backing index:** `.ds-metrics-k8sclusterreceiver.otel-default-<date>-<seq>`

Infrastructure metrics are collected by OTel receivers (k8s cluster receiver, kubelet stats, host metrics) and written directly to Elasticsearch. They use the same OTel-native layout as SDK metrics.

```jsonc
{
  "_index": ".ds-metrics-k8sclusterreceiver.otel-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "1782830602726.246597",          // nanoseconds since epoch, string
    "start_timestamp": "1782824091248.773423",

    "unit": "{node}",                              // OTel unit annotation

    "data_stream": {
      "type": "metrics",
      "dataset": "k8sclusterreceiver.otel",        // dataset encodes the receiver name
      "namespace": "default"
    },

    // Resource: Kubernetes object identity (no service.name — these are infra metrics)
    "resource": {
      "attributes": {
        "k8s.namespace.name": "opentelemetry-operator-system",
        "k8s.daemonset.name": "opentelemetry-kube-stack-daemon-collector",
        "k8s.daemonset.uid": "dadb0e17-cfd7-4597-b249-44f925679635"
      }
    },

    // Instrumentation scope: the receiver that collected the metric
    "scope": {
      "name": "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver",
      "version": "8.17.9"
    },

    // Metric values: multiple gauges per document when they share the same resource+timestamp
    "metrics": {
      "k8s.daemonset.current_scheduled_nodes": 1,
      "k8s.daemonset.desired_scheduled_nodes": 1,
      "k8s.daemonset.misscheduled_nodes": 0,
      "k8s.daemonset.ready_nodes": 1
    }
  }
}
```

### Key fields — Infrastructure Metrics

| Field | Type | Description | Example value |
|---|---|---|---|
| `@timestamp` | keyword (string) | Observation time in nanoseconds since epoch | `"1782830602726.246597"` |
| `start_timestamp` | keyword (string) | Metric collection start in nanoseconds | `"1782824091248.773423"` |
| `unit` | keyword | OTel unit annotation | `{node}`, `By`, `s` |
| `data_stream.dataset` | keyword | Encodes the receiver name | `k8sclusterreceiver.otel` |
| `metrics.<name>` | long/double | Metric value; key is the OTel metric name | `metrics.k8s.daemonset.ready_nodes: 1` |
| `resource.attributes.k8s.namespace.name` | keyword | Kubernetes namespace of the monitored resource | `opentelemetry-operator-system` |
| `resource.attributes.k8s.daemonset.name` | keyword | DaemonSet name | `opentelemetry-kube-stack-daemon-collector` |
| `resource.attributes.k8s.daemonset.uid` | keyword | DaemonSet UID | `dadb0e17-cfd7-4597-b249-44f925679635` |
| `scope.name` | keyword | OTel receiver that collected the metric | `github.com/open-telemetry/...receiver/k8sclusterreceiver` |
| `scope.version` | keyword | Collector version | `8.17.9` |

### Notes

- Infrastructure metrics have **no `service.name`** in `resource.attributes` — they carry Kubernetes object identifiers (namespace, pod, node, daemonset, etc.) instead. This distinguishes them from SDK metrics, which always have `resource.attributes.service.name`.
- Multiple gauge metrics sharing the same resource and timestamp are colocated in a single document under `metrics.*`, reducing document count.
- The `unit` field carries the OTel unit string in UCUM notation (e.g., `{node}` for node counts, `By` for bytes, `s` for seconds).
- `scope.name` encodes the full Go package path of the OTel receiver, making it easy to filter by collection source.
- Other infra receivers in this pipeline (`kubeletstatsreceiver`, `hostmetricsreceiver`) follow the same schema with different `data_stream.dataset` values and different Kubernetes object identifiers in `resource.attributes`.

---

## Logs

**Data stream:** `logs-generic.otel-default`  
**Backing index:** `.ds-logs-generic.otel-default-<date>-<seq>`

Logs follow the OTel log schema, identical to the OTel pipeline. The filelog receiver collects container logs from pods and the OTel Collector enriches them with Kubernetes metadata via the k8s attributes processor.

```jsonc
{
  "_index": ".ds-logs-generic.otel-default-2026.06.30-000001",
  "_source": {
    "@timestamp": "1782830585891.140797",          // nanoseconds since epoch, string
    "observed_timestamp": "1782830585966.451922",  // when collector received the record

    "data_stream": {
      "type": "logs",
      "dataset": "generic.otel",
      "namespace": "default"
    },

    // Resource: rich Kubernetes + host attributes from k8s attributes processor
    "resource": {
      "schema_url": "https://opentelemetry.io/schemas/1.6.1",
      "attributes": {
        // Application identity
        "service.name": "javascript-client",
        // Kubernetes metadata
        "k8s.pod.name": "javascript-client-5f8cc4679c-5vckh",
        "k8s.pod.uid": "e185babe-d3d2-4bb4-81e7-0056cb34c16d",
        "k8s.pod.ip": "10.1.1.158",
        "k8s.namespace.name": "elastiflix",
        "k8s.container.name": "javascript-client",
        "k8s.container.restart_count": "0",
        "k8s.node.name": "docker-desktop",
        "k8s.deployment.name": "javascript-client",
        "k8s.replicaset.name": "javascript-client-5f8cc4679c",
        "k8s.pod.start_time": "2026-06-30T12:02:06Z",
        // Host metadata
        "host.name": "docker-desktop",
        "host.arch": "arm64",
        "os.type": "linux",
        "os.description": "Ubuntu 20.04.6 LTS (Focal Fossa) (Linux docker-desktop 6.12.54-linuxkit ...)"
      }
    },

    // Scope is empty for filelog receiver
    "scope": {},

    // Log-specific attributes (file path, stream)
    "attributes": {
      "log.file.path": "/var/log/pods/elastiflix_javascript-client-5f8cc4679c-5vckh_.../0.log",
      "log.iostream": "stdout"
    },

    // Log body: the raw log line from the container
    "body": {
      "text": "10.1.0.1 - - [30/Jun/2026:14:43:05 +0000] \"GET / HTTP/1.1\" 200 20821 \"-\" \"kube-probe/1.34\" \"-\"\n"
    }
  }
}
```

### Key fields — Logs

| Field | Type | Description | Example value |
|---|---|---|---|
| `@timestamp` | keyword (string) | Log event time in nanoseconds since epoch | `"1782830585891.140797"` |
| `observed_timestamp` | keyword (string) | Time the OTel Collector received the record | `"1782830585966.451922"` |
| `body.text` | text | Raw log line — the log body in OTel terms | `"10.1.0.1 - - [30/Jun/2026:14:43:05 +0000] \"GET / HTTP/1.1\" 200 20821..."` |
| `attributes.log.file.path` | keyword | Full path to the log file on the node | `/var/log/pods/elastiflix_javascript-client-...log` |
| `attributes.log.iostream` | keyword | `stdout` or `stderr` | `stdout` |
| `resource.attributes.service.name` | keyword | Service name (from k8s pod labels or annotations) | `javascript-client` |
| `resource.attributes.k8s.pod.name` | keyword | Kubernetes pod name | `javascript-client-5f8cc4679c-5vckh` |
| `resource.attributes.k8s.namespace.name` | keyword | Kubernetes namespace | `elastiflix` |
| `resource.attributes.k8s.container.name` | keyword | Container name within the pod | `javascript-client` |
| `resource.attributes.k8s.node.name` | keyword | Node the pod runs on | `docker-desktop` |
| `resource.attributes.k8s.deployment.name` | keyword | Owning Deployment | `javascript-client` |
| `resource.attributes.host.name` | keyword | Node hostname | `docker-desktop` |
| `resource.attributes.host.arch` | keyword | Node CPU architecture | `arm64` |
| `resource.attributes.os.type` | keyword | OS type | `linux` |
| `resource.schema_url` | keyword | OTel semantic convention schema version | `https://opentelemetry.io/schemas/1.6.1` |
| `data_stream.dataset` | keyword | Always `generic.otel` | `generic.otel` |

### Notes

- **Log body** is in `body.text` (OTel semconv), not `message`. For plain container logs (access logs, unstructured text), `body.text` is the full raw line.
- **Severity** — for unstructured logs (like the nginx access log example above), there is no `attributes.log.level` field. For structured JSON logs (e.g., Python application logs with a `log.level` key), the filelog receiver extracts it into `attributes.log.level`.
- **Trace correlation** — not present for plain container logs. Applications that write structured JSON logs including OTel trace context fields will have them embedded in `body.text` but they are only promoted to `attributes.*` if a JSON parsing operator is configured on the receiver.
- The `resource` object in OTel-APM logs is significantly richer than in the OTel pipeline — it includes `host.*`, `os.*`, and full k8s metadata including pod IPs and restart counts, because the k8s attributes processor is configured more broadly in this pipeline.
- `resource.schema_url` is present in this pipeline's logs, indicating the OTel Collector was configured to attach the semantic convention schema version to resource attributes.
