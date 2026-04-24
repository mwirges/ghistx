// Package db manages the SQLite database for ghistx.
//
// The schema is identical to the C histx version for full cross-tool
// compatibility. Migration 1 adds the cwd column to cmdraw if not present.
package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // register sqlite driver
)

const thisVersion = 2

const ddl = `
CREATE TABLE IF NOT EXISTS cmdlut (
    host   TEXT,
    ngram  INTEGER,
    hash   TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS ngramindex     ON cmdlut(ngram, hash);
CREATE INDEX        IF NOT EXISTS ngramhashindex ON cmdlut(hash);

CREATE TABLE IF NOT EXISTS cmdraw (
    hash TEXT,
    ts   INTEGER,
    cmd  TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS hashindex ON cmdraw(hash);
CREATE INDEX        IF NOT EXISTS tsindex   ON cmdraw(ts);

CREATE TABLE IF NOT EXISTS cmdan (
    hash TEXT PRIMARY KEY,
    type INTEGER,
    desc TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS anindex ON cmdan(hash, type);

CREATE TABLE IF NOT EXISTS histxversion (
    version INTEGER PRIMARY KEY ASC,
    whence  INTEGER
);
`

// Open opens (or creates) the SQLite database at path, applies the schema,
// and runs any pending migrations. Use ":memory:" for in-process testing.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("db: open %q: %w", path, err)
	}

	// Retry for up to 5 seconds when another process holds a write lock
	// (common when multiple hook invocations fire concurrently).
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		db.Close()
		return nil, fmt.Errorf("db: set busy_timeout: %w", err)
	}
	// WAL mode serialises writers while allowing concurrent readers, which
	// further reduces contention between simultaneous hook processes.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("db: set journal_mode: %w", err)
	}

	if _, err := db.Exec(ddl); err != nil {
		db.Close()
		return nil, fmt.Errorf("db: init schema: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// version returns the highest migration version recorded, or 0 if none.
func version(db *sql.DB) (int, error) {
	var v sql.NullInt64
	err := db.QueryRow(`SELECT MAX(version) FROM histxversion`).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("db: read version: %w", err)
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}

func migrate(db *sql.DB) error {
	v, err := version(db)
	if err != nil {
		return err
	}
	if v < 1 {
		if err := migration1(db); err != nil {
			return err
		}
	}
	if v < 2 {
		if err := migration2(db); err != nil {
			return err
		}
	}
	return nil
}

// migration2 adds the cmdmeta table for ghistx-specific key/value metadata
// (e.g. source annotation). This table is unknown to C histx; it ignores it.
func migration2(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS cmdmeta (
			hash  TEXT,
			key   TEXT,
			value TEXT,
			UNIQUE(hash, key)
		);
		CREATE INDEX IF NOT EXISTS idx_cmdmeta_hash ON cmdmeta(hash);
	`)
	if err != nil {
		return fmt.Errorf("db: migration 2 create cmdmeta: %w", err)
	}
	_, err = db.Exec(
		`INSERT OR IGNORE INTO histxversion(version, whence) VALUES(?, unixepoch())`,
		2,
	)
	if err != nil {
		return fmt.Errorf("db: mark migration 2: %w", err)
	}
	return nil
}

// migration1 adds the cwd column to cmdraw (compatibility with histx v1 schema).
func migration1(db *sql.DB) error {
	// ALTER TABLE fails if the column already exists; swallow that error.
	_, err := db.Exec(`ALTER TABLE cmdraw ADD COLUMN cwd TEXT`)
	if err != nil {
		// modernc sqlite returns a non-nil error with "duplicate column name"
		// if the column already exists — that is fine.
		_ = err
	}
	_, err = db.Exec(
		`INSERT INTO histxversion(version, whence) VALUES(?, unixepoch())`,
		thisVersion,
	)
	if err != nil {
		return fmt.Errorf("db: mark migration 1: %w", err)
	}
	return nil
}
