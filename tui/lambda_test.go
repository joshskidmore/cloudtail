package tui

import (
	"testing"
	"time"
)

const (
	rid1 = "11111111-1111-1111-1111-111111111111"
	rid2 = "22222222-2222-2222-2222-222222222222"
)

func TestSplitPrefix(t *testing.T) {
	tests := []struct {
		in                string
		when, stream, msg string
	}{
		{"[2026-11-10T10:00:00Z] (s1) hello world", "2026-11-10T10:00:00Z", "s1", "hello world"},
		{"[t] (a/b[$LATEST]c) multi\nline", "t", "a/b[$LATEST]c", "multi\nline"},
		{"[t] (s) ", "t", "s", ""},
		{"raw line", "", "", "raw line"},
		{"[t] no stream", "", "", "[t] no stream"},
		{"", "", "", ""},
	}
	for _, tt := range tests {
		w, s, m := splitPrefix(tt.in)
		if w != tt.when || s != tt.stream || m != tt.msg {
			t.Errorf("splitPrefix(%q) = (%q, %q, %q), want (%q, %q, %q)", tt.in, w, s, m, tt.when, tt.stream, tt.msg)
		}
	}
}

func newTrackModel() (*model, map[string]string) {
	m := newModel(&runner{}, func() time.Time { return time.Now() }, "")
	return &m, map[string]string{}
}

func TestTrackStartInheritsAndReport(t *testing.T) {
	m, open := newTrackModel()
	const p = "[2026-11-10T10:00:00Z] (s1) "

	if got := m.track(p+"before start", 0, open); got != "" {
		t.Errorf("line before START = %q, want empty", got)
	}
	if got := m.track(p+"START RequestId: "+rid1+" Version: $LATEST", 0, open); got != rid1 {
		t.Errorf("START id = %q, want %q", got, rid1)
	}
	if open["0|s1"] != rid1 {
		t.Errorf("open request not set: %v", open)
	}
	if got := m.track(p+"no id here", 0, open); got != rid1 {
		t.Errorf("inherited id = %q, want %q", got, rid1)
	}
	if got := m.track(p+"no id here", 1, open); got != "" {
		t.Errorf("other group inherited id = %q, want empty", got)
	}
	if got := m.track("[2026-11-10T10:00:00Z] (s2) no id here", 0, open); got != "" {
		t.Errorf("other stream inherited id = %q, want empty", got)
	}
	rep := p + "REPORT RequestId: " + rid1 + "\tDuration: 12.34 ms\tBilled Duration: 13 ms\tMemory Size: 128 MB\tMax Memory Used: 64 MB\tInit Duration: 200.50 ms"
	if got := m.track(rep, 0, open); got != rid1 {
		t.Errorf("REPORT id = %q, want %q", got, rid1)
	}
	r, ok := m.reports[rid1]
	if !ok {
		t.Fatal("report not recorded")
	}
	if got, want := r.String(), "12.34 ms · 64/128 MB · init 200.50 ms"; got != want {
		t.Errorf("report = %q, want %q", got, want)
	}
	if _, ok := open["0|s1"]; ok {
		t.Error("REPORT did not close the request")
	}
	if got := m.track(p+"after report", 0, open); got != "" {
		t.Errorf("line after REPORT = %q, want empty", got)
	}
}

func TestTrackReportWithoutInit(t *testing.T) {
	m, open := newTrackModel()
	rep := "[t] (s) REPORT RequestId: " + rid2 + "\tDuration: 1.00 ms\tBilled Duration: 2 ms\tMemory Size: 256 MB\tMax Memory Used: 30 MB"
	m.track(rep, 0, open)
	if got, want := m.reports[rid2].String(), "1.00 ms · 30/256 MB"; got != want {
		t.Errorf("report = %q, want %q", got, want)
	}
}

func TestTrackFindsID(t *testing.T) {
	const p = "[2026-11-10T10:00:00Z] (s1) "
	tests := []struct {
		name, line, want string
	}{
		{"tab separated", p + "2026-11-10T10:00:00.000Z\t" + rid2 + "\tINFO\tmsg", rid2},
		{"json requestId", p + `{"level":"info","requestId":"` + rid2 + `"}`, rid2},
		{"json aws_request_id", p + `{"aws_request_id":"` + rid2 + `"}`, rid2},
		{"json awsRequestId", p + `{"awsRequestId": "` + rid2 + `"}`, rid2},
		{"END line", p + "END RequestId: " + rid2, rid2},
		{"raw no id", "plain raw line", ""},
		{"prefixed no id", p + "nothing", ""},
		{"raw with tab id", "a\t" + rid2 + "\tb", rid2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, open := newTrackModel()
			if got := m.track(tt.line, 0, open); got != tt.want {
				t.Errorf("track(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestTrackRawLineIgnoresOpen(t *testing.T) {
	m, open := newTrackModel()
	m.track("[t] (s) START RequestId: "+rid1, 0, open)
	if got := m.track("raw line", 0, open); got != "" {
		t.Errorf("raw line = %q, want empty", got)
	}
}
