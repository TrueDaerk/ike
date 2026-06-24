# 0082/17 — Search everywhere · `Cmd+Shift+A` / `Shift Shift`

| Field | Value |
|-------|-------|
| Chords | `cmd+shift+a`; `shift shift` (undetectable in terminal — needs `space space` alias) |
| Command id | `palette.searchEverywhere` |
| Context | Global |
| Owner | 07 (palette exists; this unified mode not yet) |
| Status today | **blocked: 07** |

## What it should do

Open the palette in a unified mode that searches across everything at once —
files, commands, symbols, recent — ranked together, JetBrains "Search Everywhere"
style.

## Usability checklist (palette UX)

- [ ] Single input searching all sources; results labelled by kind (file/command/symbol).
- [ ] Sensible cross-source ranking (exact > prefix > fuzzy; recent boosted).
- [ ] Fuzzy matching with match highlighting in results.
- [ ] Fast/incremental — no lag while typing on a large project.
- [ ] Keyboard nav + Enter to open/run; Esc closes restoring prior focus.
- [ ] Empty query shows something useful (recent files/commands), not blank.
- [ ] Opens centered, sized responsively; reuses existing palette component.
- [ ] `shift shift` is undetectable → `space space` alias is the real entry; both documented.

## Manual test protocol

1. Open via `space space`, type a partial name → mixed files/commands/symbols ranked.
2. Confirm kind labels + match highlighting.
3. Enter on a file opens it; on a command runs it.
4. Esc restores previous focus.
5. Empty query → recents shown.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
