package tui

import (
	"testing"
	"time"
)

func TestParseJumpTime(t *testing.T) {
	loc := time.FixedZone("TEST", -7*3600)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, loc)
	tests := []struct {
		in   string
		want time.Time
	}{
		{"15:30", time.Date(2026, 11, 9, 15, 30, 0, 0, loc)},
		{"15:30:10", time.Date(2026, 11, 9, 15, 30, 10, 0, loc)},
		{"09:05", time.Date(2026, 11, 10, 9, 5, 0, 0, loc)},
		{"09:05:07", time.Date(2026, 11, 10, 9, 5, 7, 0, loc)},
		{"12:00", time.Date(2026, 11, 10, 12, 0, 0, 0, loc)},
		{"12:00:01", time.Date(2026, 11, 9, 12, 0, 1, 0, loc)},
		{"-10m", now.Add(-10 * time.Minute)},
		{"10m", now.Add(-10 * time.Minute)},
		{"-1h30m", now.Add(-90 * time.Minute)},
		{"2026-11-10 15:30", time.Date(2026, 11, 10, 15, 30, 0, 0, loc)},
		{"2026-11-10 15:30:10", time.Date(2026, 11, 10, 15, 30, 10, 0, loc)},
		{"  09:05  ", time.Date(2026, 11, 10, 9, 5, 0, 0, loc)},
		{" 2026-11-10 15:30 ", time.Date(2026, 11, 10, 15, 30, 0, 0, loc)},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseJumpTime(tt.in, now)
			if err != nil {
				t.Fatalf("parseJumpTime(%q): %v", tt.in, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("parseJumpTime(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseJumpTimeInvalid(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.FixedZone("TEST", -7*3600))
	for _, s := range []string{"", "   ", "abc", "25:00", "15:61", "2026-13-01 10:00", "2026-11-10", "10x", "15:30:10:5"} {
		if got, err := parseJumpTime(s, now); err == nil {
			t.Errorf("parseJumpTime(%q) = %v, nil; want error", s, got)
		}
	}
}
