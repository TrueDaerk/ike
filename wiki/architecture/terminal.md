---
type: concept
title: Integrated Terminal
description: Roadmap 0170 — PTY-spawned shell rendered through a VT emulator as a pane; raw key routing with a documented reserved set, scrollback paging + search, clickable file:line references, layout restore as fresh shells, sessions surviving project switches; command sessions + occupied tracking for run-in-terminal (0350); popup terminal overlay outside the pane layout (#1398) with side-by-side split and input broadcast (#1427), titlebar move with persisted position, tab tear-out into z-ordered floating panels, and a global (cross-project) panel toggle (#1793).
resource: internal/terminal
tags: [architecture, terminal, pty, vt, pane, run]
timestamp: 2026-08-11T12:00:00Z
---

# Integrated Terminal (Roadmap 0170)

`internal/terminal` embeds a real shell as a pane (spec: epic #88), complete
across the epic's four slices: PTY + VT core (#95), workspace integration
(#96), commands & UX (#97) and toolchain environment activation (#98, #652).

## Command sessions & run reuse (0350, #574)

- `StartCommandSession(key, argv, dir, …)` / `terminal.NewCommand` spawn a
  **program with arguments directly on the PTY** (no wrapping shell) — the
  run-in-terminal seam for run configurations (Epic #572). The program is
  interactive (stdin is the PTY); `Session.ExitCode()` keeps the exit status,
  and a finished command session renders `[process exited with code N]`
  instead of the bare marker. `IsCommand()`/`Argv()` distinguish it from a
  shell.
- `Model.Occupied()` tracks whether the user ever sent input (a forwarded key
  or a paste; scrollback paging does not count). `Model.StartCommand` replaces
  a model's session in place — the reuse path when a run takes over a
  terminal — resetting scroll, selection and occupancy.
- `Registry.ReusableRunTerminal()` (internal/pane) scans panes and terminal
  tabs in insertion order for a take-over candidate: never typed into, or its
  process already ended (a finished run's terminal is fair game again). The
  debuggee terminal pane (#1370, instance flag `debugTerm`) is excluded.

## Pipe sessions (#1370)

- `NewPipeSession(key, w, h, send)` / `terminal.NewPipe` build a
  **process-less session**: the emulator, spool and feed loop exist as usual
  but no PTY and no child — bytes arrive via `Session.FeedBytes` /
  `Model.FeedText` (which normalizes bare `\n` to `\r\n`). The debug
  integration feeds DAP `output` events through one, so debuggee output gets
  the real pane's reflow, scrollback and search for free.
- `FinishPipe(exitCode, hasCode)` marks the debuggee ended: the pane renders
  the `[process exited with code N]` dead view (the pipe carries an empty
  non-nil argv so it reports like a command session), while the session stays
  open and feedable for trailing output; it only closes with the pane or when
  a new session replaces it. `IsPipe()` distinguishes it.

## Session (`session.go`)

- **PTY lifecycle** via `creack/pty`: the shell (`terminal.shell` config
  override → `$SHELL` → `/bin/sh`) spawns in the project root with
  `TERM=xterm-256color`; pane resizes propagate through `pty.Setsize`
  (SIGWINCH for the child) and the emulator — **debounced** (#804): the first
  resize applies immediately, a rapid burst (divider drag) folds into one
  trailing apply of the final size, so the child redraws once instead of per
  drag step; `Close` kills the child and releases the PTY (bounded — the loop
  joins continue in the background, #1786), and a shell `exit` sends
  `ExitedMsg` so the root model closes the pane.
- **VT emulation** via `charmbracelet/x/vt` (`SafeEmulator` — the read loop
  writes while Update/View read): PTY output feeds `Write`, the screen
  renders with `Render()` (ANSI-styled, so 16/256/truecolor pass through),
  and key presses go through `SendKey`, which encodes per the emulator's
  input modes (application cursor keys etc.); a write loop pumps the
  emulator's host-bound bytes (key encodings, DA/DSR query replies) back
  into the PTY. The emulator drops non-special keys that still carry a
  modifier, so the pane normalizes text-producing presses whose only
  modifiers are shift/caps-lock/num-lock (`toVTKeys` in `model.go`) —
  uppercase letters reach the shell as their produced text (#224).
- **Batching**: output notifications are coalesced per session (`OutputMsg`,
  one per 8ms quiet interval), and the app's input coalescer (#602) folds
  concurrent OutputMsgs **across sessions** into one batch per adaptive flush
  (#803) — so `yes`, a build log, or eight busy TUI panes at once cannot
  flood the render loop or starve input handling.
- **Width reflow** (#935): any width change on the primary screen rewraps the
  whole history — scrollback and screen — at the new width, as if the terminal
  had always been that size (iTerm2/kitty behaviour): shrink rewraps overlong
  logical lines onto continuation rows, grow merges soft-wrapped segments back
  out to the new edge, hard newlines never merge, and wide→narrow→wide
  round-trips reproduce the original layout. Implementation is a **replay**:
  under `mu`+`gridMu` the logical lines are reconstructed (rows joined where
  the soft-wrap heuristic says the renderer broke them, styles preserved via
  `uv.Line.Render`), the emulator is resized, then `2J 3J H` plus the lines
  (`\r\n`-separated, no trailing newline) are fed back through `em.Write` —
  the emulator re-wraps natively, so wrap state, cursor and scrollback come
  out consistent. Rows below the last content row are dropped; the reserve is
  cleared (everything was rewritten). The alt screen never reflows — its apps
  repaint on SIGWINCH. Two guards keep repeated resizes lossless (#953):
  - **Reflow cache**: the logical lines the last replay wrote. The next
    extraction consumes grid rows that still match these lines (rewrapped at
    the current width), so their hard breaks are *known* — the exact-width
    heuristic ambiguity (a hard line exactly filling some intermediate width
    reads as wrapped) cannot merge them. Only content written since the last
    replay takes the heuristic; the cache resets on clear.
  - **Verbatim tail**: the last logical content line — the shell's live edit
    line — is never reflowed; its physical rows replay unchanged (clipped on
    shrink), anchored on the last content row rather than the cursor (a
    resize can catch the shell mid-redraw with the cursor parked elsewhere).
    The shell's own SIGWINCH repaint then finds the row geometry it
    remembers instead of walking up over relaid-out history.
- **Resize content preservation** (#807, #826): **height-only** changes keep
  the reserve machinery — the upstream emulator
  hard-truncates the grid on shrink (clipped cells are destroyed — on a height
  shrink the **bottom** rows, i.e. the newest output and the prompt).
  `Session` layers two mechanisms on top:
  - **Scroll-on-shrink** (#826, real-terminal semantics): when a height shrink
    would clip the cursor line, the top rows scroll into the scrollback and
    the screen slides up, so the cursor-side content survives no matter which
    pane edge was dragged. A later height grow pulls exactly those rows back
    out of the scrollback (round-trip identical) — but only while they are
    still the newest scrollback lines; child output that scrolled meanwhile
    buries them and the pull is abandoned (the rows stay retrievable in the
    scrollback, never duplicated). Skipped on the alt screen (no scrollback;
    TUIs redraw on SIGWINCH).
  - **Resize reserve** (#807): the fullest known content per screen row,
    snapshotted before every applied resize; a grow writes the clipped region
    back, guarded by a per-row prefix match so content the child rewrote is
    never overwritten. The height-restore guard compares **before** the
    grow's own snapshot syncs the overlap (post-snapshot the comparison is
    vacuously true — the #826 fix closed that stale-resurrection hole). This
    path covers width clips and top-anchored height shrinks where nothing
    scrolls.

  Scrollback lines keep their full width upstream — only the render clips —
  so scrollback needs no reserve. `gridMu` serializes the feed loop against
  the whole snapshot/scroll/restore sequence (CellAt returns pointers into
  the live buffer — `LineText`/`HistoryLine` take it for the same reason);
  the grow-side cursor follows the pulled rows via a CUP injected through
  the emulator's input path. Inside the reflow, `softWrappedLocked` runs
  under the **caller's** gridMu (#1786): it used to re-take the lock for its
  reserve match, which self-deadlocked the resize path — with `s.mu` and
  `gridMu` held forever, freezing the whole IDE — whenever the reserve was
  wider than the grid (a width change during an alt-screen phase leaves it
  that way, since only primary-screen width changes reset the reserve).
- **View render cache** (#803): `Session.View` caches the rendered grid keyed
  by a mutation version (bumped on feed writes, resize, clear); a frame
  re-renders only grids that actually changed (measured ~270µs per 200×60
  grid render vs ~13ns cached), so N terminal panes no longer multiply the
  per-frame render cost.
- **Output spooling** (#734, `spool.go`): the PTY read loop no longer writes
  into the emulator directly — it drains the kernel TTY queue into an
  in-process FIFO (`spool`, 16 MiB soft cap) and a separate feed loop replays
  the chunks into the emulator in order. A stalled emulator or render loop
  (app suspend/resume around a macOS lock/sleep window) therefore cannot
  backpressure into the kernel queue, where buffered output can be flushed
  and lost; everything buffers in-process and replays on resume.
  Exception (#989): a `ctrl+c` key press discards the spooled backlog before
  the interrupt is encoded — whatever is still queued is pre-abort output,
  and replaying it makes the process look alive after SIGINT already landed.
  At most the single chunk the feed loop already took still renders; bytes
  arriving afterwards (the `^C` echo, the prompt) flow normally.
- **Teardown sequencing** (#748, #1786): teardown is split into a bounded
  `release` — stop the resize timer, kill the child, close the PTY, close the
  spool; signals only — and a blocking `join`. Upstream vt's `Emulator.Close`
  is not safe concurrently with `Read`/`Write` (plain-bool closed flag), so
  `join` collects the loops in order — read loop (closed PTY errors its
  read), feed loop (spool drains, exit output kept), then the write loop,
  woken by a sentinel byte through the host-bound pipe — and closes the
  emulator last. `join` never runs on the update loop (#1786): `Close`
  performs the release and hands the join to a background goroutine (the
  UI-initiated close paths — tab close, popup collapse, project switch, quit
  — all call `Close` from bubbletea's update loop), and the exit path sends
  `ExitedMsg` right after the release, **before** its join, so a wedged loop
  can neither withhold the exit (leaving a dead tab rendering — and blocking
  on — the stuck session) nor stall the caller.
  `go test -race ./internal/terminal/` is clean.

## Pane citizenship (#96)

`pane.KindTerminal` joins explorer/editor in the instance registry
(`AddTerminal`, keys `terminal`, `terminal:2`, …; `Close` ends the session).
The pane title shows **shell + origin dir** (`TERMINAL — zsh · goproj`; the
dir compacts once it differs from the working directory). The cursor cell
reverse-videos while focused (`model.go` splices it ANSI-aware). While a
terminal (or the explorer) holds focus, the **status line names that pane
kind** — `TERMINAL │ zsh · goproj` (plus `[exited]` for a dead shell) or
`EXPLORER` — instead of mirroring the active editor's mode/file/cursor, so
the line always says where keystrokes go (#381).
`terminal.new` splits the active editor's leaf toward the bottom — the
conventional JetBrains placement.

**Layout persistence**: terminal leaves save with their origin dir
(`paneIdentity{Kind: "terminal", Path: dir}`) and restore as **fresh shells**
in the saved position — no process resurrection, the cwd respawns.

**Project switch (0090)**: live sessions are adopted into the freshly built
workspace (`adoptTerminals` in app/switch.go), split below the new active
editor and titled with their origin root; dead ones close for good. New
terminals root in the new project as always (spawn dir is pinned absolute).
When the target's layout restore already recreated a terminal under the same
key — a fresh placeholder shell for the very session being carried over — the
live session **takes over that pane** (`Registry.AdoptTerminal` closes the
placeholder and swaps in place, #320) instead of splitting a second leaf,
which would both duplicate the terminal and render one instance in two
mirrored panes.

## Popup terminal (#1398)

`internal/app/popupterm.go` adds a **quake-style floating terminal overlay**,
toggled by `terminal.popup` (default `cmd+alt+t`; `terminal.new` moved to
`cmd+alt+shift+t` for it). Design decisions:

- The popup owns a **detached `pane.Instance` tab host**
  (`pane.NewDetachedTerminalHost`) that lives outside every registry and
  outside the split tree: toggling never touches the pane layout, `layout()`
  never sizes it (every size-affecting event calls `applyPopupSize`), and
  layout persistence never records it. Session keys mint in their own
  `popup:term:N` namespace; `terminalModelForSession` and the `ExitedMsg`
  handler check the popup first, so output and exits resolve while hidden.
- **Hide ≠ close**: the toggle only flips rendering/routing off — the sessions
  keep pumping (the same goroutine independence tool-window hide relies on)
  and reopening reveals the same tabs and scrollback. A shell exit closes its
  tab; the last tab drops the instance and hides the popup, and the next
  toggle spawns fresh. App quit ends the popup's sessions tidily — the parked
  workspaces' popups included (#1407). Nothing
  resurrects across restarts; only the resize delta persists (`ui.WinSizes`
  key `popupterm`).
- **Start size** (`popupSize`, #1714): the box defaults to 0.60 × 0.55 of the
  screen, then takes a resize delta resolved through a three-step cascade —
  the project's own `popupterm` delta in `.ike/winsize.json`, else the
  user-scoped **last-resize** delta in `~/.ike/winsize-global.json`
  (`globalWinSizeFile`, `IKE_CONFIG_DIR`-redirectable like every state file),
  else none. So a size dragged in one project carries over to projects that
  were never resized, while a project that has its own delta keeps it.
  Both stores hold *deltas*, never absolute sizes, so the box keeps
  re-clamping against `popupTermMinW`/`popupTermMinH`, the live terminal
  bounds and the `ui.popup_max_width` cap (#932) whichever source it came
  from. Every resize — the chords (#774) and the mouse drag (#933) — routes
  through `popupTermResize`, which first seeds the project store from the
  inherited global delta (so the first resize continues from the size on
  screen instead of jumping back to the default), applies the step, and on
  persist mirrors the project delta into the global store. `WinSizes.Has`
  drives the cascade, so a delta resized back to zero still counts as the
  project's own choice.
- **Per-project** (#1407): the popup belongs to its project like pane
  terminals do (#777). A seamless switch parks it with the workspace
  (`wsExtras` in `Workspace.Aux`) — tabs, scrollback, running processes and
  open state come back unchanged when the project resumes; the new project
  starts with its own empty popup (nothing renders, no keys are swallowed).
  Project-owned floating panels (#1793) park alongside (`wsExtras.floats`);
  global ones never do — see the floating-panels section. Workspace teardown
  (LRU eviction #780, close-from-list #820, `project.close` #1355) ends the
  parked popup's and panels' sessions (`parkedPopupInstances`), and the busy
  guards count popup activity — a running popup process prompts before
  dying unseen.
- It is **not a `ui.Floating`** — the shell's dismiss/filter/scroll priority
  is the inverse of a PTY's raw pass-through (esc must reach vim). Instead it
  renders pane-style chrome (`paneBox` + the regular tab bar) placed via
  `overlay.Place` at its centered-plus-offset rect (#1793), below the
  exclusive overlays: palette/finder draw on top, the settings modal
  suppresses it for its lifetime.
- **Move** (#1793): a left press on the title row outside every tab segment
  starts a titlebar move drag (`floatMoveDrag`); the border ring stays the
  resize drag (#933). The box's position is stored as an **offset from
  center** under the `popupterm:pos` WinSizes key, resolved and persisted
  through exactly the size delta's #1714 cascade — project store, user-scoped
  fallback, mirror on release — and re-clamped in `popupTermRect` so the box
  always stays fully on screen whatever terminal it resumes under.
- **Keys**: while open, the popup's branch in the funnel sits after the modal
  prompts and before the pane-terminal block. Its reserved set mirrors the
  pane one: the toggle chord (resolved via the live binding table) hides from
  inside, `cmd+t` opens a sibling popup tab, `cmd+d` splits the popup
  (#1427), `cmd+shift+i` toggles input broadcast (#1427), `ctrl+tab` and the
  `editor.tab.next/prev` chords cycle the focused side's tabs, `cmd+w` closes
  the active tab through the busy guard (`termClosePopup` targets the shared
  prompt; the guard pins its target by session key, `termCloseSess`, and the
  confirm re-resolves it — a shell that exited while the prompt was open
  closes nothing else, #1786), the float resize chords (#774) adjust the box, cmd+c/cmd+v
  copy/paste, and the `terminalGlobalCommands` allowlist stays with the IDE.
  Everything else goes raw to the shell. `terminal.popup` is itself
  allowlisted, so a focused pane terminal can summon the popup.
- **Split & broadcast** (#1427): the reserved `cmd+d` (the pane-terminal split
  chord, #982) splits the popup into two side-by-side detached tab hosts —
  `popupTerm.split` holds the right side, each side is its own `pane.Instance`
  with its own tabs, and only one split level is supported. The fresh right
  side takes focus; the spatial focus keys (default `ctrl+left/right`, the
  same `focusKeys` map pane terminals use) move the keyboard between sides,
  and a mouse press claims focus for its side. Only the focused side renders
  the focus border. `cmd+shift+i` toggles **input broadcast**: while on, typed
  keys and pastes mirror to both sides' active shells, a `⇉` marker prefixes
  both title rows, and both sides render the focus border (#1592) — the
  shared-input state is visible at a glance. A side's last shell exit collapses the split back
  to a single box (the right side is promoted when the primary empties) and
  resets broadcast. Every whole-popup walk (theme, quit, teardown, busy
  guards) iterates `popupTerm.instances()` instead of touching `inst` alone.
- **Mouse**: a press outside every layer box hides the whole layer (state
  retained), the border ring starts a `popupterm` resize drag (centered
  doubled-delta math), the tab-bar row activates/closes tabs on its side —
  and a segment press arms the tear-out drag (#1793) — body presses anchor
  selections / hit the scrollbar and cmd+click links, the wheel routes like a
  terminal pane — split, the x offset picks the side box first. Selection and
  scrollbar drags run through the generic drag machinery via the `popup`
  sentinel pane key (`termLocal` / `dragTerminal`, resolving the focused
  host — a floating panel's rect when one owns the keyboard).

## Floating terminal panels (#1793)

`internal/app/floatterm.go` lets a terminal tab leave the popup box as its own
**floating panel**: a tab-bar press arms a `dragTab` drag carrying the source
host (`dragState.srcInst`); engaged (#559 threshold) it previews a panel-sized
ghost under the pointer, and the release commits without ever touching the
pane layout:

- **Onto free space** — the tab tears out into a fresh `floatTerm` panel: a
  detached tab host with the popup's chrome, spawned at the source box's size
  with the title row under the pointer, hosting the **same live session**
  (`DetachTerminalTab`, the #708 pattern — no shell restart). A single-tab
  source re-homes its whole host instead (`DetachTerminalTab` refuses the last
  tab) and its slot collapses with the #1427 semantics: split sides promote,
  a fully torn-out box leaves the layer to its panels.
- **Onto another layer box** — popup side or panel — the tab moves there,
  session live: the reverse direction. Re-docking a floating panel into the
  *pane layout* is deliberately out of scope — the layer stays independent of
  the split tree; the way back into a pane is the existing route (move the
  tab into the popup box, or keep using it as a panel).

**Z-order and focus** follow the #1237 stack rules, panel-list flavored
(`floatTerms`, bottom→top): the focused panel — `floatFocus`, falling back to
the topmost when the box is gone — owns the keyboard through the same funnel
branch (`popupFocused` resolves it), a box click reclaims the keys, and only
the keyboard owner renders the focus border. The **popup box is a surface of
the same order**, not a fixed base layer: `popupTerm.boxZ` holds its slot —
the number of panels drawn below it — and `floatTermsSplit` cuts the list at
it for compositing (`render`), hit testing (`popupBoxAt`, top-down) and focus
stepping. Focusing **always raises** (#1806): `setFloatFocus` lifts the panel
to the top of `floatTerms` (`raiseFloatTerm`, moving `boxZ` down with it) and
`setPopupFocus` lifts the box above every panel (`raisePopupBox`), so every
route into focus — click, focus chord, tear-out, tab drop — keeps the #1237
invariant that the topmost surface owns the keyboard, and neither a panel nor
the box can sit covered while it holds the keys.
The **focus keys** (default `ctrl+left`/`ctrl+right`, #228 overrides apply)
step the keyboard through the layer's surfaces in stack order — panels below
the box, the box's split sides at its slot, panels above it, wrapping around
(`popupSurfaces`/`stepPopupFocus`); with a single surface they stay with the
shell. Since the surface stepped onto rises, repeated steps alternate between
the two frontmost surfaces, alt-tab style. The toggle chord and the
outside-press dismiss act on the **whole layer** — box and panels show and
hide as one unit, sessions always retained; panels have no per-panel hidden
state. Panel chrome repeats the popup's: tab bar, `cmd+t` sibling tabs,
`cmd+w` through the busy guard, tab cycling, cmd+c/v, scrollback search —
everything `popupFocused` routes; the resize chords (#774) and the border
drag (#933, corner-anchored 1:1 math) size the focused panel. Box-only
affairs (`cmd+d` split, `cmd+shift+i` broadcast) no-op while a panel holds
the keys — the focus keys are the exception: they cross the whole layer
(#1806).

**Global toggle** (#1793): every panel's title row leads with the ●/○ button.
`○` (project-owned, the default) keeps #1407 semantics: the panel parks in
`wsExtras.floats` on a switch, counts in the busy guards, and dies with the
workspace teardown. A click flips to `●` **global**: the panel now belongs to
the app — a project switch carries it into the fresh model (the #1514
notification-history pattern) with its process, scrollback and CWD untouched,
stacked above whatever layer the incoming project restores (a visible layer
stays visible), and no workspace teardown/eviction can reap it, because it
never enters `Aux`. It ends only with an explicit close or app quit (the quit
path walks `popupLayerInstances`). Toggling back to `○` binds the running
session to the **current** project — it parks with this workspace from then
on. Session keys mint from a package-level counter so a carried global
session can never collide with a fresh model's popup keys. Panel geometry is
runtime state: sessions never survive an app restart (#1398 rule), so
positions/sizes of panels aren't persisted — only the popup box's offset and
delta are (`popupterm`/`popupterm:pos`).

## Key routing — the reserved set

While a live terminal is focused, **every key goes raw to the PTY** — vim,
htop and less must see tab, ctrl+c, esc and the F-keys. The documented
reserved set (`terminalReservedKey` in internal/app) is exactly:

| Key | Effect |
|---|---|
| `ctrl+tab` | move focus to the next pane (delivery is terminal-dependent — many terminals cannot send it; 0081's reality probe owns the call) |
| `alt+f12` | `terminal.toggle` — return focus to the previous pane (the reliable hatch) |
| `cmd+t` | new terminal tab (#729/#983, iTerm-style): a terminal tab hosted by an editor pane gets a sibling tab in the same pane (#573); a dedicated single-session terminal pane **converts into a tab host in place** (the same conversion a tab drop performs, #836) — its live shell becomes the first tab, the fresh one the second, with the regular tab bar on top. Outside terminals `cmd+t` has no global binding anymore (its former `vcs.updateProject` was removed in #750) |
| `cmd+d` | split right (#982, iTerm-style): a fresh terminal pane opens to the right of the focused terminal's pane and takes focus — the same for dedicated terminal panes and editor-hosted terminal tabs. Outside terminals `cmd+d` keeps its global binding (`editor.duplicateLine`) |
| `cmd+w` | close the terminal (#986): an idle shell gets an EOF (ctrl+d) — it exits and the regular exit path closes the pane/tab; a **busy** terminal (foreground process group ≠ shell, or a still-running command session — `Session.Busy`) raises a centered guard first: enter closes, esc cancels. `ctrl+w` stays with the shell (delete word); outside terminals `cmd+w` keeps its global binding (`editor.closeTab`) |
| `cmd+f` | open the scrollback search (#1504) — the muscle-memory entry point to the same inline search `/` starts from scrollback (#1169), working from the live view too (`Model.StartSearch`; esc then returns to the live view). Under an alt-screen or mouse-reporting child the chord stays with the child (vim/lazygit own their find); outside terminals `cmd+f` keeps its global binding (`editor.find`). The popup terminal reserves it too, on the focused split side |
| `ctrl+arrows` | spatial focus moves out of the terminal (#228) — the same `keymap.bindings.focus_*` overrides apply; a disabled direction stays with the shell. Inside the popup layer left/right instead step through its surfaces — split sides and floating panels — raising the one they land on (#1806) |
| `cmd+c` | copy an active mouse selection (#227) — without one the key stays with the shell |
| `cmd+v` | paste the system clipboard through the bracketed-paste path (#727) — under the Kitty protocol the host delivers cmd+v as a key, so the app performs the paste itself; the debuggee terminal pane (#1370) is an ordinary terminal pane and needs no special casing |
| global IDE chords | the chords bound to the `terminalGlobalCommands` allowlist dispatch in the IDE instead of the shell (#805, widened in #973): `palette.searchEverywhere` (`cmd+shift+a`), `palette.recentFiles` (`cmd+e`), `project.switch`, `settings.open` (`cmd+,`), `project.goToFile`/`goToClass`, `project.findInPath`/`replaceInPath`, `explorer.toggle` (`cmd+1`), `window.hideAllTools`, `nav.pins` (`cmd+2`) and `nav.pinGoto1..4`, `todo.list` (`cmd+6`), `vcs.panel` (`cmd+9`), `notifications.history`, `editor.tab.next`/`tab.prev` (`ctrl+cmd+right/left`, #997 — switches the focused tab host's tabs; the `ctrl+alt+arrow` secondaries deliberately stay with the shell, `terminalShellChords`, since alt-arrows are common readline navigation), plus a configured `palette.toggle_key` — resolved via the live binding table, so rebinds move along. Single-step chords, and the **double-shift tap** (#973): two bare shift presses within 600ms open Search Everywhere — a bare modifier means nothing to the shell, unlike esc-esc, which deliberately stays with it (vim/lazygit would see side effects) |

`shift+pgup` / `shift+pgdn` page the **scrollback** inside the pane (half a
grid per step, position marker on the bottom line, any typed key snaps back
to live — except `/`, which opens the scrollback search, #1169 below). A dead session (shell exited) falls back to normal key handling so
`ctrl+w` can close the pane.

## Scrollback bound (#1545)

`terminal.scrollback_lines` (default 10000, clamped to ≥100) bounds each
session's scrollback buffer — the dominant per-pane memory cost (a wide styled
line costs tens of KB upstream), multiplied by parked background workspaces
whose sessions keep ingesting PTY output while invisible. The value lives as a
process-wide default in the terminal package
(`terminal.SetDefaultScrollbackLines`, set at startup and on every config
reload) so **every** creation path — registry panes, terminal tabs, popup
terminal, run sessions, debug pipe sessions — applies it via
`vt.SetScrollbackSize` right after the emulator is built, without threading
config through each call site. A live config reload additionally re-bounds the
active workspace's running sessions (`Instance.configure` →
`Model.SetScrollbackLines`): lowering trims history forward; raising cannot
resurrect already-trimmed lines.

## Scrollback scrollbar (#1368)

`scrollbar.go` overlays the shared track/thumb bar (`internal/scrollbar`,
#1367) on the pane's rightmost column, mapping the virtual buffer
`[scrollback ++ screen]` so the thumb shows where the view sits in the
history. It renders while scrolled back, and on the live view once scrollback
exists — but never over a mouse-reporting or alt-screen child at the live
view: their UI owns the grid, and since `ScrollbarHit` is false there, every
click keeps passing through to the child untouched. A thumb press starts a
`dragTermScroll` drag whose motion feeds `ScrollbarDrag`; a track press jumps
proportionally into the scrollback. The wheel keeps its existing routing
(#226/#669). The overlay wraps the composed view last (`View` →
`overlayScrollbar(baseView())`), so scrolled, live, search and dead views all
carry the bar. Pipe sessions (#1370) track DEC mouse modes like PTY sessions
so the gating applies to them too.

**Reserved gutter column** (#1500): the child grid — PTY, command and pipe
sessions alike — is sized one column narrower than the pane (`Model.gridW`,
pane width minus one; a one-column pane keeps its width). The bar renders in
that reserved rightmost column, so it never covers content, and its
appearing/disappearing never resizes the child (no reflow churn, no
`SIGWINCH` on every alt-screen switch). Panes without a visible bar simply
leave the gutter blank.

## Theme ANSI palette (#1363)

The emulator stores a cell's colour exactly as the program set it, so a shell
painting with SGR 30–37 / 90–97 (or a 256-colour index below 16) used to render
in the **outer** terminal's palette while its background came from IKE's own
frame wash — foreground and background regularly landed too close to read.

`ansipalette.go` closes that gap: every rendered line passes through
`gridPalette.remap`, which rewrites the indexed colour parameters of each SGR
sequence into the theme's truecolor values before the line reaches the screen.

- **Scope**: SGR 30–37, 40–47, 90–97, 100–107, the `38;5;n` / `48;5;n` forms
  with `n` below 16, and the defaults `39` / `49` (to the theme's terminal
  foreground/background). Indexes 16–255 are the fixed xterm cube and pass
  through, as do truecolor runs, underline colours (58), sub-parameter forms
  (`4:3`), non-SGR CSI sequences and OSC hyperlinks.
- **Where**: `Session.View` (inside the version-keyed render cache, #803, next
  to the link decoration) and `Session.HistoryLine`, so the live screen and the
  scrollback are remapped identically.
- **Threading**: `Model.SetPalette` forwards to `Session.SetPalette`, which
  pre-renders the 16 colours as `"r;g;b"` parameter strings (no per-cell colour
  formatting) and drops the render cache — a live theme switch repaints the
  terminal in the new colours. A session without a palette (tests, a model
  before the registry threads one in) renders exactly as the emulator wrote it.

The colours themselves — including the derived fallback for themes that ship
none and the contrast floor every entry clears — belong to
[Themes](./themes.md), together with the `[theme.terminal]` overrides.

## File:line links (#1168)

Terminal output referencing `path/file.ext:12[:col]` — compiler errors, test
failures, grep output — is clickable (`links.go`):

- **Detection** is a pragmatic regex over the rendered plain text: relative
  (`file.go:12`, `./pkg/x.go:3:14`) and absolute paths, line plus optional
  column; the last path component must carry a letter-led extension, which
  keeps clock times (`12:30`) and `host:port` pairs out (extensionless files
  like `Makefile:3` are deliberately not detected).
- **cmd+click** (`ModSuper`/`ModMeta`, mirroring the editor's cmd+click
  go-to-definition) resolves the reference under the pointer: relative paths
  against the session's live cwd (OSC 7, falling back to the spawn dir), then
  a cheap `os.Stat` **at click time only** gates on an existing regular file
  before `Model.LinkAt` hands the target to the app's `openPathAt` funnel
  (nav history records). A plain click stays a selection press, untouched; a
  cmd+click without a link under it is inert so it can never steal a
  selection anchor. `LinkAt` scans the whole soft-wrap-joined logical line,
  so wrapped references still resolve.
- **Affordance**: an always-on subtle underline (SGR 4/24, no color reset) on
  reference spans. The live screen decorates inside the version-keyed render
  cache (#803) — one scan per grid change, the cached fast path returns the
  already-decorated string; scrollback rows decorate as `scrolledView`
  windows them in, so links work throughout the history.

## Scrollback search (#1169)

`/` while the pane is **scrolled into scrollback** opens a one-line search
field on the pane's bottom row (`search.go`), the explorer speed-search
pattern (#1087): case-insensitive contains over the plain line text, no
regex. Typing jumps incrementally to the nearest match **at or above** the
anchored view — history search goes backward — wrapping to the newest match
when nothing older matches; `ctrl+p`/up step older, `ctrl+n`/down newer,
both with wrap (plain `n`/`N` would collide with typing the query). Matches
on the visible rows highlight reverse-video and the field carries a `3/17`
counter (`no matches` in the Error colour on a miss). enter accepts and
keeps the position, esc restores the offset the search opened on; every
other key is consumed while the field is open — nothing leaks into the
shell mid-query.

Capture is deliberately narrow: at the **live view** `/` is everyday shell
input (`ls /tmp`) and always passes through — enter scrollback first
(`shift+pgup` or the wheel), then search. Alt-screen or mouse-reporting
children (vim, lazygit) own their own `/` and are never captured
(`searchCaptures` checks the same #96/#226 routing state the wheel uses).
`cmd+f` (#1504) is the second entry point, reserved app-side
(`terminalReservedKey` / `popupReservedKey`): an explicit chord carries
intent, so `Model.StartSearch` opens the same field from the live view too —
esc then restores `prevScroll` (0, the live view). The alt-screen/mouse
guard applies identically; there `StartSearch` reports false and the chord
stays with the child.
`terminal.clear` and a command-session restart drop an open search along
with the history it indexed.

## Command completion popup (#740)

JetBrains-style completion at the shell prompt (`complete.go`). The command
line is read straight off the emulator — the cursor row's soft-wrap chain is
walked back to its first row and joined (#1431), then cut left of the cursor,
so a command longer than the pane width keeps its head and current word after
wrapping instead of the continuation row's tail opening the popup on garbage;
the prompt (only ever on the chain's first row) is stripped heuristically
(`$ `, `% `, `> `, `# `,
`❯ `), command separators (`|`, `;`, `&&`, `||`) start a fresh command — so
the shell keeps owning line editing and history. Sources per word: PATH
executables while the first word is typed, make targets after `make`
(Makefile/makefile/GNUmakefile in the live cwd), files/dirs
relative to the live cwd otherwise (dir part in the word honoured, `~/` and
absolute paths too, dotfiles only on a `.` prefix, dirs keep a trailing `/`).
Candidates match the typed
word **case-insensitively** (#968, like every other typed search in the UI);
an exact-prefix candidate strictly extends the typed word, so **accepting
types just the remainder**, while a candidate matching only
case-insensitively erases the typed word with backspaces and types the
canonical spelling (`mak` → `Makefile`). The accepted text goes in as
**plain key presses, not a bracketed paste** (#1442): zsh standout-highlights
a pasted region by default, which made the completed text sit on the command
line with a background.
Accepting always **ends** the interaction, directories included (#1335) — the
popup closes and the pending refresh is cleared, so the echo of the typed
remainder cannot reopen it and the next enter submits the command line
(`cd an` → tab → enter runs `cd ansible/`, it does not insert
`ansible/ansible.cfg`). Typing on, or `ctrl+space`, completes inside the
accepted directory. `ctrl+space` opens the popup on demand
(empty word shows everything); **auto-suggest** re-arms on every printable
key and recomputes on the next `OutputMsg` — the shell must echo the
keystroke before the cursor row reads current — and is togglable via
`terminal.autosuggest` (default on, applies live). up/down move, esc
dismisses, any other key invalidates and passes through raw.
Auto-suggest **stays silent while the command soft-wraps** (#1464) — an
uninvited popup mid-wrap is noise, and if the wrap heuristic ever misses the
chain the tail would complete as garbage; `ctrl+space` still completes with
the chain joined. As a safety net, auto-suggest also stays quiet when the
text left of the cursor carries **no recognizable prompt marker** although
the row above holds content — then the cursor row may be a continuation row
whose chain start could not be identified, and the "word" a wrapped tail.
**Focus rule (#1432):** an auto-suggest popup opens *unfocused* — it was
never asked for, so **enter passes through to the shell** and runs the typed
line (the popup closes, nothing is inserted). An unfocused popup highlights
**no row** (#1442) — a reversed first entry would falsely promise that enter
accepts it; the selection highlight appears once the popup is focused.
up/down focus the popup; once
focused, enter accepts the selection, and an auto refresh from typing on
keeps the focus. `ctrl+space` opens the popup focused, so enter accepts
right away. **Tab accepts in both states**, esc closes in both states. The popup is
inactive on the alternate screen (vim/htop), in command sessions, while
paging scrollback, and **whenever the shell is not at its prompt** (#1340);
it renders as a bordered list composited over the grid at
the word's start column, below the cursor row when it fits, above otherwise;
an anchor near the right pane edge shifts the box left so it stays one intact
box inside the pane (#1463) — `overlay.Place` also hard-clips composited rows
to the canvas width, so an overhanging box can never wrap and tear apart.

**Prompt gating (#1340).** Completion is a shell feature, so it only runs
while the shell itself is the foreground job — `Session.AtPrompt()` compares
the PTY's foreground process group (`TIOCGPGRP`) with the shell's pid, the
same signal `Busy()` (#986) uses, so it needs no shell prompt integration and
works with any shell. While a program owns stdin
(`python3 -c 'input("…")'`, a REPL, an interactive installer) its own prompt
is not a shell command line: typing never opens the popup, `ctrl+space` does
nothing, an already-open popup is dropped on the next key or `OutputMsg`, and
the popup-bound keys (tab/enter/up/down/esc) stay unconsumed so the raw route
delivers them to the program. Completion returns the moment the shell prompt
does. If the ioctl is unavailable the shell counts as at its prompt, so the
gate can only ever fail open.

**Live cwd (OSC 7, #770).** Shells with prompt integration emit
`OSC 7 ; file://host/path` on every prompt; the emulator's
`WorkingDirectory` callback stores it on the session (`Session.Cwd()`,
percent-decoded, bare absolute paths accepted too). Completion candidates,
the pane title and the status-line segment all read `Cwd()`, so they follow
a `cd`. Without any OSC 7 report — a shell without prompt integration —
`Cwd()` asks the kernel for the child process's actual working directory
(#1383): `proc_pidinfo(PROC_PIDVNODEPATHINFO)` as a raw cgo-free syscall on
darwin (`cwd_darwin.go`), `readlink /proc/<pid>/cwd` on linux
(`cwd_linux.go`). The kernel answer is symlink-resolved (macOS `/tmp` →
`/private/tmp`). Precedence: OSC 7 report > kernel query > start directory
(other platforms, child gone). `Dir()` itself stays the origin root, used
for respawn and layout persistence.

**Mouse selection & copy** (#227, `MousePress`/`MouseDrag`/`MouseRelease` in
`model.go`): a left drag over the grid selects text — highlighted in reverse
video, anchored in virtual coordinates (indices into [scrollback ++ screen])
so it survives scrollback paging and can span history and live rows. The
selection is linear (stream-style): start line from the anchor column, full
middle lines, end line up to the head column; `cmd+c` copies it right-trimmed
to the system clipboard and drops the highlight. Soft-wrapped rows join
without a newline on copy (#936): only hard newlines put `\n` in the
clipboard, so a long command that merely wrapped pastes back as one line. The
emulator keeps no per-row wrap metadata, so the copy path uses the classic
heuristic (`Session.SoftWrapped`): a row whose final column is occupied
continued into the next one — the sole ambiguity is a hard-newline line that
exactly fills the width, which joins too. Width changes reflow the whole
history (#935), so resizes no longer clip lines; the #947 guards remain for
content predating a reflow (an alt-screen phase, a legacy session) — lines
stored wider than the viewport, or screen rows still prefix-matching a wider
resize reserve, read as clips, never wraps (better a missed join than
chaining unrelated lines). Any key routed to the shell (and
`terminal.clear`) clears the selection. When the child enabled mouse
reporting, press/drag/release forward to it instead — selection is
unavailable then, like in xterm.

**Multi-click selection** (#936): a second press on the same cell within
500 ms selects the word under the pointer, a third the whole logical line
(across its soft-wrapped rows), then the cycle restarts. Word boundaries are
shell-friendly: alphanumerics plus `/.-_~+@$%=:`, so `/usr/local/bin`,
`--flag=value` or `user@host:path` select whole, and a word spanning the wrap
break stays one word. `cmd+c` copies multi-click selections through the same
path as drags. Keeping the button down after the second/third click and
dragging extends unit-wise (#951): word by word after a double click, whole
logical lines after a triple click — in both directions, with the originally
clicked word/line always fully selected; a plain click keeps character-wise
dragging.

**Mouse wheel** (#226, `MouseWheel` in `model.go`): the wheel goes to whoever
asked for it — a child that enabled a DEC mouse-reporting mode
(?9/?1000–?1003, tracked via the emulator's `EnableMode`/`DisableMode`
callbacks) gets the encoded event through `SendMouse`; an alt-screen child
without mouse reporting gets arrow keys, three per notch (the xterm
"alternate scroll" convention — this is how `less`/`man` scroll); a plain
shell pages the pane's scrollback.

A coalesced wheel burst (#238) arrives as **one call carrying the whole line
delta** (#669) — `flushWheel` no longer replays the batch event-by-event.
What may be forwarded to the child is bounded by `wheelChildBudget`
(~one screenful of arrow keys, wheel events per notch derived from it), so a
fast trackpad burst can no longer flood the PTY and leave the child scrolling
for seconds after the user stopped; the pane's own scrollback path applies
the full distance (cheap, clamped to history).

**macOS editing chords** (#225, #240, `motionKey` in `model.go`): the pane
translates the iTerm "natural text editing" motions to the readline/ZLE
emacs-mode defaults — `option+left`/`right` → `ESC b`/`ESC f` (word jump),
`cmd+left`/`right` → `ctrl+a`/`ctrl+e` (line start/end),
`option+backspace` → `ESC DEL` (kill previous word), `option+forward-delete`
→ `ESC d` (kill next word, #733), `cmd+backspace` →
`ctrl+u` (kill to line start). Shift-augmented variants behave the same (a
PTY has no selection). Cmd delivery is terminal-dependent (the 0081
reality-probe caveat).

## Commands (#97)

- **`terminal.toggle`** (default `alt+f12`, fragile like every alt+F-key):
  the JetBrains state machine — no terminal → open one split off the active
  editor at the adaptive placement (`auxZone`, #1588 — below, or right of a
  wide landscape host); one exists unfocused → focus it (remembering where
  focus was);
  focused → return focus to the remembered pane (falling back to the active
  editor, then the explorer). Inside a focused terminal the reserved-set
  handler catches `alt+f12` before the raw pass-through. Custom tool panes
  (#741) never count as "the terminal" here (#772): with only tool panes
  open, toggle spawns a new regular terminal instead of focusing a tool —
  the same rule keeps `terminal.clear` off tool panes.
- **`terminal.new`** opens an additional session; **`terminal.clear`** wipes
  screen and scrollback via the canonical `CSI 2J` + `CSI 3J` pair (2J alone
  pushes the visible lines *into* the scrollback — the xterm behaviour) and
  asks the shell to repaint its prompt with the ctrl+l convention.
- The Tools menu carries "Terminal" (toggle) and "New Terminal"; all three
  commands are palette-reachable.
- **Titles**: the shell's OSC 0/2 reports (the running command) append to
  the pane title — `TERMINAL — zsh · goproj · npm run build`. Inside OSC
  strings the raw byte `0x9C` (8-bit C1 ST) is kept as payload
  (`internal/terminal/oscpatch.go`, #561): many UTF-8 runes carry it as a
  continuation byte (the U+2700 dingbats, e.g. Claude Code's `✳` spinner
  titles), and dispatching on it would split the rune and print the rest of
  the title into the grid as ghost text. Only BEL and `ESC \` terminate,
  matching xterm/Ghostty.

## Toolchain environment activation (#98, #652)

The **effective** interpreter per language — the explicit settings-page
choice beating project detection, through the same `lang.Interpreter` seam
LSP, debug and the statusline read — is activated in fresh IDE terminals the
way JetBrains does it, so `which python3` shows the real interpreter
(`internal/terminal/env.go`, `PlanActivation`). Per mapping, one of four
modes applies:

- **venv** (the interpreter's bin parent carries `pyvenv.cfg`): activate like
  `source bin/activate` — `<venv>/bin` is prepended to PATH and
  `VIRTUAL_ENV` is set. No shim; `which python3`/`python`/`pip` all print
  the venv paths. A detected project `.venv` activates too — the old
  "silent detection never injects" rule is gone.
- **PATH prepend** (private toolchain dir — pyenv versions, mise/asdf
  installs, `/usr/local/go/bin`, anything outside the shared-system list):
  the interpreter's own directory goes ahead of PATH, so real versioned
  paths win `which`. A *detected* interpreter whose directory already wins
  the base-PATH lookup for its own name is skipped (env untouched — it is
  what PATH gives anyway).
- **shim** (explicit choice in a shared system dir: `/bin`, `/usr/bin`,
  `/usr/local/bin`, `/opt/homebrew/bin`, sbin variants): prepending would
  reorder the whole PATH and shadow unrelated tools, so the per-project
  **shim directory** (`.ike/shims`, IKE_CONFIG_DIR-overridable) keeps
  `#!/bin/sh` exec wrappers for just that language's command names (python
  covers `python` + `python3`). Stale shims sweep when the setting is
  removed or the mapping moves to venv/prepend mode.
- **none** (detected interpreter in a shared system dir): ambient — the
  environment stays untouched.

With nothing to inject the spawn environment is exactly the inherited one.
The overlay applies to **new** terminals; running sessions keep their
environment (a PATH prepend cannot retarget a live shell — JetBrains behaves
the same). A config reload re-plans, so the next terminal picks up changes.

- The pane **title indicates the active mappings**:
  `… · python→~/proj/.venv/bin/python` (only mappings that actually inject).
- **Windows**: the shims are POSIX `sh` scripts; a windows port writes
  `<name>.cmd` wrappers into the same directory (`@"%target%" %*`) —
  documented here, darwin/linux land first like the rest of the PTY stack.

## Quality bar

Verified inside the pane: `vim` (alt screen, insert/normal, `:wq` writes),
`less` (paging), shell line editing and wrapping, colored output, `stty size`
reflecting pane resizes, scrollback paging over `seq` output, layout restore
with a fresh prompt, a session surviving a project switch with its
origin-root title, and `command -v python3` in a project with an active venv
mapping resolving to the venv interpreter itself (`VIRTUAL_ENV` set).
