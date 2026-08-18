---
type: concept
title: Regex Tester
description: Floating pattern + test-text dialog with live RE2 evaluation — matches highlighted in the text, capture groups of the selected match listed, compile errors inline, session pattern history and quoted copy.
resource: internal/regextest/regextest.go
tags: [architecture, regex, tools, floating, modal]
timestamp: 2026-08-18T00:00:00Z
---

# Regex Tester

#1937. IKE's answer to JetBrains' **Check RegExp**: a floating dialog holding a
pattern and a block of test text, re-evaluated on every keystroke, with all
matches highlighted in the text and the capture groups of the selected match
listed underneath. It exists because building the regexes IKE itself consumes —
problem matchers, highlight rules, HTML/log extractors — otherwise means
leaving the IDE for an external tester or a throwaway script.

Opened by the **`tools.regexTester`** command (command palette, Tools menu; no
default chord). With a visual selection open in the focused editor, the
selected lines prefill the test area, so the pattern is tried against the lines
at hand rather than against retyped ones.

## Structure

```
internal/regextest/
  regextest.go   evaluation core: Evaluate, LineSpans, Quote, History — pure, no UI state
internal/app/
  regextester.go the dialog: fields, cursor, rendering, key routing, async evaluation
  commands.go    tools.regexTester → OpenRegexTesterMsg
```

The split is the usual one: everything interesting — match and group
extraction, span mapping, quoting, history — is pure and testable in
`internal/regextest`; `internal/app` owns the terminal.

- **`Evaluate(pattern, text)`** compiles the pattern and collects its matches
  with their capture groups. An empty pattern is *idle*, not an error: it
  compiles fine and would match the empty string everywhere, which is noise
  while the user is typing. A compile failure returns the error message (with
  regexp's `error parsing regexp:` preamble trimmed) and no matches. The scan
  stops at `MaxMatches` (5000) and says so, bounding the work on a pattern like
  `.?` over a large paste.
- **Groups** carry index, name (empty when unnamed), byte range, value and a
  `Set` flag. `Set` distinguishes a group that did not participate in the match
  — `(a)|(b)` always leaves one unset — from one that matched the empty string;
  the dialog renders the former as `(no match)`.
- **`LineSpans(text, matches)`** maps the matches' byte offsets onto per-line
  **rune columns**, splitting a match that crosses a line break into one span
  per line. That is the same shape as the editor's search-highlight spans, so
  the renderer colors cells without knowing about offsets. Zero-width matches
  produce no span (there is nothing to color) but still count.

## RE2, stated on screen

Matching is Go's `regexp` — **RE2**, not PCRE — and the dialog says so on a
permanent line: no backreferences, no lookaround, linear time in the input. A
user arriving from a PCRE tool would otherwise read `missing argument to
repetition operator` as an IKE bug. The inline flags `(?i)`, `(?m)` and `(?s)`
work exactly as RE2 defines them.

Linear time is also why live evaluation is safe at all — there is no
catastrophic backtracking to freeze the UI. The remaining risk is the constant
factor on a very large text, so evaluation runs **inline up to
`AsyncThreshold` (16 KiB) and off the event loop past it**: a generation-stamped
`tea.Cmd` whose `regexEvalDoneMsg` is dropped when a newer keystroke already
superseded it.

## The dialog

Hosted in the root's floating shell as a `ui.Content` (so the body is laid out
to the shell's width budget), like the clone and new-project prompts. It owns
the keyboard while it is open.

| Key | Effect |
| --- | --- |
| `tab` / `shift+tab` | switch between the pattern line and the test-text area |
| `enter` | in the pattern: record it in the history and move to the test text; in the text area: split the line |
| `↑` / `↓` (pattern) | walk the session pattern history |
| `ctrl+n` / `ctrl+p` | select the next/previous match — the one whose groups are listed |
| `ctrl+o` | cycle the copy format: Go raw, Go, TOML, JSON |
| `ctrl+y` | copy the pattern as a literal in that format |
| `esc` | close (recording the pattern in the history) |

Everything else is ordinary line editing (`ui.EditKey`), and a bracketed paste
routes here through `routeOverlayPaste` — the **only** prompt that keeps a
paste's line breaks, since a multi-line log excerpt is exactly what the test
area is for.

The test area is a plain `[]string` with a `(line, col)` cursor, not an editor
pane: the tester is scratch space for a pattern, so there is no undo, no vim
mode and no file behind it. It shows a window of 12 rows that scrolls with the
cursor, and clips long lines horizontally, so a large prefill cannot grow the
shell past the terminal.

Rendering marks the **selected** match in the theme's selection colors and the
other matches in the muted selection color, so "which match are these groups
from" reads at a glance.

## Copy formats

A pattern is written once and pasted somewhere that needs it escaped, so the
copy offers the forms that matter here:

- **Go raw** (`` `\d+` ``) — the default, because backslashes stay literal;
  falls back to the quoted form when the pattern contains a backtick.
- **Go** (`"\\d+"`) — interpreted string literal.
- **TOML** (`'\d+'`) — the literal string IKE's own config takes for the
  regexes it embeds, e.g. a `[[tasks.matcher]]` rule
  ([Tasks & Problem Matchers](/architecture/tasks.md)); falls back to a basic
  string when the pattern contains a single quote.
- **JSON** (`"\\d+"`) — for the VS Code-style configs that embed matchers as
  JSON.

## History

Patterns are remembered **per session, in memory only** (newest first, repeats
moved to the front, capped at 50). A regex under construction is scratch work;
persisting it into the project state would be noise. The history lives on the
root model, not on the dialog, so it survives closing and reopening the tester.

## Boundaries

- No replace preview, no split-per-flag toggles, no PCRE compatibility layer:
  the tester tests what IKE's own matching engine does.
- Not a pane. It is modal by design (a pattern is written, tried, copied and
  forgotten); a persistent pane is only worth it if persistent use emerges.
- Highlighting inside the *editor* from a tester pattern is out of scope — the
  editor's own search (`/`) already owns that.
