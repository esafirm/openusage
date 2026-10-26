package detect

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/samber/lo"
)

type DetectedTool struct {
	Name       string // e.g. "Copilot CLI", "Claude Code CLI"
	BinaryPath string // resolved path to binary, if applicable
	ConfigDir  string // path to the tool's config directory
	Type       string // "ide", "cli", "api"
}

type Result struct {
	Tools    []DetectedTool
	Accounts []core.AccountConfig

	// accountIDs is an internal index used by addAccount to avoid the
	// quadratic lo.ContainsBy scan over Accounts. Always in sync with the
	// IDs in Accounts. Not exported; not part of the wire format.
	accountIDs map[string]struct{} `json:"-"`
}

func AutoDetect() Result {
	var result Result

	// Phase 1: tool-binding detectors for the supported provider set.
	detectClaudeCode(&result)
	detectGHCopilot(&result)
	detectAntigravity(&result)

	// Phase 2: process env vars. Most authoritative; runs before any
	// file-based credential adoption so a freshly-set env var always
	// overrides stale values found in dotfiles.
	detectEnvKeys(&result)

	// Phase 3: file-based credential adoption. Each detector here
	// re-checks os.Getenv per-var so it skips anything Phase 2 already
	// adopted, and addAccount is idempotent on account ID.
	detectShellRC(&result)
	detectOpenCodeAuth(&result)

	// Phase 4: credential-store probes. We only annotate accounts (or
	// create minimal placeholders) — the providers themselves still read
	// the secret value at fetch time.
	detectMacOSKeychainCredentials(&result)
	detectCredentialFiles(&result)

	// Phase 5: browser-session credentials for console-only data. Runs
	// last so it can attach to an account created by any earlier phase.
	detectOpenCodeConsoleSession(&result)

	return result
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

func findBinary(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		for _, dir := range candidateBinaryDirs() {
			candidate := filepath.Join(dir, name)
			if isExecutableFile(candidate) {
				return candidate
			}
		}
		return ""
	}
	return path
}

func candidateBinaryDirs() []string {
	var dirs []string

	// When OPENUSAGE_DETECT_BIN_DIRS is explicitly set (even to empty), use
	// only its dirs + PATH and skip hardcoded system dirs. This gives tests
	// full control over binary lookup isolation.
	customVal, customSet := os.LookupEnv("OPENUSAGE_DETECT_BIN_DIRS")
	if customSet && customVal != "" {
		parts := strings.Split(customVal, string(os.PathListSeparator))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				dirs = append(dirs, part)
			}
		}
	}

	if pathEnv := strings.TrimSpace(os.Getenv("PATH")); pathEnv != "" {
		for _, part := range strings.Split(pathEnv, string(os.PathListSeparator)) {
			part = strings.TrimSpace(part)
			if part != "" {
				dirs = append(dirs, part)
			}
		}
	}

	if !customSet {
		home := homeDir()
		if home != "" {
			dirs = append(dirs, userBinaryDirs(home)...)
		}

		dirs = append(dirs,
			"/opt/homebrew/bin",
			"/home/linuxbrew/.linuxbrew/bin",
			"/usr/local/bin",
			"/usr/bin",
			"/bin",
			"/snap/bin",
		)
	}
	return lo.Uniq(dirs)
}

// userBinaryDirs lists per-user install locations used by the common
// package managers for CLI tools (npm, nvm, volta, pnpm, bun, yarn, cargo).
// The telemetry daemon runs under launchd/systemd with a minimal PATH, so
// tools installed there are invisible to exec.LookPath in the daemon even
// though they resolve fine from the user's shell.
func userBinaryDirs(home string) []string {
	dirs := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "bin"),
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".npm-packages", "bin"),
		filepath.Join(home, ".volta", "bin"),
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, ".yarn", "bin"),
		filepath.Join(home, ".local", "share", "pnpm"),
		filepath.Join(home, "Library", "pnpm"),
		filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, ".local", "share", "fnm", "aliases", "default", "bin"),
	}
	if nvmDir := strings.TrimSpace(os.Getenv("NVM_DIR")); nvmDir != "" {
		dirs = append(dirs, nvmNodeBinDirs(nvmDir)...)
	}
	dirs = append(dirs, nvmNodeBinDirs(filepath.Join(home, ".nvm"))...)
	return dirs
}

// nvmNodeBinDirs returns <nvmDir>/versions/node/*/bin, newest version first.
func nvmNodeBinDirs(nvmDir string) []string {
	matches, err := filepath.Glob(filepath.Join(nvmDir, "versions", "node", "*", "bin"))
	if err != nil || len(matches) == 0 {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool {
		return compareNodeVersionDirs(matches[i], matches[j]) > 0
	})
	return matches
}

// compareNodeVersionDirs compares two .../vX.Y.Z/bin paths by semantic
// version. Unparseable components compare as zero.
func compareNodeVersionDirs(a, b string) int {
	va := parseNodeVersion(filepath.Base(filepath.Dir(a)))
	vb := parseNodeVersion(filepath.Base(filepath.Dir(b)))
	for i := range va {
		if va[i] != vb[i] {
			if va[i] > vb[i] {
				return 1
			}
			return -1
		}
	}
	return strings.Compare(a, b)
}

func parseNodeVersion(name string) [3]int {
	var out [3]int
	parts := strings.SplitN(strings.TrimPrefix(name, "v"), ".", 3)
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err == nil {
			out[i] = n
		}
	}
	return out
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func addAccount(result *Result, acct core.AccountConfig) {
	if result.accountIDs == nil {
		// Lazily build the index. Prevents callers that constructed a
		// Result{} literal (tests) from blowing up.
		result.accountIDs = make(map[string]struct{}, len(result.Accounts)+1)
		for _, existing := range result.Accounts {
			result.accountIDs[existing.ID] = struct{}{}
		}
	}
	if _, dup := result.accountIDs[acct.ID]; dup {
		return
	}
	result.accountIDs[acct.ID] = struct{}{}
	result.Accounts = append(result.Accounts, acct)
}

func detectGHCopilot(result *Result) {
	home := homeDir()
	if home == "" {
		return
	}

	// Prefer the standalone copilot binary. Checking it costs one LookPath and
	// avoids running `gh`, which can block for seconds on a network or update
	// check. The provider resolves gh separately when it needs quota data.
	copilotBin := findBinary("copilot")
	copilotDir := filepath.Join(home, ".copilot")
	if copilotBin != "" && dirExists(copilotDir) {
		log.Printf("[detect] Found standalone copilot binary at %s", copilotBin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "GitHub Copilot CLI",
			BinaryPath: copilotBin,
			ConfigDir:  copilotDir,
			Type:       "cli",
		})
		addAccount(result, core.AccountConfig{
			ID:       "copilot",
			Provider: "copilot",
			Auth:     "cli",
			Binary:   copilotBin,
			RuntimeHints: map[string]string{
				"copilot_binary": copilotBin,
				"config_dir":     copilotDir,
			},
		})
		return
	}

	// Fall back to the deprecated gh copilot extension. Probe at most once per
	// process so a background poll never pays the exec cost more than once.
	ghBin, ghCopilotOK := probeGHCopilot()
	if !ghCopilotOK {
		log.Printf("[detect] No gh copilot extension or standalone copilot binary found, skipping")
		return
	}

	configDir := filepath.Join(home, ".config", "github-copilot")
	result.Tools = append(result.Tools, DetectedTool{
		Name:       "GitHub Copilot (gh CLI)",
		BinaryPath: ghBin,
		ConfigDir:  configDir,
		Type:       "cli",
	})
	addAccount(result, core.AccountConfig{
		ID:       "copilot",
		Provider: "copilot",
		Auth:     "cli",
		Binary:   ghBin,
	})
}

var ghCopilotProbe struct {
	once sync.Once
	bin  string
	ok   bool
}

func probeGHCopilot() (string, bool) {
	ghCopilotProbe.once.Do(func() {
		ghBin := findBinary("gh")
		if ghBin == "" {
			return
		}
		log.Printf("[detect] Found gh CLI at %s", ghBin)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, ghBin, "copilot", "--version").Run(); err == nil {
			ghCopilotProbe.bin = ghBin
			ghCopilotProbe.ok = true
			return
		}
		log.Printf("[detect] gh copilot extension not installed or timed out")
	})
	return ghCopilotProbe.bin, ghCopilotProbe.ok
}

func detectAntigravity(result *Result) {
	bin := findBinary("agy")
	if bin == "" {
		return
	}

	home := homeDir()
	if home == "" {
		return
	}
	configDir := strings.TrimSpace(os.Getenv("ANTIGRAVITY_CONFIG_DIR"))
	if configDir == "" {
		configDir = filepath.Join(home, ".gemini", "antigravity-cli")
	}
	if !dirExists(configDir) {
		log.Printf("[detect] Antigravity CLI config dir %s not found, skipping", configDir)
		return
	}

	settingsFile := filepath.Join(configDir, "settings.json")
	brainDir := filepath.Join(configDir, "brain")
	if !fileExists(settingsFile) && !dirExists(brainDir) {
		log.Printf("[detect] Antigravity CLI config dir exists but no settings.json or brain directory found, skipping")
		return
	}

	log.Printf("[detect] Found Antigravity CLI at %s", bin)
	result.Tools = append(result.Tools, DetectedTool{
		Name:       "Antigravity CLI",
		BinaryPath: bin,
		ConfigDir:  configDir,
		Type:       "cli",
	})

	acct := core.AccountConfig{
		ID:           "antigravity",
		Provider:     "antigravity",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	acct.SetHint("config_dir", configDir)
	addAccount(result, acct)
}

// envKeyMappingEntry is the single source of truth for "this env var name
// belongs to this provider/account". Every file-based detector that adopts a
// raw API key funnels through this table.
type envKeyMappingEntry struct {
	EnvVar    string
	Provider  string
	AccountID string
}

var envKeyMapping = []envKeyMappingEntry{
	{EnvVar: "OPENAI_API_KEY", Provider: "openai", AccountID: "openai"},
	{EnvVar: "ZEN_API_KEY", Provider: "opencode", AccountID: "opencode"},
	{EnvVar: "OPENCODE_API_KEY", Provider: "opencode", AccountID: "opencode"},
}

// envKeyByVar indexes envKeyMapping by env-var name for O(1) lookup. Built
// once at init.
var envKeyByVar = func() map[string]envKeyMappingEntry {
	out := make(map[string]envKeyMappingEntry, len(envKeyMapping))
	for _, m := range envKeyMapping {
		out[m.EnvVar] = m
	}
	return out
}()

// adoptAPIKey is the shared "register an api_key account from a known env-var
// mapping" path. Used by every file-based detector (shell rc, Aider .env /
// YAML, future Tier-1 detectors). Honours "process env wins" by short-
// circuiting when the env var is already set, defers to addAccount's
// id-dedupe for cross-detector precedence, and emits a uniform masked log
// line on success.
func adoptAPIKey(result *Result, mapping envKeyMappingEntry, value, source string) {
	if os.Getenv(mapping.EnvVar) != "" {
		return
	}
	acct := core.AccountConfig{
		ID:        mapping.AccountID,
		Provider:  mapping.Provider,
		Auth:      "api_key",
		APIKeyEnv: mapping.EnvVar,
		Token:     value,
	}
	acct.SetHint("credential_source", source)

	before := len(result.Accounts)
	addAccount(result, acct)
	if len(result.Accounts) > before {
		log.Printf("[detect] %s → %s/%s (%s=%s)",
			source, mapping.Provider, mapping.AccountID, mapping.EnvVar, MaskKey(value))
	}
}

func detectEnvKeys(result *Result) {
	for _, mapping := range envKeyMapping {
		val := os.Getenv(mapping.EnvVar)
		if val == "" {
			continue
		}

		log.Printf("[detect] Found %s=%s", mapping.EnvVar, MaskKey(val))

		addAccount(result, core.AccountConfig{
			ID:        mapping.AccountID,
			Provider:  mapping.Provider,
			Auth:      "api_key",
			APIKeyEnv: mapping.EnvVar,
		})
	}
}

// ApplyCredentials fills in Token for accounts that have no API key from env vars,
// using stored credentials from the credentials file. It also creates new accounts
// for stored credentials that don't match any existing account.
func ApplyCredentials(result *Result) {
	creds, err := config.LoadCredentials()
	if err != nil {
		log.Printf("[detect] Failed to load credentials: %v", err)
		return
	}
	if len(creds.Keys) == 0 {
		return
	}

	// Apply to existing accounts
	applied := make(map[string]bool, len(result.Accounts))
	for i := range result.Accounts {
		acct := &result.Accounts[i]
		if acct.Token != "" || acct.ResolveAPIKey() != "" {
			applied[acct.ID] = true
			continue
		}
		if key, ok := creds.Keys[acct.ID]; ok {
			acct.Token = key
			applied[acct.ID] = true
			log.Printf("[detect] Applied stored credential for %s", acct.ID)
		}
	}

	// Create accounts for stored credentials that don't match any existing account
	for accountID, key := range creds.Keys {
		if applied[accountID] {
			continue
		}
		provider := providerForStoredCredential(accountID)
		if provider == "" {
			log.Printf("[detect] Stored credential for unknown account %s, skipping", accountID)
			continue
		}
		result.Accounts = append(result.Accounts, core.AccountConfig{
			ID:       accountID,
			Provider: provider,
			Auth:     "api_key",
			Token:    key,
		})
		log.Printf("[detect] Created account %s from stored credential", accountID)
	}
}

// providerForStoredCredential maps a stored credential's account ID to its
// provider. Linear scan over envKeyMapping; the table is small and this runs
// at most once per stored credential.
func providerForStoredCredential(accountID string) string {
	for _, mapping := range envKeyMapping {
		if mapping.AccountID == accountID {
			return mapping.Provider
		}
	}
	return ""
}

func (r Result) Summary() string {
	var sb strings.Builder
	if len(r.Tools) > 0 {
		sb.WriteString(fmt.Sprintf("Detected %d tool(s):\n", len(r.Tools)))
		for _, t := range r.Tools {
			sb.WriteString(fmt.Sprintf("  • %s (%s)", t.Name, t.Type))
			if t.BinaryPath != "" {
				sb.WriteString(fmt.Sprintf(" at %s", t.BinaryPath))
			}
			sb.WriteString("\n")
		}
	}
	if len(r.Accounts) > 0 {
		sb.WriteString(fmt.Sprintf("Auto-configured %d account(s):\n", len(r.Accounts)))
		for _, a := range r.Accounts {
			sb.WriteString(fmt.Sprintf("  • %s (provider: %s)\n", a.ID, a.Provider))
		}
	}
	if len(r.Tools) == 0 && len(r.Accounts) == 0 {
		sb.WriteString("No AI tools or API keys detected on this workstation.\n")
	}
	return sb.String()
}
