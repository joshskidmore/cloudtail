// Package tui shows streaming log lines in a scrollable, searchable viewer
// with a fixed title bar.
package tui

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/aymanbagabas/go-osc52/v2"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// maxEntries caps how many log entries the viewer keeps in memory.
const maxEntries = 50000

var (
	barStyle     = lipgloss.NewStyle().Reverse(true)
	matchStyle   = lipgloss.NewStyle().Reverse(true)
	currentStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("11"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	// statusStyle is the bottom bar. An AdaptiveColor uses its Light or Dark
	// value by the terminal background, which lipgloss reads from the
	// terminal.
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "236", Dark: "252"}).
			Background(lipgloss.AdaptiveColor{Light: "254", Dark: "236"})
	statusErrStyle = statusStyle.Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "9"})
	// statusNoteStyle is a one-time message at the start of the bar. The
	// rest of the bar stays, so the key hints are always there.
	statusNoteStyle = statusStyle.Bold(true)

	// pausedStyle colors the title bar and the bottom bar while follow mode
	// is off. It sets both the text and the background to fixed colors, so
	// the contrast is the same on light and dark terminals.
	pausedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("232")).
			Background(lipgloss.Color("214"))
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	selStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
)

// Log levels, detected from the text of each entry.
const (
	levelNone = iota
	levelWarn
	levelError
)

var (
	errorPattern = regexp.MustCompile(`\b(ERROR|FATAL|CRITICAL|PANIC)\b|"(level|severity)"\s*:\s*"(?i:error|fatal|critical)"|Task timed out|Traceback \(most recent`)
	warnPattern  = regexp.MustCompile(`\b(WARN|WARNING)\b|"(level|severity)"\s*:\s*"(?i:warn|warning)"`)
)

func levelOf(plain string) int {
	switch {
	case errorPattern.MatchString(plain):
		return levelError
	case warnPattern.MatchString(plain):
		return levelWarn
	}
	return levelNone
}

// entry is one log event.
type entry struct {
	id    uint64 // unique, so the selection survives reordering
	eid   string // event ID from the source, to drop duplicates
	time  int64  // milliseconds since the Unix epoch
	group int    // index into model.groups
	text  string // as received, with color codes
	plain string // without color codes, for search
	level int
	req   string // Lambda request ID, or ""
}

// pane is the scroll position of the row view. The rows live in model.rows,
// so inserting a line does not copy them.
type pane struct {
	YOffset, Width, Height int
	total                  int // number of rows
}

func (p *pane) maxY() int { return max(0, p.total-p.Height) }

func (p *pane) atBottom() bool { return p.YOffset >= p.maxY() }

func (p *pane) setYOffset(n int) { p.YOffset = max(0, min(n, p.maxY())) }

func (p *pane) gotoBottom() { p.YOffset = p.maxY() }

// update scrolls on the page keys and the mouse wheel.
func (p *pane) update(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "pgdown", " ":
			p.setYOffset(p.YOffset + p.Height)
		case "pgup", "b":
			p.setYOffset(p.YOffset - p.Height)
		case "d", "ctrl+d":
			p.setYOffset(p.YOffset + p.Height/2)
		case "u", "ctrl+u":
			p.setYOffset(p.YOffset - p.Height/2)
		}
	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress || msg.Shift {
			return
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			p.setYOffset(p.YOffset - 3)
		case tea.MouseButtonWheelDown:
			p.setYOffset(p.YOffset + 3)
		}
	}
}

type model struct {
	rt      *runner
	startAt func() time.Time // start time for a new set of groups
	header  string           // title override from --header; cleared on group change

	groups     []string
	tagWidth   int             // width of the group tag; 0 with one group
	ranges     []span          // loaded time ranges, oldest first; the last one is live
	seen       map[string]bool // event IDs already loaded
	streamErr  string
	fetching   int       // group fetches still running
	fetchFrom  time.Time // range of the current or last fetch
	fetchTo    time.Time // requested end of that fetch
	fetchUntil time.Time // end that all groups reached
	fetchErr   error

	picking       bool
	pick          picker
	allGroups     []string
	loadingGroups bool
	groupsErr     error

	vp     pane // the scroll position and size of the row view
	input  textinput.Model
	ready  bool
	width  int
	height int

	entries []entry
	nextID  uint64
	shown   []int    // indexes of entries that pass the filters, ascending
	starts  []int    // first wrapped row of each shown entry
	ends    []int    // row after the last wrapped row of each shown entry
	rows    []string // wrapped rows of the shown entries

	openReq map[string]string // group|stream -> request in progress
	reports map[string]report // request ID -> REPORT numbers

	sel int // position in shown of the selected entry, or -1

	inputMode  string // "/" (search), `\` (filter), or "t" (go to time) while typing
	filter     query  // see query.go for the syntax
	filterText string
	reqFilter  string // show only this request ID when set

	query     *regexp.Regexp
	queryText string
	matches   []int // indexes into entries
	cur       int   // index into matches

	detail      bool
	detailEntry entry
	detailText  string
	dvp         viewport.Model

	help     bool      // the key list is open
	errCount int       // error entries in m.entries
	jumpTo   time.Time // go to this time after a fetch is done
	now      time.Time // time for compact display, set per rebuild and batch
	drawnCur int       // entry index drawn as the current match, or -1

	full   bool // show lines as received instead of compact
	follow bool
	notice string // one-time message shown in the status line
}

// Run starts the viewer. With no groups, it opens the group picker first.
// startAt returns the time to start streaming from. header, when not empty,
// replaces the title until the user changes groups.
func Run(src Source, groups []string, startAt func() time.Time, header string) error {
	rt := &runner{src: src}
	m := newModel(rt, startAt, header)
	if len(groups) > 0 {
		m.setGroups(groups)
	} else {
		m.picking, m.loadingGroups = true, true
	}
	// Ask the terminal for its background now. A later query would compete
	// with the program for the terminal's reply on stdin.
	lipgloss.HasDarkBackground()
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	rt.p = p
	_, err := p.Run()
	rt.stop()
	return err
}

func newModel(rt *runner, startAt func() time.Time, header string) model {
	in := textinput.New()
	in.PromptStyle, in.TextStyle = statusStyle, statusStyle
	in.Cursor.Style, in.Cursor.TextStyle = statusStyle, statusStyle
	return model{
		rt:       rt,
		startAt:  startAt,
		header:   header,
		input:    in,
		follow:   true,
		sel:      -1,
		openReq:  map[string]string{},
		reports:  map[string]report{},
		drawnCur: -1,
	}
}

func (m model) Init() tea.Cmd {
	if len(m.groups) > 0 {
		m.rt.start(m.groups, m.ranges[0].from)
		return nil
	}
	return m.rt.loadGroups()
}

// setGroups clears the log and sets the groups to watch. It does not start
// the streams.
func (m *model) setGroups(groups []string) {
	m.groups = groups
	m.tagWidth = 0
	if len(groups) > 1 {
		for _, g := range groups {
			m.tagWidth = max(m.tagWidth, ansi.StringWidth(shortGroup(g)))
		}
	}
	m.ranges = []span{{from: m.startAt(), live: true}}
	m.seen = map[string]bool{}
	m.entries, m.shown, m.starts, m.ends, m.rows, m.matches = nil, nil, nil, nil, nil, nil
	m.openReq, m.reports = map[string]string{}, map[string]report{}
	m.filter, m.filterText, m.reqFilter = nil, "", ""
	m.errCount, m.jumpTo, m.drawnCur = 0, time.Time{}, -1
	m.query, m.queryText, m.cur = nil, "", 0
	m.sel, m.follow, m.detail = -1, true, false
	m.streamErr, m.fetching, m.fetchFrom = "", 0, time.Time{}
	m.rebuild()
}

// switchGroups watches new groups.
func (m *model) switchGroups(groups []string) {
	m.header = ""
	m.setGroups(groups)
	m.rt.start(m.groups, m.ranges[0].from)
}

func (m model) title() string {
	switch {
	case m.picking:
		return "[cloudwatch] pick log groups"
	case m.header != "":
		return m.header
	}
	return "[cloudwatch] " + strings.Join(m.groups, ", ")
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		h := max(msg.Height-2, 1) // title bar and status line
		m.vp.Width, m.vp.Height = msg.Width-1, h
		m.ready = true
		m.rebuild()
		if m.detail {
			m.renderDetail()
		}
		return m, nil

	case groupsMsg:
		m.allGroups, m.groupsErr, m.loadingGroups = msg.names, msg.err, false
		m.pick = newPicker(msg.names)
		return m, nil

	case eventsMsg:
		if msg.gen == m.rt.gen {
			m.streamErr = ""
			m.insert(msg.group, msg.events, msg.history, m.openReq)
		}
		return m, nil

	case fetchMsg:
		if msg.gen == m.rt.gen {
			m.fetchDone(msg)
		}
		return m, nil

	case streamErrMsg:
		if msg.gen == m.rt.gen {
			m.streamErr = msg.group + ": " + msg.err.Error()
		}
		return m, nil
	}

	if m.picking {
		return m.updatePicker(msg)
	}
	if m.help {
		if k, ok := msg.(tea.KeyMsg); ok {
			m.help = false
			if k.String() == "ctrl+c" {
				return m, tea.Quit
			}
		}
		return m, nil
	}
	if m.detail {
		return m.updateDetail(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.inputMode != "" {
			return m.updateInput(msg)
		}
		m.notice = ""
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			m.selectPos(m.sel - 1)
			return m, nil
		case "down", "j":
			m.selectPos(m.sel + 1)
			return m, nil
		case "g", "home":
			m.selectPos(0)
			return m, nil
		case "G", "end":
			m.selectPos(len(m.shown) - 1)
			return m, nil
		case "enter":
			if m.sel >= 0 {
				m.openDetail(m.entries[m.shown[m.sel]])
			}
			return m, nil
		case "y":
			if m.sel >= 0 {
				m.copyText(m.entries[m.shown[m.sel]].plain)
			}
			return m, nil
		case "f":
			m.full = !m.full
			m.rebuild()
			return m, nil
		case "w":
			m.save()
			return m, nil
		case "o":
			m.picking = true
			if m.allGroups == nil && !m.loadingGroups {
				m.loadingGroups = true
				return m, m.rt.loadGroups()
			}
			m.pick = newPicker(m.allGroups)
			return m, nil
		case "[":
			m.extend(-1)
			return m, nil
		case "]":
			m.extend(1)
			return m, nil
		case "?":
			m.help = true
			return m, nil
		case "/", `\`, "t":
			m.inputMode = msg.String()
			m.input.Prompt = m.inputMode
			m.input.SetValue("")
			if m.inputMode == "t" {
				m.input.Prompt = "go to time (15:30, -10m)> "
			} else if m.inputMode == `\` && m.filterText != "" {
				m.input.SetValue(m.filterText) // edit the current filter
				m.input.CursorEnd()
			}
			return m, m.input.Focus()
		case "n":
			m.jump(1)
			return m, nil
		case "N":
			m.jump(-1)
			return m, nil
		case "e":
			m.jumpError(1)
			return m, nil
		case "E":
			m.jumpError(-1)
			return m, nil
		case "r":
			m.toggleRequest()
			return m, nil
		case "esc":
			// Clear the search first, then the text filter, then the request.
			switch {
			case m.query != nil:
				m.setQuery("")
			case m.filterText != "":
				m.setFilter("")
			default:
				m.reqFilter = ""
				m.rebuild()
			}
			return m, nil
		}

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			row := m.vp.YOffset + msg.Y - 1 // row 0 is the title bar
			if msg.Y >= 1 && row < len(m.rows) {
				m.selectPos(m.entryAt(row))
			}
			return m, nil
		}
	}

	if !m.ready {
		return m, nil
	}
	// Page keys and the mouse wheel scroll the view. Keep the selection on
	// screen, and follow new lines when the view reaches the bottom.
	off := m.vp.YOffset
	m.vp.update(msg)
	if m.vp.YOffset == off {
		return m, nil
	}
	m.follow = m.vp.atBottom()
	if m.follow {
		m.sel = len(m.shown) - 1
	} else if m.sel >= 0 {
		if m.starts[m.sel] < m.vp.YOffset {
			m.sel = m.entryAt(m.vp.YOffset)
		} else if bottom := m.vp.YOffset + m.vp.Height - 1; m.starts[m.sel] > bottom {
			m.sel = m.entryAt(bottom)
		}
	}
	return m, nil
}

// updatePicker handles keys while the group picker is open.
func (m model) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.loadingGroups || m.groupsErr != nil {
		if s := key.String(); s == "esc" || s == "ctrl+c" || s == "q" {
			return m.closePicker()
		}
		return m, nil
	}
	var cmd tea.Cmd
	var chosen []string
	var done bool
	m.pick, cmd, chosen, done = m.pick.update(key, m.height-1)
	if !done {
		return m, cmd
	}
	if chosen == nil {
		return m.closePicker()
	}
	m.picking = false
	m.switchGroups(chosen)
	return m, nil
}

// closePicker returns to the log, or quits when no groups were chosen yet.
func (m model) closePicker() (tea.Model, tea.Cmd) {
	if len(m.groups) == 0 {
		return m, tea.Quit
	}
	m.picking = false
	return m, nil
}

// updateInput handles keys while the user types a search or a filter.
func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		mode := m.inputMode
		m.inputMode = ""
		m.input.Blur()
		switch mode {
		case `\`:
			m.setFilter(m.input.Value())
			return m, nil
		case "t":
			t, err := parseJumpTime(m.input.Value(), time.Now())
			if err != nil {
				m.notice = err.Error()
				return m, nil
			}
			m.goToTime(t)
			return m, nil
		}
		m.setQuery(m.input.Value())
		if len(m.matches) > 0 {
			m.cur = len(m.matches) - 1
			m.show(m.matches[m.cur])
		}
		return m, nil
	case "esc", "ctrl+c":
		m.inputMode = ""
		m.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if !m.ready {
		return ""
	}
	style := barStyle
	if !m.follow && !m.picking && !m.help {
		style = pausedStyle
	}
	bar := style.Render(ansi.Truncate(padRight(" "+m.title()+" ", m.width), m.width, ""))
	switch {
	case m.picking:
		return padLines(bar+"\n"+m.pick.view(m.width, m.height-1, m.loadingGroups, m.groupsErr), m.height)
	case m.help:
		return padLines(bar+"\n"+helpView(m.width, m.height-1), m.height)
	case m.detail:
		return bar + "\n" + m.dvp.View() + "\n" + m.detailStatus()
	}
	return bar + "\n" + m.body() + "\n" + m.status()
}

// padLines adds empty lines so that s has h lines. A frame that gets
// shorter while its last line stays the same loses that line: bubbletea
// does not repaint an unchanged line, then erases from it to the end of the
// screen.
func padLines(s string, h int) string {
	if n := h - 1 - strings.Count(s, "\n"); n > 0 {
		return s + strings.Repeat("\n", n)
	}
	return s
}

// body renders the visible rows with a marker beside the selected entry.
func (m model) body() string {
	lines := make([]string, m.vp.Height)
	selFirst, selEnd := -1, -1
	if m.sel >= 0 {
		selFirst, selEnd = m.starts[m.sel], m.ends[m.sel]
	}
	for r := range lines {
		row := m.vp.YOffset + r
		if row >= len(m.rows) {
			break
		}
		gutter := " "
		if row >= selFirst && row < selEnd {
			gutter = selStyle.Render("▌")
		}
		lines[r] = gutter + m.rows[row]
	}
	return strings.Join(lines, "\n")
}

func (m model) status() string {
	if m.inputMode != "" {
		return statusLine(m.input.View(), m.width, statusStyle)
	}
	// While paused, all parts use the paused colors.
	st, errSt := statusStyle, statusErrStyle
	if !m.follow {
		st, errSt = pausedStyle, pausedStyle.Bold(true)
	}
	var parts []string
	if m.notice != "" {
		parts = append(parts, st.Bold(true).Render(m.notice))
	}
	if m.streamErr != "" {
		parts = append(parts, errSt.Render("error "+m.streamErr))
	}
	mode := "FOLLOW"
	if !m.follow {
		mode = "PAUSED · G follow"
	}
	parts = append(parts, st.Bold(!m.follow).Render(mode))
	if m.filterText != "" || m.reqFilter != "" {
		parts = append(parts, st.Render(fmt.Sprintf("%d/%d lines", len(m.shown), len(m.entries))))
	} else {
		parts = append(parts, st.Render(fmt.Sprintf("%d lines", len(m.entries))))
	}
	if m.errCount > 0 {
		parts = append(parts, errSt.Render(fmt.Sprintf("%d err/min · %d err", m.errorsLastMinute(), m.errCount)))
	}
	if m.fetching > 0 {
		parts = append(parts, st.Render("loading…"))
	}
	if m.filterText != "" {
		parts = append(parts, st.Render(`\`+m.filterText))
	}
	if m.reqFilter != "" {
		parts = append(parts, st.Render("only req "+short(m.reqFilter)))
	}
	if m.query != nil {
		pos := 0
		if len(m.matches) > 0 {
			pos = m.cur + 1
		}
		parts = append(parts, st.Render(fmt.Sprintf("/%s %d/%d", m.queryText, pos, len(m.matches))))
	}
	if m.sel >= 0 {
		if id := m.entries[m.shown[m.sel]].req; id != "" {
			s := "req " + short(id)
			if r, ok := m.reports[id]; ok {
				s += " " + r.String()
			}
			parts = append(parts, st.Render(s))
		}
	}
	parts = append(parts, st.Render(`? help · enter detail · / search · \ filter · t time · [ ] load · o groups · q quit`))
	return statusLine(st.Render(" ")+strings.Join(parts, st.Render("  ")), m.width, st)
}

// statusLine cuts or pads styled text to the full width of the bottom bar.
// Each part of s must have the background of st already.
func statusLine(s string, width int, st lipgloss.Style) string {
	if ansi.StringWidth(s) > width {
		s = ansi.Truncate(s, width-1, "") + st.Render("…")
	}
	return s + st.Render(strings.Repeat(" ", max(width-ansi.StringWidth(s), 0)))
}

func (m model) detailStatus() string {
	hint := statusStyle.Render(" DETAIL  esc back · y copy · ↑/↓ scroll")
	if m.notice != "" {
		hint = statusStyle.Render(" ") + statusNoteStyle.Render(m.notice) + statusStyle.Render(" ") + hint
	}
	return statusLine(hint, m.width, statusStyle)
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// passes reports whether entry i passes the text and request filters.
func (m *model) passes(i int) bool {
	e := &m.entries[i]
	if m.filter != nil && !m.filter.match(e.plain) {
		return false
	}
	return m.reqFilter == "" || e.req == m.reqFilter
}

// insert adds events from group gi. open tracks Lambda requests that are in
// progress. Live events go at the end in the order they arrive. History
// goes in time order, which needs a full rebuild when it lands before
// newer entries.
func (m *model) insert(gi int, events []Event, history bool, open map[string]string) {
	if len(events) == 0 {
		return
	}
	add := make([]entry, 0, len(events))
	m.now = time.Now()
	for _, ev := range events {
		if ev.ID != "" {
			if m.seen[ev.ID] {
				continue
			}
			m.seen[ev.ID] = true
		}
		text := sanitize(ev.Text)
		plain := ansi.Strip(text)
		m.nextID++
		add = append(add, entry{
			id:    m.nextID,
			eid:   ev.ID,
			time:  ev.Time,
			group: gi,
			text:  text,
			plain: plain,
			level: levelOf(plain),
			req:   m.track(plain, gi, open),
		})
		if add[len(add)-1].level == levelError {
			m.errCount++
		}
	}
	if len(add) == 0 {
		return
	}
	sort.SliceStable(add, func(a, b int) bool { return add[a].time < add[b].time })

	n := len(m.entries)
	inOrder := n == 0 || !history || add[0].time >= m.entries[n-1].time
	if inOrder && n+len(add) <= maxEntries {
		m.entries = append(m.entries, add...)
		if !m.ready {
			return
		}
		for i := n; i < len(m.entries); i++ {
			if !m.passes(i) {
				continue
			}
			m.shown = append(m.shown, i)
			matched := m.query != nil && m.query.MatchString(m.entries[i].plain)
			if matched {
				m.matches = append(m.matches, i)
			}
			m.appendRows(i, matched)
		}
		m.refresh()
		return
	}

	id := m.selectedID()
	if inOrder {
		m.entries = append(m.entries, add...)
	} else {
		m.entries = mergeByTime(m.entries, add)
	}
	m.trim()
	m.rebuildKeep(id)
}

// sanitize removes what could make the terminal do more than show text: it
// keeps color codes (ESC [ digits/semicolons m), newlines, and tabs, and
// drops every other escape sequence and control character, including the
// C1 controls U+0080 to U+009F.
func sanitize(s string) string {
	i := 0
	for ; i < len(s); i++ {
		if c := s[i]; c < 0x20 && c != '\n' && c != '\t' || c == 0x7f || c == 0xc2 && i+1 < len(s) && s[i+1] >= 0x80 && s[i+1] <= 0x9f {
			break
		}
	}
	if i == len(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:i])
	for i < len(s) {
		c := s[i]
		switch {
		case c == 0x1b:
			i = skipEscape(s, i, &b)
			continue
		case c == 0xc2 && i+1 < len(s) && s[i+1] >= 0x80 && s[i+1] <= 0x9f:
			i += 2
			continue
		case c < 0x20 && c != '\n' && c != '\t', c == 0x7f:
			i++
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// skipEscape handles the escape sequence that starts at s[i]. It writes the
// sequence to b when it is a color code, and returns the index after it.
func skipEscape(s string, i int, b *strings.Builder) int {
	start := i
	i++
	if i >= len(s) {
		return i
	}
	switch c := s[i]; {
	case c == '[':
		i++
		color := true
		for i < len(s) && s[i] >= 0x30 && s[i] <= 0x3f {
			if s[i] != ';' && (s[i] < '0' || s[i] > '9') {
				color = false
			}
			i++
		}
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
			color = false
			i++
		}
		if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7e {
			i++
			if color && s[i-1] == 'm' {
				b.WriteString(s[start:i])
			}
		}
		return i
	case c == ']' || c == 'P' || c == 'X' || c == '^' || c == '_':
		// A string sequence ends with BEL or ESC \.
		for i++; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return i
	case c >= 0x20 && c <= 0x2f:
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
			i++
		}
		if i < len(s) && s[i] >= 0x30 && s[i] <= 0x7e {
			i++
		}
		return i
	case c >= 0x30 && c <= 0x7e:
		return i + 1
	}
	return i
}

// mergeByTime merges two lists sorted by time. On equal times, entries from
// a come first.
func mergeByTime(a, b []entry) []entry {
	out := make([]entry, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if b[j].time < a[i].time {
			out = append(out, b[j])
			j++
		} else {
			out = append(out, a[i])
			i++
		}
	}
	out = append(out, a[i:]...)
	return append(out, b[j:]...)
}

// setQuery sets a case-insensitive literal search and recomputes matches.
func (m *model) setQuery(q string) {
	m.query, m.queryText = literal(q), q
	m.cur = 0
	m.rebuild()
}

// setFilter sets the filter (see query.go for the syntax). An empty f
// shows all entries. On a syntax error it keeps the current filter.
func (m *model) setFilter(f string) {
	q, err := parseQuery(f)
	if err != nil {
		m.notice = "filter: " + err.Error()
		return
	}
	m.filter, m.filterText = q, strings.TrimSpace(f)
	m.cur = 0
	m.rebuild()
}

// toggleRequest shows only the selected entry's Lambda request, or shows
// all requests again.
func (m *model) toggleRequest() {
	if m.reqFilter != "" {
		m.reqFilter = ""
		m.rebuild()
		return
	}
	if m.sel < 0 || m.entries[m.shown[m.sel]].req == "" {
		m.notice = "no request ID on this line"
		return
	}
	m.reqFilter = m.entries[m.shown[m.sel]].req
	m.cur = 0
	m.rebuild()
}

// literal returns a case-insensitive regexp for s, or nil when s is empty.
func literal(s string) *regexp.Regexp {
	if s == "" {
		return nil
	}
	return regexp.MustCompile("(?i)" + regexp.QuoteMeta(s))
}

// selectedID returns the id of the selected entry, or 0.
func (m *model) selectedID() uint64 {
	if m.sel >= 0 && m.sel < len(m.shown) && m.shown[m.sel] < len(m.entries) {
		return m.entries[m.shown[m.sel]].id
	}
	return 0
}

// rebuild recomputes shown entries, matches, and all wrapped rows, for
// example after a resize, a new search, or a new filter.
func (m *model) rebuild() {
	m.rebuildKeep(m.selectedID())
}

// rebuildKeep rebuilds and selects the entry with the given id when it is
// still shown, or else the newest entry.
func (m *model) rebuildKeep(id uint64) {
	if !m.ready {
		return
	}
	m.now = time.Now()
	m.drawnCur = -1
	m.shown = m.shown[:0]
	m.matches = m.matches[:0]
	matched := make([]bool, len(m.entries))
	for i := range m.entries {
		if !m.passes(i) {
			continue
		}
		m.shown = append(m.shown, i)
		if m.query != nil && m.query.MatchString(m.entries[i].plain) {
			m.matches = append(m.matches, i)
			matched[i] = true
		}
	}
	if m.cur >= len(m.matches) {
		m.cur = 0
	}
	m.rows = m.rows[:0]
	m.starts = m.starts[:0]
	m.ends = m.ends[:0]
	m.sel = len(m.shown) - 1
	for pos, i := range m.shown {
		m.appendRows(i, matched[i])
		if id != 0 && m.entries[i].id == id {
			m.sel = pos
		}
	}
	m.refresh()
	if !m.follow {
		m.scrollTo(m.sel)
	}
}

// entryRows returns the wrapped rows of entry i. Error and warning entries
// show in red and yellow. Search matches show highlighted. matched says
// whether the search matches the entry.
func (m *model) entryRows(i int, matched bool) []string {
	text, plain := m.display(m.entries[i])
	base := lipgloss.NewStyle()
	switch m.entries[i].level {
	case levelError:
		base = errorStyle
	case levelWarn:
		base = warnStyle
	}
	if matched {
		style := matchStyle
		if len(m.matches) > 0 && m.matches[m.cur] == i {
			style = currentStyle
			m.drawnCur = i
		}
		text = highlight(plain, m.query, style, base)
	} else if m.entries[i].level != levelNone {
		text = paint(base, plain)
	}
	if m.vp.Width > 0 {
		text = ansi.Hardwrap(text, m.vp.Width, true)
	}
	return strings.Split(text, "\n")
}

// appendRows wraps entry i and adds its rows to the view.
func (m *model) appendRows(i int, matched bool) {
	rows := m.entryRows(i, matched)
	// A gap row goes before the first entry of a new range.
	if pos := len(m.starts); pos > 0 {
		a := m.rangeAt(m.entries[m.shown[pos-1]].time)
		b := m.rangeAt(m.entries[i].time)
		if a >= 0 && b > a {
			m.rows = append(m.rows, m.gapRow(a, b))
		}
	}
	m.starts = append(m.starts, len(m.rows))
	m.rows = append(m.rows, rows...)
	m.ends = append(m.ends, len(m.rows))
}

// refresh updates the scroll position for the current rows. In follow mode it shows the
// bottom and selects the newest entry.
func (m *model) refresh() {
	off := m.vp.YOffset
	m.vp.total = len(m.rows)
	if m.follow {
		m.vp.gotoBottom()
		m.sel = len(m.shown) - 1
	} else {
		m.vp.setYOffset(off)
	}
}

// selectPos selects shown entry pos and scrolls it into view. Selecting the
// newest entry turns follow mode on; any other entry pauses it.
func (m *model) selectPos(pos int) {
	if len(m.shown) == 0 {
		return
	}
	pos = max(min(pos, len(m.shown)-1), 0)
	m.sel = pos
	m.follow = pos == len(m.shown)-1
	if m.follow {
		m.vp.gotoBottom()
		return
	}
	m.scrollTo(pos)
}

// scrollTo moves the view the least amount that shows all of entry pos, or
// its first rows when it is taller than the view.
func (m *model) scrollTo(pos int) {
	if pos < 0 || pos >= len(m.starts) {
		return
	}
	first, end := m.starts[pos], m.ends[pos]
	switch {
	case first < m.vp.YOffset:
		m.vp.setYOffset(first)
	case end > m.vp.YOffset+m.vp.Height:
		m.vp.setYOffset(min(first, end-m.vp.Height))
	}
}

// jump moves to the next (dir 1) or previous (dir -1) match, wrapping around.
func (m *model) jump(dir int) {
	if len(m.matches) == 0 {
		return
	}
	m.cur = (m.cur + dir + len(m.matches)) % len(m.matches)
	m.show(m.matches[m.cur])
}

// jumpError selects the next (dir 1) or previous (dir -1) error entry,
// counted from the selected entry.
func (m *model) jumpError(dir int) {
	for pos := m.sel + dir; pos >= 0 && pos < len(m.shown); pos += dir {
		if i := m.shown[pos]; m.entries[i].level == levelError {
			m.show(i)
			return
		}
	}
	m.notice = "no more errors"
}

// show selects entry i, puts it at the top of the view, and pauses follow
// mode. It redraws only the entries whose current match highlight changes.
func (m *model) show(i int) {
	m.follow = false
	pos := sort.SearchInts(m.shown, i)
	if pos >= len(m.shown) || m.shown[pos] != i {
		m.rebuildKeep(m.entries[i].id)
	} else {
		m.sel = pos
		if m.query != nil && len(m.matches) > 0 && m.drawnCur != m.matches[m.cur] {
			old := m.drawnCur
			m.redraw(m.matches[m.cur])
			m.redraw(old)
		}
	}
	if m.sel >= 0 {
		m.vp.setYOffset(m.starts[m.sel])
	}
}

// redraw renders the rows of matching entry i again, in place. The highlight
// changes no text, so the row count stays the same.
func (m *model) redraw(i int) {
	pos := sort.SearchInts(m.shown, i)
	if i < 0 || pos >= len(m.shown) || m.shown[pos] != i {
		return
	}
	rows := m.entryRows(i, true)
	if len(rows) != m.ends[pos]-m.starts[pos] {
		m.rebuild()
		return
	}
	copy(m.rows[m.starts[pos]:], rows)
}

// copyText puts text on the clipboard with an OSC 52 escape code. This works
// in local terminals, over SSH, and in tmux with set-clipboard on.
func (m *model) copyText(text string) {
	if _, err := osc52.New(text).WriteTo(os.Stdout); err != nil {
		m.notice = "copy failed: " + err.Error()
		return
	}
	m.notice = "copied to clipboard"
}

// save writes the shown entries, without color codes, to a file in the
// current directory. With a filter active, it saves only matching entries.
func (m *model) save() {
	name := strings.Trim(unsafeChars.ReplaceAllString(m.title(), "_"), "_")
	if len(name) > 80 {
		name = name[:80]
	}
	path := fmt.Sprintf("cloudtail-%s-%s.log", name, time.Now().Format("20060102-150405"))
	var b strings.Builder
	for _, i := range m.shown {
		if m.tagWidth > 0 {
			b.WriteString(m.groups[m.entries[i].group] + " ")
		}
		b.WriteString(m.entries[i].plain + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		m.notice = "save failed: " + err.Error()
		return
	}
	m.notice = fmt.Sprintf("saved %d lines to %s", len(m.shown), path)
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// entryAt returns the position in m.shown of the entry that contains
// wrapped row r.
func (m *model) entryAt(r int) int {
	pos := sort.Search(len(m.starts), func(p int) bool { return m.starts[p] > r }) - 1
	return max(min(pos, len(m.shown)-1), 0)
}

// highlight renders s in the base style, with each match of re in style.
func highlight(s string, re *regexp.Regexp, style, base lipgloss.Style) string {
	var b strings.Builder
	last := 0
	for _, loc := range re.FindAllStringIndex(s, -1) {
		b.WriteString(paint(base, s[last:loc[0]]))
		b.WriteString(paint(style, s[loc[0]:loc[1]]))
		last = loc[1]
	}
	b.WriteString(paint(base, s[last:]))
	return b.String()
}

// paint renders each line of s on its own. lipgloss pads multi-line text to
// one width, which would add blank wrapped rows.
func paint(style lipgloss.Style, s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = style.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

func padRight(s string, w int) string {
	if n := w - ansi.StringWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}
