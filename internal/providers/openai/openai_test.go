package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFetch_ProbesChatCompletionsAndParsesHeaders(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		w.Header().Set("x-ratelimit-limit-requests", "200")
		w.Header().Set("x-ratelimit-remaining-requests", "150")
		w.Header().Set("x-ratelimit-reset-requests", "30s")
		w.Header().Set("x-ratelimit-limit-tokens", "40000")
		w.Header().Set("x-ratelimit-remaining-tokens", "35000")
		w.Header().Set("x-ratelimit-reset-tokens", "30s")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": {"message": "invalid max_completion_tokens"}}`))
	}))
	defer server.Close()

	os.Setenv("TEST_OPENAI_KEY", "test-key")
	defer os.Unsetenv("TEST_OPENAI_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:         "test-openai",
		Provider:   "openai",
		APIKeyEnv:  "TEST_OPENAI_KEY",
		ProbeModel: "gpt-4.1-mini",
		BaseURL:    server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-key")
	}
	if gotBody["model"] != "gpt-4.1-mini" {
		t.Errorf("body model = %v, want gpt-4.1-mini", gotBody["model"])
	}
	if gotBody["max_completion_tokens"] != float64(0) {
		t.Errorf("max_completion_tokens = %v, want 0", gotBody["max_completion_tokens"])
	}
	if _, ok := gotBody["messages"]; !ok {
		t.Error("body is missing messages")
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK for the expected validation response", snap.Status)
	}

	rpm, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("missing rpm metric")
	}
	if rpm.Limit == nil || *rpm.Limit != 200 {
		t.Errorf("rpm limit = %v, want 200", rpm.Limit)
	}
	if rpm.Remaining == nil || *rpm.Remaining != 150 {
		t.Errorf("rpm remaining = %v, want 150", rpm.Remaining)
	}

	tpm, ok := snap.Metrics["tpm"]
	if !ok {
		t.Fatal("missing tpm metric")
	}
	if tpm.Limit == nil || *tpm.Limit != 40000 {
		t.Errorf("tpm limit = %v, want 40000", tpm.Limit)
	}
	if tpm.Remaining == nil || *tpm.Remaining != 35000 {
		t.Errorf("tpm remaining = %v, want 35000", tpm.Remaining)
	}
}

func TestFetch_AuthRequired(t *testing.T) {
	os.Unsetenv("TEST_OPENAI_MISSING")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-openai",
		Provider:  "openai",
		APIKeyEnv: "TEST_OPENAI_MISSING",
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %v, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-limit-requests", "200")
		w.Header().Set("x-ratelimit-remaining-requests", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error": "rate limited"}`))
	}))
	defer server.Close()

	os.Setenv("TEST_OPENAI_KEY", "test-key")
	defer os.Unsetenv("TEST_OPENAI_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-openai",
		Provider:  "openai",
		APIKeyEnv: "TEST_OPENAI_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusLimited {
		t.Errorf("Status = %v, want LIMITED", snap.Status)
	}
}

func TestFetch_ProbeModelNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": {"message": "model not found"}}`))
	}))
	defer server.Close()

	os.Setenv("TEST_OPENAI_KEY", "test-key")
	defer os.Unsetenv("TEST_OPENAI_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:         "test-openai",
		Provider:   "openai",
		APIKeyEnv:  "TEST_OPENAI_KEY",
		ProbeModel: "missing-model",
		BaseURL:    server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusError {
		t.Errorf("Status = %v, want ERROR", snap.Status)
	}
}

func TestFetch_OrgUsageMetrics(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAI_ADMIN_KEY", "admin-key")

	now := time.Now().UTC()
	todayStart := now.Truncate(24 * time.Hour).Unix()
	oldStart := now.Add(-48 * time.Hour).Unix()

	var orgCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/chat/completions":
			w.Header().Set("x-ratelimit-limit-requests", "100")
			w.Header().Set("x-ratelimit-remaining-requests", "90")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error": {"message": "expected"}}`))
		case "/organization/usage/completions":
			orgCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer admin-key" {
				t.Errorf("usage Authorization = %q", got)
			}
			_, _ = w.Write([]byte(`{"data":[` +
				`{"start_time":` + strconv.FormatInt(todayStart, 10) + `,"results":[{"input_tokens":100,"output_tokens":50,"input_cached_tokens":10,"num_model_requests":3}]},` +
				`{"start_time":` + strconv.FormatInt(oldStart, 10) + `,"results":[{"input_tokens":200,"output_tokens":100,"input_cached_tokens":20,"num_model_requests":4}]}` +
				`]}`))
		case "/organization/costs":
			orgCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer admin-key" {
				t.Errorf("costs Authorization = %q", got)
			}
			_, _ = w.Write([]byte(`{"data":[` +
				`{"start_time":` + strconv.FormatInt(todayStart, 10) + `,"results":[{"amount":{"value":0.25,"currency":"usd"}}]},` +
				`{"start_time":` + strconv.FormatInt(oldStart, 10) + `,"results":[{"amount":{"value":0.75,"currency":"usd"}}]}` +
				`]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OPENAI_KEY", "test-key")
	defer os.Unsetenv("TEST_OPENAI_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "openai",
		Provider:  "openai",
		APIKeyEnv: "TEST_OPENAI_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if orgCalls != 2 {
		t.Fatalf("org API calls = %d, want 2", orgCalls)
	}
	if got := metricUsed(snap.Metrics, "today_api_cost"); got != 0.25 {
		t.Errorf("today_api_cost = %v, want 0.25", got)
	}
	if got := metricUsed(snap.Metrics, "7d_api_cost"); got != 1.00 {
		t.Errorf("7d_api_cost = %v, want 1.00", got)
	}
	if got := metricUsed(snap.Metrics, "monthly_spend"); got != 1.00 {
		t.Errorf("monthly_spend = %v, want 1.00", got)
	}
	if got := metricUsed(snap.Metrics, "requests_today"); got != 3 {
		t.Errorf("requests_today = %v, want 3", got)
	}
	if got := metricUsed(snap.Metrics, "today_input_tokens"); got != 100 {
		t.Errorf("today_input_tokens = %v, want 100", got)
	}
	if got := snap.Attributes["usage_api"]; got != "organization" {
		t.Errorf("usage_api = %q, want organization", got)
	}
}

func TestFetch_OrgUsageForbiddenKeepsRateLimits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAI_ADMIN_KEY", "bad-admin-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			w.Header().Set("x-ratelimit-limit-requests", "100")
			w.Header().Set("x-ratelimit-remaining-requests", "90")
			w.WriteHeader(http.StatusBadRequest)
		case "/organization/usage/completions", "/organization/costs":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"missing scopes"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_OPENAI_KEY", "test-key")
	defer os.Unsetenv("TEST_OPENAI_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "openai",
		Provider:  "openai",
		APIKeyEnv: "TEST_OPENAI_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %s, want OK (rate limits still work)", snap.Status)
	}
	if _, ok := snap.Metrics["rpm"]; !ok {
		t.Fatal("rpm metric missing")
	}
	if _, ok := snap.Metrics["today_api_cost"]; ok {
		t.Fatal("today_api_cost should be absent when org usage is forbidden")
	}
	if !strings.Contains(snap.Diagnostics["openai_usage_error"], "403") {
		t.Fatalf("diagnostic = %q, want HTTP 403", snap.Diagnostics["openai_usage_error"])
	}
}

func metricUsed(metrics map[string]core.Metric, key string) float64 {
	m, ok := metrics[key]
	if !ok || m.Used == nil {
		return 0
	}
	return *m.Used
}
