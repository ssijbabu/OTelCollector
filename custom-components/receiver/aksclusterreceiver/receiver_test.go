package aksclusterreceiver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

type fakeCred struct{ scopes atomic.Value }

func (f *fakeCred) GetToken(_ context.Context, o policy.TokenRequestOptions) (azcore.AccessToken, error) {
	f.scopes.Store(o.Scopes)
	return azcore.AccessToken{Token: "test-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

// fakeExt is an extension that also provides Azure tokens, like azure_auth.
type fakeExt struct {
	component.StartFunc
	component.ShutdownFunc
	fakeCred
}

type fakeHost struct {
	ext map[component.ID]component.Component
}

func (h fakeHost) GetExtensions() map[component.ID]component.Component { return h.ext }

func loadCluster(t *testing.T) json.RawMessage {
	b, err := os.ReadFile("testdata/cluster.json")
	require.NoError(t, err)
	return b
}

// armServer serves a two-page cluster list and a single-cluster GET.
func armServer(t *testing.T, cluster json.RawMessage) (*httptest.Server, *atomic.Int32) {
	var calls atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "2025-05-01", r.URL.Query().Get("api-version"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Query().Get("page") == "2":
			_ = json.NewEncoder(w).Encode(map[string]any{"value": []json.RawMessage{cluster}})
		case strings.HasSuffix(r.URL.Path, "/managedClusters"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"value":    []json.RawMessage{cluster},
				"nextLink": srv.URL + r.URL.Path + "?api-version=2025-05-01&page=2",
			})
		case strings.HasSuffix(r.URL.Path, "/managedClusters/example-aks"):
			_, _ = w.Write(cluster)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"ResourceNotFound","message":"not found"}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func testConfig(endpoint string) *Config {
	cfg := createDefaultConfig().(*Config)
	cfg.Endpoint = endpoint
	cfg.SubscriptionID = "00000000-0000-0000-0000-000000000000"
	cfg.InitialDelay = 0
	cfg.CollectionInterval = time.Minute
	authID := component.MustNewID("azure_auth")
	cfg.Auth = &authID
	return cfg
}

func TestListFollowsNextLink(t *testing.T) {
	srv, calls := armServer(t, loadCluster(t))
	cfg := testConfig(srv.URL)
	cred := &fakeCred{}
	c := &armClient{http: srv.Client(), cred: cred, cfg: cfg}

	clusters, err := c.listClusters(context.Background())
	require.NoError(t, err)
	assert.Len(t, clusters, 2)
	assert.Equal(t, int32(2), calls.Load())
	assert.Equal(t, []string{srv.URL + "/.default"}, cred.scopes.Load())
}

func TestSingleCluster(t *testing.T) {
	srv, _ := armServer(t, loadCluster(t))
	cfg := testConfig(srv.URL)
	cfg.ResourceGroup = "example-rg"
	cfg.ClusterName = "example-aks"
	c := &armClient{http: srv.Client(), cred: &fakeCred{}, cfg: cfg}

	clusters, err := c.listClusters(context.Background())
	require.NoError(t, err)
	require.Len(t, clusters, 1)
	assert.Equal(t, "example-aks", clusters[0].Name)
	assert.Equal(t, "Stopped", clusters[0].Properties.PowerState.Code)
}

func TestARMError(t *testing.T) {
	srv, _ := armServer(t, loadCluster(t))
	cfg := testConfig(srv.URL)
	cfg.ResourceGroup = "rg"
	cfg.ClusterName = "missing"
	c := &armClient{http: srv.Client(), cred: &fakeCred{}, cfg: cfg}

	_, err := c.listClusters(context.Background())
	require.ErrorContains(t, err, "404 ResourceNotFound: not found")
}

func findMetric(t *testing.T, rm pmetric.ResourceMetrics, name string) pmetric.NumberDataPointSlice {
	ms := rm.ScopeMetrics().At(0).Metrics()
	for i := 0; i < ms.Len(); i++ {
		if ms.At(i).Name() == name {
			return ms.At(i).Gauge().DataPoints()
		}
	}
	t.Fatalf("metric %s not found", name)
	return pmetric.NumberDataPointSlice{}
}

func attr(m pcommon.Map, k string) string {
	v, _ := m.Get(k)
	return v.AsString()
}

func TestBuildMetrics(t *testing.T) {
	mc, err := decodeCluster(loadCluster(t))
	require.NoError(t, err)
	md := buildMetrics([]managedCluster{mc}, "sub-1", pcommon.NewTimestampFromTime(time.Now()))
	require.Equal(t, 1, md.ResourceMetrics().Len())
	rm := md.ResourceMetrics().At(0)

	res := rm.Resource().Attributes()
	assert.Equal(t, "azure", attr(res, "cloud.provider"))
	assert.Equal(t, "azure.aks", attr(res, "cloud.platform"))
	assert.Equal(t, "eastus", attr(res, "cloud.region"))
	assert.Equal(t, "sub-1", attr(res, "cloud.account.id"))
	assert.Equal(t, "example-aks", attr(res, "k8s.cluster.name"))
	assert.Equal(t, "example-rg", attr(res, "azure.resource_group.name"))

	assert.Equal(t, int64(0), findMetric(t, rm, "aks.cluster.up").At(0).IntValue())
	assert.Equal(t, int64(1), findMetric(t, rm, "aks.cluster.provisioning.succeeded").At(0).IntValue())
	assert.Equal(t, int64(2), findMetric(t, rm, "aks.node_pool.count").At(0).IntValue())

	info := findMetric(t, rm, "aks.cluster.info").At(0).Attributes()
	assert.Equal(t, "1.37.1", attr(info, "aks.kubernetes.version"))
	assert.Equal(t, "Stopped", attr(info, "aks.power_state"))
	assert.Equal(t, "Free", attr(info, "aks.sku.tier"))
	assert.Equal(t, "overlay", attr(info, "aks.network.plugin_mode"))
	assert.Equal(t, "true", attr(info, "aks.workload_identity.enabled"))

	nodes := findMetric(t, rm, "aks.node_pool.node.count")
	require.Equal(t, 2, nodes.Len())
	assert.Equal(t, int64(2), nodes.At(0).IntValue())
	assert.Equal(t, "nodepool1", attr(nodes.At(0).Attributes(), "aks.node_pool.name"))
	assert.Equal(t, "Standard_D2s_v4", attr(nodes.At(0).Attributes(), "aks.node_pool.vm_size"))
	assert.Equal(t, int64(3), nodes.At(1).IntValue())

	up := findMetric(t, rm, "aks.node_pool.up")
	assert.Equal(t, int64(0), up.At(0).IntValue())
	assert.Equal(t, int64(1), up.At(1).IntValue())

	assert.Equal(t, int64(250), findMetric(t, rm, "aks.node_pool.pods.max").At(0).IntValue())

	// Autoscaler bounds are only reported for pools with autoscaling enabled.
	minN := findMetric(t, rm, "aks.node_pool.autoscaling.min")
	require.Equal(t, 1, minN.Len())
	assert.Equal(t, "user1", attr(minN.At(0).Attributes(), "aks.node_pool.name"))
	assert.Equal(t, int64(1), minN.At(0).IntValue())
	assert.Equal(t, int64(5), findMetric(t, rm, "aks.node_pool.autoscaling.max").At(0).IntValue())
}

func TestBuildLogs(t *testing.T) {
	mc, err := decodeCluster(loadCluster(t))
	require.NoError(t, err)
	ld := buildLogs([]managedCluster{mc}, "sub-1", pcommon.NewTimestampFromTime(time.Now()))
	require.Equal(t, 1, ld.LogRecordCount())
	lr := ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	assert.Equal(t, "aks.cluster.snapshot", lr.EventName())
	assert.Equal(t, "Stopped", attr(lr.Attributes(), "aks.power_state"))

	body := lr.Body().Map()
	assert.Equal(t, "example-aks", attr(body, "name"))
	props, ok := body.Get("properties")
	require.True(t, ok)
	pm := props.Map()
	assert.Equal(t, "1.37.1", attr(pm, "currentKubernetesVersion"))

	// Sensitive fields are removed; their non-sensitive siblings are kept.
	for _, k := range []string{"linuxProfile", "windowsProfile", "httpProxyConfig"} {
		_, found := pm.Get(k)
		assert.False(t, found, k)
	}
	sp, ok := pm.Get("servicePrincipalProfile")
	require.True(t, ok)
	_, found := sp.Map().Get("secret")
	assert.False(t, found)
	assert.Equal(t, "msi", attr(sp.Map(), "clientId"))
	aad, ok := pm.Get("aadProfile")
	require.True(t, ok)
	_, found = aad.Map().Get("serverAppSecret")
	assert.False(t, found)
	assert.Equal(t, "true", attr(aad.Map(), "managed"))

	raw, err := json.Marshal(body.AsRaw())
	require.NoError(t, err)
	for _, secret := range []string{"ssh-rsa", "azureuser", "not-a-real", "user:pass", "LS0tLS1CRUdJTg"} {
		assert.NotContains(t, string(raw), secret)
	}
}

func TestRedactToleratesMissingAndOddShapes(t *testing.T) {
	doc := map[string]any{"properties": map[string]any{
		"servicePrincipalProfile": "not-a-map",
		"aadProfile":              nil,
	}}
	redact(doc)
	redact(map[string]any{})
	assert.Equal(t, "not-a-map", doc["properties"].(map[string]any)["servicePrincipalProfile"])
}

func TestResourceGroupFromID(t *testing.T) {
	assert.Equal(t, "rg-1", resourceGroupFromID("/subscriptions/s/resourceGroups/rg-1/providers/x/y/z"))
	assert.Equal(t, "rg-2", resourceGroupFromID("/subscriptions/s/resourcegroups/rg-2/providers/x"))
	assert.Empty(t, resourceGroupFromID("/subscriptions/s"))
}

func TestEndToEndWithAuthExtension(t *testing.T) {
	srv, _ := armServer(t, loadCluster(t))
	cfg := testConfig(srv.URL)
	cfg.ResourceGroup = "example-rg"
	cfg.ClusterName = "example-aks"
	authID := component.MustNewID("azure_auth")
	cfg.Auth = &authID

	f := NewFactory()
	set := receivertest.NewNopSettings(componentType)
	host := fakeHost{ext: map[component.ID]component.Component{authID: &fakeExt{}}}

	metricsSink := new(consumertest.MetricsSink)
	mr, err := f.CreateMetrics(context.Background(), set, cfg, metricsSink)
	require.NoError(t, err)
	logsSink := new(consumertest.LogsSink)
	lrcv, err := f.CreateLogs(context.Background(), set, cfg, logsSink)
	require.NoError(t, err)

	require.NoError(t, mr.Start(context.Background(), host))
	require.NoError(t, lrcv.Start(context.Background(), host))
	require.Eventually(t, func() bool {
		return metricsSink.DataPointCount() > 0 && logsSink.LogRecordCount() > 0
	}, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, mr.Shutdown(context.Background()))
	require.NoError(t, lrcv.Shutdown(context.Background()))
}

func TestAuthExtensionErrors(t *testing.T) {
	cfg := testConfig("https://management.azure.com")
	cfg.Auth = nil
	r := newReceiver(receivertest.NewNopSettings(componentType), cfg)
	require.ErrorContains(t, r.Start(context.Background(), componenttest.NewNopHost()), "auth is required")

	missing := component.MustNewID("azure_auth")
	cfg.Auth = &missing
	require.ErrorContains(t, r.Start(context.Background(), componenttest.NewNopHost()), "not found")

	wrongID := component.MustNewID("health_check")
	cfg.Auth = &wrongID
	type plainExt struct {
		component.StartFunc
		component.ShutdownFunc
	}
	host := fakeHost{ext: map[component.ID]component.Component{wrongID: &plainExt{}}}
	require.ErrorContains(t, r.Start(context.Background(), host), "does not provide Azure tokens")
}

func TestShutdownWithoutStart(t *testing.T) {
	r := newReceiver(receivertest.NewNopSettings(componentType), testConfig("https://x"))
	require.NoError(t, r.Shutdown(context.Background()))
}
