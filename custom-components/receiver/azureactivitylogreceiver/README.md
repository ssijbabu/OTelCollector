# Azure activity log receiver (`azure_activity_log`)

| Status    |                     |
|-----------|---------------------|
| Stability | development: logs   |
| Source    | Azure Resource Manager (`Microsoft.Insights/eventtypes/management/values`) |

Reads a subscription's **activity log** (control-plane events) by polling the Azure Resource
Manager API. That covers who created, changed, deleted, started or stopped which resource,
failed role assignments, policy evaluations, service health and resource health events.
Each event becomes one log record.

No diagnostic setting or Event Hub is needed. The alternative is a push setup: a diagnostic
setting streams the activity log to Event Hubs, and the `kafka` receiver reads it with the
`azure_encoding` extension. That has lower latency and scales better for many subscriptions,
but needs that extra infrastructure.

Full example: [`azure_activity_log.example.yaml`](../../../otelcollector/doc/receivers/azure_activity_log.example.yaml)

## Configuration

| Key | Default | Description |
|---|---|---|
| `subscription_id` | — (required) | Subscription whose activity log is read. |
| `auth` | — (required) | ID of the [`azure_auth`](../../../otelcollector/doc/extensions/azure_auth.md) extension that provides the Azure tokens. |
| `resource_group` | — | Only events for this resource group. |
| `resource_id` | — | Only events for this resource (full resource ID). |
| `resource_provider` | — | Only events for this provider, e.g. `Microsoft.ContainerService`. |
| `collection_interval` | `1m` | How often to poll. Minimum `30s`. |
| `max_ingestion_delay` | `15m` | How far back each poll re-reads to catch events that Azure publishes late. Events already emitted are skipped. |
| `initial_lookback` | `15m` | How far back the first poll after startup reads. Up to `2160h` (90 days, the activity log retention). |
| `initial_delay` | `1s` | Delay before the first poll. |
| `timeout` | `1m` | Time limit for one poll, including paging. |
| `endpoint` | `https://management.azure.com` | ARM endpoint. Change it for sovereign clouds. |
| `api_version` | `2015-04-01` | Activity log API version. |

Only one of `resource_group`, `resource_id` and `resource_provider` can be set, because the
Azure API accepts a single extra filter. To filter further, use a `filter` processor.

```yaml
extensions:
  azure_auth:
    workload_identity:
      client_id: ${env:AZURE_CLIENT_ID}
      tenant_id: ${env:AZURE_TENANT_ID}
      federated_token_file: ${env:AZURE_FEDERATED_TOKEN_FILE}

receivers:
  azure_activity_log:
    subscription_id: ${env:AZURE_SUBSCRIPTION_ID}
    auth: azure_auth
    collection_interval: 1m
```

### Authentication and permissions

Tokens come from the `azure_auth` extension. The receiver requests the
`<endpoint>/.default` scope itself, so `azure_auth` needs no `scopes` setting. Any
`azure_auth` credential type works (workload identity, managed identity, service principal,
`use_default: true` for local `az login`).

The identity needs **Reader** (or **Monitoring Reader**) on the subscription:

```sh
az role assignment create --assignee-object-id <PRINCIPAL_ID> --assignee-principal-type ServicePrincipal \
  --role "Monitoring Reader" --scope /subscriptions/<SUBSCRIPTION_ID>
```

## How polling works

```
poll 1 (startup)  [now - initial_lookback ............ now]
poll 2                    [prev end - max_ingestion_delay .. now]
poll 3                              [prev end - max_ingestion_delay .. now]
```

- **Overlapping windows:** Azure can publish an event several minutes after its
  `eventTimestamp`, so each window overlaps the previous one by `max_ingestion_delay`.
- **De-duplication:** event IDs (`eventDataId`) emitted within the overlap are remembered
  and skipped.
- **Retries:** if a poll fails (throttling, auth, network) or the next consumer rejects
  the data, the window doesn't advance. The next poll retries it.
- **Late events:** an event published later than `max_ingestion_delay` after its timestamp
  is missed. Raise the value if you see gaps.

**State is in memory.** After a restart, the first poll reads `initial_lookback` again. If
it overlaps what was already sent, those events are sent twice; anything older than
`initial_lookback` that happened while the collector was down is missed. To avoid both,
set `initial_lookback` to cover your longest expected downtime and de-duplicate in the
backend. In Elasticsearch, `azure.activity_log.event_data_id` is unique per event.

## Log records

| Field | Value |
|---|---|
| Timestamp | `eventTimestamp` |
| Observed timestamp | time of the poll |
| Event name | `azure.activity_log` |
| Severity | from `level`: Critical → FATAL, Error → ERROR, Warning → WARN, Informational → INFO, Verbose → DEBUG |
| Body | the event document, minus the sensitive fields below |

Records are grouped by the resource the event is about. Resource attributes:
`cloud.provider` (`azure`), `cloud.account.id`, `cloud.resource_id` and
`azure.resource_group.name`. Events about an AKS cluster share `cloud.resource_id` with the
`aks` receiver's data for that cluster.

Log attributes (only set when present):

| Attribute | Source |
|---|---|
| `azure.activity_log.event_data_id` | `eventDataId` (unique per event) |
| `azure.activity_log.operation.name` | `operationName.value`, e.g. `Microsoft.ContainerService/managedClusters/stop/action` |
| `azure.activity_log.operation.id` | `operationId` |
| `azure.activity_log.category` | `category.value`: `Administrative`, `Policy`, `ServiceHealth`, `ResourceHealth`, `Security`, `Alert`, `Autoscale`, `Recommendation` |
| `azure.activity_log.status` / `.sub_status` | e.g. `Started`, `Succeeded`, `Failed` / `Forbidden` |
| `azure.activity_log.caller` | who did it: UPN, service principal or app ID |
| `azure.activity_log.correlation_id` | groups the events of one operation |
| `azure.activity_log.resource_provider` / `.resource_type` | e.g. `Microsoft.ContainerService` |
| `azure.activity_log.authorization.action` | RBAC action checked |
| `azure.tenant.id` | `tenantId` |
| `client.address` | caller's IP (`httpRequest.clientIpAddress`) |
| `http.request.method` | `httpRequest.method` |

### Removed sensitive fields

| Removed | Contains |
|---|---|
| `claims` | the caller's full token claims: name, IP address, object ID, authentication methods |
| `properties.requestbody` | request payloads, which can include passwords and keys |
| `properties.responseBody` | response payloads |

The caller identity (`caller`) and IP address (`client.address`) are kept because they are
the core of an audit trail. Drop them with a `transform` processor if you don't want them.

## Notes

- **Run one replica.** Every replica polls and de-duplicates independently, so running
  several duplicates the events. On a scaled-out gateway, use a separate single-replica
  Deployment.
- **One subscription per receiver.** For several subscriptions, add one instance each
  (`azure_activity_log/prod`, `azure_activity_log/dev`).
- **Event volume:** a busy subscription can produce thousands of events per hour, many of
  them `Policy` audits or `read` operations. Filter them out with a `filter` processor if
  you don't need them.

## Development

```sh
cd custom-components/receiver/azureactivitylogreceiver
go test -race ./...
```

The module is wired into the build through `receivers` and `replaces` in
[`manifest.yaml`](../../../otelcollector/manifest.yaml).
