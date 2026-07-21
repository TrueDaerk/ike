---
type: concept
title: Integrated Terminal
description: Roadmap 0170 — PTY-spawned shell rendered through a VT emulator as a pane; raw key routing with a documented reserved set, scrollback paging, layout restore as fresh shells, sessions surviving project switches; command sessions + occupied tracking for run-in-terminal (0350).
resource: internal/terminal
tags: [architecture, terminal, pty, vt, pane, run]
timestamp: 2026-07-21T00:00:00Z
---

# Integrated Terminal (Roadmap 0170)

`internal/terminal` embeds a real shell as a pane (spec: epic #88), complete
across the epic's four slices: PTY + VT core (#95), workspace integration
(#96), commands & UX (#97) and toolchain environment activation (#98, #652).

## Command sessions & run reuse (0350, #574)

- `StartCommandSession(key, argv, dir, …)` / `terminal.NewCommand` spawn a
  **program with arguments directly on the PTY** (no wrapping shell) — the
  run-in-terminal seam for run configurations (Epic #572). The program is
  interactive (stdin is the PTY); `Session.ExitCode()` keeps the exit status,
  and a finished command session renders `[process exited with code N]`
  instead of the bare marker. `IsCommand()`/`Argv()` distinguish it from a
  shell.
- `Model.Occupied()` tracks whether the user ever sent input (a forwarded key
  or a paste; scrollback paging does not count). `Model.StartCommand` replaces
  a model's session in place — the reuse path when a run takes over a
  terminal — resetting scroll, selection and occupancy.
- `Registry.ReusableRunTerminal()` (internal/pane) scans panes and terminal
  tabs in insertion order for a take-over candidate: never typed into, or its
  process already ended (a finished run's terminal is fair game again).

## Session (`session.go`)

- **PTY lifecycle** via `creack/pty`: the shell (`terminal.shell` config
  override → `$SHELL` → `/bin/sh`) spawns in the project root with
  `TERM=xterm-256color`; pane resizes propagate through `pty.Setsize`
  (SIGWINCH for the child) and the emulator — **debounced** (#804): the first
  resize applies immediately, a rapid burst (divider drag) folds into one
  trailing apply of the final size, so the child redraws once instead of per
  drag step; `Close` kills the child and releases the PTY, and a shell `exit`
  sends `ExitedMsg` so the root model closes the pane.
- **VT emulation** via `charmbracelet/x/vt` (`SafeEmulator` — the read loop
  writes while Update/View read): PTY output feeds `Write`, the screen
  renders with `Render()` (ANSI-styled, so 16/256/truecolor pass through),
  and key presses go through `SendKey`, which encodes per the emulator's
  input modes (application cursor keys etc.); a write loop pumps the
  emulator's host-bound bytes (key encodings, DA/DSR query replies) back
  into the PTY. The emulator drops non-special keys that still carry a
  modifier, so the pane normalizes text-producing presses whose only
  modifiers are shift/caps-lock/num-lock (`toVTKeys` in `model.go`) —
  uppercase letters reach the shell as their produced text (#224).
- **Batching**: output notifications are coalesced per session (`OutputMsg`,
  one per 8ms quiet interval), and the app's input coalescer (#602) folds
  concurrent OutputMsgs **across sessions** into one batch per adaptive flush
  (#803) — so `yes`, a build log, or eight busy TUI panes at once cannot
  flood the render loop or starve input handling.
- **Resize content preservation** (#807): the upstream emulator hard-truncates
  the grid on shrink (clipped cells are destroyed). `Session` keeps a resize
  reserve — the fullest known content per screen row, snapshotted before every
  applied resize — and writes the clipped region back after a grow, guarded by
  a per-row prefix match so content the child rewrote meanwhile is never
  overwritten (a height restore additionally requires every overlapping row to
  match, since scrolled content shifts row indexes). Scrollback lines keep
  their full width upstream — only the render clips — so scrollback needs no
  reserve. `gridMu` serializes the feed loop against the snapshot/restore
  sequence (CellAt returns pointers into the live buffer).
- **View render cache** (#803): `Session.View` caches the rendered grid keyed
  by a mutation version (bumped on feed writes, resize, clear); a frame
  re-renders only grids that actually changed (measured ~270µs per 200×60
  grid render vs ~13ns cached), so N terminal panes no longer multiply the
  per-frame render cost.
- **Output spooling** (#734, `spool.go`): the PTY read loop no longer writes
  into the emulator directly — it drains the kernel TTY queue into an
  in-process FIFO (`spool`, 16 MiB soft cap) and a separate feed loop replays
  the chunks into the emulator in order. A stalled emulator or render loop
  (app suspend/resume around a macOS lock/sleep window) therefore cannot
  backpressure into the kernel queue, where buffered output can be flushed
  and lost; everything buffers in-process and replays on resume.
- **Teardown sequencing** (#748): upstream vt's `Emulator.Close` is not safe
  concurrently with `Read`/`Write` (plain-bool closed flag), so `teardown`
  joins the loops in order — read loop (closed PTY errors its read), feed
  loop (spool drains, exit output kept), then the write loop, woken by a
  sentinel byte through the host-bound pipe — and closes the emulator last.
  `go test -race ./internal/terminal/` is clean.

## Pane citizenship (#96)

`pane.KindTerminal` joins explorer/editor in the instance registry
(`AddTerminal`, keys `terminal`, `terminal:2`, …; `Close` ends the session).
The pane title shows **shell + origin dir** (`TERMINAL — zsh · goproj`; the
dir compacts once it differs from the working directory). The cursor cell
reverse-videos while focused (`model.go` splices it ANSI-aware). While a
terminal (or the explorer) holds focus, the **status line names that pane
kind** — `TERMINAL │ zsh · goproj` (plus `[exited]` for a dead shell) or
`EXPLORER` — instead of mirroring the active editor's mode/file/cursor, so
the line always says where keystrokes go (#381).
`terminal.new` splits the active editor's leaf toward the bottom — the
conventional JetBrains placement.

**Layout persistence**: terminal leaves save with their origin dir
(`paneIdentity{Kind: "terminal", Path: dir}`) and restore as **fresh shells**
in the saved position — no process resurrection, the cwd respawns.

**Project switch (0090)**: live sessions are adopted into the freshly built
workspace (`adoptTerminals` in app/switch.go), split below the new active
editor and titled with their origin root; dead ones close for good. New
terminals root in the new project as always (spawn dir is pinned absolute).
When the target's layout restore already recreated a terminal under the same
key — a fresh placeholder shell for the very session being carried over — the
live session **takes over that pane** (`Registry.AdoptTerminal` closes the
placeholder and swaps in place, #320) instead of splitting a second leaf,
which would both duplicate the terminal and render one instance in two
mirrored panes.

## Key routing — the reserved set

While a live terminal is focused, **every key goes raw to the PTY** — vim,
htop and less must see tab, ctrl+c, esc and the F-keys. The documented
reserved set (`terminalReservedKey` in internal/app) is exactly:

| Key | Effect |
|---|---|
| `ctrl+tab` | move focus to the next pane (delivery is terminal-dependent — many terminals cannot send it; 0081's reality probe owns the call) |
| `alt+f12` | `terminal.toggle` — return focus to the previous pane (the reliable hatch) |
| `cmd+t` | new sibling terminal (#729, iTerm-style): a terminal tab hosted by an editor pane gets a sibling tab in the same pane (#573); a dedicated single-session terminal pane gets a fresh terminal pane split below it — focused either way. Outside terminals `cmd+t` keeps its global binding (`vcs.updateProject`) |
| `ctrl+arrows` | spatial focus moves out of the terminal (#228) — the same `keymap.bindings.focus_*` overrides apply; a disabled direction stays with the shell |
| `cmd+c` | copy an active mouse selection (#227) — without one the key stays with the shell |
| `cmd+v` | paste the system clipboard through the bracketed-paste path (#727) — under the Kitty protocol the host delivers cmd+v as a key, so the app performs the paste itself; the debug panel's embedded debuggee terminal (#676) gets the same treatment |
| global navigation chords | the chords bound to `palette.searchEverywhere` (default `cmd+shift+a`), `palette.recentFiles` (`cmd+e`) and `project.switch` (`cmd+shift+p` / `ctrl+shift+p`), plus a configured `palette.toggle_key`, dispatch in the IDE instead of the shell (#805) — resolved via the live binding table, so rebinds move along. Single-step chords only; esc-esc stays with the shell (vim/lazygit would see side effects) |

`shift+pgup` / `shift+pgdn` page the **scrollback** inside the pane (half a
grid per step, position marker on the bottom line, any typed key snaps back
to live). A dead session (shell exited) falls back to normal key handling so
`ctrl+w` can close the pane.

## Command completion popup (#740)

JetBrains-style completion at the shell prompt (`complete.go`). The command
line is read straight off the emulator — `LineText` of the cursor row, left
of the cursor; the prompt is stripped heuristically (`$ `, `% `, `> `, `# `,
`❯ `), command separators (`|`, `;`, `&&`, `||`) start a fresh command — so
the shell keeps owning line editing and history. Sources per word: PATH
executables while the first word is typed, make targets after `make`
(Makefile/makefile/GNUmakefile in the session's start dir), files/dirs
relative to the start dir otherwise (dir part in the word honoured, `~/` and
absolute paths too, dotfiles only on a `.` prefix, dirs keep a trailing `/`).
Every candidate strictly extends the typed word, so **accepting (enter/tab)
pastes just the remainder** through the bracketed-paste path; a directory
re-arms the popup to keep descending. `ctrl+space` opens the popup on demand
(empty word shows everything); **auto-suggest** re-arms on every printable
key and recomputes on the next `OutputMsg` — the shell must echo the
keystroke before the cursor row reads current — and is togglable via
`terminal.autosuggest` (default on, applies live). up/down move, esc
dismisses, any other key invalidates and passes through raw. The popup is
inactive on the alternate screen (vim/htop), in command sessions, and while
paging scrollback; it renders as a bordered list composited over the grid at
the word's start column, below the cursor row when it fits, above otherwise.

**Mouse selection & copy** (#227, `MousePress`/`MouseDrag`/`MouseRelease` in
`model.go`): a left drag over the grid selects text — highlighted in reverse
video, anchored in virtual coordinates (indices into [scrollback ++ screen])
so it survives scrollback paging and can span history and live rows. The
selection is linear (stream-style): start line from the anchor column, full
middle lines, end line up to the head column; `cmd+c` copies it right-trimmed
and newline-joined to the system clipboard and drops the highlight. Any key
routed to the shell (and `terminal.clear`) clears it. When the child enabled
mouse reporting, press/drag/release forward to it instead — selection is
unavailable then, like in xterm.

**Mouse wheel** (#226, `MouseWheel` in `model.go`): the wheel goes to whoever
asked for it — a child that enabled a DEC mouse-reporting mode
(?9/?1000–?1003, tracked via the emulator's `EnableMode`/`DisableMode`
callbacks) gets the encoded event through `SendMouse`; an alt-screen child
without mouse reporting gets arrow keys, three per notch (the xterm
"alternate scroll" convention — this is how `less`/`man` scroll); a plain
shell pages the pane's scrollback.

A coalesced wheel burst (#238) arrives as **one call carrying the whole line
delta** (#669) — `flushWheel` no longer replays the batch event-by-event.
What may be forwarded to the child is bounded by `wheelChildBudget`
(~one screenful of arrow keys, wheel events per notch derived from it), so a
fast trackpad burst can no longer flood the PTY and leave the child scrolling
for seconds after the user stopped; the pane's own scrollback path applies
the full distance (cheap, clamped to history).

**macOS editing chords** (#225, #240, `motionKey` in `model.go`): the pane
translates the iTerm "natural text editing" motions to the readline/ZLE
emacs-mode defaults — `option+left`/`right` → `ESC b`/`ESC f` (word jump),
`cmd+left`/`right` → `ctrl+a`/`ctrl+e` (line start/end),
`option+backspace` → `ESC DEL` (kill previous word), `option+forward-delete`
→ `ESC d` (kill next word, #733), `cmd+backspace` →
`ctrl+u` (kill to line start). Shift-augmented variants behave the same (a
PTY has no selection). Cmd delivery is terminal-dependent (the 0081
reality-probe caveat).

## Commands (#97)

- **`terminal.toggle`** (default `alt+f12`, fragile like every alt+F-key):
  the JetBrains state machine — no terminal → open one below the active
  editor; one exists unfocused → focus it (remembering where focus was);
  focused → return focus to the remembered pane (falling back to the active
  editor, then the explorer). Inside a focused terminal the reserved-set
  handler catches `alt+f12` before the raw pass-through. Custom tool panes
  (#741) never count as "the terminal" here (#772): with only tool panes
  open, toggle spawns a new regular terminal instead of focusing a tool —
  the same rule keeps `terminal.clear` off tool panes.
- **`terminal.new`** opens an additional session; **`terminal.clear`** wipes
  screen and scrollback via the canonical `CSI 2J` + `CSI 3J` pair (2J alone
  pushes the visible lines *into* the scrollback — the xterm behaviour) and
  asks the shell to repaint its prompt with the ctrl+l convention.
- The Tools menu carries "Terminal" (toggle) and "New Terminal"; all three
  commands are palette-reachable.
- **Titles**: the shell's OSC 0/2 reports (the running command) append to
  the pane title — `TERMINAL — zsh · goproj · npm run build`. Inside OSC
  strings the raw byte `0x9C` (8-bit C1 ST) is kept as payload
  (`internal/terminal/oscpatch.go`, #561): many UTF-8 runes carry it as a
  continuation byte (the U+2700 dingbats, e.g. Claude Code's `✳` spinner
  titles), and dispatching on it would split the rune and print the rest of
  the title into the grid as ghost text. Only BEL and `ESC \` terminate,
  matching xterm/Ghostty.

## Toolchain environment activation (#98, #652)

The **effective** interpreter per language — the explicit settings-page
choice beating project detection, through the same `lang.Interpreter` seam
LSP, debug and the statusline read — is activated in fresh IDE terminals the
way JetBrains does it, so `which python3` shows the real interpreter
(`internal/terminal/env.go`, `PlanActivation`). Per mapping, one of four
modes applies:

- **venv** (the interpreter's bin parent carries `pyvenv.cfg`): activate like
  `source bin/activate` — `<venv>/bin` is prepended to PATH and
  `VIRTUAL_ENV` is set. No shim; `which python3`/`python`/`pip` all print
  the venv paths. A detected project `.venv` activates too — the old
  "silent detection never injects" rule is gone.
- **PATH prepend** (private toolchain dir — pyenv versions, mise/asdf
  installs, `/usr/local/go/bin`, anything outside the shared-system list):
  the interpreter's own directory goes ahead of PATH, so real versioned
  paths win `which`. A *detected* interpreter whose directory already wins
  the base-PATH lookup for its own name is skipped (env untouched — it is
  what PATH gives anyway).
- **shim** (explicit choice in a shared system dir: `/bin`, `/usr/bin`,
  `/usr/local/bin`, `/opt/homebrew/bin`, sbin variants): prepending would
  reorder the whole PATH and shadow unrelated tools, so the per-project
  **shim directory** (`.ike/shims`, IKE_CONFIG_DIR-overridable) keeps
  `#!/bin/sh` exec wrappers for just that language's command names (python
  covers `python` + `python3`). Stale shims sweep when the setting is
  removed or the mapping moves to venv/prepend mode.
- **none** (detected interpreter in a shared system dir): ambient — the
  environment stays untouched.

With nothing to inject the spawn environment is exactly the inherited one.
The overlay applies to **new** terminals; running sessions keep their
environment (a PATH prepend cannot retarget a live shell — JetBrains behaves
the same). A config reload re-plans, so the next terminal picks up changes.

- The pane **title indicates the active mappings**:
  `… · python→~/proj/.venv/bin/python` (only mappings that actually inject).
- **Windows**: the shims are POSIX `sh` scripts; a windows port writes
  `<name>.cmd` wrappers into the same directory (`@"%target%" %*`) —
  documented here, darwin/linux land first like the rest of the PTY stack.

## Quality bar

Verified inside the pane: `vim` (alt screen, insert/normal, `:wq` writes),
`less` (paging), shell line editing and wrapping, colored output, `stty size`
reflecting pane resizes, scrollback paging over `seq` output, layout restore
with a fresh prompt, a session surviving a project switch with its
origin-root title, and `command -v python3` in a project with an active venv
mapping resolving to the venv interpreter itself (`VIRTUAL_ENV` set).
