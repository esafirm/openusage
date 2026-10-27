package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// OpenCode console exposes data behind SolidStart server functions reachable
// at https://opencode.ai/_server. Each function has a content-hash ID
// (sha256 of its server-side source); these IDs change on every backend
// deploy. Pinned IDs below were captured 2026-04-30 from the user's HAR
// and cross-referenced against CodexBar's captures.
// The IDs are paired with a stable "purpose" name so we can grep / replace
// them in one place when they rotate.
const (
	consoleBaseURL = "https://opencode.ai"

	// queryBillingInfo — returns balance, monthly limit, monthly usage,
	// auto-reload config, payment method, subscription state.
	// Args: [workspaceID].
	rpcBillingInfoID = "c83b78a614689c38ebee981f9b39a8b377716db85c1fd7dbab604adc02d3313d"
)

// ConsoleClient is a minimal SolidStart RPC client for the OpenCode console.
// Cookie-authed; never writes mutations.
type ConsoleClient struct {
	httpClient *http.Client
	baseURL    string

	// Cookie is the session cookie value (typically the `auth` cookie's
	// content). The runtime composes a Cookie header from this on every
	// request — it's a credential, never logged.
	Cookie     string
	CookieName string

	// WorkspaceID identifies which OpenCode workspace to query. Required
	// for billing.get, queryKeys, etc. — without it we'd query the empty
	// "default" which most of the RPCs reject.
	WorkspaceID string
}

// NewConsoleClient returns a client with sane defaults: 15s HTTP timeout,
// pointing at https://opencode.ai. Tests can override baseURL.
func NewConsoleClient(cookieValue, cookieName, workspaceID string) *ConsoleClient {
	return &ConsoleClient{
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		baseURL:     consoleBaseURL,
		Cookie:      cookieValue,
		CookieName:  cookieName,
		WorkspaceID: workspaceID,
	}
}

// SerovalArg matches the JSON shape SolidStart's call serialisation uses.
// Each argument is a tiny tagged-union: `{t: 1, s: "<string>"}` for a
// string, `{t: 0, s: <number>}` for a number. Arrays of args wrap into
// `{t: 9, i: 0, l: <count>, a: [...args], o: 0}`.
type serovalArg struct {
	T int `json:"t"`
	S any `json:"s,omitempty"`
}

type serovalCall struct {
	T int          `json:"t"`
	I int          `json:"i"`
	L int          `json:"l"`
	A []serovalArg `json:"a"`
	O int          `json:"o"`
}

type serovalRequest struct {
	T serovalCall `json:"t"`
	F int         `json:"f"`
	M []any       `json:"m"`
}

// buildArgsPayload constructs the SolidStart args envelope. Mirrors what
// the browser sends — verified against captured HAR requests.
func buildArgsPayload(args ...any) serovalRequest {
	encoded := make([]serovalArg, 0, len(args))
	for _, a := range args {
		switch v := a.(type) {
		case string:
			encoded = append(encoded, serovalArg{T: 1, S: v})
		case int:
			encoded = append(encoded, serovalArg{T: 0, S: v})
		case float64:
			encoded = append(encoded, serovalArg{T: 0, S: v})
		default:
			// Fallback — treat anything else as a string. SolidStart
			// rejects unexpected shapes anyway, so this just forwards
			// the error rather than masking it.
			encoded = append(encoded, serovalArg{T: 1, S: fmt.Sprintf("%v", v)})
		}
	}
	return serovalRequest{
		T: serovalCall{T: 9, I: 0, L: len(args), A: encoded, O: 0},
		F: 31,
		M: []any{},
	}
}

// callGET invokes a GET-style server function (queryBillingInfo, queryKeys,
// queryUsage). The args payload is URL-encoded into the `args` query
// parameter; the function ID goes in both the `id` query param and the
// `x-server-id` header (browser sends both; the server checks one of them).
func (c *ConsoleClient) callGET(ctx context.Context, fnID string, args ...any) ([]byte, error) {
	if c.Cookie == "" || c.CookieName == "" {
		return nil, errors.New("console: missing session cookie")
	}
	payload := buildArgsPayload(args...)
	argsJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("console: encode args: %w", err)
	}

	u := fmt.Sprintf("%s/_server?id=%s&args=%s", c.baseURL, fnID, url.QueryEscape(string(argsJSON)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c.applyHeaders(req, fnID)

	return c.do(req)
}

func (c *ConsoleClient) applyHeaders(req *http.Request, fnID string) {
	req.Header.Set("Accept", "*/*")
	if fnID != "" {
		req.Header.Set("x-server-id", fnID)
		req.Header.Set("x-server-instance", "openusage")
	}
	// Cookie header — single cookie, not a full jar. The session cookie
	// is the only one we need; OpenCode's console doesn't gate on
	// CSRF/anti-forgery for these GETs.
	req.AddCookie(&http.Cookie{Name: c.CookieName, Value: c.Cookie})
	req.Header.Set("User-Agent", "openusage/console-client")
}

// DiscoverWorkspaceID resolves the user's personal workspace through the
// console's own API. The old /auth redirect now points at /console/login even
// with a valid console session, so it can't be used for discovery.
func (c *ConsoleClient) DiscoverWorkspaceID(ctx context.Context) (string, error) {
	body, err := c.getConsoleJSON(ctx, "/console/api/user/workspaces", false)
	if err != nil {
		return "", err
	}

	var workspaces []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if err := json.Unmarshal(body, &workspaces); err != nil {
		return "", fmt.Errorf("console: parsing workspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return "", errors.New("console: no workspaces for this session")
	}
	for _, ws := range workspaces {
		if strings.TrimSpace(ws.ID) == "" {
			continue
		}
		if ws.Role == "owner" {
			return ws.ID, nil
		}
	}
	if id := strings.TrimSpace(workspaces[0].ID); id != "" {
		return id, nil
	}
	return "", errors.New("console: workspace list has no usable id")
}

// SubscriptionUsage is the parsed shape of a subscription.get response.
// Contains rolling 5-hour, weekly, and monthly usage percentages with reset timers.
//
// The *OK fields distinguish "field present in the response with a value of
// 0" from "field absent" — a usagePercent of 0 is a legitimate reading (the
// quota window just reset) and must not be treated the same as "not found".
type SubscriptionUsage struct {
	RollingUsagePct float64
	RollingUsageOK  bool
	RollingResetSec int
	WeeklyUsagePct  float64
	WeeklyUsageOK   bool
	WeeklyResetSec  int
	MonthlyUsagePct float64
	MonthlyUsageOK  bool
	MonthlyResetSec int
	RenewAt         string
}

func (c *ConsoleClient) do(req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("console: request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("console: read body: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &ConsoleAuthError{StatusCode: resp.StatusCode, Body: shortenBody(body)}
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("console: http %d: %s", resp.StatusCode, shortenBody(body))
	}
	return body, nil
}

// FetchPageRaw fetches a page from the OpenCode console with cookie auth.
// Used for scraping the Go usage page HTML.
func (c *ConsoleClient) FetchPageRaw(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.applyHeaders(req, "")
	return c.do(req)
}

// FetchGoUsagePage fetches the OpenCode Go usage page and extracts billing
// and subscription usage data from the embedded Seroval script. This is more
// reliable than individual RPC calls whose IDs rotate on every deploy.
func (c *ConsoleClient) FetchGoUsagePage(ctx context.Context, workspaceID string) (SubscriptionUsage, BillingInfo, error) {
	if workspaceID == "" {
		return SubscriptionUsage{}, BillingInfo{}, errors.New("console: workspace ID required")
	}

	url := fmt.Sprintf("%s/workspace/%s/go", c.baseURL, workspaceID)
	body, err := c.FetchPageRaw(ctx, url)
	if err != nil {
		return SubscriptionUsage{}, BillingInfo{}, fmt.Errorf("console: fetch go page: %w", err)
	}

	html := string(body)
	if looksSignedOutFromHTML(html) {
		return SubscriptionUsage{}, BillingInfo{}, &ConsoleAuthError{StatusCode: 401, Body: "page indicates signed out"}
	}

	subscription, billing, err := parseGoUsagePageHTML(html)
	if err != nil {
		return SubscriptionUsage{}, BillingInfo{}, fmt.Errorf("console: parse go page: %w", err)
	}
	return subscription, billing, nil
}

// looksSignedOutFromHTML checks if an HTML response indicates the user is
// not authenticated.
func looksSignedOutFromHTML(html string) bool {
	lower := strings.ToLower(html)
	return strings.Contains(lower, "sign in") ||
		strings.Contains(lower, "login") ||
		strings.Contains(lower, "auth/authorize")
}

// parseGoUsagePageHTML extracts billing and subscription usage data from the
// embedded Seroval script in the OpenCode Go usage page HTML.
func parseGoUsagePageHTML(html string) (SubscriptionUsage, BillingInfo, error) {
	subscription := SubscriptionUsage{}
	billing := BillingInfo{}

	// Extract the main Seroval script blob
	scriptRE := regexp.MustCompile(`self\.\$R=self\.\$R\|\|\[\].*`)
	scriptMatch := scriptRE.FindString(html)
	if scriptMatch == "" {
		return subscription, billing, errors.New("console: go usage page missing embedded usage data")
	}

	// Extract billing data by finding the billing.get assignment followed by
	// the billing fields object. The pattern is:
	// billing.get["wrk_..."]}=$R[N]=$R[M]($R[O]={...billing fields...})
	billingObjRE := regexp.MustCompile(`billing\.get\["[^"]*"\].*?\$R\[\d+\]=\{([^}]+)\}`)
	billingMatches := billingObjRE.FindStringSubmatch(scriptMatch)
	if len(billingMatches) <= 1 {
		return subscription, billing, errors.New("console: go usage page missing billing data")
	}
	billingFields := billingMatches[1]
	if !hasRecognizedBillingField(billingFields) {
		return subscription, billing, errors.New("console: go usage page has unrecognized billing data")
	}
	billing = parseBillingFields(billingFields)

	// Extract subscription usage. The pattern is:
	// rollingUsage:$R[N]={status:"ok",resetInSec:N,usagePercent:N},...
	// Note: field order varies; use independent extractions.
	rollingBlockRE := regexp.MustCompile(`rollingUsage:\$R\[\d+\]=\{([^}]+)\}`)
	if matches := rollingBlockRE.FindStringSubmatch(scriptMatch); len(matches) > 1 {
		block := matches[1]
		if v, ok := extractFloatFieldOK(block, "usagePercent"); ok {
			subscription.RollingUsagePct = v
			subscription.RollingUsageOK = true
		}
		if v := extractFloatField(block, "resetInSec"); v > 0 {
			subscription.RollingResetSec = int(v)
		}
	}

	weeklyBlockRE := regexp.MustCompile(`weeklyUsage:\$R\[\d+\]=\{([^}]+)\}`)
	if matches := weeklyBlockRE.FindStringSubmatch(scriptMatch); len(matches) > 1 {
		block := matches[1]
		if v, ok := extractFloatFieldOK(block, "usagePercent"); ok {
			subscription.WeeklyUsagePct = v
			subscription.WeeklyUsageOK = true
		}
		if v := extractFloatField(block, "resetInSec"); v > 0 {
			subscription.WeeklyResetSec = int(v)
		}
	}

	monthlyBlockRE := regexp.MustCompile(`monthlyUsage:\$R\[\d+\]=\{([^}]+)\}`)
	if matches := monthlyBlockRE.FindStringSubmatch(scriptMatch); len(matches) > 1 {
		block := matches[1]
		if v, ok := extractFloatFieldOK(block, "usagePercent"); ok {
			subscription.MonthlyUsagePct = v
			subscription.MonthlyUsageOK = true
		}
		if v := extractFloatField(block, "resetInSec"); v > 0 {
			subscription.MonthlyResetSec = int(v)
		}
	}

	return subscription, billing, nil
}

func hasRecognizedBillingField(fields string) bool {
	for _, key := range []string{
		"balance",
		"reloadAmount",
		"reloadTrigger",
		"monthlyLimit",
		"monthlyUsage",
		"customerID",
		"paymentMethodType",
		"subscriptionID",
		"subscriptionPlan",
	} {
		if strings.Contains(fields, key+":") {
			return true
		}
	}
	return false
}

// parseBillingFields extracts billing info from a Seroval object fragment
// like: customerID:"cus_xxx",balance:0,reloadAmount:20,...
func parseBillingFields(fields string) BillingInfo {
	b := BillingInfo{}
	b.CustomerID = extractStringField(fields, "customerID")
	b.PaymentMethodType = extractStringField(fields, "paymentMethodType")
	b.SubscriptionPlan = extractStringField(fields, "subscriptionPlan")

	if v := extractFloatField(fields, "balance"); v != 0 {
		b.Balance = v
	}
	if v := extractFloatField(fields, "reloadAmount"); v != 0 {
		b.ReloadAmount = v
	}
	if v := extractFloatField(fields, "reloadTrigger"); v != 0 {
		b.ReloadTrigger = v
	}

	if v := extractNullableFloat(fields, "monthlyLimit"); v != nil {
		b.MonthlyLimit = v
	}
	if v := extractNullableFloat(fields, "monthlyUsage"); v != nil {
		b.MonthlyUsage = *v
	}

	if strings.Contains(fields, "subscriptionID:") && !strings.Contains(fields, "subscriptionID:null") {
		b.HasSubscription = true
	}

	return b
}

// extractStringField pulls a string value from a Seroval object fragment.
// Handles: field:"value" and field:null
func extractStringField(fields, key string) string {
	re := regexp.MustCompile(key + `:"([^"]*)"`)
	if matches := re.FindStringSubmatch(fields); len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// extractFloatFieldOK pulls a numeric value from a Seroval object fragment,
// reporting via ok whether the field was present at all — distinguishing
// "field:0" (found, value 0) from "field absent" (not found).
func extractFloatFieldOK(fields, key string) (float64, bool) {
	re := regexp.MustCompile(key + `:(-?[0-9]+(?:\.[0-9]+)?)`)
	matches := re.FindStringSubmatch(fields)
	if len(matches) < 2 {
		return 0, false
	}
	f, _ := strconv.ParseFloat(matches[1], 64)
	return f, true
}

// extractFloatField pulls a numeric value from a Seroval object fragment.
// Handles: field:123, field:0, field:1.5
func extractFloatField(fields, key string) float64 {
	re := regexp.MustCompile(key + `:(-?[0-9]+(?:\.[0-9]+)?)`)
	if matches := re.FindStringSubmatch(fields); len(matches) > 1 {
		f, _ := strconv.ParseFloat(matches[1], 64)
		return f
	}
	return 0
}

// extractNullableFloat pulls a nullable numeric value from a Seroval fragment.
// Returns nil for null, pointer to value otherwise.
func extractNullableFloat(fields, key string) *float64 {
	re := regexp.MustCompile(key + `:(null|-?[0-9]+(?:\.[0-9]+)?)`)
	if matches := re.FindStringSubmatch(fields); len(matches) > 1 {
		if matches[1] == "null" {
			return nil
		}
		f, _ := strconv.ParseFloat(matches[1], 64)
		return &f
	}
	return nil
}

func shortenBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// ConsoleAuthError is returned when the OpenCode console rejects our cookie
// (401/403). Callers treat this as "session expired — user needs to re-login
// in the browser" and surface AUTH on the tile.
type ConsoleAuthError struct {
	StatusCode int
	Body       string
}

func (e *ConsoleAuthError) Error() string {
	return fmt.Sprintf("opencode console auth failed: HTTP %d (%s)", e.StatusCode, e.Body)
}

// BillingInfo is the parsed shape of a queryBillingInfo response. Field names
// mirror the wire format so the parser → struct mapping is mechanical.
type BillingInfo struct {
	CustomerID         string
	PaymentMethodID    string
	PaymentMethodType  string
	PaymentMethodLast4 string
	Balance            float64 // in cents per OpenCode's persistence (formatBalance divides by 1e8 in their UI)
	MonthlyLimit       *float64
	MonthlyUsage       float64
	ReloadAmount       float64
	ReloadTrigger      float64
	SubscriptionPlan   string
	HasSubscription    bool
}

// QueryBillingInfo returns the user's billing state. Does not trigger any
// mutation server-side; safe to poll.
func (c *ConsoleClient) QueryBillingInfo(ctx context.Context) (BillingInfo, error) {
	if c.WorkspaceID == "" {
		return BillingInfo{}, errors.New("console: workspace ID required")
	}
	body, err := c.callGET(ctx, rpcBillingInfoID, c.WorkspaceID)
	if err != nil {
		return BillingInfo{}, err
	}
	parsed, err := ParseSeroval(body)
	if err != nil {
		return BillingInfo{}, err
	}
	return billingInfoFromMap(parsed)
}

func billingInfoFromMap(parsed any) (BillingInfo, error) {
	m, ok := parsed.(map[string]any)
	if !ok {
		return BillingInfo{}, fmt.Errorf("console: billing response not an object: %T", parsed)
	}
	out := BillingInfo{}
	out.CustomerID = stringField(m, "customerID")
	out.PaymentMethodID = stringField(m, "paymentMethodID")
	out.PaymentMethodType = stringField(m, "paymentMethodType")
	out.PaymentMethodLast4 = stringField(m, "paymentMethodLast4")
	out.Balance = floatField(m, "balance")
	out.MonthlyUsage = floatField(m, "monthlyUsage")
	out.ReloadAmount = floatField(m, "reloadAmount")
	out.ReloadTrigger = floatField(m, "reloadTrigger")
	out.SubscriptionPlan = stringField(m, "subscriptionPlan")
	if v, ok := m["subscriptionID"]; ok && v != nil {
		out.HasSubscription = true
	}
	if v, ok := m["monthlyLimit"]; ok {
		if f, ok := v.(float64); ok {
			out.MonthlyLimit = &f
		}
	}
	return out, nil
}

// stringField pulls a string out of a parsed map, returning "" for nil /
// missing / non-string. Tolerant by design — OpenCode populates many
// fields as null on fresh accounts and we'd rather show empty than crash.
func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// floatField pulls a number out of a parsed map. Returns 0 for nil /
// missing / non-numeric. JSON-unmarshalled numbers always come back as
// float64.
func floatField(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}
