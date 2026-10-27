---
title: OpenAI
description: Track OpenAI API rate limits and quotas in OpenUsage.
sidebar_label: OpenAI
keywords: [openai usage tracker, openai quota tracking, openai cost tracking, openai token usage, track openai spend locally]
---

# OpenAI

Rate-limit probe for the OpenAI API, with optional organization-wide usage
and spend when an admin key is configured. The regular API key only exposes
RPM/TPM headers; the admin key unlocks the `/organization/*` usage and cost
endpoints.

## At a glance

- **Provider ID** — `openai`
- **Detection** — `OPENAI_API_KEY` environment variable (inference key)
- **Optional admin key** — `OPENAI_ADMIN_KEY` or `credentials.json` key `openai-admin`
- **Auth** — API key
- **Type** — API platform
- **Tracks**:
  - RPM and TPM rate limits (limit, remaining, reset)
  - Auth status
  - Optional: today / 7-day / 30-day spend and token usage from the organization API

## Setup

### Auto-detection

Set `OPENAI_API_KEY`. OpenUsage registers the provider on next start.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "openai",
      "provider": "openai",
      "api_key_env": "OPENAI_API_KEY",
      "base_url": "https://api.openai.com",
      "extra": {
        "probe_model": "gpt-4.1-mini"
      }
    }
  ]
}
```

`probe_model` defaults to `gpt-4.1-mini`. Override `base_url` for proxies or Azure-style gateways.

### Optional admin key for usage and spend

OpenAI's `/organization/*` endpoints require an **admin** key with the
`api.usage.read` scope. A normal project key cannot read them. Add the admin
key in one of these ways:

```bash
# Option A: environment variable (then reinstall the daemon so it captures it)
export OPENAI_ADMIN_KEY=sk-admin-...
openusage telemetry daemon install
```

```json
// Option B: ~/.config/openusage/credentials.json
{
  "keys": {
    "openai": "sk-proj-...",
    "openai-admin": "sk-admin-..."
  }
}
```

The regular inference key stays in `OPENAI_API_KEY` / `keys.openai`. When no
admin key is configured the tile shows rate limits only.

## Data sources & how each metric is computed

OpenUsage sends one `POST https://api.openai.com/v1/chat/completions` per poll cycle (default every 30 seconds in daemon mode). The probe model is `gpt-4.1-mini` unless `extra.probe_model` is set.

The probe deliberately asks for `max_completion_tokens: 0`, which OpenAI rejects with HTTP 400. OpenAI attaches the `x-ratelimit-*` headers to that validation response, so the probe still reports the limits without generating (or billing) any completion tokens. The expected 400 is treated as success; `401`/`403` and `429` keep their usual meaning.

A plain `GET /v1/models/{probe_model}` no longer returns rate-limit headers, which is why the probe moved to the chat endpoint.

Request headers:

- `Authorization: Bearer $OPENAI_API_KEY`
- `Content-Type: application/json`

### `rpm` — requests per minute

- Source: response headers
  - `x-ratelimit-limit-requests`
  - `x-ratelimit-remaining-requests`
  - `x-ratelimit-reset-requests`
- Transform: copied verbatim into `Limit` / `Remaining`. Reset is decoded into `Resets["rpm"]`.
- Window: 1 minute. **Scope: the probe model only** — different models can have different per-minute limits on the same key.

### `tpm` — tokens per minute

- Source: response headers
  - `x-ratelimit-limit-tokens`
  - `x-ratelimit-remaining-tokens`
  - `x-ratelimit-reset-tokens`
- Transform: same shape as `rpm` but for tokens.

### Optional organization usage / spend

When an admin key is configured, the provider also calls:

- `GET /v1/organization/usage/completions?start_time=...&end_time=...&bucket_width=1d`
- `GET /v1/organization/costs?start_time=...&end_time=...&bucket_width=1d`

It sums the last 30 daily buckets and exposes:

- `today_api_cost` — spend in the current UTC day
- `7d_api_cost` — spend in the last 7 days
- `monthly_spend` — spend in the last 30 days
- `requests_today`, `today_input_tokens`, `today_output_tokens`, `today_cached_tokens`

The tile leads with today's spend once these metrics exist; the rate-limit
percentages stay in the detail line. Without an admin key these calls are
skipped and the tile shows rate limits only.

### Auth status

- Source: HTTP status code.
- Transform: `401`/`403` → `auth`; `429` → `limited` (with `retry_after` from `Retry-After` if present); otherwise `ok`.

### What's NOT tracked

- **Account-wide rate limits.** The rate-limit numbers are scoped to the probe model.
- **ChatGPT subscription usage.** This provider reads the API platform, not ChatGPT; use the local Codex provider for ChatGPT/Codex sessions.
- **Spend without an admin key.** Project keys cannot read `/organization/*`.

### How fresh is the data?

- Rate limits: one request per poll (default every 30 s).
- Organization usage/spend: two additional requests per poll when an admin key is configured.

## API endpoints used

- `POST /v1/chat/completions` — header-only probe (default model `gpt-4.1-mini`). The request is intentionally invalid (`max_completion_tokens: 0`) so OpenAI returns the rate-limit headers without running inference.
- `GET /v1/organization/usage/completions` — optional, admin key only.
- `GET /v1/organization/costs` — optional, admin key only.

## Caveats

:::note
A regular project API key cannot read OpenAI usage or spend. Add an admin key
with `api.usage.read` (see above) to unlock the organization usage metrics.
:::

- Rate limits come from response headers; they reflect the probe model's quota, not your account-wide spend.
- The probe is one request per poll cycle and generates no completion tokens. It still counts as an API request for rate-limit purposes.

## Troubleshooting

- **Auth failed** — verify `OPENAI_API_KEY` is set and valid; rotate if leaked.
- **No data** — the probe model may be unavailable on your tier. Set `probe_model` to a model your key can access.
- **Spend is missing** — check `openai_usage_error` in the daemon's read model. `403` means the admin key is absent or lacks `api.usage.read`.

### Why is there no $ spend?

A regular project key cannot read `/organization/*`. Create an admin key in the
OpenAI dashboard, add it as `OPENAI_ADMIN_KEY` or the `openai-admin` entry in
`credentials.json`, and restart the daemon. Codex (for ChatGPT Pro/Plus
accounts) and OpenRouter (when proxying OpenAI) also expose usage without an
admin key.

### Why are my RPM/TPM different from the OpenAI dashboard?

The numbers come from headers attached to a request for `probe_model`. Different models share different rate-limit pools on the same account. Set `extra.probe_model` to the model you actually call most.

## Related

- [Codex CLI](./codex.md) — OpenAI's coding agent with local session and credit data
- [OpenRouter](./openrouter.md) — proxy with full billing visibility for OpenAI models
