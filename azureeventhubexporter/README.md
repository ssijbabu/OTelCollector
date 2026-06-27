# Azure Event Hub Exporter

Exports OpenTelemetry logs, metrics, and traces to Azure Event Hubs using OTLP JSON encoding. Supports the native AMQP protocol and the Kafka-compatible endpoint, with SAS key or Azure AD (auth extension) authentication.

## Configuration reference

### Core settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `protocol` | string | `amqp` | Wire protocol: `amqp` or `kafka`. |
| `event_hub.namespace` | string | — | Fully qualified namespace, e.g. `mynamespace.servicebus.windows.net`. |
| `event_hub.name` | string | — | Event Hub name (also the Kafka topic). |
| `event_hub.shared_access_key_name` | string | — | SAS policy name. Required when `auth` is not set. |
| `event_hub.shared_access_key` | string | — | SAS key value. Required when `auth` is not set. |
| `auth` | component ID | — | Auth extension (e.g. `azureauthextension`). When set, SAS key fields are ignored. |

### Partition settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `partition_traces_by_id` | bool | `false` | Use the trace ID as the partition key, keeping all spans of a trace on one partition. |
| `partition_metrics_by_resource_attributes` | bool | `false` | Hash resource attributes as the partition key, keeping metrics from the same resource on one partition. |
| `partition_logs_by_resource_attributes` | bool | `false` | Hash resource attributes as the partition key. Mutually exclusive with `partition_logs_by_trace_id`. |
| `partition_logs_by_trace_id` | bool | `false` | Use the log record's trace ID as the partition key. Mutually exclusive with `partition_logs_by_resource_attributes`. |

When no partition flag is set for a signal, Event Hubs distributes messages across partitions automatically (round-robin).

### Reliability settings

| Field | Description |
|-------|-------------|
| `retry_on_failure` | Standard OTel retry configuration (`enabled`, `initial_interval`, `max_interval`, `max_elapsed_time`). |
| `sending_queue` | Standard OTel queue configuration (`enabled`, `num_consumers`, `queue_size`). |
| `timeout` | Per-request timeout. |

---

## Examples

### AMQP — SAS key authentication

```yaml
exporters:
  azure_event_hub:
    protocol: amqp
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry
      shared_access_key_name: RootManageSharedAccessKey
      shared_access_key: ${env:EVENTHUB_SAS_KEY}
```

### Kafka — SAS key authentication

The Kafka endpoint is derived automatically from `event_hub.namespace` as `<namespace>:9093` with TLS and SASL/PLAIN enabled.

```yaml
exporters:
  azure_event_hub:
    protocol: kafka
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

exporters:
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

exporters:
  azure_event_hub:
    protocol: kafka
    auth: azureauth
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry

service:
  extensions: [azureauth]
```

### Service principal authentication

```yaml
extensions:
  azureauth:
    service_principal:
      tenant_id: ${env:AZURE_TENANT_ID}
      client_id: ${env:AZURE_CLIENT_ID}
      client_secret: ${env:AZURE_CLIENT_SECRET}
    scopes:
      - https://eventhubs.azure.net/.default

exporters:
  azure_event_hub:
    auth: azureauth
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry
    retry_on_failure:
      enabled: true
      initial_interval: 5s
      max_interval: 30s
      max_elapsed_time: 300s
    sending_queue:
      enabled: true
      num_consumers: 4
      queue_size: 1000

service:
  extensions: [azureauth]
  pipelines:
    logs:
      receivers: [otlp]
      exporters: [azure_event_hub]
    metrics:
      receivers: [otlp]
      exporters: [azure_event_hub]
    traces:
      receivers: [otlp]
      exporters: [azure_event_hub]
```

### Separate Event Hubs per signal (recommended for production)

Event Hubs does not filter by content type. Mixing logs, metrics, and traces in one hub complicates consumer-side routing. Use three hubs and three named exporter instances:

```yaml
exporters:
  azure_event_hub/logs:
    protocol: kafka
    auth: azureauth
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-logs
    partition_logs_by_resource_attributes: true

  azure_event_hub/metrics:
    protocol: kafka
    auth: azureauth
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-metrics
    partition_metrics_by_resource_attributes: true

  azure_event_hub/traces:
    protocol: kafka
    auth: azureauth
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-traces
    partition_traces_by_id: true

service:
  extensions: [azureauth]
  pipelines:
    logs:
      receivers: [otlp]
      exporters: [azure_event_hub/logs]
    metrics:
      receivers: [otlp]
      exporters: [azure_event_hub/metrics]
    traces:
      receivers: [otlp]
      exporters: [azure_event_hub/traces]
```

---

## Partition strategies

The three signal-level flags are independent — enable any combination on a single exporter. The only constraint within logs: `partition_logs_by_resource_attributes` and `partition_logs_by_trace_id` are mutually exclusive.

| Flag | Signal | Partition key | Use when |
|------|--------|---------------|----------|
| `partition_logs_by_resource_attributes` | Logs | Hash of resource attributes | All logs from the same service should land on the same partition |
| `partition_logs_by_trace_id` | Logs | Trace ID | Logs carry `trace_id` and should be co-located with their traces |
| `partition_metrics_by_resource_attributes` | Metrics | Hash of resource attributes | All metrics from the same host/service should land on the same partition |
| `partition_traces_by_id` | Traces | Trace ID | All spans of a trace should land on the same partition |
| *(none set)* | Any | None (round-robin) | Order does not matter; simplest setup |

> **Message size:** Each partition group is sent as a single Event Hub message. If a resource's data exceeds the Event Hub message size limit (1 MB on Standard, 100 MB on Premium), the export fails. Reduce the batch size in the `batch` processor or `sending_queue` to stay within the limit.

### Partition by resource attributes — service-level ordering

All logs from `service-a` go to one partition; `service-b` to another. A consumer on partition 2 always sees `service-a` logs in emission order.

```yaml
exporters:
  azure_event_hub:
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-logs
      shared_access_key_name: RootManageSharedAccessKey
      shared_access_key: ${env:EVENTHUB_SAS_KEY}
    partition_logs_by_resource_attributes: true
```

```
Incoming batch (2 resources):
  service-a logs  →  hash({service.name="service-a"})  →  partition 1
  service-b logs  →  hash({service.name="service-b"})  →  partition 3
```

### Partition by trace ID — co-locate logs and spans

Log records and trace spans with the same trace ID land on the same partition.

```yaml
exporters:
  azure_event_hub:
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry
      shared_access_key_name: RootManageSharedAccessKey
      shared_access_key: ${env:EVENTHUB_SAS_KEY}
    partition_logs_by_trace_id: true
    partition_traces_by_id: true
```

```
Logs with trace_id=aaa   →  partition key "aaa..."  →  partition 0
Spans of trace aaa       →  partition key "aaa..."  →  partition 0  ← same partition
```

### All three signals partitioned simultaneously

```yaml
exporters:
  azure_event_hub:
    event_hub:
      namespace: mynamespace.servicebus.windows.net
      name: otel-telemetry
      shared_access_key_name: RootManageSharedAccessKey
      shared_access_key: ${env:EVENTHUB_SAS_KEY}
    partition_logs_by_resource_attributes: true
    partition_metrics_by_resource_attributes: true
    partition_traces_by_id: true
```

---

## Azure setup

### Create an Event Hub namespace and hub

```bash
RESOURCE_GROUP="my-rg"
LOCATION="eastus"
NAMESPACE="my-otel-ns"
EVENTHUB="otel-telemetry"

az eventhubs namespace create \
  --resource-group "$RESOURCE_GROUP" \
  --name "$NAMESPACE" \
  --location "$LOCATION" \
  --sku Standard

az eventhubs eventhub create \
  --resource-group "$RESOURCE_GROUP" \
  --namespace-name "$NAMESPACE" \
  --name "$EVENTHUB" \
  --partition-count 4 \
  --retention-time-in-hours 24
```

### Grant send permissions

The minimum required role is **Azure Event Hubs Data Sender** scoped to the namespace or a specific hub.

```bash
NAMESPACE_ID=$(az eventhubs namespace show \
  --resource-group "$RESOURCE_GROUP" \
  --name "$NAMESPACE" \
  --query id -o tsv)

# For a managed identity or service principal
az role assignment create \
  --assignee "<principal-id>" \
  --role "Azure Event Hubs Data Sender" \
  --scope "$NAMESPACE_ID"
```

### Rotate a client secret

1. Add a new credential to the App Registration **before** deleting the old one.
2. Update `client_secret` in the collector configuration and redeploy.
3. After confirming the collector is healthy, delete the old credential.

```bash
NEW_SECRET=$(az ad app credential reset \
  --id "$APP_ID" --append --years 1 --query password -o tsv)

# List existing credentials to find the old key ID, then remove it
az ad app credential list --id "$APP_ID"
az ad app credential delete --id "$APP_ID" --key-id "<OLD_KEY_ID>"
```
