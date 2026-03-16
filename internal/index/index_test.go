package index

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/mwirges/ghistx/internal/db"
)

func TestHashVector(t *testing.T) {
	// From test.c: sha256("histx test hash") == 06afb70aa2b22ddc...
	got := Hash("histx test hash")
	want := "06afb70aa2b22ddc874af3881454dca9d6cfd4fedc81b36f85928f0ac3c752d1"
	if got != want {
		t.Errorf("Hash = %q, want %q", got, want)
	}
}

func TestIndexAndVerify(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	cmd := "git status"
	cwd := "/home/user"

	if err := Cmd(d, cmd, cwd); err != nil {
		t.Fatalf("Cmd: %v", err)
	}

	// Verify cmdraw row.
	var hash, storedCmd, storedCWD string
	var ts int64
	err = d.QueryRow(`SELECT hash, cmd, cwd, ts FROM cmdraw WHERE hash = ?`, Hash(cmd)).
		Scan(&hash, &storedCmd, &storedCWD, &ts)
	if err != nil {
		t.Fatalf("query cmdraw: %v", err)
	}
	if hash != Hash(cmd) {
		t.Errorf("hash mismatch: got %q, want %q", hash, Hash(cmd))
	}
	decoded, _ := base64.StdEncoding.DecodeString(storedCmd)
	if string(decoded) != cmd {
		t.Errorf("cmd round-trip: got %q, want %q", decoded, cmd)
	}
	decodedCWD, _ := base64.StdEncoding.DecodeString(storedCWD)
	if string(decodedCWD) != cwd {
		t.Errorf("cwd round-trip: got %q, want %q", decodedCWD, cwd)
	}
	if ts <= 0 {
		t.Error("ts should be positive")
	}

	// Verify cmdlut rows exist.
	var count int
	d.QueryRow(`SELECT COUNT(*) FROM cmdlut WHERE hash = ?`, Hash(cmd)).Scan(&count)
	if count == 0 {
		t.Error("no cmdlut rows inserted")
	}
}

func TestIndexDuplicateIdempotent(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	cmd := "ls -la"
	if err := Cmd(d, cmd, "/"); err != nil {
		t.Fatalf("first Cmd: %v", err)
	}
	if err := Cmd(d, cmd, "/"); err != nil {
		t.Fatalf("second Cmd: %v", err)
	}

	var count int
	d.QueryRow(`SELECT COUNT(*) FROM cmdraw WHERE hash = ?`, Hash(cmd)).Scan(&count)
	if count != 1 {
		t.Errorf("cmdraw count = %d, want 1 (duplicate should replace)", count)
	}
}

func TestIndexManyCommands(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	for i := 0; i < 10; i++ {
		cmd := fmt.Sprintf("echo command %d", i)
		if err := Cmd(d, cmd, "/tmp"); err != nil {
			t.Fatalf("Cmd(%q): %v", cmd, err)
		}
	}

	var count int
	d.QueryRow(`SELECT COUNT(*) FROM cmdraw`).Scan(&count)
	if count != 10 {
		t.Errorf("cmdraw count = %d, want 10", count)
	}
}
