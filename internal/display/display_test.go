package display

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mwirges/ghistx/internal/find"
)

// writeToBuffer is a helper that calls Render with a bytes.Buffer as colorRef.
// Since a bytes.Buffer is not a terminal, lipgloss emits no ANSI codes,
// making it easy to assert on plain text content.
func renderPlain(hits []find.Hit) string {
	var buf bytes.Buffer
	return Render(hits, &buf)
}

func TestRenderEmpty(t *testing.T) {
	got := renderPlain(nil)
	if got != "" {
		t.Errorf("Render(nil) = %q, want empty string", got)
	}
	got = renderPlain([]find.Hit{})
	if got != "" {
		t.Errorf("Render([]) = %q, want empty string", got)
	}
}

func TestRenderSingleHit(t *testing.T) {
	hits := []find.Hit{
		{
			Hash: strings.Repeat("a", 64),
			Cmd:  "git status",
			TS:   0,
		},
	}
	got := renderPlain(hits)
	if !strings.Contains(got, "git status") {
		t.Errorf("output missing cmd: %q", got)
	}
	// Hashlet should be the first 4 chars of the hash.
	if !strings.Contains(got, "aaaa") {
		t.Errorf("output missing hashlet prefix: %q", got)
	}
	// Should have a newline at the end.
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("output should end with newline: %q", got)
	}
}

func TestRenderIncludesCWD(t *testing.T) {
	hits := []find.Hit{
		{
			Hash: strings.Repeat("b", 64),
			Cmd:  "make test",
			CWD:  "/home/user/project",
			TS:   0,
		},
	}
	got := renderPlain(hits)
	if !strings.Contains(got, "/home/user/project") {
		t.Errorf("output missing CWD: %q", got)
	}
}

func TestRenderOmitsCWDWhenEmpty(t *testing.T) {
	hits := []find.Hit{
		{
			Hash: strings.Repeat("c", 64),
			Cmd:  "ls -la",
			CWD:  "",
			TS:   0,
		},
	}
	got := renderPlain(hits)
	// Should not contain empty parens.
	if strings.Contains(got, "()") {
		t.Errorf("output should not contain empty parens: %q", got)
	}
}

func TestRenderMultipleHitsHashletLength(t *testing.T) {
	// Two hashes differing at position 5 — hashlet length must be >= 6.
	h1 := "aaaaa0" + strings.Repeat("0", 58)
	h2 := "aaaaa1" + strings.Repeat("1", 58)
	hits := []find.Hit{
		{Hash: h1, Cmd: "cmd one", TS: 1},
		{Hash: h2, Cmd: "cmd two", TS: 2},
	}
	got := renderPlain(hits)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), got)
	}
	// Each line should start with a hashlet of length >= 6.
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			t.Fatal("empty line in output")
		}
		if len(fields[0]) < 6 {
			t.Errorf("hashlet %q is shorter than expected minimum 6", fields[0])
		}
	}
}

func TestRenderEachHitOnOwnLine(t *testing.T) {
	hits := []find.Hit{
		{Hash: strings.Repeat("1", 64), Cmd: "first", TS: 1},
		{Hash: strings.Repeat("2", 64), Cmd: "second", TS: 2},
		{Hash: strings.Repeat("3", 64), Cmd: "third", TS: 3},
	}
	got := renderPlain(hits)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	for i, cmd := range []string{"first", "second", "third"} {
		if !strings.Contains(lines[i], cmd) {
			t.Errorf("line %d missing %q: %q", i, cmd, lines[i])
		}
	}
}

func TestPrintHitsWritesToWriter(t *testing.T) {
	hits := []find.Hit{
		{Hash: strings.Repeat("d", 64), Cmd: "docker ps", TS: 0},
	}
	var buf bytes.Buffer
	if err := PrintHits(&buf, hits); err != nil {
		t.Fatalf("PrintHits: %v", err)
	}
	if !strings.Contains(buf.String(), "docker ps") {
		t.Errorf("PrintHits output missing cmd: %q", buf.String())
	}
}

func TestPrintHitsEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintHits(&buf, nil); err != nil {
		t.Fatalf("PrintHits(nil): %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("PrintHits(nil) wrote %d bytes, want 0", buf.Len())
	}
}
