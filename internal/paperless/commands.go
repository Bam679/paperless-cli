package paperless

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Bam679/paperless-cli/internal/agentcli"
)

var (
	docFields = []string{"id", "title", "created", "added", "modified", "correspondent", "correspondent_id",
		"document_type", "document_type_id", "tags", "archive_serial_number", "original_file_name", "mime_type", "page_count"}
	docCols      = []string{"id", "created", "title", "correspondent", "tags"}
	getFields    = append(append([]string{}, docFields...), "content")
	searchFields = append(append([]string{}, docFields...), "score", "rank", "highlights")
	searchCols   = []string{"id", "created", "title", "highlights"}
	tagFields    = []string{"id", "name", "document_count", "is_inbox"}
	namedFields  = []string{"id", "name", "document_count"}
	uploadFields = []string{"task_id", "status", "id", "title", "file", "bytes", "tags", "correspondent", "document_type"}
	uploadCols   = []string{"task_id", "status", "id", "title", "file"}
	dlFields     = []string{"id", "title", "path", "bytes", "mime_type", "file_name", "original"}
)

// Paging of the server's document list.
const (
	maxPageSize  = 100
	maxScanPages = 10 // per call; read-scope filtering may skip many documents
	maxUpload    = 1 << 30
	maxTagName   = 128
)

// NewApp wires the CLI commands to client cl.
func NewApp(cl Client, version string) *agentcli.App {
	h := &handlers{cl: cl}
	S, Str, B := agentcli.Strings, agentcli.String, agentcli.Bool
	corr := agentcli.Flag{Name: "correspondent", Type: Str, Usage: "correspondent name or id"}
	typ := agentcli.Flag{Name: "type", Type: Str, Usage: "document type name or id"}
	idArg := agentcli.Arg{Name: "id", Usage: "document id", Required: true}
	return &agentcli.App{
		Name:         "paperless",
		Version:      version,
		Summary:      "Paperless-ngx for AI agents (read-only unless the policy allows writes)",
		ScopeHelp:    "document tag as \"tag:<name>\", or \"untagged\" for documents without tags",
		ScopeExample: "tag:agent-*",
		Commands: []*agentcli.Command{
			{Name: "documents", Summary: "list documents, newest first", Kind: agentcli.Read,
				Flags: []agentcli.Flag{
					{Name: "query", Type: Str, Usage: "case-insensitive text in title or content"},
					{Name: "tag", Type: S, Usage: "only documents with this tag (repeatable: all must match)"},
					corr, typ,
					{Name: "created-after", Type: Str, Usage: "created on or after this date"},
					{Name: "created-before", Type: Str, Usage: "created before this date"}},
				Fields: docCols, AllFields: docFields, Run: h.documents,
				Example: "paperless documents --tag invoices --created-after 2026-01-01 --json"},
			{Name: "search", Summary: "full-text search, best match first", Kind: agentcli.Read,
				Args:   []agentcli.Arg{{Name: "text", Usage: "search query (Paperless full-text syntax)", Required: true}},
				Fields: searchCols, AllFields: searchFields, Run: h.search,
				Example: "paperless search 'insurance 2026' --limit 10"},
			{Name: "get", Summary: "show one document (OCR text only with --content)", Kind: agentcli.Read,
				Args:   []agentcli.Arg{idArg},
				Flags:  []agentcli.Flag{{Name: "content", Type: B, Usage: "include the OCR text (cap it with --max-chars)"}},
				Fields: getFields, AllFields: getFields, Run: h.get,
				Example: "paperless get 42 --content --max-chars 4000"},
			{Name: "download", Summary: "save a document file (archived PDF unless --original)", Kind: agentcli.Read, Op: "download",
				Args: []agentcli.Arg{idArg},
				Flags: []agentcli.Flag{
					{Name: "out", Type: Str, Usage: "file to create; never overwritten, mode 0600", Required: true},
					{Name: "original", Type: B, Usage: "the original upload instead of the archived version"}},
				Fields: []string{"id", "path", "bytes", "mime_type"}, AllFields: dlFields, Run: h.download,
				Example: "paperless download 42 --out ./invoice-42.pdf"},
			{Name: "tags", Summary: "list tags", Kind: agentcli.Read,
				Fields: tagFields, AllFields: tagFields, Run: h.tags},
			{Name: "correspondents", Summary: "list correspondents", Kind: agentcli.Read,
				Fields: namedFields, AllFields: namedFields, Run: h.correspondents},
			{Name: "document-types", Summary: "list document types", Kind: agentcli.Read,
				Fields: namedFields, AllFields: namedFields, Run: h.documentTypes},
			{Name: "doctor", Summary: "check configuration, token and server", Kind: agentcli.Read,
				Fields: []string{"check", "ok", "detail"}, Run: h.doctor},
			{Name: "upload", Summary: "upload a file for consumption", Kind: agentcli.Write, Op: "upload",
				Args: []agentcli.Arg{{Name: "file", Usage: "path of the file to upload", Required: true}},
				Flags: []agentcli.Flag{
					{Name: "title", Type: Str, Usage: "document title (default: from the file name)"},
					{Name: "tag", Type: S, Usage: "tag name or id (repeatable)"}, corr, typ,
					{Name: "wait", Type: B, Usage: "wait until consumed and return the document id"}},
				Fields: uploadCols, AllFields: uploadFields, Plan: h.upload,
				Example: "paperless upload ./scan.pdf --tag agent-inbox --wait --dry-run"},
			{Name: "update", Summary: "change a document's title, tags, correspondent or type", Kind: agentcli.Write, Op: "update",
				Args: []agentcli.Arg{idArg},
				Flags: []agentcli.Flag{
					{Name: "title", Type: Str, Usage: "new title"},
					{Name: "add-tag", Type: S, Usage: "tag name or id to add (repeatable)"},
					{Name: "remove-tag", Type: S, Usage: "tag name or id to remove (repeatable)"}, corr, typ},
				Fields: docCols, AllFields: docFields, Plan: h.update,
				Example: "paperless update 42 --add-tag agent-reviewed --confirm"},
			{Name: "delete", Summary: "delete documents (Paperless moves them to the trash)", Kind: agentcli.Write, Op: "delete",
				Args:   []agentcli.Arg{{Name: "id", Usage: "document id(s)", Required: true, Variadic: true}},
				Fields: docCols, AllFields: append(append([]string{}, docFields...), "deleted"), Plan: h.delete},
			{Name: "tag-create", Summary: "create a tag", Kind: agentcli.Write, Op: "tag-create",
				Args:   []agentcli.Arg{{Name: "name", Usage: "new tag name", Required: true}},
				Fields: tagFields, AllFields: tagFields, Plan: h.tagCreate},
		},
	}
}

type handlers struct{ cl Client }

// session caches lookups for one command invocation.
type session struct {
	cl    Client
	c     *agentcli.Ctx
	tags  []Tag
	corr  []Named
	types []Named
}

func (h *handlers) session(c *agentcli.Ctx) *session { return &session{cl: h.cl, c: c} }

func (s *session) loadTags() ([]Tag, error) {
	if s.tags == nil {
		ts, err := s.cl.Tags(s.c)
		if err != nil {
			return nil, err
		}
		s.tags = append([]Tag{}, ts...)
	}
	return s.tags, nil
}

func (s *session) loadNamed(types bool) ([]Named, error) {
	dst, fetch := &s.corr, s.cl.Correspondents
	if types {
		dst, fetch = &s.types, s.cl.DocumentTypes
	}
	if *dst == nil {
		ns, err := fetch(s.c)
		if err != nil {
			return nil, err
		}
		*dst = append([]Named{}, ns...)
	}
	return *dst, nil
}

func (s *session) tagName(id int) string {
	for _, t := range s.tags {
		if t.ID == id {
			return t.Name
		}
	}
	return "#" + strconv.Itoa(id) // not visible to this API user
}

func nameOf(ns []Named, id int) string {
	if id == 0 {
		return ""
	}
	for _, n := range ns {
		if n.ID == id {
			return n.Name
		}
	}
	return "#" + strconv.Itoa(id)
}

// resolve finds an object by id, exact name, or unique case-insensitive name.
func resolve[T any](kind, ref string, all []T, id func(T) int, name func(T) string) (T, error) {
	var zero T
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return zero, agentcli.Usagef("empty %s name", kind)
	}
	if n, err := strconv.Atoi(ref); err == nil {
		for _, x := range all {
			if id(x) == n {
				return x, nil
			}
		}
	}
	var fold []T
	for _, x := range all {
		if name(x) == ref {
			return x, nil
		}
		if strings.EqualFold(name(x), ref) {
			fold = append(fold, x)
		}
	}
	switch len(fold) {
	case 0:
		return zero, agentcli.NotFoundf("no %s %q", kind, ref)
	case 1:
		return fold[0], nil
	}
	return zero, agentcli.Conflictf("%d %ss match %q; pass the id instead", len(fold), kind, ref)
}

func (s *session) tag(ref string) (Tag, error) {
	ts, err := s.loadTags()
	if err != nil {
		return Tag{}, err
	}
	return resolve("tag", ref, ts, func(t Tag) int { return t.ID }, func(t Tag) string { return t.Name })
}

func (s *session) named(types bool, ref string) (Named, error) {
	ns, err := s.loadNamed(types)
	if err != nil {
		return Named{}, err
	}
	kind := "correspondent"
	if types {
		kind = "document type"
	}
	return resolve(kind, ref, ns, func(n Named) int { return n.ID }, func(n Named) string { return n.Name })
}

// prepare loads every lookup needed to render documents.
func (s *session) prepare() error {
	if _, err := s.loadTags(); err != nil {
		return err
	}
	if _, err := s.loadNamed(false); err != nil {
		return err
	}
	_, err := s.loadNamed(true)
	return err
}

// tagScope is the policy scope string of a tag.
func tagScope(name string) string { return "tag:" + name }

// scopes returns a document's policy scopes: one per tag, or "untagged".
func (s *session) scopes(d Document) []string {
	if len(d.Tags) == 0 {
		return []string{"untagged"}
	}
	out := make([]string, 0, len(d.Tags))
	for _, id := range d.Tags {
		out = append(out, tagScope(s.tagName(id)))
	}
	return out
}

// visible reports whether every scope of d is readable.
func (s *session) visible(d Document) bool {
	for _, sc := range s.scopes(d) {
		if !s.c.CanRead(sc) {
			return false
		}
	}
	return true
}

func (s *session) checkVisible(d Document) error {
	for _, sc := range s.scopes(d) {
		if err := s.c.CheckRead(sc); err != nil {
			return err
		}
	}
	return nil
}

var markup = regexp.MustCompile(`<[^>]*>`)

func (s *session) docItem(d Document) agentcli.Item {
	tags := make([]string, 0, len(d.Tags))
	for _, id := range d.Tags {
		tags = append(tags, s.tagName(id))
	}
	it := agentcli.Item{
		"id": d.ID, "title": d.Title, "created": agentcli.FormatTime(d.Created, true),
		"added": agentcli.FormatTime(d.Added, false), "modified": agentcli.FormatTime(d.Modified, false),
		"correspondent": nameOf(s.corr, d.Correspondent), "document_type": nameOf(s.types, d.DocumentType),
		"correspondent_id": optInt(d.Correspondent), "document_type_id": optInt(d.DocumentType),
		"tags": tags, "archive_serial_number": optInt(d.ASN), "original_file_name": d.OriginalFileName,
		"mime_type": d.MimeType, "page_count": optInt(d.PageCount),
	}
	if d.Search != nil {
		it["score"] = d.Search.Score
		it["rank"] = d.Search.Rank
		it["highlights"] = html.UnescapeString(markup.ReplaceAllString(d.Search.Highlights, ""))
	}
	return it
}

func optInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func parseID(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0, agentcli.Usagef("invalid document id %q (expected a positive number)", s)
	}
	return n, nil
}

// page fetches server pages starting at the cursor offset until --limit
// visible documents are collected. The cursor is the raw offset into the
// server's result list, so documents hidden by read scopes are skipped
// without breaking pagination.
func (s *session) page(q Query, keyParts ...string) error {
	c := s.c
	if err := s.prepare(); err != nil {
		return err
	}
	key := agentcli.QueryKey(append([]string{c.Cmd.Name, strconv.Itoa(c.Limit)}, keyParts...)...)
	raw, err := agentcli.DecodeCursor(c.Cursor, key)
	if err != nil {
		return err
	}
	size := c.Limit
	if size > maxPageSize {
		size = maxPageSize
	}
	if size < 25 {
		size = 25
	}
	var items []agentcli.Item
	total := 0
	for scanned := 0; len(items) < c.Limit && scanned < maxScanPages; scanned++ {
		dp, err := s.cl.Documents(c, q, raw/size+1, size)
		if err != nil {
			return err
		}
		total = dp.Count
		skip := raw % size
		if skip >= len(dp.Docs) {
			break
		}
		for _, d := range dp.Docs[skip:] {
			if len(items) == c.Limit {
				break
			}
			raw++
			if s.visible(d) {
				items = append(items, s.docItem(d))
			}
		}
		if len(dp.Docs) < size || raw >= total {
			break
		}
	}
	p := agentcli.Page{Items: items}
	if raw < total {
		p.NextCursor = agentcli.EncodeCursor(raw, key)
	}
	return c.Out.List(p, c.Cmd.Fields)
}

func (s *session) date(flag string) (string, error) {
	v := s.c.String(flag)
	if v == "" {
		return "", nil
	}
	t, _, err := agentcli.ParseTime(v, s.c.Now(), time.Local)
	if err != nil {
		return "", err
	}
	return t.Format("2006-01-02"), nil
}

func (h *handlers) documents(c *agentcli.Ctx) error {
	s := h.session(c)
	q := Query{TitleContent: strings.TrimSpace(c.String("query"))}
	var err error
	if q.CreatedFrom, err = s.date("created-after"); err != nil {
		return err
	}
	if q.CreatedBefore, err = s.date("created-before"); err != nil {
		return err
	}
	var tagKey []string
	for _, ref := range c.Strings("tag") {
		t, err := s.tag(ref)
		if err != nil {
			return err
		}
		if err := c.CheckRead(tagScope(t.Name)); err != nil {
			return err
		}
		q.TagsAll = append(q.TagsAll, t.ID)
		tagKey = append(tagKey, strconv.Itoa(t.ID))
	}
	if ref := c.String("correspondent"); c.IsSet("correspondent") {
		n, err := s.named(false, ref)
		if err != nil {
			return err
		}
		q.Correspondent = n.ID
	}
	if ref := c.String("type"); c.IsSet("type") {
		n, err := s.named(true, ref)
		if err != nil {
			return err
		}
		q.DocumentType = n.ID
	}
	return s.page(q, q.TitleContent, strings.Join(tagKey, ","), strconv.Itoa(q.Correspondent),
		strconv.Itoa(q.DocumentType), q.CreatedFrom, q.CreatedBefore)
}

func (h *handlers) search(c *agentcli.Ctx) error {
	text := strings.TrimSpace(c.Args[0])
	if text == "" {
		return agentcli.Usagef("search text must not be empty")
	}
	return h.session(c).page(Query{FullText: text}, text)
}

func (s *session) document(id int, content bool) (Document, error) {
	if err := s.prepare(); err != nil {
		return Document{}, err
	}
	d, err := s.cl.Document(s.c, id, content)
	if err != nil {
		return Document{}, err
	}
	return d, s.checkVisible(d)
}

func (h *handlers) get(c *agentcli.Ctx) error {
	id, err := parseID(c.Args[0])
	if err != nil {
		return err
	}
	s := h.session(c)
	content := c.Bool("content")
	d, err := s.document(id, content)
	if err != nil {
		return err
	}
	it := s.docItem(d)
	if content {
		it["content"] = d.Content
	}
	return c.Show(it)
}

func (h *handlers) download(c *agentcli.Ctx) error {
	id, err := parseID(c.Args[0])
	if err != nil {
		return err
	}
	out := c.String("out")
	if out == "" || out == "-" {
		return agentcli.Usagef("--out must be a file path (stdout stays machine-readable)")
	}
	out = filepath.Clean(out)
	if _, err := os.Lstat(out); err == nil {
		return agentcli.Conflictf("%s already exists; refusing to overwrite", out)
	}
	s := h.session(c)
	d, err := s.document(id, false)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- user-chosen output path, created exclusively
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return agentcli.Conflictf("%s already exists; refusing to overwrite", out)
		}
		return agentcli.Usagef("cannot create %s: %v", out, errors.Unwrap(err))
	}
	name, mime, n, err := s.cl.Download(c, id, c.Bool("original"), f)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = agentcli.Internalf("writing %s: %v", out, cerr)
	}
	if err != nil {
		_ = os.Remove(out)
		return err
	}
	return c.Show(agentcli.Item{"id": d.ID, "title": d.Title, "path": out, "bytes": n, "mime_type": mime,
		"file_name": name, "original": c.Bool("original")})
}

func (h *handlers) tags(c *agentcli.Ctx) error {
	ts, err := h.session(c).loadTags()
	if err != nil {
		return err
	}
	sort.Slice(ts, func(i, j int) bool {
		return ts[i].Name < ts[j].Name || (ts[i].Name == ts[j].Name && ts[i].ID < ts[j].ID)
	})
	var items []agentcli.Item
	for _, t := range ts {
		if c.CanRead(tagScope(t.Name)) {
			items = append(items, tagItem(t))
		}
	}
	return c.List(items)
}

func tagItem(t Tag) agentcli.Item {
	return agentcli.Item{"id": t.ID, "name": t.Name, "document_count": t.DocumentCount, "is_inbox": t.IsInbox}
}

func (h *handlers) listNamed(c *agentcli.Ctx, types bool) error {
	ns, err := h.session(c).loadNamed(types)
	if err != nil {
		return err
	}
	sort.Slice(ns, func(i, j int) bool {
		return ns[i].Name < ns[j].Name || (ns[i].Name == ns[j].Name && ns[i].ID < ns[j].ID)
	})
	items := make([]agentcli.Item, 0, len(ns))
	for _, n := range ns {
		items = append(items, agentcli.Item{"id": n.ID, "name": n.Name, "document_count": n.DocumentCount})
	}
	return c.List(items)
}

func (h *handlers) correspondents(c *agentcli.Ctx) error { return h.listNamed(c, false) }
func (h *handlers) documentTypes(c *agentcli.Ctx) error  { return h.listNamed(c, true) }

func (h *handlers) doctor(c *agentcli.Ctx) error {
	info, err := h.cl.Info(c)
	row := agentcli.Item{"check": "server", "ok": err == nil}
	if err != nil {
		row["detail"] = agentcli.AsError(err).Message
	} else {
		ver := info.Version
		if ver == "" {
			ver = "unknown"
		}
		row["detail"] = fmt.Sprintf("%s reachable, Paperless-ngx %s, token from %s", info.BaseURL, ver, info.TokenSource)
	}
	if lerr := c.List([]agentcli.Item{row}); lerr != nil {
		return lerr
	}
	return err
}

func (h *handlers) upload(c *agentcli.Ctx) (*agentcli.WritePlan, error) {
	path := filepath.Clean(c.Args[0])
	st, err := os.Stat(path)
	if err != nil {
		return nil, agentcli.NotFoundf("cannot read %s", path)
	}
	if !st.Mode().IsRegular() {
		return nil, agentcli.Usagef("%s is not a regular file", path)
	}
	if st.Size() == 0 || st.Size() > maxUpload {
		return nil, agentcli.Usagef("%s must be between 1 byte and %d MiB", path, maxUpload>>20)
	}
	s := h.session(c)
	u := Upload{FileName: filepath.Base(path), Title: strings.TrimSpace(c.String("title"))}
	if c.IsSet("title") && u.Title == "" {
		return nil, agentcli.Usagef("title must not be empty")
	}
	target := agentcli.Item{"file": u.FileName, "bytes": st.Size(), "title": u.Title}
	var scopes, names []string
	for _, ref := range c.Strings("tag") {
		t, err := s.tag(ref)
		if err != nil {
			return nil, err
		}
		u.Tags = append(u.Tags, t.ID)
		names = append(names, t.Name)
		scopes = append(scopes, tagScope(t.Name))
	}
	if len(scopes) == 0 {
		scopes = []string{"untagged"}
	}
	target["tags"] = names
	if c.IsSet("correspondent") {
		n, err := s.named(false, c.String("correspondent"))
		if err != nil {
			return nil, err
		}
		u.Correspondent, target["correspondent"] = n.ID, n.Name
	}
	if c.IsSet("type") {
		n, err := s.named(true, c.String("type"))
		if err != nil {
			return nil, err
		}
		u.DocumentType, target["document_type"] = n.ID, n.Name
	}
	wait := c.Bool("wait")
	return &agentcli.WritePlan{Scopes: scopes, Targets: []agentcli.Item{target},
		Apply: func(ctx context.Context) ([]agentcli.Item, error) {
			f, err := os.Open(path) // #nosec G304 -- the file the user asked to upload
			if err != nil {
				return nil, agentcli.NotFoundf("cannot read %s", path)
			}
			defer f.Close()
			fst, err := f.Stat()
			if err != nil || fst.Size() == 0 || fst.Size() > maxUpload {
				return nil, agentcli.Usagef("%s changed or became unreadable", path)
			}
			u.File, u.Size = f, fst.Size()
			task, err := s.cl.Upload(ctx, u)
			if err != nil {
				return nil, err
			}
			it := agentcli.Item{"task_id": task, "status": "queued", "file": u.FileName, "title": u.Title}
			if !wait {
				return []agentcli.Item{it}, nil
			}
			t, err := waitTask(ctx, s.cl, task)
			if err != nil {
				return nil, err
			}
			it["status"], it["id"] = "consumed", t.Document
			if d, err := s.cl.Document(ctx, t.Document, false); err == nil {
				it["title"] = d.Title
			}
			return []agentcli.Item{it}, nil
		}}, nil
}

// pollEvery is the task polling interval; shortened in tests.
var pollEvery = time.Second

// waitTask polls a consumption task until it finishes or ctx expires.
func waitTask(ctx context.Context, cl Client, id string) (Task, error) {
	for {
		t, err := cl.Task(ctx, id)
		if err != nil && agentcli.AsError(err).Exit != agentcli.ExitNotFound {
			return Task{}, err
		}
		switch {
		case err != nil:
		case t.Status == "SUCCESS" && t.Document > 0:
			return t, nil
		case t.Status == "FAILURE" || t.Status == "REVOKED":
			msg := agentcli.Sanitize(t.Result)
			if strings.Contains(strings.ToLower(msg), "duplicate") {
				return Task{}, agentcli.Conflictf("consumption failed: %s", msg)
			}
			return Task{}, agentcli.Unavailablef("consumption failed: %s", msg)
		}
		select {
		case <-ctx.Done():
			return Task{}, agentcli.Unavailablef("timed out waiting for task %s; the upload itself succeeded", id)
		case <-time.After(pollEvery):
		}
	}
}

func (h *handlers) update(c *agentcli.Ctx) (*agentcli.WritePlan, error) {
	id, err := parseID(c.Args[0])
	if err != nil {
		return nil, err
	}
	s := h.session(c)
	d, err := s.document(id, false)
	if err != nil {
		return nil, err
	}
	scopes := s.scopes(d)
	var p Patch
	if c.IsSet("title") {
		v := strings.TrimSpace(c.String("title"))
		if v == "" {
			return nil, agentcli.Usagef("title must not be empty")
		}
		p.Title = &v
	}
	if c.IsSet("add-tag") || c.IsSet("remove-tag") {
		add, remove := map[int]bool{}, map[int]bool{}
		var added []int
		for _, x := range []struct {
			flag string
			set  map[int]bool
		}{{"add-tag", add}, {"remove-tag", remove}} {
			for _, ref := range c.Strings(x.flag) {
				t, err := s.tag(ref)
				if err != nil {
					return nil, err
				}
				if x.flag == "add-tag" && !add[t.ID] {
					added = append(added, t.ID)
				}
				x.set[t.ID] = true
				scopes = append(scopes, tagScope(t.Name))
			}
		}
		tags := []int{}
		for _, t := range d.Tags {
			if remove[t] && add[t] {
				return nil, agentcli.Usagef("tag %q is both added and removed", s.tagName(t))
			}
			if !remove[t] {
				tags = append(tags, t)
			}
			delete(add, t)
		}
		for _, t := range added {
			if remove[t] {
				return nil, agentcli.Usagef("tag %q is both added and removed", s.tagName(t))
			}
			if add[t] {
				tags = append(tags, t)
			}
		}
		if len(tags) == 0 {
			scopes = append(scopes, "untagged")
		}
		p.Tags = &tags
	}
	if c.IsSet("correspondent") {
		n, err := s.named(false, c.String("correspondent"))
		if err != nil {
			return nil, err
		}
		p.Correspondent = &n.ID
	}
	if c.IsSet("type") {
		n, err := s.named(true, c.String("type"))
		if err != nil {
			return nil, err
		}
		p.DocumentType = &n.ID
	}
	if p == (Patch{}) {
		return nil, agentcli.Usagef("nothing to change; pass --title, --add-tag, --remove-tag, --correspondent or --type")
	}
	preview := d
	if p.Title != nil {
		preview.Title = *p.Title
	}
	if p.Tags != nil {
		preview.Tags = *p.Tags
	}
	if p.Correspondent != nil {
		preview.Correspondent = *p.Correspondent
	}
	if p.DocumentType != nil {
		preview.DocumentType = *p.DocumentType
	}
	return &agentcli.WritePlan{Scopes: dedupe(scopes), Targets: []agentcli.Item{s.docItem(preview)},
		Apply: func(ctx context.Context) ([]agentcli.Item, error) {
			u, err := s.cl.Update(ctx, id, p)
			if err != nil {
				return nil, err
			}
			return []agentcli.Item{s.docItem(u)}, nil
		}}, nil
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	out := xs[:0]
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func (h *handlers) delete(c *agentcli.Ctx) (*agentcli.WritePlan, error) {
	s := h.session(c)
	var docs []Document
	var scopes []string
	var targets []agentcli.Item
	seen := map[int]bool{}
	for _, arg := range c.Args {
		id, err := parseID(arg)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		d, err := s.document(id, false)
		if err != nil {
			return nil, err
		}
		docs = append(docs, d)
		scopes = append(scopes, s.scopes(d)...)
		targets = append(targets, s.docItem(d))
	}
	return &agentcli.WritePlan{Scopes: dedupe(scopes), Targets: targets, Apply: func(ctx context.Context) ([]agentcli.Item, error) {
		var out []agentcli.Item
		for _, d := range docs {
			if err := s.cl.Delete(ctx, d.ID); err != nil {
				return nil, partial(err, len(out), len(docs))
			}
			it := s.docItem(d)
			it["deleted"] = true
			out = append(out, it)
		}
		return out, nil
	}}, nil
}

// partial annotates an error that happened after some writes succeeded.
func partial(err error, done, total int) error {
	if done == 0 {
		return err
	}
	e := *agentcli.AsError(err)
	e.Message = strconv.Itoa(done) + " of " + strconv.Itoa(total) + " writes succeeded before: " + e.Message
	return &e
}

func (h *handlers) tagCreate(c *agentcli.Ctx) (*agentcli.WritePlan, error) {
	name := strings.TrimSpace(c.Args[0])
	if name == "" || len([]rune(name)) > maxTagName {
		return nil, agentcli.Usagef("tag name must be 1-%d characters", maxTagName)
	}
	if agentcli.Sanitize(name) != name {
		return nil, agentcli.Usagef("tag name must not contain control characters or line breaks")
	}
	s := h.session(c)
	ts, err := s.loadTags()
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		if strings.EqualFold(t.Name, name) {
			return nil, agentcli.Conflictf("tag %q already exists (id %d)", t.Name, t.ID)
		}
	}
	return &agentcli.WritePlan{Scopes: []string{tagScope(name)}, Targets: []agentcli.Item{{"name": name}},
		Apply: func(ctx context.Context) ([]agentcli.Item, error) {
			t, err := s.cl.CreateTag(ctx, name)
			if err != nil {
				return nil, err
			}
			return []agentcli.Item{tagItem(t)}, nil
		}}, nil
}
