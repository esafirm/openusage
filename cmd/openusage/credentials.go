package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/browsercookies"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newCredentialsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "credentials",
		Short: "Manage credentials stored in credentials.json",
	}
	cmd.AddCommand(newCredentialsSetCommand())
	cmd.AddCommand(newCredentialsImportSessionCommand())
	return cmd
}

func newCredentialsSetCommand() *cobra.Command {
	var stdin bool
	cmd := &cobra.Command{
		Use:   "set <account-id>",
		Short: "Store an API key for an account",
		Long: strings.Join([]string{
			"Store an API key in credentials.json (0600). The key is read from a",
			"prompt by default, or from stdin with --stdin.",
			"",
			"Examples:",
			"  openusage credentials set openai-admin",
			"  printf '%s' \"$OPENAI_ADMIN_KEY\" | openusage credentials set openai-admin --stdin",
		}, "\n"),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			accountID := strings.TrimSpace(args[0])
			if accountID == "" {
				return fmt.Errorf("account id is required")
			}

			key, err := readCredentialKey(cmd.InOrStdin(), stdin)
			if err != nil {
				return err
			}
			if key == "" {
				return fmt.Errorf("empty API key")
			}
			if err := config.SaveCredential(accountID, key); err != nil {
				return err
			}
			fmt.Printf("Saved credential for %s\n", accountID)
			return nil
		},
	}
	cmd.Flags().BoolVar(&stdin, "stdin", false, "read the key from stdin instead of prompting")
	return cmd
}

func readCredentialKey(in io.Reader, fromStdin bool) (string, error) {
	if fromStdin {
		data, err := io.ReadAll(io.LimitReader(in, 1<<20))
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	fmt.Fprint(os.Stderr, "API key: ")
	data, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading API key: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func newCredentialsImportSessionCommand() *cobra.Command {
	var browser, profile, domain, cookieName string
	cmd := &cobra.Command{
		Use:   "import-session <session-key>",
		Short: "Import a browser session cookie into credentials.json",
		Long: strings.Join([]string{
			"Import a session cookie from a browser profile and store it under",
			"the given session key. The provider reads that key at poll time.",
			"",
			"Examples:",
			"  openusage credentials import-session opencode --profile \"Esa Bandlab\"",
			"  openusage credentials import-session opencode-go --profile \"Esa\"",
		}, "\n"),
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			sessionKey := strings.TrimSpace(args[0])
			if sessionKey == "" {
				return fmt.Errorf("session key is required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			reader := browsercookies.NewWithTimeout(90 * time.Second)
			var (
				cookie browsercookies.Cookie
				err    error
			)
			if strings.TrimSpace(profile) != "" {
				cookie, err = reader.ReadCookieFromProfile(ctx, domain, cookieName, browser, profile)
			} else {
				cookie, err = reader.ReadCookie(ctx, domain, cookieName, browser)
			}
			if err != nil {
				return err
			}

			sess := config.BrowserSession{
				Domain:        domain,
				CookieName:    cookieName,
				Value:         cookie.Value,
				SourceBrowser: cookie.Source,
				CapturedAt:    time.Now().UTC().Format(time.RFC3339),
			}
			if !cookie.Expires.IsZero() {
				sess.ExpiresAt = cookie.Expires.UTC().Format(time.RFC3339)
			}
			if err := config.SaveSession(sessionKey, sess); err != nil {
				return err
			}
			expires := "session cookie"
			if !cookie.Expires.IsZero() {
				expires = cookie.Expires.Format(time.RFC3339)
			}
			fmt.Printf("Saved session %s from %s profile %q (expires %s)\n", sessionKey, cookie.Source, cookie.Profile, expires)
			return nil
		},
	}
	cmd.Flags().StringVar(&browser, "browser", "chrome", "browser to read from (chrome, edge, brave, firefox, safari)")
	cmd.Flags().StringVar(&profile, "profile", "", "browser profile name (e.g. \"Esa\"); empty uses the most recently used profile")
	cmd.Flags().StringVar(&domain, "domain", "opencode.ai", "cookie domain")
	cmd.Flags().StringVar(&cookieName, "cookie", "__Host-console_session", "cookie name")
	return cmd
}
