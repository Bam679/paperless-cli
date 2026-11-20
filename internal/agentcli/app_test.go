package agentcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatalf("missing golden %s (run go test -update): %v", p, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden %s mismatch\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

type fixture struct {
	app     *App
	applied int
	pol     Effective
	polErr  error
}

func newFixture() *fixture {
	f := &fixture{pol: Effective{Layers: []Policy{DefaultPolicy()}}}
	items := []Item{
		{"id": "1", "title": "Milk", "list": "Shopping", "done": false},
		{"id": "2", "title": "Tab\tand\nnewline", "list": "Shopping", "done": true},
		{"id": "3", "title": "Secret", "list": "Private", "done": false},
		{"id": "4", "title": "Sandbox item", "list": "Agent Sandbox", "done": false},
	}
	f.app = &App{
		Name: "fake", Version: "0.0.0-test", Summary: "fake tool",
		ScopeHelp: "list name", ScopeExample: "Agent*",
		LoadPolicy: func() (Effective, error) { return f.pol, f.polErr },
		Now:        func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
		Commands: []*Command{
			{Name: "items", Summary: "list items", Kind: Read,
				Flags:  []Flag{{Name: "list", Type: String, Usage: "filter by list"}},
				Fields: []string{"id", "title", "list"}, AllFields: []string{"id", "title", "list", "done"},
				Run: func(c *Ctx) error {
					var out []Item
					for _, it := range items {
						l := it["list"].(string)
						if !c.CanRead(l) || (c.IsSet("list") && c.String("list") != l) {
							continue
						}
						out = append(out, it)
					}
					return c.List(out, c.String("list"))
				}},
			{Name: "get", Summary: "get item", Kind: Read, Args: []Arg{{Name: "id", Required: true}},
				Fields: []string{"id", "title"}, AllFields: []string{"id", "title", "list", "done", "body"},
				Run: func(c *Ctx) error {
					for _, it := range items {
						if it["id"] == c.Args[0] {
							if err := c.CheckRead(it["list"].(string)); err != nil {
								return err
							}
							cp := Item{"body": strings.Repeat("x", 400)}
							for k, v := range it {
								cp[k] = v
							}
							return c.Show(cp)
						}
					}
					return NotFoundf("no item %s", c.Args[0])
				}},
			{Name: "add", Summary: "add item", Kind: Write, Op: "add",
				Flags:  []Flag{{Name: "list", Type: String, Required: true, Usage: "target list"}, {Name: "title", Type: String, Usage: "title"}},
				Fields: []string{"id", "title", "list"}, Stdin: "NDJSON objects {title}",
				Plan: func(c *Ctx) (*WritePlan, error) {
					titles := []string{c.String("title")}
					if len(c.Args) > 0 && c.Args[0] == "-" {
						in, err := DecodeInputs[struct {
							Title string `json:"title"`
						}](c.Stdin)
						if err != nil {
							return nil, err
						}
						titles = nil
						for _, x := range in {
							titles = append(titles, x.Title)
						}
					}
					var targets []Item
					for _, ti := range titles {
						targets = append(targets, Item{"title": ti, "list": c.String("list")})
					}
					return &WritePlan{Scopes: []string{c.String("list")}, Targets: targets,
						Apply: func(context.Context) ([]Item, error) {
							f.applied++
							return []Item{{"id": "new", "title": titles[0], "list": c.String("list")}}, nil
						}}, nil
				}, Args: []Arg{{Name: "input", Usage: "- for stdin"}}},
		},
	}
	return f
}

func (f *fixture) run(stdin string, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := f.app.Main(context.Background(), args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func writable(lists ...string) Effective {
	return Effective{Layers: []Policy{{Version: 1,
		Operations: OpRule{Allow: []string{OpRead, "add"}},
		Scopes:     ScopeRules{Read: Rule{Allow: []string{"*"}}, Write: Rule{Allow: lists}}}}}
}

func TestOutputFormatsGolden(t *testing.T) {
	f := newFixture()
	for name, args := range map[string][]string{
		"items.txt":          {"items"},
		"items.json":         {"items", "--json"},
		"items.ndjson":       {"items", "--ndjson", "--limit", "2"},
		"items_fields.txt":   {"--fields", "title,done", "items"},
		"items_page.txt":     {"items", "--limit", "1"},
		"get.txt":            {"get", "1", "--max-chars", "120"},
		"get.json":           {"get", "1", "--json", "--fields", "id,body", "--max-chars", "100"},
		"items_maxchars.txt": {"items", "--max-chars", "20"},
	} {
		code, out, errOut := f.run("", args...)
		if code != 0 {
			t.Fatalf("%s: exit %d: %s", name, code, errOut)
		}
		golden(t, name, []byte(out))
	}
}

func TestSchemaAndHelpGolden(t *testing.T) {
	f := newFixture()
	_, out, _ := f.run("", "schema")
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	pol := v["policy"].(map[string]any)
	delete(pol, "system_path") // OS-specific
	b, _ := json.MarshalIndent(v, "", "  ")
	golden(t, "schema.json", b)
	_, out, _ = f.run("", "--help")
	golden(t, "help.txt", []byte(out))
	_, out, _ = f.run("", "add", "--help")
	golden(t, "help_add.txt", []byte(out))
}

func TestCursorRoundTrip(t *testing.T) {
	f := newFixture()
	_, out, _ := f.run("", "items", "--json", "--limit", "2")
	var env listEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil || env.NextCursor == "" || env.Count != 2 {
		t.Fatalf("bad first page: %s", out)
	}
	_, out, _ = f.run("", "items", "--json", "--limit", "2", "--cursor", env.NextCursor)
	var env2 listEnvelope
	_ = json.Unmarshal([]byte(out), &env2)
	if env2.Count != 2 || env2.NextCursor != "" || env2.Items[0]["id"] != "3" {
		t.Fatalf("bad second page: %s", out)
	}
	code, _, errOut := f.run("", "items", "--json", "--limit", "2", "--list", "Shopping", "--cursor", env.NextCursor)
	if code != ExitUsage || !strings.Contains(errOut, "different query") {
		t.Fatalf("cursor reuse across queries must fail: %d %s", code, errOut)
	}
}

func TestReadScopeFiltering(t *testing.T) {
	f := newFixture()
	f.pol = Effective{Layers: []Policy{{Version: 1, Operations: OpRule{Allow: []string{OpRead}},
		Scopes: ScopeRules{Read: Rule{Allow: []string{"*"}, Deny: []string{"Private"}}}}}}
	_, out, _ := f.run("", "items")
	if strings.Contains(out, "Secret") {
		t.Fatal("denied scope leaked into list")
	}
	code, _, _ := f.run("", "get", "3")
	if code != ExitPolicy {
		t.Fatalf("get in denied scope: exit %d", code)
	}
}

func TestWriteGate(t *testing.T) {
	type tc struct {
		name    string
		pol     Effective
		args    []string
		want    int
		applied int
	}
	cases := []tc{
		{"default policy denies", Effective{Layers: []Policy{DefaultPolicy()}}, []string{"add", "--list", "Agent Sandbox", "--title", "x", "--confirm"}, ExitPolicy, 0},
		{"default policy denies dry-run too", Effective{Layers: []Policy{DefaultPolicy()}}, []string{"add", "--list", "Agent Sandbox", "--title", "x", "--dry-run"}, ExitPolicy, 0},
		{"scope denied", writable("Agent*"), []string{"add", "--list", "Shopping", "--title", "x", "--confirm"}, ExitPolicy, 0},
		{"needs confirm", writable("Agent*"), []string{"add", "--list", "Agent Sandbox", "--title", "x"}, ExitConfirm, 0},
		{"dry run", writable("Agent*"), []string{"add", "--list", "Agent Sandbox", "--title", "x", "--dry-run"}, ExitOK, 0},
		{"confirmed", writable("Agent*"), []string{"add", "--list", "Agent Sandbox", "--title", "x", "--confirm"}, ExitOK, 1},
		{"bypass flag rejected", Effective{Layers: []Policy{DefaultPolicy()}}, []string{"add", "--list", "A", "--confirm", "--policy", "/tmp/p.json"}, ExitUsage, 0},
		{"no layers deny", Effective{}, []string{"items"}, ExitPolicy, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture()
			f.pol = c.pol
			code, _, errOut := f.run("", c.args...)
			if code != c.want || f.applied != c.applied {
				t.Fatalf("exit %d (want %d), applied %d (want %d): %s", code, c.want, f.applied, c.applied, errOut)
			}
			if code != 0 {
				var e map[string]Error
				if err := json.Unmarshal([]byte(errOut), &e); err != nil || strings.Count(errOut, "\n") != 1 || e["error"].Exit != code {
					t.Fatalf("stderr must be one JSON line with exit code: %q", errOut)
				}
			}
		})
	}
}

func TestBatchLimitAndStdin(t *testing.T) {
	f := newFixture()
	f.pol = writable("Agent*")
	f.pol.Layers[0].Limits.MaxWriteBatch = 2
	code, out, _ := f.run(`{"title":"a"}`+"\n"+`{"title":"b"}`, "add", "-", "--list", "Agent X", "--dry-run", "--ndjson")
	if code != 0 || strings.Count(out, "\n") != 2 {
		t.Fatalf("dry-run of 2: %d %q", code, out)
	}
	code, _, _ = f.run(`[{"title":"a"},{"title":"b"},{"title":"c"}]`, "add", "-", "--list", "Agent X", "--confirm")
	if code != ExitPolicy || f.applied != 0 {
		t.Fatalf("batch over limit must be denied: %d", code)
	}
	code, _, _ = f.run(`{"title":"a","evil":1}`, "add", "-", "--list", "Agent X", "--confirm")
	if code != ExitUsage {
		t.Fatalf("unknown input field must be rejected: %d", code)
	}
}

func TestPolicyErrorFailsClosed(t *testing.T) {
	f := newFixture()
	f.polErr = errors.New("broken")
	if code, _, _ := f.run("", "items"); code != ExitPolicy {
		t.Fatalf("read with broken policy: %d", code)
	}
	if code, out, _ := f.run("", "policy"); code != ExitPolicy || !strings.Contains(out, "fail closed") {
		t.Fatalf("policy command must report: %d %s", code, out)
	}
	if code, _, _ := f.run("", "schema"); code != 0 {
		t.Fatal("schema must work without policy")
	}
}

func TestUsageErrors(t *testing.T) {
	f := newFixture()
	for _, args := range [][]string{
		{}, {"nope"}, {"items", "--bogus"}, {"items", "-x"}, {"get"}, {"get", "1", "2"},
		{"items", "--json", "--ndjson"}, {"items", "--fields", "nope"}, {"items", "--limit", "x"},
		{"items", "--dry-run"}, {"items", "--timeout", "0"}, {"add", "--title", "x"},
		{"items", "--limit"}, {"items", "--json=maybe"}, {"items", "--list", "a", "--list", "b"},
		{"items", "--max-chars", "-1"}, {"items", "--cursor", "garbage"},
	} {
		if code, _, _ := f.run("", args...); code != ExitUsage {
			t.Errorf("%v: exit %d, want %d", args, code, ExitUsage)
		}
	}
	if code, _, _ := f.run("", "get", "99"); code != ExitNotFound {
		t.Errorf("not found exit %d", code)
	}
}

func TestBuiltins(t *testing.T) {
	f := newFixture()
	if _, out, _ := f.run("", "version"); out != "fake 0.0.0-test\n" {
		t.Fatalf("version: %q", out)
	}
	if _, out, _ := f.run("", "--version"); out != "fake 0.0.0-test\n" {
		t.Fatalf("--version: %q", out)
	}
	if code, out, _ := f.run("", "help", "items"); code != 0 || !strings.Contains(out, "Usage: fake items") {
		t.Fatalf("help items: %s", out)
	}
	if code, out, _ := f.run("", "policy", "--example"); code != 0 || !strings.Contains(out, `"add"`) {
		t.Fatalf("policy --example: %s", out)
	}
	if code, out, _ := f.run("", "policy", "--json"); code != 0 || !strings.Contains(out, "built-in default") {
		t.Fatalf("policy: %s", out)
	}
}

func TestDeadlineMapsToUnavailable(t *testing.T) {
	f := newFixture()
	f.app.Commands[0].Run = func(c *Ctx) error { <-c.Done(); return c.Err() }
	if code, _, _ := f.run("", "items", "--timeout", "1ms"); code != ExitUnavailable {
		t.Fatalf("exit %d", code)
	}
}

func FuzzMain(f *testing.F) {
	f.Add("add --list Agent --confirm")
	f.Add("items --fields id --json --limit 1")
	f.Add("--limit 3 items --cursor c1.eyJvIjoxfQ")
	f.Fuzz(func(t *testing.T, line string) {
		fx := newFixture()
		fx.pol = Effective{Layers: []Policy{DefaultPolicy()}}
		code, _, errOut := fx.run("", strings.Fields(line)...)
		if fx.applied != 0 {
			t.Fatalf("write applied under read-only policy: %q", line)
		}
		if code != 0 && !json.Valid([]byte(errOut)) {
			t.Fatalf("stderr not JSON for %q: %q", line, errOut)
		}
	})
}

func TestDecodeInputsLimits(t *testing.T) {
	type in struct {
		A int `json:"a"`
	}
	if _, err := DecodeInputs[in](strings.NewReader("")); err == nil {
		t.Fatal("empty input must fail")
	}
	big := strings.Repeat(" ", 4<<20+2)
	if _, err := DecodeInputs[in](strings.NewReader(big)); err == nil {
		t.Fatal("oversized input must fail")
	}
	got, err := DecodeInputs[in](strings.NewReader(`{"a":1} {"a":2}`))
	if err != nil || len(got) != 2 {
		t.Fatalf("concatenated objects: %v %v", got, err)
	}
	if _, err := DecodeInputs[in](strings.NewReader(`[{"a":"x"}]`)); err == nil {
		t.Fatal("wrong type must fail")
	}
}

func TestDefaultOnOperation(t *testing.T) {
	f := newFixture()
	f.app.DefaultOn = []string{"add"}
	// Default policy: allowed without any policy file.
	f.pol = Effective{Layers: []Policy{DefaultPolicy()}}
	if code, _, e := f.run("", "add", "--list", "Shopping", "--title", "x", "--confirm"); code != 0 || f.applied != 1 {
		t.Fatalf("default-on op must be allowed by the default policy: %d %s", code, e)
	}
	// A user policy that does not mention it still allows it.
	f.pol = Effective{Layers: []Policy{{Version: 1, Operations: OpRule{Allow: []string{OpRead}}}}}
	if code, _, _ := f.run("", "add", "--list", "Shopping", "--title", "x", "--confirm"); code != 0 {
		t.Fatal("default-on op must survive a policy that does not mention it")
	}
	for name, pol := range map[string]Policy{
		"op deny":    {Version: 1, Operations: OpRule{Allow: []string{OpRead}, Deny: []string{"add"}}},
		"class deny": {Version: 1, Operations: OpRule{Allow: []string{OpRead}, Deny: []string{OpWrite}}},
		"scope deny": {Version: 1, Operations: OpRule{Allow: []string{OpRead}}, Scopes: ScopeRules{Write: Rule{Deny: []string{"Shop*"}}}},
	} {
		f.pol = Effective{Layers: []Policy{pol}}
		before := f.applied
		if code, _, _ := f.run("", "add", "--list", "Shopping", "--title", "x", "--confirm"); code != ExitPolicy || f.applied != before {
			t.Fatalf("%s must block the default-on op: %d", name, code)
		}
	}
	f.pol = Effective{}
	if code, _, _ := f.run("", "add", "--list", "Shopping", "--title", "x", "--confirm"); code != ExitPolicy {
		t.Fatal("no policy layers must deny everything")
	}
	if _, out, _ := f.run("", "--help"); !strings.Contains(out, "on unless the policy denies it") {
		t.Fatal("help must mark default-on writes")
	}
	if code, out, _ := f.run("", "policy"); code != 0 || !strings.Contains(out, "default_on") {
		t.Fatalf("policy must show default-on ops: %s", out)
	}
}
