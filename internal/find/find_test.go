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
		if err := index.Cmd(d, c, "/", nil); err != nil {
			t.Fatalf("index.Cmd(%q): %v", c, err)
		}
		time.Sleep(2 * time.Millisecond) // ensure distinct timestamps
	}

	res, err := Cmd(d, []string{"git", "status"}, 5, "", "user")
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
		if err := index.Cmd(d, c, "/", nil); err != nil {
			t.Fatalf("index.Cmd(%q): %v", c, err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	// "ls" is 2 chars -> triggers ACS path
	res, err := Cmd(d, []string{"ls"}, 5, "", "user")
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

	if err := index.Cmd(d, "git status", "/", nil); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	res, err := Cmd(d, []string{"zzznomatch"}, 5, "", "user")
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

	if err := index.Cmd(d, "git status", "/", nil); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	res, err := Cmd(d, []string{""}, 5, "", "user")
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
		if err := index.Cmd(d, cmd, "/", nil); err != nil {
			t.Fatalf("index.Cmd: %v", err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	res, err := Cmd(d, []string{"testing"}, 3, "", "user")
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

	if err := index.Cmd(d, "make build", "/home/user/project", nil); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	res, err := Cmd(d, []string{"make"}, 5, "", "user")
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

	if err := index.Cmd(d, "make test", "/home/user/project", nil); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}
	if err := index.Cmd(d, "make build", "/other/project", nil); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	res, err := Cmd(d, []string{"make"}, 5, "/home/user/project", "user")
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

	if err := index.Cmd(d, "make test", "/home/user/project", nil); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	// Search from a different directory — no local match.
	res, err := Cmd(d, []string{"make"}, 5, "/tmp", "user")
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

	if err := index.Cmd(d, "make test", "/home/user/project", nil); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	// Empty cwdFilter = global (no filtering).
	res, err := Cmd(d, []string{"make"}, 5, "", "user")
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

func TestFindSourceFilterUser(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()
	if err := index.Cmd(d, "git status", "/", nil); err != nil {
		t.Fatalf("index user: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := index.Cmd(d, "kubectl apply", "/", map[string]string{"source": "claude"}); err != nil {
		t.Fatalf("index claude: %v", err)
	}

	// "user" filter should only return the shell-indexed command.
	res, err := Cmd(d, []string{"git"}, 5, "", "user")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Cmd != "git status" {
		t.Errorf("user filter: got %v, want [git status]", hitCmds(res.Hits))
	}
	for _, h := range res.Hits {
		if h.Source == "claude" {
			t.Error("user filter returned a claude-sourced command")
		}
	}
}

func TestFindSourceFilterClaude(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()
	if err := index.Cmd(d, "git status", "/", nil); err != nil {
		t.Fatalf("index user: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := index.Cmd(d, "kubectl apply", "/", map[string]string{"source": "claude"}); err != nil {
		t.Fatalf("index claude: %v", err)
	}

	res, err := Cmd(d, []string{"kubectl"}, 5, "", "claude")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Cmd != "kubectl apply" {
		t.Errorf("claude filter: got %v, want [kubectl apply]", hitCmds(res.Hits))
	}
	if res.Hits[0].Source != "claude" {
		t.Errorf("Source = %q, want \"claude\"", res.Hits[0].Source)
	}
}

func TestFindSourceFilterAll(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()
	if err := index.Cmd(d, "make test", "/", nil); err != nil {
		t.Fatalf("index user: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := index.Cmd(d, "make build", "/", map[string]string{"source": "claude"}); err != nil {
		t.Fatalf("index claude: %v", err)
	}

	res, err := Cmd(d, []string{"make"}, 5, "", "all")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) != 2 {
		t.Errorf("all filter: got %d hits, want 2", len(res.Hits))
	}
}

func TestFindSourceFilterUserExcludesClaude(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()
	if err := index.Cmd(d, "make build", "/", map[string]string{"source": "claude"}); err != nil {
		t.Fatalf("index claude: %v", err)
	}

	// Only a claude command exists; user filter should return nothing.
	res, err := Cmd(d, []string{"make"}, 5, "", "user")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Errorf("user filter returned claude command: %v", hitCmds(res.Hits))
	}
}

func TestFindSourceFilterClaudeExcludesUser(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()
	if err := index.Cmd(d, "make build", "/", nil); err != nil {
		t.Fatalf("index user: %v", err)
	}

	// Only a user command exists; claude filter should return nothing.
	res, err := Cmd(d, []string{"make"}, 5, "", "claude")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Errorf("claude filter returned user command: %v", hitCmds(res.Hits))
	}
}

func hitCmds(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Cmd
	}
	return out
}
