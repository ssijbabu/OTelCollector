package azureactivitylogreceiver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"sync"
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
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

type fakeCred struct{}

func (fakeCred) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "test-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

type fakeExt struct {
	component.StartFunc
	component.ShutdownFunc
	fakeCred
}

type fakeHost struct {
	ext map[component.ID]component.Component
}

func (h fakeHost) GetExtensions() map[component.ID]component.Component { return h.ext }

func loadEvents(t *testing.T) []json.RawMessage {
	b, err := os.ReadFile("testdata/events.json")
	require.NoError(t, err)
	var evs []json.RawMessage
	require.NoError(t, json.Unmarshal(b, &evs))
	return evs
}

// fakeARM serves the activity log API: it applies the eventTimestamp range from
// $filter and pages the result two events at a time.
type fakeARM struct {
	t      *testing.T
	mu     sync.Mutex
	events []json.RawMessage
	fail   bool
	calls  atomic.Int32
	srv    *httptest.Server
}

var filterRE = regexp.MustCompile(`eventTimestamp ge '([^']+)' and eventTimestamp le '([^']+)'`)

func newFakeARM(t *testing.T, events []json.RawMessage) *fakeARM {
	f := &fakeARM{t: t, events: events}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeARM) add(ev json.RawMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
}

func (f *fakeARM) serve(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	assert.Equal(f.t, "Bearer test-token", r.Header.Get("Authorization"))
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"TooManyRequests","message":"throttled"}}`))
		return
	}
	m := filterRE.FindStringSubmatch(r.URL.Query().Get("$filter"))
	require.Len(f.t, m, 3, "filter: %s", r.URL.Query().Get("$filter"))
	from, _ := time.Parse(time.RFC3339Nano, m[1])
	to, _ := time.Parse(time.RFC3339Nano, m[2])

	var match []json.RawMessage
	for _, raw := range f.events {
		var e struct {
			EventTimestamp time.Time `json:"eventTimestamp"`
		}
		require.NoError(f.t, json.Unmarshal(raw, &e))
		if !e.EventTimestamp.Before(from) && !e.EventTimestamp.After(to) {
			match = append(match, raw)
		}
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	end := min(page*2+2, len(match))
	resp := map[string]any{"value": match[min(page*2, len(match)):end]}
	if end < len(match) {
		q := r.URL.Query()
		q.Set("page", strconv.Itoa(page+1))
		resp["nextLink"] = f.srv.URL + r.URL.Path + "?" + q.Encode()
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func testConfig(endpoint string) *Config {
	cfg := createDefaultConfig().(*Config)
	cfg.Endpoint = endpoint
	cfg.SubscriptionID = "00000000-0000-0000-0000-000000000000"
	cfg.InitialDelay = 0
	authID := component.MustNewID("azure_auth")
	cfg.Auth = &authID
	return cfg
}

// newTestReceiver returns a receiver wired to the fake server with a controllable clock.
func newTestReceiver(t *testing.T, f *fakeARM, sink *consumertest.LogsSink, now *time.Time) *activityLogReceiver {
	cfg := testConfig(f.srv.URL)
	r := newReceiver(receivertest.NewNopSettings(componentType), cfg, sink)
	r.now = func() time.Time { return *now }
	r.client = &armClient{http: f.srv.Client(), cred: fakeCred{}, cfg: cfg}
	r.cursor = now.Add(-cfg.InitialLookback)
	r.first = true
	return r
}

func recordIDs(sink *consumertest.LogsSink) []string {
	var ids []string
	for _, ld := range sink.AllLogs() {
		rls := ld.ResourceLogs()
		for i := 0; i < rls.Len(); i++ {
			lrs := rls.At(i).ScopeLogs().At(0).LogRecords()
			for j := 0; j < lrs.Len(); j++ {
				v, _ := lrs.At(j).Attributes().Get("azure.activity_log.event_data_id")
				ids = append(ids, v.Str())
			}
		}
	}
	return ids
}

func TestPollPagingDedupAndLateEvents(t *testing.T) {
	evs := loadEvents(t)
	f := newFakeARM(t, evs)
	sink := new(consumertest.LogsSink)
	now := time.Date(2026, 11, 7, 10, 10, 0, 0, time.UTC)
	r := newTestReceiver(t, f, sink, &now)

	// First poll: window [09:55, 10:10] holds all three events (two pages).
	r.poll(context.Background())
	assert.Equal(t, int32(2), f.calls.Load())
	// Grouped by resource (AKS cluster first: its first event is the earliest),
	// sorted by event time within each resource.
	assert.Equal(t, []string{
		"aaaaaaaa-0000-0000-0000-000000000001",
		"aaaaaaaa-0000-0000-0000-000000000002",
		"aaaaaaaa-0000-0000-0000-000000000003",
	}, recordIDs(sink))
	assert.Equal(t, now, r.cursor)

	// Second poll re-reads the overlap window but emits nothing new.
	now = now.Add(time.Minute)
	r.poll(context.Background())
	assert.Len(t, recordIDs(sink), 3)

	// An event that shows up late, timestamped before the last poll, is still picked up once.
	f.add(json.RawMessage(`{"eventDataId":"late-1","eventTimestamp":"2026-11-07T10:08:00Z","level":"Warning","resourceId":"/subscriptions/s/resourceGroups/rg/providers/p/t/n","subscriptionId":"s"}`))
	now = now.Add(time.Minute)
	r.poll(context.Background())
	now = now.Add(time.Minute)
	r.poll(context.Background())
	ids := recordIDs(sink)
	assert.Len(t, ids, 4)
	assert.Equal(t, "late-1", ids[3])

	// Entries older than the overlap window are forgotten.
	now = now.Add(time.Hour)
	r.poll(context.Background())
	assert.Empty(t, r.seen)
}

func TestPollErrorKeepsWindow(t *testing.T) {
	f := newFakeARM(t, loadEvents(t))
	sink := new(consumertest.LogsSink)
	now := time.Date(2026, 11, 7, 10, 10, 0, 0, time.UTC)
	r := newTestReceiver(t, f, sink, &now)
	start := r.cursor

	f.fail = true
	r.poll(context.Background())
	assert.Equal(t, start, r.cursor, "cursor must not move on error")
	assert.True(t, r.first)
	assert.Zero(t, sink.LogRecordCount())

	f.fail = false
	now = now.Add(time.Minute)
	r.poll(context.Background())
	assert.Equal(t, 3, sink.LogRecordCount())
}

func TestConsumeErrorRetriesEvents(t *testing.T) {
	f := newFakeARM(t, loadEvents(t))
	now := time.Date(2026, 11, 7, 10, 10, 0, 0, time.UTC)
	cfg := testConfig(f.srv.URL)
	calls := 0
	failing := consumertest.NewErr(errors.New("downstream full"))
	sink := new(consumertest.LogsSink)
	r := newReceiver(receivertest.NewNopSettings(componentType), cfg, failing)
	r.now = func() time.Time { return now }
	r.client = &armClient{http: f.srv.Client(), cred: fakeCred{}, cfg: cfg}
	r.cursor = now.Add(-cfg.InitialLookback)
	r.first = true

	r.poll(context.Background())
	calls++
	assert.Empty(t, r.seen)
	assert.True(t, r.first)

	r.next = sink
	r.poll(context.Background())
	assert.Equal(t, 3, sink.LogRecordCount(), "events are delivered after the consumer recovers (after %d failed polls)", calls)
}

func attr(m pcommon.Map, k string) string {
	v, _ := m.Get(k)
	return v.AsString()
}

func TestBuildLogs(t *testing.T) {
	var evs []event
	for _, raw := range loadEvents(t) {
		ev, err := decodeEvent(raw)
		require.NoError(t, err)
		evs = append(evs, ev)
	}
	observed := time.Date(2026, 11, 7, 11, 0, 0, 0, time.UTC)
	ld := buildLogs(evs, "fallback-sub", observed)

	// Two resources: the AKS cluster (2 events) and the role assignment (1 event).
	require.Equal(t, 2, ld.ResourceLogs().Len())
	rl := ld.ResourceLogs().At(0)
	ra := rl.Resource().Attributes()
	assert.Equal(t, "azure", attr(ra, "cloud.provider"))
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", attr(ra, "cloud.account.id"))
	assert.Equal(t, "example-rg", attr(ra, "azure.resource_group.name"))
	assert.Contains(t, attr(ra, "cloud.resource_id"), "/managedClusters/example-aks")
	require.Equal(t, 2, rl.ScopeLogs().At(0).LogRecords().Len())

	lr := rl.ScopeLogs().At(0).LogRecords().At(0)
	assert.Equal(t, "azure.activity_log", lr.EventName())
	assert.Equal(t, time.Date(2026, 11, 7, 10, 0, 0, 123456700, time.UTC), lr.Timestamp().AsTime())
	assert.Equal(t, observed, lr.ObservedTimestamp().AsTime())
	assert.Equal(t, plog.SeverityNumberInfo, lr.SeverityNumber())
	assert.Equal(t, "Informational", lr.SeverityText())
	a := lr.Attributes()
	assert.Equal(t, "Microsoft.ContainerService/managedClusters/stop/action", attr(a, "azure.activity_log.operation.name"))
	assert.Equal(t, "Administrative", attr(a, "azure.activity_log.category"))
	assert.Equal(t, "Started", attr(a, "azure.activity_log.status"))
	assert.Equal(t, "user@example.com", attr(a, "azure.activity_log.caller"))
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", attr(a, "azure.activity_log.correlation_id"))
	assert.Equal(t, "Microsoft.ContainerService", attr(a, "azure.activity_log.resource_provider"))
	assert.Equal(t, "203.0.113.10", attr(a, "client.address"))
	assert.Equal(t, "POST", attr(a, "http.request.method"))
	_, hasEmpty := a.Get("azure.activity_log.sub_status")
	assert.False(t, hasEmpty, "empty values are not set")

	failed := ld.ResourceLogs().At(1).ScopeLogs().At(0).LogRecords().At(0)
	assert.Equal(t, plog.SeverityNumberError, failed.SeverityNumber())
	assert.Equal(t, "Forbidden", attr(failed.Attributes(), "azure.activity_log.sub_status"))

	// Body is the event document with sensitive fields removed.
	body := lr.Body().Map()
	_, hasClaims := body.Get("claims")
	assert.False(t, hasClaims)
	props, ok := body.Get("properties")
	require.True(t, ok)
	_, hasReq := props.Map().Get("requestbody")
	assert.False(t, hasReq)
	assert.Equal(t, "Administrative", attr(props.Map(), "eventCategory"))

	raw, err := json.Marshal(ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(1).Body().AsRaw())
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "not-a-real")
	raw, err = json.Marshal(body.AsRaw())
	require.NoError(t, err)
	for _, s := range []string{"not-a-real", "Example User", "objectidentifier"} {
		assert.NotContains(t, string(raw), s)
	}
}

func TestSeverity(t *testing.T) {
	for level, want := range map[string]plog.SeverityNumber{
		"Critical": plog.SeverityNumberFatal, "Error": plog.SeverityNumberError,
		"Warning": plog.SeverityNumberWarn, "Informational": plog.SeverityNumberInfo,
		"Verbose": plog.SeverityNumberDebug, "": plog.SeverityNumberUnspecified,
	} {
		got, text := severity(level)
		assert.Equal(t, want, got, level)
		assert.Equal(t, level, text)
	}
}

func TestWindowClampedToRetention(t *testing.T) {
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	r := newReceiver(receivertest.NewNopSettings(componentType), testConfig("https://x"), consumertest.NewNop())
	r.cursor = now.Add(-200 * 24 * time.Hour)
	from, to := r.window(now)
	assert.Equal(t, now.Add(-maxRetention), from)
	assert.Equal(t, now, to)

	r.cursor, r.first = now.Add(-time.Minute), false
	from, _ = r.window(now)
	assert.Equal(t, now.Add(-time.Minute-15*time.Minute), from)
}

func TestEndToEnd(t *testing.T) {
	f := newFakeARM(t, nil)
	now := time.Now().UTC()
	f.add(json.RawMessage(`{"eventDataId":"e2e-1","eventTimestamp":"` + now.Add(-5*time.Minute).Format(time.RFC3339Nano) + `","level":"Informational","resourceId":"/subscriptions/s/resourceGroups/rg/providers/p/t/n"}`))
	cfg := testConfig(f.srv.URL)
	authID := *cfg.Auth
	host := fakeHost{ext: map[component.ID]component.Component{authID: &fakeExt{}}}

	sink := new(consumertest.LogsSink)
	rcv, err := NewFactory().CreateLogs(context.Background(), receivertest.NewNopSettings(componentType), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, rcv.Start(context.Background(), host))
	require.Eventually(t, func() bool { return sink.LogRecordCount() == 1 }, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, rcv.Shutdown(context.Background()))
}

func TestAuthExtensionErrors(t *testing.T) {
	cfg := testConfig("https://management.azure.com")
	r := newReceiver(receivertest.NewNopSettings(componentType), cfg, consumertest.NewNop())

	cfg.Auth = nil
	require.ErrorContains(t, r.Start(context.Background(), componenttest.NewNopHost()), "auth is required")

	missing := component.MustNewID("azure_auth")
	cfg.Auth = &missing
	require.ErrorContains(t, r.Start(context.Background(), componenttest.NewNopHost()), "not found")

	type plainExt struct {
		component.StartFunc
		component.ShutdownFunc
	}
	wrong := component.MustNewID("health_check")
	cfg.Auth = &wrong
	host := fakeHost{ext: map[component.ID]component.Component{wrong: &plainExt{}}}
	require.ErrorContains(t, r.Start(context.Background(), host), "does not provide Azure tokens")
	require.NoError(t, r.Shutdown(context.Background()))
}
