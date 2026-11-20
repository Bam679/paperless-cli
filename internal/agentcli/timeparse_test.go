package agentcli

import (
	"testing"
	"time"
)

func TestParseTime(t *testing.T) {
	loc := time.FixedZone("X", 3600)
	now := time.Date(2026, 11, 20, 22, 30, 0, 0, time.UTC) // 23:30 in loc
	cases := []struct {
		in       string
		want     string
		dateOnly bool
	}{
		{"today", "2026-11-20T00:00:00+01:00", true},
		{"Tomorrow", "2026-11-21T00:00:00+01:00", true},
		{"yesterday", "2026-11-19T00:00:00+01:00", true},
		{"+3d", "2026-11-23T00:00:00+01:00", true},
		{"-1d", "2026-11-19T00:00:00+01:00", true},
		{"2026-12-24", "2026-12-24T00:00:00+01:00", true},
		{"2026-12-24T18:30", "2026-12-24T18:30:00+01:00", false},
		{"2026-12-24 18:30", "2026-12-24T18:30:00+01:00", false},
		{"2026-12-24T18:30:15", "2026-12-24T18:30:15+01:00", false},
		{"2026-12-24T18:30:00Z", "2026-12-24T19:30:00+01:00", false},
	}
	for _, c := range cases {
		got, d, err := ParseTime(c.in, now, loc)
		if err != nil || got.Format(time.RFC3339) != c.want || d != c.dateOnly {
			t.Errorf("%q: got %s %v %v want %s %v", c.in, got.Format(time.RFC3339), d, err, c.want, c.dateOnly)
		}
	}
	for _, bad := range []string{"", "soon", "+d", "+3x", "+99999999999d", "2026-13-01", "+3.5d"} {
		if _, _, err := ParseTime(bad, now, loc); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func FuzzParseTime(f *testing.F) {
	f.Add("+12d")
	f.Add("2026-01-02T03:04")
	f.Fuzz(func(t *testing.T, s string) {
		_, _, _ = ParseTime(s, time.Unix(0, 0), time.UTC)
	})
}

func TestFormatTime(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if FormatTime(ts, true) != "2026-01-02" || FormatTime(ts, false) != "2026-01-02T03:04:05Z" || FormatTime(time.Time{}, false) != "" {
		t.Fatal("format")
	}
}
