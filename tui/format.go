package tui

import (
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Compact display removes text that repeats on every line. It changes only
// what the list shows. Search, copy, save, and the detail view use the full
// line.

var (
	// A fixed gray, not Faint: many dark themes render faint text too dim.
	timeStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "250"})

	// sgr matches one color escape code.
	sgr = `(?:\x1b\[[0-9;]*m)*`

	// coloredPrefix matches the "[time] (stream) " prefix, with or without
	// color codes around the time and the stream.
	coloredPrefix = regexp.MustCompile(`^\[` + sgr + `([^\x1b\]]*)` + sgr + `\] \(` + sgr + `[^\x1b)]*` + sgr + `\) `)

	// innerTime matches the timestamp that Lambda runtimes put at the start
	// of each message, after an optional "[LEVEL]". the line already shows the
	// event time, so it repeats.
	innerTime = regexp.MustCompile(`^((?:\[[A-Z]+\]\s+)?)\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z\s+`)
)

// compact splits a log line, with or without color codes, into a short time and a shorter message:
//   - local time without the date (the date stays when it is not today)
//   - no log stream name
//   - no repeated runtime timestamp
//   - the line's request ID cut to 8 characters
//   - tabs as single spaces
//
// t is "" when the line has no "[time] (stream)" prefix with a valid time.
func compact(s, req string, now time.Time) (t, msg string) {
	if loc := coloredPrefix.FindStringSubmatchIndex(s); loc != nil {
		if st, ok := shortTime(s[loc[2]:loc[3]], now); ok {
			t = st
			s = s[loc[1]:]
		}
	}
	s = innerTime.ReplaceAllString(s, "$1")
	if req != "" {
		s = strings.ReplaceAll(s, req, short(req))
	}
	return t, strings.ReplaceAll(s, "\t", " ")
}

// shortTime turns an RFC 3339 time into local "15:04:05", or
// "Jan 02 15:04:05" when the day is not today. ok is false when s is not an
// RFC 3339 time, so the caller keeps the line unchanged.
func shortTime(s string, now time.Time) (string, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "", false
	}
	t = t.Local()
	if y, mo, d := t.Date(); y == now.Year() && mo == now.Month() && d == now.Day() {
		return t.Format("15:04:05"), true
	}
	return t.Format("Jan 02 15:04:05"), true
}

// display returns the colored and plain text that the list shows for e.
// With several groups, each line starts with a short group tag.
func (m *model) display(e entry) (colored, plain string) {
	tag, ptag := "", ""
	if m.tagWidth > 0 {
		ptag = padRight(shortGroup(m.groups[e.group]), m.tagWidth) + " "
		tag = tagStyles[e.group%len(tagStyles)].Render(ptag)
	}
	if m.full {
		return tag + strings.ReplaceAll(e.text, "\t", "  "), ptag + strings.ReplaceAll(e.plain, "\t", "  ")
	}
	now := m.now
	if now.IsZero() {
		now = time.Now()
	}
	ct, cmsg := compact(e.text, e.req, now)
	pt, pmsg := ct, cmsg
	if e.text != e.plain {
		pt, pmsg = compact(e.plain, e.req, now)
	}
	if ct == "" {
		return tag + cmsg, ptag + pmsg
	}
	return tag + timeStyle.Render(ct) + " " + cmsg, ptag + pt + " " + pmsg
}

var tagStyles = []lipgloss.Style{
	lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("13")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
}

// shortGroup returns the last part of a log group name, for example
// "orders" for "/aws/lambda/orders".
func shortGroup(g string) string {
	if i := strings.LastIndexByte(strings.TrimRight(g, "/"), '/'); i >= 0 {
		return strings.TrimRight(g, "/")[i+1:]
	}
	return g
}
