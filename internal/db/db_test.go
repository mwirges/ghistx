package db

import (
	"testing"
)

func TestOpenInMemory(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(\":memory:\") error: %v", err)
	}
	defer d.Close()

	// All expected tables must exist.
	tables := []string{"cmdraw", "cmdlut", "cmdan", "histxversion"}
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
}
