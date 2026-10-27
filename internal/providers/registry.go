package providers

import (
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/antigravity"
	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
	"github.com/janekbaraniewski/openusage/internal/providers/codex"
	"github.com/janekbaraniewski/openusage/internal/providers/copilot"
	"github.com/janekbaraniewski/openusage/internal/providers/openai"
	"github.com/janekbaraniewski/openusage/internal/providers/opencode"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// AllProviders returns the provider set supported by this build.
//
// The set is intentionally small: OpenCode, Claude Code, Codex, Antigravity,
// OpenAI, and Copilot. Adding a provider here also requires a matching
// detector in internal/detect.
func AllProviders() []core.UsageProvider {
	return []core.UsageProvider{
		openai.New(),
		opencode.New(),
		antigravity.New(),
		claude_code.New(),
		codex.New(),
		copilot.New(),
	}
}

func TelemetrySourceBySystem(system string) (shared.TelemetrySource, bool) {
	target := strings.TrimSpace(system)
	if target == "" {
		return nil, false
	}
	for _, provider := range AllProviders() {
		source, ok := provider.(shared.TelemetrySource)
		if !ok {
			continue
		}
		if strings.EqualFold(source.System(), target) {
			return source, true
		}
	}
	return nil, false
}
