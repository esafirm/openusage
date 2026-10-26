---
title: Ways to use OpenUsage
description: The surfaces this build of OpenUsage exposes: the live terminal dashboard, the background daemon, and tool integrations.
sidebar_position: 2
sidebar_label: Ways to use it
---

This build of OpenUsage focuses on the live dashboard and the background
daemon. Both read the same five providers and the same local history.

| Surface | Command | Use it when |
| --- | --- | --- |
| [Live dashboard](#live-terminal-dashboard) | `openusage` | You want the interactive view |
| [Background daemon](#always-on-background-daemon) | `openusage telemetry daemon install` | You want history over time |
| [Tool integrations](#tool-integrations) | `openusage integrations install <id>` | You want richer real-time data |

## Live terminal dashboard

The default. Auto-detects your tools and shows live tiles with a master/detail
panel and an Analytics screen (Tab).

```bash
openusage
```

See [first run](./first-run.md).

## Always-on background daemon

The daemon polls providers, ingests hook events, and keeps history in a local
SQLite database. The dashboard reads from it over a Unix socket.

```bash
openusage telemetry daemon install
openusage telemetry daemon status
```

See the [daemon overview](../daemon/overview.md).

## Tool integrations

Optional hooks and bridges add richer real-time data:

```bash
openusage integrations install claude_code
openusage integrations install opencode
openusage integrations install antigravity
```

OpenCode Go quota needs a console browser session. Open **Settings -> 5 KEYS ->
opencode** and press `c`.

## See also

- [Providers](../providers/index.md)
- [Configuration](../reference/configuration.md)
- [Keybindings](../customization/keybindings.md)
