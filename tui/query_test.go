package tui

import "testing"

const qLine = "[2026-11-10T10:00:00Z] (stream-a) Hello World error: disk full"

func TestParseQueryMatch(t *testing.T) {
	tests := []struct {
		name   string
		filter string
		line   string
		want   bool
	}{
		{"single term", "disk", qLine, true},
		{"single term miss", "memory", qLine, false},
		{"and", "disk error", qLine, true},
		{"and one missing", "disk memory", qLine, false},
		{"not", "!memory", qLine, true},
		{"not hit", "!disk", qLine, false},
		{"and not", "disk !memory", qLine, true},
		{"or first", "memory | disk", qLine, true},
		{"or second", "disk | memory", qLine, true},
		{"or none", "memory | cpu", qLine, false},
		{"or binds tighter than space", "error disk | memory", qLine, true},
		{"or group then and fails", "disk | memory cpu", qLine, false},
		{"phrase", `"error: disk"`, qLine, true},
		{"phrase miss", `"disk error"`, qLine, false},
		{"quoted pipe is a term", `"|"`, "a | b", true},
		{"quoted pipe miss", `"|"`, "a b", false},
		{"start anchor", "^hello", qLine, true},
		{"start anchor miss", "^world", qLine, false},
		{"start anchor ignores prefix", "^[2026", qLine, false},
		{"end anchor", "full$", qLine, true},
		{"end anchor miss", "disk$", qLine, false},
		{"both anchors", "^foo$", "[2026-11-10T10:00:00Z] (s) foo", true},
		{"anchors without prefix", "^foo", "foo bar", true},
		{"exact quote", "'disk", qLine, true},
		{"exact quote miss", "'memory", qLine, false},
		{"lowercase ignores case", "hello", qLine, true},
		{"lowercase ignores case upper text", "world", qLine, true},
		{"uppercase is case sensitive", "Hello", qLine, true},
		{"uppercase case mismatch", "HELLO", qLine, false},
		{"uppercase term vs lowercase text", "Disk", qLine, false},
		{"literal dollar", `cost\$`, "cost$ 5", true},
		{"literal dollar not end anchor", `cost\$`, "cost 5", false},
		{"dot is literal", "a.c", "abc", false},
		{"dot literal hit", "a.c", "a.c", true},
		{"star is literal", "a*", "aaa", false},
		{"star literal hit", "a*", "a*", true},
		{"brackets literal", "[x]", "[x]", true},
		{"parens literal", "(a)", "(a)", true},
		{"plus question literal", "a+b?", "a+b?", true},
		{"plus literal miss", "a+b?", "aab", false},
		{"not anchored", "!^hello", qLine, false},
		{"not anchored miss", "!^world", qLine, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := parseQuery(tt.filter)
			if err != nil {
				t.Fatalf("parseQuery(%q): %v", tt.filter, err)
			}
			if got := q.match(tt.line); got != tt.want {
				t.Errorf("parseQuery(%q).match(%q) = %v, want %v", tt.filter, tt.line, got, tt.want)
			}
		})
	}
}

func TestParseQueryEmpty(t *testing.T) {
	for _, s := range []string{"", "   ", "\t"} {
		q, err := parseQuery(s)
		if err != nil || q != nil {
			t.Errorf("parseQuery(%q) = %v, %v; want nil, nil", s, q, err)
		}
	}
}

func TestParseQueryErrors(t *testing.T) {
	for _, s := range []string{
		"|", "| a", "a |", "a | | b",
		`"a b`, `a "b`,
		"!", "^", `""`, "'", "$", "!^", "!$",
	} {
		t.Run(s, func(t *testing.T) {
			if q, err := parseQuery(s); err == nil {
				t.Errorf("parseQuery(%q) = %v, nil; want error", s, q)
			}
		})
	}
}

func TestSplitWords(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  a   b ", []string{"a", "b"}},
		{`"a b" c`, []string{`"a b"`, "c"}},
		{`!"a b"`, []string{`!"a b"`}},
		{`""`, []string{`""`}},
		{"a | b", []string{"a", "|", "b"}},
	}
	for _, tt := range tests {
		got, err := splitWords(tt.in)
		if err != nil {
			t.Errorf("splitWords(%q): %v", tt.in, err)
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("splitWords(%q) = %q, want %q", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitWords(%q) = %q, want %q", tt.in, got, tt.want)
				break
			}
		}
	}
	if _, err := splitWords(`a "b`); err == nil {
		t.Error("splitWords with open quote: want error")
	}
}

func TestParseTerm(t *testing.T) {
	tests := []struct {
		in      string
		neg     bool
		message bool
	}{
		{"a", false, false},
		{"!a", true, false},
		{"^a", false, true},
		{"a$", false, true},
		{`a\$`, false, false},
		{"!^a$", true, true},
		{"'a", false, false},
		{`"a b"`, false, false},
	}
	for _, tt := range tests {
		got, err := parseTerm(tt.in)
		if err != nil {
			t.Errorf("parseTerm(%q): %v", tt.in, err)
			continue
		}
		if got.neg != tt.neg || got.message != tt.message {
			t.Errorf("parseTerm(%q) neg=%v message=%v, want neg=%v message=%v", tt.in, got.neg, got.message, tt.neg, tt.message)
		}
	}
	for _, s := range []string{"", "!", "^", `""`} {
		if _, err := parseTerm(s); err == nil {
			t.Errorf("parseTerm(%q): want error", s)
		}
	}
}
