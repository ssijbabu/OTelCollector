# `beats_encoding` extension

EDOT extension that decodes raw log bytes into Beats / Elastic Agent-style records with data-stream routing, so Elastic integrations' dashboards and pipelines work.

Full example: [`beats_encoding.example.yaml`](beats_encoding.example.yaml)

## Use case on Kubernetes

Elastic's **Azure integration** has ready-made dashboards and ingest pipelines for Azure
activity logs. They expect documents shaped like Elastic Agent output, in the
`azure.activitylogs` data stream.

The gateway reads activity logs from an Event Hub, `beats_encoding` unwraps Azure's
`records` array (one log per record) and sets the data-stream fields, and the
Elasticsearch exporter in **ECS** mapping mode writes documents the integration understands.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | The Deployment consuming the Event Hub. |
| Agent (DaemonSet) | ❌ No | Nothing node-local to decode. |

## Configuration

```yaml
extensions:
  beats_encoding/activitylogs:
    format: json
    unwrap: [records]           # {"records":[{...},{...}]} -> one log per record
    data_stream:
      dataset: azure.activitylogs
```

Wired into the pipelines:

```yaml
service:
  extensions: [azure_auth, beats_encoding/activitylogs]
  pipelines:
    logs:
      receivers: [kafka/activitylogs]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

- **Azure Workload Identity:** the AKS cluster has the OIDC issuer and workload identity
  enabled. A user-assigned managed identity is federated with the collector's service
  account (`system:serviceaccount:<namespace>:<service-account>`).
- **Role:** the identity has **Azure Event Hubs Data Receiver** on the Event Hubs namespace.
- **Pod:** the service account carries the client-ID annotation and the pod has the
  `azure.workload.identity/use: "true"` label. The webhook then injects `AZURE_CLIENT_ID`,
  `AZURE_TENANT_ID` and `AZURE_FEDERATED_TOKEN_FILE`.
- **Network:** egress to `login.microsoftonline.com:443` and `<namespace>.servicebus.windows.net:9093`.
- `EVENTHUB_NAMESPACE` (for example `myns.servicebus.windows.net`) from a ConfigMap.

- Install the **Azure** integration assets in Kibana (dashboards and ingest pipelines).

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: otel-gateway
  namespace: otel-gateway
  annotations:
    azure.workload.identity/client-id: <MANAGED_IDENTITY_CLIENT_ID>
---
# Deployment pod template
metadata:
  labels:
    azure.workload.identity/use: "true"   # webhook injects AZURE_* env + token file
spec:
  serviceAccountName: otel-gateway
```

## Tips and pitfalls

- Use `mapping.mode: ecs` on the Elasticsearch exporter; integrations expect ECS fields.
- One `beats_encoding/<name>` per source type (activity logs, sign-in logs, ...), each with its own `dataset`.
- Use [`azure_encoding`](azure_encoding.md) instead for OTel-native records.

## Try it

```sh
./otelcol validate --config doc/extensions/beats_encoding.example.yaml
```
