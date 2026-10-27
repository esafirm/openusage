package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

// TestMain neutralizes the stored-session lookup for the whole package. Left at
// its default it reads the developer's real credentials file, so a machine with
// a connected OpenCode account would drive console enrichment against the live
// service instead of the test server. Tests that exercise console enrichment
// override this seam with a session of their own.
const (
	testConsoleCookieName = "__Host-console_session"
	testConsoleDomain     = "opencode.ai"
)

func TestMain(m *testing.M) {
	loadStoredSession = func(string) (config.BrowserSession, bool, error) {
		return config.BrowserSession{}, false, nil
	}
	os.Exit(m.Run())
}

func zenModelsBody() string {
	return `{
		"object": "list",
		"data": [
			{"id": "minimax-m2.7", "object": "model", "created": 1, "owned_by": "opencode"},
			{"id": "kimi-k2.6",   "object": "model", "created": 1, "owned_by": "opencode"},
			{"id": "glm-5",       "object": "model", "created": 1, "owned_by": "opencode"}
		]
	}`
}

func startFakeZen(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	if status == 0 {
		status = http.StatusOK
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != modelsPath {
			http.NotFound(w, r)
			return
		}
		// Verify the request carries Bearer auth — the provider would lose its
		// reason for existing if it forgot to attach it.
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "missing bearer", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func newAcct(t *testing.T, baseURL string) core.AccountConfig {
	t.Helper()
	t.Setenv("TEST_OPENCODE_KEY", "sk-zen-test-1234567890")
	return core.AccountConfig{
		ID:        "opencode",
		Provider:  "opencode",
		APIKeyEnv: "TEST_OPENCODE_KEY",
		BaseURL:   baseURL,
	}
}

func TestFetch_Success_AuthOKExposesModels(t *testing.T) {
	server := startFakeZen(t, http.StatusOK, zenModelsBody())
	defer server.Close()

	snap, err := New().Fetch(context.Background(), newAcct(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %s (msg=%q), want OK", snap.Status, snap.Message)
	}
	if got := snap.Attributes["available_models_count"]; got != "3" {
		t.Errorf("available_models_count = %q, want 3", got)
	}
	if got := snap.Attributes["available_models"]; !strings.Contains(got, "minimax-m2.7") || !strings.Contains(got, "glm-5") {
		t.Errorf("available_models missing expected ids: %q", got)
	}
	if got := snap.Attributes["auth_scope"]; got != "zen" {
		t.Errorf("auth_scope = %q, want zen", got)
	}
	if !strings.Contains(snap.Message, "3") {
		t.Errorf("message should reference the model count: %q", snap.Message)
	}
}

func TestFetch_AuthRequired_NoKey(t *testing.T) {
	acct := core.AccountConfig{
		ID:        "opencode",
		Provider:  "opencode",
		APIKeyEnv: "TEST_OPENCODE_MISSING",
	}
	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("status = %s, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_AuthFailed_401(t *testing.T) {
	server := startFakeZen(t, http.StatusUnauthorized, `{"error":"unauthorized"}`)
	defer server.Close()

	snap, err := New().Fetch(context.Background(), newAcct(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("status = %s, want AUTH on 401", snap.Status)
	}
	if !strings.Contains(snap.Message, "401") {
		t.Errorf("message = %q, expected to mention 401", snap.Message)
	}
}

func TestFetch_RateLimited_429(t *testing.T) {
	server := startFakeZen(t, http.StatusTooManyRequests, `{}`)
	defer server.Close()

	snap, err := New().Fetch(context.Background(), newAcct(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusLimited {
		t.Errorf("status = %s, want LIMITED on 429", snap.Status)
	}
}

func TestFetch_ConsoleEnrichmentAutoDiscoversWorkspaceID(t *testing.T) {
	origLoadStoredSession := loadStoredSession
	origNewConsoleClient := newConsoleClient
	t.Cleanup(func() {
		loadStoredSession = origLoadStoredSession
		newConsoleClient = origNewConsoleClient
	})

	// enrichFromConsole calls loadStoredSession, a pure credentials-file
	// read with no browser refresh.
	loadStoredSession = func(accountID string) (config.BrowserSession, bool, error) {
		return config.BrowserSession{
			Value:         "test-cookie-value",
			CookieName:    testConsoleCookieName,
			SourceBrowser: "firefox",
		}, true, nil
	}

	var billingOrgID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == modelsPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(zenModelsBody()))
		case r.URL.Path == "/console/api/user/workspaces":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"org_DISCOVERED","name":"Personal","role":"owner"}]`))
		case r.URL.Path == "/console/api/billing/status":
			billingOrgID = r.Header.Get("x-org-id")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"billingMode":"prepaid","mode":"pay-as-you-go","balanceMicroCents":"972127265","availableMicroCents":"972127265"}`))
		case r.URL.Path == "/console/api/usage/summary":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"totalRequests":"138","totalCostMicroCents":"27872735"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	newConsoleClient = func(cookieValue, cookieName, workspaceID string) *ConsoleClient {
		client := NewConsoleClient(cookieValue, cookieName, workspaceID)
		client.baseURL = server.URL
		return client
	}

	acct := newAcct(t, server.URL)
	acct.BrowserCookie = &core.BrowserCookieRef{
		Domain:        testConsoleDomain,
		CookieName:    testConsoleCookieName,
		SourceBrowser: "firefox",
	}

	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %s (msg=%q), want OK", snap.Status, snap.Message)
	}
	if got := snap.Attributes["auth_scope"]; got != "zen+console" {
		t.Fatalf("auth_scope = %q, want zen+console", got)
	}
	balance, ok := snap.Metrics["console_balance"]
	if !ok || balance.Remaining == nil {
		t.Fatal("console_balance metric missing after workspace auto-discovery")
	}
	if got := *balance.Remaining; got < 9.72 || got > 9.73 {
		t.Fatalf("console_balance = %v, want ~9.72", got)
	}
	if _, ok := snap.Metrics["monthly_usage"]; !ok {
		t.Fatal("monthly_usage metric missing after usage summary")
	}
	if billingOrgID != "org_DISCOVERED" {
		t.Fatalf("x-org-id = %q, want org_DISCOVERED", billingOrgID)
	}
	if _, ok := snap.Diagnostics["opencode_console_workspace_error"]; ok {
		t.Fatalf("unexpected workspace discovery diagnostic: %+v", snap.Diagnostics)
	}
}

// TestFetch_BrowserSessionOnlyNoAPIKey_ConsoleFailureSurfacesAuthNotOK covers
// an account configured for browser-session auth only (no Zen API key) whose
// stored session is missing/expired: previously the Zen probe was skipped
// (no key to probe with) and enrichFromConsole's failure was silently
// swallowed, leaving snap.Status empty and shared.FinalizeStatus defaulting
// it to a false StatusOK. It must now surface the underlying auth-required
// status instead.
func TestFetch_BrowserSessionOnlyNoAPIKey_ConsoleFailureSurfacesAuthNotOK(t *testing.T) {
	origLoadStoredSession := loadStoredSession
	t.Cleanup(func() { loadStoredSession = origLoadStoredSession })

	loadStoredSession = func(accountID string) (config.BrowserSession, bool, error) {
		return config.BrowserSession{}, false, nil
	}

	acct := core.AccountConfig{
		ID:        "opencode-personal",
		Provider:  "opencode",
		APIKeyEnv: "TEST_OPENCODE_MISSING_FOR_BROWSER_ONLY",
		BrowserCookie: &core.BrowserCookieRef{
			Domain:        testConsoleDomain,
			CookieName:    testConsoleCookieName,
			SourceBrowser: "safari",
		},
	}

	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status == core.StatusOK {
		t.Fatalf("status = OK, want a non-OK status — account has neither a valid API key nor a working browser session (msg=%q)", snap.Message)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("status = %s, want AUTH_REQUIRED", snap.Status)
	}
}

// TestFetch_ConsoleDoubleFailure_DoesNotFabricateZeroBalance covers the case
// where both the HTML usage-page scrape and the billing-RPC fallback fail
// (e.g. an expired session cookie). Previously both errors were discarded
// and a zero-valued billing struct was written into the snapshot as if it
// were real data ("$0.00 balance"). It must now surface the failure instead.
func TestFetch_ConsoleDoubleFailure_DoesNotFabricateZeroBalance(t *testing.T) {
	origLoadStoredSession := loadStoredSession
	origNewConsoleClient := newConsoleClient
	t.Cleanup(func() {
		loadStoredSession = origLoadStoredSession
		newConsoleClient = origNewConsoleClient
	})

	loadStoredSession = func(accountID string) (config.BrowserSession, bool, error) {
		return config.BrowserSession{
			Value:         "test-cookie-value",
			CookieName:    testConsoleCookieName,
			SourceBrowser: "firefox",
		}, true, nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Both the go-usage-page fetch and the billing RPC fallback come
		// back unauthorized, simulating an expired session cookie.
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	newConsoleClient = func(cookieValue, cookieName, workspaceID string) *ConsoleClient {
		client := NewConsoleClient(cookieValue, cookieName, workspaceID)
		client.baseURL = server.URL
		return client
	}

	acct := core.AccountConfig{
		ID:        "opencode-personal",
		Provider:  "opencode",
		APIKeyEnv: "TEST_OPENCODE_MISSING_FOR_DOUBLE_FAILURE",
		BrowserCookie: &core.BrowserCookieRef{
			Domain:        testConsoleDomain,
			CookieName:    testConsoleCookieName,
			SourceBrowser: "firefox",
		},
	}
	acct.SetHint("opencode_workspace_id", "wrk_test")

	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if _, ok := snap.Metrics["console_balance"]; ok {
		t.Fatalf("console_balance metric present despite both page scrape and billing fallback failing: %+v", snap.Metrics["console_balance"])
	}
	if snap.Status == core.StatusOK {
		t.Fatalf("status = OK, want a non-OK status when console enrichment fully failed (msg=%q)", snap.Message)
	}
	if _, ok := snap.Diagnostics["opencode_console_auth_error"]; !ok {
		t.Errorf("expected opencode_console_auth_error diagnostic, got diagnostics=%+v", snap.Diagnostics)
	}
}

func TestFetch_ConsoleBillingError_DoesNotFabricateZeroBalance(t *testing.T) {
	origLoadStoredSession := loadStoredSession
	origNewConsoleClient := newConsoleClient
	t.Cleanup(func() {
		loadStoredSession = origLoadStoredSession
		newConsoleClient = origNewConsoleClient
	})

	loadStoredSession = func(string) (config.BrowserSession, bool, error) {
		return config.BrowserSession{
			Value:         "test-cookie-value",
			CookieName:    testConsoleCookieName,
			SourceBrowser: "firefox",
		}, true, nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/console/api/billing/status" {
			http.Error(w, "billing backend unavailable", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	newConsoleClient = func(cookieValue, cookieName, workspaceID string) *ConsoleClient {
		client := NewConsoleClient(cookieValue, cookieName, workspaceID)
		client.baseURL = server.URL
		return client
	}

	acct := core.AccountConfig{
		ID:        "opencode-billing-error",
		Provider:  "opencode",
		APIKeyEnv: "TEST_OPENCODE_MISSING_BILLING",
		BrowserCookie: &core.BrowserCookieRef{
			Domain:        testConsoleDomain,
			CookieName:    testConsoleCookieName,
			SourceBrowser: "firefox",
		},
	}
	acct.SetHint("opencode_workspace_id", "org_test")

	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Fatalf("status = %s, want AUTH for billing failure (msg=%q)", snap.Status, snap.Message)
	}
	if _, ok := snap.Metrics["console_balance"]; ok {
		t.Fatalf("console_balance metric present for billing failure: %+v", snap.Metrics["console_balance"])
	}
	if snap.Raw["console_error"] == "" {
		t.Errorf("expected console_error diagnostic for billing failure, got raw=%v", snap.Raw)
	}
}

func TestFetch_MergesPrimaryAndGoConsoleSessions(t *testing.T) {
	origLoadStoredSession := loadStoredSession
	origNewConsoleClient := newConsoleClient
	t.Cleanup(func() {
		loadStoredSession = origLoadStoredSession
		newConsoleClient = origNewConsoleClient
	})

	loadStoredSession = func(accountID string) (config.BrowserSession, bool, error) {
		switch accountID {
		case "opencode":
			return config.BrowserSession{Value: "primary-cookie", CookieName: testConsoleCookieName, SourceBrowser: "chrome"}, true, nil
		case "opencode-go":
			return config.BrowserSession{Value: "go-cookie", CookieName: testConsoleCookieName, SourceBrowser: "chrome"}, true, nil
		default:
			return config.BrowserSession{}, false, nil
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		org := r.Header.Get("x-org-id")
		switch r.URL.Path {
		case modelsPath:
			_, _ = w.Write([]byte(zenModelsBody()))
		case "/console/api/billing/status":
			if org == "org_primary" {
				_, _ = w.Write([]byte(`{"billingMode":"prepaid","mode":"pay-as-you-go","balanceMicroCents":"972127265","availableMicroCents":"972127265"}`))
				return
			}
			_, _ = w.Write([]byte(`{"billingMode":"prepaid","mode":"pay-as-you-go","balanceMicroCents":"194561281","availableMicroCents":"194561281"}`))
		case "/console/api/usage/summary":
			_, _ = w.Write([]byte(`{"totalCostMicroCents":"27872735"}`))
		case "/console/api/go/status":
			if org == "wrk_go" {
				_, _ = w.Write([]byte(`{"product":"go","useBalance":false,"cancelAtPeriodEnd":true,"access":{"endsAt":"2026-10-30T21:28:17.000Z","meters":{"fiveHour":{"resetsAt":"2026-10-27T03:52:26.000Z","limitMicroCents":"1200000000","usedMicroCents":"97608179"},"week":{"resetsAt":"2026-11-02T00:00:00.000Z","limitMicroCents":"3000000000","usedMicroCents":"321442418"},"month":{"resetsAt":"2026-10-30T21:28:17.000Z","limitMicroCents":"6000000000","usedMicroCents":"4245278075"}}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"access":null}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	newConsoleClient = func(cookieValue, cookieName, workspaceID string) *ConsoleClient {
		client := NewConsoleClient(cookieValue, cookieName, workspaceID)
		client.baseURL = server.URL
		return client
	}

	acct := newAcct(t, server.URL)
	acct.SetHint("opencode_workspace_id", "org_primary")
	acct.SetHint("opencode_go_workspace_id", "wrk_go")

	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	bal, ok := snap.Metrics["console_balance"]
	if !ok || bal.Remaining == nil {
		t.Fatal("console_balance missing")
	}
	if got := *bal.Remaining; got != 9.72127265 {
		t.Fatalf("console_balance = %v, want primary balance 9.72127265", got)
	}
	for key, want := range map[string]float64{
		"rolling_usage":     8.133,
		"weekly_usage":      10.714,
		"monthly_usage_pct": 70.754,
	} {
		if got := metricUsed(snap.Metrics, key); got < want-0.01 || got > want+0.01 {
			t.Errorf("%s = %.3f, want %.3f", key, got, want)
		}
	}
	if got := snap.Attributes["go_product"]; got != "go" {
		t.Errorf("go_product = %q, want go", got)
	}
}

func metricUsed(metrics map[string]core.Metric, key string) float64 {
	m, ok := metrics[key]
	if !ok || m.Used == nil {
		return 0
	}
	return *m.Used
}
