// Package config parses the ~/.histx configuration file.
//
// The file format is simple key=value lines, e.g.:
//
//	explore-basic = true
//	search-limit  = 10
//	vi-mode       = true
//
// Lines starting with '#' or that are blank are ignored.
// Unknown keys are silently ignored for forward compatibility.
package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds the parsed settings from ~/.histx.
type Config struct {
	ExploreBasic bool
	SearchLimit  int // clamped to [5, 20], default 5
	ViMode       bool
}

// Default returns a Config with default values.
func Default() Config {
	return Config{
		SearchLimit: 5,
	}
}

// Load reads the config from the default path (~/.histx).
// If the file does not exist, defaults are returned without error.
func Load() (Config, error) {
	path := filepath.Join(os.Getenv("HOME"), ".histx")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return Default(), err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads config key=value lines from r.
func Parse(r io.Reader) (Config, error) {
	cfg := Default()
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(strings.ToLower(val))

		switch key {
		case "explore-basic":
			cfg.ExploreBasic = val == "true"
		case "vi-mode":
			cfg.ViMode = val == "true"
		case "search-limit":
			n, err := strconv.Atoi(val)
			if err != nil {
				return cfg, fmt.Errorf("config: invalid search-limit %q: %w", val, err)
			}
			if n < 5 {
				n = 5
			} else if n > 20 {
				n = 20
			}
			cfg.SearchLimit = n
		}
	}
	return cfg, scanner.Err()
}
