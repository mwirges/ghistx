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

	res, err := Cmd(d, []string{"git", "status"}, 5, "")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	if res.Hits[0].Cmd != "git status" {
		t.Errorf("first hit = %q, want %q", res.Hits[0].Cmd, "git status")
	}
	if res.IsGlobal {
		t.Error("IsGlobal should be false for empty cwdFilter")
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
	res, err := Cmd(d, []string{"ls"}, 5, "")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	for _, h := range res.Hits {
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

	res, err := Cmd(d, []string{"zzznomatch"}, 5, "")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Errorf("expected 0 hits, got %d", len(res.Hits))
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

	res, err := Cmd(d, []string{""}, 5, "")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Errorf("expected 0 hits for empty keyword, got %d", len(res.Hits))
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

	res, err := Cmd(d, []string{"testing"}, 3, "")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) > 3 {
		t.Errorf("expected at most 3 hits, got %d", len(res.Hits))
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

	res, err := Cmd(d, []string{"make"}, 5, "")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("expected hit")
	}
	if res.Hits[0].CWD != "/home/user/project" {
		t.Errorf("CWD = %q, want \"/home/user/project\"", res.Hits[0].CWD)
	}
}

func TestFindCWDFilterLocalMatch(t *testing.T) {
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

	res, err := Cmd(d, []string{"make"}, 5, "/home/user/project")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("expected 1 local hit, got %d", len(res.Hits))
	}
	if res.Hits[0].Cmd != "make test" {
		t.Errorf("hit = %q, want \"make test\"", res.Hits[0].Cmd)
	}
	if res.IsGlobal {
		t.Error("IsGlobal should be false when local results exist")
	}
}

func TestFindCWDFilterNoLocalFallsBack(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	if err := index.Cmd(d, "make test", "/home/user/project"); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	// Search from a different directory — no local match.
	res, err := Cmd(d, []string{"make"}, 5, "/tmp")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("expected global fallback to return hits")
	}
	if !res.IsGlobal {
		t.Error("IsGlobal should be true after local-miss fallback")
	}
}

func TestFindCWDFilterGlobalOverride(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	if err := index.Cmd(d, "make test", "/home/user/project"); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	// Empty cwdFilter = global (no filtering).
	res, err := Cmd(d, []string{"make"}, 5, "")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	if res.IsGlobal {
		t.Error("IsGlobal should be false when cwdFilter is empty")
	}
}
