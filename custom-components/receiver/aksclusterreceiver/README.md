# AKS receiver (`aks`)

| Status    |                     |
|-----------|---------------------|
| Stability | development: metrics, logs |
| Source    | Azure Resource Manager (`Microsoft.ContainerService/managedClusters`) |

Polls the Azure Resource Manager API for Azure Kubernetes Service clusters and reports
their state from the Azure side. That includes data the Kubernetes API can't give you,
because it's only visible while the cluster runs:

- whether the cluster or a node pool is **stopped**
- the provisioning state
- the Kubernetes and node image versions
- SKU tier, network plugin and upgrade channels
- node pool sizes and autoscaler bounds

Use it alongside `k8s_cluster` and `kubelet_stats`. Those report what runs *inside* the
cluster; this receiver reports the cluster *resource* in Azure.

Full example: [`doc/receivers/aks.example.yaml`](../../../otelcollector/doc/receivers/aks.example.yaml)

## Configuration

| Key | Default | Description |
|---|---|---|
| `subscription_id` | — (required) | Subscription that holds the clusters. |
| `resource_group` | — | Only clusters in this resource group. |
| `cluster_name` | — | Only this cluster. Requires `resource_group`. |
| `auth` | — (required) | ID of the [`azure_auth`](../../../otelcollector/doc/extensions/azure_auth.md) extension that provides the Azure tokens. The extension must be listed in `service.extensions`. |
| `collection_interval` | `5m` | How often to poll. Minimum `1m`. |
| `initial_delay` | `1s` | Delay before the first poll. |
| `timeout` | `30s` | Time limit for one poll, including paging. |
| `endpoint` | `https://management.azure.com` | ARM endpoint. Change it for sovereign clouds. The token scope is `<endpoint>/.default`. |
| `api_version` | `2025-05-01` | `Microsoft.ContainerService` API version. |

With only `subscription_id` set, the receiver lists every cluster in the subscription.

```yaml
extensions:
  azure_auth:
    workload_identity:
      client_id: ${env:AZURE_CLIENT_ID}
      tenant_id: ${env:AZURE_TENANT_ID}
      federated_token_file: ${env:AZURE_FEDERATED_TOKEN_FILE}

receivers:
  aks:
    subscription_id: ${env:AZURE_SUBSCRIPTION_ID}
    resource_group: example-rg
    cluster_name: example-aks
    auth: azure_auth
```

### Authentication

All tokens come from the `azure_auth` extension; the receiver has no credentials of its own.
It requests the scope `<endpoint>/.default` (`https://management.azure.com/.default`) itself,
so `azure_auth` needs no `scopes` setting, and the same extension instance can also serve
other components such as the Kafka receiver.

Any `azure_auth` credential type works:

| Where the collector runs | `azure_auth` block |
|---|---|
| AKS with Workload Identity (recommended) | `workload_identity: {tenant_id, client_id, federated_token_file}` |
| Azure VM, or the AKS kubelet identity | `managed_identity: {client_id: <optional>}` |
| Anywhere, with a service principal | `service_principal: {tenant_id, client_id, client_secret}` or `client_certificate_path` |
| Local development (`az login`) | `use_default: true` |

The receiver fails to start if `auth` points to a missing extension, or to one that doesn't
provide Azure tokens.

### Permissions

The identity needs **Reader** (or any role with
`Microsoft.ContainerService/managedClusters/read`) on the subscription, resource group
or cluster you collect from:

```sh
az role assignment create --assignee-object-id <PRINCIPAL_ID> --assignee-principal-type ServicePrincipal \
  --role Reader --scope /subscriptions/<SUBSCRIPTION_ID>
```

## Resource attributes

Each cluster is one resource.

| Attribute | Example |
|---|---|
| `cloud.provider` | `azure` |
| `cloud.platform` | `azure.aks` |
| `cloud.account.id` | subscription ID |
| `cloud.region` | `eastus` |
| `cloud.resource_id` | full ARM resource ID |
| `azure.resource_group.name` | `example-rg` |
| `k8s.cluster.name` | `example-aks` |

`k8s.cluster.name` matches what the `resource_detection` AKS detector and `k8s_attributes`
set on in-cluster telemetry, so the two can be correlated in Kibana.

## Metrics

All metrics are gauges.

| Metric | Unit | Attributes | Description |
|---|---|---|---|
| `aks.cluster.up` | `1` | | 1 when the cluster's power state is `Running`. |
| `aks.cluster.provisioning.succeeded` | `1` | | 1 when the provisioning state is `Succeeded`. |
| `aks.cluster.info` | `1` | `aks.kubernetes.version`, `aks.kubernetes.version.requested`, `aks.power_state`, `aks.provisioning_state`, `aks.sku.tier`, `aks.support_plan`, `aks.node_resource_group`, `aks.network.plugin`, `aks.network.plugin_mode`, `aks.network.policy`, `aks.network.outbound_type`, `aks.upgrade_channel`, `aks.node_os_upgrade_channel`, `aks.workload_identity.enabled`, `aks.oidc_issuer.enabled` | Always 1; carries the configuration. |
| `aks.node_pool.count` | `{node_pool}` | | Number of node pools. |
| `aks.node_pool.node.count` | `{node}` | `aks.node_pool.name`, `.mode`, `.vm_size`, `.os_type`, `.os_sku`, `.power_state`, `.provisioning_state`, `.kubernetes.version`, `.node_image.version` | **Configured** node count. Stays at the configured value while the pool is stopped. |
| `aks.node_pool.up` | `1` | `aks.node_pool.name` | 1 when the pool's power state is `Running`. |
| `aks.node_pool.pods.max` | `{pod}` | `aks.node_pool.name` | Max pods per node. |
| `aks.node_pool.autoscaling.enabled` | `1` | `aks.node_pool.name` | 1 when the cluster autoscaler is on. |
| `aks.node_pool.autoscaling.min` / `.max` | `{node}` | `aks.node_pool.name` | Autoscaler bounds. Only reported when autoscaling is on. |

## Logs

When the receiver is in a logs pipeline, it emits one log record per cluster per poll:

- **event name:** `aks.cluster.snapshot`
- **body:** the `managedClusters` resource JSON as Azure Resource Manager returns it, minus the sensitive fields listed below
- **attributes:** `aks.power_state` and `aks.provisioning_state`

Sensitive fields are removed from the snapshot before it leaves the receiver:

| Removed | Contains |
|---|---|
| `properties.linuxProfile` | admin username, SSH public keys |
| `properties.windowsProfile` | admin username, password |
| `properties.servicePrincipalProfile.secret` | service principal secret (legacy clusters) |
| `properties.aadProfile.serverAppSecret` | legacy AAD integration secret |
| `properties.httpProxyConfig` | proxy URLs (which can embed credentials) and trusted CA |

Identifiers such as the cluster's managed identity client and object IDs are kept. If you
don't want those stored either, drop them with a `transform` processor.

## Notes

- **Run one replica.** Every replica polls independently, which duplicates data and uses the
  subscription's ARM read quota. The receiver has no leader election, so on a scaled-out
  gateway, run it in a separate single-replica Deployment.
- **Metrics and logs pipelines poll separately.** Using the receiver in both doubles the
  API calls. At the default 5m interval that's negligible.
- **Failed polls:** errors are logged with the ARM error code (for example
  `403 AuthorizationFailed`) and nothing is emitted for that poll. If a later page of a
  subscription-wide listing fails, the clusters already read are still emitted.

## Development

```sh
cd custom-components/receiver/aksclusterreceiver
go test -race ./...
```

The module is wired into the build through `receivers` and `replaces` in
[`manifest.yaml`](../../../otelcollector/manifest.yaml).
