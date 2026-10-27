package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "gpt-4.1-mini"

	// chatCompletionsPath is the cheapest endpoint that reports both the
	// request and token rate-limit headers. GET /v1/models no longer does.
	chatCompletionsPath = "/chat/completions"
)

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "openai",
			Info: core.ProviderInfo{
				Name:         "OpenAI",
				Capabilities: []string{"headers"},
				DocURL:       "https://platform.openai.com/docs/guides/rate-limits",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "OPENAI_API_KEY",
				DefaultAccountID: "openai",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{"Set OPENAI_API_KEY to a valid OpenAI API key."},
			},
			Dashboard: providerbase.DefaultDashboard(providerbase.WithColorRole(core.DashboardColorRoleGreen)),
		}),
	}
}

// probePayload asks for zero completion tokens. OpenAI rejects it with HTTP
// 400, but still returns x-ratelimit-* headers on the validation error, so
// the probe costs no tokens. A valid one-token completion would report the
// same limits but bill the account on every poll.
func probePayload(model string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"model":                 model,
		"messages":              []map[string]string{{"role": "user", "content": "ping"}},
		"max_completion_tokens": 0,
	})
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey, authSnap := shared.RequireAPIKey(acct, p.ID())
	if authSnap != nil {
		return *authSnap, nil
	}

	baseURL := strings.TrimRight(shared.ResolveBaseURL(acct, defaultBaseURL), "/")
	model := acct.ProbeModel
	if model == "" {
		model = defaultModel
	}

	body, err := probePayload(model)
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("openai: encoding probe request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+chatCompletionsPath, bytes.NewReader(body))
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("openai: creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.Client().Do(req)
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	snap, err := shared.ProcessStandardResponse(resp, acct, p.ID())
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("openai: processing response: %w", err)
	}

	shared.ApplyStandardRateLimits(resp, &snap)

	// Optional organization-wide usage/spend. Regular project keys cannot
	// read /organization/*, so this only runs when a separate admin key is
	// configured. Failures are non-fatal: the rate-limit tile still works.
	if adminKey := p.adminKey(acct); adminKey != "" {
		if summary, usageErr := p.fetchOrgUsage(ctx, baseURL, adminKey); usageErr != nil {
			snap.SetDiagnostic("openai_usage_error", usageErr.Error())
		} else {
			applyOrgUsage(&snap, summary)
			if snap.Status == "" {
				snap.Status = core.StatusOK
				snap.Message = "Rate limits and organization usage"
			}
		}
	} else {
		snap.SetDiagnostic("openai_usage_error", "no admin key: set OPENAI_ADMIN_KEY or credentials key openai-admin")
	}

	// Validation errors (the expected 400) still carry the rate-limit
	// headers; anything else means the probe never reached the model, so
	// report it instead of rendering an empty, healthy tile.
	switch resp.StatusCode {
	case http.StatusOK, http.StatusBadRequest, http.StatusTooManyRequests:
		// 200: the API accepted max_completion_tokens=0 and returned no
		// output. 400: expected header-only probe. 429: limited, already
		// classified by ProcessStandardResponse.
	case http.StatusNotFound:
		snap.Status = core.StatusError
		snap.Message = fmt.Sprintf("probe model %q not available; set probe_model on the account", model)
	default:
		if snap.Status == "" {
			snap.Status = core.StatusError
			snap.Message = fmt.Sprintf("probe failed (HTTP %d)", resp.StatusCode)
		}
	}

	shared.FinalizeStatus(&snap)
	return snap, nil
}
