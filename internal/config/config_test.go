package config

import (
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Default()
	if cfg.SearchLimit != 5 {
		t.Errorf("default SearchLimit = %d, want 5", cfg.SearchLimit)
	}
	if cfg.ExploreBasic {
		t.Error("default ExploreBasic should be false")
	}
	if cfg.ViMode {
		t.Error("default ViMode should be false")
	}
	if !cfg.LocalOnly {
		t.Error("default LocalOnly should be true")
	}
}

func TestMissingFile(t *testing.T) {
	// Load falls back to defaults when file is missing; tested indirectly via Parse.
	cfg, err := Parse(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SearchLimit != 5 {
		t.Errorf("SearchLimit = %d, want 5", cfg.SearchLimit)
	}
}

func TestParseAllKeys(t *testing.T) {
	input := `
# this is a comment

explore-basic = true
vi-mode = True
search-limit = 10
`
	cfg, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.ExploreBasic {
		t.Error("ExploreBasic should be true")
	}
	if !cfg.ViMode {
		t.Error("ViMode should be true")
	}
	if cfg.SearchLimit != 10 {
		t.Errorf("SearchLimit = %d, want 10", cfg.SearchLimit)
	}
}

func TestSearchLimitClamping(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"search-limit = 1", 5},
		{"search-limit = 5", 5},
		{"search-limit = 20", 20},
		{"search-limit = 25", 20},
		{"search-limit = 12", 12},
	}
	for _, tc := range tests {
		cfg, err := Parse(strings.NewReader(tc.input))
		if err != nil {
			t.Fatalf("input %q: unexpected error: %v", tc.input, err)
		}
		if cfg.SearchLimit != tc.want {
			t.Errorf("input %q: SearchLimit = %d, want %d", tc.input, cfg.SearchLimit, tc.want)
		}
	}
}

func TestBadSearchLimit(t *testing.T) {
	_, err := Parse(strings.NewReader("search-limit = abc"))
	if err == nil {
		t.Error("expected error for non-integer search-limit")
	}
}

func TestUnknownKeysIgnored(t *testing.T) {
	input := "unknown-key = whatever\nsearch-limit = 7\n"
	cfg, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SearchLimit != 7 {
		t.Errorf("SearchLimit = %d, want 7", cfg.SearchLimit)
	}
}

func TestFalseValues(t *testing.T) {
	input := "explore-basic = false\nvi-mode = FALSE\n"
	cfg, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ExploreBasic {
		t.Error("ExploreBasic should be false")
	}
	if cfg.ViMode {
		t.Error("ViMode should be false")
	}
}

func TestSquelchRepeatedKeys(t *testing.T) {
	input := "squelch = make clean\nsquelch = go mod tidy\nsquelch = cd ..\n"
	cfg, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"make clean", "go mod tidy", "cd .."}
	if len(cfg.SquelchList) != len(want) {
		t.Fatalf("SquelchList = %v, want %v", cfg.SquelchList, want)
	}
	for i, v := range want {
		if cfg.SquelchList[i] != v {
			t.Errorf("SquelchList[%d] = %q, want %q", i, cfg.SquelchList[i], v)
		}
	}
}

func TestSquelchPreservesCase(t *testing.T) {
	// Pattern values are case-sensitive; must not be lowercased.
	input := "squelch = Make Clean\nsquelch = GO MOD TIDY\n"
	cfg, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SquelchList[0] != "Make Clean" {
		t.Errorf("SquelchList[0] = %q, want \"Make Clean\"", cfg.SquelchList[0])
	}
	if cfg.SquelchList[1] != "GO MOD TIDY" {
		t.Errorf("SquelchList[1] = %q, want \"GO MOD TIDY\"", cfg.SquelchList[1])
	}
}

func TestSquelchEmptyValue(t *testing.T) {
	// A blank squelch value should be ignored.
	input := "squelch = \nsquelch = make clean\n"
	cfg, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.SquelchList) != 1 || cfg.SquelchList[0] != "make clean" {
		t.Errorf("SquelchList = %v, want [make clean]", cfg.SquelchList)
	}
}

func TestSquelchClearDefaults(t *testing.T) {
	// Default is false.
	cfg, err := Parse(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SquelchClearDefaults {
		t.Error("default SquelchClearDefaults should be false")
	}

	cfg, err = Parse(strings.NewReader("squelch-clear-defaults = true\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.SquelchClearDefaults {
		t.Error("SquelchClearDefaults should be true")
	}

	cfg, err = Parse(strings.NewReader("squelch-clear-defaults = false\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SquelchClearDefaults {
		t.Error("SquelchClearDefaults should be false")
	}
}

func TestSquelchNoEntries(t *testing.T) {
	cfg, err := Parse(strings.NewReader("search-limit = 7\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.SquelchList) != 0 {
		t.Errorf("expected empty SquelchList, got %v", cfg.SquelchList)
	}
}

func TestLocalOnly(t *testing.T) {
	// Default is true.
	cfg, err := Parse(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.LocalOnly {
		t.Error("default LocalOnly should be true")
	}

	// Explicitly set to false.
	cfg, err = Parse(strings.NewReader("local-only = false\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LocalOnly {
		t.Error("LocalOnly should be false after 'local-only = false'")
	}

	// Explicitly set to true.
	cfg, err = Parse(strings.NewReader("local-only = true\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.LocalOnly {
		t.Error("LocalOnly should be true after 'local-only = true'")
	}
}
