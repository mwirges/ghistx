// Package cat implements the "cat" subcommand.
//
// It returns all indexed commands ordered oldest-first (ascending ts).
package cat

import (
	"database/sql"
	"encoding/base64"
	"fmt"

	"github.com/mwirges/ghistx/internal/find"
)

// Cmd returns all commands from the database, ordered oldest-first.
func Cmd(db *sql.DB) ([]find.Hit, error) {
	rows, err := db.Query(`
		SELECT hash, ts, cmd, cwd
		FROM cmdraw
		ORDER BY ts ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("cat: query: %w", err)
	}
	defer rows.Close()

	var hits []find.Hit
	for rows.Next() {
		var hash, b64cmd string
		var b64cwd sql.NullString
		var ts int64
		if err := rows.Scan(&hash, &ts, &b64cmd, &b64cwd); err != nil {
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
			Hash: hash,
			Cmd:  string(cmd),
			CWD:  cwd,
			TS:   ts,
		})
	}
	return hits, rows.Err()
}
