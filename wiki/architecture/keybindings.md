---
type: concept
title: Keybindings & Shortcuts
description: The keybinding layer between the registry and config — a chord/key model, JetBrains-like default set, context-scoped resolution with multi-step chords and timeout, build-time conflict detection, platform normalisation, and a cheatsheet view. Binds keys to command ids; defines no commands.
resource: internal/keymap
tags: [architecture, keymap, keybindings, chords, jetbrains, bubbletea]
timestamp: 2026-07-18T00:00:00Z
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
  three shapes: single (`esc`), modified (`cmd+t`), multi-step (`cmd+k z`).
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
  (so `cmd+k down` stays reachable); the exact `cmd+k` is recovered on `Timeout`.
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
the cheatsheet can show the palette fallback. Every bound action stays
reachable from the command palette (07), the universal escape hatch.

## Default set

The JetBrains-flavoured defaults live in `defaults.go` as data (chord, command id,
title, context, owner, fragile). `help.go` groups the effective bindings by
context (Global first) for the `palette.keymapHelp` cheatsheet.

Actions whose JetBrains chord uses `Cmd` — undeliverable in macOS terminals —
additionally get an everywhere-deliverable `Ctrl` chord: undo (`ctrl+z`), redo
(`ctrl+shift+z`), and save (`ctrl+s`, alongside `cmd+s`; raw mode disables XOFF
flow control, so `ctrl+s` arrives as a normal key). Tab cycling follows
JetBrains' macOS keymap export: `ctrl+cmd+right`/`ctrl+cmd+left` cycle tabs
(with `ctrl+alt+right`/`ctrl+alt+left` as secondaries), while
`ctrl+shift+pgdown`/`ctrl+shift+pgup` move the active tab. These Cmd/Option
chords only reach a TUI in a terminal that forwards the modifiers (Ghostty with
the Kitty protocol) — accepted per user preference; the palette is the
delivered fallback for `editor.tab.next`/`editor.tab.prev`. Save targets
`editor.write`,
the command the editor registers for `:w`, and works from insert mode because
modified chords stay eligible for the keymap layer.

Root-model actions are exposed as registry commands by the compile-in `app`
plugin (`internal/app/commands.go`), so their default bindings are live and the
palette can invoke them: `editor.closeTab` (`cmd+w`, same behavior as the
hardcoded `ctrl+w` / the editor's `:q`), `palette.keymapHelp` (`f1` —
the cheatsheet overlay; the hardcoded `?`/`f1` branch remains
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
`editor.gotoDeclaration`). Since the 0082 sheet-11/13 verdicts (#18),
`lsp.definition` also has `f4` (JetBrains jump-to-source) as its delivered
primary, and `shift+f6` is context-aware refactor-rename: `lsp.rename` with an
editor focused, `file.rename` everywhere else (the Editor row shadows the
Global one). Quick documentation (`lsp.hover`, #378) binds `ctrl+q` — the
JetBrains Windows/Linux quick-doc chord, delivered everywhere because raw mode
disables XON flow control. Diagnostic navigation (#369) binds `f2` /
`shift+f2` — the JetBrains next/previous-highlighted-error keys, both
delivered — to `lsp.nextDiagnostic` / `lsp.prevDiagnostic`, which walk the
focused document's cached diagnostics in document order (wrapping) and toast
the message. Parameter info (#523) binds `ctrl+p` — the palette's former
default toggle chord, freed because the palette's primary entry is esc-esc
(`palette.toggle_key` now defaults to empty and stays configurable) — plus
`cmd+p` (the JetBrains chord) for terminals that deliver Cmd; both rows
collapse to one `ctrl+p` binding off macOS. `lsp.parameterInfo` opens the
signature-help popup on demand, in insert and normal mode.

Editor clipboard and line navigation are live default bindings: `cmd+c` /
`cmd+x` / `cmd+v` target the registered `editor.copy` / `editor.cut` /
`editor.paste` commands (visual selection or current line, through the system
clipboard via the `"+` register), and `cmd+left` / `cmd+right` target
`editor.lineStart` (also `home`) / `editor.lineEnd`. Word/paragraph navigation
(`alt+arrows`, with `ctrl+arrows` fallback) and `shift+arrow` /
`shift+alt+arrow` selection are vim-layer keys handled inside the editor, not
rows in this table. Tab cycling uses JetBrains' `ctrl+cmd+arrow` primaries with
`ctrl+alt+arrow` secondaries (see above).

## Keymap editor (Roadmap 0160, #93)

The settings panel's **Keymap** page (`internal/settings/keymap_page.go`, a
`settings.PageModel`) edits this table interactively:

- It lists the **effective** bindings — chord, command, context, source layer
  (`@default`/`@user`) — rebuilt from the live config on every render;
  blocked-ledger ids render disabled with their unblocking reason (the page
  shows the whole default table truthfully); fragile chords carry ⚠. `/` opens
  the filter input (#531) — while it is open every printable key is filter
  text (including the action letters `u`/`r`/`j`/`k`), enter keeps the filter,
  esc clears it; enter on a row starts a **capture**: each key press appends a
  chord step
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

## JetBrains keymap XML import (#677)

`internal/keymap/jbimport` translates a JetBrains IDE keymap export
(`<keymap version="1"><action id="SaveDocument"><keyboard-shortcut
first-keystroke="meta pressed S"/></action></keymap>`) into
`keymap.bindings.*` overrides:

- **Keystroke grammar** — `ParseKeystroke` reads the Swing keystroke tokens
  (modifiers `meta`/`ctrl`/`control`/`alt`/`shift`, the `pressed`/`typed`
  filler, then one key token: letters, digits, `F1`…`F24`, or `VK_` names like
  `ENTER`/`BACK_SPACE`/`OPEN_BRACKET`) and emits a canonical logical IKE step
  (`meta` stays `cmd`; platform folding happens at table build). A
  `second-keystroke` becomes a multi-step chord (`cmd+k cmd+s`).
  Mouse shortcuts are ignored; untranslatable keystrokes (e.g. `PERIOD`,
  whose `.` cannot round-trip the dotted config key) are collected in
  `Result.Skipped`, never fatal.
- **Action mapping** — a table maps IntelliJ action ids onto IKE command ids
  (`SaveDocument`→`editor.write`, `GotoDeclaration`→`lsp.definition`,
  `FindInPath`→`project.findInPath`, `ReformatCode`→`lsp.format`, run/debug,
  VCS, tabs, tool windows, …), covering every default-set command with a
  plausible JetBrains counterpart. Unmapped ids land in `Result.Unmapped`.
- **Semantics** — `Plan` yields `Bind` (chord→command overrides) and `Unbind`:
  preset-default chords of imported commands the export did not keep are
  written `= ""`, so the imported chord *replaces* the default rather than
  joining it (matching the keymap page's unbind semantics — an unbind drops
  the whole chord across contexts). `Apply` writes both sets through the
  caller's writer (config.WriteKey at **user scope**) and the normal reload
  pipeline re-resolves the table.
- **Entry points** — the palette command `keymap.importJetBrains`
  (`internal/app/jbimport_prompt.go`: a shell prompt with tab filesystem
  completion via `pathcomplete`, summary toast via `host.Notify`) and the
  settings Keymap page's `i` action (inline path input with the shared
  `pathSuggest` completion; the summary lands in the pinned footer).

## Boundaries

Vim-internal keymaps inside the editor (normal/insert/visual motions, operators,
text objects) belong to Roadmap 0060 and are **not** in this table — this package
owns only global / IDE-level shortcuts. The VCS ids (`vcs.commit`,
`vcs.updateProject`, `vcs.revertFile`, …) went live with Epic 0320 — see
[VCS / Git Integration](/architecture/vcs.md); their fragile Cmd primaries stay
reachable through the palette, and the
blocked ledger is currently empty (its machinery stays test-covered through
`keymap.StubBlockedForTest`).

## Terminal reality: the chord reachability table (0081/10)

Terminal truth beats aspiration: every default chord is classified in
`internal/keymap/reachability.go` (`Classify`/`ReachabilityNote`/
`ReachabilityReport`), and the downstream 0081 work — default re-picks (#14),
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

## Modifier-chord policy (#711)

The leader layer (space/`ctrl+k` mnemonics, 0081/30) is **retired**: every
default binding is a single modifier chord (`cmd`/`ctrl`/`alt`/`shift` + key,
or a delivered F-key/named key), matching JetBrains wherever a JetBrains
default exists. Exactly five multi-step sequences remain, all under the
`cmd+k` prefix: `cmd+k down/up/left/right` (pane splits) and `cmd+k z`
(maximize pane). `shift shift` stays as JetBrains' double-shift double-tap.
A policy test (`TestAllDefaultsAreModifierChords`) enforces both rules. The
`[keymap] leader` config key is gone.

**Honest fragility**: the per-row `fragile` flags are no longer
hand-maintained — `Defaults()` derives them from the reachability table
(`Classify`), so every `cmd+*`/`alt+*`/collapsing chord now reports itself
truthfully. A completeness test enforces that every fragile, non-blocked
default has another delivered chord or a documented exception (vim-native
equivalents, palette reach via esc-esc).

## Discoverability (0081/40)

- **Which-key**: holding a chord prefix (`cmd+k`)
  pops a bottom-centered panel listing the available continuations — letter
  mnemonics first, digits next — built live from the resolver's pending
  state (`Resolver.PendingContinuations` / `BindingTable.Continuations`).
  It clears on resolve, timeout or abort.
- **Live, honest labels** (`keymap.LiveBindings`): the cheatsheet and the
  palette's shortcut column read the *effective* table through a stable
  holder that follows every keymap reload. Labelling is honest by rule:
  a delivered chord shows plainly (`ctrl+s`; fewer keystrokes win before
  shorter labels, so `lsp.rename` shows the single-step `shift+f6`); a
  fragile-only binding warns (`cmd+d ⚠ terminal-dependent`); blocked
  commands render `✗ blocked: <dependency>`.
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
| `debug.continue` | `f9` | delivered | `—` | live |
| `debug.start` | `shift+f9` | delivered | `—` | live |
| `debug.stepInto` | `f7` | delivered | `—` | live |
| `debug.stepOut` | `shift+f8` | delivered | `—` | live |
| `debug.stepOver` | `f8` | delivered | `—` | live |
| `debug.toggleBreakpoint` | `ctrl+f8` | delivered | `—` | live |
| `diff.nextChange` | `f7` | delivered | `—` | live |
| `diff.prevChange` | `shift+f7` | delivered | `—` | live |
| `editor.caret.addAll` | `ctrl+shift+g` | fragile | `palette` | live via palette |
| `editor.caret.addNext` | `ctrl+g` | delivered | `—` | live |
| `editor.closeTab` | `cmd+w` | fragile | `palette` | live via palette |
| `editor.commentBlock` | `cmd+shift+7` | fragile | `palette` | live via palette |
| `editor.commentLine` | `cmd+7` | fragile | `palette` | live via palette |
| `editor.copy` | `cmd+c` | fragile | `vim y` | live via vim y |
| `editor.cut` | `cmd+x` | fragile | `vim d` | live via vim d |
| `editor.duplicateLine` | `cmd+d` | fragile | `vim yyp` | live via vim yyp |
| `editor.find` | `cmd+f` | fragile | `vim /` | live via vim / |
| `editor.lineEnd` | `cmd+right` | fragile | `vim $` | live via vim $ |
| `editor.lineStart` | `cmd+left` | fragile | `home` | live via home |
| `editor.paste` | `cmd+v` | fragile | `vim p` | live via vim p |
| `editor.pasteFromHistory` | `cmd+shift+v` | fragile | `palette` | live via palette |
| `editor.redo` | `cmd+shift+z` | fragile | `vim ctrl+r` | live via vim ctrl+r |
| `editor.replace` | `cmd+r` | fragile | `palette` | live via palette |
| `editor.saveAll` | `cmd+shift+s` | fragile | `palette` | live via palette |
| `editor.splitViewDown` | `cmd+alt+shift+down` | fragile | `palette` | live via palette |
| `editor.splitViewRight` | `cmd+alt+shift+right` | fragile | `palette` | live via palette |
| `editor.tab.moveLeft` | `ctrl+shift+pgup` | delivered | `—` | live |
| `editor.tab.moveRight` | `ctrl+shift+pgdown` | delivered | `—` | live |
| `editor.tab.next` | `cmd+ctrl+right` | fragile | `palette` | live via palette |
| `editor.tab.prev` | `cmd+ctrl+left` | fragile | `palette` | live via palette |
| `editor.tab.reopenClosed` | `alt+shift+t` | fragile | `palette` | live via palette |
| `editor.tab.select1` | `alt+1` | fragile | `palette` | live via palette |
| `editor.tab.select2` | `alt+2` | fragile | `palette` | live via palette |
| `editor.tab.select3` | `alt+3` | fragile | `palette` | live via palette |
| `editor.tab.select4` | `alt+4` | fragile | `palette` | live via palette |
| `editor.tab.select5` | `alt+5` | fragile | `palette` | live via palette |
| `editor.tab.select6` | `alt+6` | fragile | `palette` | live via palette |
| `editor.tab.select7` | `alt+7` | fragile | `palette` | live via palette |
| `editor.tab.select8` | `alt+8` | fragile | `palette` | live via palette |
| `editor.tab.select9` | `alt+9` | fragile | `palette` | live via palette |
| `editor.undo` | `ctrl+z` | delivered | `—` | live |
| `editor.write` | `cmd+s` | fragile | `ctrl+s` | live via ctrl+s |
| `explorer.redo` | `cmd+shift+z` | fragile | `palette` | live via palette |
| `explorer.reveal` | `alt+f1` | fragile | `palette` | live via palette |
| `explorer.toggle` | `cmd+1` | fragile | `palette` | live via palette |
| `explorer.undo` | `ctrl+z` | delivered | `—` | live |
| `file.move` | `f6` | delivered | `—` | live |
| `file.rename` | `shift+f6` | delivered | `—` | live |
| `lsp.callHierarchy` | `ctrl+alt+h` | fragile | `palette` | live via palette |
| `lsp.codeAction` | `alt+enter` | fragile | `palette` | live via palette |
| `lsp.definition` | `f4` | delivered | `—` | live |
| `lsp.format` | `cmd+alt+l` | fragile | `palette` | live via palette |
| `lsp.hover` | `ctrl+q` | delivered | `—` | live |
| `lsp.nextDiagnostic` | `f2` | delivered | `—` | live |
| `lsp.parameterInfo` | `cmd+p` | fragile | `ctrl+p` | live via ctrl+p |
| `lsp.prevDiagnostic` | `shift+f2` | delivered | `—` | live |
| `lsp.references` | `alt+f7` | fragile | `palette` | live via palette |
| `lsp.rename` | `shift+f6` | delivered | `—` | live |
| `markdown.preview` | `cmd+alt+m` | fragile | `palette` | live via palette |
| `menu.open` | `f10` | delivered | `—` | live |
| `nav.back` | `cmd+left-bracket` | fragile | `palette` | live via palette |
| `nav.forward` | `cmd+right-bracket` | fragile | `palette` | live via palette |
| `notifications.history` | `cmd+alt+n` | fragile | `palette` | live via palette |
| `palette.keymapHelp` | `f1` | delivered | `—` | live |
| `palette.recentFiles` | `cmd+e` | fragile | `palette` | live via palette |
| `palette.searchEverywhere` | `cmd+shift+a` | fragile | `palette (esc esc)` | live via palette (esc esc) |
| `pane.maximize` | `cmd+k z` | fragile | `palette` | live via palette |
| `pane.splitDown` | `cmd+k down` | fragile | `palette` | live via palette |
| `pane.splitLeft` | `cmd+k left` | fragile | `palette` | live via palette |
| `pane.splitRight` | `cmd+k right` | fragile | `palette` | live via palette |
| `pane.splitUp` | `cmd+k up` | fragile | `palette` | live via palette |
| `pane.switcher` | `ctrl+tab` | fragile | `tab key` | live via tab key |
| `project.findInPath` | `cmd+shift+f` | fragile | `palette` | live via palette |
| `project.goToClass` | `cmd+o` | fragile | `palette` | live via palette |
| `project.goToFile` | `cmd+shift+o` | fragile | `palette` | live via palette |
| `project.replaceInPath` | `cmd+shift+r` | fragile | `palette` | live via palette |
| `project.switch` | `cmd+shift+p` | fragile | `palette` | live via palette |
| `run.file` | `shift+f10` | delivered | `—` | live |
| `search.nextMatch` | `f3` | delivered | `—` | live |
| `search.prevMatch` | `shift+f3` | delivered | `—` | live |
| `settings.open` | `cmd+,` | fragile | `palette` | live via palette |
| `terminal.new` | `cmd+alt+t` | fragile | `palette` | live via palette |
| `terminal.toggle` | `alt+f12` | fragile | `palette` | live via palette |
| `todo.list` | `cmd+6` | fragile | `palette` | live via palette |
| `vcs.commit` | `cmd+k` | fragile | `palette` | live via palette |
| `vcs.panel` | `cmd+9` | fragile | `palette` | live via palette |
| `vcs.revertFile` | `cmd+alt+z` | fragile | `palette` | live via palette |
| `vcs.updateProject` | `cmd+t` | fragile | `palette` | live via palette |
