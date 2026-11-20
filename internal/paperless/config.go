package paperless

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/Bam679/paperless-cli/internal/agentcli"
)

// Environment variables and the Keychain service name. There is deliberately
// no flag or argument for the token.
const (
	EnvURL          = "PAPERLESS_URL"
	EnvToken        = "PAPERLESS_TOKEN"
	KeychainService = "paperless-cli"
	securityBin     = "/usr/bin/security"
)

// Config is the resolved connection configuration.
type Config struct {
	BaseURL     string // normalized, no trailing slash
	Token       string
	TokenSource string // "env" or "keychain"
}

// Replaceable in tests only; the shipped binary uses the real environment,
// OS and Keychain.
var (
	getenv         = os.Getenv
	goos           = runtime.GOOS
	keychainLookup = securityLookup
)

func configErr(msg string) error {
	return &agentcli.Error{Code: "usage", Exit: agentcli.ExitUsage, Message: msg,
		Hint: "set PAPERLESS_URL to your server, e.g. https://paperless.example.com"}
}

func tokenErr(msg string) error {
	return &agentcli.Error{Code: "permission_denied", Exit: agentcli.ExitPermission, Message: msg,
		Hint: "set PAPERLESS_TOKEN, or on macOS run: security add-generic-password -s paperless-cli -a <PAPERLESS_URL> -w"}
}

// NormalizeURL validates a server base URL and returns its canonical form,
// which is also the Keychain account name. https is required except for
// loopback hosts.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", configErr("PAPERLESS_URL is not set")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return "", configErr("PAPERLESS_URL must be an absolute URL like https://paperless.example.com")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", configErr("PAPERLESS_URL must not contain credentials, a query or a fragment")
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	switch {
	case scheme == "https":
	case scheme == "http" && isLoopback(host):
	default:
		return "", configErr("PAPERLESS_URL must use https (plain http is only allowed for localhost)")
	}
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	hostport := host
	if strings.Contains(host, ":") {
		hostport = "[" + host + "]"
	}
	if port != "" {
		hostport = net.JoinHostPort(host, port)
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	path = strings.TrimSuffix(path, "/api")
	return scheme + "://" + hostport + path, nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// LoadConfig resolves the base URL from PAPERLESS_URL and the token from
// PAPERLESS_TOKEN, then from the macOS Keychain item (service paperless-cli,
// account = normalized base URL). Keying by URL means a changed URL can never
// pull the token for another server.
func LoadConfig(ctx context.Context) (Config, error) {
	base, err := NormalizeURL(getenv(EnvURL))
	if err != nil {
		return Config{}, err
	}
	if t := strings.TrimSpace(getenv(EnvToken)); t != "" {
		return Config{BaseURL: base, Token: t, TokenSource: "env"}, nil
	}
	if goos != "darwin" {
		return Config{}, tokenErr("no API token: PAPERLESS_TOKEN is not set")
	}
	t, err := keychainLookup(ctx, base)
	if err != nil {
		return Config{}, err
	}
	if t == "" {
		return Config{}, tokenErr("no API token: PAPERLESS_TOKEN is not set and the Keychain has no paperless-cli item for " + base)
	}
	return Config{BaseURL: base, Token: t, TokenSource: "keychain"}, nil
}

// errItemNotFound is security(1)'s exit status for a missing item.
const errItemNotFound = 44

// securityLookup reads the token with fixed argv (no shell). Its output is
// never echoed; stderr is dropped because it can't contain anything useful
// for an agent.
func securityLookup(ctx context.Context, account string) (string, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, securityBin, "find-generic-password", "-s", KeychainService, "-a", account, "-w") // #nosec G204 -- fixed binary, argv only
	cmd.Stdout = &limitedBuffer{buf: &out, max: 64 << 10}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == errItemNotFound {
			return "", nil
		}
		if ctx.Err() != nil {
			return "", agentcli.Unavailablef("Keychain lookup timed out")
		}
		return "", tokenErr("Keychain lookup failed (is the Keychain locked, or was access denied?)")
	}
	return strings.TrimRight(out.String(), "\r\n"), nil
}

// limitedBuffer stops accepting data after max bytes.
type limitedBuffer struct {
	buf *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.buf.Len(); room > 0 {
		if len(p) > room {
			l.buf.Write(p[:room])
		} else {
			l.buf.Write(p)
		}
	}
	return len(p), nil
}

// redact removes every occurrence of secret from s.
func redact(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "[redacted]")
}
