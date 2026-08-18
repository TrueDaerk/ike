---
type: concept
title: Scratch Files
description: JetBrains-style scratch buffers — language-aware quick files under the user state dir, created from the palette, listed and deleted in the Scratch Files tool window, surviving restarts as ordinary files.
resource: internal/scratch
tags: [architecture, scratch, palette, languages, tool-window, pane]
timestamp: 2026-08-18T14:00:00Z
---

# Scratch Files

Roadmap 0280 (from idea #169). Quick throwaway buffers for notes, JSON
snippets, regex tests. The design premise: **scratches are ordinary files**
under the user state dir — no special buffer type, no new persistence.
Everything language-aware (highlighting, LSP, comment toggling, smart indent)
flows from the file extension through the [language
registry](./languages.md); open scratches restore with the session like any
other absolute-path tab.

## Store (`internal/scratch`)

The single owner of scratch naming and location — the app never assembles
scratch paths itself:

```go
func Dir() (string, error)        // $IKE_CONFIG_DIR/scratches, else ~/.ike/scratches
func Create(ext string) (string, error) // next free scratch-N.<ext>, seeded with the language template
func List() ([]string, error)     // existing scratches, newest-first (mod time)
func Entries() ([]Entry, error)   // the same order, with each file's mod time
func Delete(path string) error    // remove one scratch; refuses anything outside the dir
```

`Dir` mirrors `config.Discover`'s user-layer override, so a sandboxed IKE
keeps its scratches in the sandbox. `Create` is race-free (`O_CREATE|O_EXCL`)
and writes the language's rendered file template (`lang.TemplateFor`, #1223)
through the winning handle, so the content belongs to the allocation that won
the race and a PHP scratch is runnable as created; the extension is
dot-optional, empty means `txt`. A missing directory lists as empty, not as an
error.

`Delete` (#1932) is the removal half, and it is deliberately narrow: the path
must name a file lying **directly** in the scratch dir. A nested path, a
directory, a traversal through `..` or anything outside is refused with an
error rather than deleted — the panel below can therefore pass whatever it has
selected without the app re-deriving the guard rail. `Entries` is `List` with
the mod times kept, so the panel's rows need no second stat pass and nothing
outside this package reads the scratch directory.

## Creating (#351, #1223)

Command family, rebuilt on every registry query (`Capabilities` is lazy) so
late-registered languages appear without ordering constraints:

- `scratch.new` ("New Scratch File…", `cmd+shift+n`, File menu) opens the
  **language picker** — `scratchNewMode` in `internal/app/scratch_new_mode.go`,
  prefix `+`, opened locked exactly like `scratch.list`. Rows: "Plain Text"
  pinned first, then one per registered language that has an extension, fuzzy
  matched on the title with the extension in the detail column. A row emits
  `palette.RunCommandMsg` for the matching command below, so the picker owns no
  creation logic.
- `scratch.new.text` creates a plain `.txt` scratch with no prompt (what
  `scratch.new` used to do — a chord or script still needs the direct path).
- `scratch.new.<id>` ("New Scratch File: Python") per registered language,
  built from `lang.All()` with the language's first extension, no prompt.

The handler creates via the store and opens through the standard funnel
(`openPath`, absolute path): the new scratch lands as a focused tab with
highlighting/LSP live.

## Running a scratch (#1223)

`run.file` needs no scratch-specific code. A scratch lies outside the project
tree, so `run.Default` stores its path absolute (`relTo` keeps only paths under
the root relative) with an empty `Cwd` — which resolves to the **project
root**. The argv is synthesized through the language's `RunCommandProvider`
with the project's resolved interpreter, so a Python scratch runs under the
project's virtualenv and a PHP scratch under its configured `php`. See
[run configurations](./run-configurations.md).

## Listing (#352)

`scratch.list` ("Open Scratch File…", palette + File menu) opens the palette
locked to `ScratchMode` (`internal/palette/scratch_mode.go`, prefix `~`, the
recent-files pattern): file names newest-first from the injected
`scratch.List`, fuzzy-filtered by the query, enter opens through the standard
funnel (`OpenFileMsg`). An empty store renders one inert hint row pointing at
"New Scratch File".

**Reachable from the `@` file finder too (#1812).** A query that fuzzy-matches
the word "scratch" surfaces the same `scratch.List` rows, newest-first, inline
in the `@` finder below the project matches — no mode switch needed for the
common case of typing "scratch" to find a scratch file. See [Command
Palette](/architecture/command-palette.md) for the file-mode ranking this
slots into.

## Scratch Files tool window (#1932)

The panel that makes scratches manageable without a terminal: the scratch dir
lies outside the project root, so the [explorer](./explorer.md) cannot reach
it — before this, an old scratch could only be deleted by leaving the IDE.

`scratch.panel` ("Scratch Files", palette + View menu) toggles a slim
singleton pane (`internal/scratchpanel`, `pane.KindScratch`, key `"scratch"`,
context id `"scratch"`) wired by `internal/app/scratch_panel.go` with the
`vcs.panel` state machine (open → focus → return focus). Unlike the adaptive
tool windows (#1588) it always docks **below** the active editor: the list is
a wide few-rows strip, never a column.

Rows are the store's files newest-first — name, language (the [language
registry](./languages.md)'s id, falling back to the bare extension) and
relative mod time. Navigation is the shared
[list semantics](./list-navigation.md); `enter` and a double click open the
scratch through the standard open funnel — the same focused tab `scratch.list`
produces — and `r` re-reads the store. The list is also refreshed by the app
whenever a scratch is created or deleted, so it never needs a restart.

**Delete asks first.** `d` (or `delete`/`backspace`) arms a confirmation the
footer spells out (`Delete scratch-3.py? [y]es [n]o`); `y`/`enter` performs
it, *any* other key — and losing focus, and a click — cancels. The delete goes
out as `scratchpanel.DeleteMsg`, so the app can do what the panel must not:
`scratch.Delete` removes the file, `closeEditorsForPath` closes its tabs
across every pane (the explorer's delete path), the
[Problems](./problems.md) store drops its findings, and the list refreshes. A
path the store refuses only warns — nothing is closed.

**Resizing is not special.** The panel is an ordinary leaf of the
[split tree](./pane-layout.md), so the line above it is the usual draggable
pane edge and the dragged height persists in `layout.json` like any other
split ratio.

Two settings (Settings → Tools → *Scratch Files*) shape it:

| key | default | meaning |
| --- | --- | --- |
| `scratch.panel` | `false` | open the panel on start; hidden by default, so users who don't want it lose no editor rows |
| `scratch.panel_height` | `8` | rows the panel opens at, border and title row included (8 ≈ four scratches plus the hint line); 5–60 |

`scratch.panel` is a one-shot at startup, not a live mirror of the pane:
toggling the panel away afterwards sticks for the session, and a panel left
open at quit comes back through the layout either way.
