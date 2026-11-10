package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var helpSections = []struct {
	title string
	keys  [][2]string
}{
	{"Move", [][2]string{
		{"↑↓ j k", "select line"},
		{"PgUp PgDn", "scroll (or wheel)"},
		{"g G", "first / newest + follow"},
		{"click", "select line"},
		{"enter", "detail, indented JSON"},
	}},
	{"Find", [][2]string{
		{"/  n N", "search, next, previous"},
		{`\`, "filter (fzf syntax, exact match):"},
		{"", `  a b  both · !a  not · a | b  either`},
		{"", `  "a b"  phrase · ^a a$  message start/end`},
		{"", "  lowercase ignores case"},
		{"e E", "next / previous error"},
		{"t", "go to time: 15:30, -10m"},
		{"esc", "clear search, filter, request"},
	}},
	{"Lambda", [][2]string{
		{"r", "only this request; r again: all"},
	}},
	{"Groups and history", [][2]string{
		{"o", "change groups (tab marks)"},
		{"[  ]", "load 15 min before / after"},
	}},
	{"Other", [][2]string{
		{"y", "copy line"},
		{"w", "save shown lines to file"},
		{"f", "full / compact lines"},
		{"q", "quit"},
	}},
}

// helpView lists all keys, in several columns when one column does not
// fit the height. Any key closes it.
func helpView(width, height int) string {
	var cols [][]string
	var col []string
	for _, sec := range helpSections {
		block := []string{"", " " + sec.title}
		for _, k := range sec.keys {
			block = append(block, fmt.Sprintf("   %s  %s", padRight(k[0], 10), dimStyle.Render(k[1])))
		}
		if len(col) > 0 && len(col)+len(block) > height-2 {
			cols = append(cols, col)
			col = nil
		}
		col = append(col, block...)
	}
	cols = append(cols, append(col, "", dimStyle.Render(" press any key to close")))

	rendered := make([]string, len(cols))
	for i, c := range cols {
		w := 0
		for _, l := range c {
			w = max(w, ansi.StringWidth(l))
		}
		for j, l := range c {
			c[j] = padRight(l, w+2)
		}
		rendered[i] = strings.Join(c, "\n")
	}
	lines := strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, rendered...), "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "…")
	}
	return strings.Join(lines, "\n")
}

// errorsLastMinute counts error entries from the last 60 seconds. Entries
// are in time order except for small gaps between groups, so the scan stops
// a little past the cutoff.
func (m *model) errorsLastMinute() int {
	cutoff := time.Now().Add(-time.Minute).UnixMilli()
	stop := cutoff - 10_000
	n := 0
	for i := len(m.entries) - 1; i >= 0; i-- {
		e := &m.entries[i]
		if e.time < stop {
			break
		}
		if e.time >= cutoff && e.level == levelError {
			n++
		}
	}
	return n
}

// parseJumpTime reads "15:30", "15:30:10" (today, or yesterday when that
// is in the future), "-10m" or "10m" (before now), or "2026-11-10 15:30".
// Times are local.
func parseJumpTime(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if d, err := time.ParseDuration(strings.TrimPrefix(s, "-")); err == nil {
		return now.Add(-d), nil
	}
	for _, f := range []string{"15:04", "15:04:05"} {
		if t, err := time.ParseInLocation(f, s, now.Location()); err == nil {
			y, mo, d := now.Date()
			t = time.Date(y, mo, d, t.Hour(), t.Minute(), t.Second(), 0, now.Location())
			if t.After(now) {
				t = t.AddDate(0, 0, -1)
			}
			return t, nil
		}
	}
	for _, f := range []string{"2006-01-02 15:04", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(f, s, now.Location()); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot read time %q (use 15:30, 15:30:10, -10m, or 2026-11-10 15:30)", s)
}
