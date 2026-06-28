# OTelCollector

A custom OpenTelemetry Collector built on the [Elastic Distribution of OpenTelemetry (EDOT) Collector](https://www.elastic.co/docs/reference/edot-collector/custom-collector), extended with Azure Event Hub ingestion and export.

Compatible with OTel Collector **v1.58.0** / contrib **v0.152.0** / EDOT components **v0.50.0**.

## What's included

The full EDOT component set plus two Azure-specific additions from this repo:

| Component | Type | Purpose |
|---|---|---|
| `azure_event_hub` | Receiver | Ingests telemetry from Azure Event Hub (AMQP or Kafka protocol) |
| `azure_event_hub` | Exporter | Forwards telemetry to Azure Event Hub (AMQP or Kafka protocol) |
| `azure_auth` | Extension | Azure AD authentication — managed identity, workload identity, service principal |

---

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| [Go](https://go.dev/dl/) | 1.26+ | Required for local binary builds |
| [Docker](https://docs.docker.com/get-docker/) | 24+ with BuildKit | Required for container builds |
| [OCB](https://opentelemetry.io/docs/collector/extend/ocb/) | 0.152.0 | Required only to regenerate sources from `manifest.yaml` |

Install OCB:

```sh
make install-ocb
# or directly:
go install go.opentelemetry.io/collector/cmd/builder@v0.152.0
```

---

## Repository structure

```
otelcollector/
├── manifest.yaml            # OCB component manifest — source of truth for included components
├── collector-config.yaml    # Default runtime configuration
├── Dockerfile               # Three-stage build: OCB → static binary → distroless image
├── Makefile                 # Build, run, and test targets
├── azure-pipelines.yml      # Azure DevOps CI/CD pipeline
│
├── main.go                  # OCB-generated entry point
├── main_others.go           # OCB-generated — Linux / macOS
├── main_windows.go          # OCB-generated — Windows
├── components.go            # OCB-generated — registers all component factories
├── go.mod / go.sum          # OCB-generated module graph (with transitive pins applied)
│
└── examples/
    ├── binary/
    │   └── config.yaml      # Minimal config for local binary testing (Kafka, SAS key)
    └── k8s/
        ├── cluster-info.yaml        # Shared cluster metadata ConfigMap
        ├── collector/               # DaemonSet deployment (one pod per node)
        │   ├── namespace.yaml
        │   ├── serviceaccount.yaml
        │   ├── configmap.yaml
        │   ├── env-configmap.yaml
        │   ├── deployment.yaml      # Kind: DaemonSet
        │   ├── service.yaml
        │   └── pdb.yaml
        └── gateway/                 # Deployment (2 replicas, HA)
            ├── namespace.yaml
            ├── serviceaccount.yaml
            ├── configmap.yaml
            ├── env-configmap.yaml
            ├── deployment.yaml      # Kind: Deployment, replicas: 2
            ├── service.yaml
            └── pdb.yaml
```

The OCB-generated files (`main.go`, `components.go`, `go.mod`, `go.sum`) are committed so the binary can be built with plain `go build` without installing OCB.

---

## Quick start — local binary

### 1. Build

```sh
make build
# Produces ./OTelCollector
```

### 2. Set environment variables

```sh
export EVENTHUB_NAMESPACE=myns.servicebus.windows.net
export EVENTHUB_NAME=otel-telemetry
export EVENTHUB_SAS_KEY_NAME=RootManageSharedAccessKey
export EVENTHUB_SAS_KEY=<key-value>
```

### 3. Run

```sh
make run
# or directly:
./OTelCollector --config collector-config.yaml
```

Health check: `curl http://localhost:13133/`

### 4. Send test data

```sh
# Install telemetrygen once
make install-telemetrygen

# Send logs, metrics, and traces
make send-logs
make send-metrics
make send-traces
```

---

## Docker build and run

The Docker build context must be the **repo root** so the local receiver and exporter source folders are accessible to the build:

```sh
# From the repo root:
docker build -f otelcollector/Dockerfile -t otelcollector:latest .
```

Run the image:

```sh
docker run --rm \
  -e EVENTHUB_NAMESPACE="myns.servicebus.windows.net" \
  -e EVENTHUB_NAME="otel-telemetry" \
  -e EVENTHUB_SAS_KEY_NAME="RootManageSharedAccessKey" \
  -e EVENTHUB_SAS_KEY="<key-value>" \
  -p 4317:4317 \
  -p 4318:4318 \
  -p 13133:13133 \
  otelcollector:latest
```

Or with the Makefile (from the `otelcollector/` directory):

```sh
make docker-build
make docker-run
```

| Port | Protocol | Purpose |
|---|---|---|
| 4317 | OTLP gRPC | Receive traces, metrics, logs |
| 4318 | OTLP HTTP | Receive traces, metrics, logs |
| 13133 | HTTP | Health check |

---

## Configuration

### Environment variables

All four are required when using SAS key authentication:

| Variable | Example | Description |
|---|---|---|
| `EVENTHUB_NAMESPACE` | `myns.servicebus.windows.net` | Fully qualified Event Hubs namespace |
| `EVENTHUB_NAME` | `otel-telemetry` | Event Hub name |
| `EVENTHUB_SAS_KEY_NAME` | `RootManageSharedAccessKey` | SAS policy name |
| `EVENTHUB_SAS_KEY` | `abc123==` | SAS key value |

### Default pipeline

`collector-config.yaml` sets up the following pipeline:

```
OTLP gRPC/HTTP (ports 4317/4318)
        ↓
memory_limiter → resourcedetection → batch
        ↓
debug exporter + azure_event_hub exporter (AMQP)
```

### Choosing a protocol

The exporter and receiver both support AMQP (default) and Kafka. Switch by setting `protocol:`:

```yaml
exporters:
  azure_event_hub:
    protocol: kafka          # or "amqp"
    event_hub:
      namespace: ${env:EVENTHUB_NAMESPACE}
      name: ${env:EVENTHUB_NAME}
      shared_access_key_name: ${env:EVENTHUB_SAS_KEY_NAME}
      shared_access_key: ${env:EVENTHUB_SAS_KEY}
```

The Kafka endpoint (`<namespace>:9093`, TLS + SASL/PLAIN) is derived automatically from `event_hub.namespace`. No extra configuration is required.

**When to use Kafka:**
- Consumer group offset tracking is built in — the receiver resumes from its last committed offset after a restart without needing a storage extension.
- Multiple independent consumer groups can each read the full stream (e.g. the collector uses `$Default`, the gateway uses `otel-gateway`, a local binary uses `binary-test`).

**When to use AMQP:**
- Native Azure Event Hub protocol — lower overhead for simple send/receive.
- Supports per-partition targeting (`partition:`) and distributed coordination via `blob_checkpoint_store`.

### Using Azure AD authentication (no SAS key)

Replace the SAS key fields with an `auth:` reference and configure the `azure_auth` extension. The protocol choice determines which OAuth scope is used — this is handled automatically by the exporter and receiver, not by the extension config.

#### OAuth scopes by protocol

| Protocol | OAuth scope used | Why |
|---|---|---|
| **AMQP** | `https://eventhubs.azure.net/.default` | Standard Azure Event Hubs resource URI; used by the `azeventhubs` SDK for all AMQP connections |
| **Kafka** | `https://<namespace>.servicebus.windows.net/.default` | The Kafka endpoint validates the token's `aud` claim against the namespace hostname — the generic `eventhubs.azure.net` audience causes a **"Invalid tenant name 'eventhubs'"** SASL auth error |

The scope is set internally by the exporter and receiver based on the configured namespace. You do not need to set `scopes:` in the `azure_auth` extension for Event Hub auth.

#### Kafka (SASL/OAUTHBEARER)

```yaml
extensions:
  azure_auth:
    use_default: true          # or managed_identity / workload_identity / service_principal

exporters:
  azure_event_hub:
    protocol: kafka
    auth: azure_auth
    event_hub:
      namespace: myns.servicebus.windows.net   # scope becomes https://myns.servicebus.windows.net/.default
      name: otel-telemetry

receivers:
  azure_event_hub:
    protocol: kafka
    group: otel-collector
    auth: azure_auth
    event_hub:
      namespace: myns.servicebus.windows.net
      name: otel-telemetry

service:
  extensions: [health_check, azure_auth]
```

#### AMQP

```yaml
extensions:
  azure_auth:
    use_default: true

exporters:
  azure_event_hub:
    protocol: amqp
    auth: azure_auth
    event_hub:
      namespace: myns.servicebus.windows.net   # scope becomes https://eventhubs.azure.net/.default
      name: otel-telemetry

receivers:
  azure_event_hub:
    protocol: amqp
    auth: azure_auth
    event_hub:
      namespace: myns.servicebus.windows.net
      name: otel-telemetry

service:
  extensions: [health_check, azure_auth]
```

#### Authentication methods

The `azure_auth` extension supports four methods — use whichever matches your environment:

```yaml
# Workload Identity via DefaultAzureCredential (AKS — no secret material in the cluster)
# Relies on AZURE_CLIENT_ID, AZURE_TENANT_ID, AZURE_FEDERATED_TOKEN_FILE being injected
# by the Azure Workload Identity webhook. Simpler but opaque — credential type is inferred at runtime.
azure_auth:
  use_default: true

# Explicit Workload Identity — same webhook-injected token file, but config is self-describing.
# Prefer this when you want the credential type visible in the config rather than inferred.
# The token file path is fixed; the webhook always writes to this location.
azure_auth:
  workload_identity:
    tenant_id: <tenant-id>
    client_id: <client-id>
    federated_token_file: /var/run/secrets/azure/tokens/azure-identity-token

# Managed Identity
azure_auth:
  managed_identity:
    client_id: <client-id>   # omit for system-assigned

# Service Principal — client secret
azure_auth:
  service_principal:
    tenant_id: <tenant-id>
    client_id: <client-id>
    client_secret: ${env:AZURE_CLIENT_SECRET}

# Service Principal — certificate (PEM or unencrypted PFX)
azure_auth:
  service_principal:
    tenant_id: <tenant-id>
    client_id: <client-id>
    client_certificate_path: /etc/otel-cert/cert.pem
```

> **Note:** `client_certificate_path` must point to an unencrypted PEM or PFX file — the extension does not support passphrase-protected certificates natively. For passphrase-protected PFX files in Kubernetes, use an init container to decrypt the PFX to a PEM file in a memory-backed `emptyDir` volume before the collector starts.

For AKS Workload Identity, annotate the ServiceAccount and add the pod label — see the [Kubernetes deployment](#kubernetes-deployment) section.

---

## Binary example (`examples/binary/`)

A standalone config for local round-trip testing using the Kafka protocol. Ports are offset from the defaults (13134, 14317, 14318) so they don't conflict with a locally port-forwarded k8s collector.

```sh
cd examples/binary

export EVENTHUB_NAMESPACE=myns.servicebus.windows.net
export EVENTHUB_NAME=otel-telemetry
export EVENTHUB_SAS_KEY_NAME=RootManageSharedAccessKey
export EVENTHUB_SAS_KEY=<key-value>

./OTelCollector --config config.yaml
```

To rebuild the binary from source:

```sh
# From the otelcollector/ directory:
go build -o examples/binary/OTelCollector .
```

Send test data and watch it arrive back via the Kafka receiver:

```sh
telemetrygen logs \
  --otlp-insecure \
  --otlp-endpoint localhost:14317 \
  --duration 5s --rate 3 \
  --body "hello from binary"
```

The `logs/from_hub` pipeline in `examples/binary/config.yaml` reads from the `binary-test` consumer group and prints received messages to the debug exporter.

---

## Kubernetes deployment

The `examples/k8s/` folder contains production-ready manifests for two deployment patterns:

### Collector (DaemonSet)

One pod per node. Receives OTLP from workloads on the same node and forwards to Event Hub via Kafka.

```
examples/k8s/collector/
```

Key design choices:
- **DaemonSet** with `tolerations: - operator: Exists` — runs on every node including control-plane and tainted nodes.
- **Azure Workload Identity** — no SAS keys in the cluster; the pod acquires a token from the OIDC issuer.
- **Resource processor** with `action: upsert` — stamps `k8s.cluster.name`, `cloud.provider`, `cloud.region`, and `k8s.node.name` (from the downward API) onto every signal.
- **sending_queue** with `queue_size: 5000` and `retry_on_failure: max_elapsed_time: 10m` — buffers up to 5000 batches in memory during Event Hub outages.
- **PodDisruptionBudget** `maxUnavailable: 1` — allows rolling node drains without dropping a node's data.

### Gateway (Deployment)

Two replicas behind a Service. Reads from Event Hub and forwards downstream (e.g. to Elasticsearch or another OTLP endpoint).

```
examples/k8s/gateway/
```

Key design choices:
- **Deployment** with 2 replicas, `topologySpreadConstraints` across zones and nodes, and required `podAntiAffinity` — no two gateway pods on the same node.
- **Azure Workload Identity** — separate ServiceAccount and managed identity from the collector.
- **Resource processor** with `action: insert` — adds cluster metadata only if not already set (does not override values from the collector).
- **Kafka consumer group** `otel-gateway` — independent offset tracking from the collector's `$Default` group.
- **PodDisruptionBudget** `minAvailable: 1` — at least one gateway pod is always running during voluntary disruptions.

### Cluster info ConfigMap

Before deploying either component, create the shared `cluster-info` ConfigMap in each namespace. It injects cluster-level metadata into every signal:

```sh
# Edit cluster-info.yaml: fill in CLUSTER_NAME, AZURE_REGION, ENVIRONMENT
kubectl apply -f examples/k8s/cluster-info.yaml -n otel-collector
kubectl apply -f examples/k8s/cluster-info.yaml -n otel-gateway
```

### Deploying

```sh
# Collector
kubectl apply -f examples/k8s/collector/namespace.yaml
kubectl apply -f examples/k8s/collector/serviceaccount.yaml
kubectl apply -f examples/k8s/cluster-info.yaml -n otel-collector
kubectl apply -f examples/k8s/collector/env-configmap.yaml
kubectl apply -f examples/k8s/collector/configmap.yaml
kubectl apply -f examples/k8s/collector/service.yaml
kubectl apply -f examples/k8s/collector/deployment.yaml
kubectl apply -f examples/k8s/collector/pdb.yaml

# Gateway
kubectl apply -f examples/k8s/gateway/namespace.yaml
kubectl apply -f examples/k8s/gateway/serviceaccount.yaml
kubectl apply -f examples/k8s/cluster-info.yaml -n otel-gateway
kubectl apply -f examples/k8s/gateway/env-configmap.yaml
kubectl apply -f examples/k8s/gateway/configmap.yaml
kubectl apply -f examples/k8s/gateway/service.yaml
kubectl apply -f examples/k8s/gateway/deployment.yaml
kubectl apply -f examples/k8s/gateway/pdb.yaml
```

### Collector log levels

The collector's own internal logs are controlled by `service.telemetry.logs.level` in the config. This is separate from the telemetry data it processes.

| Level | When to use |
|---|---|
| `debug` | Development and troubleshooting — logs every component lifecycle event, pipeline startup, auth token fetch, and Kafka consumer group join |
| `info` | Staging — logs normal operational events; the `debug` exporter emits received telemetry at this level |
| `warn` | Production — logs only unexpected conditions; silent under normal operation |
| `error` | Production (minimal) — logs only failures |

To change the level on a running collector without redeployment:

```sh
# Patch the ConfigMap
kubectl patch configmap otel-collector-config -n otel-collector \
  --type=json \
  -p '[{"op":"replace","path":"/data/config.yaml","value":"..."}]'

# Then restart the DaemonSet to pick up the change
kubectl rollout restart daemonset/otel-collector -n otel-collector
```

> **Note:** `service.telemetry.logs.level` controls the collector process's own logs. The `debug` exporter's output verbosity is a separate setting under `exporters.debug.verbosity` (`basic`, `normal`, `detailed`) and is unaffected by the log level.

### Azure Workload Identity setup

1. Create a user-assigned managed identity and federate it with the AKS OIDC issuer:

   ```sh
   az identity create --name otel-collector-identity --resource-group <rg>

   az identity federated-credential create \
     --name otel-collector-federated \
     --identity-name otel-collector-identity \
     --resource-group <rg> \
     --issuer <aks-oidc-issuer-url> \
     --subject system:serviceaccount:otel-collector:otel-collector \
     --audience api://AzureADTokenExchange
   ```

2. Grant the identity the **Azure Event Hubs Data Sender** (collector) and **Azure Event Hubs Data Receiver** (gateway) roles on the namespace:

   ```sh
   NAMESPACE_ID=$(az eventhubs namespace show \
     --resource-group <rg> --name <namespace> --query id -o tsv)

   az role assignment create \
     --assignee <collector-identity-principal-id> \
     --role "Azure Event Hubs Data Sender" \
     --scope "$NAMESPACE_ID"

   az role assignment create \
     --assignee <gateway-identity-principal-id> \
     --role "Azure Event Hubs Data Receiver" \
     --scope "$NAMESPACE_ID"
   ```

3. Annotate the ServiceAccount in `examples/k8s/collector/serviceaccount.yaml`:

   ```yaml
   annotations:
     azure.workload.identity/client-id: <managed-identity-client-id>
   ```

---

## Azure DevOps pipeline

`azure-pipelines.yml` defines a three-job pipeline:

| Stage | Job | Agent | Output |
|---|---|---|---|
| Build | Linux (amd64) | `ubuntu-latest` | `OTelCollector` binary artifact |
| Build | Windows (amd64) | `windows-latest` | `OTelCollector.exe` binary artifact |
| Docker | Build & push | `ubuntu-latest` | Image pushed to ACR (main branch only) |

Linux and Windows build jobs run in parallel. The Docker stage depends on both and only pushes on the `main` branch.

Setup steps:

1. Create an Azure Container Registry if needed:
   ```sh
   az acr create --resource-group <rg> --name <registry> --sku Basic
   ```

2. Create a Docker Registry service connection in Azure DevOps (Project Settings → Service connections → Docker Registry → Azure Container Registry).

3. Set these pipeline variables in Azure DevOps:

   | Variable | Example |
   |---|---|
   | `CONTAINER_REGISTRY_SERVICE_CONNECTION` | `my-acr-connection` |
   | `CONTAINER_REGISTRY_LOGIN_SERVER` | `myregistry.azurecr.io` |

---

## Adding or removing components

All included components are declared in `manifest.yaml`. To add a new one:

1. Add the `gomod:` entry under the appropriate section in `manifest.yaml`.
2. Regenerate sources:

   ```sh
   make generate
   ```

3. Add the component to `collector-config.yaml` and rebuild.

> After `make generate`, OCB rewrites `go.mod` from scratch. The transitive version pins in the `replaces:` section of `manifest.yaml` are applied automatically during generation. The Makefile's `generate` target also re-applies two additional pins via `sed` (see the Dockerfile for the same logic used in container builds).

The eBPF profiling receiver (`ebpfprofilingreceiver`) is intentionally excluded — it requires Linux-only syscalls incompatible with macOS and Windows build environments.

---

## Component inventory

<details>
<summary>Extensions (16)</summary>

| Component ID | Source |
|---|---|
| `api_key` | `apikeyauthextension` (EDOT) |
| `apm_config` | `apmconfigextension` (EDOT) |
| `aws_logs_encoding` | `awslogsencodingextension` |
| `azure_encoding` | `azureencodingextension` |
| `azure_auth` | `azureauthextension` |
| `bearer_token_auth` | `bearertokenauthextension` |
| `cgroup_runtime` | `cgroupruntimeextension` |
| `file_storage` | `filestorage` |
| `headers_setter` | `headerssetterextension` |
| `health_check` | `healthcheckextension` |
| `health_check/v2` | `healthcheckv2extension` |
| `k8s_leader_elector` | `k8sleaderelector` |
| `k8s_observer` | `k8sobserver` |
| `memory_limiter` | `memorylimiterextension` |
| `opamp` | `opampextension` |
| `pprof` | `pprofextension` |

</details>

<details>
<summary>Receivers (39)</summary>

Includes `otlp`, `azure_event_hub`, `elastic_apm_intake`, `hostmetrics`, `filelog`, `prometheus`, `jaeger`, `zipkin`, `k8s_cluster`, `kubeletstats`, `kafka`, `statsd`, `sqlserver`, `windowseventlog`, `windowsperfcounters`, and more. See `manifest.yaml` for the full list.

</details>

<details>
<summary>Processors (14)</summary>

| Component ID | Source |
|---|---|
| `attributes` | `attributesprocessor` |
| `batch` | `batchprocessor` |
| `cumulativetodelta` | `cumulativetodeltaprocessor` |
| `elastic_apm` | `elasticapmprocessor` (EDOT) |
| `filter` | `filterprocessor` |
| `geoip` | `geoipprocessor` |
| `k8sattributes` | `k8sattributesprocessor` |
| `logdedup` | `logdedupprocessor` |
| `memory_limiter` | `memorylimiterprocessor` |
| `rate_limit` | `ratelimitprocessor` (EDOT) |
| `resource` | `resourceprocessor` |
| `resourcedetection` | `resourcedetectionprocessor` |
| `tail_sampling` | `tailsamplingprocessor` |
| `transform` | `transformprocessor` |

</details>

<details>
<summary>Exporters (9)</summary>

| Component ID | Source |
|---|---|
| `azure_event_hub` | `azureeventhubexporter` |
| `debug` | `debugexporter` |
| `elasticsearch` | `elasticsearchexporter` |
| `file` | `fileexporter` |
| `kafka` | `kafkaexporter` |
| `loadbalancing` | `loadbalancingexporter` |
| `nop` | `nopexporter` |
| `otlp` | `otlpexporter` |
| `otlphttp` | `otlphttpexporter` |

</details>

<details>
<summary>Connectors (6)</summary>

`elastic_apm`, `forward`, `otlpjson`, `profiling_metrics` (EDOT), `routing`, `spanmetrics`

</details>
