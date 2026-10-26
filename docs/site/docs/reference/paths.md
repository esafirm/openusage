---
title: Paths reference
description: Every file and directory OpenUsage reads or writes, by operating system.
---

# Paths reference

OpenUsage follows the [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/) on Linux and macOS. Every path below can be overridden. See the **Override** column.

## OpenUsage paths

| Path | Purpose | Override |
|---|---|---|
| `~/.config/openusage/settings.json` | Main config file. | — |
| `~/.config/openusage/settings.json.corrupt` | Copy of a `settings.json` OpenUsage had to salvage, kept for inspection. Overwritten on each repair. See [common issues](../troubleshooting/common-issues.md). | — |
| `~/.config/openusage/custom-pricing.json` | User pricing overrides. | `OPENUSAGE_CUSTOM_PRICING`, `XDG_CONFIG_HOME` |
| `~/.config/openusage/themes/` | External themes directory (scanned for `*.json`). | `OPENUSAGE_THEME_DIR` (extra dirs only) |
| `~/.config/openusage/hooks/` | Hook scripts installed by `openusage integrations`. | — |
| `~/.local/state/openusage/` | State directory (DB, socket, spool, logs). | `XDG_STATE_HOME` |
| `~/.local/state/openusage/telemetry.db` | Daemon SQLite store. | `--db-path` |
| `~/.local/state/openusage/telemetry.sock` | Daemon Unix domain socket. | `--socket-path`, `OPENUSAGE_TELEMETRY_SOCKET` |
| `~/.local/state/openusage/telemetry-spool/` | Hook spool — events queued while the daemon is offline. | `--spool-dir` |
| `~/.local/state/openusage/daemon.stdout.log` | Daemon stdout when running as a service. | — |
| `~/.local/state/openusage/daemon.stderr.log` | Daemon stderr when running as a service. | — |

## Service files

| Path | OS | Purpose |
|---|---|---|
| `~/Library/LaunchAgents/com.openusage.telemetryd.plist` | macOS | launchd unit. Label `com.openusage.telemetryd`. |
| `~/.config/systemd/user/openusage-telemetry.service` | Linux | systemd-user unit. |

Created by `openusage telemetry daemon install`, removed by `openusage telemetry daemon uninstall`.

## Tool integration paths

These belong to the third-party tools OpenUsage hooks into.

| Path | Tool | Purpose | Override |
|---|---|---|---|
| `~/.claude/settings.json` | Claude Code | Hook registration. | `CLAUDE_SETTINGS_FILE` |
| `~/.config/opencode/opencode.json` or `opencode.jsonc` | OpenCode | Read only. Plugin registration is not required — OpenCode auto-discovers the plugin directory. Legacy entries written by openusage < 0.24.5 are still honored and removed on uninstall. | — |
| `~/.config/opencode/plugins/openusage-telemetry.ts` | OpenCode | Plugin source installed by `integrations install opencode`. | — |
| `~/.local/share/opencode/auth.json` | OpenCode | API keys adopted by auto-detection (OpenCode's data dir). | `XDG_DATA_HOME` |

## Per-OS expansion

### macOS

| Logical path | Resolved |
|---|---|
| Config dir | `~/.config/openusage/` (hardcoded; `XDG_CONFIG_HOME` is not honored) |
| State dir | `~/.local/state/openusage/` (or `$XDG_STATE_HOME/openusage/` if set) |
| Service file | `~/Library/LaunchAgents/com.openusage.telemetryd.plist` |

### Linux

| Logical path | Resolved |
|---|---|
| Config dir | `~/.config/openusage/` (hardcoded; `XDG_CONFIG_HOME` is not honored) |
| State dir | `~/.local/state/openusage/` (or `$XDG_STATE_HOME/openusage/` if set) |
| Service file | `~/.config/systemd/user/openusage-telemetry.service` |
| Logs | Files plus `journalctl --user-unit openusage-telemetry.service` |

## Theme search order

Themes are loaded in this order; later files with the same `name` override earlier ones:

1. Built-in themes compiled into the binary.
2. `<config_dir>/themes/*.json` - that is, `~/.config/openusage/themes/` on Linux and macOS.
3. Each path in `OPENUSAGE_THEME_DIR`, separated by `:`.

See [External themes](../customization/external-themes.md).

## See also

- [Environment variables](./env-vars.md) — every override variable
- [Daemon overview](../daemon/overview.md) — how the daemon uses the state directory
- [Configuration reference](./configuration.md) — what lives in `settings.json`
