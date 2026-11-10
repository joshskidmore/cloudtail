package tui

import (
	"sort"
	"time"
)

// The viewer can hold several time ranges of history with gaps between
// them: the live tail, plus windows that "t", "[" and "]" load. A gap row
// shows between entries of different ranges.

// span is a loaded time range [from, to). The live range has no end.
type span struct {
	from, to time.Time
	live     bool
}

func (s span) contains(ms int64) bool {
	return ms >= s.from.UnixMilli() && (s.live || ms < s.to.UnixMilli())
}

// rangeAt returns the index of the range that contains time ms, or -1.
func (m *model) rangeAt(ms int64) int {
	for i, r := range m.ranges {
		if r.contains(ms) {
			return i
		}
	}
	return -1
}

// addRange adds [from, to) and merges ranges that overlap or touch.
func (m *model) addRange(from, to time.Time) {
	if !from.Before(to) {
		return
	}
	rs := append(m.ranges, span{from: from, to: to})
	sort.Slice(rs, func(a, b int) bool { return rs[a].from.Before(rs[b].from) })
	merged := rs[:1]
	for _, r := range rs[1:] {
		last := &merged[len(merged)-1]
		if last.live || !r.from.After(last.to) {
			if !last.live && (r.live || r.to.After(last.to)) {
				last.to, last.live = r.to, r.live
			}
			continue
		}
		merged = append(merged, r)
	}
	m.ranges = merged
}

// startFetch loads [from, to) for every group. It returns false when a
// fetch is already running.
func (m *model) startFetch(from, to time.Time) bool {
	if m.fetching > 0 {
		m.notice = "still loading; try again"
		return false
	}
	if len(m.groups) == 0 || !from.Before(to) {
		return false
	}
	m.fetching = len(m.groups)
	m.fetchFrom, m.fetchUntil, m.fetchTo, m.fetchErr = from, to, to, nil
	m.rt.fetch(m.groups, from, to)
	return true
}

// fetchDone handles the result of one group's fetch.
func (m *model) fetchDone(msg fetchMsg) {
	m.fetching--
	if msg.err != nil {
		m.fetchErr = msg.err
	} else {
		m.insert(msg.group, msg.events, true, map[string]string{})
		if msg.until.Before(m.fetchUntil) {
			m.fetchUntil = msg.until
		}
	}
	if m.fetching > 0 {
		return
	}
	if m.fetchErr != nil {
		m.notice = "load failed: " + m.fetchErr.Error()
		m.jumpTo = time.Time{}
		return
	}
	m.addRange(m.fetchFrom, m.fetchUntil)
	m.rebuild()
	m.notice = "loaded " + clock(m.fetchFrom) + " → " + clock(m.fetchUntil)
	if m.fetchUntil.Before(m.fetchTo) {
		m.notice += " (limit reached; ] loads more)"
	}
	if !m.jumpTo.IsZero() {
		m.goToTime(m.jumpTo)
	}
}

// extend loads windowStep before (dir -1) or after (dir 1) the range of the
// selected entry, up to the next range.
func (m *model) extend(dir int) {
	ri := len(m.ranges) - 1
	if m.sel >= 0 {
		if i := m.rangeAt(m.entries[m.shown[m.sel]].time); i >= 0 {
			ri = i
		}
	}
	if ri < 0 {
		return
	}
	r := m.ranges[ri]
	if dir < 0 {
		from := r.from.Add(-windowStep)
		if ri > 0 && m.ranges[ri-1].to.After(from) {
			from = m.ranges[ri-1].to
		}
		m.startFetch(from, r.from)
		return
	}
	if r.live {
		m.notice = "this range is the live tail"
		return
	}
	to := r.to.Add(windowStep)
	if ri+1 < len(m.ranges) && m.ranges[ri+1].from.Before(to) {
		to = m.ranges[ri+1].from
	}
	m.startFetch(r.to, to)
}

// goToTime selects the first shown entry at or after t. When t is outside
// the loaded ranges, it loads a windowStep window from t, up to the next
// range, and jumps when the load is done.
func (m *model) goToTime(t time.Time) {
	m.jumpTo = time.Time{}
	ms := t.UnixMilli()
	if m.rangeAt(ms) < 0 {
		to := t.Add(windowStep)
		for _, r := range m.ranges {
			if r.from.After(t) && r.from.Before(to) {
				to = r.from
				break
			}
		}
		if m.startFetch(t, to) {
			m.jumpTo = t
			m.notice = "loading " + clock(t) + " → " + clock(to) + "…"
		}
		return
	}
	for _, i := range m.shown {
		if m.entries[i].time >= ms {
			m.show(i)
			m.notice = "at " + clock(time.UnixMilli(m.entries[i].time))
			return
		}
	}
	m.selectPos(len(m.shown) - 1)
	m.notice = "no lines after " + clock(t) + "; showing newest"
}

// trim keeps at most maxEntries entries. It drops whole windows first,
// oldest first, and never the window that was loaded last. Then it drops
// the oldest entries of the live tail.
func (m *model) trim() {
	for len(m.entries) > maxEntries {
		drop := -1
		for i, r := range m.ranges {
			last := r.contains(m.fetchFrom.UnixMilli())
			if !r.live && !last {
				drop = i
				break
			}
		}
		if drop >= 0 {
			r := m.ranges[drop]
			m.keep(func(e *entry) bool { return !r.contains(e.time) })
			m.ranges = append(m.ranges[:drop], m.ranges[drop+1:]...)
			continue
		}
		live := &m.ranges[len(m.ranges)-1]
		if !live.live {
			return // only the newest window is left; keep it whole
		}
		// Cut a chunk below the cap, so the next batches take the fast
		// append path instead of trimming each time.
		var cut int64
		for _, excess := range []int{len(m.entries) - maxEntries + maxEntries/10, len(m.entries) - maxEntries} {
			for _, e := range m.entries {
				if live.contains(e.time) {
					if excess--; excess < 0 {
						cut = e.time
						break
					}
				}
			}
			if cut != 0 {
				break
			}
		}
		if cut == 0 {
			return
		}
		m.keep(func(e *entry) bool { return !live.contains(e.time) || e.time >= cut })
		live.from = time.UnixMilli(cut)
		return
	}
}

// keep removes the entries for which ok returns false, and the Lambda
// reports of requests that have no entry left.
func (m *model) keep(ok func(e *entry) bool) {
	kept := m.entries[:0]
	var gone map[string]bool
	for i := range m.entries {
		e := &m.entries[i]
		if ok(e) {
			kept = append(kept, *e)
			continue
		}
		delete(m.seen, e.eid)
		if e.level == levelError {
			m.errCount--
		}
		if e.req != "" {
			if gone == nil {
				gone = map[string]bool{}
			}
			gone[e.req] = true
		}
	}
	m.entries = kept
	if gone == nil {
		return
	}
	for i := range kept {
		delete(gone, kept[i].req)
	}
	for id := range gone {
		delete(m.reports, id)
	}
	for stream, id := range m.openReq {
		if gone[id] {
			delete(m.openReq, stream)
		}
	}
}

// gapRow returns the row that marks the gap between range a and range b.
func (m *model) gapRow(a, b int) string {
	text := "── gap " + clock(m.ranges[a].to) + " → " + clock(m.ranges[b].from) + " · [ ] load "
	return dimStyle.Render(padRight(text, m.vp.Width))
}

func clock(t time.Time) string {
	return t.Local().Format("15:04:05")
}
