# 0082/26 — Switch pane focus · `Ctrl+Tab`

| Field | Value |
|-------|-------|
| Chords | `ctrl+tab` (fragile — terminal tab-switch); app also `Tab` (cycle) and `Ctrl+arrow` (directional) |
| Command id | `pane.switcher` |
| Context | Global |
| Owner | app (focus cycle exists) |
| Status today | **live via `Tab` / `Ctrl+arrow`**, `ctrl+tab` fragile |

## What it should do

Move focus between panes. Two complementary modes already exist: `Tab` cycles,
`Ctrl+arrow` moves directionally by geometry. JetBrains `Ctrl+Tab` would be a
recent-pane switcher.

## Usability checklist

- [ ] `Tab` cycles focus through all panes in a stable, predictable order.
- [ ] `Ctrl+arrow` moves to the geometrically adjacent pane in that direction (no surprise jumps).
- [ ] Focused pane is visually obvious (border/title accent).
- [ ] Focus never lands "nowhere"; wrapping at the ends is sensible.
- [ ] Single-pane workspace → switch is a quiet no-op.
- [ ] `ctrl+tab` fragility acknowledged; a delivered alias (`Tab` / `space w`) is documented as primary.
- [ ] Optional MRU pane switching (true `Ctrl+Tab` feel) — decide in/out of scope.

## Manual test protocol

1. Split into 3+ panes; `Tab` cycles in a consistent order.
2. `Ctrl+arrow` each direction → lands on the visually adjacent pane.
3. Focus indicator clearly tracks the active pane.
4. Single pane → no-op.
5. Confirm whether `ctrl+tab` arrives in your terminal; else alias suffices.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
