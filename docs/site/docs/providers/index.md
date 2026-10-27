---
title: Providers
description: The AI tools and API platforms this build of OpenUsage tracks.
sidebar_label: Providers
---

# Providers

This build supports six providers. Most are auto-detected on first run. Each
tile on the dashboard maps to one provider page below.

## Coding agents

<div className="provider-grid">
  <a href="./claude-code/">
    <strong>Claude Code</strong>
    <span>Sessions, billing blocks, burn rate, per-model tokens</span>
  </a>
  <a href="./codex/">
    <strong>Codex CLI</strong>
    <span>ChatGPT/Codex plan usage, 5h/7d rate limits, session tokens</span>
  </a>
  <a href="./opencode/">
    <strong>OpenCode</strong>
    <span>OpenCode Go quota, Zen models, console balance, local token usage</span>
  </a>
  <a href="./antigravity/">
    <strong>Antigravity CLI</strong>
    <span>Status-line context, session tokens, model quotas</span>
  </a>
  <a href="./copilot/">
    <strong>GitHub Copilot</strong>
    <span>Chat and completions quota, org billing, session usage</span>
  </a>
</div>

## API platforms

<div className="provider-grid">
  <a href="./openai/">
    <strong>OpenAI</strong>
    <span>RPM and TPM rate limits via header probing</span>
  </a>
</div>

## Detection

- **Claude Code** - `claude` binary plus `~/.claude`
- **OpenCode** - `OPENCODE_API_KEY`, `ZEN_API_KEY`, or `~/.local/share/opencode/auth.json`
- **Antigravity** - `agy` binary plus `~/.gemini/antigravity-cli`
- **GitHub Copilot** - `copilot` binary plus `~/.copilot`, or the `gh copilot` extension
- **OpenAI** - `OPENAI_API_KEY`

OpenCode Go quota needs a console browser session. Open **Settings -> 5 KEYS ->
opencode** and press `c`.
