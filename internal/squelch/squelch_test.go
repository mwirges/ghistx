package squelch

import (
	"strings"
	"testing"

	"github.com/mwirges/ghistx/internal/find"
)

// helpers

func compile(t *testing.T, patterns ...string) []Pattern {
	t.Helper()
	compiled, warnings := Compile(patterns)
	for _, w := range warnings {
		t.Errorf("unexpected warning: %s", w)
	}
	return compiled
}

func hits(cmds ...string) []find.Hit {
	out := make([]find.Hit, len(cmds))
	for i, c := range cmds {
		out[i] = find.Hit{Cmd: c}
	}
	return out
}

func cmdNames(hs []find.Hit) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Cmd
	}
	return out
}

// --- Exact match ---

func TestFilterExactMatch(t *testing.T) {
	p := compile(t, "ls", "cd", "pwd")
	input := hits("ls", "git status", "cd", "make test", "pwd", "docker ps")
	got := Filter(input, p)
	want := []string{"git status", "make test", "docker ps"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", cmdNames(got), want)
	}
	for i, h := range got {
		if h.Cmd != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, h.Cmd, want[i])
		}
	}
}

func TestFilterExactMatchOnly(t *testing.T) {
	// "ls" should NOT squelch "ls -la", "lsof", or "ls -ltr".
	p := compile(t, "ls")
	input := hits("ls", "ls -la", "lsof", "ls -ltr")
	got := Filter(input, p)
	want := []string{"ls -la", "lsof", "ls -ltr"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", cmdNames(got), want)
	}
}

func TestFilterExactCaseSensitive(t *testing.T) {
	p := compile(t, "LS")
	input := hits("ls", "LS", "Ls")
	got := Filter(input, p)
	want := []string{"ls", "Ls"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", cmdNames(got), want)
	}
	for i, h := range got {
		if h.Cmd != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, h.Cmd, want[i])
		}
	}
}

func TestFilterCdDotDotIsDistinctFromCd(t *testing.T) {
	// "cd" and "cd .." are different exact patterns.
	p := compile(t, "cd")
	input := hits("cd", "cd ..", "cd ~")
	got := Filter(input, p)
	want := []string{"cd ..", "cd ~"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", cmdNames(got), want)
	}
}

// --- Glob ---

func TestFilterGlobStar(t *testing.T) {
	// filepath.Match treats '/' as a separator, so '*' does NOT match '/'.
	// "glob:ls *" matches "ls -la" but NOT "ls /tmp" (slash blocks the *).
	// Bare "ls" and "lsof" are also not matched.
	p := compile(t, "glob:ls *")
	input := hits("ls", "ls -la", "ls /tmp", "lsof", "git status")
	got := Filter(input, p)
	want := []string{"ls", "ls /tmp", "lsof", "git status"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", cmdNames(got), want)
	}
}

func TestFilterGlobMatchesAllLs(t *testing.T) {
	// "glob:ls*" matches "ls", "ls -la", "ls -ltr", "lsof"
	p := compile(t, "glob:ls*")
	input := hits("ls", "ls -la", "lsof", "git status")
	got := Filter(input, p)
	want := []string{"git status"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", cmdNames(got), want)
	}
}

func TestFilterGlobQuestion(t *testing.T) {
	// "glob:cd ?" matches "cd ~" and "cd ." but not "cd .."
	p := compile(t, "glob:cd ?")
	input := hits("cd ~", "cd .", "cd ..", "cd")
	got := Filter(input, p)
	want := []string{"cd ..", "cd"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", cmdNames(got), want)
	}
}

func TestFilterInvalidGlobWarning(t *testing.T) {
	_, warnings := Compile([]string{"glob:["}) // unclosed bracket
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "invalid glob") {
		t.Errorf("warning should mention 'invalid glob', got: %s", warnings[0])
	}
}

func TestFilterInvalidGlobSkipped(t *testing.T) {
	// Invalid glob should be skipped; valid patterns still apply.
	compiled, warnings := Compile([]string{"glob:[", "ls"})
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	input := hits("ls", "git status")
	got := Filter(input, compiled)
	// Only "ls" should be filtered (the invalid glob is skipped).
	if len(got) != 1 || got[0].Cmd != "git status" {
		t.Errorf("got %v, want [git status]", cmdNames(got))
	}
}

// --- Regex ---

func TestFilterRegexBasic(t *testing.T) {
	p := compile(t, `regex:^git\s`)
	input := hits("git status", "git diff", "git commit -m msg", "docker ps", "gitk")
	got := Filter(input, p)
	want := []string{"docker ps", "gitk"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", cmdNames(got), want)
	}
}

func TestFilterRegexAnchored(t *testing.T) {
	// "regex:^cd$" matches only bare "cd", not "cd .." or "cdx"
	p := compile(t, `regex:^cd$`)
	input := hits("cd", "cd ..", "cdx")
	got := Filter(input, p)
	want := []string{"cd ..", "cdx"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", cmdNames(got), want)
	}
}

func TestFilterInvalidRegexWarning(t *testing.T) {
	_, warnings := Compile([]string{`regex:[invalid`})
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "invalid regex") {
		t.Errorf("warning should mention 'invalid regex', got: %s", warnings[0])
	}
}

func TestFilterInvalidRegexSkipped(t *testing.T) {
	// Invalid regex should be skipped; valid patterns still apply.
	compiled, warnings := Compile([]string{`regex:[invalid`, "ls"})
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	input := hits("ls", "git status")
	got := Filter(input, compiled)
	if len(got) != 1 || got[0].Cmd != "git status" {
		t.Errorf("got %v, want [git status]", cmdNames(got))
	}
}

func TestFilterMixedWarnings(t *testing.T) {
	// Multiple invalid patterns each produce a warning.
	_, warnings := Compile([]string{`regex:[bad`, `glob:[also_bad`, "ls"})
	if len(warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d: %v", len(warnings), warnings)
	}
}

// --- Mixed pattern types ---

func TestFilterMixedTypes(t *testing.T) {
	p := compile(t, "pwd", "glob:ls *", `regex:^cd(\s|$)`)
	input := hits("pwd", "ls -la", "ls", "cd", "cd ..", "git status", "make test")
	got := Filter(input, p)
	// "pwd" → exact match
	// "ls -la" → glob "ls *" matches
	// "ls" → glob "ls *" does NOT match (no space+suffix); exact "ls" not listed
	// "cd" → regex ^cd(\s|$) matches
	// "cd .." → regex ^cd(\s|$) matches (space after cd)
	// "git status" and "make test" → no match
	// "ls" → not squelched
	want := []string{"ls", "git status", "make test"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", cmdNames(got), want)
	}
	for i, h := range got {
		if h.Cmd != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, h.Cmd, want[i])
		}
	}
}

// --- Edge cases ---

func TestFilterEmptyPatterns(t *testing.T) {
	input := hits("ls", "cd", "git status")
	got := Filter(input, nil)
	if len(got) != len(input) {
		t.Errorf("nil patterns: got %d hits, want %d", len(got), len(input))
	}
	got = Filter(input, []Pattern{})
	if len(got) != len(input) {
		t.Errorf("empty slice: got %d hits, want %d", len(got), len(input))
	}
}

func TestFilterEmptyHits(t *testing.T) {
	p := compile(t, "ls")
	got := Filter(nil, p)
	if len(got) != 0 {
		t.Errorf("expected 0 hits, got %d", len(got))
	}
}

func TestFilterAllSquelched(t *testing.T) {
	p := compile(t, "ls", "cd")
	got := Filter(hits("ls", "cd"), p)
	if len(got) != 0 {
		t.Errorf("expected all filtered, got %v", cmdNames(got))
	}
}

func TestFilterPreservesHitFields(t *testing.T) {
	p := compile(t, "cd")
	input := []find.Hit{
		{Hash: "abc123", Cmd: "git status", CWD: "/home/user", TS: 12345, Source: "user"},
		{Hash: "def456", Cmd: "cd", CWD: "/home/user", TS: 12346},
	}
	got := Filter(input, p)
	if len(got) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(got))
	}
	h := got[0]
	if h.Hash != "abc123" || h.CWD != "/home/user" || h.TS != 12345 || h.Source != "user" {
		t.Errorf("hit fields not preserved: %+v", h)
	}
}

func TestCompileNoWarningsForValidPatterns(t *testing.T) {
	_, warnings := Compile([]string{"ls", "glob:cd *", `regex:^git\s+`})
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got: %v", warnings)
	}
}

// --- ActiveList ---

func TestActiveListDefault(t *testing.T) {
	list := ActiveList(false, nil)
	if len(list) != len(DefaultList) {
		t.Errorf("expected %d entries, got %d", len(DefaultList), len(list))
	}
}

func TestActiveListMergesUserPatterns(t *testing.T) {
	user := []string{"make clean", "go mod tidy"}
	list := ActiveList(false, user)
	if len(list) != len(DefaultList)+len(user) {
		t.Errorf("expected %d entries, got %d", len(DefaultList)+len(user), len(list))
	}
	if list[len(list)-2] != "make clean" || list[len(list)-1] != "go mod tidy" {
		t.Errorf("user patterns not at end: %v", list[len(list)-2:])
	}
}

func TestActiveListClearDefaults(t *testing.T) {
	list := ActiveList(true, []string{"make clean"})
	if len(list) != 1 || list[0] != "make clean" {
		t.Errorf("expected [make clean], got %v", list)
	}
}

func TestActiveListClearDefaultsNoUser(t *testing.T) {
	list := ActiveList(true, nil)
	if len(list) != 0 {
		t.Errorf("expected empty list, got %v", list)
	}
}

func TestActiveListDoesNotMutateDefaultList(t *testing.T) {
	original := make([]string, len(DefaultList))
	copy(original, DefaultList)

	ActiveList(false, []string{"extra1", "extra2"})

	if len(DefaultList) != len(original) {
		t.Errorf("DefaultList was mutated: len %d → %d", len(original), len(DefaultList))
	}
	for i, v := range original {
		if DefaultList[i] != v {
			t.Errorf("DefaultList[%d] changed from %q to %q", i, v, DefaultList[i])
		}
	}
}

func TestDefaultListContainsExpectedEntries(t *testing.T) {
	expected := []string{"ls", "cd", "cd ..", "cd ~", "pwd", "ls -la", "ls -al", "ls -ltr", "exit", "clear"}
	set := make(map[string]struct{}, len(DefaultList))
	for _, p := range DefaultList {
		set[p] = struct{}{}
	}
	for _, e := range expected {
		if _, ok := set[e]; !ok {
			t.Errorf("expected %q in DefaultList", e)
		}
	}
}

func TestPatternRawFieldPreserved(t *testing.T) {
	raws := []string{"ls", "glob:ls *", `regex:^cd`}
	compiled, _ := Compile(raws)
	for i, p := range compiled {
		if p.Raw != raws[i] {
			t.Errorf("compiled[%d].Raw = %q, want %q", i, p.Raw, raws[i])
		}
	}
}
