# 0082/07 — Comment line · `Cmd+/`

| Field | Value |
|-------|-------|
| Chords | `cmd+/` (→ `ctrl+/` off macOS); chord alias `cmd+k cmd+c` |
| Command id | `editor.commentLine` |
| Context | Editor |
| Owner | 06 (not registered) |
| Status today | **blocked: 06** |

## What it should do

Toggle a line comment on the current line or selection using the file's language
comment token (`//`, `#`, `--`, …). Toggling again uncomments.

## Usability checklist

- [ ] Correct comment token per filetype (Go `//`, shell `#`, SQL `--`, etc.).
- [ ] Toggle: commented lines uncomment on second press.
- [ ] Mixed selection (some commented) → consistent rule (comment-all then toggle), not half-toggled.
- [ ] Comment marker inserted at correct indentation (after leading whitespace, JetBrains-style) — not column 0.
- [ ] Cursor/selection preserved across the toggle.
- [ ] Single undo reverts the whole toggle.
- [ ] Unknown filetype → safe fallback or quiet no-op (documented).

## Manual test protocol

1. On a Go line, press → `// ` prefix at indent; press again → removed.
2. Select 4 mixed lines, press → all commented; press → all uncommented.
3. Repeat in a `.sh` and `.sql` file → correct tokens.
4. Confirm `cmd+k cmd+c` does the same as `cmd+/`.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
