package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/mwirges/ghistx/internal/cat"
	"github.com/mwirges/ghistx/internal/config"
	"github.com/mwirges/ghistx/internal/explore"
	"github.com/mwirges/ghistx/internal/find"
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

// indexCmd indexes one or more commands.
// Usage:
//
//	ghistx index "git status"
//	echo "git status" | ghistx index -
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
		},
		Action: func(c *cli.Context) error {
			d := getDB(c)
			cwd, _ := os.Getwd()

			if c.Bool("stdin") || (c.NArg() == 1 && c.Args().First() == "-") {
				scanner := bufio.NewScanner(os.Stdin)
				for scanner.Scan() {
					line := scanner.Text()
					if line == "" {
						continue
					}
					if err := index.Cmd(d, line, cwd); err != nil {
						return err
					}
				}
				return scanner.Err()
			}

			if c.NArg() == 0 {
				return cli.ShowCommandHelp(c, "index")
			}

			cmd := strings.Join(c.Args().Slice(), " ")
			return index.Cmd(d, cmd, cwd)
		},
	}
}

// findCmd searches the history database.
func findCmd() *cli.Command {
	return &cli.Command{
		Name:      "find",
		Usage:     "Search history for matching commands",
		ArgsUsage: "<keyword> [keyword...]",
		Flags:     []cli.Flag{globalFlag},
		Action: func(c *cli.Context) error {
			d := getDB(c)
			cfg := getCfg(c)

			keywords := c.Args().Slice()
			if len(keywords) == 0 {
				return cli.ShowCommandHelp(c, "find")
			}

			res, err := find.Cmd(d, keywords, cfg.SearchLimit, resolveCWDFilter(c, cfg))
			if err != nil {
				return err
			}
			if res.IsGlobal && len(res.Hits) > 0 {
				fmt.Println("── no local results, showing global ──")
			}
			for _, h := range res.Hits {
				when := util.FormatRelative(h.TS)
				if h.CWD != "" {
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
		Flags: []cli.Flag{globalFlag},
		Action: func(c *cli.Context) error {
			d := getDB(c)
			cfg := getCfg(c)
			hits, err := cat.Cmd(d, resolveCWDFilter(c, cfg))
			if err != nil {
				return err
			}
			for _, h := range hits {
				when := util.FormatRelative(h.TS)
				if h.CWD != "" {
					fmt.Printf("[%s] %s (%s)\n", when, h.Cmd, h.CWD)
				} else {
					fmt.Printf("[%s] %s\n", when, h.Cmd)
				}
			}
			return nil
		},
	}
}

// exploreCmd opens the interactive TUI.
func exploreCmd() *cli.Command {
	return &cli.Command{
		Name:      "explore",
		Usage:     "Interactively search and select a history entry",
		ArgsUsage: "[tmpfile]",
		Flags:     []cli.Flag{globalFlag},
		Action: func(c *cli.Context) error {
			d := getDB(c)
			cfg := getCfg(c)

			var tmpFile string
			if c.NArg() > 0 {
				tmpFile = c.Args().First()
			}

			return explore.Run(d, cfg, explore.ModeExplore, tmpFile, resolveCWDFilter(c, cfg))
		},
	}
}

// pruneCmd opens the TUI in prune mode.
func pruneCmd() *cli.Command {
	return &cli.Command{
		Name:  "prune",
		Usage: "Interactively mark and delete history entries",
		Flags: []cli.Flag{globalFlag},
		Action: func(c *cli.Context) error {
			d := getDB(c)
			cfg := getCfg(c)
			return explore.Run(d, cfg, explore.ModePrune, "", resolveCWDFilter(c, cfg))
		},
	}
}

// splitKeywords splits a raw CLI arg string on whitespace.
// Used when the shell passes the entire query as one argument.
func splitKeywords(s string) []string {
	return strings.Fields(s)
}
