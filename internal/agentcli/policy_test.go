package agentcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlob(t *testing.T) {
	cases := []struct {
		p, s string
		want bool
	}{
		{"*", "", true},
		{"*", "anything/at all", true},
		{"Agent*", "Agent Sandbox", true},
		{"Agent*", "agent sandbox", false},
		{"*Sandbox", "Agent Sandbox", true},
		{"iCloud/*", "iCloud/Notes", true},
		{"iCloud/*", "Google/Notes", false},
		{"a*b*c", "aXXbYYc", true},
		{"a*b*c", "aXXbYY", false},
		{"exact", "exact", true},
		{"exact", "exactly", false},
		{"", "", true},
		{"", "x", false},
		{"**", "x", true},
		{"tag:private", "tag:private", true},
	}
	for _, c := range cases {
		if got := Glob(c.p, c.s); got != c.want {
			t.Errorf("Glob(%q,%q)=%v want %v", c.p, c.s, got, c.want)
		}
	}
}

// globRef is an obviously-correct exponential reference implementation.
func globRef(p, s []rune) bool {
	if len(p) == 0 {
		return len(s) == 0
	}
	if p[0] == '*' {
		for i := 0; i <= len(s); i++ {
			if globRef(p[1:], s[i:]) {
				return true
			}
		}
		return false
	}
	return len(s) > 0 && p[0] == s[0] && globRef(p[1:], s[1:])
}

func FuzzGlob(f *testing.F) {
	for _, s := range []string{"*", "a*b", "**x*", "Agent*", ""} {
		f.Add(s, "Agent Sandbox")
	}
	f.Fuzz(func(t *testing.T, p, s string) {
		if len(p) > 12 || len(s) > 24 {
			return
		}
		if got, want := Glob(p, s), globRef([]rune(p), []rune(s)); got != want {
			t.Fatalf("Glob(%q,%q)=%v want %v", p, s, got, want)
		}
	})
}

func TestDefaultPolicyIsReadOnly(t *testing.T) {
	e := Effective{Layers: []Policy{DefaultPolicy()}}
	if !e.AllowsOp("list", OpRead) || !e.AllowsScope(OpRead, "Anything") {
		t.Fatal("default must allow reads")
	}
	for _, op := range []string{"add", "delete", "update"} {
		if e.AllowsOp(op, OpWrite) {
			t.Fatalf("default must deny %s", op)
		}
	}
	if e.AllowsScope(OpWrite, "Anything") {
		t.Fatal("default must deny write scopes")
	}
}

func TestEmptyEffectiveDeniesAll(t *testing.T) {
	var e Effective
	if e.AllowsOp("list", OpRead) || e.AllowsScope(OpRead, "x") {
		t.Fatal("no layers must deny")
	}
}

func TestLayersIntersectAndDenyWins(t *testing.T) {
	system := Policy{Operations: OpRule{Allow: []string{OpRead, OpWrite}, Deny: []string{"delete"}},
		Scopes: ScopeRules{Read: Rule{Allow: []string{"*"}, Deny: []string{"Private*"}}, Write: Rule{Allow: []string{"*"}}}}
	userP := Policy{Operations: OpRule{Allow: []string{OpRead, "add", "delete"}},
		Scopes: ScopeRules{Read: Rule{Allow: []string{"*"}}, Write: Rule{Allow: []string{"Agent*"}}},
		Limits: Limits{MaxResults: 20, MaxWriteBatch: 3}}
	e := Effective{Layers: []Policy{system, userP}}
	if !e.AllowsOp("add", OpWrite) {
		t.Error("add allowed by both")
	}
	if e.AllowsOp("delete", OpWrite) {
		t.Error("system deny must win")
	}
	if e.AllowsOp("update", OpWrite) {
		t.Error("update not allowed by user layer")
	}
	if e.AllowsScope(OpRead, "Private Stuff") {
		t.Error("system read deny must win")
	}
	if !e.AllowsScope(OpWrite, "Agent Sandbox") || e.AllowsScope(OpWrite, "Work") {
		t.Error("write scope intersection wrong")
	}
	if e.MaxResults(1000) != 20 || e.MaxWriteBatch(100) != 3 || e.MaxResults(10) != 10 {
		t.Error("limits must take the minimum")
	}
}

func TestParsePolicy(t *testing.T) {
	good := `{"version":1,"operations":{"allow":["read","add"]},"scopes":{"write":{"allow":["Agent*"]}}}`
	if _, err := ParsePolicy([]byte(good), "t"); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{
		"unknown key":  `{"version":1,"operation":{}}`,
		"version":      `{"version":2}`,
		"trailing":     `{"version":1}{"version":1}`,
		"negative":     `{"version":1,"limits":{"max_results":-1}}`,
		"not json":     `version: 1`,
		"wrong type":   `{"version":1,"operations":{"allow":"write"}}`,
		"missing vers": `{}`,
	}
	for name, src := range bad {
		if _, err := ParsePolicy([]byte(src), "t"); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func FuzzParsePolicy(f *testing.F) {
	f.Add([]byte(`{"version":1,"operations":{"allow":["read"]}}`))
	f.Add([]byte(`{"version":1,"scopes":{"read":{"deny":["*"]}}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := ParsePolicy(data, "fuzz")
		if err == nil && p.Version != 1 {
			t.Fatal("accepted wrong version")
		}
	})
}

func writePolicy(t *testing.T, dir, body string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, "tool.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFrom(t *testing.T) {
	dir := t.TempDir()
	uid := os.Getuid()
	missing := filepath.Join(dir, "nope.json")

	e, err := loadFrom(missing, missing, uid)
	if err != nil || len(e.Layers) != 1 || e.AllowsOp("add", OpWrite) {
		t.Fatalf("missing files must give read-only default: %v %v", e, err)
	}

	p := writePolicy(t, dir, `{"version":1,"operations":{"allow":["read","add"]},"scopes":{"read":{"allow":["*"]},"write":{"allow":["A*"]}}}`, 0o600)
	e, err = loadFrom(missing, p, uid)
	if err != nil || !e.AllowsOp("add", OpWrite) || !e.AllowsScope(OpWrite, "Agent") {
		t.Fatalf("user policy not applied: %v", err)
	}

	if err := os.Chmod(p, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFrom(missing, p, uid); err == nil || !strings.Contains(err.Error(), "writable") {
		t.Fatalf("world-writable policy must be rejected: %v", err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFrom(missing, p, uid+1); err == nil {
		t.Fatal("foreign-owned policy must be rejected")
	}
	if _, err := loadFrom(missing, dir, uid); err == nil {
		t.Fatal("directory as policy must be rejected")
	}
	if err := os.WriteFile(p, []byte(`{"version":1,"bogus":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFrom(missing, p, uid); err == nil {
		t.Fatal("invalid policy must fail closed")
	}
	// A system layer (owned by root in production) is checked against uid 0.
	if err := os.WriteFile(p, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFrom(p, missing, uid); uid != 0 && err == nil {
		t.Fatal("system policy not owned by root must be rejected")
	}
}

func TestPolicyPathsIgnoreHOME(t *testing.T) {
	t.Setenv("HOME", "/tmp/evil-home")
	_, u, err := PolicyPaths("apple-x")
	if err != nil {
		t.Skip("no user database:", err)
	}
	if strings.HasPrefix(u, "/tmp/evil-home") {
		t.Fatalf("policy path must not follow $HOME: %s", u)
	}
	if !strings.HasSuffix(u, filepath.Join(".config", "agent-cli", "apple-x.json")) {
		t.Fatalf("unexpected path %s", u)
	}
}

func TestExamplePolicyParses(t *testing.T) {
	if _, err := ParsePolicy([]byte(ExamplePolicy([]string{"add"}, "Agent*")), "ex"); err != nil {
		t.Fatal(err)
	}
}
