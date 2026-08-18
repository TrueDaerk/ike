# Scratch files and snippets

Two unrelated features that solve the same kind of annoyance: getting text out
of your head without ceremony.

## Scratch files

A scratch file is a throwaway buffer that is nevertheless a real file. Use one
for a quick query, a chunk of JSON you want highlighted, a snippet of code you
are reasoning about — anything you would otherwise paste into an untitled tab
and lose.

++cmd+shift+n++ creates one and asks for the language first: **Plain Text**,
**Python**, **PHP**, **SQL** — one row per registered language, filtered as you
type. Or skip the prompt and run the language's own command from the palette:
**New Scratch File: Python**, **New Scratch File: SQL**, **New Scratch File:
Plain Text**.

![A Go scratch file open as a second tab next to a project file, highlighted like any other Go buffer, its path in the status line pointing outside the project](../screenshots/features/scratch-file.png)

The screenshot uses the `monokai-pro` theme.

The language decides the file extension, and everything language-aware follows
from that — syntax highlighting, the language server, comment toggling, smart
indentation, and the language's file template (a PHP scratch opens with
`<?php`). A scratch file is not a special buffer type; it is a normal file that
happens to live somewhere else.

### Running one

++shift+f10++ (**Run File**) runs a scratch like any other file: from your
**project root**, with the interpreter that project resolves — its virtualenv,
its `[lang.<id>] interpreter` setting, its detected toolchain version. So a
Python or PHP scratch can exercise the project it sits next to without being
part of it. Languages that contribute no run command say so instead.

An **Http** scratch is the exception worth knowing: it is not run with
++shift+f10++ but with ++cmd+enter++, which sends the request under the cursor
— a throwaway API call without adding a file to the repository. See
[The HTTP client](http-client.md).

### Where they live

Outside your project tree, under the user state directory. That is the
important property: a scratch file can never accidentally end up in a commit,
and it is still there when you come back next week — including from a
different project.

**Open Scratch File…** lists them, newest first, with fuzzy filtering. Open
scratch tabs also come back with your session like any other file.

The explorer shows them too: a **Scratches** section sits behind a divider at
the bottom of the file tree, sorted by name (switch to newest-first with the
`scratch.sort` setting). The cursor walks into it with the explorer's normal
keys, ++enter++ opens a scratch exactly like a project file, ++d++ deletes and
++shift+r++ renames with the explorer's dialogs, and ++a++ creates a new
scratch through the language picker. Click the divider to collapse the
section, or drag it to resize; both stick across restarts.

## Snippets

Type a trigger word, press ++tab++ with the cursor right after it, and it
expands into a template. The cursor lands on the first placeholder;
++tab++ and ++shift+tab++ cycle through the rest, ++esc++ ends the session.

A handful ship built in — `iferr`, `main` and `forr` for Go, `main` and `def`
for Python, `log` and `fn` for TypeScript and JavaScript.

If the word before the cursor is not a trigger, ++tab++ does what it always
does and inserts indentation. Nothing is stolen from you.

Templates also appear in the completion popup, marked `template …`. That works
with no language server running — the local completion engine answers them
independently.

### Writing your own

```toml
[[snippets]]
trigger = "ifn"
language = "go"
body = "if $1 == nil {\\n\\t$0\\n}"

[[snippets]]
trigger = "todo"
body = "TODO($1): $0"
```

`trigger` is the word you type. `body` is the template, with `$1`, `$2` … as
placeholders and `$0` as the final cursor position. `language` restricts it to
one language; leave it out for a template that applies everywhere.

Multi-line bodies re-indent themselves on expansion: literal tabs become your
buffer's indent unit — honouring `tab_width`, `use_spaces` and any
`.editorconfig` — and continuation lines inherit the current line's
indentation. You write the template with tabs and it comes out matching the
file it lands in.

Resolution when several templates share a trigger:

1. Your template for this language
2. A built-in for this language
3. Your language-independent template
4. A language-independent built-in

So defining `main` for Go shadows the built-in `main` for Go, and leaves it
alone everywhere else.

Config changes apply live — a new snippet works in the next keystroke, with no
restart.

!!! warning "`[[snippets]]` replaces across settings layers"
    Like every TOML array, a project-level `[[snippets]]` block hides your
    user-level ones rather than adding to them. The built-ins are the
    exception: they live in code and are only ever shadowed per
    trigger-and-language, never wiped.

## Related

- [The modal editor](../concepts/modal-editor.md) — where ++tab++ otherwise
  goes
- [Settings reference](../reference/settings.md) — every configuration key
