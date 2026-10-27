package browsercookies

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestCookieIsFresher(t *testing.T) {
	old := time.Date(2026, 10, 22, 15, 46, 0, 0, time.UTC)
	recent := time.Date(2026, 10, 27, 1, 54, 0, 0, time.UTC)

	tests := []struct {
		name string
		a, b Cookie
		want bool
	}{
		{
			name: "last access wins over later expiry",
			a:    Cookie{LastAccess: recent, Expires: old.AddDate(0, 1, 0)},
			b:    Cookie{LastAccess: old, Expires: old.AddDate(0, 2, 0)},
			want: true,
		},
		{
			name: "older last access loses",
			a:    Cookie{LastAccess: old, Expires: old.AddDate(0, 2, 0)},
			b:    Cookie{LastAccess: recent, Expires: old},
		},
		{
			name: "one side has last access",
			a:    Cookie{LastAccess: recent},
			b:    Cookie{Expires: old.AddDate(1, 0, 0)},
			want: true,
		},
		{
			name: "fallback to expiry without last access",
			a:    Cookie{Expires: recent},
			b:    Cookie{Expires: old},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cookieIsFresher(tt.a, tt.b); got != tt.want {
				t.Fatalf("cookieIsFresher() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChromiumLastAccess(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "Cookies")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE cookies (host_key TEXT, name TEXT, path TEXT, last_access_utc INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	accessed := time.Date(2026, 10, 27, 1, 54, 0, 0, time.UTC)
	webkit := accessed.UnixMicro() + 11644473600000000
	if _, err := db.Exec(
		`INSERT INTO cookies (host_key, name, path, last_access_utc) VALUES (?, ?, ?, ?)`,
		"opencode.ai", "__Host-console_session", "/", webkit,
	); err != nil {
		t.Fatalf("insert cookie: %v", err)
	}

	got := chromiumLastAccess("chrome", dbPath, "opencode.ai", "__Host-console_session")
	if !got.Equal(accessed) {
		t.Fatalf("last access = %v, want %v", got, accessed)
	}

	if got := chromiumLastAccess("firefox", dbPath, "opencode.ai", "__Host-console_session"); !got.IsZero() {
		t.Fatalf("firefox last access = %v, want zero", got)
	}
	if got := chromiumLastAccess("chrome", dbPath, "opencode.ai", "missing"); !got.IsZero() {
		t.Fatalf("missing cookie last access = %v, want zero", got)
	}
}
