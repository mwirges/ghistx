// Package db manages the SQLite database for ghistx.
//
// The schema is identical to the C histx version for full cross-tool
// compatibility. Migration 1 adds the cwd column to cmdraw. Migration 2
// adds the ghistx-only cmdmeta table. Each step records its own version
// only after that step commits.
package db

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // register sqlite driver
)

const (
	versionCWD     = 1
	versionCmdmeta = 2
	// thisVersion is the highest migration a successful Open records.
	thisVersion = versionCmdmeta
)

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
	if v < versionCWD {
		if err := migration1(db); err != nil {
			return err
		}
		// The next step follows the version that committed, not the value
		// read before migration 1.
		v, err = version(db)
		if err != nil {
			return err
		}
	}
	if v < versionCmdmeta {
		return migration2(db)
	}
	// A previous migration1 inserted version 2 and then migration2 failed,
	// leaving no cmdmeta table. Create it without advancing the version.
	return repairCmdmetaIfMissing(db)
}

// migration2 adds the cmdmeta table for ghistx-specific key/value metadata
// (e.g. source annotation). This table is unknown to C histx; it ignores it.
func migration2(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("db: migration 2 begin: %w", err)
	}
	defer tx.Rollback()

	if err := createCmdmeta(tx); err != nil {
		return fmt.Errorf("db: migration 2 create cmdmeta: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO histxversion(version, whence) VALUES(?, unixepoch())`,
		versionCmdmeta,
	); err != nil {
		return fmt.Errorf("db: mark migration 2: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: migration 2 commit: %w", err)
	}
	return nil
}

// migration1 adds the cwd column to cmdraw (compatibility with histx v1 schema).
// A duplicate-column error means cwd is already there. Any other ALTER error
// aborts the step and leaves the recorded version unchanged.
func migration1(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("db: migration 1 begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`ALTER TABLE cmdraw ADD COLUMN cwd TEXT`); err != nil {
		if !isDuplicateColumn(err) {
			return fmt.Errorf("db: migration 1 add cwd: %w", err)
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO histxversion(version, whence) VALUES(?, unixepoch())`,
		versionCWD,
	); err != nil {
		return fmt.Errorf("db: mark migration 1: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: migration 1 commit: %w", err)
	}
	return nil
}

func createCmdmeta(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS cmdmeta (
			hash  TEXT,
			key   TEXT,
			value TEXT,
			UNIQUE(hash, key)
		)`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_cmdmeta_hash ON cmdmeta(hash)`)
	return err
}

// repairCmdmetaIfMissing creates cmdmeta when version 2 is already recorded
// but the table is absent. It does not insert another histxversion row.
func repairCmdmetaIfMissing(db *sql.DB) error {
	exists, err := tableExists(db, "cmdmeta")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("db: repair cmdmeta begin: %w", err)
	}
	defer tx.Rollback()

	if err := createCmdmeta(tx); err != nil {
		return fmt.Errorf("db: repair cmdmeta: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: repair cmdmeta commit: %w", err)
	}
	return nil
}

func tableExists(db *sql.DB, name string) (bool, error) {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`,
		name,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("db: lookup table %s: %w", name, err)
	}
	return n > 0, nil
}

func isDuplicateColumn(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column")
}
