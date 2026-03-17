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

// Cmd returns commands from the database, ordered oldest-first.
// When cwdFilter is non-empty, only commands indexed from that directory are returned.
func Cmd(db *sql.DB, cwdFilter string) ([]find.Hit, error) {
	query := `SELECT hash, ts, cmd, cwd FROM cmdraw`
	var args []any
	if cwdFilter != "" {
		b64filter := base64.StdEncoding.EncodeToString([]byte(cwdFilter))
		query += ` WHERE cwd = ?`
		args = append(args, b64filter)
	}
	query += ` ORDER BY ts ASC`

	rows, err := db.Query(query, args...)
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
