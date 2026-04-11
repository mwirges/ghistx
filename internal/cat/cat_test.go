package cat

import (
	"testing"
	"time"

	"github.com/mwirges/ghistx/internal/db"
	"github.com/mwirges/ghistx/internal/find"
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

	hits, err := Cmd(d, "", "user", 0)
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

	hits, err := Cmd(d, "", "user", 0)
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

	hits, err := Cmd(d, "", "user", 0)
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

func TestCatCWDFilter(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	if err := index.Cmd(d, "make test", "/home/user/project"); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}
	if err := index.Cmd(d, "make build", "/other/project"); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	// Filter to /home/user/project — only one result.
	hits, err := Cmd(d, "/home/user/project", "user", 0)
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit with CWD filter, got %d", len(hits))
	}
	if hits[0].Cmd != "make test" {
		t.Errorf("hit = %q, want \"make test\"", hits[0].Cmd)
	}

	// Empty filter returns all.
	hits, err = Cmd(d, "", "user", 0)
	if err != nil {
		t.Fatalf("Cmd (global): %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("expected 2 hits for global, got %d", len(hits))
	}
}

func TestCatSourceFilterUser(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	if err := index.Cmd(d, "git status", "/"); err != nil {
		t.Fatalf("index user: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := index.Cmd(d, "kubectl apply", "/", "claude"); err != nil {
		t.Fatalf("index claude: %v", err)
	}

	// Default "user" filter: only shell-indexed command.
	hits, err := Cmd(d, "", "user", 0)
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(hits) != 1 || hits[0].Cmd != "git status" {
		t.Errorf("user filter: got %v, want [git status]", cmdList(hits))
	}
}

func TestCatSourceFilterClaude(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	if err := index.Cmd(d, "git status", "/"); err != nil {
		t.Fatalf("index user: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := index.Cmd(d, "kubectl apply", "/", "claude"); err != nil {
		t.Fatalf("index claude: %v", err)
	}

	hits, err := Cmd(d, "", "claude", 0)
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(hits) != 1 || hits[0].Cmd != "kubectl apply" {
		t.Errorf("claude filter: got %v, want [kubectl apply]", cmdList(hits))
	}
	if hits[0].Source != "claude" {
		t.Errorf("Source = %q, want \"claude\"", hits[0].Source)
	}
}

func TestCatSourceFilterAll(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	if err := index.Cmd(d, "git status", "/"); err != nil {
		t.Fatalf("index user: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := index.Cmd(d, "kubectl apply", "/", "claude"); err != nil {
		t.Fatalf("index claude: %v", err)
	}

	hits, err := Cmd(d, "", "all", 0)
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("all filter: got %d hits, want 2", len(hits))
	}
}

func cmdList(hits []find.Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Cmd
	}
	return out
}
