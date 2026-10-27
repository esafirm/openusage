package opencode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

var newConsoleClient = NewConsoleClient

// OpenCode Zen exposes only OpenAI-compatible chat/messages/models endpoints
// behind its API-key auth (verified via reverse-engineering against the
// upstream source at github.com/anomalyco/opencode). Billing, usage history,
// and key management live behind session-cookie SolidStart RPCs that this
// provider does not (yet) authenticate against — those would need a separate
// cookie-based code path.
//
// As a result, the only signal we get from a poll is "is this key valid?".
// Tile metrics (token spend, model burn, project breakdown, tool usage,
// activity totals) come from the OpenCode telemetry plugin and flow in via
// the telemetry pipeline once an account with provider_id=opencode exists.
const (
	defaultBaseURL = "https://opencode.ai"
	modelsPath     = "/zen/v1/models"
)

type Provider struct {
	providerbase.Base
	telemetryCacheMu        sync.Mutex
	telemetryDBFingerprints map[string]string
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "opencode",
			Info: core.ProviderInfo{
				Name:         "OpenCode",
				Capabilities: []string{"zen_models_endpoint", "telemetry_driven_metrics", "console_billing", "subscription_usage"},
				DocURL:       "https://opencode.ai/docs/",
			},
			Auth: core.ProviderAuthSpec{
				Type:                core.ProviderAuthTypeAPIKey,
				APIKeyEnv:           "OPENCODE_API_KEY",
				DefaultAccountID:    "opencode",
				SupplementalTypes:   []core.ProviderAuthType{core.ProviderAuthTypeBrowserSession},
				BrowserCookieDomain: "opencode.ai",
				BrowserCookieName:   "__Host-console_session",
				BrowserConsoleURL:   "https://opencode.ai/console",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Set OPENCODE_API_KEY (or ZEN_API_KEY) with your OpenCode Zen key for chat-surface auth.",
					"For OpenCode Go balance / monthly usage / subscription data: open Settings → 5 KEYS, highlight opencode, and press c to import the session cookie from your browser.",
					"Tile spend / model / activity metrics are populated from the OpenCode telemetry plugin; see Settings → 7 INTEG.",
				},
			},
			// OpenCode is a coding tool whose spend/model/client/tool data
			// comes from the telemetry plugin, so it uses the coding-tool
			// preset (client, tool, language and code-stats sections, with
			// raw model_/client_/tool_/lang_ metrics folded into those
			// sections instead of dumped as "Other Data").
			Detail: core.CodingToolDetailWidget(true),
			Dashboard: providerbase.CodingToolDashboard(
				providerbase.WithColorRole(core.DashboardColorRoleBlue),
				providerbase.WithSectionOrder(
					core.DashboardSectionHeader,
					core.DashboardSectionTopUsageProgress,
					core.DashboardSectionModelBurn,
					core.DashboardSectionProviderBurn,
					core.DashboardSectionClientBurn,
					core.DashboardSectionProjectBreakdown,
					core.DashboardSectionToolUsage,
					core.DashboardSectionMCPUsage,
					core.DashboardSectionLanguageBurn,
					core.DashboardSectionCodeStats,
					core.DashboardSectionOtherData,
				),
				providerbase.WithHideMetricPrefixes("project_", "provider_", "window_"),
				// 7d_tool_calls is projected over the selected window (not a
				// fixed 7 days) and duplicates tool_calls_today; the window_*
				// figures are already the tile's hero line.
				providerbase.WithHideMetricKeys("7d_tool_calls"),
				providerbase.WithGaugePriority("rolling_usage", "weekly_usage", "monthly_usage_pct"),
				// OpenCode Go quota has three meaningful usage-window
				// percentages (5h / 7d / ~30d monthly) — the default cap of 2
				// gauge lines would always bump the monthly figure in favor
				// of the two shorter windows.
				providerbase.WithGaugeMaxLines(3),
				providerbase.WithCompactRows(
					core.DashboardCompactRow{
						Label:       "Quota",
						Keys:        []string{"rolling_usage", "weekly_usage", "monthly_usage_pct"},
						MaxSegments: 3,
					},
					core.DashboardCompactRow{
						Label:       "Credits",
						Keys:        []string{"console_balance", "monthly_usage", "monthly_limit"},
						MaxSegments: 3,
					},
				),
				providerbase.WithMetricLabels(map[string]string{
					"rolling_usage":     "5h Usage",
					"weekly_usage":      "Weekly",
					"monthly_usage_pct": "Monthly",
					"console_balance":   "Balance",
					"monthly_usage":     "Month Spend",
					"monthly_limit":     "Month Limit",
					"reload_amount":     "Reload",
					"reload_trigger":    "Reload At",
				}),
				providerbase.WithCompactLabels(map[string]string{
					"rolling_usage":     "5h",
					"weekly_usage":      "wk",
					"monthly_usage_pct": "mo",
					"console_balance":   "bal",
					"monthly_usage":     "mo$",
					"monthly_limit":     "cap",
				}),
			),
		}),
	}
}

type modelsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey, authSnap := shared.RequireAPIKey(acct, p.ID())
	hasBrowserSession := acct.BrowserCookie != nil

	// If no API key and no browser session configured, require auth.
	if authSnap != nil && !hasBrowserSession {
		return *authSnap, nil
	}

	baseURL := shared.ResolveBaseURL(acct, defaultBaseURL)
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.SetAttribute("auth_scope", "zen")
	snap.SetAttribute("api_base_url", baseURL)

	// Try Zen API key probe if we have a key.
	if authSnap == nil {
		var models modelsResponse
		statusCode, _, err := shared.FetchJSON(ctx, baseURL+modelsPath, apiKey, &models, p.Client())
		if err != nil {
			switch statusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				snap.Status = core.StatusAuth
				snap.Message = fmt.Sprintf("HTTP %d – check OPENCODE_API_KEY", statusCode)
				return snap, nil
			case http.StatusTooManyRequests:
				snap.Status = core.StatusLimited
				snap.Message = "rate limited (HTTP 429)"
				return snap, nil
			}
			// If we have a browser session, don't fail on Zen API errors —
			// the console enrichment can still provide useful data.
			if !hasBrowserSession {
				return snap, fmt.Errorf("opencode zen models: %w", err)
			}
		}

		if len(models.Data) > 0 {
			ids := make([]string, 0, len(models.Data))
			for _, m := range models.Data {
				if id := strings.TrimSpace(m.ID); id != "" {
					ids = append(ids, id)
				}
			}
			snap.SetAttribute("available_models", strings.Join(ids, ", "))
			snap.SetAttribute("available_models_count", fmt.Sprintf("%d", len(ids)))
		}
	}

	// Optional: enrich the snapshot with console-side data (balance,
	// monthly usage, subscription) when a browser-session cookie is
	// configured for this account. Failures are non-fatal when we already
	// have a validated Zen API key — the snapshot is in a good state, we
	// just skip the enrichment and surface a hint.
	if err := p.enrichFromConsole(ctx, acct, &snap); err != nil {
		// Distinguish "no cookie configured" (silent) from "cookie
		// rejected" (loud diagnostic for the tile).
		var authErr *ConsoleAuthError
		switch {
		case errors.As(err, &authErr):
			snap.SetDiagnostic("opencode_console_auth_error", authErr.Error())
			snap.Raw["console_auth_status"] = fmt.Sprintf("%d", authErr.StatusCode)
		case errors.Is(err, errNoCookieConfigured):
			// expected when user hasn't connected a browser session
		default:
			snap.Raw["console_error"] = err.Error()
		}

		// We never validated a Zen API key (authSnap != nil, so the probe
		// above was skipped) and console enrichment also failed to produce
		// usable data. Without this, shared.FinalizeStatus would default
		// the still-empty snap.Status to StatusOK — reporting a healthy
		// tile for an account that has neither a working API key nor a
		// working browser session.
		if authSnap != nil && snap.Status == "" {
			snap.Status = authSnap.Status
			if authSnap.Message != "" {
				snap.Message = authSnap.Message
			}
		}
	}

	// Quota meters (OpenCode Go 5h / weekly / monthly) and the Zen balance
	// only come from the console. When they are missing, tell the user how
	// to connect instead of rendering empty meters.
	if _, hasQuota := snap.Metrics["rolling_usage"]; !hasQuota {
		if _, hasBalance := snap.Metrics["console_balance"]; !hasBalance {
			snap.SetAttribute(core.QuotaConnectHintAttribute, consoleConnectHint(acct, snap))
		}
	}
	if plan := strings.TrimSpace(acct.Hint("opencode_plan", "")); plan != "" {
		snap.SetAttribute("opencode_plan", plan)
	}

	shared.FinalizeStatus(&snap)
	if snap.Status == core.StatusOK {
		modelCount := snap.Attributes["available_models_count"]
		if bal, ok := snap.Metrics["console_balance"]; ok && bal.Remaining != nil {
			msg := fmt.Sprintf("$%.2f balance", *bal.Remaining)
			if modelCount != "" {
				msg += fmt.Sprintf(" · %s Zen models", modelCount)
			}
			if rolling, ok := snap.Metrics["rolling_usage"]; ok && rolling.Used != nil {
				msg += fmt.Sprintf(" · %.0f%% 5h", *rolling.Used)
			}
			if weekly, ok := snap.Metrics["weekly_usage"]; ok && weekly.Used != nil {
				msg += fmt.Sprintf(" · %.0f%% weekly", *weekly.Used)
			}
			snap.Message = msg
		} else if modelCount != "" {
			snap.Message = fmt.Sprintf("Auth OK · %s Zen models", modelCount)
		} else {
			snap.Message = "Auth OK"
		}
	}
	return snap, nil
}

var errNoCookieConfigured = errors.New("opencode: no browser session configured")

// consoleConnectHint is the one-line tile hint shown when no console data is
// available. An expired/rejected session gets a reconnect hint instead.
func consoleConnectHint(acct core.AccountConfig, snap core.UsageSnapshot) string {
	const how = "log into opencode.ai, then Settings → 5 KEYS → opencode → c"
	if _, rejected := snap.Raw["console_auth_status"]; rejected {
		return "OpenCode console session expired: " + how
	}
	if strings.EqualFold(acct.Hint("opencode_plan", ""), "go") {
		return "Connect OpenCode console for Go quota meters: " + how
	}
	return "Connect OpenCode console for quota/balance: " + how
}

// loadStoredSession reads a browser session directly from the credentials file
// without refreshing from the browser. This avoids the destructive refresh in
// LoadOrRefreshBrowserSession that overwrites stored sessions when multiple
// accounts use different browsers for the same domain.
//
// A package-level var (not a plain func) so tests can stub it — mirrors the
// newConsoleClient seam above.
var loadStoredSession = func(accountID string) (config.BrowserSession, bool, error) {
	return config.LoadSession(accountID)
}

// enrichFromConsole merges the account's console sessions into the snapshot.
//
// Two sessions are supported:
//   - sessions[accountID]        the primary (Zen balance + 30d spend)
//   - sessions[accountID+"-go"]  the OpenCode Go account (5h/weekly/monthly
//     meters). A user can have different accounts in different browser
//     profiles, so this is a separate credential rather than a second account
//     on the dashboard.
//
// Returns errNoCookieConfigured when neither session is stored.
func (p *Provider) enrichFromConsole(ctx context.Context, acct core.AccountConfig, snap *core.UsageSnapshot) error {
	type consoleSession struct {
		session config.BrowserSession
		primary bool
		hintKey string
	}

	var sessions []consoleSession
	if sess, ok, err := loadStoredSession(acct.ID); err == nil && ok && sess.Value != "" {
		sessions = append(sessions, consoleSession{session: sess, primary: true, hintKey: "opencode_workspace_id"})
	}
	if sess, ok, err := loadStoredSession(acct.ID + "-go"); err == nil && ok && sess.Value != "" {
		sessions = append(sessions, consoleSession{session: sess, primary: false, hintKey: "opencode_go_workspace_id"})
	}
	if len(sessions) == 0 {
		return errNoCookieConfigured
	}

	var lastErr error
	for _, item := range sessions {
		if err := p.enrichConsoleSession(ctx, acct, snap, item.session, item.primary, item.hintKey); err != nil {
			lastErr = err
			key := "opencode_console_error"
			if !item.primary {
				key = "opencode_go_console_error"
			}
			snap.SetDiagnostic(key, err.Error())
			continue
		}
	}

	if lastErr != nil && !hasConsoleData(snap) {
		return lastErr
	}
	return nil
}

func (p *Provider) enrichConsoleSession(
	ctx context.Context,
	acct core.AccountConfig,
	snap *core.UsageSnapshot,
	session config.BrowserSession,
	primary bool,
	hintKey string,
) error {
	client := newConsoleClient(session.Value, session.CookieName, "")
	workspaceID := strings.TrimSpace(acct.Hint(hintKey, ""))
	if workspaceID == "" {
		discovered, err := client.DiscoverWorkspaceID(ctx)
		if err != nil {
			return err
		}
		workspaceID = discovered
	}
	client.WorkspaceID = workspaceID

	var errs []error
	billing, billingErr := client.FetchBillingStatus(ctx)
	if billingErr != nil {
		errs = append(errs, billingErr)
	} else {
		summary, summaryErr := client.FetchUsageSummary(ctx)
		if summaryErr != nil {
			snap.SetDiagnostic("opencode_console_usage_error", summaryErr.Error())
		}
		if _, exists := snap.Metrics["console_balance"]; !exists {
			applyConsoleBilling(snap, billing, summary, summaryErr == nil)
		}
		snap.SetAttribute("console_session_browser", session.SourceBrowser)
		snap.SetAttribute("auth_scope", "zen+console")
	}

	goStatus, goErr := client.FetchGoStatus(ctx)
	if goErr != nil {
		if !isConsoleNotFound(goErr) {
			errs = append(errs, goErr)
		}
	} else if goStatus.Access != nil {
		if _, exists := snap.Metrics["rolling_usage"]; !exists {
			applyGoStatus(snap, goStatus)
		}
		snap.SetAttribute("console_session_browser", session.SourceBrowser)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func hasConsoleData(snap *core.UsageSnapshot) bool {
	if _, ok := snap.Metrics["console_balance"]; ok {
		return true
	}
	_, ok := snap.Metrics["rolling_usage"]
	return ok
}

func isConsoleNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "http 404")
}

func applyConsoleBilling(snap *core.UsageSnapshot, billing consoleBillingStatus, summary consoleUsageSummary, hasSummary bool) {
	available := microCentsToUSD(billing.AvailableMicroCents)
	if billing.AvailableMicroCents == 0 {
		available = microCentsToUSD(billing.BalanceMicroCents)
	}
	snap.Metrics["console_balance"] = core.Metric{
		Remaining: core.Float64Ptr(available),
		Unit:      "USD",
		Window:    "current",
	}

	var monthUsage float64
	if hasSummary {
		monthUsage = microCentsToUSD(summary.TotalCostMicroCents)
		snap.Metrics["monthly_usage"] = core.Metric{
			Used:   core.Float64Ptr(monthUsage),
			Unit:   "USD",
			Window: "month",
		}
	}
	if billing.CreditLimitMicroCents != nil {
		limit := microCentsToUSD(*billing.CreditLimitMicroCents)
		snap.Metrics["monthly_limit"] = core.Metric{
			Limit:     core.Float64Ptr(limit),
			Used:      core.Float64Ptr(monthUsage),
			Remaining: core.Float64Ptr(limit - monthUsage),
			Unit:      "USD",
			Window:    "month",
		}
	}
	if billing.Mode != "" {
		snap.SetAttribute("billing_type", billing.Mode)
	}
	if billing.BillingMode != "" {
		snap.SetAttribute("billing_mode", billing.BillingMode)
	}
}

func applyGoStatus(snap *core.UsageSnapshot, status consoleGoStatus) {
	access := status.Access
	if access == nil {
		return
	}

	applyMeter := func(key string, meter consoleMeter, window, resetKey string) {
		if meter.LimitMicroCents <= 0 {
			return
		}
		pct := float64(meter.UsedMicroCents) / float64(meter.LimitMicroCents) * 100
		snap.Metrics[key] = core.Metric{
			Used:   core.Float64Ptr(pct),
			Limit:  core.Float64Ptr(100),
			Unit:   "percent",
			Window: window,
		}
		if resetsAt, err := time.Parse(time.RFC3339, meter.ResetsAt); err == nil {
			snap.Resets[resetKey] = resetsAt
		}
	}
	applyMeter("rolling_usage", access.Meters.FiveHour, "rolling-5h", "rolling_usage_reset")
	applyMeter("weekly_usage", access.Meters.Week, "7d", "weekly_usage_reset")
	applyMeter("monthly_usage_pct", access.Meters.Month, "month", "monthly_usage_pct_reset")

	snap.SetAttribute("go_product", status.Product)
	snap.SetAttribute("go_period_ends_at", access.EndsAt)
	snap.SetAttribute("go_cancel_at_period_end", strconv.FormatBool(status.CancelAtPeriodEnd))
	snap.SetAttribute("go_use_balance", strconv.FormatBool(status.UseBalance))
}
