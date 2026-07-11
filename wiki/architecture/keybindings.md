---
type: concept
title: Keybindings & Shortcuts
description: The keybinding layer between the registry and config — a chord/key model, JetBrains-like default set, context-scoped resolution with multi-step chords and timeout, build-time conflict detection, platform normalisation, and a cheatsheet view. Binds keys to command ids; defines no commands.
resource: internal/keymap
tags: [architecture, keymap, keybindings, chords, jetbrains, bubbletea]
timestamp: 2026-07-11T11:30:00Z
---

# Keybindings & Shortcuts

Roadmap 0080. `internal/keymap` owns the layer that resolves a **key chord** (in
a focus **context**) to a registered **Command id**. Roadmap 0020 defines the
`Keymap` capability and the registry; Roadmap 0040 owns the `[keymap]` config
section and its precedence. This package sits between them: the binding *model*,
the *default* JetBrains-flavoured set, scope/context resolution, conflict
detection, platform normalisation, and a help/cheatsheet view.

It **defines no Commands.** A binding is `(Chord, Context) → commandID`; the
target ids are owned by the editor (06), explorer (05), palette (07), project
switching (09), and a future VCS roadmap. If a command id is not registered the
binding is **inert** — it still appears in the cheatsheet, but pressing it falls
through to the focused pane. The exception is an id documented in the blocked
ledger: pressing such a chord consumes the key and raises an info toast naming
the blocking dependency (#267), so a dead default reads as "not yet" rather
than as a typo.

## The binding model

- **`Key`** (`key.go`) — a base key (`a`, `f7`, `esc`, `left-bracket`, `/`) plus
  a `Mod` bitset (`Meta`/`Ctrl`/`Alt`/`Shift`). Authors write logical modifiers;
  `Meta` (Cmd) is folded to a concrete modifier at build time. Glyph spellings
  canonicalise in `ParseKey`'s `baseAlias` (`[` → `left-bracket`, `]` →
  `right-bracket`), so a modified press like `cmd+[` normalises the same way
  as a bare one and matches the default table (#284).
- **`Chord`** (`chord.go`) — an ordered list of `Key` steps. One type models all
  three shapes: single (`esc`), modified (`cmd+t`), multi-step (`cmd+k cmd+c`).
- **`parse.go`** — `ParseChord`/`ParseKey` accept whitespace-separated steps with
  `+`-joined modifier tokens; `String()` renders the canonical form (modifiers in
  fixed order meta, ctrl, alt, shift), so parse→format→parse is idempotent. A bare
  uppercase letter folds to base+`Shift`; an underscore base is rejected (so the
  `focus_*` config stopgap sharing the bindings map is treated as a non-chord).
- **`Binding`** (`binding.go`) — `Chord`, `Command`, `Context`, presentation
  metadata (`Title`, `Owner`), a `Fragile` flag, and a `Layer` (default < user <
  project).

## Context & precedence

`Context` (`context.go`) values equal the context ids panes advertise (`editor`,
`explorer`, `palette`); the zero value `Global` matches everywhere. A chord
resolves against the **active** focus context, preferring the most specific match:
a pane-scoped binding shadows a `Global` one for the same chord while that pane is
focused.

## Platform normalisation

`platform.go` folds the logical `Meta` modifier once at table-build time: on macOS
the terminal can forward Cmd as Meta, so `Meta` is kept; everywhere else `Cmd → Ctrl`.
Normalisation is idempotent, so the resolver only ever compares concrete keys.

## Table & conflicts

`BuildTable(defaults, overrides, goos)` (`table.go`) starts from the normalised
default set and overlays the merged `[keymap.bindings]` map (chord string →
command id; `""` unbinds). Overrides arrive already merged by config precedence
(04), so each non-empty entry replaces matching default chords (a brand-new chord
becomes a `Global` user binding). `conflict.go` then detects same chord+context
clashes: it keeps the highest-`Layer` binding and surfaces the rest as non-fatal
diagnostics — never a silent shadow. Unparseable override keys are skipped as
diagnostics too.

## Resolver

`Resolver` (`resolver.go`) feeds keys against the table, tracking partial
multi-step state. Each `Feed(key, context)` returns:

- **Pending** — the sequence is a prefix of a longer chord; the caller arms a
  `TimeoutDuration` (600ms) timer. A prefix wins over an equal-length exact match
  (so `cmd+k cmd+c` stays reachable); the exact `cmd+k` is recovered on `Timeout`.
- **Resolved** — a binding matched; `Command` carries the id.
- **NoMatch** — nothing; the caller lets the key fall through. An aborted prefix
  restarts the sequence from the new key rather than stranding it.

`fromkeymsg.go` adapts a Bubble Tea v2 `tea.KeyPressMsg` into a `Key`. It reads the
press purely through `String()` — v2 still encodes modifiers as `ctrl+/alt+/shift+`
tokens and names specials (`esc`, `space`, `f7`, `left`, …) — so the same code that
parses authored chords (`ParseKey`) parses live keys. (See
former Roadmap 0085, spec in git history, for the v1→v2 key-model change:
`key.Type`/`key.Runes` became `key.Code`/`key.Text`/`key.Mod`.)

## Root-model integration

`internal/app` builds the resolver from config (`buildKeymap`) and, in its
`tea.KeyPressMsg` path, attempts resolution before pane dispatch (`resolveKeymap`):

- In a text-capturing editor (insert mode) only **modified** chords — or a chord
  already in progress — are eligible; plain letters always reach the editor.
- A **Resolved** id that names a registered command runs it via `host.API`; an
  inert id falls through — unless the blocked ledger documents it, in which
  case the chord is consumed with an explanatory toast (#267). **Pending**
  swallows the key and schedules a
  `keymapTimeoutMsg`; on timeout the held chord resolves as an exact binding or is
  discarded.

## Terminal limits & fallback

Many modifier combos (`Cmd+T`, `Ctrl+Tab`, `Cmd+1`) are intercepted by the
terminal/OS and never reach the program; such bindings carry a `Fragile` flag so
the cheatsheet can show the palette/leader fallback. Every bound action stays
reachable from the command palette (07), the universal escape hatch.

## Default set

The JetBrains-flavoured defaults live in `defaults.go` as data (chord, command id,
title, context, owner, fragile). `help.go` groups the effective bindings by
context (Global first) for the `palette.keymapHelp` cheatsheet.

Actions whose JetBrains chord uses `Cmd` — undeliverable in macOS terminals —
additionally get an everywhere-deliverable `Ctrl` chord: undo (`ctrl+z`), redo
(`ctrl+shift+z`), and save (`ctrl+s`, alongside `cmd+s`; raw mode disables XOFF
flow control, so `ctrl+s` arrives as a normal key). Likewise the tab family
(#248): on macOS Option is a composition key (no Alt at all — and QWERTZ needs
Option for `{}[]|~@`, so option-as-meta is not viable), so the tab commands get
delivered primaries following the terminal-tab-cycling convention —
`ctrl+pgdown`/`ctrl+pgup` cycle tabs, `ctrl+shift+pgdown`/`ctrl+shift+pgup`
move the active tab. Because Macs without physical page keys lose
`fn+ctrl+arrows` to macOS globals, the tab family carries an `alt+end`/
`alt+home` pair (`alt+shift+end`/`alt+shift+home` to move) as additional
defaults (#328) — `fn+option+left/right` arrives as exactly these chords, and
named CSI-parameter keys keep their Alt modifier even in Option-as-compose
terminals. Save targets `editor.write`,
the command the editor registers for `:w`, and works from insert mode because
modified chords stay eligible for the keymap layer.

Root-model actions are exposed as registry commands by the compile-in `app`
plugin (`internal/app/commands.go`), so their default bindings are live and the
palette can invoke them: `editor.closeTab` (`cmd+w`, same behavior as the
hardcoded `ctrl+w` / the editor's `:q`), `palette.keymapHelp` (`f1`,
`cmd+k cmd+s` — the cheatsheet overlay; the hardcoded `?`/`f1` branch remains
as the registry-less fallback), `pane.switcher` (`ctrl+tab`, still flagged
fragile; same cycle as the hardcoded `tab`), and `project.goToFile`
(`cmd+shift+o`, the centered palette locked to the `@` file mode).

Every default binding's command id is either **registered** (live) or listed in
the **blocked ledger** (`blocked.go`) with the dependency that unblocks it —
the coverage test in `internal/app` (`TestNoSilentlyDeadDefaultBindings`) fails
on ids that are neither (silently dead) or both (stale ledger entry). Live
since the 0081/20 reconciliation: `editor.find` (`cmd+f`, opens the vim `/`
search), `editor.duplicateLine` (`cmd+d`), `editor.saveAll` (`cmd+shift+s`),
`explorer.toggle` (`cmd+1`, focus flip between tree and editor), and `cmd+b`
reconciled onto the registered `lsp.definition` id (instead of the forked
`editor.gotoDeclaration`).

Editor clipboard and line navigation are live default bindings: `cmd+c` /
`cmd+x` / `cmd+v` target the registered `editor.copy` / `editor.cut` /
`editor.paste` commands (visual selection or current line, through the system
clipboard via the `"+` register), and `cmd+left` / `cmd+right` target
`editor.lineStart` / `editor.lineEnd`. Word/paragraph navigation
(`alt+arrows`, with `ctrl+arrows` fallback) and `shift+arrow` /
`shift+alt+arrow` selection are vim-layer keys handled inside the editor, not
rows in this table — the alt+arrow tab-cycling secondaries were removed for
this (#303); tab cycling keeps its delivered `ctrl+pgup/pgdown` primaries plus
the `alt+home/end` pair (#328).

## Keymap editor (Roadmap 0160, #93)

The settings panel's **Keymap** page (`internal/settings/keymap_page.go`, a
`settings.PageModel`) edits this table interactively:

- It lists the **effective** bindings — chord, command, context, source layer
  (`@default`/`@user`) — rebuilt from the live config on every render;
  blocked-ledger ids render disabled with their unblocking reason (the page
  shows the whole default table truthfully); fragile chords carry ⚠. Typing
  filters; enter starts a **capture**: each key press appends a chord step
  (`keymap.FromKeyMsg` + platform normalisation, multi-step supported), enter
  confirms, esc cancels.
- On confirm the capture runs conflict detection against the effective table;
  a collision names the other command and waits — enter overrides, any other
  key cancels. Capturing a cmd chord (or ctrl+tab/i/m) raises the 0081 honesty
  warning.
- A rebind writes `keymap.bindings.<new-chord> = <command>` and unbinds the
  old chord (`= ""`) in one write-back + reload; `u` unbinds, `r` resets to
  the preset (removes the override). The root model rebuilds its resolver on
  `ConfigReloadedMsg`, so edits re-resolve live.

## Boundaries

Vim-internal keymaps inside the editor (normal/insert/visual motions, operators,
text objects) belong to Roadmap 0060 and are **not** in this table — this package
owns only global / IDE-level shortcuts. `vcs.commit`, `vcs.updateProject`,
`vcs.revertFile` are bound to placeholder ids and stay inert until a future VCS
roadmap registers them.

## Terminal reality: the chord reachability table (0081/10)

Terminal truth beats aspiration: every default chord is classified in
`internal/keymap/reachability.go` (`Classify`/`ReachabilityNote`/
`ReachabilityReport`), and the downstream 0081 work — leader defaults (#14),
discoverability labels (#15), the status matrix (#16) — keys off these
classes, not off JetBrains nostalgia.

| Class | Meaning | Chord families |
|---|---|---|
| **delivered** | arrives in every mainstream terminal | plain keys, `ctrl+letter`, `f1–f12`, `shift+fN` |
| **fragile** | terminal/configuration/protocol dependent | `cmd+*` (Kitty protocol required; OS/terminal menus intercept several), `alt+*` (option-as-meta), `ctrl+shift+letter` (collapses without Kitty disambiguation), `ctrl+tab` (terminal-eaten) |

The ctrl+shift collapse only affects **character keys**: CSI-parameter-encoded
keys (arrows, home/end, pgup/pgdown, insert/delete, fN) carry their modifier
bitset in the legacy encoding (`CSI 5;6~` = ctrl+shift+pgup), so chords like
`ctrl+shift+pgup` are **delivered** (`csiParamEncoded` in `reachability.go`).
The C0-mapped keys (enter, tab, space, esc, backspace) are not exempt.
| **undetectable** | invisible to key-press events | bare-modifier taps (`shift shift` — needs key-up reporting) |

Multi-step chords take the worst class of their steps.

**Probe** (`cmd/keyprobe`): run it in a target terminal, press the listed
chords, finish with `ctrl+d`; it prints one `PROBE\t<chord>\t<state>` line
per target (parsed by `keymap.ParseProbeReport`), recording collapse evidence
(`got=<key>`) when a shifted chord arrives as its unshifted twin.

Ground truth recorded 2026-07 (tmux 3.x on macOS, client announcing the Kitty
protocol):

- `ctrl+tab` — **not delivered** (tmux consumes it; the reason the terminal
  pane's reliable escape hatch is `alt+f12`, not the `pane.switcher` chord).
- `ctrl+shift+z` — **not delivered as itself**: arrives collapsed as
  `ctrl+z` (`got=ctrl+z` in the probe report), confirming the
  ctrl+shift-collapse rule.
- `alt+*` (letters, digits, F-keys, arrows, enter) — delivered (ESC-prefix
  encoding).
- `cmd+*` — delivered **when sent as Kitty CSI-u sequences**; plain macOS
  terminals without the protocol swallow them.
- plain keys, `ctrl+letter`, `f1/f6/f10`, `shift+f6` — delivered.

## Leader key (0081/30)

Every fragile-primary action has a reachable two-keystroke path, driven by
the existing multi-step resolver (no new engine):

- **`space <mnemonic>`** — plain keys never reach the chord layer inside a
  capturing editor, so the space leader is automatically scoped to "outside
  the editor" (explorer, terminal-less panes). Tunable via `[keymap]
  leader = "<key>"`.
- **`ctrl+k <mnemonic>`** — ctrl-modified chords are eligible everywhere,
  making this the universal variant that also works mid-edit.

Curated mnemonics (`internal/keymap/leader.go`): `f` go to file, `g` find in
path, `r` replace in path, `p` switch project, `t` toggle terminal, `T` new
terminal, `h` notification history, `e` explorer/editor toggle, `s` save all,
`w` save, `d` definition, `u` usages, `a` code actions, `n` rename, `l`
reformat, `c` comment line, `x` close tab, `o` reopen tab, `m` recent files
(MRU), `b` navigate back, `i` navigate forward, `,` settings, `1–9` tab N. The long tail stays reachable through the palette (`ctrl+p`,
delivered everywhere).

**Honest fragility**: the per-row `fragile` flags are no longer
hand-maintained — `Defaults()` derives them from the reachability table
(`Classify`), so every `cmd+*`/`alt+*`/collapsing chord now reports itself
truthfully. A completeness test enforces that every fragile, non-blocked
default has a leader mnemonic, another delivered chord, or a documented
exception (vim-native equivalents, palette reach).

## Discoverability (0081/40)

- **Which-key**: holding a chord prefix (the leader, `ctrl+k`, `cmd+k`)
  pops a bottom-centered panel listing the available continuations — letter
  mnemonics first, digits next — built live from the resolver's pending
  state (`Resolver.PendingContinuations` / `BindingTable.Continuations`).
  It clears on resolve, timeout or abort.
- **Live, honest labels** (`keymap.LiveBindings`): the cheatsheet and the
  palette's shortcut column read the *effective* table through a stable
  holder that follows every keymap reload. Labelling is honest by rule:
  a delivered chord shows plainly (`ctrl+s`; leader rows count, so
  `lsp.definition` shows `space d` instead of the fragile `cmd+b`); a
  fragile-only binding warns and names the escape (`cmd+d ⚠
  terminal-dependent`, or `… ⚠ use space d`); blocked commands render
  `✗ blocked: <dependency>`.
- **Cheatsheet blocked section**: `palette.keymapHelp` appends a
  "blocked (dependency not landed)" group listing every default binding
  whose command has no owner yet, with its dependency — never hidden,
  never silently inert.

## Per-binding status matrix (0081/50) — the acceptance ledger

Generated from `keymap.StatusMatrix` against the shipped plugin set (run
`IKE_GEN_MATRIX=<file> go test ./cmd/ike -run TestGenerateMatrixMarkdown` to
regenerate); the final-gate test in `cmd/ike` fails the build if any row is
| command | primary | reachability | fallback | status |
|---|---|---|---|---|
| `editor.closeTab` | `cmd+w` | fragile | `space x` | live via space x |
| `editor.commentBlock` | `cmd+shift+7` | fragile | `palette` | live via palette |
| `editor.commentLine` | `cmd+7` | fragile | `space c` | live via space c |
| `editor.copy` | `cmd+c` | fragile | `vim y` | live via vim y |
| `editor.cut` | `cmd+x` | fragile | `vim d` | live via vim d |
| `editor.duplicateLine` | `cmd+d` | fragile | `vim yyp` | live via vim yyp |
| `editor.find` | `cmd+f` | fragile | `vim /` | live via vim / |
| `editor.lineEnd` | `cmd+right` | fragile | `vim $` | live via vim $ |
| `editor.lineStart` | `cmd+left` | fragile | `vim 0` | live via vim 0 |
| `editor.paste` | `cmd+v` | fragile | `vim p` | live via vim p |
| `editor.redo` | `cmd+shift+z` | fragile | `vim ctrl+r` | live via vim ctrl+r |
| `editor.replace` | `cmd+r` | fragile | `space shift+r` | live via space shift+r |
| `editor.saveAll` | `cmd+shift+s` | fragile | `space s` | live via space s |
| `editor.splitViewDown` | `cmd+k shift+down` | fragile | `palette` | live via palette |
| `editor.splitViewRight` | `cmd+k shift+right` | fragile | `palette` | live via palette |
| `editor.tab.moveLeft` | `ctrl+shift+pgup` | delivered | `—` | live |
| `editor.tab.moveRight` | `ctrl+shift+pgdown` | delivered | `—` | live |
| `editor.tab.next` | `ctrl+pgdown` | delivered | `—` | live |
| `editor.tab.prev` | `ctrl+pgup` | delivered | `—` | live |
| `editor.tab.reopenClosed` | `alt+shift+t` | fragile | `space o` | live via space o |
| `editor.tab.select1` | `alt+1` | fragile | `space 1` | live via space 1 |
| `editor.tab.select2` | `alt+2` | fragile | `space 2` | live via space 2 |
| `editor.tab.select3` | `alt+3` | fragile | `space 3` | live via space 3 |
| `editor.tab.select4` | `alt+4` | fragile | `space 4` | live via space 4 |
| `editor.tab.select5` | `alt+5` | fragile | `space 5` | live via space 5 |
| `editor.tab.select6` | `alt+6` | fragile | `space 6` | live via space 6 |
| `editor.tab.select7` | `alt+7` | fragile | `space 7` | live via space 7 |
| `editor.tab.select8` | `alt+8` | fragile | `space 8` | live via space 8 |
| `editor.tab.select9` | `alt+9` | fragile | `space 9` | live via space 9 |
| `editor.undo` | `ctrl+z` | delivered | `—` | live |
| `editor.write` | `cmd+s` | fragile | `ctrl+s` | live via ctrl+s |
| `explorer.redo` | `cmd+shift+z` | fragile | `palette` | live via palette |
| `explorer.reveal` | `alt+f1` | fragile | `palette` | live via palette |
| `explorer.toggle` | `cmd+1` | fragile | `space e` | live via space e |
| `explorer.undo` | `ctrl+z` | delivered | `—` | live |
| `file.move` | `f6` | delivered | `—` | live |
| `file.rename` | `shift+f6` | delivered | `—` | live |
| `lsp.codeAction` | `alt+enter` | fragile | `space a` | live via space a |
| `lsp.definition` | `cmd+b` | fragile | `space d` | live via space d |
| `lsp.format` | `cmd+alt+l` | fragile | `space l` | live via space l |
| `lsp.references` | `alt+f7` | fragile | `space u` | live via space u |
| `lsp.rename` | `space n` | delivered | `—` | live |
| `menu.open` | `f10` | delivered | `—` | live |
| `nav.back` | `cmd+left-bracket` | fragile | `space b` | live via space b |
| `nav.forward` | `cmd+right-bracket` | fragile | `space i` | live via space i |
| `notifications.history` | `space h` | delivered | `—` | live |
| `palette.keymapHelp` | `cmd+k cmd+s` | fragile | `f1` | live via f1 |
| `palette.recentFiles` | `cmd+e` | fragile | `space m` | live via space m |
| `palette.searchEverywhere` | `cmd+shift+a` | fragile | `space space` | live via space space |
| `pane.splitDown` | `cmd+k down` | fragile | `palette` | live via palette |
| `pane.splitLeft` | `cmd+k left` | fragile | `palette` | live via palette |
| `pane.splitRight` | `cmd+k right` | fragile | `palette` | live via palette |
| `pane.splitUp` | `cmd+k up` | fragile | `palette` | live via palette |
| `pane.switcher` | `ctrl+tab` | fragile | `tab key` | live via tab key |
| `project.findInPath` | `cmd+shift+f` | fragile | `space g` | live via space g |
| `project.goToClass` | `cmd+o` | fragile | `space shift+s` | live via space shift+s |
| `project.goToFile` | `cmd+shift+o` | fragile | `space f` | live via space f |
| `project.replaceInPath` | `cmd+shift+r` | fragile | `space r` | live via space r |
| `project.switch` | `alt+shift+p` | fragile | `space p` | live via space p |
| `search.nextMatch` | `f3` | delivered | `—` | live |
| `search.prevMatch` | `shift+f3` | delivered | `—` | live |
| `settings.open` | `cmd+,` | fragile | `space ,` | live via space , |
| `terminal.new` | `space shift+t` | delivered | `—` | live |
| `terminal.toggle` | `alt+f12` | fragile | `space t` | live via space t |
| `vcs.commit` | `cmd+k` | fragile | `—` | blocked: VCS integration (idea #28) |
| `vcs.revertFile` | `cmd+shift+t` | fragile | `—` | blocked: VCS integration (idea #28) |
| `vcs.updateProject` | `cmd+t` | fragile | `—` | blocked: VCS integration (idea #28) |
