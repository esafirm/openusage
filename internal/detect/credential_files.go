package detect

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectCredentialFiles probes a small set of well-known credential files
// that AI CLIs and adjacent tools write outside our existing tool detectors:
//
//   - Claude Code CLI on Linux: ~/.claude/.credentials.json (JSON with
//     accessToken/refreshToken), the file equivalent of the macOS
//     "Claude Code-credentials" keychain entry.
//   - GitHub CLI: ~/.config/gh/hosts.yml. Plaintext fallback when the system
//     keychain is unavailable, contains a usable OAuth token.
//
// We never extract OAuth refresh values into Token here. Those need a
// provider-specific refresh exchange before they're usable. We surface
// presence with a credential_source hint so the user can see "yes, OpenUsage
// found your credential" via `openusage detect`.
func detectCredentialFiles(result *Result) {
	probeClaudeCodeCredentialsFile(result)
	probeGHHostsFile(result)
}

// probeClaudeCodeCredentialsFile annotates / creates a claude-code account
// when the OAuth credentials file is present. Skipped on macOS — the
// keychain probe in keychain_darwin.go covers that platform.
func probeClaudeCodeCredentialsFile(result *Result) {
	if runtime.GOOS == "darwin" {
		return
	}
	home := homeDir()
	if home == "" {
		return
	}
	path := filepath.Join(home, ".claude", ".credentials.json")
	if !fileExists(path) {
		return
	}

	// Quick parse to confirm it has an accessToken — avoids annotating on a
	// truncated / aborted-login file. We don't expose the value.
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[detect] claude code credentials read error: %v", err)
		return
	}
	var creds struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(data, &creds); err != nil || creds.AccessToken == "" {
		return
	}

	source := "file:" + path
	annotateOrCreateAccount(result, "claude-code", "claude_code", "local", source,
		filepath.Join(home, ".claude"))
	log.Printf("[detect] claude code credentials present at %s", path)
}

// probeGHHostsFile annotates / creates a copilot account when the gh CLI
// stored an OAuth token in plaintext (no system keychain available, e.g.
// CI / SSH boxes).
func probeGHHostsFile(result *Result) {
	home := homeDir()
	if home == "" {
		return
	}
	path := ghHostsPath(home)
	if path == "" || !fileExists(path) {
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[detect] gh hosts.yml read error: %v", err)
		return
	}
	// hosts.yml is keyed by hostname; we only care about github.com having
	// an oauth_token. Top-level structure is `<host>: { oauth_token: ..., user: ... }`.
	var hosts map[string]struct {
		OAuthToken string `yaml:"oauth_token"`
	}
	if err := yaml.Unmarshal(data, &hosts); err != nil {
		log.Printf("[detect] gh hosts.yml parse error: %v", err)
		return
	}
	github, ok := hosts["github.com"]
	if !ok || github.OAuthToken == "" {
		return
	}

	source := "file:" + path
	annotateOrCreateAccount(result, "copilot", "copilot", "cli", source, "")
	log.Printf("[detect] gh hosts.yml github.com oauth_token present (%s)", MaskKey(github.OAuthToken))
}

// ghHostsPath returns the path to gh CLI's hosts.yml on macOS and Linux.
func ghHostsPath(home string) string {
	return filepath.Join(home, ".config", "gh", "hosts.yml")
}

// annotateOrCreateAccount sets credential_source on an existing account with
// the given ID, or registers a minimal new account if none exists. Used by
// file-based and keychain-style detectors that surface presence without a
// directly-usable Token.
func annotateOrCreateAccount(result *Result, accountID, provider, auth, source, defaultConfigDir string) {
	for i := range result.Accounts {
		if result.Accounts[i].ID == accountID {
			result.Accounts[i].SetHint("credential_source", source)
			return
		}
	}
	acct := core.AccountConfig{
		ID:       accountID,
		Provider: provider,
		Auth:     auth,
	}
	acct.SetHint("credential_source", source)
	if defaultConfigDir != "" {
		acct.SetPath("config_dir", defaultConfigDir)
	}
	addAccount(result, acct)
}
