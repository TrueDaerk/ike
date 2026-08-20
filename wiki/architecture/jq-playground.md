---
type: concept
title: jq Playground
description: Inline jq query line mounted in the pane it queries — the pane's body becomes a read-only editor buffer holding the live result; gojq as the engine, debounced generation-stamped evaluation, inline compile/runtime errors, result cap, copy and open-as-scratch, one session-wide program history shared by every buffer and response pane, and a completion popup offering the snapshot's keys after a dot and gojq's builtins on an identifier.
resource: internal/jqplay/jqplay.go
tags: [architecture, json, jq, tools, inline, editor, http, completion]
timestamp: 2026-08-20T00:00:00Z
---

# jq Playground

#1936, inline since #1970. JSON is everywhere in the daily workflow — `.http`
responses, Elasticsearch hits, parser output, fixtures — and until now the only
way to run a jq program against one was to leave the IDE. The playground is a
**query line mounted inline in the pane holding the JSON**, with the pane's
body replaced by a **read-only editor buffer showing the program's live
output**: no floating dialog, the result reads and navigates like any buffer.

Opened by the **`json.jqPlayground`** command (command palette, Tools menu; no
default chord), on JSON editor buffers and the HTTP response pane alike.
`esc` leaves the mode; the pane's own content was never touched, so leaving
restores it bit-identically — editability included.

## Structure

```
internal/jqplay/
  jqplay.go      evaluation core: Parse, Run, Evaluate, Result, History — pure, no UI state
  highlight.go   the query line's jq scanner: Tokens/KindAt, single pass, never fails
  complete.go    the typing aid: Complete — snapshot keys at a path, gojq's builtin list
internal/app/
  jqplayground.go the inline mode: query header, result buffer, key routing, debounce and async eval
  jqcomplete.go   the completion popup: state, keys, rendering and compositing
  commands.go     json.jqPlayground → OpenJQPlaygroundMsg
```

The split is the usual one: everything interesting — parsing, running, error
and cap handling, colorization, history — is pure and testable in
`internal/jqplay`; `internal/app` owns the terminal.

## The inline mount

While the mode is active on a pane:

- The pane's first two content rows are the **query header**: the colored
  query line, then one info row (input origin and size, result summary, key
  hints — or the error, or a transient status). The header height is fixed, so
  an error appearing mid-keystroke never resizes the buffer below it.
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

The query line is **prefilled with the caret's jq path** (`.spec.containers[2].name`,
from the [path breadcrumb](./editor.md) of #1660) when the buffer has one, so
"the value I was looking at" is already a program; otherwise it opens on `.`,
which pretty-prints the input.

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
| `pgup` / `pgdn` | page the result buffer without leaving the query line |
| `ctrl+y` | copy the **whole** result (not just the visible part) |
| `ctrl+o` | open the result as a fresh `.json` scratch |
| `esc` | close (recording the program in the history) |

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
state would be noise. The history lives on the root model, not on the mode
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
just recorded, and reopening over the same caret seeds the path that was last
run — so without it the first `↑` would visibly change nothing and the history
would read as broken.

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
  switch or `esc`. A program is written, tried, copied and forgotten — a
  persistent pane is only worth it if persistent use emerges.
- **The result buffer is a substitute, not the document.** The hosting pane's
  own component keeps its entire state and is simply not rendered; the mode
  never mutates it, which is what makes `esc` a perfect restore.
- **No settings.** The caps are the safety net, not a preference; exposing them
  would invite raising them past what the pane can render.
