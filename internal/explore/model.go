// Package explore implements the interactive TUI for ghistx.
//
// It uses the bubbletea Elm-architecture framework and supports two modes:
//   - ModeExplore: search + select a command to inject/print
//   - ModePrune:   search + mark commands for deletion
package explore

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mwirges/ghistx/internal/config"
	"github.com/mwirges/ghistx/internal/find"
)

// Mode controls whether the TUI is in explore or prune mode.
type Mode int

const (
	ModeExplore Mode = iota
	ModePrune
)

// searchResultMsg carries results from an async find query.
type searchResultMsg struct {
	hits     []find.Hit
	isGlobal bool
	err      error
}

// model is the bubbletea model for the explore/prune TUI.
type model struct {
	db          *sql.DB
	cfg         config.Config
	mode        Mode
	cwdFilter   string
	query       string
	hits        []find.Hit
	isGlobal    bool // true when last search fell back to global
	cursor      int
	commandMode bool // vi command mode
	termWidth   int
	termHeight  int
	done        bool
	selection   *find.Hit // set on Enter
}

func newModel(db *sql.DB, cfg config.Config, mode Mode, cwdFilter string) model {
	m := model{
		db:        db,
		cfg:       cfg,
		mode:      mode,
		cwdFilter: cwdFilter,
	}
	if cfg.ViMode {
		m.commandMode = true
	}
	return m
}

// Init emits initial commands: request window size + first search.
func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		doSearch(m.db, []string{}, m.cfg.SearchLimit, m.cwdFilter),
	)
}

func doSearch(db *sql.DB, keywords []string, limit int, cwdFilter string) tea.Cmd {
	return func() tea.Msg {
		if db == nil {
			return searchResultMsg{}
		}
		res, err := find.Cmd(db, keywords, limit, cwdFilter)
		return searchResultMsg{hits: res.Hits, isGlobal: res.IsGlobal, err: err}
	}
}

// Update handles incoming messages and returns the updated model + next command.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		return m, nil

	case searchResultMsg:
		if msg.err == nil {
			m.hits = msg.hits
			m.isGlobal = msg.isGlobal
			// Clamp cursor.
			if len(m.hits) == 0 {
				m.cursor = 0
			} else if m.cursor >= len(m.hits) {
				m.cursor = len(m.hits) - 1
			}
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// ESC
	if key == "esc" {
		if m.cfg.ViMode {
			if !m.commandMode {
				m.commandMode = true
			}
		} else {
			// Non-vi: ESC exits without selection.
			m.done = true
			return m, tea.Quit
		}
		return m, nil
	}

	// Enter — confirm selection.
	if key == "enter" {
		if len(m.hits) > 0 && m.cursor < len(m.hits) {
			h := m.hits[m.cursor]
			m.selection = &h
		}
		m.done = true
		return m, tea.Quit
	}

	// Navigation.
	if key == "down" || key == "ctrl+n" || (m.commandMode && key == "j") {
		if m.cursor < len(m.hits)-1 {
			m.cursor++
		}
		return m, nil
	}
	if key == "up" || key == "ctrl+p" || (m.commandMode && key == "k") {
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	}

	// Prune toggle (prune mode only).
	if m.mode == ModePrune {
		if key == "right" || key == "ctrl+p" || (m.commandMode && key == "x") {
			if len(m.hits) > 0 && m.cursor < len(m.hits) {
				h := m.hits[m.cursor]
				if h.AnnotationType != 1 {
					_ = markPrune(m.db, h.Hash)
				}
				return m, doSearch(m.db, splitQuery(m.query), m.cfg.SearchLimit, m.cwdFilter)
			}
		}
		if key == "left" {
			if len(m.hits) > 0 && m.cursor < len(m.hits) {
				h := m.hits[m.cursor]
				if h.AnnotationType == 1 {
					_ = unmarkPrune(m.db, h.Hash)
				}
				return m, doSearch(m.db, splitQuery(m.query), m.cfg.SearchLimit, m.cwdFilter)
			}
		}
	}

	// Vi insert mode.
	if m.commandMode && key == "i" {
		m.commandMode = false
		return m, nil
	}

	// Ctrl+C / q (in command mode) to quit.
	if key == "ctrl+c" || (m.commandMode && key == "q") {
		m.done = true
		return m, tea.Quit
	}

	// Text input (only in insert mode or non-vi mode).
	if !m.commandMode {
		switch key {
		case "backspace", "ctrl+h":
			if len(m.query) > 0 {
				// Remove last UTF-8 rune.
				runes := []rune(m.query)
				m.query = string(runes[:len(runes)-1])
			}
		default:
			// Only printable single-char keys.
			if len(msg.Runes) == 1 {
				m.query += string(msg.Runes)
			}
		}
		m.cursor = 0
		return m, doSearch(m.db, splitQuery(m.query), m.cfg.SearchLimit, m.cwdFilter)
	}

	return m, nil
}

// View renders the TUI.
func (m model) View() string {
	var b strings.Builder

	// Prompt line.
	prefix := ""
	if m.commandMode {
		prefix = "[CMD]"
	}
	tag := ""
	if m.isGlobal {
		tag = " [global]"
	}
	// Use block cursor character to indicate input position (matching C █ U+2588).
	b.WriteString(stylePrompt.Render(prefix+"Search"+tag+": ") + m.query + "█\n")

	// Result lines (up to SearchLimit).
	limit := m.cfg.SearchLimit
	if limit <= 0 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		if i >= len(m.hits) {
			b.WriteString("\n")
			continue
		}
		h := m.hits[i]
		text := truncate(h.Cmd, h.CWD, m.termWidth)
		line := renderLine(text, h.AnnotationType == 1, i == m.cursor)
		b.WriteString(line + "\n")
	}

	return b.String()
}

// truncate builds the display string respecting the terminal width.
// Format: "cmd (cwd)" — cwd is dropped if it doesn't fit.
func truncate(cmd, cwd string, termWidth int) string {
	budget := termWidth - 2
	if budget < 0 {
		budget = 0
	}

	extra := 0
	if cwd != "" {
		extra = 3 + len(cwd) // " (" + cwd + ")"
	}
	allowed := budget - extra
	if allowed < 0 {
		// Drop cwd.
		cwd = ""
		extra = 0
		allowed = budget
	}

	if allowed > 0 && len(cmd) > allowed {
		cmd = cmd[:allowed]
	}

	if cwd != "" {
		return cmd + styleDim.Render(" ("+cwd+")")
	}
	return cmd
}

func splitQuery(q string) []string {
	return strings.Fields(q)
}

// markPrune inserts a prune annotation for hash.
func markPrune(db *sql.DB, hash string) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(
		`INSERT INTO cmdan(hash, type, desc)
		 VALUES(?, 1, 'prune')
		 ON CONFLICT(hash) DO UPDATE SET type=excluded.type, desc=excluded.desc`,
		hash,
	)
	return err
}

// unmarkPrune removes the prune annotation for hash.
func unmarkPrune(db *sql.DB, hash string) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(`DELETE FROM cmdan WHERE hash = ? AND type = 1`, hash)
	return err
}

// Run launches the bubbletea program and handles post-exit actions.
func Run(db *sql.DB, cfg config.Config, mode Mode, tmpFile string, cwdFilter string) error {
	m := newModel(db, cfg, mode, cwdFilter)

	opts := []tea.ProgramOption{tea.WithInputTTY()}
	p := tea.NewProgram(m, opts...)

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("explore: program: %w", err)
	}

	fm, ok := finalModel.(model)
	if !ok {
		return nil
	}

	// Handle selection (explore mode only).
	if mode == ModeExplore && fm.selection != nil {
		selected := fm.selection.Cmd
		if tmpFile != "" {
			f, err := os.Create(tmpFile)
			if err != nil {
				return fmt.Errorf("explore: create tmpfile: %w", err)
			}
			defer f.Close()
			fmt.Fprintln(f, selected)
		} else if cfg.ExploreBasic {
			fmt.Println(selected)
		} else {
			if err := InjectTIOCSTI(selected); err != nil {
				// Graceful fallback.
				fmt.Println(selected)
			}
		}
	}

	// Handle prune confirmation.
	if mode == ModePrune {
		if err := confirmPrune(db); err != nil {
			return err
		}
	}

	return nil
}

// confirmPrune shows prune candidates and asks for user confirmation.
func confirmPrune(db *sql.DB) error {
	rows, err := db.Query(`
		SELECT r.hash, r.cmd, r.ts
		FROM cmdraw AS r
		INNER JOIN cmdan AS a ON r.hash = a.hash
		WHERE a.type = 1
		GROUP BY r.hash
	`)
	if err != nil {
		return fmt.Errorf("prune: query candidates: %w", err)
	}

	type candidate struct{ hash string }
	var candidates []candidate
	var count int

	fmt.Printf("\033[45m### TO BE PRUNED ######################\033[0m\n")
	for rows.Next() {
		var hash, b64cmd string
		var ts int64
		if err := rows.Scan(&hash, &b64cmd, &ts); err != nil {
			rows.Close()
			return fmt.Errorf("prune: scan: %w", err)
		}
		candidates = append(candidates, candidate{hash: hash})
		count++
		fmt.Printf("  %s\n", hash)
	}
	rows.Close()

	if count == 0 {
		fmt.Printf("\033[45mNothing to prune\033[0m\n")
		return nil
	}
	fmt.Printf("\033[45m#######################################\033[0m\n")

	// Prompt for confirmation.
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("Confirm prune? [y/n] ")
		if !scanner.Scan() {
			break
		}
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if answer == "y" {
			_, err := db.Exec(`
				BEGIN;
				DELETE FROM cmdraw WHERE hash IN (SELECT hash FROM cmdan WHERE type = 1);
				DELETE FROM cmdlut WHERE hash IN (SELECT hash FROM cmdan WHERE type = 1);
				DELETE FROM cmdan WHERE type = 1;
				COMMIT;
			`)
			if err != nil {
				return fmt.Errorf("prune: delete: %w", err)
			}
			fmt.Printf("%d items pruned\n", count)
			return nil
		} else if answer == "n" {
			fmt.Println("Aborting prune")
			return nil
		}
	}
	return scanner.Err()
}
