package find

import (
	"testing"
	"time"

	"github.com/mwirges/ghistx/internal/db"
	"github.com/mwirges/ghistx/internal/index"
)

func setup(t *testing.T, cmds ...string) interface {
	Query(string, ...any) (*interface{}, error)
} {
	t.Helper()
	return nil
}

func openDB(t *testing.T) interface{ Close() error } {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestFindExactMatch(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	cmds := []string{
		"git status",
		"git commit -m test",
		"ls -la",
		"docker ps",
	}
	for _, c := range cmds {
		if err := index.Cmd(d, c, "/"); err != nil {
			t.Fatalf("index.Cmd(%q): %v", c, err)
		}
		time.Sleep(2 * time.Millisecond) // ensure distinct timestamps
	}

	hits, err := Cmd(d, []string{"git", "status"}, 5)
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	if hits[0].Cmd != "git status" {
		t.Errorf("first hit = %q, want %q", hits[0].Cmd, "git status")
	}
}

func TestFindShortKeywordACS(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	cmds := []string{"ls", "ls -la", "cat foo.txt", "go build ./..."}
	for _, c := range cmds {
		if err := index.Cmd(d, c, "/"); err != nil {
			t.Fatalf("index.Cmd(%q): %v", c, err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	// "ls" is 2 chars -> triggers ACS path
	hits, err := Cmd(d, []string{"ls"}, 5)
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	for _, h := range hits {
		found := false
		for _, c := range []string{"ls", "ls -la"} {
			if h.Cmd == c {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unexpected hit: %q", h.Cmd)
		}
	}
}

func TestFindNoResults(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	if err := index.Cmd(d, "git status", "/"); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	hits, err := Cmd(d, []string{"zzznomatch"}, 5)
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits, got %d", len(hits))
	}
}

func TestFindEmptyKeywords(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	if err := index.Cmd(d, "git status", "/"); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	hits, err := Cmd(d, []string{""}, 5)
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits for empty keyword, got %d", len(hits))
	}
}

func TestFindLimit(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	for i := 0; i < 10; i++ {
		cmd := "echo testing command"
		// Make unique by appending index
		cmd = cmd + " " + string(rune('0'+i))
		if err := index.Cmd(d, cmd, "/"); err != nil {
			t.Fatalf("index.Cmd: %v", err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	hits, err := Cmd(d, []string{"testing"}, 3)
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(hits) > 3 {
		t.Errorf("expected at most 3 hits, got %d", len(hits))
	}
}

func TestFindCWDDecoded(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	if err := index.Cmd(d, "make build", "/home/user/project"); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	hits, err := Cmd(d, []string{"make"}, 5)
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hit")
	}
	if hits[0].CWD != "/home/user/project" {
		t.Errorf("CWD = %q, want \"/home/user/project\"", hits[0].CWD)
	}
}
