package claude_code

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3" // already in go.mod for opencode telemetry

	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/term"
)

type usageResponse struct {
	FiveHour          *usageBucket `json:"five_hour"`
	SevenDay          *usageBucket `json:"seven_day"`
	SevenDaySonnet    *usageBucket `json:"seven_day_sonnet"`
	SevenDayOpus      *usageBucket `json:"seven_day_opus"`
	SevenDayCowork    *usageBucket `json:"seven_day_cowork"`
	SevenDayOAuthApps *usageBucket `json:"seven_day_oauth_apps"`
	ExtraUsage        *usageBucket `json:"extra_usage"`
}

type usageBucket struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

func getClaudeSessionCookies() (map[string]string, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("cookie extraction only supported on macOS")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	cookiesPath := filepath.Join(home, "Library", "Application Support", "Claude", "Cookies")
	if _, err := os.Stat(cookiesPath); os.IsNotExist(err) {
		// Check the file before touching the keychain. The "Claude Safe
		// Storage" item can exist without the desktop app ever writing its
		// cookie DB, and a keychain read prompts or blocks.
		return nil, fmt.Errorf("Claude desktop Cookies DB not found: %s", cookiesPath)
	}

	encKey, err := getChromiumEncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("getting encryption key: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "claude-cookies-*.db")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	srcData, err := os.ReadFile(cookiesPath)
	if err != nil {
		return nil, fmt.Errorf("reading cookies DB: %w", err)
	}
	if err := os.WriteFile(tmpPath, srcData, 0600); err != nil {
		return nil, fmt.Errorf("writing temp cookies DB: %w", err)
	}

	db, err := sql.Open("sqlite3", tmpPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("opening cookies DB: %w", err)
	}
	defer db.Close()

	wantCookies := []string{"sessionKey", "cf_clearance", "anthropic-device-id", "lastActiveOrg", "__cf_bm"}
	placeholders := make([]string, len(wantCookies))
	args := make([]interface{}, len(wantCookies))
	for i, name := range wantCookies {
		placeholders[i] = "?"
		args[i] = name
	}

	query := fmt.Sprintf(
		"SELECT name, encrypted_value FROM cookies WHERE host_key LIKE '%%claude.ai%%' AND name IN (%s)",
		strings.Join(placeholders, ","),
	)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying cookies: %w", err)
	}
	defer rows.Close()

	cookies := make(map[string]string)
	for rows.Next() {
		var name string
		var encValue []byte
		if err := rows.Scan(&name, &encValue); err != nil {
			return nil, fmt.Errorf("scan cookie row: %w", err)
		}
		decrypted, err := decryptChromiumCookie(encValue, encKey)
		if err != nil {
			continue // skip cookies we can't decrypt
		}
		cookies[name] = decrypted
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cookie rows: %w", err)
	}

	if _, ok := cookies["sessionKey"]; !ok {
		return nil, fmt.Errorf("sessionKey cookie not found (Claude desktop app may not be logged in)")
	}

	return cookies, nil
}

// claudeSafeStorageAccounts are the Keychain "account" values Claude
// Desktop has stored its Chromium Safe Storage password under across
// releases. Older builds used "Claude"; current builds use "Claude Key".
// Try each in order so we work across versions.
var claudeSafeStorageAccounts = []string{"Claude Key", "Claude"}

// claudeDesktopCookieAuthAllowed reports whether the macOS desktop-cookie
// source may run. The source can read the login keychain, which prompts or
// blocks in a background process, so it is limited to interactive setup
// commands. Set OPENUSAGE_CLAUDE_DESKTOP_COOKIES=1 to force it on, or =0 to
// force it off.
func claudeDesktopCookieAuthAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OPENUSAGE_CLAUDE_DESKTOP_COOKIES"))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func getChromiumEncryptionKey() ([]byte, error) {
	var lastErr error
	for _, account := range claudeSafeStorageAccounts {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := exec.CommandContext(ctx, "security", "find-generic-password", "-w", "-s", "Claude Safe Storage", "-a", account)
		out, err := cmd.Output()
		cancel()
		if err != nil {
			if !keychainItemNotFound(err) {
				return nil, fmt.Errorf("keychain lookup for account %q failed: %w", account, err)
			}
			lastErr = err
			continue
		}
		password := strings.TrimSpace(string(out))
		key := pbkdf2.Key([]byte(password), []byte("saltysalt"), 1003, 16, sha1.New)
		return key, nil
	}
	if lastErr == nil {
		return nil, fmt.Errorf("keychain lookup failed: no Safe Storage account names configured")
	}
	return nil, fmt.Errorf("keychain lookup failed (is Claude desktop installed and signed in?): %w", lastErr)
}

func keychainItemNotFound(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}

	message := strings.ToLower(string(exitErr.Stderr))
	return strings.Contains(message, "could not be found") || strings.Contains(message, "item not found")
}

func decryptChromiumCookie(encrypted []byte, key []byte) (string, error) {
	if len(encrypted) < 3 {
		return "", fmt.Errorf("encrypted value too short")
	}

	prefix := string(encrypted[:3])
	if prefix != "v10" {
		return "", fmt.Errorf("unexpected cookie encryption version: %q", prefix)
	}
	ciphertext := encrypted[3:]

	if len(ciphertext) == 0 {
		return "", fmt.Errorf("empty ciphertext after prefix")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating AES cipher: %w", err)
	}

	iv := []byte("                ") // 16 spaces

	if len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("ciphertext not aligned to block size")
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	if len(plaintext) == 0 {
		return "", fmt.Errorf("empty plaintext")
	}
	padLen := int(plaintext[len(plaintext)-1])
	if padLen > aes.BlockSize || padLen > len(plaintext) || padLen == 0 {
		return "", fmt.Errorf("invalid PKCS7 padding")
	}
	plaintext = plaintext[:len(plaintext)-padLen]

	const chromiumPrefixLen = 32
	if len(plaintext) <= chromiumPrefixLen {
		return "", fmt.Errorf("decrypted value too short after padding removal (len=%d)", len(plaintext))
	}
	plaintext = plaintext[chromiumPrefixLen:]

	return string(plaintext), nil
}

// fetchUsageAPIWithAuth performs the GET/parse round trip shared by every
// usage-API source (cookie-authenticated org endpoint, OAuth-authenticated
// account endpoint): only how the request is authenticated differs between
// them, so callers supply that as a closure over an already-resolved
// credential rather than duplicating the request/response plumbing.
func fetchUsageAPIWithAuth(ctx context.Context, url string, setAuth func(*http.Request)) (*usageResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var usage usageResponse
	if err := json.Unmarshal(body, &usage); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &usage, nil
}

// oauthUsageURL is Anthropic's OAuth-scoped usage endpoint. It is a package
// variable rather than a constant so tests can point it at an httptest server.
var oauthUsageURL = "https://api.anthropic.com/api/oauth/usage"

// claudeCodeKeychainService is the macOS login-keychain item current Claude
// Code releases write their OAuth credentials to. It mirrors the probe in
// internal/detect.
const claudeCodeKeychainService = "Claude Code-credentials"

// claudeKeychainTokenReader is a test seam for the macOS keychain lookup.
var claudeKeychainTokenReader = readClaudeCodeKeychainToken

// readClaudeCodeOAuthToken loads the Claude Code CLI's OAuth access token.
//
// Current macOS releases store it in the login keychain, while older releases
// and non-macOS installs keep it in ~/.claude/.credentials.json. Try the
// keychain first on macOS: once the CLI switches to keychain storage the file
// copy stops being refreshed and goes stale. Fall back to the file so older
// installs keep working.
//
// It errors if neither source has a token, or if the token has already
// expired (Claude Code refreshes it on next use, so a stale value would only
// produce 401s).
func readClaudeCodeOAuthToken() (string, error) {
	var keychainErr error
	if runtime.GOOS == "darwin" {
		token, err := claudeKeychainTokenReader()
		if err == nil {
			return token, nil
		}
		keychainErr = err
	}

	token, fileErr := readClaudeCodeOAuthTokenFile()
	if fileErr == nil {
		return token, nil
	}
	if keychainErr != nil {
		return "", fmt.Errorf("%v; %w", keychainErr, fileErr)
	}
	return "", fileErr
}

// readClaudeCodeOAuthTokenFile reads the pre-keychain credentials file.
func readClaudeCodeOAuthTokenFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}

	credsPath := filepath.Join(home, ".claude", ".credentials.json")
	credsData, err := os.ReadFile(credsPath)
	if err != nil {
		return "", fmt.Errorf("reading Claude Code credentials: %w", err)
	}
	return parseClaudeOAuthToken(credsData, credsPath)
}

// readClaudeCodeKeychainToken shells out to the macOS security CLI. The
// timeout keeps a locked keychain or a UI prompt from blocking the daemon's
// poll loop; the caller falls back to the credentials file on failure.
func readClaudeCodeKeychainToken() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "/usr/bin/security",
		"find-generic-password", "-w", "-s", claudeCodeKeychainService).Output()
	if err != nil {
		if keychainItemNotFound(err) {
			return "", fmt.Errorf("no %q item in keychain", claudeCodeKeychainService)
		}
		return "", fmt.Errorf("reading %q from keychain: %w", claudeCodeKeychainService, err)
	}

	return parseClaudeOAuthToken(out, "keychain "+claudeCodeKeychainService)
}

// parseClaudeOAuthToken extracts and validates the OAuth access token from
// the Claude Code credentials JSON (file or keychain, same shape).
func parseClaudeOAuthToken(data []byte, source string) (string, error) {
	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("parsing Claude Code credentials from %s: %w", source, err)
	}
	token := creds.ClaudeAiOauth.AccessToken
	if token == "" {
		return "", fmt.Errorf("no OAuth access token in %s", source)
	}
	if exp := creds.ClaudeAiOauth.ExpiresAt; exp > 0 && time.Now().UnixMilli() >= exp {
		return "", fmt.Errorf("Claude Code OAuth token from %s expired (refreshed on next Claude Code use)", source)
	}
	return token, nil
}
