package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPatchSettingsJSONNewFile(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	scriptPath := "/usr/local/bin/ghistx-index.sh"

	added, err := patchSettingsJSON(settingsPath, scriptPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !added {
		t.Error("expected added=true for new file")
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	assertHookPresent(t, root, scriptPath)
}

func TestPatchSettingsJSONExistingEmptyObject(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	scriptPath := "/usr/local/bin/ghistx-index.sh"

	added, err := patchSettingsJSON(settingsPath, scriptPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !added {
		t.Error("expected added=true")
	}

	data, _ := os.ReadFile(settingsPath)
	var root map[string]interface{}
	json.Unmarshal(data, &root)
	assertHookPresent(t, root, scriptPath)
}

func TestPatchSettingsJSONPreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	existing := `{"theme": "dark", "model": "claude-opus-4-6", "someOther": true}`
	os.WriteFile(settingsPath, []byte(existing), 0644)

	added, err := patchSettingsJSON(settingsPath, "/path/to/script.sh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !added {
		t.Error("expected added=true")
	}

	data, _ := os.ReadFile(settingsPath)
	var root map[string]interface{}
	json.Unmarshal(data, &root)

	if root["theme"] != "dark" {
		t.Errorf("theme field lost: %v", root["theme"])
	}
	if root["model"] != "claude-opus-4-6" {
		t.Errorf("model field lost: %v", root["model"])
	}
	if root["someOther"] != true {
		t.Errorf("someOther field lost: %v", root["someOther"])
	}
}

func TestPatchSettingsJSONIdempotent(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	scriptPath := "/usr/local/bin/ghistx-index.sh"

	// First install.
	added, err := patchSettingsJSON(settingsPath, scriptPath)
	if err != nil || !added {
		t.Fatalf("first install: added=%v err=%v", added, err)
	}

	// Second install — should report not added.
	added, err = patchSettingsJSON(settingsPath, scriptPath)
	if err != nil {
		t.Fatalf("second install error: %v", err)
	}
	if added {
		t.Error("second install should return added=false (already present)")
	}

	// Verify there's still exactly one Bash matcher entry.
	data, _ := os.ReadFile(settingsPath)
	var root map[string]interface{}
	json.Unmarshal(data, &root)
	hooks := root["hooks"].(map[string]interface{})
	postToolUse := hooks["PostToolUse"].([]interface{})
	count := 0
	for _, item := range postToolUse {
		m, _ := item.(map[string]interface{})
		if m["matcher"] == "Bash" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 Bash matcher, got %d", count)
	}
}

func TestPatchSettingsJSONPreservesExistingHooks(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// Existing settings with a different PostToolUse hook.
	existing := `{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write",
        "hooks": [{"type": "command", "command": "/other/hook.sh"}]
      }
    ]
  }
}`
	os.WriteFile(settingsPath, []byte(existing), 0644)

	scriptPath := "/usr/local/bin/ghistx-index.sh"
	added, err := patchSettingsJSON(settingsPath, scriptPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !added {
		t.Error("expected added=true")
	}

	data, _ := os.ReadFile(settingsPath)
	var root map[string]interface{}
	json.Unmarshal(data, &root)

	// Both the original Write matcher and the new Bash matcher should be present.
	hooks := root["hooks"].(map[string]interface{})
	postToolUse := hooks["PostToolUse"].([]interface{})
	if len(postToolUse) != 2 {
		t.Errorf("expected 2 matchers, got %d", len(postToolUse))
	}
	assertHookPresent(t, root, scriptPath)
}

func TestPatchSettingsJSONInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	os.WriteFile(settingsPath, []byte("{not valid json"), 0644)

	_, err := patchSettingsJSON(settingsPath, "/path/to/script.sh")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestPatchSettingsJSONWritesTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	patchSettingsJSON(settingsPath, "/path/to/script.sh")

	data, _ := os.ReadFile(settingsPath)
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("settings.json should end with a newline")
	}
}

// assertHookPresent verifies that scriptPath appears in hooks.PostToolUse[].hooks[].command.
func assertHookPresent(t *testing.T, root map[string]interface{}, scriptPath string) {
	t.Helper()
	hooks, _ := root["hooks"].(map[string]interface{})
	if hooks == nil {
		t.Fatal("no hooks key in settings")
	}
	postToolUse, _ := hooks["PostToolUse"].([]interface{})
	if len(postToolUse) == 0 {
		t.Fatal("PostToolUse is empty")
	}
	for _, item := range postToolUse {
		m, _ := item.(map[string]interface{})
		if m == nil || m["matcher"] != "Bash" {
			continue
		}
		innerHooks, _ := m["hooks"].([]interface{})
		for _, h := range innerHooks {
			hm, _ := h.(map[string]interface{})
			if hm != nil && hm["command"] == scriptPath {
				return // found
			}
		}
	}
	t.Errorf("hook command %q not found in settings.json", scriptPath)
}
