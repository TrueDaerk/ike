---
type: concept
title: Picker Speed Search
description: The shared type-ahead every modal picker narrows with — printable keys filter the visible rows live, space stays the toggle, backspace peels the query before it falls through, and esc clears the query before it closes the modal.
resource: internal/ui/speedsearch.go
tags: [architecture, ui, lists, navigation, keys, reusable, pickers]
timestamp: 2026-08-25T12:00:00Z
---

# Picker Speed Search (#2111)

JetBrains popups all have **speed search**: you open a list and start typing,
and the list narrows to what you typed. IKE's modal pickers only arrow-walked,
so a repository with forty labels meant walking the label picker row by row —
the complaint that came out of the issues-window UX review (#2104, F11).

`internal/ui/speedsearch.go` is the one place that defines what typing into a
picker does. Pickers keep their own state, rows and rendering; they take the
*semantics* from here, exactly as they take cursor semantics from
[Selection-List Navigation](/architecture/list-navigation.md).

## Semantics

A picker embeds one `ui.SpeedSearch`, resets it when it opens, and asks it for
the rows it should render. The key rules exist because the pickers already
spent most of their keyboard:

- **Printable keys are the query.** They append to it and re-narrow the rows
  live. This is why a speed-searchable picker runs on `ui.NavDefault` rather
  than `NavFull`: `j`/`k`/`g`/`G` would eat the query's first rune.
- **`space` is never consumed.** Every picker toggles the row under the cursor
  with it, and a type-ahead that stole the toggle would be a bad trade.
- **`backspace` peels one rune, then falls through.** While a query is running
  it deletes its last rune; once the query is empty the picker gets the key
  back and does its own thing with it (the mutation pickers clear the whole
  selection, the filter overlay clears the row's section). So both meanings
  stay reachable, in the order a user needs them.
- **`esc` clears before it closes.** The first press drops the query and
  restores every row, the second closes the modal. `EscClears()` reports which
  of the two happened, so the caller never has to track it.
- **The narrowing filters, it does not re-rank.** Matching rows keep their
  original order, so a row never jumps around under the cursor while the query
  grows.
- **The match is a case-insensitive substring**, deliberately *not* the fuzzy
  subsequence `internal/fuzzy` gives the palette and the issue-list match row.
  A type-ahead is read as literal typing: with a subsequence match, `bro` in
  the action menu also hits "Filter **b**y label (the filte**r**'s label
  secti**o**n)", and narrowing that surprises is worse than none.
- **A fruitless query is not a dead end.** A picker whose query matches
  nothing renders one faint placeholder row (`(nothing matches zzz)`) instead
  of collapsing, so the cursor still has somewhere to sit while the query is
  edited back into something that matches.

## API

```go
// internal/ui/speedsearch.go
type SpeedSearch struct{ /* … */ }          // the zero value is idle

(*SpeedSearch) Query() string               // the typed text, "" when idle
(*SpeedSearch) Active() bool                // is a query narrowing the rows?
(*SpeedSearch) Reset()                      // drop it (call when a modal opens)
(*SpeedSearch) EscClears() bool             // esc's first job; false = close the modal
(*SpeedSearch) Key(msg) (handled, changed bool)
(*SpeedSearch) Matches(text string) bool
(*SpeedSearch) Filter(rows []string) []int  // matching indices, in row order
(*SpeedSearch) Hint() string                // "/bug▏" for the modal's heading

Narrow[T](s, items []T, label func(T) string) []T
NarrowStrings(s, rows []string) []string
```

`Key` reports `changed` separately from `handled`, because a caller that sees
`changed` must re-clamp its cursor: the visible row set just moved.

`Narrow` is generic over the row type and takes the text to match against, so
a picker can search a colored chip, a login, or a whole sentence — the action
menu matches `key + " " + label` together, so the key a user half remembers
finds its row too.

## The rendering contract

Two things a speed-searchable picker owes its user:

- **The query is visible.** The heading carries `Hint()` (`Labels of #12
  /bug▏`) — the one line every picker already has, so no layout changes with
  the query appearing.
- **The footer says what typing does.** It reads `type to narrow · space
  toggle · …` while idle and switches to `typing narrows · backspace deletes ·
  … · esc clears the search` while a query runs.

## Narrowed rows are hidden, not unticked

The one correctness rule for a picker with a working set: **the query is a
view, not an edit**. The issues pane keeps `editRows()` (every row) apart from
`editViewRows()` (what the query left). Cursor, row count, rendering and the
`space` toggle index into the *narrowed* set; the `enter` that writes the
change reads the *full* one. Without the split, typing `enh` and pressing
`enter` in the label picker would remove every label the query happened to
hide.

## Where it is used

The issues tool window (#2111): the label and assignee mutation pickers, the
filter overlay's label section, and the action menu. The filter overlay is the
only compound case — its fixed rows (match input, state radio, sort cycle,
grouping toggle) are toggles, not a list, and are never narrowed. Typing only
starts a query once the cursor is inside the label section, and while a query
runs the section **owns the overlay**: the cursor stays inside the narrowed
labels and `up`/`down` wrap within them, so a label named `state` is reachable
even though the row above is called state.

The helper is deliberately free of any pane dependency, so the other modal
pickers (pins, local history, VCS history, the setup wizards) can adopt it
without a refactor.

## See also

- [Selection-List Navigation](/architecture/list-navigation.md)
- [Issues Tool Window](/architecture/github-issues.md)
- [Single-Line Text Input](/architecture/text-input.md)
- [Command Palette](/architecture/command-palette.md)
