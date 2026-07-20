---
name: verify
description: Build and drive the ike TUI in an isolated tmux session to verify editor/keymap changes end-to-end.
---

# Verifying ike changes at the TUI surface

## Build & launch

```bash
go build -o "$SCRATCH/ike" ./cmd/ike
mkdir -p "$SCRATCH/proj" && printf 'alpha bravo charlie delta\nsecond line here\n' > "$SCRATCH/proj/sample.txt"
tmux -L ikeverify new-session -d -s v -x 120 -y 35 "cd $SCRATCH/proj && $SCRATCH/ike 2>$SCRATCH/stderr.log"
sleep 2
```

- File args are ignored; the app opens with the explorer focused. Open a file
  with `tmux -L ikeverify send-keys -t v Down Enter` (navigate the tree).
- Always use a private socket (`-L ikeverify`) and `kill-server` when done.

## Driving keys

- Plain/vim keys: `send-keys -t v g g 0`, `Escape`, `S-Right` (shift),
  `M-Right` (alt/option), `Down`, `Enter`.
- **Cmd chords cannot be sent symbolically.** Send the Kitty CSI-u encoding as
  raw hex (`send-keys -H`); the super modifier param is `1+8=9`:
  - `cmd+c` → `1b 5b 39 39 3b 39 75` (`ESC[99;9u`)
  - `cmd+x` → `1b 5b 31 32 30 3b 39 75` (`ESC[120;9u`)
  - `cmd+v` → `1b 5b 31 31 38 3b 39 75` (`ESC[118;9u`)
  - `cmd+right` → `1b 5b 31 3b 39 43` (`ESC[1;9C`), `cmd+left` → `...44` (`D`)

## Observing

- `capture-pane -t v -p` for buffer text; `sed -n '3p'` is the first text line
  when the explorer is open.
- `capture-pane -p -e | LC_ALL=C cat -v` to see selection styling
  (selection background is `48;5;238`).
- The hardware cursor position (`display-message '#{cursor_x}'`) is
  unreliable; verify cursor motions by inserting a marker char (`i X Escape`
  or `a X Escape`) and reading the buffer, then `u` to undo.
- The bottom status line (`NORMAL │ … Ln, Col`) is always present; event
  messages (e.g. LSP server status changes) render as toasts
  above it and expire after ~4s.
- Clipboard flows are real on macOS: back up with `pbpaste > backup` first,
  assert with `pbpaste`, restore afterwards.
