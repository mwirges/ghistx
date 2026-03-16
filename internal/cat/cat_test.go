package cat

import (
	"testing"
	"time"

	"github.com/mwirges/ghistx/internal/db"
	"github.com/mwirges/ghistx/internal/index"
)

func TestCatOldestFirst(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	// Index three commands with distinct timestamps.
	cmds := []string{"first command", "second command", "third command"}
	for _, c := range cmds {
		if err := index.Cmd(d, c, "/"); err != nil {
			t.Fatalf("index.Cmd(%q): %v", c, err)
		}
		time.Sleep(5 * time.Millisecond) // ensure ts ordering
	}

	hits, err := Cmd(d)
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("len(hits) = %d, want 3", len(hits))
	}

	// Verify ascending order.
	for i := 1; i < len(hits); i++ {
		if hits[i].TS < hits[i-1].TS {
			t.Errorf("hits[%d].TS (%d) < hits[%d].TS (%d): not sorted oldest-first",
				i, hits[i].TS, i-1, hits[i-1].TS)
		}
	}

	// Verify content matches insertion order.
	for i, c := range cmds {
		if hits[i].Cmd != c {
			t.Errorf("hits[%d].Cmd = %q, want %q", i, hits[i].Cmd, c)
		}
	}
}

func TestCatEmpty(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	hits, err := Cmd(d)
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits for empty DB, got %d", len(hits))
	}
}

func TestCatCWDPreserved(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	if err := index.Cmd(d, "make test", "/home/user/project"); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	hits, err := Cmd(d)
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].CWD != "/home/user/project" {
		t.Errorf("CWD = %q, want \"/home/user/project\"", hits[0].CWD)
	}
}
