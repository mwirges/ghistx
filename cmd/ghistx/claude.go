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
		Flags: []cli.Flag{
			withSquelchFlag,
			reverseFlag,
			&cli.StringFlag{
				Name:    "tool",
				Aliases: []string{"t"},
				Usage:   "filter by tool name (e.g. Bash, Read, Edit)",
			},
			&cli.StringFlag{
				Name:    "category",
				Aliases: []string{"c"},
				Usage:   "filter by tool category (e.g. shell, file, search, web, agent, cron)",
			},
		},
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
	hits, err := cat.Cmd(d, "", "claude", 0, c.String("tool"), c.String("category"))
	if err != nil {
		return err
	}
	hits = squelch.Filter(hits, resolveSquelchPatterns(c))
	if !c.Bool("reverse") {
		// Default: newest-first.
		for i, j := 0, len(hits)-1; i < j; i, j = i+1, j-1 {
			hits[i], hits[j] = hits[j], hits[i]
		}
	}
	content := display.Render(hits, os.Stdout)
	return runWithPager(content)
}

// hookScript is the content written to ~/.claude/hooks/ghistx-index.sh.
// %s is replaced with the absolute path to the ghistx binary.
// The script handles all PostToolUse events (no matcher), dispatching by tool_name.
const hookScript = `#!/bin/sh
# Index Claude Code tool calls into ghistx.
# Reads JSON from stdin: {"tool_name": "...", "tool_input": {...}, "cwd": "..."}
json=$(cat)
result=$(printf '%%s' "$json" | python3 - <<'PYEOF'
import sys, json

d = json.load(sys.stdin)
tool = d.get('tool_name', '')
inp = d.get('tool_input', {})
cwd = d.get('cwd', '')

SKIP = {
    'TodoWrite', 'TodoRead',
    'TaskCreate', 'TaskUpdate', 'TaskList', 'TaskGet', 'TaskOutput', 'TaskStop',
    'EnterPlanMode', 'ExitPlanMode',
    'EnterWorktree', 'ExitWorktree',
    'ToolSearch', 'RemoteTrigger',
}
if tool in SKIP:
    sys.exit(0)

ARG_KEY = {
    'Bash': 'command',
    'Read': 'file_path',
    'Write': 'file_path',
    'Edit': 'file_path',
    'MultiEdit': 'file_path',
    'NotebookEdit': 'notebook_path',
    'Glob': 'pattern',
    'Grep': 'pattern',
    'WebFetch': 'url',
    'WebSearch': 'query',
    'Agent': 'description',
    'Skill': 'skill',
    'CronCreate': 'schedule',
    'CronDelete': 'trigger_id',
}

CATEGORY = {
    'Bash': 'shell',
    'Read': 'file', 'Write': 'file', 'Edit': 'file',
    'MultiEdit': 'file', 'NotebookEdit': 'file',
    'Glob': 'search', 'Grep': 'search',
    'WebFetch': 'web', 'WebSearch': 'web',
    'Agent': 'agent', 'Skill': 'agent',
    'CronCreate': 'cron', 'CronDelete': 'cron', 'CronList': 'cron',
}

arg_key = ARG_KEY.get(tool)
arg = inp.get(arg_key, '').strip() if arg_key else ''
display = '[' + tool + '] ' + arg if arg else '[' + tool + ']'
cat = CATEGORY.get(tool, 'other')

# Tab-delimited: display\tcategory\ttool\tcwd
print('\t'.join([display, cat, tool, cwd]))
PYEOF
2>/dev/null)

[ -z "$result" ] && exit 0

display=$(printf '%%s' "$result" | cut -f1)
category=$(printf '%%s' "$result" | cut -f2)
tool=$(printf '%%s' "$result" | cut -f3)
cwd=$(printf '%%s' "$result" | cut -f4)

exec %s index --source claude --tool "$tool" --category "$category" --cwd "$cwd" -- "$display"
`

// runClaudeInstall creates the hook script and patches ~/.claude/settings.json.
func runClaudeInstall() error {
	ghistxPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine ghistx path: %w", err)
	}
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

	// 1. Write hook script.
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

// patchSettingsJSON idempotently registers the ghistx PostToolUse hook (all tools,
// no matcher) in ~/.claude/settings.json. Any stale entry referencing our script
// under a different matcher (e.g. old "Bash"-only installs) is replaced.
// Returns true if the file was written (new install or upgrade); false if already correct.
func patchSettingsJSON(settingsPath, scriptPath string) (bool, error) {
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

	hooks, _ := root["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
		root["hooks"] = hooks
	}

	postToolUse, _ := hooks["PostToolUse"].([]interface{})

	// If our script already exists under a no-matcher entry, nothing to do.
	for _, item := range postToolUse {
		m, _ := item.(map[string]interface{})
		if m == nil || m["matcher"] != nil {
			continue // skip entries with any matcher
		}
		innerHooks, _ := m["hooks"].([]interface{})
		for _, h := range innerHooks {
			hm, _ := h.(map[string]interface{})
			if hm != nil && hm["command"] == scriptPath {
				return false, nil // already correct
			}
		}
	}

	// Remove any stale entry that references our script (different matcher).
	filtered := make([]interface{}, 0, len(postToolUse))
	for _, item := range postToolUse {
		m, _ := item.(map[string]interface{})
		if m == nil {
			filtered = append(filtered, item)
			continue
		}
		innerHooks, _ := m["hooks"].([]interface{})
		hasOurs := false
		for _, h := range innerHooks {
			hm, _ := h.(map[string]interface{})
			if hm != nil && hm["command"] == scriptPath {
				hasOurs = true
				break
			}
		}
		if !hasOurs {
			filtered = append(filtered, item)
		}
	}

	// Add the new no-matcher all-tools entry.
	newEntry := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": scriptPath,
			},
		},
	}
	hooks["PostToolUse"] = append(filtered, newEntry)

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
