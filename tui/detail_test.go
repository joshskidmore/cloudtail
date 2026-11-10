package tui

import "testing"

func TestDetailText(t *testing.T) {
	const p = "[2026-11-10T10:00:00Z] (s) "
	tests := []struct {
		name  string
		plain string
		want  string
	}{
		{"plain message", p + "hello world", "hello world"},
		{"no prefix", "hello world", "hello world"},
		{"json object", p + `{"a":1,"b":{"c":2}}`, "{\n  \"a\": 1,\n  \"b\": {\n    \"c\": 2\n  }\n}"},
		{"json array", p + `[1,2]`, "[\n  1,\n  2\n]"},
		{"already spaced json", p + `{ "a" : 1 }`, "{\n  \"a\": 1\n}"},
		{"text before json", p + `INFO {"a":1}`, "INFO\n{\n  \"a\": 1\n}"},
		{"invalid json object", p + `{not json`, "{not json"},
		{"invalid json after text", p + `INFO {"a":`, `INFO {"a":`},
		{"tabs replaced", p + "a\tb", "a  b"},
		{"tabs before json", p + "a\t{\"k\":1}", "a\n{\n  \"k\": 1\n}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detailText(tt.plain); got != tt.want {
				t.Errorf("detailText(%q) = %q, want %q", tt.plain, got, tt.want)
			}
		})
	}
}
