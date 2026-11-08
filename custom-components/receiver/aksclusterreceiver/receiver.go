package aksclusterreceiver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"
)

const scopeName = "github.com/ssijbabu/OTelCollector/custom-components/receiver/aksclusterreceiver"

type aksReceiver struct {
	cfg     *Config
	set     receiver.Settings
	metrics consumer.Metrics
	logs    consumer.Logs
	client  *armClient
	httpDo  *http.Client
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func newReceiver(set receiver.Settings, cfg *Config) *aksReceiver {
	return &aksReceiver{
		cfg:    cfg,
		set:    set,
		httpDo: &http.Client{},
	}
}

func (r *aksReceiver) Start(_ context.Context, host component.Host) error {
	cred, err := r.credential(host)
	if err != nil {
		return err
	}
	r.client = &armClient{http: r.httpDo, cred: cred, cfg: r.cfg}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.wg.Add(1)
	go r.run(ctx)
	return nil
}

func (r *aksReceiver) Shutdown(context.Context) error {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
	return nil
}

// credential returns the token source provided by the configured azure_auth extension.
func (r *aksReceiver) credential(host component.Host) (azcore.TokenCredential, error) {
	if r.cfg.Auth == nil {
		return nil, errors.New("auth is required: set it to the ID of an azure_auth extension")
	}
	ext, ok := host.GetExtensions()[*r.cfg.Auth]
	if !ok {
		return nil, fmt.Errorf("auth extension %q not found; add it to service.extensions", r.cfg.Auth)
	}
	cred, ok := ext.(azcore.TokenCredential)
	if !ok {
		return nil, fmt.Errorf("extension %q does not provide Azure tokens; use azure_auth", r.cfg.Auth)
	}
	return cred, nil
}

func (r *aksReceiver) run(ctx context.Context) {
	defer r.wg.Done()
	select {
	case <-ctx.Done():
		return
	case <-time.After(r.cfg.InitialDelay):
	}
	ticker := time.NewTicker(r.cfg.CollectionInterval)
	defer ticker.Stop()
	for {
		r.collect(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *aksReceiver) collect(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	logger := r.set.Logger
	clusters, err := r.client.listClusters(ctx)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return
		}
		logger.Error("Failed to read AKS clusters", zap.Error(err))
		// Partial results from earlier pages are still emitted.
	}
	if len(clusters) == 0 {
		return
	}
	now := pcommon.NewTimestampFromTime(time.Now())

	if r.metrics != nil {
		if err := r.metrics.ConsumeMetrics(ctx, buildMetrics(clusters, r.cfg.SubscriptionID, now)); err != nil {
			logger.Error("Failed to consume AKS metrics", zap.Error(err))
		}
	}
	if r.logs != nil {
		if err := r.logs.ConsumeLogs(ctx, buildLogs(clusters, r.cfg.SubscriptionID, now)); err != nil {
			logger.Error("Failed to consume AKS logs", zap.Error(err))
		}
	}
}

// resourceGroupFromID extracts the resource group name from an Azure resource ID.
func resourceGroupFromID(id string) string {
	parts := strings.Split(strings.Trim(id, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if strings.EqualFold(parts[i], "resourceGroups") {
			return parts[i+1]
		}
	}
	return ""
}

func putResource(res pcommon.Resource, mc *managedCluster, subscriptionID string) {
	a := res.Attributes()
	a.PutStr("cloud.provider", "azure")
	a.PutStr("cloud.platform", "azure.aks")
	a.PutStr("cloud.account.id", subscriptionID)
	a.PutStr("cloud.region", mc.Location)
	a.PutStr("cloud.resource_id", mc.ID)
	if rg := resourceGroupFromID(mc.ID); rg != "" {
		a.PutStr("azure.resource_group.name", rg)
	}
	a.PutStr("k8s.cluster.name", mc.Name)
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// gauges groups data points by metric name within one scope.
type gauges struct {
	sm  pmetric.ScopeMetrics
	idx map[string]pmetric.Metric
	ts  pcommon.Timestamp
}

func (g *gauges) point(name, unit, desc string, v int64) pcommon.Map {
	m, ok := g.idx[name]
	if !ok {
		m = g.sm.Metrics().AppendEmpty()
		m.SetName(name)
		m.SetUnit(unit)
		m.SetDescription(desc)
		m.SetEmptyGauge()
		g.idx[name] = m
	}
	dp := m.Gauge().DataPoints().AppendEmpty()
	dp.SetTimestamp(g.ts)
	dp.SetIntValue(v)
	return dp.Attributes()
}

func buildMetrics(clusters []managedCluster, subscriptionID string, ts pcommon.Timestamp) pmetric.Metrics {
	md := pmetric.NewMetrics()
	for i := range clusters {
		mc := &clusters[i]
		p := &mc.Properties
		rm := md.ResourceMetrics().AppendEmpty()
		putResource(rm.Resource(), mc, subscriptionID)
		sm := rm.ScopeMetrics().AppendEmpty()
		sm.Scope().SetName(scopeName)
		g := &gauges{sm: sm, idx: map[string]pmetric.Metric{}, ts: ts}

		g.point("aks.cluster.up", "1",
			"1 when the cluster's power state is Running, otherwise 0.",
			boolToInt(p.PowerState.Code == "Running"))
		g.point("aks.cluster.provisioning.succeeded", "1",
			"1 when the cluster's provisioning state is Succeeded, otherwise 0.",
			boolToInt(p.ProvisioningState == "Succeeded"))

		info := g.point("aks.cluster.info", "1",
			"Always 1; the cluster's configuration is in the attributes.", 1)
		info.PutStr("aks.kubernetes.version", p.CurrentKubernetesVersion)
		info.PutStr("aks.kubernetes.version.requested", p.KubernetesVersion)
		info.PutStr("aks.power_state", p.PowerState.Code)
		info.PutStr("aks.provisioning_state", p.ProvisioningState)
		info.PutStr("aks.sku.tier", mc.SKU.Tier)
		info.PutStr("aks.support_plan", p.SupportPlan)
		info.PutStr("aks.node_resource_group", p.NodeResourceGroup)
		info.PutStr("aks.network.plugin", p.NetworkProfile.NetworkPlugin)
		info.PutStr("aks.network.plugin_mode", p.NetworkProfile.NetworkPluginMode)
		info.PutStr("aks.network.policy", p.NetworkProfile.NetworkPolicy)
		info.PutStr("aks.network.outbound_type", p.NetworkProfile.OutboundType)
		info.PutStr("aks.upgrade_channel", p.AutoUpgradeProfile.UpgradeChannel)
		info.PutStr("aks.node_os_upgrade_channel", p.AutoUpgradeProfile.NodeOSUpgradeChannel)
		info.PutBool("aks.workload_identity.enabled", p.SecurityProfile.WorkloadIdentity.Enabled)
		info.PutBool("aks.oidc_issuer.enabled", p.OIDCIssuerProfile.Enabled)

		g.point("aks.node_pool.count", "{node_pool}",
			"Number of node pools in the cluster.", int64(len(p.AgentPoolProfiles)))

		for _, ap := range p.AgentPoolProfiles {
			if ap.Count != nil {
				a := g.point("aks.node_pool.node.count", "{node}",
					"Configured node count of the node pool. Nodes are not running while the pool or cluster is stopped.", *ap.Count)
				a.PutStr("aks.node_pool.name", ap.Name)
				a.PutStr("aks.node_pool.mode", ap.Mode)
				a.PutStr("aks.node_pool.vm_size", ap.VMSize)
				a.PutStr("aks.node_pool.os_type", ap.OSType)
				a.PutStr("aks.node_pool.os_sku", ap.OSSKU)
				a.PutStr("aks.node_pool.power_state", ap.PowerState.Code)
				a.PutStr("aks.node_pool.provisioning_state", ap.ProvisioningState)
				a.PutStr("aks.node_pool.kubernetes.version", ap.CurrentOrchestratorVersion)
				a.PutStr("aks.node_pool.node_image.version", ap.NodeImageVersion)
			}
			g.point("aks.node_pool.up", "1",
				"1 when the node pool's power state is Running, otherwise 0.",
				boolToInt(ap.PowerState.Code == "Running")).PutStr("aks.node_pool.name", ap.Name)
			if ap.MaxPods != nil {
				g.point("aks.node_pool.pods.max", "{pod}",
					"Maximum pods per node in the node pool.", *ap.MaxPods).PutStr("aks.node_pool.name", ap.Name)
			}
			g.point("aks.node_pool.autoscaling.enabled", "1",
				"1 when the cluster autoscaler is enabled for the node pool.",
				boolToInt(ap.EnableAutoScaling)).PutStr("aks.node_pool.name", ap.Name)
			if ap.EnableAutoScaling && ap.MinCount != nil {
				g.point("aks.node_pool.autoscaling.min", "{node}",
					"Autoscaler minimum node count.", *ap.MinCount).PutStr("aks.node_pool.name", ap.Name)
			}
			if ap.EnableAutoScaling && ap.MaxCount != nil {
				g.point("aks.node_pool.autoscaling.max", "{node}",
					"Autoscaler maximum node count.", *ap.MaxCount).PutStr("aks.node_pool.name", ap.Name)
			}
		}
	}
	return md
}

// buildLogs emits one log record per cluster whose body is the full
// managedClusters resource document as returned by Azure Resource Manager.
func buildLogs(clusters []managedCluster, subscriptionID string, ts pcommon.Timestamp) plog.Logs {
	ld := plog.NewLogs()
	for i := range clusters {
		mc := &clusters[i]
		rl := ld.ResourceLogs().AppendEmpty()
		putResource(rl.Resource(), mc, subscriptionID)
		sl := rl.ScopeLogs().AppendEmpty()
		sl.Scope().SetName(scopeName)
		lr := sl.LogRecords().AppendEmpty()
		lr.SetTimestamp(ts)
		lr.SetObservedTimestamp(ts)
		lr.SetEventName("aks.cluster.snapshot")
		lr.SetSeverityNumber(plog.SeverityNumberInfo)
		lr.SetSeverityText("INFO")
		lr.Attributes().PutStr("aks.power_state", mc.Properties.PowerState.Code)
		lr.Attributes().PutStr("aks.provisioning_state", mc.Properties.ProvisioningState)
		if err := lr.Body().SetEmptyMap().FromRaw(mc.Raw); err != nil {
			lr.Body().SetStr(fmt.Sprintf("%v", mc.Raw))
		}
	}
	return ld
}
