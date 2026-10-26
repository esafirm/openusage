---
title: CLI reference
description: The openusage commands and subcommands in this build.
---

# CLI reference

This build supports the dashboard, the telemetry daemon, integrations, and
setup helpers. Provider support is limited to Claude Code, OpenCode,
Antigravity, OpenAI, and GitHub Copilot.

## Top-level

```
openusage                                       # run the dashboard (default)
openusage version                               # print version and build info
openusage detect [--all]                        # print credential auto-detection report
openusage antigravity statusline [--state-file] # capture an agy status line
openusage telemetry hook <source> [flags]       # forward an event from a tool hook
openusage telemetry daemon <subcommand> [flags] # daemon lifecycle
openusage integrations <subcommand> [flags]     # tool integration management
```

## `openusage`

Runs the TUI dashboard. With no flags it auto-detects accounts, connects to the
daemon over its Unix socket, and opens the dashboard. If the daemon is not
installed yet, run `openusage telemetry daemon install` first.

The dashboard needs an interactive terminal on both stdin and stdout. If either
is redirected or piped, it exits with status 2 and a one-line error instead of
opening `/dev/tty`.

On `SIGINT`, `SIGTERM`, or `SIGHUP` the dashboard quits cleanly and restores the
terminal. A second signal forces it to exit.

### Flags

The default command takes no flags beyond cobra's built-ins. Configuration lives
in `~/.config/openusage/settings.json`.

## `openusage version`

```
openusage version
```

Prints the binary version, commit, and build date. Useful for bug reports.

## `openusage detect`

Runs the same auto-detection pipeline used at dashboard startup and prints a
report:

- **Tools detected** - name, type, and binary path.
- **Accounts detected** - provider, account ID, auth mode, masked credential,
  and a `SOURCE` column with the precise locator.
- **No credentials found for** - every registered provider that produced no
  account.

```
openusage detect
openusage detect --all      # also list every registered provider
```

Tokens are masked (`first4...last4`) and nothing is written to disk.

## `openusage antigravity statusline`

Reads the Antigravity status-line JSON from stdin, saves the latest snapshot for
the provider, and prints the compact `AGY` line back to Antigravity.

```
agy --output-format json | openusage antigravity statusline
cat statusline.json | openusage antigravity statusline --state-file /tmp/agy.json
```

## `openusage telemetry`

```
openusage telemetry hook <source> [flags]       # forward a hook payload
openusage telemetry daemon run [flags]          # run the daemon in the foreground
openusage telemetry daemon install [flags]      # install launchd/systemd user service
openusage telemetry daemon status               # show service status
openusage telemetry daemon uninstall            # remove the service
```

Supported hook sources are `claude_code`, `opencode`, `antigravity`, and
`copilot`.

## `openusage integrations`

```
openusage integrations list [--all]     # list integration statuses
openusage integrations install <id>     # install hook/plugin
openusage integrations uninstall <id>   # remove
openusage integrations upgrade <id>     # upgrade to the embedded version
```

Supported integration IDs are `claude_code`, `opencode`, and `antigravity`.
