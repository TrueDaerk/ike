---
type: concept
title: Settings UI & Menu Bar
description: Roadmap 0160 — the menu bar over the command registry; the settings panel (pages, schema-driven forms) lands in later sub-issues.
resource: internal/menu
tags: [architecture, menu, settings, ui, commands]
timestamp: 2026-07-28T01:40:00Z
---

# Settings UI & Menu Bar

Roadmap 0160. File-based settings stay the source of truth; this stream adds a
JetBrains-like discovery layer: a **menu bar** and (in later sub-issues) a
**settings panel** whose changes persist through the config
[write-back layer](./config.md) and hot-reload.

## Menu bar (#90)

`internal/menu` renders the top row — File · Edit · View · Navigate · Tools ·
Settings · Help — above the pane tree (the layout's `bodyRect` starts one row
lower; `ui.menu_bar = false` hides it and returns the row).

- **Menus are data.** Every entry references a registered command id
  (`menu.Defaults`). The app resolves each id through the registry: registered
  entries show the same shortcut the cheatsheet shows (`registry.Binding`,
  falling back to the command's doc hint); unregistered ids render **disabled**
  with the blocked-ledger dependency (or "not available yet") as the hint.
  There is no parallel dispatch: selecting an entry emits `menu.RunMsg`, which
  the root model feeds into `RunCommand`.
- **Keyboard:** `f10` (command `menu.open`) toggles the first menu; while a
  dropdown is open the menu owns the keys — ←/→ switch menus, ↑/↓ navigate
  (skipping disabled entries, wrapping), enter runs, esc closes. Pressing a
  title's first letter jumps to (and opens) that menu, case-insensitively
  (duplicate letters cycle forward); while open, the bar underlines each
  title's first letter as the hint.
- **Mouse:** clicking a title on the bar row opens/switches that menu; clicking
  an entry runs it; clicking elsewhere closes the dropdown. Moving the mouse
  over a dropdown entry selects it (hover follows focus; disabled entries are
  skipped, like keyboard navigation).
- **Rendering:** the dropdown is an overlay (`overlay.Place`) below the bar,
  framed by a rounded border so it separates from the content it floats over,
  never disturbing the pane layout. Hit-testing accounts for the border: the
  first entry sits on row 2, one column inside the frame.

## Settings panel framework (#91)

`internal/settings` is a centered **floating panel** (#115): a rounded-border
box capped at ~110×32 cells above the workspace, laid out as the
**three-column master·detail grid** described under
[Master·detail grid (0460)](#masterdetail-grid-0460-1295), opened via
`settings.open` (cmd+, / menu bar / palette).

- **Schema-driven.** A `Page` is a titled list of `Entry` descriptors — config
  key, control type (`Bool`/`Int`/`String`/`Enum`/`Path`/`Chord`/`List`), write scope,
  title, description, enum options, int bounds. The form renders from the
  descriptor; there are no hand-built page UIs.
- **Apply-on-change, single source of truth.** The panel never caches values:
  every render reads `config.Get().Flat()`, and every edit returns a
  `config.WriteAndReload` command — the write-back layer persists the key and
  the reload pipeline re-applies it. Bool toggles apply on enter (space too);
  every other type opens its **typed editor in the detail column** (0460,
  #1295) — see the grid section below. On the settings row itself, →/l
  quick-cycles an enum to the next option (wrapping) without leaving the
  column (#383) and steps an int; ← mirrors it on those two types and
  otherwise returns to the category column (#533). List (#1139) commits a
  **TOML string array** (`[]string`), because the typed schema field is a
  string slice and a raw comma string would fail the decode. Path inputs get shell-style
  **tab completion** (#541) via the shared `internal/pathcomplete` engine:
  matching entries render as a suggestion list under the row (final path
  component only, capped with a `+N more` tail), tab extends the input to the
  longest unambiguous prefix — a single directory match completes with its
  trailing separator so repeated tab descends; `~` notation is preserved and
  matching falls back to case-insensitive (`~/dev` finds `~/Development`). The selected entry's
  description, key and validation error render in a **footer pinned to the
  bottom of the form column** — not inline under the row — so ↑↓ never shifts
  the other rows (#535); only the enum picker expands inline. The custom pages
  (Toolchain, Keymap, Language Servers, Tools, PHP Debug Mappings #832)
  follow the same layout via a shared
  `pinFooter` helper (#537): header line(s) pinned top, the list scrolls to
  follow the selection, and hints / failure details / env status / inline
  override inputs render in a constant-height footer pinned bottom. Custom-
  page footer lines word-wrap to the column width through the shared
  `wrapFooter` helper (#553) — Toolchain: two hint lines + status, Language
  Servers: three lines, Keymap: two — so long key hints stay readable on
  narrow windows instead of clipping mid-word.
- **Layer indicator + reset.** Each row shows `@default` / `@user` /
  `@project` (`config.Origin`); overridden values are tinted; `r` resets
  (RemoveAndReload — fall back through the layers).
- **Write-scope selector (0380, #794).** `s` cycles the write target:
  `auto` (each entry's conventional `DefaultScope` layer) → `user` →
  `project`. A forced scope renders as a `[scope: …]` chip on the title row
  and routes **every** write and reset (`scopeFor`) — so any setting can be
  overridden per project (`.ike/settings.toml` is created on the first
  project write) and a project override removed with `r` falls straight
  back to the user/global value on the reload. Custom pages keep their own
  keys (`s` on the Tools page still opens suggestions — the panel's
  selector only applies to schema rows).
- **Filter.** `/` starts a type-to-filter across all schema pages (titles,
  keys, page names); matches render as `Page › Title`, and the result list
  names the custom pages the filter cannot search (`(not searched: Keymap,
  …)`, #383). Esc clears the filter, then closes the panel.
- **Keys.** ↑↓/jk navigate, ←→ (and h/l) or tab switch columns, enter edits,
  esc cancels/closes (#383). On custom pages only arrow-left returns to the
  categories — plain `h` is forwarded to the page (it may be filter text
  there).
- **Scrolling.** Both columns scroll to follow the selection on short windows
  (a shared `follow` offset helper in `view.go`); nothing is hard-truncated
  out of reach (#383).
- **Focus clarity.** The focused column shows the vivid selection bar; the
  unfocused column keeps a dimmed (faint) selection background, so keyboard
  ownership is always visible (#383).
- **Mouse (#127, #673).** Clicking a category selects that page; clicking a
  form entry selects it, and a second click on the selection activates it
  (enter semantics). The wheel scrolls the column under the pointer by moving
  its selection (categories switch pages, form rows move like j/k). While an
  enum picker is open, clicking an option applies it and clicking anywhere
  else closes the picker; while an inline edit is active, a click on the row
  keeps the edit and a click elsewhere commits it (cancelling instead when
  the input does not validate — chord capture just cancels). Clicks outside
  the panel dismiss it (#116). Custom pages take part through optional
  `PageClicker` / `PageWheeler` interfaces on the `PageModel` seam (#674):
  the panel forwards form-column presses page-locally ((0,0) = the page's
  render origin) and wheel deltas via type assertion, so pages without the
  seams stay valid. All five custom pages implement them — click selects a
  row, a click on the selection performs the page's enter-equivalent action
  (Toolchain opens the picker and picker rows are clickable, Keymap starts
  the chord capture and the header row opens the filter, LSP toggles the
  per-server enable, Plugins/Marketplace toggle the detail expansion), and
  the wheel moves the selection (picker highlight / package window in their
  modes); clicks cancel modal captures/inputs instead of being swallowed.
- **Registry seam.** Plugins contribute pages via
  `Capabilities.SettingsPages`; the app appends `reg.SettingsPages()` to the
  built-in `settings.BasePages()` (the toolchain page #94 uses this).

## Resizing (#774)

`cmd+shift+arrows (macOS; spelled shift+super) / ctrl+shift+arrows / alt+shift+arrows` resize the open panel (width ±4, height ±1) unless the
panel is capturing keys verbatim (`Model.Capturing()`: an edit/pick/filter
input or a custom page's chord capture). The root model owns the chord: it
adjusts the shared `ui.WinSizes` store (kind `"settings"`, persisted in the
per-project `winsize.json`) and re-derives `settingsSize()`, which clamps
base+delta into the live terminal bounds. **Mouse resize** (#933): pressing
the panel's border ring starts a drag — edges resize one axis, corners both —
applied through the same store (un-persisted per motion step, flushed on
release), so key and mouse resizes share one remembered size. The panel's
default width honours `ui.popup_max_width` (#932, default 110) instead of a
hardcoded cap; the Appearance page exposes the setting and edits apply live.

## Page catalog (#92)

`settings.BasePages(themes)` ships the core pages; every entry carries a
description (the panel doubles as settings documentation), and a test fails on
any entry whose key the typed schema does not expose (no dead keys).

- **Editor** — tab width, use spaces, auto indent, auto save (focus|off,
  #174), trim trailing whitespace, insert final newline, line numbers
  (+relative), scroll offset, soft wrap, show whitespace: every key
  `applyConfig` reads live.
- **Diagnostics** (#1259) — the per-source, per-severity decoration toggles
  (`editor.marks.lsp_*`, `editor.marks.git_*`; user scope) and the
  project-scoped `lsp.diagnostics_ignore` rule list the editor's
  "Ignore Diagnostic Under Caret" command appends to.
- **Appearance** — theme (enum fed from the registry's theme list; writing
  `theme.name` hot-reloads, so selection previews immediately), menu bar
  on/off, command-palette chord.
- **Files & Session** — restore last project, `files.watch`, `files.auto_reload`
  (clean|never, #81), `files.persistent_undo` (undo survives restarts, #148).
- **Backup** — crash recovery on/off (`backup.enable`; disabling purges existing
  snapshots), snapshot debounce (`backup.debounce_ms`), snapshot max age
  (`backup.max_age_days`) (#167, see [crash recovery](./crash-recovery.md)).
- **Notifications** — toast timeout, severity floor.

## Keymap page (#93)

A custom `PageModel` (the framework's seam for self-rendered pages, forwarded
every key while focused — verbatim during chord capture and while the `/`
filter input is open, #531). See
[Keybindings](./keybindings.md) for the full editor behavior: effective-table
listing with layer badges and blocked/fragile flags, capture-based rebinding
with conflict confirmation, unbind and reset-to-preset.

## Toolchain page (#94)

A custom `PageModel` listing every registered language with a server or
toolchain: effective interpreter (`lang.Interpreter` — explicit `[lang.<id>]
interpreter` beats detection), source badge (`@config`/`@detected`) and an
async version probe (`p`, `python --version` / `php -v` as `tea.Cmd`s routed
back via `settings.VersionMsg` → `Model.Deliver`). Enter opens the discovery
picker — Python: active venv, project `.venv`/`venv`, `uv python list`, pyenv
shims, PATH; PHP: PATH + common install locations; every language: the
versioned install directories (#675 — Homebrew `opt/<formula>[@*]/bin`,
pyenv `~/.pyenv/versions/*`, Go `~/sdk/go*`, newest first, deduplicated by
resolved path). The picker opens pre-selected on the currently effective
interpreter and probes every candidate's version eagerly, so versions render
without pressing `p` — plus a validated custom
path input with tab completion and a live suggestion list (#541, same
`internal/pathcomplete` engine as the schema Path entries). A choice writes
the **project** config and triggers `lsp.restart`
so servers respawn against the new interpreter; `r` resets to detection.
Python rows additionally show an environment **provenance** column
(`uv venv`/`venv`/`uv managed`/`pyenv`/`system`), `i` opens an inline
installed-packages view (async) and `n` runs
the guided environment-creation wizard (tool → Python → target directory) —
see [Language Registry](./languages.md) (#569).

The package view manages packages too (#571, PyCharm-style): `j`/`k` move a
row selection, `+` opens an install input (`name` or `name==version`), `-`
uninstalls the selection after a `y/n` confirm, `u` upgrades it, and rows
with an available upgrade carry an `↑ <latest>` marker (fetched async via
`pip list --outdated --format=json` / the uv equivalent when the view
opens). All actions run in `tea.Cmd`s — the UI never blocks; the busy
marker shows in-flight work, results (including the decisive stderr line on
failure) land in the view's status row, and the listing refreshes in the
same message. Backend selection per environment: a uv project
(pyproject.toml + uv.lock, interpreter inside the project) goes through
`uv add`/`uv remove`/`uv lock --upgrade-package` + `uv sync` so manifest
and lockfile stay in sync; otherwise `uv pip … --python <interp>` when uv
is on PATH, plain `<interp> -m pip …` else.

## Language Servers page (0180, #130)

A custom `PageModel` contributed by the **LSP plugin** via
`plugin.Capabilities.SettingsPages` (`internal/settings/lsp_page.go`): one row
per registered language carrying a server — live status (`ready` / `idle` /
`crashed` / `missing` / `disabled` / `off (master)`, from language-tagged
`ServerStatusMsg`s the root model forwards via `Model.Deliver` plus the
manager's `RunningLangs`), the effective command line (config overlay over the
plugin baseline, mirroring the launch path) and the layer supplying it
(`@project`/`@user`/`@built-in`). Controls: `E` flips the `lsp.enabled` master
switch, `e` the per-server `lsp.servers.<id>.enabled`, `c`/`a`/`s` edit
command / args / settings (JSON object) overrides inline — written to the
**project** config via write-back, empty input resets the key — `x` clears all
of a server's overrides, `r` restarts one server (`Manager.StopLang`, async
per #123: work inside the returned `tea.Cmd`), `R` restarts all. A missing
binary renders the launch-failure reason; `i` runs the plugin's install
recipe manually and `A` toggles `lsp.auto_install` (#131 — the automatic
install on first use, with the manual action as fallback/retry). `I` toggles
`lsp.inlay_hints` (default off, #523), `S` toggles `lsp.signature_auto`
(the automatic signature popup on trigger characters; the manual
`lsp.parameterInfo` command works regardless) and `C` toggles
`lsp.completion_auto` (the as-you-type completion popup on identifier
characters, #527; server trigger characters and `ctrl+space` work
regardless), all shown in the header row.

## Marketplace page (0310, #446)

A custom `PageModel` (`internal/settings/marketplace_page.go`) over
`internal/market`: browse the plugin catalog, review a plugin's requested
capabilities, install/update/remove. Install (`i`) is only reachable from the
expanded detail (`enter`) where the full capability list renders — the trust
model's review step; `x` removes, `r` re-fetches. Async results arrive as
`MarketCatalogMsg`/`MarketActionMsg` through `Model.Deliver`; opening the
panel prefetches the catalog once. See
[Plugin Marketplace](./marketplace.md).

## Sub-panels (0420, #883)

Multi-field and multi-step flows run in **pushed sub-panels** instead of
pinned-footer state machines: `settings.SubPanel` (Title/Update/View/
Capturing/Buttons) levels stack on the panel (`Push`/`Pop` via the
`SubPanelHost` seam injected into `hostAware` custom pages), rendered as a
bordered box centered over the panel with a breadcrumb header
(`Settings › Tools › New Tool`), the content area, and a clickable button row.
Esc pops exactly one level (capturing panels — text forms, chord capture —
own every key and pop themselves); a button's optional `Key` triggers it from
the keyboard on non-capturing panels; `SubPanelClicker`/`SubPanelWheeler`
route content-local mouse events, and presses outside the box are swallowed,
never a close. Async messages `Deliver`ed to the panel reach open sub-panels
implementing `MsgReceiver`. First migration: the Tools add/edit form — fields
are click-to-focus rows, Save/Cancel are buttons, a stray click no longer
destroys typed input, and backspace is rune-safe.

## Venv wizard (0420, #884)

The Python environment creation is a four-step sub-panel (`venv_wizard.go`),
replacing the hidden `n` state machine: **Tool** (uv recommended / stdlib
venv — both always listed, unavailable ones disabled with the reason; the uv
scaffold side effect `pyproject.toml + uv.lock` disclosed up front), **Python**
(uv versions / discovered interpreters with provenance, fetched off the UI
goroutine, highlight-followed windowing), **Location** (path input prefilled
`.venv`, clickable completion suggestions, live already-exists note),
**Run** (spinner + elapsed via `WizardTickMsg`, cancel kills the child through
`context`, failures show the tools' combined output tail — `execRunCtx`
captures stderr the old `execRun` swallowed). Esc means back one step. The
result still lands as `EnvMsg`: the app writes `lang.python.interpreter` at
project scope and offers the LSP restart, unchanged. Entry points: the
visible `+ New environment…` action row in the Toolchain list (enter/click),
the `n` shortcut, and the `python.newEnvironment` palette command
(`Model.OpenPythonEnvWizard`).

## Mouse parity (0420, #885)

The panel is pointer-complete: **hover** (routed `MouseMotionMsg`) underlines
the rail row, form row or plugin row under the cursor and moves an open enum
picker's highlight (menu-bar parity; custom pages opt in via `PageHoverer`).
The **wheel scrolls viewports, not selections** — one category per notch on
the rail, list lines on the schema form; render only re-follows the selection
after it actually moved, so wheel-browsing is never snapped back. Clickable
chrome: the **scope chip** is always visible on the title row and cycles
auto/user/project on click, the **hint-row keys** execute their action
(enter/r/s///esc), and **path-completion suggestion rows** complete the edit
instead of cancelling it. The Plugins and Marketplace lists scroll through
`pinFooter` offsets — previously a `MaxHeight` clip made rows past the window
unreachable.

## Search over everything (0420, #886)

The "/" filter reaches the whole product: schema entries (edit in place, as
before), **category titles** (jump rows — enter lands on the page) and
**custom-page items** through the `Searchable{SearchItems()}` seam — a
`SearchItem` carries a label, extra keywords and an `Activate` callback that
positions the page (select the row) after the panel navigates there.
`/python` lists `Toolchain › python` and `Toolchain › New Python environment`;
enter clears the filter and selects the row. The rail stays alive while
filtering — a click clears the filter and jumps to the page. The
"not searched" note now lists only pages that don't export items yet.
Implemented: Toolchain, Tools, Plugins; the remaining pages join with their
sub-panel migrations (#892).

## Unified keys (0420, #887)

One table across the panel and every page: **enter** activates, **space**
toggles booleans, **r** always means reset (LSP server overrides included —
restart moved to **R** selected / **ctrl+r** all; the marketplace refresh
moved to **g**), **s** is reserved for the write scope everywhere (the LSP
options JSON edit moved to **o**; the Tools suggestions gained a visible
`+ Suggestions…` action row next to the `s` shortcut). Every list understands
**pgup/pgdn/home/end** through the shared `listNav` helper. Schema `Chord`
entries capture through a shared sub-panel with keymap-page semantics —
multi-step chords, enter confirms, backspace undoes a step — instead of
grabbing the next keypress. **?** opens a key-help sub-panel listing the
shared keys plus the active page's (`KeyHelper` seam).

## Shared text input (0420, #888)

Every settings inline edit routes through one `textField` wrapping the
shared `ui.EditKey`/`ui.CursorView` (#763): a movable cursor, home/end,
word motions, word deletes — and **rune-safe backspace** (the nine
hand-rolled append-only inputs byte-sliced backspace and corrupted umlauts).
Ported: schema String/Int/Path edits, the keymap import path, the toolchain
custom path, the venv wizard's location step, the Tools and PHP-debug-mapping
forms, and the LSP override fields.

## Widget affordances (0420, #889)

Every schema row announces how it edits before enter is pressed. The glyphs
were unified into the wireframes' **value markers** in 0460 (#1295): `◉`
toggle · `‹›` stepper · `▸` list · `⌨` capture · `≡` multi-value list · `✎`
free text. The row still carries `←/→` cycling for enums (← on other rows
returns to the rail, #533) and `+/−/←/→` stepping for ints, range-clamped.
Range clamps are never silent: stepping or typing past Min/Max shows an
`ℹ clamped to N` notice in the detail column.

## Rail & chrome (0420, #890)

The category rail groups into **sections** (`Page.Section` starts one: CORE /
TOOLS / PLUGINS today), rendered as dim non-clickable headers. **First-letter
jump** hops to the next page starting with the pressed letter (menu parity).
The panel **remembers its page**: reopening lands where you left, and the
choice persists per project in `.ike/settings-last.json`
(IKE_CONFIG_DIR-redirectable). The title row reads `SETTINGS › <Page>`, and
overflowing rail/form windows show `▲ more` / `▼ more` scroll indicators.

## Feedback & safety (0420, #891)

Destructive actions confirm in a small sub-panel (`confirmPanel`: enter/y
confirm, esc/n cancel) — Tools delete, PHP-mapping delete, Marketplace
remove, Keymap unbind. Successful schema writes flash `✓ saved to
user/project` in the detail footer; config write/reload diagnostics surface
**inline** there too (error-styled, until the next action), not only as
toasts. The enum option list **follows the highlighted option** — a long theme
list can no longer move the highlight below the fold.

## Modal-flow migrations complete (0420, #892)

Every remaining inline modal state now runs as a sub-panel: the **keymap
chord capture** (same semantics — multi-step chords, fragile warning,
conflict confirm — in a dialog with Apply/Cancel), the **JetBrains import
path** (cursor input, clickable completion suggestions), the **LSP override
editor** (command / args / options JSON, validated in place), the **uv
Python-install picker** (windowed list, wheel, click-to-install) and the
**PHP path-mapping form** (click-to-focus fields, Save/Cancel). The pages
themselves no longer capture keys; all seven custom pages export
`SearchItems`, so the "not searched" note is gone. Toolchain package
management (#571) should land as a sub-panel on this same pattern.

## Master·detail grid (0460, #1295)

The panel renders a **fixed three-column raster** on every schema page, taken
verbatim from the 0460 wireframes (epic #1294). The rule behind it: *every
value has a type, and every type has a picker* — never a free text field where
a list is possible.

```
24ch nav │ 44ch settings + value marker │ rest detail = explanation + editor
```

- **Geometry** (`gridFor`, `internal/settings/view.go`). The rail is fixed at
  24 columns. The settings column takes its nominal 44 where there is room and
  **shrinks to 28 before the detail column is dropped** — three columns beat a
  wide value column. Below that the detail becomes a **band under the list**,
  separated by a rule: a narrow terminal loses the column, never the content.
- **Focus** is a three-state cycle: `tab` walks nav → settings → detail → nav,
  `↑↓`/`jk` move inside the focused column. `←`/`h` still returns to the rail
  from the settings column except on enum/int rows, where it is a value change.
- **The detail column is never empty.** With the rail focused — or with nothing
  selectable — it shows the **page description** (`Page.Description`, filled
  for every built-in page) and what the page contains. With an entry selected
  it shows title, wrapped description, a meta row (`key · type · default: …`,
  the default read from the new `config.Defaults()`), the editor, and a pinned
  bottom band carrying the write feedback and `set in <origin> · writes to
  <scope>`. Provenance moved here from the old `@layer` column — the marker
  earns that space.
- **Typed editors** (`internal/settings/editor.go`) implement one interface:

  ```go
  type Editor interface {
      View(w, h int) []string
      Update(key tea.KeyPressMsg) tea.Cmd
      Value() any
      Dirty() bool
      Capturing() bool
  }
  ```

  `boolEditor` (◉/○ radio rows), `intEditor` (`‹ n ›` stepper plus typed
  entry, both clamped), `enumEditor` (**type-to-filter** option list, current
  value marked `●`), `pathEditor` (text plus live `pathcomplete` candidates and
  an existence check), `listEditor` (indexed rows, `enter` edits, `d` removes,
  `+ add value…`), `chordEditor` (hands off to the shared capture sub-panel)
  and `textEditor` (the last-resort free text). Adding a setting needs a type
  and documentation, never new UI.
- **Key routing.** While the detail column has the focus its editor receives
  every key, including `esc` — each editor decides whether that cancels its
  input or hands the focus back. `tab` is the only reserved chord, and only
  when the editor is not a text input (a path editor needs tab for
  completion).
- **Nothing expands inline any more.** The settings rows map 1:1 to lines, so a
  selection move cannot shift what is under the pointer, and a click hit-test
  is a plain offset.
- **The footer is three context keys**, not a nine-key legend: what the focused
  column can do, plus `? all keys`. The full set lives in the `?` cheatsheet
  overlay, grouped move / edit / global.

## Staged apply (0460, #1296)

Schema edits no longer hit disk per keystroke. They collect in an ordered
**staging buffer** (`internal/settings/staged.go`) and reach the config only
when the batch is applied — one write pass, **one** reload, so the app
re-themes and rebuilds its keymaps once instead of once per changed key
(`config.ApplyAndReload`).

- **Reads** go through `m.value(key)`: the staged value when one exists,
  otherwise the live config. Nothing else in the panel had to learn about
  staging.
- **Counting.** The header carries `● n changes · ctrl+s apply` (clickable),
  the rail marks each page with `●n`, and the detail column shows the selected
  row's `● old → new`. A value edited back to where it started drops out of the
  buffer, so the counter cannot lie.
- **Applying** is `ctrl+s`, not enter — enter is the editor key on every row.
  It opens the **diff panel**: one line per change as `page · key · old → new`,
  the target layer in the title, `enter` writes, `u` drops the selected line,
  `s` retargets the whole batch at another layer, `d` discards everything.
  Clicking a line selects it; clicking the selected line drops it.
- **esc never discards silently.** With edits pending it opens the same diff,
  and writing or discarding from there completes the close.
- **Live preview.** Keys whose whole point is their appearance (today
  `theme.name`) emit a `settings.PreviewMsg` when staged; the app applies it
  without persisting. Discarding — or dropping the line — sends the previous
  value back the same way, so a previewed theme is always undone.
- **Reset (`r`) stages a removal** like any other edit; the diff shows
  `old → default`.
- Custom pages keep writing directly: installing a plugin or creating a
  virtualenv is not "a value in a file" and cannot be staged meaningfully.

## Search inside the grid (0460, #1297)

`/` no longer flattens everything into one list beside a dead rail. The query
takes over the grid instead, keeping all three columns doing their job:

- **Column 1** becomes *pages with hits* — one row per page carrying matches,
  with its count. Moving there jumps the match list to that page's first hit;
  moving in the match list walks the rail back (`syncHitSel`), so the two
  always agree on "where am I".
- **Column 2** lists every match as `Page › Title`, with the matched substring
  marked and the value marker intact.
- **Column 3** stays the editor for the highlighted match, so `enter` **sets
  the value right there** — the search is not a navigation detour.
- `tab` leaves for the match's own page, positioned on that row; `esc` clears
  the query.
- The header reads `⌕ query · 7 hits · 5 pages`, and the footer switches to
  `enter set here · tab open page · ? all keys`.

Custom-page items keep coming through the `Searchable` seam (#886) and still
navigate on enter.

## Keymap page on the grid (0460, #1298)

The keymap page adopts the same raster through `splitGrid`, so a custom page
looks like a schema page:

- **Settings column**: `chord · command`. Context, layer and provenance left
  the table — they are detail, not scanning cues.
- **Detail column** (`keymap_detail.go`): the selected command's title and id,
  then `bindings · n` listing **every** chord bound to it with its context and
  `@default` / `@user` layer, then its conflict state — `✓ no conflicts`, or
  the other commands sharing the chord plus two **free chords** taken from the
  live table (`suggestChords` never offers a bound one). Fragile chords keep
  their warning. Clicks there are inert: it is read-only chrome.
- **Conflicts are a decision, not a yes/no.** The capture sub-panel offers
  *Replace & unbind other* (`enter`), *Pick a different chord* (`p`, which
  clears the recorded steps and keeps capturing) and *Cancel*. The wireframes'
  third option — keep both, resolve by context — needs a config spelling for
  context-qualified overrides and is tracked as #1312.

Keys stay as they were (`enter` rebind · `u` unbind · `r` reset · `i` import):
the wireframe's `r rebind` would have collided with the established reset.
Range folding of `alt+1 … alt+9` style runs lands with the rest of the noise
folding in #1300.

## Toolchain page on the grid (0460, #1299)

Two things were wrong before: the flat list mixed twelve `(not found)` rows in
with the three that mattered, and the candidate picker only appeared *after*
pressing enter — so a first run looked like an empty slate you were expected to
type paths into.

- **Grouping.** Rows group by state: `configured`, `detected · not configured`,
  `not installed · n`. The last group is **folded** behind its counted caption
  until `z`. Captions are structure, not targets: navigation and clicks skip
  them, and search reaches into the folded group (activating a result unfolds
  it).
- **The selection follows the language, not the line.** Configuring an
  interpreter moves its row into another group; `selKey` re-resolves the
  cursor after every regrouping.
- **Detail column** (`toolchain_detail.go`): the discovered candidates for the
  selected language — every one a real find, with its provenance and probed
  version, the one in use marked `●`, and *enter a path manually…* as the
  **last** row. While discovery is in flight it says `scanning…`, never an
  empty field. Long paths elide from the left so the version and provenance —
  what actually distinguishes two candidates — stay visible.
- **First run.** With nothing selected the column explains what a toolchain is
  and offers `a · accept all n recommendations`: one key writes every detected
  interpreter (one batch, one reload) instead of a guided picker per language.

Still open on the epic: noise folding (#1300).
