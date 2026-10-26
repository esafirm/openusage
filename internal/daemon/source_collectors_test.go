package daemon

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func TestBuildCollectors_ScopesConfiguredAccount(t *testing.T) {
	collectors, warnings := buildCollectorsFromSources([]core.AccountConfig{
		{
			ID:       "opencode-main",
			Provider: "opencode",
			RuntimeHints: map[string]string{
				"db_path": "/tmp/opencode-main",
			},
		},
	}, telemetrySourcesBySystem())

	collector := findSourceCollector(t, collectors, "opencode")
	if collector.AccountOverride != "opencode-main" {
		t.Fatalf("account override = %q, want opencode-main", collector.AccountOverride)
	}
	if got := collector.Options.Paths["account_id"]; got != "opencode-main" {
		t.Fatalf("account_id option = %q, want opencode-main", got)
	}
	if got := collector.Options.Paths["db_path"]; got != "/tmp/opencode-main" {
		t.Fatalf("db_path = %q, want /tmp/opencode-main", got)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func TestBuildCollectors_AmbiguousAccountsFallBackToSourceScope(t *testing.T) {
	collectors, warnings := buildCollectorsFromSources([]core.AccountConfig{
		{ID: "opencode-a", Provider: "opencode"},
		{ID: "opencode-b", Provider: "opencode"},
	}, telemetrySourcesBySystem())

	collector := findSourceCollector(t, collectors, "opencode")
	if collector.AccountOverride != "" {
		t.Fatalf("account override = %q, want empty", collector.AccountOverride)
	}
	if got := collector.Options.Paths["account_id"]; got != "" {
		t.Fatalf("account_id option = %q, want empty", got)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings len = %d, want 1", len(warnings))
	}
}

func TestBuildCollectorsFromSourcesReusesTelemetrySourceInstances(t *testing.T) {
	sources := telemetrySourcesBySystem()

	first, firstWarnings := buildCollectorsFromSources(nil, sources)
	second, secondWarnings := buildCollectorsFromSources(nil, sources)
	if len(firstWarnings) != 0 || len(secondWarnings) != 0 {
		t.Fatalf("warnings = %v, %v; want none", firstWarnings, secondWarnings)
	}

	firstOpenCode := findSourceCollector(t, first, "opencode")
	secondOpenCode := findSourceCollector(t, second, "opencode")
	if firstOpenCode.Source != secondOpenCode.Source {
		t.Fatal("opencode telemetry source was recreated between collector builds")
	}
}

func TestResolveTelemetrySourceOptionsFromAccounts_UsesExplicitAccount(t *testing.T) {
	source, ok := providers.TelemetrySourceBySystem("opencode")
	if !ok {
		t.Fatal("opencode telemetry source not found")
	}

	options, accountID, warnings := resolveTelemetrySourceOptionsFromAccounts(source, []core.AccountConfig{
		{
			ID:       "opencode-a",
			Provider: "opencode",
			RuntimeHints: map[string]string{
				"db_path": "/tmp/opencode-a",
			},
		},
		{
			ID:       "opencode-b",
			Provider: "opencode",
			RuntimeHints: map[string]string{
				"db_path": "/tmp/opencode-b",
			},
		},
	}, "opencode-b")

	if accountID != "opencode-b" {
		t.Fatalf("account id = %q, want opencode-b", accountID)
	}
	if got := options.Paths["db_path"]; got != "/tmp/opencode-b" {
		t.Fatalf("db_path = %q, want /tmp/opencode-b", got)
	}
	if got := options.Paths["account_id"]; got != "opencode-b" {
		t.Fatalf("account_id option = %q, want opencode-b", got)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func findSourceCollector(t *testing.T, collectors []telemetry.Collector, name string) *telemetry.SourceCollector {
	t.Helper()
	for _, collector := range collectors {
		if collector.Name() != name {
			continue
		}
		sourceCollector, ok := collector.(*telemetry.SourceCollector)
		if !ok {
			t.Fatalf("collector %q has type %T, want *telemetry.SourceCollector", name, collector)
		}
		return sourceCollector
	}
	t.Fatalf("collector %q not found", name)
	return nil
}
