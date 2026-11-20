package agentcli

import (
	"strings"
	"time"
)

// ParseTime parses agent-friendly date/time input in loc:
// "today", "tomorrow", "yesterday", "+3d"/"-2d", "2006-01-02" (date only),
// "2006-01-02T15:04", "2006-01-02 15:04" and RFC 3339. dateOnly reports
// whether the input had no time of day.
func ParseTime(s string, now time.Time, loc *time.Location) (t time.Time, dateOnly bool, err error) {
	s = strings.TrimSpace(s)
	now = now.In(loc)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	switch strings.ToLower(s) {
	case "today":
		return midnight, true, nil
	case "tomorrow":
		return midnight.AddDate(0, 0, 1), true, nil
	case "yesterday":
		return midnight.AddDate(0, 0, -1), true, nil
	}
	if len(s) >= 3 && (s[0] == '+' || s[0] == '-') && s[len(s)-1] == 'd' {
		n := 0
		for _, r := range s[1 : len(s)-1] {
			if r < '0' || r > '9' || n > 36500 {
				return time.Time{}, false, Usagef("invalid relative date %q (use +3d)", s)
			}
			n = n*10 + int(r-'0')
		}
		if s[0] == '-' {
			n = -n
		}
		return midnight.AddDate(0, 0, n), true, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return t, true, nil
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, false, nil
		}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(loc), false, nil
	}
	return time.Time{}, false, Usagef("invalid date %q (use YYYY-MM-DD, YYYY-MM-DDTHH:MM, RFC 3339, today, tomorrow or +Nd)", s)
}

// FormatTime renders t for output: date only for all-day values, otherwise
// RFC 3339 in the local zone. A zero time renders as "".
func FormatTime(t time.Time, dateOnly bool) string {
	if t.IsZero() {
		return ""
	}
	if dateOnly {
		return t.Format("2006-01-02")
	}
	return t.Format(time.RFC3339)
}
