# Component examples

For every processor, connector, extension and provider in this build (`manifest.yaml`)
there are two files (the custom receivers' guides are their own READMEs under [`custom-components/`](../../custom-components/)):

- **`<name>.md`:** a guide from the Kubernetes perspective: the use case, whether to run it
  on the agent DaemonSet or the gateway Deployment, the RBAC, volumes and env it needs,
  manifest snippets, and pitfalls.
- **`<name>.example.yaml`:** a complete, runnable collector configuration for that scenario.

Each example config:

- describes a real-world scenario in its header comment, with the environment variables
  and Kubernetes permissions it needs
- is a full config (receivers, processors, exporters, service), not a fragment
- passes `otelcol validate` against the `otelcol` binary built from `manifest.yaml`

Most examples receive OTLP and write to Elasticsearch, so they need:

```sh
export ELASTICSEARCH_ENDPOINT=https://<deployment>.es.<region>.azure.elastic-cloud.com:443
export ELASTICSEARCH_API_KEY=<base64-api-key>
./otelcol --config doc/processors/batch.example.yaml
```

Check a config without starting it:

```sh
./otelcol validate --config doc/processors/batch.example.yaml
```

> `validate` checks the configuration and creates every component, but does not connect to
> anything. Examples that use Kubernetes components must run in a cluster (or with a
> kubeconfig); `geoip` needs a MaxMind `.mmdb` file; `profilingmetrics` needs
> `--feature-gates=service.profilesSupport`.

## Receivers

| Example | Scenario |
|---|---|
| [aks](../../custom-components/receiver/aksclusterreceiver/README.md) · [config](receivers/aks.example.yaml) | Custom receiver: state of every AKS cluster in a subscription (stopped, version, node pools) from Azure Resource Manager |
| [azure_activity_log](../../custom-components/receiver/azureactivitylogreceiver/README.md) · [config](receivers/azure_activity_log.example.yaml) | Custom receiver: audit trail of control-plane changes (who changed/stopped/deleted what) from the subscription activity log |

## Processors

| Example | Scenario |
|---|---|
| [attributes](processors/attributes.md) · [config](processors/attributes.example.yaml) | Scrub credentials, hash user IDs, extract tenant from URL path |
| [batch](processors/batch.md) · [config](processors/batch.example.yaml) | Large batches for Elasticsearch bulk requests, split per tenant |
| [cumulative_to_delta](processors/cumulative_to_delta.md) · [config](processors/cumulative_to_delta.example.yaml) | Convert cumulative HTTP/RPC counters to per-interval deltas |
| [elasticapm](processors/elasticapm.md) · [config](processors/elasticapm.example.yaml) | Enrich traces and logs for Kibana APM (with the elasticapm connector) |
| [elasticinframetrics](processors/elasticinframetrics.md) · [config](processors/elasticinframetrics.example.yaml) | Derive Elastic Kubernetes metrics from kubelet stats for Kibana Infrastructure |
| [elasticmetadata](processors/elasticmetadata.md) · [config](processors/elasticmetadata.example.yaml) | Copy sender headers (tenant, region) into log documents |
| [elastictrace](processors/elastictrace.md) · [config](processors/elastictrace.example.yaml) | Deprecated — migration example, use elasticapm instead |
| [filter](processors/filter.md) · [config](processors/filter.example.yaml) | Drop health-check spans, debug logs in production and unused metrics |
| [geoip](processors/geoip.md) · [config](processors/geoip.example.yaml) | Add country/city/location from client IPs |
| [k8s_attributes](processors/k8s_attributes.md) · [config](processors/k8s_attributes.example.yaml) | Tag data with pod, namespace, workload, node, labels |
| [log_dedup](processors/log_dedup.md) · [config](processors/log_dedup.example.yaml) | Collapse repeated container log lines into one with a count |
| [memory_limiter](processors/memory_limiter.md) · [config](processors/memory_limiter.example.yaml) | Refuse data before the pod is OOM-killed |
| [ratelimit](processors/ratelimit.md) · [config](processors/ratelimit.example.yaml) | Per-tenant request budget on a shared gateway |
| [resource](processors/resource.md) · [config](processors/resource.example.yaml) | Stamp cluster, region, environment on every signal |
| [resource_detection](processors/resource_detection.md) · [config](processors/resource_detection.example.yaml) | Auto-detect host, Azure VM and AKS attributes |
| [tail_sampling](processors/tail_sampling.md) · [config](processors/tail_sampling.example.yaml) | Keep errors, slow traces and checkout; sample 5% of the rest |
| [transform](processors/transform.md) · [config](processors/transform.example.yaml) | Parse JSON logs, set severity, normalise span names, rename metrics |

## Connectors

| Example | Scenario |
|---|---|
| [elasticapm](connectors/elasticapm.md) · [config](connectors/elasticapm.example.yaml) | Aggregated APM metrics (latency, throughput, errors) for Kibana APM |
| [forward](connectors/forward.md) · [config](connectors/forward.example.yaml) | Container logs and OTLP logs processed separately, exported through one shared pipeline |
| [otlp_json](connectors/otlp_json.md) · [config](connectors/otlp_json.example.yaml) | Re-create traces/metrics/logs from OTLP JSON files |
| [profilingmetrics](connectors/profilingmetrics.md) · [config](connectors/profilingmetrics.example.yaml) | CPU-usage-by-runtime metrics from profiles |
| [routing](connectors/routing.md) · [config](connectors/routing.example.yaml) | Payments namespace to a separate cluster, audit logs to their own data stream |
| [span_metrics](connectors/span_metrics.md) · [config](connectors/span_metrics.example.yaml) | RED metrics from all spans, before tail sampling |

## Extensions

| Example | Scenario |
|---|---|
| [apikeyauth](extensions/apikeyauth.md) · [config](extensions/apikeyauth.example.yaml) | Only accept senders with a valid Elasticsearch API key |
| [apmconfig](extensions/apmconfig.md) · [config](extensions/apmconfig.example.yaml) | Serve Kibana-managed APM agent configuration over OpAMP |
| [azure_auth](extensions/azure_auth.md) · [config](extensions/azure_auth.example.yaml) | Read from Event Hubs (Kafka) with Workload Identity, no secrets |
| [azure_encoding](extensions/azure_encoding.md) · [config](extensions/azure_encoding.example.yaml) | Decode Azure diagnostic logs/metrics streamed to Event Hubs |
| [beats_encoding](extensions/beats_encoding.md) · [config](extensions/beats_encoding.example.yaml) | Azure activity logs from Event Hubs in Elastic integration format |
| [cgroup_runtime](extensions/cgroup_runtime.md) · [config](extensions/cgroup_runtime.example.yaml) | Respect the pod's CPU and memory limits |
| [file_storage](extensions/file_storage.md) · [config](extensions/file_storage.example.yaml) | Persistent log offsets and on-disk export queue |
| [headers_setter](extensions/headers_setter.md) · [config](extensions/headers_setter.example.yaml) | Forward to a multi-tenant backend with per-tenant headers |
| [health_check](extensions/health_check.md) · [config](extensions/health_check.example.yaml) | Liveness/readiness endpoint for Kubernetes probes |
| [healthcheckv2](extensions/healthcheckv2.md) · [config](extensions/healthcheckv2.example.yaml) | Per-pipeline/component health over HTTP and gRPC |
| [k8s_leader_elector](extensions/k8s_leader_elector.md) · [config](extensions/k8s_leader_elector.example.yaml) | Run k8s_cluster in one replica only, with failover |
| [k8s_observer](extensions/k8s_observer.md) · [config](extensions/k8s_observer.example.yaml) | Start a kubelet_stats receiver for every node automatically |
| [memory_limiter](extensions/memory_limiter.md) · [config](extensions/memory_limiter.example.yaml) | Reject requests at the HTTP/gRPC layer when memory is high |
| [opamp](extensions/opamp.md) · [config](extensions/opamp.example.yaml) | Register the collector with an OpAMP fleet manager |
| [pprof](extensions/pprof.md) · [config](extensions/pprof.example.yaml) | Profile the collector's own CPU and memory use |

## Providers

| Example | Scenario |
|---|---|
| [env](providers/env.md) · [config](providers/env.example.yaml) | One config for every environment; values and defaults from env vars |
| [file](providers/file.md) · [config](providers/file.example.yaml) | API key read from a mounted Kubernetes Secret file |
| [yaml](providers/yaml.md) · [config](providers/yaml.example.yaml) | Inline command-line overrides for incident debugging |
