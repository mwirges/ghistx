// Package index implements the "index" subcommand.
//
// A command string is:
//  1. SHA-256 hashed (hex) to produce a unique key
//  2. Base64-encoded (StdEncoding) for storage in SQLite
//  3. N-grammed and stored in cmdlut for fast full-text search
package index

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/mwirges/ghistx/internal/ngram"
)

const (
	insertRaw = `INSERT OR REPLACE INTO cmdraw(hash, ts, cmd, cwd) VALUES(?, ?, ?, ?)`
	insertLUT = `INSERT OR IGNORE INTO cmdlut(host, ngram, hash) VALUES(?, ?, ?)`
)

// Hash returns the 64-character hex SHA-256 digest of cmd.
// This is the primary key used in cmdraw and cmdlut.
func Hash(cmd string) string {
	sum := sha256.Sum256([]byte(cmd))
	return fmt.Sprintf("%x", sum)
}

// Cmd indexes a single command string into the database.
// cwd is the current working directory at the time the command was run.
// If the command already exists (same hash), its timestamp and cwd are updated.
// meta is an optional map of key/value pairs stored in cmdmeta (e.g. "source",
// "tool", "category"). A nil or empty map stores no metadata.
func Cmd(db *sql.DB, cmd, cwd string, meta map[string]string) error {
	hash := Hash(cmd)
	b64cmd := base64.StdEncoding.EncodeToString([]byte(cmd))
	b64cwd := base64.StdEncoding.EncodeToString([]byte(cwd))
	ts := time.Now().UnixMilli()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("index: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(insertRaw, hash, ts, b64cmd, b64cwd); err != nil {
		return fmt.Errorf("index: insert cmdraw: %w", err)
	}

	grams := ngram.Gen(cmd)
	for _, g := range grams {
		if _, err := tx.Exec(insertLUT, "", g, hash); err != nil {
			return fmt.Errorf("index: insert cmdlut ngram %d: %w", g, err)
		}
	}

	for k, v := range meta {
		if v == "" {
			continue
		}
		_, err := tx.Exec(
			`INSERT OR REPLACE INTO cmdmeta(hash, key, value) VALUES(?, ?, ?)`,
			hash, k, v,
		)
		if err != nil {
			return fmt.Errorf("index: insert cmdmeta %q: %w", k, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("index: commit: %w", err)
	}
	return nil
}
