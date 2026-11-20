package paperless

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Bam679/paperless-cli/internal/agentcli"
)

// Limits for what the CLI accepts from the server.
const (
	apiVersion    = "9"
	maxJSON       = 32 << 20 // one JSON response
	maxErrBody    = 4 << 10  // error excerpt read from a failed response
	maxDownload   = 1 << 30  // one downloaded file
	maxLookupPage = 200      // pages of tags/correspondents/types
	lookupSize    = 1000
)

// HTTP is the production Client over the Paperless-ngx REST API.
type HTTP struct {
	load func(context.Context) (Config, error)
	hc   *http.Client

	mu  sync.Mutex
	cfg *Config
}

// NewHTTP returns the production client. Configuration is resolved lazily on
// the first request, so schema/help work without a server.
func NewHTTP() *HTTP { return &HTTP{load: LoadConfig, hc: newHTTPClient()} }

// NewHTTPWithConfig returns a client for an explicit configuration.
func NewHTTPWithConfig(cfg Config) *HTTP {
	return &HTTP{load: func(context.Context) (Config, error) { return cfg, nil }, hc: newHTTPClient()}
}

var errRedirect = errors.New("refusing redirect to another host or scheme")

func newHTTPClient() *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          4,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   10 * time.Minute, // hard ceiling; --timeout is the usual limit
		// The Authorization header must never reach another host, so any
		// cross-host or cross-scheme redirect is refused outright.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host {
				return errRedirect
			}
			return nil
		},
	}
}

func (h *HTTP) config(ctx context.Context) (Config, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg != nil {
		return *h.cfg, nil
	}
	cfg, err := h.load(ctx)
	if err != nil {
		return Config{}, err
	}
	h.cfg = &cfg
	return cfg, nil
}

// do sends one request. Errors never contain the token.
func (h *HTTP) do(ctx context.Context, method, path string, q url.Values, body io.Reader, ctype string) (*http.Response, error) {
	cfg, err := h.config(ctx)
	if err != nil {
		return nil, err
	}
	u := cfg.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, agentcli.Internalf("building request: %s", redact(err.Error(), cfg.Token))
	}
	req.Header.Set("Authorization", "Token "+cfg.Token)
	req.Header.Set("Accept", "application/json; version="+apiVersion)
	req.Header.Set("User-Agent", "paperless-cli")
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if sb, ok := body.(sized); ok {
		req.ContentLength = sb.n
	}
	resp, err := h.hc.Do(req) // #nosec G704 -- URL is the user's validated PAPERLESS_URL plus fixed paths
	if err != nil {
		if errors.Is(err, errRedirect) {
			return nil, agentcli.Unavailablef("server redirected to another host or scheme; check PAPERLESS_URL")
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, agentcli.Unavailablef("cannot reach %s: %s", cfg.BaseURL, redact(netReason(err), cfg.Token))
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
		return nil, statusErr(resp.StatusCode, redact(agentcli.Sanitize(string(excerpt)), cfg.Token))
	}
	return resp, nil
}

func netReason(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

func statusErr(code int, excerpt string) error {
	if len(excerpt) > 300 {
		excerpt = excerpt[:300] + "…"
	}
	detail := fmt.Sprintf("HTTP %d", code)
	if excerpt != "" && !strings.HasPrefix(excerpt, "<") {
		detail += ": " + excerpt
	}
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return &agentcli.Error{Code: "permission_denied", Exit: agentcli.ExitPermission,
			Message: "authentication or permission failed (" + detail + ")",
			Hint:    "check PAPERLESS_TOKEN or the Keychain item and the Paperless user's permissions; run paperless doctor"}
	case code == http.StatusNotFound:
		return agentcli.NotFoundf("not found (%s)", detail)
	case code == http.StatusConflict:
		return agentcli.Conflictf("%s", detail)
	case code == http.StatusRequestEntityTooLarge:
		return agentcli.Usagef("file too large for the server (%s)", detail)
	case code >= 500 || code == http.StatusTooManyRequests:
		return agentcli.Unavailablef("server error (%s)", detail)
	default:
		return agentcli.Usagef("server rejected the request (%s)", detail)
	}
}

// getJSON GETs path and decodes the JSON body into out.
func (h *HTTP) getJSON(ctx context.Context, path string, q url.Values, out any) (http.Header, error) {
	resp, err := h.do(ctx, http.MethodGet, path, q, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return resp.Header, decodeBody(resp.Body, out)
}

func (h *HTTP) sendJSON(ctx context.Context, method, path string, in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return agentcli.Internalf("encoding request: %v", err)
	}
	resp, err := h.do(ctx, method, path, nil, bytes.NewReader(b), "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrBody))
		return nil
	}
	return decodeBody(resp.Body, out)
}

func decodeBody(r io.Reader, out any) error {
	data, err := io.ReadAll(io.LimitReader(r, maxJSON+1))
	if err != nil {
		return agentcli.Unavailablef("reading response: %v", err)
	}
	if len(data) > maxJSON {
		return agentcli.Unavailablef("response larger than %d MiB", maxJSON>>20)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return agentcli.Unavailablef("unexpected response from server (not Paperless-ngx JSON?)")
	}
	return nil
}

type wireHit struct {
	Score      float64 `json:"score"`
	Rank       int     `json:"rank"`
	Highlights string  `json:"highlights"`
}

type wireDoc struct {
	ID               int      `json:"id"`
	Title            string   `json:"title"`
	Content          string   `json:"content"`
	Created          string   `json:"created"`
	Added            string   `json:"added"`
	Modified         string   `json:"modified"`
	Correspondent    *int     `json:"correspondent"`
	DocumentType     *int     `json:"document_type"`
	Tags             []int    `json:"tags"`
	ASN              *int     `json:"archive_serial_number"`
	OriginalFileName string   `json:"original_file_name"`
	MimeType         string   `json:"mime_type"`
	PageCount        *int     `json:"page_count"`
	Hit              *wireHit `json:"__search_hit__"`
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func parseStamp(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.Local()
	}
	return time.Time{}
}

// parseDate keeps the calendar date the server reports, whatever its offset.
func parseDate(s string) time.Time {
	if len(s) >= 10 {
		if t, err := time.ParseInLocation("2006-01-02", s[:10], time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (w wireDoc) doc() Document {
	d := Document{
		ID: w.ID, Title: w.Title, Content: w.Content, Created: parseDate(w.Created),
		Added: parseStamp(w.Added), Modified: parseStamp(w.Modified),
		Correspondent: deref(w.Correspondent), DocumentType: deref(w.DocumentType), Tags: w.Tags,
		ASN: deref(w.ASN), OriginalFileName: w.OriginalFileName, MimeType: w.MimeType, PageCount: deref(w.PageCount),
	}
	if w.Hit != nil {
		d.Search = &SearchHit{Score: w.Hit.Score, Rank: w.Hit.Rank, Highlights: w.Hit.Highlights}
	}
	if d.Tags == nil {
		d.Tags = []int{}
	}
	return d
}

const listFields = "id,title,created,added,modified,correspondent,document_type,tags,archive_serial_number,original_file_name,mime_type,page_count"

func idList(ids []int) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.Itoa(id)
	}
	return strings.Join(s, ",")
}

// Info implements Client.
func (h *HTTP) Info(ctx context.Context) (ServerInfo, error) {
	cfg, err := h.config(ctx)
	if err != nil {
		return ServerInfo{}, err
	}
	var page struct {
		Count int `json:"count"`
	}
	hdr, err := h.getJSON(ctx, "/api/tags/", url.Values{"page_size": {"1"}}, &page)
	if err != nil {
		return ServerInfo{}, err
	}
	return ServerInfo{BaseURL: cfg.BaseURL, TokenSource: cfg.TokenSource,
		Version: agentcli.Sanitize(hdr.Get("X-Version")), APIVersion: agentcli.Sanitize(hdr.Get("X-Api-Version"))}, nil
}

// Documents implements Client.
func (h *HTTP) Documents(ctx context.Context, q Query, page, pageSize int) (DocPage, error) {
	v := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(pageSize)}, "fields": {listFields}}
	if q.FullText != "" {
		v.Set("query", q.FullText)
		v.Set("fields", listFields+",__search_hit__")
	} else {
		v.Set("ordering", "-created,-id")
	}
	if q.TitleContent != "" {
		v.Set("title_content", q.TitleContent)
	}
	if len(q.TagsAll) > 0 {
		v.Set("tags__id__all", idList(q.TagsAll))
	}
	if q.Correspondent > 0 {
		v.Set("correspondent__id", strconv.Itoa(q.Correspondent))
	}
	if q.DocumentType > 0 {
		v.Set("document_type__id", strconv.Itoa(q.DocumentType))
	}
	if q.CreatedFrom != "" {
		v.Set("created__gte", q.CreatedFrom)
	}
	if q.CreatedBefore != "" {
		v.Set("created__lt", q.CreatedBefore)
	}
	var resp struct {
		Count   int       `json:"count"`
		Results []wireDoc `json:"results"`
	}
	if _, err := h.getJSON(ctx, "/api/documents/", v, &resp); err != nil {
		var e *agentcli.Error
		// Paperless answers 404 for a page past the end.
		if errors.As(err, &e) && e.Exit == agentcli.ExitNotFound && page > 1 {
			return DocPage{Count: resp.Count}, nil
		}
		return DocPage{}, err
	}
	out := DocPage{Count: resp.Count, Docs: make([]Document, 0, len(resp.Results))}
	for _, w := range resp.Results {
		out.Docs = append(out.Docs, w.doc())
	}
	return out, nil
}

func docPath(id int, suffix string) string {
	return "/api/documents/" + strconv.Itoa(id) + "/" + suffix
}

func notFoundDoc(err error, id int) error {
	var e *agentcli.Error
	if errors.As(err, &e) && e.Exit == agentcli.ExitNotFound {
		return agentcli.NotFoundf("no document with id %d", id)
	}
	return err
}

// Document implements Client.
func (h *HTTP) Document(ctx context.Context, id int, content bool) (Document, error) {
	fields := listFields
	if content {
		fields += ",content"
	}
	var w wireDoc
	if _, err := h.getJSON(ctx, docPath(id, ""), url.Values{"fields": {fields}}, &w); err != nil {
		return Document{}, notFoundDoc(err, id)
	}
	return w.doc(), nil
}

// Download implements Client.
func (h *HTTP) Download(ctx context.Context, id int, original bool, w io.Writer) (string, string, int64, error) {
	var q url.Values
	if original {
		q = url.Values{"original": {"true"}}
	}
	resp, err := h.do(ctx, http.MethodGet, docPath(id, "download/"), q, nil, "")
	if err != nil {
		return "", "", 0, notFoundDoc(err, id)
	}
	defer resp.Body.Close()
	n, err := io.Copy(w, io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		if ctx.Err() != nil {
			return "", "", n, ctx.Err()
		}
		return "", "", n, agentcli.Unavailablef("download interrupted: %v", err)
	}
	if n > maxDownload {
		return "", "", n, agentcli.Unavailablef("file larger than %d MiB", maxDownload>>20)
	}
	name := ""
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		name = filepath.Base(filepath.Clean("/" + params["filename"]))
		if name == "/" || name == "." {
			name = ""
		}
	}
	ctype, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return agentcli.Sanitize(name), ctype, n, nil
}

type wirePage[T any] struct {
	Next    *string `json:"next"`
	Results []T     `json:"results"`
}

func listAll[T any](ctx context.Context, h *HTTP, path string) ([]T, error) {
	var out []T
	for page := 1; page <= maxLookupPage; page++ {
		var p wirePage[T]
		v := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(lookupSize)}}
		if _, err := h.getJSON(ctx, path, v, &p); err != nil {
			return nil, err
		}
		out = append(out, p.Results...)
		if p.Next == nil || len(p.Results) == 0 {
			return out, nil
		}
	}
	return out, nil
}

type wireTag struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	DocumentCount int    `json:"document_count"`
	IsInbox       bool   `json:"is_inbox_tag"`
}

func (w wireTag) tag() Tag {
	return Tag(w)
}

type wireNamed struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	DocumentCount int    `json:"document_count"`
}

// Tags implements Client.
func (h *HTTP) Tags(ctx context.Context) ([]Tag, error) {
	ws, err := listAll[wireTag](ctx, h, "/api/tags/")
	if err != nil {
		return nil, err
	}
	out := make([]Tag, len(ws))
	for i, w := range ws {
		out[i] = w.tag()
	}
	return out, nil
}

func (h *HTTP) named(ctx context.Context, path string) ([]Named, error) {
	ws, err := listAll[wireNamed](ctx, h, path)
	if err != nil {
		return nil, err
	}
	out := make([]Named, len(ws))
	for i, w := range ws {
		out[i] = Named(w)
	}
	return out, nil
}

// Correspondents implements Client.
func (h *HTTP) Correspondents(ctx context.Context) ([]Named, error) {
	return h.named(ctx, "/api/correspondents/")
}

// DocumentTypes implements Client.
func (h *HTTP) DocumentTypes(ctx context.Context) ([]Named, error) {
	return h.named(ctx, "/api/document_types/")
}

// Upload implements Client. The file is streamed, not buffered, but with a
// Content-Length: Paperless ignores chunked multipart bodies.
func (h *HTTP) Upload(ctx context.Context, u Upload) (string, error) {
	var head, tail bytes.Buffer
	sw := &switchWriter{w: &head}
	mw := multipart.NewWriter(sw)
	fields := [][2]string{}
	if u.Title != "" {
		fields = append(fields, [2]string{"title", u.Title})
	}
	if u.Correspondent > 0 {
		fields = append(fields, [2]string{"correspondent", strconv.Itoa(u.Correspondent)})
	}
	if u.DocumentType > 0 {
		fields = append(fields, [2]string{"document_type", strconv.Itoa(u.DocumentType)})
	}
	for _, t := range u.Tags {
		fields = append(fields, [2]string{"tags", strconv.Itoa(t)})
	}
	for _, f := range fields {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			return "", agentcli.Internalf("building upload: %v", err)
		}
	}
	if _, err := mw.CreateFormFile("document", u.FileName); err != nil {
		return "", agentcli.Internalf("building upload: %v", err)
	}
	sw.w = &tail // the file bytes go between the part header and the closing boundary
	if err := mw.Close(); err != nil {
		return "", agentcli.Internalf("building upload: %v", err)
	}
	body := sized{Reader: io.MultiReader(&head, io.LimitReader(u.File, u.Size), &tail),
		n: int64(head.Len()) + u.Size + int64(tail.Len())}
	resp, err := h.do(ctx, http.MethodPost, "/api/documents/post_document/", nil, body, mw.FormDataContentType())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var task string
	if err := decodeBody(resp.Body, &task); err != nil {
		return "", err
	}
	return task, nil
}

type switchWriter struct{ w io.Writer }

func (s *switchWriter) Write(p []byte) (int, error) { return s.w.Write(p) }

// sized is a request body with a known length.
type sized struct {
	io.Reader
	n int64
}

type wireTask struct {
	TaskID          string `json:"task_id"`
	Status          string `json:"status"`
	Result          string `json:"result"`
	RelatedDocument any    `json:"related_document"`
}

// Task implements Client.
func (h *HTTP) Task(ctx context.Context, id string) (Task, error) {
	var ws []wireTask
	if _, err := h.getJSON(ctx, "/api/tasks/", url.Values{"task_id": {id}}, &ws); err != nil {
		return Task{}, err
	}
	if len(ws) == 0 {
		return Task{}, agentcli.NotFoundf("no task %s", id)
	}
	w := ws[0]
	t := Task{ID: w.TaskID, Status: w.Status, Result: w.Result}
	switch v := w.RelatedDocument.(type) {
	case float64:
		t.Document = int(v)
	case string:
		t.Document, _ = strconv.Atoi(v)
	}
	return t, nil
}

// Update implements Client.
func (h *HTTP) Update(ctx context.Context, id int, p Patch) (Document, error) {
	body := map[string]any{}
	if p.Title != nil {
		body["title"] = *p.Title
	}
	if p.Tags != nil {
		body["tags"] = *p.Tags
	}
	if p.Correspondent != nil {
		body["correspondent"] = *p.Correspondent
	}
	if p.DocumentType != nil {
		body["document_type"] = *p.DocumentType
	}
	var w wireDoc
	if err := h.sendJSON(ctx, http.MethodPatch, docPath(id, ""), body, &w); err != nil {
		return Document{}, notFoundDoc(err, id)
	}
	return w.doc(), nil
}

// Delete implements Client.
func (h *HTTP) Delete(ctx context.Context, id int) error {
	resp, err := h.do(ctx, http.MethodDelete, docPath(id, ""), nil, nil, "")
	if err != nil {
		return notFoundDoc(err, id)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrBody))
	return resp.Body.Close()
}

// CreateTag implements Client.
func (h *HTTP) CreateTag(ctx context.Context, name string) (Tag, error) {
	var w wireTag
	if err := h.sendJSON(ctx, http.MethodPost, "/api/tags/", map[string]any{"name": name}, &w); err != nil {
		return Tag{}, err
	}
	return w.tag(), nil
}
