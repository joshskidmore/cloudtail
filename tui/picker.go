package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

var (
	hitStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	cursorStyle = lipgloss.NewStyle().Reverse(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	markStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
)

// maxRecent is how many recent groups the picker remembers.
const maxRecent = 30

// picker is a fuzzy-search list of log groups. Tab marks several groups.
type picker struct {
	items   []string
	recent  map[string]bool
	marked  map[string]bool
	input   textinput.Model
	matches fuzzy.Matches
	cursor  int
	offset  int
}

// newPicker lists recent groups first, in recent order, then the rest.
func newPicker(groups []string) picker {
	in := textinput.New()
	in.Prompt = "log group> "
	in.Focus()

	exists := map[string]bool{}
	for _, g := range groups {
		exists[g] = true
	}
	p := picker{input: in, recent: map[string]bool{}, marked: map[string]bool{}}
	for _, g := range loadRecent() {
		if exists[g] && !p.recent[g] {
			p.recent[g] = true
			p.items = append(p.items, g)
		}
	}
	for _, g := range groups {
		if !p.recent[g] {
			p.items = append(p.items, g)
		}
	}
	p.filter()
	return p
}

// update handles a key. done is true when the user chose groups (chosen is
// not empty) or cancelled (chosen is nil).
func (p picker) update(msg tea.KeyMsg, height int) (_ picker, cmd tea.Cmd, chosen []string, done bool) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return p, nil, nil, true
	case "enter":
		for _, g := range p.items {
			if p.marked[g] {
				chosen = append(chosen, g)
			}
		}
		if len(chosen) == 0 && len(p.matches) > 0 {
			chosen = []string{p.matches[p.cursor].Str}
		}
		if len(chosen) == 0 {
			return p, nil, nil, false
		}
		saveRecent(chosen)
		return p, nil, chosen, true
	case "tab":
		if len(p.matches) > 0 {
			g := p.matches[p.cursor].Str
			p.marked[g] = !p.marked[g]
			p.cursor++
			p.clamp(height)
		}
		return p, nil, nil, false
	case "up", "ctrl+p", "ctrl+k":
		p.cursor--
	case "down", "ctrl+n", "ctrl+j":
		p.cursor++
	case "pgup":
		p.cursor -= listHeight(height)
	case "pgdown":
		p.cursor += listHeight(height)
	default:
		prev := p.input.Value()
		p.input, cmd = p.input.Update(msg)
		if p.input.Value() != prev {
			p.filter()
		}
		return p, cmd, nil, false
	}
	p.clamp(height)
	return p, nil, nil, false
}

func (p picker) view(width, height int, loading bool, err error) string {
	var b strings.Builder
	b.WriteString(p.input.View() + "\n")
	switch {
	case err != nil:
		b.WriteString(errorStyle.Render("  error: " + err.Error()))
		return b.String()
	case loading:
		b.WriteString(dimStyle.Render("  loading log groups…"))
		return b.String()
	}
	info := fmt.Sprintf("  %d/%d", len(p.matches), len(p.items))
	marked := 0
	for _, on := range p.marked {
		if on {
			marked++
		}
	}
	if marked > 0 {
		info += fmt.Sprintf(" · %d marked", marked)
	}
	b.WriteString(dimStyle.Render(info + " · tab mark · enter watch · esc cancel"))
	end := min(p.offset+listHeight(height), len(p.matches))
	for i := p.offset; i < end; i++ {
		g := p.matches[i].Str
		mark := "  "
		if p.marked[g] {
			mark = markStyle.Render("● ")
		}
		name := highlightRunes(p.matches[i])
		if i == p.cursor {
			name = cursorStyle.Render(g)
		}
		if p.recent[g] {
			name += dimStyle.Render("  recent")
		}
		b.WriteString("\n" + ansi.Truncate(mark+name, width, "…"))
	}
	return b.String()
}

// filter recomputes matches for the current query. An empty query lists all
// items in their original order.
func (p *picker) filter() {
	q := p.input.Value()
	if q == "" {
		p.matches = make(fuzzy.Matches, len(p.items))
		for i, s := range p.items {
			p.matches[i] = fuzzy.Match{Str: s, Index: i}
		}
	} else {
		p.matches = fuzzy.Find(q, p.items)
	}
	p.cursor, p.offset = 0, 0
}

// listHeight is the number of rows left for items under the prompt and info.
func listHeight(height int) int {
	return max(height-2, 1)
}

// clamp keeps the cursor in range and scrolls the list to show it.
func (p *picker) clamp(height int) {
	p.cursor = max(min(p.cursor, len(p.matches)-1), 0)
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if h := listHeight(height); p.cursor >= p.offset+h {
		p.offset = p.cursor - h + 1
	}
}

func highlightRunes(match fuzzy.Match) string {
	hit := make(map[int]bool, len(match.MatchedIndexes))
	for _, i := range match.MatchedIndexes {
		hit[i] = true
	}
	var b strings.Builder
	for i, r := range match.Str {
		if hit[i] {
			b.WriteString(hitStyle.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// recentPath is $XDG_STATE_HOME/cloudtail/recent, or ~/.local/state/cloudtail/recent.
func recentPath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "cloudtail", "recent")
}

// loadRecent returns recent groups, most recent first.
func loadRecent() []string {
	path := recentPath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Fields(string(data))
}

// saveRecent puts groups at the front of the recent list. Errors are
// ignored: the list is a convenience.
func saveRecent(groups []string) {
	path := recentPath()
	if path == "" {
		return
	}
	list := append([]string{}, groups...)
	seen := map[string]bool{}
	for _, g := range groups {
		seen[g] = true
	}
	for _, g := range loadRecent() {
		if !seen[g] && len(list) < maxRecent {
			seen[g] = true
			list = append(list, g)
		}
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte(strings.Join(list, "\n")+"\n"), 0o600)
}
