// Package analyze computes statistics over the command history database.
package analyze

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mwirges/ghistx/internal/squelch"
)

// Freq is a label paired with an occurrence count.
type Freq struct {
	Label string
	Count int
}

// Stats holds all computed statistics for the analyze command.
type Stats struct {
	TotalCommands  int
	UniqueCommands int
	UniquePrograms int
	FirstSeen      time.Time
	LastSeen       time.Time
	AvgPerDay      float64

	TopCommands   []Freq // top N full commands by frequency
	TopPrograms   []Freq // top N programs (first token) by frequency
	TopCategories []Freq
	TopDirs       []Freq

	HourlyDist [24]int
	DailyDist  map[string]int // "2006-01-02" → count (all available dates)
}

// Compute fetches all matching rows from db and returns computed Stats.
// cwdFilter restricts to a specific directory (empty = global).
// sourceFilter is "user", "claude", or "all".
// patterns are squelch patterns; nil means no filtering.
func Compute(db *sql.DB, cwdFilter, sourceFilter string, patterns []squelch.Pattern) (Stats, error) {
	rows, err := fetchRows(db, cwdFilter, sourceFilter)
	if err != nil {
		return Stats{}, err
	}
	rows = filterSquelch(rows, patterns)

	if len(rows) == 0 {
		return Stats{DailyDist: make(map[string]int)}, nil
	}

	cmdCount := make(map[string]int)
	progCount := make(map[string]int)
	catCount := make(map[string]int)
	dirCount := make(map[string]int)
	daily := make(map[string]int)
	var hourly [24]int

	var minTS, maxTS int64 = rows[0].ts, rows[0].ts
	for _, r := range rows {
		cmdCount[r.cmd]++
		prog := programName(r.cmd)
		progCount[prog]++
		catCount[Category(r.cmd)]++
		if r.cwd != "" {
			dirCount[r.cwd]++
		}
		t := time.UnixMilli(r.ts)
		hourly[t.Hour()]++
		daily[t.Format("2006-01-02")]++
		if r.ts < minTS {
			minTS = r.ts
		}
		if r.ts > maxTS {
			maxTS = r.ts
		}
	}

	first := time.UnixMilli(minTS)
	last := time.UnixMilli(maxTS)
	days := last.Sub(first).Hours() / 24
	if days < 1 {
		days = 1
	}

	return Stats{
		TotalCommands:  len(rows),
		UniqueCommands: len(cmdCount),
		UniquePrograms: len(progCount),
		FirstSeen:      first,
		LastSeen:       last,
		AvgPerDay:      float64(len(rows)) / days,
		TopCommands:    topN(cmdCount, 15),
		TopPrograms:    topN(progCount, 15),
		TopCategories:  topN(catCount, 10),
		TopDirs:        topN(dirCount, 10),
		HourlyDist:     hourly,
		DailyDist:      daily,
	}, nil
}

type row struct {
	cmd string
	cwd string
	ts  int64
}

func fetchRows(db *sql.DB, cwdFilter, sourceFilter string) ([]row, error) {
	query := `
		SELECT r.cmd, r.ts, COALESCE(r.cwd, '') AS cwd
		FROM cmdraw AS r
		LEFT OUTER JOIN cmdmeta AS m ON r.hash = m.hash AND m.key = 'source'`

	var args []any
	var conds []string
	if cwdFilter != "" {
		b64 := base64.StdEncoding.EncodeToString([]byte(cwdFilter))
		conds = append(conds, "r.cwd = ?")
		args = append(args, b64)
	}
	switch sourceFilter {
	case "all":
		// no filter
	case "claude":
		conds = append(conds, "COALESCE(m.value, '') = 'claude'")
	default: // "user" or ""
		conds = append(conds, "COALESCE(m.value, '') = ''")
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY r.ts ASC"

	sqlRows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("analyze: query: %w", err)
	}
	defer sqlRows.Close()

	var out []row
	for sqlRows.Next() {
		var b64cmd string
		var b64cwd sql.NullString
		var ts int64
		if err := sqlRows.Scan(&b64cmd, &ts, &b64cwd); err != nil {
			return nil, fmt.Errorf("analyze: scan: %w", err)
		}
		cmd, err := base64.StdEncoding.DecodeString(b64cmd)
		if err != nil {
			continue
		}
		cwd := ""
		if b64cwd.Valid && b64cwd.String != "" {
			c, err := base64.StdEncoding.DecodeString(b64cwd.String)
			if err == nil {
				cwd = string(c)
			}
		}
		out = append(out, row{cmd: string(cmd), cwd: cwd, ts: ts})
	}
	return out, sqlRows.Err()
}

func filterSquelch(rows []row, patterns []squelch.Pattern) []row {
	if len(patterns) == 0 {
		return rows
	}
	out := make([]row, 0, len(rows))
	for _, r := range rows {
		if !squelch.Matches(r.cmd, patterns) {
			out = append(out, r)
		}
	}
	return out
}

// programName extracts the program name (first token, basename) from a command.
// Leading env var assignments (FOO=bar) are skipped.
func programName(cmd string) string {
	fields := strings.Fields(cmd)
	// Skip leading VAR=value tokens.
	for len(fields) > 0 && strings.Contains(fields[0], "=") {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return cmd
	}
	p := fields[0]
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		p = p[idx+1:]
	}
	return p
}

// categoryMap maps program names to category labels.
var categoryMap = map[string]string{
	// VCS
	"git": "vcs", "gh": "vcs", "svn": "vcs", "hg": "vcs",
	// Containers / orchestration
	"docker": "containers", "kubectl": "containers", "k9s": "containers",
	"helm": "containers", "podman": "containers", "kind": "containers",
	"minikube": "containers", "docker-compose": "containers", "compose": "containers",
	// Build / package managers
	"make": "build", "cargo": "build", "go": "build",
	"npm": "build", "yarn": "build", "pnpm": "build",
	"gradle": "build", "mvn": "build", "maven": "build",
	"pip": "build", "pip3": "build", "python": "build", "python3": "build",
	"node": "build", "mix": "build", "bundle": "build", "rake": "build",
	"cmake": "build", "ninja": "build", "meson": "build",
	// Cloud / infra
	"aws": "cloud", "gcloud": "cloud", "az": "cloud",
	"terraform": "cloud", "tf": "cloud", "pulumi": "cloud",
	"fly": "cloud", "flyctl": "cloud", "heroku": "cloud",
	"ansible": "cloud", "vault": "cloud", "consul": "cloud",
	// System admin
	"sudo": "system", "systemctl": "system", "service": "system",
	"kill": "system", "killall": "system", "pkill": "system",
	"ps": "system", "df": "system", "du": "system",
	"free": "system", "uname": "system", "uptime": "system",
	"mount": "system", "umount": "system", "chmod": "system",
	"chown": "system", "useradd": "system", "usermod": "system",
	// Network
	"ssh": "network", "scp": "network", "rsync": "network",
	"curl": "network", "wget": "network", "ping": "network",
	"nmap": "network", "netstat": "network", "nc": "network",
	"telnet": "network", "traceroute": "network", "dig": "network",
	"nslookup": "network", "ftp": "network", "sftp": "network",
	// Editor
	"vim": "editor", "nvim": "editor", "vi": "editor",
	"nano": "editor", "emacs": "editor", "code": "editor",
	"subl": "editor", "atom": "editor", "hx": "editor",
	// Shell utilities
	"ls": "shell", "cd": "shell", "pwd": "shell", "echo": "shell",
	"cat": "shell", "grep": "shell", "awk": "shell", "sed": "shell",
	"find": "shell", "xargs": "shell", "sort": "shell", "uniq": "shell",
	"wc": "shell", "head": "shell", "tail": "shell", "less": "shell",
	"more": "shell", "cp": "shell", "mv": "shell", "rm": "shell",
	"mkdir": "shell", "touch": "shell", "ln": "shell", "export": "shell",
	"source": "shell", "alias": "shell", "history": "shell",
	"clear": "shell", "exit": "shell",
}

// Category returns the category label for a command string.
func Category(cmd string) string {
	prog := programName(cmd)
	if cat, ok := categoryMap[prog]; ok {
		return cat
	}
	return "other"
}

// topN returns up to n entries from m, sorted by count descending then label ascending.
func topN(m map[string]int, n int) []Freq {
	out := make([]Freq, 0, len(m))
	for k, v := range m {
		out = append(out, Freq{Label: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
