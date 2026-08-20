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

The same mechanic carries a whole family of *decoded* stand-ins — epoch
timestamps drawn as dates, byte counts as `10 MiB`, `\uXXXX` escapes as the
characters they name, an octal mode as `rw-r--r--`, a certificate as a one-line
summary. [Conceal and decoded values](conceal.md) documents every one of them,
with the settings that control them.

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
`+450ms`, `+7s 300ms`, `+30s` — so stalls and slow operations are visible
without comparing timestamps by eye. The hints of a file share one column
layout: seconds under seconds, milliseconds under milliseconds, so the column
can be swept by eye instead of read row by row. A gap that is large for *this* file (ten times its
usual cadence) is drawn in the warning colour, so a hang stands out. Lines
without a readable timestamp show no hint and do not break the chain: the next
timestamped line still measures from the last real one, which is what makes a
stack trace's total cost readable. The hint only appears where the line leaves
room for it, so it never covers text.

Toggle it per view with **Toggle Log Rendering**.

### Rotated logs as one timeline

A rotated log is one log split across files: `app.log` holds the last hour,
`app.log.1` the hour before, `app.log.2.gz` the day before that. Opening one of
them says how many more files belong to it, and **Open Rotated Log Set (Merged
Timeline)** puts the whole set into a single read-only buffer, oldest lines
first. Compressed members are read decompressed, and each region opens with a
separator naming the file it came from:

```
──── app.log.2.gz ────
2026-08-08 08:00:00 INFO  compressed line
──── app.log.1 ────
2026-08-09 09:00:00 INFO  yesterday
──── app.log ────
2026-08-10 10:00:00 INFO  live
```

Everything the log rendering does keeps working across the whole timeline —
severities, deltas, repeat collapsing, search — so a question that spans a
rotation ("what happened around midnight") is one buffer and one search away.
The elapsed-time hint on a region's first line measures against the last
timestamped line above it, which is exactly the gap across the boundary.

**Toggle Follow (Tail -f)** works on the timeline too: it tails the newest file
of the set, and when the log rotates under it — the live file moving to
`app.log.1`, a new one taking its place — the set is merged again and the tail
continues, so nothing is lost at the boundary. Run the command again to refresh
a timeline you are not following.

The set is found next to the file: same directory, same name, a numeric or date
suffix (`.1`, `.2026-08-01`, `.20260801`) with an optional `.gz`. Very large
sets are cut at the [large-file
limits](../reference/settings.md) — from the *oldest* end, so the lines next to
the live log always survive, and a toast says what was left out.

## Turning it off

Each layer is one setting, and each has a palette command that flips it for the
current view only:

| Layer | Setting | Per-view toggle |
|---|---|---|
| Markdown | `editor.markdown_rendering` | Toggle Markdown Rendering |
| CSV / TSV | `editor.csv_rendering` | — |
| Logs | `editor.log_rendering` | Toggle Log Rendering |
| Whitespace | `editor.show_whitespace` | Toggle Whitespace Rendering |

Those are the three *markup* layers. The decoding families — timestamps,
numbers, escapes, cron, file modes, certificates, secrets — have one switch
each as well; they are listed in
[Conceal and decoded values](conceal.md#the-settings).

The [settings reference](../reference/settings.md) has the full list, including
the highlighting limits that apply to very large files.

## Related

- [Conceal and decoded values](conceal.md) — every conceal family in depth,
  and the reveal rules they share
- [The modal editor](../concepts/modal-editor.md) — moving the caret that
  drives the reveal
- [Code intelligence](code-intelligence.md) — language servers on top of the
  highlighting
- [Git](git.md) — the gutter markers and the diff viewer
