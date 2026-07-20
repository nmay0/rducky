# rducky — hotkey AI sidebar for your terminal

Press a tmux hotkey, get a chat sidebar that already sees your terminal (visible
content + a little scrollback), ask your question, close it, get back to work.
Works anywhere tmux does — Linux and macOS, any terminal emulator.

- **Q&A only** — it reads your pane for context; it never runs anything.
- **Fresh session each open** — no background state, nothing persists.
- Follow-up questions automatically include a fresh pane snapshot **only when
  the pane content changed**, so the model stays current without wasting tokens.

## Install

```sh
go build -o rducky .                 # or: go install (puts it in ~/go/bin)
sudo mv rducky /usr/local/bin/       # anywhere on PATH works
```

### 1. API key

`rducky` talks to the Claude API. Any one of these works:

```sh
export ANTHROPIC_API_KEY=sk-ant-...   # from console.anthropic.com → API keys
# or: ant auth login                  # Anthropic CLI OAuth profile
```

Note: tmux keybindings run through the tmux server, so put the export in your
shell profile (`~/.zshrc` / `~/.bashrc`) — the sidebar inherits the shell env.

### 2. tmux keybinding

```sh
rducky install          # prints the snippet
rducky install --write  # appends it to ~/.tmux.conf for you
tmux source-file ~/.tmux.conf
```

The default binding is `prefix + a` ("ask"):

```tmux
bind-key a run-shell "/path/to/rducky toggle -t '#{pane_id}'"
```

Prefer a single keystroke? `bind-key -n M-a run-shell "..."` binds Alt+a with
no prefix.

## Use

| Action | How |
|---|---|
| Open / close the sidebar | `prefix + a` (toggles) |
| Ask | just type |
| Cancel an answer mid-stream | `Ctrl+C` |
| Force a fresh pane snapshot | `/refresh` |
| Close | `Ctrl+D`, `exit`, or the hotkey again |

You can also run `rducky` with no arguments inside tmux — same as the hotkey.

## Config (optional)

`~/.config/rducky/config.yaml` — everything has a default; the file may not exist.

```yaml
model: claude-opus-4-8   # e.g. claude-haiku-4-5 for cheaper/faster answers
max_tokens: 8192
context_lines: 200       # scrollback lines captured above the visible screen
split: h                 # h = right sidebar, v = bottom panel
size: 35%
```

## How it works

`rducky toggle` finds or creates the sidebar: it splits a pane next to yours running
`rducky chat --target <your-pane>`, marked with the `@rducky_sidebar` pane option so the
next toggle can find and kill it. The chat captures your pane with
`tmux capture-pane` (visible screen + `context_lines` of history), plus your OS,
shell, cwd, and foreground command, and streams answers from the Claude API.
