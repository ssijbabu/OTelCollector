# Azure Event Hub Receiver

Pulls telemetry from an Azure Event Hub and pushes it through the collector pipeline. Supports the native AMQP protocol and the Kafka-compatible endpoint, with SAS key or Azure AD (auth extension) authentication.

## Configuration reference

### Core settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `protocol` | string | `amqp` | Wire protocol: `amqp` or `kafka`. |
| `event_hub.namespace` | string | — | Fully qualified namespace, e.g. `mynamespace.servicebus.windows.net`. |
| `event_hub.name` | string | — | Event Hub name. |
| `event_hub.shared_access_key_name` | string | — | SAS policy name. Required when `auth` is not set. |
| `event_hub.shared_access_key` | string | — | SAS key value. Required when `auth` is not set. |
| `auth` | component ID | — | Auth extension (e.g. `azureauthextension`). When set, SAS key fields are ignored. |
| `group` | string | `$Default` | Consumer group name. Applies to both AMQP and Kafka. |

### AMQP-only settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `partition` | string | `""` | Listen to a single partition. Empty means all partitions. |
| `offset` | string | `""` | Starting offset within `partition`. Only valid when `partition` is set. |
| `storage` | component ID | — | Storage extension for per-partition checkpoint persistence. Mutually exclusive with `blob_checkpoint_store`. |
| `blob_checkpoint_store.connection` | string | — | Azure Blob Storage connection string. Required when not using `auth`. |
| `blob_checkpoint_store.storage_account_url` | string | — | Blob service URL, e.g. `https://myaccount.blob.core.windows.net`. Required when using `auth`. |
| `blob_checkpoint_store.container_name` | string | — | Blob container for checkpoint data. Must exist before the collector starts. |
| `max_poll_events` | int | `100` | Maximum events per poll. |
| `poll_rate` | int | `5` | Maximum seconds to wait before returning fewer than `max_poll_events`. |
| `prefetch_count` | int32 | `0` | SDK prefetch buffer size per partition. `0` uses the SDK default (300); negative disables prefetch. |

`blob_checkpoint_store` is mutually exclusive with `storage`, `partition`, and `offset`.

### Data transformation settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `format` | string | `azure` | Message format: `azure`, `raw`, or `""`. Mutually exclusive with `encoding`. |
| `encoding` | component ID | — | Encoding extension to unmarshal the message body. Mutually exclusive with `format`. |
| `apply_semantic_conventions` | bool | `false` | Translate Azure Resource Logs using OTel semantic convention attribute names. |
| `time_formats.logs` | []string | — | Custom time formats for logs ([Go time layout](https://pkg.go.dev/time#Layout)). Falls back to ISO8601. |
| `time_formats.metrics` | []string | — | Custom time formats for metrics. |
| `time_formats.traces` | []string | — | Custom time formats for traces. |
| `metric_aggregation` | string | — | Set to `average` to aggregate datapoints as `sum/count`. Default creates separate `_TOTAL`, `_MIN`, `_MAX`, `_AVG`, `_COUNT` metrics. |

---

## Examples

### AMQP — SAS key authentication

```yaml
receivers:
  azure_event_hub:
    protocol: amqp
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry
      shared_access_key_name: RootManageSharedAccessKey
      shared_access_key: ${env:EVENTHUB_SAS_KEY}
```

### Kafka — SAS key authentication

The Kafka endpoint is derived automatically from `event_hub.namespace` as `<namespace>:9093` with TLS and SASL/PLAIN enabled. Consumer group offsets are committed to Event Hub, so the receiver resumes from where it left off after a restart.

```yaml
receivers:
  azure_event_hub:
    protocol: kafka
    group: my-consumer-group   # optional, defaults to $Default
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry
      shared_access_key_name: RootManageSharedAccessKey
      shared_access_key: ${env:EVENTHUB_SAS_KEY}
```

### AMQP — Azure Workload Identity (AKS)

```yaml
extensions:
  azureauth:
    scopes:
      - https://eventhubs.azure.net/.default

receivers:
  azure_event_hub:
    protocol: amqp
    auth: azureauth
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry

service:
  extensions: [azureauth]
```

The pod must carry the label `azure.workload.identity/use: "true"` and the ServiceAccount must be annotated with `azure.workload.identity/client-id: <client-id>`.

### Kafka — Azure Workload Identity (AKS)

```yaml
extensions:
  azureauth:
    scopes:
      - https://eventhubs.azure.net/.default

receivers:
  azure_event_hub:
    protocol: kafka
    group: otel-gateway
    auth: azureauth
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry

service:
  extensions: [azureauth]
```

### AMQP — auth extension with service principal

```yaml
extensions:
  azureauth:
    service_principal:
      tenant_id: ${env:AZURE_TENANT_ID}
      client_id: ${env:AZURE_CLIENT_ID}
      client_secret: ${env:AZURE_CLIENT_SECRET}
    scopes:
      - https://eventhubs.azure.net/.default

receivers:
  azure_event_hub:
    protocol: amqp
    auth: azureauth
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry

service:
  extensions: [azureauth]
```

### AMQP — checkpoint persistence with storage extension

Prevents message reprocessing after a collector restart. Without this, the receiver starts from the latest offset on each restart.

```yaml
extensions:
  file_storage:
    directory: /var/lib/otelcol/eventhub

receivers:
  azure_event_hub:
    protocol: amqp
    storage: file_storage
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry
      shared_access_key_name: RootManageSharedAccessKey
      shared_access_key: ${env:EVENTHUB_SAS_KEY}

service:
  extensions: [file_storage]
```

### AMQP — distributed consumption with blob checkpoint store

Coordinates partition ownership across multiple collector instances via Azure Blob leases. Rebalances automatically when instances are added or removed.

The blob container must exist before the collector starts. All instances must use the same consumer group and container name.

```yaml
# With SAS key
receivers:
  azure_event_hub:
    protocol: amqp
    group: my-consumer-group
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry
      shared_access_key_name: RootManageSharedAccessKey
      shared_access_key: ${env:EVENTHUB_SAS_KEY}
    blob_checkpoint_store:
      connection: ${env:BLOB_CONNECTION_STRING}
      container_name: eventhub-checkpoints
```

```yaml
# With auth extension
extensions:
  azureauth:
    scopes:
      - https://eventhubs.azure.net/.default

receivers:
  azure_event_hub:
    protocol: amqp
    group: my-consumer-group
    auth: azureauth
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry
    blob_checkpoint_store:
      storage_account_url: https://myaccount.blob.core.windows.net
      container_name: eventhub-checkpoints

service:
  extensions: [azureauth]
```

### AMQP — single partition with custom offset

```yaml
receivers:
  azure_event_hub:
    protocol: amqp
    partition: "0"
    offset: "1234-5566"
    group: my-consumer-group
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry
      shared_access_key_name: RootManageSharedAccessKey
      shared_access_key: ${env:EVENTHUB_SAS_KEY}
```

---

## Protocol comparison

| Feature | AMQP | Kafka |
|---------|------|-------|
| Default protocol | Yes | No |
| Consumer group offset tracking | Via storage / blob extension | Built-in (committed to Event Hub) |
| Per-partition targeting | Yes (`partition` field) | No |
| Distributed coordination | Via `blob_checkpoint_store` | Native (Kafka consumer group protocol) |
| Restart behaviour without persistence | Starts from latest | Resumes from last committed offset |

---

## Format

### `raw`

Maps AMQP properties and data into the attributes and body of an OTel `LogRecord`. The body is a raw byte array.

> Only supported for Logs pipelines.

### `azure`

#### Logs

Extracts Azure log records from the AMQP message, parses them, and maps fields to OTel attributes.

| Azure field | OTel |
|---|---|
| `callerIpAddress` | `network.peer.address` (attribute) |
| `correlationId` | `azure.correlation.id` (attribute) |
| `category` | `azure.category` (attribute) |
| `durationMs` | `azure.duration` (attribute) |
| `Level` | `severity_number`, `severity_text` |
| `location` | `cloud.region` (attribute) |
| `operationName` | `azure.operation.name` (attribute) |
| `operationVersion` | `azure.operation.version` (attribute) |
| `properties` | `azure.properties` (attribute, nested) |
| `resourceId` | `azure.resource.id` (resource attribute) |
| `resultDescription` | `azure.result.description` (attribute) |
| `resultSignature` | `azure.result.signature` (attribute) |
| `resultType` | `azure.result.type` (attribute) |
| `tenantId` | `azure.tenant.id` (attribute) |
| `time` / `timeStamp` | `time_unix_nano` |
| `identity` | `azure.identity` (attribute, nested) |

#### Metrics — Platform metrics

| Azure field | OTel |
|---|---|
| `time` | `time_unix_nano` |
| `resourceId` | `azure.resource.id` (resource attribute) |
| `timeGrain` | `start_time_unix_nano` |
| `total` | `<metricName>_TOTAL` |
| `count` | `<metricName>_COUNT` |
| `minimum` | `<metricName>_MINIMUM` |
| `maximum` | `<metricName>_MAXIMUM` |
| `average` | `<metricName>_AVERAGE` |

#### Metrics — Application Insights

| Azure field | OTel |
|---|---|
| `AppRoleInstance` | `service.instance.id` (resource attribute) |
| `AppRoleName` | `service.name` (resource attribute) |
| `AppVersion` | `service.version` (resource attribute) |
| `ClientCountryOrRegion` | `cloud.region` (resource attribute) |
| `ClientOS` | `os.name` (resource attribute) |
| `Sum` | `<metricName>_TOTAL` |
| `ItemCount` | `<metricName>_COUNT` |

#### Traces (Application Insights)

| Azure field | OTel |
|---|---|
| `Time` | `start_time_unix_nano` |
| `Time + durationMs` | `end_time_unix_nano` |
| `Name` | span name |
| `OperationId` | trace ID |
| `ParentId` | parent span ID |
| `Id` | span ID |
| `AppRoleName` | `service.name` |

---

## Encoding extension

As an alternative to `format`, delegate unmarshaling to an [encoding extension]. Mutually exclusive with `format`.

```yaml
extensions:
  azure_encoding:

receivers:
  azure_event_hub:
    encoding: azure_encoding
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry
      shared_access_key_name: RootManageSharedAccessKey
      shared_access_key: ${env:EVENTHUB_SAS_KEY}

service:
  extensions: [azure_encoding]
```

> The encoding extension receives the message body only. AMQP properties and enqueued time (available via `format: raw`) are not passed through. Use `format: raw` if you need those fields.

[encoding extension]: https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/encoding
