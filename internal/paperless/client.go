// Package paperless implements the paperless CLI on top of a Client for the
// Paperless-ngx REST API (net/http in production, a fake in tests).
package paperless

import (
	"context"
	"io"
	"time"
)

// Document is one Paperless-ngx document. Related objects are ids; the
// commands resolve them to names.
type Document struct {
	ID               int
	Title            string
	Content          string // only filled by Document(..., content=true)
	Created          time.Time
	Added            time.Time
	Modified         time.Time
	Correspondent    int // 0 = none
	DocumentType     int // 0 = none
	Tags             []int
	ASN              int // archive serial number, 0 = none
	OriginalFileName string
	MimeType         string
	PageCount        int
	Search           *SearchHit // set for full-text search results
}

// SearchHit is the full-text search metadata of a result.
type SearchHit struct {
	Score      float64
	Rank       int
	Highlights string
}

// Tag is a Paperless tag.
type Tag struct {
	ID            int
	Name          string
	DocumentCount int
	IsInbox       bool
}

// Named is a correspondent or document type.
type Named struct {
	ID            int
	Name          string
	DocumentCount int
}

// Query filters the document list. Zero values mean "no filter".
type Query struct {
	TitleContent  string // case-insensitive substring of title or content
	FullText      string // full-text index query (ranked)
	TagsAll       []int  // documents carrying all of these tags
	Correspondent int
	DocumentType  int
	CreatedFrom   string // YYYY-MM-DD, inclusive
	CreatedBefore string // YYYY-MM-DD, exclusive
}

// DocPage is one page of the document list.
type DocPage struct {
	Count int // total matches on the server
	Docs  []Document
}

// Upload is the input for Upload. File is streamed, never buffered whole.
type Upload struct {
	FileName      string
	File          io.Reader
	Size          int64 // exact number of bytes File yields
	Title         string
	Tags          []int
	Correspondent int
	DocumentType  int
}

// Task is a consumption task created by an upload.
type Task struct {
	ID       string
	Status   string // PENDING, STARTED, SUCCESS, FAILURE, …
	Result   string
	Document int // related document id once consumed
}

// Patch changes document fields; nil means unchanged.
type Patch struct {
	Title         *string
	Tags          *[]int
	Correspondent *int
	DocumentType  *int
}

// ServerInfo describes the server for doctor.
type ServerInfo struct {
	BaseURL     string
	TokenSource string // "env" or "keychain"; never the token itself
	Version     string
	APIVersion  string
}

// Client is the Paperless-ngx boundary. Implementations take typed
// parameters only and must never expose the API token.
type Client interface {
	Info(ctx context.Context) (ServerInfo, error)
	Documents(ctx context.Context, q Query, page, pageSize int) (DocPage, error)
	Document(ctx context.Context, id int, content bool) (Document, error)
	// Download streams the archived (or original) file to w and returns the
	// server's file name and media type.
	Download(ctx context.Context, id int, original bool, w io.Writer) (name, mime string, n int64, err error)
	Tags(ctx context.Context) ([]Tag, error)
	Correspondents(ctx context.Context) ([]Named, error)
	DocumentTypes(ctx context.Context) ([]Named, error)
	Upload(ctx context.Context, u Upload) (taskID string, err error)
	Task(ctx context.Context, id string) (Task, error)
	Update(ctx context.Context, id int, p Patch) (Document, error)
	Delete(ctx context.Context, id int) error
	CreateTag(ctx context.Context, name string) (Tag, error)
}
