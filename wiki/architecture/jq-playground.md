---
type: concept
title: jq Playground
description: Inline jq query line mounted in the pane it queries — the pane's body becomes a read-only editor buffer holding the live result; gojq as the engine, debounced generation-stamped evaluation, inline compile/runtime errors, result cap, copy and open-as-scratch, opening on `.` or the input's last valid program with the caret's path behind its own command, one session-wide program history shared by every buffer and response pane, a completion popup offering the snapshot's keys after a dot (pipeline-aware: pipe segments, select/map arguments and object constructions set the context) and gojq's builtins on an identifier, a library of named saved filters in a project and a global scope with a picker that inserts, renames and deletes them, and a toggleable full-query view laying a program too wide for the query line out over several pipe-broken rows.
resource: internal/jqplay/jqplay.go
tags: [architecture, json, jq, tools, inline, editor, http, completion]
timestamp: 2026-08-21T12:00:00Z
---

# jq Playground

#1936, inline since #1970. JSON is everywhere in the daily workflow — `.http`
responses, Elasticsearch hits, parser output, fixtures — and until now the only
way to run a jq program against one was to leave the IDE. The playground is a
**query line mounted inline in the pane holding the JSON**, with the pane's
body replaced by a **read-only editor buffer showing the program's live
output**: no floating dialog, the result reads and navigates like any buffer.

Opened by the **`json.jqPlayground`** command (command palette, Tools menu; no
default chord), on JSON editor buffers and the HTTP response pane alike — and
by **`json.jqPlaygroundAtPath`**, the same mode seeded with the caret's jq
path (#1982). `esc` leaves the mode; the pane's own content was never touched,
so leaving restores it bit-identically — editability included.

## Structure

```
internal/jqplay/
  jqplay.go      evaluation core: Parse, Run, Evaluate, Result, History — pure, no UI state
  raw.go         EvaluateRaw: the `jq -r`-shaped single-value form (used by .http captures, #1993)
  highlight.go   the query line's jq scanner: Tokens/KindAt, single pass, never fails
  complete.go    the typing aid: Complete — snapshot keys at a path, gojq's builtin list
  library.go     the named saved-filter store: Library, Filter, Scope — path-agnostic, one type for both scopes
  wrap.go        the full-query view's line breaking: Wrap/LineAt, pipe-aware, rune-indexed
internal/app/
  jqplayground.go the inline mode: query header, result buffer, key routing, debounce and async eval
  jqcomplete.go   the completion popup: state, keys, rendering and compositing
  jqfilters.go    the filter library's UI: the two store paths, the name prompt, the palette picker
  commands.go     json.jqPlayground / json.jqPlaygroundAtPath → the two open messages,
                  json.jqSaveFilter / json.jqFilters / json.jqRenameFilter → the library,
                  json.jqQueryView → the full-query view toggle
```

The split is the usual one: everything interesting — parsing, running, error
and cap handling, colorization, history — is pure and testable in
`internal/jqplay`; `internal/app` owns the terminal.

The package is also the one place gojq is spoken to. Besides the playground,
the `.http` client's `# @capture name = <jq-expr>` directive (#1993, see
[HTTP client](/architecture/http-client.md)) evaluates through
`jqplay.EvaluateRaw` and colours its expression with `jqplay.Tokens` — the
same engine and the same scanner, so a program that works in the playground
works in a request file.

## The inline mount

While the mode is active on a pane:

- The pane's first two content rows are the **query header**: the colored
  query line, then one info row (input origin and size, result summary, key
  hints — or the error, or a transient status). The header height is fixed, so
  an error appearing mid-keystroke never resizes the buffer below it — the
  [full-query view](#the-full-query-view) is the one thing that grows it, and
  only on the user's key.
- The info row is composed of **styled segments** (#1978): the summary in the
  theme's Hint, the caps — `(stopped at 500)`, `(first 10000 only)` — and a
  zero-value result in **Warning** (the buffer is blank then, and the summary
  is the only signal that nothing matched). On a narrow pane the key hints are
  **dropped as whole `·`-separated segments** instead of being cut mid-word;
  the input and result summary always survive. Truncation is cell-aware, so a
  wide glyph in the source label cannot overflow the row.
- The query line's `> ` marker is **blanked while the line does not hold the
  keyboard** — the result buffer has it, or the focus is on another pane —
  the same inactive affordance the regex tester's field labels use; the `jq:`
  label renders in the chrome's Secondary either way. The prefix width never
  changes, so the cursor window math is focus-independent.
- The rest of the pane shows a **substitute read-only editor**
  (`ShowReadOnly`, the #1762 buffer) holding the result under the virtual
  path `jq result.json`, so JSON highlighting applies. It is a full editor:
  motions, search, folds, **visual selection and yank**, mouse click/drag
  selection, wheel and scrollbar all work; mutations are refused with the
  usual `E45`.
- The pane's own component — the document editor, the HTTP viewer — is not
  rendered but keeps its entire state. The breadcrumbs row (#1153) is
  suppressed for the pane; the query header takes its place in the mouse
  translation (`contentYOff`).

The keyboard is modal **while the hosting pane is focused** and starts on the
query line; **tab** moves it into the result buffer and back. A mouse click
into the result body also focuses it (and places the caret); a click on the
header returns to the query line. Moving the focus to another pane — a click,
or the spatial focus keys (default ctrl+arrows), which escape the mode the way
they escape a focused terminal — **leaves the playground mounted** (#1980):
query, result and history position stay intact while the other pane takes keys
normally, so another file can be edited with the filtered result still
visible. Returning the focus to the pane resumes the query line as it was;
the pane's `esc` remains the way to close the mode. The hosting pane closing
from elsewhere closes the playground with it.

## Engine: gojq, not a jq binary

Evaluation runs on **[gojq](https://github.com/itchyny/gojq)** (MIT), the
pure-Go jq reimplementation. That keeps the build **cgo-free** and needs no
`jq` on `PATH` — the same reasons the [Data Viewer](./data-viewer.md) takes a
pure-Go SQLite driver. Two gojq properties matter here beyond "it is jq":

- **Arbitrary-precision integers.** The input is decoded with `UseNumber`, so a
  64-bit id survives a round trip that `float64` would round.
- **Copy-on-write updates.** gojq only mutates containers it allocated itself,
  so the parsed snapshot is safe to reuse across runs without a deep copy per
  keystroke.

A system `jq` fallback is deliberately absent: two engines would mean two
dialects to explain in one playground.

## What is queried

The input is **snapshotted and parsed once** when the playground opens — a jq
program is written against the document that was on screen, and re-parsing a
10 MB response on every rune would make the query line stutter for no gain. The
snapshot is resolved in this order, and the mode mounts **in the pane the
snapshot came from** (focus moves there):

1. the **focused HTTP response pane**'s body ([HTTP Client](./http-client.md)) —
   the pane the user is looking at is the one they mean;
2. the focused editor's **visual selection**, so a JSON blob embedded in a log
   file is queryable without extracting it first;
3. the focused editor's **whole buffer**;
4. a visible-but-unfocused HTTP response pane — the usual focus right after a
   dispatch is the editor holding the `.http` file; a tab-nested viewer
   (#1778) gets its tab activated.

With none of those the command notifies instead of opening over nothing.

The text is decoded as a **JSON stream**: one value for an ordinary document,
many for a `.jsonl` export or a concatenated body, and the program runs against
every one, as jq's own stdin does (capped at `MaxInputValues`, 10 000). A buffer
that is not JSON is an inline message naming the offending **line** — a byte
offset says nothing to the reader of a pretty-printed document.

## What the query line opens on

The ordinary open starts on **`.`**, which pretty-prints the input (#1982).
Until then it was prefilled with the caret's jq path, and that was the wrong
default: most openings only check something, so the prefilled program was a
deletion to perform before the first keystroke of the program actually wanted.

Two things override the identity:

- **This input's last valid program of the session.** Every run that comes
  back without an error records its program under the input's key — the file
  path, the unsaved buffer's editor key, the response pane's key — and the
  next open over that same input starts on it. Reopening a file therefore
  resumes the look that was interrupted, which the session program **history**
  cannot express: that list is deliberately one shared, buffer-agnostic
  sequence, so its newest entry is whatever was run last *anywhere*. A
  program that failed to compile or raised at runtime is not recorded, so a
  half-typed one never displaces the last one that worked, and `.` is not
  recorded either — it is the default already.
- **`json.jqPlaygroundAtPath`**, the explicit form of the old behavior: the
  caret's jq path (`.spec.containers[2].name`, from the
  [path breadcrumb](./editor.md) of #1660), so "the value I was looking at" is
  already a program. It needs the whole focused buffer as the input — against
  a response body or a selection the caret's path indexes the *file* and would
  name a location the input does not contain — and falls back to `.` when the
  caret has no path.

The memory is in-memory for the session, like the history: a jq program is
scratch work, and persisting it into the project state would be noise.

## Debounce, generations, cancellation

Evaluation never runs on the event loop:

- Each program change **cancels the run in flight**, bumps a generation and
  schedules a `tea.Tick` (120 ms) stamped with it. Only the tick still holding
  the current generation starts a run — that is the debounce.
- The run itself is a `tea.Cmd` under a `context.WithTimeout(EvalTimeout)`; its
  `jqEvalDoneMsg` carries both the generation and the state it belongs to, so a
  result superseded by a newer keystroke — or one arriving after the mode
  closed — is dropped instead of overwriting the current one. A current one
  reinstalls the result buffer's content (cursor and scroll reset with it —
  the text they pointed into just changed).
- `enter` and the initial evaluation skip the debounce; the result is wanted
  now, not a tick later.

Three independent bounds keep a hostile program from hanging the IDE, because
no single one covers all of them:

| Bound | Constant | Catches |
| --- | --- | --- |
| Output count | `MaxOutputs` (500) | `range(infinite)`, `repeat(0)` |
| Output size | `MaxResultBytes` (256 KiB) | few values, each enormous |
| Wall clock | `EvalTimeout` (5 s) | `def f: f; f` — loops emitting *nothing* |

A capped run is **not an error**: the result summary says `(stopped at 500)`
— in the theme's Warning color, since the user is seeing less than the run
produced — and the values collected stand. Opening the playground over an
input larger than `AsyncThreshold` (64 KiB) parses off the loop too, so even
the open is not a stall.

While a re-run is pending the summary keeps the **previous count** with an
`· evaluating…` suffix rather than replacing it (#1978): pending is set the
moment the debounce tick is scheduled, and swapping the text outright made
the row shimmer on every keystroke. A bare `Result — evaluating…` appears
only when there is no previous count to keep.

## Errors are inline, never a crash

Everything that can go wrong shows on the header's info row, in the theme's
error color:

- an **input** that is not JSON (which beats a program error — with no parsed
  input no program could have run);
- a **compile** error from gojq (`unexpected token`, `function not defined:
  wat/0`, `variable not defined: $x`), which produces no output;
- a **runtime** error, which may arrive *after* some values were produced —
  `.[] | .x` over `[{"x":1},3]` prints one value and then fails. `Err` and
  `Outputs` are therefore not mutually exclusive, and the error row keeps the
  count — `E: … · 1 value(s) before the error` — because those values are
  sitting in the buffer below it (#1978).

`halt` ends a run cleanly rather than as a diagnostic, as it does in jq.

## Syntax highlighting

The query line is colorized by a **single-pass rune scanner** (`jqplay.Tokens`),
not a parser. A live query line spends most of its time holding a program that
does not parse — an unterminated string, a half-typed `map(` — which is exactly
when the color helps most, so the scanner never fails: an unterminated string
runs to the end of the line, an unknown rune is punctuation. It classifies
paths, strings, numbers, keywords, functions, `$variables`, `@formats`,
operators and comments, mapped onto the **chrome** palette (Accent, Success,
Info, Secondary, Warning, Hint) rather than the editor's capture colors — the
header is chrome over the pane surface, not buffer text. The **result** is
highlighted separately, as JSON, by the substitute editor's ordinary pipeline.

## The full-query view

The query line is one row, windowed around the cursor and cut with `…` at both
edges. That is right for typing and wrong for reading: a pipeline like
`.hits.hits[]._source | .keyword as $keyword | .ser[] | select(…) | {…}` is
never on screen as a whole, so the overview is lost exactly where it is needed
— while building a long pipeline (#2032).

**`json.jqQueryView`** (`ctrl+alt+e`, or the palette / Tools menu) toggles the
**full-query view**: the same program laid out over several rows, in place, with
the program, the cursor, the history position and the result untouched. It
works from the query line and from the result buffer alike (the mode resolves
Global chords it does not claim, #1983), and the info row's key hints name the
chord actually bound to it — a rebind renames the hint with it.

- **The wrap breaks at the pipes** (`jqplay.Wrap`, pure and rune-indexed like
  the scanner): a jq pipeline reads as a sequence of stages, so a row boundary
  on a `|` keeps a stage whole. Stages are packed greedily, a stage wider than
  the row is cut at the width (there is nothing better to break it on), and a
  `|` inside a string or a comment — or a `||` — is not a boundary. The blanks
  that separated a stage from the pipe before it are dropped, so no row starts
  on a space; a hard cut inside a stage keeps every rune, because it may land
  inside a string literal whose spaces are text.
- **Highlighting is the same** — the rows are colored rune by rune by
  `jqplay.Tokens`, and the cursor cell is drawn on its own row, so the
  expanded view is the one-line view with more rows rather than a second
  renderer.
- **The header grows, the result shrinks.** `jqHeaderRowsFor` reports the query
  rows plus the info row, and the rendering, the result buffer's height and the
  mouse translation all read that one number, so the mode still fills its pane
  exactly. The view is capped at **8 rows** and always leaves **3 rows of
  result** standing: expanding is for seeing the program, not for hiding the
  output. Past the cap the rows window around the **cursor's** row and mark the
  cut with `…`, the way the one-line view windows around the cursor cell.
- **A cut is announced.** Whenever the rows show less than the program holds,
  the info row carries a `· query cut` marker in the theme's Warning — the same
  "you are seeing less than there is" color the output caps use, because the
  `…` at the row's edge alone is too easy to miss.

The one-line header stays the **default**: the full-query view costs result
rows, and the resting layout is the one to type in. It is view state only —
nothing about it reaches the program, the history or the saved filters.

## Completion

The query line has a typing aid (#1979), synchronous and bounded, with the
candidate logic in `internal/jqplay/complete.go` and the popup in
`internal/app/jqcomplete.go`:

- **Path completion from the input.** A `.` — and deeper, `.foo.`,
  `.items[].`, `.items[0].`, `."a b".` — offers the object keys that actually
  exist at that path in the parsed snapshot, sorted, each with its value's
  type as the detail. The chain before the dot is parsed right-to-left
  (fields, quoted fields, `[]` iteration through arrays and objects, integer
  indexes, the `?` marker); a chain not rooted at the input — `f(x).`,
  `$v.`, `(.a).`, `env.` — offers **nothing**, because a wrong list is worse
  than none. A key that is no identifier inserts its quoted form (`."a b"`).
- **Pipeline-aware context (#2019).** A chain rooted at a boundary resolves
  in the context the preceding program establishes: pipe segments that are
  themselves simple path chains prepend their steps (`.[] | .` offers the
  element keys, `.foo | .` the keys under `foo`), a `select(...)` segment
  passes its input through, and inside the argument of `select` (same input)
  or `map` and the `_by` functions (per element, one extra iterate step) the
  inner dot inherits the enclosing position's context — nesting composes
  (`select(map(.`). Object construction is a context too: `{` and a bare
  identifier after it or after a comma inside it are jq's `{a: .a}` shorthand
  key position and offer the context value's keys instead of builtins, and a
  value chain (`{x: .`) resolves the same way. Anything the analysis cannot
  answer statically — an unknown function, `$var`, arithmetic pipe segments,
  `reduce`/`as` — stays **silent**; keys that may not exist are never
  offered.
- **Builtin completion.** A bare identifier offers gojq's builtins — the
  engine's own `builtins` list, evaluated once and memoized, so it names
  exactly what the playground accepts whatever gojq version ships (entries
  the plain compiler rejects, like `input`, are probed out). The arity
  notation (`select /1`, `range /1 /2 /3`) is the detail, and the common
  builtins carry a curated one-line description shown under the popup.
- **Bounded, never blocking.** The snapshot was parsed once at open; the key
  walk visits at most a fixed node budget (10 000) and offers what it saw,
  the list caps at 200 candidates, and everything runs inline on the
  keystroke — no async round-trip to go stale.

The popup follows the **editor completion pattern** (its keys, its look, the
same accept hint row): typing a `.` or an identifier rune opens it,
`ctrl+space` asks explicitly (the full builtin list on an empty line), typing
re-filters, a matchless partial closes it. While it shows it owns
`↑`/`↓`/`ctrl+n`/`ctrl+p` (step, wrapping), `pgup`/`pgdn` (page), `enter` and
plain `tab` (accept), `esc` (dismiss) — the query line's own meaning of those
keys returns the moment it closes. A cursor motion, a paste, a focus change
into the result buffer all drop it. It is composited over the pane below the
query line like the editor's popups (#316), shifting at the screen edges.

## Keys

Query line (the default focus; while the completion popup is open its keys
above win):

| Key | Effect |
| --- | --- |
| *(typing)* | edit the program; each change re-evaluates, debounced, and re-filters or opens the completion popup |
| `ctrl+space` | open the completion popup explicitly (the full builtin list on an empty line) |
| `enter` | record the program in the history and run it now |
| `↑` / `↓` | walk the session program history (`↓` past the newest restores the draft) |
| `tab` | move the keyboard into the result buffer |
| `ctrl+alt+e` | toggle the [full-query view](#the-full-query-view) (`json.jqQueryView`) |
| `pgup` / `pgdn` | page the result buffer without leaving the query line |
| `ctrl+s` | save the program as a **named filter** (`json.jqSaveFilter`) |
| `ctrl+l` | open the **saved-filter picker** (`json.jqFilters`) |
| `ctrl+y` | copy the **whole** result (not just the visible part) |
| `ctrl+o` | open the result as a fresh `.json` scratch |
| `esc` | close (recording the program in the history) |

`ctrl+alt+e` works from the result buffer too.

Result buffer (after `tab`): the **full editor keymap** — motions, search,
folds, visual selection, `y` yank of the selection — against the read-only
buffer, with four exceptions: `tab` returns to the query line, `ctrl+y` /
`ctrl+o` keep their result-action meaning (shadowing the editor's scroll and
jumplist keys — a throwaway result has no jumplist worth keeping), and `esc`
closes the mode only from resting normal mode; a visual selection, a pending
operator or a search prompt is quit first, like in any buffer. The app
keymap's copy chord (`editor.copy`, default cmd+c) copies the visual selection
like in any read-only buffer (#1980): the mode resolves the chord against the
editor context itself, since its modal routing keeps it from the keymap layer,
and the Edit menu's copy (an `editor.ActionMsg`) is routed to the substitute
buffer rather than the pane's hidden document while the pane is focused. The
pane border signals the buffer's input mode (#1353) while it holds the
keyboard.

The spatial focus keys (default ctrl+arrows) work from both the query line and
the result buffer: they move the focus out of the pane with the playground
still mounted (#1980).

**Global-scope chords keep working** (#1983): a key the playground leaves over
resolves against the Global scope of the live binding table and dispatches its
command — `cmd+shift+a` opens Search Everywhere, `cmd+e` opens Recent Files,
and every other Global binding that does not collide with the mode's own keys
behaves as it would in any pane. From the query line that is any key neither
the mode nor the single-line editing claims; from the result buffer only
modified chords (ctrl/alt/cmd) are eligible — plain and shift-only keys stay
with the buffer as motions, search input and prompt text, the same rule the
main dispatch applies to a capturing editor. Local keys keep priority where
they collide, pane-scoped bindings never fire (the mode replaces the pane's
component, so its context keys would act on a hidden editor), and multi-step
chords are left alone — resolving them would mean buffering query input, the
same trade the terminal makes (#805).

A bracketed paste always lands in the query line, **flattened** to one line,
like every other single-field prompt — the result buffer refuses pastes with
everything else.

Editing output belongs in a writable buffer, which is exactly what `ctrl+o`
makes — a [scratch file](./scratch-files.md) opened through the standard
funnel, so folding and the path breadcrumb apply, and the playground can be
run again over the result. That is how a multi-step jq session actually goes.

## History

Programs are remembered **per session, in memory only** (newest first, repeats
moved to the front, capped at 50), like the regex tester's patterns: a jq
program under construction is scratch work, and persisting it into the project
state would be noise. The program that *is* worth keeping gets a name instead —
see [the saved-filter library](#the-saved-filter-library) below. The history lives on the root model, not on the mode
state, so it survives closing and reopening the playground.

It is **one session-wide list, shared by every playground** — a program run
over a `.http` response is offered by `↑` in a `.json` buffer and the other way
round, because the mode was never the thing that owned it. The open mode holds
a *pointer* to that one list and writes into it the moment `enter` records a
program (#1977); it used to carry a copy that was only handed back on close,
so any exit that skipped the close — reopening the playground over another
buffer while one was still up, which the Tools menu allows with a click — took
that session's programs with it and the history read as per-file.

Browsing it is draft-preserving and skips the redundant first step (#1973).
The query line a walk starts from is kept, so `↓` back past the newest entry
restores the half-written program instead of clearing the line; and a newest
entry that only repeats what the query line already holds is stepped over on
the way out. That second rule is what makes `↑` do something: both commit
points leave the line equal to the newest entry — `enter` keeps the program it
just recorded, and reopening over the same file seeds the last program run on
it (#1982) — so without it the first `↑` would visibly change nothing and the
history would read as broken.

The history and the per-file recall are **different memories on purpose**: the
history answers "what did I run recently, anywhere", the recall answers "where
was I in *this* file". The list is not keyed by file precisely because a
program written against one response is usually worth trying against the next.

## The saved-filter library

The history above is the playground's *short* memory. #1995 adds the long one:
a **library of named filters**, because the program that took an afternoon to
get right — the mapping-flattening one for an Elasticsearch response — was
until now indistinguishable from the fifty one-off experiments around it in
the history, and rotated out with them.

A saved filter is a **name and a program**, in one of two scopes:

| Scope | File | For |
| --- | --- | --- |
| **project** | `.ike/jqfilters.json` | filters shaped by *this* project's data — next to the HTTP environment selection |
| **global** | `~/.ike/jqfilters-global.json` | filters that are about jq, not about a project — next to the [saved window layouts](./pane-layout.md) |

Both files follow the `IKE_CONFIG_DIR` redirection seam every other state store
uses, under **distinct file names** (the `winsize.json` / `winsize-global.json`
precedent, #1714), so redirecting one directory still yields two libraries. A
missing or malformed store loads as an empty library — the playground must open
even when a hand-edited file is broken — and entries with an empty name or
program are dropped on the way in.

The store is a *file per scope*, not a config key. A jq program is data the
user creates from inside the IDE, like a saved layout; adding one by
hand-editing `settings.toml` would be the wrong affordance, and TOML lists
*replace* across the config layers, so a project file would hide the user's
whole library instead of adding to it.

### Saving

`ctrl+s` on the query line (or `json.jqSaveFilter`) opens a one-line name
prompt over the current program. `tab` toggles the scope the save goes to,
which starts on **project** — a filter written against this project's data
usually belongs to it, and promoting it is one keystroke. The identity program
is refused: `.` is the playground's default, not a filter. A name already taken
**in the target scope** holds the prompt open for a second `enter`, the
save-layout store's guard (#1175) — and that confirmed overwrite is also how a
filter is *edited*: insert it, change it on the query line, save it under the
same name.

### The picker

`ctrl+l` (or `json.jqFilters`) opens a locked palette mode listing **both**
scopes, project first:

- Rows are fuzzy-matched over the **name** — what a saved filter is remembered
  by — never over the program; searching for `select` would otherwise match
  half the library.
- The program rides along as the row's **detail chip**, collapsed to one line
  and cut at 48 columns, so the pipeline behind a name is visible without
  picking it.
- The **scope is the row's accent badge** (`project` / `global`), which is what
  tells the two apart at a glance. A name may exist in both scopes — they are
  separate stores, and shadowing one with the other would hide a filter that
  was saved deliberately — so both rows are listed.
- `enter` puts the program on the query line and runs it. With no playground up
  the command still completes: it opens one over the JSON at hand first (and
  says so when there is no JSON to query).
- `shift+delete` (or `cmd+backspace`) **deletes** the row's filter from its own
  store and refreshes the list in place — the palette's aux convention (#1113).
- `json.jqRenameFilter` opens the same picker in its **rename** spelling, where
  `enter` opens the name prompt over the entry instead of inserting it. Renaming
  onto a taken name is refused: only the confirmed save overwrite may replace a
  filter, never a rename that happens to collide.

The list is re-read from both files on every open and after every delete, so a
library changed by another window — or by hand — is never stale.

## Boundaries

- **No raw output mode** (`jq -r`), no `--slurp`, no `--arg`: the playground is
  a JSON-in/JSON-out tool, and every one of those would be a mode the result
  actions then have to reason about.
- **No live re-read of the buffer.** The snapshot is taken at open; editing the
  file underneath and re-running means reopening the playground.
- **In-pane, but not a pane.** #1970 revised the old "floating modal" boundary:
  the playground now lives inside the pane it queries — but it is still a
  *mode*, not a layout leaf. It has no key of its own, cannot be split, moved
  or persisted, and the keyboard is modal only while its pane is focused
  (#1980); it survives focus changes but not its pane closing, a project
  switch or `esc`. The *mode* is disposable even now that programs are not: a
  saved filter outlives the session, the pane it was written in does not.
- **The result buffer is a substitute, not the document.** The hosting pane's
  own component keeps its entire state and is simply not rendered; the mode
  never mutates it, which is what makes `esc` a perfect restore.
- **No settings.** The caps are the safety net, not a preference; exposing them
  would invite raising them past what the pane can render. The filter library
  has none either: it is data the user creates, not a preference to configure —
  the only knob is a `MaxFilters` runaway guard nobody should ever feel.
- **The history is still not persisted.** #1995 gives the durable programs a
  *name* and a file; the anonymous ones stay in memory. Persisting the history
  too would put every half-typed experiment into the project state, which is
  exactly the noise the library exists to separate out.
