# Collector config: custom AKS receivers (test deployment)

The ConfigMap of the `otelcol-custom` test deployment, exported from a test AKS cluster
(namespace `otel-eh-test`) on 2026-11-08. It runs this repo's two custom receivers:

- [`aks`](custom-components/receiver/aksclusterreceiver/README.md): state of the AKS clusters in a resource group, as metrics.
- [`azure_activity_log`](custom-components/receiver/azureactivitylogreceiver/README.md): the resource group's Azure activity log, as logs.

Both send to `debug` exporters, so the data shows up in the pod log.

## ConfigMap

Exported with `kubectl -n otel-eh-test get configmap otelcol-custom-config -o yaml`.
Cluster-generated metadata (`uid`, `resourceVersion`, `creationTimestamp`, the
last-applied annotation) is removed. The subscription ID and resource group are replaced
with environment variables. Set them on the container, for example from a ConfigMap
via `envFrom`.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  labels:
    app: otelcol-custom
  name: otelcol-custom-config
  namespace: otel-eh-test
data:
  config.yaml: |
    extensions:
      health_check:
        endpoint: 0.0.0.0:13133
      azure_auth:
        workload_identity:
          client_id: ${env:AZURE_CLIENT_ID}
          tenant_id: ${env:AZURE_TENANT_ID}
          federated_token_file: ${env:AZURE_FEDERATED_TOKEN_FILE}

    receivers:
      aks:
        subscription_id: ${env:AZURE_SUBSCRIPTION_ID}
        resource_group: ${env:AZURE_RESOURCE_GROUP}
        auth: azure_auth
        collection_interval: 1m
      azure_activity_log:
        subscription_id: ${env:AZURE_SUBSCRIPTION_ID}
        resource_group: ${env:AZURE_RESOURCE_GROUP}
        auth: azure_auth
        collection_interval: 1m
        initial_lookback: 3h

    exporters:
      debug/metrics:
        verbosity: detailed
      debug/logs:
        verbosity: detailed

    service:
      extensions: [health_check, azure_auth]
      telemetry:
        logs:
          level: info
      pipelines:
        metrics:
          receivers: [aks]
          exporters: [debug/metrics]
        logs:
          receivers: [azure_activity_log]
          exporters: [debug/logs]
```

## What each part does

| Section | Setting | Purpose |
|---|---|---|
| `extensions.health_check` | `0.0.0.0:13133` | Readiness probe endpoint for the pod. |
| `extensions.azure_auth` | `workload_identity` | Gets Entra ID tokens for the pod's service principal. The three `AZURE_*` values are injected by the AKS Workload Identity webhook; nothing is stored in the config. |
| `receivers.aks` | resource group from `AZURE_RESOURCE_GROUP`, every `1m` | Reads every AKS cluster in the resource group from Azure Resource Manager: power state, Kubernetes version, node pools. |
| `receivers.azure_activity_log` | same resource group, every `1m`, `initial_lookback: 3h` | Reads the resource group's activity log. The first poll goes back 3 hours, later polls overlap by 15 minutes (`max_ingestion_delay` default) to catch late events, and repeats are skipped. |
| `auth: azure_auth` | on both receivers | Required: both receivers take their tokens from the `azure_auth` extension. |
| `exporters.debug/*` | `verbosity: detailed` | Prints every data point and log record to the pod log (testing only). |
| `service.pipelines` | `metrics` ← `aks`; `logs` ← `azure_activity_log` | One pipeline per receiver. |

## What else the deployment needs

The config alone isn't enough; the pod must run with an identity that can read the resources.

| Piece | Value |
|---|---|
| Image | `<registry>.azurecr.io/otelcol:<tag>`, built from `otelcollector/manifest.yaml` |
| Environment | `AZURE_SUBSCRIPTION_ID`, `AZURE_RESOURCE_GROUP` (the `AZURE_CLIENT_ID`, `AZURE_TENANT_ID` and `AZURE_FEDERATED_TOKEN_FILE` values come from the webhook) |
| Service account | `otel-eh-test/otelcol-custom`, annotated with `azure.workload.identity/client-id` (and `tenant-id`) |
| Pod label | `azure.workload.identity/use: "true"` |
| Azure identity | an Entra app registration / service principal with **no secret**: a federated credential trusts the service account above on the cluster's OIDC issuer |
| Azure role | **Reader** on the resource group (covers `managedClusters/read` and the activity log) |
| Mount | ConfigMap at `/etc/otelcol/config.yaml`, args `--config=/etc/otelcol/config.yaml` |

Without the Reader role, `aks` logs `403 AuthorizationFailed` every minute, and
`azure_activity_log` silently returns **no events**: the activity log API returns an
empty list, not an error, when the caller can't see anything.

## Test results (2026-11-08)

The resource group held two AKS clusters: the one running the collector, and a second one
that was stopped.

| Check | Result |
|---|---|
| Errors | none |
| `aks` | one batch per minute, 2 clusters, 16 data points: the running cluster up (Running, node pool up, 2 nodes, 1.37.1), the stopped cluster down (Stopped) |
| `azure_activity_log` | the cluster start (Started → Accepted → Succeeded), the kubeconfig fetch, and the Reader role assignment (Started → Succeeded), with callers and timestamps |
| Duplicates | none: 7 records, 7 unique event IDs across overlapping polls |
| Late events | the start's *Succeeded* event, published late by Azure, was picked up by the overlap window |

See the data:

```sh
kubectl -n otel-eh-test logs deploy/otelcol-custom
```

## Using it outside this test

- Replace the `debug` exporters with `elasticsearch` (see
  [`otelcollector/doc/receivers/aks.example.yaml`](otelcollector/doc/receivers/aks.example.yaml) and
  [`azure_activity_log.example.yaml`](otelcollector/doc/receivers/azure_activity_log.example.yaml)).
- Run a single replica: each replica polls on its own and would duplicate the data.
