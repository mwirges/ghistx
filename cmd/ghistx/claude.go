package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v2"

	"github.com/mwirges/ghistx/internal/cat"
	"github.com/mwirges/ghistx/internal/display"
	"github.com/mwirges/ghistx/internal/squelch"
)

// claudeCmd shows Claude Code command history and provides the install subcommand.
func claudeCmd() *cli.Command {
	return &cli.Command{
		Name:  "claude",
		Usage: "View Claude Code command history or manage the Claude Code integration",
		// Default action: show claude history globally, newest-first.
		Flags: []cli.Flag{withSquelchFlag},
		Action: func(c *cli.Context) error {
			return runClaudeHistory(c)
		},
		Subcommands: []*cli.Command{
			{
				Name:  "install",
				Usage: "Install the ghistx hook into Claude Code",
				Action: func(c *cli.Context) error {
					return runClaudeInstall()
				},
			},
		},
	}
}

// runClaudeHistory shows claude-sourced history globally, newest-first, through $PAGER.
func runClaudeHistory(c *cli.Context) error {
	d := getDB(c)
	// Always global, always claude source — that's the point of this command.
	hits, err := cat.Cmd(d, "", "claude")
	if err != nil {
		return err
	}
	hits = squelch.Filter(hits, resolveSquelchPatterns(c))
	// Reverse to newest-first.
	for i, j := 0, len(hits)-1; i < j; i, j = i+1, j-1 {
		hits[i], hits[j] = hits[j], hits[i]
	}
	content := display.Render(hits, os.Stdout)
	return runWithPager(content)
}

// hookScript is the content written to ~/.claude/hooks/ghistx-index.sh.
// %s is replaced with the absolute path to the ghistx binary.
const hookScript = `#!/bin/sh
# Index Claude Code Bash tool calls into ghistx.
# Reads JSON from stdin: {"tool_input": {"command": "..."}, "cwd": "..."}
json=$(cat)
cmd=$(printf '%%s' "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['tool_input']['command'])" 2>/dev/null)
cwd=$(printf '%%s' "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('cwd',''))" 2>/dev/null)
[ -z "$cmd" ] && exit 0
exec %s index --source claude --cwd "$cwd" -- "$cmd"
`

// runClaudeInstall creates the hook script and patches ~/.claude/settings.json.
func runClaudeInstall() error {
	// Resolve ghistx binary path for use inside the hook script.
	ghistxPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine ghistx path: %w", err)
	}
	// Follow any symlinks so the script points to the real binary.
	if resolved, err := filepath.EvalSymlinks(ghistxPath); err == nil {
		ghistxPath = resolved
	}

	home := os.Getenv("HOME")
	if home == "" {
		return fmt.Errorf("$HOME is not set")
	}
	hooksDir := filepath.Join(home, ".claude", "hooks")
	scriptPath := filepath.Join(hooksDir, "ghistx-index.sh")
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	// 1. Create hook script.
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("create hooks directory: %w", err)
	}
	content := fmt.Sprintf(hookScript, ghistxPath)
	if err := os.WriteFile(scriptPath, []byte(content), 0755); err != nil {
		return fmt.Errorf("write hook script: %w", err)
	}
	fmt.Printf("wrote %s\n", scriptPath)

	// 2. Patch settings.json.
	added, err := patchSettingsJSON(settingsPath, scriptPath)
	if err != nil {
		return fmt.Errorf("update settings.json: %w", err)
	}
	if added {
		fmt.Printf("registered hook in %s\n", settingsPath)
	} else {
		fmt.Printf("hook already registered in %s\n", settingsPath)
	}
	fmt.Println("done — restart Claude Code to activate")
	return nil
}

// patchSettingsJSON idempotently adds the ghistx PostToolUse Bash hook to
// ~/.claude/settings.json. Returns true if the entry was newly added.
func patchSettingsJSON(settingsPath, scriptPath string) (bool, error) {
	// Load existing settings (or start fresh if the file doesn't exist).
	var root map[string]interface{}
	data, err := os.ReadFile(settingsPath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read settings.json: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &root); err != nil {
			return false, fmt.Errorf("parse settings.json: %w", err)
		}
	}
	if root == nil {
		root = make(map[string]interface{})
	}

	// Navigate to hooks.PostToolUse, creating missing nodes as needed.
	hooks, _ := root["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
		root["hooks"] = hooks
	}

	postToolUse, _ := hooks["PostToolUse"].([]interface{})

	// Check whether our command is already present under a Bash matcher.
	for _, item := range postToolUse {
		m, _ := item.(map[string]interface{})
		if m == nil || m["matcher"] != "Bash" {
			continue
		}
		innerHooks, _ := m["hooks"].([]interface{})
		for _, h := range innerHooks {
			hm, _ := h.(map[string]interface{})
			if hm != nil && hm["command"] == scriptPath {
				return false, nil // already installed
			}
		}
	}

	// Append a new Bash matcher entry with our hook command.
	newMatcher := map[string]interface{}{
		"matcher": "Bash",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": scriptPath,
			},
		},
	}
	hooks["PostToolUse"] = append(postToolUse, newMatcher)

	// Write back with two-space indentation.
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false, fmt.Errorf("marshal settings.json: %w", err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		return false, fmt.Errorf("write settings.json: %w", err)
	}
	return true, nil
}
