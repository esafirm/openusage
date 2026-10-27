package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestConsoleClient_QueryBillingInfo_RoundTrip(t *testing.T) {
	billing := loadFixture(t, "seroval_c83b78a61468.txt")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the cookie made it through.
		c, err := r.Cookie("auth")
		if err != nil || c.Value != "test-cookie-value" {
			t.Errorf("auth cookie missing/wrong: %v %v", c, err)
		}
		// Verify the action ID is in the URL/headers.
		if got := r.URL.Query().Get("id"); got != rpcBillingInfoID {
			t.Errorf("id query = %q, want %s", got, rpcBillingInfoID)
		}
		if got := r.Header.Get("x-server-id"); got != rpcBillingInfoID {
			t.Errorf("x-server-id = %q", got)
		}
		// Verify the args payload includes the workspace ID.
		args := r.URL.Query().Get("args")
		if !strings.Contains(args, "wrk_TEST123") {
			t.Errorf("args missing workspace id: %q", args)
		}
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write(billing)
	}))
	defer server.Close()

	c := NewConsoleClient("test-cookie-value", "auth", "wrk_TEST123")
	c.baseURL = server.URL

	got, err := c.QueryBillingInfo(context.Background())
	if err != nil {
		t.Fatalf("QueryBillingInfo error: %v", err)
	}
	if got.Balance != 0 {
		t.Errorf("Balance = %v, want 0 (fresh acc)", got.Balance)
	}
	if got.ReloadAmount != 20 {
		t.Errorf("ReloadAmount = %v, want 20", got.ReloadAmount)
	}
	if got.ReloadTrigger != 5 {
		t.Errorf("ReloadTrigger = %v, want 5", got.ReloadTrigger)
	}
	if got.HasSubscription {
		t.Error("HasSubscription should be false on this fixture")
	}
}

func TestConsoleClient_AuthError_Surfaces401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("session expired"))
	}))
	defer server.Close()

	c := NewConsoleClient("expired-cookie", "auth", "wrk_X")
	c.baseURL = server.URL

	_, err := c.QueryBillingInfo(context.Background())
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	authErr, ok := err.(*ConsoleAuthError)
	if !ok {
		t.Fatalf("expected *ConsoleAuthError, got %T: %v", err, err)
	}
	if authErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", authErr.StatusCode)
	}
}

func TestConsoleClient_RequiresWorkspaceID(t *testing.T) {
	c := NewConsoleClient("v", "auth", "")
	if _, err := c.QueryBillingInfo(context.Background()); err == nil {
		t.Error("expected error when workspace id missing")
	}
}

func TestConsoleClient_RequiresCookie(t *testing.T) {
	c := NewConsoleClient("", "auth", "wrk_X")
	if _, err := c.QueryBillingInfo(context.Background()); err == nil {
		t.Error("expected error when cookie missing")
	}
}

func TestParseGoUsagePageHTML_ZeroUsagePercentIsNotDroppedAsMissing(t *testing.T) {
	html := `<html><script>self.$R=self.$R||[];` +
		`billing.get["wrk_x"]}=$R[1]=$R[2]($R[3]={balance:0,reloadAmount:2000000000,reloadTrigger:500000000});` +
		`rollingUsage:$R[4]={status:"ok",resetInSec:1800,usagePercent:0};` +
		`weeklyUsage:$R[5]={status:"ok",resetInSec:200000,usagePercent:12.5};` +
		`monthlyUsage:$R[6]={status:"ok",resetInSec:900000,usagePercent:0};` +
		`</script></html>`

	subscription, _, err := parseGoUsagePageHTML(html)
	if err != nil {
		t.Fatalf("parseGoUsagePageHTML error: %v", err)
	}

	if !subscription.RollingUsageOK {
		t.Fatalf("RollingUsageOK = false, want true (usagePercent:0 is a legitimate reading, not a missing field)")
	}
	if subscription.RollingUsagePct != 0 {
		t.Errorf("RollingUsagePct = %v, want 0", subscription.RollingUsagePct)
	}
	if !subscription.WeeklyUsageOK || subscription.WeeklyUsagePct != 12.5 {
		t.Errorf("WeeklyUsageOK/Pct = %v/%v, want true/12.5", subscription.WeeklyUsageOK, subscription.WeeklyUsagePct)
	}
	if !subscription.MonthlyUsageOK {
		t.Fatalf("MonthlyUsageOK = false, want true (usagePercent:0 is a legitimate reading, not a missing field)")
	}
	if subscription.MonthlyUsagePct != 0 {
		t.Errorf("MonthlyUsagePct = %v, want 0", subscription.MonthlyUsagePct)
	}
}

func TestParseGoUsagePageHTML_MissingBlockLeavesUsageOKFalse(t *testing.T) {
	html := `<html><script>self.$R=self.$R||[];` +
		`billing.get["wrk_x"]}=$R[1]=$R[2]($R[3]={balance:0,reloadAmount:0,reloadTrigger:0});` +
		`</script></html>`

	subscription, _, err := parseGoUsagePageHTML(html)
	if err != nil {
		t.Fatalf("parseGoUsagePageHTML error: %v", err)
	}

	if subscription.RollingUsageOK || subscription.WeeklyUsageOK || subscription.MonthlyUsageOK {
		t.Errorf("expected all *UsageOK flags false when no usage blocks are present, got rolling=%v weekly=%v monthly=%v",
			subscription.RollingUsageOK, subscription.WeeklyUsageOK, subscription.MonthlyUsageOK)
	}
}

func TestParseGoUsagePageHTML_ChangedShapeReturnsError(t *testing.T) {
	_, _, err := parseGoUsagePageHTML(`<html><script>self.$R=self.$R||[];` +
		`usagePage:{shape:"changed"}</script></html>`)
	if err == nil {
		t.Fatal("parseGoUsagePageHTML returned nil error for an unrecognized page shape")
	}
}

func TestFetchGoStatus_ParsesMeters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/console/api/go/status" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("x-org-id"); got != "wrk_TEST" {
			t.Errorf("x-org-id = %q, want wrk_TEST", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"product":"go","useBalance":false,"cancelAtPeriodEnd":true,
			"access":{
				"startsAt":"2026-09-30T21:28:17.000Z",
				"endsAt":"2026-10-30T21:28:17.000Z",
				"meters":{
					"fiveHour":{"resetsAt":"2026-10-27T03:52:26.000Z","limitMicroCents":"1200000000","usedMicroCents":"97608179"},
					"week":{"resetsAt":"2026-11-02T00:00:00.000Z","limitMicroCents":"3000000000","usedMicroCents":"321442418"},
					"month":{"resetsAt":"2026-10-30T21:28:17.000Z","limitMicroCents":"6000000000","usedMicroCents":"4245278075"}
				}
			}
		}`))
	}))
	defer server.Close()

	client := NewConsoleClient("cookie", "__Host-console_session", "wrk_TEST")
	client.baseURL = server.URL
	status, err := client.FetchGoStatus(context.Background())
	if err != nil {
		t.Fatalf("FetchGoStatus: %v", err)
	}
	if status.Access == nil {
		t.Fatal("access is nil")
	}
	if got := status.Access.Meters.FiveHour.UsedMicroCents; got != 97608179 {
		t.Fatalf("fiveHour used = %d, want 97608179", got)
	}

	snap := core.NewUsageSnapshot("opencode", "opencode")
	applyGoStatus(&snap, status)
	for key, want := range map[string]float64{
		"rolling_usage":     8.133,
		"weekly_usage":      10.714,
		"monthly_usage_pct": 70.754,
	} {
		m, ok := snap.Metrics[key]
		if !ok || m.Used == nil {
			t.Fatalf("%s missing", key)
		}
		if diff := *m.Used - want; diff > 0.01 || diff < -0.01 {
			t.Errorf("%s = %.3f, want %.3f", key, *m.Used, want)
		}
	}
	if _, ok := snap.Resets["rolling_usage_reset"]; !ok {
		t.Error("rolling_usage_reset missing")
	}
}
