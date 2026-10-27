package tui

import (
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// TestComputeDisplayInfo_RateLimitUsesUsedPercent pins the rpm/tpm header
// semantics: the metrics carry remaining quota, so a nearly untouched limit
// must render as ~0% used, not as 100% used.
func TestComputeDisplayInfo_RateLimitUsesUsedPercent(t *testing.T) {
	limitRPM, remainingRPM := 10000.0, 9999.0
	limitTPM, remainingTPM := 10000000.0, 9999997.0

	snap := core.UsageSnapshot{
		ProviderID: "openai",
		AccountID:  "openai",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"rpm": {Limit: &limitRPM, Remaining: &remainingRPM, Unit: "requests", Window: "1m"},
			"tpm": {Limit: &limitTPM, Remaining: &remainingTPM, Unit: "tokens", Window: "1m"},
		},
	}

	di := computeDisplayInfo(snap, dashboardWidget("openai"), false)

	if di.gaugePercent > 1 {
		t.Fatalf("gaugePercent = %v, want ~0", di.gaugePercent)
	}
	if di.summary != "0% used" {
		t.Fatalf("summary = %q, want %q", di.summary, "0% used")
	}
	for _, want := range []string{"RPM 0%", "TPM 0%"} {
		if !strings.Contains(di.detail, want) {
			t.Errorf("detail = %q, missing %q", di.detail, want)
		}
	}
}

// When organization spend is available, the OpenAI tile should lead with cost
// and keep the rate-limit percentages in the detail line.
func TestComputeDisplayInfo_RateLimitWithCostUsesCreditsHero(t *testing.T) {
	limitRPM, remainingRPM := 10000.0, 9999.0
	todayCost, weekCost := 0.25, 1.00

	snap := core.UsageSnapshot{
		ProviderID: "openai",
		AccountID:  "openai",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"rpm":            {Limit: &limitRPM, Remaining: &remainingRPM, Unit: "requests", Window: "1m"},
			"today_api_cost": {Used: &todayCost, Unit: "USD", Window: "today"},
			"7d_api_cost":    {Used: &weekCost, Unit: "USD", Window: "7d"},
		},
	}

	di := computeDisplayInfo(snap, dashboardWidget("openai"), false)
	if di.tagLabel != "Credits" {
		t.Fatalf("tagLabel = %q, want Credits", di.tagLabel)
	}
	if di.summary != "~$0.25 today" {
		t.Fatalf("summary = %q, want %q", di.summary, "~$0.25 today")
	}
	for _, want := range []string{"~$1.00 7d", "RPM 0%"} {
		if !strings.Contains(di.detail, want) {
			t.Errorf("detail = %q, missing %q", di.detail, want)
		}
	}
}
