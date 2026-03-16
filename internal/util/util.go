// Package util provides shared formatting helpers.
package util

import "time"

// FormatRelative returns a human-readable description of how long ago
// tsMillis (milliseconds since Unix epoch) occurred relative to now.
//
// Format matches the C histx when_pretty / format_when output:
//
//	"A few moments ago"
//	"N minute(s) ago"
//	"N hour(s) ago"
//	"N day(s) ago"
func FormatRelative(tsMillis int64) string {
	now := time.Now().UnixMilli()
	delta := now - tsMillis
	if delta < 0 {
		delta = 0
	}

	secs := delta / 1000
	mins := secs / 60
	hours := mins / 60
	days := hours / 24

	switch {
	case days > 0:
		if days == 1 {
			return "1 day ago"
		}
		return pluralize(int(days), "day")
	case hours > 0:
		return pluralize(int(hours), "hour")
	case mins > 0:
		return pluralize(int(mins), "minute")
	default:
		return "A few moments ago"
	}
}

func pluralize(n int, unit string) string {
	if n == 1 {
		return "1 " + unit + " ago"
	}
	return itoa(n) + " " + unit + "s ago"
}

func itoa(n int) string {
	// Fast integer-to-string without importing strconv in the hot path.
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
