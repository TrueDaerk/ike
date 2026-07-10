---
type: concept
title: Navigation History (Back/Forward)
description: Cursor-position history across jumps — per-jump entries with JetBrains Back/Forward semantics, recorded at the open funnel, traversed by nav.back / nav.forward.
resource: internal/nav/history.go
tags: [architecture, navigation, editor, keybindings]
timestamp: 2026-07-10T18:30:00Z
---

# Navigation History (Back/Forward)

Roadmap 0220, promoted from idea #51. `nav.back` / `nav.forward` return the
caret to where it was before a jump and re-traverse after going back —
JetBrains Navigate Back/Forward semantics. The commands back the
`cmd+left-bracket` / `cmd+right-bracket` defaults (fragile on many
terminals and awkward on QWERTZ), the leader mnemonics `space b` /
`space i` (and `ctrl+k b` / `ctrl+k i`), the Navigate menu entries, and the
palette.

## Semantics

- **Per-jump entries, not per-keystroke.** An entry is recorded when the
  caret jumps through the open funnel: switching files (explorer, finder,
  palette, `host.OpenFileRequest`), go-to-definition, a references-list
  pick, a find-in-path result — any open that lands somewhere else
  (different file, or same file + different line). Small in-file motions
  never record (large motions are the 0220/20 slice).
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
- `Back(current)` / `Forward(current)` take the caret's current position so
  the opposite stack stays consistent; navigation itself goes through
  `openPathAt` with recording suppressed (`navSkip`), reusing the standard
  open flow (tab reuse, focus, hooks) — no remembered pane identities,
  which keeps entries valid across layout changes.
- `nav.back` / `nav.forward` are `appCommand`s (compile-in `app` plugin)
  dispatching `NavBackMsg` / `NavForwardMsg`; the Navigate menu was already
  wired to these ids.

## Keybinding status

With the commands registered, both ids left the 0081 blocked ledger
(`internal/keymap/blocked.go`); the status matrix rows read "live via
space b / space i". See [Keybindings & Shortcuts](/architecture/keybindings.md).
