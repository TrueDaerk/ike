---
type: concept
title: Navigation History (Back/Forward)
description: Cursor-position history across jumps — per-jump entries with JetBrains Back/Forward semantics, recorded at the open funnel, traversed by nav.back / nav.forward.
resource: internal/nav/history.go
tags: [architecture, navigation, editor, keybindings]
timestamp: 2026-07-21T00:00:00Z
---

# Navigation History (Back/Forward)

Roadmap 0220, promoted from idea #51. `nav.back` / `nav.forward` return the
caret to where it was before a jump and re-traverse after going back —
JetBrains Navigate Back/Forward semantics. The commands back the
`cmd+left-bracket` / `cmd+right-bracket` defaults (fragile on many
terminals and awkward on QWERTZ), the **mouse back/forward buttons** (#816:
buttons 4/5 arrive as the synthetic single-step chords `mouse-back` /
`mouse-forward` and resolve through the normal keymap, so they rebind like
keys; terminals without SGR extended buttons simply never deliver them, and
an unbound press is swallowed rather than leaked into a pane), the Navigate
menu entries, and the palette.

## Semantics

- **Per-jump entries, not per-keystroke.** An entry is recorded when the
  caret jumps through the open funnel — switching files (explorer, finder,
  palette, `host.OpenFileRequest`), go-to-definition, a references-list
  pick, a find-in-path result — and for in-file jumps (#219): large
  motions (`gg`, `G`, `{count}G`) and search landings (the initial `/`/`?`
  jump, `n`/`N`, `*`/`#`). Small motions (hjkl, w/b, paragraphs, page
  scrolls) never record, and an operator composed over a large motion
  (`dG`) is an edit, not a jump.
- **Back** returns to the departure point; **forward** re-traverses after a
  back. A fresh jump while back in history truncates the forward tail.
- **Dedup**: consecutive entries on the same file+line collapse (keeping
  the freshest column); column-only drift is not a jump.
- **Bounded**: 100 entries per direction, oldest fall off. Session-scoped —
  not persisted, and a project switch starts fresh.
- **Exhausted direction** → info toast ("no earlier/later position"), no-op.

## Architecture

```
internal/nav/        pure data structure: Position{Path,Line,Col} (0-based),
                     History{RecordJump, Back, Forward, CanBack, CanForward}
internal/app/nav.go  integration: currentNavPos (active editor file+caret),
                     NavBackMsg/NavForwardMsg handling, navigateHistory
```

- Recording sits at the root model's open funnel: `openPath` records when
  the target *file* differs, `openPathAt` additionally records same-file
  jumps to another *line* — so every jump source (definition, references,
  search results, file switches) is covered at two choke points instead of
  per-feature hooks.
- In-file jumps come through the editor's event seam (#219): the editor
  emits `EventJump` carrying the *departure* position immediately before a
  large motion or search landing moves the caret (`motion.Result.Jump` for
  `gg`/`G`, `jumpTo` for search sites); the app's `editorEmitter` adapter
  records it into the shared history and swallows the event — the landing
  follows as an ordinary cursor-move, so the LSP bridge sees nothing new.
- `Back(current)` / `Forward(current)` take the caret's current position so
  the opposite stack stays consistent; navigation itself goes through
  `openPathAt` with recording suppressed (`navSkip`), reusing the standard
  open flow (tab reuse, focus, hooks) — no remembered pane identities,
  which keeps entries valid across layout changes. With split layouts,
  back/forward acts in the *active* editor pane (focused, else most
  recent); other panes are untouched (#220).
- **Stale entries** (#220): traversal passes a validity filter
  (`BackWhere`/`ForwardWhere` with an `os.Stat` check) — an entry whose
  file was deleted or renamed is silently dropped and traversal continues
  in the same direction; the current position lands on the opposite stack
  only when a real target is found, so skipped attempts leave no
  duplicates.
- `nav.back` / `nav.forward` are `appCommand`s (compile-in `app` plugin)
  dispatching `NavBackMsg` / `NavForwardMsg`; the Navigate menu was already
  wired to these ids.

## Keybinding status

With the commands registered, both ids left the 0081 blocked ledger
(`internal/keymap/blocked.go`); the status matrix rows read "live via
palette". See [Keybindings & Shortcuts](/architecture/keybindings.md).
