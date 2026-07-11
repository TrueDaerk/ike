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
func Create(ext string) (string, error) // next free scratch-N.<ext>, created empty
func List() ([]string, error)     // existing scratches, newest-first (mod time)
```

`Dir` mirrors `config.Discover`'s user-layer override, so a sandboxed IKE
keeps its scratches in the sandbox. `Create` is race-free (`O_CREATE|O_EXCL`);
the extension is dot-optional, empty means `txt`. A missing directory lists as
empty, not as an error.

## Creating (#351)

`scratch.new` ("New Scratch File", plain `.txt`) plus one `scratch.new.<id>`
("New Scratch File: Python") per registered language, built from `lang.All()`
with the language's first extension — **picking the command is the language
picker**, no extra overlay UI. The command family is rebuilt on every registry
query (`Capabilities` is lazy), so late-registered languages appear without
ordering constraints. The handler creates via the store and opens through the
standard funnel (`openPath`, absolute path): the new scratch lands as a
focused tab with highlighting/LSP live. Also in the File menu.

## Listing (#352)

`scratch.list` ("Open Scratch File…", palette + File menu) opens the palette
locked to `ScratchMode` (`internal/palette/scratch_mode.go`, prefix `~`, the
recent-files pattern): file names newest-first from the injected
`scratch.List`, fuzzy-filtered by the query, enter opens through the standard
funnel (`OpenFileMsg`). An empty store renders one inert hint row pointing at
"New Scratch File".
