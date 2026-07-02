# Kibana Observability Field Guidelines

Pipeline: **OTel SDK → OTel Collector → Kafka → Logstash → Elasticsearch → Kibana**

These guidelines document the exact Elasticsearch fields required for each signal type so
that Kibana's Observability and APM UIs render correctly. Each field was validated against
Kibana 8.17 running against this local stack.

---

## Data Stream Naming

The Kibana-installed index templates create **conflicts** on certain name patterns. Use the
safe patterns below.

| Signal  | Safe name              | Avoid                     | Why                                                                 |
|---------|------------------------|---------------------------|---------------------------------------------------------------------|
| Traces  | `traces-generic.otel-default` | —                | Kibana APM index pattern `traces-*.otel-*` matches this correctly.  |
| Logs    | `logs-otel-default`    | `logs-*.otel-*`           | `logs-otel@template` maps `message` as a field alias — writes fail. |
| Metrics | `metrics-otel-default` | `metrics-*.otel-*`        | `metrics-otel@template` enables TSDB mode which requires OTel-native dimension fields. |

---

## Elasticsearch Index Template Requirements

All three signals need a custom index template with explicit field mappings. The most
important rules:

1. **String fields used for aggregation or filtering must be `keyword`, not `text`.**
   Kibana's APM queries run term aggregations directly on `service.name`, `transaction.type`,
   `event.outcome`, etc. A `text` mapping silently breaks those aggregations.
2. **Use a `dynamic_template` that maps all unrecognised strings as `keyword`** to avoid
   accidentally getting `text` from Elasticsearch's dynamic mapping.
3. **Boolean fields** (`transaction.sampled`) must be explicitly typed as `boolean` — the
   string `"true"` gets mapped as keyword otherwise and term filters fail.
4. **Numeric duration/timestamp fields** (`timestamp.us`, `span.duration.us`,
   `transaction.duration.us`) must be `long` — Kibana performs range and math operations on them.

Minimum dynamic template:

```json
"dynamic_templates": [
  {
    "strings_as_keyword": {
      "match_mapping_type": "string",
      "mapping": { "type": "keyword" }
    }
  }
]
```

And explicit overrides for `message` (text) and all numeric fields.

---

## Traces

Traces flow through `otel-traces` Kafka topic → Logstash → `traces-generic.otel-default`.

### Kibana APM features and what drives them

| Kibana feature               | Required field(s)                                                              |
|------------------------------|--------------------------------------------------------------------------------|
| Service Inventory            | `processor.event: "transaction"`, `service.name`, `transaction.type`           |
| Transaction list             | `transaction.name`, `transaction.type`, `transaction.duration.us`              |
| Error rate column            | `event.outcome: "failure"` on transaction docs                                 |
| Latency distribution         | `transaction.duration.us`                                                      |
| Trace samples panel          | `transaction.sampled: true`                                                    |
| Trace waterfall (root bar)   | `transaction.id`, `transaction.duration.us`, `timestamp.us`                   |
| Trace waterfall (child bars) | `span.id`, `span.duration.us`, `parent.id`, `transaction.id`, `timestamp.us`  |
| Agent badge                  | `agent.name`                                                                   |
| HTTP result badge            | `transaction.result` ("HTTP 2xx" / "HTTP 4xx" / "HTTP 5xx")                   |
| HTTP status code             | `http.response.status_code` (integer)                                          |

### Transaction document (root span, `processor.event: "transaction"`)

```json
{
  "@timestamp":               "2026-06-29T16:34:15.164229Z",
  "timestamp":                { "us": 1782748455164229 },
  "processor":                { "event": "transaction" },
  "trace":                    { "id": "a173fef1e9f96ca272e1da7c445456ac" },
  "transaction": {
    "id":                     "172bef6ef8c7a0e8",
    "name":                   "GET /crash",
    "type":                   "request",
    "duration":               { "us": 2993 },
    "result":                 "HTTP 5xx",
    "sampled":                true
  },
  "event":                    { "outcome": "failure" },
  "service":                  { "name": "demo-order-service", "version": "1.0.0" },
  "agent":                    { "name": "opentelemetry/python" },
  "http": {
    "response":               { "status_code": 500 }
  },
  "message":                  "GET /crash"
}
```

### Span document (child span, `processor.event: "span"`)

```json
{
  "@timestamp":               "2026-06-29T16:34:15.165059Z",
  "timestamp":                { "us": 1782748455165059 },
  "processor":                { "event": "span" },
  "trace":                    { "id": "a173fef1e9f96ca272e1da7c445456ac" },
  "transaction":              { "id": "172bef6ef8c7a0e8" },
  "parent":                   { "id": "172bef6ef8c7a0e8" },
  "span": {
    "id":                     "1aa230fe9614f71d",
    "name":                   "crash.simulate",
    "type":                   "custom",
    "duration":               { "us": 749 }
  },
  "event":                    { "outcome": "failure" },
  "service":                  { "name": "demo-order-service", "version": "1.0.0" },
  "agent":                    { "name": "opentelemetry/python" },
  "message":                  "crash.simulate"
}
```

### Field rules for traces

**`timestamp.us`** — microseconds since Unix epoch. Kibana's internal
`unflattenKnownApmEventFields` function throws `Missing required fields timestamp.us` without
it and the waterfall never renders, even if `@timestamp` is correct.

**`transaction.sampled: true`** — must be present on every transaction document. Kibana's
"Trace samples" panel queries `transaction.sampled: true`; without it the latency
distribution panel appears empty and no traces are selectable.

**`parent.id`** — present on all child spans; must equal the `transaction.id` (or `span.id`)
of the immediate parent. This is how Kibana constructs the waterfall hierarchy. Root
transactions have no `parent.id`.

**`span.id`** — the child span's own identifier. Kibana needs this separately from
`transaction.id` to distinguish span nodes in the tree.

**`span.duration.us`** — duration of the child span in microseconds. Without this Kibana
cannot draw the span bar in the waterfall (zero-width bars are invisible).

**`event.outcome`** — either `"success"` or `"failure"`. Kibana uses this for the error rate
column and failure highlighting on the transaction list. For HTTP server spans treat HTTP
status >= 400 as `"failure"` (not just >= 500), because 4xx responses represent server-side
failures from Kibana's APM perspective.

**`transaction.result`** — a bucketed string such as `"HTTP 2xx"`, `"HTTP 4xx"`,
`"HTTP 5xx"`, `"success"`, or `"failure"`. Shown in the transaction list and used for
grouping result distribution charts.

**`transaction.id` on span docs** — set to the immediate parent's ID. For shallow traces
(one level of children) this equals the root transaction's `transaction.id`. For deeper
hierarchies every span in the tree needs the root transaction's `transaction.id`; without
this Kibana cannot associate a span with its owning transaction when querying by `transactionId`.

### How OTel span status maps to APM fields

| OTel span status       | `event.outcome` | `transaction.result` |
|------------------------|-----------------|----------------------|
| ERROR (code=2)         | `"failure"`     | `"failure"` (or HTTP result) |
| HTTP status >= 400     | `"failure"`     | `"HTTP 4xx"` / `"HTTP 5xx"` |
| HTTP status < 400      | `"success"`     | `"HTTP 2xx"` / `"HTTP 3xx"` |
| OK / UNSET (no HTTP)   | `"success"`     | `"success"` |

---

## Logs

Logs flow through `otel-logs` Kafka topic → Logstash → `logs-otel-default`.

### Kibana features and what drives them

| Kibana feature         | Required field(s)                                      |
|------------------------|--------------------------------------------------------|
| Logs Explorer message  | `message` (text field, top-level)                      |
| Timestamp              | `@timestamp`                                           |
| Service filter         | `service.name`                                         |
| Severity filter        | `log.severity_text` or `log.level`                     |
| Trace correlation link | `trace.id`, `span.id` (optional but enables linking)   |

### Log document

```json
{
  "@timestamp":   "2026-06-29T16:34:15.164229Z",
  "message":      "Processing order",
  "service":      { "name": "demo-order-service", "version": "1.0.0" },
  "log": {
    "severity_text":   "INFO",
    "severity_number": 9,
    "trace_id":        "a173fef1e9f96ca272e1da7c445456ac",
    "span_id":         "172bef6ef8c7a0e8"
  },
  "trace":        { "id": "a173fef1e9f96ca272e1da7c445456ac" }
}
```

### Field rules for logs

**`message` must be a real `text` field**, not a `keyword` or a field alias. The Kibana
template `logs-otel@template` (which matches `logs-*.otel-*`) defines `message` as a field
alias pointing to `body.text`. Writing to `message` directly against that template fails
with `Cannot write to a field alias`. Use the `logs-otel-default` data stream name (no dot
before `otel`) to avoid the template.

**`@timestamp`** — must be parsed from the OTel `timeUnixNano` or `observedTimeUnixNano`
field (nanosecond integer → ISO 8601). Do not rely on Logstash's default ingestion time.

**`log.severity_text`** — the human-readable level string (`DEBUG`, `INFO`, `WARN`,
`ERROR`). Kibana Logs Explorer uses this for the colour-coded severity badge.

---

## Metrics

Metrics flow through `otel-metrics` Kafka topic → Logstash → `metrics-otel-default`.

### Kibana features and what drives them

| Kibana feature         | Required field(s)                              |
|------------------------|------------------------------------------------|
| Infrastructure metrics | `service.name`, `@timestamp`, metric value     |
| Custom dashboards      | Any keyword dimension + numeric value field    |

### Metric document

```json
{
  "@timestamp":  "2026-06-29T16:34:10.000000Z",
  "service":     { "name": "demo-order-service" },
  "metric": {
    "name":      "orders.processed",
    "unit":      "1",
    "type":      "sum",
    "value":     42
  },
  "otel_signal": "metrics"
}
```

### Field rules for metrics

**Avoid `metrics-*.otel-*` data streams.** Kibana installs a `metrics-otel@template` that
enables TSDB (Time Series Data Base) mode on all matching indices. TSDB requires every
document to carry specific OTel-native dimension routing fields; ordinary Logstash-written
documents fail with `Error extracting routing: source didn't contain any routing fields`.
Use `metrics-otel-default` (dataset = `otel`, no dot) to bypass this template.

**`@timestamp`** — must be parsed from the OTel data point `timeUnixNano`.

---

## Common Pitfalls

| Symptom                                    | Root cause                                              | Fix                                                         |
|--------------------------------------------|---------------------------------------------------------|-------------------------------------------------------------|
| Service Inventory empty                    | `service.name` mapped as `text`, aggregation fails      | Explicit `keyword` mapping in index template                |
| Trace samples panel empty                  | `transaction.sampled` missing or not `true`             | Set `transaction.sampled: true` on every root span          |
| Waterfall renders blank                    | `timestamp.us` missing                                  | Derive from `startTimeUnixNano / 1000`                      |
| Waterfall shows root only, no child spans  | `span.duration.us` missing on child spans               | Compute `(endTimeUnixNano - startTimeUnixNano) / 1000`      |
| Waterfall hierarchy broken / flat          | `parent.id` missing or not matching `transaction.id`    | Set `parent.id = parentSpanId` on every non-root span       |
| Error rate always 0%                       | `event.outcome` field deleted by mutate filter          | Don't `remove_field => ["event"]`; use `[event][original]`  |
| 4xx requests not counted as failures       | `event.outcome` only checked OTel ERROR status          | Treat HTTP status >= 400 as `"failure"` for server spans    |
| Logs `message` write fails                 | Data stream matches `logs-otel@template` alias          | Route logs to `logs-otel-default`, not `logs-*.otel-*`      |
| Metrics routing error                      | Data stream matches `metrics-otel@template` TSDB        | Route metrics to `metrics-otel-default`                     |

---

## Logstash Derivation Reference

Key derivations performed in the Logstash ruby filter for traces:

```ruby
start_ns = span["startTimeUnixNano"]
end_ns   = span["endTimeUnixNano"]

# @timestamp — ISO from nanosecond epoch
ts = Time.at(start_ns.to_i / 1_000_000_000, (start_ns.to_i % 1_000_000_000) / 1000.0).utc.iso8601(6)
event.set("@timestamp", LogStash::Timestamp.new(Time.parse(ts)))

# timestamp.us — microseconds since epoch (required by Kibana APM)
event.set("[timestamp][us]", start_ns.to_i / 1000)

# duration
dur_us = (end_ns.to_i - start_ns.to_i) / 1_000.0

# OTLP attribute array → flat hash
def otlp_attrs(arr)
  return {} unless arr.is_a?(Array)
  arr.each_with_object({}) { |kv, h| h[kv["key"]] = otlp_val(kv["value"]) }
end

# event.outcome — treat HTTP >= 400 as failure for server spans
http_status = attrs["http.status_code"]&.to_i
span_status  = span.dig("status", "code").to_i   # 0=UNSET, 1=OK, 2=ERROR
outcome = (span_status == 2 || (http_status && http_status >= 400)) ? "failure" : "success"

# Root vs child branching
is_root = span["parentSpanId"].nil? || span["parentSpanId"].to_s.empty?
```
