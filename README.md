<p align="center">
  <img src="./assets/logo.gif" alt="OpenUsage logo">
</p>

<p align="center"><strong>OpenUsage: terminal-first local quota and usage tracking for Claude Code, Codex, OpenCode, Antigravity, OpenAI, and GitHub Copilot.</strong></p>

<p align="center">
  <a href="#install">Install</a> &middot;
  <a href="#supported-providers">Providers</a> &middot;
  <a href="#configuration">Config</a> &middot;
  <a href="#keybindings">Keybindings</a> &middot;
  <a href="#development">Development</a>
</p>

---

OpenUsage is a terminal dashboard for local AI coding tool usage. It reads local
stats, conversation logs, API rate-limit headers, and optional hook events, then
shows quota, spend, resets, and per-model usage in one view. It is built for
macOS and Linux.

![OpenUsage dashboard](./assets/dashboard.png)

Run it side-by-side with your coding agent:

<p align="center">
  <img src="./assets/sidebyside.png" alt="OpenUsage side by side">
  <br>
  <em>OpenUsage running alongside OpenCode.</em>
</p>

## Install

### macOS (Homebrew, recommended)

```bash
brew install janekbaraniewski/tap/openusage
```

### macOS and Linux (quick install script)

```bash
curl -fsSL https://github.com/janekbaraniewski/openusage/releases/latest/download/install.sh | bash
```

### From source (Go 1.25+)

```bash
go install github.com/janekbaraniewski/openusage/cmd/openusage@latest
```

Requires CGO (`CGO_ENABLED=1`) for the SQLite telemetry store. Pre-built
binaries are available on the [Releases](https://github.com/janekbaraniewski/openusage/releases)
page.

## Run

```bash
openusage
```

Auto-detection picks up local tools and common API key env vars. No config is
needed.

## Supported providers

This build ships six providers:

| Provider | Detection | What it tracks |
|---|---|---|
| **Claude Code** | `claude` binary + `~/.claude` | Daily activity, per-model tokens, 5-hour blocks, burn rate, cost estimates |
| **Codex CLI** | `~/.codex` sessions and auth | ChatGPT/Codex plan usage, 5h/7d rate limits, session tokens, cost estimates |
| **OpenCode** | `OPENCODE_API_KEY` / `ZEN_API_KEY` or `~/.local/share/opencode/auth.json` | OpenCode Go quota, Zen models, console balance, token usage from the local SQLite store |
| **Antigravity CLI** | `agy` binary + `~/.gemini/antigravity-cli` | Status-line context, session tokens, quota windows |
| **OpenAI** | `OPENAI_API_KEY` | RPM/TPM rate limits from the API |
| **GitHub Copilot** | `copilot` binary + `~/.copilot` or the `gh copilot` extension | Copilot quota, org data, and session usage |

### OpenCode Go quota

OpenCode Go quota (5h, weekly, monthly) comes from the OpenCode console and
needs a browser session. Open the dashboard, go to **Settings -> 5 KEYS ->
opencode**, and press `c` to import the session cookie. Token usage is read
from OpenCode's local SQLite store without a browser session.

### Antigravity status line

The Antigravity integration registers this binary as the `agy` status-line
command:

```bash
openusage integrations install antigravity
```

## Configuration

No config file is needed. Override or extend via:

- macOS/Linux: `~/.config/openusage/settings.json`

```json
{
  "auto_detect": true,
  "ui": { "refresh_interval_seconds": 30 },
  "accounts": [
    {
      "id": "openai-personal",
      "provider": "openai",
      "api_key_env": "OPENAI_API_KEY",
      "probe_model": "gpt-4.1-mini"
    }
  ]
}
```

Full reference: [`configs/example_settings.json`](configs/example_settings.json)

### External themes

Custom themes are JSON files loaded at startup from:

- `~/.config/openusage/themes/*.json`
- Any extra directory in `OPENUSAGE_THEME_DIR` (path-list separated)

Theme files use the same color token fields as the built-in themes. Every
bundled theme lives under [`internal/tui/bundled_themes/`](internal/tui/bundled_themes/).

## Daemon

The daemon collects data continuously, even when the dashboard is closed:

```bash
openusage telemetry daemon                # Run in foreground
openusage telemetry daemon install        # Install as a launchd or systemd user service
openusage telemetry daemon status         # Check status
openusage telemetry daemon uninstall      # Uninstall
```

Installed services snapshot the provider env vars currently set in your shell.
If you change API key env vars later, rerun `openusage telemetry daemon install`
to refresh the service environment.

Manage tool integrations:

```bash
openusage integrations list [--all]       # List integration statuses
openusage integrations install <id>       # Install hook/plugin
openusage integrations uninstall <id>     # Remove
```

## Keybindings

| Key | Action |
|---|---|
| `Tab` | Switch views |
| `j` / `k`, `Up` / `Down` | Move cursor |
| `h` / `l`, `Left` / `Right` | Navigate panels |
| `Enter` / `Esc` | Open detail / back |
| `PgUp` / `PgDn` | Scroll tile |
| `[ ]` | Switch detail tabs |
| `r` | Refresh all |
| `/` | Filter providers |
| `t` | Cycle theme |
| `w` | Cycle time window |
| `c` | Cycle cost visibility for focused tile (auto -> hide -> show -> auto, persists per-account) |
| `,` | Open settings |
| `Shift+J` / `Shift+K` | Reorder providers |
| `?` | Help |
| `q` | Quit |

## Development

```bash
make build    # Build binary to ./bin/openusage
make test     # Run tests with -race and coverage
make lint     # golangci-lint
make run      # go run cmd/openusage/main.go
```

Debug mode: `OPENUSAGE_DEBUG=1 openusage`

## License

[MIT](LICENSE)
