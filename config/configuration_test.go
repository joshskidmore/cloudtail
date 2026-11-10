package config

import (
	"testing"
	"time"
)

var testTimeNow = time.Date(2018, 12, 01, 15, 30, 0, 0, time.UTC)

/*
 * Helper method to assert equality of time objects in our tests
 */
func assertTimeEquality(t *testing.T, expected *time.Time, result *time.Time, err error) bool {
	if err != nil || !result.Equal(*expected) {
		t.Errorf("Expected to parse absolute time from simple string. Error: %s. Expected Time: %s, Absolute Time: %s", err, expected.String(), result.String())

		return false
	}

	return true
}

// test the relative parsing functionality of ParseTime
func TestGetTimeRelative(t *testing.T) {
	expectedTime1 := time.Date(2018, 12, 01, 13, 30, 0, 0, time.UTC)
	relativeTime1, err := ParseTime("-2h", testTimeNow)
	assertTimeEquality(t, &expectedTime1, &relativeTime1, err)

	expectedTime2 := time.Date(2018, 12, 01, 14, 15, 0, 0, time.UTC)
	relativeTime2, err := ParseTime("-1h15m", testTimeNow)
	assertTimeEquality(t, &expectedTime2, &relativeTime2, err)
}

// test the absolute RFC3339 parsing functionality of ParseTime
func TestGetTimeAbsoluteRFC3339(t *testing.T) {
	expectedTime := time.Date(2006, 1, 2, 23, 4, 5, 0, time.UTC)
	absoluteTime1, err := ParseTime("2006-01-02T15:04:05-08:00", testTimeNow)

	assertTimeEquality(t, &expectedTime, &absoluteTime1, err)
}

func TestGetTimeAbsoluteSimpleDate(t *testing.T) {
	expectedTime := time.Date(2018, 6, 26, 0, 0, 0, 0, time.UTC)
	absoluteTime1, err := ParseTime("2018-06-26", testTimeNow)

	assertTimeEquality(t, &expectedTime, &absoluteTime1, err)
}

func TestGetTimeAbsoluteSimpleDateAndTime(t *testing.T) {
	expectedTime := time.Date(2018, 6, 26, 12, 43, 30, 0, time.UTC)
	absoluteTime1, err := ParseTime("2018-06-26 12:43:30", testTimeNow)

	assertTimeEquality(t, &expectedTime, &absoluteTime1, err)
}

func TestParseTimeLocalZone(t *testing.T) {
	loc := time.FixedZone("test", -8*60*60)
	now := time.Date(2018, 12, 1, 15, 30, 0, 0, loc)
	expected := time.Date(2018, 6, 26, 12, 43, 30, 0, loc)
	got, err := ParseTime("2018-06-26 12:43:30", now)
	assertTimeEquality(t, &expected, &got, err)
}

func TestParseTimeInvalid(t *testing.T) {
	if _, err := ParseTime("yesterday-ish", testTimeNow); err == nil {
		t.Error("expected an error for an invalid time")
	}
}

func TestFilterLogEventsInputInvalidTime(t *testing.T) {
	if _, err := (&Configuration{Group: "g", Start: "nope"}).FilterLogEventsInput(); err == nil {
		t.Error("expected an error for an invalid --start")
	}
	if _, err := (&Configuration{Group: "g", End: "nope"}).FilterLogEventsInput(); err == nil {
		t.Error("expected an error for an invalid --stop")
	}
}
