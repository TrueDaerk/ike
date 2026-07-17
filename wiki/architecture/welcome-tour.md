---
type: concept
title: Welcome Tour
description: Passive, paged first-orientation walkthrough in the floating shell — five pages of entry keys, vim-mode basics, layout, tools, and customization, with resolver-truth shortcuts; opened via the help.welcomeTour palette command.
resource: internal/tour/tour.go
tags: [architecture, onboarding, tour, help, overlay]
timestamp: 2026-07-17T00:00:00Z
---

# Welcome Tour

Roadmap 0110 (#657). A passive, paged **welcome tour**: five short pages that
orient a new user — the entry keys (search everywhere, help, project switch,
how to quit), the vim modal editor trap ("you type and nothing appears —
press `i`"), pane layout and navigation, the tool windows (including the
terminal focus-escape hatch), and customization — ending with the handoff
line to the LSP server setup dialog. It executes nothing and is deliberately
not interactive: no guided exercises, no tips-of-the-day.

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
close; every other key is swallowed. Skip and finish are identical — no
confirmation. Consequently every page (body + legend) must fit ~72×16 so the
shell never scrolls it and `space` stays unambiguous; a test asserts the
budget.

## Resolver-truth shortcuts

Shortcuts render through a `BindingResolver` (the same narrow seam help
uses), so a user remap displays truthfully. Multi-bound or fragile commands
carry a curated preferred-order default ("shift shift · cmd+shift+a") that
the resolver only replaces when the resolved chord is *outside* that list —
the tour never teaches a possibly-dead chord (double-shift needs kitty-
protocol modifier reporting) alone. Unresolved commands keep their curated
default text; commands without a reliable default say "via palette".

## Design rules

- **Passive.** The tour presents; it never drives the app.
- **Fit, don't scroll.** Pages fit the shell body; no wide ASCII diagrams.
- **Resolver truth.** Never display a chord the live keymap contradicts.
- **Always reopenable.** Skipping is safe; the reopen hint is on every page.
