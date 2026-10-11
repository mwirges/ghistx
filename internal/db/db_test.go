package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenInMemory(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(\":memory:\") error: %v", err)
	}
	defer d.Close()

	// All expected tables must exist.
	tables := []string{"cmdraw", "cmdlut", "cmdan", "histxversion", "cmdmeta"}
	for _, tbl := range tables {
		var name string
		err := d.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", tbl, err)
		}
	}
}

func TestCWDColumnExists(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer d.Close()

	// Insert a row using the cwd column — if the column is absent this fails.
	_, err = d.Exec(
		`INSERT INTO cmdraw(hash, ts, cmd, cwd) VALUES('testhash', 0, 'cmd', 'cwd')`,
	)
	if err != nil {
		t.Fatalf("cwd column missing or unusable: %v", err)
	}
}

func TestMigrationIdempotent(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer d.Close()

	// Running migrate a second time must not error.
	if err := migrate(d); err != nil {
		t.Errorf("second migrate call failed: %v", err)
	}
}

func TestVersionTracked(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer d.Close()

	v, err := version(d)
	if err != nil {
		t.Fatalf("version() error: %v", err)
	}
	if v != thisVersion {
		t.Errorf("version = %d, want %d", v, thisVersion)
	}
	assertVersions(t, d, versionCWD, versionCmdmeta)
}

func TestMigration1DuplicateColumnCompletes(t *testing.T) {
	d := openBare(t, ":memory:")
	if _, err := d.Exec(`ALTER TABLE cmdraw ADD COLUMN cwd TEXT`); err != nil {
		t.Fatalf("pre-add cwd: %v", err)
	}
	_, dupErr := d.Exec(`ALTER TABLE cmdraw ADD COLUMN cwd TEXT`)
	if dupErr == nil {
		t.Fatal("second ALTER succeeded, want duplicate column")
	}
	if !isDuplicateColumn(dupErr) {
		t.Fatalf("duplicate column error not recognized: %v", dupErr)
	}

	if err := migrate(d); err != nil {
		t.Fatalf("migrate with cwd already present: %v", err)
	}
	assertVersions(t, d, versionCWD, versionCmdmeta)
	assertCWD(t, d, true)
	assertCmdmeta(t, d)

	if err := migrate(d); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	assertVersions(t, d, versionCWD, versionCmdmeta)
}

func TestMigration1OtherAlterErrorDoesNotAdvanceVersion(t *testing.T) {
	d := openBare(t, ":memory:")
	if _, err := d.Exec(`DROP TABLE cmdraw`); err != nil {
		t.Fatalf("drop cmdraw: %v", err)
	}

	err := migration1(d)
	if err == nil {
		t.Fatal("migration1 succeeded, want ALTER error")
	}
	if isDuplicateColumn(err) {
		t.Fatalf("error classified as duplicate column: %v", err)
	}
	if !strings.Contains(err.Error(), "migration 1 add cwd") {
		t.Fatalf("error = %v, want migration 1 add cwd", err)
	}

	v, verr := version(d)
	if verr != nil {
		t.Fatalf("version: %v", verr)
	}
	if v != 0 {
		t.Fatalf("version = %d, want 0", v)
	}
	assertVersions(t, d)
}

func TestMigration1RollsBackCWDWhenVersionInsertFails(t *testing.T) {
	d := openBare(t, ":memory:")
	if _, err := d.Exec(`DROP TABLE histxversion`); err != nil {
		t.Fatalf("drop histxversion: %v", err)
	}
	if _, err := d.Exec(`
		CREATE TABLE histxversion (
			version INTEGER PRIMARY KEY ASC,
			whence  INTEGER,
			CHECK (version <> 1)
		)`); err != nil {
		t.Fatalf("recreate histxversion: %v", err)
	}

	err := migration1(d)
	if err == nil {
		t.Fatal("migration1 succeeded, want version insert failure")
	}
	if !strings.Contains(err.Error(), "mark migration 1") {
		t.Fatalf("error = %v, want mark migration 1", err)
	}

	v, verr := version(d)
	if verr != nil {
		t.Fatalf("version: %v", verr)
	}
	if v != 0 {
		t.Fatalf("version = %d, want 0", v)
	}
	assertCWD(t, d, false)
}

func TestMigration2RollsBackWhenVersionInsertFails(t *testing.T) {
	d := openBare(t, ":memory:")
	if err := migration1(d); err != nil {
		t.Fatalf("migration1: %v", err)
	}
	if _, err := d.Exec(`
		CREATE TRIGGER fail_v2 BEFORE INSERT ON histxversion
		WHEN NEW.version = 2
		BEGIN
			SELECT RAISE(ABORT, 'fail version 2');
		END`); err != nil {
		t.Fatalf("trigger: %v", err)
	}

	err := migration2(d)
	if err == nil {
		t.Fatal("migration2 succeeded, want version insert failure")
	}
	if !strings.Contains(err.Error(), "mark migration 2") {
		t.Fatalf("error = %v, want mark migration 2", err)
	}

	v, verr := version(d)
	if verr != nil {
		t.Fatalf("version: %v", verr)
	}
	if v != versionCWD {
		t.Fatalf("version = %d, want %d", v, versionCWD)
	}
	exists, err := tableExists(d, "cmdmeta")
	if err != nil {
		t.Fatalf("tableExists: %v", err)
	}
	if exists {
		t.Fatal("cmdmeta exists after rolled-back migration 2")
	}
	assertCWD(t, d, true)
}

func TestMigration2FailureLeavesVersion1AndNextOpenRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "histx.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := d.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	if err := migration1(d); err != nil {
		t.Fatalf("migration1: %v", err)
	}
	// A table named cmdmeta with no hash column makes CREATE INDEX fail
	// after CREATE TABLE IF NOT EXISTS succeeds.
	if _, err := d.Exec(`CREATE TABLE cmdmeta (not_hash TEXT)`); err != nil {
		t.Fatalf("poison cmdmeta: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := Open(path); err == nil {
		t.Fatal("Open succeeded, want migration 2 error")
	}

	d, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	v, err := version(d)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v != versionCWD {
		t.Fatalf("version after failed migration 2 = %d, want %d", v, versionCWD)
	}
	assertVersions(t, d, versionCWD)
	assertCWD(t, d, true)

	if _, err := d.Exec(`DROP TABLE cmdmeta`); err != nil {
		t.Fatalf("drop poison cmdmeta: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	d, err = Open(path)
	if err != nil {
		t.Fatalf("retry Open: %v", err)
	}
	defer d.Close()

	v, err = version(d)
	if err != nil {
		t.Fatalf("version after retry: %v", err)
	}
	if v != versionCmdmeta {
		t.Fatalf("version after retry = %d, want %d", v, versionCmdmeta)
	}
	assertVersions(t, d, versionCWD, versionCmdmeta)
	assertCmdmeta(t, d)
}

func TestVersion2MissingCmdmetaRepaired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "histx.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := d.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	if _, err := d.Exec(`ALTER TABLE cmdraw ADD COLUMN cwd TEXT`); err != nil {
		t.Fatalf("add cwd: %v", err)
	}
	// The old migration1 inserted thisVersion (2) before migration2 ran.
	if _, err := d.Exec(
		`INSERT INTO histxversion(version, whence) VALUES(?, 0)`,
		versionCmdmeta,
	); err != nil {
		t.Fatalf("insert version 2: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	d, err = Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	assertVersions(t, d, versionCmdmeta)
	v, err := version(d)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v != versionCmdmeta {
		t.Fatalf("version = %d, want %d", v, versionCmdmeta)
	}
	assertCmdmeta(t, d)

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM histxversion`).Scan(&n); err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if n != 1 {
		t.Fatalf("histxversion rows = %d, want 1", n)
	}
}

func TestFreshFileOpenRecordsVersions1And2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "histx.db")
	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	v, err := version(d)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v != versionCmdmeta {
		t.Fatalf("MAX(version) = %d, want %d", v, versionCmdmeta)
	}
	assertVersions(t, d, versionCWD, versionCmdmeta)
	assertCWD(t, d, true)
	assertCmdmeta(t, d)
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	d, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer d.Close()

	v, err = version(d)
	if err != nil {
		t.Fatalf("version after reopen: %v", err)
	}
	if v != versionCmdmeta {
		t.Fatalf("MAX(version) after reopen = %d, want %d", v, versionCmdmeta)
	}
	assertVersions(t, d, versionCWD, versionCmdmeta)

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM histxversion`).Scan(&n); err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if n != 2 {
		t.Fatalf("histxversion rows = %d, want 2", n)
	}
	// assertCmdmeta inserted this row. Reopening must not rebuild cmdmeta empty.
	var value string
	if err := d.QueryRow(
		`SELECT value FROM cmdmeta WHERE hash='h' AND key='source'`,
	).Scan(&value); err != nil {
		t.Fatalf("cmdmeta row after reopen: %v", err)
	}
	if value != "user" {
		t.Fatalf("cmdmeta value = %q, want user", value)
	}
}

func TestVersion2WithCmdmetaIsNotRewritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "histx.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := d.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	if _, err := d.Exec(`ALTER TABLE cmdraw ADD COLUMN cwd TEXT`); err != nil {
		t.Fatalf("add cwd: %v", err)
	}
	if _, err := d.Exec(`
		CREATE TABLE cmdmeta (
			hash  TEXT,
			key   TEXT,
			value TEXT,
			UNIQUE(hash, key)
		)`); err != nil {
		t.Fatalf("create cmdmeta: %v", err)
	}
	if _, err := d.Exec(`CREATE INDEX idx_cmdmeta_hash ON cmdmeta(hash)`); err != nil {
		t.Fatalf("create index: %v", err)
	}
	const whence = 424242
	if _, err := d.Exec(
		`INSERT INTO histxversion(version, whence) VALUES(?, ?)`,
		versionCmdmeta, whence,
	); err != nil {
		t.Fatalf("insert version: %v", err)
	}
	if _, err := d.Exec(
		`INSERT INTO cmdraw(hash, ts, cmd, cwd) VALUES('hash-keep', 1710000001, 'echo keep', '/work')`,
	); err != nil {
		t.Fatalf("insert cmdraw: %v", err)
	}
	if _, err := d.Exec(
		`INSERT INTO cmdmeta(hash, key, value) VALUES('hash-keep', 'source', 'claude')`,
	); err != nil {
		t.Fatalf("insert cmdmeta: %v", err)
	}
	before := migratedState(t, d)
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	d, err = Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	after := migratedState(t, d)
	if before != after {
		t.Fatalf("version-2 database changed\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestDuplicateColumnFileOpenRecordsVersion1And2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "histx.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := d.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	if _, err := d.Exec(`ALTER TABLE cmdraw ADD COLUMN cwd TEXT`); err != nil {
		t.Fatalf("add cwd: %v", err)
	}
	if _, err := d.Exec(
		`INSERT INTO cmdraw(hash, ts, cmd, cwd) VALUES('hash-cwd', 1710000002, 'echo cwd', '/already')`,
	); err != nil {
		t.Fatalf("insert cmdraw: %v", err)
	}
	before := commandRows(t, d)
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	d, err = Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	v, err := version(d)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v != versionCmdmeta {
		t.Fatalf("MAX(version) = %d, want %d", v, versionCmdmeta)
	}
	assertVersions(t, d, versionCWD, versionCmdmeta)
	assertCWD(t, d, true)
	if got := commandRows(t, d); got != before {
		t.Fatalf("command rows = %q, want %q", got, before)
	}
	assertCmdmeta(t, d)
}

func TestMigration2FailureRollsBackAndNextOpenCompletes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "histx.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := d.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	if err := migration1(d); err != nil {
		t.Fatalf("migration1: %v", err)
	}
	if _, err := d.Exec(
		`INSERT INTO cmdraw(hash, ts, cmd, cwd) VALUES('hash-stay', 1710000003, 'echo stay', '/home')`,
	); err != nil {
		t.Fatalf("insert cmdraw: %v", err)
	}
	before := commandRows(t, d)
	if _, err := d.Exec(`
		CREATE TRIGGER fail_v2 BEFORE INSERT ON histxversion
		WHEN NEW.version = 2
		BEGIN
			SELECT RAISE(ABORT, 'fail version 2');
		END`); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := Open(path); err == nil {
		t.Fatal("Open succeeded, want migration 2 error")
	}

	d, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	v, err := version(d)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if v != versionCWD {
		t.Fatalf("version after failed migration 2 = %d, want %d", v, versionCWD)
	}
	assertVersions(t, d, versionCWD)
	exists, err := tableExists(d, "cmdmeta")
	if err != nil {
		t.Fatalf("tableExists: %v", err)
	}
	if exists {
		t.Fatal("cmdmeta exists after rolled-back migration 2")
	}
	if got := commandRows(t, d); got != before {
		t.Fatalf("command rows after failed Open = %q, want %q", got, before)
	}
	if _, err := d.Exec(`DROP TRIGGER fail_v2`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	d, err = Open(path)
	if err != nil {
		t.Fatalf("retry Open: %v", err)
	}
	defer d.Close()

	v, err = version(d)
	if err != nil {
		t.Fatalf("version after retry: %v", err)
	}
	if v != versionCmdmeta {
		t.Fatalf("version after retry = %d, want %d", v, versionCmdmeta)
	}
	assertVersions(t, d, versionCWD, versionCmdmeta)
	if got := commandRows(t, d); got != before {
		t.Fatalf("command rows after retry = %q, want %q", got, before)
	}
	assertCmdmeta(t, d)
}

func TestOpenDoesNotRewriteExistingCommandRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "histx.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := d.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	if _, err := d.Exec(
		`INSERT INTO cmdraw(hash, ts, cmd) VALUES(?, ?, ?), (?, ?, ?)`,
		"pre-a", int64(555), "echo 'kept'",
		"pre-b", int64(556), "ls /tmp",
	); err != nil {
		t.Fatalf("insert cmdraw: %v", err)
	}
	before := commandIdentity(t, d)
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	d, err = Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	if got := commandIdentity(t, d); got != before {
		t.Fatalf("command identity = %q, want %q", got, before)
	}
	var n int
	if err := d.QueryRow(
		`SELECT COUNT(*) FROM cmdraw WHERE cwd IS NOT NULL`,
	).Scan(&n); err != nil {
		t.Fatalf("count cwd: %v", err)
	}
	if n != 0 {
		t.Fatalf("rows with cwd = %d, want 0", n)
	}
	assertVersions(t, d, versionCWD, versionCmdmeta)
}

func openBare(t *testing.T, path string) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", path, err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	return d
}

func assertVersions(t *testing.T, d *sql.DB, want ...int) {
	t.Helper()
	rows, err := d.Query(`SELECT version FROM histxversion ORDER BY version`)
	if err != nil {
		t.Fatalf("select versions: %v", err)
	}
	defer rows.Close()

	var got []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan version: %v", err)
		}
		got = append(got, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("versions: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("versions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("versions = %v, want %v", got, want)
		}
	}
}

func assertCWD(t *testing.T, d *sql.DB, want bool) {
	t.Helper()
	var n int
	err := d.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('cmdraw') WHERE name='cwd'`,
	).Scan(&n)
	if err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	got := n > 0
	if got != want {
		t.Fatalf("cmdraw.cwd present = %v, want %v", got, want)
	}
}

func assertCmdmeta(t *testing.T, d *sql.DB) {
	t.Helper()
	exists, err := tableExists(d, "cmdmeta")
	if err != nil {
		t.Fatalf("tableExists: %v", err)
	}
	if !exists {
		t.Fatal("cmdmeta table missing")
	}

	cols := map[string]bool{}
	rows, err := d.Query(`SELECT name FROM pragma_table_info('cmdmeta')`)
	if err != nil {
		t.Fatalf("cmdmeta columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("cmdmeta columns: %v", err)
	}
	for _, name := range []string{"hash", "key", "value"} {
		if !cols[name] {
			t.Fatalf("cmdmeta missing column %q (have %v)", name, cols)
		}
	}

	var index string
	err = d.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_cmdmeta_hash'`,
	).Scan(&index)
	if err != nil {
		t.Fatalf("idx_cmdmeta_hash: %v", err)
	}

	if _, err := d.Exec(
		`INSERT INTO cmdmeta(hash, key, value) VALUES('h', 'source', 'user')`,
	); err != nil {
		t.Fatalf("insert cmdmeta: %v", err)
	}
	_, err = d.Exec(
		`INSERT INTO cmdmeta(hash, key, value) VALUES('h', 'source', 'claude')`,
	)
	if err == nil {
		t.Fatal("duplicate (hash, key) insert succeeded")
	}
}

// migratedState is the version, schema, and stored rows of a database that
// already has cwd and cmdmeta. Open must leave it unchanged.
func migratedState(t *testing.T, d *sql.DB) string {
	t.Helper()
	return strings.Join([]string{
		queryLines(t, d, `SELECT version || ',' || whence FROM histxversion ORDER BY version`),
		queryLines(t, d, `SELECT name || '|' || type || '|' || ifnull(sql, '') FROM sqlite_master WHERE name IN ('cmdraw', 'cmdmeta', 'idx_cmdmeta_hash', 'histxversion') ORDER BY name`),
		commandRows(t, d),
		queryLines(t, d, `SELECT rowid || '|' || hash || '|' || key || '|' || value FROM cmdmeta ORDER BY rowid`),
	}, "\n")
}

func commandRows(t *testing.T, d *sql.DB) string {
	t.Helper()
	return queryLines(t, d, `SELECT rowid || '|' || hash || '|' || ts || '|' || cmd || '|' || ifnull(cwd, '<null>') FROM cmdraw ORDER BY rowid`)
}

// commandIdentity is hash, timestamp, command text, and rowid before cwd exists.
func commandIdentity(t *testing.T, d *sql.DB) string {
	t.Helper()
	return queryLines(t, d, `SELECT rowid || '|' || hash || '|' || ts || '|' || cmd FROM cmdraw ORDER BY rowid`)
}

func queryLines(t *testing.T, d *sql.DB, q string) string {
	t.Helper()
	rows, err := d.Query(q)
	if err != nil {
		t.Fatalf("query %s: %v", q, err)
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan: %v", err)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("query %s: %v", q, err)
	}
	return b.String()
}
