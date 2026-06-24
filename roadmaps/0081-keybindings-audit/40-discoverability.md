# 0081/40 — Discoverability

A shortcut nobody can find is not user-friendly. Make the working bindings
visible at the moment of need, and make blocked ones honestly labelled instead of
silently dead.

## Surfaces

- **Live cheatsheet (`palette.keymapHelp`).** Wire 0080's `BindingTable.Help()`
  into a real overlay opened by `f1` / `space ?` / `cmd+k cmd+s`. Group by
  context (Global first), show the **primary** chord, a **fallback** column
  (leader/palette), and a **status** badge: live / blocked(<roadmap>) /
  macOS-only. Reuse the floating shell (0035) and the existing help overlay
  styling (0030).
- **Which-key on held leader.** When a leader prefix is pending (doc 30), render
  the available continuations as a small anchored panel: `space →  f files · s
  search · e explorer · g git · ? help`. Drills down as more steps are typed.
  Driven by `BindingTable` filtered to chords with the current prefix.
- **Palette shortcut column.** The command palette (07) lists commands; show each
  command's resolved primary chord (and "—" when none/blocked) so the palette
  doubles as a searchable shortcut index. Uses `BindingTable` reverse lookup
  (command id → chord).
- **Honest fragile / blocked labelling.** Intercepted/undetectable JetBrains
  chords are shown with their working alternative ("`Cmd+T` (macOS only) → `space
  g u`"); blocked commands show "needs Roadmap NNNN" rather than appearing
  pressable-but-dead.
- **Inert-press hint.** Pressing a blocked binding shows a transient status-line
  message naming the action and its blocking roadmap (coordinated with doc 20),
  so the key never feels broken.

## Approach

- Add a reverse index (command id → primary chord) to `internal/keymap` for the
  palette column and which-key.
- The cheatsheet and which-key are pure consumers of `BindingTable` /
  reachability metadata — no binding logic in the view layer.
- Keep the help overlay responsive (column reflow from 0030) and scrollable for
  the full table.

## Milestones

- [ ] Wire `palette.keymapHelp` to a live, grouped, status-badged cheatsheet overlay (primary + fallback + status columns).
- [ ] Which-key panel on a held leader prefix, drilling down per typed step.
- [ ] Palette shortcut column via command-id → chord reverse lookup.
- [ ] Honest labelling: macOS-only/fragile chords show their working alternative; blocked commands show the needed roadmap.
- [ ] Inert-press transient status hint for blocked bindings.
- [ ] Tests: cheatsheet groups + status badges reflect live vs blocked; reverse lookup returns the primary chord; which-key filters by prefix.
