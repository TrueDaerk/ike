# 0082/22 — Replace in path · `Cmd+Shift+R`

| Field | Value |
|-------|-------|
| Chords | `cmd+shift+r` |
| Command id | `project.replaceInPath` |
| Context | Global |
| Owner | 09 |
| Status today | **blocked: 09** |

## What it should do

Project-wide find + replace: the Find-in-path UI (21) plus a replacement field
and a preview/confirm step before writing changes across files.

## Usability checklist

- [ ] Builds on Find-in-path (21): same search + options, plus a Replace field.
- [ ] **Preview before write**: shows per-match before/after diff; nothing changes until confirmed.
- [ ] Selective apply: include/exclude individual matches or files.
- [ ] Replace-all summary ("replaced 42 in 9 files"); failures listed, others still applied.
- [ ] Open buffers update in place; closed files written and re-readable.
- [ ] Undoable per file (or a clearly-documented bulk-undo limitation).
- [ ] Esc before confirm → zero changes.
- [ ] Regex capture refs supported or explicitly not.

## Manual test protocol

1. Open, search + replacement → preview shows before/after per match.
2. Deselect some matches → only selected ones apply.
3. Confirm → summary count; open a changed file to verify.
4. Esc before confirm → nothing changed.
5. Test undo behavior on a changed file.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
