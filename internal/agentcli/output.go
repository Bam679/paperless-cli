package agentcli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Format selects the output encoding.
type Format int

// Output formats.
const (
	FormatText   Format = iota // one line per item, tab-separated, no header
	FormatJSON                 // one JSON document
	FormatNDJSON               // one JSON object per line
)

// Item is one output record. Values must be JSON-encodable.
type Item map[string]any

// Page is a list result with an optional continuation cursor.
type Page struct {
	Items      []Item
	NextCursor string
}

// Renderer writes results according to the output contract.
type Renderer struct {
	W        io.Writer
	Format   Format
	Fields   []string // selected fields; empty = command default
	MaxChars int      // 0 = unlimited
}

// listEnvelope is the --json shape for lists.
type listEnvelope struct {
	Items      []Item `json:"items"`
	Count      int    `json:"count"`
	NextCursor string `json:"next_cursor,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

// List renders a page. defaults is the command's default field order.
func (r *Renderer) List(p Page, defaults []string) error {
	fields := r.fieldsOr(defaults)
	items := make([]Item, 0, len(p.Items))
	for _, it := range p.Items {
		items = append(items, project(it, fields, r.Fields != nil))
	}
	switch r.Format {
	case FormatJSON:
		env := listEnvelope{Items: items, Count: len(items), NextCursor: p.NextCursor}
		if r.MaxChars > 0 {
			total := jsonLen(listEnvelope{Items: []Item{}, NextCursor: p.NextCursor, Truncated: true})
			keep := 0
			for _, it := range items {
				n := jsonLen(it) + 1
				if total+n > r.MaxChars {
					env.Truncated = true
					break
				}
				total += n
				keep++
			}
			env.Items, env.Count = items[:keep], keep
		}
		return r.writeJSON(env)
	case FormatNDJSON:
		var buf bytes.Buffer
		truncated := false
		for _, it := range items {
			line := mustJSON(it) + "\n"
			if r.MaxChars > 0 && buf.Len()+len(line) > r.MaxChars {
				truncated = true
				break
			}
			buf.WriteString(line)
		}
		if p.NextCursor != "" || truncated {
			buf.WriteString(mustJSON(Item{"_meta": metaOf(p.NextCursor, truncated)}) + "\n")
		}
		_, err := r.W.Write(buf.Bytes())
		return err
	default:
		var buf bytes.Buffer
		truncated := false
		for _, it := range items {
			line := textLine(it, fields) + "\n"
			if r.MaxChars > 0 && buf.Len()+len(line) > r.MaxChars {
				truncated = true
				break
			}
			buf.WriteString(line)
		}
		if truncated {
			buf.WriteString("# truncated: output exceeded --max-chars\n")
		}
		if p.NextCursor != "" {
			buf.WriteString("# next_cursor=" + p.NextCursor + "\n")
		}
		_, err := r.W.Write(buf.Bytes())
		return err
	}
}

func metaOf(cursor string, truncated bool) Item {
	m := Item{}
	if cursor != "" {
		m["next_cursor"] = cursor
	}
	if truncated {
		m["truncated"] = true
	}
	return m
}

// Object renders a single record. In text mode it prints "key: value" lines
// in field order. Long string fields are shortened to honour --max-chars.
func (r *Renderer) Object(it Item, defaults []string) error {
	fields := r.fieldsOr(defaults)
	out := project(it, fields, r.Fields != nil)
	if r.MaxChars > 0 {
		out = shrink(out, r.MaxChars, r.Format == FormatText, fields)
	}
	if r.Format == FormatText {
		var buf bytes.Buffer
		for _, f := range orderedKeys(out, fields) {
			fmt.Fprintf(&buf, "%s: %s\n", f, textValue(out[f]))
		}
		_, err := r.W.Write(buf.Bytes())
		return err
	}
	return r.writeJSON(out)
}

func (r *Renderer) fieldsOr(defaults []string) []string {
	if len(r.Fields) > 0 {
		return r.Fields
	}
	return defaults
}

func (r *Renderer) writeJSON(v any) error {
	enc := json.NewEncoder(r.W)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// project keeps only the requested fields. When the user did not pass
// --fields, JSON output keeps every field (text uses defaults for columns).
func project(it Item, fields []string, explicit bool) Item {
	if !explicit {
		return it
	}
	out := Item{}
	for _, f := range fields {
		if v, ok := it[f]; ok {
			out[f] = v
		}
	}
	return out
}

// orderedKeys returns fields present in it: first the given order, then the rest sorted.
func orderedKeys(it Item, fields []string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, f := range fields {
		if _, ok := it[f]; ok && !seen[f] {
			keys = append(keys, f)
			seen[f] = true
		}
	}
	var rest []string
	for k := range it {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

// shrink trims the longest string fields until the rendering fits max.
func shrink(it Item, max int, text bool, fields []string) Item {
	size := func(x Item) int {
		if text {
			n := 0
			for _, k := range orderedKeys(x, fields) {
				n += len(k) + 3 + len(textValue(x[k]))
			}
			return n
		}
		return jsonLen(x)
	}
	out := Item{}
	for k, v := range it {
		out[k] = v
	}
	for i := 0; i < 64 && size(out) > max; i++ {
		key, longest := "", 0
		for k, v := range out {
			if s, ok := v.(string); ok && len(s) > longest {
				key, longest = k, len(s)
			}
		}
		if key == "" || longest <= 16 {
			break
		}
		over := size(out) - max
		keep := longest - over - 16
		if keep < 0 {
			keep = 0
		}
		out[key] = cutUTF8(out[key].(string), keep) + "…[truncated]"
		out["truncated"] = true
	}
	return out
}

func cutUTF8(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func jsonLen(v any) int { return len(mustJSON(v)) }

func mustJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return `{"error":"unencodable"}`
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

func textLine(it Item, fields []string) string {
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = textValue(it[f])
	}
	return strings.Join(parts, "\t")
}

// textValue renders a value on a single line without control characters.
func textValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "-"
	case string:
		if x == "" {
			return "-"
		}
		return Sanitize(x)
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case time.Time:
		return x.Format(time.RFC3339)
	case []string:
		if len(x) == 0 {
			return "-"
		}
		s := make([]string, len(x))
		for i := range x {
			s[i] = Sanitize(x[i])
		}
		return strings.Join(s, ",")
	default:
		return Sanitize(mustJSON(x))
	}
}

// Sanitize collapses whitespace and strips control and bidi-override
// characters so a value can never break the one-line-per-item contract or
// smuggle terminal escape sequences.
func Sanitize(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case r == utf8.RuneError:
			continue
		case unicode.IsSpace(r):
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = true
			continue
		case unicode.IsControl(r), unicode.Is(unicode.Bidi_Control, r), r == ' ', r == ' ':
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return strings.TrimRight(b.String(), " ")
}
