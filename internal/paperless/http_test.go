package paperless

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Bam679/paperless-cli/internal/agentcli"
)

const testToken = "fake-test-token-aaaa"

// fakeServer is a minimal Paperless-ngx API. Every request must carry the
// token; requests are recorded for assertions.
type fakeServer struct {
	mu   sync.Mutex
	reqs []string
	body map[string][]byte
	srv  *httptest.Server
}

func newServer(t *testing.T, routes map[string]http.HandlerFunc) *fakeServer {
	t.Helper()
	fs := &fakeServer{body: map[string][]byte{}}
	fs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token "+testToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"detail":"Invalid token."}`)
			return
		}
		if r.Header.Get("Accept") != "application/json; version=9" {
			t.Errorf("Accept header %q", r.Header.Get("Accept"))
		}
		key := r.Method + " " + r.URL.Path
		b, _ := io.ReadAll(r.Body)
		fs.mu.Lock()
		fs.reqs = append(fs.reqs, key+"?"+r.URL.RawQuery)
		fs.body[key] = b
		fs.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(b))
		if h, ok := routes[key]; ok {
			h(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"detail":"Not found."}`)
	}))
	t.Cleanup(fs.srv.Close)
	return fs
}

func (fs *fakeServer) client() *HTTP {
	return NewHTTPWithConfig(Config{BaseURL: fs.srv.URL, Token: testToken, TokenSource: "env"})
}

func rawReply(code int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}
}

const docJSON = `{"id":7,"title":"Synthetic","content":"ocr text","created":"2026-11-01","added":"2026-11-02T09:30:00Z",
"modified":"2026-11-03T10:00:00+01:00","correspondent":2,"document_type":null,"tags":[1,3],"archive_serial_number":12,
"original_file_name":"s.pdf","mime_type":"application/pdf","page_count":3}`

func TestHTTPDocuments(t *testing.T) {
	fs := newServer(t, map[string]http.HandlerFunc{
		"GET /api/documents/": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") == "3" {
				rawReply(404, `{"detail":"Invalid page."}`)(w, r)
				return
			}
			_, _ = fmt.Fprintf(w, `{"count":1,"next":null,"results":[%s]}`, strings.Replace(docJSON, `"page_count":3`,
				`"page_count":3,"__search_hit__":{"score":0.5,"rank":1,"highlights":"<b>x</b>"}`, 1))
		},
	})
	cl := fs.client()
	ctx := context.Background()
	p, err := cl.Documents(ctx, Query{TitleContent: "acme", TagsAll: []int{1, 3}, Correspondent: 2, DocumentType: 4,
		CreatedFrom: "2026-01-01", CreatedBefore: "2026-12-01"}, 1, 25)
	if err != nil || p.Count != 1 || len(p.Docs) != 1 {
		t.Fatalf("%v %+v", err, p)
	}
	d := p.Docs[0]
	if d.ID != 7 || d.Correspondent != 2 || d.DocumentType != 0 || d.ASN != 12 || d.PageCount != 3 ||
		d.Created.Format("2006-01-02") != "2026-11-01" || d.Added.IsZero() || d.Search.Highlights != "<b>x</b>" {
		t.Fatalf("%+v", d)
	}
	q := fs.reqs[0]
	for _, want := range []string{"title_content=acme", "tags__id__all=1%2C3", "correspondent__id=2", "document_type__id=4",
		"created__gte=2026-01-01", "created__lt=2026-12-01", "ordering=-created%2C-id", "page=1", "page_size=25", "fields="} {
		if !strings.Contains(q, want) {
			t.Errorf("query %s lacks %s", q, want)
		}
	}
	if _, err := cl.Documents(ctx, Query{FullText: "zebra"}, 2, 10); err != nil {
		t.Fatal(err)
	}
	if q := fs.reqs[1]; !strings.Contains(q, "query=zebra") || strings.Contains(q, "ordering") || !strings.Contains(q, "__search_hit__") {
		t.Errorf("full-text query %s", q)
	}
	if p, err := cl.Documents(ctx, Query{}, 3, 10); err != nil || len(p.Docs) != 0 {
		t.Fatalf("page past the end: %v %+v", err, p)
	}
}

func TestHTTPDocumentAndDownload(t *testing.T) {
	fs := newServer(t, map[string]http.HandlerFunc{
		"GET /api/documents/7/": func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, docJSON) },
		"GET /api/documents/7/download/": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", `attachment; filename="../../etc/x.pdf"; filename*=utf-8''..%2F..%2Fetc%2Fsynthetic%20file.pdf`)
			_, _ = io.WriteString(w, "%PDF-1.4 synthetic")
		},
		"GET /api/documents/8/download/": rawReply(200, "plain"),
	})
	cl := fs.client()
	ctx := context.Background()
	d, err := cl.Document(ctx, 7, true)
	if err != nil || d.Content != "ocr text" || len(d.Tags) != 2 {
		t.Fatalf("%v %+v", err, d)
	}
	if !strings.Contains(fs.reqs[0], "content") {
		t.Errorf("content not requested: %s", fs.reqs[0])
	}
	if _, err := cl.Document(ctx, 99, false); agentcli.AsError(err).Exit != agentcli.ExitNotFound || !strings.Contains(err.Error(), "99") {
		t.Fatalf("missing document: %v", err)
	}
	var buf bytes.Buffer
	name, mime, n, err := cl.Download(ctx, 7, true, &buf)
	if err != nil || name != "synthetic file.pdf" || mime != "application/pdf" || n != int64(buf.Len()) {
		t.Fatalf("%v %q %q %d", err, name, mime, n)
	}
	if !strings.Contains(fs.reqs[len(fs.reqs)-1], "original=true") {
		t.Error("original not requested")
	}
	if name, _, _, err := cl.Download(ctx, 8, false, io.Discard); err != nil || name != "" {
		t.Fatalf("no disposition: %v %q", err, name)
	}
	if _, _, _, err := cl.Download(ctx, 9, false, io.Discard); agentcli.AsError(err).Exit != agentcli.ExitNotFound {
		t.Fatal(err)
	}
}

func TestHTTPLookupsPaginate(t *testing.T) {
	fs := newServer(t, map[string]http.HandlerFunc{
		"GET /api/tags/": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") == "1" {
				_, _ = io.WriteString(w, `{"next":"x","results":[{"id":1,"name":"a","document_count":2,"is_inbox_tag":true}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"next":null,"results":[{"id":2,"name":"b"}]}`)
		},
		"GET /api/correspondents/":  rawReply(200, `{"next":null,"results":[{"id":1,"name":"Acme","document_count":4}]}`),
		"GET /api/document_types/":  rawReply(200, `{"next":"y","results":[]}`),
		"POST /api/tags/":           rawReply(201, `{"id":9,"name":"agent-x","document_count":0}`),
		"DELETE /api/documents/7/":  rawReply(204, ""),
		"PATCH /api/documents/7/":   func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, docJSON) },
		"GET /api/tasks/":           taskRoute,
		"DELETE /api/documents/99/": rawReply(404, `{"detail":"No Document matches the given query."}`),
	})
	cl := fs.client()
	ctx := context.Background()
	tags, err := cl.Tags(ctx)
	if err != nil || len(tags) != 2 || !tags[0].IsInbox || tags[0].DocumentCount != 2 {
		t.Fatalf("%v %+v", err, tags)
	}
	cs, err := cl.Correspondents(ctx)
	if err != nil || len(cs) != 1 || cs[0].DocumentCount != 4 {
		t.Fatalf("%v %+v", err, cs)
	}
	if ts, err := cl.DocumentTypes(ctx); err != nil || len(ts) != 0 {
		t.Fatalf("%v %+v", err, ts)
	}
	if tg, err := cl.CreateTag(ctx, "agent-x"); err != nil || tg.ID != 9 {
		t.Fatalf("%v %+v", err, tg)
	}
	if string(fs.body["POST /api/tags/"]) != `{"name":"agent-x"}` {
		t.Errorf("tag body %s", fs.body["POST /api/tags/"])
	}
	title, tagIDs, corr, typ := "New", []int{3}, 2, 5
	if _, err := cl.Update(ctx, 7, Patch{Title: &title, Tags: &tagIDs, Correspondent: &corr, DocumentType: &typ}); err != nil {
		t.Fatal(err)
	}
	var patch map[string]any
	_ = json.Unmarshal(fs.body["PATCH /api/documents/7/"], &patch)
	if patch["title"] != "New" || patch["correspondent"] != float64(2) || patch["document_type"] != float64(5) || len(patch["tags"].([]any)) != 1 {
		t.Errorf("patch body %v", patch)
	}
	if _, err := cl.Update(ctx, 99, Patch{Title: &title}); agentcli.AsError(err).Exit != agentcli.ExitNotFound {
		t.Fatal(err)
	}
	if err := cl.Delete(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if err := cl.Delete(ctx, 99); agentcli.AsError(err).Exit != agentcli.ExitNotFound {
		t.Fatal(err)
	}
	for id, want := range map[string]int{"int": 7, "str": 8, "none": 0} {
		tk, err := cl.Task(ctx, id)
		if err != nil || tk.Document != want || tk.Status != "SUCCESS" {
			t.Errorf("%s: %v %+v", id, err, tk)
		}
	}
	if _, err := cl.Task(ctx, "missing"); agentcli.AsError(err).Exit != agentcli.ExitNotFound {
		t.Fatal(err)
	}
}

func taskRoute(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("task_id") {
	case "int":
		_, _ = io.WriteString(w, `[{"task_id":"int","status":"SUCCESS","result":"ok","related_document":7}]`)
	case "str":
		_, _ = io.WriteString(w, `[{"task_id":"str","status":"SUCCESS","result":null,"related_document":"8"}]`)
	case "none":
		_, _ = io.WriteString(w, `[{"task_id":"none","status":"SUCCESS","related_document":null}]`)
	default:
		_, _ = io.WriteString(w, `[]`)
	}
}

func TestHTTPUpload(t *testing.T) {
	var got struct {
		file   string
		fields map[string][]string
	}
	fs := newServer(t, map[string]http.HandlerFunc{
		"POST /api/documents/post_document/": func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				rawReply(400, `{"document":["broken upload"]}`)(w, r)
				return
			}
			if r.ContentLength <= 0 || len(r.TransferEncoding) > 0 {
				t.Error("upload must send a Content-Length, not a chunked body")
			}
			f, hdr, err := r.FormFile("document")
			if err != nil || hdr.Filename != "scan.txt" {
				t.Errorf("file part: %v", err)
				return
			}
			b, _ := io.ReadAll(f)
			got.file, got.fields = string(b), r.MultipartForm.Value
			_, _ = io.WriteString(w, `"0b9d-task"`)
		},
	})
	task, err := fs.client().Upload(context.Background(), Upload{FileName: "scan.txt", File: strings.NewReader("synthetic body"), Size: 14,
		Title: "T", Tags: []int{1, 2}, Correspondent: 3, DocumentType: 4})
	if err != nil || task != "0b9d-task" {
		t.Fatalf("%v %q", err, task)
	}
	if got.file != "synthetic body" || got.fields["title"][0] != "T" || len(got.fields["tags"]) != 2 ||
		got.fields["correspondent"][0] != "3" || got.fields["document_type"][0] != "4" {
		t.Fatalf("%+v", got)
	}
	if fs.reqs[0] == "" || !strings.Contains(string(fs.body["POST /api/documents/post_document/"]), "synthetic body") {
		t.Fatal("body")
	}
	if _, err := fs.client().Upload(context.Background(), Upload{FileName: "x", File: errReader{}, Size: 10}); err == nil {
		t.Fatal("read error must fail the upload")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestHTTPInfo(t *testing.T) {
	fs := newServer(t, map[string]http.HandlerFunc{
		"GET /api/tags/": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Version", "3.5.1")
			w.Header().Set("X-Api-Version", "10")
			_, _ = io.WriteString(w, `{"count":0,"results":[]}`)
		},
	})
	info, err := fs.client().Info(context.Background())
	if err != nil || info.Version != "3.5.1" || info.APIVersion != "10" || info.TokenSource != "env" {
		t.Fatalf("%v %+v", err, info)
	}
}

func TestHTTPErrorsNeverLeakToken(t *testing.T) {
	echo := func(code int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = io.WriteString(w, `{"detail":"bad `+strings.TrimPrefix(r.Header.Get("Authorization"), "Token ")+`"}`)
		}
	}
	fs := newServer(t, map[string]http.HandlerFunc{
		"GET /api/tags/":           echo(400),
		"GET /api/correspondents/": echo(500),
		"GET /api/document_types/": rawReply(409, "<html>conflict</html>"),
		"GET /api/documents/1/":    rawReply(403, `{"detail":"`+strings.Repeat("x", 400)+`"}`),
		"GET /api/documents/2/":    rawReply(200, "not json"),
		"GET /api/documents/3/":    rawReply(413, ""),
		"GET /api/documents/4/":    rawReply(429, ""),
	})
	cl, ctx := fs.client(), context.Background()
	checks := []struct {
		call func() error
		exit int
	}{
		{func() error { _, err := cl.Tags(ctx); return err }, agentcli.ExitUsage},
		{func() error { _, err := cl.Correspondents(ctx); return err }, agentcli.ExitUnavailable},
		{func() error { _, err := cl.DocumentTypes(ctx); return err }, agentcli.ExitConflict},
		{func() error { _, err := cl.Document(ctx, 1, false); return err }, agentcli.ExitPermission},
		{func() error { _, err := cl.Document(ctx, 2, false); return err }, agentcli.ExitUnavailable},
		{func() error { _, err := cl.Document(ctx, 3, false); return err }, agentcli.ExitUsage},
		{func() error { _, err := cl.Document(ctx, 4, false); return err }, agentcli.ExitUnavailable},
	}
	for i, c := range checks {
		err := c.call()
		e := agentcli.AsError(err)
		if e.Exit != c.exit || strings.Contains(e.Message, testToken) || strings.Contains(e.Hint, testToken) || len(e.Message) > 400 {
			t.Errorf("%d: %+v", i, e)
		}
	}
	bad := NewHTTPWithConfig(Config{BaseURL: fs.srv.URL, Token: "wrong-token"})
	if _, err := bad.Tags(ctx); agentcli.AsError(err).Exit != agentcli.ExitPermission {
		t.Fatal(err)
	}
}

func TestHTTPRedirects(t *testing.T) {
	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = true
		_, _ = io.WriteString(w, `{"next":null,"results":[]}`)
	}))
	defer other.Close()
	fs := newServer(t, map[string]http.HandlerFunc{
		"GET /api/tags/": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, other.URL+"/api/tags/", http.StatusFound)
		},
		"GET /api/correspondents/": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/api/document_types/", http.StatusMovedPermanently)
		},
		"GET /api/document_types/": rawReply(200, `{"next":null,"results":[{"id":1,"name":"Letter"}]}`),
		"GET /api/documents/1/": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/api/documents/1/", http.StatusFound)
		},
	})
	cl := fs.client()
	if _, err := cl.Tags(context.Background()); agentcli.AsError(err).Exit != agentcli.ExitUnavailable {
		t.Fatalf("cross-host redirect: %v", err)
	}
	if leaked {
		t.Fatal("request reached another host")
	}
	// Same-host redirects keep working (the token check passes on the target).
	if ns, err := cl.Correspondents(context.Background()); err != nil || len(ns) != 1 {
		t.Fatalf("same-host redirect: %v %+v", err, ns)
	}
	if _, err := cl.Document(context.Background(), 1, false); agentcli.AsError(err).Exit != agentcli.ExitUnavailable {
		t.Fatalf("redirect loop: %v", err)
	}
}

func TestHTTPUnreachableAndConfig(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	cl := NewHTTPWithConfig(Config{BaseURL: url, Token: testToken})
	_, err := cl.Tags(context.Background())
	if e := agentcli.AsError(err); e.Exit != agentcli.ExitUnavailable || strings.Contains(e.Message, testToken) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cl.Tags(ctx); err == nil {
		t.Fatal("cancelled context")
	}
	broken := &HTTP{load: func(context.Context) (Config, error) { return Config{}, configErr("PAPERLESS_URL is not set") }, hc: newHTTPClient()}
	if _, err := broken.Info(context.Background()); agentcli.AsError(err).Exit != agentcli.ExitUsage {
		t.Fatal(err)
	}
	if _, err := broken.Tags(context.Background()); agentcli.AsError(err).Exit != agentcli.ExitUsage {
		t.Fatal(err)
	}
	bad := NewHTTPWithConfig(Config{BaseURL: "http://127.0.0.1:1\x7f", Token: testToken})
	if _, err := bad.Tags(context.Background()); agentcli.AsError(err).Exit != agentcli.ExitError {
		t.Fatal(err)
	}
	if NewHTTP() == nil {
		t.Fatal("NewHTTP")
	}
}

// End-to-end through the App with the real HTTP client.
func TestAppOverHTTP(t *testing.T) {
	fs := newServer(t, map[string]http.HandlerFunc{
		"GET /api/tags/":           rawReply(200, `{"next":null,"results":[{"id":1,"name":"invoices"},{"id":3,"name":"medical"}]}`),
		"GET /api/correspondents/": rawReply(200, `{"next":null,"results":[{"id":2,"name":"Acme"}]}`),
		"GET /api/document_types/": rawReply(200, `{"next":null,"results":[]}`),
		"GET /api/documents/":      rawReply(200, `{"count":1,"next":null,"results":[`+docJSON+`]}`),
	})
	code, out, e := run(t, fs.client(), readOnly, "documents", "--json")
	if code != 0 || !strings.Contains(out, `"correspondent":"Acme"`) || !strings.Contains(out, `"medical"`) {
		t.Fatalf("%d %s %s", code, out, e)
	}
	code, out, _ = run(t, fs.client(), sandboxRW, "documents", "--json")
	if code != 0 || strings.Contains(out, "Synthetic") {
		t.Fatalf("medical document must be hidden: %s", out)
	}
}
