package integrations

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseIntegrationVersion(t *testing.T) {
	data := []byte("# openusage-integration-version: 2026-02-23.1\n")
	got := parseIntegrationVersion(data)
	if got != "2026-02-23.1" {
		t.Fatalf("parseIntegrationVersion() = %q, want 2026-02-23.1", got)
	}
}

func TestManagerInstallAndListStatuses(t *testing.T) {
	root := t.TempDir()
	dirs := Dirs{
		Home:         root,
		ConfigRoot:   filepath.Join(root, ".config"),
		HooksDir:     filepath.Join(root, ".config", "openusage", "hooks"),
		OpenusageBin: filepath.Join(root, "openusage-bin"),
	}
	// Antigravity reports a missing binary as outdated.
	if err := os.WriteFile(dirs.OpenusageBin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	m := Manager{dirs: dirs}

	for _, id := range []ID{OpenCodeID, ClaudeCodeID, AntigravityID} {
		if err := m.Install(id); err != nil {
			t.Fatalf("Install(%s) error = %v", id, err)
		}
	}

	statuses := m.ListStatuses()
	if len(statuses) != len(AllDefinitions()) {
		t.Fatalf("ListStatuses() returned %d statuses, want %d", len(statuses), len(AllDefinitions()))
	}

	for _, st := range statuses {
		if st.State != "ready" {
			t.Errorf("status for %s: State = %q, want ready", st.ID, st.State)
		}
		if st.InstalledVersion != IntegrationVersion {
			t.Errorf("status for %s: InstalledVersion = %q, want %q", st.ID, st.InstalledVersion, IntegrationVersion)
		}
	}
}

func TestManagerInstallUnknownID(t *testing.T) {
	root := t.TempDir()
	dirs := Dirs{
		Home:         root,
		ConfigRoot:   filepath.Join(root, ".config"),
		HooksDir:     filepath.Join(root, ".config", "openusage", "hooks"),
		OpenusageBin: "/tmp/openusage-bin",
	}
	m := Manager{dirs: dirs}

	err := m.Install("nonexistent")
	if err == nil {
		t.Fatal("Install(nonexistent) should return an error")
	}
}

func TestManagerListStatusesMissing(t *testing.T) {
	root := t.TempDir()
	dirs := Dirs{
		Home:         root,
		ConfigRoot:   filepath.Join(root, ".config"),
		HooksDir:     filepath.Join(root, ".config", "openusage", "hooks"),
		OpenusageBin: "/tmp/openusage-bin",
	}
	m := Manager{dirs: dirs}

	statuses := m.ListStatuses()
	for _, st := range statuses {
		if st.State != "missing" {
			t.Errorf("status for %s: State = %q, want missing", st.ID, st.State)
		}
	}
}
