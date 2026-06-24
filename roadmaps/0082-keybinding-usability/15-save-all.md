# 0082/15 — Save all · `Cmd+Shift+S`

| Field | Value |
|-------|-------|
| Chords | `cmd+shift+s` |
| Command id | `editor.saveAll` |
| Context | Global |
| Owner | 06 (not registered) |
| Status today | **blocked: 06** |

## What it should do

Write every modified buffer across all panes to disk in one action. Summary
feedback of how many were saved.

## Usability checklist

- [ ] Saves all dirty buffers; clean ones skipped.
- [ ] Summary feedback ("saved 3 files").
- [ ] Per-file write errors reported without aborting the rest; lists which failed.
- [ ] Respects per-buffer save config (trim/newline) like single Save.
- [ ] No-op with quiet hint when nothing is dirty.
- [ ] Does not change focus or layout.

## Manual test protocol

1. Modify 3 of 4 open buffers, press → "saved 3 files"; clean one untouched.
2. Make one buffer unwritable, save-all → others saved, failure reported.
3. With nothing dirty → quiet no-op.
4. Focus/layout unchanged afterwards.

## Verdict (you fill after testing)

- Status: ☐ pending · ☐ OK passt · ☐ needs change
- Notes:
- Follow-ups:
