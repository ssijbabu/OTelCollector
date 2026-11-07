# Hubble network flows (AKS, Azure CNI Powered by Cilium) → OpenTelemetry Collector

On AKS with **Azure CNI Powered by Cilium**, Cilium and Hubble are managed by AKS: the
`cilium-config` ConfigMap, the Cilium DaemonSet and Hubble Relay are reconciled by the
platform, so Cilium's own Helm values (`hubble.export.*`) can't be set. Changes made by hand
are reverted.

AKS exposes flow export through **Advanced Container Networking Services (ACNS) → container
network logs** instead. You select traffic with a `ContainerNetworkLog` custom resource, and
the Cilium agent on each node writes the matching flows as JSON to
**`/var/log/acns/hubble/events.log`**. The collector agent on each node reads that file.

```
ContainerNetworkLog CR (what to log)
        │
Cilium agent (AKS-managed, every node) ──► /var/log/acns/hubble/events.log (JSON, rotates at 50 MB)
                                                    │
                     otel-collector agent (DaemonSet) file_log/hubble ──► gateway ──► Elasticsearch
```

### Why not Hubble Relay?

Hubble Relay is a gRPC **server** (`observer.Observer/GetFlows`) that the `hubble` CLI and
Hubble UI stream from. It doesn't push anywhere and doesn't speak OTLP, so the collector
can't receive from it. Use Relay for live troubleshooting (`hubble observe`) and the node
files for continuous export: that path needs no extra component, scales with the nodes, and
keeps working when Relay is down.

---

## 1. Prerequisites

| Requirement | Check |
|---|---|
| Cilium data plane | `az aks show -g <rg> -n <cluster> --query networkProfile.networkDataplane -o tsv` → `cilium` |
| Kubernetes 1.36 or later | `az aks show ... --query currentKubernetesVersion -o tsv` |
| Azure CLI 2.85.0+, `aks-preview` 22.0.0b15+ | `az version`; `az extension add --name aks-preview` / `az extension update --name aks-preview` |
| Permission to create cluster-scoped `ContainerNetworkLog` resources | `kubectl auth can-i create containernetworklogs.acn.azure.com` |

ACNS is a **paid** add-on, charged per node.

> The clusters in this repo's examples (`ocm-hub`, `ocm-spoke`) report
> `networkDataplane: azure`, so stored flow logs aren't available there. Azure CNI clusters
> can be moved to the Cilium data plane with
> `az aks update -g <rg> -n <cluster> --network-dataplane cilium`. This is a **one-way**
> change that reimages the nodes; plan it like an upgrade.

---

## 2. Enable ACNS

```sh
RESOURCE_GROUP=<rg>
CLUSTER_NAME=<cluster>

az aks update --name $CLUSTER_NAME --resource-group $RESOURCE_GROUP --enable-acns
az aks get-credentials --name $CLUSTER_NAME --resource-group $RESOURCE_GROUP

# both should be true
az aks show -g $RESOURCE_GROUP -n $CLUSTER_NAME \
  --query "networkProfile.advancedNetworking.{enabled:enabled, observability:observability.enabled}"
```

Enabling ACNS turns on stored-log mode, but **nothing is written until a
`ContainerNetworkLog` exists**.

### With Bicep

ACNS is part of the cluster resource: `properties.networkProfile.advancedNetworking`.
[`otelcollector/examples/bicep/aks-acns.bicep`](otelcollector/examples/bicep/aks-acns.bicep)
is a complete cluster with the Cilium data plane, ACNS and Workload Identity:

```sh
az deployment group create -g <rg> -f otelcollector/examples/bicep/aks-acns.bicep \
  -p clusterName=<name> advancedNetworkPolicies=FQDN
```

The part that enables ACNS:

```bicep
resource aks 'Microsoft.ContainerService/managedClusters@2026-06-01' = {
  // ...
  properties: {
    networkProfile: {
      networkPlugin: 'azure'
      networkPluginMode: 'overlay'
      networkDataplane: 'cilium'          // required for stored flow logs and ACNS security
      networkPolicy: 'cilium'
      advancedNetworking: {
        enabled: true
        observability: {
          enabled: true                   // metrics, Hubble, container network logs
        }
        security: {
          enabled: true                   // Cilium FQDN / L7 policies
          advancedNetworkPolicies: 'FQDN' // 'None' | 'FQDN' | 'L7' (L7 for HTTP details in flows)
        }
      }
    }
  }
}
```

**Existing cluster deployed with Bicep:** add the `advancedNetworking` block to the cluster's
existing template and redeploy. Don't deploy a template that contains only
`advancedNetworking`: a deployment sends the whole cluster definition, so properties
missing from the template can be reset. If the cluster wasn't created with Bicep, use
`az aks update --enable-acns` instead, or start from the cluster's exported template
(portal → cluster → **Export template**).

Preview a change before applying it:

```sh
az deployment group what-if -g <rg> -f otelcollector/examples/bicep/aks-acns.bicep -p clusterName=<name>
```

**Optional, Azure Monitor forwarding** (not needed for the OpenTelemetry path):

```bicep
azureMonitorProfile: {
  containerInsights: {
    enabled: true
    logAnalyticsWorkspaceResourceId: workspaceId
    containerNetworkLogs: 'Enabled'
  }
}
```

The `ContainerNetworkLog` resource in the next step is a Kubernetes object, not an Azure
resource, so apply it with `kubectl` (or your GitOps tool), not with Bicep.

You don't need the Azure Monitor add-on or `--enable-container-network-logs` for this setup:
those forward the same files to Log Analytics. The OpenTelemetry collector replaces that
step (or runs alongside it).

---

## 3. Select the traffic to log

`ContainerNetworkLog` is cluster-scoped. Each entry in `includefilters` is an alternative
(OR); within an entry, every criterion must match.

```yaml
# containernetworklog.yaml
apiVersion: acn.azure.com/v1alpha1
kind: ContainerNetworkLog
metadata:
  name: otel-flows
spec:
  includefilters:
    # Everything leaving or entering the application namespace
    - name: shop-egress
      from:
        namespacedPod: ["shop/"]          # "namespace/" = every pod in the namespace
      protocol: [tcp, udp, dns]
      verdict: [forwarded, dropped]
    - name: shop-ingress
      to:
        namespacedPod: ["shop/"]
      protocol: [tcp, udp, dns]
      verdict: [forwarded, dropped]
    # Every dropped flow in the cluster (policy denials, etc.)
    - name: all-drops
      from:
        ip: ["0.0.0.0/0"]
      verdict: [dropped]
  # Optional: keep extra detail in the 30-second aggregated records (costs volume)
  # aggregation:
  #   additionalFields:
  #     - IP.destination        # individual destination IPs
  #     - traffic_direction     # separate ingress/egress records
```

```sh
kubectl apply --dry-run=server -f containernetworklog.yaml
kubectl apply -f containernetworklog.yaml
kubectl describe containernetworklog otel-flows    # Status > State must be CONFIGURED
```

| Field | Values |
|---|---|
| `from` / `to` | `namespacedPod` (`namespace/pod-prefix`, or `namespace/`), `labelSelector` (use **one** `matchLabels` or one `matchExpressions` condition per endpoint), `ip` (IP or CIDR), `identities` (numeric Cilium identities, e.g. `2` world) |
| `protocol` | `tcp`, `udp`, `dns` |
| `verdict` | `forwarded`, `dropped` |
| `aggregation.additionalFields` | extra Hubble `Flow` field paths kept when flows are aggregated; applies **cluster-wide** |

**What's in the records.** By default, flows are **aggregated every 30 seconds** per node:
flows with the same verdict, namespaces, workloads, identities, destination port, drop
reason and response codes become one record with ingress/egress counts. Pod names, IP
addresses, source ports, DNS names and URLs are **not** kept unless you add them under
`aggregation.additionalFields`.

**DNS and HTTP details** only appear when the traffic is inspected: DNS needs a Cilium FQDN
policy, HTTP needs Layer 7 policy support. A `ContainerNetworkLog` alone doesn't turn
inspection on.

### Check the file on a node

```sh
NODE=$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')
kubectl debug node/$NODE -it --image=busybox -- sh -c 'ls -l /host/var/log/acns/hubble/; tail -n 2 /host/var/log/acns/hubble/events.log'
kubectl get pods -o name | grep node-debugger | xargs kubectl delete    # clean up
```

Look at a line before relying on specific field names downstream: the collector config
below doesn't depend on them, but your Kibana queries and dashboards will.

---

## 4. Configure the collector agent

Add this to the agent DaemonSet config ([`k8s/collector/configmap.yaml`](otelcollector/examples/k8s/collector/configmap.yaml)).
It reuses the agent's processors and `otlp_grpc/gateway` exporter.

```yaml
extensions:
  file_storage:
    directory: /var/lib/otelcol/file_storage   # file offsets survive agent restarts

receivers:
  file_log/hubble:
    include:
      - /var/log/acns/hubble/events*.log       # current file and rotated ones
    start_at: end
    storage: file_storage
    operators:
      # Skip anything that isn't a JSON object (partial or corrupt lines)
      - type: filter
        expr: 'not (body matches "^\\s*\\{")'
      - type: json_parser
        parse_from: body
        parse_to: attributes["hubble"]
      # Use the flow record's time when present
      - type: time_parser
        if: 'attributes.hubble?.time != nil'
        parse_from: attributes["hubble"]["time"]
        layout_type: gotime
        layout: "2006-01-02T15:04:05.999999999Z07:00"
      # FORWARDED -> INFO, DROPPED -> WARN, ERROR -> ERROR
      - type: severity_parser
        if: 'attributes.hubble?.flow?.verdict != nil'
        parse_from: attributes["hubble"]["flow"]["verdict"]
        mapping:
          info: [forwarded, redirected, traced, translated]
          warn: [dropped, audit]
          error: error
      # The parsed record becomes the body, indexed as structured fields
      - type: move
        from: attributes["hubble"]
        to: body
      - type: add
        field: attributes["event.name"]
        value: hubble.flow

processors:
  resource/node:
    attributes:
      - key: k8s.node.name
        value: ${env:K8S_NODE_NAME}            # already set in the agent DaemonSet
        action: upsert

service:
  extensions: [health_check, file_storage]     # keep the agent's existing extensions
  pipelines:
    logs/hubble:
      receivers: [file_log/hubble]
      processors: [memory_limiter, resource/cluster, resource/node, resource_detection, batch]
      exporters: [otlp_grpc/gateway]
```

### DaemonSet changes ([`k8s/collector/deployment.yaml`](otelcollector/examples/k8s/collector/deployment.yaml))

```yaml
spec:
  template:
    spec:
      securityContext:
        runAsUser: 0                 # the ACNS log is owned by root on the node
        runAsGroup: 0
        seccompProfile: {type: RuntimeDefault}
      containers:
        - name: otel-collector
          securityContext:
            runAsNonRoot: false
            runAsUser: 0
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities: {drop: [ALL]}
          volumeMounts:
            - {name: acns-hubble, mountPath: /var/log/acns/hubble, readOnly: true}
            - {name: otelcol-state, mountPath: /var/lib/otelcol}
      volumes:
        - name: acns-hubble
          hostPath: {path: /var/log/acns/hubble, type: DirectoryOrCreate}
        - name: otelcol-state
          hostPath: {path: /var/lib/otelcol, type: DirectoryOrCreate}
```

`DirectoryOrCreate` lets the agent start on nodes where nothing has been logged yet.

If you also use the [Falco guide](falco.md), both receivers share the same `file_storage`
extension, `otelcol-state` volume and root security context.

---

## 5. What arrives in Elasticsearch

| Field | Value |
|---|---|
| `@timestamp` | the record's `time` (falls back to the time the line was read) |
| `severity_text` | the verdict: `FORWARDED`, `DROPPED`, `ERROR` |
| `body.structured.*` | the flow record: `flow.verdict`, `flow.source.namespace`, `flow.source.workloads`, `flow.destination.*`, `flow.l4.TCP.destination_port`, `flow.drop_reason_desc` …, plus the ingress/egress counts of aggregated records |
| `attributes.event.name` | `hubble.flow` |
| `resource.attributes.k8s.node.name` | the node that observed the flow |

Example KQL for policy drops: `attributes.event.name : "hubble.flow" and severity_text : "DROPPED"`.

---

## Notes

- **Retention on the node is short.** The file rotates at 50 MB and old data is overwritten.
  If the agent is down longer than one rotation, flows are lost. Keep the agent running
  (PodDisruptionBudget, resource requests) and filter narrowly.
- **Start narrow.** Begin with one namespace, check the volume in Elasticsearch, then widen.
  High-cardinality `additionalFields` (IPs, pod names, DNS names, URLs) multiply the record count.
- **Sensitive data.** DNS names and URLs, when you add them, can reveal internal
  architecture and personal data in query strings. Add them only when needed.
- **Turn it off:** `kubectl delete containernetworklog otel-flows` stops collection; already
  written files stay until they rotate.
- **Verified:** the collector configuration above was tested with this repo's `otelcol`
  binary against sample lines in Hubble's flow-export format (forwarded, dropped, a record
  without a timestamp, and a non-JSON line): JSON records were emitted with timestamps and
  verdict severities, and the non-JSON line was skipped. It wasn't tested against a live
  ACNS cluster, because the clusters available use the Azure data plane.

Sources:
[Container network logs overview](https://learn.microsoft.com/azure/aks/container-network-observability-logs) ·
[Set up container network logs](https://learn.microsoft.com/azure/aks/how-to-configure-container-network-logs)
