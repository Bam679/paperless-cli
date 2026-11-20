package agentcli

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"plain":                 "plain",
		"a\tb\nc\r\nd":          "a b c d",
		"  lead":                "lead",
		"trail  ":               "trail",
		"esc\x1b[31mred":        "esc[31mred",
		"bidi\u202eevil":        "bidievil",
		"line\u2028sep":         "line sep",
		"bad\xffutf8":           "badutf8",
		"emoji 🙂 ok":            "emoji 🙂 ok",
		"many    spaces\t\tand": "many spaces and",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q)=%q want %q", in, got, want)
		}
	}
}

func FuzzSanitize(f *testing.F) {
	f.Add("hello\nworld")
	f.Add("\x1b]0;title\x07")
	f.Fuzz(func(t *testing.T, s string) {
		out := Sanitize(s)
		if !utf8.ValidString(out) {
			t.Fatalf("invalid utf8 from %q", s)
		}
		for _, r := range out {
			if r == '\n' || r == '\r' || r == '\t' || unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
				t.Fatalf("control rune %U survived in %q", r, out)
			}
		}
	})
}

func TestTextValue(t *testing.T) {
	ts := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	cases := []struct {
		in   any
		want string
	}{
		{nil, "-"}, {"", "-"}, {"x", "x"}, {true, "true"}, {3, "3"}, {int64(4), "4"},
		{1.5, "1.5"}, {ts, "2026-03-04T05:06:07Z"}, {[]string{"a", "b\nc"}, "a,b c"}, {[]string{}, "-"},
		{map[string]int{"k": 1}, `{"k":1}`},
	}
	for _, c := range cases {
		if got := textValue(c.in); got != c.want {
			t.Errorf("textValue(%v)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestListMaxCharsNeverExceeds(t *testing.T) {
	var items []Item
	for i := 0; i < 50; i++ {
		items = append(items, Item{"id": i, "title": strings.Repeat("t", i)})
	}
	for _, f := range []Format{FormatJSON, FormatNDJSON, FormatText} {
		var buf bytes.Buffer
		r := &Renderer{W: &buf, Format: f, MaxChars: 300}
		if err := r.List(Page{Items: items}, []string{"id", "title"}); err != nil {
			t.Fatal(err)
		}
		if buf.Len() > 300+80 { // trailer line may be appended
			t.Errorf("format %d: %d bytes exceeds budget", f, buf.Len())
		}
		if !strings.Contains(buf.String(), "truncated") {
			t.Errorf("format %d: truncation not flagged", f)
		}
	}
}

func TestShrinkKeepsUTF8(t *testing.T) {
	it := Item{"body": strings.Repeat("ä", 500), "id": "1"}
	out := shrink(it, 200, false, []string{"id", "body"})
	b := out["body"].(string)
	if !utf8.ValidString(b) || len(b) >= 1000 || out["truncated"] != true {
		t.Fatalf("bad shrink: len=%d valid=%v", len(b), utf8.ValidString(b))
	}
}

func TestCursorFuzzSeeds(t *testing.T) {
	c := EncodeCursor(5, "k")
	if off, err := DecodeCursor(c, "k"); err != nil || off != 5 {
		t.Fatalf("roundtrip: %d %v", off, err)
	}
	for _, bad := range []string{"x", "c1.!!", "c1." + strings.Repeat("A", 300), EncodeCursor(-1, "k")} {
		if _, err := DecodeCursor(bad, "k"); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func FuzzDecodeCursor(f *testing.F) {
	f.Add(EncodeCursor(3, "abc"))
	f.Add("c1.e30")
	f.Fuzz(func(t *testing.T, c string) {
		off, err := DecodeCursor(c, "abc")
		if err == nil && off < 0 {
			t.Fatal("negative offset accepted")
		}
	})
}

func TestPaginateBounds(t *testing.T) {
	items := []Item{{"i": 1}, {"i": 2}, {"i": 3}}
	p, err := Paginate(items, EncodeCursor(10, "k"), "k", 2)
	if err != nil || len(p.Items) != 0 || p.NextCursor != "" {
		t.Fatalf("offset past end: %v %v", p, err)
	}
	p, _ = Paginate(items, "", "k", 0)
	if len(p.Items) != 3 {
		t.Fatal("limit 0 means all")
	}
}

func TestAsError(t *testing.T) {
	if AsError(nil) != nil {
		t.Fatal("nil")
	}
	e := AsError(strings.NewReader("").UnreadByte())
	if e.Exit != ExitError {
		t.Fatal("plain errors are internal")
	}
	long := AsError(errString(strings.Repeat("x", 900)))
	if len([]rune(long.Message)) > 501 {
		t.Fatal("internal messages must be truncated")
	}
	for _, f := range []func(string, ...any) *Error{Usagef, NotFoundf, Deniedf, Permissionf, Unavailablef, Conflictf, Internalf} {
		if f("m").Error() == "" {
			t.Fatal("empty")
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
