package util

import (
	"strings"
	"testing"
	"time"
)

func msAgo(d time.Duration) int64 {
	return time.Now().Add(-d).UnixMilli()
}

func TestFewMomentsAgo(t *testing.T) {
	ts := msAgo(30 * time.Second)
	got := FormatRelative(ts)
	if got != "A few moments ago" {
		t.Errorf("got %q, want \"A few moments ago\"", got)
	}
}

func TestMinutesAgo(t *testing.T) {
	ts := msAgo(5 * time.Minute)
	got := FormatRelative(ts)
	if !strings.Contains(got, "minute") {
		t.Errorf("got %q, expected minutes", got)
	}
}

func TestSingularMinute(t *testing.T) {
	ts := msAgo(90 * time.Second) // 1 min 30 sec -> 1 minute
	got := FormatRelative(ts)
	if got != "1 minute ago" {
		t.Errorf("got %q, want \"1 minute ago\"", got)
	}
}

func TestPluralMinutes(t *testing.T) {
	ts := msAgo(3 * time.Minute)
	got := FormatRelative(ts)
	if got != "3 minutes ago" {
		t.Errorf("got %q, want \"3 minutes ago\"", got)
	}
}

func TestHoursAgo(t *testing.T) {
	ts := msAgo(2 * time.Hour)
	got := FormatRelative(ts)
	if got != "2 hours ago" {
		t.Errorf("got %q, want \"2 hours ago\"", got)
	}
}

func TestSingularHour(t *testing.T) {
	ts := msAgo(90 * time.Minute)
	got := FormatRelative(ts)
	if got != "1 hour ago" {
		t.Errorf("got %q, want \"1 hour ago\"", got)
	}
}

func TestDaysAgo(t *testing.T) {
	ts := msAgo(48 * time.Hour)
	got := FormatRelative(ts)
	if got != "2 days ago" {
		t.Errorf("got %q, want \"2 days ago\"", got)
	}
}

func TestSingularDay(t *testing.T) {
	ts := msAgo(25 * time.Hour)
	got := FormatRelative(ts)
	if got != "1 day ago" {
		t.Errorf("got %q, want \"1 day ago\"", got)
	}
}

func TestFutureTimestamp(t *testing.T) {
	// Future timestamps should not panic; delta is clamped to 0.
	ts := time.Now().Add(10 * time.Minute).UnixMilli()
	got := FormatRelative(ts)
	if got != "A few moments ago" {
		t.Errorf("got %q, want \"A few moments ago\"", got)
	}
}
