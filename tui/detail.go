package tui

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	labelStyle = lipgloss.NewStyle().Faint(true)
	keyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	jsonKey    = regexp.MustCompile(`(?m)^(\s*)("(?:[^"\\]|\\.)*")(:)`)
)

// openDetail shows entry e on its own screen, with JSON indented.
func (m *model) openDetail(e entry) {
	m.detail = true
	m.detailEntry = e
	m.detailText = detailText(e.plain)
	m.dvp = viewport.New(m.width, m.vp.Height)
	m.renderDetail()
}

// renderDetail fills the detail viewport at the current width.
func (m *model) renderDetail() {
	e := m.detailEntry
	when, stream, _ := splitPrefix(e.plain)
	var head []string
	add := func(label, value string) {
		if value != "" {
			head = append(head, labelStyle.Render(label)+" "+value)
		}
	}
	if len(m.groups) > 1 {
		add("group:  ", m.groups[e.group])
	}
	add("time:   ", when)
	add("stream: ", stream)
	if id := e.req; id != "" {
		add("request:", id)
		if r, ok := m.reports[id]; ok {
			add("report: ", r.String())
		}
	}
	body := jsonKey.ReplaceAllString(m.detailText, "$1"+keyStyle.Render("$2")+"$3")
	text := strings.Join(head, "\n")
	if text != "" {
		text += "\n\n"
	}
	text += body
	m.dvp.Width, m.dvp.Height = m.width, m.vp.Height
	m.dvp.SetContent(ansi.Hardwrap(text, m.width, true))
}

func (m model) updateDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		m.notice = ""
		switch k.String() {
		case "esc", "q", "enter", "backspace":
			m.detail = false
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		case "y":
			m.copyText(m.detailText)
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.dvp, cmd = m.dvp.Update(msg)
	return m, cmd
}

// detailText returns the message of a log line. When the message holds JSON,
// it returns the JSON indented, after any text that comes before it.
func detailText(plain string) string {
	_, _, msg := splitPrefix(plain)
	msg = strings.ReplaceAll(msg, "\t", "  ")
	start := strings.IndexAny(msg, "{[")
	if start < 0 {
		return msg
	}
	var compact, indented bytes.Buffer
	if json.Compact(&compact, []byte(strings.TrimSpace(msg[start:]))) != nil {
		return msg
	}
	if json.Indent(&indented, compact.Bytes(), "", "  ") != nil {
		return msg
	}
	prefix := strings.TrimSpace(msg[:start])
	if prefix == "" {
		return indented.String()
	}
	return prefix + "\n" + indented.String()
}
