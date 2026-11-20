package paperless

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/Bam679/paperless-cli/internal/agentcli"
)

func TestNormalizeURL(t *testing.T) {
	ok := map[string]string{
		"https://Paperless.Example.com/":         "https://paperless.example.com",
		"https://paperless.example.com:443":      "https://paperless.example.com",
		"https://example.com:8443/paperless/":    "https://example.com:8443/paperless",
		"https://example.com/paperless/api/":     "https://example.com/paperless",
		"http://localhost:8000":                  "http://localhost:8000",
		"http://127.0.0.1:80/":                   "http://127.0.0.1",
		"http://[::1]:8000":                      "http://[::1]:8000",
		"http://[::1]":                           "http://[::1]",
		"  https://paperless.example.com/x%20y ": "https://paperless.example.com/x%20y",
	}
	for in, want := range ok {
		got, err := NormalizeURL(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "paperless.example.com", "http://paperless.example.com", "ftp://example.com",
		"https://user:pw@example.com", "https://example.com/?a=1", "https://example.com/#x", "http://localhost.evil.com",
		"https://example.com/?", "mailto:x@example.com", "https://%zz"} {
		if _, err := NormalizeURL(in); agentcli.AsError(err).Exit != agentcli.ExitUsage {
			t.Errorf("%q accepted", in)
		}
	}
}

func withEnv(t *testing.T, env map[string]string, os string, kc func(context.Context, string) (string, error)) {
	t.Helper()
	oldEnv, oldOS, oldKC := getenv, goos, keychainLookup
	getenv = func(k string) string { return env[k] }
	goos, keychainLookup = os, kc
	t.Cleanup(func() { getenv, goos, keychainLookup = oldEnv, oldOS, oldKC })
}

func TestLoadConfig(t *testing.T) {
	ctx := context.Background()
	var asked string
	kc := func(_ context.Context, account string) (string, error) {
		asked = account
		if account == "https://paperless.example.com" {
			return "kc-token", nil
		}
		return "", nil
	}
	withEnv(t, map[string]string{EnvURL: "https://Paperless.example.com/", EnvToken: " env-token "}, "darwin", kc)
	cfg, err := LoadConfig(ctx)
	if err != nil || cfg.Token != "env-token" || cfg.TokenSource != "env" || asked != "" {
		t.Fatalf("env: %v %+v", err, cfg)
	}

	withEnv(t, map[string]string{EnvURL: "https://paperless.example.com"}, "darwin", kc)
	cfg, err = LoadConfig(ctx)
	if err != nil || cfg.Token != "kc-token" || cfg.TokenSource != "keychain" || asked != "https://paperless.example.com" {
		t.Fatalf("keychain: %v %+v", err, cfg)
	}

	// A different URL is a different Keychain account and gets no token.
	withEnv(t, map[string]string{EnvURL: "https://evil.example.com"}, "darwin", kc)
	if _, err := LoadConfig(ctx); agentcli.AsError(err).Exit != agentcli.ExitPermission {
		t.Fatalf("other host: %v", err)
	}

	withEnv(t, map[string]string{EnvURL: "https://paperless.example.com"}, "linux", kc)
	if _, err := LoadConfig(ctx); agentcli.AsError(err).Exit != agentcli.ExitPermission {
		t.Fatalf("linux without token: %v", err)
	}

	withEnv(t, map[string]string{EnvURL: "https://paperless.example.com"}, "darwin",
		func(context.Context, string) (string, error) { return "", agentcli.Unavailablef("locked") })
	if _, err := LoadConfig(ctx); agentcli.AsError(err).Exit != agentcli.ExitUnavailable {
		t.Fatalf("keychain error: %v", err)
	}

	withEnv(t, map[string]string{}, "darwin", kc)
	if _, err := LoadConfig(ctx); agentcli.AsError(err).Exit != agentcli.ExitUsage {
		t.Fatalf("no URL: %v", err)
	}
}

// Through the CLI: doctor reports the config problem without a server.
func TestDoctorWithoutConfig(t *testing.T) {
	withEnv(t, map[string]string{EnvURL: "http://paperless.example.com"}, "linux", nil)
	code, out, errOut := run(t, NewHTTP(), readOnly, "doctor")
	if code != agentcli.ExitUsage || !strings.Contains(out, "https") || !strings.Contains(errOut, "PAPERLESS_URL") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
}

func TestSecurityLookupMissingItem(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS Keychain only")
	}
	// A random account that cannot exist: security exits 44, which means "no token".
	tok, err := securityLookup(context.Background(), "https://agent-cli-test-missing.invalid")
	if err != nil || tok != "" {
		t.Fatalf("%q %v", tok, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := securityLookup(ctx, "https://agent-cli-test-missing.invalid"); err == nil {
		t.Fatal("cancelled lookup must fail")
	}
}

func TestLimitedBufferAndRedact(t *testing.T) {
	var b bytes.Buffer
	lb := &limitedBuffer{buf: &b, max: 4}
	for _, s := range []string{"ab", "cdef", "gh"} {
		if n, err := lb.Write([]byte(s)); n != len(s) || err != nil {
			t.Fatal(n, err)
		}
	}
	if b.String() != "abcd" {
		t.Fatal(b.String())
	}
	if redact("a secret b secret", "secret") != "a [redacted] b [redacted]" || redact("x", "") != "x" {
		t.Fatal("redact")
	}
	if !errors.Is(context.Canceled, context.Canceled) {
		t.Fatal()
	}
}
