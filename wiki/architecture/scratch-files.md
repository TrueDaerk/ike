---
type: concept
title: Scratch Files
description: JetBrains-style scratch buffers — language-aware quick files under the user state dir, created from the palette (curated language rows plus a free "Custom…" extension), from the active selection or generated as synthetic test data in nine formats, managed from a floating manager and the explorer's Scratches section (open, rename, delete, change language, promote to a project file), surviving restarts as ordinary files.
resource: internal/scratch
tags: [architecture, scratch, palette, languages, explorer, testdata]
timestamp: 2026-08-31T18:00:00Z
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
func SetExt(path, ext string) (string, error)  // keep the stem, swap the extension (the language change)
func IsScratch(path string) bool  // does this path lie directly in the store?
func Promote(path, target string) error // move one scratch out of the store to target
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
the mod times **and byte sizes** kept, so neither the section's rows nor the
manager's metadata columns need a second stat pass and nothing outside this
package reads the scratch directory.

`SetExt` (#2256) is the language change expressed in the only terms the store
knows: everything language-aware flows from the extension, so re-languaging a
scratch *is* renaming `scratch-1.txt` to `scratch-1.py`. It keeps the stem
(a name that is all extension, `.env`, counts as the stem) and runs `Rename`,
inheriting its guards — an existing target is refused, never overwritten.

`Promote` (#2339) is the store's **exit**, and the only operation whose target
lies outside it. The source is guarded like `Delete`'s (a file directly in the
store); the target must lie *outside* the store, because moving a scratch to
another scratch name is a `Rename`, not a promotion; an existing target is
refused rather than overwritten, and missing parent directories are created.
The move is an `os.Rename` when the filesystem allows it and a copy-flush-unlink
otherwise — the store lives under the user state dir and the project tree may
sit on a different device — with the source removed **only after** the copy is
durably written, so a failed write can never leave a store entry gone with no
file to show for it. `IsScratch` is the location predicate the promote command
gates on before it does anything.

## Creating (#351, #1223)

Command family, rebuilt on every registry query (`Capabilities` is lazy) so
late-registered languages appear without ordering constraints:

- `scratch.new` ("New Scratch File…", `cmd+shift+n`, File menu) opens the
  **language picker** — `scratchNewMode` in `internal/app/scratch_new_mode.go`,
  prefix `+`, opened locked exactly like `scratch.list`. Rows: "Plain Text"
  pinned first, then the shared offering below, fuzzy matched on the title —
  and, when the title does not match, on the extension, so `.js` finds
  JavaScript — with the extension in the detail column. A row emits
  `palette.RunCommandMsg` for the matching command below, so the picker owns no
  creation logic. The last row, "Custom…" (#2340), is the exception: it opens a
  prompt for any extension the offering does not list.
- `scratch.new.text` creates a plain `.txt` scratch with no prompt (what
  `scratch.new` used to do — a chord or script still needs the direct path).
- `scratch.new.<id>` ("New Scratch File: Python") per offered row, no prompt;
  a dialect row (below) gets `scratch.new.<id>.<ext>`.

The handler creates via the store and opens through the standard funnel
(`openPath`, absolute path): the new scratch lands as a focused tab with
highlighting/LSP live. `NewScratchMsg` carries an optional **content** since
#2339; an empty one keeps the language-template seeding above.

### From the active selection (#2339)

`scratch.newFromSelection` ("New Scratch from Selection",
`cmd+alt+shift+s`, palette + File menu) skips both prompts of the flow above:
the content is the **active selection** and the extension is **inherited from
the file it came from**, because a selection already decides its own language.
The four-step "copy, create, pick a language, paste" is the case this exists
for.

- The selection is `activeSelectionText` (`internal/app/selection.go`) — the
  same seam Find in Path prefills from, so a selection in a diff pane or a
  terminal works too; `internal/app/scratch_from_selection.go` walks those very
  panes in the same order for the *extension*, so the suffix always belongs to
  the buffer the text came from.
- **No whitelist.** The store takes any extension, so a selection out of a
  `.tf`, a `.bazel` or an in-house suffix with no picker row still produces a
  scratch of that suffix — the only answer that keeps the scratch classified
  like its source. A source with no extension at all (an untyped untitled
  buffer, a `Dockerfile`) falls back to `txt`; a typed file-less buffer (#2033)
  lends its synthetic language name's extension.
- **No selection is a refusal, not an empty file**: the command says "scratch:
  select some text first" rather than creating a scratch that is
  indistinguishable from a lost selection.

### The offering: a row is not a language (#2333)

Every scratch surface — the commands above, the `scratch.new` picker and the
manager's language step — reads one shared list,
`scratchLangRows()` in `internal/app/scratch_langs.go`: `(title, extension,
command)` triples, sorted by title, built per call from `lang.All()` so
late-registered languages appear without ordering constraints.

Each language contributes its **first extension** under `langTitle`'s
acronym-or-capitalize title, plus the **curated dialect aliases** of
`scratchAliasExts`, keyed by language id:

| language | alias rows |
| --- | --- |
| `typescript` | JavaScript `.js`, JSX `.jsx`, TSX `.tsx` |
| `css` | SCSS `.scss`, Less `.less` |

Before the table, a surface offered only `Extensions[0]`, so a dialect sharing
a language id — JavaScript, which the `typescript` language selects through its
`.js`/`.jsx`/`.mjs` extensions — had no row at all: no `.js` scratch could be
created and none switched to one. The table is deliberately curated rather than
"every extension of every language": rows nobody looks for (`Typescript .mts`,
`Html .htm`) would bloat the picker. An alias the language does not actually
register is ignored, and an extension already claimed by an earlier row is
skipped, so the list can neither invent an extension nor list one twice.

### The free extension: "Custom…" (#2340)

The curated table above has a price: every extension it does not list used to
be a code change (#2333 was exactly that for `.js`, and `.mjs`, `.sql`, `.tf`
or an in-house suffix would each be the next one). The **"Custom…" row** closes
the list without growing it — it is offered by *both* language surfaces and
leads to a one-line prompt for the extension
(`internal/app/scratch_custom_ext.go`):

- In the `scratch.new` picker it is the row that carries **no**
  `scratch.new.<id>` command — it is not a creator but a doorway, so it emits
  `ShowScratchCustomExtMsg` and the root model opens the prompt. `enter`
  creates the scratch through the very same `newScratch` funnel a language row
  uses (typing `py` is therefore not a special case but the "Python" row by
  another route), `esc` returns to the language list rather than to the editor.
- In the manager it is a **step** of the dialog (`smStepCustomExt`), modelled
  on the rename step: the field is seeded with the scratch's current
  extension, `enter` re-languages through `scratch.SetExt` like every language
  row, and `esc` walks one step back into the row list — the manager's standing
  esc semantics.

**No filter ever hides the row.** It is needed precisely when the query matches
nothing else, so the picker keeps it on a score floor below every real match
(it sorts last as long as anything else is left) and the manager's
`filteredLangs` narrows the languages and appends it unconditionally.

The typed extension is **validated, never silently corrected**
(`normalizeScratchExt`): the leading dot is optional (`tf` and `.tf` both mean
`tf`), a dot *inside* is allowed (`d.ts`), and an empty input, a path separator,
whitespace, a leading/trailing dot or any other character an extension is not
made of is refused with a message that names the reason while the prompt stays
open. The case is kept as typed; beyond these rules the store decides.

Typed extensions are **not remembered**. A remembered extension would have to
become a row of its own to be worth anything, which is the list growth the
curated table exists to prevent — and re-typing `tf` costs two keys next to a
row that would then sit in the list forever.

Nothing about language *registration* changes — resolution stays path-keyed
through the [language registry](./languages.md), so a `.js` scratch (and every
`.js` file) highlights, indents and attaches to LSP exactly as before. A path's
row title is `scratchRowTitle`: the alias title where one exists
("JavaScript" for `.js`), the language title otherwise, "Plain Text" for an
unregistered extension.

## Test-data generator (#2134)

Exercising the CSV table, the [data viewer](./data-viewer.md), the
[log timeline](./log-timeline.md) or the
[jq/yq playgrounds](./jq-playground.md) used to need a hand-made sample file —
in practice, asking an external agent for "a 2000-row CSV" and pasting it in.
`internal/testdata` generates one instead, and drops it into the scratch store.

**The spec is the whole model.** A generation is a DSL text defining the
fields, plus a row count, a seed, a table name and the render format:

```go
type Spec struct{ Format Format; Rows int; Seed uint64; Table string; DSL string }
```

**The DSL (#2392)** is one field per line, `name = expression`; blank lines
and `#` comments are skipped. An expression is one of three things:

- a **generator call** from the catalog — `first_name()`, `int(1..1000)`,
  `from_list(red, green, blue)` — with the same parameter grammars the
  pre-#2392 wizard used;
- a **template string** — a quoted literal whose `{field}` placeholders
  interpolate other fields of the same row
  (`url = "https://{host}/api/{id}"`; escapes `\" \\ \{ \} \n \t`);
- **`weighted(...)`** — alternatives over arbitrary sub-expressions with
  positive weights, normalized rather than required to sum to 100
  (`weighted(70: email({domain}), 30: "")`).

`{field}` references also work inside generator arguments
(`host = hostname({domain})`). `ParseDSL` (`internal/testdata/dsl.go`)
resolves the references into a **dependency order** — a stable topological
sort, so definition order rules where no dependency does — and rejects an
unknown reference or a cycle, naming the cycle path (`a → b → a`). Output
columns always follow definition order; only evaluation is reordered. Every
parse error is a `*ParseError` carrying its 1-based line, which is what the
dialog's inline error shows. A generator argument that interpolates a
reference can only be validated **per row**, when its text exists — a `{ref}`
that turns out not to be a domain name fails the generation with the field
and line named.

`Spec.Validate` is the single definition of "valid" — the dialog, the live
preview and `Write` all run it, so a hand-edited store cannot slip past the
form's checks. It refuses a row count below 1 or above `MaxRows` (1 000 000),
an unknown format, and any DSL parse error. The cap is a **constant, not a
setting**: it exists to catch a typo in the row field, not to be tuned.

**Weighted draws stay seeded.** A `weighted(...)` makes exactly one draw from
the spec's instance faker and then evaluates only the winning branch, so a
seeded run is byte-identical regardless of which branches win, and the other
branches consume no draws.

**Values ride [gofakeit](https://github.com/brianvoe/gofakeit) v7**, always
through an *instance* faker seeded from the spec — never the package global —
which is what makes "same seed + same spec → byte-identical output" hold even
when two generations interleave. For the same reason nothing in the catalog
reads the wall clock: the `date` kind defaults to a fixed 2000–2030 window and
generated log timestamps start at a fixed epoch. **Seed 0 means "draw a fresh
random seed"**; any other seed repeats.

The catalog (`testdata.Catalog()`, the dialog's autocomplete and the docs'
table): `id` (the 1-based row number, not random), `uuid`, `first_name`,
`last_name`, `full_name`, `email`, `url`, `hostname`, `domain`, `ipv4`,
`ipv6`, `mac`, `phone`, `street`, `city`, `country`, `company`, `job_title`,
`sentence`, `paragraph`, `int`, `float`, `bool`, `date`, `hex_color`,
`user_agent`, `from_list`. The parameterized kinds: `email`/`url`/`hostname`
take a **domain**, which constrains every generated value to it
(`https://example.com/…`, `srv12.example.com`), `int`/`float` a `min..max`
range, `date` a `from..to` range (`YYYY-MM-DD` or RFC3339), and `from_list`
(#2228) **comma-separated entries** the value is drawn from at random
(`red, green, blue`; entries are trimmed, empties dropped, a literal comma in
an entry is out of scope). `from_list` is the one kind whose parameter is
**required** — every other kind has a default, but a list to pick from cannot
be invented. `weighted` is not a catalog kind — it wraps arbitrary
sub-expressions — but is reserved next to the catalog (`WeightedName`,
`WeightedInfo`) so the autocomplete offers it the same way.

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

### The dialog

**`scratch.generate` ("Generate Test Data…", palette + File menu,
`cmd+alt+shift+n`)** is the one entry point — the pre-#2392 per-format quick
commands (`scratch.generate.<format>`) are gone, along with their audit-ledger
family entry; the palette keeps a single row and the format is picked inside
the dialog. It opens a **single-screen** modal shell dialog
(`internal/app/scratch_generate.go`), in the shell-dialog family of the
[new-project wizard](./project-switching.md) and the scratch manager, so it
needs no page host:

- a **header** of five knobs — the *template* picker, the *format* picker
  (←/→ cycle, a click on the focused picker cycles too), and the *rows* /
  *seed* / *table* text fields — walked with tab/shift-tab;
- the **DSL spec editor**, a multi-line text area with a gutter of line
  numbers (matching the parse errors), full cursor movement, multi-line paste,
  and **autocomplete**: typing after a line's `=` offers the catalog (name,
  parameter grammar and description, `weighted` included), typing after `{`
  offers the fields defined above the cursor; enter/tab accepts, `ctrl+space`
  asks explicitly, esc dismisses the popup before it closes the dialog;
- a **live preview** of the first `PreviewRows` (5) rows in the selected
  format, re-rendered debounced (`tdPreviewDebounce`, generation-stamped so a
  stale tick is dropped) as the spec or any header knob changes —
  `testdata.Preview` runs the very same parser, evaluator and writers as the
  generation, so the preview is byte-for-byte the head of the file a seeded
  run writes. An invalid spec shows its line-numbered error in the preview's
  place, and `ctrl+g` / the `[generate]` button refuse until it parses;
- a **button row** — `[cancel]`, `[save template]`, `[delete template]`
  (only over a deletable user template), `[generate]`.

The dialog is **fully mouse-operable**: every rendered line records a hit
region while it renders — header rows focus (and cycle, dropdown-style, when
already focused), editor lines place the cursor where clicked, suggestion rows
accept, buttons act. The wheel scrolls the shell's own viewport; there are no
windowed lists left to steal it.

Generation runs as a `tea.Cmd`, never on the update loop — `MaxRows` worth of
faker calls has no business blocking the UI — and so does the preview render.
The finished document is written with `scratch.CreateWithContent`, the
explorer's Scratches section is refreshed and the file opens through the
standard funnel, so it lands as a focused tab with the right language.

### Templates and the store (`~/.ike/testdata.json`)

**Templates are format-free named specs** — a DSL body only, no
format/rows/seed; the format is chosen in the dialog at generation time.
Built-ins ship in `internal/testdata/templates.go` — *Person*, *Address*,
*Order*, *URL / Web* (the issue's domain/host/url example), *Server log* —
deliberately doubling as working examples of references, template strings and
weighted alternatives; a test parses and renders every one. Picking a template
loads its body into the editor; edits never write back unless saved again.
`ctrl+s` (or `[save template]`) prompts for a name and saves the current spec
via `testdata.SaveTemplate`, which refuses an empty name, a name shadowing a
built-in, and a body that does not parse. `ctrl+d` / `[delete template]`
removes a user template; built-ins refuse.

The store is **user-level**, following the same `IKE_CONFIG_DIR` seam as the
layout store (`internal/app/layouts.go`) — a test-data shape is a habit, not a
property of a project. It holds the **last used spec** (the next dialog starts
where the previous generation left off; validated on read, so a stale or
hand-edited entry falls back to the stock default of 100 rows and
`id`/`first_name`/`last_name`/`email`) and the **user templates** by name. A
failed generation never overwrites the last spec. The pre-#2392 per-format
preset schema is simply unreadable under the new one and reads as an empty
store — those presets were low-value and start over.

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
name. The **Detail chip carries the language**, resolved from the path by
`scratchRowTitle` — the same row title the `scratch.new` picker shows, so a
`.js` scratch reads "JavaScript" — falling back to "Plain Text" for an
unregistered extension. Fuzzy matching runs against the title, so typing part
of a scratch's content finds it.

Deleting a scratch from this picker is a deliberate non-goal: the explorer's
Scratches section below already deletes (with the confirm dialog) and renames
scratches through the same store, so the picker stays a pure finder rather
than duplicating that flow.

**Reachable from the `@` file finder too (#1812, #2341).** The `@` finder
surfaces the same `scratch.List` rows inline, below the project matches, so no
mode switch is needed: a query fuzzy-matching the word "scratch" lists the
whole store newest-first, and **any other query is matched against each
scratch's own file name** (#2341) — a scratch named `notes.go` is found by
typing `@notes`. Scratch rows never displace a project hit and are
de-duplicated against the filesystem fallback. See [Command
Palette](/architecture/command-palette.md) for the file-mode ranking this
slots into.

## The manager (#2256)

`scratch.manage` ("Manage Scratch Files…", palette + File menu) opens the
**scratch manager** — the surface that manages the store from anywhere, since
the explorer's Scratches section needs the explorer pane and `scratch.list` is
a deliberate pure finder. It is a floating shell dialog
(`internal/app/scratch_manager.go`) in the shape of the [test-data
dialog](#the-dialog): steps walked by enter/esc, type-ahead
narrowing through `ui.SpeedSearch`, clickable rows and buttons, wheel-scrolled
lists.

The list is the store newest-first with the metadata a decision needs — **name,
language, size, last-modified** — and the type-ahead matches name *and*
language, so `py` finds the Python scratches. `enter` opens the selection
through the standard funnel (`openPath`), which is also what a second click on
a row does.

The actions are chords, not letters, because a letter belongs to the
type-ahead:

| key | action |
| --- | --- |
| `enter` | open the scratch (closes the manager) |
| `ctrl+r` / `f2` | rename — the prompt is prefilled with the current name |
| `ctrl+l` | change language — a filterable list of the offered rows, preselected on the scratch's current extension, ending in "Custom…" for a free extension (#2340) |
| `ctrl+p` | promote to a project file (closes the manager, #2339) |
| `ctrl+d` / `delete` | delete, after a confirmation |
| `esc` | clear the query, walk a step back, then close |

Every mutation runs through the store, so the dialog owns no validation of its
own: a rename collision, a pathy name and a `..` traversal come back as the
store's error and keep the prompt open with it on the error line. A delete
always confirms first — scratches have no trash.

**Open buffers follow.** Nothing here touches an editor: a rename (and a
language change, which is a rename of the extension) emits
`explorer.FileMovedMsg` and a delete `explorer.FileDeletedMsg` — the very
messages the explorer's file ops announce — so a scratch open in a tab
re-points through the one existing path (#175, `followMovedFile`), its tab
title follows the new name and `editor.SetPath` resets the language state, and
a deleted scratch's tab closes. After each mutation the list reloads and the
cursor stays on the file that was acted on, and the explorer's Scratches
section is refreshed.

`ctrl+p` is the one action that **leaves** the manager rather than adding a
step to it: the scratch is on its way out of the store, so the manager closes
and hands its marked row to `scratch.promote`'s own prompt below.

**Reachable from the creation flow**: the `scratch.new` language picker carries
an "Open existing scratch…" row (sorted last, so it never displaces a
language) that runs `scratch.manage` — "new scratch" is where one notices the
wanted scratch already exists.

## Promoting a scratch to a project file (#2339)

The counterpart of creation: a scratch that turned into something worth keeping
gets a real path. Before, the manager could rename, delete and re-language a
scratch but never let it leave, so the last step of "quick experiment → actual
code" was a manual copy through the file system.

`scratch.promote` ("Promote Scratch to File…", `cmd+alt+shift+p`, palette +
File menu, and the manager's `ctrl+p`) names the subject first: the scratch the
manager marked, else the **focused editor's file** — which has to be a scratch,
since promoting anything else is not a thing the command can mean, and it says
so for a project file.

The target-path prompt (`internal/app/scratch_promote.go`) is the [untitled
save-as prompt](./editor.md)'s twin (#730): one line of path input, relative to
the project root unless absolute, `enter` accepts, `esc` cancels, prefilled with
the scratch's own file name. It is a separate state rather than a reuse of the
save-as one because the two act on different things — save-as *binds an unnamed
buffer*, promote *moves a named file that need not even be open*, which is the
manager's case.

Accepting, in order:

1. **An existing target is refused** with `file exists: …` on the error line
   and the prompt stays open — the same no-clobber rule save-as follows.
2. An open, **dirty** buffer on the scratch is flushed first, so the promoted
   file carries what is on screen rather than the last written state.
3. `scratch.Promote` moves the file, source-removed-last (see the store above),
   so a write error leaves the scratch and the tab untouched with the store's
   own message on the error line.
4. The move is announced as `explorer.FileMovedMsg` — the very message the
   manager's rename emits — so open tabs and deferred ones re-point through
   `followMovedFile`, the buffer keeps its undo history, the watcher follows,
   bookmarks re-key and the tab title updates. **Saving after a promote writes
   to the project file, not into the store.** The explorer's Scratches section
   is refreshed (one row less) and the project tree picks the new file up
   through its watcher like any other externally created file.

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
