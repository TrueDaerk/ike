---
type: concept
title: Keybindings & Shortcuts
description: The keybinding layer between the registry and config — a chord/key model, JetBrains-like default set, context-scoped resolution (per-pane contexts plus language-scoped editor bindings, one chord per context) with multi-step chords and timeout, build-time conflict detection, platform normalisation, and a cheatsheet view. Binds keys to command ids; defines no commands.
resource: internal/keymap
tags: [architecture, keymap, keybindings, chords, contexts, jetbrains, bubbletea]
timestamp: 2026-09-04T12:00:00Z
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
  The separator can also be the base: a trailing empty part *preceded by another
  empty one* is the literal plus key (#1331) — `"+"` and `"cmd++"` parse to base
  `+`, while a single trailing `+` (`"cmd+"`) stays a missing-base error. That
  makes `String()` → `ParseKey` stable for plus bindings, which previously could
  neither be captured nor read back from config.
- **`Binding`** (`binding.go`) — `Chord`, `Command`, `Context`, presentation
  metadata (`Title`, `Owner`), a `Fragile` flag, and a `Layer` (default < user <
  project).

## Context & precedence

`Context` (`context.go`) values equal the context ids panes advertise
(`internal/pane`'s `ContextID`); the zero value `Global` matches everywhere.
Since #1794 **every focusable pane kind has a context**: `editor`, `explorer`,
`palette`, `diff`, `terminal`, `preview` (markdown preview and image viewer
share the advertised `preview` id), `vcs`, `debug`, `problems`, `structure`,
`usages`, `http`, `breakpoints`, `archive` and `data`. A chord resolves
against the **active** focus context, preferring the most specific match: a
pane-scoped binding shadows a `Global` one for the same chord while that pane
is focused. The specificity order has three levels (#1876) —
`editor[lang]` above `editor` above `Global`; contexts on the same level are
mutually disjoint — so one chord may carry a different command **per context**
without any conflict: the shipped example is `ctrl+t`, a new terminal tab
(`terminal.newTab`) in the `terminal` context and a new empty editor tab
(`editor.tab.new`) in the `editor` context, unbound everywhere else. `ctrl+r`
(#2314) is the second: JetBrains' Rerun chord re-sends the shown request
(`http.resend`) in the `http` context and reloads the listing
(`archive.reload`) in the `archive` one — the two panes that own a "do that
again" action. Both were pane-local keys before, which the keymap layer could
neither list nor rebind, and which the usage log therefore recorded as unbound
presses.

### Language-scoped editor bindings (#1876)

The `editor` context can carry a **language qualifier**: `editor[http]` (the
value `keymap.WithLang(Editor, "http")`) scopes a binding to editors whose
buffer is classified as the language id `http` — by file name through
`internal/lang`, or by the buffer-level override (#2033). `Context.Split`
takes a context apart into pane base and language; `Matches` requires the
active context to carry the same language when the binding side names one.
The **active** context is where the language enters: the root model's
`keyContext()` (`internal/app`) is `focusContext()` narrowed to
`editor[<LangID>]` when the focus is an editor showing a classified buffer,
and it is what every resolver call (`Feed`, `Continues`, `Timeout`,
`PendingContinuations`, the terminal-branch `Lookup`) is fed. Every other
consumer of the context id — palette scoping, registry, help snapshot — keeps
the plain `focusContext()`. A buffer with no registered language leaves the
active context at plain `editor`, so language-scoped bindings simply never
match there.

The motivating case: `"editor[http].cmd+e" = "http.selectEnvironment"` runs
the environment picker only in `.http` buffers; every other editor keeps the
global default `cmd+e` → `palette.recentFiles`. Before #1876 the narrowest
available scope was `editor.cmd+e`, which took the chord in **every** editor.

### One chord, three playgrounds (#2415)

The playgrounds asked the obvious follow-up question: can two commands share
one chord and be told apart by the buffer language — `editor[json]` →
`json.jqPlayground`, `editor[yaml]` → `yaml.yqPlayground`, `editor[xml]` →
`xml.xmqPlayground`? **They can.** Language-scoped contexts sit on the same
specificity level and are mutually **disjoint** (`Context.MoreSpecific`,
`Context.Shadows`): at most one of them matches a given focus, so the build
reports neither a conflict (`TestNoSameContextConflicts` — conflicts are
same-chord/*same*-context) nor a shadow (siblings never hide each other). A
user can therefore write exactly that in their keymap:

```toml
"editor[json].cmd+shift+j" = "json.jqPlayground"
"editor[yaml].cmd+shift+j" = "yaml.yqPlayground"
```

The defaults nonetheless do **not** ship those three rows. Three bindings that
differ only in which dialect they name are three places to edit for every
later language (jsonc, ansible, html …), and the language→playground mapping
would then live in the keymap instead of next to the playgrounds. The default
table ships **one** row instead — the dispatcher command `playground.open`
(`cmd+shift+j`, "Open playground for this file") — which resolves the dialect
from the focused buffer's `LangID` in `internal/app/playgroundopen.go`:
`json`/`jsonc`/`ndjson` → jq, `yaml`/`ansible` → yq, `xml`/`html` → xmq,
anything else → the notification `no playground for <lang>`.

That row is written **twice**, once per context the chord means something in:
`Editor` and `HTTP` (#2451). The response pane is a second source of documents
to query — `q` already opens jq over the shown body (#2157) — and a
context-scoped binding is only dispatched while that context has focus, so an
`Editor`-only row left the chord dead in the viewer. A second row rather than
promoting it to `Global`, following the `http.*` rows: the two places the
command resolves something are named, and every other pane stays free to bind
`cmd+shift+j` to its own thing.

*Collision check for the chord:* `cmd+shift+j` is unclaimed in the defaults,
and so is `ctrl+shift+j`, which it folds onto off macOS. `cmd+alt+p` — the
first proposal — is out: `ctrl+alt+p` is the performance HUD and `cmd+alt+p`
folds onto it on Linux. Like every Cmd chord the row is fragile, and escapes
through the palette and the Tools menu (`reachableAlternatives`).

The per-dialect commands stay exactly as they were: `json.jqPlayground`
(`ctrl+alt+j`) and `yaml.yqPlayground` (`ctrl+alt+y`) keep their own Global
rows, remain separately rebindable, and count separately in the palette's
frecency. Rebinding either side is one line:

```toml
"editor.cmd+shift+j" = "json.jqPlayground"   # the dispatcher's chord, jq only
"editor[yaml].cmd+shift+y" = "yaml.yqPlayground"
```

The `palette` context is a special case (#2055): the palette overlay owns the
keyboard, so a key never reaches the resolver above. The overlay branch of the
root model's key dispatch therefore looks the chord up in the `palette`
context itself (`paletteBindingCmd`, `internal/app/find_panel.go`) and runs it
only when it resolves to a command on a small allowlist — today just
`find.openInPanel` ("Open in Find window", `cmd+enter` / `ctrl+enter`).
Everything else falls through to the overlay's own editing keys, so a
Palette-context binding can never swallow query typing. The bindings appear in
the cheatsheet like any other context group.

### Cross-context shadow detection (#1875, #1876)

That layering is a feature — but it must never happen wordlessly. `shadow.go`
scans the effective (post-conflict) table for two bindings sharing a chord
while naming **different** commands, where one context can hide the other
(`Context.Shadows`): a pane context over `Global`, and — the #1876 level — a
language-scoped `editor[lang]` over both plain `editor` and `Global`, in its
narrower scope only. A `Shadow` records the winner (wins while its scope has
focus) and the hidden binding (unreachable there, still winning everywhere
else). Sibling scopes (`editor[http]` vs `editor[go]`, or two different
panes) never both match a focus and are no shadow. The motivating case: a
user override `editor.cmd+e = http.selectEnvironment` silently swallowed the
global default `cmd+e` → `palette.recentFiles` in every editor — with #1876
the right spelling is `editor[http].cmd+e`, which shadows the default only in
`.http` buffers (and is still reported, naming exactly that scope).

- **Same command = no shadow** — the dual-chord/fallback pattern
  (`editor.write` on both `cmd+s` and `ctrl+s`, `editor.undo` bound per pane)
  stays quiet, as do two pane-scoped bindings in different panes
  (`separableContexts`, the "keep both" shape).
- **Intentional default layering is allowlisted** — `intentionalDefaultShadows`
  names the deliberate pairs of the shipped set (`shift+f6`: `lsp.rename` over
  `file.rename`; `f7`: `diff.nextChange` over `debug.stepInto`; the linux
  Cmd-fold landing tab cycling's `ctrl+cmd+arrow` under the editor's
  `ctrl+arrow` line start/end). The allowlist only applies when **both** sides
  are `LayerDefault`; a user or project binding shadowing anything is always
  reported. `TestDefaultShadowsAreIntentional` keeps the list exact on both
  platforms: every default shadow must be listed, every entry must still
  correspond to a real pair.
- **Surfacing** — `BuildTable` appends each shadow to the table's diagnostics
  (`Shadows()` exposes the structured list). The root model toasts every
  binding-table diagnostic as a warning notification (`notifyKeymapDiags`, at
  startup and on every config reload, deduped per session); the diagnostic
  names which command wins where and the qualified key to unbind. The settings
  keymap page marks both halves with a `⊘` gutter marker at the chord, and its
  detail column spells out the direction (`⊘ hides … while editor is focused` /
  `⊘ hidden by …`) plus the resolution.


## Platform normalisation

`platform.go` folds the logical `Meta` modifier once at table-build time: on macOS
the terminal can forward Cmd as Meta, so `Meta` is kept; everywhere else `Cmd → Ctrl`.
Normalisation is idempotent, so the resolver only ever compares concrete keys.

## Table & conflicts

`BuildTable(defaults, overrides, goos)` (`table.go`) starts from the normalised
default set and overlays the merged `[keymap.bindings]` map (binding key →
command id; `""` unbinds). Overrides arrive already merged by config precedence
(04), so each non-empty entry replaces matching default chords (a brand-new chord
becomes a user binding). `conflict.go` then detects same chord+context
clashes: it keeps the highest-`Layer` binding and surfaces the rest as non-fatal
diagnostics — never a silent shadow. Unparseable override keys are skipped as
diagnostics too.

### Binding keys: bare, context-qualified, language-qualified (0460, #1312, #1876)

`override.go` parses the map keys. A key is either

- a **bare chord** — `"ctrl+s"` — which applies **wherever the chord is bound**:
  a rebind replaces the command in every context, an unbind drops them all; or
- a **context-qualified chord** — `"editor.ctrl+s"` — which touches that one
  context only. The qualifier is any context spelling from `ContextNames()`:
  `global`, `editor`, `explorer`, `palette`, `diff`, and since #1794 the full
  pane set — `archive`, `breakpoints`, `data`, `debug`, `http`, `preview`,
  `problems`, `structure`, `terminal`, `usages`, `vcs` (`ParseContextName`);
  anything else is not a qualifier and is parsed as part of the chord, so
  dotted chords (`cmd+.`) keep working. `global.<chord>` is therefore narrower
  than the bare form: it binds in the `Global` context and leaves pane-scoped
  bindings of the same chord alone. Old override files predate the wider set
  unchanged — a bare chord still applies wherever the chord is bound, and the
  original five qualifiers kept their meaning; or
- a **language-qualified chord** — `"editor[http].cmd+e"` (#1876) — the editor
  qualifier narrowed to one language id. The bracket form is accepted on
  `editor` only; the id must have the registered-language-id shape
  (`ValidLangQualifier`: lowercase letters, digits, `-_+#` — no dots, since
  the grammar splits at the first dot). A malformed qualifier is not a
  context, so the key falls back to chord parsing and surfaces as an ignored
  key diagnostic; the language itself is deliberately **not** checked against
  the runtime registry at parse time (plugins register lazily). Bracket
  chords (`cmd+[`) are unaffected — `editor[http` without the closing bracket
  is not a qualifier.

Qualified keys are applied **after** bare ones (both in sorted order, which
also puts `editor.<chord>` before `editor[<lang>].<chord>`), so the narrower
statement wins regardless of Go's map iteration order. Because conflict
detection groups by chord *and* context — and `editor[http]` is its own
context slot — qualified bindings in different scopes are not a conflict at
all: the resolver simply picks the most specific one matching the focus. That
is the config spelling behind the keymap page's "keep both, resolve by
context" and "keep both, limit to file type".

TOML's own grammar invites writing a dotted map key as a sub-table, so both
spellings are accepted and mean the same entry (the config package flattens
nested slot-map tables per layer before merging, `config/load.go`):

```toml
[keymap.bindings.editor]
"ctrl+g" = "editor.nextOccurrence"

[keymap.bindings]
"editor.ctrl+g" = "editor.nextOccurrence"
```

`BindingConfigKey(ctx, chord, qualified)` renders the dotted config key the
settings page writes back through.

## Resolver

`Resolver` (`resolver.go`) feeds keys against the table, tracking partial
multi-step state. Each `Feed(key, context)` returns:

- **Pending** — the sequence is a prefix of a longer chord; the caller arms a
  `TimeoutDuration` (600ms) timer. A prefix wins over an equal-length exact match
  (so `cmd+k down` stays reachable); an exact binding on the bare prefix is
  recovered on `Timeout`. A bare prefix without an exact binding **survives**
  the timeout (#1482): `Timeout` keeps the pending chord and reports Pending,
  so the which-key popup stays open until a continuation key, a non-matching
  key (Feed restarts from it), an `esc` (#1909) or an explicit `Reset` (mouse
  click) ends the sequence. `Continues(key, ctx)` answers, without touching
  the held state, whether a key would carry the sequence forward — the
  which-key layer uses it to tell a cancelling `esc` from one a binding
  claims.
- **Resolved** — a binding matched; `Command` carries the id.
- **NoMatch** — nothing; the caller lets the key fall through. An aborted prefix
  restarts the sequence from the new key rather than stranding it.

The root model's `resolveKeymap` also feeds the local usage log (#2235): a
Resolved chord records a `key` event (chord, context, command), a blocked
default records `blocked`, and a NoMatch on a command-modified chord or
function key records `unbound` — the expected-but-missing-keybind signal.
Plain and shift-only presses are typing and are never recorded (see
[usage telemetry](./usage-telemetry.md)).

With an **editor focused the `unbound` verdict waits for the pane** (#2303).
The keymap layer sees a chord before the editor does, and the editor owns
editing chords the table never lists — `alt+delete`, `alt+backspace`,
`ctrl+u`, the word motions. Those used to be logged as missing keybinds
(`alt+delete` was the single most frequent one) and buried the real signal.
`resolveKeymap` therefore parks the event in `pendUnbound` instead of writing
it; `routeKey` writes it after dispatch only when `editor.HandledLastKey()`
reports the editor ignored the key too. A chord consumed app-side before
reaching a pane never reaches `routeKey` and is dropped on the next press —
it was bound after all. Panes other than the editor are unchanged: their
chords are recorded immediately.

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
  swallows the key, schedules a `keymapTimeoutMsg` and — unless the popup is
  already up or the delay is 0 — a `whichKeyDelayMsg` that surfaces the
  which-key popup (bordered, bottom-centered above the status line) once the
  configured delay has passed (#1909). The delay message carries the
  pending-sequence generation (`whichKeyGen`, bumped by every pend and every
  clear), so a timer whose sequence has since resolved or been cancelled is
  dropped instead of flashing a stale popup. On timeout the held chord
  resolves as an exact binding, or — as a bare prefix — stays pending with
  the popup open (#1482). A mouse click while a chord is pending resets the
  resolver and closes the popup; the click then acts normally.

## Terminal-context bindings (#1794)

A focused live terminal forwards keys raw to the PTY, so terminal-scoped
bindings resolve **before** the forwarding, in the root model's terminal
branch (`terminalContextChord` in `internal/app`), after the hardcoded
reserved set (`ctrl+tab`, `alt+f12`, `cmd+t/d/w/f`, `cmd+shift+l` for link
hint mode #2254), the spatial focus moves
and the `terminalGlobalCommands` allowlist. Guard rails keep the shell
usable:

- only bindings **explicitly scoped** to the `terminal` context are eligible;
  a `Global` chord never intercepts unless allowlisted,
- unmodified keys are never intercepted (they are typing),
- `terminalShellEssential` (`ctrl+c`, `ctrl+d`, `ctrl+z` — interrupt, EOF,
  suspend) and the `terminalShellChords` readline arrows always forward, even
  when a terminal-scoped binding names them.

The design trade, documented here deliberately: **`ctrl+t` no longer reaches
the shell** — readline's rarely-used transpose-chars loses the position to
the new-terminal-tab chord (iTerm and JetBrains both spend this key on
new-tab). `keymap.bindings."terminal.ctrl+t" = ""` restores the forwarding.
Inside the popup terminal the same binding acts on the popup's tab strip
(`popupReservedKey` maps `terminal.newTab` onto a sibling popup tab).

## Terminal limits & fallback

Many modifier combos (`Cmd+T`, `Ctrl+Tab`, `Cmd+1`) are intercepted by the
terminal/OS and never reach the program; such bindings carry a `Fragile` flag.
Every bound action stays reachable from the command palette (07), the
universal escape hatch.

Since #720 the palette/cheatsheet no longer appends "⚠ terminal-dependent" to
fragile-only bindings — post-#711 every default is a Cmd/Alt chord, so the
suffix marked the whole table and carried no signal. Instead the app probes
the environment at startup (`internal/app/termcheck.go`): it collects
bubbletea's `KeyboardEnhancementsMsg` (Kitty keyboard protocol handshake —
silence within a 3s grace tick means "unsupported") and `ColorProfileMsg`,
and detects tmux/screen from `$TMUX`/`$TERM`. Detected deficiencies open one
centered floating report at startup (headline + fix per issue — missing Kitty
protocol, tmux without extended-keys, sub-true-color profile), dismissed with
esc; it waits for the tour/recovery/onboarding modals to clear first. The per-chord `Fragile` classes stay
visible in the settings keymap page (per-row ⚠) and the reachability matrix.

## Default set

The JetBrains-flavoured defaults live in `defaults.go` as data (chord, command id,
title, context, owner, fragile). `help.go` groups the effective bindings by
context (Global first) for the `palette.keymapHelp` cheatsheet, and the
settings keymap page tags every pane-scoped row with its context
(`[editor]`, `[terminal]`, …).

The #1794 context audit walked every `Global` row and re-scoped the ones that
only make sense in one pane: `lsp.documentSymbols` (`cmd+f12`),
`run.testAtCursor` (`ctrl+shift+f10`) and the split-view pair
(`editor.splitViewRight`/`Down`) are `editor`-scoped now, and the new
per-context `ctrl+t` pair ships `terminal`/`editor`-scoped. Deliberately
**kept Global**: the real app commands (palette, project, pane/window
management, run/debug session control, settings, menu), the `editor.tab.*`
family — a tab host whose active tab is a terminal or content viewer
advertises that tab's context, so tab switching scoped to `editor` would die
exactly where it is needed (#997) — and the deliberate cross-context chords
(`explorer.newFile` with an editor focused, #374; `file.rename` under
`shift+f6`'s editor shadow).

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

`pane.resizeMode` (`ctrl+alt+r`, #2150) is the one **mode-entering** default:
it arms the sticky keyboard pane-resize mode instead of running a one-shot
action. While the mode is armed the root model consumes every key press before
this layer sees it — `h`/`j`/`k`/`l` and the arrows step the focused pane's
edge, `esc`/`enter`/`q` leave, everything else is inert — so the mode's own
keys are deliberately *not* rows in this table (they cannot be rebound; the
entry chord can). The chord joins the `ctrl+alt` family and sits on the
terminal global-command allowlist, so it also arms from a focused terminal or
tool pane. See [Pane Layout & Drag](/architecture/pane-layout.md).

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

The unbound-command audit (#1378) gave five more palette-only commands their
JetBrains chords: `lsp.documentSymbols` (`cmd+f12`, the File Structure popup —
the `cmd+3` Structure tool window stays the persistent counterpart),
`lsp.peekDefinition` (`cmd+y`, Quick Definition), `lsp.referencesPanel`
(`cmd+alt+f7`, Show Usages — the persistent panel variant of the `alt+f7`
popup), `run.testAtCursor` (`ctrl+shift+f10`, the Windows-scheme
run-context-configuration chord; the macOS `ctrl+shift+r` would collide with
`project.replaceInPath` once Cmd folds onto Ctrl off macOS) and
`nav.bookmarks` (`cmd+f3`, Show Bookmarks). The project bookmarks (#55) took
the JetBrains *Windows* chords, since the macOS Toggle Bookmark chord (`f3`)
is `search.nextMatch` here: `bookmark.toggle` (`f11`), `bookmark.next`
(`shift+f11`), `bookmark.previous` (`ctrl+shift+f11`), plus the macOS
mnemonic chord `alt+f3` for `bookmark.toggleMnemonic`; all editor-scoped,
since they act on the caret's line. Everything else stays palette-only
deliberately: enumerated variants (`scratch.new.*`, `themes.select.*`,
`file.setEncoding.*`/`file.setLineEndings.*`), pane-local commands the pane
already keys (`explorer.*` speed keys, terminal pass-through), commands with a
vim-native equivalent (`editor.fold.*` = `za`/`zc`/`zo`/`zM`/`zR`,
`vcs.nextChange`/`vcs.prevChange` = `]c`/`[c`, `merge.acceptOurs`/
`merge.acceptTheirs`/`merge.acceptBoth`/`merge.keepManual` =
`go`/`gt`/`gb`/`gm` and `merge.nextConflict`/`merge.prevConflict` =
`]n`/`[n` (#2258), `editor.quit`/
`editor.write_quit` = `:q`/`:wq`), and commands JetBrains itself ships without
a default (local history, pin/close-others tab actions, window layouts, HTTP
client utilities, maintenance commands).

Editor clipboard and line navigation are live default bindings: `cmd+c` /
`cmd+x` / `cmd+v` target the registered `editor.copy` / `editor.cut` /
`editor.paste` commands (visual selection or current line, through the system
clipboard via the `"+` register). Vim-native yanks reach the system clipboard
too when `editor.clipboard_sync` is on (the default, #1256), so `yy` needs no
chord at all. Note `cmd+c` may never arrive: Ghostty binds `super+c` to
`copy_to_clipboard:mixed` by default and a terminal-side selection wins —
`keybind = super+c=unbind` hands the chord back to IKE (#1255). `cmd+left` /
`cmd+right` target
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
  confirms, esc cancels. A press the chord format cannot carry — or a step that
  would not survive `String()` → `ParseChord` — is rejected with a visible
  warning instead of being dropped in silence (#1331).
- On confirm the capture runs conflict detection against the effective table;
  a collision names the other command **and its context** and waits for a
  decision (see "Conflict resolution" below). Capturing a cmd chord (or
  ctrl+tab/i/m) raises the 0081 honesty warning.
- A rebind writes `keymap.bindings.<new-chord> = <command>` and unbinds the
  old chord (`= ""`) in one write-back + reload; `u` unbinds, `r` resets to
  the preset (removes the override). The root model rebuilds its resolver on
  `ConfigReloadedMsg`, so edits re-resolve live.
- A preset default that is no longer effective — its chord unbound or rebound
  to another command — stays listed as an `(unbound)` row (#736): enter
  captures a fresh chord for the command, `r` removes exactly that chord's
  override (per-binding reset to the shipped default), `u` is a no-op.
- Every **registered command without any binding** — plugin/palette-only
  commands and configured custom tools (`tool.<name>`, #741) — is listed too,
  as a `(no binding)` row trailing the list (#771). The rows come from the
  registry (`Registry.Commands()`), match the `/` filter (also via the
  literal words "no binding"), and enter captures the command's first chord
  through the normal capture/conflict flow; `u`/`r` are no-ops.

### Conflict resolution (0460, #1298, #1312)

A captured chord that another command already carries is a decision, not a
yes/no. `conflictWith` reports the colliding binding — including one in a
context that never overlaps, because the plain rebind writes an *unqualified*
override and would take the chord in every context silently. The dialog offers:

| choice | key | what it writes |
|---|---|---|
| Replace & unbind other | `enter` | `keymap.bindings.<chord> = <command>` (the other binding loses the chord) |
| Keep both, resolve by context | `b` | `keymap.bindings.<context>.<chord> = <command>` — both bindings survive |
| Keep both, limit to file type | `l` | `keymap.bindings.editor[<lang>].<chord> = <command>` — the rebound command runs only in editors of that language (#1876) |
| Pick a different chord | `p` | nothing; capture restarts |
| Cancel | `esc` | nothing |

"Keep both, resolve by context" is offered only when `separableContexts`
holds: the two scopes never both match a focus — different panes, or (#1876)
two different language scopes on the editor. A `Global` binding matches in
every pane, so keeping both by context would just shadow one of them and the
option is hidden. "Keep both, limit to file type" is offered when the rebound
command can live under `editor[<lang>]` — its row is `Global` or
editor-scoped and not language-scoped already. `l` opens a language-id input;
the id is validated for shape (`ValidLangQualifier`) **and** against the
language registry (`lang.ByID`), rejecting bad input with a message. Unlike
the by-context option this one shadows deliberately — the colliding command
stays bound and merely loses the chord inside the chosen file type; the
shadow diagnostic names exactly that scope.

Two consequences for the other page keys:

- `r` (reset) removes the context-qualified override when one is set for
  exactly this chord+context, else the flat one (`overrideKeyFor`).
- `u` (unbind) writes the *qualified* `= ""` when the chord is also bound in
  another context (`unbindKeyFor`) — a flat unbind would drop the other half of
  a keep-both pair too.

The detail column lists both sides of a shared chord with their contexts, and
says `↔ … resolved by context` (with no replacement suggestions) instead of
warning when the contexts cannot overlap.

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
  That coverage contract is enforced by `TestActionMapCoversDefaults`
  (`coverage_test.go`): every default-set command must be an `actionMap`
  value or excused in its `noCounterpart` list — a new default command
  fails the test until the import knows about it (#1496).
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
owns only global / IDE-level shortcuts. The file-context VCS ids
(`vcs.revertFile`, `vcs.diff`, `vcs.panel`, …) went live with Epic 0320 — see
[VCS / Git Integration](/architecture/vcs.md); the workflow ids (`vcs.commit`
on `cmd+k`, `vcs.updateProject` on `cmd+t`, `vcs.branches`) were removed in
#750 in favour of custom tool panes (lazygit). Fragile Cmd primaries stay
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
| **fragile** | terminal/configuration/protocol dependent | `cmd+*` (Kitty protocol required; OS/terminal menus intercept several), `alt+*` (option-as-meta), `ctrl+shift+letter` (collapses without Kitty disambiguation), `ctrl+tab` (terminal-eaten), plain `ctrl+fN` **on darwin builds only** (macOS system shortcuts — ctrl+F2 "move focus to menu bar" etc. — swallow them before the terminal sees them, #1374; shifted variants like `ctrl+shift+f10` stay delivered) |

The ctrl+shift collapse only affects **character keys**: CSI-parameter-encoded
keys (arrows, home/end, pgup/pgdown, insert/delete, fN) carry their modifier
bitset in the legacy encoding (`CSI 5;6~` = ctrl+shift+pgup), so chords like
`ctrl+shift+pgup` are **delivered** (`csiParamEncoded` in `reachability.go`).
The C0-mapped keys (enter, tab, space, esc, backspace) are not exempt.

The `ctrl+fN` rule (#1374) makes `Classify` platform-aware for the first time
(it reads `keymap.GOOS`): Ghostty's Terminal Inspector confirmed plain
ctrl+F-keys never reach the terminal on macOS, while `opt+F[x]` and `cmd+F[x]`
are delivered. Consequently the run/debug F-key family ships **both** forms —
the JetBrains-macOS cmd chord as the darwin primary and the Windows-scheme
ctrl chord alongside (`cmd+f8`/`ctrl+f8` toggle breakpoint,
`cmd+f2`/`ctrl+f2` stop, `cmd+f5`/`ctrl+f5` rerun,
`cmd+f1`/`ctrl+f1` diagnostic under caret; JetBrains' macOS Rerun `cmd+r` is
taken by `editor.replace`, so rerun keeps the F5 position). Off macOS both
rows fold onto the ctrl chord. The status matrix below is generated on a
darwin build, so those rows honestly read fragile-with-palette-fallback; a
Linux build classifies the ctrl forms delivered.
| **undetectable** | invisible to key-press events | bare-modifier taps (`shift shift` — needs key-up reporting) |

Multi-step chords take the worst class of their steps.

**Probe** (`cmd/keyprobe`): run it in a target terminal, press the listed
chords, finish with `ctrl+d`; it prints one `PROBE\t<chord>\t<state>` line
per target (parsed by `keymap.ParseProbeReport`), recording collapse evidence
(`got=<key>`) when a shifted chord arrives as its unshifted twin. The probe
also enables mouse reporting (#816), so the `mouse-back` / `mouse-forward`
targets are answered by clicking those buttons — a terminal that does not
report SGR extended buttons 4/5 leaves them `missing`.

### In-app keymap doctor (#2080)

The probe also runs inside a live session: the `keymap.doctor` command
(palette, or `p` on the settings Keymap page) opens a full-screen overlay
(`internal/keydoctor`) over the same target set. The matching engine is
shared with the binary (`keymap.ProbeSession` in
`internal/keymap/probesession.go`): direct hits, the shifted-chord collapse
rule, mouse nav buttons via `FromMouseButton`. While the overlay is open the
root model hands it every raw `tea.KeyPressMsg` (and mouse press) **before**
toast dismissal, overlay paste and keymap resolution — first guard in the
key arm — so probing sees exactly what the terminal delivers. Unlike
`cmd/keyprobe`, the in-app run must not flip
`KeyboardEnhancements.ReportEventTypes` on the app view (#622,
`TestViewDoesNotRequestEventTypes`); it probes whatever the app's normal
keyboard mode receives, which is precisely the reality the bindings live in.

Controls: a computed free `ctrl+letter` (normally `ctrl+n` — picked to never
collide with a target) skips the highlighted pending chord (skipped chords
get **no verdict**), `ctrl+d`/`esc` end probing into a summary, where
`enter`/`y` saves, `r` saves and opens the dead-binding report (#2161),
`d`/`n` discards without touching the store, and `esc` resumes probing.

A saved run persists as a **per-terminal override set** in
`keyprobe.json` under the config dir (`$IKE_CONFIG_DIR`, else `~/.ike`) —
`keymap.ProbeStore` in `internal/keymap/probestore.go`, keyed by
`keymap.TerminalID`: `tmux` when `$TMUX` is set (tmux's chord rewriting is
its own reality, whatever emulator hosts it), else `$TERM_PROGRAM`, else
`$TERM`. On startup the running terminal's verdicts install via
`SetProbeVerdicts` before the first table build; `Classify`,
`classifyKey` and `ReachabilityNote` consult them **ahead of** the static
rules and the hand-pinned overrides — a chord that probed delivered
classifies delivered however the table fears, one that probed missing
classifies fragile with the note "probed missing in this terminal
(arrives as `<got>`)". Since `Defaults` derives every binding's `Fragile`
flag from `Classify`, the config reload a save triggers re-derives the
settings-list ⚠ marks and the help cheatsheet from probed truth. A default
whose chord probed missing is flagged `✗ probed missing` in the keymap
settings list. Stored runs are viewable and clearable in the Keymap page's
"Keymap Doctor" sub-panel; a `cmd/keyprobe` capture can feed the same store
through `ParseProbeReport`.

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

### Dead-binding report (#2161)

Probing answers "does this chord arrive?" one keypress at a time. The doctor's
second job answers it for the **whole active keymap** without pressing
anything: the `keymap.deadBindings` command (palette, or `d` in the Keymap
page's "Keymap Doctor" sub-panel) audits every effective binding and lists the
ones that cannot arrive here, with the reason and a rebind offer. The summary
of a probe run reaches it directly with `r` ("save + review dead bindings"),
so a fresh run flows into the repairs it justifies.

**Verdicts** (`TerminalEnv.Deliverability`, `internal/keymap/deadchords.go`)
sharpen the three reachability classes into what a user can act on. `Fragile`
lumps "needs a terminal setting" together with "certainly swallowed here", and
only the second is worth rebinding proactively:

| Verdict | Meaning |
|---|---|
| **live** | `Delivered`, or probed delivered in this terminal |
| **at risk** | `Fragile` with no known-dead pattern matching — it may work, and it may silently not (`cmd+*` without the Kitty protocol, `ctrl+shift+letter`, `alt+*` in an unidentified terminal) |
| **dead** | known not to arrive: probed missing (with the collapse evidence), plain `ctrl+fN` on darwin (#1374), `ctrl+tab` under tmux, `alt+<character key>` in a macOS terminal that composes the Option layer by default, and the `Undetectable` bare-modifier taps |

`TerminalEnv` is `{GOOS, Terminal}` — the terminal identity is
`keymap.TerminalID`, the same key the probe store uses, so a stored run and a
static verdict always talk about the same terminal. The
option-composition rule fires only for terminals whose *default* composes
(`optionComposingTerminals`: Terminal.app, iTerm2, Ghostty, kitty, WezTerm,
Alacritty, VS Code); an unidentified terminal — or tmux, whose outer emulator
decides — stays "at risk" rather than being condemned. It applies to character
keys only: `alt+f7` and `alt+left` carry their modifier in the CSI parameter
and arrive whatever Option does. **Probed truth wins in both directions**: a
chord probed delivered is never called dead however grim the rules look, and
one probed missing is dead however clean they look.

**Suggestions** (`internal/keydoctor/analysis.go`) are generated best-first —
the same base key under a delivered modifier (`ctrl+f8` → `shift+f8`), then
mnemonic `ctrl+<letter>` chords from the command id, then two-step sequences
under a `ctrl+k`/`ctrl+x`/`ctrl+w` prefix — and each candidate must be *live*
in this setup and **free across the whole keymap**: not bound, not the prefix
of a multi-step chord, not prefixed by one. The check is deliberately
context-blind because the write is an unqualified
`keymap.bindings.<chord>` override, which claims the chord everywhere. Within
one audit no two findings offer the same chord (dead bindings pick first), so
applying every offer stays conflict-free.

**Applying** emits `keydoctor.RebindMsg`, which the root model runs through
the ordinary customization path (`internal/app/deadbindings.go`): the new
chord binds the command at user scope, the dead chord unbinds, one config
reload rebuilds the table. The report stays open and re-audits on that reload,
so a repaired binding drops off the list and the remaining suggestions are
re-checked against the keymap as it now stands.

## macOS: the Option key as Alt (#2064)

`alt+*` is the largest fragile family in the default set (`alt+f7` find usages,
`alt+1…9` tab select, `alt+enter` intentions, `ctrl+alt+h`, `cmd+alt+b`, …), and
on a Mac the Option key is where it either works or silently does nothing. What
arrives depends entirely on how the terminal is configured to treat Option.

### What the wire carries

| Mode | Terminals | opt+b on the wire | What bubbletea reports |
|---|---|---|---|
| **ESC-prefix (meta)** | Terminal.app "Use Option as Meta Key", iTerm2 "Esc+", tmux forwarding | `ESC b` — the whole sequence is prefixed, so `opt+F7` is `ESC ESC [ 18 ~` | `alt+b` / `alt+f7`, `Key.Text` empty |
| **CSI-parameter (legacy)** | every mainstream terminal, for arrows / F-keys / nav keys | `CSI 18;3~` (bitset shift=1, alt=2, ctrl=4, meta=8, offset by 1) | `alt+f7` |
| **Kitty keyboard protocol** | kitty, Ghostty, WezTerm, tmux 3.4+ passthrough | `CSI 98;3u` | `alt+b`; Cmd rides along as the protocol's *super* bit (`CSI 98;11u` = `cmd+alt+b`) |
| **xterm modifyOtherKeys** | xterm and clones | `CSI 27;3;98~` | `alt+b` |
| **composed (macOS default)** | Terminal.app / iTerm2 "Normal" / kitty & Ghostty with option-as-alt off | `∫` (UTF-8, three bytes) | the rune `∫`, **`Mod` empty** |

bubbletea always requests basic key disambiguation (Kitty flag 1) — `View()`
deliberately leaves `ReportEventTypes` off (#622) — so on a Kitty-protocol
terminal the modifier set arrives explicitly and no configuration beyond
option-as-alt is needed.

### What IKE hardens

`FromKeyMsg` (`fromkeymsg.go`) reads bubbletea's string form, but **which**
string form matters. `Key.String()` returns `Key.Text` — the characters the
terminal produced — whenever it is non-empty, which is right for plain typing
(`?` rather than `shift+/`) and wrong for a chord: with the Kitty protocol's
report-associated-text flag, or on the Windows Console API, a modified key
carries both the modifiers *and* the text, and `String()` hands back a bare
`"b"` for `opt+b`. Since #2064 any press carrying a chord modifier
(ctrl/alt/meta/super/hyper) is read through `Keystroke()` instead, which always
spells the modifiers out; shift-only presses keep the textual form.

The Cmd-class modifiers all fold onto the single `ModMeta` bit
(`modAlias` in `parse.go`): the Kitty protocol can report `meta`, `super` *and*
`hyper` for what IKE calls Cmd, and an unrecognised token would make
`ParseKey` fail and the event be dropped outright rather than resolved. Lock
states (caps lock, num lock) never reach the chord — bubbletea's `Keystroke()`
omits them.

`internal/keymap/optkey_test.go` pins all of this from raw bytes: every
sequence in the table above is decoded with ultraviolet's real `EventDecoder`,
adapted through `FromKeyMsg`, and — for the default alt chords — looked up in
the darwin binding table, so "opt+F7 fires Find usages" is a test, not a claim.

### Troubleshooting: `alt+…` does nothing on my Mac

The composed row of the table is **not fixable in IKE**. macOS applies the
Option layer of the keyboard layout before any escape sequence exists, so
`opt+b` reaches the program as the literal rune `∫` with an empty modifier
set — indistinguishable from typing `∫`. IKE cannot recover the modifier, and
guessing it back from the glyph would break typing those characters. The fix is
one terminal setting:

| Terminal | Setting |
|---|---|
| **Terminal.app** | Settings → Profiles → *Keyboard* → **Use Option as Meta Key** |
| **iTerm2** | Settings → Profiles → *Keys* → Left/Right Option key → **Esc+** (*not* "Meta": that mode sets the 8th bit, which is ambiguous with UTF-8 and decodes as garbage) |
| **kitty** | `macos_option_as_alt yes` (or `left` / `right`) |
| **Ghostty** | `macos-option-as-alt = true` (or `left` / `right`) |
| **WezTerm** | `send_composed_key_when_left_alt_is_pressed = false` (and the `right` twin) |
| **Alacritty** | `[window] option_as_alt = "Both"` (or `OnlyLeft` / `OnlyRight`) |

The trade is real and worth stating: with option-as-alt on, the Option layer
stops producing `∫ ƒ å …`, so anyone who types those characters wants the
one-sided (`left`/`right`) variant. Everything bound to `alt+*` also stays
reachable from the command palette, the universal escape hatch — and
`cmd/keyprobe` answers "does this chord actually arrive?" for a given terminal
before anyone changes a binding. Anyone who would rather move the bindings than
the terminal setting can run the [dead-binding report](#dead-binding-report-2161)
(`keymap.deadBindings`): in a composing terminal it lists exactly the `alt+*`
chords on character keys and offers a deliverable chord for each.

Two further macOS notes:

- **`alt+f3`** (toggle bookmark with mnemonic) is encoded `CSI 1;3R`, which
  collides with the cursor-position report. ultraviolet resolves the ambiguity
  by emitting *both* events, so the chord still fires; the stray
  `CursorPositionMsg` is ignored.
- **Cmd chords** (`cmd+alt+b`, `cmd+alt+f7`, …) need a Kitty-protocol terminal
  on top of option-as-alt — Terminal.app and iTerm2 never forward Cmd. That is
  the `Fragile` class above, not an Option-key problem.

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
  Only bindings whose context matches the focused pane are listed, and each
  row carries the binding's cheatsheet title, so labels stay consistent with
  the help sheet.
  The panel opens after `keymap.which_key_delay_ms` (default 300ms, #1909)
  of pending time — a sequence typed at speed never flashes one — and
  `keymap.which_key = false` switches it off entirely; both are edited on the
  settings panel's **Keymap Hints** page. Once open it follows every further
  step immediately, narrowing to the deeper prefix's continuations without
  waiting again. It clears on resolve, on a non-matching key, on a mouse
  click, and on `esc` — which, while a sequence is pending, cancels the
  sequence and is **consumed** (`Resolver.Continues` first checks that no
  binding claims it), so abandoning a chord never doubles as an `esc` for the
  focused pane.
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

## Every command is bound or justified (#2305)

Usage is overwhelmingly keybind-driven, so a command that ships palette-only is
effectively invisible. The standing rule — written down in
[Change Workflow](/process/change-workflow.md) — is that a **new command ships with a
default keybind**, and staying keybind-less needs a **recorded reason**.

The guardrail is the audit ledger in `cmd/ike/keybind_audit_test.go`, run against the exact
plugin set the shipped binary compiles in. Every registered command must be bound in
`Defaults()` or carry a ledger entry with one of the audit's reasons — vim-native key,
pane-local single key, one entry of a picker, flavour of an already-bound command, an
`alt+enter` intention doorway, a menu home, or a genuine one-off. A new command fails the
build until someone decides; a ledger entry whose command disappeared fails too.

The #2305 pass turned these palette-only commands into defaults:

| command | chord | why it earned one |
|---|---|---|
| `file.copyPath` | `cmd+shift+c` | JetBrains' Copy Path verbatim; the everyday path action |
| `lsp.organizeImports` | `ctrl+alt+o` | JetBrains Optimize Imports, both keymaps |
| `json.jqPlayground` | `ctrl+alt+j` | the playground family next to its `ctrl+alt+e` query view |
| `yaml.yqPlayground` | `ctrl+alt+y` | the yq twin of the row above |
| `scratch.generate` | `cmd+alt+shift+n` | test-data dialog, one modifier past `cmd+shift+n` |
| `vcs.diff` | `cmd+alt+d` | JetBrains' `cmd+d` Show Diff is taken by duplicate-line |
| `tests.toggle` | `cmd+4` | JetBrains' Run tool window number |
| `debug.console` | `cmd+5` | JetBrains' Debug tool window number |
| `run.select` | `alt+shift+f10` | JetBrains' Run… popup verbatim |
| `debug.testAtCursor` | `alt+shift+f9` | JetBrains' Debug… position; the debug twin of `ctrl+shift+f10` |
| `pane.close` | `ctrl+alt+w` | closes the pane whole, next to `cmd+w`'s close-tab |
| `view.toggleWrap` | `alt+shift+w` | the one view toggle flipped by the hour |
| `window.layouts` | `alt+shift+f12` | the layout family's third F12 chord |

All thirteen are Cmd/Alt-modified and therefore fragile; each records its palette (or menu)
escape route in `reachableAlternatives`, and each shows up in the cheatsheet and the
palette's shortcut column automatically.

## Open in Browser earns its chord (#2365)

`file.openInBrowser` (#1429, #2298) shipped deliberately menu-only: the #2305 pass left it in
the ledger with the *menu home* reason. Telemetry then recorded it several times in a single
day, **every** invocation through the File or context menu — the detour costs real time in
HTML-preview work, which is exactly the signal the ledger reason was waiting on.

| command | chord | why it earned one |
|---|---|---|
| `file.openInBrowser` | `alt+f2` | JetBrains' `WebOpenInAction` verbatim, free in both keymaps |

`alt+f2` is free next to the F2 family already in use (`f2`/`shift+f2` step through
diagnostics, `cmd`/`ctrl+f2` stops the debugger) and needs no bracket or slash key, so QWERTZ
types it unchanged. The row is **Global**, not one row per context: the command resolves its
subject through `refactorTarget` exactly like `file.copyPath`, so a single binding covers the
editor and the explorer — the two places the menu offers the action. Being Alt-modified it is
fragile, so it records its palette/context-menu escape route in `reachableAlternatives` like
every chord in the table above, and its ledger entry is gone.

## The copy chord outside the editor (#2315)

Unbound-chord telemetry recorded `cmd+c` in the `http` and `explorer` contexts:
the editor has bound it since 06, and users expect the same key to mean "copy"
wherever they are. The response pane in fact *handled* it already (its own
`copyChord`, #2051/#2062), but the keymap layer had never heard of it — so it
appeared in no listing, could not be rebound, and kept reporting as unbound.
The explorer did nothing at all.

| context | chord | command | what it copies |
|---|---|---|---|
| `http` | `cmd+c` | `http.copyResponse` | the live selection, else the whole response body |
| `explorer` | `cmd+c` | `file.copyPath` | the selected entry's absolute path |

Two things this pass deliberately did *not* do:

- **No `ctrl+c` secondary rows.** On macOS `ctrl+c` is the global quit chord,
  and #2062 already settled the compromise: it copies over a live selection and
  quits otherwise. A row would take the key outright. Off macOS the point is
  moot — `NormalizeChord` folds `cmd+c` onto `ctrl+c` anyway, exactly like the
  editor's copy row, so those panes lose quit-by-`ctrl+c` there the way editors
  already had.
- **No file copy/paste pair in the explorer.** There is none to bind to; the
  path is the copy the tree can actually make today. If a real file
  copy/paste lands, this chord is where it belongs and `file.copyPath` moves
  back to its `cmd+shift+c` home alone.

`http.copyResponse` exists because binding `http.copyBody` would have been a
*different* key: the pane's copy key prefers the selection. The command
forwards to `httppane.Model.CopyKeyCmd`, the exported form of the pane-local
`copyKeyCmd`, so the chord and the pane key cannot drift apart.

## The find chord outside the editor (#2409)

`cmd+f` is the same story as the copy chord one section up. `/` starts a search
or a filter in a dozen panes — the explorer speed search, the Problems / Usages
/ TODO filter rows, the response viewer's prompt, the DOM selector, the data
grid's filter, the Issues filter overlay, terminal copy mode, the settings
pages — but only the response pane had ever heard of `cmd+f`, and telemetry
recorded the chord pressed unbound in the `http` context.

One Global command covers all of them:

| command | chord | context | what it does |
|---|---|---|---|
| `search.open` | `cmd+f` | Global | asks the focused pane to open its own search |
| `editor.find` | `cmd+f` | `editor` | the editor's own find — shadows the Global row while an editor has focus |

`search.open` dispatches `OpenSearchMsg`; the root model asks the focused
`pane.Instance` for the **`pane.Searchable`** capability
(`internal/pane/searchable.go`) and calls `OpenSearch()`. A pane kind with no
search of its own is not `Searchable`, and the root model notifies
*"No search in this pane"* rather than swallowing the key — a silent no-op is
indistinguishable from a broken binding (#267). An editor pane hosting a
content tab (#1778) or a terminal (#573) delegates to what the tab holds, so
the chord acts on what the pane actually shows.

Two panes had no search at all and gained one in the same change:

- **Archive viewer** — the shared filter row (#2156) over `name:` / `type:`
  plus free match text, gating the *tree* so the directories a tar never named
  filter like the ones it did (`internal/archview/filter.go`).
- **Diff viewer** — a prompt on the pane's last row searching the diff *rows*
  (raw left/right text, so the layout and horizontal scroll are irrelevant);
  `n`/`N` walk the matches while a search is open and keep stepping hunks
  while none is (`internal/diff/search.go`).

The markdown preview gained the same prompt over its rendered lines
(`internal/preview/search.go`), so every pane the issue listed answers the
chord.

### Why `ctrl+f` is not in the table

The issue asked for `ctrl+f` in the Global context too. It is deliberately
**not** bound: `ctrl+f` is vim's page-forward motion in the editor, including
operator-pending (`d ctrl+f`), and a Global binding resolves *ahead* of the
pane — modified chords are eligible even while an editor captures text — so it
would silently take the motion away. Instead each searchable pane answers the
chord itself (`ui.FindChord`, `internal/ui/findkey.go`), which works precisely
because the keymap table leaves the chord unbound and it therefore reaches the
pane intact. Off macOS the `Cmd`→`Ctrl` fold gives `ctrl+f` the `search.open`
binding anyway, with the editor's own `editor.find` row shadowing it there — an
allowlisted intentional shadow (`intentionalDefaultShadows`).

The one casualty is the data grid, where `ctrl+f` used to page forward; `n` and
`pgdown` still do, and the chord now filters like everywhere else.

The `http` context keeps its own `http.search` row from #2400 — it names the
same gesture (`httppane.Model.BeginSearch`) the Global command reaches through
the capability, so which of the two wins there is immaterial; the pane row is
the more specific one and is allowlisted as an intentional shadow.

## Stepping the matches outside the editor (#2410)

`cmd+f` opening a pane's search left the second half of the gesture behind.
Inside the editor `cmd+g` / `cmd+shift+g` (`search.nextMatch` /
`search.prevMatch`) walk the matches with the find widget still up; in every
other search field the query had to be applied and the input blurred with
`enter` first — the filter rows blur on `enter`, the response viewer's `n`/`N`
are query text until then. Telemetry (2026-09-01/02) recorded 19
`search.nextMatch` presses in streaks of up to 7, all in the editor: the other
panes could not be measured because the chord never resolved there.

The capability from #2409 carries the second half too:

```go
type Searchable interface {
    OpenSearch() bool          // #2409
    NextMatch() ui.MatchStep   // #2410
    PrevMatch() ui.MatchStep
}
```

`Instance.StepMatch(delta)` picks the direction; the root model's
`MatchStepMsg` handler asks the focused pane **first** and only falls back to
the older readings — repeat the editor's in-file search (#376), walk the
retained find-in-path results — when the pane reports `ui.NoStep`. That is the
whole gate: a pane whose search is closed is not claiming the chord, so the
editor path is unchanged.

`ui.MatchStep` (`internal/ui/matchstep.go`) is the shared outcome:

| field | meaning |
|---|---|
| `Handled` | the pane's search was open and took the chord |
| `Total` | matches the current query has |
| `Index` | 1-based position within them |
| `Wrapped` | the step ran off an end and came back around |
| `Cmd` | anything the step has to emit (the DOM inspector jumps the editor) |

`Handled` with `Total == 0` is the deliberate no-op the issue asks for: the
pane owns the chord, there is nothing to step to, and the root model notifies
*"No matches"* rather than moving something unrelated — the #267 rule again.

Every pane keeps its own stepping code, because "one step" means a scroll in
one pane and a tree cursor in the next. What they share are the primitives
(`ui.StepWrap`, `ui.StepSorted`, `ui.StepOver`) and the counter
(`ui.MatchCounter`), which is where `1/12 (wrapped)` comes from: the marker
has to read identically everywhere or it reads as a bug in the pane that
differs. It describes one step, so every query edit drops it.

What counts as a match follows the pane's own search:

| pane | matches |
|---|---|
| Explorer, terminal scrollback, terminal copy mode | the query's hits, stepped by row / virtual line |
| Diff, markdown preview, HTTP response, DOM inspector | the query's hits, stepped by match index |
| Problems, Usages, TODO index, archive viewer, Issues | the rows the filter left standing, group headers skipped (`ui.StepOver`) |
| Data viewer | the rows of the loaded page (paging silently would step over rows nobody can see) |
| Settings | the search result rows across all pages |

The filter-row panes render the counter through `filterbar.Model.ShowStep`, so
all four wear it identically.

### Why `ctrl+g` is not in the table

The same argument as `ctrl+f` one section up: `ctrl+g` / `ctrl+shift+g` are
the editor's `editor.caret.addNext` / `editor.caret.addAll` (#145), and a
Global binding would resolve ahead of the editor. The chords are answered
outside the keymap table instead — by the root model for registry panes (only
while the focused pane's search is open, so an editor-less context never loses
anything) and by `ui.MatchStepChord` in the surfaces that own the keyboard
first: the settings panel, the TODO index overlay, the explorer's speed
search, the terminal. Off macOS the `Cmd`→`Ctrl` fold makes `ctrl+g` the
`search.nextMatch` binding anyway, with the multi-caret rows shadowing it in
the editor — already allowlisted in `intentionalDefaultShadows`.

`ui.MatchStepChord` matches by *parts* rather than by spelling: bubbletea
reports the Command key as `meta+` or `super+` depending on the terminal
protocol, the keymap layer's canonical form is `cmd+`, and the modifier order
differs between the two (`shift+meta+g` vs `cmd+shift+g`). Enumerating that
product is how a chord ends up working in one terminal and not the next.

The terminal reserves `cmd+g` / `cmd+shift+g` in `terminalReservedKey`
alongside `cmd+f`, and only claims them when a search is actually open — with
none open the chord stays with the child, which may own its own find. `ctrl+g`
is deliberately *not* reserved there: it is a real control character the shell
expects.

## The digit chord families (#2489)

Four families now share the number row, one modifier each, and the split is
what makes them all fit:

| chords | family | notes |
|---|---|---|
| `cmd+0` … `cmd+9` | tool windows | explorer, pins, structure, run, debug, TODO, problems, VCS, dependencies |
| `alt+1` … `alt+9` | editor tabs (`editor.tab.selectN`) | |
| `ctrl+1` … `ctrl+9` | pane numbers (`pane.focusN`, #2407) | **macOS only** — off macOS the Cmd→Ctrl fold puts the tool windows here, so `pane.focusByIndex` is the doorway there |
| `ctrl+alt+1` … `ctrl+alt+9` | recent projects (`project.switchMRUN`, #2489) | the only digit family free on *both* platforms |

`project.switchMRU1…9` therefore ships one table (`jetbrainsRows`), not a
platform pair: the chords are spelled with a literal Ctrl, so no fold moves
them, and the only `cmd+alt+digit` default — `cmd+alt+0`, the time report
(#2426) — folds onto `ctrl+alt+0`, which is not one of the nine. Like the
other project entry points they sit on the terminal global-command allowlist
(`terminalGlobalCommands`), so the hop also works from a focused shell; the
digits themselves are learned from the picker and the Recent Projects column,
which render each project's number in front of its name. See
[Project Switching](/architecture/project-switching.md).

## Go to Line (#2486)

Telemetry recorded `cmd+l` pressed in an editor with status `unbound`: it is
JetBrains' *Go to Line:Column* on macOS and IKE had no such command. It is now
`editor.goToLine`, bound in the `Editor` context:

| chord | context | command |
|---|---|---|
| `cmd+l` (`ctrl+l` off macOS, via the `Cmd`→`Ctrl` fold) | Editor | `editor.goToLine` |

JetBrains' Linux chord for the action is `ctrl+g`, which is deliberately *not*
bound here: `editor.caret.addNext` already owns it in the `Editor` context
(#145, and the section above), and one everyday command per chord is the rule
for the default table. Off macOS the fold puts Go to Line on `ctrl+l`, which no
other default claims. The chord is fragile like every `Cmd`-modified JetBrains
binding, so the palette and the **Navigate** menu are the recorded fallback.

See [editor.md](editor.md#go-to-line-2486) for the prompt and its target
grammar.

## The line-editing family and the pane chords (#2400)

A second telemetry export (two sessions, ~9,900 events) left 37 presses on
`unbound`. They split into two groups, and the group decides the fix.

**Commands that did not exist.** The JetBrains line-editing family had only
vim gestures behind it, so the chord resolved to nothing at all. Each is now a
registry command in `internal/editor` (`lineops.go`), selection-aware wherever
JetBrains is:
| command | primary | reachability | fallback | status |
|---|---|---|---|---|
| `archive.reload` | `ctrl+r` | delivered | `—` | live |
| `bookmark.next` | `shift+f11` | delivered | `—` | live |
| `bookmark.previous` | `ctrl+shift+f11` | delivered | `—` | live |
| `bookmark.toggle` | `f11` | delivered | `—` | live |
| `bookmark.toggleMnemonic` | `alt+f3` | fragile | `palette / Navigate menu` | live via palette / Navigate menu |
| `debug.breakpointProperties` | `cmd+alt+f8` | fragile | `palette / Run menu` | live via palette / Run menu |
| `debug.breakpoints` | `cmd+shift+f8` | fragile | `palette / Run menu` | live via palette / Run menu |
| `debug.console` | `cmd+5` | fragile | `palette` | live via palette |
| `debug.continue` | `f9` | delivered | `—` | live |
| `debug.copy` | `cmd+c` | fragile | `palette` | live via palette |
| `debug.evaluate` | `alt+f8` | fragile | `palette / Run menu` | live via palette / Run menu |
| `debug.runToCursor` | `alt+f9` | fragile | `palette / Run menu` | live via palette / Run menu |
| `debug.start` | `shift+f9` | delivered | `—` | live |
| `debug.stepInto` | `f7` | delivered | `—` | live |
| `debug.stepOut` | `shift+f8` | delivered | `—` | live |
| `debug.stepOver` | `f8` | delivered | `—` | live |
| `debug.stop` | `cmd+f2` | fragile | `palette / Run menu` | live via palette / Run menu |
| `debug.testAtCursor` | `alt+shift+f9` | fragile | `palette / Run menu` | live via palette / Run menu |
| `debug.toggleBreakpoint` | `cmd+f8` | fragile | `palette / Run menu` | live via palette / Run menu |
| `deps.toggle` | `cmd+0` | fragile | `palette` | live via palette |
| `diff.nextChange` | `f7` | delivered | `—` | live |
| `diff.prevChange` | `shift+f7` | delivered | `—` | live |
| `editor.caret.addAbove` | `alt+shift+up` | fragile | `palette` | live via palette |
| `editor.caret.addAll` | `ctrl+shift+g` | fragile | `palette` | live via palette |
| `editor.caret.addBelow` | `alt+shift+down` | fragile | `palette` | live via palette |
| `editor.caret.addNext` | `ctrl+g` | delivered | `—` | live |
| `editor.case.cycle` | `alt+shift+u` | fragile | `palette` | live via palette |
| `editor.case.toggle` | `cmd+shift+u` | fragile | `vim g~ (g~~ linewise, ~ on a selection)` | live via vim g~ (g~~ linewise, ~ on a selection) |
| `editor.closeTab` | `cmd+w` | fragile | `palette` | live via palette |
| `editor.commentBlock` | `cmd+shift+7` | fragile | `palette` | live via palette |
| `editor.commentLine` | `cmd+7` | fragile | `palette` | live via palette |
| `editor.copy` | `cmd+c` | fragile | `vim y` | live via vim y |
| `editor.copyDocPath` | `cmd+alt+shift+c` | fragile | `palette` | live via palette |
| `editor.cut` | `cmd+x` | fragile | `vim d` | live via vim d |
| `editor.deleteLine` | `cmd+backspace` | fragile | `vim dd` | live via vim dd |
| `editor.deleteWordBackward` | `alt+backspace` | fragile | `vim db` | live via vim db |
| `editor.docEnd` | `cmd+end` | fragile | `ctrl+end` | live via ctrl+end |
| `editor.docStart` | `cmd+home` | fragile | `ctrl+home` | live via ctrl+home |
| `editor.duplicateLine` | `cmd+d` | fragile | `vim yyp` | live via vim yyp |
| `editor.escapeSelection` | `cmd+alt+shift+e` | fragile | `palette` | live via palette |
| `editor.find` | `cmd+f` | fragile | `vim /` | live via vim / |
| `editor.goToLine` | `cmd+l` | fragile | `palette / Navigate menu` | live via palette / Navigate menu |
| `editor.lineEnd` | `cmd+right` | fragile | `vim $` | live via vim $ |
| `editor.lineStart` | `cmd+left` | fragile | `home` | live via home |
| `editor.moveLineDown` | `cmd+shift+down` | fragile | `ctrl+shift+down` | live via ctrl+shift+down |
| `editor.moveLineUp` | `cmd+shift+up` | fragile | `ctrl+shift+up` | live via ctrl+shift+up |
| `editor.paste` | `cmd+v` | fragile | `vim p` | live via vim p |
| `editor.pasteFromHistory` | `cmd+shift+v` | fragile | `palette` | live via palette |
| `editor.redo` | `cmd+shift+z` | fragile | `vim ctrl+r` | live via vim ctrl+r |
| `editor.replace` | `cmd+r` | fragile | `palette` | live via palette |
| `editor.saveAll` | `cmd+shift+s` | fragile | `palette` | live via palette |
| `editor.selectAll` | `cmd+a` | fragile | `vim ggVG` | live via vim ggVG |
| `editor.selectLineEnd` | `shift+end` | delivered | `—` | live |
| `editor.selectLineStart` | `shift+home` | delivered | `—` | live |
| `editor.selection.extend` | `alt+up` | fragile | `palette` | live via palette |
| `editor.selection.shrink` | `alt+down` | fragile | `palette` | live via palette |
| `editor.sortLines` | `alt+shift+s` | fragile | `vim :sort / Edit menu` | live via vim :sort / Edit menu |
| `editor.splitViewDown` | `cmd+alt+shift+down` | fragile | `palette` | live via palette |
| `editor.splitViewRight` | `cmd+alt+shift+right` | fragile | `palette` | live via palette |
| `editor.tab.moveLeft` | `ctrl+shift+pgup` | delivered | `—` | live |
| `editor.tab.moveRight` | `ctrl+shift+pgdown` | delivered | `—` | live |
| `editor.tab.new` | `ctrl+t` | delivered | `—` | live |
| `editor.tab.next` | `cmd+ctrl+right` | fragile | `palette` | live via palette |
| `editor.tab.picker` | `alt+e` | fragile | `palette` | live via palette |
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
| `editor.undo` | `cmd+z` | fragile | `ctrl+z` | live via ctrl+z |
| `editor.unescapeSelection` | `cmd+alt+shift+u` | fragile | `palette` | live via palette |
| `editor.write` | `cmd+s` | fragile | `ctrl+s` | live via ctrl+s |
| `explorer.newFile` | `cmd+n` | fragile | `palette (or a in the explorer)` | live via palette (or a in the explorer) |
| `explorer.redo` | `cmd+shift+z` | fragile | `palette` | live via palette |
| `explorer.reveal` | `alt+f1` | fragile | `palette` | live via palette |
| `explorer.toggle` | `cmd+1` | fragile | `palette` | live via palette |
| `explorer.undo` | `cmd+z` | fragile | `ctrl+z` | live via ctrl+z |
| `file.copyPath` | `cmd+c` | fragile | `palette / context menu` | live via palette / context menu |
| `file.move` | `f6` | delivered | `—` | live |
| `file.openAs` | `cmd+alt+shift+o` | fragile | `palette / context menu` | live via palette / context menu |
| `file.openInBrowser` | `alt+f2` | fragile | `palette / context menu` | live via palette / context menu |
| `file.rename` | `shift+f6` | delivered | `—` | live |
| `find.openInPanel` | `cmd+enter` | fragile | `ctrl+enter` | live via ctrl+enter |
| `http.cancel` | `cmd+.` | fragile | `ctrl+.` | live via ctrl+. |
| `http.copyResponse` | `cmd+c` | fragile | `response pane "y" / palette` | live via response pane "y" / palette |
| `http.diffPreviousRun` | `cmd+shift+d` | fragile | `palette` | live via palette |
| `http.resend` | `ctrl+r` | delivered | `—` | live |
| `http.run` | `ctrl+f9` | fragile | `palette` | live via palette |
| `http.search` | `cmd+f` | fragile | `ctrl+f` | live via ctrl+f |
| `http.showResponse` | `cmd+shift+enter` | fragile | `ctrl+shift+f9` | live via ctrl+shift+f9 |
| `issues.copy` | `cmd+c` | fragile | `issues pane "y" / palette` | live via issues pane "y" / palette |
| `issues.selectNext` | `ctrl+down` | delivered | `—` | live |
| `issues.selectPrev` | `ctrl+up` | delivered | `—` | live |
| `json.jqPlayground` | `ctrl+alt+j` | fragile | `palette / Tools menu` | live via palette / Tools menu |
| `json.jqQueryView` | `ctrl+alt+e` | fragile | `palette / Tools menu` | live via palette / Tools menu |
| `lsp.callHierarchy` | `ctrl+alt+h` | fragile | `palette` | live via palette |
| `lsp.codeAction` | `alt+enter` | fragile | `palette` | live via palette |
| `lsp.definition` | `f4` | delivered | `—` | live |
| `lsp.diagnosticInfo` | `cmd+f1` | fragile | `palette` | live via palette |
| `lsp.documentSymbols` | `cmd+f12` | fragile | `palette (or the cmd+3 Structure panel)` | live via palette (or the cmd+3 Structure panel) |
| `lsp.format` | `cmd+alt+l` | fragile | `palette` | live via palette |
| `lsp.goToSuper` | `cmd+u` | fragile | `palette / Navigate menu / context menu` | live via palette / Navigate menu / context menu |
| `lsp.hover` | `ctrl+q` | delivered | `—` | live |
| `lsp.implementations` | `cmd+alt+b` | fragile | `palette / Navigate menu / context menu` | live via palette / Navigate menu / context menu |
| `lsp.nextDiagnostic` | `f2` | delivered | `—` | live |
| `lsp.organizeImports` | `ctrl+alt+o` | fragile | `palette / context menu` | live via palette / context menu |
| `lsp.parameterInfo` | `cmd+p` | fragile | `ctrl+p` | live via ctrl+p |
| `lsp.peekDefinition` | `cmd+y` | fragile | `palette` | live via palette |
| `lsp.prevDiagnostic` | `shift+f2` | delivered | `—` | live |
| `lsp.references` | `alt+f7` | fragile | `palette` | live via palette |
| `lsp.referencesPanel` | `cmd+alt+f7` | fragile | `palette` | live via palette |
| `lsp.rename` | `shift+f6` | delivered | `—` | live |
| `lsp.typeHierarchy` | `ctrl+h` | delivered | `—` | live |
| `markdown.preview` | `cmd+alt+m` | fragile | `palette` | live via palette |
| `menu.open` | `f10` | delivered | `—` | live |
| `nav.back` | `cmd+alt+left` | fragile | `mouse-back` | live via mouse-back |
| `nav.bookmarks` | `cmd+f3` | fragile | `palette` | live via palette |
| `nav.forward` | `cmd+alt+right` | fragile | `mouse-forward` | live via mouse-forward |
| `nav.pinGoto1` | `ctrl+shift+1` | fragile | `palette (or the cmd+2 picker)` | live via palette (or the cmd+2 picker) |
| `nav.pinGoto2` | `ctrl+shift+2` | fragile | `palette (or the cmd+2 picker)` | live via palette (or the cmd+2 picker) |
| `nav.pinGoto3` | `ctrl+shift+3` | fragile | `palette (or the cmd+2 picker)` | live via palette (or the cmd+2 picker) |
| `nav.pinGoto4` | `ctrl+shift+4` | fragile | `palette (or the cmd+2 picker)` | live via palette (or the cmd+2 picker) |
| `nav.pins` | `cmd+2` | fragile | `palette` | live via palette |
| `notifications.history` | `cmd+alt+n` | fragile | `palette` | live via palette |
| `palette.keymapHelp` | `f1` | delivered | `—` | live |
| `palette.recentFiles` | `cmd+e` | fragile | `palette` | live via palette |
| `palette.searchEverywhere` | `cmd+shift+a` | fragile | `palette (esc esc)` | live via palette (esc esc) |
| `pane.close` | `ctrl+alt+w` | fragile | `palette / pane context menu` | live via palette / pane context menu |
| `pane.focus1` | `ctrl+1` | delivered | `—` | live |
| `pane.focus2` | `ctrl+2` | delivered | `—` | live |
| `pane.focus3` | `ctrl+3` | delivered | `—` | live |
| `pane.focus4` | `ctrl+4` | delivered | `—` | live |
| `pane.focus5` | `ctrl+5` | delivered | `—` | live |
| `pane.focus6` | `ctrl+6` | delivered | `—` | live |
| `pane.focus7` | `ctrl+7` | delivered | `—` | live |
| `pane.focus8` | `ctrl+8` | delivered | `—` | live |
| `pane.focus9` | `ctrl+9` | delivered | `—` | live |
| `pane.maximize` | `cmd+k z` | fragile | `palette` | live via palette |
| `pane.resizeMode` | `ctrl+alt+r` | fragile | `palette / pane context menu` | live via palette / pane context menu |
| `pane.splitDown` | `cmd+k down` | fragile | `palette` | live via palette |
| `pane.splitLeft` | `cmd+k left` | fragile | `palette` | live via palette |
| `pane.splitRight` | `cmd+k right` | fragile | `palette` | live via palette |
| `pane.splitUp` | `cmd+k up` | fragile | `palette` | live via palette |
| `pane.switcher` | `ctrl+tab` | fragile | `tab key` | live via tab key |
| `perf.hud` | `ctrl+alt+p` | fragile | `palette / View menu` | live via palette / View menu |
| `playground.open` | `cmd+shift+j` | fragile | `palette / Tools menu` | live via palette / Tools menu |
| `problems.toggle` | `cmd+8` | fragile | `palette` | live via palette |
| `project.close` | `cmd+shift+w` | fragile | `palette / File menu` | live via palette / File menu |
| `project.findInAllProjects` | `cmd+alt+shift+f` | fragile | `palette` | live via palette |
| `project.findInAllProjectsResults` | `cmd+alt+shift+r` | fragile | `palette` | live via palette |
| `project.findInPath` | `cmd+shift+f` | fragile | `palette` | live via palette |
| `project.goToClass` | `cmd+o` | fragile | `palette` | live via palette |
| `project.goToFile` | `cmd+shift+o` | fragile | `palette` | live via palette |
| `project.peek.return` | `cmd+shift+b` | fragile | `palette` | live via palette |
| `project.replaceInPath` | `cmd+shift+r` | fragile | `palette` | live via palette |
| `project.switch` | `cmd+shift+p` | fragile | `palette` | live via palette |
| `project.switchLast` | `cmd+shift+e` | fragile | `palette` | live via palette |
| `project.switchMRU1` | `ctrl+alt+1` | fragile | `palette (or the cmd+e recent-projects column)` | live via palette (or the cmd+e recent-projects column) |
| `project.switchMRU2` | `ctrl+alt+2` | fragile | `palette (or the cmd+e recent-projects column)` | live via palette (or the cmd+e recent-projects column) |
| `project.switchMRU3` | `ctrl+alt+3` | fragile | `palette (or the cmd+e recent-projects column)` | live via palette (or the cmd+e recent-projects column) |
| `project.switchMRU4` | `ctrl+alt+4` | fragile | `palette (or the cmd+e recent-projects column)` | live via palette (or the cmd+e recent-projects column) |
| `project.switchMRU5` | `ctrl+alt+5` | fragile | `palette (or the cmd+e recent-projects column)` | live via palette (or the cmd+e recent-projects column) |
| `project.switchMRU6` | `ctrl+alt+6` | fragile | `palette (or the cmd+e recent-projects column)` | live via palette (or the cmd+e recent-projects column) |
| `project.switchMRU7` | `ctrl+alt+7` | fragile | `palette (or the cmd+e recent-projects column)` | live via palette (or the cmd+e recent-projects column) |
| `project.switchMRU8` | `ctrl+alt+8` | fragile | `palette (or the cmd+e recent-projects column)` | live via palette (or the cmd+e recent-projects column) |
| `project.switchMRU9` | `ctrl+alt+9` | fragile | `palette (or the cmd+e recent-projects column)` | live via palette (or the cmd+e recent-projects column) |
| `run.file` | `shift+f10` | delivered | `—` | live |
| `run.rerun` | `cmd+f5` | fragile | `palette / Run menu` | live via palette / Run menu |
| `run.select` | `alt+shift+f10` | fragile | `palette / Run menu` | live via palette / Run menu |
| `run.testAtCursor` | `ctrl+shift+f10` | delivered | `—` | live |
| `scratch.generate` | `cmd+alt+shift+n` | fragile | `palette / File menu` | live via palette / File menu |
| `scratch.new` | `cmd+shift+n` | fragile | `palette` | live via palette |
| `scratch.newFromSelection` | `cmd+alt+shift+s` | fragile | `palette` | live via palette |
| `scratch.promote` | `cmd+alt+shift+p` | fragile | `scratch manager ctrl+p / palette` | live via scratch manager ctrl+p / palette |
| `search.nextMatch` | `f3` | delivered | `—` | live |
| `search.open` | `cmd+f` | fragile | `vim / (every pane binds it, #2409)` | live via vim / (every pane binds it, #2409) |
| `search.prevMatch` | `shift+f3` | delivered | `—` | live |
| `settings.open` | `cmd+,` | fragile | `palette` | live via palette |
| `structure.toggle` | `cmd+3` | fragile | `palette` | live via palette |
| `terminal.new` | `cmd+alt+shift+t` | fragile | `palette` | live via palette |
| `terminal.newTab` | `ctrl+t` | delivered | `—` | live |
| `terminal.popup` | `cmd+alt+t` | fragile | `palette` | live via palette |
| `terminal.popup.pin` | `cmd+alt+shift+k` | fragile | `palette` | live via palette |
| `terminal.toggle` | `alt+f12` | fragile | `palette` | live via palette |
| `tests.toggle` | `cmd+4` | fragile | `palette / View menu` | live via palette / View menu |
| `time.toggle` | `cmd+alt+0` | fragile | `palette / Tools menu` | live via palette / Tools menu |
| `todo.list` | `cmd+6` | fragile | `palette` | live via palette |
| `vcs.diff` | `cmd+alt+d` | fragile | `palette` | live via palette |
| `vcs.panel` | `cmd+9` | fragile | `palette` | live via palette |
| `vcs.revertFile` | `cmd+alt+z` | fragile | `palette` | live via palette |
| `view.followFilter` | `alt+shift+g` | fragile | `palette` | live via palette |
| `view.toggleFollow` | `alt+shift+f` | fragile | `palette` | live via palette |
| `view.toggleWrap` | `alt+shift+w` | fragile | `palette` | live via palette |
| `view.zenMode` | `ctrl+alt+f` | fragile | `palette / View menu` | live via palette / View menu |
| `window.hideAllTools` | `cmd+shift+f12` | fragile | `palette` | live via palette |
| `window.layouts` | `alt+shift+f12` | fragile | `palette` | live via palette |
| `window.restoreLayout` | `shift+f12` | delivered | `—` | live |
| `xml.xmqPlayground` | `ctrl+alt+x` | fragile | `palette / Tools menu` | live via palette / Tools menu |
| `yaml.yqPlayground` | `ctrl+alt+y` | fragile | `palette / Tools menu` | live via palette / Tools menu |
