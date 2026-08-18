---
type: concept
title: jq Playground
description: Floating jq query line over a JSON buffer or HTTP response body with a live result view — gojq as the engine, debounced generation-stamped evaluation, inline compile/runtime errors, result cap, copy and open-as-scratch, session program history.
resource: internal/jqplay/jqplay.go
tags: [architecture, json, jq, tools, floating, modal, http]
timestamp: 2026-08-18T00:00:00Z
---

# jq Playground

#1936. JSON is everywhere in the daily workflow — `.http` responses,
Elasticsearch hits, parser output, fixtures — and until now the only way to run
a jq program against one was to leave the IDE. The playground is a **floating
query line over a JSON snapshot with the program's output live underneath**:
the `/`-filter pattern of the [Data Viewer](./data-viewer.md) (#1777) and the
[Regex Tester](./regex-tester.md) (#1937), applied to jq.

Opened by the **`json.jqPlayground`** command (command palette, Tools menu; no
default chord).

## Structure

```
internal/jqplay/
  jqplay.go      evaluation core: Parse, Run, Evaluate, Result, History — pure, no UI state
  highlight.go   the query line's jq scanner: Tokens/KindAt, single pass, never fails
internal/app/
  jqplayground.go the dialog: query line, result window, key routing, debounce and async eval
  commands.go     json.jqPlayground → OpenJQPlaygroundMsg
```

The split is the usual one: everything interesting — parsing, running, error
and cap handling, colorization, history — is pure and testable in
`internal/jqplay`; `internal/app` owns the terminal.

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
dialects to explain in one dialog.

## What is queried

The input is **snapshotted and parsed once** when the playground opens — a jq
program is written against the document that was on screen, and re-parsing a
10 MB response on every rune would make the query line stutter for no gain. The
snapshot is resolved in this order:

1. the **focused HTTP response pane**'s body ([HTTP Client](./http-client.md)) —
   the pane the user is looking at is the one they mean;
2. the focused editor's **visual selection**, so a JSON blob embedded in a log
   file is queryable without extracting it first;
3. the focused editor's **whole buffer**;
4. a visible-but-unfocused HTTP response pane — the usual focus right after a
   dispatch is the editor holding the `.http` file.

With none of those the command notifies instead of opening an empty dialog.

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
  result superseded by a newer keystroke — or one arriving after the dialog
  closed — is dropped instead of overwriting the current one.
- `enter` and the initial evaluation skip the debounce; the result is wanted
  now, not a tick later.

Three independent bounds keep a hostile program from hanging the IDE, because
no single one covers all of them:

| Bound | Constant | Catches |
| --- | --- | --- |
| Output count | `MaxOutputs` (500) | `range(infinite)`, `repeat(0)` |
| Output size | `MaxResultBytes` (256 KiB) | few values, each enormous |
| Wall clock | `EvalTimeout` (5 s) | `def f: f; f` — loops emitting *nothing* |

A capped run is **not an error**: the result header says `(stopped at 500)` and
the values collected stand. Opening the playground over an input larger than
`AsyncThreshold` (64 KiB) parses off the loop too, so even the open is not a
stall.

## Errors are inline, never a crash

Everything that can go wrong shows on one error line under the query, in the
theme's error color:

- an **input** that is not JSON (which beats a program error — with no parsed
  input no program could have run);
- a **compile** error from gojq (`unexpected token`, `function not defined:
  wat/0`, `variable not defined: $x`), which produces no output;
- a **runtime** error, which may arrive *after* some values were produced —
  `.[] | .x` over `[{"x":1},3]` prints one value and then fails. `Err` and
  `Outputs` are therefore not mutually exclusive, and the header says
  `… before the error`.

`halt` ends a run cleanly rather than as a diagnostic, as it does in jq.

## Syntax highlighting

The query line is colorized by a **single-pass rune scanner** (`jqplay.Tokens`),
not a parser. A live query line spends most of its time holding a program that
does not parse — an unterminated string, a half-typed `map(` — which is exactly
when the color helps most, so the scanner never fails: an unterminated string
runs to the end of the line, an unknown rune is punctuation. It classifies
paths, strings, numbers, keywords, functions, `$variables`, `@formats`,
operators and comments, mapped onto the **chrome** palette (Accent, Success,
Info, Secondary, Warning, Hint) rather than the editor's capture colors — this
is a dialog over the shell surface, not a buffer.

## Keys

| Key | Effect |
| --- | --- |
| *(typing)* | edit the program; each change re-evaluates, debounced |
| `enter` | record the program in the history and run it now |
| `↑` / `↓` | walk the session program history |
| `ctrl+n` / `ctrl+p` | scroll the result one row |
| `pgdn` / `pgup` | scroll the result one page |
| `ctrl+y` | copy the **whole** result (not just the visible window) |
| `ctrl+o` | open the result as a fresh `.json` scratch |
| `esc` | close (recording the program in the history) |

Everything else is ordinary line editing (`ui.EditKey`). A bracketed paste is
**flattened** into the one-line query, like every other single-field prompt.

The result area is read-only text: editing output belongs in a buffer, which is
exactly what `ctrl+o` makes — a [scratch file](./scratch-files.md) opened
through the standard funnel, so highlighting, folding and the path breadcrumb
all apply, and the playground can be run again over the result. That is how a
multi-step jq session actually goes.

## History

Programs are remembered **per session, in memory only** (newest first, repeats
moved to the front, capped at 50), like the regex tester's patterns: a jq
program under construction is scratch work, and persisting it into the project
state would be noise. The history lives on the root model, not on the dialog,
so it survives closing and reopening the playground.

## Boundaries

- **No raw output mode** (`jq -r`), no `--slurp`, no `--arg`: the dialog is a
  JSON-in/JSON-out playground, and every one of those would be a mode the
  result actions then have to reason about.
- **No live re-read of the buffer.** The snapshot is taken at open; editing the
  file underneath and re-running means reopening the playground.
- **Not a pane.** Like the regex tester it is modal by design — a program is
  written, tried, copied and forgotten. A persistent pane is only worth it if
  persistent use emerges.
- **No settings.** The caps are the safety net, not a preference; exposing them
  would invite raising them past what the dialog can render.
