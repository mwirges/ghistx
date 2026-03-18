# ghistx

A Go port of [histx](https://github.com/mwirges/histx) — a shell command history indexer with fast fuzzy search and an interactive TUI. Shares the same SQLite database format as the C version; both tools can coexist.

## Installation

```sh
go install github.com/mwirges/ghistx/cmd/ghistx@latest
```

Or build from source:

```sh
git clone https://github.com/mwirges/ghistx
cd ghistx
make build
```

## Usage

```
ghistx                    # browse history newest-to-oldest (paged via $PAGER)
ghistx <hashlet>          # re-execute a command by its hash prefix (see Shell Integration)
ghistx index <cmd>        # index a command
ghistx find <keyword...>  # search history
ghistx cat                # dump all history oldest-first
ghistx explore [tmpfile]  # interactive fuzzy-search TUI
ghistx prune              # interactive TUI to mark and delete entries
```

Add `--global` / `-g` to any command to search across all directories instead of just the current one.

### Hashlets

Every indexed command is identified by the first few characters of its SHA-256 hash — just enough to be unambiguous within the current result set (minimum 4 characters, like git short SHAs). They appear at the start of each output line:

```
$ ghistx
b563  [2 minutes ago]  kubectl apply -f deploy.yaml  (/home/user/myapp)
a1f2  [1 hour ago]     git push origin main           (/home/user/myapp)
3dc9  [3 hours ago]    make test                      (/home/user/myapp)
```

## Shell Integration

Shell integration lets `ghistx <hashlet>` place the resolved command into your prompt for editing before you run it — the same experience as `ctrl+r` reverse search.

> **Why a shell function?** TIOCSTI (the ioctl used to inject text into a terminal input buffer) is unavailable on macOS since Ventura and on Linux since kernel 6.2+. Instead, `ghistx --tmpfile <file> <hashlet>` writes the command to a file, and the shell wrapper reads it back and stuffs it into the line editor buffer.

### zsh

Add to `~/.zshrc`:

```zsh
# Wrap ghistx so that hashlet arguments inject the command into the prompt.
function ghistx() {
  if [[ $# -eq 1 ]] && [[ "$1" =~ ^[0-9a-fA-F]{4,}$ ]]; then
    local tmpfile
    tmpfile=$(mktemp)
    command ghistx --tmpfile "$tmpfile" "$1"
    local rc=$?
    [[ $rc -eq 0 && -s "$tmpfile" ]] && print -z -- "$(< "$tmpfile")"
    rm -f "$tmpfile"
    return $rc
  fi
  command ghistx "$@"
}

# Optional: bind ctrl+h to open the explore TUI (ctrl+r style).
function _ghistx_explore() {
  local tmpfile
  tmpfile=$(mktemp)
  command ghistx explore "$tmpfile"
  if [[ -s "$tmpfile" ]]; then
    BUFFER=$(< "$tmpfile")
    CURSOR=${#BUFFER}
  fi
  rm -f "$tmpfile"
  zle reset-prompt
}
zle -N _ghistx_explore
bindkey '^h' _ghistx_explore
```

`print -z` pushes the command into the zsh line editor buffer. Press Enter to run it, or edit it first.

#### Remap `!` for hashlet lookup (zsh)

Override `accept-line` so that typing `!<hashlet>` and pressing Enter resolves the command into the buffer — exactly like zsh's `HIST_VERIFY` does for normal `!` expansion. Press Enter a second time to run it, or edit first.

```zsh
function _ghistx_accept_line() {
  if [[ "$BUFFER" =~ '^![0-9a-fA-F]{4,}$' ]]; then
    local tmpfile
    tmpfile=$(mktemp)
    command ghistx --tmpfile "$tmpfile" "${BUFFER[2,-1]}" 2>/dev/null
    if [[ -s "$tmpfile" ]]; then
      BUFFER=$(< "$tmpfile")
      CURSOR=${#BUFFER}
      rm -f "$tmpfile"
      zle reset-prompt
      return  # stay in editor — press Enter again to run
    fi
    rm -f "$tmpfile"
  fi
  zle .accept-line
}
zle -N accept-line _ghistx_accept_line
```

If the hashlet isn't found, the line falls through to normal `accept-line` behavior (including zsh's own `!` history expansion).

### bash

Add to `~/.bashrc`:

```bash
# Wrap ghistx so that hashlet arguments present the command for editing.
function ghistx() {
  if [[ $# -eq 1 ]] && [[ "$1" =~ ^[0-9a-fA-F]{4,}$ ]]; then
    local tmpfile
    tmpfile=$(mktemp)
    command ghistx --tmpfile "$tmpfile" "$1"
    local rc=$?
    if [[ $rc -eq 0 && -s "$tmpfile" ]]; then
      local cmd
      cmd=$(< "$tmpfile")
      rm -f "$tmpfile"
      # Pre-fill readline with the command; press Enter to run or edit first.
      read -re -i "$cmd" cmd && eval "$cmd"
      return
    fi
    rm -f "$tmpfile"
    return $rc
  fi
  command ghistx "$@"
}

# Optional: bind ctrl+h to open the explore TUI.
_ghistx_explore() {
  local tmpfile
  tmpfile=$(mktemp)
  command ghistx explore "$tmpfile"
  if [[ -s "$tmpfile" ]]; then
    READLINE_LINE=$(< "$tmpfile")
    READLINE_POINT=${#READLINE_LINE}
  fi
  rm -f "$tmpfile"
}
bind -x '"\C-h": _ghistx_explore'
```

`read -re -i "$cmd"` opens a readline prompt pre-filled with the command. Edit and press Enter to run it.

### fish

Add to `~/.config/fish/config.fish` (or save as `~/.config/fish/functions/ghistx.fish`):

```fish
function ghistx
  if test (count $argv) -eq 1; and string match -qr '^[0-9a-fA-F]{4,}$' -- $argv[1]
    set tmpfile (mktemp)
    command ghistx --tmpfile $tmpfile $argv[1]
    set rc $status
    if test $rc -eq 0 -a -s $tmpfile
      commandline -- (cat $tmpfile)
      commandline -f repaint
    end
    rm -f $tmpfile
    return $rc
  end
  command ghistx $argv
end

# Optional: bind ctrl+h to open the explore TUI.
function _ghistx_explore
  set tmpfile (mktemp)
  command ghistx explore $tmpfile
  if test -s $tmpfile
    commandline -- (cat $tmpfile)
    commandline -f repaint
  end
  rm -f $tmpfile
end
bind \ch _ghistx_explore
```

### Indexing commands automatically

To index every command you run, add a hook to your shell. This is the same hook format used by the C histx.

**zsh** — add to `~/.zshrc`:
```zsh
autoload -Uz add-zsh-hook
_ghistx_index() { command ghistx index -- "$1" 2>/dev/null; }
add-zsh-hook preexec _ghistx_index
```

**bash** — add to `~/.bashrc`:
```bash
_ghistx_index() { command ghistx index -- "$BASH_COMMAND" 2>/dev/null; }
trap '_ghistx_index' DEBUG
```

**fish** — add to `~/.config/fish/config.fish`:
```fish
function _ghistx_index --on-event fish_preexec
  command ghistx index -- $argv[1] 2>/dev/null
end
```

## Configuration

Configuration is read from `~/.histx` (shared with the C version):

```
# ~/.histx
local-only    = true    # filter searches to current directory (default: true)
search-limit  = 5       # max results for find/explore (range: 5–20, default: 5)
explore-basic = false   # skip TIOCSTI; print selection to stdout (default: false)
vi-mode       = false   # start explore TUI in vi command mode (default: false)
```

## Compatibility

`ghistx` uses the same SQLite database schema and encoding as the C `histx`:

- Database: `~/.histx.db` (override with `--db` or `$HISTX_DB_FILE`)
- Commands are stored as standard base64 with padding
- Hashes are lowercase SHA-256 hex strings
- Both tools can read and write the same database simultaneously
