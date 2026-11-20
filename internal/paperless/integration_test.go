//go:build integration

package paperless

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Bam679/paperless-cli/internal/agentcli"
)

// The integration test starts a throwaway Paperless-ngx (testdata/
// docker-compose.yml) as compose project agent-cli-test-paperless-<random>
// on a random 127.0.0.1 port, gets a token via POST /api/token/ with the
// test-only admin credentials, runs the CLI against it and always removes
// the project with `down -v`. All documents are synthetic.

func randHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// composeCmd prefers the `docker compose` plugin and falls back to the
// standalone docker-compose binary.
func composeCmd(t *testing.T) []string {
	t.Helper()
	if docker, err := exec.LookPath("docker"); err == nil {
		if exec.Command(docker, "compose", "version").Run() == nil {
			return []string{docker, "compose"}
		}
	}
	if dc, err := exec.LookPath("docker-compose"); err == nil {
		return []string{dc}
	}
	t.Skip("docker compose not available")
	return nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func startPaperless(t *testing.T) (base, token string) {
	t.Helper()
	compose := composeCmd(t)
	file, err := filepath.Abs(filepath.Join("..", "..", "testdata", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	project := "agent-cli-test-paperless-" + randHex(t)
	user := envOr("PAPERLESS_ADMIN_USER", "agent-cli-test-admin")
	pass := envOr("PAPERLESS_ADMIN_PASSWORD", "agent-cli-test-only")
	dc := func(timeout time.Duration, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		argv := append(append(append([]string{}, compose[1:]...), "-f", file, "-p", project), args...)
		cmd := exec.CommandContext(ctx, compose[0], argv...)
		cmd.Env = append(os.Environ(), "PAPERLESS_ADMIN_USER="+user, "PAPERLESS_ADMIN_PASSWORD="+pass)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		return strings.TrimSpace(out.String()), err
	}
	t.Cleanup(func() {
		if out, err := dc(3*time.Minute, "down", "-v", "--remove-orphans"); err != nil {
			t.Errorf("compose down: %v\n%s", err, out)
		}
	})
	if out, err := dc(8*time.Minute, "up", "-d"); err != nil {
		t.Fatalf("compose up: %v\n%s", err, out)
	}
	hostport, err := dc(time.Minute, "port", "webserver", "8000")
	if err != nil || !strings.HasPrefix(hostport, "127.0.0.1:") {
		t.Fatalf("compose port: %v %q", err, hostport)
	}
	base = "http://" + hostport
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	deadline := time.Now().Add(6 * time.Minute)
	for time.Now().Before(deadline) {
		resp, err := http.Post(base+"/api/token/", "application/json", bytes.NewReader(body)) // #nosec G107 -- local test server
		if err == nil {
			var tok struct{ Token string }
			_ = json.NewDecoder(resp.Body).Decode(&tok)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && tok.Token != "" {
				return base, tok.Token
			}
		}
		time.Sleep(3 * time.Second)
	}
	logs, _ := dc(time.Minute, "logs", "--tail", "50", "webserver")
	t.Fatalf("paperless did not become ready:\n%s", logs)
	return "", ""
}

func TestIntegrationPaperless(t *testing.T) {
	base, token := startPaperless(t)
	t.Setenv(EnvURL, base)
	t.Setenv(EnvToken, token)

	run := randHex(t)
	sandbox, extra := "agent-cli-test-sandbox", "agent-cli-test-extra"
	// Main policy: read everything except tag:secret, write only agent-cli-test-* tags.
	pol := agentcli.Effective{Layers: []agentcli.Policy{{Version: 1,
		Operations: agentcli.OpRule{Allow: []string{agentcli.OpRead, agentcli.OpWrite}},
		Scopes: agentcli.ScopeRules{Read: agentcli.Rule{Allow: []string{"*"}, Deny: []string{"tag:secret"}},
			Write: agentcli.Rule{Allow: []string{"tag:agent-cli-test-*"}}}}}}
	// Setup policy for fixtures the main policy must not be able to create.
	setup := agentcli.Effective{Layers: []agentcli.Policy{{Version: 1,
		Operations: agentcli.OpRule{Allow: []string{agentcli.OpRead, agentcli.OpWrite}},
		Scopes:     agentcli.ScopeRules{Read: agentcli.Rule{Allow: []string{"*"}}, Write: agentcli.Rule{Allow: []string{"*"}}}}}}
	cli := func(p agentcli.Effective, args ...string) (int, string) {
		t.Helper()
		app := NewApp(NewHTTP(), "it") // real client, configured from the environment
		app.LoadPolicy = func() (agentcli.Effective, error) { return p, nil }
		var out, errb bytes.Buffer
		code := app.Main(context.Background(), append(args, "--timeout", "180s"), strings.NewReader(""), &out, &errb)
		if code != 0 {
			t.Logf("%v -> %d %s", args, code, errb.String())
		}
		if strings.Contains(out.String()+errb.String(), token) {
			t.Fatalf("%v leaked the token", args)
		}
		return code, out.String()
	}
	mustJSON := func(out string, v any) {
		t.Helper()
		if err := json.Unmarshal([]byte(out), v); err != nil {
			t.Fatalf("bad JSON %q: %v", out, err)
		}
	}

	if code, out := cli(pol, "doctor"); code != 0 || !strings.Contains(out, "token from env") {
		t.Fatalf("doctor: %s", out)
	}
	for _, name := range []string{sandbox, extra} {
		if code, _ := cli(pol, "tag-create", name, "--confirm"); code != 0 {
			t.Fatalf("tag-create %s", name)
		}
	}
	if code, _ := cli(pol, "tag-create", "secret", "--confirm"); code != agentcli.ExitPolicy {
		t.Fatal("tag-create outside the write scope must be denied")
	}
	if code, _ := cli(setup, "tag-create", "secret", "--confirm"); code != 0 {
		t.Fatal("setup tag-create secret")
	}

	dir := t.TempDir()
	upload := func(p agentcli.Effective, name, text string, tags ...string) int {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		args := []string{"upload", path, "--title", strings.TrimSuffix(name, ".txt"), "--wait", "--confirm", "--json"}
		for _, tg := range tags {
			args = append(args, "--tag", tg)
		}
		code, out := cli(p, args...)
		if code != 0 {
			t.Fatalf("upload %s failed", name)
		}
		var res struct{ Items []struct{ ID int } }
		mustJSON(out, &res)
		if len(res.Items) != 1 || res.Items[0].ID == 0 {
			t.Fatalf("upload %s: %s", name, out)
		}
		return res.Items[0].ID
	}
	word := "zebraquartz" + run
	a := upload(pol, "it-sandbox-"+run+".txt", "Synthetic sandbox note "+word+"\nTotal 42.00 EUR\n", sandbox)
	b := upload(setup, "it-secret-"+run+".txt", "Synthetic secret note hiddenword"+run+"\n", "secret")
	c := upload(setup, "it-untagged-"+run+".txt", "Synthetic untagged note keepme"+run+"\n")
	ids := func(out string) map[int]bool {
		var res struct{ Items []struct{ ID int } }
		mustJSON(out, &res)
		m := map[int]bool{}
		for _, it := range res.Items {
			m[it.ID] = true
		}
		return m
	}

	// list
	if code, out := cli(pol, "documents", "--tag", sandbox, "--json"); code != 0 || !ids(out)[a] || len(ids(out)) != 1 {
		t.Fatalf("documents --tag: %s", out)
	}
	if code, out := cli(pol, "documents", "--json", "--limit", "1"); code != 0 || !strings.Contains(out, "next_cursor") {
		t.Fatalf("documents --limit 1: %s", out)
	}
	code, out := cli(pol, "documents", "--json")
	if got := ids(out); code != 0 || !got[a] || !got[c] || got[b] {
		t.Fatalf("documents (read scope): %s", out)
	}
	if code, out := cli(pol, "documents", "--query", "keepme"+run, "--json"); code != 0 || !ids(out)[c] {
		t.Fatalf("documents --query: %s", out)
	}
	if code, _ := cli(pol, "documents", "--tag", "secret"); code != agentcli.ExitPolicy {
		t.Fatal("documents --tag secret must be denied")
	}
	if code, out := cli(pol, "tags"); code != 0 || strings.Contains(out, "secret") || !strings.Contains(out, sandbox) {
		t.Fatalf("tags: %s", out)
	}

	// search (the index may lag consumption slightly)
	found := false
	for i := 0; i < 30 && !found; i++ {
		_, out := cli(pol, "search", word, "--json")
		found = ids(out)[a]
		if !found {
			time.Sleep(time.Second)
		}
	}
	if !found {
		t.Fatal("search did not find the sandbox document")
	}
	if code, out := cli(pol, "search", "hiddenword"+run, "--json"); code != 0 || ids(out)[b] {
		t.Fatalf("search leaked the secret document: %s", out)
	}

	// get
	if code, out := cli(pol, "get", itoa(a), "--content", "--json"); code != 0 || !strings.Contains(out, word) {
		t.Fatalf("get --content: %s", out)
	}
	if code, out := cli(pol, "get", itoa(a), "--json"); code != 0 || strings.Contains(out, word) {
		t.Fatalf("get without --content: %s", out)
	}
	if code, _ := cli(pol, "get", itoa(b)); code != agentcli.ExitPolicy {
		t.Fatal("get secret must be denied")
	}

	// download
	dst := filepath.Join(dir, "out.txt")
	if code, _ := cli(pol, "download", itoa(a), "--out", dst, "--original"); code != 0 {
		t.Fatal("download")
	}
	if data, err := os.ReadFile(dst); err != nil || !strings.Contains(string(data), word) { // #nosec G304 -- test file
		t.Fatalf("downloaded file: %v %q", err, data)
	}
	if st, _ := os.Stat(dst); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if code, _ := cli(pol, "download", itoa(a), "--out", dst); code != agentcli.ExitConflict {
		t.Fatal("download must not overwrite")
	}
	if code, _ := cli(pol, "download", itoa(b), "--out", filepath.Join(dir, "secret.txt")); code != agentcli.ExitPolicy {
		t.Fatal("download secret must be denied")
	}

	// update tags
	if code, _ := cli(pol, "update", itoa(a), "--add-tag", extra, "--title", "it-renamed-"+run, "--confirm"); code != 0 {
		t.Fatal("update add-tag")
	}
	var doc struct {
		Title string
		Tags  []string
	}
	_, out = cli(pol, "get", itoa(a), "--json")
	mustJSON(out, &doc)
	if doc.Title != "it-renamed-"+run || len(doc.Tags) != 2 {
		t.Fatalf("after update: %+v", doc)
	}
	if code, _ := cli(pol, "update", itoa(a), "--remove-tag", extra, "--confirm"); code != 0 {
		t.Fatal("update remove-tag")
	}
	_, out = cli(pol, "get", itoa(a), "--json")
	mustJSON(out, &doc)
	if len(doc.Tags) != 1 || doc.Tags[0] != sandbox {
		t.Fatalf("after remove: %+v", doc)
	}
	if code, _ := cli(pol, "update", itoa(a), "--add-tag", "secret", "--confirm"); code != agentcli.ExitPolicy {
		t.Fatal("adding a non-writable tag must be denied")
	}

	// policy-denied delete leaves the document intact
	if code, _ := cli(pol, "delete", itoa(c), "--confirm"); code != agentcli.ExitPolicy {
		t.Fatal("delete of an untagged document must be denied")
	}
	if code, _ := cli(pol, "delete", itoa(a), itoa(c), "--confirm"); code != agentcli.ExitPolicy {
		t.Fatal("mixed delete batch must be denied")
	}
	if code, out := cli(pol, "get", itoa(c), "--json"); code != 0 || !strings.Contains(out, "it-untagged-"+run) {
		t.Fatalf("denied delete changed data: %s", out)
	}
	if code, out := cli(pol, "get", itoa(a), "--json"); code != 0 {
		t.Fatalf("denied batch changed data: %s", out)
	}

	// delete
	if code, out := cli(pol, "delete", itoa(a), "--confirm", "--json"); code != 0 || !strings.Contains(out, `"deleted":true`) {
		t.Fatalf("delete: %s", out)
	}
	if code, _ := cli(pol, "get", itoa(a)); code != agentcli.ExitNotFound {
		t.Fatal("deleted document still readable")
	}

	// A wrong token is an auth failure, never echoed.
	t.Setenv(EnvToken, "agent-cli-test-wrong-token")
	if code, _ := cli(pol, "tags"); code != agentcli.ExitPermission {
		t.Fatal("wrong token must exit 6")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
