---
type: concept
title: Scratch Files
description: JetBrains-style scratch buffers — language-aware quick files under the user state dir, created from the palette, surviving restarts as ordinary files.
resource: internal/scratch
tags: [architecture, scratch, palette, languages]
timestamp: 2026-07-11T13:15:00Z
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
func List() ([]string, error)     // existing scratches, newest-first (mod time)
```

`Dir` mirrors `config.Discover`'s user-layer override, so a sandboxed IKE
keeps its scratches in the sandbox. `Create` is race-free (`O_CREATE|O_EXCL`)
and writes the language's rendered file template (`lang.TemplateFor`, #1223)
through the winning handle, so the content belongs to the allocation that won
the race and a PHP scratch is runnable as created; the extension is
dot-optional, empty means `txt`. A missing directory lists as empty, not as an
error.

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

## Running a scratch (#1223)

`run.file` needs no scratch-specific code. A scratch lies outside the project
tree, so `run.Default` stores its path absolute (`relTo` keeps only paths under
the root relative) with an empty `Cwd` — which resolves to the **project
root**. The argv is synthesized through the language's `RunCommandProvider`
with the project's resolved interpreter, so a Python scratch runs under the
project's virtualenv and a PHP scratch under its configured `php`. See
[run configurations](./run-configurations.md).

## Listing (#352)

`scratch.list` ("Open Scratch File…", palette + File menu) opens the palette
locked to `ScratchMode` (`internal/palette/scratch_mode.go`, prefix `~`, the
recent-files pattern): file names newest-first from the injected
`scratch.List`, fuzzy-filtered by the query, enter opens through the standard
funnel (`OpenFileMsg`). An empty store renders one inert hint row pointing at
"New Scratch File".

**Reachable from the `@` file finder too (#1812).** A query that fuzzy-matches
the word "scratch" surfaces the same `scratch.List` rows, newest-first, inline
in the `@` finder below the project matches — no mode switch needed for the
common case of typing "scratch" to find a scratch file. See [Command
Palette](/architecture/command-palette.md) for the file-mode ranking this
slots into.
