// Package squelch filters out mundane/noisy shell commands from history results.
//
// Three pattern types are supported, selected by prefix:
//
//	"glob:ls *"       — filepath.Match glob (*, ?, [...])
//	"regex:^git diff" — compiled regular expression
//	"ls -la"          — exact byte-for-byte match (no prefix)
//
// The active pattern list is built from DefaultList merged with user-defined
// patterns from config. squelch-clear-defaults discards the built-in list.
package squelch

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mwirges/ghistx/internal/find"
)

// DefaultList is the built-in set of mundane commands squelched by default.
// All entries use exact matching (no prefix needed).
var DefaultList = []string{
	// Directory navigation
	"cd",
	"cd ..",
	"cd ~",
	"pwd",
	// File listing
	"ls",
	"ls -l",
	"ls -la",
	"ls -al",
	"ls -lah",
	"ls -ltr",
	"ll",
	"la",
	// Shell builtins / session
	"exit",
	"clear",
	"history",
	// Common one-shot commands
	"echo",
	"cat",
	"man",
	"which",
	"whoami",
	"date",
	"top",
	"htop",
}

type patternKind int

const (
	kindExact patternKind = iota
	kindGlob
	kindRegex
)

// Pattern is a compiled squelch pattern ready for matching.
type Pattern struct {
	Raw  string // original string as written in config
	kind patternKind
	re   *regexp.Regexp // non-nil for kindRegex
}

// Compile parses and compiles a list of raw pattern strings.
// Returns the compiled patterns and a (possibly empty) list of warning messages
// for patterns that failed to compile (they are skipped).
func Compile(rawPatterns []string) ([]Pattern, []string) {
	var patterns []Pattern
	var warnings []string

	for _, raw := range rawPatterns {
		switch {
		case strings.HasPrefix(raw, "regex:"):
			expr := strings.TrimPrefix(raw, "regex:")
			re, err := regexp.Compile(expr)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("squelch: invalid regex %q: %v", raw, err))
				continue
			}
			patterns = append(patterns, Pattern{Raw: raw, kind: kindRegex, re: re})

		case strings.HasPrefix(raw, "glob:"):
			g := strings.TrimPrefix(raw, "glob:")
			// Validate the glob at compile time — filepath.Match returns ErrBadPattern
			// for malformed patterns (e.g. unclosed bracket).
			if _, err := filepath.Match(g, ""); err != nil {
				warnings = append(warnings, fmt.Sprintf("squelch: invalid glob %q: %v", raw, err))
				continue
			}
			patterns = append(patterns, Pattern{Raw: raw, kind: kindGlob})

		default:
			patterns = append(patterns, Pattern{Raw: raw, kind: kindExact})
		}
	}
	return patterns, warnings
}

// matches reports whether a command string matches this pattern.
func (p Pattern) matches(cmd string) bool {
	switch p.kind {
	case kindRegex:
		return p.re.MatchString(cmd)
	case kindGlob:
		g := strings.TrimPrefix(p.Raw, "glob:")
		matched, err := filepath.Match(g, cmd)
		return err == nil && matched
	default: // kindExact
		return p.Raw == cmd
	}
}

// Filter removes hits whose Cmd matches any of the compiled patterns.
// If patterns is empty, hits is returned unchanged.
func Filter(hits []find.Hit, patterns []Pattern) []find.Hit {
	if len(patterns) == 0 {
		return hits
	}
	out := make([]find.Hit, 0, len(hits))
	for _, h := range hits {
		if !matchesAny(h.Cmd, patterns) {
			out = append(out, h)
		}
	}
	return out
}

// Matches reports whether cmd matches any of the compiled patterns.
func Matches(cmd string, patterns []Pattern) bool {
	return matchesAny(cmd, patterns)
}

func matchesAny(cmd string, patterns []Pattern) bool {
	for _, p := range patterns {
		if p.matches(cmd) {
			return true
		}
	}
	return false
}

// ActiveList returns the combined raw pattern list given user config.
// If clearDefaults is true, only userPatterns are used.
// Otherwise DefaultList and userPatterns are merged.
func ActiveList(clearDefaults bool, userPatterns []string) []string {
	if clearDefaults {
		return userPatterns
	}
	merged := make([]string, len(DefaultList)+len(userPatterns))
	copy(merged, DefaultList)
	copy(merged[len(DefaultList):], userPatterns)
	return merged
}
