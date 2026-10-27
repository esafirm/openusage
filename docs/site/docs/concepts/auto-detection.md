---
title: Auto-detection
description: How OpenUsage discovers AI tools and API keys on first run, and how detected accounts merge with manual configuration.
---

The first time you run `openusage`, no config file is required. The detector inspects your environment and creates accounts for everything it finds. You can override or extend its results by editing `~/.config/openusage/settings.json`.

## What gets scanned

Detection runs in four phases. Earlier phases win when the same provider/account ID would be produced twice; the process environment beats every file source.

### 1. Tool detectors

Tool-specific local checks for Claude Code, Codex CLI, GitHub Copilot, and Antigravity CLI.

### 2. Environment variables (API platforms)

For each supported provider, the detector checks whether a known env var is set in the running process environment:

| Env var | Provider |
|---|---|
| `OPENAI_API_KEY` | openai |
| `OPENCODE_API_KEY` / `ZEN_API_KEY` | opencode |

If the env var is present, an account is created with `api_key_env` set to that variable name. The actual key value is read at fetch time, never persisted.

### 3. File-based credential adoption

When an env var isn't set in the running process — for example because OpenUsage was launched from Spotlight, the Dock, or a desktop launcher that didn't source your shell startup files — the detector falls back to a small set of well-defined credential files:

| Source | Where | What's adopted |
|---|---|---|
| Shell rc files | `~/.zshenv`, `~/.zprofile`, `~/.zshrc`, `~/.bash_profile`, `~/.bashrc`, `~/.profile`, `~/.config/fish/config.fish`, plus modular `~/.zshrc.d/*.zsh`, `~/.bashrc.d/*.sh`, `~/.config/fish/conf.d/*.fish` | `export VAR=...`, plain `VAR=...`, and fish `set -gx VAR ...` lines whose name matches one of the API key envs above. Lines that contain shell substitutions (`$VAR`, `$(...)`, backticks) are skipped — we never invoke a shell. |
| Codex CLI | `~/.codex/sessions/` and `~/.codex/auth.json` | Local session logs and the Codex CLI login. Registers the `codex-cli` account; no key is copied. |
| OpenCode | `~/.local/share/opencode/auth.json` | API-key entries for OpenCode (Zen) and OpenCode Go. OAuth-typed entries are recognised but not adopted. |

A discovered key always sets the account's `credential_source` runtime hint with a precise locator (`shell_rc:/path`, `opencode_auth_json`) so you can audit where a credential came from with `openusage detect`.

### 4. OS keychain probes

| Source | Where | What it does |
|---|---|---|
| macOS keychain | `Claude Code-credentials` generic password (Anthropic's Claude Code CLI) | Annotates the existing `claude-code` account with `credential_source: keychain:Claude Code-credentials`, or creates a minimal one if file detection missed it (e.g. when the binary isn't on `$PATH` over SSH). The secret value itself is read by the `claude_code` provider at fetch time, not at detect time. |

## Inspecting what was detected

Run the dedicated subcommand to see exactly what the pipeline found, including which file, env var, or keychain entry every credential came from. Tokens are masked; nothing is written to disk.

```
$ openusage detect
Tools detected:
  Claude Code CLI          cli  /usr/local/bin/claude
  GitHub Copilot CLI       cli  /usr/local/bin/copilot
  Antigravity CLI          cli  /usr/local/bin/agy

Accounts detected:
  PROVIDER     ACCOUNT       AUTH     CREDENTIAL                   SOURCE
  claude_code  claude-code   local    -                            keychain:Claude Code-credentials
  copilot      copilot       cli      -                            file:~/.config/gh/hosts.yml
  openai       openai        api_key  $OPENAI_API_KEY=sk-t...cdef  env
  opencode     opencode      api_key  oc_s...2haW                  opencode_auth_json

No credentials found for:
  - anthropic
  - groq
  …
```

Pass `--all` to also list every provider in the registry. The same logic runs on dashboard startup; set `OPENUSAGE_DEBUG=1 openusage` to see the per-source `[detect]` log lines instead.

## Merging with manual configuration

`settings.json` accepts an `accounts` array. When you launch the dashboard, the resolver:

1. Loads manually configured accounts.
2. Runs auto-detection.
3. **Manual wins.** For each `(provider, id)` pair, the manual entry takes precedence. Detected accounts that don't conflict are appended.

That means you can:

- **Disable a detected provider** by setting `auto_detect: false` (turns off pass 1–3 entirely).
- **Override a detected account** by declaring an account with the same `id` and overriding fields like `base_url` or `probe_model`.
- **Add a second account for a provider** by giving it a different `id` and pointing `api_key_env` at a different env var.

```json
{
  "auto_detect": true,
  "accounts": [
    {
      "id": "openai-work",
      "provider": "openai",
      "api_key_env": "OPENAI_WORK_KEY",
      "probe_model": "gpt-4.1-mini"
    }
  ]
}
```

In the example above, auto-detection still creates `openai-default` from `OPENAI_API_KEY` if set, and `openai-work` runs alongside it from the manually declared env var.

## When detection misses something

If a provider you expected does not show up, walk through:

1. Run `openusage detect` and check the "No credentials found for:" list — that's the authoritative inventory of what's missing.
2. Is the env var either exported in your shell *or* present in one of the file sources above? `openusage detect` will show the `SOURCE` column when something is picked up.
3. Is the binary on the same `$PATH` OpenUsage sees? `which claude` from the same shell.
4. Did the tool's config dir get created? Run the tool once before relaunching.
5. Run `OPENUSAGE_DEBUG=1 openusage` and look at stderr for skipped detections — every adoption logs `[detect] credential_source=...`.

See [provider not detected](../troubleshooting/provider-not-detected.md) for a per-provider checklist.

## What detection does and does not do

- It **does** read raw API key values from a small set of documented locations: shell rc files and OpenCode `auth.json`. Adopted values live only in memory under the runtime-only `Token` field (`json:"-"`) — they are never written to `settings.json`.
- It **does not** invoke any shell or run any user code; shell rc parsing skips lines that would require expansion.
- It **does not** make network calls during detection itself; that only happens when a provider's `Fetch()` runs.
- It **does not** read the secret value of OS keychain entries — only their presence. The `claude_code` provider performs the actual keychain read at fetch time.
- It **does not** modify any tool's config (only the integration installer does that).
