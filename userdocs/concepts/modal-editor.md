# The modal editor

The editor is modal, like vim. If you have vim in your fingers, almost
everything you expect works. If you do not, this page is the part of IKE worth
reading before the rest.

## Modes

**Normal** is where you start. Keys are commands, not text: `j` moves down,
`dw` deletes a word, `u` undoes. If you type a sentence in normal mode you will
trigger a dozen unrelated things.

**Insert** is where you type. ++i++ enters it (also `a`, `o`, `O`, `I`, `A` —
each at a different position), ++esc++ leaves it.

**Visual** selects. `v` for characters, `V` for whole lines, ++ctrl+v++ for a
block.

The active mode is always shown at the left of the status bar. When in doubt,
press ++esc++ — from anywhere it lands you in normal mode.

## The grammar

Normal mode composes rather than enumerates. A command is

```
["register] [count] operator [count] motion-or-text-object
```

so `d2w` deletes two words, `"ayy` yanks a line into register `a`, and `c3j`
changes three lines down. Learning the operators and the motions separately
gets you every combination of them.

**Operators:** `d` delete, `c` change, `y` yank, `>` / `<` indent, `gu` / `gU`
/ `g~` lowercase / uppercase / toggle case, `=` reindent, `gq` reflow (hard-wrap
at `editor.text_width`). Doubling an operator makes it linewise (`dd`, `guu`,
`gqq`, `==`). Commenting is a command (++cmd+7++) rather than an operator, and
`~` on its own toggles the case of the character under the cursor.

**Motions:** `h j k l`, `w b e` by word (`ge` back to the previous word's end),
`0 ^ $` within the line, `gg G` for the file, `{ }` by paragraph, `f t F T` to
a character, `%` to the matching bracket, `H M L` to the top/middle/bottom of
the screen. With soft wrap on, `g0 g$ gj gk` move by display line instead of
buffer line.

**Text objects:** `iw` / `aw` a word, `ip` / `ap` a paragraph, `is` / `as` a
sentence, `i(` / `a(` inside/around brackets (also `[`, `{`, `<`, and the
aliases `b` for `(` and `B` for `{`), `i"` / `a"` a quoted string, and `it` /
`at` an XML/HTML tag.

**Scrolling:** ++ctrl+f++ / ++ctrl+b++ by page, ++ctrl+d++ / ++ctrl+u++ by half
a page; `zz` / `zt` / `zb` put the cursor line at the centre / top / bottom.

Counts work in visual mode too: `V3j` extends the selection three lines. A
selection also takes `u` / `U` / `~` (case), `J` (join the lines), `r` (replace
every character), `=`, and `x` / `s` as aliases of `d` / `c`.

## Registers, marks and repeat

Yanks and deletes land in registers — `"a` through `"z`, plus the unnamed one.
`"ayy` yanks into `a`, `"ap` pastes it back. Macros use the same letters: `q`
records, `@` replays.

Marks remember positions: `ma` sets mark `a`, `` `a `` jumps back to it, `'a`
to its line. Bookmarks are the same idea with a UI in front of them.

`.` repeats the last change. It is the highest-leverage key in the editor and
the reason the grammar is worth learning: make an edit once, move, press `.`.

`g` also prefixes a few navigation commands: `gg` to the top, `gp` paste, `g-`
/ `g+` for chronological undo and redo, `g;` / `g,` to walk your recent edit
positions, `gv` to reselect the last visual selection, `gi` to resume inserting
where you last left insert mode, `gJ` to join lines without a space, and `gf`
to open the file whose name is under the cursor.

`ZZ` saves and closes the pane (like `:x`), `ZQ` closes it discarding changes
(like `:q!`).

## Ex commands

`:` opens the command line. Recognised commands:

| Command | What it does |
|---|---|
| `:w` `:w path` | Save, optionally to a different path |
| `:q` `:q!` | Close (with `!`, discarding changes) |
| `:wq` `:x` | Save and close |
| `:e path` | Reload a file, or open a new unsaved buffer if it does not exist |
| `:s/pat/repl/g` | Substitute — add `c` to confirm each match interactively |
| `:42` `:$` | Jump to a line |

Ranges work as in vim: `%` is the whole file, `1,5` a span, `.` the current
line, `'<,'>` the last visual selection (pre-filled automatically when you
press `:` from visual mode), and addresses take offsets like `.+2` or `$-1`.

++tab++ completes paths on `:e` and `:w` lines.

## Where IKE is not vim

A handful of deliberate deviations, all of them to make the editor behave like
a modern GUI where vim has no opinion:

- **Shift+arrows select.** In normal mode they enter visual mode and extend;
  releasing shift and pressing a plain arrow drops the selection instead of
  keeping it (`keymodel=stopsel` behaviour). Selections you start with `v` or
  `V` keep vim's semantics.
- **Word motions with modifiers stay on the line.** ++alt+left++ /
  ++alt+right++ (or ++ctrl++ + arrows) move by word but stop at the line's
  start and end rather than wrapping to the next line; `.` inside identifiers
  counts as a stop, so `config.editor.tabWidth` has sub-word stops. Vim's
  `w` / `b` / `e` still cross lines.
- **An insert is one undo unit.** Everything typed between ++i++ and ++esc++
  undoes as a single step, and asking for undo *while* inserting commits the
  session first, so it behaves the same from either mode.
- **Terminal-style kills work mid-insert.** ++alt+backspace++ or ++ctrl+w++
  delete the previous word, ++ctrl+u++ deletes to the line start, and
  ++cmd+backspace++ deletes the whole line.
- **JetBrains chords work everywhere.** ++cmd+s++, ++cmd+z++, ++cmd+f++,
  ++cmd+7++ and the rest are bound alongside the vim keys, not instead of
  them. Use whichever you reach for.

## Beyond the basics

- **Multi-caret** — ++ctrl+g++ adds a caret at the next occurrence of the
  selection, ++ctrl+shift+g++ at all of them; then type once and edit
  everywhere.
- **Folding** — `za` toggles a fold, `zc` / `zo` close and open one, `zM` /
  `zR` do the whole file.
- **Undo tree** — undo is a tree, not a line. **Undo Tree** from the palette
  shows the branches; `g-` and `g+` walk the history chronologically instead
  of by branch.
- **Smart indent** — with `editor.auto_indent` on, ++enter++ and `o` derive the
  new line's indentation from the language's block openers rather than just
  copying the previous line.
- **Snippets** — type a trigger word and press ++tab++ to expand a live
  template; they also appear in the completion popup.

## Configuration worth knowing

| Setting | Default | Why you might change it |
|---|---|---|
| `editor.relative_line_numbers` | `false` | Makes counts (`5j`, `d3k`) readable at a glance |
| `editor.search_ignore_case` | `false` | Off means smartcase: lowercase folds, any uppercase matches exactly |
| `editor.auto_save` | `focus` | Saves when focus leaves a pane; `idle` adds a timer, `off` disables |
| `editor.tab_width` / `editor.use_spaces` | `4` / `true` | Indentation, unless `.editorconfig` overrides it |
| `editor.format_on_save` | `false` | Runs the language server's formatter on manual saves |
| `editor.text_width` | `80` | The column `gq` reflows text to (0 falls back to 79) |

The [settings reference](../reference/settings.md) has the full list.
