# `azure_auth` extension

Gets Microsoft Entra ID tokens for Azure services: workload identity, managed identity, service principal, or the default credential chain.

Full example: [`azure_auth.example.yaml`](azure_auth.example.yaml)

## Use case on Kubernetes

The gateway reads telemetry from **Azure Event Hubs** over the Kafka protocol. The secure way
to authenticate on AKS is **Azure Workload Identity**: the pod's Kubernetes service account
is federated with a managed identity, and the pod gets short-lived Entra ID tokens with
**no secret stored in the cluster**.

The Kafka receiver uses SASL `OAUTHBEARER` with `oauthbearer_token_source: azure_auth` to
present those tokens to Event Hubs.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | The Deployment that consumes from Event Hubs. |
| Agent (DaemonSet) | ✅ Yes | If agents produce to Event Hubs with the Kafka exporter (Data Sender role). |

## Configuration

```yaml
extensions:
  azure_auth:
    workload_identity:
      client_id: ${env:AZURE_CLIENT_ID}
      tenant_id: ${env:AZURE_TENANT_ID}
      federated_token_file: ${env:AZURE_FEDERATED_TOKEN_FILE}
    # Required for Kafka: the Event Hubs Kafka endpoint checks the token audience.
    scopes: ["https://${env:EVENTHUB_NAMESPACE}/.default"]
  # Alternatives:
  #   managed_identity: {client_id: <id>}            # VM / AKS kubelet identity
  #   service_principal: {tenant_id: ..., client_id: ..., client_secret: ${env:AZURE_CLIENT_SECRET}}
  #   use_default: true                               # DefaultAzureCredential chain
```

Wired into the pipelines:

```yaml
service:
  extensions: [azure_auth]
  pipelines:
    logs:
      receivers: [kafka]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
    metrics:
      receivers: [kafka]
      processors: [memory_limiter, batch]
      exporters: [elasticsearch]
    traces:
      receivers: [kafka]
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

- `scopes` is **required** for Kafka: `https://<namespace>.servicebus.windows.net/.default`. Without it, the token has the wrong audience and SASL authentication fails.
- All gateway replicas in the same Kafka `group_id` share the Event Hub's partitions; more replicas than partitions leaves replicas idle.
- Event Hubs Standard rejects Kafka compression (lz4, gzip) from producers; leave compression off on the producing side.
- For local testing, `use_default: true` picks up `az login` credentials.

## Try it

```sh
./otelcol validate --config doc/extensions/azure_auth.example.yaml
```
