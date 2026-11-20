package agentcli

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// cursorPrefix versions the opaque cursor format.
const cursorPrefix = "c1."

type cursorData struct {
	Offset int    `json:"o"`
	Key    string `json:"k"`
}

// QueryKey derives a short fingerprint of the query a cursor belongs to, so a
// cursor from one query cannot silently be applied to another.
func QueryKey(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:6])
}

// EncodeCursor returns an opaque cursor for offset within query key.
func EncodeCursor(offset int, key string) string {
	b, _ := json.Marshal(cursorData{Offset: offset, Key: key})
	return cursorPrefix + base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor parses a cursor and verifies it belongs to key. An empty
// cursor means offset 0.
func DecodeCursor(c, key string) (int, error) {
	if c == "" {
		return 0, nil
	}
	if !strings.HasPrefix(c, cursorPrefix) || len(c) > 256 {
		return 0, Usagef("invalid cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(c[len(cursorPrefix):])
	if err != nil {
		return 0, Usagef("invalid cursor")
	}
	var d cursorData
	if err := json.Unmarshal(raw, &d); err != nil || d.Offset < 0 {
		return 0, Usagef("invalid cursor")
	}
	if d.Key != key {
		return 0, Usagef("cursor belongs to a different query; rerun without --cursor")
	}
	return d.Offset, nil
}

// Paginate slices items by cursor and limit and returns the page with a
// cursor for the next slice, if any.
func Paginate(items []Item, cursor, key string, limit int) (Page, error) {
	off, err := DecodeCursor(cursor, key)
	if err != nil {
		return Page{}, err
	}
	if off > len(items) {
		off = len(items)
	}
	end := len(items)
	if limit > 0 && off+limit < end {
		end = off + limit
	}
	p := Page{Items: items[off:end]}
	if end < len(items) {
		p.NextCursor = EncodeCursor(end, key)
	}
	return p, nil
}
