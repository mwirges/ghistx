package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/mwirges/ghistx/internal/cat"
	"github.com/mwirges/ghistx/internal/config"
	"github.com/mwirges/ghistx/internal/display"
	"github.com/mwirges/ghistx/internal/explore"
	"github.com/mwirges/ghistx/internal/find"
	"github.com/mwirges/ghistx/internal/hashlet"
	"github.com/mwirges/ghistx/internal/index"
	interndb "github.com/mwirges/ghistx/internal/db"
	"github.com/mwirges/ghistx/internal/util"
)

type contextKey string

const (
	keyDB     contextKey = "db"
	keyConfig contextKey = "config"
)

func main() {
	app := &cli.App{
		Name:  "ghistx",
		Usage: "Shell command history indexer and interactive searcher",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "db",
				Aliases: []string{"d"},
				EnvVars: []string{"HISTX_DB_FILE"},
				Value:   filepath.Join(os.Getenv("HOME"), ".histx.db"),
				Usage:   "path to the histx SQLite database",
			},
			globalFlag, // app-level --global for default action\n\t\t\tsourceFlag, // app-level --source for default action
			&cli.StringFlag{
				Name:   "tmpfile",
				Usage:  "write resolved command to this file (used by shell integration)",
				Hidden: true,
			},
		},
		Before: func(c *cli.Context) error {
			dbPath := c.String("db")
			d, err := interndb.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening database %q: %w", dbPath, err)
			}
			cfg, err := config.Load()
			if err != nil {
				// Non-fatal: fall back to defaults.
				cfg = config.Default()
			}
			c.App.Metadata[string(keyDB)] = d
			c.App.Metadata[string(keyConfig)] = cfg
			return nil
		},
		After: func(c *cli.Context) error {
			if d, ok := c.App.Metadata[string(keyDB)].(*sql.DB); ok && d != nil {
				d.Close()
			}
			return nil
		},
		// Default action: no subcommand supplied.
		// A single hex argument >= 4 chars is treated as a hashlet re-execution.
		Action: func(c *cli.Context) error {
			if c.NArg() == 1 {
				arg := c.Args().First()
				if isHexString(arg) {
					return runHashlet(c, arg)
				}
			}
			if c.NArg() > 0 {
				return fmt.Errorf("unknown command %q; see --help", c.Args().First())
			}
			return runHistory(c)
		},
		Commands: []*cli.Command{
			indexCmd(),
			findCmd(),
			catCmd(),
			exploreCmd(),
			pruneCmd(),
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func getDB(c *cli.Context) *sql.DB {
	return c.App.Metadata[string(keyDB)].(*sql.DB)
}

func getCfg(c *cli.Context) config.Config {
	if cfg, ok := c.App.Metadata[string(keyConfig)].(config.Config); ok {
		return cfg
	}
	return config.Default()
}

// resolveCWDFilter returns the current working directory to use as a CWD
// filter, or "" if global search is requested (via --global flag or config).
func resolveCWDFilter(c *cli.Context, cfg config.Config) string {
	if c.Bool("global") || !cfg.LocalOnly {
		return ""
	}
	cwd, _ := os.Getwd()
	return cwd
}

// globalFlag is the shared --global/-g flag added to search subcommands.
var globalFlag = &cli.BoolFlag{
	Name:    "global",
	Aliases: []string{"g"},
	Usage:   "search all directories, not just the current one",
}

// sourceFlag is the shared --source flag added to search subcommands.
var sourceFlag = &cli.StringFlag{
	Name:  "source",
	Usage: "filter by command source: user (default), claude, or all",
	Value: "user",
}

// resolveSourceFilter returns the source filter string from the --source flag.
func resolveSourceFilter(c *cli.Context) string {
	return c.String("source")
}

// isHexString returns true if s is a valid hex string of at least 4 characters.
func isHexString(s string) bool {
	if len(s) < 4 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

// clearCWD zeroes out the CWD field on all hits. Used when results are
// already scoped to the current directory, making the CWD redundant.
func clearCWD(hits []find.Hit) {
	for i := range hits {
		hits[i].CWD = ""
	}
}

// isatty reports whether f is connected to a terminal.
func isatty(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// runHistory shows history newest-to-oldest, piped through $PAGER when set and
// stdout is a terminal.
func runHistory(c *cli.Context) error {
	d := getDB(c)
	cfg := getCfg(c)
	cwdFilter := resolveCWDFilter(c, cfg)
	hits, err := cat.Cmd(d, cwdFilter, resolveSourceFilter(c))
	if err != nil {
		return err
	}
	// Reverse to newest-first.
	for i, j := 0, len(hits)-1; i < j; i, j = i+1, j-1 {
		hits[i], hits[j] = hits[j], hits[i]
	}
	if cwdFilter != "" {
		clearCWD(hits)
	}
	// Render with color keyed off the real stdout (not the pipe we may write to).
	content := display.Render(hits, os.Stdout)
	return runWithPager(content)
}

// runWithPager pipes content through $PAGER when set and stdout is a terminal.
// If $PAGER is unset or stdout is not a terminal, content is written directly.
func runWithPager(content string) error {
	pager := os.Getenv("PAGER")
	if pager != "" && isatty(os.Stdout) {
		pr, pw, err := os.Pipe()
		if err == nil {
			cmd := exec.Command("sh", "-c", pager)
			cmd.Stdin = pr
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if startErr := cmd.Start(); startErr == nil {
				pr.Close()
				_, writeErr := fmt.Fprint(pw, content)
				pw.Close()
				waitErr := cmd.Wait()
				if writeErr != nil {
					return writeErr
				}
				return waitErr
			}
			pw.Close()
			pr.Close()
		}
	}
	_, err := fmt.Fprint(os.Stdout, content)
	return err
}

// runHashlet looks up a command by hash prefix and either writes it to a
// tmpfile (shell integration) or prints it to stdout.
func runHashlet(c *cli.Context, prefix string) error {
	d := getDB(c)
	h, err := hashlet.FindByPrefix(d, prefix)
	if err != nil {
		return err
	}
	if tmpFile := c.String("tmpfile"); tmpFile != "" {
		f, err := os.Create(tmpFile)
		if err != nil {
			return fmt.Errorf("hashlet: create tmpfile: %w", err)
		}
		defer f.Close()
		fmt.Fprintln(f, h.Cmd)
		return nil
	}
	fmt.Println(h.Cmd)
	return nil
}

// indexCmd indexes one or more commands.
// Usage:
//
//	ghistx index "git status"
//	echo "git status" | ghistx index -
//	ghistx index --source claude --cwd /some/path "git status"
func indexCmd() *cli.Command {
	return &cli.Command{
		Name:  "index",
		Usage: "Index a command into the history database",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "stdin",
				Aliases: []string{"s"},
				Usage:   "read commands from stdin (one per line)",
			},
			&cli.StringFlag{
				Name:  "source",
				Usage: "tag the command with a source label (e.g. 'claude')",
			},
			&cli.StringFlag{
				Name:  "cwd",
				Usage: "override the working directory recorded with the command",
			},
		},
		Action: func(c *cli.Context) error {
			d := getDB(c)
			cwd := c.String("cwd")
			if cwd == "" {
				cwd, _ = os.Getwd()
			}
			source := c.String("source")

			if c.Bool("stdin") || (c.NArg() == 1 && c.Args().First() == "-") {
				scanner := bufio.NewScanner(os.Stdin)
				for scanner.Scan() {
					line := scanner.Text()
					if line == "" {
						continue
					}
					if err := index.Cmd(d, line, cwd, source); err != nil {
						return err
					}
				}
				return scanner.Err()
			}

			if c.NArg() == 0 {
				return cli.ShowCommandHelp(c, "index")
			}

			cmd := strings.Join(c.Args().Slice(), " ")
			return index.Cmd(d, cmd, cwd, source)
		},
	}
}

// findCmd searches the history database.
func findCmd() *cli.Command {
	return &cli.Command{
		Name:      "find",
		Usage:     "Search history for matching commands",
		ArgsUsage: "<keyword> [keyword...]",
		Flags:     []cli.Flag{globalFlag, sourceFlag},
		Action: func(c *cli.Context) error {
			d := getDB(c)
			cfg := getCfg(c)

			keywords := c.Args().Slice()
			if len(keywords) == 0 {
				return cli.ShowCommandHelp(c, "find")
			}

			cwdFilter := resolveCWDFilter(c, cfg)
			res, err := find.Cmd(d, keywords, cfg.SearchLimit, cwdFilter, resolveSourceFilter(c))
			if err != nil {
				return err
			}
			if res.IsGlobal && len(res.Hits) > 0 {
				fmt.Println("── no local results, showing global ──")
			}
			showCWD := cwdFilter == "" || res.IsGlobal
			for _, h := range res.Hits {
				when := util.FormatRelative(h.TS)
				if h.CWD != "" && showCWD {
					fmt.Printf("[%s] %s (%s)\n", when, h.Cmd, h.CWD)
				} else {
					fmt.Printf("[%s] %s\n", when, h.Cmd)
				}
			}
			return nil
		},
	}
}

// catCmd dumps all history oldest-first.
func catCmd() *cli.Command {
	return &cli.Command{
		Name:  "cat",
		Usage: "Print all history entries ordered oldest-first",
		Flags: []cli.Flag{globalFlag, sourceFlag},
		Action: func(c *cli.Context) error {
			d := getDB(c)
			cfg := getCfg(c)
			cwdFilter := resolveCWDFilter(c, cfg)
			hits, err := cat.Cmd(d, cwdFilter, resolveSourceFilter(c))
			if err != nil {
				return err
			}
			if cwdFilter != "" {
				clearCWD(hits)
			}
			return display.PrintHits(os.Stdout, hits)
		},
	}
}

// exploreCmd opens the interactive TUI.
func exploreCmd() *cli.Command {
	return &cli.Command{
		Name:      "explore",
		Usage:     "Interactively search and select a history entry",
		ArgsUsage: "[tmpfile]",
		Flags:     []cli.Flag{globalFlag, sourceFlag},
		Action: func(c *cli.Context) error {
			d := getDB(c)
			cfg := getCfg(c)

			var tmpFile string
			if c.NArg() > 0 {
				tmpFile = c.Args().First()
			}

			return explore.Run(d, cfg, explore.ModeExplore, tmpFile, resolveCWDFilter(c, cfg), resolveSourceFilter(c))
		},
	}
}

// pruneCmd opens the TUI in prune mode.
func pruneCmd() *cli.Command {
	return &cli.Command{
		Name:  "prune",
		Usage: "Interactively mark and delete history entries",
		Flags: []cli.Flag{globalFlag, sourceFlag},
		Action: func(c *cli.Context) error {
			d := getDB(c)
			cfg := getCfg(c)
			return explore.Run(d, cfg, explore.ModePrune, "", resolveCWDFilter(c, cfg), resolveSourceFilter(c))
		},
	}
}

// splitKeywords splits a raw CLI arg string on whitespace.
// Used when the shell passes the entire query as one argument.
func splitKeywords(s string) []string {
	return strings.Fields(s)
}

