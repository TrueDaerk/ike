# Roadmap 0060 — Vim-Like Editor

This roadmap turns the minimal modal editor scoped in Roadmap 0010 into a genuinely
usable vim-like editor: a real buffer model, the full set of modes, motions,
operators, text objects, registers, undo/redo and repeat, plus viewport management
driven by user settings. The editor remains a `tea.Model`-shaped pane in
`internal/editor`; this roadmap extends that pane rather than replacing it.

All editor actions and ex-commands are surfaced through the plugin registry
(Roadmap 0020) as `Command` capabilities, so the command palette (07) and the
keybinding layer (08) reach them through a single mechanism — never a parallel one.
Configuration (tab width, expand tabs, line numbers, relative numbers, scrolloff,
…) is consumed from the `[editor]` section provided by `internal/config` (04).
Hooks are left for LSP (10) but no language intelligence is implemented here.

## Prerequisites / Dependencies

- **01 Foundation** — editor pane scaffold in `internal/editor`, the "open file"
  `tea.Msg` routed by the root, and the basic normal/insert modal loop with simple
  motions and `:w`/`:q`. This roadmap extends that code in place.
- **02 Plugins registry** — `internal/plugin` (`Command`, `Keymap`, `Pane`,
  `FileHandler`, `Hook`), `internal/registry`, `internal/host` (`host.API`).
  Editor actions and ex-commands register as `Command` capabilities.
- **04 Settings** — `internal/config` exposes the `[editor]` section. The editor
  reads it; it does not own the loader, file format, or reload mechanism.

Soft / forward dependencies (seams only, not implemented here): **07** command
palette UI, **08** keybinding defaults, **10** LSP.

## Architecture

```
internal/editor/
  editor.go            # Model: the tea.Model pane (extends 01); wires sub-parts
  buffer/
    buffer.go          # Buffer: line-slice ([]string + byte/rune helpers)
    buffer_test.go
    position.go        # Position{Line,Col}, Range, clamping, rune/byte mapping
    edit.go            # primitive edits: Insert/Delete/Replace over a Range
  mode/
    mode.go            # Mode enum: Normal, Insert, Visual, VisualLine,
                       #            VisualBlock, CommandLine, Replace
    pending.go         # pending-operator state machine (op + count + register)
  motion/
    motion.go          # Motion func(Buffer, Position, count) Position + kind
    charwise.go        # h j k l 0 ^ $ f t F T ;
    wordwise.go        # w b e (and W B E)
    linewise.go        # gg G { }
    match.go           # % bracket match
    find.go            # in-line f/t/F/T state, repeat with ; ,
  search/
    search.go          # / ? n N, incremental match, regex/literal toggle
  operator/
    operator.go        # Operator func over a resolved Range -> edit + register
    ops.go             # d c y p (and gp), dd cc yy, line/char/block apply
    compose.go         # operator + motion / text-object composition + counts
  textobject/
    textobject.go      # TextObject -> Range
    pairs.go           # i( a( i" a" i{ a[ i< ... (inner/around pairs)
    word.go            # iw aw (word / WORD)
  register/
    register.go        # named "a-"z, unnamed ", yank "0, small-delete "-, "+ clip seam
  history/
    history.go         # undo/redo (linear stack now; tree-ready API), repeat (.)
  viewport/
    viewport.go        # scroll offset, scrolloff, cursor->screen, gutter sizing
    gutter.go          # absolute / relative line numbers from config
  excmd/
    excmd.go           # :w :q :wq :q! :e parser; each registered as a Command
  commands.go          # registers editor actions + ex-commands as registry Commands
  events.go            # on-change / cursor-move / completion-trigger hook seam (LSP)
```

`editor.go` owns the `Model` and dispatches key input through the mode/pending
state machine, which resolves operators against motions/text-objects, applies
primitive edits to the `Buffer`, records them in `history`, and asks `viewport`
to render. `commands.go` is the single bridge to `internal/registry`.

## Design rules

- **One buffer representation: line slice (`[]string`).** Justification: IKE edits
  source files that are naturally line-oriented; vim motions, visual-line/block
  ops, and the gutter all reason in lines. A `[]string` keeps line addressing O(1),
  makes linewise operators trivial, and is simple to test. Within a line we work on
  rune slices for column-accurate motions. (A piece table / gap buffer is a later
  optimization if profiling on large files demands it — the `buffer` package API is
  written to allow swapping the backing store without touching callers.)
- **Columns are rune-based, storage is byte-based.** `position.go` is the single
  place that maps between rune columns (what motions/cursor use) and byte offsets
  (what edits touch). No other package does rune/byte arithmetic.
- **Motions return a target + a kind** (charwise/linewise/inclusive/exclusive);
  operators consume `(start, end, kind)`. This keeps operator+motion composition
  uniform and is what makes counts and text objects fall out cleanly.
- **Edits go through `buffer/edit.go` only**, and every applied edit produces a
  reversible record pushed to `history`. `.` replays the last change; undo/redo pop
  the stack. The history API is tree-shaped (parent links) even though the first
  implementation walks it linearly.
- **No direct registry imports from the hot path.** Actions are plain functions
  with stable IDs; `commands.go` wraps them as `Command` capabilities. The palette
  (07) and keybindings (08) invoke by command ID — the editor never grows its own
  command-dispatch system.
- **Config is read, not cached as truth.** Tab width, expand-tabs, line-number
  style, scrolloff etc. are pulled from `internal/config`; changing settings at
  runtime must take effect without an editor restart.
- **LSP-free.** `events.go` emits on-change / cursor-move / completion-trigger
  signals through the `Hook` seam, but no diagnostics, highlighting, or completion
  logic lives here (that is Roadmap 0100).
- **Tests ship with code** — every package above has a `_test.go` counterpart;
  motions, operators, text objects and undo are table-driven.

## Milestones

- [ ] Buffer model in `internal/editor/buffer` (line slice + rune/byte position
      mapping, `Insert`/`Delete`/`Replace` over a `Range`), justified as above.
- [ ] Mode state machine: Normal, Insert, Visual (charwise), Visual-Line,
      Visual-Block, Command-Line, Replace — with the pending-operator/count/register
      sub-state.
- [ ] Motions: `h j k l`, `w b e` (+ `W B E`), `0 ^ $`, `gg G`, `{ }`,
      `f t F T` with `; ,`, `%` bracket match.
- [ ] Search: `/` and `?` incremental, `n` / `N`, literal-vs-regex toggle.
- [ ] Operators: `d c y p` (+ `gp`), doubled `dd cc yy`, with charwise/linewise/
      blockwise application.
- [ ] Operator + motion + count composition (e.g. `3dd`, `d2w`, `c$`).
- [ ] Text objects: `iw aw`, pairs `i( a( i" a" i{ a[ i<` … wired into operators
      (`di(`, `ci"`, `daw`).
- [ ] Registers: unnamed `"`, named `"a`-`"z`, yank `"0`, small-delete `"-`, and a
      system-clipboard seam (`"+`).
- [ ] Undo/redo (linear now, tree-ready API) and repeat (`.`). Marks optional.
- [ ] Viewport: vertical/horizontal scrolling, `scrolloff`, cursor management,
      gutter with absolute / relative line numbers driven by `[editor]` config.
- [ ] Save / dirty tracking and ex-commands `:w :q :wq :q! :e`, each registered as
      a registry `Command` via `commands.go`.
- [ ] Editor actions exposed as registry `Command`s (stable IDs) so palette (07)
      and keybindings (08) reach them; verify no parallel dispatch path exists.
- [ ] LSP seam in `events.go`: emit on-change, cursor-move, and completion-trigger
      hooks (no LSP behavior implemented).
- [ ] Tests: table-driven coverage for buffer, motions, operators, text objects,
      registers, undo/redo, search, and viewport scrolling math.
- [ ] Wiki: add/refresh the editor concept doc(s) under `wiki/` (frontmatter with
      `type`, `title`, `description`, `resource` -> `internal/editor`), refresh
      `timestamp`, and add a `log.md` entry.

## Out of scope

- Syntax highlighting, autocomplete, and diagnostics — LSP, **Roadmap 0100**
  (only seams left here).
- Command palette UI — **Roadmap 0070**.
- Default keybinding tables / user keymap config — **Roadmap 0080**.
- File explorer / tree — **Roadmap 0050**.
- Full undo *tree* UI/navigation, macros (`q`/`@`), folding, multiple cursors,
  and large-file backing-store optimization (gap buffer / piece table) — possible
  later roadmaps; the buffer and history APIs are shaped to allow them.
