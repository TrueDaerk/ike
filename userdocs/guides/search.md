# Search and replace

Four separate things share the word "search": finding text in the open file,
finding it across the project, replacing in either, and finding *files* by
name. This page covers the first three; file and symbol lookup lives in
[Go to file](#finding-files-and-symbols) at the bottom.

## In the current file

With an **editor pane focused**, ++cmd+f++ opens the search line — or `/` for
forwards and `?` for backwards if your fingers already know vim. All
screenshots on this page use the `monokai-pro` theme.

![Find in File: the query on the bottom line with a match counter, every match highlighted in the buffer, the current one in the cursor colour](../screenshots/features/find-in-file.png)

| Keys | What it does |
|---|---|
| ++cmd+f++, `/`, `?` | Start a search |
| ++f3++ / ++shift+f3++ | Next / previous match |
| `n` / `N` | The same, vim-style |
| `*` / `#` | Search for the word under the cursor, forwards / backwards |
| ++esc++ | Clear the match highlighting |

Matching is **smartcase** by default: an all-lowercase query ignores case,
while any uppercase character makes the whole query case-sensitive. Prefix a
query with `\C` to force exact case regardless, or with `\c` to force the
opposite. ++ctrl+c++ toggles between them while the search line is open — it
writes the marker into the query, so the current mode is always visible. If
you would rather always ignore case, set `editor.search_ignore_case = true`.

++up++ and ++down++ recall previous queries, and the history survives
restarts.

### Structural search in JSON and YAML

In a JSON or YAML buffer, prefix the query with `\j` — or press ++ctrl+x++ on
the open search line — and it becomes a **jq expression**: the matches are
the document nodes the query selects, not text occurrences. Searching
`\j.users[].name` in a large export lands on each name value in turn, where a
text search for `"name"` would stop on every key in the file. Full jq works,
including filters:

```
\j.users[] | select(.age > 40) | .name
```

Everything else behaves like an ordinary search: the matches highlight, the
counter counts them, and `n`/`N` and ++f3++ step through them. A query that
is not valid jq (or that computes new values instead of selecting locations
in the document) shows its error inline on the search line instead of quietly
matching nothing. YAML documents resolve anchors, aliases and `<<:` merge
keys the same way the yq playground does.

### Replacing in the file

++cmd+r++ opens replace, or use vim's substitute directly:

```
:s/old/new/           " this line, first match
:%s/old/new/g         " whole file, every match
:'<,'>s/old/new/g     " the visual selection
:s/old/new/gc         " confirm each match interactively
```

The `c` flag turns the substitution into a walk: each match is shown in turn
and you accept or skip it. `\v` switches the pattern to a fuller regex
dialect, and an empty pattern (`:s//new/`) reuses your last search.

## Across the project

++cmd+shift+f++ opens find-in-path, from any pane. It searches as you type and
streams results in, grouped by file with per-file match counts.

![Find in Path: the query and its three toggles, results grouped under each file with the match count in brackets, the total at the bottom](../screenshots/features/find-in-path.png)

| Keys | What it does |
|---|---|
| ++tab++ / ++shift+tab++ | Cycle between the query, include and exclude fields |
| ++ctrl+c++ | Toggle case sensitivity |
| ++ctrl+w++ | Toggle whole-word matching |
| ++ctrl+x++ | Toggle regex |
| ++ctrl+up++ / ++ctrl+down++ | Recall an earlier query |
| ++enter++ | Open the selected match |
| ++esc++ | Close — the results stay live |

If text is selected when you open it — an editor visual selection, or a mouse
selection in a terminal, a diff, a merge or the HTTP response viewer — the
query starts out filled with that text, already selected, so typing replaces
it and ++enter++ searches for it. A selection spanning several lines prefills
nothing, and in regex mode the text is escaped so it is found literally.

The include and exclude fields take comma-separated globs: `*.go`,
`!vendor/**`, `internal/**,cmd/**`. Each keystroke restarts the scan and
cancels the previous one, so a long search never blocks the next one.

Closing the overlay does not throw the results away. ++f3++ and ++shift+f3++
keep stepping through the matches, wrapping across files, with no overlay in
the way. Whichever search you ran last owns those keys — commit an in-file
search and ++f3++ repeats *that* instead, until the next project search
reclaims it.

Reopening find-in-path preselects your previous query: type to replace it,
press ++backspace++ or an arrow to keep it.

### Replacing across the project

++cmd+shift+r++ opens the same overlay in replace mode, with a replacement
field and a before/after preview of the selected match.

| Keys | What it does |
|---|---|
| ++enter++ | Replace the selected match and step to the next |
| ++ctrl+f++ | Replace every match in the selected file |
| ++ctrl+a++ | Replace everything |
| ++ctrl+enter++ | Jump to the match instead of replacing it |

With regex on, `$1` and `${name}` expand capture groups in the replacement.
Literal replacements never expand anything, so a `$` stays a `$`.

Two things worth knowing about how replacements are applied:

- **Open, modified files** are edited through the buffer, as **one undo unit
  per file** — a single `u` reverts the whole batch in that file. Everything
  else is rewritten on disk, and any clean buffer you have open picks the
  change up through the normal external-change reload.
- **Stale matches are skipped, not forced.** If a line changed since the scan
  found it, that match is left alone and counted in the summary
  (`N replacements in M files (K stale matches skipped)`). It never applies a
  replacement to text it no longer recognises.

## Across every project

++cmd+alt+shift+f++ searches one text across **every project in your recent
history**, in the background. The form takes the query, the same three
toggles, include/exclude globs and the list of projects to search — a project
you toggle off stays off next time, and a root that no longer exists on disk
is greyed out and skipped.

Confirming closes the form and hands the keyboard straight back: the scan runs
behind you, and the only thing on screen is a status-line counter —
`⌕ all projects 3/12 · 41 hits` — that you can click to look at what has been
found so far.

When the scan finishes, the hits open in the same results overlay as
find-in-path, one level deeper: the project name heads each block, its files
below it, their matches under those. Every key means what it means in
find-in-path.

| Keys | What it does |
|---|---|
| ++up++ / ++down++ | Step the matches, wrapping at both ends |
| ++pgup++ / ++pgdown++ | Page through them |
| ++cmd+g++ / ++cmd+shift+g++ | Step the matches, with the position counter |
| ++enter++ | Open the selected match — switching projects if it lives in another one |
| ++alt+p++ / ++ctrl+e++ | Focus the excerpt column beside the list |
| ++esc++ | Close — the results stay |

Opening a hit in another project switches IKE to that project and lands on the
line. The result list survives the switch: ++cmd+alt+shift+r++ brings it back,
and ++cmd+g++ keeps walking the hits from there — across project boundaries,
switching again whenever the next hit lives elsewhere.

A search that matched nothing anywhere just says so; nothing opens.

## The search backend

If [ripgrep](https://github.com/BurntSushi/ripgrep) is on your `PATH`, IKE
uses it (`rg --json`) and your flags map onto rg's directly. Without it, a
pure-Go walker takes over — same semantics, noticeably slower on large trees.

Both honour `.gitignore`. The fallback's gitignore support is deliberately
simple: directory rules and plain globs work, negation (`!pattern`) does not.
Where the two disagree on an exotic pattern, ripgrep is the one that is right
— which is a reason to install it beyond speed.

Hidden dot-entries and `.git` are skipped by both.

!!! note "`explorer.exclude` does not apply here"
    That setting hides entries from the **file tree** only. Find-in-path and
    Go to file still see them. Use the exclude field, or `.gitignore`, to keep
    something out of search results.

## In any other pane

++cmd+f++ is not an editor key. Pressed anywhere else it opens **that pane's**
search or filter — the same thing `/` has always opened there, so muscle
memory works whichever pane happens to hold the focus:

| Pane | What ++cmd+f++ opens |
|---|---|
| Explorer | The type-to-select speed search |
| Problems, Usages, TODO index | The filter row (`severity:`, `file:`, `tag:`, …) |
| GitHub Issues | The filter overlay, on its match input |
| HTTP response | The in-pane search over status line, headers and body |
| Archive viewer | The filter row (`name:`, `type:`) |
| Diff viewer | A search over the diff, with `n` / `N` stepping matches |
| Markdown preview | A search over the rendered document |
| DOM inspector | The CSS selector line |
| Data viewer | The SQL filter clause |
| Terminal | The scrollback search — or copy mode's own search while copy mode is on |
| Settings | The page filter |

++ctrl+f++ does the same in every one of them. In an **editor** both chords
keep their editor meaning: ++cmd+f++ is Find in File and ++ctrl+f++ stays
vim's page-forward motion.

A pane with no search of its own says so ("No search in this pane") rather
than doing nothing.

### Stepping the matches

++cmd+g++ and ++cmd+shift+g++ move to the next and previous match **while the
input keeps the focus**. There is no need to press ++enter++ first: the query
stays editable, so you can narrow it, look at where the next hit lands, and
narrow it again without ever leaving the field.

| Keys | What it does |
|---|---|
| ++cmd+g++ / ++cmd+shift+g++ | Next / previous match in the focused pane's search |
| ++ctrl+g++ / ++ctrl+shift+g++ | The same, outside an editor |
| ++f3++ / ++shift+f3++ | The same again, the JetBrains keys |

What counts as a "match" is what the pane's search produces: a hit in the
diff, response body or scrollback; a row in a filtered list; an element the
CSS selector selected. Filter rows step their surviving rows, skipping the
file headers between them.

The walk wraps at both ends, and the pane's search line marks the step that
came back around — `1/12 (wrapped)` — so a repeat is never mistaken for new
ground. With nothing matching, the chord is a no-op with a short "No matches"
hint rather than a silent one.

++enter++ keeps its meaning next to all of this: it applies the filter and
leaves the input, or opens the selected row. In the HTTP response viewer `n`
and `N` still step the matches once the prompt has closed.

Inside an **editor** the chords keep their editor meaning: they repeat the
in-file search, or step the retained find-in-path results — which is what
they always did, and what the pane chords are modelled on.

## Finding files and symbols

Different problem, different keys:

| Keys | What it looks up |
|---|---|
| ++cmd+shift+o++ | Files, by fuzzy name match |
| ++cmd+o++ | Symbols — needs a language server |
| ++cmd+e++ | Recently opened files |
| ++shift++ twice | Everything at once |

Search everywhere is the one to reach for when you are not sure which of those
you want: type `@` to narrow it to files, `:` to narrow it to commands.

## Related

- **Problems** (++cmd+8++) — every diagnostic in the project, in the same
  result-list component.
- **TODO index** (++cmd+6++) — every `TODO`/`FIXME` comment, likewise.
- **Find usages** (++alt+f7++) — semantic references rather than text matches;
  see [Code intelligence](code-intelligence.md).
