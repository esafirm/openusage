package main

import (
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/config"
)

func TestCredentialsSetFromStdin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newCredentialsSetCommand()
	cmd.SetIn(strings.NewReader("sk-admin-test\n"))
	cmd.SetArgs([]string{"openai-admin", "--stdin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	creds, err := config.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if got := creds.Keys["openai-admin"]; got != "sk-admin-test" {
		t.Fatalf("openai-admin key = %q, want sk-admin-test", got)
	}
}

func TestCredentialsSetRejectsEmptyKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newCredentialsSetCommand()
	cmd.SetIn(strings.NewReader("  \n"))
	cmd.SetArgs([]string{"openai-admin", "--stdin"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected empty key error")
	}
}
