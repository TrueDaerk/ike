---
type: concept
title: jq & yq Playground
description: Inline query line mounted in the pane it queries — the pane's body becomes a read-only editor buffer holding the live result; two dialects over one implementation (jq for JSON buffers and HTTP responses, yq for YAML buffers), gojq as the shared engine with YAML as a second input/output path, debounced generation-stamped evaluation, the input snapshot re-read and re-run when the source file changes externally (whole-file editor sources only, last valid result kept on a broken parse, a removed file ending the mode definitely), inline compile/runtime errors that leave the last successful result in the buffer under a stale banner instead of clearing it, result cap, copy and open-as-scratch in the dialect's own extension, opening on `.` or the input's last valid program with the caret's path behind its own command, one session-wide program history shared by every buffer and both dialects, a completion popup offering the snapshot's keys after a dot (pipeline-aware: pipe segments, select/map arguments and object constructions set the context) and gojq's builtins on an identifier, per-dialect libraries of named saved filters in a project and a global scope with a picker that inserts, renames and deletes them, vim-style folding of the result's objects, arrays and YAML blocks with member-counting placeholders, and a toggleable multi-line view laying a program too wide for the query line out over several pipe-broken rows and editing it there — caret motion across the rows with a goal column, row-local home/end, click-to-place, history on alt+arrows and the completion popup anchored on the caret's row, while the program itself stays one line; the mode's keyboard is documented as its own cheatsheet context and its *language* has a second, searchable cheatsheet of syntax, one-line example programs and every builtin — generated from the engine's own list, dialect-aware, and inserting a picked row into the query line — `esc esc` reaches the command palette out of the query line, and the code-action chord answers with a plain "not available here" instead of a silent nothing, while the find chord opens the result buffer's search from either focus and leaves the keyboard there for `n`/`N`; the mode is bound to the document it queries, not to its pane alone, so a pane switched to another file or tab shows that file at once while the playground stays mounted and hidden, and it closes when its document leaves the workspace.
resource: internal/jqplay/jqplay.go
tags: [architecture, json, yaml, jq, yq, tools, inline, editor, http, completion, folding]
timestamp: 2026-09-01T00:00:00Z
---

# jq & yq Playground

#1936, inline since #1970, two dialects since #2039. JSON is everywhere in the
daily workflow — `.http` responses, Elasticsearch hits, parser output,
fixtures — and until now the only way to run a jq program against one was to
leave the IDE. The playground is a **query line mounted inline in the pane
holding the document**, with the pane's body replaced by a **read-only editor
buffer showing the program's live output**: no floating dialog, the result
reads and navigates like any buffer.

Opened by the **`json.jqPlayground`** command (command palette, Tools menu; no
default chord), on JSON editor buffers and the HTTP response pane alike — and
by **`json.jqPlaygroundAtPath`**, the same mode seeded with the caret's jq
path (#1982). `esc` leaves the mode; the pane's own content was never touched,
so leaving restores it bit-identically — editability included.

YAML gets the **same mode** under `yaml.yqPlayground` /
`yaml.yqPlaygroundAtPath`: same query line, same result buffer, same history,
same keys — only the decoder, the rendering, the fold scan and the filter
library differ. See [the yq dialect](#the-yq-dialect); everything else in this
document describes both.

## Structure

```
internal/jqplay/
  jqplay.go      evaluation core: Parse, Run, Evaluate, Result, History — pure, no UI state
  dialect.go     the jq/yq seam: Dialect — how a buffer is read, a value written, a result folded
  yaml.go        the yq input/output path (#2039): YAML stream → gojq values → YAML
  yamlfold.go    yamlFolds: the YAML result's foldable blocks, by indentation
  raw.go         EvaluateRaw: the `jq -r`-shaped single-value form (used by .http captures, #1993)
  fold.go        Fold + jsonFolds: the JSON result's foldable objects/arrays with their member counts (#2029)
  highlight.go   the query line's jq scanner: Tokens/KindAt, single pass, never fails
  complete.go    the typing aid: Complete — snapshot keys at a path, gojq's builtin list
  library.go     the named saved-filter store: Library, Filter, Scope — path-agnostic, one type for every store
  cheatsheet.go  the language sheet (#2382): Cheatsheet, CheatEntry, Sample — syntax, one-line examples, every builtin
  wrap.go        the multi-line view's line breaking and caret coordinates: Wrap/LineAt/RowCol/PosAt
internal/app/
  playground.go   the inline mode: query header, result buffer, key routing, debounce and async eval
  playcomplete.go the completion popup: state, keys, rendering and compositing
  playfilters.go  the filter library's UI: the store paths, the name prompt, the palette picker
  playcheat.go    the language cheatsheet's UI: the locked palette mode, the search, the two insertions (#2382)
  playhelp.go     the mode's key inventory for the cheatsheet: query line, result buffer, keymap chords (#2237)
  commands.go     json.jqPlayground / …AtPath and yaml.yqPlayground / …AtPath → the two open messages,
                  json.jqSaveFilter / {json.jq,yaml.yq}Filters / …RenameFilter → the libraries,
                  {json.jq,yaml.yq}Cheatsheet → the language sheet,
                  json.jqQueryView → the multi-line view toggle
```

The split is the usual one: everything interesting — parsing, running, error
and cap handling, colorization, history — is pure and testable in
`internal/jqplay`; `internal/app` owns the terminal.

The app-side identifiers say **`play`**, not `jq`: there is one playground with
two dialects, and a `playState` carrying a `jqplay.Dialect` is what keeps the
hosting, the geometry, the key routing and the rendering from existing twice.

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
  [multi-line view](#the-multi-line-view) and the one-row
  [stale banner](#a-failed-run-keeps-the-last-good-result-2412) are the only
  things that grow it.
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
  (or `yq:`) label renders in the chrome's Secondary either way. Both dialect
  names are two cells wide and the prefix width never changes, so the cursor
  window math is independent of both focus and dialect.
- The rest of the pane shows a **substitute read-only editor**
  (`ShowReadOnly`, the #1762 buffer) holding the result under the virtual
  path `jq result.json` — `yq result.yaml` in the other dialect — so the
  matching highlighting applies. It is a full editor:
  motions, search, **folds** (see below), **visual selection and yank**, mouse
  click/drag selection, wheel and scrollbar all work; mutations are refused
  with the usual `E45`.
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

## Bound to the document, not to the pane

The mode is mounted in a pane, but it belongs to the **document it queries**
(#2355). `playState` therefore keeps that document itself — `srcEd`, the
queried editor model, or `srcInst`, the queried HTTP response instance —
beside the `paneKey` it mounted in, and `playSrcShown` asks the pane whether it
*still shows* that document: the active tab's editor must be the very model
that was queried and still stand for the same document (an editor retargeted
to another file keeps its pointer but changes its doc key), or the pane's
active content must be the queried response instance.

Everything that used to decide on the pane alone now goes through that
question — rendering and pane title, the reserved header rows
(`playHeaderRowsFor`, and with them the mouse translation and the result
buffer's height), the pane's mouse routing, the wheel and the key routing
(`playFocused`). Deciding it in one place is what keeps a half-rendered state —
a query header over a foreign document, or reserved rows nobody draws — from
existing at all.

The consequence: opening another file into the hosting pane, or switching to
another tab, shows that file **immediately**. The playground is not closed by
it — it stays mounted with query, result and history position intact, exactly
as it survives a focus change (#1980), and simply renders nothing while the
pane shows something else. Switching back to the source document brings it
back as it was. The HTTP response playground is unaffected: its source is a
response instance, not a file document, and it is matched by that instance.

A mounted playground never outlives its document: `syncPlaygroundSource` runs
on the settled `Update` pass and closes the mode when the queried editor model
(or response instance) is no longer anywhere in the workspace, so tab closes,
pane closes and explorer deletions are all covered by one hook instead of each
close path remembering the mode.

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

## The yq dialect

#2039. The machinery above is not about JSON — a query line, a result window,
a history and a filter library are the same tools whatever the document is.
YAML is the other format the daily workflow is full of (manifests, CI
pipelines, compose files), and it had no counterpart. It has one now, and it is
**the same playground**: `jqplay.Dialect` is the only thing that differs, and
it decides exactly three things — how the buffer is decoded, how a value is
written back, and how the result folds.

### Engine: gojq again, with YAML on both ends

The obvious alternative was a Go port of mikefarah's yq. It was not taken.
yq's expression language *is* jq's for everything anyone types into a query
line, so decoding YAML into the value shapes gojq already runs over buys the
whole language for the price of a decoder — the trade `gojq --yaml-input
--yaml-output` makes. A second engine would have meant a second builtin list
for the completion popup, a second scanner for the highlighting and a second
set of error spellings, all for a dialect the user cannot tell apart.

What is given up is yq's own extensions — comment and anchor *preservation*,
`style`/`tag` operators, in-place document editing. The playground is a
read-only query tool, so none of them has a job here; a program that needs
them is a `yq` invocation in a terminal, not a query line. What is gained is
that a jq program written in one playground runs unchanged in the other.

The decoder (`internal/jqplay/yaml.go`) walks `yaml.Node` rather than
unmarshalling into `any`, which is what makes four YAML facts survive:

| YAML | Becomes | Why not the convenient form |
| --- | --- | --- |
| anchors / aliases (`*base`) | resolved, under a node budget | `Decode(&any)` expands them with no bound — a "billion laughs" file would be an OOM instead of an error line |
| merge keys (`<<: *base`) | folded into the mapping, explicit keys winning | jq has no merge operator; without this a compose file's inherited keys are invisible |
| non-string keys (`1:`, `true:`) | their source text (`."1"`) | jq objects have string keys; `map[any]any` is a value gojq refuses |
| `0x1f`, `1_000`, a 20-digit id | `json.Number` with its decimal digits | plain decoding yields `int` (losing width) or `time.Time` (which the engine cannot compute on) |

Everything the YAML core schema does not cover — a timestamp, a `!Ref`, a
binary blob — arrives as the string it was written as: a query language has
nothing better to do with a value it cannot compute on, and a string at least
stays visible and greppable. `MaxYAMLNodes` (1 Mi values) bounds the alias
expansion; exceeding it is an ordinary input error on the info row.

Rendering is the mirror image: the encoder builds a `yaml.Node` tree instead
of marshalling the Go value, because gojq's outputs contain `json.Number` and
`*big.Int`, which the reflective encoder would write as a quoted string and as
a struct. Building the nodes also fixes the key order to gojq's own, so the
same value reads the same in both playgrounds. Strings that would read back as
another type are quoted (`v: "123"`); multi-line strings become literal blocks
(`script: |`) rather than one `\n`-riddled row.

A **multi-document** file (`---`) is a stream, exactly like a `.jsonl` export:
the program runs over each document, and the outputs come back separated by
`---` rather than merged.

### What differs, concretely

| | jq | yq |
| --- | --- | --- |
| Commands | `json.jqPlayground`, `json.jqPlaygroundAtPath` | `yaml.yqPlayground`, `yaml.yqPlaygroundAtPath` |
| Input | focused HTTP response, else editor selection, else whole buffer | editor selection, else whole buffer |
| Query-line label | `> jq: ` | `> yq: ` |
| Result | pretty JSON, outputs joined by a newline | block YAML, documents joined by `---` |
| `ctrl+o` scratch | `.json` | `.yaml` |
| Folding | multi-line objects / arrays, `{ ⋯ 3 keys }` | indented blocks and block scalars, `⋯ 3 keys` (YAML closes nothing) |
| Filter library | `jqfilters.json` / `jqfilters-global.json` | `yqfilters.json` / `yqfilters-global.json` |
| Seeded path | `.spec.["my-key"]` (`DocPathJQ`) | `.spec."my-key"` (`DocPathYQ`) |

The yq playground deliberately does **not** consider HTTP response bodies. A
response is JSON in every workflow the [HTTP client](./http-client.md) serves,
and letting a focused response outrank the YAML file the user asked about would
answer "yq Playground" with a parse error over somebody else's pane.

Only **one** playground is open at a time — it replaces a pane's content, and
that is one pane's worth of content whichever dialect it is. Opening the other
closes the first, recording its program in the history like any other close.

### What is shared

Everything not in the table above, which is the point of the issue:

- the inline mount, the geometry and the header layout;
- the query line: editing, highlighting, the multi-line view, click-to-place;
- the completion popup — the builtins are gojq's for both, and the key
  candidates come from the parsed snapshot whatever decoded it;
- the debounce, the generation stamping and the cancellation;
- the read-only result buffer, its folding keys and the copy / scratch actions;
- the session program history — **one list for both dialects**: a yq program
  *is* a jq program here, and the list was already deliberately promiscuous
  across buffers and response panes;
- the saved-filter *store* (`jqplay.Library`), the name prompt and the picker,
  parameterized by which pair of files they read.

The **libraries themselves are separate** rather than tagged. A saved filter is
written against a shape of document —
`.spec.template.spec.containers[0].image` is a Kubernetes-manifest filter and
has no business in the picker over a JSON API response — so mixing them would
make the picker noisier for both. The two commands name their own library;
`ctrl+l` on a query line passes the **open playground's**, so the chord means
"my filters" in either mode. Picking a filter with nothing open — or with the
other dialect's playground up — starts the playground of the filter's own
dialect, since that is the only kind of document its program can run against.

Saving is the one command that stays single: `json.jqSaveFilter` ("Save
Playground Filter…") writes into whichever playground is open, because the
program being named is that playground's. A `yaml.yqSaveFilter` would be a
second name for one behavior.

## Opening by file type (#2415)

Three dialects mean three commands to remember, so there is a fourth in front
of them: **`playground.open`** ("Open Playground for This File",
`cmd+shift+j`, Editor context) resolves the playground from the focused
buffer's language and opens it —

| buffer language id | playground |
|---|---|
| `json`, `jsonc`, `ndjson` | jq |
| `yaml`, `ansible` | yq |
| `xml`, `html` | xmq |
| anything else | none — the notification `no playground for <lang>` |

— in `internal/app/playgroundopen.go`. It only *routes*: every branch ends in
`startPlayground`, so how a playground mounts stays one implementation. The
xmq route is wired ahead of the playground itself through the
`startXMQPlayground` hook; while that hook is nil, an XML or HTML buffer is
answered with "the xmq playground is not available yet" rather than being
opened in the wrong dialect, and the route is covered by a test that installs
a stub.

The per-dialect commands are deliberately **not** replaced. `json.jqPlayground`
(`ctrl+alt+j`) and `yaml.yqPlayground` (`ctrl+alt+y`) keep their own chords,
stay separately rebindable, and count separately in the palette's frecency
(#2153) — someone who works in one dialect all day wants that command on its
own key, and "open jq on this HTTP response" is not a question about the
focused buffer's file type at all. The keymap's own answer to the same
question — whether the *chord* could be shared instead, one per
`editor[lang]` — is recorded in
[Keybindings & Shortcuts](/architecture/keybindings.md#one-chord-three-playgrounds-2415):
it can be, and a user can write it, but the defaults keep the mapping next to
the playgrounds instead of in the keymap.

## What is queried

The input is **snapshotted and parsed once** when the playground opens — a jq
program is written against the document that was on screen, and re-parsing a
10 MB response on every rune would make the query line stutter for no gain.
Once, that is, per *detected change*: an external write to the file the
snapshot came from renews it (see [Following the source
file](#following-the-source-file)). The snapshot is resolved in this order, and
the mode mounts **in the pane the snapshot came from** (focus moves there):

1. the **focused HTTP response pane**'s body ([HTTP Client](./http-client.md)) —
   the pane the user is looking at is the one they mean;
2. the focused editor's **visual selection**, so a JSON blob embedded in a log
   file is queryable without extracting it first;
3. the focused editor's **whole buffer**;
4. a visible-but-unfocused HTTP response pane — the usual focus right after a
   dispatch is the editor holding the `.http` file; a tab-nested viewer
   (#1778) gets its tab activated.

The yq playground skips steps 1 and 4 (see [the yq
dialect](#the-yq-dialect)); 2 and 3 work there identically, so a YAML block
selected inside a Markdown file is queryable without extracting it first.

With none of those the command notifies instead of opening over nothing.

The text is decoded as a **document stream**: one value for an ordinary
document, many for a `.jsonl` export, a concatenated body or a `---`-separated
YAML file, and the program runs against every one, as jq's own stdin does
(capped at `MaxInputValues`, 10 000). A buffer the dialect cannot decode is an
inline message — for JSON naming the offending **line**, since a byte offset
says nothing to the reader of a pretty-printed document; for YAML the decoder's
own complaint, which already carries its line.

## Following the source file

#2356. The snapshot principle earns its keep against *typing*; against a file
that changed underneath it, it produces a result describing a document that no
longer exists — and says nothing about being stale. So the one exception: when
the [external watcher](./editor.md#external-file-changes-roadmap-0140) reports a change to the file the snapshot
came from, the playground re-reads its input and re-runs the current program
against it.

- **Automatic, not offered.** The alternative — "the input changed, press `r`
  to reload" — buys nothing the machinery does not already give: parses past
  `AsyncThreshold` run off the event loop, the watcher debounces bursts into
  one flush, and superseded parses and runs are dropped on their generation
  stamps. What it costs is an answer required before every look.
- **The input is renewed, not the playground.** Query, caret, history
  position, the expanded view and the result buffer's focus all stay where
  they were. A refresh is the same path a program change takes — new
  generation, cancel the run in flight, parse, run — with a new input instead
  of a new program. Parses carry their **own** counter (`pgen`) beside the run
  generation, because typing during a long parse bumps the run generation and
  must not discard the input being decoded.
- **Only a whole-file editor source.** An HTTP response is not a file and is
  never followed. A **selection** is not followed either: after an edit its
  character range names a different stretch of the file, so "re-read the
  selection" has no honest answer, and re-querying something the user never
  selected is worse than a snapshot they can refresh by reopening.
- **The buffer is the truth, the event is the trigger.** The refreshed text is
  read from the editor showing the file, after that editor has applied its own
  reload — so a **dirty** buffer, which the editor deliberately does not
  auto-reload (see [the editor's external-change handling](./editor.md)), does not get foreign content
  pushed under its unsaved edits. A digest of the last parsed text makes that
  case, an unchanged rewrite and a disabled auto-reload a no-op rather than a
  wasted parse. IKE's own saves never arrive at all: the watcher suppresses
  them through its save epoch.
- **A broken new input keeps the last result.** A document saved mid-edit — a
  `,` short, a key half written — reports its parse error on the info row and
  raises the [stale banner](#a-failed-run-keeps-the-last-good-result-2412)
  while the previous result stays on screen and the playground stays open. The
  next change that parses picks the mode back up.
- **A removed source ends the mode definitely.** File deleted or renamed away:
  with unsaved edits the buffer is the only copy of the document left, so the
  mode stays up over content that still exists and warns on its status line;
  otherwise the hosting pane is closing with the file, and the playground
  closes with a notification naming it rather than vanishing unexplained.
- **The info row says it happened.** A `reloaded 15:04:05` segment next to the
  input summary, plus the transient status line right after the refresh. The
  stamp outlives the next keystroke — the status line does not, and "is this
  still current?" would be unanswered again by the next key.

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
  `Outputs` are therefore not mutually exclusive.

`halt` ends a run cleanly rather than as a diagnostic, as it does in jq.

### A failed run keeps the last good result (#2412)

**A failing run never touches the result buffer.** While a query is being
typed it is invalid most of the time — `. | {a, b,` is a normal intermediate
state — and clearing the output for every such keystroke defeats the lookup
the playground is most often opened for: *what was that field called again?*
So the model keeps two things apart: `result` is the last **successful**
evaluation, `runErr` the error of the last run. A run that fails writes only
`runErr`; the buffer's text, its scroll position and its find highlights stay
exactly as the last good run left them. This is the same rule the
[external refresh](#following-the-source-file) already applied to a broken
**input** parse, now applied to the program as well — and the input error
still beats the program error on the info row.

The state is marked, not hidden, in two places:

- the info row keeps the `E: …` message, followed by
  `· showing the last good result (n value(s))` — the count is the *good*
  result's, since the failed run's partial output was never installed;
- a one-line **stale banner** in the palette's Warning sits between the info
  row and the result buffer: `stale — the query has an error; showing the last
  good result`, or `… the input has an error` when the snapshot is the broken
  one. It is part of the header for geometry purposes (`playStaleRows` feeds
  both `playHeaderRowsFor` and `sizePlayResult`), so the mouse translation and
  the result editor's height never disagree with what is drawn. The banner
  disappears with the next successful run.

Staleness needs a good result to be stale *about*: before the first successful
run the buffer is empty and no banner is claimed over it. Generation stamps do
the rest — a late result from a superseded run is dropped before any of this,
whether it succeeded or failed, so an old error can never raise a banner over
a newer good result.

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

## The multi-line view

The query line is one row, windowed around the cursor and cut with `…` at both
edges. That is right for typing and wrong for reading: a pipeline like
`.hits.hits[]._source | .keyword as $keyword | .ser[] | select(…) | {…}` is
never on screen as a whole, so the overview is lost exactly where it is needed
— while building a long pipeline (#2032) — and building one blind is how an
afternoon goes into a filter that should have taken minutes (#2038).

**`json.jqQueryView`** (`ctrl+alt+e`, or the palette / Tools menu) toggles the
**multi-line view**: the same program laid out over several rows, in place,
with the program, the cursor, the history position and the result untouched. It
is one mechanism for both halves of the ask — *seeing* the whole expression
(#2032) and *editing* it as a small jq script (#2038) — because a view you can
read but not type in would only be a second mode to leave. It works from the
query line and from the result buffer alike (the mode resolves Global chords it
does not claim, #1983), and the info row's key hints name the chord actually
bound to it — a rebind renames the hint with it.

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

The one-line header stays the **default**: the multi-line view costs result
rows, and the resting layout is the one to type in. It is view state only —
nothing about it reaches the program, the history or the saved filters.

### Editing across the rows

The rows are not a preview: the caret moves through them and the program is
edited wherever it stands (#2038), with the same highlighting, the same
completion popup and the same live run per keystroke as on the one-line query
line — it is one query line with more rows, never a second editor.

| Key | Effect in the multi-line view |
| --- | --- |
| `↑` / `↓` | move the caret one row up / down, keeping its **goal column** |
| `alt+↑` / `alt+↓` | walk the session program history |
| `home` / `end` | start / end of the **caret's row** |
| `ctrl+home` / `ctrl+end` | start / end of the whole program |
| *click* | put the caret on the clicked cell of any query row |

- **The arrows are motion first, history second.** `↑`/`↓` are the history
  walk on the one-line query line (#1973) and rows to walk once there are rows.
  A `↑` on the *first* row — or a `↓` on the last — has no row to go to and
  hands over to the history from there, the way a multi-line shell prompt does;
  `alt+↑`/`alt+↓` reach it from any row. The info row's hints say which meaning
  is in front of the user, so the rebound arrows are never a surprise.
- **The goal column survives a short row.** A run of vertical motions aims for
  the column it started in (`jqPlayState.qgoal`, cleared by every other key),
  so stepping over a short stage and on returns to the column left behind
  instead of dragging the caret to the left edge.
- **A caret on a row's end stays on that row.** `jqplay.RowCol` — not `LineAt`
  — resolves the caret's coordinates: a position on the end of a row the wrap
  broke at a pipe belongs to that row, past its last rune, because the blank
  the break dropped separates the two. Without the distinction `↑` from the row
  below would land on a position that reads as being on the row below again,
  and the motion would never arrive. Where a row was cut at the width the two
  rows touch, and the position is the next row's first cell — a real cell, and
  unambiguous.
- **The window follows the caret.** Past the 8-row cap the rows scroll under
  the caret's row, so editing a stage 20 rows into a program keeps it on
  screen. The rendering, the click mapping and the completion popup's anchor
  all read one layout (`jqQueryWindow`), so the row drawn, the row clicked and
  the row the popup hangs under can never disagree.
- **The popup follows too**: with the view up, the completion list opens under
  the caret's row at the partial's column in it, not under the header's first
  row.

**Line breaks stay a display and editing device — the program is always one
line.** The wrap is recomputed from the program on every render, nothing about
it is stored, and a break is never a rune: the history (`↑`/`↓`), the
seeded last-valid program per file (#1982), the saved filters (#1995) and the
`.http` client's `# @capture` expressions all keep speaking about one-line jq
programs, and a program written across rows here can be pasted anywhere a jq
one-liner goes. A multi-line **paste** is still flattened into the line
(`ui.PasteText`) for the same reason. The alternative — carrying real newlines
— would have bought nothing the pipe-aware wrap does not already give (a
pipeline reads stage per row either way) and would have leaked into every store
that holds a program.

None of these keys is a keymap binding: they are the mode's own, like `tab`,
`enter` and the history arrows before them. Only the *toggle* is a command
(`json.jqQueryView`), because only a toggle is worth reaching from the palette
or rebinding.

## Folding the result

A result is regularly taller than the pane, and reading its *shape* before
opening the interesting branch is what folding is for. Every multi-line object
and array of the result window — every indented block and block scalar of a
YAML one — collapses (#2029, #2039) with the editor's own vim fold keys, from
the result buffer (`tab`):

| Key | Effect |
| --- | --- |
| `za` | toggle the innermost node at the cursor |
| `zc` / `zo` | close / open one level |
| `zM` / `zR` | close every node / open every node |
| `zy` | copy the collapsed node whole (#1787), the `⧉` affordance's keyboard form |

A collapsed node is **one row** carrying a placeholder that names its size —
`"spec": { ⋯ 3 keys }`, `"ports": [ ⋯ 12 items ]`, and in YAML `spec: ⋯ 3 keys`
with no closer, because YAML has none to restore. A block scalar counts its
`⋯ 7 lines`, since "3 keys" over a shell script would be nonsense. The row
still reads as a complete value, and every fold-aware behaviour of an ordinary buffer (`j`/`k`
stepping over a fold as one row, scrolling, the mouse map, a linewise operator
taking the whole fold, #1741) applies unchanged. Folding **nests**: opening a
node reveals one level, with the nodes inside it still folded.

Two deliberate choices behind it:

- **The ranges are the playground's, the folding is the editor's.**
  `Dialect.Folds` scans the rendered result — for JSON
  (`internal/jqplay/fold.go`) a rune walk counting delimiters outside strings,
  for YAML (`yamlfold.go`) a line walk over the indentation — not a re-decode,
  and hands the ranges to the result editor through `SetHostFolds`
  (`internal/editor/hostfold.go`), where they merge over the Tree-sitter ranges
  and win on a shared header line. No second fold engine: the collapsed set,
  the z-commands and every fold-aware motion stay the editor's (#144, #1741).
  Computing the ranges here rather than taking the parse's is what makes the
  result window fold in a **cgo-free build** too (no grammar there) — and only
  the structural scan knows how many *members* a node holds, which is what the
  placeholder says. `SetFoldSummary` is the hook that lets it say "3 keys"
  where a file says "3 lines".
- **A new result is a new document.** `ShowReadOnly` resets cursor, scroll and
  fold state together, and the playground installs the fresh result's ranges
  right after — so a changed filter can never leave a fold of the previous
  result behind, and a fold never outlives the lines it hid.

Both scans read text the playground's *own* encoder wrote, which is what makes
a delimiter walk and a line walk sufficient. The YAML scan only treats a row as
a block header when it cannot be a wrapped scalar — it ends on a colon, it is a
bare dash, it is a `|`/`>` indicator, or it is a `- key: value` entry — so a
long value the emitter broke across rows never folds as if it had members.

Raw output has no folds because there is none: the playground is
document-in / document-out (`jq -r` lives in `raw.go` for the `.http` client,
not in the window).

## Completion

The query line has a typing aid (#1979), synchronous and bounded, with the
candidate logic in `internal/jqplay/complete.go` and the popup in
`internal/app/playcomplete.go`:

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
| `↑` / `↓` | walk the session program history (`↓` past the newest restores the draft) — the program's rows in the [multi-line view](#editing-across-the-rows) |
| `alt+↑` / `alt+↓` | walk the history from any row of the multi-line view |
| `home` / `end` | ends of the program — of the caret's **row** in the multi-line view |
| `tab` | move the keyboard into the result buffer |
| `cmd+f` | search the **result buffer** from here (`editor.find`) — the keyboard moves in with it |
| `ctrl+alt+e` | toggle the [multi-line view](#the-multi-line-view) (`json.jqQueryView`) |
| `pgup` / `pgdn` | page the result buffer without leaving the query line |
| `ctrl+s` | save the program as a **named filter** (`json.jqSaveFilter`) |
| `ctrl+l` | open the **saved-filter picker** over this playground's library (`json.jqFilters` / `yaml.yqFilters`) |
| `ctrl+g` | open the **[language cheatsheet](#the-language-cheatsheet)** of this playground's dialect (`json.jqCheatsheet` / `yaml.yqCheatsheet`) |
| `ctrl+y` | copy the **whole** result (not just the visible part) |
| `ctrl+o` | open the result as a fresh scratch in the dialect's extension (`.json` / `.yaml`) |
| `esc` | close (recording the program in the history) |
| `esc esc` | close **and** open the command palette (#2237) |
| `f1` | the cheatsheet, opened on the playground's own context (#2237) |

`ctrl+alt+e`, `ctrl+g` and `cmd+f` work from the result buffer too.

Result buffer (after `tab`): the **full editor keymap** — motions, search,
folds (`za` / `zc` / `zo` / `zM` / `zR`, see
[Folding the result](#folding-the-result)), visual selection, `y` yank of the
selection — against the read-only
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

The same chord works **from the query line** (#2062). A selection in the
result survives the focus moving back — `tab`, or a click on the query header
— and stays highlighted, so `updatePlaygroundKey` reserves the chord ahead of
the query input whenever the result buffer holds one. Without a selection the
chord falls through to the query line unchanged, so nothing about typing a
program changes.

The spatial focus keys (default ctrl+arrows) work from both the query line and
the result buffer: they move the focus out of the pane with the playground
still mounted (#1980).

### `esc esc` reaches the palette

#2237. The mode's `esc` used to return straight out of the modal routing —
which sits well above the double-esc detector in `Update` — so the first press
left the playground and the second one found nothing armed: the palette was
simply unreachable from the query line, and the key felt broken rather than
scoped.

`leavePlaygroundOnEsc` closes the mode **and arms the detector**, which is the
shape the rest of the app already uses: the single `esc` keeps its immediate
meaning (no waiting out a chord timeout on the one key pressed most), and the
second `esc`, now landing in an ordinary focus, opens the palette. Both focuses
go through it — the query line's `esc` and the result buffer's resting-normal
`esc`. An `esc` the [completion popup](#completion) consumes as a dismissal
does *not* arm it: it is that popup's key, exactly as the editor's insert-mode
`esc` is insert mode's.

### Code actions say so instead of doing nothing

#2237. `alt+enter` (`lsp.codeAction`) is bound in the **editor** context, so
the mode's routing kept it from the keymap layer and the Global fallback
(#1983) never matched it — the key did a silent nothing, the single most
confusing thing a key can do. There is nothing behind it worth wiring: a jq
program in a query line and a throwaway read-only result have no language
server, no document and no diagnostics.

So the playground answers the chord instead of dropping it, from either focus:
the info row says *no code actions in the playground* and names the two keys
that do the nearest thing it actually has — `ctrl+space` for completion and
`ctrl+l` for the filter library. The line renders in the status row's **warning**
colour rather than its success green (`playState.statusWarn`); "that key does
not apply here" is not a confirmation. Like every status it clears on the next
key.

### `cmd+f` searches the result from either focus

#2383. Searching a result used to cost three keys' worth of focus work: `tab`
into the result buffer, `/`, and `tab` back. `cmd+f` — the search key
everywhere else in the IDE — did nothing at all, for the same reason
`alt+enter` did: `editor.find` is bound in the **editor** context, and the
mode's routing keeps editor-context chords from the keymap layer.

`playFindChord` recognizes it the way `playCopyChord` recognizes the copy chord
— resolved against the editor context in the **live** binding table, so a
rebound `editor.find` is the chord that works here too; a hard-coded `cmd+f`
would be a lie to anyone who changed it. `beginPlayResultSearch` then does one
thing from both focuses: it moves the keyboard into the result buffer and sends
the buffer an `editor.ActionMsg{Action: "find"}` — the very action `/` triggers
there.

**The focus goes into the result buffer, and stays there.** It has to: the
search prompt needs the typing, and `n` / `N` afterwards are the result
buffer's keys. Bouncing back to the query line after the first match would make
the shortcut useful for exactly one match and then wrong for every following
one, and it would leave the two starting focuses behaving differently. So
closing the search with `esc` also leaves the keyboard in the result buffer —
the same place, whichever focus the search was started from — and one more
`esc` from resting normal mode closes the mode as always. `tab` is the way
back to the query line, unchanged.

`/` in the result buffer is untouched: the chord is a shortcut past the `tab`,
not a replacement. The buffer stays read-only (#1762) — a search is a motion —
and the program, the history and the result are not involved at all. The
cheatsheet lists the chord in **both** focus tables (`playhelp.go`), resolved
live from the binding table like the chords in the third group, since a key
that works from two places and is documented in neither is a key nobody finds.

### The cheatsheet knows the playground

#2237, on top of the context-sensitive help (#2182). The playground owns the
keyboard but advertises no pane context and its keys belong to no registered
command, so `f1` used to open on the *editor's* bindings — none of which apply
while the mode is up — and never mentioned the history, the completion popup or
the filter library at all.

`helpContext` reports `playground` while the mode is focused (help only — the
keymap layer, palette scoping and the mode indicator keep the plain
`focusContext`), and `internal/app/playhelp.go` contributes three groups:
the query line, the result buffer, and the keymap-bound chords that still reach
the mode, the last resolved live from the binding table so a rebind is
reflected rather than a default advertised. All three are flagged `Focused`, so
`help.withExtraLeading` puts them at the **head** of the context view, ahead of
the global bindings, exactly where a focused pane's registered scope would sit.
With the mode closed — or the focus elsewhere — they contribute nothing.

The info row's hint tail ends in `f1 keys`, last so it is the first segment a
narrow pane drops: the row can only ever name a handful of keys, and the one
worth naming last is the one that lists all of them.

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

A bracketed paste lands in the query line, **flattened** to one line, like
every other single-field prompt — the result buffer refuses pastes with
everything else. It lands there only while the playground's own pane holds the
focus, though: the mode stays mounted when the focus moves (#1980, see
[The inline mount](#the-inline-mount)), and the paste router follows the key chain
rather than the mounted mode, so an open [popup terminal
layer](./terminal.md) — box or floating panel — takes it instead, and a focused
editor or tool pane takes its own (#2236).

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

It is **one session-wide list, shared by every playground and both dialects**
(#2039) — a program run over a `.http` response is offered by `↑` in a `.json`
buffer, and a yq program by `↑` in the jq playground, because the mode was
never the thing that owned it and the language is the same one either way. That
is the opposite call from the saved filters, deliberately: the history is
unnamed scratch work where a stale entry costs one `↑`, a library is a curated
list where a foreign entry costs attention on every open. The open mode holds
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

A saved filter is a **name and a program**, in one of two scopes — and since
#2039 in one of two dialects, which is a third file name rather than a flag on
the entry (see [the yq dialect](#the-yq-dialect) for why):

| Scope | File (jq) | File (yq) | For |
| --- | --- | --- | --- |
| **project** | `.ike/jqfilters.json` | `.ike/yqfilters.json` | filters shaped by *this* project's data — next to the HTTP environment selection |
| **global** | `~/.ike/jqfilters-global.json` | `~/.ike/yqfilters-global.json` | filters that are about the language, not about a project — next to the [saved window layouts](./pane-layout.md) |

Every file follows the `IKE_CONFIG_DIR` redirection seam every other state
store uses, under **distinct file names** (the `winsize.json` /
`winsize-global.json` precedent, #1714), so redirecting one directory still
yields four libraries. A missing or malformed store loads as an empty library —
the playground must open even when a hand-edited file is broken — and entries
with an empty name or program are dropped on the way in.

The store is a *file per scope and dialect*, not a config key. A jq program is
data the user creates from inside the IDE, like a saved layout; adding one by
hand-editing `settings.toml` would be the wrong affordance, and TOML lists
*replace* across the config layers, so a project file would hide the user's
whole library instead of adding to it.

### Saving

`ctrl+s` on the query line (or `json.jqSaveFilter`, "Save Playground Filter…")
opens a one-line name prompt over the current program; it lands in the **open
playground's** dialect, since that is what the program was written against.
`tab` toggles the scope the save goes to,
which starts on **project** — a filter written against this project's data
usually belongs to it, and promoting it is one keystroke. The identity program
is refused: `.` is the playground's default, not a filter. A name already taken
**in the target scope** holds the prompt open for a second `enter`, the
save-layout store's guard (#1175) — and that confirmed overwrite is also how a
filter is *edited*: insert it, change it on the query line, save it under the
same name.

### The picker

`ctrl+l` (or `json.jqFilters` / `yaml.yqFilters`) opens a locked palette mode
listing **both** scopes of one dialect, project first — the chord takes the
open playground's, each command its own:

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
  — or with the *other* dialect's open — the command still completes: it opens
  one of the filter's own dialect over the document at hand first (and says so
  when there is nothing to query).
- `shift+delete` (or `cmd+backspace`) **deletes** the row's filter from its own
  store and refreshes the list in place — the palette's aux convention (#1113).
- `json.jqRenameFilter` / `yaml.yqRenameFilter` opens the same picker in its
  **rename** spelling, where `enter` opens the name prompt over the entry
  instead of inserting it. Renaming onto a taken name is refused: only the
  confirmed save overwrite may replace a filter, never a rename that happens to
  collide.

The list is re-read from both files on every open and after every delete, so a
library changed by another window — or by hand — is never stale.

## The language cheatsheet

#2382. The keyboard has been documented since #2237; the **language** was not.
Someone who does not already know jq could open the playground, see a query
line, and have no way to find out that `group_by` exists — let alone what a
program that uses it looks like. The completion popup only offers what you
have already half-typed, so it finds what you know and nothing else.

`ctrl+g` from either focus (or `json.jqCheatsheet` / `yaml.yqCheatsheet` from
the palette and the Tools menu) opens a **locked palette mode** listing the
whole language of the open playground's dialect, in three sections:

| Badge | What | Where it comes from |
| --- | --- | --- |
| `syntax` | the parts of the language that are **not** functions: the pipe, `.[]`, `.[]?`, slices, `,`, object and array construction, `//`, string interpolation, `as`, `reduce`, `if`, `try`, `=` / `\|=`, `..` | authored in `cheatsheet.go` |
| `example` | complete **one-line programs** for the everyday operations: pick a field, iterate, filter, map, sort, group, count per group, rebuild an object, walk nested paths, deduplicate, default a missing value, interpolate | authored in `cheatsheet.go` |
| `builtin` | **every** function the engine accepts, with its arities and, where one is curated, its one-line description | `jqplay.Builtins()` + `builtinDocs` |

The builtin section is **generated, never hand-listed**. A second copy of the
function list would drift from the engine's the first time gojq gained one, and
the whole point of the sheet is that it tells the truth about *this*
playground — it is the same list `builtins` prints and the same list the
completion popup offers, so the two views of a function never disagree. The
long tail without a curated description is still listed: knowing that
`truncate_stream` exists is worth more than the blank where its sentence would
go, and the arity note still marks it as a function. #2382 also **grew
`builtinDocs`** by some sixty commonly reached-for names it was missing
(`ceil`, `walk`, `objects`, `setpath`, `strftime`, the type filters, …), which
the completion popup gets for free.

### Every example is a tested program

`cheatsheet_test.go` evaluates **every authored program against a sample
document that lives next to it** (`jqplay.Sample`), and fails on a compile
error, a runtime error or an empty result. A typo in an example would otherwise
sit in the reference forever, teaching the language wrong to exactly the reader
who cannot tell — and prose about `map` is much easier to keep honest than a
program is. The sample is a small `{users, meta, counts}` document in both
languages, so a single list of programs serves both dialects; the placeholder
line names it, so `.users[]` is not a field out of nowhere.

### The dialect decides, and shows only one side

The sheet's title, its placeholder and its document-language rows follow the
**open playground's dialect** — never both side by side, which would make the
sheet something to filter rather than something to read. Almost every row is
shared (yq speaks jq for everything typed into a query line), and the handful
that are genuinely about the document language are written twice, once per
dialect:

- jq only — *several values in one buffer* (a `.jsonl` stream), *a number keeps
  its exact spelling* (a 19-digit id survives), *a JSON string field as data*.
- yq only — *several documents in one file* (`---`), *aliases and merge keys
  are resolved* (the decoder expands them before the program runs), *a JSON
  string inside the YAML*.

### Picking a row

`enter` writes the row into the query line, in the shape the row actually is:

- a **syntax or example** row is a whole program, so it **replaces** the query
  line and runs, the way an inserted saved filter does. The program it replaced
  is put in the **history first**, so `↑` brings it straight back: looking
  something up must never be able to cost the work on the line. The status row
  says the example's field names come from the sample document — a program
  erroring with no explanation would read as a broken cheatsheet.
- a **builtin** row is half a program, so its **name lands at the caret**,
  exactly like accepting a completion. Replacing `.users | ` with `group_by`
  would be the wrong half.

Unlike the filter picker the sheet never *opens* a playground: a saved filter
is written against the user's own documents and inserting one is a complete
action anywhere, while an example is written against the sample document, and
opening a playground over an unrelated buffer just to run `.users[]` against it
would produce a red error line and call it a feature. With no playground up the
sheet is still readable — it is a reference — and inserting says which
playground to open.

### Why the palette

The playground already has two ways to show a list, and the sheet fits neither
badly enough to justify a third. The **completion popup** is anchored on the
caret and sized for eight rows; the sheet is several hundred rows long, and the
one thing the issue insists on is that it be searchable rather than paged. The
**palette** already *is* that — one fuzzy-matched, scrollable, mouse-aware list
with a query line — and it is the same doorway `ctrl+l` uses for the other body
of programs, so both land in one place instead of in two unrelated widgets. It
also composes with the mode for free: opening the palette does not touch the
playground, so the query, the result and the history are all still there when
`esc` closes it.

Rows are matched over the **title plus its description**, because the sheet is
browsed by what one wants to do ("sort an array by a field"), not by the name
of a function one does not know yet. A query matching no label is retried
against the **program**, ranked below every title hit, so half-remembering
`from_entries` still finds the example that uses it.

`ctrl+l` and `ctrl+g` stay **disjoint on purpose**: the library is where the
user's *own* programs live, the sheet is where the *language* lives. Neither
ever lists the other's rows.

Neither command carries a default keybind: like the library's, they are
single-key bindings inside the owning mode and are recorded that way in the
unbound-command audit ledger (`cmd/ike/keybind_audit_test.go`, #2305).

## Boundaries

- **No raw output mode** (`jq -r`), no `--slurp`, no `--arg`: the playground is
  a document-in/document-out tool, and every one of those would be a mode the
  result actions then have to reason about.
- **No yq-only operators.** The yq dialect speaks jq, not mikefarah's
  extensions: no comment or anchor *preservation*, no `style`/`tag` operators,
  no in-place edit. The playground reads documents; a program that needs to
  rewrite one is a `yq` invocation in a terminal.
- **Two dialects, one open playground.** The mode replaces a pane's content,
  and that is one pane's worth of content; opening either closes the other.
- **No live re-read of the buffer.** #2356 narrowed this boundary to what it
  was always about: the snapshot is not re-read *per keystroke*. It **is**
  re-read when the source file changes on disk (see [Following the source
  file](#following-the-source-file)). Unsaved edits in the buffer are still not
  followed — nothing has happened yet that the watcher, or anybody else, could
  call a change.
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
- **The cheatsheet is a reference, not a tutorial.** It says what a construct
  does in one line and shows one program that uses it; it does not explain jq.
  Nor does it document `jq -r`, `--slurp` or `--arg`, which the playground does
  not have — a sheet listing what the tool cannot do would be worse than none.
- **No second builtin list, ever.** The function rows are `Builtins()`; the
  descriptions are `builtinDocs`, the map the completion popup already reads.
  Anything else would be a copy with its own decay schedule.
- **The history is still not persisted.** #1995 gives the durable programs a
  *name* and a file; the anonymous ones stay in memory. Persisting the history
  too would put every half-typed experiment into the project state, which is
  exactly the noise the library exists to separate out.
