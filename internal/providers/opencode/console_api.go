package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// console_api.go talks to the OpenCode console's JSON API
// (/console/api/*). These endpoints are stable, versionless routes used by the
// console's own web app; unlike the old /_server SolidStart RPCs, they don't
// rotate content-hash IDs on every deploy.

// flexInt64 accepts either a JSON number or a quoted number. The console API
// is inconsistent: balances arrive as strings ("972127265"), while other
// counters are plain numbers.
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	raw = strings.Trim(raw, `"`)
	if raw == "" || raw == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("opencode: parse number %q: %w", raw, err)
	}
	*f = flexInt64(v)
	return nil
}

type consoleWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type consoleBillingStatus struct {
	BillingMode           string     `json:"billingMode"`
	Mode                  string     `json:"mode"`
	BalanceMicroCents     flexInt64  `json:"balanceMicroCents"`
	AvailableMicroCents   flexInt64  `json:"availableMicroCents"`
	CreditLimitMicroCents *flexInt64 `json:"creditLimitMicroCents"`
	CanPurchaseCredits    bool       `json:"canPurchaseCredits"`
	CanEnableAutoRecharge bool       `json:"canEnableAutoRecharge"`
}

type consoleUsageSummary struct {
	TotalRequests           flexInt64 `json:"totalRequests"`
	TotalInputTokens        flexInt64 `json:"totalInputTokens"`
	TotalOutputTokens       flexInt64 `json:"totalOutputTokens"`
	TotalCacheReadTokens    flexInt64 `json:"totalCacheReadTokens"`
	TotalCacheWrite5mTokens flexInt64 `json:"totalCacheWrite5mTokens"`
	TotalCacheWrite1hTokens flexInt64 `json:"totalCacheWrite1hTokens"`
	TotalCostMicroCents     flexInt64 `json:"totalCostMicroCents"`
}

// getConsoleJSON performs an authenticated GET against the console API.
// withOrg adds the x-org-id header required by workspace-scoped endpoints.
func (c *ConsoleClient) getConsoleJSON(ctx context.Context, path string, withOrg bool) ([]byte, error) {
	if c.Cookie == "" || c.CookieName == "" {
		return nil, fmt.Errorf("console: missing session cookie")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("console: request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "openusage/console-client")
	if withOrg {
		if c.WorkspaceID == "" {
			return nil, fmt.Errorf("console: workspace ID required")
		}
		req.Header.Set("x-org-id", c.WorkspaceID)
	}
	req.AddCookie(&http.Cookie{Name: c.CookieName, Value: c.Cookie})
	return c.do(req)
}

// FetchBillingStatus returns the Zen balance and billing mode for the
// workspace. balanceMicroCents/availableMicroCents are 1e-8 dollar units.
func (c *ConsoleClient) FetchBillingStatus(ctx context.Context) (consoleBillingStatus, error) {
	body, err := c.getConsoleJSON(ctx, "/console/api/billing/status", true)
	if err != nil {
		return consoleBillingStatus{}, err
	}
	var status consoleBillingStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return consoleBillingStatus{}, fmt.Errorf("console: parsing billing status: %w", err)
	}
	return status, nil
}

// FetchUsageSummary returns aggregate requests, tokens and spend for the
// rolling 30-day window.
func (c *ConsoleClient) FetchUsageSummary(ctx context.Context) (consoleUsageSummary, error) {
	body, err := c.getConsoleJSON(ctx, "/console/api/usage/summary?range=30d", true)
	if err != nil {
		return consoleUsageSummary{}, err
	}
	var summary consoleUsageSummary
	if err := json.Unmarshal(body, &summary); err != nil {
		return consoleUsageSummary{}, fmt.Errorf("console: parsing usage summary: %w", err)
	}
	return summary, nil
}

// microCentsToUSD converts the console API's 1e-8 dollar units to dollars.
func microCentsToUSD(v flexInt64) float64 {
	return float64(v) / 1e8
}

// consoleMeter is one Go quota window. The console reports the allowance in
// 1e-8 dollar units; only the used/limit ratio matters for the gauge.
type consoleMeter struct {
	StartsAt        string    `json:"startsAt"`
	ResetsAt        string    `json:"resetsAt"`
	LimitMicroCents flexInt64 `json:"limitMicroCents"`
	UsedMicroCents  flexInt64 `json:"usedMicroCents"`
}

type consoleGoMeters struct {
	FiveHour consoleMeter `json:"fiveHour"`
	Week     consoleMeter `json:"week"`
	Month    consoleMeter `json:"month"`
}

type consoleGoAccess struct {
	StartsAt          string          `json:"startsAt"`
	EndsAt            string          `json:"endsAt"`
	CancelAtPeriodEnd bool            `json:"cancelAtPeriodEnd"`
	Meters            consoleGoMeters `json:"meters"`
}

type consoleGoStatus struct {
	Product           string           `json:"product"`
	UseBalance        bool             `json:"useBalance"`
	CancelAtPeriodEnd bool             `json:"cancelAtPeriodEnd"`
	Access            *consoleGoAccess `json:"access"`
}

// FetchGoStatus returns the Go subscription meters (5h/weekly/monthly) for the
// current workspace. A 404 or a null access object means the session is not a
// Go subscriber; callers treat that as "no Go data", not an error.
func (c *ConsoleClient) FetchGoStatus(ctx context.Context) (consoleGoStatus, error) {
	body, err := c.getConsoleJSON(ctx, "/console/api/go/status", true)
	if err != nil {
		return consoleGoStatus{}, err
	}
	var status consoleGoStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return consoleGoStatus{}, fmt.Errorf("console: parsing go status: %w", err)
	}
	return status, nil
}
