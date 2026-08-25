---
type: concept
title: Scratch Files
description: JetBrains-style scratch buffers — language-aware quick files under the user state dir, created from the palette or generated as synthetic test data in nine formats, listed as the explorer's Scratches section with the explorer's own open/rename/delete semantics, surviving restarts as ordinary files.
resource: internal/scratch
tags: [architecture, scratch, palette, languages, explorer, testdata]
timestamp: 2026-08-25T12:00:00Z
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
func CreateWithContent(ext string, content []byte) (string, error) // …seeded with content instead
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

`CreateWithContent` (#2134) is the same allocation with a different seed: the
caller's bytes instead of the language template, still written through the
handle that won the `O_EXCL` race. It exists so the [test-data
generator](#test-data-generator-2134) has no path assembly and no naming logic
of its own — a generated CSV is a scratch like every other. A nil content is
`Create`; an empty (non-nil) one means an empty file, not "fall back to the
template".

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

## Test-data generator (#2134)

Exercising the CSV table, the [data viewer](./data-viewer.md), the
[log timeline](./log-timeline.md) or the
[jq/yq playgrounds](./jq-playground.md) used to need a hand-made sample file —
in practice, asking an external agent for "a 2000-row CSV" and pasting it in.
`internal/testdata` generates one instead, and drops it into the scratch store.

**The spec is the whole model.** A generation is a list of named fields, each
with a catalog *kind* and an optional parameter, plus a row count, a seed and a
table name:

```go
type Field struct{ Name string; Kind Kind; Param string }
type Spec  struct{ Format Format; Rows int; Seed uint64; Table string; Fields []Field }
```

`Spec.Validate` is the single definition of "valid" — the wizard, the quick
commands and `Write` all run it, so a hand-edited preset cannot slip past the
form's checks. It refuses a row count below 1 or above `MaxRows` (1 000 000),
an empty field list, an empty or duplicate field name, an unknown kind, and a
parameter that does not match its kind's grammar (or is given to a kind that
takes none). The cap is a **constant, not a setting**: it exists to catch a
typo in the row field, not to be tuned.

**Values ride [gofakeit](https://github.com/brianvoe/gofakeit) v7**, always
through an *instance* faker seeded from the spec — never the package global —
which is what makes "same seed + same spec → byte-identical output" hold even
when two generations interleave. For the same reason nothing in the catalog
reads the wall clock: the `date` kind defaults to a fixed 2000–2030 window and
generated log timestamps start at a fixed epoch. **Seed 0 means "draw a fresh
random seed"**; any other seed repeats.

The catalog (`testdata.Catalog()`, the wizard's reference and the docs' table):
`id` (the 1-based row number, not random), `uuid`, `first_name`, `last_name`,
`full_name`, `email`, `url`, `hostname`, `domain`, `ipv4`, `ipv6`, `mac`,
`phone`, `street`, `city`, `country`, `company`, `job_title`, `sentence`,
`paragraph`, `int`, `float`, `bool`, `date`, `hex_color`, `user_agent`. Four
take a parameter: `email`/`url`/`hostname` a **domain**, which constrains every
generated value to it (`https://example.com/…`, `srv12.example.com`),
`int`/`float` a `min..max` range and `date` a `from..to` range (`YYYY-MM-DD` or
RFC3339).

**Writers stream.** Each format is a `begin` / one call per row / `end` encoder
over a 64 KiB buffer, so a million-row CSV never materializes as a million rows
in memory. Values stay typed (`string`, `int64`, `float64`, `bool`,
`time.Time`) all the way into the writer, so each format renders them in its
own idiom rather than re-parsing pre-formatted text:

| format | ext | shape |
| --- | --- | --- |
| CSV / TSV | `.csv` `.tsv` | header row, `encoding/csv` quoting |
| JSON | `.json` | indented array of objects (folds and jq nicely) |
| NDJSON | `.ndjson` | one compact object per line |
| XML | `.xml` | `<Table>` root, one `<row>` per row, names sanitized to legal XML names |
| YAML | `.yaml` | list of maps; strings always double-quoted (JSON escape rules) |
| TOML | `.toml` | `[[Table]]` array of tables, dates as native datetimes |
| SQL | `.sql` | one `INSERT INTO "Table" (…) VALUES (…);` per row |
| Log | `.log` | logfmt `ts=… level=… msg=…` plus the spec's fields as extra pairs |

Escaping is hand-rolled per grammar (a document encoder would want the whole
file in memory first), which is why every writer is **round-tripped in tests
through the real parser** — `encoding/csv`, `encoding/json`, `encoding/xml`,
`yaml.v3`, `BurntSushi/toml`, and for SQL an actual in-memory SQLite that
executes the generated statements — against a spec whose field names carry a
space, a double quote and a leading digit.

The log writer draws its `ts`/`level`/`msg` from the same faker as the row
values. Levels follow a service-log distribution (≈55 % info, 20 % debug, 15 %
warn, 8 % error, 2 % fatal) and timestamps only ever move forward, so the
generated file reads as a stream.

### Commands and the wizard

- **`scratch.generate` ("Generate Test Data…", palette + File menu)** opens a
  four-step shell dialog (`internal/app/scratch_generate.go`), modelled on the
  [new-project wizard](./project-switching.md) rather than a settings form, so
  it needs no page host: **format** (↑↓, enter) → **rows / seed / table** (tab
  between fields) → **field list** (`a` add, `e` edit, `d` delete, enter
  generates) → **field editor** (name / kind / param; ↑↓ on the Kind row cycle
  the catalog, so 26 kinds need no typing). Every accept validates through
  `Spec.Validate`, and a failure keeps the dialog open with the reason on its
  error line. `esc` walks the steps backwards and closes on the first.
- **`scratch.generate.<format>`** — one per format, mirroring
  `scratch.new.<lang>`: no prompt at all, generated straight from that format's
  stored preset.

Generation runs as a `tea.Cmd`, never on the update loop — `MaxRows` worth of
faker calls has no business blocking the UI. The finished document is written
with `scratch.CreateWithContent`, the explorer's Scratches section is refreshed
and the file opens through the standard funnel, so it lands as a focused tab
with the right language.

### Presets (`~/.ike/testdata.json`)

The spec that produced a file is remembered **per format**, in a user-level
store following the same `IKE_CONFIG_DIR` seam as the layout store
(`internal/app/layouts.go`) — a test-data shape is a habit, not a property of a
project. Picking a format in the wizard loads that format's preset; a format
with none starts from the stock default (100 rows; `id`, `first_name`,
`last_name`, `email`). The store is validated **on read**: a hand-edited or
stale entry falls back to the default instead of putting an unusable spec into
the dialog, and a failed generation never overwrites a working preset.

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
