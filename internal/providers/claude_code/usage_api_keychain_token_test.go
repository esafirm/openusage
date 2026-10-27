package claude_code

import (
	"errors"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// stubKeychainToken swaps the keychain reader for the duration of a test.
func stubKeychainToken(t *testing.T, token string, err error) {
	t.Helper()
	original := claudeKeychainTokenReader
	claudeKeychainTokenReader = func() (string, error) { return token, err }
	t.Cleanup(func() { claudeKeychainTokenReader = original })
}

// TestReadClaudeCodeOAuthToken_PrefersKeychainOnDarwin covers the current
// macOS layout: Claude Code refreshes the keychain item while the legacy
// credentials file stays behind with an expired token.
func TestReadClaudeCodeOAuthToken_PrefersKeychainOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("keychain source is macOS-only")
	}

	past := strconv.FormatInt(time.Now().Add(-time.Hour).UnixMilli(), 10)
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"stale-file-token","expiresAt":`+past+`}}`)
	stubKeychainToken(t, "fresh-keychain-token", nil)

	got, err := readClaudeCodeOAuthToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "fresh-keychain-token" {
		t.Fatalf("token = %q, want keychain token", got)
	}
}

// TestReadClaudeCodeOAuthToken_FallsBackToFile covers older installs where the
// token only exists in ~/.claude/.credentials.json.
func TestReadClaudeCodeOAuthToken_FallsBackToFile(t *testing.T) {
	future := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"file-token","expiresAt":`+future+`}}`)
	stubKeychainToken(t, "", errors.New("no keychain item"))

	got, err := readClaudeCodeOAuthToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "file-token" {
		t.Fatalf("token = %q, want file token", got)
	}
}

func TestParseClaudeOAuthToken(t *testing.T) {
	future := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	past := strconv.FormatInt(time.Now().Add(-time.Hour).UnixMilli(), 10)

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name: "valid token",
			body: `{"claudeAiOauth":{"accessToken":"tok","expiresAt":` + future + `}}`,
			want: "tok",
		},
		{
			name:    "expired token",
			body:    `{"claudeAiOauth":{"accessToken":"tok","expiresAt":` + past + `}}`,
			wantErr: true,
		},
		{
			name:    "missing token",
			body:    `{"claudeAiOauth":{}}`,
			wantErr: true,
		},
		{
			name:    "malformed json",
			body:    `{`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseClaudeOAuthToken([]byte(tt.body), "test")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got token %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("token = %q, want %q", got, tt.want)
			}
		})
	}
}
