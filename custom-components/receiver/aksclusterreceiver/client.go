package aksclusterreceiver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// managedCluster holds the fields of a Microsoft.ContainerService/managedClusters
// resource that the receiver turns into metrics. Raw keeps the full document.
type managedCluster struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Location   string `json:"location"`
	Properties struct {
		ProvisioningState        string      `json:"provisioningState"`
		PowerState               powerState  `json:"powerState"`
		KubernetesVersion        string      `json:"kubernetesVersion"`
		CurrentKubernetesVersion string      `json:"currentKubernetesVersion"`
		SupportPlan              string      `json:"supportPlan"`
		NodeResourceGroup        string      `json:"nodeResourceGroup"`
		AgentPoolProfiles        []agentPool `json:"agentPoolProfiles"`
		NetworkProfile           struct {
			NetworkPlugin     string `json:"networkPlugin"`
			NetworkPluginMode string `json:"networkPluginMode"`
			NetworkPolicy     string `json:"networkPolicy"`
			OutboundType      string `json:"outboundType"`
		} `json:"networkProfile"`
		SecurityProfile struct {
			WorkloadIdentity struct {
				Enabled bool `json:"enabled"`
			} `json:"workloadIdentity"`
		} `json:"securityProfile"`
		OIDCIssuerProfile struct {
			Enabled bool `json:"enabled"`
		} `json:"oidcIssuerProfile"`
		AutoUpgradeProfile struct {
			UpgradeChannel       string `json:"upgradeChannel"`
			NodeOSUpgradeChannel string `json:"nodeOSUpgradeChannel"`
		} `json:"autoUpgradeProfile"`
	} `json:"properties"`
	SKU struct {
		Name string `json:"name"`
		Tier string `json:"tier"`
	} `json:"sku"`

	Raw map[string]any `json:"-"`
}

type powerState struct {
	Code string `json:"code"`
}

type agentPool struct {
	Name                       string     `json:"name"`
	Count                      *int64     `json:"count"`
	VMSize                     string     `json:"vmSize"`
	MaxPods                    *int64     `json:"maxPods"`
	Mode                       string     `json:"mode"`
	OSType                     string     `json:"osType"`
	OSSKU                      string     `json:"osSKU"`
	ProvisioningState          string     `json:"provisioningState"`
	PowerState                 powerState `json:"powerState"`
	CurrentOrchestratorVersion string     `json:"currentOrchestratorVersion"`
	NodeImageVersion           string     `json:"nodeImageVersion"`
	EnableAutoScaling          bool       `json:"enableAutoScaling"`
	MinCount                   *int64     `json:"minCount"`
	MaxCount                   *int64     `json:"maxCount"`
}

type listResponse struct {
	Value    []json.RawMessage `json:"value"`
	NextLink string            `json:"nextLink"`
}

// armClient reads managed clusters from Azure Resource Manager.
type armClient struct {
	http *http.Client
	cred azcore.TokenCredential
	cfg  *Config
}

// maxPages guards against a nextLink loop.
const maxPages = 100

// listClusters returns the clusters in the configured scope, following nextLink.
func (c *armClient) listClusters(ctx context.Context) ([]managedCluster, error) {
	if c.cfg.ClusterName != "" {
		body, err := c.get(ctx, c.cfg.requestURL())
		if err != nil {
			return nil, err
		}
		mc, err := decodeCluster(body)
		if err != nil {
			return nil, err
		}
		return []managedCluster{mc}, nil
	}

	var out []managedCluster
	next := c.cfg.requestURL()
	for page := 0; next != ""; page++ {
		if page == maxPages {
			return out, fmt.Errorf("stopped after %d pages of results", maxPages)
		}
		body, err := c.get(ctx, next)
		if err != nil {
			return out, err
		}
		var lr listResponse
		if err := json.Unmarshal(body, &lr); err != nil {
			return out, fmt.Errorf("decoding cluster list: %w", err)
		}
		for _, raw := range lr.Value {
			mc, err := decodeCluster(raw)
			if err != nil {
				return out, err
			}
			out = append(out, mc)
		}
		next = lr.NextLink
	}
	return out, nil
}

func decodeCluster(b []byte) (managedCluster, error) {
	var mc managedCluster
	if err := json.Unmarshal(b, &mc); err != nil {
		return mc, fmt.Errorf("decoding cluster: %w", err)
	}
	if err := json.Unmarshal(b, &mc.Raw); err != nil {
		return mc, fmt.Errorf("decoding cluster: %w", err)
	}
	redact(mc.Raw)
	return mc, nil
}

// sensitivePaths are removed from the raw resource document before it is
// emitted, so credentials and admin access details never leave the collector.
var sensitivePaths = [][]string{
	{"properties", "linuxProfile"},                      // admin username, SSH public keys
	{"properties", "windowsProfile"},                    // admin username, password
	{"properties", "servicePrincipalProfile", "secret"}, // legacy service principal secret
	{"properties", "aadProfile", "serverAppSecret"},     // legacy AAD integration secret
	{"properties", "httpProxyConfig"},                   // proxy URLs may embed credentials
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
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
