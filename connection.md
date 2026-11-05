# OpenTelemetry Collector: Azure Event Hubs with Workload Identity, and Agent→Gateway mTLS with SPIFFE/SPIRE

This guide covers two separate setups:

1. **Collector ↔ Azure Event Hubs** using the stock `kafkaexporter` / `kafkareceiver` with the
   `azure_auth` extension and **Azure Workload Identity** (no secrets). This setup was tested end to end on AKS.
2. **Agent collector → Gateway collector** using **mutual TLS with SPIFFE/SPIRE certificates (SVIDs)**.
   This setup has **not been tested yet**; treat it as a reference design.

---

## Part 1: Event Hubs over Kafka with Workload Identity

### How it works

```
OTLP ──► collector ──► kafkaexporter ──(SASL OAUTHBEARER, TLS :9093)──► Event Hubs
                              ▲
                     azure_auth extension
                     (WorkloadIdentityCredential → Entra ID token)
```

The Kafka client asks the `azure_auth` extension for an OAuth token on every SASL handshake
(`auth.sasl.oauthbearer_token_source: azure_auth`). The extension gets that token from Microsoft Entra ID
by exchanging the projected Kubernetes service account token (federated credential).

### Azure prerequisites

```bash
RG=<resource-group>; AKS=<aks-name>; NS=<eventhubs-namespace>; K8S_NS=<k8s-namespace>; SA=otel-collector

# 1. AKS with OIDC issuer + workload identity enabled
az aks update -g $RG -n $AKS --enable-oidc-issuer --enable-workload-identity

# 2. User-assigned managed identity
az identity create -g $RG -n otel-collector-id
CLIENT_ID=$(az identity show -g $RG -n otel-collector-id --query clientId -o tsv)
PRINCIPAL_ID=$(az identity show -g $RG -n otel-collector-id --query principalId -o tsv)

# 3. Federated credential bound to the Kubernetes service account
ISSUER=$(az aks show -g $RG -n $AKS --query oidcIssuerProfile.issuerUrl -o tsv)
az identity federated-credential create -g $RG --identity-name otel-collector-id -n otel-collector-fic \
  --issuer "$ISSUER" --subject system:serviceaccount:$K8S_NS:$SA --audiences api://AzureADTokenExchange

# 4. RBAC on the Event Hubs namespace
NSID=$(az eventhubs namespace show -g $RG -n $NS --query id -o tsv)
for r in "Azure Event Hubs Data Sender" "Azure Event Hubs Data Receiver"; do
  az role assignment create --assignee-object-id $PRINCIPAL_ID --assignee-principal-type ServicePrincipal \
    --role "$r" --scope $NSID
done
```

### Kubernetes prerequisites

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: otel-collector
  namespace: <k8s-namespace>
  annotations:
    azure.workload.identity/client-id: <CLIENT_ID>
---
# In the collector Deployment pod template:
metadata:
  labels:
    azure.workload.identity/use: "true"   # webhook injects the token file + AZURE_* env vars
spec:
  serviceAccountName: otel-collector
```

### Collector configuration

Logs are shown here. For metrics or traces, add `metrics:` / `traces:` blocks to the receiver and exporter,
and give each signal its own Event Hub.

```yaml
extensions:
  azure_auth:
    workload_identity:
      tenant_id: <TENANT_ID>
      client_id: <CLIENT_ID>
      federated_token_file: /var/run/secrets/azure/tokens/azure-identity-token
    scopes: ["https://<NAMESPACE>.servicebus.windows.net/.default"]   # REQUIRED for Kafka

receivers:
  otlp:
    protocols:
      grpc: {endpoint: 0.0.0.0:4317}
  kafka:
    brokers: ["<NAMESPACE>.servicebus.windows.net:9093"]
    tls: {insecure: false}
    auth:
      sasl:
        mechanism: OAUTHBEARER
        oauthbearer_token_source: azure_auth
    use_leader_epoch: false            # Event Hubs doesn't fully support leader epochs
    group_id: <CONSUMER_GROUP>
    logs: {topics: [<EVENT_HUB_NAME>], encoding: otlp_proto}

exporters:
  kafka:
    brokers: ["<NAMESPACE>.servicebus.windows.net:9093"]
    tls: {insecure: false}
    auth:
      sasl:
        mechanism: OAUTHBEARER
        oauthbearer_token_source: azure_auth
    logs: {topic: <EVENT_HUB_NAME>, encoding: otlp_proto}
    timeout: 60s
    producer:
      compression: none                # Event Hubs Standard rejects lz4 and gzip
      # keep max_message_bytes at the 1000000 default; never raise it
    sending_queue:
      sizer: bytes
      queue_size: 50000000             # 50 MB in-memory queue
      batch:
        sizer: bytes
        min_size: 1
        max_size: 900000               # 900 KB per message, under the 1 MB Event Hubs limit
        flush_timeout: 1s

service:
  extensions: [azure_auth]
  pipelines:
    logs/out: {receivers: [otlp], exporters: [kafka]}
    logs/in:  {receivers: [kafka], exporters: [<YOUR_EXPORTER>]}
```

### Other Azure auth types

All of these use the same `OAUTHBEARER` setup; only the `azure_auth` block changes. Each one still needs `scopes`.

| Auth type | `azure_auth` block |
|---|---|
| Workload identity | `workload_identity: {tenant_id, client_id, federated_token_file}` |
| Managed identity | `managed_identity: {client_id: <optional, for user-assigned>}` |
| Service principal, secret | `service_principal: {tenant_id, client_id, client_secret: ${env:AZURE_CLIENT_SECRET}}` |
| Service principal, certificate | `service_principal: {tenant_id, client_id, client_certificate_path}` |
| Default credential chain | `use_default: true` |

Connection strings don't use `azure_auth`. Use SASL `PLAIN` with `username: "$$ConnectionString"` and
`password: ${env:EH_CONN}` (`$$` escapes `$` in collector config).

### Test results

Tested on AKS against Event Hubs Standard (1 throughput unit) with collector v0.164.

| Test | Result |
|---|---|
| Logs round trip, each auth type (workload identity, managed identity, SP secret, SP certificate, default chain, connection string) | ~600 sent / ~600 received each, 0 failed |
| Metrics (Gauge, Sum, Histogram, ExponentialHistogram), workload identity | 366 / 366 data points |
| Traces with `partition_traces_by_id`, workload identity | 228 / 228 spans |
| Throughput, workload identity, 8 partitions, no compression | 283,706 logs in 60 s (~4.7k/s), 0 failed |
| Throughput with `lz4` or `gzip` | **100% dropped**: `UNSUPPORTED_FOR_MESSAGE_FORMAT` |
| Workload identity without `scopes` | **Nothing sent**: `WorkloadIdentityCredential.GetToken() requires at least one scope` |

### Gotchas

- **`scopes` is mandatory.** With Kafka, the extension passes the configured scopes straight through and has no default.
- **No compression on Standard tier.** It is a permanent error, so the data is dropped, not retried.
- **The 1 MB limit isn't reported cleanly.** For an oversized send, Event Hubs resets the TCP connection
  (`connection reset by peer`) instead of returning `MESSAGE_TOO_LARGE`. The client retries until it times out.
  - The default `max_message_bytes: 1000000` catches oversize locally and fails fast (permanent error).
  - Raising `max_message_bytes` lets the oversized data through to Event Hubs, which then resets the connection over and over.
- **The exporter doesn't split payloads.** A single export larger than the limit is dropped. Batching with
  `sizer: bytes` + `max_size: 900000` prevents that.
- **900 KB batches can still see resets.** In testing, all data arrived, but only after several retries. The likely cause is
  the Kafka client packing batches for several partitions into one send request totalling more than 1 MB. If you see this in
  debug logs, lower `max_size` (e.g. 450000) or partition so each request goes to a single partition.
- **Pod readiness doesn't show auth failures.** `health_check` reports ready even when SASL authentication is failing.
  Alert on `otelcol_exporter_send_failed_*` and on exporter logs instead.

---

## Part 2: Agent → Gateway mTLS with SPIFFE/SPIRE

> **Status: reference design, not tested yet.**

### How it works

The collector cannot talk to the SPIRE Workload API itself (`configtls` has no SPIFFE support).
A **spiffe-helper** sidecar fetches the SVID (the SPIFFE certificate) and the trust bundle, then writes them to a shared
in-memory volume. The collector loads them with its standard TLS settings and reloads them as they rotate.

```
SPIRE agent (DaemonSet)
   │  Workload API socket (SPIFFE CSI driver)
   ▼
spiffe-helper sidecar ──writes──► /certs/svid.pem, svid_key.pem, bundle.pem  (emptyDir, Memory)
                                        ▲
collector container ──reads + reloads──┘

agent pod  ──OTLP gRPC + mutual TLS──►  gateway pod
```

### Gateway: OTLP receiver requiring client certificates

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
        tls:
          cert_file: /certs/svid.pem
          key_file: /certs/svid_key.pem
          client_ca_file: /certs/bundle.pem   # this is what makes it mutual TLS: clients must present a cert
          client_ca_file_reload: true          # picks up trust bundle changes
          reload_interval: 5m                  # re-reads own cert/key
          min_version: "1.3"
```

### Agent: OTLP exporter presenting its certificate

```yaml
exporters:
  otlp:
    endpoint: otel-gateway.observability.svc.cluster.local:4317
    tls:
      ca_file: /certs/bundle.pem
      cert_file: /certs/svid.pem
      key_file: /certs/svid_key.pem
      reload_interval: 5m
      min_version: "1.3"
```

### spiffe-helper sidecar (both pods)

```yaml
spec:
  initContainers:
    - name: spiffe-helper
      image: ghcr.io/spiffe/spiffe-helper:<version>
      restartPolicy: Always          # native sidecar (k8s 1.29+); starts before the collector
      args: ["-config", "/etc/spiffe-helper/helper.conf"]
      volumeMounts:
        - {name: spire-agent-socket, mountPath: /spiffe-workload-api, readOnly: true}
        - {name: certs, mountPath: /certs}
        - {name: helper-conf, mountPath: /etc/spiffe-helper}
  containers:
    - name: otelcol
      volumeMounts:
        - {name: certs, mountPath: /certs, readOnly: true}
  volumes:
    - name: spire-agent-socket
      csi: {driver: csi.spiffe.io, readOnly: true}
    - name: certs
      emptyDir: {medium: Memory}      # private key never touches disk
    - name: helper-conf
      configMap: {name: spiffe-helper}
```

`helper.conf` (ConfigMap `spiffe-helper`):

```hcl
agent_address         = "/spiffe-workload-api/spire-agent.sock"
cert_dir              = "/certs"
svid_file_name        = "svid.pem"
svid_key_file_name    = "svid_key.pem"
svid_bundle_file_name = "bundle.pem"
daemon_mode           = true
```

### SPIRE registration (spire-controller-manager)

The gateway's SVID **must include its Service DNS name**. Without it, the agent's TLS hostname check fails,
because SVIDs only carry a `spiffe://` URI by default.

```yaml
apiVersion: spire.spiffe.io/v1alpha1
kind: ClusterSPIFFEID
metadata: {name: otel-gateway}
spec:
  spiffeIDTemplate: "spiffe://{{ .TrustDomain }}/ns/{{ .PodMeta.Namespace }}/sa/{{ .PodSpec.ServiceAccountName }}"
  podSelector: {matchLabels: {app: otel-gateway}}
  dnsNameTemplates: ["otel-gateway.observability.svc.cluster.local"]
---
apiVersion: spire.spiffe.io/v1alpha1
kind: ClusterSPIFFEID
metadata: {name: otel-agent}
spec:
  spiffeIDTemplate: "spiffe://{{ .TrustDomain }}/ns/{{ .PodMeta.Namespace }}/sa/{{ .PodSpec.ServiceAccountName }}"
  podSelector: {matchLabels: {app: otel-agent}}
```

### Certificate rotation

- spiffe-helper renews the SVID at about half its lifetime (default lifetime 1h) and rewrites the files.
- The collector re-reads its certificate and key every `reload_interval`. Keep it well under half the lifetime (5m is fine).
- The gateway reloads the trust bundle when the file changes (`client_ca_file_reload: true`).
- It is unclear whether `reload_interval` also reloads the agent's `ca_file`. Restart agents after a CA rotation to be safe.

### Limitations and caveats

- **No authorization by SPIFFE ID.** The gateway accepts *any* client whose certificate chains to the trust bundle, so
  any workload in the trust domain can connect. To allowlist only agent SPIFFE IDs, choose one of:
  - an Envoy or ghostunnel sidecar in front of the gateway receiver that checks the client's SPIFFE ID, or
  - a dedicated trust domain or intermediate CA used only by the collectors.
- **Never use `insecure_skip_verify`.** Use `dnsNameTemplates` (preferred) or `server_name_override`.
- **Load balancing.** gRPC keeps long-lived connections to one gateway pod. For even load use a headless Service with
  `loadbalancing` exporter, or a mesh or L7 load balancer. Every gateway pod's SVID needs the DNS name the agents dial.

### Validation checklist (to run when tested)

- [ ] Agent → gateway export succeeds (`otelcol_exporter_sent_*` > 0, no TLS errors)
- [ ] A client without a certificate is rejected by the gateway
- [ ] A client with a certificate from a different trust domain is rejected
- [ ] Data keeps flowing across an SVID rotation (shorten the lifetime to ~5m to test)
- [ ] Gateway hostname check passes without `server_name_override` (DNS SAN present)
