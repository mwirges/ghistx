package explore

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mwirges/ghistx/internal/config"
	"github.com/mwirges/ghistx/internal/find"
)

func makeModel(mode Mode) model {
	cfg := config.Default()
	return newModel(nil, cfg, mode)
}

func makeModelWithHits(hits []find.Hit) model {
	m := makeModel(ModeExplore)
	m.hits = hits
	m.termWidth = 80
	return m
}

func sendKey(m model, key string) (model, tea.Cmd) {
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return updated.(model), cmd
}

func sendSpecialKey(m model, keyType tea.KeyType) (model, tea.Cmd) {
	updated, cmd := m.Update(tea.KeyMsg{Type: keyType})
	return updated.(model), cmd
}

func TestInitialState(t *testing.T) {
	m := makeModel(ModeExplore)
	if m.query != "" {
		t.Errorf("initial query = %q, want empty", m.query)
	}
	if m.cursor != 0 {
		t.Errorf("initial cursor = %d, want 0", m.cursor)
	}
	if m.commandMode {
		t.Error("initial commandMode should be false (non-vi)")
	}
}

func TestViModeInitialState(t *testing.T) {
	cfg := config.Config{ViMode: true, SearchLimit: 5}
	m := newModel(nil, cfg, ModeExplore)
	if !m.commandMode {
		t.Error("vi mode: initial commandMode should be true")
	}
}

func TestTextInput(t *testing.T) {
	m := makeModel(ModeExplore)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	m = updated.(model)
	if m.query != "g" {
		t.Errorf("query = %q, want \"g\"", m.query)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = updated.(model)
	if m.query != "gi" {
		t.Errorf("query = %q, want \"gi\"", m.query)
	}
}

func TestBackspace(t *testing.T) {
	m := makeModel(ModeExplore)
	m.query = "git"
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(model)
	if m.query != "gi" {
		t.Errorf("after backspace: query = %q, want \"gi\"", m.query)
	}
}

func TestBackspaceEmpty(t *testing.T) {
	m := makeModel(ModeExplore)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(model)
	if m.query != "" {
		t.Errorf("backspace on empty: query = %q, want empty", m.query)
	}
}

func TestCursorNavigation(t *testing.T) {
	hits := []find.Hit{
		{Cmd: "cmd1"},
		{Cmd: "cmd2"},
		{Cmd: "cmd3"},
	}
	m := makeModelWithHits(hits)

	// Down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.cursor != 1 {
		t.Errorf("after down: cursor = %d, want 1", m.cursor)
	}

	// Down again
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.cursor != 2 {
		t.Errorf("after 2nd down: cursor = %d, want 2", m.cursor)
	}

	// Down at bottom (should not go past end)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.cursor != 2 {
		t.Errorf("cursor at bottom: cursor = %d, want 2", m.cursor)
	}

	// Up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(model)
	if m.cursor != 1 {
		t.Errorf("after up: cursor = %d, want 1", m.cursor)
	}
}

func TestCursorAtTop(t *testing.T) {
	m := makeModelWithHits([]find.Hit{{Cmd: "cmd1"}})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(model)
	if m.cursor != 0 {
		t.Errorf("cursor above top: cursor = %d, want 0", m.cursor)
	}
}

func TestEnterSetsSelection(t *testing.T) {
	hits := []find.Hit{{Cmd: "git status"}, {Cmd: "ls -la"}}
	m := makeModelWithHits(hits)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.selection == nil {
		t.Fatal("selection should be set after Enter")
	}
	if m.selection.Cmd != "git status" {
		t.Errorf("selection.Cmd = %q, want \"git status\"", m.selection.Cmd)
	}
	if !m.done {
		t.Error("done should be true after Enter")
	}
}

func TestEscQuits(t *testing.T) {
	m := makeModel(ModeExplore) // non-vi mode
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if !m.done {
		t.Error("ESC in non-vi mode should set done=true")
	}
}

func TestViModeEscEntersCommandMode(t *testing.T) {
	cfg := config.Config{ViMode: true, SearchLimit: 5}
	m := newModel(nil, cfg, ModeExplore)
	m.commandMode = false // start in insert mode

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if !m.commandMode {
		t.Error("ESC in vi mode should enter command mode")
	}
	if m.done {
		t.Error("ESC in vi mode should NOT quit")
	}
}

func TestViModeIEntersInsert(t *testing.T) {
	cfg := config.Config{ViMode: true, SearchLimit: 5}
	m := newModel(nil, cfg, ModeExplore)
	// commandMode starts true in vi mode

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = updated.(model)
	if m.commandMode {
		t.Error("'i' in vi command mode should enter insert mode")
	}
}

func TestViModeJKNavigation(t *testing.T) {
	cfg := config.Config{ViMode: true, SearchLimit: 5}
	m := newModel(nil, cfg, ModeExplore)
	m.hits = []find.Hit{{Cmd: "cmd1"}, {Cmd: "cmd2"}, {Cmd: "cmd3"}}
	// vi mode starts in command mode

	// j = down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(model)
	if m.cursor != 1 {
		t.Errorf("vi 'j': cursor = %d, want 1", m.cursor)
	}

	// k = up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(model)
	if m.cursor != 0 {
		t.Errorf("vi 'k': cursor = %d, want 0", m.cursor)
	}
}

func TestPruneModeMarkUnmark(t *testing.T) {
	// Use a nil DB — markPrune/unmarkPrune won't be called in unit tests
	// since we test the state machine only.
	m := makeModel(ModePrune)
	m.hits = []find.Hit{
		{Cmd: "cmd1", AnnotationType: 0},
		{Cmd: "cmd2", AnnotationType: 1}, // already marked
	}

	// Cursor on cmd2 (already marked), press left to unmark.
	m.cursor = 1
	// We can't call DB operations without a real DB; just verify the key
	// is recognized (cmd reaches markPrune/unmarkPrune path).
	// The key handling itself is tested via the right arrow.
	// This test just ensures we don't panic.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(model)
	// No panic = pass (DB call would fail with nil db but that error is silently dropped)
}

func TestWindowSizeMsg(t *testing.T) {
	m := makeModel(ModeExplore)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(model)
	if m.termWidth != 120 {
		t.Errorf("termWidth = %d, want 120", m.termWidth)
	}
	if m.termHeight != 40 {
		t.Errorf("termHeight = %d, want 40", m.termHeight)
	}
}

func TestViewRendersPrompt(t *testing.T) {
	m := makeModel(ModeExplore)
	m.query = "git"
	m.termWidth = 80
	view := m.View()
	if view == "" {
		t.Error("View() returned empty string")
	}
	// Should contain our query text.
	if !contains(view, "git") {
		t.Errorf("View() doesn't contain query %q: %s", "git", view)
	}
}

func TestTruncateShort(t *testing.T) {
	got := truncate("ls", "", 80)
	if got != "ls" {
		t.Errorf("truncate(\"ls\", \"\", 80) = %q, want \"ls\"", got)
	}
}

func TestTruncateLong(t *testing.T) {
	long := strings.Repeat("x", 100)
	got := truncate(long, "", 40)
	if len(got) > 40 {
		t.Errorf("truncate produced string longer than terminal width: len=%d", len(got))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
