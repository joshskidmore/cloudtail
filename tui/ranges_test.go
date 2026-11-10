package tui

import (
	"fmt"
	"testing"
	"time"
)

func ms(n int64) time.Time { return time.UnixMilli(1_700_000_000_000 + n*1000) }

func rangeModel() *model {
	m := newModel(&runner{}, time.Now, "")
	m.setGroups([]string{"g"})
	m.ranges = nil
	return &m
}

func fmtRanges(rs []span) string {
	s := ""
	for _, r := range rs {
		if r.live {
			s += fmt.Sprintf("[%d,live) ", (r.from.UnixMilli()-1_700_000_000_000)/1000)
		} else {
			s += fmt.Sprintf("[%d,%d) ", (r.from.UnixMilli()-1_700_000_000_000)/1000, (r.to.UnixMilli()-1_700_000_000_000)/1000)
		}
	}
	return s
}

func TestAddRange(t *testing.T) {
	type rg struct {
		from, to int64
		live     bool
	}
	tests := []struct {
		name  string
		start []rg
		add   [2]int64
		want  []rg
	}{
		{"into empty", nil, [2]int64{10, 20}, []rg{{10, 20, false}}},
		{"disjoint after", []rg{{10, 20, false}}, [2]int64{30, 40}, []rg{{10, 20, false}, {30, 40, false}}},
		{"disjoint before", []rg{{30, 40, false}}, [2]int64{10, 20}, []rg{{10, 20, false}, {30, 40, false}}},
		{"overlap end", []rg{{10, 20, false}}, [2]int64{15, 30}, []rg{{10, 30, false}}},
		{"overlap start", []rg{{10, 20, false}}, [2]int64{5, 15}, []rg{{5, 20, false}}},
		{"touching", []rg{{10, 20, false}}, [2]int64{20, 30}, []rg{{10, 30, false}}},
		{"touching before", []rg{{10, 20, false}}, [2]int64{0, 10}, []rg{{0, 20, false}}},
		{"contained", []rg{{10, 40, false}}, [2]int64{20, 30}, []rg{{10, 40, false}}},
		{"contains existing", []rg{{20, 30, false}}, [2]int64{10, 40}, []rg{{10, 40, false}}},
		{"bridges two", []rg{{10, 20, false}, {30, 40, false}}, [2]int64{15, 35}, []rg{{10, 40, false}}},
		{"into live overlap", []rg{{50, 0, true}}, [2]int64{40, 60}, []rg{{40, 0, true}}},
		{"into live touching", []rg{{50, 0, true}}, [2]int64{40, 50}, []rg{{40, 0, true}}},
		{"into live inside", []rg{{50, 0, true}}, [2]int64{60, 70}, []rg{{50, 0, true}}},
		{"before live disjoint", []rg{{50, 0, true}}, [2]int64{10, 20}, []rg{{10, 20, false}, {50, 0, true}}},
		{"merge window and live", []rg{{10, 20, false}, {50, 0, true}}, [2]int64{15, 55}, []rg{{10, 0, true}}},
		{"empty ignored", []rg{{10, 20, false}}, [2]int64{30, 30}, []rg{{10, 20, false}}},
		{"inverted ignored", []rg{{10, 20, false}}, [2]int64{40, 30}, []rg{{10, 20, false}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := rangeModel()
			for _, r := range tt.start {
				s := span{from: ms(r.from), live: r.live}
				if !r.live {
					s.to = ms(r.to)
				}
				m.ranges = append(m.ranges, s)
			}
			m.addRange(ms(tt.add[0]), ms(tt.add[1]))
			var want []span
			for _, r := range tt.want {
				s := span{from: ms(r.from), live: r.live}
				if !r.live {
					s.to = ms(r.to)
				}
				want = append(want, s)
			}
			if fmtRanges(m.ranges) != fmtRanges(want) {
				t.Errorf("ranges = %s, want %s", fmtRanges(m.ranges), fmtRanges(want))
			}
		})
	}
}

func TestSpanContains(t *testing.T) {
	w := span{from: ms(10), to: ms(20)}
	live := span{from: ms(10), live: true}
	tests := []struct {
		name string
		s    span
		at   int64
		want bool
	}{
		{"before from", w, 9999, false},
		{"from inclusive", w, 10000, true},
		{"inside", w, 15000, true},
		{"just before to", w, 19999, true},
		{"to exclusive", w, 20000, false},
		{"after to", w, 30000, false},
		{"live before from", live, 9999, false},
		{"live from inclusive", live, 10000, true},
		{"live far future", live, 1 << 40, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.contains(ms(0).UnixMilli() + tt.at); got != tt.want {
				t.Errorf("contains = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRangeAt(t *testing.T) {
	m := rangeModel()
	m.ranges = []span{
		{from: ms(10), to: ms(20)},
		{from: ms(30), to: ms(40)},
		{from: ms(50), live: true},
	}
	at := func(s int64) int64 { return ms(s).UnixMilli() }
	tests := []struct {
		at   int64
		want int
	}{
		{at(5), -1}, {at(10), 0}, {at(19), 0}, {at(20), -1}, {at(25), -1},
		{at(30), 1}, {at(39), 1}, {at(40), -1}, {at(50), 2}, {at(5000), 2},
	}
	for _, tt := range tests {
		if got := m.rangeAt(tt.at); got != tt.want {
			t.Errorf("rangeAt(%d) = %d, want %d", tt.at-ms(0).UnixMilli(), got, tt.want)
		}
	}
	m.ranges = nil
	if got := m.rangeAt(at(10)); got != -1 {
		t.Errorf("rangeAt with no ranges = %d, want -1", got)
	}
}

func TestMergeByTime(t *testing.T) {
	e := func(id uint64, time int64) entry { return entry{id: id, time: time} }
	ids := func(es []entry) []uint64 {
		var out []uint64
		for _, x := range es {
			out = append(out, x.id)
		}
		return out
	}
	tests := []struct {
		name string
		a, b []entry
		want []uint64
	}{
		{"both empty", nil, nil, nil},
		{"a empty", nil, []entry{e(1, 1), e(2, 2)}, []uint64{1, 2}},
		{"b empty", []entry{e(1, 1), e(2, 2)}, nil, []uint64{1, 2}},
		{"interleaved", []entry{e(1, 1), e(3, 3)}, []entry{e(2, 2), e(4, 4)}, []uint64{1, 2, 3, 4}},
		{"b before a", []entry{e(3, 3), e(4, 4)}, []entry{e(1, 1), e(2, 2)}, []uint64{1, 2, 3, 4}},
		{"equal times a first", []entry{e(1, 5), e(2, 5)}, []entry{e(3, 5), e(4, 5)}, []uint64{1, 2, 3, 4}},
		{"equal mixed", []entry{e(1, 1), e(2, 5)}, []entry{e(3, 5), e(4, 6)}, []uint64{1, 2, 3, 4}},
		{"stable within list", []entry{e(1, 2), e(2, 2), e(3, 2)}, []entry{e(4, 1), e(5, 1)}, []uint64{4, 5, 1, 2, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ids(mergeByTime(tt.a, tt.b))
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("mergeByTime ids = %v, want %v", got, tt.want)
			}
		})
	}
}

// fill appends n entries with times in [from, to) milliseconds, marks every
// tenth as an error, and records them in m.seen and m.errCount.
func fill(m *model, name string, n int, from int64) {
	for i := 0; i < n; i++ {
		e := entry{id: uint64(len(m.entries)), eid: fmt.Sprintf("%s-%d", name, i), time: from + int64(i)}
		if i%10 == 0 {
			e.level = levelError
			m.errCount++
		}
		m.entries = append(m.entries, e)
		m.seen[e.eid] = true
	}
}

func checkConsistent(t *testing.T, m *model) {
	t.Helper()
	errs := 0
	seen := map[string]bool{}
	for _, e := range m.entries {
		seen[e.eid] = true
		if e.level == levelError {
			errs++
		}
	}
	if errs != m.errCount {
		t.Errorf("errCount = %d, entries hold %d errors", m.errCount, errs)
	}
	if len(seen) != len(m.seen) {
		t.Errorf("len(seen) = %d, entries have %d ids", len(m.seen), len(seen))
	}
	for id := range m.seen {
		if !seen[id] {
			t.Errorf("seen has %q with no entry", id)
			break
		}
	}
}

func entriesIn(m *model, name string) int {
	n := 0
	for _, e := range m.entries {
		if len(e.eid) > len(name) && e.eid[:len(name)+1] == name+"-" {
			n++
		}
	}
	return n
}

const base = int64(1_700_000_000_000)

func TestKeep(t *testing.T) {
	m := rangeModel()
	fill(m, "a", 20, base)
	wantErrs := m.errCount
	m.keep(func(e *entry) bool { return true })
	if len(m.entries) != 20 || m.errCount != wantErrs || len(m.seen) != 20 {
		t.Errorf("keep all changed state: %d entries, %d errs, %d seen", len(m.entries), m.errCount, len(m.seen))
	}
	m.keep(func(e *entry) bool { return e.time >= base+10 })
	if len(m.entries) != 10 {
		t.Fatalf("len(entries) = %d, want 10", len(m.entries))
	}
	if m.errCount != 1 {
		t.Errorf("errCount = %d, want 1 (errors were at 0 and 10; only 10 is kept)", m.errCount)
	}
	checkConsistent(t, m)
}

func TestTrimUnderLimit(t *testing.T) {
	m := rangeModel()
	m.ranges = []span{{from: time.UnixMilli(base), live: true}}
	fill(m, "a", 100, base)
	m.trim()
	if len(m.entries) != 100 {
		t.Errorf("len(entries) = %d, want 100", len(m.entries))
	}
	checkConsistent(t, m)
}

func TestTrimDropsOldestWindowFirst(t *testing.T) {
	m := rangeModel()
	// window A, window B (last loaded), window C, live.
	a0, b0, c0, l0 := base, base+100_000, base+200_000, base+300_000
	m.ranges = []span{
		{from: time.UnixMilli(a0), to: time.UnixMilli(a0 + 100_000)},
		{from: time.UnixMilli(b0), to: time.UnixMilli(b0 + 100_000)},
		{from: time.UnixMilli(c0), to: time.UnixMilli(c0 + 100_000)},
		{from: time.UnixMilli(l0), live: true},
	}
	m.fetchFrom = time.UnixMilli(c0)
	fill(m, "a", 15000, a0)
	fill(m, "b", 15000, b0)
	fill(m, "c", 15000, c0)
	fill(m, "l", 10000, l0)
	m.trim()
	if len(m.entries) > maxEntries {
		t.Fatalf("len(entries) = %d, want <= %d", len(m.entries), maxEntries)
	}
	if n := entriesIn(m, "a"); n != 0 {
		t.Errorf("oldest window kept %d entries, want 0", n)
	}
	if n := entriesIn(m, "b"); n != 15000 {
		t.Errorf("second window has %d entries, want 15000 (only the oldest window should drop)", n)
	}
	if n := entriesIn(m, "c"); n != 15000 {
		t.Errorf("last loaded window has %d entries, want 15000", n)
	}
	if n := entriesIn(m, "l"); n != 10000 {
		t.Errorf("live has %d entries, want 10000", n)
	}
	if len(m.ranges) != 3 || m.ranges[0].from.UnixMilli() != b0 {
		t.Errorf("ranges after trim = %s, want window A removed", fmtRanges(m.ranges))
	}
	checkConsistent(t, m)
}

func TestTrimKeepsLastLoadedThenCutsLive(t *testing.T) {
	m := rangeModel()
	a0, b0, l0 := base, base+100_000, base+200_000
	m.ranges = []span{
		{from: time.UnixMilli(a0), to: time.UnixMilli(a0 + 100_000)},
		{from: time.UnixMilli(b0), to: time.UnixMilli(b0 + 100_000)},
		{from: time.UnixMilli(l0), live: true},
	}
	m.fetchFrom = time.UnixMilli(b0)
	fill(m, "a", 10000, a0)
	fill(m, "b", 30000, b0)
	fill(m, "l", 30000, l0)
	m.trim()
	if len(m.entries) > maxEntries {
		t.Fatalf("len(entries) = %d, want <= %d", len(m.entries), maxEntries)
	}
	if n := entriesIn(m, "a"); n != 0 {
		t.Errorf("window A kept %d entries, want 0", n)
	}
	if n := entriesIn(m, "b"); n != 30000 {
		t.Errorf("last loaded window has %d entries, want 30000", n)
	}
	n := entriesIn(m, "l")
	if n == 0 || n >= 30000 {
		t.Errorf("live has %d entries, want 0 < n < 30000", n)
	}
	// the oldest live entries go, the newest stay
	for _, e := range m.entries {
		if e.eid == "l-29999" {
			goto newest
		}
	}
	t.Error("newest live entry was dropped")
newest:
	for _, e := range m.entries {
		if e.eid == "l-0" {
			t.Error("oldest live entry kept")
		}
	}
	checkConsistent(t, m)
}

func TestTrimOnlyLastLoadedWindowIsKeptWhole(t *testing.T) {
	m := rangeModel()
	b0 := base
	m.ranges = []span{{from: time.UnixMilli(b0), to: time.UnixMilli(b0 + 100_000)}}
	m.fetchFrom = time.UnixMilli(b0)
	fill(m, "b", maxEntries+500, b0)
	m.trim()
	if len(m.entries) != maxEntries+500 {
		t.Errorf("len(entries) = %d, want %d (the newest window stays whole)", len(m.entries), maxEntries+500)
	}
	checkConsistent(t, m)
}
