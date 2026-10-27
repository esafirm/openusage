package openai

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	orgUsageCompletionsPath = "/organization/usage/completions"
	orgCostsPath            = "/organization/costs"
)

// orgUsageSummary is the aggregate spend/token picture for the last 30 days.
type orgUsageSummary struct {
	TodayCostUSD  float64
	WeekCostUSD   float64
	MonthCostUSD  float64
	TodayRequests int64
	TodayInput    int64
	TodayOutput   int64
	TodayCached   int64
	MonthRequests int64
	MonthInput    int64
	MonthOutput   int64
	MonthCached   int64
}

type orgUsageBucket struct {
	StartTime int64 `json:"start_time"`
	EndTime   int64 `json:"end_time"`
	Results   []struct {
		InputTokens       int64 `json:"input_tokens"`
		OutputTokens      int64 `json:"output_tokens"`
		InputCachedTokens int64 `json:"input_cached_tokens"`
		NumModelRequests  int64 `json:"num_model_requests"`
	} `json:"results"`
}

type orgCostBucket struct {
	StartTime int64 `json:"start_time"`
	EndTime   int64 `json:"end_time"`
	Results   []struct {
		Amount struct {
			Value    float64 `json:"value"`
			Currency string  `json:"currency"`
		} `json:"amount"`
	} `json:"results"`
}

// adminKey resolves the optional organization admin key. A regular project key
// cannot read /organization/* (it lacks api.usage.read), so the key is kept
// separate from the inference key. Sources, in order:
//   - OPENAI_ADMIN_KEY
//   - credentials.json key "<account-id>-admin" (e.g. openai-admin)
func (p *Provider) adminKey(acct core.AccountConfig) string {
	if key := strings.TrimSpace(os.Getenv("OPENAI_ADMIN_KEY")); key != "" {
		return key
	}
	creds, err := config.LoadCredentials()
	if err != nil {
		return ""
	}
	for _, id := range []string{acct.ID + "-admin", "openai-admin"} {
		if key := strings.TrimSpace(creds.Keys[id]); key != "" {
			return key
		}
	}
	return ""
}

func (p *Provider) fetchOrgUsage(ctx context.Context, baseURL, adminKey string) (orgUsageSummary, error) {
	now := time.Now().UTC()
	start := now.Add(-30 * 24 * time.Hour)
	query := fmt.Sprintf("?start_time=%d&end_time=%d&bucket_width=1d&limit=31", start.Unix(), now.Unix())

	var usage struct {
		Data []orgUsageBucket `json:"data"`
	}
	status, _, err := shared.FetchJSON(ctx, baseURL+orgUsageCompletionsPath+query, adminKey, &usage, p.Client())
	if err != nil {
		return orgUsageSummary{}, orgAPIError("usage", status, err)
	}

	var costs struct {
		Data []orgCostBucket `json:"data"`
	}
	status, _, err = shared.FetchJSON(ctx, baseURL+orgCostsPath+query, adminKey, &costs, p.Client())
	if err != nil {
		return orgUsageSummary{}, orgAPIError("costs", status, err)
	}

	todayStart := now.Truncate(24 * time.Hour)
	weekStart := now.Add(-7 * 24 * time.Hour)

	var summary orgUsageSummary
	for _, bucket := range usage.Data {
		for _, result := range bucket.Results {
			if bucket.StartTime >= todayStart.Unix() {
				summary.TodayRequests += result.NumModelRequests
				summary.TodayInput += result.InputTokens
				summary.TodayOutput += result.OutputTokens
				summary.TodayCached += result.InputCachedTokens
			}
		}
	}

	for _, bucket := range costs.Data {
		for _, result := range bucket.Results {
			if currency := strings.TrimSpace(result.Amount.Currency); currency != "" && !strings.EqualFold(currency, "usd") {
				continue
			}
			summary.MonthCostUSD += result.Amount.Value
			if bucket.StartTime >= weekStart.Unix() {
				summary.WeekCostUSD += result.Amount.Value
			}
			if bucket.StartTime >= todayStart.Unix() {
				summary.TodayCostUSD += result.Amount.Value
			}
		}
	}

	return summary, nil
}

func orgAPIError(label string, status int, err error) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("organization %s API: HTTP %d (admin key needs api.usage.read): %w", label, status, err)
	default:
		return fmt.Errorf("organization %s API: %w", label, err)
	}
}

func applyOrgUsage(snap *core.UsageSnapshot, summary orgUsageSummary) {
	snap.Metrics["today_api_cost"] = core.Metric{
		Used:   core.Float64Ptr(summary.TodayCostUSD),
		Unit:   "USD",
		Window: "today",
	}
	snap.Metrics["7d_api_cost"] = core.Metric{
		Used:   core.Float64Ptr(summary.WeekCostUSD),
		Unit:   "USD",
		Window: "7d",
	}
	snap.Metrics["monthly_spend"] = core.Metric{
		Used:   core.Float64Ptr(summary.MonthCostUSD),
		Unit:   "USD",
		Window: "30d",
	}
	if summary.TodayRequests > 0 {
		snap.Metrics["requests_today"] = core.Metric{
			Used:   core.Float64Ptr(float64(summary.TodayRequests)),
			Unit:   "requests",
			Window: "today",
		}
	}
	if summary.TodayInput > 0 {
		snap.Metrics["today_input_tokens"] = core.Metric{
			Used:   core.Float64Ptr(float64(summary.TodayInput)),
			Unit:   "tokens",
			Window: "today",
		}
	}
	if summary.TodayOutput > 0 {
		snap.Metrics["today_output_tokens"] = core.Metric{
			Used:   core.Float64Ptr(float64(summary.TodayOutput)),
			Unit:   "tokens",
			Window: "today",
		}
	}
	if summary.TodayCached > 0 {
		snap.Metrics["today_cached_tokens"] = core.Metric{
			Used:   core.Float64Ptr(float64(summary.TodayCached)),
			Unit:   "tokens",
			Window: "today",
		}
	}
	snap.SetAttribute("usage_api", "organization")
	snap.SetAttribute("usage_api_range", "30d")
}
