package azureactivitylogreceiver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// localizable is Azure's {"value", "localizedValue"} pair.
type localizable struct {
	Value string `json:"value"`
}

// event holds the activity log fields the receiver maps to log record
// fields and attributes. Raw keeps the full (redacted) event document.
type event struct {
	EventDataID          string      `json:"eventDataId"`
	ID                   string      `json:"id"`
	OperationID          string      `json:"operationId"`
	CorrelationID        string      `json:"correlationId"`
	Caller               string      `json:"caller"`
	Level                string      `json:"level"`
	ResourceID           string      `json:"resourceId"`
	ResourceGroupName    string      `json:"resourceGroupName"`
	ResourceProviderName localizable `json:"resourceProviderName"`
	ResourceType         localizable `json:"resourceType"`
	OperationName        localizable `json:"operationName"`
	Category             localizable `json:"category"`
	Status               localizable `json:"status"`
	SubStatus            localizable `json:"subStatus"`
	EventTimestamp       time.Time   `json:"eventTimestamp"`
	SubmissionTimestamp  time.Time   `json:"submissionTimestamp"`
	SubscriptionID       string      `json:"subscriptionId"`
	TenantID             string      `json:"tenantId"`
	HTTPRequest          struct {
		ClientIPAddress string `json:"clientIpAddress"`
		Method          string `json:"method"`
	} `json:"httpRequest"`
	Authorization struct {
		Action string `json:"action"`
		Scope  string `json:"scope"`
	} `json:"authorization"`

	Raw map[string]any `json:"-"`
}

type listResponse struct {
	Value    []json.RawMessage `json:"value"`
	NextLink string            `json:"nextLink"`
}

// armClient reads activity log events from Azure Resource Manager.
type armClient struct {
	http *http.Client
	cred azcore.TokenCredential
	cfg  *Config
}

// maxPages guards against a nextLink loop and very large backfills in one poll.
const maxPages = 1000

var errTooManyPages = errors.New("stopped after the maximum number of result pages")

// listEvents returns the events with eventTimestamp in [from, to], following nextLink.
// On error, the events read so far are returned with the error.
func (c *armClient) listEvents(ctx context.Context, from, to time.Time) ([]event, error) {
	var out []event
	next := c.cfg.requestURL(from, to)
	for page := 0; next != ""; page++ {
		if page == maxPages {
			return out, errTooManyPages
		}
		body, err := c.get(ctx, next)
		if err != nil {
			return out, err
		}
		var lr listResponse
		if err := json.Unmarshal(body, &lr); err != nil {
			return out, fmt.Errorf("decoding activity log page: %w", err)
		}
		for _, raw := range lr.Value {
			ev, err := decodeEvent(raw)
			if err != nil {
				return out, err
			}
			out = append(out, ev)
		}
		next = lr.NextLink
	}
	return out, nil
}

func decodeEvent(b []byte) (event, error) {
	var ev event
	if err := json.Unmarshal(b, &ev); err != nil {
		return ev, fmt.Errorf("decoding activity log event: %w", err)
	}
	if err := json.Unmarshal(b, &ev.Raw); err != nil {
		return ev, fmt.Errorf("decoding activity log event: %w", err)
	}
	redact(ev.Raw)
	return ev, nil
}

// sensitivePaths are removed from each event before it is emitted.
var sensitivePaths = [][]string{
	{"claims"},                    // the caller's full token claims (names, IPs, object IDs, auth methods)
	{"properties", "requestbody"}, // request payloads can contain secrets (passwords, keys)
	{"properties", "responseBody"},
	{"properties", "responsebody"},
}

func redact(doc map[string]any) {
	for _, p := range sensitivePaths {
		m := doc
		for i, key := range p {
			if i == len(p)-1 {
				delete(m, key)
				break
			}
			next, ok := m[key].(map[string]any)
			if !ok {
				break
			}
			m = next
		}
	}
}

func (c *armClient) get(ctx context.Context, u string) ([]byte, error) {
	tok, err := c.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{c.cfg.scope()}})
	if err != nil {
		return nil, fmt.Errorf("getting Azure token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, armError(resp.StatusCode, body)
	}
	return body, nil
}

// armError turns an Azure Resource Manager error response into an error.
func armError(status int, body []byte) error {
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Code != "" {
		return fmt.Errorf("azure resource manager returned %d %s: %s", status, e.Error.Code, e.Error.Message)
	}
	if len(body) > 512 {
		body = body[:512]
	}
	return fmt.Errorf("azure resource manager returned %d: %s", status, body)
}
