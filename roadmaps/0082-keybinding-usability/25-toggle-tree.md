# 0082/25 — Toggle project tree · `Cmd+1`

| Field | Value |
|-------|-------|
| Chords | `cmd+1` (intercepted by many terminals — needs `space e` alias) |
| Command id | `explorer.toggle` |
| Context | Global |
| Owner | 05 / app (explorer pane exists) |
| Status today | **partial** (explorer exists; toggle command/chord pending) |

## What it should do

Show/hide the file-explorer pane. Hidden → reclaim its width for editors; shown
→ restore it and (JetBrains-style) optionally focus it.

## Usability checklist

- [ ] Toggles explorer visibility; editors reflow to use/return the width smoothly.
- [ ] Second press restores the explorer at its prior width.
- [ ] When shown, focus behavior is sensible (JetBrains: first press focuses tree, second hides) — pick and document.
- [ ] Explorer state (expansion, selection, scroll) preserved across hide/show.
- [ ] Layout persistence reflects the toggle across sessions.
- [ ] `Cmd+1` often intercepted → `space e` alias is the real entry; both documented.
- [ ] No focus left "nowhere" if the explorer was focused when hidden.

## Manual test protocol

1. `space e` → explorer hides, editors widen; press → explorer back at same width.
2. Expand folders, hide, show → expansion preserved.
3. Focus explorer, hide it → focus moves to an editor (not lost).
4. Restart IKE → visibility state restored.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
