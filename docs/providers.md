# Providers

OpenUsage ships with five provider integrations. All are auto-detected when
available, so no manual config is needed.

## Coding agents

### Claude Code

**Detection:** `claude` binary + `~/.claude` directory

Tracks daily activity, per-model token usage, 5-hour billing blocks, burn rate,
and cost estimates from local stats and conversation JSONL. The optional hook
integration adds real-time telemetry:

```bash
openusage integrations install claude_code
```

### OpenCode

**Detection:** `OPENCODE_API_KEY` / `ZEN_API_KEY`, `~/.local/share/opencode/auth.json`,
or an OpenCode Go entry in `auth.json`

Tracks OpenCode Go quota (5h, weekly, monthly), Zen models, console balance, and
monthly usage. Token usage is read from OpenCode's local SQLite store.

OpenCode Go quota comes from the OpenCode console and needs a browser session.
Open the dashboard, go to **Settings -> 5 KEYS -> opencode**, and press `c` to
import the session cookie.

### Antigravity CLI

**Detection:** `agy` binary + `~/.gemini/antigravity-cli` directory

Tracks status-line context usage, session tokens, model quotas, workspace, and
agent state without reading credentials or estimating spend.

```bash
openusage integrations install antigravity
```

### GitHub Copilot

**Detection:** `copilot` binary + `~/.copilot` directory, or the `gh copilot`
extension

Tracks Copilot quota, org billing, org metrics, and session usage. The provider
uses the standalone `copilot` binary when present and falls back to `gh` when it
needs GitHub API data.

## API platforms

### OpenAI

**Detection:** `OPENAI_API_KEY` environment variable

Tracks rate limits via lightweight header probing. This provider reports RPM and
TPM limits, not spend or token history.

## Browser-session auth

Some console-only data is gated by a web session cookie. OpenUsage supports a
"connect via browser" flow that reads the session cookie from your chosen
browser's cookie store:

1. Open **Settings -> 5 KEYS**.
2. Move to the provider row.
3. Press `c` for mixed-auth providers such as OpenCode.
4. Pick the browser.

The cookie is stored in `credentials.json` with `0600` permissions and scoped to
one `(domain, cookie name)` pair. It is never sent anywhere except the provider's
console. macOS may ask for Keychain access the first time OpenUsage reads a
Chrome cookie store.

Currently shipping:

- OpenCode -> `opencode.ai` for balance, monthly limit, subscription, and
  OpenCode Go quota.

## OpenCode credential adoption

If OpenCode is installed and you have authenticated any of its providers,
OpenUsage reads `~/.local/share/opencode/auth.json` and adopts API keys for the
providers this build supports:

| OpenCode entry | OpenUsage account |
|---|---|
| `opencode` (api) | `opencode` |
| `opencode-go` (api) | `opencode` with the Go plan hint |

OAuth-typed entries are skipped. Environment variables are checked first and win
over `auth.json`.
