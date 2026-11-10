package tui

import (
	"testing"
	"time"
)

func TestShortTime(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	today := now.UTC().Format(time.RFC3339)
	old := now.AddDate(0, 0, -3).UTC().Format(time.RFC3339)

	got, ok := shortTime(today, now)
	if want := now.Format("15:04:05"); !ok || got != want {
		t.Errorf("shortTime(today) = %q, %v; want %q, true", got, ok, want)
	}
	got, ok = shortTime(old, now)
	if want := now.AddDate(0, 0, -3).Format("Jan 02 15:04:05"); !ok || got != want {
		t.Errorf("shortTime(old) = %q, %v; want %q, true", got, ok, want)
	}
	for _, s := range []string{"", "nope", "2026-11-10", "12:00:00"} {
		if got, ok := shortTime(s, now); ok || got != "" {
			t.Errorf("shortTime(%q) = %q, %v; want \"\", false", s, got, ok)
		}
	}
}

func TestShortGroup(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/aws/lambda/orders", "orders"},
		{"/aws/lambda/orders/", "orders"},
		{"orders", "orders"},
		{"/orders", "orders"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := shortGroup(tt.in); got != tt.want {
			t.Errorf("shortGroup(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCompact(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	ts := now.UTC().Format(time.RFC3339)
	wantT := now.Format("15:04:05")
	oldTime := now.AddDate(0, 0, -3)
	oldTS := oldTime.UTC().Format(time.RFC3339)
	const id = "12345678-1234-1234-1234-123456789abc"
	prefix := "[" + ts + "] (2026/11/10/[$LATEST]abc) "

	tests := []struct {
		name     string
		in, req  string
		wantTime string
		wantMsg  string
	}{
		{"plain prefix", prefix + "hello", "", wantT, "hello"},
		{
			"colored time and stream",
			"[\x1b[2m" + ts + "\x1b[0m] (\x1b[36mstream\x1b[0m) hello", "",
			wantT, "hello",
		},
		{"non-today date", "[" + oldTS + "] (s) hello", "", oldTime.Format("Jan 02 15:04:05"), "hello"},
		{"inner timestamp", "[" + ts + "] (s) 2026-11-10T10:00:00.123Z hello", "", wantT, "hello"},
		{"inner timestamp no fraction", "[" + ts + "] (s) 2026-11-10T10:00:00Z   hello", "", wantT, "hello"},
		{"inner timestamp after level", "[" + ts + "] (s) [INFO] 2026-11-10T10:00:00.123Z hello", "", wantT, "[INFO] hello"},
		{"request id cut", "[" + ts + "] (s) START RequestId: " + id, id, wantT, "START RequestId: 12345678"},
		{"tabs to spaces", "[" + ts + "] (s) a\tb", "", wantT, "a b"},
		{"no prefix", "just a line", "", "", "just a line"},
		{"no prefix tabs", "a\tb\tc", "", "", "a b c"},
		{"invalid time keeps prefix", "[notatime] (s) hello", "", "", "[notatime] (s) hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gt, gm := compact(tt.in, tt.req, now)
			if gt != tt.wantTime || gm != tt.wantMsg {
				t.Errorf("compact(%q) = (%q, %q), want (%q, %q)", tt.in, gt, gm, tt.wantTime, tt.wantMsg)
			}
		})
	}
}
