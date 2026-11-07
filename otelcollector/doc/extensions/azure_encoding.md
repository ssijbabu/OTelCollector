# `azure_encoding` extension

Decodes Azure's native diagnostic formats (resource logs, activity logs, platform metrics) into OpenTelemetry logs and metrics.

Full example: [`azure_encoding.example.yaml`](azure_encoding.example.yaml)

## Use case on Kubernetes

Azure services around the cluster (Key Vault, Application Gateway, the AKS control plane,
Azure SQL) export **diagnostic settings** to an Event Hub. Those messages are Azure JSON, not
OTLP, so the standard Kafka receiver can't interpret them.

With `encoding: azure_encoding` on the Kafka receiver, the gateway turns them into proper
OTel log records and metrics, stored in Elasticsearch next to the cluster's own telemetry.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | The Deployment consuming the diagnostics Event Hub. |
| Agent (DaemonSet) | ❌ No | Nothing node-local to decode. |

## Configuration

```yaml
extensions:
  azure_encoding: {}
```

Wired into the pipelines:

```yaml
service:
  extensions: [azure_auth, azure_encoding]
  pipelines:
    logs:
      receivers: [kafka/azure-diagnostics]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
    metrics:
      receivers: [kafka/azure-diagnostics]
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

- In Azure, configure each resource's **Diagnostic settings** to stream to the Event Hub
  used in the example (`insights-logs`).

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

- Use a dedicated `group_id` per Event Hub consumer, so it doesn't compete with other gateways.
- AKS control-plane logs (`kube-apiserver`, `kube-audit`) are very high volume; select only the categories you need in the diagnostic setting.
- Use [`beats_encoding`](beats_encoding.md) instead when you want Elastic integration-style documents.

## Try it

```sh
./otelcol validate --config doc/extensions/azure_encoding.example.yaml
```
