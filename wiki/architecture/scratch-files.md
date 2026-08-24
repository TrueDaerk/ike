---
type: concept
title: Scratch Files
description: JetBrains-style scratch buffers — language-aware quick files under the user state dir, created from the palette, listed as the explorer's Scratches section with the explorer's own open/rename/delete semantics, surviving restarts as ordinary files.
resource: internal/scratch
tags: [architecture, scratch, palette, languages, explorer]
timestamp: 2026-08-24T12:00:00Z
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
func FirstLine(path string) string // first non-empty line, capped read, "" if empty/blank/unreadable
func Delete(path string) error    // remove one scratch; refuses anything outside the dir
func Rename(path, name string) (string, error) // rename inside the dir; both ends guarded
```

`Dir` mirrors `config.Discover`'s user-layer override, so a sandboxed IKE
keeps its scratches in the sandbox. `Create` is race-free (`O_CREATE|O_EXCL`)
and writes the language's rendered file template (`lang.TemplateFor`, #1223)
through the winning handle, so the content belongs to the allocation that won
the race and a PHP scratch is runnable as created; the extension is
dot-optional, empty means `txt`. A missing directory lists as empty, not as an
error.

The store has a **second producer** since #2056: "Materialize to File"
(`editor.materializeBuffer`) allocates a `scratch-N.<ext>` here for a file-less
buffer that was given a language, and binds the buffer to it, so LSP and every
other path-keyed feature apply. A materialized buffer is a scratch like any
other from then on — listed, renameable and deletable in the Scratches section
— which is exactly why it does not get a temp store of its own; see
[Language Registry](./languages.md#language-tools-from-a-typed-buffer-2056).

`Delete` (#1932) and `Rename` (#1963) are the mutation half, and they are
deliberately narrow: the path must name a file lying **directly** in the
scratch dir, and a rename's new name must be a bare file name — a nested path,
a directory, a traversal through `..`, a pathy name or anything outside is
refused with an error rather than acted on, and a rename never overwrites an
existing target. The explorer section below can therefore pass whatever it has
selected without the app re-deriving the guard rail. `Entries` is `List` with
the mod times kept, so the section's rows need no second stat pass and nothing
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

## Listing (#352, #2057)

`scratch.list` ("Open Scratch File…", palette + File menu) opens the palette
locked to `ScratchMode` (`internal/palette/scratch_mode.go`, prefix `~`, the
recent-files pattern): rows newest-first, fuzzy-filtered by the query, enter
opens through the standard funnel (`OpenFileMsg`). An empty store renders one
inert hint row pointing at "New Scratch File".

The palette owns no store or language knowledge — it is injected a
`[]palette.ScratchEntry{Path, Title, Lang}` per query (`scratchEntries` in
`internal/app/scratch_cmd.go`), built over `scratch.Entries` (newest-first,
mod time). Each row's **title is its first non-empty content line** — read
lazily by `scratch.FirstLine`, capped at 64KiB rather than loading the whole
file, so a large scratch still resolves a title cheaply — trimmed of
whitespace, falling back to the placeholder "Empty scratch" when the file is
empty or blank within the cap. This is the JetBrains scratch-view convention:
a scratch reads by what it contains, not by its allocated `scratch-N.<ext>`
name. The **Detail chip carries the language**, resolved via `lang.ByPath`
and rendered with the same acronym-or-capitalize heuristic as the `scratch.new`
language picker (`langTitle`), falling back to "Plain Text" for an
unregistered extension. Fuzzy matching runs against the title, so typing part
of a scratch's content finds it.

Deleting a scratch from this picker is a deliberate non-goal: the explorer's
Scratches section below already deletes (with the confirm dialog) and renames
scratches through the same store, so the picker stays a pure finder rather
than duplicating that flow.

**Reachable from the `@` file finder too (#1812).** A query that fuzzy-matches
the word "scratch" surfaces the same `scratch.List` rows, newest-first, inline
in the `@` finder below the project matches — no mode switch needed for the
common case of typing "scratch" to find a scratch file. See [Command
Palette](/architecture/command-palette.md) for the file-mode ranking this
slots into.

## The explorer's Scratches section (#1963)

The surface that makes scratches manageable without a terminal: the scratch
dir lies outside the project root, so the [explorer](./explorer.md)'s tree
cannot reach it. #1932 first shipped this as a separate tool pane
(`internal/scratchpanel`) with its own keys; #1963 replaced that pane with a
**section of the explorer itself** (`internal/explorer/scratches.go`), so
there is exactly one interaction model: the scratch list sits behind a
horizontal divider (`▾ Scratches ───`) at the bottom of the explorer pane and
is operated with the explorer's own semantics. See the
[explorer doc](./explorer.md#scratches-section-1963) for the mechanics; the
short version:

- **One unified cursor.** `j`/`k` walk off the last tree row into the section
  and back; `G` lands on the last scratch, wrap-around passes both ends.
- **Open like a project file.** `enter`/double-click go through the standard
  open funnel (`OpenFileMsg`), `o` opens in a split.
- **The explorer's dialogs.** `d` opens the confirm box, `R` the prefilled
  rename prompt, both anchored next to the row (#1884); the accepts run
  `scratch.Delete` / `scratch.Rename` and announce the standard
  `FileDeletedMsg` / `FileMovedMsg`, so open tabs close or re-point exactly
  like a tree delete/rename. Scratch deletes are permanent (the store has no
  trash), which is also why the section has no multi-select delete.
- **`a` delegates to `scratch.new`**: the store owns naming and templates, so
  the explorer's new-file affordance opens the language picker.
- **Scrolls like the tree** (#1965): the cursor pulls the section's window
  along, and the wheel over the section scrolls the section — over the tree it
  scrolls the tree.
- **A right-aligned "last opened" column** (#1965): `ui.ShortAge` over the
  MRU store's last-opened time (the app pushes it in with `SetScratchOpened`),
  falling back to the file's mtime — "now", "5m", "3h", "7d", "6w".
- The divider is **drag-resizable** and a click on it **collapses** the
  section; both persist with the explorer's session state
  (`session.json`, `explorerSession.ScratchCollapsed`/`ScratchHeight`).
- The scratch dir joins the explorer's poll set, so an external change shows
  up like any project-tree change; `r` refreshes it too.

`scratch.panel` ("Scratch Files", palette + View menu) survives as a
**redirect**: it focuses the explorer with the cursor on the section's first
entry (`ScratchSectionFocusMsg`, `internal/app/scratch_section.go`) instead of
opening a second pane.

Three settings (Settings → Tools → *Scratch Files*) shape it:

| key | default | meaning |
| --- | --- | --- |
| `scratch.section` | `true` | render the Scratches section at all; off removes it (scratches stay reachable via `scratch.list`) |
| `scratch.section_height` | `5` | rows the section shows when expanded; it never grows past its content, and a divider drag overrides it for the session; 1–30 |
| `scratch.sort` | `name` | row order: `name` like the tree, or `modified` newest-first |

The legacy #1932 keys still decode and migrate silently in `config.Validate`:
`scratch.panel_height` seeds `scratch.section_height` (minus the old pane's
chrome rows — the old default 8 lands on the new default 5) unless the new key
was set explicitly, `scratch.panel` is dropped, and a leftover
`[tools.layout]` assignment of the removed `scratch` tool id is discarded
without a diagnostic. Persisted layouts containing the old pane prune its leaf
on restore.
