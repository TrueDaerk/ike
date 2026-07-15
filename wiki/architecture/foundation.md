---
type: concept
title: Foundation Slice
description: Root model that hosts the explorer and editor panes, owns layout/focus, and routes messages between them.
resource: internal/app/app.go
tags: [architecture, bubbletea, foundation]
timestamp: 2026-07-15T14:00:00Z
---

# Foundation Slice

The first vertical slice of IKE (roadmap 0010). Goal: open a directory, browse
the tree, open a file, edit it with vim controls, save it.

## Structure

```
cmd/ike/main.go     entrypoint, tea.NewProgram(app.New())
internal/app/       root model: layout + focus + global keys + routing
internal/explorer/  file tree pane
internal/editor/    line buffer + modal vim state machine
```

IKE runs on **Bubble Tea v2** (`charm.land/bubbletea/v2`; see
former Roadmap 0085 — planning moved to GitHub issues, spec in git history). Under v2, alt-screen, mouse mode
and the kitty keyboard enhancements are declared on the root model's `View()` (which
returns a `tea.View` struct), not passed as `tea.NewProgram` options. The root dispatches
`tea.KeyPressMsg` only, ignoring `tea.KeyReleaseMsg`, and normalises the four v2 mouse
messages (`MouseClickMsg`/`MouseReleaseMsg`/`MouseWheelMsg`/`MouseMotionMsg`) into one
internal `mouseEvent` for the drag handler.

Each pane is a `tea.Model`-shaped component (Init/Update/View) embedded in the
root `app.Model`. The root forwards `tea.Msg` to the focused child and owns
layout. Layout geometry itself is no longer hard-coded: the root drives a pure
split tree (see [Pane Layout & Drag](/architecture/pane-layout.md)) that computes
each pane's rectangle and supports mouse divider-resize and title-bar move.

## CLI open targets (Roadmap 0270)

`ike path[:line[:col]]... [+N path]` opens files from the command line.
`main.go` parses argv through the pure grammar in `internal/cli` (`cli.Parse`;
a malformed invocation prints usage and exits before any UI), then calls
`Model.OpenCLITargets` (`internal/app/cli_open.go`) **after** construction —
session restore already ran in `newWithHost`, so the requested files win focus
over the restored layout. Targets open as tabs in argument order through the
standard funnel (`openPathAt`: canonicalization, tab reuse, shared buffers);
the first target ends focused with its 1-based line/col mapped to the editor's
0-based cursor (out-of-range clamps), and the explorer reveals it. A path that
does not exist on disk opens as an empty unsaved buffer with that path
(vim-style, `editor.NewFile`); the first `:w` creates the file. `EventFileOpened`
hooks and the initial reparse fire in `Init` (#332) like for every file already
open at launch.

`command | ike -` (#344) reads piped stdin to EOF before the UI starts
(`readStdin` in `cmd/ike/main.go`; the package deliberately stays a single
file so `go run cmd/ike/main.go` keeps compiling, #362) and opens it as a
pathless scratch buffer after any file
targets, focused (`Model.OpenStdinBuffer`). The buffer is dirty + never-saved
(`editor.RestoreText`, the untitled-crash-restore flow), so quitting runs the
unsaved-changes guard and `:w <path>` names it. The keyboard comes from an
explicitly opened `/dev/tty` via `tea.WithInput` — bubbletea's own
non-terminal-stdin fallback does not deliver key events in this setup. `ike -`
on a TTY fails fast with usage exit code 2 (nothing piped; a blocking read
would hang).

## Message routing

- `explorer.OpenFileMsg{Path}` — emitted when the user opens a file; the root
  calls `editor.Load(path)` and moves focus to the editor.
- `editor.CloseMsg{}` — emitted by `:q` / `:wq`; the root replaces the editor
  with a fresh empty one and returns focus to the explorer.

## External file changes (Roadmap 0140)

`internal/watch` is the file-watcher service (#80): fsnotify on the project
root, recursive (watching newly created directories) but pruning
dot-directories and vendored/noise dirs (`node_modules`, `__pycache__`,
`site-packages`, `vendor`) via `skipWatchDir` so a populated `.venv` does not
register thousands of watches (#596), debounced ~100ms with per-path coalescing
(removal wins; create survives a follow-up write). It emits `watch.EventMsg{Kind, Path}` — `FileChanged` /
`FileCreated` / `FileRemoved` / `DirChanged` — through `host.Send`; the root
model routes file kinds to the editor leaf owning the path
(`editorKeyForPath`) and `DirChanged` to the explorer (consumers land in
#81–#83).

- **Self-event suppression:** the editor emits `EventSave` after every disk
  write; the app's emitter adapter stamps `watcher.MarkSaved(path)`, and
  events for that path within 500ms are dropped — IKE's own saves never
  round-trip as external changes.
- **Poll fallback:** for filesystems where fsnotify under-reports (network
  mounts), open buffers are `Track`ed and `Poll()` compares mtime+size,
  hashing on suspicion (an mtime-only touch never reports), behind the same
  message shape.
- **Config:** `files.watch = true|false` (default true). `main.go` starts the
  watcher after wiring `Send`; a project switch (Roadmap 0090) calls
  `StartWatcher` again, which restarts on the new root.

## Input coalescing (#602)

bubbletea reads one message at a time from an unbuffered channel and runs
`Update` + a full `View` render for **every** message, with no lookahead. A mouse
burst — one `MouseWheelMsg` per scroll notch, one `MouseMotionMsg` per drag cell —
would therefore make a keystroke typed right after wait behind dozens of
Update+render passes: the IDE stays correct but feels frozen.

`internal/app/inputcoalesce.go` installs a `tea.WithFilter` hook
(`MouseCoalescer`). Wheel and motion events are absorbed into an accumulator and
the filter returns `nil`, so bubbletea skips both Update and the render for them
and the queue drains at channel speed — a following key is reached at once. A
~16ms timer re-injects the folded events as one `coalescedInputMsg`, which
`applyCoalescedInput` replays in a single pass (one render), preserving net scroll
distance and the latest motion. Every other message — keys, mouse
press/release/click, resize, paste — passes straight through, so keys are never
dropped or delayed. `main.go` wires the coalescer's flush sender to the program's
`Send` alongside the host's.

The flush is **single-in-flight with backpressure** (#606). The re-inject `Send`
blocks until the loop accepts it, and a sustained scroll is render-bound (a
full-frame terminal write is syscall-heavy), so the loop accepts slower than the
wheel emits. The `armed` flag therefore stays set across the whole flush —
including the blocking send — and is re-armed only *after* the send returns, and
only if new events arrived meanwhile. That bounds the coalescer to one in-flight
flush; without it every 16ms tick spawned another flush goroutine that blocked in
`send`, and the pile grew without bound the longer you scrolled — latency creeping
back up to the pre-coalescer feel.

The flush cadence is also **adaptive to render cost** (#610). `Model.render`
records its wall-clock cost in `renderNanos`; the coalescer waits
`renderCost × renderBudgetDivisor` before the next batch (clamped to a 16ms floor
and a 66ms ceiling), so rendering coalesced input stays near `1/divisor` of a core
no matter how expensive a single frame is. A fixed 60fps cadence would recompose
the whole fullscreen frame — every pane's `View` plus lipgloss `paneBox`/`Join`
re-measuring every line — back-to-back and peg a core during a sustained scroll.
With pacing, cheap frames still flush at ~60fps while an expensive fullscreen
frame throttles toward ~15–22fps, bounding CPU instead of saturating it. Keys and
clicks bypass the coalescer, so they are never paced.

## Render hot path (#608)

bubbletea recomputes the whole frame (`Model.View`) and writes it every message,
so anything `View` touches runs at frame rate and its cost scales with window
size — a fullscreen multi-pane layout is the worst case. The working directory is
the notable trap: `os.Getwd()` is a `stat` syscall on macOS, and it was called
every frame from the terminal title (`displayDir`), the status line
(`displayPath`), and the breakpoint gutter (`projectRoot`→`bpKey`), once per
pane — ~49% of all CPU under a fullscreen scroll. It is now cached
(`cachedGetwd`, `internal/app/cwdcache.go`), read once and reused until a project
switch invalidates it (`invalidateCwd`, right after the switch's `os.Chdir`). The
guideline: `View` and everything it calls must avoid syscalls, config reads, and
filesystem work — memoize or cache anything that does not change per frame.

## Slow-update diagnostics (#125)

Anything that stalls the root model's `Update` freezes the whole UI (the #123
deadlock was invisible until it hung). Every Update pass over 200ms appends a
timestamped line — message type + duration — to the per-project state log
(`.ike/debug.log`, `IKE_CONFIG_DIR`-aware like the layout store). Logging is
best-effort; a failed write never affects the editor.

## Focus and global keys

`Tab` toggles focus between panes. `Ctrl+C` always quits; `q` quits when the
explorer is focused or when the editor is focused in normal mode
(`app.quitKey`). Panes without an editor tab — diff viewer, markdown preview,
VCS tool window — never quit on `q`; the key routes to the pane (#529). While
the editor is capturing text (insert or command mode), global single-letter
keys are suppressed so typed characters reach the buffer
(`app.editorCapturing`); a diff pane in edit mode (#496) counts the same way,
so text typed into its embedded editor is never stolen.

## Status line

A one-row bar renders the editor mode, file name, dirty marker, and 1-based
cursor position; when the editor is in command mode it shows the typed
`:command` line instead.
