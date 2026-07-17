---
type: concept
title: Welcome Tour
description: Paged first-orientation walkthrough in the floating shell — five pages of entry keys, vim-mode basics, layout, tools, and customization, with resolver-first platform-normalized shortcuts and interactive try-it tasks; opened via the help.welcomeTour palette command.
resource: internal/tour/tour.go
tags: [architecture, onboarding, tour, help, overlay]
timestamp: 2026-07-17T00:00:00Z
---

# Welcome Tour

Roadmap 0110 (#657). A paged **welcome tour**: five short pages that
orient a new user — the entry keys (search everywhere, help, project switch,
how to quit), the vim modal editor trap ("you type and nothing appears —
press `i`"), pane layout and navigation, the tool windows (including the
terminal focus-escape hatch), and customization — ending with the handoff
line to the LSP server setup dialog. The tour itself executes nothing;
since #680 selected pages carry **try-it tasks** (see below) where the
taught key passes through and really drives the app.

Opened via the registered command `help.welcomeTour` ("Welcome Tour" in the
palette; also listed in the help Essentials view).

## First-run wiring (#658)

On a first start (`ui.onboarded` unset — the flag ALONE gates it, #671: main
records the project open into the settings file before the model is built, so
the file exists on every launch) the tour opens automatically once the window
is sized. Startup-prompt precedence: **crash
recovery → welcome tour → LSP onboarding dialog** — the tour waits while the
recovery prompt holds the shell, and the LSP dialog queues behind the tour
(`closeTour` re-triggers it explicitly, since its `maybeOpen` refuses while
the shell is open).

`ui.onboarded = true` persists to the user-scope settings the moment the tour
**opens** — not when it closes — so quitting mid-tour never re-triggers it
and, crucially, never leaves a half-created settings file that would suppress
the LSP dialog. For the same reason the LSP dialog's first-run scan gates on
`lsp.onboarded` alone rather than on the settings file's existence.

## Structure

```
internal/tour/
  tour.go     Tour: ui.Content (Title with "n/5" indicator, Render), page
              state (Next/Prev clamp), curated page copy, chord resolution
internal/app/
  tour.go     host: openTour / updateTour / closeTour; ShowWelcomeTourMsg
  commands.go registers help.welcomeTour
```

## Key routing

The tour is **not** plain scrollable shell content — the floating shell's
scroller owns `space`/arrows. The host handles keys itself (`updateTour`,
same pattern as the LSP onboarding dialog): `→`/`l`/`space`/`enter` page
forward (finishing on the last page), `←`/`h` page back (clamped), `esc`/`q`
close. Every other key is swallowed on a passive page; on a page with an
**unfinished try-it task** (#680) it is instead passed through to normal key
handling (the tour reports "not consumed" and the root model's key switch
continues — skipping the shell-scroller branch). Skip and finish are
identical — no confirmation, task state notwithstanding. Consequently every
page (body + legend) must fit ~72×16 so the shell never scrolls it and
`space` stays unambiguous; a test asserts the budget for fresh AND all-done
task states (the task header becomes the "all done — press → to continue"
hint in place, keeping the row count constant).

## Try-it tasks (#680)

Selected pages declare `TryTask`s (command id + prompt + curated chord,
rendered resolver-first like every row, #678): search everywhere on the
welcome page, the file-tree toggle on the layout page, the terminal toggle
on the tools page — chosen so the chord acts visibly around the shell (pane
toggles) or in an overlay that naturally covers the tour, and never collides
with the paging keys. Each renders as a `[ ]` checkbox row.

Ticking rides the command-executed signal (#679): the root model's
`CommandExecutedMsg` case calls `tour.NoteExecuted(id)`, which marks the task
done on **any** page (trying ahead counts).

Overlay interplay: a try-it command that opens the palette family simply
covers the tour — the View overlay switch prefers the palette over the shell
and the key routing does too, so the tour is hidden and inert while the
overlay is up, and visible again (task ticked) when it closes. A command
that takes the floating shell itself (f1 help) **suspends** the tour:
`tourOpen()` requires the shell content to still be the tour, and
`maybeResumeTour` reopens it on the same page once shell and palette are
free (the resuming key then acts on the tour normally).

## Resolver-truth shortcuts

Shortcuts resolve through a `BindingResolver` (the same narrow seam help
uses) **first** (#678), so the shown chord is always the live keymap's
preferred one (custom > default), read from the platform-normalized
effective table. Multi-bound or fragile commands carry a curated
preferred-order default ("shift shift · cmd+shift+a") that is kept whenever
the resolved chord is one of its options or another known default — the tour
never teaches a possibly-dead chord (double-shift needs kitty-protocol
modifier reporting) alone. A resolved chord outside all known defaults is a
real remap and leads the display; curated vim hints (":w", "?", handled
outside the keymap layer) survive it as secondary options, replaced keymap
chords are dropped. The curated list is only the fallback for unbound
commands, and even then it is platform-normalized for display (Meta→Ctrl off
macOS) — no row ever renders a hardcoded mac chord on Linux/Windows. Every
row with a real command id goes through this path, including the help
cheat-sheet row.

## Design rules

- **The tour never drives the app.** It presents; a try-it key is the USER
  driving the app — the tour only observes the execution signal.
- **Fit, don't scroll.** Pages fit the shell body; no wide ASCII diagrams.
- **Resolver truth.** Never display a chord the live keymap contradicts.
- **Always reopenable.** Skipping is safe; the reopen hint is on every page.
