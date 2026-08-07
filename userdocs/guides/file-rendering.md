# How files are rendered

Every buffer goes through the same two layers before it reaches the screen:
Tree-sitter **syntax highlighting**, and — for the formats where raw text is
hard to read — a **rendering layer** that styles the structure and *conceals*
the markup that carries it.

All screenshots on this page use the `monokai-pro` theme.

## Syntax highlighting

Highlighting is Tree-sitter based, not regex based: the file is parsed, and the
colours come from the syntax tree. That is why a keyword inside a string stays
a string, and why the highlighting survives half-typed code.

![A Go file with syntax highlighting in the full IDE window](../screenshots/features/syntax-go.png)

Every bundled language is highlighted the same way — the grammar changes, the
mechanism does not:

![A Python file with syntax highlighting](../screenshots/features/syntax-python.png)

![A TypeScript file with syntax highlighting](../screenshots/features/syntax-typescript.png)

Which languages are bundled, and how to add one, is covered in
[Code intelligence](code-intelligence.md).

## Conceal, and why the caret reveals

The rendering layers hide the characters that only exist to carry structure:
the `**` around bold text, the commas between CSV fields, the ANSI escape
bytes in a log. Hiding them is safe because nothing is rewritten — the file on
disk is untouched, and the hidden text comes back the moment you need it:

**the span the caret is in renders raw.** Move onto a bold word and its `**`
markers reappear; move away and they are gone again. So you can still edit the
markup, and you never edit blind.

## Markdown

`editor.markdown_rendering` styles inline spans, conceals the markers, and
draws pipe tables with box characters. Raw source first:

![A Markdown file with rendering turned off](../screenshots/features/markdown-raw.png)

The same file rendered:

![The same Markdown file rendered](../screenshots/features/markdown-rendered.png)

And with the caret inside the `**order events**` span on line 3 — only that
span shows its markers:

![Markdown conceal revealing the markers under the caret](../screenshots/features/markdown-reveal.png)

Toggle it for the current view with **Toggle Markdown Rendering** from the
palette; `editor.markdown_rendering` sets the default.

## CSV and TSV

`editor.csv_rendering` turns a delimited file into a table: fields aligned into
columns, each column its own colour, the header row pinned while you scroll,
and the separators concealed. Raw:

![A CSV file with table rendering turned off](../screenshots/features/csv-raw.png)

Rendered:

![The same CSV file rendered as an aligned table](../screenshots/features/csv-rendered.png)

The alignment is display-only. Column widths never reach the file, and typing
in a cell edits the plain text underneath.

## Logs

`editor.log_rendering` makes a `.log` file readable: severities coloured,
timestamps dimmed, thread and logger names rainbow-coloured so one component's
lines are easy to follow, `key=value` pairs split into key and value, and ANSI
escape sequences drawn as the styles they mean — with the escape bytes
themselves concealed. Raw, escape bytes and all:

![A log file with log rendering turned off](../screenshots/features/log-raw.png)

Rendered:

![The same log file rendered](../screenshots/features/log-rendered.png)

The escapes follow the same positional-reveal rule as every other conceal.
With the caret on the coloured line, its escape bytes are back:

![The caret revealing the ANSI escape bytes on one line](../screenshots/features/log-escape-reveal.png)

Polling services write the same line over and over. A run of consecutive
identical lines collapses into the first one with a dimmed `×N` marker counting
the run — lines only differing in their timestamps count as repeats. Move the
caret onto the collapsed line and the whole run comes back, the same positional
reveal the escapes use.

At the right edge of each line sits the time elapsed since the previous one —
`+450ms`, `+2.1s`, `+30s` — so stalls and slow operations are visible without
comparing timestamps by eye. A gap that is large for *this* file (ten times its
usual cadence) is drawn in the warning colour, so a hang stands out. Lines
without a readable timestamp show no hint and do not break the chain: the next
timestamped line still measures from the last real one, which is what makes a
stack trace's total cost readable. The hint only appears where the line leaves
room for it, so it never covers text.

Toggle it per view with **Toggle Log Rendering**.

## Turning it off

Each layer is one setting, and each has a palette command that flips it for the
current view only:

| Layer | Setting | Per-view toggle |
|---|---|---|
| Markdown | `editor.markdown_rendering` | Toggle Markdown Rendering |
| CSV / TSV | `editor.csv_rendering` | — |
| Logs | `editor.log_rendering` | Toggle Log Rendering |
| Whitespace | `editor.show_whitespace` | Toggle Whitespace Rendering |

The [settings reference](../reference/settings.md) has the full list, including
the highlighting limits that apply to very large files.

## Related

- [The modal editor](../concepts/modal-editor.md) — moving the caret that
  drives the reveal
- [Code intelligence](code-intelligence.md) — language servers on top of the
  highlighting
- [Git](git.md) — the gutter markers and the diff viewer
