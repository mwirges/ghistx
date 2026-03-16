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
