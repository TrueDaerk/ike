# Search and replace

Four separate things share the word "search": finding text in the open file,
finding it across the project, replacing in either, and finding *files* by
name. This page covers the first three; file and symbol lookup lives in
[Go to file](#finding-files-and-symbols) at the bottom.

## In the current file

++cmd+f++ opens the search line, or `/` for forwards and `?` for backwards if
your fingers already know vim.

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

++cmd+shift+f++ opens find-in-path. It searches as you type and streams
results in, grouped by file with per-file match counts.

| Keys | What it does |
|---|---|
| ++tab++ / ++shift+tab++ | Cycle between the query, include and exclude fields |
| ++ctrl+c++ | Toggle case sensitivity |
| ++ctrl+w++ | Toggle whole-word matching |
| ++ctrl+x++ | Toggle regex |
| ++ctrl+up++ / ++ctrl+down++ | Recall an earlier query |
| ++enter++ | Open the selected match |
| ++esc++ | Close — the results stay live |

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
