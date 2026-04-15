// Package cat implements the "cat" subcommand.
//
// It returns all indexed commands ordered oldest-first (ascending ts).
package cat

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/mwirges/ghistx/internal/find"
)

// Cmd returns commands from the database, ordered oldest-first.
// cwdFilter restricts to a specific directory (raw path); empty = all directories.
// sourceFilter controls which commands are shown: "user" (default), "claude", or "all".
// limit, when > 0, returns only the limit most-recent commands (still oldest-first).
// toolFilter and categoryFilter, when non-empty, restrict results by cmdmeta values.
func Cmd(db *sql.DB, cwdFilter, sourceFilter string, limit int, toolFilter, categoryFilter string) ([]find.Hit, error) {
	query := `
		SELECT r.hash, r.ts, r.cmd, r.cwd, COALESCE(m.value, '') AS source
		FROM cmdraw r
		LEFT OUTER JOIN cmdmeta m ON r.hash = m.hash AND m.key = 'source'`
	if toolFilter != "" {
		query += ` LEFT OUTER JOIN cmdmeta mt ON r.hash = mt.hash AND mt.key = 'tool'`
	}
	if categoryFilter != "" {
		query += ` LEFT OUTER JOIN cmdmeta mc ON r.hash = mc.hash AND mc.key = 'category'`
	}
	var args []any
	var conditions []string
	if cwdFilter != "" {
		b64filter := base64.StdEncoding.EncodeToString([]byte(cwdFilter))
		conditions = append(conditions, "r.cwd = ?")
		args = append(args, b64filter)
	}
	switch sourceFilter {
	case "all":
		// no filter
	case "claude":
		conditions = append(conditions, "COALESCE(m.value, '') = 'claude'")
	default: // "user" or ""
		conditions = append(conditions, "COALESCE(m.value, '') = ''")
	}
	if toolFilter != "" {
		conditions = append(conditions, "mt.value = ?")
		args = append(args, toolFilter)
	}
	if categoryFilter != "" {
		conditions = append(conditions, "mc.value = ?")
		args = append(args, categoryFilter)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	if limit > 0 {
		// Fetch the N most recent, then reverse below for oldest-first display.
		query += ` ORDER BY r.ts DESC LIMIT ?`
		args = append(args, limit)
	} else {
		query += ` ORDER BY r.ts ASC`
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("cat: query: %w", err)
	}
	defer rows.Close()

	var hits []find.Hit
	for rows.Next() {
		var hash, b64cmd, source string
		var b64cwd sql.NullString
		var ts int64
		if err := rows.Scan(&hash, &ts, &b64cmd, &b64cwd, &source); err != nil {
			return nil, fmt.Errorf("cat: scan: %w", err)
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
		hits = append(hits, find.Hit{
			Hash:   hash,
			Cmd:    string(cmd),
			CWD:    cwd,
			TS:     ts,
			Source: source,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if limit > 0 {
		// Results arrived newest-first; reverse to oldest-first for display.
		for i, j := 0, len(hits)-1; i < j; i, j = i+1, j-1 {
			hits[i], hits[j] = hits[j], hits[i]
		}
	}
	return hits, nil
}
