# OTelCollector

A custom build of the [Elastic Distribution of OpenTelemetry (EDOT) Collector](https://www.elastic.co/docs/reference/edot-collector/custom-collector), produced with the [OpenTelemetry Collector Builder (OCB)](https://opentelemetry.io/docs/collector/extend/ocb/).

Built on OTel Collector core **v1.70.0 / v0.164.0**, contrib **v0.164.1**, and EDOT components **v0.78–v0.79** — the same versions Elastic's `main` branch uses for EDOT. (Contrib v0.165.0 is out, but the EDOT components have not moved to it yet; mixing them breaks the build.)

The default build (`manifest.yaml`) is aimed at **Azure Event Hubs (Kafka) → Elasticsearch / OpenSearch** on Kubernetes: the Kafka, Elastic/EDOT, OpenSearch and Kubernetes components plus common processors, connectors and extensions. See [Component inventory](#component-inventory). Two other manifests are kept for reference only — see [Manifests](#manifests).

---

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| [Go](https://go.dev/dl/) | 1.27+ | Required for local binary builds |
| [Docker](https://docs.docker.com/get-docker/) | 24+ with BuildKit | Required for container builds |
| [OCB](https://opentelemetry.io/docs/collector/extend/ocb/) | 0.164.0 | Required only to regenerate sources from `manifest.yaml` |

Install OCB:

```sh
make install-ocb
# or directly:
go install go.opentelemetry.io/collector/cmd/builder@v0.164.0
```

---

## Repository structure

```
custom-components/           # Custom components, each its own Go module
└── receiver/
    ├── aksclusterreceiver/        # `aks` receiver: AKS cluster state from Azure Resource Manager
    └── azureactivitylogreceiver/  # `azure_activity_log` receiver: subscription activity log from Azure Resource Manager
falco.md                     # Falco alerts (JSON output) → file_log on the agent
hubble.md                    # AKS ACNS / Hubble flow logs → file_log on the agent (incl. Bicep)
otelcollector/
├── manifest.yaml            # OCB manifest used by make, Docker and the pipeline (Event Hubs/Kafka → Elasticsearch)
├── manifest-old.yaml        # Previous broad EDOT component set (reference only)
├── manifest-core.yaml       # Core-only component list (reference only)
├── collector-config.yaml    # Default runtime configuration (baked into the image)
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
├── doc/                     # One full example config per component, plus the custom aks receiver
│                            # (see doc/README.md)
└── examples/
    ├── connection.md        # Event Hubs with Workload Identity; agent→gateway mTLS with SPIFFE/SPIRE
    ├── bicep/aks-acns.bicep # AKS with Cilium data plane + ACNS (Bicep)
    ├── binary/
    │   └── config.yaml      # Local binary: OTLP → Elasticsearch + debug
    └── k8s/
        ├── collector/       # Agent DaemonSet: OTLP from workloads → gateway
        ├── gateway/         # Gateway Deployment: OTLP from agents → Elasticsearch
        └── telemetrygen/    # Job that sends test logs, metrics, and traces to the agent
```

The OCB-generated files (`main.go`, `components.go`, `go.mod`, `go.sum`) are committed so the binary can be built with plain `go build` without installing OCB.

---

## Quick start — local binary

All commands run from the `otelcollector/` directory.

```sh
make build          # produces ./otelcol
make run            # runs with collector-config.yaml (OTLP in, debug out)
```

Health check: `curl http://localhost:13133/`

Send test data from another terminal:

```sh
make install-telemetrygen   # once
make send-all               # or send-logs / send-metrics / send-traces
```

---

## Docker build and run

The Dockerfile runs OCB inside the build, so the generated files are always fresh. The build context is the **repo root**:

```sh
# From the repo root:
docker build -f otelcollector/Dockerfile --build-arg BINARY_NAME=otelcol -t otelcol:latest .

docker run --rm -p 4317:4317 -p 4318:4318 -p 13133:13133 otelcol:latest
```

Or from `otelcollector/`:

```sh
make docker-build
make docker-run
```

The image runs `collector-config.yaml` by default. Mount your own config to override it:

```sh
docker run --rm -v "$PWD/my-config.yaml:/etc/otelcol/config.yaml:ro" -p 4317:4317 otelcol:latest
```

| Port | Protocol | Purpose |
|---|---|---|
| 4317 | OTLP gRPC | Receive traces, metrics, logs |
| 4318 | OTLP HTTP | Receive traces, metrics, logs |
| 13133 | HTTP | Health check |

---

## Default configuration

`collector-config.yaml` sets up:

```
OTLP gRPC/HTTP (4317/4318)
        ↓
memory_limiter → resource_detection → batch
        ↓
debug exporter
```

An `elasticsearch` exporter block is included but commented out. To use it, set `ELASTICSEARCH_ENDPOINT` and `ELASTICSEARCH_API_KEY`, uncomment the block, and add `elasticsearch` to each pipeline's exporters.

---

## Binary example (`examples/binary/`)

A config that ships to Elasticsearch, using the EDOT `elasticapm` processor and connector so traces show up in Kibana's APM UI. Ports are offset from the defaults (14317, 14318, 13134) so they don't conflict with a port-forwarded in-cluster collector.

```sh
# From otelcollector/:
go build -o examples/binary/otelcol .

export ELASTICSEARCH_ENDPOINT=https://<deployment>.es.<region>.azure.elastic-cloud.com:443
export ELASTICSEARCH_API_KEY=<base64-api-key>

./examples/binary/otelcol --config examples/binary/config.yaml
```

```sh
make send-traces OTLP_ENDPOINT=localhost:14317
```

---

## Kubernetes deployment (`examples/k8s/`)

The standard EDOT agent → gateway layout:

```
workloads ──OTLP──► otel-collector (DaemonSet, one per node)
                          │ OTLP gRPC
                          ▼
                    otel-gateway (Deployment, 2 replicas)
                          │ elasticsearch exporter
                          ▼
                    Elasticsearch / Elastic Cloud
```

Before applying, replace the placeholders:

| File | Placeholder |
|---|---|
| `collector/deployment.yaml`, `gateway/deployment.yaml` | `<REGISTRY>` — your image registry |
| `collector/env-configmap.yaml`, `gateway/env-configmap.yaml` | `<CLUSTER_NAME>`, `<AZURE_REGION>`, `<ENVIRONMENT>` |
| `collector/networkpolicy.yaml`, `gateway/networkpolicy.yaml` | `<NODE_SUBNET_CIDR>` — node subnet for kubelet health probes |
| `gateway/secret.yaml` | `<ELASTICSEARCH_ENDPOINT>`, `<ELASTICSEARCH_API_KEY>` (or create the Secret with `kubectl create secret`, as shown in the file) |

### Collector (DaemonSet)

One pod per node. Receives OTLP from workloads and forwards it to the gateway.

- **DaemonSet** with `tolerations: - operator: Exists` — runs on every node, including tainted ones.
- **Resource processor** with `action: upsert` — stamps `k8s.cluster.name`, `cloud.*`, `deployment.environment.name`, and `k8s.node.name` (from the downward API) onto every signal.
- **sending_queue** (`queue_size: 5000`) and **retry_on_failure** (`max_elapsed_time: 10m`) — buffers data in memory while the gateway is unavailable.
- **PodDisruptionBudget** `maxUnavailable: 1` — allows rolling node drains.

Applications should send to their node's collector, e.g. `OTEL_EXPORTER_OTLP_ENDPOINT=http://$(NODE_IP):4317` with `NODE_IP` from `fieldRef: status.hostIP`, or to the `otel-collector.otel-collector.svc` Service.

### Gateway (Deployment)

Two replicas behind a ClusterIP Service. Receives OTLP from the agents and writes to Elasticsearch.

- **Topology spread** across zones and nodes, plus required `podAntiAffinity` — no two gateway pods on the same node.
- **Resource processor** with `action: insert` — adds cluster metadata only if the agent didn't already set it.
- **EDOT `elasticapm` processor and connector** — enrich traces and produce the aggregated APM metrics used by Kibana.
- **PodDisruptionBudget** `minAvailable: 1`.

> gRPC balances per connection, so long-lived agent connections can pin to one gateway pod. For even distribution at scale, use a headless Service with the `load_balancing` exporter on the agents.

### Deploying

```sh
cd otelcollector/examples/k8s

# Gateway first, so the agents have somewhere to send
kubectl apply -f gateway/namespace.yaml
kubectl apply -f gateway/

# Agents
kubectl apply -f collector/namespace.yaml
kubectl apply -f collector/

# Optional: send test telemetry
kubectl apply -f telemetrygen/job.yaml
```

### Collector log levels

`service.telemetry.logs.level` controls the collector's own logs (not the telemetry it processes):

| Level | When to use |
|---|---|
| `debug` | Troubleshooting — component lifecycle, pipeline startup, exporter connections |
| `info` | Default — normal operational events |
| `warn` | Production — only unexpected conditions |
| `error` | Production (minimal) — only failures |

After editing a ConfigMap, restart to pick up the change:

```sh
kubectl rollout restart daemonset/otel-collector -n otel-collector
kubectl rollout restart deployment/otel-gateway -n otel-gateway
```

---

## Azure DevOps pipeline

`azure-pipelines.yml` defines a three-job pipeline:

| Stage | Job | Agent | Output |
|---|---|---|---|
| Build | Linux (amd64) | `ubuntu-latest` | `otelcol` binary artifact |
| Build | Windows (amd64) | `windows-latest` | `otelcol.exe` binary artifact |
| Docker | Build & push | `ubuntu-latest` | Image pushed to ACR (main branch only) |

Linux and Windows builds run in parallel. The Docker stage depends on both and only pushes on `main`.

Setup:

1. Create an Azure Container Registry if needed:
   ```sh
   az acr create --resource-group <rg> --name <registry> --sku Basic
   ```

2. Create a Docker Registry service connection in Azure DevOps (Project Settings → Service connections → Docker Registry → Azure Container Registry).

3. Set these pipeline variables:

   | Variable | Example |
   |---|---|
   | `CONTAINER_REGISTRY_SERVICE_CONNECTION` | `my-acr-connection` |
   | `CONTAINER_REGISTRY_LOGIN_SERVER` | `myregistry.azurecr.io` |

---

## Adding or removing components

All components are declared in `manifest.yaml`. To change the set:

1. Add or remove the `gomod:` entry under the right section.
2. Regenerate sources:

   ```sh
   make generate
   ```

3. Reference the component in your config and rebuild.

> `make generate` rewrites `go.mod` from scratch. Keep every component on the same release train (core v0.164.0 / v1.70.0, contrib v0.164.1, and the EDOT component versions built against them). If OCB resolves an incompatible transitive version, pin it in a `replaces:` section of `manifest.yaml`.

### Upgrading

1. Check which collector version the latest EDOT components require — `go.opentelemetry.io/collector/component` in, for example, [elasticapmconnector's go.mod](https://github.com/elastic/opentelemetry-collector-components/blob/main/connector/elasticapmconnector/go.mod) — or follow [elastic-agent's `internal/edot/go.mod`](https://github.com/elastic/elastic-agent/blob/main/internal/edot/go.mod).
2. Bump the core, contrib, and EDOT entries in `manifest.yaml` plus `otelcol_version`, and the OCB version in the `Makefile`, `Dockerfile`, and this README.
3. `make generate && make build`.

Renaming the binary: change `dist.name` in `manifest.yaml` and run `make generate`. The Makefile, pipeline, and ignore files all derive the name from it.

## Manifests

| File | Purpose |
|---|---|
| `manifest.yaml` | The build. Used by `make generate`, the Dockerfile and the pipeline; produces the `otelcol` binary and image. |
| `manifest-old.yaml` | Previous broad EDOT component set, kept for reference only. Same layout as `manifest.yaml`, so `diff manifest-old.yaml manifest.yaml` shows exactly what differs. |
| `manifest-core.yaml` | Core-only component list, kept for reference only. |

## Component inventory

Component type names as reported by `./otelcol components`. Contrib v0.164 moved many types to snake_case (e.g. `resource_detection`, `otlp_grpc`); the old names (`resourcedetection`, `otlp`) still load as deprecated aliases.

| Kind | Count | Types |
|---|---|---|
| Extensions | 15 | `apikeyauth` (EDOT), `apmconfig` (EDOT), `azure_auth`, `azure_encoding`, `beats_encoding` (EDOT), `cgroup_runtime`, `file_storage`, `headers_setter`, `health_check`, `healthcheckv2`, `k8s_leader_elector`, `k8s_observer`, `memory_limiter`, `opamp`, `pprof` |
| Receivers | 12 | `aks` (custom, [this repo](custom-components/receiver/aksclusterreceiver/README.md)), `azure_activity_log` (custom, [this repo](custom-components/receiver/azureactivitylogreceiver/README.md)), `elasticapmintake` (EDOT), `file_log`, `k8s_cluster`, `k8s_events`, `k8s_objects`, `kafka`, `kafka_metrics`, `kubelet_stats`, `otlp`, `receiver_creator` |
| Processors | 17 | `attributes`, `batch`, `cumulative_to_delta`, `elasticapm` (EDOT), `elasticinframetrics` (EDOT), `elasticmetadata` (EDOT), `elastictrace` (EDOT, deprecated), `filter`, `geoip`, `k8s_attributes`, `log_dedup`, `memory_limiter`, `ratelimit` (EDOT), `resource`, `resource_detection`, `tail_sampling`, `transform` |
| Exporters | 6 | `debug`, `elasticsearch`, `kafka`, `opensearch`, `otlp_grpc`, `otlp_http` |
| Connectors | 6 | `elasticapm` (EDOT), `forward`, `otlp_json`, `profilingmetrics` (EDOT), `routing`, `span_metrics` |
