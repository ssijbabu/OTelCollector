package azureactivitylogreceiver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"
)

const scopeName = "github.com/ssijbabu/OTelCollector/custom-components/receiver/azureactivitylogreceiver"

type activityLogReceiver struct {
	cfg    *Config
	set    receiver.Settings
	next   consumer.Logs
	client *armClient
	httpDo *http.Client
	now    func() time.Time

	// Polling state. Only the polling goroutine touches it after Start.
	cursor time.Time            // end of the last fully read window
	first  bool                 // the first window starts exactly at cursor
	seen   map[string]time.Time // emitted event IDs -> event time, for de-duplication

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newReceiver(set receiver.Settings, cfg *Config, next consumer.Logs) *activityLogReceiver {
	return &activityLogReceiver{
		cfg:    cfg,
		set:    set,
		next:   next,
		httpDo: &http.Client{},
		now:    time.Now,
		seen:   map[string]time.Time{},
	}
}

func (r *activityLogReceiver) Start(_ context.Context, host component.Host) error {
	cred, err := r.credential(host)
	if err != nil {
		return err
	}
	r.client = &armClient{http: r.httpDo, cred: cred, cfg: r.cfg}
	r.cursor = r.now().Add(-r.cfg.InitialLookback)
	r.first = true

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.wg.Add(1)
	go r.run(ctx)
	return nil
}

func (r *activityLogReceiver) Shutdown(context.Context) error {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
	return nil
}

// credential returns the token source provided by the configured azure_auth extension.
func (r *activityLogReceiver) credential(host component.Host) (azcore.TokenCredential, error) {
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

func (r *activityLogReceiver) run(ctx context.Context) {
	defer r.wg.Done()
	select {
	case <-ctx.Done():
		return
	case <-time.After(r.cfg.InitialDelay):
	}
	ticker := time.NewTicker(r.cfg.CollectionInterval)
	defer ticker.Stop()
	for {
		r.poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// window returns the time range for the next poll. After the first poll it
// starts MaxIngestionDelay before the last fully read point, to pick up
// events that Azure made available late.
func (r *activityLogReceiver) window(now time.Time) (time.Time, time.Time) {
	from := r.cursor
	if !r.first {
		from = from.Add(-r.cfg.MaxIngestionDelay)
	}
	if oldest := now.Add(-maxRetention); from.Before(oldest) {
		from = oldest
	}
	return from, now
}

func eventKey(ev *event) string {
	if ev.EventDataID != "" {
		return ev.EventDataID
	}
	return ev.ID + "|" + ev.EventTimestamp.String()
}

func (r *activityLogReceiver) poll(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	logger := r.set.Logger

	now := r.now()
	from, to := r.window(now)
	events, listErr := r.client.listEvents(ctx, from, to)
	if listErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return
		}
		logger.Error("Failed to read the Azure activity log; the window is retried on the next poll",
			zap.Time("from", from), zap.Time("to", to), zap.Int("events_read", len(events)), zap.Error(listErr))
	}

	fresh := events[:0]
	for i := range events {
		if _, dup := r.seen[eventKey(&events[i])]; !dup {
			fresh = append(fresh, events[i])
		}
	}
	sort.SliceStable(fresh, func(i, j int) bool { return fresh[i].EventTimestamp.Before(fresh[j].EventTimestamp) })

	if len(fresh) > 0 {
		if err := r.next.ConsumeLogs(ctx, buildLogs(fresh, r.cfg.SubscriptionID, now)); err != nil {
			logger.Error("Failed to consume activity log events; the window is retried on the next poll", zap.Error(err))
			return
		}
		for i := range fresh {
			r.seen[eventKey(&fresh[i])] = fresh[i].EventTimestamp
		}
	}

	if listErr == nil {
		r.cursor = to
		r.first = false
	}
	// Forget events that can no longer appear in a future window.
	horizon := r.cursor.Add(-r.cfg.MaxIngestionDelay)
	for k, ts := range r.seen {
		if ts.Before(horizon) {
			delete(r.seen, k)
		}
	}
}

func severity(level string) (plog.SeverityNumber, string) {
	switch strings.ToLower(level) {
	case "critical":
		return plog.SeverityNumberFatal, level
	case "error":
		return plog.SeverityNumberError, level
	case "warning":
		return plog.SeverityNumberWarn, level
	case "informational":
		return plog.SeverityNumberInfo, level
	case "verbose":
		return plog.SeverityNumberDebug, level
	}
	return plog.SeverityNumberUnspecified, level
}

func putStr(m pcommon.Map, k, v string) {
	if v != "" {
		m.PutStr(k, v)
	}
}

// buildLogs emits one log record per event, grouped by the resource the event is about.
func buildLogs(events []event, subscriptionID string, observed time.Time) plog.Logs {
	ld := plog.NewLogs()
	byResource := map[string]plog.LogRecordSlice{}
	for i := range events {
		ev := &events[i]
		key := strings.ToLower(ev.ResourceID)
		records, ok := byResource[key]
		if !ok {
			rl := ld.ResourceLogs().AppendEmpty()
			ra := rl.Resource().Attributes()
			ra.PutStr("cloud.provider", "azure")
			sub := ev.SubscriptionID
			if sub == "" {
				sub = subscriptionID
			}
			ra.PutStr("cloud.account.id", sub)
			putStr(ra, "cloud.resource_id", ev.ResourceID)
			putStr(ra, "azure.resource_group.name", ev.ResourceGroupName)
			sl := rl.ScopeLogs().AppendEmpty()
			sl.Scope().SetName(scopeName)
			records = sl.LogRecords()
			byResource[key] = records
		}

		lr := records.AppendEmpty()
		lr.SetTimestamp(pcommon.NewTimestampFromTime(ev.EventTimestamp))
		lr.SetObservedTimestamp(pcommon.NewTimestampFromTime(observed))
		lr.SetEventName("azure.activity_log")
		sev, text := severity(ev.Level)
		lr.SetSeverityNumber(sev)
		lr.SetSeverityText(text)

		a := lr.Attributes()
		putStr(a, "azure.activity_log.event_data_id", ev.EventDataID)
		putStr(a, "azure.activity_log.operation.name", ev.OperationName.Value)
		putStr(a, "azure.activity_log.operation.id", ev.OperationID)
		putStr(a, "azure.activity_log.category", ev.Category.Value)
		putStr(a, "azure.activity_log.status", ev.Status.Value)
		putStr(a, "azure.activity_log.sub_status", ev.SubStatus.Value)
		putStr(a, "azure.activity_log.caller", ev.Caller)
		putStr(a, "azure.activity_log.correlation_id", ev.CorrelationID)
		putStr(a, "azure.activity_log.resource_provider", ev.ResourceProviderName.Value)
		putStr(a, "azure.activity_log.resource_type", ev.ResourceType.Value)
		putStr(a, "azure.activity_log.authorization.action", ev.Authorization.Action)
		putStr(a, "azure.tenant.id", ev.TenantID)
		putStr(a, "client.address", ev.HTTPRequest.ClientIPAddress)
		putStr(a, "http.request.method", ev.HTTPRequest.Method)

		if err := lr.Body().SetEmptyMap().FromRaw(ev.Raw); err != nil {
			lr.Body().SetStr(fmt.Sprintf("%v", ev.Raw))
		}
	}
	return ld
}
