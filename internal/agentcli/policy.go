package agentcli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

// Operation classes. Commands declare a concrete op name (e.g. "delete");
// a policy may allow the class ("read"/"write") or the concrete op.
const (
	OpRead  = "read"
	OpWrite = "write"
)

// Policy is one policy layer, loaded from JSON. Unknown keys are rejected.
type Policy struct {
	Version    int        `json:"version"`
	Operations OpRule     `json:"operations"`
	Scopes     ScopeRules `json:"scopes"`
	Limits     Limits     `json:"limits"`
	Source     string     `json:"-"`
}

// OpRule allows or denies operations. Deny always wins.
type OpRule struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

// ScopeRules restricts which containers (lists, calendars, folders, mailboxes,
// tags) may be read or written. Patterns use * as wildcard; deny wins.
type ScopeRules struct {
	Read  Rule `json:"read"`
	Write Rule `json:"write"`
}

// Rule is an allow/deny list of glob patterns.
type Rule struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

// Limits caps result sizes and write batches. Zero means "use default".
type Limits struct {
	MaxResults    int `json:"max_results"`
	MaxWriteBatch int `json:"max_write_batch"`
}

// DefaultPolicy is used when no user policy file exists: read everything,
// write nothing.
func DefaultPolicy() Policy {
	return Policy{
		Version:    1,
		Operations: OpRule{Allow: []string{OpRead}},
		Scopes:     ScopeRules{Read: Rule{Allow: []string{"*"}}},
		Source:     "built-in default (read-only)",
	}
}

// Effective is the intersection of all loaded policy layers.
type Effective struct {
	Layers []Policy
}

// AllowsOp reports whether every layer allows op (of the given class) and
// none denies it.
func (e Effective) AllowsOp(op, class string) bool {
	if len(e.Layers) == 0 {
		return false
	}
	for _, p := range e.Layers {
		if contains(p.Operations.Deny, op) || contains(p.Operations.Deny, class) {
			return false
		}
		if !contains(p.Operations.Allow, op) && !contains(p.Operations.Allow, class) {
			return false
		}
	}
	return true
}

// AllowsScope reports whether scope is allowed for the class by every layer.
func (e Effective) AllowsScope(class, scope string) bool {
	if len(e.Layers) == 0 {
		return false
	}
	for _, p := range e.Layers {
		r := p.Scopes.Read
		if class == OpWrite {
			r = p.Scopes.Write
		}
		if matchAny(r.Deny, scope) || !matchAny(r.Allow, scope) {
			return false
		}
	}
	return true
}

// MaxResults returns the smallest positive max_results across layers, or def.
func (e Effective) MaxResults(def int) int {
	return e.minLimit(def, func(l Limits) int { return l.MaxResults })
}

// MaxWriteBatch returns the smallest positive max_write_batch, or def.
func (e Effective) MaxWriteBatch(def int) int {
	return e.minLimit(def, func(l Limits) int { return l.MaxWriteBatch })
}

func (e Effective) minLimit(def int, f func(Limits) int) int {
	v := def
	for _, p := range e.Layers {
		if n := f(p.Limits); n > 0 && n < v {
			v = n
		}
	}
	return v
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func matchAny(patterns []string, s string) bool {
	for _, p := range patterns {
		if Glob(p, s) {
			return true
		}
	}
	return false
}

// Glob matches s against pattern where '*' matches any run of characters
// (including '/') and every other rune matches itself, case-sensitively.
func Glob(pattern, s string) bool {
	p, t := []rune(pattern), []rune(s)
	pi, ti, star, mark := 0, 0, -1, 0
	for ti < len(t) {
		switch {
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, ti
			pi++
		case pi < len(p) && p[pi] == t[ti]:
			pi++
			ti++
		case star >= 0:
			pi = star + 1
			mark++
			ti = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// ParsePolicy decodes and validates one policy layer.
func ParsePolicy(data []byte, source string) (Policy, error) {
	var p Policy
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Policy{}, fmt.Errorf("%s: %w", source, err)
	}
	if dec.More() {
		return Policy{}, fmt.Errorf("%s: trailing data after policy object", source)
	}
	if p.Version != 1 {
		return Policy{}, fmt.Errorf("%s: unsupported policy version %d (want 1)", source, p.Version)
	}
	if p.Limits.MaxResults < 0 || p.Limits.MaxWriteBatch < 0 {
		return Policy{}, fmt.Errorf("%s: limits must not be negative", source)
	}
	p.Source = source
	return p, nil
}

// PolicyPaths returns the fixed system and user policy paths for tool.
// They are intentionally not configurable via flags or environment variables,
// so an agent cannot point the binary at a more permissive policy.
func PolicyPaths(tool string) (system, userPath string, err error) {
	system = filepath.Join("/etc/agent-cli", tool+".json")
	if runtime.GOOS == "darwin" {
		system = filepath.Join("/Library/Application Support/agent-cli", tool+".json")
	}
	u, err := user.Current() // ignores $HOME on purpose
	if err != nil {
		return system, "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return system, filepath.Join(u.HomeDir, ".config", "agent-cli", tool+".json"), nil
}

// fileOwner extracts the owner uid of a file; overridable in tests.
var fileOwner = ownerUID

// LoadPolicy loads the effective policy from the fixed paths. A missing user
// file yields the read-only default. Any invalid or insecure file is an error:
// callers must fail closed.
func LoadPolicy(tool string) (Effective, error) {
	system, userPath, err := PolicyPaths(tool)
	if err != nil {
		return Effective{}, err
	}
	return loadFrom(system, userPath, os.Getuid())
}

func loadFrom(system, userPath string, uid int) (Effective, error) {
	var eff Effective
	if p, ok, err := readLayer(system, 0); err != nil {
		return Effective{}, err
	} else if ok {
		eff.Layers = append(eff.Layers, p)
	}
	if userPath == "" {
		eff.Layers = append(eff.Layers, DefaultPolicy())
		return eff, nil
	}
	p, ok, err := readLayer(userPath, uid)
	if err != nil {
		return Effective{}, err
	}
	if !ok {
		p = DefaultPolicy()
	}
	eff.Layers = append(eff.Layers, p)
	return eff, nil
}

// readLayer reads a policy file that must be owned by wantUID and must not be
// writable by group or others.
func readLayer(path string, wantUID int) (Policy, bool, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Policy{}, false, nil
	}
	if err != nil {
		return Policy{}, false, fmt.Errorf("policy %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return Policy{}, false, fmt.Errorf("policy %s: not a regular file", path)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return Policy{}, false, fmt.Errorf("policy %s: must not be group/world writable (chmod 600)", path)
	}
	if owner, ok := fileOwner(fi); ok && owner != wantUID {
		return Policy{}, false, fmt.Errorf("policy %s: owned by uid %d, want %d", path, owner, wantUID)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- fixed, validated policy path
	if err != nil {
		return Policy{}, false, fmt.Errorf("policy %s: %w", path, err)
	}
	if len(data) > 1<<20 {
		return Policy{}, false, fmt.Errorf("policy %s: larger than 1 MiB", path)
	}
	p, err := ParsePolicy(data, path)
	if err != nil {
		return Policy{}, false, err
	}
	return p, true, nil
}

// ExamplePolicy returns a commented-free example policy for docs and `policy --example`.
func ExamplePolicy(writeOps []string, scopeExample string) string {
	ops := append([]string{OpRead}, writeOps...)
	b, _ := json.MarshalIndent(map[string]any{
		"version":    1,
		"operations": map[string]any{"allow": ops, "deny": []string{}},
		"scopes": map[string]any{
			"read":  map[string]any{"allow": []string{"*"}, "deny": []string{}},
			"write": map[string]any{"allow": []string{scopeExample}, "deny": []string{}},
		},
		"limits": map[string]any{"max_results": 200, "max_write_batch": 10},
	}, "", "  ")
	return strings.TrimSpace(string(b))
}
