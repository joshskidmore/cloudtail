package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func testModel(t testing.TB) model {
	t.Helper()
	m := newModel(&runner{}, time.Now, "")
	m.setGroups([]string{"g"})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return tm.(model)
}

func line(i int, msg string) Event {
	ts := time.Now().Add(time.Duration(i) * time.Millisecond)
	return Event{
		ID:   fmt.Sprint("e", i),
		Time: ts.UnixMilli(),
		Text: fmt.Sprintf("[%s] (stream) %s", ts.UTC().Format(time.RFC3339), msg),
	}
}

func TestSanitize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"a\x1b]52;c;aGk=\x07b", "ab"},
		{"a\x1b]52;c;aGk=\x1b\\b", "ab"},
		{"a\x1b]0;title\x07b", "ab"},
		{"a\x1bPdata\x1b\\b", "ab"},
		{"a\x1b_apc\x07b", "ab"},
		{"a\x1b[2Jb\x1b[10;5Hc\x1b[?25ld", "abcd"},
		{"a\rb\bc\x07d\x7fe", "abcde"},
		{"a\x1b\nb", "a\nb"},
		{"a\x1b", "a"},
		{"a\u0085b\u009bc", "abc"},
		{"\x1b[31;1mred\x1b[0m ok\tx\ny", "\x1b[31;1mred\x1b[0m ok\tx\ny"},
		{"\x1b[38;2;1;2;3mx\x1b[0m", "\x1b[38;2;1;2;3mx\x1b[0m"},
		{"héllo wörld ✓ 日本", "héllo wörld ✓ 日本"},
		{"\x1b[31x", ""},
	}
	for _, c := range cases {
		if got := sanitize(c.in); got != c.want {
			t.Errorf("sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestInsertSanitizes(t *testing.T) {
	m := testModel(t)
	m.insert(0, []Event{line(0, "hi \x1b]52;c;aGk=\x07 \x1b]0;pwned\x1b\\ \x1b[2J \rx \x1b[32mgreen\x1b[0m")}, false, m.openReq)
	for _, r := range m.entries[0].plain {
		if r < 0x20 && r != '\n' && r != '\t' {
			t.Fatalf("control character in plain: %q", m.entries[0].plain)
		}
	}
	if !strings.Contains(m.entries[0].text, "\x1b[32mgreen") {
		t.Errorf("color lost: %q", m.entries[0].text)
	}
	if v := m.View(); strings.Contains(v, "\x1b]") || strings.Contains(v, "\x1b[2J") || strings.Contains(v, "\r") {
		t.Errorf("view has control sequences: %q", v)
	}
}

func TestInsertCap(t *testing.T) {
	m := testModel(t)
	n := 0
	for b := 0; b < 12; b++ {
		var evs []Event
		for j := 0; j < 5000; j++ {
			n++
			evs = append(evs, line(n, fmt.Sprintf("ERROR boom RequestId: %08d-0000-0000-0000-000000000000", n)))
		}
		m.insert(0, evs, false, m.openReq)
		if len(m.entries) > maxEntries {
			t.Fatalf("batch %d: %d entries", b, len(m.entries))
		}
	}
	if len(m.entries) < maxEntries*8/10 {
		t.Errorf("trimmed too much: %d", len(m.entries))
	}
	if len(m.seen) != len(m.entries) || m.errCount != len(m.entries) {
		t.Errorf("seen %d errCount %d entries %d", len(m.seen), m.errCount, len(m.entries))
	}
	for _, e := range m.entries {
		if !m.seen[e.eid] {
			t.Fatal("kept entry not in seen")
		}
	}
	if len(m.reports) > len(m.entries) || len(m.openReq) > len(m.entries) {
		t.Errorf("reports %d openReq %d", len(m.reports), len(m.openReq))
	}
}

func TestReportsPruned(t *testing.T) {
	m := testModel(t)
	var evs []Event
	for i := 0; i < maxEntries+10; i++ {
		id := fmt.Sprintf("%08d-0000-0000-0000-000000000000", i)
		txt := "REPORT RequestId: " + id + "\tDuration: 1.00 ms\tBilled Duration: 2 ms\tMemory Size: 128 MB\tMax Memory Used: 64 MB"
		evs = append(evs, line(i, txt))
	}
	m.insert(0, evs, false, m.openReq)
	if len(m.reports) != len(m.entries) {
		t.Errorf("reports %d, entries %d", len(m.reports), len(m.entries))
	}
}

func rowsState(m model) ([]string, []int, []int) {
	return slices.Clone(m.rows), slices.Clone(m.starts), slices.Clone(m.ends)
}

func TestJumpMatchesRebuild(t *testing.T) {
	m := testModel(t)
	var evs []Event
	for i := 0; i < 300; i++ {
		msg := fmt.Sprintf("line %d needle", i)
		if i%7 == 0 {
			msg = fmt.Sprintf("ERROR line %d needle %s", i, strings.Repeat("wide ", 40))
		}
		evs = append(evs, line(i, msg))
	}
	m.insert(0, evs, false, m.openReq)
	m.setQuery("needle")
	m.cur = len(m.matches) - 1
	m.show(m.matches[m.cur])
	for step, dir := range []int{1, 1, -1, -1, -1, 1} {
		m.jump(dir)
		rows, starts, ends := rowsState(m)
		sel, cur := m.sel, m.cur
		m.rebuild()
		if !slices.Equal(rows, m.rows) || !slices.Equal(starts, m.starts) || !slices.Equal(ends, m.ends) {
			t.Fatalf("step %d: rows differ from full rebuild", step)
		}
		if m.sel != sel || m.cur != cur {
			t.Fatalf("step %d: sel %d/%d cur %d/%d", step, sel, m.sel, cur, m.cur)
		}
	}
	m.jumpError(1)
	rows, _, _ := rowsState(m)
	m.rebuild()
	if !slices.Equal(rows, m.rows) {
		t.Fatal("jumpError rows differ from full rebuild")
	}
}

func TestStreamErrCleared(t *testing.T) {
	m := testModel(t)
	m.streamErr = "g: boom"
	tm, _ := m.Update(eventsMsg{gen: m.rt.gen, events: []Event{line(1, "x")}})
	if tm.(model).streamErr != "" {
		t.Error("stream error not cleared")
	}
}

func bigModel(t testing.TB) model {
	m := testModel(t)
	for b := 0; b < 10; b++ {
		var evs []Event
		for j := 0; j < 4999; j++ {
			i := b*4999 + j
			evs = append(evs, line(i, fmt.Sprintf("message %d needle", i)))
		}
		m.insert(0, evs, false, m.openReq)
	}
	return m
}

func TestTimingAtScale(t *testing.T) {
	m := bigModel(t)
	m.setQuery("needle")
	start := time.Now()
	m.insert(0, []Event{line(1_000_000, "one more needle")}, false, m.openReq)
	live := time.Since(start)
	start = time.Now()
	m.jump(-1)
	jump := time.Since(start)
	t.Logf("%d entries: live insert %v, n jump %v", len(m.entries), live, jump)
	if live > 20*time.Millisecond || jump > 20*time.Millisecond {
		t.Errorf("too slow: insert %v, jump %v", live, jump)
	}
}

func BenchmarkLiveInsert(b *testing.B) {
	m := bigModel(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.insert(0, []Event{line(2_000_000+i, "live")}, false, m.openReq)
	}
}

func BenchmarkJump(b *testing.B) {
	m := bigModel(b)
	m.setQuery("needle")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.jump(1)
	}
}

// The picker and help frames must fill the screen. A shorter frame whose
// last line did not change loses that line in the bubbletea renderer.
func TestViewFillsHeight(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := newModel(&runner{}, time.Now, "")
	m.picking = true
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.Update(groupsMsg{names: []string{"prod-api", "a", "b", "c"}})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("prod-api")})
	v := tm.View()
	if got := strings.Count(v, "\n") + 1; got != 30 {
		t.Errorf("picker frame has %d lines, want 30", got)
	}
	if !strings.Contains(v, "1/4") || strings.Count(v, "prod-api") != 2 {
		t.Errorf("filtered picker does not show the match:\n%s", v)
	}
	hm := tm.(model)
	hm.picking, hm.help = false, true
	if got := strings.Count(hm.View(), "\n") + 1; got != 30 {
		t.Errorf("help frame has %d lines, want 30", got)
	}
}

// The bottom bar must fill the full width in each mode.
func TestStatusFillsWidth(t *testing.T) {
	m := newModel(&runner{}, time.Now, "")
	m.setGroups([]string{"g"})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = tm.(model)
	m.streamErr = "g: " + strings.Repeat("x", 100)
	check := func(name, s string) {
		if w := ansi.StringWidth(s); w != 60 {
			t.Errorf("%s: width %d, want 60", name, w)
		}
	}
	check("status", m.status())
	check("detail", m.detailStatus())
	m.notice = "saved"
	check("notice", m.status())
	m.inputMode = "/"
	m.input.SetValue("abc")
	check("input", m.status())
}

// While paused, the title bar and the bottom bar use the paused colors over
// their full width. The colors are fixed, so they do not depend on the
// terminal background.
func TestPausedBars(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(old)
	for _, dark := range []bool{true, false} {
		lipgloss.SetHasDarkBackground(dark)
		m := testModel(t)
		now := time.Now().UnixMilli()
		m.insert(0, []Event{{ID: "1", Time: now, Text: "a"}, {ID: "2", Time: now + 1, Text: "ERROR b"}}, false, m.openReq)
		const paused = "38;5;232;48;5;214"
		if strings.Contains(m.View(), paused) {
			t.Errorf("dark=%v: follow mode shows the paused colors", dark)
		}
		m.selectPos(0)
		if m.follow {
			t.Fatal("selecting an older entry did not pause")
		}
		lines := strings.Split(m.View(), "\n")
		for name, l := range map[string]string{"title": lines[0], "status": lines[len(lines)-1]} {
			if !strings.Contains(l, paused) {
				t.Errorf("dark=%v: %s bar has no paused colors: %q", dark, name, l)
			}
			if w := ansi.StringWidth(l); w != m.width {
				t.Errorf("dark=%v: %s bar width %d, want %d", dark, name, w, m.width)
			}
		}
		// Every colored part of the bottom bar must set the paused background.
		for _, part := range strings.Split(lines[len(lines)-1], "\x1b[0m") {
			if part != "" && !strings.Contains(part, "48;5;214") {
				t.Errorf("dark=%v: status part without the paused background: %q", dark, part)
			}
		}
		if !strings.Contains(ansi.Strip(lines[len(lines)-1]), "PAUSED") {
			t.Errorf("dark=%v: status does not say PAUSED", dark)
		}
	}
}
