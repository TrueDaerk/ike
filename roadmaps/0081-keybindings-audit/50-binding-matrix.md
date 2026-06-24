# 0081/50 — Per-Binding Status Matrix

The acceptance ledger. Every default binding gets a row; the roadmap is done when
every row reaches a resolved end-state (live, or honestly blocked) and is
verified. This is the "kontrolliert überprüfen" — a controlled pass, one binding
at a time, against a fixed Definition of Done.

## Definition of Done (per binding)

A binding is complete when **all** hold:

1. **Command real** — the target id is registered (doc 20) or explicitly tagged
   `blocked: <roadmap>`.
2. **Reachable** — its primary chord is `delivered` per the probe (doc 10), or a
   `delivered` leader/palette alternative exists (doc 30).
3. **Conflict-free** — no same chord+context clash (0080 conflict pass clean).
4. **Discoverable** — appears in the cheatsheet with correct status; reachable
   from the palette; fragile/blocked honestly labelled (doc 40).
5. **Verified** — an automated test asserts resolution to the expected id (or
   blocked tag), **and** one manual terminal check is recorded in the row.

End-states: **LIVE** (1–5 all true, command works) · **READY** (reachable +
discoverable, command blocked on another roadmap) · **TODO** (not yet audited).

## Matrix

Fill `Reach`, `Cmd state`, `Primary`, `Fallback`, `End-state`, `Verified` as each
binding is worked. `Primary` is the re-picked default (doc 30).

| # | JetBrains chord | Action | Context | Reach | Cmd state | Primary chord | Fallback | End-state | Verified |
|---|-----------------|--------|---------|-------|-----------|---------------|----------|-----------|----------|
| 1 | `cmd+z` | Undo | Editor | TBD | live (`editor.undo`) | `ctrl+z` | palette | TODO | ☐ |
| 2 | `cmd+shift+z` | Redo | Editor | TBD | live (`editor.redo`) | `ctrl+y`? | palette | TODO | ☐ |
| 3 | `cmd+s` | Save | Editor | TBD | reconcile→`editor.write` | `ctrl+k s` | `:w` | TODO | ☐ |
| 4 | `cmd+shift+s` | Save all | Global | TBD | blocked: 06 | `ctrl+k a` | palette | TODO | ☐ |
| 5 | `cmd+w` | Close tab | Global | TBD | reconcile→close | `ctrl+w` | palette | TODO | ☐ |
| 6 | `cmd+1` | Toggle tree | Global | TBD | app cmd `explorer.toggle` | `space e` | palette | TODO | ☐ |
| 7 | `ctrl+tab` | Switch pane | Global | TBD | app cmd `pane.switcher` | `space w`/`ctrl+w` | `tab` | TODO | ☐ |
| 8 | `cmd+shift+a` | Search everywhere | Global | TBD | blocked: 07 | `space space` | palette | TODO | ☐ |
| 9 | `shift shift` | Search everywhere | Global | undetectable | blocked: 07 | `space space` | palette | TODO | ☐ |
| 10 | `cmd+shift+o` | Go to file | Global | TBD | blocked: 09 | `space f f` | palette | TODO | ☐ |
| 11 | `cmd+o` | Go to symbol | Global | TBD | blocked: 09/10 | `space f s` | palette | TODO | ☐ |
| 12 | `cmd+e` | Recent files | Global | TBD | blocked: 07 | `space f r` | palette | TODO | ☐ |
| 13 | `cmd+shift+f` | Find in path | Global | TBD | blocked: 09 | `space s p` | palette | TODO | ☐ |
| 14 | `cmd+shift+r` | Replace in path | Global | TBD | blocked: 09 | `space s r` | palette | TODO | ☐ |
| 15 | `cmd+d` | Duplicate line | Editor | TBD | blocked: 06 | `space d` | palette | TODO | ☐ |
| 16 | `cmd+/` | Comment line | Editor | TBD | blocked: 06 | `ctrl+k c` | palette | TODO | ☐ |
| 17 | `cmd+shift+/` | Comment block | Editor | TBD | blocked: 06 | `ctrl+k b` | palette | TODO | ☐ |
| 18 | `cmd+c`/`x`/`v` | Copy/Cut/Paste | Editor | TBD | blocked: 06 | vim `y`/`d`/`p` | palette | TODO | ☐ |
| 19 | `cmd+f` | Find in file | Editor | TBD | blocked: 06 | vim `/` | palette | TODO | ☐ |
| 20 | `cmd+r` | Replace in file | Editor | TBD | blocked: 06 | `:s` | palette | TODO | ☐ |
| 21 | `alt+f7` | Find usages | Editor | fragile | blocked: 06/10 | `space u` | palette | TODO | ☐ |
| 22 | `shift+f6` | Rename symbol | Editor | TBD | blocked: 06/10 | `space r` | palette | TODO | ☐ |
| 23 | `cmd+b` | Go to declaration | Editor | TBD | blocked: 06/10 | `space b` | palette | TODO | ☐ |
| 24 | `cmd+left-bracket` | Navigate back | Global | TBD | app cmd `nav.back`? | `ctrl+o`? | palette | TODO | ☐ |
| 25 | `cmd+right-bracket` | Navigate forward | Global | TBD | app cmd `nav.forward`? | `ctrl+i`? | palette | TODO | ☐ |
| 26 | `cmd+k` | Commit | Global | TBD | blocked: VCS | `space g c` | palette | TODO | ☐ |
| 27 | `cmd+t` | Update project | Global | intercepted | blocked: VCS | `space g u` | palette | TODO | ☐ |
| 28 | `cmd+shift+t` | Revert file | Global | TBD | blocked: VCS | `space g x` | palette | TODO | ☐ |
| 29 | `cmd+k cmd+c` | Comment (chord) | Editor | TBD | blocked: 06 | `ctrl+k c` | palette | TODO | ☐ |
| 30 | `cmd+k cmd+s` | Keymap cheatsheet | Global | TBD | this roadmap | `space ?` / `f1` | palette | TODO | ☐ |
| 31 | `f1` | Help / cheatsheet | Global | delivered | this roadmap | `f1` | `space ?` | TODO | ☐ |

(Fallback secondary chords like `ctrl+y`, `ctrl+o`/`ctrl+i` are proposals to
finalise with the editor roadmap; vim-native equivalents are noted where the
editor already provides the motion.)

## Milestones

- [ ] Every row's `Reach` filled from the doc-10 probe.
- [ ] Every row's `Cmd state` resolved (live / app-cmd / reconciled / blocked:<roadmap>) per doc 20.
- [ ] Every row's `Primary` + `Fallback` set and conflict-free per doc 30.
- [ ] Every row discoverable per doc 40 (cheatsheet status correct, palette-reachable).
- [ ] Every row `Verified`: automated test asserts resolution/blocked-tag, and a manual terminal check recorded.
- [ ] No row left `TODO`; all are `LIVE` or `READY`.
