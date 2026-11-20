package paperless

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Bam679/paperless-cli/internal/agentcli"
)

var update = flag.Bool("update", false, "rewrite golden files")

// TestMain pins the zone so golden files match on every machine.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	pollEvery = time.Millisecond
	os.Exit(m.Run())
}

// fakeClient is an in-memory Client. Fixture data is synthetic only.
type fakeClient struct {
	tags   []Tag
	corr   []Named
	types  []Named
	docs   []Document
	files  map[int]string
	tasks  map[string]Task
	next   int
	writes int   // calls of any write method
	fail   error // returned by every method
	wfail  error // returned by write methods only
	calls  []string
}

func day(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.Local) }

func newFake() *fakeClient {
	at := time.Date(2026, 11, 2, 9, 30, 0, 0, time.Local)
	return &fakeClient{
		tags: []Tag{{ID: 1, Name: "invoices", DocumentCount: 2}, {ID: 2, Name: "medical", DocumentCount: 2},
			{ID: 3, Name: "agent-sandbox", DocumentCount: 1, IsInbox: true}, {ID: 4, Name: "dup"}, {ID: 5, Name: "DUP"}},
		corr:  []Named{{ID: 1, Name: "Acme Corp", DocumentCount: 1}, {ID: 2, Name: "Example Bank", DocumentCount: 1}},
		types: []Named{{ID: 1, Name: "Invoice", DocumentCount: 1}, {ID: 2, Name: "Letter", DocumentCount: 1}},
		docs: []Document{
			{ID: 1, Title: "Acme invoice 2026-11", Created: day(11, 1), Added: at, Modified: at, Tags: []int{1},
				Correspondent: 1, DocumentType: 1, Content: "Invoice total 42.00", OriginalFileName: "acme.pdf", MimeType: "application/pdf", PageCount: 1},
			{ID: 2, Title: "Lab results", Created: day(10, 15), Added: at, Modified: at, Tags: []int{2}, Content: "synthetic lab values"},
			{ID: 3, Title: "Agent scratch", Created: day(11, 10), Added: at, Modified: at, Tags: []int{3}, Content: "hello\nworld\n" + strings.Repeat("synthetic line. ", 30), ASN: 7},
			{ID: 4, Title: "Unfiled letter", Created: day(9, 1), Added: at, Modified: at, Tags: []int{}, Correspondent: 2, DocumentType: 2, Content: "dear customer"},
			{ID: 5, Title: "Mixed invoice", Created: day(11, 5), Added: at, Modified: at, Tags: []int{1, 2}, Content: "invoice for therapy"},
		},
		files: map[int]string{1: "%PDF-synthetic", 3: "scratch"},
		tasks: map[string]Task{},
	}
}

func (f *fakeClient) Info(context.Context) (ServerInfo, error) {
	return ServerInfo{BaseURL: "https://paperless.example.test", TokenSource: "env", Version: "3.5.1", APIVersion: "10"}, f.fail
}

func (f *fakeClient) match(d Document, q Query) bool {
	low := func(s string) string { return strings.ToLower(s) }
	if q.TitleContent != "" && !strings.Contains(low(d.Title+" "+d.Content), low(q.TitleContent)) {
		return false
	}
	if q.FullText != "" && !strings.Contains(low(d.Title+" "+d.Content), low(q.FullText)) {
		return false
	}
	for _, t := range q.TagsAll {
		if !containsInt(d.Tags, t) {
			return false
		}
	}
	if (q.Correspondent > 0 && d.Correspondent != q.Correspondent) || (q.DocumentType > 0 && d.DocumentType != q.DocumentType) {
		return false
	}
	c := d.Created.Format("2006-01-02")
	return (q.CreatedFrom == "" || c >= q.CreatedFrom) && (q.CreatedBefore == "" || c < q.CreatedBefore)
}

func containsInt(l []int, n int) bool {
	for _, x := range l {
		if x == n {
			return true
		}
	}
	return false
}

func (f *fakeClient) Documents(_ context.Context, q Query, page, size int) (DocPage, error) {
	f.calls = append(f.calls, fmt.Sprintf("documents page=%d size=%d", page, size))
	if f.fail != nil {
		return DocPage{}, f.fail
	}
	var all []Document
	for _, d := range f.docs {
		if f.match(d, q) {
			if q.FullText != "" {
				d.Search = &SearchHit{Score: 1.5, Highlights: "…<b>" + q.FullText + "</b> &amp; more…"}
			}
			d.Content = ""
			all = append(all, d)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].Created.Equal(all[j].Created) {
			return all[i].Created.After(all[j].Created)
		}
		return all[i].ID > all[j].ID
	})
	start := (page - 1) * size
	if start > len(all) {
		start = len(all)
	}
	end := start + size
	if end > len(all) {
		end = len(all)
	}
	for i := start; i < end; i++ {
		if all[i].Search != nil {
			all[i].Search.Rank = i + 1
		}
	}
	return DocPage{Count: len(all), Docs: all[start:end]}, nil
}

func (f *fakeClient) idx(id int) int {
	for i, d := range f.docs {
		if d.ID == id {
			return i
		}
	}
	return -1
}

func (f *fakeClient) Document(_ context.Context, id int, content bool) (Document, error) {
	if f.fail != nil {
		return Document{}, f.fail
	}
	i := f.idx(id)
	if i < 0 {
		return Document{}, agentcli.NotFoundf("no document with id %d", id)
	}
	d := f.docs[i]
	if !content {
		d.Content = ""
	}
	return d, nil
}

func (f *fakeClient) Download(_ context.Context, id int, original bool, w io.Writer) (string, string, int64, error) {
	if f.fail != nil {
		return "", "", 0, f.fail
	}
	body, ok := f.files[id]
	if !ok {
		_, _ = io.WriteString(w, "partial")
		return "", "", 0, agentcli.Unavailablef("download interrupted")
	}
	n, err := io.WriteString(w, body)
	name := fmt.Sprintf("doc-%d.pdf", id)
	if original {
		name = "original-" + name
	}
	return name, "application/pdf", int64(n), err
}

func (f *fakeClient) Tags(context.Context) ([]Tag, error) { return append([]Tag{}, f.tags...), f.fail }
func (f *fakeClient) Correspondents(context.Context) ([]Named, error) {
	return append([]Named{}, f.corr...), f.fail
}
func (f *fakeClient) DocumentTypes(context.Context) ([]Named, error) {
	return append([]Named{}, f.types...), f.fail
}

func (f *fakeClient) write() error {
	f.writes++
	if f.fail != nil {
		return f.fail
	}
	return f.wfail
}

func (f *fakeClient) Upload(_ context.Context, u Upload) (string, error) {
	if err := f.write(); err != nil {
		return "", err
	}
	data, _ := io.ReadAll(u.File)
	f.next++
	id := 100 + f.next
	title := u.Title
	if title == "" {
		title = strings.TrimSuffix(u.FileName, filepath.Ext(u.FileName))
	}
	f.docs = append(f.docs, Document{ID: id, Title: title, Tags: append([]int{}, u.Tags...), Created: day(11, 20),
		Correspondent: u.Correspondent, DocumentType: u.DocumentType, Content: string(data)})
	task := fmt.Sprintf("task-%d", f.next)
	f.tasks[task] = Task{ID: task, Status: "SUCCESS", Document: id}
	return task, nil
}

func (f *fakeClient) Task(_ context.Context, id string) (Task, error) {
	t, ok := f.tasks[id]
	if !ok {
		return Task{}, agentcli.NotFoundf("no task %s", id)
	}
	return t, nil
}

func (f *fakeClient) Update(_ context.Context, id int, p Patch) (Document, error) {
	if err := f.write(); err != nil {
		return Document{}, err
	}
	d := &f.docs[f.idx(id)]
	if p.Title != nil {
		d.Title = *p.Title
	}
	if p.Tags != nil {
		d.Tags = *p.Tags
	}
	if p.Correspondent != nil {
		d.Correspondent = *p.Correspondent
	}
	if p.DocumentType != nil {
		d.DocumentType = *p.DocumentType
	}
	return *d, nil
}

func (f *fakeClient) Delete(_ context.Context, id int) error {
	if err := f.write(); err != nil {
		return err
	}
	i := f.idx(id)
	f.docs = append(f.docs[:i], f.docs[i+1:]...)
	return nil
}

func (f *fakeClient) CreateTag(_ context.Context, name string) (Tag, error) {
	if err := f.write(); err != nil {
		return Tag{}, err
	}
	t := Tag{ID: 10 + len(f.tags), Name: name}
	f.tags = append(f.tags, t)
	return t, nil
}

var (
	readOnly  = agentcli.Effective{Layers: []agentcli.Policy{agentcli.DefaultPolicy()}}
	sandboxRW = agentcli.Effective{Layers: []agentcli.Policy{{Version: 1,
		Operations: agentcli.OpRule{Allow: []string{agentcli.OpRead, agentcli.OpWrite}},
		Scopes: agentcli.ScopeRules{Read: agentcli.Rule{Allow: []string{"*"}, Deny: []string{"tag:medical"}},
			Write: agentcli.Rule{Allow: []string{"tag:agent-*"}}}}}}
)

func run(t *testing.T, cl Client, pol agentcli.Effective, args ...string) (int, string, string) {
	t.Helper()
	app := NewApp(cl, "test")
	app.LoadPolicy = func() (agentcli.Effective, error) { return pol, nil }
	app.Now = func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.Local) }
	var out, errb bytes.Buffer
	code := app.Main(context.Background(), args, strings.NewReader(""), &out, &errb)
	return code, out.String(), errb.String()
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", "golden", name)
	if *update {
		_ = os.MkdirAll(filepath.Dir(p), 0o750)
		if err := os.WriteFile(p, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatalf("missing golden %s: run go test -update", p)
	}
	if got != string(want) {
		t.Fatalf("golden %s mismatch:\n%s\nwant:\n%s", name, got, want)
	}
}

func TestReadCommandsGolden(t *testing.T) {
	cases := map[string][]string{
		"documents.txt":        {"documents"},
		"documents.json":       {"documents", "--json"},
		"documents_filter.txt": {"documents", "--tag", "INVOICES", "--correspondent", "acme corp", "--type", "1", "--created-after", "2026-10-01", "--created-before", "+1d"},
		"documents_query.txt":  {"documents", "--query", "LETTER"},
		"documents_page.txt":   {"documents", "--limit", "2"},
		"search.txt":           {"search", "invoice"},
		"search.ndjson":        {"search", "invoice", "--ndjson", "--fields", "id,title,score,rank,highlights"},
		"get.txt":              {"get", "3", "--content"},
		"get_plain.json":       {"get", "1", "--json"},
		"get_maxchars.json":    {"get", "3", "--content", "--json", "--max-chars", "400"},
		"tags.txt":             {"tags"},
		"correspondents.txt":   {"correspondents"},
		"document_types.txt":   {"document-types", "--json"},
		"doctor.txt":           {"doctor"},
		"schema.json":          {"schema"},
	}
	for name, args := range cases {
		code, out, errOut := run(t, newFake(), sandboxRW, args...)
		if code != 0 {
			t.Fatalf("%s: exit %d %s", name, code, errOut)
		}
		golden(t, name, out)
	}
}

func TestReadScopeHidesTag(t *testing.T) {
	for _, args := range [][]string{{"documents"}, {"documents", "--json"}, {"search", "lab"}, {"search", "invoice"}, {"tags"}} {
		_, out, _ := run(t, newFake(), sandboxRW, args...)
		for _, leak := range []string{"Lab results", "Mixed invoice", "medical"} {
			if strings.Contains(out, leak) {
				t.Fatalf("%v leaked %q: %s", args, leak, out)
			}
		}
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"get", "2"}, {"get", "5", "--content"}, {"documents", "--tag", "medical"},
		{"download", "2", "--out", filepath.Join(dir, "x")},
	} {
		if code, _, _ := run(t, newFake(), sandboxRW, args...); code != agentcli.ExitPolicy {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "x")); err == nil {
		t.Fatal("denied download created a file")
	}
	// Writes on a hidden document are refused before the write gate.
	if code, _, _ := run(t, newFake(), sandboxRW, "update", "5", "--title", "x", "--dry-run"); code != agentcli.ExitPolicy {
		t.Fatal("update of hidden document")
	}
}

// Pages through a larger archive where every third document is hidden.
func TestPaginationSkipsHiddenDocuments(t *testing.T) {
	f := newFake()
	f.docs = nil
	want := map[int]bool{}
	for i := 1; i <= 130; i++ {
		tags := []int{1}
		if i%3 == 0 {
			tags = []int{1, 2}
		} else {
			want[i] = true
		}
		f.docs = append(f.docs, Document{ID: i, Title: fmt.Sprintf("Doc %d", i), Created: day(1, 1).AddDate(0, 0, i), Tags: tags})
	}
	got := map[int]bool{}
	cursor := ""
	for pages := 0; pages < 100; pages++ {
		args := []string{"documents", "--limit", "7", "--json"}
		if cursor != "" {
			args = append(args, "--cursor", cursor)
		}
		code, out, errOut := run(t, f, sandboxRW, args...)
		if code != 0 {
			t.Fatalf("exit %d %s", code, errOut)
		}
		var page struct {
			Items      []struct{ ID int }
			NextCursor string `json:"next_cursor"`
		}
		if err := json.Unmarshal([]byte(out), &page); err != nil {
			t.Fatal(err)
		}
		for _, it := range page.Items {
			if got[it.ID] || !want[it.ID] {
				t.Fatalf("unexpected or duplicate id %d", it.ID)
			}
			got[it.ID] = true
		}
		if cursor = page.NextCursor; cursor == "" {
			break
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d documents, want %d", len(got), len(want))
	}
	// Large limits scan several server pages per call.
	code, out, _ := run(t, f, sandboxRW, "documents", "--limit", "200", "--json")
	if code != 0 || strings.Contains(out, "next_cursor") || strings.Count(out, `"id":`) != len(want) {
		t.Fatalf("limit 200: %d %s", code, out)
	}
	code, _, _ = run(t, f, sandboxRW, "documents", "--limit", "7", "--cursor", agentcli.EncodeCursor(7, "other"))
	if code != agentcli.ExitUsage {
		t.Fatal("cursor from another query must be rejected")
	}
}

func TestScanCapReturnsCursor(t *testing.T) {
	f := newFake()
	f.docs = nil
	for i := 1; i <= 1200; i++ {
		f.docs = append(f.docs, Document{ID: i, Title: "hidden", Created: day(1, 1), Tags: []int{2}})
	}
	code, out, _ := run(t, f, sandboxRW, "documents", "--limit", "5", "--json")
	if code != 0 || !strings.Contains(out, `"count":0`) || !strings.Contains(out, "next_cursor") {
		t.Fatalf("%d %s", code, out)
	}
	if len(f.calls) != maxScanPages {
		t.Fatalf("scanned %d pages", len(f.calls))
	}
}

func TestPolicyDeniedWriteNeverReachesClient(t *testing.T) {
	file := filepath.Join(t.TempDir(), "scan.txt")
	if err := os.WriteFile(file, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newFake()
	for _, args := range [][]string{
		{"upload", file, "--tag", "agent-sandbox", "--confirm"},
		{"update", "3", "--title", "x", "--confirm"},
		{"delete", "3", "--confirm"},
		{"tag-create", "agent-new", "--dry-run"},
	} {
		code, _, errOut := run(t, f, readOnly, args...)
		if code != agentcli.ExitPolicy || !strings.Contains(errOut, "policy_denied") {
			t.Fatalf("read-only %v: exit %d %s", args, code, errOut)
		}
	}
	// Write-enabled policy, but scopes outside tag:agent-* are denied.
	for _, args := range [][]string{
		{"delete", "1", "--confirm"},                                    // tag:invoices
		{"delete", "3", "1", "--confirm"},                               // mixed batch
		{"delete", "4", "--confirm"},                                    // untagged
		{"update", "3", "--add-tag", "invoices", "--confirm"},           // adding a foreign tag
		{"update", "1", "--remove-tag", "invoices", "--confirm"},        // removing a foreign tag
		{"update", "3", "--remove-tag", "agent-sandbox", "--confirm"},   // would become untagged
		{"upload", file, "--confirm"},                                   // untagged upload
		{"upload", file, "--tag", "invoices", "--confirm"},              // foreign tag upload
		{"tag-create", "invoices-2026", "--confirm"},                    // outside tag:agent-*
		{"upload", file, "--tag", "agent-sandbox", "--tag", "invoices"}, // mixed tags
	} {
		code, _, errOut := run(t, f, sandboxRW, args...)
		if code != agentcli.ExitPolicy {
			t.Fatalf("%v: exit %d %s", args, code, errOut)
		}
	}
	if f.writes != 0 || len(f.docs) != 5 || len(f.tags) != 5 {
		t.Fatalf("a denied write reached the client: writes=%d", f.writes)
	}
}

func TestWriteFlow(t *testing.T) {
	f := newFake()
	code, out, _ := run(t, f, sandboxRW, "tag-create", "agent-new", "--dry-run")
	if code != 0 || !strings.Contains(out, "agent-new") || f.writes != 0 {
		t.Fatalf("dry-run: %d %s", code, out)
	}
	if code, _, _ := run(t, f, sandboxRW, "tag-create", "agent-new"); code != agentcli.ExitConfirm {
		t.Fatalf("missing confirm: %d", code)
	}
	if code, _, e := run(t, f, sandboxRW, "tag-create", "agent-new", "--confirm"); code != 0 {
		t.Fatalf("tag-create: %s", e)
	}
	if code, _, _ := run(t, f, sandboxRW, "tag-create", "AGENT-NEW", "--confirm"); code != agentcli.ExitConflict {
		t.Fatal("duplicate tag must conflict")
	}
	code, out, e := run(t, f, sandboxRW, "update", "3", "--title", "Renamed", "--add-tag", "agent-new", "--correspondent", "Acme Corp", "--type", "Letter", "--confirm", "--json")
	if code != 0 || f.docs[2].Title != "Renamed" || len(f.docs[2].Tags) != 2 || f.docs[2].DocumentType != 2 {
		t.Fatalf("update: %d %s %s", code, out, e)
	}
	if code, _, e := run(t, f, sandboxRW, "update", "3", "--remove-tag", "agent-sandbox", "--add-tag", "agent-new", "--confirm"); code != 0 || len(f.docs[2].Tags) != 1 || f.docs[2].Tags[0] != 10+5 {
		t.Fatalf("remove tag: %d %s %v", code, e, f.docs[2].Tags)
	}

	file := filepath.Join(t.TempDir(), "scan.txt")
	if err := os.WriteFile(file, []byte("synthetic scan"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, e = run(t, f, sandboxRW, "upload", file, "--tag", "agent-sandbox", "--title", "Scan", "--correspondent", "2", "--type", "invoice", "--dry-run")
	if code != 0 || !strings.Contains(out, "scan.txt") || f.writes != 3 {
		t.Fatalf("upload dry-run: %d %s %s", code, out, e)
	}
	code, out, e = run(t, f, sandboxRW, "upload", file, "--tag", "agent-sandbox", "--confirm", "--json")
	if code != 0 || !strings.Contains(out, `"status":"queued"`) || !strings.Contains(out, "task-1") {
		t.Fatalf("upload: %d %s %s", code, out, e)
	}
	code, out, e = run(t, f, sandboxRW, "upload", file, "--tag", "agent-sandbox", "--title", "Waited", "--wait", "--confirm", "--json")
	if code != 0 || !strings.Contains(out, `"status":"consumed"`) || !strings.Contains(out, `"id":102`) || !strings.Contains(out, "Waited") {
		t.Fatalf("upload --wait: %d %s %s", code, out, e)
	}
	if code, out, _ := run(t, f, sandboxRW, "delete", "3", "102", "102", "--confirm", "--json"); code != 0 || strings.Count(out, `"deleted":true`) != 2 || f.idx(3) >= 0 {
		t.Fatalf("delete: %s", out)
	}
}

func TestDownload(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "a.pdf")
	code, stdout, e := run(t, newFake(), sandboxRW, "download", "1", "--out", out, "--original", "--json")
	if code != 0 || !strings.Contains(stdout, "original-doc-1.pdf") {
		t.Fatalf("download: %d %s %s", code, stdout, e)
	}
	st, err := os.Stat(out)
	if err != nil || st.Mode().Perm() != 0o600 || st.Size() != int64(len("%PDF-synthetic")) {
		t.Fatalf("file: %v %v", err, st)
	}
	if code, _, _ := run(t, newFake(), sandboxRW, "download", "1", "--out", out); code != agentcli.ExitConflict {
		t.Fatal("existing file must not be overwritten")
	}
	broken := filepath.Join(dir, "b.pdf")
	if code, _, _ := run(t, newFake(), sandboxRW, "download", "4", "--out", broken); code != agentcli.ExitUnavailable {
		t.Fatal("failed download")
	}
	if _, err := os.Stat(broken); err == nil {
		t.Fatal("partial download was left behind")
	}
	if code, _, _ := run(t, newFake(), sandboxRW, "download", "1", "--out", filepath.Join(dir, "missing", "x")); code != agentcli.ExitUsage {
		t.Fatal("missing directory")
	}
	// The default policy allows the download op (it is a read).
	if code, _, _ := run(t, newFake(), readOnly, "download", "3", "--out", filepath.Join(dir, "c")); code != 0 {
		t.Fatal("download under read-only policy")
	}
	deny := agentcli.Effective{Layers: []agentcli.Policy{{Version: 1,
		Operations: agentcli.OpRule{Allow: []string{agentcli.OpRead}, Deny: []string{"download"}},
		Scopes:     agentcli.ScopeRules{Read: agentcli.Rule{Allow: []string{"*"}}}}}}
	if code, _, _ := run(t, newFake(), deny, "download", "3", "--out", filepath.Join(dir, "d")); code != agentcli.ExitPolicy {
		t.Fatal("download op deny")
	}
}

func TestValidation(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		"bad id":           {"get", "abc"},
		"zero id":          {"get", "0"},
		"bad delete id":    {"delete", "3", "x", "--dry-run"},
		"nothing":          {"update", "3", "--dry-run"},
		"empty title":      {"update", "3", "--title", " ", "--dry-run"},
		"add and remove":   {"update", "3", "--add-tag", "agent-sandbox", "--remove-tag", "agent-sandbox", "--dry-run"},
		"add and remove 2": {"update", "4", "--add-tag", "invoices", "--remove-tag", "1", "--dry-run"},
		"empty tag":        {"tag-create", " ", "--dry-run"},
		"control tag":      {"tag-create", "agent\nx", "--dry-run"},
		"long tag":         {"tag-create", "agent-" + strings.Repeat("x", 200), "--dry-run"},
		"bad date":         {"documents", "--created-after", "someday"},
		"bad date 2":       {"documents", "--created-before", "x"},
		"stdout out":       {"download", "1", "--out", "-"},
		"empty search":     {"search", " "},
		"directory":        {"upload", dir, "--dry-run"},
		"empty file":       {"upload", empty, "--dry-run"},
		"empty up tag":     {"upload", "commands.go", "--tag", " ", "--dry-run"},
		"empty name":       {"documents", "--tag", " "},
		"upload title":     {"upload", "commands.go", "--title", " ", "--dry-run"},
	}
	for name, args := range cases {
		if code, _, e := run(t, newFake(), sandboxRW, args...); code != agentcli.ExitUsage {
			t.Errorf("%s: exit %d %s", name, code, e)
		}
	}
	notFound := [][]string{
		{"get", "99"}, {"upload", filepath.Join(dir, "nope"), "--dry-run"}, {"documents", "--correspondent", "nobody"},
		{"documents", "--type", "nothing"}, {"update", "3", "--add-tag", "nope", "--dry-run"},
		{"upload", "commands.go", "--correspondent", "nobody", "--dry-run"}, {"upload", "commands.go", "--type", "x", "--dry-run"},
		{"update", "3", "--correspondent", "nobody", "--dry-run"}, {"update", "3", "--type", "x", "--dry-run"},
		{"delete", "99", "--dry-run"},
	}
	for _, args := range notFound {
		if code, _, e := run(t, newFake(), sandboxRW, args...); code != agentcli.ExitNotFound {
			t.Errorf("%v: exit %d %s", args, code, e)
		}
	}
	if code, _, _ := run(t, newFake(), sandboxRW, "documents", "--tag", "Dup"); code != agentcli.ExitConflict {
		t.Error("ambiguous tag must conflict")
	}
	if code, _, _ := run(t, newFake(), sandboxRW, "documents", "--tag", "dup"); code != 0 {
		t.Error("exact tag name wins over case-insensitive matches")
	}
}

func TestClientErrors(t *testing.T) {
	f := newFake()
	f.fail = agentcli.Permissionf("denied")
	for _, args := range [][]string{{"documents"}, {"search", "x"}, {"tags"}, {"correspondents"}, {"document-types"}, {"get", "1"}} {
		if code, _, _ := run(t, f, sandboxRW, args...); code != agentcli.ExitPermission {
			t.Errorf("%v: %d", args, code)
		}
	}
	if code, out, _ := run(t, f, sandboxRW, "doctor"); code != agentcli.ExitPermission || !strings.Contains(out, "false") {
		t.Errorf("doctor: %d %s", code, out)
	}
	g := newFake()
	g.wfail = agentcli.Unavailablef("x")
	if code, _, e := run(t, g, sandboxRW, "tag-create", "agent-x", "--confirm"); code != agentcli.ExitUnavailable {
		t.Errorf("tag-create failure: %d %s", code, e)
	}
	if code, _, _ := run(t, g, sandboxRW, "update", "3", "--title", "x", "--confirm"); code != agentcli.ExitUnavailable {
		t.Error("update failure")
	}
	file := filepath.Join(t.TempDir(), "s.txt")
	_ = os.WriteFile(file, []byte("x"), 0o600)
	if code, _, _ := run(t, g, sandboxRW, "upload", file, "--tag", "agent-sandbox", "--confirm"); code != agentcli.ExitUnavailable {
		t.Error("upload failure")
	}
	g.docs = append(g.docs, Document{ID: 9, Title: "second", Tags: []int{3}})
	if code, _, e := run(t, g, sandboxRW, "delete", "3", "9", "--confirm"); code != agentcli.ExitUnavailable || strings.Contains(e, "of 2") {
		t.Errorf("first failure is not partial: %d %s", code, e)
	}
}

type failingTasks struct {
	*fakeClient
	task Task
	err  error
}

func (f failingTasks) Task(context.Context, string) (Task, error) { return f.task, f.err }

func TestWaitTask(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		f    failingTasks
		exit int
	}{
		{failingTasks{task: Task{Status: "FAILURE", Result: "Not consuming x: It is a duplicate of y"}}, agentcli.ExitConflict},
		{failingTasks{task: Task{Status: "FAILURE", Result: "parser error"}}, agentcli.ExitUnavailable},
		{failingTasks{err: agentcli.Permissionf("no")}, agentcli.ExitPermission},
	}
	for _, c := range cases {
		if _, err := waitTask(ctx, c.f, "t"); agentcli.AsError(err).Exit != c.exit {
			t.Errorf("%+v: %v", c.f.task, err)
		}
	}
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err := waitTask(short, failingTasks{task: Task{Status: "STARTED"}}, "t")
	if e := agentcli.AsError(err); e.Exit != agentcli.ExitUnavailable || !strings.Contains(e.Message, "upload itself succeeded") {
		t.Fatal(err)
	}
	short2, cancel2 := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel2()
	if _, err := waitTask(short2, failingTasks{err: agentcli.NotFoundf("not yet")}, "t"); agentcli.AsError(err).Exit != agentcli.ExitUnavailable {
		t.Fatal("unknown task keeps polling until timeout")
	}
}

func TestPartial(t *testing.T) {
	e := agentcli.AsError(partial(agentcli.Unavailablef("boom"), 2, 5))
	if e.Exit != agentcli.ExitUnavailable || !strings.HasPrefix(e.Message, "2 of 5") {
		t.Fatal(e)
	}
}

func TestUnknownRelatedIDs(t *testing.T) {
	f := newFake()
	f.docs[0].Tags = []int{77}
	f.docs[0].Correspondent = 88
	_, out, _ := run(t, f, sandboxRW, "get", "1", "--json")
	if !strings.Contains(out, `"#77"`) || !strings.Contains(out, `"#88"`) {
		t.Fatal(out)
	}
	restrictive := agentcli.Effective{Layers: []agentcli.Policy{{Version: 1,
		Operations: agentcli.OpRule{Allow: []string{agentcli.OpRead}},
		Scopes:     agentcli.ScopeRules{Read: agentcli.Rule{Allow: []string{"tag:invoices"}}}}}}
	if code, _, _ := run(t, f, restrictive, "get", "1"); code != agentcli.ExitPolicy {
		t.Fatal("tags unknown to the API user must not pass an allowlist")
	}
}

func TestSchemaListsWriteOps(t *testing.T) {
	_, out, _ := run(t, newFake(), readOnly, "schema")
	var s struct {
		Commands []struct{ Name, Kind, Op string }
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}
	ops := map[string]string{}
	for _, c := range s.Commands {
		ops[c.Name] = c.Kind + "/" + c.Op
	}
	want := map[string]string{"upload": "write/upload", "update": "write/update", "delete": "write/delete",
		"tag-create": "write/tag-create", "download": "read/download", "documents": "read/read"}
	for k, v := range want {
		if ops[k] != v {
			t.Errorf("%s: %s, want %s", k, ops[k], v)
		}
	}
}
