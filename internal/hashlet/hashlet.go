// Package hashlet provides utilities for abbreviated hash prefixes.
//
// Hashlets are the shortest prefix of a SHA-256 hex hash that uniquely
// identifies a command within a result set, with a minimum length of 4.
// They function similarly to git's abbreviated commit SHAs.
package hashlet

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/mwirges/ghistx/internal/find"
)

// MinLen returns the shortest prefix length >= 4 such that every hash in
// the slice is uniquely identified by that many leading characters.
// Returns 4 for empty or single-element slices.
func MinLen(hashes []string) int {
	if len(hashes) <= 1 {
		return 4
	}
	for l := 4; l <= 64; l++ {
		seen := make(map[string]struct{}, len(hashes))
		all := true
		for _, h := range hashes {
			p := h[:l]
			if _, ok := seen[p]; ok {
				all = false
				break
			}
			seen[p] = struct{}{}
		}
		if all {
			return l
		}
	}
	return 64
}

// FindByPrefix looks up a command whose hash starts with prefix.
// Returns an error if zero or more than one command matches.
// The prefix is case-insensitive (normalized to lowercase).
func FindByPrefix(db *sql.DB, prefix string) (find.Hit, error) {
	if len(prefix) == 0 {
		return find.Hit{}, fmt.Errorf("hashlet: empty prefix")
	}
	prefix = strings.ToLower(prefix)
	for _, c := range prefix {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return find.Hit{}, fmt.Errorf("hashlet: invalid hex character in prefix %q", prefix)
		}
	}

	rows, err := db.Query(`
		SELECT r.hash, r.ts, r.cmd, r.cwd, COALESCE(m.value, '') AS source
		FROM cmdraw r
		LEFT OUTER JOIN cmdmeta m ON r.hash = m.hash AND m.key = 'source'
		WHERE r.hash LIKE ? LIMIT 3`,
		prefix+"%",
	)
	if err != nil {
		return find.Hit{}, fmt.Errorf("hashlet: query: %w", err)
	}
	defer rows.Close()

	var results []find.Hit
	for rows.Next() {
		var hash, b64cmd, source string
		var b64cwd sql.NullString
		var ts int64
		if err := rows.Scan(&hash, &ts, &b64cmd, &b64cwd, &source); err != nil {
			return find.Hit{}, fmt.Errorf("hashlet: scan: %w", err)
		}
		cmd, err := base64.StdEncoding.DecodeString(b64cmd)
		if err != nil {
			continue
		}
		cwd := ""
		if b64cwd.Valid && b64cwd.String != "" {
			c, err := base64.StdEncoding.DecodeString(b64cwd.String)
			if err == nil {
				cwd = string(c)
			}
		}
		results = append(results, find.Hit{
			Hash:   hash,
			Cmd:    string(cmd),
			CWD:    cwd,
			TS:     ts,
			Source: source,
		})
	}
	if err := rows.Err(); err != nil {
		return find.Hit{}, fmt.Errorf("hashlet: rows: %w", err)
	}

	switch len(results) {
	case 0:
		return find.Hit{}, fmt.Errorf("hashlet: no command found for prefix %q", prefix)
	case 1:
		return results[0], nil
	default:
		return find.Hit{}, fmt.Errorf("hashlet: ambiguous prefix %q (matches multiple commands)", prefix)
	}
}
