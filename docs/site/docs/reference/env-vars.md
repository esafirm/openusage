---
title: Environment variables
description: Every environment variable this build of OpenUsage reads.
---

# Environment variables

OpenUsage reads two kinds of environment variables: runtime overrides (debug,
paths, sockets) and API key envs referenced from `accounts[].api_key_env`.

## Runtime overrides

| Variable | Purpose |
|---|---|
| `OPENUSAGE_DEBUG` | When set to any non-empty value, enables verbose logging to stderr. |
| `OPENUSAGE_BIN` | Override the binary path embedded in hook scripts. |
| `OPENUSAGE_TELEMETRY_SOCKET` | Override the daemon Unix socket path. |
| `OPENUSAGE_GITHUB_TOKEN` | Token used for the in-app update check against GitHub. Optional. |
| `OPENUSAGE_THEME_DIR` | Colon-separated list of extra directories scanned for theme JSON files. |
| `OPENUSAGE_CUSTOM_PRICING` | Override the path to `custom-pricing.json`. |
| `OPENUSAGE_CLAUDE_DESKTOP_COOKIES` | Set to `1` to allow the Claude desktop cookie source during setup. It is off by default for background processes. |
| `XDG_CONFIG_HOME` | Honored when resolving `custom-pricing.json` and the integrations hooks directory. Not honored for `settings.json`. |
| `XDG_STATE_HOME` | Override the state base directory (telemetry db/socket/spools). |
| `CLAUDE_SETTINGS_FILE` | Override the path to `~/.claude/settings.json`. |
| `ANTIGRAVITY_CONFIG_DIR` | Override the path to `~/.gemini/antigravity-cli`. |
| `ANTIGRAVITY_SETTINGS_FILE` | Override the Antigravity settings file path. |

## API key environment variables

Each provider account references its key via `api_key_env`, the name of the
variable rather than its value.

| Provider | Default env var |
|---|---|
| OpenAI | `OPENAI_API_KEY` |
| OpenCode | `OPENCODE_API_KEY` or `ZEN_API_KEY` |

The local providers (Claude Code, Antigravity, Copilot) use their own local
credentials and do not need an API key env var.
