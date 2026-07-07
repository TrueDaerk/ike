---
type: concept
title: Keybindings & Shortcuts
description: The keybinding layer between the registry and config — a chord/key model, JetBrains-like default set, context-scoped resolution with multi-step chords and timeout, build-time conflict detection, platform normalisation, and a cheatsheet view. Binds keys to command ids; defines no commands.
resource: internal/keymap
tags: [architecture, keymap, keybindings, chords, jetbrains, bubbletea]
timestamp: 2026-07-07T00:00:00Z
---

# Keybindings & Shortcuts

Roadmap 0080. `internal/keymap` owns the layer that resolves a **key chord** (in
a focus **context**) to a registered **Command id**. Roadmap 0020 defines the
`Keymap` capability and the registry; Roadmap 0040 owns the `[keymap]` config
section and its precedence. This package sits between them: the binding *model*,
the *default* JetBrains-flavoured set, scope/context resolution, conflict
detection, platform normalisation, and a help/cheatsheet view.

It **defines no Commands.** A binding is `(Chord, Context) → commandID`; the
target ids are owned by the editor (06), explorer (05), palette (07), project
switching (09), and a future VCS roadmap. If a command id is not registered the
binding is **inert** — it still appears in the cheatsheet, but pressing it falls
through to the focused pane.

## The binding model

- **`Key`** (`key.go`) — a base key (`a`, `f7`, `esc`, `left-bracket`, `/`) plus
  a `Mod` bitset (`Meta`/`Ctrl`/`Alt`/`Shift`). Authors write logical modifiers;
  `Meta` (Cmd) is folded to a concrete modifier at build time.
- **`Chord`** (`chord.go`) — an ordered list of `Key` steps. One type models all
  three shapes: single (`esc`), modified (`cmd+t`), multi-step (`cmd+k cmd+c`).
- **`parse.go`** — `ParseChord`/`ParseKey` accept whitespace-separated steps with
  `+`-joined modifier tokens; `String()` renders the canonical form (modifiers in
  fixed order meta, ctrl, alt, shift), so parse→format→parse is idempotent. A bare
  uppercase letter folds to base+`Shift`; an underscore base is rejected (so the
  `focus_*` config stopgap sharing the bindings map is treated as a non-chord).
- **`Binding`** (`binding.go`) — `Chord`, `Command`, `Context`, presentation
  metadata (`Title`, `Owner`), a `Fragile` flag, and a `Layer` (default < user <
  project).

## Context & precedence

`Context` (`context.go`) values equal the context ids panes advertise (`editor`,
`explorer`, `palette`); the zero value `Global` matches everywhere. A chord
resolves against the **active** focus context, preferring the most specific match:
a pane-scoped binding shadows a `Global` one for the same chord while that pane is
focused.

## Platform normalisation

`platform.go` folds the logical `Meta` modifier once at table-build time: on macOS
the terminal can forward Cmd as Meta, so `Meta` is kept; everywhere else `Cmd → Ctrl`.
Normalisation is idempotent, so the resolver only ever compares concrete keys.

## Table & conflicts

`BuildTable(defaults, overrides, goos)` (`table.go`) starts from the normalised
default set and overlays the merged `[keymap.bindings]` map (chord string →
command id; `""` unbinds). Overrides arrive already merged by config precedence
(04), so each non-empty entry replaces matching default chords (a brand-new chord
becomes a `Global` user binding). `conflict.go` then detects same chord+context
clashes: it keeps the highest-`Layer` binding and surfaces the rest as non-fatal
diagnostics — never a silent shadow. Unparseable override keys are skipped as
diagnostics too.

## Resolver

`Resolver` (`resolver.go`) feeds keys against the table, tracking partial
multi-step state. Each `Feed(key, context)` returns:

- **Pending** — the sequence is a prefix of a longer chord; the caller arms a
  `TimeoutDuration` (600ms) timer. A prefix wins over an equal-length exact match
  (so `cmd+k cmd+c` stays reachable); the exact `cmd+k` is recovered on `Timeout`.
- **Resolved** — a binding matched; `Command` carries the id.
- **NoMatch** — nothing; the caller lets the key fall through. An aborted prefix
  restarts the sequence from the new key rather than stranding it.

`fromkeymsg.go` adapts a Bubble Tea v2 `tea.KeyPressMsg` into a `Key`. It reads the
press purely through `String()` — v2 still encodes modifiers as `ctrl+/alt+/shift+`
tokens and names specials (`esc`, `space`, `f7`, `left`, …) — so the same code that
parses authored chords (`ParseKey`) parses live keys. (See
former Roadmap 0085, spec in git history, for the v1→v2 key-model change:
`key.Type`/`key.Runes` became `key.Code`/`key.Text`/`key.Mod`.)

## Root-model integration

`internal/app` builds the resolver from config (`buildKeymap`) and, in its
`tea.KeyPressMsg` path, attempts resolution before pane dispatch (`resolveKeymap`):

- In a text-capturing editor (insert mode) only **modified** chords — or a chord
  already in progress — are eligible; plain letters always reach the editor.
- A **Resolved** id that names a registered command runs it via `host.API`; an
  inert id falls through. **Pending** swallows the key and schedules a
  `keymapTimeoutMsg`; on timeout the held chord resolves as an exact binding or is
  discarded.

## Terminal limits & fallback

Many modifier combos (`Cmd+T`, `Ctrl+Tab`, `Cmd+1`) are intercepted by the
terminal/OS and never reach the program; such bindings carry a `Fragile` flag so
the cheatsheet can show the palette/leader fallback. Every bound action stays
reachable from the command palette (07), the universal escape hatch.

## Default set

The JetBrains-flavoured defaults live in `defaults.go` as data (chord, command id,
title, context, owner, fragile). `help.go` groups the effective bindings by
context (Global first) for the `palette.keymapHelp` cheatsheet.

Actions whose JetBrains chord uses `Cmd` — undeliverable in macOS terminals —
additionally get an everywhere-deliverable `Ctrl` chord: undo (`ctrl+z`), redo
(`ctrl+shift+z`), and save (`ctrl+s`, alongside `cmd+s`; raw mode disables XOFF
flow control, so `ctrl+s` arrives as a normal key). Save targets `editor.write`,
the command the editor registers for `:w`, and works from insert mode because
modified chords stay eligible for the keymap layer.

## Boundaries

Vim-internal keymaps inside the editor (normal/insert/visual motions, operators,
text objects) belong to Roadmap 0060 and are **not** in this table — this package
owns only global / IDE-level shortcuts. `vcs.commit`, `vcs.updateProject`,
`vcs.revertFile` are bound to placeholder ids and stay inert until a future VCS
roadmap registers them.
