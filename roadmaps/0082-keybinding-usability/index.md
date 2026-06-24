# Roadmap 0082 — Per-Keybinding Usability Review

One file per existing keybinding. Each file isolates a single binding and reviews
the **usability of what it triggers** — not whether the chord resolves (that is
0080/0081), but whether the *experience* behind it is good: does the search field
behave incrementally, does the picker remember the last query, does Esc do the
right thing, is there visible feedback, etc.

## The loop

Each binding file is a small, self-contained unit you test by hand:

1. I prepare the file: the binding's identity, what it *should* do, a concrete
   **usability checklist**, and a **manual test protocol**.
2. **You run the protocol in a real terminal** and fill the **Verdict** section
   with either:
   - **"OK passt"** → I tick the file done.
   - **"X und Y passen nicht, lass uns das So-und-So machen"** → I capture the
     follow-ups and we implement the fix, then you re-test.
3. The roadmap is done when every file's Verdict is **OK passt**.

A binding whose command is not built yet (blocked on another roadmap) is still
reviewed for its *intended* UX so the spec is ready the moment the command lands;
its Verdict stays **pending** until then.

## Files

Grouped by area; numbers are stable handles.

### Editing (editor)
* [01 — Undo · `Cmd+Z`](./01-undo.md)
* [02 — Redo · `Cmd+Shift+Z`](./02-redo.md)
* [03 — Copy · `Cmd+C`](./03-copy.md)
* [04 — Cut · `Cmd+X`](./04-cut.md)
* [05 — Paste · `Cmd+V`](./05-paste.md)
* [06 — Duplicate line · `Cmd+D`](./06-duplicate-line.md)
* [07 — Comment line · `Cmd+/`](./07-comment-line.md)
* [08 — Comment block · `Cmd+Shift+/`](./08-comment-block.md)

### Search & code navigation (editor / LSP)
* [09 — Find in file · `Cmd+F`](./09-find-in-file.md)
* [10 — Replace in file · `Cmd+R`](./10-replace-in-file.md)
* [11 — Go to declaration · `Cmd+B`](./11-goto-declaration.md)
* [12 — Find usages · `Alt+F7`](./12-find-usages.md)
* [13 — Rename symbol · `Shift+F6`](./13-rename-symbol.md)

### Files & buffers
* [14 — Save · `Cmd+S`](./14-save.md)
* [15 — Save all · `Cmd+Shift+S`](./15-save-all.md)
* [16 — Close tab · `Cmd+W`](./16-close-tab.md)

### Project-wide search & navigation
* [17 — Search everywhere · `Cmd+Shift+A` / `Shift Shift`](./17-search-everywhere.md)
* [18 — Go to file · `Cmd+Shift+O`](./18-go-to-file.md)
* [19 — Go to symbol · `Cmd+O`](./19-go-to-symbol.md)
* [20 — Recent files · `Cmd+E`](./20-recent-files.md)
* [21 — Find in path · `Cmd+Shift+F`](./21-find-in-path.md)
* [22 — Replace in path · `Cmd+Shift+R`](./22-replace-in-path.md)

### Workspace & navigation
* [23 — Navigate back · `Cmd+[`](./23-navigate-back.md)
* [24 — Navigate forward · `Cmd+]`](./24-navigate-forward.md)
* [25 — Toggle project tree · `Cmd+1`](./25-toggle-tree.md)
* [26 — Switch pane focus · `Ctrl+Tab`](./26-switch-pane.md)
* [27 — Keymap cheatsheet · `F1` / `Cmd+K Cmd+S`](./27-keymap-help.md)

### VCS (blocked — future roadmap)
* [28 — Commit · `Cmd+K`](./28-commit.md)
* [29 — Update project · `Cmd+T`](./29-update-project.md)
* [30 — Revert file · `Cmd+Shift+T`](./30-revert-file.md)

## Status board

Update as verdicts come in.

| # | Binding | Status today | Verdict |
|---|---------|--------------|---------|
| 01 | Undo | live | ☐ pending |
| 02 | Redo | live | ☐ pending |
| 03 | Copy | blocked 06 | ☐ pending |
| 04 | Cut | blocked 06 | ☐ pending |
| 05 | Paste | blocked 06 | ☐ pending |
| 06 | Duplicate line | blocked 06 | ☐ pending |
| 07 | Comment line | blocked 06 | ☐ pending |
| 08 | Comment block | blocked 06 | ☐ pending |
| 09 | Find in file | blocked 06 | ☐ pending |
| 10 | Replace in file | blocked 06 | ☐ pending |
| 11 | Go to declaration | blocked 06/10 | ☐ pending |
| 12 | Find usages | blocked 06/10 | ☐ pending |
| 13 | Rename symbol | blocked 06/10 | ☐ pending |
| 14 | Save | live (`editor.write`) | ☐ pending |
| 15 | Save all | blocked 06 | ☐ pending |
| 16 | Close tab | live (app) | ☐ pending |
| 17 | Search everywhere | blocked 07 | ☐ pending |
| 18 | Go to file | partial (`@` finder) | ☐ pending |
| 19 | Go to symbol | blocked 09/10 | ☐ pending |
| 20 | Recent files | blocked 07 | ☐ pending |
| 21 | Find in path | blocked 09 | ☐ pending |
| 22 | Replace in path | blocked 09 | ☐ pending |
| 23 | Navigate back | blocked 06/01 | ☐ pending |
| 24 | Navigate forward | blocked 06/01 | ☐ pending |
| 25 | Toggle project tree | partial (app) | ☐ pending |
| 26 | Switch pane focus | live (app) | ☐ pending |
| 27 | Keymap cheatsheet | partial (help overlay) | ☐ pending |
| 28 | Commit | blocked VCS | ☐ pending |
| 29 | Update project | blocked VCS | ☐ pending |
| 30 | Revert file | blocked VCS | ☐ pending |

## Relationship to other roadmaps

- **0080** built the keymap engine; **0081** makes bindings reachable/discoverable
  in the terminal. **0082** reviews the *downstream UX* of each binding's action,
  one at a time, gated on your manual test.
- The real commands are owned by 05/06/07/09/VCS; 0082 specifies and verifies
  their *usability*, and feeds concrete change requests back to those roadmaps.
