# ghistx — Codebase Architecture

`ghistx` is a Go port of the C [`histx`](https://github.com/mwirges/histx) shell history indexer. It stores, searches, and interactively browses shell command history in a SQLite database that is **byte-for-byte compatible** with the C version.

---

## Package Map

```
cmd/ghistx/main.go          CLI wiring (urfave/cli v2); subcommands: index, find, cat, explore, prune
internal/config/config.go   ~/.histx key=value parser; Config struct with defaults
internal/db/db.go           Open(), schema creation, migrations (pure-Go SQLite via modernc.org/sqlite)
internal/ngram/ngram.go     3-char byte-level n-gram generation — Gen(s) []uint32
internal/util/util.go       FormatRelative(tsMillis int64) string — human-readable timestamps
internal/index/index.go     Index subcommand: SHA-256 hash, base64-encode, insert cmdraw + cmdlut rows
internal/find/find.go       Find subcommand: hybrid ngram+ACS search; Result type with IsGlobal flag
internal/cat/cat.go         Cat subcommand: ordered history dump with optional CWD filter
internal/squelch/squelch.go Squelch filtering: DefaultList, Filter(hits, patterns), ActiveList(clearDefaults, user)
internal/explore/model.go   Bubbletea TUI model (explore + prune modes); cwdFilter threading
internal/explore/styles.go  Lipgloss style definitions
internal/explore/tiocsti_unix.go   TIOCSTI injection (//go:build unix)
internal/explore/tiocsti_other.go  No-op stub for non-unix builds
```

---

## Key Invariants (DB Compatibility with C histx)

These must not change — they ensure the SQLite database is shared between `ghistx` and C `histx`.

| Concern | Rule |
|---------|------|
| **Hash** | `crypto/sha256` → `fmt.Sprintf("%x", ...)` (lowercase hex, 64 chars) |
| **Base64** | `encoding/base64.StdEncoding` with `=` padding — **not** `RawStdEncoding` |
| **N-grams** | Byte-level (not rune), 24-bit mask, 3-char sliding window |
| **N-gram vector** | `ngram.Gen("testing")` → 5 grams summing to `0x22B2525` |
| **SHA-256 vector** | `sha256("histx test hash")` = `06afb70aa2b22ddc874af3881454dca9d6cfd4fedc81b36f85928f0ac3c752d1` |

---

## Database Schema

```sql
CREATE TABLE cmdraw  (hash TEXT PRIMARY KEY, ts INTEGER, cmd TEXT, cwd TEXT);
CREATE TABLE cmdlut  (host TEXT, ngram INTEGER, hash TEXT, UNIQUE(ngram,hash));
CREATE TABLE cmdan   (hash TEXT PRIMARY KEY, type INTEGER, desc TEXT, UNIQUE(hash,type));
CREATE TABLE histxversion (version INTEGER PRIMARY KEY, whence INTEGER);
-- Indices: cmdlut(ngram), cmdlut(hash), cmdan(hash,type)
```

**Migration 1**: `ALTER TABLE cmdraw ADD COLUMN cwd TEXT` (applied if `histxversion.version < 1`).

`cmd` and `cwd` columns are stored as base64-encoded strings matching the C implementation.

---

## Config (`~/.histx`)

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `explore-basic` | bool | `false` | Skip TIOCSTI; print selection to stdout |
| `vi-mode` | bool | `false` | Start explore TUI in vi command mode |
| `search-limit` | int | `5` | Max results returned (clamped to [5, 20]) |
| `local-only` | bool | `true` | Restrict searches to current working directory |
| `squelch` | string (repeated) | — | Add squelch pattern (one per line); prefix `glob:` or `regex:` for non-exact match |
| `squelch-clear-defaults` | bool | `false` | Discard built-in squelch list; use only user-defined patterns |

CLI flags (not stored in config file):

| Flag | Values | Default | Description |
|------|--------|---------|-------------|
| `--source` | `user`, `claude`, `all` | `user` | Filter results by command source |
| `--global` / `-g` | — | — | Bypass CWD filtering for this invocation |
| `--with-squelch` / `-s` | — | — | Include squelched commands in results |

Example:
```
local-only = false
search-limit = 10
vi-mode = true
```

---

## CWD-Local Filtering

By default (`local-only = true`), all search commands (`find`, `cat`, `explore`, `prune`) filter results to the current working directory. If no local results are found, `find` and `explore` automatically fall back to global history:

- **`find`**: prints `── no local results, showing global ──` before results
- **`explore`**: shows `Search [global]: ` in the TUI prompt
- **`cat`**: no fallback — either local or global, no mixing

Use `--global` / `-g` flag on any subcommand to bypass CWD filtering for that invocation.

---

## Search Strategy (`internal/find`)

1. **Long keywords (>2 chars)**: collect n-grams, query `cmdlut` ranked by match count
2. **Short keywords (≤2 chars)** or **no ngram hits**: Aho-Corasick full-scan of `cmdraw` ordered by `ts DESC`
3. CWD filter is applied as `WHERE r.cwd = ?` (base64-encoded path) in both paths
4. If `cwdFilter != ""` and no local hits, retry without filter → `Result.IsGlobal = true`

---

## Explore TUI (`internal/explore`)

- Built with [bubbletea](https://github.com/charmbracelet/bubbletea) (Elm architecture)
- Two modes: `ModeExplore` (select a command) and `ModePrune` (mark for deletion)
- **Exit output** is always the raw command text — unchanged regardless of global fallback
- Uses `tea.WithInputTTY()` because stdin may be a pipe in shell hooks
- TIOCSTI injection happens after `p.Run()` (terminal already restored)
- Linux 6.2+ restricts TIOCSTI; `explore-basic = true` is the documented workaround

**Lipgloss colors** (matching C ANSI codes):
- Grey: `lipgloss.Color("7")`
- Selected: `lipgloss.Color("6")` background (cyan)
- Prune: `lipgloss.Color("5")` background (magenta)
- Dim: `lipgloss.NewStyle().Faint(true)`

---

## Development

```bash
make build        # build for current platform → ./ghistx
make test         # run all unit tests
make build-all    # cross-compile for linux/darwin × amd64/arm64
```

Tests use `modernc.org/sqlite` with `":memory:"` DSN. Integration tests (real temp file DB) are tagged `//go:build integration` and run with `go test -tags integration ./...`.

---

## Updating This File

Keep this file in sync as features are added. In particular, update:
- The **Package Map** when new packages or files are added
- The **Config** table when new `~/.histx` keys are introduced
- The **Key Invariants** section if encoding or hashing changes (they shouldn't)
