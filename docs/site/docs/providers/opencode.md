---
title: OpenCode
description: Track OpenCode auth, available zen models, and spend via the telemetry plugin in OpenUsage.
sidebar_label: OpenCode
keywords: [opencode usage tracker, opencode quota tracking, opencode cost tracking, opencode token usage, track opencode spend locally]
---

# OpenCode

Tracks the OpenCode tool's auth status and available models. Spend and per-session activity come from the OpenCode telemetry plugin, not the public API.

## At a glance

- **Provider ID** — `opencode`
- **Detection** — `OPENCODE_API_KEY` or `ZEN_API_KEY` environment variable (`OPENCODE_API_KEY` is the primary env var; `ZEN_API_KEY` is an alias). Also adopts API keys written to OpenCode's `auth.json` (both `opencode`/Zen and `opencode-go`/Go catalog entries land on the same `opencode` tile).
- **Auth** — API key
- **Type** — coding agent
- **Tracks**:
  - Auth status
  - Available zen models with `owned_by` metadata
  - Spend and activity (only via the telemetry plugin)

## Setup

### Auto-detection

Set `OPENCODE_API_KEY` (preferred) or `ZEN_API_KEY` (alias). Both work; the first non-empty value wins.

OpenUsage also adopts API keys written to OpenCode's `auth.json` automatically. That file lives at `~/.local/share/opencode/auth.json` on Linux and macOS, at `$XDG_DATA_HOME/opencode/auth.json` when `XDG_DATA_HOME` is set, and at `%USERPROFILE%\.local\share\opencode\auth.json` on Windows (OpenCode uses the XDG-style location on Windows too). See the [paths reference](../reference/paths.md#tool-integration-paths).

### Quota meters (OpenCode Go) and balance

The OpenCode Go 5h / weekly / monthly quota windows and the Zen balance are only available from the opencode.ai web console. API keys (Zen or Go) can't read them. To get console data on the tile, OpenUsage needs the console session cookie (`__Host-console_session` on `opencode.ai`).

A user can have **two OpenCode accounts** with different browser profiles, for example a work profile with Zen credits and a personal profile with a Go subscription. OpenUsage keeps those as two session keys:

- `opencode` — primary session, used for the Zen balance and rolling 30-day spend.
- `opencode-go` — Go session, used for the 5h / weekly / monthly quota meters.

The tile merges both, so you see the balance and the quota diagram together.

1. Log into the [OpenCode console](https://opencode.ai/console) in the browser profile for each account.
2. **Firefox or Safari:** auto-detection imports the session silently (it retries at most every 10 minutes until it finds one) and stores it in `credentials.json`.
3. **Chrome, Arc, Brave, Edge and other Chromium browsers:** their cookie stores are keychain-protected, so OpenUsage never reads them in the background. Import each profile explicitly:

```bash
# Work profile: Zen balance and spend
openusage credentials import-session opencode --browser chrome --profile "Esa Bandlab"

# Personal profile: OpenCode Go quota
openusage credentials import-session opencode-go --browser chrome --profile "Esa"
```

The profile names are the ones shown in Chrome's profile menu. You may get one keychain prompt per import. The same commands work from Settings → 5 KEYS → `opencode` → `c` for the primary session; the CLI is the easiest way to add the second session.

Once a session is stored, each poll reads it from `credentials.json` and never re-reads the browser. Until then, the tile shows a one-line hint such as `Connect OpenCode console for quota/balance`. If a session expires, the hint changes to `OpenCode console session expired`; log in again and re-import it.

When the key comes from the `opencode-go` entry in OpenCode's `auth.json`, the account gets an `opencode_plan: go` hint.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "opencode",
      "provider": "opencode",
      "api_key_env": "OPENCODE_API_KEY",
      "base_url": "https://opencode.ai"
    }
  ]
}
```

## Data sources & how each metric is computed

The OpenCode provider has two data paths:

1. **Polling.** The provider hits `GET https://opencode.ai/zen/v1/models` to list available Zen models and confirm the API key works. **The Zen API does not expose spend, balance, or per-session activity to API keys**, so polling alone never produces a usage figure on the OpenCode tile.
2. **Telemetry plugin.** When the OpenCode telemetry plugin is installed, OpenCode posts per-turn events (model, token counts, tools) to the OpenUsage daemon over its socket. **Those events are tagged with the upstream provider** (the model the turn actually called: `anthropic`, `openai`, `google`, etc.), not with `opencode`.
3. **Optional console enrichment.** When you import console sessions, the provider calls OpenCode's console JSON API to populate the Zen balance, rolling 30-day spend, and Go quota meters. The primary session uses `/console/api/billing/status` and `/console/api/usage/summary`; the Go session uses `/console/api/go/status`. This is opt-in.

### Available zen models

- Source: `data[].id` from `GET /zen/v1/models`. Each entry also carries an `owned_by` field surfaced in the detail view.
- Transform: count is stored as `Attributes["available_models_count"]`; the joined list is stored as `Attributes["available_models"]`.

### Auth status

- Source: HTTP status code of the models call. `401`/`403` → `auth`; `429` → `limited`; otherwise `ok`. The OpenUsage tile message shows `Auth OK · N Zen models` (or, when enrichment succeeded, `$X.XX balance · N Zen models`).

### `rolling_usage` / `weekly_usage` / `monthly_usage_pct`

- Source: `access.meters.fiveHour` / `week` / `month` from `/console/api/go/status`, on the `opencode-go` session.
- Transform: `usedMicroCents / limitMicroCents * 100` becomes the gauge percentage; `resetsAt` becomes the reset countdown. The console's microcents are an allowance unit, so only the ratio is used.

### `console_balance` / `monthly_usage` / `monthly_limit`

- Source: optional console API, only when a browser-session cookie is configured.
- `console_balance` comes from `/console/api/billing/status` (`availableMicroCents`, falling back to `balanceMicroCents`).
- `monthly_usage` comes from `/console/api/usage/summary?range=30d` (`totalCostMicroCents`).
- `monthly_limit` is only set when the console reports a non-null `creditLimitMicroCents`.
- Transform: the console represents money in 1e-8 dollar units; the provider divides by `1e8` before storing. Workspace ID is auto-discovered from `/console/api/user/workspaces` or provided via `extra.opencode_workspace_id`.

### Subscription metadata

- The new console API exposes `hasGoSubscription` on `/console/api/orgs/current`, but the provider does not currently surface it as a tile attribute.

### Where spend actually shows up

The OpenCode telemetry plugin streams events tagged with the upstream provider that served each turn. Examples of how that data lands on the dashboard:

- A Claude Sonnet turn through OpenCode → event tagged `anthropic` → spend appears on the Claude Code tile (or anywhere `anthropic` is mapped via `telemetry.provider_links`).
- A GPT-4o turn through OpenCode → event tagged `openai` → spend appears on the OpenAI tile.
- A Gemini turn through OpenCode → event tagged `google` → spend appears on the Gemini API tile (`google` is the default mapping for `gemini_api`).

If the upstream provider doesn't have an account configured in OpenUsage, the events sit in the telemetry store and surface as `telemetry_unmapped_providers` diagnostics — the OpenCode tile itself does **not** absorb them, because it's a different provider.

### Tile layout

- **Headline.** With both sessions connected, the top section shows the Go quota meters (`Usage 5h`, `Weekly`, `Monthly`) and the hero line `5h X% · 7d Y% · mo Z%`, with the primary session's balance as the detail line (`$9.72 balance`). Without a Go session, it shows the Zen balance and rolling 30-day spend. Without any console session, it shows the connect hint and then the request and token activity for the selected time window, for example `11 reqs · 166.7k tok in Today`.
- **Sections.** OpenCode events tagged `opencode` / `opencode-go` populate Model Burn, Provider Burn, Clients, Project Breakdown, Tool Usage, MCP Usage, and Language, the same layout other coding-tool tiles use.
- **Cache hit ratio.** `cache_hit_ratio` is cache reads divided by (input + cache reads + cache writes). It matches the `N% cached` figure in the Token Breakdown. It is a ratio, not a quota, so it never appears as the tile's `% used` headline.

### What's NOT tracked

- **Spend from the Zen API key alone.** The Zen API does not expose it. Balance and 30-day spend require the console session cookie.
- **Per-session detail without the plugin.** Token counts, tools, and per-message breakdowns require the telemetry plugin.

### How fresh is the data?

- Polling: every 30 s by default.
- Telemetry: real-time (events ingested as the plugin emits them, dedup'd in the daemon's SQLite store).
- Console enrichment: same cadence as polling.

## API endpoints used

- `GET /zen/v1/models` — auth probe + model list.
- Console API (browser-session auth, opt-in):
  - `GET /console/api/user/workspaces`
  - `GET /console/api/billing/status`
  - `GET /console/api/usage/summary?range=30d`
  - `GET /console/api/go/status`

## Caveats

:::tip
To see spend on this tile, install the OpenCode telemetry plugin and run OpenUsage in daemon mode. See [Daemon integrations](../daemon/integrations.md).
:::

- Without telemetry the tile shows model availability only; this is expected.
- `base_url` defaults to `https://opencode.ai`.

## Troubleshooting

- **No models listed** — verify the API key is valid and not rate-limited.
- **Empty spend tile** — install and configure the OpenCode telemetry plugin; see daemon docs.
- **Tile shows "No ... data for this time range" with `Auth OK · N Zen models`**: the API key works but there are no OpenCode telemetry events in the selected window and no browser session for console quota. Widen the window (`w`), connect a browser session for Go quota, or check that the telemetry plugin is still emitting events. The OpenUsage loading banner only appears while the daemon has not resolved the account yet.

### Why does the OpenCode tile not show spend even with the plugin installed?

The plugin tags each event with the **upstream provider** that served the turn (`anthropic`, `openai`, `google`, …) rather than with `opencode`. The OpenCode tile only owns events whose source provider is `opencode`. The spend is being recorded — it's just routed to the upstream provider's tile, or to `telemetry_unmapped_providers` if you have not configured that provider in OpenUsage. Set the upstream's env var (e.g. `OPENAI_API_KEY`) so a tile exists, or remap with `telemetry.provider_links`.

### What do I see if I only set OPENCODE_API_KEY and nothing else?

The OpenCode tile renders auth status and the Zen model count. Telemetry events from the plugin are written to the store but have nowhere to display: there is no Anthropic or OpenAI tile to absorb them. They appear in the daemon's `telemetry_unmapped_providers` diagnostic. Setting the upstream provider env vars (or remapping) makes the data visible.
