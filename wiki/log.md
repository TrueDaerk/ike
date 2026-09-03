# Log

## 2026-09-03 (Dependencies tool window, #2419)

- **A singleton Dependencies pane** (`deps.toggle`, `cmd+0`, bottom zone)
  lists the project's declared dependencies per manifest — name, current,
  latest, direct/indirect, vulnerability count — grouped by manifest file.
  One provider per ecosystem (`internal/deps`) shells out to the toolchain:
  `go list -m -u`/govulncheck, npm/pnpm/yarn (package manager by lockfile),
  composer, cargo (with cargo-outdated/cargo-audit when installed), and
  pip/uv with pip-audit. Missing tools are reported with install hints in
  the centered dialog, never as a crash; all network traffic stays inside
  the toolchain.
- **Background scans with an mtime cache.** Scans run in a `tea.Cmd` with a
  `⟳ deps: scanning` status-line segment; results cache per manifest mtime
  and `deps.refresh`/`deps.audit` force past it. `deps.auto_scan` (Settings
  ▸ Dependencies, default on) scans on project open when manifests exist.
  Manifests count only while a registered language declares them
  (`lang.Language.DepManifests`), so disabling a language plugin silences
  its ecosystem.
- **In the pane**: `enter` opens the manifest at the dependency's line, `u`
  rewrites the version (constraint prefixes preserved) and offers the
  install step behind a confirmation — never unasked — `v` shows the
  advisory details, `r` rescans, `/`+find chord filter (`manifest:`,
  `state:`).
- **In the editor**: manifest hovers show `current → latest` plus
  advisories, alt+enter offers "Update … to latest" (`deps.updateLatest`),
  and vulnerable entries feed the Problems store as warnings under source
  `deps`.

## 2026-09-03 (telemetry v4: 60s heartbeat, command outcomes, dismissals, project time, #2408)

- **The heartbeat beats once a minute, not every ten seconds.** At the old
  cadence the liveness stamps were 61% of a two-day export — the diagnostic
  was burying the usage data it was meant to accompany. A minute still
  brackets a freeze closely enough to tell "the loop is stuck" from "the
  process ended", and the per-interval `top` breakdown (#2402) names an idle
  session's loudest wake sources just as well from a coarser beat.
- **`command` events now say whether the dispatch worked and how long it
  took** — but only when there is something to say: `ok` and `ms` appear when
  the command id resolves to nothing (the dispatch funnel's one failure mode,
  previously a silent no-op) or when the synchronous dispatch took at least
  50 ms. A fast success keeps the old two-field shape, so the fields read as
  markers rather than noise.
- **Every palette mode reports its dismissals**, as the new `palette.dismiss`
  type carrying the mode prefix, the typed query's *length* and how long the
  box stood open. #2399 recorded this for the recent-files dialog only, as a
  pseudo-command; a dismissal is the single palette outcome that leaves no
  other trace, and the re-open streaks it exists to make visible are not a
  recent-files-only phenomenon.
- **`project.leave` gives per-project time a real number** instead of an
  inference from session markers: emitted on switch, close and quit with the
  hashed project token and the **foreground** milliseconds spent there. The
  clock pauses while the terminal reports itself blurred, so a project parked
  in a background tab overnight does not read as a night of work.
- **Schema `v` is 4**, and the wiki's event catalogue now carries a version
  table an analysis script can branch on: v1 files hide internal dispatches
  under `command`, pre-v4 files have no outcome fields and six times the
  heartbeats per hour.
- The privacy line is unchanged: query *lengths* and prefix runes, project
  *hashes*, never text or paths.

## 2026-09-03 (pane numbers in the chrome, focus by number, #2407)

- **Every visible pane wears its number.** The pane title bars carry a lazygit
  style `[1] EDITOR` badge — focus colour on the focused pane, dim on the
  rest — numbered in reading order over the *computed rectangles*, so nested
  splits are numbered the way they look. The order is derived from `m.lay` on
  every read: a split, move, close, restore or zoom renumbers on the next
  frame with nothing to invalidate. Tool windows count while they are on
  screen; the popup terminal and the floating panels are not layout leaves and
  never take a number.
- **`ctrl+1`…`ctrl+9` focus that pane** (`pane.focus1`…`pane.focus9`), with an
  out-of-range number answered by a notification rather than by silence. The
  chords are delivered plain ctrl chords and stay clear of JetBrains'
  `cmd+digit` tool-window numbering. They are macOS-only rows: off macOS the
  Cmd→Ctrl fold puts `explorer.toggle`, `nav.pins` and `vcs.panel` on exactly
  those chords, so there the new `pane.focusByIndex` ("Focus Pane by Number…")
  prompt is the doorway — the reason is recorded in the keybind audit ledger.
- **`layout.pane_numbers` = `on` / `off` / `focus-only`** (Settings →
  Appearance). `focus-only` shows the badges only while the which-pane hint is
  up: a keyboard pane switch raises it and schedules a generation-tagged
  expiry, so a faster second switch outruns the older timer and the numbers
  are on screen exactly while panes are being switched.

## 2026-09-03 (run to cursor, PHP inline values, "Add Condition…", #2405)

- **`debug.runToCursor` (alt+F9) resumes towards the cursor line** instead of
  making the user press F8 eight times to get there — the streaks the stepping
  telemetry recorded. `debug.runToLine` is the palette flavour, asking for a
  line number. Both install one temporary breakpoint that lives on the session
  state, is merged into the file's `setBreakpoints` list on the wire and is
  retired on the next stop; the persisted store never sees it, so a user
  breakpoint on the same line survives the cleanup untouched (and needs no
  temporary one at all). The push and the resume share one goroutine, in that
  order, so `continue` can never overtake its own breakpoint.
- **Only alt+F9.** The obvious darwin partner, cmd+F9, folds onto ctrl+F9 off
  macOS, where the editor already runs the HTTP request under the cursor — the
  shadow the keymap policy test rejects. The Run menu and the palette are the
  delivered escape, recorded in the reachability ledger.
- **PHP sessions show inline values again.** Locals were filtered to plain
  identifiers, which dropped every xdebug local — they arrive as `$name` — so
  an Xdebug session had no inline values at all. One leading sigil is now part
  of the name and of the whole-word match (`$s` matches `$s`, never `$sum`).
- **The frame's line and the two above it are a focus window.** A hint that
  does not fit is still dropped elsewhere (code is never truncated for a
  value), but inside the window it shrinks with an ellipsis instead of
  vanishing: on the line the debugger stopped at, the value is what the step
  was for.
- **alt+enter offers "Add Condition…" on a breakpoint line** ("Edit
  Condition…" when one exists), so `debug.breakpointProperties` is discovered
  where it applies rather than through a chord nobody presses.

## 2026-09-03 (recent files ranks by frecency and reopens on the last pick, #2399)

- **Both lists of the `cmd+e` dialog rank by frecency** — frequency blended
  with a recency decay, the `internal/frecency` store the `@` finder already
  uses (#2155). The file list reads that same file-open history, so the two
  windows agree on what the project works on; the projects column is ranked by
  its own **user-scoped** store (`~/.ike/projfrecency.json`), because a
  per-project copy would forget every switch the moment it was made.
- **Historyless entries keep plain MRU order** — they score 0 and the blend
  leaves the input order intact. That is the pure-recency fallback, and it is
  the whole listing on a fresh project.
- **`palette.recent.ranking` (`frecency`|`recency`)** turns the blend off, in
  the config, the Settings form and the generated settings reference. The gate
  is consulted per listing, so a flip applies to the very next open.
- **The dialog reopens on the row it was last used to activate**, via two new
  generic Mode extensions: `PreselectMode` (a mode names the `Item.Key` to
  start on) and `PickRecorder` (the palette reports the activated row). The
  memory lives in `.ike/recentpick.json`, its own file rather than
  `session.json`, because a pick is recorded from inside the palette — which
  has no model to snapshot the session from — and a memory a kill can lose is
  worthless. A remembered row that is no longer listed leaves the default
  first-row selection.
- **`p:` filters the projects column alone.** The prefix empties the file list,
  so the existing automatic focus placement (#819) hands the keyboard to the
  projects column. `tab` stays the way *to* the column; `p:` is the way to
  filter it. A third extension, `HintMode`, documents both in one dim line
  under the box, styled and clipped by the palette.
- **Dismissals are now visible in telemetry.** `esc` leaves a
  `Dismissal{Prefix, QueryLen}` record the root model pulls with
  `TakeDismissal` in the same Update pass (a pulled record, not a dispatched
  msg, so `diff.files`' dismissal-sensitive two-step pick is not raced); a
  recent-files one lands as the `command`
  event `palette.recentFiles.dismiss` carrying the query's *length* (structure
  only, never the text) through the new `Recorder.CommandDetail`. Picks keep
  going through the ordinary command funnel, so their event count is unchanged,
  and no other palette mode reports its dismissals.

## 2026-09-03 (HTTP response pane shows the request line, #2424)

- **The request that actually went out**: the response pane header now shows
  a line under the status — method and final URL, after placeholder
  substitution — for the live response and for every browsed history entry,
  read off the same `RequestSnapshot` the resend/curl/httpie exports already
  use (`Model.requestLine`). Editing a request and re-running it, or
  stepping back through history, used to leave the path and query invisible;
  the status line alone never said which one was actually hit.
- **Middle truncation, not tail**: a URL longer than the pane elides its
  middle (`middleTruncateURL`) so the host and the tail of the query both
  stay legible — the two places an environment difference usually shows up —
  instead of the tail disappearing behind a trailing `…`. `U` copies the
  full, untruncated URL regardless of what is on screen.
- **`i` expands the line** into the as-sent request headers and, under 2 KiB,
  the body — collapsed by default, capped at 20 lines, and belonging to the
  view rather than to one entry, so it survives history browsing like the
  raw/pretty toggle (#2157) does. A legacy entry with no snapshot shows no
  request line, same gate as `CanResend`.

## 2026-09-03 (one chord that opens the right playground, #2415)

- **`playground.open`** ("Open Playground for This File", `cmd+shift+j`,
  Editor context) resolves the playground from the focused buffer's language:
  `json`/`jsonc`/`ndjson` → jq, `yaml`/`ansible` → yq, `xml`/`html` → xmq,
  anything else → the notification `no playground for <lang>`. It only routes;
  every branch ends in the existing `startPlayground`.
- **The xmq route is wired ahead of its playground** through the
  `startXMQPlayground` hook (`internal/app/playgroundopen.go`): nil means "not
  available yet" and is answered as such instead of opening the wrong dialect;
  a test installs a stub to cover the route today.
- **The per-dialect commands are untouched**: `json.jqPlayground`
  (`ctrl+alt+j`) and `yaml.yqPlayground` (`ctrl+alt+y`) keep their chords, stay
  separately rebindable and keep counting separately in the palette's frecency.
- **Shared-chord check recorded**: language-scoped contexts *can* share one
  chord (`editor[json]` vs `editor[yaml]` are disjoint siblings — no conflict,
  no shadow), so a user may write it; the defaults keep the mapping next to the
  playgrounds instead. Written up in Keybindings & Shortcuts along with the
  collision check for `cmd+shift+j` (free, and `cmd+alt+p` is out because it
  folds onto the perf HUD's `ctrl+alt+p` off macOS).

## 2026-09-03 (the playground's find returns to the query line, #2411)

- **`esc` hands the keyboard back**: a result search opened with `cmd+f` from
  the jq/yq playground's query line (#2383) now *ends* there — `esc` closes the
  find widget and returns the focus to the query line with its program and
  caret untouched, instead of stranding the user in the result buffer. A search
  started **in** the result buffer keeps its old `esc` (close the prompt, then
  close the mode from resting normal mode); `playState.findQuery` marks which
  of the two it was and every other focus change clears it.
- **The matches stay lit**: `esc` after a committed search is deliberately not
  forwarded to the buffer — a normal-mode `esc` is vim's `:noh` — so the
  highlights are still on screen while the program is edited again. They live
  until the result they were found in is replaced: `syncPlayResultBuffer` calls
  the editor's new `ClearSearch` right after `ShowReadOnly`.
- **`cmd+g` / `cmd+shift+g` step from either focus**: the match-step chords are
  Global bindings and would otherwise step the *hosting pane's* document, which
  is the playground's input; `playMatchStepChord` + `stepPlayResultSearch`
  repeat the result buffer's search instead, without taking the focus. With no
  search committed they fall through to their ordinary meaning.
- Dialect-neutral, as the mode is: it is one host, so jq, yq and any later
  playground get it at once. Both cheatsheet focus tables list the chords live.

## 2026-09-03 (all-projects results in the find-in-path pane, #2413)

- **No corner popup any more**: Find in All Projects (#2394) shows its
  finished hits in the *same* results overlay find-in-path uses — the shared
  `internal/locations` list with the code-preview column beside it — grouped
  project → file → matches, with find-in-path's keys: `enter` (and
  `alt`/`ctrl+enter`) opens, `up`/`down` step with wrap, `pgup`/`pgdown` page,
  `cmd+g` / `cmd+shift+g` step the matches with the shared `3/17` counter,
  `alt+p`/`ctrl+e` focuses the excerpt, `esc` closes. A shared table test
  drives both surfaces over the same match set and compares where `enter`
  lands.
- **The list learned sections**: `locations.Item.Section` adds one grouping
  level above the file — a header row over the file headers of every
  consecutive group that shares it, labelled by the host through
  `List.SectionLabel(section, items)`. Cursor, paging, hit-testing and the
  match-step chord all count the new rows; a sectionless list is byte-for-byte
  what it was.
- **Progress moved to the status line**: while the scan runs nothing covers
  the editor — the new `allfind` segment counts
  `⌕ all projects 3/12 · 41 hits` from the new
  `search.MultiProgressMsg{Gen, Root, Done, Total}` (a root without matches
  emits no batch, so the batches alone could not say how far the scan had
  come), and a click on it opens the results.
- **Across the switch**: opening a hit in another project switches as before,
  but the result set rides the rebuild with the scan service, so
  `cmd+alt+shift+r` brings it straight back and `cmd+g` keeps stepping the
  hits — switching projects again where the next one needs it. A scan that
  matched nothing never takes the keyboard; it toasts, and the show-results
  command opens the empty set on demand. See
  [Project Search](/architecture/search.md).

## 2026-09-02 (cmd+f finds wherever "/" does, #2409)

- **One shared find chord**: the new Global command `search.open`
  ("Find in Pane", `cmd+f`) asks the focused pane for the new
  **`pane.Searchable`** capability (`internal/pane/searchable.go`,
  `OpenSearch() bool`) and opens whatever that pane calls its search — the
  explorer speed search, the Problems / Usages / TODO / Archive filter rows,
  the HTTP prompt, the DOM selector, the data grid's filter, the Issues filter
  overlay, the diff and markdown-preview prompts, terminal scrollback search
  and copy mode. `editor.find` keeps the chord inside an editor (an
  allowlisted intentional shadow); a pane with no search notifies
  *"No search in this pane"* instead of swallowing the key. `/` is unchanged
  everywhere.
- **`ctrl+f` stays unbound in the keymap table on purpose** — it is vim's
  page-forward motion in the editor, including operator-pending — so the
  panes answer it themselves through `ui.FindChord`
  (`internal/ui/findkey.go`), including the surfaces that own the keyboard
  ahead of the keymap layer (settings pages, the TODO overlay, copy mode).
  Off macOS the Cmd→Ctrl fold gives `ctrl+f` the same binding anyway. The one
  behaviour change: the data grid's `ctrl+f` now filters instead of paging
  (`n` / `pgdown` still page).
- **Two panes gained a search**: the **archive viewer** wears the shared
  filter row (`name:` / `type:` plus free text, gating the tree so
  synthesized directories filter like named ones) and the **diff viewer**
  gained a prompt over the diff rows, where `n`/`N` step matches while a
  search is open and hunks while none is. The **markdown preview** gained the
  same prompt over its rendered lines.

## 2026-09-02 (missing editor/pane chords, #2400)

- **Line-editing family**: `editor.moveLineUp`/`moveLineDown`,
  `editor.deleteLine`, `editor.deleteWordBackward`, `editor.docStart`/`docEnd`
  and `editor.selectLineStart`/`selectLineEnd` are commands now
  (`internal/editor/lineops.go`), selection-aware and bound to their JetBrains
  chords with a delivered secondary where the primary is `cmd`-modified. The
  two kills defer to the open insert session's own paths (#246, #955).
- **Pane chords named**: `http.search` (`cmd+f`/`ctrl+f`), `debug.copy`
  (`cmd+c`), `issues.copy` (`cmd+c`) and `issues.selectPrev`/`selectNext`
  (`ctrl+up`/`ctrl+down`) put chords the panes already handled — or could not
  do at all — into the keymap table, so they are listed and rebindable.
  `cmd+shift+l` aliases `lsp.format`.
- Chords left alone with reasons recorded: `alt+shift+arrow` (caret cloning),
  `cmd+up`/`cmd+down` (fold onto the paragraph jumps), `ctrl+e` (the diff
  view's leave-edit gesture), `cmd+ctrl+down` (no symbol-nav command exists).

## 2026-09-02 (playgrounds keep the last good result, #2412)

- **A failed jq/yq run no longer clears the result buffer**: `playState`
  separates the last *successful* `result` from the last run's `runErr`, so a
  compile or runtime error — and a broken **input** parse — leaves the output
  on screen with its scroll position and find highlights intact. The error
  keeps the info row (now `E: … · showing the last good result (n value(s))`)
  and a one-row **stale banner** in Warning sits above the buffer, folded into
  the header geometry so the mouse translation and the result editor's height
  follow it; it clears with the next successful run.

## 2026-09-02 (ike:// deep links, #2396)

- **ike:// URL scheme**: `ike://open?remote=…` / `ike://open?project=…`
  (+ optional `file=path:line`, `tool=name`) switches the running IKE to the
  matching project — resolution history → `project.directory` scan → Clone
  dialog — then opens the file at the line and shows the tool window. New
  leaf package `internal/deeplink` (parser, remote normalisation shared with
  forge detection, git-config/worktree remote reading, per-instance unix
  socket IPC); history entries carry normalised `remotes`; `ike ike://…` and
  `--url-send-only` in the CLI; macOS/Linux scheme registration via the
  desktop install script; new command `project.open_link`. New page
  [Deep Links](/architecture/deep-links.md).
- **Switching never asks** (#2396): the "Cannot save every buffer" and
  "Background workspace limit" dialogs are gone. Unsaveable dirty buffers
  park with the departing workspace; a busy LRU workspace is kept over the
  cap and only idle ones evict silently. The quit guard remains the single
  prompt about unsaved buffers / running processes.

## 2026-09-02 (Find in All Projects, #2394)

- **Background text search across the recent-projects history**:
  `project.findInAllProjects` (`cmd+alt+shift+f`) opens a form — query,
  case/word/regex, include/exclude globs, and a toggleable project list from
  `project.history` (missing roots greyed out and skipped) — then scans every
  kept root in the background (`internal/search/multi.go`, one shared result
  cap, per-root `.gitignore` stacks and errors). Results arrive in a popup
  that never steals focus, grouped by project then file;
  `project.findInAllProjectsResults` (`cmd+alt+shift+r`) focuses it, `esc`
  hides it, and `enter` on a foreign match switches projects before opening.
  Form state persists user-scoped under `project.find_all.*` (globs, excluded
  roots and the result cap editable on the Files & Session settings page).
  `ui.EditKey` gained `cmd+delete` (delete to line end) for every single-line
  input. See [Project Search](/architecture/search.md).

## 2026-09-01 (test-data generator reworked around a DSL, #2392)

- **One spec text instead of a five-step wizard**: `scratch.generate` now
  opens a single-screen dialog — template/format/rows/seed/table header, a
  multi-line DSL editor with autocomplete (catalog generators with their
  grammars, `{field}` references defined above the cursor), and a debounced
  live preview of the first five rows that shows line-numbered errors in its
  place. The per-format `scratch.generate.<format>` quick commands are gone,
  along with their keybind-audit family entry; `cmd+alt+shift+n` stays.
- **The DSL** (`internal/testdata/dsl.go`): one field per line,
  `name = expression` — generator calls with the old parameter grammars,
  quoted template strings with `{field}` placeholders, and `weighted(...)`
  alternatives over arbitrary expressions (positive weights, normalized).
  References resolve by a stable topological sort; unknown references and
  cycles are rejected naming the line and the cycle path. Weighted draws come
  from the seeded instance faker (one draw, only the winner evaluates), so
  "same seed + same spec → byte-identical output" still holds everywhere.
- **Format-free templates**: built-ins (Person, Address, Order, URL / Web,
  Server log) plus user templates saved/deleted from the dialog, persisted
  next to the last-used spec in `~/.ike/testdata.json`. The old per-format
  preset schema reads as empty and starts over.
## 2026-09-01 (the playground searches its result with `cmd+f`, #2383)

- **A search shortcut past the `tab`**: `cmd+f` (`editor.find`) opens the
  result buffer's search from the jq/yq playground's query line as well as from
  the result buffer itself, where it does exactly what `/` does. Searching a
  result no longer costs a `tab` there and a `tab` back.
- **The keyboard moves in with it**: `beginPlayResultSearch` sets the result
  focus and sends the buffer `editor.ActionMsg{Action: "find"}`, so the prompt
  gets the typing and `n` / `N` afterwards belong to the same focus — from both
  starting points, including after `esc` closes the prompt.
- **The chord is read live**: `playFindChord` resolves `editor.find` against the
  editor context in the live binding table, like `playCopyChord` does for
  `editor.copy`, so a rebound find key works in the playground too. Both focus
  tables in `playhelp.go` list it with the resolved chord.

## 2026-09-01 (large-response body files open reliably, #2385)

- **`o` and `m` work on responses past 1 MiB**, fresh and restored: the
  history store used to record a spooled body's path as `../../bodies/…`
  (`filepath.Rel` of an already-relative adopted name against the relative
  store root), which `List` collapsed to a bare `bodies/…` the viewer could
  not open. A body path now exists in exactly two shapes — `bodies/<name>` on
  disk, absolute in memory (the store resolves its root with `filepath.Abs`
  at construction) — and `List` heals the corrupted legacy form via the base
  name.
- **Body files are named after the Content-Type** (`httpclient.BodyFileExt`):
  the dispatcher spools to `body-*.json`/`.html`/…, adoption keeps the
  extension, and the editor's language choice follows it when `o` opens the
  file.
- **A gone body file withdraws the affordances**: `BodyFilePath` answers `""`
  for a path that cannot be opened, the notice/footer stop advertising
  `m`/`o`, and the hosts explain the absence instead of a raw "no such file".

## 2026-09-01 (a request exports as httpie too, #2384)

- **A second export format**: `http.copyAsHttpie` ("Copy as httpie") puts the
  request block under the caret on the clipboard as an
  [httpie](https://httpie.io/) command, and `http.copyShownAsHttpie` (`H` in
  the response viewer, next to `C`) does the same for the as-sent snapshot
  behind the shown response. Both halves of the curl export (#1994, #2059) get
  the second format, so neither one is stuck with a format the other has.
- **httpie syntax, not renamed curl flags**: `httpfile.ExportHTTPie` spells a
  request the way httpie reads it — a header as `Name:Value` (`Name;` when the
  value is empty), a query parameter as `param==value`, a JSON member as
  `field=value` or `field:=raw`, basic auth as `-a user:pass`, a form or
  multipart body behind `--form`, and the method always explicit.
- **The field syntax only where it is faithful**: a JSON object body becomes
  field items in the body's own key order (the decoder is walked token by
  token so a map cannot randomize it), with non-string members keeping their
  raw JSON behind `:=`. A JSON array, a key holding a separator or a non-JSON
  payload falls back to `--raw`, which is always correct; a binary body rides
  the base64 heredoc `RequestSnapshot.Curl` already uses, since httpie reads a
  non-tty stdin as the raw body. A query whose components are not all decodable
  `name=value` pairs stays in the URL rather than being guessed at.
- **Two sources, one serialization** — the #1994/#2059 split is kept:
  `ExportHTTPie` is the only place that knows the format, and the editor- and
  viewer-side app paths are now parameterized by serializer instead of
  duplicated. Nothing is masked: an `Authorization` header exports as sent.
  See [HTTP Client](/architecture/http-client.md).

## 2026-09-01 (the playground documents its language, #2382)

- **`ctrl+g` opens a language cheatsheet** in the jq/yq playground: the syntax
  that is not a function (pipe, `.[]`, `.[]?`, slices, construction, `//`,
  interpolation, `as`, `reduce`, `if`, `try`, the update operators, `..`),
  one-line **example programs** for the everyday operations — pick a field,
  iterate, filter, map, sort, group, count, rebuild an object, walk nested
  paths, default a missing value — and **every builtin** with its arities and
  description. The keyboard had a sheet since #2237; the language had none, so
  the completion popup only ever found what you already knew.
- **Generated, not hand-listed**: the builtin section is `jqplay.Builtins()`
  plus `builtinDocs`, the same list the engine prints and the popup offers, so
  a gojq version with a new function shows it without an edit. `builtinDocs`
  grew by ~60 commonly reached-for names it was missing, which the completion
  popup gets for free.
- **Every example is a tested program**: `cheatsheet_test.go` evaluates all of
  them against a sample document in the package and fails on a compile error, a
  runtime error or an empty result — a typo cannot live in the reference.
- **Dialect-aware**: the title, the placeholder and the document-language rows
  follow the open playground's dialect, never both side by side — a yq session
  is told about `---` and merge keys, a jq session about `.jsonl` streams and
  exact number spellings.
- **A reference that is also a tool**: `enter` on a syntax or example row
  replaces the query line and runs it (the replaced program goes into the
  history first, so `↑` brings it back); `enter` on a builtin inserts its name
  at the caret. It lives in the palette — searchable rather than paged — beside
  `ctrl+l`, which stays the place for *your own* programs.
## 2026-09-01 (the explorer's mark moves into the guide column, #2380)

- **Space no longer takes the tree apart**: the multi-select mark used to
  prepend a two-cell column to every row while anything was marked. That
  changed the row text behind the memoized row widths, so the renderer clipped
  and padded against widths two cells too small and the terminal wrapped the
  whole pane — blank lines between entries, VCS badges on their own lines,
  stray indent guides. The mark now replaces the row's **own indent-guide
  glyph** (`✓` for `│`, painted in `Accent` so it never reads as a guide), so a
  marked row is exactly as wide as an unmarked one and the tree never shifts.
  A depth-0 row, which has no guide cell, uses the expand marker's blank second
  cell instead.
- **The width memo re-measures itself**: `wcache` no longer relies on a
  documented promise that every row-text mutation calls `invalidateWidth`. It
  stores the `widthKey` it was measured under — row-set epoch, indent, pane
  width, mark count, icons flag — and re-measures on a mismatch, so a future
  state that changes row text cannot render a stale width.
  See [File Explorer](/architecture/explorer.md).

## 2026-09-01 (horizontal scroll state is visible, #2377)

- **Edge marks in every sideways-scrolling view**: the editor, the diff viewer,
  the explorer and the playground result buffer now mark the edges of their
  horizontal window — `‹` on the first cell while the view is scrolled, `›` on
  the last cell of a row that continues past it. Horizontal scrolling used to
  be invisible: a shifted view looked exactly like an unshifted one showing
  different text, and only "Ln x, Col y" hinted at anything, about the cursor
  rather than the window.
- **One shared package, one language**: `internal/hscroll` owns the glyphs, the
  "which edge hides content" predicate and the ANSI-aware stamping; each view
  supplies its own row model. The explorer's older right-clip ellipsis (#1035)
  folds into the shared `›` — same cell, same meaning.
- **Overlays, not insertions**: a mark replaces an edge cell instead of adding
  a column, the way the vertical scrollbar claims a pane's rightmost column
  (#1022), so cursor positions, mouse hit zones and column reporting are
  untouched. The editor additionally never covers the caret's own cell, keeps
  `›` clear of the scrollbar column, and renders nothing under soft wrap
  (#64), where there is no horizontal scroll to report. The editor's overflow
  verdict comes from the render pass itself, so tabs, conceal stand-ins and
  the sv table expansion (#1724) are counted as drawn, not re-counted.
- **`ui.h_scroll_marks`** (default `true`) turns the marks off everywhere, in
  the config and in the Settings UI.

## 2026-09-01 (an open issue's text selects with the mouse, #2374)

- **Press, drag, release in the detail body**: both full-area details of the
  Issues window — the issue detail and the PR detail — now select text with the
  mouse. The gesture is the shared one (`internal/textsel`, #2070): char on a
  drag, word on a double click, line on a triple, and a wheel during a running
  drag *grows* the span instead of dropping it. `y` / `cmd+c` put it on the
  clipboard through the host, and the copy clears the selection.
- **Rendered text, not the markdown source**: the body is wrapped and styled
  through glamour and the user selects what they see, so that is what is
  copied — trailing render padding trimmed. The selection is tied to the tab
  and the issue/PR it was taken in, so walking on or closing the detail retires
  it. The list views stay click-only (select, double-click-open, chips, tabs):
  their rows are targets, not prose.
  See [GitHub issues](/architecture/github-issues.md).

## 2026-08-31 (Open in Browser gets a default keybind, #2365)

- **`file.openInBrowser` is no longer menu-only**: telemetry showed the command
  in daily use, every invocation routed through the File/context menu. It now
  ships JetBrains' own `WebOpenInAction` chord verbatim — **`alt+f2`**, free on
  both platforms next to `f2`/`shift+f2` (diagnostics) and `cmd`/`ctrl+f2`
  (stop debugging), and typable on QWERTZ.
- **One Global row covers both call sites**: the command resolves its subject
  through `refactorTarget`, exactly like `file.copyPath`, so the editor and the
  explorer are served without a per-context binding. Its audit-ledger entry in
  `cmd/ike/keybind_audit_test.go` is gone; being Alt-modified it is fragile, so
  it records the palette/context-menu escape route like every other Alt chord.
  See [keybindings](/architecture/keybindings.md).

## 2026-08-31 (a failed or slow HTTP response reports itself, #2364)

- **The answer no longer waits to be found**: a dispatch that ends non-2xx, or
  one whose wall clock passes `http.notify_slow_ms`, emits an ordinary
  notification naming method, status and duration — so it toasts and lands in
  `notifications.history` — whenever the response pane is not on screen when
  the answer arrives. A visible pane stays quiet: its status row already said
  it. Visibility is read *before* the fill opens a viewer, so routing the
  answer cannot silence its own notice.
- **The threshold is a setting** (`http.notify_slow_ms`, 0–600000 ms, default
  3000, `0` = off), validated, persisted and editable on the Settings UI's
  HTTP Client page. The non-2xx branch is not configurable — an unnoticed
  failure is never wanted. Cancels and transport errors keep their existing
  notices and add none.
  See [HTTP client](/architecture/http-client.md).

## 2026-08-31 (playground follows its source file, #2356)

- **The snapshot is re-read on external change**: an open jq/yq playground
  whose source file is written by another process re-parses its input and
  re-runs the current program against it — automatically, through the same
  debounce/generation machinery a program change uses, so a burst of saves
  leaves only the newest result. Query, caret and history position are
  untouched: the input is renewed, not the playground.
- **Scope and failure modes**: only whole-file editor sources are followed (an
  HTTP response is no file, a selection's range means something else after an
  edit); the text comes from the buffer *after* its own auto-reload, so unsaved
  edits are never overwritten; a broken new input keeps the last valid result
  and shows the parse error; a removed file closes the mode with a
  notification, or — with unsaved edits — keeps it over the surviving buffer
  with a warning. The info row carries a `reloaded HH:MM:SS` stamp.
  See [jq & yq playground](/architecture/jq-playground.md).
## 2026-08-31 (jq/yq playground bound to its document, not its pane, #2355)

- **The mode belongs to the document it queries**: `playState` now keeps the
  queried editor model (or HTTP response instance) beside its `paneKey`, and
  `playSrcShown` gates rendering, header rows, mouse routing and key routing on
  the pane *still showing that document* — one predicate, so no half-rendered
  in-between state exists.
- **Opening a file into the hosting pane shows it immediately**; the playground
  is not closed by it but stays mounted, query, result and history intact — the
  #1980 focus-change semantics — and reappears unchanged when its document's
  tab is activated again. The HTTP response playground is matched by instance
  and is unaffected.
- **No playground outlives its document**: `syncPlaygroundSource` closes the
  mode on the settled `Update` pass once the queried document is gone from the
  workspace, covering tab closes, pane closes and explorer deletions in one
  hook. See [jq & yq playground](/architecture/jq-playground.md).

## 2026-08-31 (http.run freeze fixed: linear column mapping, off-loop response highlighting, #2353)

- **The #2348 freeze is root-caused and fixed**: `highlight.colMapper.runeCol`
  rescanned the line prefix per span — O(spans × line bytes), ~10¹⁰ byte
  passes on a 2 MB minified non-ASCII single line — *and* the response
  viewer's syntax pass ran synchronously inside `app.HTTPResponseMsg`. The
  mapper now caches per line the first non-ASCII offset and a rune-count
  prefix table (linear, benchmarked in `internal/highlight/colmap_test.go`).
- **The response body highlights off the update loop**: compose shows the
  body plain immediately and schedules the pass (`httppane.HighlightCmd`,
  the editor's `parseCmd` arrangement); `httppane.HighlightedMsg` paints
  spans and folds when the parse lands, generation-guarded so a newer
  response never wears an older one's colours.
- **`http.highlight_limit_kb`** (Settings UI, HTTP Client page; default
  2048 KiB): past the limit no pass is scheduled and a notice row in the
  pane says why. See [http client](/architecture/http-client.md) and
  [settings UI](/architecture/settings-ui.md).

## 2026-08-31 (free extension in the scratch language pickers, #2340)

- **"Custom…" closes the curated list**: both scratch language surfaces — the
  `scratch.new` picker and the manager's `ctrl+l` step — end in a row that is
  no creator but a doorway to a one-line extension prompt, so `.tf`, `.mjs` or
  an in-house suffix needs no code change while the offering stays curated.
  Creating goes through the same funnel a language row uses, so typing `py` is
  the "Python" row by another route.
- The row **survives every filter** (it is needed exactly when nothing else
  matches), the input is **validated, not corrected** — optional leading dot,
  refusals name their reason for empty input, path separators, whitespace or a
  dangling dot — and `esc` walks back to the row list, not out of the dialog.
  Typed extensions are deliberately not remembered. See
  [scratch files](/architecture/scratch-files.md).

## 2026-08-31 (wheel outside the popup layer scrolls the pane below, #2343)

- **The wheel is a reading gesture, not a focus gesture**: with the popup
  terminal or a torn-out floating panel (#1793) focused, a notch over no layer
  box no longer vanishes into the layer — it falls through to the pane under
  the pointer (editor, explorer, terminal, tool panes) via the ordinary wheel
  route, with no per-pane special case. The layer keeps keyboard and focus:
  nothing blurs, nothing rises, the z-order is untouched.
- Presses outside the boxes still blur the layer (#2309), the wheel over a box
  still pages that box's scrollback, and the exception skips while a drag
  (selection, scrollbar, tab tear-out) is active. Coalescing (#238) folds only
  events sharing a cell, so a burst never splits between layer and pane. See
  [terminal](/architecture/terminal.md) and [mouse](/architecture/mouse.md).

## 2026-08-31 (freeze diagnosis: session/heartbeat/flight telemetry, trace log, #2348)

- **Telemetry schema v3**: three diagnostic event types join the usage log —
  `session` (Ike version, OS, 12-hex hashed project token; deferred like
  `pane.focus`, re-emitted on project switch), `heartbeat` (every 10 s, with
  the update-loop pass count, so a log's end says *when* work stopped and
  whether the loop or the process died), and `op` (lifecycle of
  `http.flight`: `start`, then `ok`/`error`/`canceled` with duration, status
  class, streaming flag). `Recorder.FlushSoon` puts everything up to the
  flight start on disk before the exchange departs. Privacy line unchanged:
  no paths, URLs, keys or content.
- **`perf.trace_log`** (Settings UI, Performance HUD page; default off):
  one structural line per processed update-loop message into `.ike/trace.log`
  through a held handle (`heldLog`, generalized from the #2176 transcript).
- **The `http.run` dispatch path was audited** for UI-freeze potential and
  cleared: the exchange runs off-loop, each stream message re-arms exactly
  one event-channel read, the coalescer's mutex pins chunk-before-final
  ordering. The silent #2348 watchdog is explained by its scope (Update/View
  passes only) — documented with a freeze-triage procedure in
  [performance](/architecture/performance.md); event details in
  [usage telemetry](/architecture/usage-telemetry.md) and
  [http client](/architecture/http-client.md).

## 2026-08-31 (scratch files findable by their own name in the `@` finder, #2341)

- **`@notes` finds a scratch named `notes.go`**: `FileMode.scratchItems` now
  fuzzy-matches every query against each scratch's **own file name**, not only
  against the literal word "scratch" (#1812), which stays the way to list the
  **whole store** newest-first.
- **Project hits keep the top**: scratch rows are appended below the project
  matches (ordered by fuzzy score, store order as tiebreak), like the
  filesystem fallback (#1775); they take no part in frecency (#2155).
- The existing `seen` map de-duplicates scratch paths against the filesystem
  fallback, and path queries (`/`, `~/`, `./`) are untouched. See
  [command palette](/architecture/command-palette.md) and
  [scratch files](/architecture/scratch-files.md).

## 2026-08-31 (scratch from selection, promote scratch to a project file, #2339)

- **`scratch.newFromSelection`** (`cmd+alt+shift+s`, palette + File menu)
  creates a scratch holding the active selection and **inherits the source
  file's extension**, so the language picker never opens. The store has no
  whitelist, so an unregistered suffix is kept as-is; a source without one
  falls back to `txt`, and no selection is a reported refusal rather than an
  empty scratch. `NewScratchMsg` gained a `Content` field for it.
- **`scratch.promote`** (`cmd+alt+shift+p`, palette + File menu, and `ctrl+p`
  in the scratch manager) names a project path for a scratch and moves it
  there: `scratch.Promote` refuses an occupied target, removes the source only
  after the copy is durably written, and the move is announced as
  `explorer.FileMovedMsg`, so the open tab keeps its history and saves to the
  project file from then on.
- Removed a stray `<<<<<<< HEAD` conflict marker left in this file by the
  #2330 merge. See [scratch files](/architecture/scratch-files.md).

## 2026-08-30 (embedded JS/CSS intelligence in HTML via shadow documents, #2330)

- **`<script>`/`<style>` bodies in HTML get real LSP features**: the html
  language registers `EmbeddedShadow`, so the LSP manager merges its detected
  fragments per embedded language into one whole-buffer virtual document with
  everything outside the regions blanked (`manager/shadow.go`) — identity
  position mapping, and all script tags share one scope like in a browser.
- **vtsls-compatible fragment URIs**: `ServerSpec.FragmentScheme` lets a
  server pick the virtual-document URI scheme; vtsls declares `untitled`
  (the VS Code TS extension drops documents on unsupported schemes), with the
  fragment language's extension for script-kind detection. Other servers keep
  `ike-fragment:`.
- **Signature help routes into fragments** and completion/signature **trigger
  characters are position-aware** (`CompletionTriggersAt`/`SignatureTriggersAt`)
  — `.` pops vtsls completions inside a `<script>`.
- Inline `on*` attribute handlers stay without delegation (documented scope
  decision). See [lsp](/architecture/lsp.md) and
  [languages](/architecture/languages.md).

## 2026-08-30 (inline code in HTML `on*` and `style` attributes highlights, #2329)

- **The HTML injection query gates on the attribute name**: `onclick="alert(1)"`
  and friends inject the typescript grammar, `style="color: red"` the css one.
  Only the inner `attribute_value` node is captured, so the quotes stay HTML,
  and the rule is conditional through a tree-sitter text predicate
  (`#match? "(?i)^on[a-z]{3,}$"`, `"(?i)^style$"`). The go-tree-sitter binding
  evaluates text predicates while iterating matches, so no Go machinery was
  needed to make the fragment seam name-aware. The letter-run bound keeps
  `data-on-tap`, `hx-on:click`, `once` and `only` plain.
- **`.partial` is a third fragment mode** (`fragment.<lang>.partial`) for a
  snippet that is not a document in its language. It changes two things:
  `injection.go` parses it between the synthetic lines `fragmentWrapper`
  registers — css gets `*{` / `}`, because the grammar reads a bare
  declaration list as a selector and would colour `color` as a `tag` — and the
  LSP virtual-document seam skips it, since no css server can own a bare
  declaration list.
- **The wrapper cannot shift columns**: it occupies whole lines of its own, so
  `unwrapSpans`/`unwrapFolds` only drop the wrapper's own spans and the
  synthetic rule's fold and shift the rest up by one line. Host↔fragment
  mapping stays a pure offset shift, multi-line `style` values included.

## 2026-08-30 (Search previews are a read-only mini editor, #2327)

- **`internal/codepreview` renders code, not text**: the excerpt column every
  picker carries now goes through the tree-sitter pipeline of
  `internal/highlight` — the same standalone-excerpt parse the definition peek
  and the hover code fences use — behind a dim line-number gutter. A language
  with no grammar (or a build without the parser) falls back to plain text, so
  nothing can fail the frame. Tabs expand to four cells *before* the parse, so
  spans, match ranges and rendered cells share one coordinate system, and the
  styled rows are memoised per window/hit/palette: walking a result list
  re-parses only when the target actually moves.
- **The hit line reads like the editor's current line**: it carries the
  current-line background across the full column, gutter included, and
  `Target.Ranges` — the finder's per-line match ranges (#1121) — render bold
  and underlined *on top of* the capture colours instead of replacing them.
- **The column scrolls** (`Cache.Key`, `Cache.Scroll`): `alt+p` (or `ctrl+e`,
  the macOS alias, #422) focuses it in the find-in-path overlay, the palette
  pickers and the call-hierarchy overlay alike; a mouse press inside it does
  the same and the vertical rule turns accent-coloured. The motions are the
  editor's, minus every edit — `j`/`k`, `ctrl+d`/`ctrl+u`, `ctrl+f`/`ctrl+b`,
  `h`/`l`, `0`/`$`, `g`/`G`, and `z` back to the hit. Vertical scrolling walks
  the whole file (a short read tells the viewport where the file ends, so no
  full scan is needed to clamp the tail) and the horizontal offset slices the
  *styled* rows with `ansi.Cut`, so colours survive it. `esc` blurs; moving
  the result selection re-centers on the new hit and drops both offsets.
- **The column adapts its width**: `Cache.Natural` measures the widest line of
  the window around the hit plus its gutter, bounded to **50…120** cells, and
  `SplitWidth` clamps that to half the content and to whatever leaves the list
  40 cells. Measured around the *hit*, not the scrolled window, so scrolling
  never resizes the columns under the cursor. `Split(inner)` stays as the
  no-target shorthand; the popups keep their 40/11 rules from #2047.
- **`ui.Typing` is EditKey's insertion guard, exported**: the finder and the
  palette blur a focused excerpt the moment one types, so the query field
  takes the key — and they ask the shared predicate instead of re-deriving
  `msg.Text != ""`, which the #2002 sweep guard reads as a hand-rolled input.
- **No new commands, so no new keybinds**: the focus toggle and the motions
  are overlay-local keys, like the finder's `ctrl+c`/`ctrl+w`/`ctrl+x`
  toggles — the keymap layer never sees them while an overlay owns the
  keyboard.

## 2026-08-28 (cmd+c in the HTTP response pane and the explorer, #2315)

- **The copy chord is bound where users pressed it**: unbound-chord telemetry
  showed `cmd+c` in the `http` and `explorer` contexts. `Defaults()` now binds
  it in both — `http.copyResponse` in the response viewer, `file.copyPath` in
  the tree — so the key is listed in the cheatsheet, the keymap settings page
  and the generated reference, and is rebindable like every other chord.
- **`http.copyResponse` is the pane's copy key as a command**: it forwards to
  the new exported `httppane.Model.CopyKeyCmd`, which is the pane-local
  `copyKeyCmd` — the live selection when there is one, else the whole body. The
  pane handled `cmd+c` itself before, invisibly to the keymap layer; both
  entries now run one code path and cannot drift.
- **No `ctrl+c` secondary**: on macOS `ctrl+c` stays the global quit chord that
  only yields to a live selection (#2062); off macOS the `cmd+c` rows normalise
  onto `ctrl+c` on their own, the same way the editor's copy row always has.

## 2026-08-28 (ctrl+r reruns in the response pane and the archive viewer, #2314)

- **`ctrl+r` is a keybinding now** (`internal/keymap/defaults.go`): JetBrains
  spends the chord on Rerun, and the usage log showed users pressing it in the
  HTTP response pane and the archive viewer, where it was recorded as unbound.
  The response pane answered it all along — as a pane-local key the keymap
  layer neither listed, documented nor could rebind. The default set binds it
  per context: `http.resend` in `http`, `archive.reload` in `archive`. Two
  disjoint contexts, so it is neither a conflict nor a shadow, and a plain
  ctrl+letter delivers everywhere.
- **`archive.reload` is new** (`internal/archview`, `internal/app/archives.go`):
  the viewer re-lists its file, keeping collapsed directories by path and
  clamping the cursor into the new rows, so a re-packed archive shows its new
  members without the pane being closed. A broken file degrades to the pane's
  listing-error notice, and the model reports the entry count so a reload that
  changed nothing does not read as a dead key.
- **The keybindings reference grew its pane sections**: `cmd/docgen` listed
  only global/editor/explorer/diff/palette bindings, so the terminal's
  `ctrl+t` — and now both `ctrl+r` rows — were invisible there. The generated
  page has a section per pane context with a binding.

## 2026-08-28 (Unified list-filter syntax, #2156)

- **One filter language** (`internal/filterexpr`): the fielded-term syntax the
  Issues pane introduced (#2110/#2115) is now a leaf package every list pane
  shares — tokenizer, quoting, `Schema`/`Field` validation with the error
  wording, `Format`, and the two match helpers (`MatchText` over
  `internal/fuzzy`, `MatchPath` over `internal/pathglob`). Terms of different
  fields AND, repeats of one field OR, bare words are the fuzzy pattern.
- **One filter row** (`internal/filterbar`): the permanent one-line input the
  panes render, focused with `/` everywhere. `enter` applies, `esc` clears,
  `tab` accepts the inline completion (field names from the schema, values
  from the schema or the pane's own candidates); navigation keys fall through
  so the list can be steered while typing, and a half-written expression
  keeps the last good query instead of emptying the list.
- **Issues migrated, unchanged**: `internal/issuefilter` is now the Issues
  *dialect* (the `Schema` for `is:`/`label:`/`sort:` plus its `Spec` shape),
  and the live match input resolves its qualifiers against that same schema,
  so the pane and the config reader cannot drift. The conformance test and
  every existing error message stand.
- **Problems, Usages, TODO index gained the same syntax**: `severity:`
  (alias `sev:`), `file:`, `code:`, `source:`, `scope:` in Problems;
  `file:`, `text:` in Usages; `tag:`, `file:`, `scope:` in the TODO index.
  Filtered-empty files drop out header and all, and the panes' visible
  totals follow the filter.
- **Single-key filters became sugar**: Problems' `f` writes `scope:file`
  (still resolved against the *current* editor file, so the scope follows the
  editor), the TODO index's `ctrl+t`/`ctrl+o` write `tag:`/`scope:file`, and
  its chips row renders those terms rather than holding state beside them.
- Docs: new [List Filter Syntax](/architecture/list-filters.md); Problems,
  Usages, TODO index and Issues pages updated.

## 2026-08-28 (Unbound-command audit: defaults for the useful ones, #2305)

- Second pass over the commands that ship without a default keybind. Thirteen
  everyday actions got one: `file.copyPath` (`cmd+shift+c`),
  `lsp.organizeImports` (`ctrl+alt+o`), the jq/yq playgrounds (`ctrl+alt+j` /
  `ctrl+alt+y`), the test-data wizard `scratch.generate`
  (`cmd+alt+shift+n`), `vcs.diff` (`cmd+alt+d`), the `tests.toggle` and
  `debug.console` tool windows (`cmd+4` / `cmd+5`), `run.select`
  (`alt+shift+f10`), `debug.testAtCursor` (`alt+shift+f9`), `pane.close`
  (`ctrl+alt+w`), `view.toggleWrap` (`alt+shift+w`) and `window.layouts`
  (`alt+shift+f12`) — conflict- and shadow-free on both platforms, each with
  its palette/menu escape route recorded in `reachableAlternatives`.
- The remaining palette-only commands are keybind-less on purpose, and now say
  so: the audit ledger in `cmd/ike/keybind_audit_test.go` records a reason per
  command family (vim-native key, pane-local key, picker entry, flavour of a
  bound command, intention doorway, menu home, one-off) and fails the build for
  any registered command that is neither bound nor justified — and for any
  stale entry.
- Fallout of the bigger table: the Keymap Doctor's probe grid sizes each column
  to its own longest chord and shrinks the inter-column gap until the grid fits
  the box, instead of letting the rightmost column fall off the edge — a probe
  target the user cannot see is one they cannot press.
- Process: [Change Workflow](/process/change-workflow.md) now states that new
  commands ship with a default keybind, keybind-less only with a recorded
  reason.

## 2026-08-28 (Markdown preview: follow relative links, render local images inline, #2180)

- **Link following** (`internal/preview/links.go`): the preview indexes its
  links out of the rendered output's OSC 8 hyperlink sequences — glamour
  already carries the raw markdown destination there, so a link's row and byte
  span come from the rendering itself, and the label/printed-URL pair for one
  link collapses to one entry. In-document `#anchor` links, which glamour
  renders as bare text, are recovered from the source and located by label
  from where the scroll-sync mapping puts them. `tab`/`shift+tab` walk the
  links (wrapping, revealing, reverse-video marker on the label, destination
  in the status line), `enter` follows and `y` copies. A link-free preview
  leaves `tab` its global focus-cycling meaning.
- **Follow policy** (`internal/app/previewlinks.go`): `preview.LinkMsg` hands
  the destination to the root model. `#anchor` scrolls the preview itself
  (GitHub-style slugs); an absolute URL goes to the `browserOpen` seam — only
  on the explicit key, never during rendering; anything else resolves against
  the previewed file and opens through `openPath`, or `openPathAt` on the
  heading line for `file.md#section`. A missing target toasts.
- **Inline images** (`internal/preview/images.go`): local images the buffer
  references decode once (cached by resolved path) and replace their rendered
  "Image: …" line with a Unicode-placeholder block, reusing the image pane's
  protocol layer — `imgview.FitGrid`, `PlaceholderGrid`, `Transmit`, `Delete`
  and `HumanSize` are now exported and shared. Substitution runs *before* the
  scroll-sync anchors are built, so headings around an image block still map
  to the lines they really occupy.
- **Reconcile**: `imageSyncCmd` and `releaseWorkspaceImages` walk markdown
  previews alongside image panes (one pane, many ids), and
  `Registry.PreviewsMinted` joins `ImagesMinted` in the #2187 early-out.
- **Boundaries**: no network I/O — a destination with a scheme is remote by
  definition and is never opened for its bytes, so remote images stay text.
  Without Kitty graphics the alt-text line keeps its place and gains a dim
  format/size/dimensions caption; missing and undecodable targets degrade the
  same way.
## 2026-08-28 (Open in Browser: compressed HTML files, #2298)

- "Open in Browser" now reaches into a plain gzip file (`report.html.gz`)
  whose *inner* content is browser-viewable: `gzipArchiveOf` resolves the
  focused target back to the on-disk `.gz` — directly, or through an
  already-open preview buffer's `<archive>!<inner>` path — and, when
  `gzfile.IsPlain` claims it, decompresses into an ike-owned scratch
  directory (`$TMPDIR/ike-open-in-browser`, one subdirectory per source
  path so a reopen overwrites instead of accumulating) before opening the
  unpacked file. A compressed tar stays with the archive viewer; a gzip
  whose inner content is not browser-viewable (`app.log.gz`) still declines
  with the same toast as before; decompressed content past
  `files.large_file_kb` is refused with a clear notice instead of opening
  partially. The scratch directory is swept on a clean exit and again at
  the next startup, so a kill or a crash never leaves unpacked copies
  behind. See [Gz Viewer](architecture/gz-viewer.md#open-in-browser-unpacks-not-previews-2298).

## 2026-08-28 (Local history: project-wide timeline across files, #2171)

- **Project-wide read side** (`internal/localhistory/project.go`): `Scan`
  collects the whole index into one newest-first `Snapshot` list (an entry
  plus its path), bounded by `DefaultScanCap` (2000) and reporting that it cut
  the tail; `GroupByDay`/`DayLabel` bucket it into `Today` / `Yesterday` /
  `Mon 2026-08-24`, on calendar days in the local zone. Ties break on path
  then hash, so the order is total and a re-scan never reshuffles rows.
- **The panel** (`history.projectTimeline`, "Show Project History Timeline",
  `internal/app/projecthistory.go`): every file's snapshots on one day-grouped
  axis — the answer to "what did I change today?", which the per-file panel
  (#1023) and the per-file Timeline (#1916) cannot give. Printable keys narrow
  by path substring (`ui.SpeedSearch`), so navigation is the arrows / page
  keys / `ctrl+n`/`ctrl+p` and loading more is `ctrl+l`, not the Timeline's
  bare `L`. Rows reveal 100 at a time (also as the selection walks toward the
  end) and the rendered block windows to the terminal height, with a counts
  line that always says what is held back and why.
- **Handoff:** `enter` (or a click on the selected row, #2275) closes the
  timeline, opens the row's file and raises the per-file panel with that
  snapshot preselected — matched on time *and* hash. Opening the file first is
  what makes `r` restore work from there; the shared `openLocalHistoryFor` is
  the one open path both entry points use.
- [architecture/local-history.md](architecture/local-history.md) grew the
  project-wide section.

## 2026-08-28 (Transparent Ansible Vault editing, #2293)

- **Native vault format** (`internal/ansiblevault`): the Vault 1.1/1.2
  envelope (AES-256-CTR, PBKDF2-HMAC-SHA256, HMAC-SHA256) on the Go stdlib —
  no dependency, no `ansible-vault` CLI needed; fixtures generated with real
  ansible-core, interop verified both directions. Password resolution follows
  Ansible's precedence: `ANSIBLE_VAULT_PASSWORD`,
  `ANSIBLE_VAULT_PASSWORD_FILE`, then IKE's `ansible.vault_password_file`
  (user scope = global default, project scope overrides; new Settings UI page
  "Ansible Vault" with an existence-checked Path entry).
- **The round-trip**: `Load` decrypts a `$ANSIBLE_VAULT;` file into the
  buffer (diskHash keeps hashing the on-disk ciphertext), `saveAs` re-encrypts
  before `os.WriteFile` — every save flavor funnels through it — and
  `reloadFrom` decrypts external rewrites (adopting an externally encrypted
  file, dropping the state for an externally decrypted one). Without a source
  or with a wrong password the ciphertext opens as before, reason on the ex
  line, file untouched.
- **Plaintext never on disk**: persistent undo refuses vault documents in
  both directions and the crash-recovery backup paths skip them; local
  history and the TODO index re-read the file and thus see ciphertext. Vault
  state is a document property (share-copied, SyncMsg-mirrored).
- **"Treat as Vault File"** (`vault.treatAsFile`, `vaultProvider`, gated on
  the new `Context.VaultReady`/`VaultBuffer` facts): a ciphertext buffer
  decrypts in place (file untouched), a plaintext file is encrypted on disk
  immediately.
- New concept doc [architecture/ansible-vault.md](architecture/ansible-vault.md).

## 2026-08-28 (Marketplace: update notifications for installed plugins, #2257)

- **Update diff** (`internal/market/update.go`): `FindUpdates` compares the
  installed sidecars against the catalog and returns one `market.Update` per
  outdated plugin, including `AddedCapabilities` — what the new version asks
  for beyond the manifest's pin. Dropped capabilities do not count; a shorter
  list can only reduce what the runtime allows. The sidecar's capability list
  now comes back with `Engine.Installed`, which is what makes the diff
  possible.
- **The capability gate**: an update with a grown capability list is badged
  `⚠ new capabilities`, named in the row detail, **held back** from update-all
  with an inline note, and installed only through a confirmation dialog that
  spells out the added capabilities. A capability change is surfaced, never
  auto-accepted; the install path's re-pinning rules are untouched.
- **Automatic check** (`internal/app/marketupdates.go`, wired into `Init`):
  one background catalog fetch on start, rate-limited to once per 24 h through
  a timestamp in `~/.ike/marketplace-check.json`, and silent on failure — an
  unreachable catalog raises nothing and leaves the timestamp alone, so the
  next start retries instead of waiting a day. A check that finds updates
  raises exactly one notification and hands its catalog to the marketplace
  page, so opening it shows the badges without re-fetching.
- **Update-all** (`u`) installs every pending update through the existing
  checksum-verified atomic install path, in one batch; the page header counts
  the pending updates.
- **Setting**: `marketplace.auto_check` (user scope, default on) in config and
  under Settings ▸ Marketplace Catalog.

## 2026-08-28 (Scratch files: manager with rename, delete, language change, #2256)

- **The manager** (`internal/app/scratch_manager.go`): `scratch.manage`
  ("Manage Scratch Files…", palette + File menu) lists the whole store in a
  floating shell dialog with name, language, size and last-modified, narrowed
  by a `ui.SpeedSearch` type-ahead over name *and* language; `enter` (or a
  second click on a row) opens through the standard funnel.
- **Actions are chords, not letters** — a letter belongs to the type-ahead:
  `ctrl+r`/`f2` renames (prefilled, validated by the store), `ctrl+l` opens the
  language list, `ctrl+d`/`delete` deletes after a confirmation, `esc` clears
  the query, walks a step back, then closes.
- **Open buffers follow without new machinery**: mutations emit
  `explorer.FileMovedMsg` / `explorer.FileDeletedMsg`, so a scratch open in a
  tab re-points (tab title and language follow via `editor.SetPath`) or closes
  through the paths #175 already established.
- **Store** (`internal/scratch`): `Entry` carries the file's `Size`, and
  `SetExt` expresses the language change as what it is — keep the stem, swap
  the extension — running through `Rename` so its guards (no overwrite, no
  traversal, nothing outside the store) hold unchanged.
- **Reachable from the creation flow**: the `scratch.new` language picker has
  an "Open existing scratch…" row that runs the manager.

## 2026-08-28 (Bookmarks: descriptions and a project-wide overview, #2251)

- **The overview** (`internal/app/bookmarks_overview.go`, `bookmark.overview`):
  a floating list of every project bookmark, grouped by file, each row showing
  the bookmarked line's text and — where there is one — its description. The
  palette picker mixes bookmarks with the vim marks and ranks fuzzily; the
  overview is the bookmark set as a set, in stable (path, line) order.
- **Three row actions**: `enter` jumps through the standard open funnel (so the
  navigation history records), `ctrl+e` edits the description, `delete` /
  `ctrl+d` removes the bookmark and the list stays open until the last one
  goes.
- **Edit reuses the annotate prompt** (`startBookmarkNotePrompt`): the note
  prompt now targets an arbitrary bookmark rather than only the cursor line,
  and its new `back` flag reopens the overview on save *and* on cancel — the
  edit never drops the user out of the list they came from.
- **Descriptions while placing**: `bookmark.annotate` on an unbookmarked line
  already placed the bookmark together with its note; that is now the
  documented "describe while placing" path, and the menu entry sits beside the
  new overview.
- **Speed search** (#2111): printable keys narrow the list against path, line,
  mnemonic, preview and description together; `esc` clears the query before it
  closes. Line texts are read once and cached per overview, so narrowing never
  re-reads a file.
## 2026-08-28 (Archive viewer: extract members or the whole archive to disk, #2249)

- **Extraction, guarded** (`internal/archive/extract.go`): `PlanExtract` reads
  headers only and reports what a run would do — selected members, refused
  ones with a reason, existing targets, declared size — and `Extract` writes
  what the plan named. The two phases are what makes the overwrite question
  askable at all.
- **Nothing escapes the target dir** (`SafeTarget`): absolute names, any `../`
  component after `path.Clean`, and Windows volume names are refused and
  reported as `unsafe path`, never clamped silently; the joined path is
  re-checked against the destination afterwards. Sym-/hard links and special
  files are skipped rather than materialized, and an existing symlink at a
  target is replaced instead of written through.
- **Bomb guard for tars** (`DefaultExtractLimit`, 1 GiB): enforced on the
  plan's declared total *and* on the bytes actually written, since a header
  size is untrusted — the partial file is removed and the message names the
  cap.
- **Pane keys and palette** (`internal/archview`): `e` extracts the row under
  the cursor (a directory row means its subtree), `E` the whole archive; both
  only emit `archview.ExtractMsg`, so the viewer stays write-free. The palette
  entries `archive.extractEntry` / `archive.extractAll` act on the focused
  viewer.
- **The UI around it** (`internal/app/archextract.go`): a target-directory
  prompt prefilled next to the archive and named after it without its archive
  suffix (`backup.tar.gz` → `./backup`), an overwrite guard answering
  `o` / `s` / `esc` for the whole run, and a summary toast counting files,
  bytes and the skipped entries with their reasons.

## 2026-08-28 (Theme picker: live preview while scrolling, rollback on esc, #2181)

- **Preview on the highlight, not on the choice**
  (`internal/settings/livepreview.go`): every move in the theme enum's option
  list — arrows, page jumps, wheel, filter narrowing — arms a debounced
  preview of the highlighted theme. Comparing two schemes no longer means
  enter, look, reopen, repeat.
- **Debounced, one generation at a time** (`browsePreview` / `PreviewTick`,
  `previewDebounce` = 60 ms): each move bumps a generation and arms a
  `PreviewTickMsg`; only the newest deadline still matches, so a held `j`
  through eighteen themes re-threads one palette instead of eighteen.
- **Rollback that cannot be forgotten** (`CancelPreview`): esc in the list,
  tab out of the detail column, the panel's close paths and the app's
  overlay-takeover closes all route through it, restoring the *effective*
  value the browse started from (staged, else live). A browse abandoned before
  its first deadline applied nothing and rolls nothing back.
- **Browsing is not staging**: a previewed value never enters the staging
  buffer, so it can never reach disk. `enter` (and a click) calls
  `keepPreview` and hands the value to the normal staged write-back, which
  re-emits it as its own preview — no flash of the old theme in between.
- **Open buffers re-color in place**: the preview reuses the existing
  `settings.PreviewMsg` → `previewTheme` → `applyTheme` path, so
  `editor.SetPalette` rebuilds each tab's capture table and bumps the render
  epoch; syntax spans recolor without reopening anything.
- **Wheel plumbing**: `wheelEditor.Wheel` and `settings.Model.Wheel` now
  return a `tea.Cmd`, because scrolling an option list can have an effect
  beyond redrawing it.
## 2026-08-28 (Help overlay: multi-column layout restored in the context view, #2215)

- **The column width now aims at the typical entry, not the longest one**
  (`internal/help/layout.go`, `help.go`). Since #2182 the context view lists
  *every* scope at once, so the widest rendered cell was drawn from hundreds of
  commands: one verbose title pushed the shared column width past half the
  terminal, only a single column fit, and the overlay degraded to one endlessly
  tall column in every section. `TypicalColumnWidth(cells, configMin, pct)`
  answers "how wide must a column be so almost nothing is clipped" (help asks
  for 90%), and the new `ColumnLayout(width, natural, floor, maxCols)` takes the
  largest column count whose columns stay at or above that floor, shrinking them
  to their fair share of the budget when the natural width does not fit. On a
  120-cell budget the sectioned sheet goes from 1 column / 62 rows back to 2
  columns / 39 rows.
- **Overlong rows are ellipsised, never wrapped** (`renderEntry`): the *title*
  is truncated with `…` so the row stays one line and its shortcut stays
  visible — the shortcut itself is never cut. A column with less than
  `minTitleWidth` room left for a title keeps the untruncated row and lets it
  overflow, since a bare `…` says nothing; that is also what keeps very narrow
  budgets rendering as before.
- **Unchanged:** the section structure and headings from #2182 (focused context
  first, then global, then the rest), the `tab` view cycle, the two-column cap,
  and the single-column fallback on genuinely narrow terminals. `ColumnCount`
  is gone — `ColumnLayout` subsumes it and nothing else called it.
- Tests: `internal/help/columns_test.go` covers `TypicalColumnWidth`,
  `ColumnLayout`, the entry truncation, and the sectioned render at a wide and a
  narrow budget. Wiki: [Help Overlay](/architecture/help-overlay.md).
## 2026-08-28 (Change feed: batch reload/revert and per-process grouping, #2183)

- **Batch actions** (`internal/app/changefeed.go`): `A` reloads and `V` reverts
  a whole scope instead of a row. The scope is the marked rows when there are
  any and the entire listed feed otherwise — marking refines a batch, it is not
  a precondition for one. `space` marks a row, `m` marks the selection's whole
  group, which is what makes a titled section's reload/revert one keystroke
  without a second set of group-only keys; `m` unmarks only a fully marked
  group, so it completes a partial selection rather than discarding it.
- **Conflicts are never resolved by a batch** (`changeFeedConflict`): a file
  changed externally *and* edited in IKE is left alone and named in the report
  (`reloaded 3 file(s) · skipped 2 (unsaved changes): a.go, b.go`). The
  per-entry `R`/`r` still exist for settling them one at a time. Reload also
  skips what it cannot reach (not open, gone), revert what has no recorded
  previous version.
- **Revert-all is confirmed with the file list** (`openChangeFeedRevertAllPrompt`):
  every file it will touch, plus what it is leaving alone and why. Undoing one
  external write is a decision about one buffer; undoing an agent run is a
  decision about the working tree. Confirmed, each file takes the same restore
  path as the single revert (`revertChangeFeedPath`, now shared) and the batch
  reports once — `applyBufferRestore` (split out of `restoreLocalHistory`) is
  the quiet restore that makes one report possible instead of N toasts.
- **Per-process grouping** (`internal/changefeed`: `Entry.Source`, `Groups`,
  `Attributed`): entries carry a best-effort attribution and render as titled
  sections, attributed groups first, the unattributed rows last under a plain
  `unattributed` title. With nothing attributed the list stays flat.
- **Attribution is only what IKE spawned** (`changeFeedSource`,
  `terminal.Model.Argv`): a *busy* tool pane, Run task or terminal command at
  the moment of the write. Exactly one candidate is an answer, two are not —
  an ambiguous moment stays unattributed rather than blaming the formatter
  that ran beside the agent. Resolved once per watcher flush.
- Docs: `wiki/architecture/change-feed.md` (new "Batch actions" section,
  attribution and grouping).

## 2026-08-28 (Diff viewer: ignore-whitespace toggle, per-side intra-line emphasis, #2170)

- **Ignore whitespace** (`internal/diff/engine.go`): `ComputeWith(left, right,
  Options{IgnoreWhitespace: true})` compares lines by their whitespace-stripped
  key, the way `git diff -w` does. A re-indented or re-wrapped line pairs up as
  an unchanged row and drops out of the hunk list entirely, so a reformat-heavy
  branch stops drowning the changes that carry meaning. The option changes what
  is *compared*, never what is shown: each column keeps its own raw text, which
  is why the edit script now carries both sides of an equal pair (`pairEdit`)
  instead of one text. Intra-line refinement follows suit — every span is
  trimmed to its non-whitespace core and the ones left empty are dropped, so a
  re-indented *and* renamed line emphasizes the identifier, not the leading run.
- **`w` toggles it live** (`model.go`, `recompute`): the pane re-diffs the
  retained texts in place — scroll position kept, current hunk clamped to the
  shorter list — and reports itself as `diff.IgnoreWhitespaceMsg`. The root
  model persists that into `diff.ignore_whitespace` (`config.WriteAndReload`,
  the #1998 conceal-rule shape) and the reload re-applies **both** diff keys to
  every open diff pane through `Instance.configure`, so panes never drift apart
  and the next diff starts on the last choice. The state is visible where it
  matters: `DIFF │ … │ -w │ hunk i/n` in the status line, `[-w]` in the title
  band.
- **Intra-line emphasis is per side now** (`theme`, `DiffAddedEmph` /
  `DiffRemovedEmph`): a changed range used to take the single `DiffChanged`
  slot, which painted both halves of a changed pair the same colour — the
  refinement read as noise rather than as "this is what changed on *this*
  side". Each side now emphasizes in its own hue, one step stronger than its
  line background. Every builtin declares both slots; a theme that omits them
  gets them derived from its own diff backgrounds (pushed toward
  `Success`/`Error`, then pulled back inside `emphHeadroom` of the line
  background's own drift from `Surface` — the contrast audit covers the new
  slots). The step stays inside the readability envelope by design, so the
  renderer carries the rest as **bold** on the emphasized runes; underline was
  the other candidate and is out because lipgloss emits it per grapheme.
- **Settings** (`internal/settings/schema.go`): a new **Diff Viewer** page
  exposes `diff.ignore_whitespace` and `diff.context` — the latter was read by
  the pane registry but had no typed schema entry at all, so it was
  file-only-and-undocumented until now. Docs: `wiki/architecture/diff-viewer.md`
  (new "Ignore whitespace" section), `themes.md`, `settings-ui.md`.

## 2026-08-28 (Notifications: the history ring reads as a notification center, #2152)

- **Relative ages** (`internal/app/notifications.go`, `historyView`): the
  history list swapped its `HH:MM:SS` stamps for a right-aligned relative age
  off `ui.ShortAge` — "now", "5m", "3h", "4d". An expired toast is read back
  by *how long ago* it happened, which is the question being asked; the
  absolute clock time never was.
- **Clear all** (`updateNotifCenter`, `c`): the center empties its ring and
  reads "no notifications yet"; the view footers `[c] clear all   [esc]
  close`. Only `c` is consumed — everything else falls through to the shell,
  so its scrolling and Esc keep working untouched.
- **Identified by content, not a flag** (`notifCenterOpen`): the key route
  tests the shell's content heading (`notifCenterTitle`) instead of a
  `notifCenter bool`. A flag left standing when a different dialog takes the
  shell would hand that dialog's `c` to clear-all; a heading cannot go stale.
- **Live body** (`openNotifCenter`, `bindNotifCenter`): the shell content is
  the `historyView` method value rather than a string snapshotted at open, so
  ages tick while the center is open. Clear-all re-binds — the method value
  closes over a model copy, so a cleared ring needs the fresh one.
- The center was already reachable (palette, `cmd+alt+n`, Help menu, a click
  on the `●` counter, which resets on open) — #2152 is what makes it worth
  opening. Docs: `wiki/architecture/notifications.md` (renamed section),
  `status-line.md`, `userdocs/troubleshooting.md`.

## 2026-08-28 (Undo tree: diff preview, relative timestamps, time jump, #2143)

- **Diff preview** (`internal/undotree/preview.go`): the undo-tree overlay is
  no longer a list of opaque states. Under the list it renders the inline diff
  from the current buffer to the selected state — `-` lines vanish and `+`
  lines appear when you restore it — so you can read a node before jumping to
  it. The content comes from `History.ContentAt` (new `contentat.go` in
  `internal/editor/history`), which replays the same inverse/forward walk
  `JumpTo` does on a *scratch copy* of the buffer: the preview never moves the buffer or the
  history, and a state broken out of the tree by pruning reports itself
  instead of panicking. Rendering keeps one context line around each change,
  marks the skipped regions `@@`, caps itself to a quarter of the screen
  (3–10 rows, remainder as `… N more lines`) and memoizes per state seq, so
  walking the list with `j`/`k` replays each state once; `SetNodes` (i.e. a
  jump) drops the cache because every diff is then stale.
- **Relative timestamps**: rows show the state's age (`just now`, `5m ago`,
  `2h ago`, `3d ago`) instead of a wall-clock time — orientation in an undo
  tree is about *how far back*, not about what o'clock it was. The root state
  predates the history and stays blank.
- **Time jump** (`t`, `internal/undotree/timejump.go`): type an age in minutes
  and `enter` restores the newest state that is at least that old. The
  untimestamped root always qualifies, so an age larger than the whole session
  lands on the original content rather than failing; `esc` cancels the prompt
  without closing the overlay, and the prompt swallows list keys so a stray
  `j` cannot move the selection mid-typing.

## 2026-08-28 (Merge tool: conflict navigation, resolution chords, counter, offer/finish flow, #2258)

- **Resolution chords** (`internal/editor/conflict.go`, `keys_normal.go`): the
  `merge.*` commands stopped being palette-only. `go` / `gt` / `gb` accept
  ours / theirs / both on the block at the caret and `]n` / `[n` cycle the
  remaining blocks with wrap-around, joining the `]c` / `[c` hunk family. The
  resolutions are not motions — a pending operator (`dgo`) cancels — and off a
  conflict block they only notice, so the plain letters stay harmless in
  ordinary buffers.
- **Keep manual edit** (`merge.keepManual`, `gm`): the fourth resolution, for
  a block hand-merged inside the result buffer. It drops **only** the marker
  lines, keeping everything between them verbatim — the diff3 base section
  included, which is what distinguishes it from accept-both: after a manual
  merge those lines are the user's. `acceptConflict` and it now share
  `replaceConflict`, so all four spell the one-undo-unit buffer surgery once.
- **Counter** (`ConflictIndexAtCursor`, `merge.Model.ConflictIndex`): the
  merge header reads `⚠ conflict 2/3 · 3/5 unresolved` and the status line's
  `MERGE` segment the same, both flipping to an all-resolved reading that
  names `vcs.mergeApply`. Both come off the cached block scan, so an undo that
  brings a conflict back is reflected without a re-scan per frame.
- **Offer and finish** (`internal/app/merge_view.go`): opening a file git
  reports as conflicted raises the centered **Conflicted file** offer —
  `m`/enter opens the three-way view, esc leaves the markers in the editor,
  once per path per session. When a view's last conflict goes, the settled
  Update pass (`syncMergeFinish`) raises **Merge complete** — `s`/enter saves,
  stages and closes. It watches the count rather than one key route, so it
  fires however the block was resolved and re-arms after an undo.
- **Marker guard** (`ConflictMarkerLines`): the block scan only sees complete
  marker runs, so a half-edited block counts zero conflicts while still
  poisoning the file. The finish offer and `vcs.mergeApply` both check the raw
  marker lines as well — a buffer walk run on the transition to zero and
  before a write, never per frame — so a written result is marker-free.
- Wiki: [Diff Viewer](/architecture/diff-viewer.md) (three-way merge view),
  [Editor](/architecture/editor.md), [VCS](/architecture/vcs.md),
  [Status Line](/architecture/status-line.md),
  [Keybindings](/architecture/keybindings.md).

## 2026-08-28 (Mouse audit: uniform wheel / click / double click, #2259)

- **Shared list-mouse layer** (`internal/ui/listmouse.go`): `WheelWindow`
  (clamped scroll that drags the cursor along), `RowAt` (content-local row
  hit-test) and `ClickTracker` (`DoubleClickWindow` = 400 ms), adopted by the
  explorer, VCS, Problems, Usages, Structure, Breakpoints, Test Results,
  GitHub Issues, DOM inspector, Archive viewer, Data viewer, and both doctor
  panes. A dozen packages had carried their own copies.
- **Two behaviour fixes fell out of the merge**: the wheel no longer scrolls a
  list's last page off the screen (nine panes used `maxTop = n-1`; the archive
  and data viewers' `n-height` is now the rule), and a click on the footer row
  or on the blank tail under a short list no longer selects a row (Structure,
  the DOM inspector, the VCS changes list and GitHub Issues had no upper bound
  on `y`).
- **Gaps closed**: the Elasticsearch console (`internal/espane/mouse.go`) and
  the SFTP remote browser (`internal/remote/mouse.go`) had no mouse handling
  at all and now scroll, select and activate like the data viewer and the
  archive viewer they mirror; the merge view gained a wheel that moves all
  three columns (`internal/merge`); a floating-shell picker's viewport now
  scrolls with the wheel (`ui.Floating.Wheel`).
- **Tabs**: middle-click-closes reached the popup terminal box and the
  floating panels (`popupBoxTabAt` / `floatPanelTabAt` now back both the left-
  and middle-click paths), where only the `✕` zone had closed a tab.
- **Convention and matrix**: /architecture/mouse.md records the one rule every
  surface obeys — wheel scrolls (clamped), single click focuses and selects,
  double click activates, affordances answer to one click, chrome is inert —
  the tab rules, the looser single-click-picks rule transient overlays keep,
  and the audited surface×gesture table with before/after state. Row clicks
  inside floating-shell pickers stay open — the shell sees its content as
  opaque text — and are named there as such, tracked in #2275.

## 2026-08-28 (Terminal completion: type-aware trailing space on accept, #2261)

- **Candidates carry their source** (`internal/terminal/complete.go`): the
  popup's items are `candidate{text, kind}` instead of plain strings — the
  filesystem, PATH and Makefile scanners tag each entry `candDir` or
  `candFinal`, and the tag rides through to the accept.
- **The accept follows the kind** (the zsh/fish rule): a directory keeps its
  trailing `/` and nothing more, so completion continues inside it; a file, a
  PATH executable or a make target is a finished token and gets a **trailing
  space**. An explicitly picked candidate that is a strict prefix of other
  still-matching ones counts as final too — only directory semantics suppress
  the space. Both accept paths (tab, and enter on a focused popup) share it.
- **No doubled space**: `spaceFollowsCursor` reads the joined soft-wrap chain
  forwards from the cursor, so accepting mid-line (or on a row's last column,
  where the next cell lives on the continuation row) inserts the candidate
  alone.
- `lineBeforeCursor` now pads the right-trimmed cursor row back to the cursor
  column, so a trailing space — accepted or typed — is visible to
  `parseCmdline` and the finished word parses as a fresh empty one.
- See /architecture/terminal.md.

## 2026-08-28 (Data viewer: column sort and CSV/JSON export, #2248)

- **Column sort** (`internal/datasrc/sort.go`, `internal/dataview/sort.go`):
  `S` in the grid — or `data.sortColumn` — cycles the focused column through
  ascending, descending and unsorted. The pane hands a `datasrc.Sort` to
  `PageWhere`, whose signature grew it; every engine renders the same
  `ORDER BY` **outside** the filter's subquery, so it composes with a clause
  that already ends in an `ORDER BY` or a `LIMIT` and paging walks the sorted
  result. A new order restarts the walk, the count cache is untouched
  (ordering changes no count), and switching tables drops the sort like the
  filter. Parquet borrows the duckdb CLI for it, as it already does to filter.
- **Export** (`internal/datasrc/export.go`, `internal/dataview/export.go`):
  `E` — or `data.export` — writes the filtered, sorted result to a CSV or
  JSON path, the format taken from the extension. The writer uses nothing but
  the `Source` interface (it pages `PageWhere`), so all three engines export
  through the same code; it streams in 2000-row batches, caps at
  `ExportLimit` (1 000 000) and reports `Capped` so the toast can say so. CSV
  goes through `encoding/csv` with `NULL` as the empty field; JSON is a
  streamed array of objects with `NULL` as `null` and every value a string.
  The line refuses an unknown extension and a missing directory before
  scanning anything, and warns once before overwriting.
- The SQLite backend now opens **four** connections, the fourth being the
  export's, so a long write never queues page fetches behind it.
- See /architecture/data-viewer.md.

## 2026-08-28 (Per-file test coverage: Test Results listing and status segment, #2246)

- **Per-file percentages** (`internal/coverage`): the store gained
  `FilePercent(path)` — one file's executed-over-tracked ratio plus its
  staleness flag — and `FileStats()`, the whole run summarized worst coverage
  first (path-sorted within an equal percentage, untracked files dropped).
  `Percent()` now shares the same counting helper.
- **Test Results detail** (`internal/testresults`): `c` swaps the detail
  column for the run's per-file breakdown — the run-wide figure, then one
  right-aligned percentage per file with its path relative to the run
  directory. `SetCoverage(pct, files)` carries the breakdown in as
  `CoverageFile` values, so the panel still imports nothing from
  `internal/coverage`; `StartRun` drops listing and mode, so a plain re-run
  leaves nothing stale behind. The footer only advertises `c` once a coverage
  run has filled it.
- **Status segment** (`internal/app/coverage.go`, `statusline.go`): the
  focused file's percentage as `cov 82.4%`, suffixed `stale` after an edit.
  Opt-in via the new `tests.coverage_status` setting (Settings → Tests, off by
  default) and independent of `coverage.toggle` / `editor.marks.coverage` —
  those quiet the gutter, not the figure.
- The gutter bars, the toggle and edit-driven staleness themselves landed with
  #2081; see /architecture/test-results.md and /architecture/editor.md.

## 2026-08-27 (Follow mode: live filter and highlight over tailed output, #2255)

- **Filter line** (`internal/editor/logfilter.go`): `view.followFilter`
  (`alt+shift+g`, palette) opens a `|` command line over a followed buffer —
  every non-matching line is hidden, existing content and streamed appends
  alike, and the auto-scroll sticks to the *filtered* tail. Typing is live;
  `view.followHighlight` (`*` line) is the same pattern without hiding, with
  matches on their own warning-tinted background; `view.clearFollowFilter`,
  an emptied pattern, Esc or leaving follow mode restores the stream. The
  pattern language is the search line's (`\v` regex — `ctrl+r` toggles the
  marker — `\c`/`\C` case), except that a broken regex reports inline instead
  of being demoted to a literal.
- **Follow semantics**: "the end" is now the last *visible* line, so
  `followToEnd` and the pause predicate follow the filtered tail; the
  viewport test counts visible rows, since hidden lines let more of the
  buffer fit than the window height suggests. Hiding rides `lineHidden`, so
  motions, scrolling and mouse mapping need no special case; a merged rotated
  log set filters the same way.
- **Badge & cost**: the status line shows `FILTER error (12)` /
  `HIGHLIGHT ~pat (3)` / the compile error next to `FOLLOW`. The match count
  is cached per document version and extended over appends
  (`logFilterState`), never recounted per poll (#2163).
- Docs: [follow mode](/architecture/follow-mode.md), the keybinding matrix
  and the generated command/keybinding reference.

## 2026-08-27 (Breakpoint properties: form, glyphs, capability gate, #2245)

- **Properties form** (`internal/app/breakpoint_form.go`): condition, hit
  count and log message of one breakpoint in the shell dialog the
  run-configuration form uses — `p` on a Breakpoints-list row and
  `debug.breakpointProperties` (cmd/ctrl+alt+f8, palette, Run menu) on the
  editor's cursor line, JetBrains' *Edit Breakpoint*. The list's inline
  `c`/`n`/`l` editors stay for one-field edits.
- **Validation next to the store** (`internal/debug/validate.go`): both
  surfaces reject the same input with the same wording before it reaches an
  adapter — a whitespace-only field ("empty but enabled"), a hit count that is
  not a number optionally prefixed by `>`/`>=`/`<`/`<=`/`==`/`%`, an
  unbalanced/empty/nested `{}` in a log message. A rejected value keeps its
  editor open with the reason.
- **Distinct glyphs** (`Meta.Kind`): `◉`/`◎` conditional and `◆`/`◇` logpoint
  next to the plain `●`/`○`, in the gutter and the list alike — the editor
  gets two more injected line sources, the paused `▶` still wins.
- **Capability gate**: the three fields follow the live adapter's
  `supportsConditionalBreakpoints`/`supportsHitConditionalBreakpoints`/
  `supportsLogPoints`. A gated field is refused with a notice (list) or drawn
  read-only as `(unsupported by adapter)` (form), and session start warns once
  when the store holds refinements this adapter will strip. The breakpoints
  are pushed either way — an unsupported capability never breaks a session.

## 2026-08-27 (jq playground keyboard: esc esc, code actions, its own help context, #2237)

- **`esc esc` reaches the palette from the playground.** The mode's `esc`
  returned straight out of the modal routing, which sits above the double-esc
  detector in `Update`: the first press left the input, the second found
  nothing armed, and the palette was unreachable from the query line.
  `leavePlaygroundOnEsc` closes the mode *and* arms the detector, so the single
  `esc` keeps its immediate meaning (no chord-timeout latency on the most-pressed
  key) and the second one opens the palette. Both focuses go through it; an
  `esc` the completion popup consumes as a dismissal does not arm it.
- **Code actions answer instead of doing nothing.** `alt+enter`
  (`lsp.codeAction`) is editor-scoped, so the mode's routing kept it from the
  keymap layer and the Global fallback never matched — a silent no-op. There is
  no language server behind a jq program, so the playground now says so on the
  info row, in the status line's warning colour, and names `ctrl+space` and
  `ctrl+l` as the nearest things it does have.
- **The cheatsheet knows the playground.** `helpContext` reports `playground`
  while the mode owns the keyboard (help only — keymap resolution, palette
  scoping and the mode indicator keep `focusContext`), and `playhelp.go`
  contributes three groups: query line, result buffer, and the keymap chords
  that still reach the mode, resolved live from the binding table.
  `help.withExtraLeading` puts `Focused` extra groups at the head of the
  context view, so a mode owning the keyboard without owning a registry scope
  leads the sheet like a focused pane would.
- **Discoverability**: the info row's hint tail ends in `f1 keys`, last so a
  narrow pane drops it first.
- Docs: [jq & yq playground](/architecture/jq-playground.md#keys),
  [help overlay](/architecture/help-overlay.md#contexts-without-a-registry-scope-2237).

## 2026-08-27 (Terminal link hints + extensionless references, #2254)

- **Keyboard hints** (`internal/terminal/hints.go`): the reserved
  `cmd+shift+l` labels every resolvable `file:line` reference on the visible
  rows with a single home-row character; typing one opens it through
  `openPathAt`, esc / any unassigned key / a resize / a blur close the mode,
  and no key reaches the shell while it is open. Labels beat next/prev
  stepping here: a compiler run prints a screenful at once and the wanted
  reference is rarely the newest. Reserved in the popup layer too.
- **Extensionless names** (`linkNames`): `Makefile:12`, `src/Dockerfile:4`,
  `LICENSE:7` … now detect, off a closed case-sensitive list. RE2 has no
  lookbehind, so the regex consumes a left boundary outside the path capture
  group — otherwise the fixed names would match inside longer tokens
  (`foo-Makefile:3`). Timestamps and `host:port` stay out.
- **Performance posture unchanged** (#1168): the existence `os.Stat` runs at
  the user gesture (click, hint activation), never per render — `linkStat` is
  the seam a test counts through.

## 2026-08-27 (Organize imports on save: one undo unit and an on-demand command, #2253)

- **One undo unit for the save chain**: with both on-save steps enabled the
  organize-imports and format rewrites used to leave two undo levels, so
  undoing a save took two `u`. `history.Amend` merges a change into the one
  that produced the current state (appending forwards and inverses, refusing
  on a state with child branches), `ApplyTextEditsAmend` uses it while the
  buffer still sits on the anchor the previous `ApplyTextEdits*` left, and
  `FormatEditsMsg.Amend` carries the request from the bridge: the format step
  amends only when the organize step actually rewrote the buffer. Anything in
  between (a keystroke, an undo, a timed-out or command-only organize step)
  degrades to a plain push — never a wrong merge.
- **On-demand `lsp.organizeImports`** ("LSP: Organize Imports", palette and
  the editor context menu): the same capability-gated, 2 s-time-boxed step
  without a save behind it, toasting when nothing is focused or the server
  does not offer the kind.
- Docs: [editor](/architecture/editor.md#format--organize-imports-on-save-1148),
  [lsp](/architecture/lsp.md), the code-intelligence guide and the generated
  command reference.

## 2026-08-27 (Intention popup: diff preview of the highlighted action, #2252)

- **Preview under the list**: the intention popup renders a small inline diff
  of the row the highlight rests on (`internal/app/actionpreview.go`), through
  the same `miniDiffLines` renderer local history and the rename confirmation
  use. Rows with no resolvable edit — commands, pickers, side effects — read
  "no preview"; an edit that changes nothing says so.
- **LSP lazy resolve (#2252 needs it)**: `codeAction/resolve` is implemented
  (client capability `dataSupport` + `resolveSupport: ["edit"]`,
  `Capabilities.CodeActionResolve`, `Manager.ResolveCodeAction`). One offer is
  an `actionSet` (`plugins/lsp/codeaction.go`) that resolves a row once and
  keeps it, so previewing and then applying share a single round trip and
  apply produces exactly the previewed edit. Applying an unpreviewed lazy
  action now resolves instead of reporting "no edit".
- **Provider seam for built-ins**: `intention.Context.Preview(commandID)` plus
  `Context.PreviewFor(id)` wire a lazy `Item.Preview` closure; the value
  toggle and the three merge accepts opt in through read-only editor probes
  (`ToggleValuePreview`, `ConflictPreviewAtCaret`) that mutate nothing.
- **Palette seams**: `SelectionMode` (debounced `SelectionChanged`, 120 ms,
  stale ticks dropped) and `FooterMode` (lines under the list behind the same
  dim rule), plus a click guard so the footer area is inert.

## 2026-08-27 (Local-only usage telemetry, #2235)

- **New subsystem** `internal/telemetry`: a `Recorder` appending usage events
  as JSONL, one file per session under `$IKE_CONFIG_DIR/telemetry` or
  `~/.ike/telemetry`. Events carry `v` (schema version 1), `ts`, `sid`, `type`
  (`command`/`key`/`layout`) and a structural payload — never typed text,
  file contents or clear-text paths. Writes are asynchronous (buffered
  channel + writer goroutine, full buffer drops); growth is bounded (5 MiB
  per-session cap, directory pruned to the newest 20 sessions).
- **Hooks at the existing funnels**: `dispatchCommandFrom` carries the
  invocation source (palette/menu/keybind/mouse/internal) through the single
  command-dispatch funnel; `resolveKeymap` records resolved/blocked/unbound
  chords (unbound only for command-modified chords and function keys — the
  privacy line); split, pane move/focus, resize, tab switch/move and project
  switch record layout events. The recorder is session state: it rides across
  project switches and closes in the quit path.
- **Setting** `telemetry.enabled` (default on) with a Usage Telemetry
  settings page; read live per event, off writes nothing.
- New concept doc: [usage-telemetry](/architecture/usage-telemetry.md);
  schema documented there as the stable analysis interface.

## 2026-08-27 (HTTP history: re-run requests and diff against the previous run, #2247)

- **The gap**: the response history stored every run (#1251), the diff engine
  compared two of them (#1992, #2060) — but the loop between them was open.
  Re-running a request from its history meant going back to the `.http` file
  and finding the block, and the comparison that followed drowned in `Date`
  and request-id lines that differ on every single run.
- **`R` in the response pane** (palette: `http.rerun`) re-runs the shown
  entry's request from its `.http` file with the **current** variables and
  environment — the difference to `ctrl+r`, which repeats the stored bytes.
  It re-reads the open buffer when there is one, so unsaved edits count, and
  a block that was renamed or deleted since is named as such instead of
  guessed at.
- **The comparison follows by itself**: `http.diff_after_rerun` (default on)
  arms the dispatch, and `fillHTTPPanel` opens previous-vs-new the moment the
  answer is stored — the `P` path (#2060), now reached by the re-run. The
  mark is taken up front, so a failed or canceled re-run disarms too; a
  first-ever run has nothing to compare with and stays silent.
- **Volatile headers are filtered**: `http.diff_ignore_headers` (`date`, the
  request/trace ids, timing fields, `x-amz-*`-style families) keeps the noise
  out of every response diff via `httpdiff.TextFiltered`, and the notice says
  how many headers were dropped so a filtered header never passes for an
  unchanged one. Both settings live on the new **HTTP Client** settings page.

## 2026-08-27 (Problems pane: apply quick fixes from the entry, #2175)

- **The gap**: the Problems pane could navigate to a diagnostic but not fix
  one. Every fix meant jumping to the location first and pressing alt+enter
  *there* — the pane knew exactly which finding you meant and then made you
  go find it again.
- **`a` (or `alt+enter`) on a row** now lists that diagnostic's code actions
  and applies the pick, in a picker anchored under the marked row. A file
  header stands for its first diagnostic (like `enter`); a
  related-information row stands for its **parent** — the finding is what a
  server offers fixes for, the "declared here" note is not.
- **The seam is the intention seam, entered without a caret**: the pane emits
  `problems.QuickFixMsg`, the app runs `lsp.quickFixProblem`, which — like
  `project.goToClass` — answers with a continuation
  (`ilsp.QuickFixPromptMsg`) rather than asking anything itself. The app
  calls it with the row's path and the diagnostic's own range
  (`ilsp.QuickFixRequest`), so the request *carries* its location instead of
  reading one off a cursor. That is the whole reason no jump is needed. The
  command is in the palette too, meaning the same thing there.
- `bridge.quickFixAt` sends the ordinary `textDocument/codeAction` with the
  cached overlapping diagnostics as context and replies with a
  `CodeActionsMsg` carrying `QuickFix` — the flag that tells the app to skip
  the intention merge (no caret, so no provider could apply) and to report
  "no quick fixes for this problem" on an empty offer, which is the honest
  answer for a lint note, a task-matcher finding, or a file no server tracks.
- **Applying reuses the existing tail wholesale**: `applyAction` →
  `workspace_edit.go`, so an open buffer gets one `FormatEditsMsg` applied as
  a single undo unit (`u` takes the fix back) and a closed file is rewritten
  on disk, exactly as the intention path does it. Nothing refreshes the list
  by hand — the edit makes the server republish and the pane re-derives, the
  pure-consumer rule intact.
- The popup's on-screen fit (shift left at the right edge, flip above when it
  would cross the bottom) moved out of `caretPopupAnchor` into
  `fitPopupAnchor`, shared with the pane anchor: the two place their row
  differently but must land on screen identically.

## 2026-08-27 (Debugger: persisted watches and the evaluate popup, #2174)

- **The gap**: watches (#1914) lived in memory only — a restart lost the list
  — and there was no way to evaluate an ad-hoc expression at all. Nothing
  gated either on the adapter actually implementing `evaluate`.
- **Watches persist per project** (`debug.Watches`,
  `internal/debug/watches.go` → `.ike/watches.json`, `IKE_CONFIG_DIR`
  override): loaded at start beside the breakpoint store, saved after every
  add/edit/remove with a warning toast on failure. The store owns the "an
  emptied expression removes the row" rule and treats out-of-range indices as
  no-ops, so the panel and the store never have to agree on indices to stay
  safe. A missing or malformed file loads empty.
- **Evaluate popup** (`debug.evaluate`, **alt+f8** — JetBrains' chord, plus
  the palette and the Run menu): a visual selection is flattened to one line
  and evaluated straight away; with nothing selected a single-field shell
  prompt asks for the expression. The answer opens in a cursor-anchored popup
  (`internal/editor/evalpopup.go`) in the hover/peek frame, composited first
  in `compositeLSPPopups` because it is explicitly invoked and
  keyboard-driven.
- **Structured results page in on demand**: a non-zero `variablesReference`
  starts collapsed, and expanding runs `editor.EvalExpandMsg` →
  `fetchEvalChildren` → DAP `variables` → `debugEvalVarsMsg` →
  `SetEvalChildren`. Deliberately a separate message from the panel's
  `debugVarsMsg` — the two trees expand independently and a shared message
  would cross them. The editor stays protocol-free: rows cross as
  `editor.EvalVar`, the seam `editor.DebugLocal` already uses.
- **The capability gate is discovered, not advertised**: DAP has no
  `supportsEvaluate` flag, so `Session.SupportsEvaluate()` is optimistic until
  an adapter refuses the request as unimplemented — then the verdict latches
  and every later call answers `dap.ErrEvaluateUnsupported` without a round
  trip. `unsupportedRequest` classifies the message so a *bad expression*
  never counts: one mistyped watch must not disable watches. With the latch
  on, the Watches header reads `evaluate unsupported` (expressions stay listed
  and editable), `debug.evaluate` refuses before opening its prompt, and the
  explanation is notified once per session (`debugState.evalNoticed`).
- **`supportsEvaluateForHovers`** *is* a real flag and is decoded: a selection
  evaluates with the `hover` context where the adapter promises it, `repl`
  otherwise.
- **The result never outlives its frame**: `clearPausedMarker` closes the
  popup in every editor view, the rule the paused marker and the inline values
  already follow. Any key the popup does not own dismisses it and falls
  through, so a popup key never reaches the buffer.

## 2026-08-27 (LSP peek: pick between definitions inside the popup, #2168)

- **The gap**: peek definition (#1154) already showed a single target inline,
  but a symbol with several definitions fell back to the #279 modal candidate
  picker — a peek that opens a palette has given up the one thing it promised,
  staying where you are.
- **All candidates in one popup** (`openPeekCandidates`,
  `internal/app/peek.go`): every candidate's bounded excerpt is read up front —
  live buffer for open files, bounded disk scan otherwise, unreadable ones
  dropped — and handed to a single `OpenPeekTargets` call. Nothing readable
  left notifies instead of opening an empty box.
- **The popup grew a selection** (`internal/editor/peek.go`): a `(2/3)`
  counter in the header, a 4-row candidate window under the excerpt that
  follows the selection (titles truncated from the left, like the header, so
  `file:line` survives a long path), `tab` / `shift+tab` to cycle with wrap,
  and each candidate starting at the top of its own excerpt. Enter jumps to
  the *selected* candidate through the unchanged `DefinitionMsg` funnel, so
  nav history records exactly as before; esc still just closes.
- **`tab` is swallowed even with one candidate** — a peek must never leak an
  indent into the buffer — while every other unowned key keeps closing the
  popup and falling through.
- **The picker is the overflow path, not the default**: above
  `peekCandidateMax` (12) targets the answer still opens `refsMode.SetPeek`,
  where a filter beats scrolling and 13+ excerpt reads are not worth it.

## 2026-08-27 (Sticky scroll from LSP symbols, #2167)

- **The gap**: the editor breadcrumbs half of #2167 shipped with #1153 — the
  `file ▸ symbol ▸ child` row, its `editor.breadcrumbs` toggle, the shared
  per-path symbol cache. What was still missing is the issue's sticky variant
  for languages the Tree-sitter side cannot serve: sticky scroll (#168) reads
  `lang.Language.ScopeNodes`, so Rust, Java, C# — anything with no grammar
  plugin, and every language in a CGo-less build — pinned nothing, even with
  a language server answering `documentSymbol` all along.
- **Third consumer of one cache** (`internal/app/symbolscopes.go`): the tree
  `applyDocumentSymbols` already caches per path is converted to
  `highlight.Scope`s in pre-order — single-line symbols dropped, since their
  header can never scroll out — and pushed into the editor views showing the
  file. A reply installs them in the pass it lands in; a settled-pass sync
  fills the views a reply could not reach (a pane that opened the file later,
  a tab switched back to one). No request is added: `structureSyncCmd` keeps
  its per-path dedup and now merely stays alive when the Structure pane is
  closed *and* breadcrumbs are off.
- **Tree-sitter always wins** (`editor/sticky.go`, `stickySource`): the parse
  needs no server and follows every keystroke, so the symbols only fill the
  gap where it produces nothing. The fallback scopes carry the path they were
  derived for, so a buffer that loads another document ignores a stale
  delivery without a reset call, and `symEpoch` keys the header memo (#2187)
  so a post-save refresh is seen.
- **Config** `editor.sticky_scroll_symbols` (bool, default on, settings panel
  entry); `editor.sticky_scroll` still gates both sources
  (`/architecture/editor.md`, `/architecture/structure-view.md`).

## 2026-08-27 (Keydoctor: dead bindings in the active keymap, #2161)

- **The gap**: the reachability table knew which chord families terminals eat,
  but nothing applied that knowledge to the keymap a user actually runs. Dead
  bindings were discovered the way they always are — by pressing the key and
  getting nothing.
- **A verdict, not a class** (`TerminalEnv.Deliverability`,
  `internal/keymap/deadchords.go`): `Fragile` covers both "needs a terminal
  setting" and "certainly swallowed here", and only the second is worth acting
  on. The known-dead patterns are plain `ctrl+fN` on darwin, `ctrl+tab` under
  tmux, and `alt+<character key>` in a macOS terminal whose default composes
  the Option layer — with probed truth overriding all of them in both
  directions.
- **The report** (`internal/keydoctor/report.go`, `keymap.deadBindings`, or
  `r` from a finished probe run): every unreachable binding with its reason,
  dead ones first, one suggested chord per finding and `tab` for the next.
- **Suggestions are conflict-checked across the whole keymap**, not just the
  binding's own context — the write-back is an unqualified
  `keymap.bindings.<chord>` override, which claims the chord everywhere — and
  no two findings in one audit offer the same chord.
- **Applying** goes through the ordinary customization path
  (`internal/app/deadbindings.go`): bind new, unbind old, one reload; the open
  report re-audits on that reload, so the list shrinks as it is repaired.

## 2026-08-27 (LSP rename: preview multi-file edits before applying, #2149)

- **The gap**: `lsp.rename` applied the server's `WorkspaceEdit` the moment it
  arrived. A rename reaching into files the user never opened rewrote them —
  on disk, for closed files — with no way to see the blast radius first.
- **Preview built from the converted edits, not a second request**
  (`renamePreviewFiles`, `plugins/lsp/workspace_edit.go`): per affected file
  the edit count, whether an open buffer holds it, and the text before/after,
  where "after" comes from the very `applyEditsToLines` the apply path runs.
  Confirming replays those same converted edits, so what the dialog showed is
  what lands — a second `textDocument/rename` could have answered differently.
- **The dialog** (`internal/app/lsprenamepreview.go`) is the crash-recovery
  prompt's shape: a `ui.Content` in the floating shell so the body learns the
  width budget, the file list above the selected file's diff, and the diff
  cached per cursor move rather than per frame (#409). Rendering reuses
  `miniDiffLines` — the same inline renderer behind local history, the change
  feed and recovery.
- **The single-file path is untouched**: the bridge only sends
  `RenamePreviewMsg` for two or more affected files; one file still applies
  instantly. That is the half most renames take, and a dialog per identifier
  rename would have been a regression.
- **Cancel is free by construction**, not by undo: the bridge writes nothing
  while the answer is pending, so dropping the message leaves every buffer and
  file exactly as it was — covered end-to-end in `plugins/lsp` against a fake
  server returning a synthetic two-file `WorkspaceEdit`.

## 2026-08-27 (Paste follows the focus, not the mounted jq playground, #2236)

- **The bug**: `handlePaste` asked `overlayCapturesKeyboard()` first, and that
  predicate lumps surfaces the `KeyPressMsg` guard chain resolves *after* the
  popup terminal layer (#1398, #1793) in with the true modals. An open jq
  playground reports `playFocused()` on its pane's focus, which a floating
  terminal never takes — so every paste landed in the query line while the
  panel had the keyboard.
- **The fix** (`internal/app/inputcoalesce.go`): the predicate is split along
  the key chain into `overlayCapturesAbovePopup` (full-window modals, finder,
  palette, decision prompts) and `overlayCapturesBelowPopup` (single-line
  prompts, regex tester, focused playground, capturing explorer).
  `overlayCapturesPaste` — used by `handlePaste` and the overlay `cmd+v` branch
  — takes the first group always and the second only while the popup layer is
  closed, so the paste reaches the popup shell, the focused editor or the
  focused tool pane instead.
- **Guards** (`internal/app/playpasteroute_test.go`): paste with a torn-out
  floating panel focused, with the popup box open (and back into the
  playground once it is hidden), with another editor pane focused, and with
  the issues filter focused.

## 2026-08-27 (HTTP response viewer: raw/pretty toggle, jq handoff, spooled bodies, #2157)

- **Pretty stays the default, `t` toggles** (`internal/httppane/body.go`): a
  JSON body is indented before it is composed — so folding (#1330) and
  highlighting (#1270) have lines to attach to — and `t` /
  `http.toggleRawBody` shows the bytes as received instead. The flag belongs
  to the view, not the response, so history browsing keeps it; highlighting
  stays on in raw mode, since it is the formatting that is off.
- **Formatting is capped** (`PrettyLimit`, 2 MiB): past it a body composes as
  plain rows — no indent, no highlight index, no fold scan — with a warning
  row saying so. All three passes are linear in the body size and a viewer
  that freezes on arrival has stopped being one; the cap is surfaced, never
  silent.
- **Large bodies never load whole** (`internal/httpclient/spool.go`): past
  `SpoolThreshold` (1 MiB) the dispatcher streams the body to a spool file and
  `Response.Body` keeps only the head. `FullBody`/`BodyReader`/`BodyRange` are
  the explicit ways past it, so the full copy exists for one operation instead
  of for the session — captures, the raw-body save (now a stream) and
  `httpdiff` use them. `MaxBodyBytes` still caps the total at 10 MiB.
- **The head is a window** (#2157): the pane states `(showing the first 1.0
  MiB of 7.3 MiB — m load more · o open as file)`. `m` reads the next 1 MiB
  window off the spool; `o` opens the whole body as an editor tab through
  `httppane.OpenBodyFileMsg`. A missing spool degrades to the head.
- **The history store adopts the spool**: a per-process temp file dies with
  the process, so `Append` copies it into `.ike/http/bodies/` and prunes it
  with its entry. `CleanupSpool` (deferred in `cmd/ike/main.go`) removes the
  temp directory on exit; creating one sweeps directories left by a crash.
- **One key into jq**: `q` / `http.jqPlayground` opens the playground over the
  shown body, in the response pane itself. The input is the *whole* body —
  read back off the spool when the pane holds only the head — because a
  program against a truncated document answers the wrong question.

## 2026-08-27 (HTTP client: completion for env, in-file and response variables, #2158)

- **The third rung reaches completion**: `{{` completion (#2135) offered the
  file's `@name=value` definitions and the selected environment; it now also
  offers the values earlier responses were **captured** from (#1993,
  `plugins/languages/http/captured.go`, read back from `.ike/http/*.json` like
  the dispatch reads them) and the names a `# @capture` directive *declares*
  but no response has produced yet — the promise a chain's polling request is
  written against before the chain has ever run.
- **Every item names its origin** — `file`, `env <name>` (the environment is
  named: it is the one thing that changes under the author's feet), `response`,
  `capture` — because the same name may live in several rungs and which one
  answered is the whole question. Built on the new
  `httpfile.Vars.Definitions()`, which lists the chain's names once, sorted,
  tagged with the winning rung; the process environment stays out of it, since
  its hundreds of names would drown the handful that belong to the file.
- **The popup opens on the braces**: the http source claims `{` as a trigger
  character (`complete.TriggerSource`), so `{{` offers the variable list
  without waiting for a letter to follow. Which made the auto-close pair
  interaction worth fixing: with `{{|}}` already in the buffer the item now
  inserts the bare name instead of `name}}`. The value of an `@name=value`
  definition completes too — `@api = {{ho` reaches `host`.
- **Unknown references are warned about** (`internal/app/http_vars.go`): every
  `{{name}}` no rung defines — `httpfile.References` × `Vars.Defines` — is
  marked inline, reads in the diagnostic popup and lands in the Problems window
  (source `http variables`). It runs after an edit goes quiet (400 ms, the
  autosave-idle debouncer shape), after a dispatch (a capture may just have
  defined the name) and when the environment is switched. A declared capture
  name and the process environment count as defined; `${NAME}` /
  `{{$env NAME}}` are never flagged, and neither is anything in a comment.
- **Deliberately silent** while the environment file does not parse or while
  several environments exist and none is chosen: every environment name would
  read as unknown, and a wall of warnings caused by a JSON typo — or by an
  unmade choice the dispatch error already names — is worse than none.
- **Two producers, one set** (`internal/app/http_diag.go`): `applyDiagnostics`
  replaces a path's whole set, so the capture reporter (#1993) and the variable
  linter now publish their shares into a per-file merge, ordered by position.
  A dispatch no longer erases the variable warnings, and a re-lint no longer
  erases the capture markers.

## 2026-08-27 (Auto-save dirty buffers on project switch, #2186)

- **The gate** (`internal/app/switch_autosave.go`): with
  `project.auto_save_on_switch` on (new, default `true`) an orderly
  `project.switch` writes every dirty file-backed buffer of the departing
  workspace *before* `performSwitch` chdirs — through the **manual** save
  action, so format-on-save and organize-imports-on-save run as on a `:w`.
- **Why it has to wait**: that path is async when the save chain applies
  (#1148) and a parked workspace's editors are cut loose from the model's
  services, so a chain finishing after the swap would never write. The switch
  parks until the last `SaveChainDoneMsg`, with a 3s backstop that writes what
  is still pending raw.
- **One dialog for the rest**: untitled, read-only and stale ("changed on
  disk" — overwriting is the conflict guard's call) buffers, plus failed
  writes, aggregate into a single decision prompt — `[s]` save as… (loops
  through the untitled ones and re-runs the gate after each answer), `[d]`
  switch anyway, `[esc]` cancel.
- **Deliberately scoped to the plain switch**: peek-enter, `project.close` and
  peek-return keep their own busy guards, whose explicit save/discard answer
  the gate must not overrule.
- The `switchModel` test fixture now turns the setting off — those tests are
  about #777's seamless parking, which is exactly what the gate would write
  away; `autoSaveSwitchModel` is the on flavour.

## 2026-08-27 (Investigate: .http highlighting after a failed request line, #2226)

- **The report**: in a ~205-line `.http` file, everything from a
  `GET {{host}}/…` request to the end of the file rendered plain — headers and
  JSON bodies of *later* requests included.
- **Finding — no cascade exists at HEAD**: the vendored grammar
  (rest-nvim/tree-sitter-http) resynchronises at every `###` separator; a
  battery of hostile inputs (placeholder targets, garbage request lines,
  unclosed `{{`, unbalanced JSON bodies, lowercase/unknown methods, CRLF,
  `###` inside a body string) produces no ERROR node that crosses the next
  separator, at any file length. The Go layers are per-line (`spans.go`) or
  per-`###`-block (`httpfile.Parse` behind body regions), so a broken block
  cannot eat its neighbours either. The reported cascade was the pre-#2218
  placeholder gap as far as it is reproducible today; verified in the running
  app on a 249-line file with the report's exact request at line ~242.
- **Regression guards** (`plugins/languages/http/highlight_test.go`):
  `TestErrorRecoveryMidFile` plants four kinds of broken request ~200 lines
  into a long file and asserts the next section highlights again at its `###`
  separator (separator, method, header name, injected JSON body);
  `TestPlaceholderRequestHighlighted` pins the report's own request — method,
  placeholder, path, header and body captures on the `{{host}}` section
  itself.
- **A body only counts as JSON with a `Content-Type`**: body regions resolve
  the injected language from the request's `Content-Type` header (#1303) — a
  body under a header-less request renders with host styling by design, which
  is worth knowing before reading such a body as "highlighting died".

## 2026-08-27 (Settings UI: fuzzy search across all settings, #2179)

- **The gap**: the panel filter was a case-insensitive substring over page
  titles, entry titles and keys. A setting was findable only by knowing what it
  is called — its **description**, the one place saying what it *does*, was not
  searched at all.
- **Schema-driven match sources** (`internal/settings/search.go`,
  `searchRows`): key, title, description and page title, plus a custom page's
  `SearchItem` label and keywords. No registry — a new `Entry` is searchable
  the moment it exists.
- **Fuzzy, with a floor for prose**: matching goes through `internal/fuzzy`
  (the command palette's matcher), so `edtabw` finds `editor.tab_width`. Terms
  are ANDed. In a description a scattered one- or two-rune subsequence is
  noise, so short patterns there must occur literally; names have no such gate.
- **Two-level ranking**: rows stay grouped by page — the rail lists *pages with
  hits* (#1297) and needs contiguous groups — but rows inside a page and the
  pages themselves are ordered by score, so the strongest match is the first
  row of the first group. A hit in a key or title outranks the same word buried
  in someone else's description.
- **The highlight explains the ranking**: `highlightMatch` marks each term
  literally where it occurs and otherwise on exactly the runes the matcher hit.
- **Landing on the field**: `tab` on a result opens its category positioned on
  the real form row, focused with its editor live — editable with the next key.
- **Esc is the picker's speed search** (#2111): the first press clears the
  query, the second leaves the search.

## 2026-08-27 (Run configs: environment form and a kind-faithful rerun, #2173)

- **The gap**: a run configuration's environment overlay existed in the model
  and reached the process, but the only way to *edit* it was hand-writing
  `.ike/runconfigs.json`. And the rerun chord always relaunched through the
  run funnel, so repeating what was last started under the debugger silently
  downgraded it to a plain run.
- **Validation next to the store** (`internal/run/env.go`): `EnvRows` renders
  the overlay as sorted key/value rows, `ValidateEnvKey` rejects empty keys
  and keys carrying `=` or whitespace, `EnvMap` rejects duplicates by name,
  and `Config.SetEnv` validates a whole set before replacing `Env` — an
  invalid set leaves the configuration untouched, an empty one clears the
  overlay. The dialog never hands the store an unchecked map.
- **The form** (`internal/app/runconfig_form.go`, `run.editConfig`): the
  configuration picker in edit mode — stored configurations only, a
  `launch.json` import is someone else's file — opening the picked one in a
  shell dialog. `a` adds, `enter` opens the two-field row editor (`tab`
  switches key and value), `d` removes, `ctrl+s` saves, `esc` discards. A
  rejected row stays open with the reason instead of being dropped. The
  handler sits **ahead of the popup/terminal key routing**: the form is
  typically opened right after a launch, when the Run tool's terminal holds
  the focus, and a modal dialog must not lose its keys to it.
- **Rerun repeats the kind** (`Store.Touch(name, kind)` / `LastLaunch`): every
  launch funnel records how it started, so `run.rerun` (cmd+f5 / ctrl+f5)
  sends a debug launch back through `startDebugConfig`. The marker persists
  with the per-project store, so the chord still repeats yesterday's run after
  a restart; a store written before #2173 has no marker and reads as a plain
  run. Nothing run yet stays a toast, not a silent no-op.
- Wiki: run-configurations concept doc and the run/debug guide updated;
  docgen re-run for the new command.
## 2026-08-27 (Explorer: marked multi-select with bulk delete/move/copy, #2166)

- **The gap**: the explorer's only multi-select was the transient shift+j/k
  range (#1044), and only *delete* honoured it. Operating on a handful of
  scattered files meant repeating the whole flow per file.
- **Sticky marks** (`internal/explorer/marks.go`): `space` toggles a mark on
  the cursor row, `esc` clears the set. Marks are keyed by absolute path — not
  by row index — so a rescan, a sort change or a hidden-files toggle can never
  re-point them at the wrong entries; only marks whose entry left the tree are
  pruned. A marked row reads with the muted `SelectionMuted` recipe and, while
  anything is marked, every tree row gains one leading `✓ `/`  ` marker cell.
  With nothing marked the column does not exist.
- **One resolution point**: `opTargets` picks the marks, else an active range,
  else the plain cursor entry — so single-entry behaviour is unchanged when
  nothing is selected, and delete/move/copy all agree on what they act on.
- **Bulk delete/move/copy** (`internal/explorer/bulkops.go`): delete confirms
  once and **lists every target**; move and copy each ask once for a target
  directory. Copy recurses (`copyTree`), preserves modes and recreates
  symlinks as symlinks. Whatever succeeds is recorded as ONE undo step, and
  `undoBatch`/`redoBatch` now dispatch on the sub-op kind, so a bulk move and
  a bulk copy are as reversible as a bulk delete.
- **Partial failure is expected, not exceptional**: a failing entry no longer
  abandons the batch. The rest is still processed, everything applied stays
  undoable, and the failures are reported together — `moved 1 of 2 — a.txt:
  already exists`.
- **VCS follows**: `FileDeletedMsg`/`FileMovedMsg` now schedule a git status
  refresh, and a bulk copy announces its new paths with `FileCreatedMsg`, so
  the explorer's VCS colouring is correct after any of the three.
- The app's `file.move` (`f6`) picker feeds the same batch path via
  `MoveManyMsg` when the explorer's multi-select is non-empty.
- Wiki: [explorer](architecture/explorer.md).

## 2026-08-27 (Find in Path prefills the active selection, #2165)

- **The gap**: Find in Path always opened on the remembered query, so
  searching for text one had just selected meant retyping it or a manual
  yank/paste round trip — JetBrains prefills from the selection.
- **The seam** (`internal/app/selection.go`): `activeSelectionText` asks the
  focused pane for its selection and falls back to the last-focused editor, so
  the command works from a tool pane too. The audit of selection sources it
  wires: editor panes (vim visual mode), terminal panes plus terminal tabs and
  the debug console (`Instance.ActiveTerminal`), diff panes (side-column mouse
  selection and the editable right side's visual mode), merge panes, and the
  HTTP response viewer. Every other pane has a *row* cursor, not a text
  selection, and contributes nothing.
- **The prefill** (`finder.OpenPrefilled` / `OpenReplacePrefilled`): the
  selected text seeds the query and arrives **preselected** (#277), so the
  first typed character replaces it. A selection spanning more than one line
  prefills nothing — the same rule the editor's `/` (#2063) and the HTTP
  viewer's search (#2122) already follow, since the query language has no
  line-spanning match to offer; a lone trailing newline is not a span. In
  **regex** mode the prefill is `regexp.QuoteMeta`-escaped so the selection is
  found literally. A prefill outranks the remembered query *and* the restored
  result cursor (#2054); with no selection nothing changes.
- Wiki: project-search concept doc updated.
## 2026-08-27 (Crash recovery: restore dialog with diff preview, #2160)

- **The gap**: the restore offer listed file names and nothing else — the user
  had to decide "restore or discard" without seeing what the snapshot held
  versus the file on disk, and a restore wiped the buffer's history, so there
  was no way back.
- **Diff preview** (`internal/app/recovery.go`): the centered floating-shell
  dialog now shows the selected file's on-disk content against its snapshot
  (`diff.Compute`), so the `+` side is exactly what restoring would write into
  the buffer. It is cached per selection and recomputed on cursor moves and
  item drops, never per frame. Equal sides, a deleted base, and an unreadable
  or undecodable side read as text instead of an empty diff.
- **Shared inline renderer**: the local-history panel's git-style hunk renderer
  moved out into `miniDiffLines`, which the change feed and the recovery dialog
  now share instead of carrying a third copy.
- **Undoable restore**: with the base file on disk, the recovered text lands
  through `editor.RestoreContent` — one undo step, so plain `u` brings the
  on-disk version back. A snapshot with no base file still seeds via
  `RestoreText` (never-saved, so it stays dirty however far undo runs).
- **Answer for everything**: `R` restore all / `D` discard all join the per-file
  `r`/`d`/`s`; `esc` stays a *deferral* — every undecided snapshot survives to
  the next launch, never a silent discard.
- Wiki: [crash-recovery](architecture/crash-recovery.md).

## 2026-08-27 (.http highlighting continues across {{variables}}, url-shaped definitions, #2218)

- **Placeholder-led targets** (`plugins/languages/http/spans.go`): a request
  target opening with a placeholder and no scheme of its own
  (`GET {{host}}/my/path`) no longer loses highlighting after the variable —
  `pathBounds` is placeholder-aware, so the path starts at the first `/`
  outside the `{{…}}` ranges (`label`, query structure as before), and the
  stretch between the placeholder and the path (`{{host}}.test:8080`) reads
  as the authority remainder in `string.special`.
- **Url-shaped definition values**: `@host=http://www.example.com/base?a=1`
  now reads as the same segments a request target does (dimmed scheme,
  `://` punctuation, authority, path label, query structure) instead of one
  flat `string`; non-url values keep the flat `string` capture.
- Wiki: http-client concept doc updated (#1740/#1880 bullets).

## 2026-08-27 (Frecency-weighted file finder, #2155)

- **The gap**: the `@` finder ranked purely by fuzzy score, so the handful of
  files one lives in ranked no better than never-touched ones — and the empty
  query opened on the alphabetical head of the tree.
- **One shared store** (`internal/frecency`): #2153's command-execution history
  moved out of `internal/palette` into a key-agnostic, half-life-parameterized
  package and the file finder took a second instance of it — same timestamp
  format, same caps (16 hits per key, 400 keys), same corrupt-file tolerance.
  Command history keeps its 7-day half-life; file opens decay over 14 days,
  because what one works *on* turns over more slowly than what one runs.
- **Blend order** (`FileMode.Results`): up to two typed characters frecency
  leads (the fuzzy score barely discriminates there anyway); from the third the
  score leads and frecency is the tiebreak above the #1419 usage count, then
  path. Command mode keeps its own policy — a query-length-damped boost — which
  is why the store itself carries no ranking opinion.
- **Event source**: every open counts, not only palette confirmations — the
  root model records at the same two sites that feed the recent-files MRU
  (`openPathWith`, tab re-activation), keyed through `frecency.Key` so the
  finder's root-relative paths and the openers' spellings agree.
- Wiki: [command-palette](architecture/command-palette.md).

## 2026-08-27 (Command palette: frecency boost from command execution history, #2153)

- **Execution history** (`internal/palette/frecency.go`): a new per-project
  store keeps the unix timestamps of recent command executions
  (`.ike/cmdfrecency.json`, `IKE_CONFIG_DIR`-redirectable). It is written in
  `Model.dispatchCommand` — the single funnel every dispatch path goes through
  — so keybind and inline invocations count like palette picks, unlike the
  palette-only most-used counter (#773) it sits beside.
- **Frecency scoring**: `Score` sums `0.5^(age / 7d)` over a command's
  timestamps, blending frequency with recency decay. The store is capped at 16
  timestamps per command and 400 tracked commands (lowest-scoring pruned); a
  missing or corrupt file loads as empty history.
- **Ranking blend** (`internal/palette/command_mode.go`): command mode's sort
  key is now the fuzzy score plus a query-length-damped boost — dominant on an
  empty query (all fuzzy scores tie there), halved per typed rune, so a longer
  query's match quality wins and history only breaks near-ties. Context tiers
  are unchanged.

## 2026-08-27 (Branch and dirty status per project-picker row, #2178)

- **The gap**: the recent-projects picker listed roots and last-opened times,
  but not the two facts that actually decide which checkout to open — the
  branch it sits on and whether work was left uncommitted there.
- **Async by construction** (`internal/project/gitinfo.go`): the picker opens
  on the history alone and returns `EnrichCmd`, one `git status
  --porcelain=v2 --branch -z` per entry — capped at 24 rows, 4 subprocesses
  in flight, 1s deadline each. Results land as `GitInfoMsg`, go into the
  shared `GitCache` (both picker flavours read it) and render as the badge
  `⎇ main*`, merged with the `●` in-memory dot into `● ⎇ main*`.
- **Never worse than before**: a non-git root, a missing git binary, a
  timeout or an unparsable answer all resolve to the plain row — no toast, no
  error state, no delay in the open path.
- **`palette.RefreshRows`**: the in-place re-list added for this. `Refresh`
  resets selection and scroll like a query edit, which would have yanked the
  cursor away from a user arrowing down while probes trickle in.
- Wiki: [project-switching](architecture/project-switching.md).
## 2026-08-27 (Run output pane: respect the tool region, remember a move, #2191)

- **Tool-region placement** (`internal/layout/region.go`,
  `Model.toolRegionLeaf`/`dockNewPane` in `internal/app/tools.go`): a home-docked
  tool pane whose workspace edge is no dock slot — the root split runs across
  the dock axis, e.g. the explorer column beside the editor column — now joins
  the layout's **own tool region** instead of re-rooting the tree at the
  outermost edge. The active editor's ancestor path is walked outwards
  (`layout.Hops`) and the first sibling region on the placement's side holding
  nothing but tool windows is the tool area; the new pane splits beside its
  edge leaf (`layout.EdgeLeafIn`), never tabs into it (#1905). Regions
  containing an editor stay editor area and keep the full-span dock. The three
  copies of the "occupant → perpendicular split, else `DockNew`" tail
  (`openToolAtHome`, the global-tool attach, `replaceToolPane`) collapsed into
  `dockNewPane`; `anchorFromLayout` now rides `layout.Hops` too.
- **A moved Run pane keeps its place** (`internal/app/runhome.go`): a layout
  drag that relocates the Run tool records `{anchor, zone, ratio, tab, root,
  placement}` in `.ike/runhome.json`, and the next run re-opens there — beside
  the anchor pane, as a tab of it, or as the full-span workspace dock it was
  dragged to (`root`) — surviving both the pane's close and a restart, where
  the `runTool` leaf prunes on restore. Written only on an actual move and only honored while
  `run.placement` still matches, so a slot assignment (#1897) and an edited
  setting both still win.

## 2026-08-27 (Close a finished pseudo-terminal in every placement, #2192)

- **The gap**: a terminal that outlives its process — run output, a tool pane
  (#810), a popup tab (#1398), a torn-out floating panel (#1793), the debug
  area's console (#1370/#2190) — could not always be closed where it lived.
  Both close requests (`requestTerminalClose`, `requestPopupTabClose`) took
  the #986 idle arm and sent the child an **EOF**, which a finished session
  never receives; and `terminalFocused` gated on `Session.Running()`, which a
  pipe session keeps reporting true past `FinishPipe`. Docking the terminal
  into a pane as a tab was the only way out — the ordinary `ctrl+w` route
  then applied.
- **Single predicate**: `terminal.Model.Exited()` exports the existing
  `dead()` (no session, ended child, or a pipe past `FinishPipe`). Both close
  requests check it ahead of the busy guard — a finished session is never
  `Busy` — and close the tab/leaf outright: `closeFocused` for panes,
  `closePopupTab` for the popup box and floating panels (`cmd+w` and the
  active segment's ✕ share the path). `terminalFocused` and
  `focusedDeadTerminal` read `Exited()` too, so the finished DAP console
  stops swallowing its own close chord.
- **Exited chrome**: `termExitedTitle` appends `✗ exited (code N)` to the
  terminal, tool, popup-box, floating-panel and DEBUG pane titles;
  `tabLabels` marks a finished terminal segment with the bare `✗` — the tab
  bar is often the only chrome a popup-layer box shows.
- **Audit** — every terminal host and its close path: dedicated terminal pane
  and editor-hosted terminal tab (#573) → `closeFocused`; tool pane (#741) →
  same, plus the #810 dialog's `Close` button; run output (#1905, a tool
  session) → same; popup box tab (#1398) and floating panel (#1793) →
  `closePopupTab`; debug area console (#2190) → `closePane` via the ordinary
  key route. Parked global tools (#1890) have no host and end with
  `closeParkedGlobalTools`.

## 2026-08-27 (Editor tabs: overflow handling + MRU tab picker, #2151)

- **Tab strip overflow** (`internal/app/tabbar.go`): the window around the
  active tab now counts what it hides — a `+N` indicator at each overflowing
  end (`tabEnds`, `tabOverflowMark`) instead of a bare `…` — and every label
  is ellipsized at 24 cells (`fitTabLabels`), so one long filename can no
  longer push its neighbours off the bar. The active tab stays visible at any
  tab count and width; on a bar too narrow for both an indicator and a
  one-cell segment the indicators drop in favour of the tab itself.
  `tabWindow`, `tabHit` and `renderTabBar` share the fitted labels and the
  end-width math, so clicks still land on what is drawn.
- **MRU tab picker** (`internal/app/tab_picker.go`, `editor.tab.picker`,
  `alt+e`, File menu "Switch Tab…"): the focused editor pane's tabs in a
  locked palette mode, most-recently-used first, speed search filtering
  without re-sorting, enter activating the picked tab. The tab shown right now
  sits last with a `●` badge, so the preselected row is the alternate-tab
  flip. Recency reads the activation stamp the tab-limit eviction already
  keeps: `pane.Instance.TabsByMRU()` / `TabLastUsed()` over `Tab.lastUsed`,
  never-activated (restored) tabs last in tab order.

## 2026-08-27 (Pane resize mode: enter once, resize with hjkl, #2150)

- **Sticky keyboard resize mode** (`internal/app/resizemode.go`):
  `pane.resizeMode` (`ctrl+alt+r`, pane context menu → **Resize…**, palette)
  arms a mode instead of running a one-shot action. While armed it is the
  first consumer in the `tea.KeyPressMsg` branch — ahead of overlays, the
  keymap layer and pane routing — so `h`/`j`/`k`/`l` and the arrows step the
  focused pane's edge one cell per press, `esc`/`enter`/`q` leave (persisting
  the tree like a released drag), and **every other key is inert**: a mode
  left armed can never cause a stray edit. It refuses to arm where every key
  would be a no-op (no focused pane, a maximized pane, a dividerless
  workspace).
- **Same geometry truth as the mouse** (`internal/layout/resize.go`):
  `Divider.ResizeStep(delta)` is the keyboard sibling of the drag's
  `ResizeTo` — it reads the ratio back into a cell offset so repeated
  single-cell steps accumulate exactly, and clamps against the same
  `minCell`. `Layout.EdgeDivider(pane, zone)` picks the split to move: the
  band on the pane's trailing edge along the requested axis (else the
  leading one), innermost first. Direction means *where the edge travels*,
  which keeps grow and shrink on the same four keys.
- **Visible while active**: the status line swaps its segments for a
  `RESIZE <pane>  hjkl / arrows move the edge · esc to finish` banner in the
  drop-target colour, the slot an engaged move drag already uses; segment
  hit-testing goes quiet with it.

## 2026-08-27 (LSP: markdown in hover, diagnostic related information, #2147)

- **Inline markdown in hover** (`internal/editor/hovermd.go`): the prose
  around the already-highlighted fenced blocks (#379) no longer shows raw
  syntax. Headings render bold, `-`/`*`/`+` items become `•` bullets, quotes
  `│`, `**bold**`/`*italic*` become terminal attributes, `` `code` `` takes
  the accent tint, and `[text](url)` shows its text plus the dimmed URL (a
  popup cannot be clicked, so the address stays readable); `<autolinks>`
  render bare, images keep their alt text. A deliberate hand-written subset:
  `snake_case` is never emphasis, a destination with whitespace is not a link
  (`func F[T any](v T)` keeps its brackets), unmatched markers stay literal.
- **Diagnostic related information** (#2147): `publishDiagnostics`'
  `relatedInformation` — advertised all along in the client capabilities —
  is now decoded (`protocol.DiagnosticRelatedInformation`) and converted to
  editor coordinates as `ilsp.RelatedInfo`. A location inside the published
  document converts through the negotiated encoding; one in another file
  keeps the server's position, which lands on the right line.
- **Where it shows**: the diagnostic details popup (#739) lists a dimmed
  `↳ note  file:line` row per entry under the message — the popup is
  dismiss-on-any-key, so the location is spelled out rather than hidden
  behind a jump. The Problems window makes them navigable: each entry is its
  own faint child row, `enter`/double-click opens *its* file at *its*
  position through the existing `OpenLocationMsg` funnel, `y` copies the
  entry, and the rows never count toward the header's error/warning totals.

## 2026-08-27 (host.Send outbox: bounded and coalescing, #2169)

- **Bounded outbox** (#2163 follow-up): the fire-and-forget `Send` queue
  (#2027) caps at 1024 messages. At the cap the incoming message is dropped
  (newest loses, queued order kept), counted (`Host.SendDrops`) and reported
  to `debug.log` via the new `Host.SetDiagLog` seam (first drop, then every
  500th) — a firehose producer can no longer grow the queue monotonically
  and pin the Update loop on a backlog it never drains.
- **Per-type coalescing**: `host.Coalescable` (`CoalesceKey() string`, the
  jsonrpc `NotifyCoalesced` pattern #1542) marks idempotent snapshots; a
  queued message with the same non-empty key is replaced in place — same
  slot, latest payload — so snapshot classes never grow the queue and never
  drop. Producers adopt it in their own follow-up issues.

## 2026-08-27 (debugging: combined tool area — panel + console in one pane, #2190)

- **Combined debug area**: the separate debuggee terminal pane (#1370,
  `debugTerm`) is gone — the debug panel (`internal/debugpanel`) now embeds
  the debuggee's `terminal.Model` whole behind an internal `Variables │
  Console` tab bar (ghissues in-pane pattern). One `KindDebug` pane holds the
  whole session surface: movable/resizable as a unit, position persisted as
  the single `debug` leaf; legacy `debugTerm` leaves still prune on restore.
- **Routing seams**: console view → `Instance.ActiveTerminal()` returns the
  embedded terminal and `ContextID` flips to the terminal context, so keys,
  selection drags, paste, search and the busy close-guard reuse the terminal
  pane paths; `contentYOff` adds the tab-bar row (`debugConsoleRows`) so
  mouse coordinates arrive terminal-local. Scrollback/selection survive view
  switches — the model never moves.
- **View switching**: `tab`/`shift+tab` cycle views (pipe console included;
  `h`/`l` keep the column switch), tab-bar clicks, and the new
  `debug.console` command (palette) that also works over a PTY debuggee.
  `AutoTab` surfaces the console on pre-stop output / PTY install and the
  variables on a stop, until the user picks a view (per session).
- **Session end configurable**: `debug.session_end` = `keep` (default,
  #689 behavior) / `close` (config + validation + Settings UI Debug page);
  applies to the active workspace and parked sessions (#1544).
- Lifecycle: the embedded console parks/unparks (#1522), tears down with the
  workspace/quit paths, and closes with the pane (`releaseContent` →
  `CloseTerm`). `runInTerminal` PTYs install via `debugpanel.SetTerm`.

## 2026-08-27 (LSP doctor: per-server failure diagnosis with verified fixes, #2164)

- **`internal/lspdoctor`** + palette command `lsp.doctor` ("LSP: Doctor"):
  singleton tool window (pane key `lspdoctor`) running a per-server check
  chain — binary resolution (PATH → IKE fallback dirs → well-known dirs =
  the GUI-PATH gap #1614), exec bit, node-runtime probe, `--version`,
  workspace-root sanity, and a real spawn + initialize round-trip.
- **Signature→diagnosis mapping** (`classify`): binary missing (install
  recipe as fix), PATH mismatch (config-override escape hatch), not
  executable, wrong architecture (Rosetta trap), node runtime mismatch,
  crash-on-initialize with stderr evidence — incl. the launch-advice table
  and a shadowed-copy detector for the taplo/TOML "npm install did not
  help" case.
- **Verified fixes**: the app-owned report remembers the previous run's
  failure classes; `r` re-runs and marks each server resolved / still
  failing / changed instead of repeating the hint.
- **Routing**: launch-failure, repeated-crash-disable and install-unresolved
  toasts append `diagnose: "LSP: Doctor"`; the `lsp` status segment click
  now opens the doctor (was `lsp.showLog`); Tools menu entry.

## 2026-08-27 (large files: per-feature degradation, badge + detail popup, buffer fast path, #2159)

- **Per-feature thresholds**: `largefile.Feature` (highlight, LSP sync, VCS
  gutter diff, search tally, format-on-save) + `largefile.Thresholds`; the new
  `files.large_file_<feature>_kb` keys (config, validation, Settings UI, user
  docs) switch one service off *below* the base cliff — 0 follows the base
  thresholds. `editor.Model.FeatureOff`/`DegradedFeatures` are the verdicts.
- **New guards**: the VCS gutter recompute (`vcsMarksCmd` clears instead of
  `git show` + whole-file diff) and the search match tally (counter hides)
  now degrade too; the LSP `didOpen` gate honours the LSP feature threshold.
- **Surfacing**: dedicated `largefile` status segment (replaces the `[large
  file]` suffix in the `file` segment) — clickable, like
  `editor.largeFileDetails`, opening a centered popup listing every feature's
  state; any key/click closes. `editor.forceCodeInsight` lifts per-feature
  degradation as well.
- **Buffer fast path**: `buffer.Apply` rewrites line-count-preserving edits in
  place instead of rebuilding the whole `[]string`; typing on a 120k-line
  buffer dropped 407µs → ~11µs/op (`BenchmarkKeystrokeLargeFile` +
  deterministic no-full-buffer-work test guard the path).

## 2026-08-27 (replace-in-path: per-row preview, selective apply, stale-file & encoding guards, #2154)

- **Per-row preview**: in replace mode every result row now renders each match
  struck through with its replacement appended — a `locations.List.Rewrite`
  hook the finder installs per render, so editing the template redraws
  without a rescan; capture groups expand through the new
  `search.RewriteSegment` (shared with the buffer apply path).
- **Selective apply**: `ctrl+t` toggles the selected match out of the apply
  set (and steps on), `ctrl+g` the whole file. Excluded rows render faint
  with a `✗`, are counted in the status row, and survive `ctrl+a`/`ctrl+f` —
  those now apply only the non-excluded matches.
- **Stale-file guard**: the finder records each result file's mtime as its
  first match streams in; a disk file whose mtime moved on since the scan is
  skipped whole and reported as a warning. The apply path refreshes the
  shared baseline after its own write, so later batches of the same result
  set still apply.
- **Encoding-preserving disk writes**: unopened files decode through
  `internal/textenc` (BOM → UTF-8 → `files.encoding` fallback) and re-encode
  with the detected encoding + EOL flavor; undecodable or unencodable
  content is skipped, never corrupted. Details in
  [Project Search](architecture/search.md).

## 2026-08-27 (completion: per-language recency + snippet placeholder pre-selection, #2146)

- **Recency per language**: the recently-accepted store
  (`internal/complete/mru`, #854) is now scoped by the buffer's resolved
  language id — `Bump(scope, label)` / `Rank(scope, label)`, persisted as a
  scope→labels map. An accept in a Go buffer boosts Go popups only; a
  pre-scope flat-array store file loads into the `""` scope and keeps working
  through a named-scope fallback. The fuzzy-dominant ranking itself
  (`fuzzy·4 + priority + locality + MRU`) was already in place (#854) and is
  unchanged.
- **Placeholder pre-selection**: `snippet.Expand` returns tabstop *spans*
  (`Stop{Start, End}`) instead of bare offsets, and the editor's tabstop
  session pre-selects a placeholder's default text on jump — highlighted like
  a selection, replaced wholesale by the first typed rune, kept by tabbing
  on. Bare tabstops and plain-text items behave exactly as before. Details in
  [Completion Engine](architecture/completion.md) and
  [Editor](architecture/editor.md).

## 2026-08-27 (editor: saving from insert mode no longer re-dirties on esc, #2188)

- **Root cause**: the insert session keeps its edits in an open `Recorder` until
  a word/paste boundary or `esc` commits them, so a save fired mid-insert pinned
  the history's save checkpoint (`MarkSaved`) at the state *before* the typing.
  `esc` then pushed that change on top of the checkpoint and `commitInsert` set
  `dirty = true` — a buffer identical to the file on disk showed as modified and
  offered a second, redundant save.
- **Fix**: `saveAs` closes the running insert segment (`breakInsertUndo`) before
  it clears the flag and marks the state, so the checkpoint names exactly what
  was written. All write paths funnel through `saveAs`, so `:w`, `ctrl+s`,
  `:w other`, the save-as prompt, autosave (focus and idle) and the chained
  format-on-save write are covered at once; the crash-snapshot debounce follows
  from the corrected dirty flag.
- **Format-on-save ordering**: `beginSaveChain` breaks the segment when the
  chain starts, so the LSP rewrite (`ApplyTextEdits`, its own change) can no
  longer be followed by an insert tail whose inverses were recorded against the
  pre-format text.
- Typing on after the save opens the next undo segment as usual and `.` still
  replays the whole insert. Details in
  [Editor](architecture/editor.md).

## 2026-08-27 (crash recovery: no stale restore prompt on project switch, #2185)

- **Root cause**: switching projects *parks* a workspace (#777) — dirty buffers
  and all — but rebuilds the model, and `buildModel` re-runs `scanRecovery`.
  Every leftover snapshot counted as crash evidence, so returning to a project
  offered to "recover" the very buffers that were about to reopen dirty anyway.
  Second seam: the snapshot directory was the *relative* `.ike/backups`, so the
  quit path removed a parked workspace's snapshots from the active project's
  state dir instead of its own — they survived a clean quit too.
- **Session ownership**: every snapshot now records the process that wrote it
  (`session:` header, `backup.CurrentSession`). Only snapshots from a session
  that is gone are crash evidence; the restore prompt skips this session's own.
  Snapshots without a token (older builds) stay recoverable.
- **Per-project, absolute snapshot dirs** (`backupDirFor(root)`, symlinks
  resolved) — the single seam quit, project switch and workspace close resolve
  their service through, so a parked workspace is always reached in its own
  project.
- **Flush on park**: the departing workspace's pending debounce marks are
  written out on the switch instead of being dropped with the old debouncer, so
  parked dirty buffers stay crash-protected. Details in
  [Crash Recovery](architecture/crash-recovery.md).


## 2026-08-26 (session restore: every pane's tabs with caret, framing and lazy loading, #2177)

- **Per-tab view state**: `session.json` grows a `panes` section — one
  `{path, line, col, top, left}` entry per document tab of every editor pane,
  matched back by path at restore. The tab list itself stays in `layout.json`;
  quit and relaunch now returns the whole working set, each tab framed where
  it was left, not just the focused document.
- **Lazy restore**: only each pane's active tab is read at startup. The others
  come back as *deferred tabs* (`pane.Deferred`) that know their path, caret
  and framing and read the file on first activation, adopting an already-open
  document (#142) instead of loading a copy. `Instance.activate` is the single
  materialization point; `TabPath` gives the tab bar, layout save and dirty
  sweeps a path without triggering a read.
- **Missing files** are skipped with one summary notice instead of silently
  vanishing, and a file deleted after restore leaves an empty tab rather than
  breaking its pane. Details in
  [Session Restore](architecture/session-restore.md) and
  [Editor Tabs](architecture/editor-tabs.md).

## 2026-08-26 (lsp: crash restart with exponential backoff and status feedback, #2148)

- **Backoff instead of a linear retry**: a crashed language server is respawned
  after 1s, 5s, 30s (`manager/restart.go`), and the status line carries the
  progress — `<lang> language server restarting (attempt i/3)` — while the wait
  runs. A server that dies instantly can no longer produce a restart storm.
- **Give-up is a real state**: after three consecutive crashes the
  `(language, root)` key is *disabled* — `ensureServer` refuses to spawn it, so
  opening more files cannot restart the loop — the status line reads
  `<lang> language server failed — restart: "LSP: Restart Servers"`, and the
  error toast names both that command and the server log. A server that ran
  healthily for two minutes before dying gets a fresh restart budget, so a long
  session is not disabled by unrelated crashes.
- **Recovery re-syncs the buffers**: a successful restart re-sends `didOpen` for
  every tracked document and embedded fragment, drops the stale semantic-token
  result ids and asks the host to re-pull semantic tokens, inlay hints and code
  lenses — features resume without reopening files.
- **`lsp.restart` restarts instead of only stopping**: the palette command, the
  Tools menu entry and the Language Servers page now go through
  `Manager.RestartAll` / `RestartLang` (`manager/manualrestart.go`), which
  snapshot the open documents, stop the servers (clearing attempt counters and
  disable blocks) and re-open the snapshot on the fresh ones.
- Details in [LSP & Language Intelligence](architecture/lsp.md).

## 2026-08-26 (search: match counter in the status line, capped tallies, #2145)

- **Status line counter**: a `search` slot on the right list shows `⌕ 3/17`
  (`⌕ no matches`) for the focused buffer's in-file search. Unlike the `/`
  line's counter it outlives the search input, so `n`/`N` update the index in
  place until a normal-mode Esc clears the highlights.
- **Bounded tallies** (`search.ScanMatches`): counting stops at 999 matches or
  20 000 scanned lines and renders `999+`, and the capped match list is cached
  per (document version, query) — a keystroke costs one bounded scan, cursor
  motion none. The command line's own counter now reads the same cache instead
  of scanning the whole buffer per frame. Highlighting was already
  viewport-only (matches are computed per rendered line); the current match
  now takes an accent tint on top of its underline so it stands out among the
  other highlights. Details in [Editor](architecture/editor.md) and
  [Status Line Segments](architecture/status-line.md).

## 2026-08-26 (perf: freeze audit round two + update-loop stall watchdog, #2163)

- **Stall watchdog** (`internal/diag`): `app.Update`/`app.View` stamp
  `LoopEnter`/`LoopExit` (two atomics), and a monitor goroutine — never the
  blocked one — dumps every goroutine's full stack to the state dir when one
  pass overstays `perf.watchdog_seconds` (default 15s, `0` opts out, Settings
  UI on the Performance HUD page), logs the stall and the recovery to
  `debug.log`, one dump per episode, ten per session. The next freeze leaves
  evidence even when nobody is around to send SIGUSR1.
- **Busy-loop fixes from the audit**: explorer auto-refresh chains are
  generation-tagged (a project switch/resume used to grow one permanent
  stat-walker per switch, and disable→re-enable never restarted); LSP
  diagnostic line ranges clamp to the buffer (a "to end of document" range of
  2^31-1 hung the loop); the .http flight tick can no longer double-arm and
  is counted in the HUD's armed-timers gauge; backup/autosave consume
  past-due debounce marks even while disabled (a latent zero-delay tick
  loop); the watch debounce flushes at least every 10 windows under
  sustained churn; the terminal-check retry gives up after ~a minute;
  clipboard subprocesses get a 3s deadline (osascript behind a TCC prompt
  wedged the IDE); terminal scrollback-search matches are memoized (the full
  10k-line scan ran per frame); follow mode re-evaluates the large-file
  thresholds as the tail grows; the DBGp accept loop survives fd-exhaustion
  bursts.
- **Follow-ups filed** for the larger findings: host outbox bounding
  (#2169), flood coalescing (#2176), terminal resize/selection costs
  (#2184), per-frame memoizations (#2187), completion/LSP lock work
  (#2193), tick generation stamps (#2194). Audit details in
  [Performance & Diagnostics](architecture/performance.md).

## 2026-08-26 (tools: saved tool-tab placement survives a project switch, #2141)

- **The project's layout decides where a returning tool goes**: the switch-in
  re-attach of a parked global session (#1903) now consults the project's own
  `layout.json` before any runtime open rule, so a tool that was a tab of the
  lazygit/sql/yarn pane comes back to that pane instead of piling into the
  tool pane next door. Before this, a group whose host closed with its last
  global tab (#1901) dissolved into whichever tool pane came first in registry
  order — the claude pane, in the reported setup.
- **The group reforms around its first arrival**: when the recorded host is
  gone, a saved co-tenant's live pane stands in for it, and a tool the layout
  *did* place never falls back to the "group with any tool pane" rule — it
  takes its own pane. Tools the layout never placed keep the #2042 grouping,
  and `tool.<name>` stays a fresh open that follows the slot/home rules alone.

## 2026-08-25 (project: quick-peek switch with one-key return, #2136)

- **Peek a project instead of switching**: `alt+enter` on any project-picker
  row (a new palette-wide `Item.Alt` alternate-activation seam), or the new
  `project.peek` command's picker flavour, opens the project through the
  normal seamless-switch transaction but marked as a peek — the open is not
  recorded into `project.history`, so a ten-second look-up never becomes the
  startup restore head.
- **One key back**: `project.peek.return` (`cmd+shift+b`, `ctrl+shift+b`
  secondary; palette fallback in the matrix) switches back to the origin and
  drops + tears down the peeked workspace in the same action, behind the
  existing busy guard when dirty buffers or running processes would die. An
  untouched peek writes no session/layout on the way out, so it plants no
  `.ike` directory in a repo it only read. A status-line segment
  (`peek ⇢ <origin> (<chord>)`) keeps the way back visible.
- **Staying converts the peek**: `project.peek.keep` — or a plain switch
  elsewhere — records the peeked root into the history and clears the marker.
  Peek state deliberately does not survive a restart.

## 2026-08-25 (scratch: test-data generator, #2134)

- **`scratch.generate` writes synthetic sample files** into the scratch store,
  in nine formats — CSV, TSV, JSON, NDJSON, XML, YAML, TOML, SQL inserts and
  logfmt log lines — so exercising the CSV table, the data grid, the log
  timeline or the jq/yq playgrounds no longer needs a hand-made file. Nine
  no-prompt `scratch.generate.<format>` commands mirror `scratch.new.<lang>`.
- **The spec is a field list**: a name, one of 26 catalog kinds and an optional
  parameter per field, plus a row count, a seed and a table name. `email`,
  `url` and `hostname` take a **domain** that constrains every generated value
  to it; `int`/`float` take `min..max` and `date` takes `from..to`.
- **Seeded and reproducible.** Values come from `gofakeit/v7` through an
  *instance* faker seeded from the spec, and nothing in the catalog reads the
  wall clock — so the same seed and spec produce a byte-identical file. Seed 0
  is the documented "draw a fresh seed" spelling.
- **Writers stream** (begin / row / end over a 64 KiB buffer) and escape for
  their own grammar; each is round-tripped in tests through the real parser,
  SQL included — the generated `INSERT`s are executed against an in-memory
  SQLite. The row count is capped at 1 000 000 by a constant, not a setting.
- **`scratch.CreateWithContent`** is the new store entry point behind it: the
  same race-free allocation, seeded with the caller's bytes instead of the
  language template. Specs are remembered per format in `~/.ike/testdata.json`
  and validated on read, so a stale preset falls back to the default.

## 2026-08-25 (settings: Conceal & Hints control center page, #2133)

- **A dedicated Conceal & Hints settings page** is now the single surface for
  the conceal suite: every registered family grouped by what it does
  (rendering layers, decoders, value hints, secrets & field units, file rules,
  coloring layers), each row showing its state and the config layer the value
  comes from, plus an informational row for JWT decoding, which has no toggle.
  The twenty-six keys left the generic **Editor** page; the keys themselves are
  unchanged, so no migration is needed.
- **The schema entries stayed.** `settings.AttachCustom` installs a `PageModel`
  on a page that keeps its `Entries` — the panel renders the model, docgen keeps
  rendering the key table, and the no-dead-keys and coverage guards keep
  covering the keys. The page's rows are built *from* those entries, so titles,
  descriptions and bounds have one definition.
- **Structured list editing** for `conceal_include`, `conceal_exclude`,
  `conceal_file_rules`, `number_hint_units` and `secret_masking_keys`: the
  element grammar becomes form fields (family · include|exclude · glob;
  pattern · unit; mask|exempt · key pattern), and the composed element is
  validated with the loader's own checks (`concealfilter.Invalid`,
  `numhint.EntryError`) — a typo is refused in the form instead of dropped with
  a diagnostic after the write. Elements reorder with `shift+↑/↓`, because the
  first match decides a key in both pattern maps.
- **A live preview** draws the selected family's sample line raw and as the
  family renders it, marking the state the toggle currently picks, through the
  same packages the editor renders with (`epochtime`, `numhint`, `cronhint`,
  `permhint`, `nethint`, `secret`). The glob and rule lists preview as
  draws/blocked verdicts on sample paths instead, since they decide *where*
  rather than *what*.
- The page edits **config defaults**; a per-view toggle still wins in its
  buffer, which the footer says on every frame.
- Fixed along the way: `cmd/docgen` did not emit the language-scoped binding
  section of `keybindings.md` (#1876 edited the generated file directly), so
  regenerating deleted it. The prose now lives in the generator.

## 2026-08-25 (http: form/multipart/graphql body highlighting + {{var}} completion, #2135)

- **Form-urlencoded, multipart and GraphQL request bodies highlight now.**
  `application/graphql` maps to the `graphql` language like any other body
  (`bodyLanguages`, `regions.go`) when a build links it; the other two do not
  fit the "whole body is one language" region model, so they get a
  Go-produced span overlay instead: `application/x-www-form-urlencoded`
  reuses the request-target's own key/value/`&`/`=` styling on the body's
  lines, and `multipart/form-data` gets its `--boundary`/`--boundary--`
  delimiter lines and each part's header block styled like the grammar's own
  headers (`multipart.go`) — a part's own body stays plain.
- **Typing `{{` now completes variable names.** In a request line, a header
  value or a body, an unclosed `{{` offers the file's own `@name=value`
  definitions plus the active `http-client.env.json` environment's
  variables — the persisted `http.selectEnvironment` choice, read directly
  from `.ike/httpenv.json` (`envselect.go`) rather than importing
  `internal/app`. Accepting an item inserts the closing `}}` too.

## 2026-08-25 (ui: saved filters and an issues default filter, #2115)

- **`issues.default_filter` seeds a freshly opened issues pane** with a state
  gate, a label selection and a match pattern, following the seed rule
  `issues.default_tab`/`issues.default_sort` already have: the first filter
  change by hand wins for the rest of the session, so a live config reload
  never re-narrows a list somebody is working in.
- **`issues.saved_filters` names recurring filters** (`"triage=is:open
  label:bug"`). The filter overlay gained a **saved row** — present only
  while any is configured — cycling over `(none)` plus the configured names;
  picking one replaces every narrowing at once, so switching between two
  never leaves the previous one's labels behind, and `(none)` clears them
  again. The row's name is *derived* from the live filter rather than
  remembered, so it can never go stale. Reachable from the action menu too.
- Both are written in **#2110's qualifier syntax** — the one the match input
  already accepts — so an expression can be typed in the overlay and pasted
  into the config unchanged: `is:` (alias `state:`), a repeated `label:`
  (OR'd, `label:"good first issue"` for a name with spaces), `sort:`, and
  anything else as match text. A `sort:` qualifier is the more specific
  setting and wins over `issues.default_sort`. Commas separate nothing — the
  settings UI edits the saved list comma-separated.
- The strict reader is the new leaf package **`internal/issuefilter`**: the
  config layer cannot import the pane, so it is a second implementation, and
  `ghissues/qualifier_conformance_test.go` pins the two to each other
  (vocabularies and per-expression results). The one intended difference is
  strictness — in the live input an unknown token stays literal fuzzy text
  with a note, in a config value it is an error.
- Settings UI: both entries sit on the **Issues Window** page, the filter as
  validated free text and the saved list as a validated list. The `String`
  editor gained a `ValidateString` hook, so a typo is refused in the form
  with the parser's own message instead of being silently ignored when the
  pane opens; a config file naming a broken expression drops it with a
  diagnostic (the whole default filter, or just the offending saved entry).

## 2026-08-25 (ui: structured qualifiers in the issues filter match input, #2110)

- **The filter overlay's match input accepts structured qualifiers** the way
  the JetBrains PR tool window's search field does: `label:<name>`
  (repeatable, OR, quotable for spaced names), `is:open|closed|all` (alias
  `state:`) and `sort:<order>`, with the rest of the line staying the fuzzy
  pattern. A qualifier terminated by a space (or the closing `enter`) is
  extracted from the input and **written into the same filter model the
  overlay sections edit** — chips appear, `esc` reverts, a state qualifier
  refetches exactly like the state row. Inline ghost completion for
  qualifier names, label names and values; `tab` accepts, and falls back to
  row navigation without a ghost. Unknown names and bad values stay literal
  fuzzy text with a faint note on the match row explaining why. An optional
  power layer only (`:` needs Shift on QWERTZ, #48) — every dimension keeps
  its section row and accelerator key.

## 2026-08-25 (ui: and/or toggle for the issues label filter, #2112)

- **The issues window's label filter can now match _all_ selected labels.**
  The filter overlay gained a `labels: ● any of ○ all of` row directly above
  the label section: any-of keeps the previous OR semantics, all-of narrows
  to the intersection (`hasAllLabels`). The mode reads in the overlay row,
  the overlay footer ("labels match any/all selected") and the chip row,
  which leads with a `[labels: bug+feature (all) ⌫]` chip whose clear widens
  back to any-of without dropping the labels. It reverts with `esc` in the
  overlay and clears with the label selection on the list.

## 2026-08-25 (ui: issues window key-map consolidation across modes, #2114)

- **One meaning per letter family across the issues window's four modes**
  (issue list, issue detail, PR list, PR detail), documented as a key table
  in the Issues Tool Window doc. `e` (alias `E`) now always means **edit**
  and raises a unified edit picker — labels, assignees, the issue body, your
  own comments — skipping the picker when only one target is available; `u`
  and the separate `E` text-edit action are retired. `c`/`C` mean **close**
  (with a comment) on issues and PRs alike (`C` newly aliases the PR close
  dialog). `l` means labels only: the timeline's load-more moved from `L`
  to `p` (next activity page). `g`/`G` jump to the extremes in every mode —
  the lists regained the full #1666 `ui.NavFull` semantics — and grouping by
  label lost its direct key, staying reachable in the filter overlay's
  grouping row and as a menu-only action-menu entry (footer skips keyless
  actions). Footer, action menu, tests and the wiki doc updated together;
  `keymap_test.go` pins the one-meaning-per-family table.

## 2026-08-25 (ui: speed search in the issues window's pickers, #2111)

- **The issues window's pickers narrow as you type.** The label and assignee
  mutation pickers, the filter overlay's label section and the action menu all
  got JetBrains-style speed search: printable keys filter the visible rows
  live, `space` stays the toggle, `backspace` peels the query one rune at a
  time before falling back to the picker's own "clear the set", and `esc`
  clears the query before it closes the modal. The running query renders in
  the modal's heading and the footer says what typing does; a query that
  matches nothing shows a placeholder row instead of an empty box.
- The semantics live in the reusable **`ui.SpeedSearch`**
  (`internal/ui/speedsearch.go`) with `Narrow`/`NarrowStrings`, so the IDE's
  other modal pickers can adopt them unchanged. The match is a
  case-insensitive **substring**, not the fuzzy subsequence the palette uses —
  a type-ahead is read as literal typing.
- Two key changes fall out of it: the searchable overlays run on
  `ui.NavDefault` instead of `NavFull` (`j`/`k`/`g`/`G` are query runes now,
  so arrows, page keys, `ctrl+n`/`ctrl+p` and `home`/`end` do the walking),
  and the action menu's `q`/`m`/`?` close aliases are gone — `esc` is its one
  exit. The narrowing is a view, not an edit: `enter` in a mutation picker
  still writes the full diff, so a label the query hid is never removed.
- New concept doc [Picker Speed Search](/architecture/speed-search.md); the
  Issues Tool Window and Selection-List Navigation docs link to it.

## 2026-08-25 (forge: tea OAuth logins without a plaintext token, #2118)

- **Gitea/Forgejo repositories work with an OAuth tea login.** Since tea 0.14
  a login can authenticate by OAuth, and then `config.yml` carries no
  `token:` field — the access token sits in tea's own credential store. The
  issues window used to give up on that with *"no token for the tea login …"*
  even though `tea` itself worked fine in the terminal. The tea binding now
  picks a transport per login: a token login keeps the direct REST call it
  always had, and a login without one routes the same requests through
  **`tea api`**, tea's raw API passthrough, which opens that store (and
  refreshes an expired token) itself. `tea api` returns the forge's
  unprocessed JSON, so label colors and everything else survive — unlike
  `tea issues list --output json`, which is why the binding never used the
  CLI before. Bodies still travel on stdin, and the response status comes
  from `--include` because `tea api` exits 0 on a 4xx.
- Only a tea too old for the `api` command (pre-0.12) still fails, and the
  setup message now names the cause and the fix: `tea login add --token …`.
  Details: [forge layer](/architecture/forge.md).

## 2026-08-25 (forge: persistent issue cache with incremental refresh, #2108)

- **The issues window opens instantly after a restart.** Every successful
  open listing is persisted per project (`.ike/forgecache.json`, keyed to the
  origin remote); a pane that has not loaded yet renders the snapshot
  immediately, marked `cached · updating…`, until the real fetch or the first
  background poll replaces it. While the snapshot is younger than 30 minutes,
  polls and the on-open fetch ask the forge only for issues **updated since**
  it (GitHub's REST `since`, Gitea's `since`) and merge them in; manual `r`,
  every other user-driven refetch, older snapshots, incremental errors and
  possibly truncated pages all resync fully — the rule that also re-converges
  drift from deleted or transferred issues. Repository/backend switches
  invalidate the snapshot, corrupt files read as "no cache", and the
  `forge.cache` toggle (Settings → Forge, default on) switches the whole
  mechanism off. Details: [forge layer](/architecture/forge.md).

## 2026-08-25 (issue timeline: visible divider, boxed comments, #2106)

- **The activity divider spans the pane now.** The old ` ── activity ──`
  fragment in the barely-visible `Border` role is a full-width rule in
  `Secondary` with the label in `Accent` — the body/activity boundary is
  obvious at a glance in every theme.
- **Each comment is a closed block.** Every line of a comment — header,
  markdown body, the blank lines inside it — carries a colored `▌` left gutter
  bar, so two consecutive comments no longer flow into each other while
  scrolling. Own comments take `Info`, everyone else's `Accent`; the `(you)`
  marker is unchanged. Comment markdown is wrapped two columns short to pay
  for the gutter (`renderMarkdownWrap`).
- **Events stay compact.** Non-comment rows remain single faint lines, but a
  blank line is inserted where a comment block ends so they never read as its
  last line.
- **Narrow panes degrade.** Below 24 columns the gutter falls back to a plain
  indent, and a rule with no room for its label is drawn plain across the
  width.
- `clip` measures display cells (`ansi.StringWidth`/`ansi.Truncate`) instead of
  raw runes — styled lines were being cut short by their own escape sequences.

## 2026-08-25 (markdown: hanging indent for wrapped list items, #2105)

- **Wrapped list items keep their shape.** A bullet or numbered item longer
  than the pane width used to wrap back under its own marker, so the
  continuation lost the item's indent. Glamour cannot do this itself — its
  `List.Indent`/`LevelIndent`/`Margin` knobs are block level and shift the
  marker along with the text — so `ui.HangingIndent` post-processes the
  rendered output: it re-joins each item's lines and re-wraps them at the
  width the marker leaves, aligning continuations under the text (past the
  number for ordered lists) per nesting level. Used by both the markdown
  preview pane and the GitHub issues detail/timeline rendering.

## 2026-08-25 (issues window: superseded listing fetches are dropped, #2107)

- **Rapid `t t` no longer lands the wrong listing.** Cycling the state filter
  twice in quick succession left two listing fetches in flight, and whichever
  the network finished last won — so an open-only listing could overwrite the
  view while the filter said "closed", and the client-side state gate then hid
  those rows one by one. Every fetch now carries the pane's request generation
  through `IssuesMsg.Gen` (`forge.RefreshGenCmd`, `RefreshFactory`), and the
  pane applies only its newest one.
- **Background polls respect the filter.** A poll always fetches the *open*
  listing, so it is applied exactly while the pane's own filter is open and
  dropped otherwise; the next round lands normally once the filter is back.
- **Deliberate loading presentation.** A state-changing refetch clears the
  issue list and shows `(fetching issues…)` rather than keeping rows that were
  fetched for the previous filter. The PR list is untouched — it is fetched in
  every state and split client-side.
- A dropped answer still records its `Setup`/`Err`, but never clears the
  pending fetch's loading state.

## 2026-08-25 (0470 forge workflow complete — 0.5.0, #2082)

- **The change loop no longer leaves the IDE.** Epic 0470 closes with all
  eight sub-issues merged: the forge backend abstraction (#2083), the tabbed
  issues window (#2090), the issue timeline (#2084), background polling
  (#2085), prominent event notifications (#2086), editing your own texts
  (#2087), label/assignee/state mutations (#2088) and the PR view with
  merge-with-comment and close-with-comment actions (#2089).
- **What the stream delivered.** One `forge.Forge` interface with two bindings
  — `gh` on `github.com`, `tea`/REST on Gitea/Forgejo — chosen by the remote's
  host; a capability probe that gates every mutation instead of a hard-coded
  permission; a neutral `forge.TimelineEvent` union rendered as a vertical
  activity timeline under the issue body; own bodies and comments edited in
  real editor buffers whose save hook writes back and refetches; a
  per-workspace poll loop whose tick only *schedules* the fetch, so the Update
  loop never waits on the network; and snapshot diffing into typed events
  surfaced per event kind as a centered dialog, a status-line unread badge or
  a plain toast.
- **0.5.0, a minor bump rather than a patch.** The stream added the whole
  `[forge]` config section — `poll_interval_seconds` plus six `notify.*`
  styles — and new default bindings in the issues pane. That is additive but
  visible in a settings file and a keymap, which is what
  [Versioning](/architecture/versioning.md) calls a minor.
- Concept docs for the stream: [Forge Layer](/architecture/forge.md),
  [Issues Tool Window](/architecture/github-issues.md) and the prominent-event
  section of [Notifications](/architecture/notifications.md).
- Deliberately still out of scope, for later streams: review threads and
  inline PR code comments (they need a diff-anchored UI), cross-repository
  dashboards, and creating an issue from inside IKE.

## 2026-08-25 (PR detail view with merge/close-with-comment, #2089)

- **A real PR view.** `enter` on the issues window's PR tab now opens a
  full-area pull-request detail: markdown description under an
  author/branches/state/review/mergeability meta line, a `── checks ──` list
  naming every CI check with its own glyph (both backends; Gitea reads the
  head commit's combined status), and a link line for the `Closes #N` issue.
  `ctrl+j`/`ctrl+k` walk PRs, `r` refetches, `o` still opens the browser.
- **Merge and close, with a comment, behind a confirm.** With push permission
  `M`/`c` open a two-stage dialog — optional comment (posted first), then an
  explicit confirm naming PR, branches and merge method (from the repo's
  allowed/default settings). Rejections surface the forge's own reason
  (conflicts, branch protection); successes refetch listing + detail — the
  issues too, since a merge may close its linked issue.
- **Offered post-merge cleanup.** After a merge the pane offers — never runs —
  the change-workflow cleanup: delete the head branch locally and on origin,
  switch to the default branch, pull (`forge.CleanupBranchCmd`).
- **Forge layer.** `Forge` gained `PRDetail`, `CommentPR` and a
  `MergePR(pr, method)` signature; both bindings implement them (gh:
  `pr view --json`/`pr merge`; tea: the pulls endpoints, error documents'
  `message` now surfaced). Concept docs updated:
  [Issues Tool Window](/architecture/github-issues.md),
  [Forge Layer](/architecture/forge.md).

## 2026-08-25 (background forge polling with snapshot diff events, #2085)

- **IKE now notices forge changes on its own.** A `forge.Poller` per workspace
  root drives a tick chain whose handler *only dispatches* the fetch `tea.Cmd`,
  so the Update loop never waits on the network; a tick arriving while the
  previous fetch is still running is dropped rather than queued. The Issues
  tool window consumes the fresh listing, so its content stays current without
  pressing `r`. The chain is opened by `StartForgePoll` on the `StartWatcher`
  lifecycle (main.go at startup, `switch.go` per project switch) and continued
  by each finished fetch — deliberately *not* from `Init` and *not* re-armed
  from every settled Update pass. Both would hand a `tea.Tick` to the app test
  helpers' synchronous command drainers, which block a full interval per
  deadline; arming from `Init` timed the whole `internal/app` package out at
  20 s per helper-built model.
- **Snapshot diffing emits typed events** — `IssueOpened`, `IssueClosed`,
  `PROpened`, `PRMerged`, `PRClosed`, `PRChecksFailing` — as one
  `forge.EventsMsg` for any consumer. The first fetch seeds the snapshot
  **silently**, so neither startup nor a project switch replays a backlog as
  "new" — and the PR half seeds separately, since a seeding fetch whose PR
  listing failed leaves an empty stand-in that must not read as "every pull
  request opened just now" one interval later. The event types themselves live
  in `forge/events.go`, landed with the notification surface (#2086) that
  consumes them; `Diff` fills in the author and labels its dialog shows.
- **Poll results never fight the user.** The pane restores its selection by
  issue number across a refresh, keeps the `/` filter line, its pattern and the
  label filter, leaves the open detail view open *at its scroll offset* (the
  rendered body is only re-rendered when it actually changed on the forge, and
  `ensureDetail` now rewinds to the top only when the issue itself changes),
  holds the last known linked-PR states over a partial (`PRErr`) result, and
  leaves a pending manual refresh pending.
- **Robustness:** an unavailable forge (setup problem) stops polling until a
  manual refresh succeeds; consecutive fetch errors back off exponentially
  (capped at 5 minutes, never faster than the configured interval) and reset on
  success. Exactly one toast when polling degrades and one when it recovers —
  no per-failure spam.
- **New setting `forge.poll_interval_seconds`** (default 20, floor 10, ceiling
  3600, `0` = off) with its own **Forge settings page**. Its valid set has a
  hole, so `Entry.ValidateInt` was added: the form rejects a typed value inside
  it naming the floor, and the steppers jump the hole (0 ↔ 10) — while the
  config validator, which must stay lenient for a file on disk, snaps instead.
- Docs: [Forge Layer](/architecture/forge.md) gained the polling lifecycle and
  the event table; the issues window, settings UI, config and the performance
  idle rules (the forge poll is their one documented repeating tick) follow.

## 2026-08-25 (run tests with coverage + coverage gutter, #2081)

- **Run with coverage.** `run.testsWithCoverage` (palette) runs the active
  file's test scope through the captured Test Results path with coverage
  collection; the run summary line gains a `n% coverage` figure.
- **Editor coverage gutter.** Covered/uncovered/partial lines draw a `▎` bar
  in the sign column (success/error/warning tones), JetBrains-style, below
  the informational glyphs and above the diagnostic/git tints. Editing a file
  turns its marks stale — still visible, but faint in the info tone — via
  document-version comparison in the view plus a store-level flag for later
  opens. `coverage.toggle` hides/shows the marks; the new
  `editor.marks.coverage` setting (Settings → Diagnostics-page marks block)
  is the persistent gate.
- **A neutral language seam.** `lang.TestSpec` grew `CoverArgs(profile)` and
  `ParseCover(profile, dir)`; the per-run store (`internal/coverage`) and the
  gutter only ever see the neutral `lang.FileCoverage` model, so coverage.py
  or phpunit clover can register without engine changes. Go implements the
  seam (`-coverprofile` + cover-profile parsing with import-path resolution
  against the module root). The store is separate from the test-result
  parsing, so re-run / re-run-failed never invalidate coverage of untouched
  files. Concept docs updated:
  [Test Results Tool Window](/architecture/test-results.md),
  [Editor](/architecture/editor.md).
## 2026-08-25 (edit own issue texts and compose comments, #2087)

- **The issue timeline is writable now.** In the detail view `E` edits — the
  issue body or one of your own comments, straight away when only one text
  qualifies, through a centered picker when several do — and `n` composes a
  new comment. Both are gated on the probed capabilities *and* on ownership:
  your own issue (or write access) for the body, the timeline's own-comment
  flag for a comment, a resolved login for a new one. Anything else is
  *absent* — no footer entry, no menu row, an inert key — because "not your
  text" is not a permission to fix, unlike #2088's greyed mutation actions.
- **Editing happens in a real markdown buffer**, not an inline mini-editor:
  the app creates a scratch file named after what it edits
  (`issue-2087-comment-77.md`), seeded with the current text and opened
  through the ordinary funnel, so the edit gets vim motions, markdown
  highlighting, the preview pane, undo, autosave and crash recovery for free.
  Saving the buffer pushes it: on success the scratch is removed, the buffer
  closes and the issues window refetches the listing and the timeline; on
  failure the buffer and every character in it stay put and a dialog shows the
  error with `[r]` to retry.
- **A concurrently changed text is never silently clobbered.** Before writing,
  the save re-reads the forge's current text and compares it with the base the
  buffer opened with (trailing newline and CRLF normalized away, so an
  unchanged text never reads as a conflict). A mismatch writes nothing and
  raises a warning offering `[o]` overwrite, `[l]` load the forge's version
  into the buffer, `[esc]` decide later.
- **Both bindings implement the text mutations and their read halves.** gh
  sends every body on stdin (`gh issue edit/comment --body-file -`, `gh api
  --method PATCH issues/comments/{id} --input -`), tea sends JSON to the Gitea
  endpoints; comment IDs are validated as digits before reaching a request
  path, and `Capabilities` now carries the authenticated login the ownership
  check needs. Unit tests cover the target vocabulary, the check-then-push
  order with its stale verdict, the binding fixtures, the pane's gate and
  picker, and the buffer save chain end to end. Concept docs updated:
  [Forge Layer](/architecture/forge.md),
  [Issues Tool Window](/architecture/github-issues.md).

## 2026-08-25 (label, assignee and state mutations, #2088)

- **The issues window writes now.** With triage permission: `e` opens a label
  picker over the repository's whole label set (colored chips, the issue's own
  labels preselected) and applies only the diff; `u` does the same for the
  assignees and replaces the set; `c` closes or reopens the selected issue,
  `C` the same with a comment that is posted first. All four work from the
  list and from the detail view.
- **Mutations are optimistic and roll back.** The row changes immediately, the
  pre-mutation issue is kept, and a forge rejection restores it and shows the
  forge's own error (filter row + toast). A success refetches the listing and
  the open issue's timeline, so the UI shows forge truth rather than a guess.
- **Capability gating.** Without triage the four actions vanish from the
  footer and stay in the action menu greyed, naming the reason; pressing the
  key explains instead of doing nothing.
- **Both bindings implement the mutations** — gh via `gh issue edit
  --add-label/--remove-label`, `gh issue close|reopen`, `gh issue comment` and
  an assignee-replacing `gh api --method PATCH … --input -`; tea/Gitea via the
  REST label, assignee, state and comment endpoints (label names resolved to
  Gitea's numeric IDs). New: `Forge.RepoLabels`/`Collaborators`,
  `forge.Mutation`/`MutateCmd` and the one-shot `RepoMetaCmd` probe. Argument
  and payload construction is unit-tested on fixtures. Concept docs updated:
  [Forge Layer](/architecture/forge.md),
  [Issues Tool Window](/architecture/github-issues.md).

## 2026-08-24 (issue timeline in the detail view, #2084)

- **The issue detail shows the full history now.** Under the rendered body,
  behind an `── activity ──` divider: comments as glamour-rendered markdown
  blocks (accented author, `(you)` on own comments, relative age),
  label/state/assignee changes as compact one-line events with colored label
  chips. Fetched lazily on open, page by page (30 entries): `L` loads the
  next page without moving the scroll, `r` refetches, and loading / error /
  empty states each render visibly.
- **`forge.Timeline(issue, page)` is implemented on both bindings** — GitHub
  via `gh api issues/{n}/timeline`, Gitea via its typed timeline comments —
  mapping onto the neutral `TimelineEntry` vocabulary (comment, labeled,
  unlabeled, closed, reopened, assigned, unassigned; unknown kinds dropped).
  Comments carry stable forge IDs and an own-comment flag (authenticated
  login probed once per backend) for the upcoming comment editing. Parser
  unit tests run on fixture JSON for both forges. Concept docs updated:
  [Forge Layer](/architecture/forge.md),
  [Issues Tool Window](/architecture/github-issues.md).

## 2026-08-24 (issues window UX overhaul, #2090)

- **The issues window became two tabbed full-area views.** A tab bar
  (`Issues n │ PRs n`) tops the pane; `tab`/`shift+tab`, the delivered
  `ctrl+pgup`/`ctrl+pgdown` chords and a click on a label switch views, and
  each view keeps its own cursor and scroll. Pull requests moved out of the
  cryptic one-glyph markers under the issue rows into a full-width list of
  their own — number, title, head branch, review decision, CI rollup, age.
- **The detail is a real view now.** `enter` opens the issue full area under
  an `issue x/y` header; `esc` restores the list's cursor and scroll exactly;
  `ctrl+j`/`ctrl+k` walk to the next/previous issue without going back.
- **Filtering became reachable and visible.** The fuzzy filter starts on `f`
  (QWERTZ-friendly, `/` kept as an alias) and a persistent filter row spells
  out every narrowing in force. `l` opens a multi-select label picker
  (replacing the old `l` cycling, and an OR filter now), `t` cycles an
  open/closed/all state filter that refetches through the #2083 listing
  extension, `a` cycles the sort order (relevance/newest/oldest/updated/
  number), `g` groups the list by label, and `esc` clears everything.
- **Rows carry age and author columns**, shrinking in tiers before the title
  truncates; `forge.Issue`/`forge.PR` gained state, author, review decision
  and timestamps for them, on both the gh and the Gitea binding.
- **Nothing hides any more:** one table backs the footer and a new action
  menu (`m`, `?`), listing every action of the current view with its key.
- **New settings** `issues.default_tab` and `issues.default_sort`, wired on
  the Settings UI's *Issues Window* page. Concept doc rewritten:
  [Issues Tool Window](/architecture/github-issues.md).
## 2026-08-24 (prominent forge notifications: dialog + unread badge, #2086)

- **`internal/forge/events.go`** fixes the typed snapshot-diff events the
  poller (#2085) emits: `EventKind` (issue opened/closed, PR opened/merged/
  closed, checks failing), the `Event` payload and `EventsMsg`. Each kind names
  its own `forge.notify.<kind>` config leaf.
- **`internal/app/forgenotify.go`** is the surface: a centered, dismissable
  dialog over the workspace (number, title, author, labels; enter opens the
  issue in the issues window, `d`/`esc` dismisses, `a` dismisses all), with
  several pending events collapsing into **one** dialog carrying a count — no
  dialog stacking. A do-not-interrupt guard defers the dialog to a persistent
  status-line unread badge (`● 2 new issues`) while the user is typing in an
  editor or terminal, or while another overlay owns the shell; the badge stays
  until the events are viewed (opening the issues window or the dialog clears
  it, as does a click on the segment). Every event lands in the notification
  history ring exactly once.
- **Per-event-type style setting** `[forge.notify]` (`dialog` / `badge` /
  `toast` / `off`; `issue_opened` defaults to `dialog`, `pr_checks_failing` to
  `badge`, the rest to `toast`), validated with a fallback-to-default
  diagnostic and editable in Settings → Forge Notifications.
- **`ghissues.Reveal(number)`** jumps to an issue's detail view past active
  filters; a reveal for an issue the listing does not carry yet runs on the
  next fetch.

## 2026-08-24 (forge backend abstraction + Gitea/Forgejo binding, #2083)

- **`internal/forge` grew the `Forge` interface** covering the whole 0470
  stream (listings, timeline, comments, label/assignee/state mutations,
  merge/close PR, `Capabilities()`); unimplemented operations return a typed
  `*ErrUnsupported` until their sub-issues land. The #1934 gh code moved
  behind it with no behavior change to the issues window.
- **Gitea/Forgejo repositories now get the issues + PR listing** through a
  tea binding: the tea CLI's login (matched against the remote host) supplies
  authentication, the listings and the permission probe call the Gitea REST
  API directly — tea's `--output json` flattens labels and drops their
  colors. Detection (gh for github.*, tea for everything else, explanatory
  setup message otherwise) is cached per workspace root; failures are not
  cached. New concept doc: [Forge Layer](/architecture/forge.md).
- **`Capabilities()`** reports the triage/push tiers on both bindings
  (GitHub: `gh api repos/{owner}/{repo} --jq .permissions`; Gitea: the repo
  endpoint's permissions object).

## 2026-08-24 (playground and materialize for typed buffers, #2056)

- **A typed file-less buffer reaches the playgrounds.** alt+enter in a buffer
  treated as JSON or YAML (#2033) now offers **"Open in jq Playground"** /
  **"Open in yq Playground"** (`json.jqPlayground` / `yaml.yqPlayground`),
  dispatching the dialect the buffer's language names. The playground already
  queried the focused editor's text, file or not — the entry is what makes it
  discoverable in the popup where the type was chosen, so a pasted JSON blob
  is queryable without saving it first. It is gated on the buffer actually
  holding text, because an empty input is all the playground can refuse.
- **"Materialize to File"** (`editor.materializeBuffer`, View menu and
  palette) closes the LSP gap the override left open: the buffer is written to
  a `scratch-N.<ext>` carrying its language's extension and bound to it
  through the same `bindUntitled` wiring "Save As" uses, so the file-opened
  hooks (`lsp.didopen`), watcher tracking, MRU and a reparse all run and
  diagnostics, completion and navigation start working. The override is
  dropped in the process — the file name classifies the buffer from then on —
  and `:w path` / "Save As" still moves it under a real name afterwards.
- **The materialized file lands in the scratch store** (`~/.ike/scratches`,
  `$IKE_CONFIG_DIR/scratches`), not in a second temp location: the scratch
  panel already lists, renames and deletes exactly this kind of throwaway
  file, so cleaning up is deleting it there. A refused materialize leaves
  nothing behind, and the command refuses with a reason on a buffer that has a
  file, one with no chosen type, and a type recognized by base name only
  (Dockerfile), which has no extension to write a file under.
- `intention.Context` grew one precomputed fact, `LangExt`, and the
  `hasText()` probe both entries gate on.

## 2026-08-24 (opt as alt on macOS: alt chords survive the terminal, #2064)

- **The modifier can no longer be masked by the text.** bubbletea's
  `Key.String()` returns `Key.Text` whenever the terminal reported the
  characters a key produced — right for typing (`?`, not `shift+/`), wrong for
  a chord: with the Kitty protocol's report-associated-text flag (and on the
  Windows Console API) a modified key carries the modifiers *and* the text, so
  `opt+b` arrived as a bare `"b"` and no `alt+b` binding could match.
  `FromKeyMsg` now reads `Keystroke()` — which always spells the modifiers out
  — for any press carrying ctrl/alt/meta/super/hyper, and keeps the textual
  form for shift-only presses.
- **No keystroke bubbletea can produce is unparseable any more.** The Kitty
  protocol reports `hyper` alongside `meta`/`super`; the unknown token made
  `ParseKey` fail and the event be dropped outright. All three OS-class
  modifiers now fold onto IKE's single Cmd-class `ModMeta` bit.
- **Ground truth, from raw bytes.** `internal/keymap/optkey_test.go` drives
  every encoding a macOS terminal can produce for `opt+<key>` — ESC-prefix
  (Terminal.app "Use Option as Meta Key", iTerm2 "Esc+", tmux), legacy
  CSI-parameter, Kitty CSI-u, xterm modifyOtherKeys — through ultraviolet's
  real decoder and `FromKeyMsg`, and looks the default alt chords up in the
  darwin table, so "opt+F7 fires Find usages" is a test rather than a claim.
- **The unfixable case is delimited, not papered over.** With Option left in
  its default macOS role the layout composes `opt+b` into `∫` before any escape
  sequence exists; the modifier is simply gone and guessing it back from the
  glyph would break typing those characters. The keybindings wiki gained a
  macOS Option section with the wire-format matrix and a troubleshooting table
  naming the concrete setting per terminal (Terminal.app, iTerm2 — "Esc+", not
  "Meta" — kitty, Ghostty, WezTerm, Alacritty).

## 2026-08-24 (a generic code-preview column for every position picker, #2053)

- **Four more pickers show where they lead**: the **symbol picker** (cmd+o,
  and with it the class category), the **file picker** (`project.goToFile`),
  the **bookmarks picker** (`nav.bookmarks` — vim marks, global marks and
  project bookmarks alike) and the **call-hierarchy overlay** now render the
  selected row's source beside the list, behind the same vertical rule the
  find-in-path and find-usages popups got in #2047. The excerpt follows the
  cursor; a row without a position (a directory candidate, a scratch buffer's
  mark) leaves the column blank, and a deleted file still degrades to the dim
  `preview unavailable` notice.
- **One component, no per-picker copies**: `internal/codepreview` grew the two
  pieces each consumer had duplicated — `Split` (the column geometry, formerly
  `finder.splitWidths` *and* `palette.previewSplit`) and `Cache.Columns` (the
  finished list-rule-excerpt body) — beside the existing `Cache.Render`. The
  palette's `PreviewTarget` is now an alias of `codepreview.Target`, and the
  #2047 consumers were moved onto the shared calls, so the geometry lives in
  exactly one place.
- **Opting in is one method**: a palette mode implements
  `CodePreview() bool` and puts a `Target` on each row. Modes whose rows are
  not file positions — commands, recent projects, Search Everywhere's mixed
  list — are untouched, as are anchored opens, where there is no room to split.
  The call-hierarchy overlay, not a palette mode, calls `Split`/`Columns`
  directly and grew its width cap from 100 to 120 columns to carry both.

## 2026-08-24 (copy-consistency audit across selectable panes, #2062)

- **The audit.** #2051 fixed one instance of a general shape: a state that
  captures keys sits in front of the copy key, so a selection the user can
  see cannot be copied. Every surface where text can be selected was walked
  state by state — the editor (mouse and visual selections, in insert mode,
  on the `/` line, under the find/replace panel, the peek popup, the `:s///c`
  confirmation), the terminal pane in all four of its shapes (live, finished,
  popup box, floating panel) with its scrollback search open, the response
  pane, and the playground's substitute result buffer.
- **Editor and terminal came out clean, for structural reasons worth
  naming.** The editor never loses the chord because `cmd+c` is a *modified*
  chord: the keymap layer runs ahead of pane dispatch for those even while an
  editor captures text, and it dispatches `editor.copy` as an `ActionMsg`,
  which bypasses the prompt handlers inside the editor entirely. The terminal
  never loses it because the app reserves `cmd+c` over a live selection
  *before* routing into the pane, so its all-consuming scrollback search
  cannot see the key. Both are now pinned by tests rather than left implicit.
- **Three real gaps, all the #2051 shape.** `ctrl+c` over a selection in the
  response pane hit the shell's quit binding and quit the IDE — the worst of
  the three, and on a platform without a Cmd key it meant the pane's
  advertised copy key was *only* reachable as a quit. A half-typed `z` fold
  sequence in the same pane swallowed the chord for one keystroke. And the
  playground's query line swallowed it while a selection sat visible in the
  result buffer above.
- **One rule, applied three times.** Reserve the modified chord ahead of the
  capturing state, gated on there actually being something to copy, and leave
  the bare keys alone — `y` is query text inside a prompt, `zy` is the fold
  copy, and `ctrl+c` still quits when no selection is live. `httppane` grew
  `copyChord`/`copyKeyCmd` so every state that catches the chord means the
  same thing by it.
- **The DOM inspector got the chord it never had.** Its copy actions are the
  bare `c`/`Y`, both valid selector input, so the selector prompt swallowed
  them with the target node still highlighted. `cmd+c` now aliases `c` and is
  the one key that prompt does not consume. `ctrl+c` deliberately stays the
  global quit there: the tree has no text selection to protect.
- **Left for their own issues.** The diff and merge views have no text
  selection and no copy key at all (a selection model like the response
  pane's is a feature, not an audit fix), and the Problems/Usages list panels
  cannot copy their highlighted row.

## 2026-08-24 (scratch picker shows language and first content line, #2057)

- **The scratch picker (`~`, `scratch.list`) reads by content, not by
  allocation name.** Each row's title is now its first non-empty content
  line — `scratch.FirstLine`, a lazy read capped at 64KiB so a large scratch
  still resolves cheaply — falling back to "Empty scratch" for a blank file,
  the JetBrains scratch-view convention. Fuzzy filtering runs against that
  title, so typing part of a scratch's content finds it.
- **A Detail chip names the language**, resolved via `lang.ByPath` with the
  same acronym-or-capitalize heuristic as the `scratch.new` language picker,
  falling back to "Plain Text".
- **The palette stays store- and language-agnostic**: `ScratchMode` now takes
  an injected `[]palette.ScratchEntry{Path, Title, Lang}`
  (`scratchEntries` in `internal/app/scratch_cmd.go`) instead of a bare path
  list.
- **Deleting from the picker is a deliberate non-goal**: the explorer's
  Scratches section (#1932/#1963) already deletes and renames scratches with
  a confirm dialog, so the picker stays a pure finder.

## 2026-08-24 (HTTP: copy as curl, save response body, #2059)

- **The shown exchange has two exits now.** `C` in the response pane (or
  `http.copyShownAsCurl`) copies the entry's as-sent request as a runnable
  curl command; `S` (or `http.saveResponse`) writes the **raw** response body
  to a file. Both act on the entry on show, history browsing included, and
  both are also intention-popup entries.
- **One curl serializer, two directions.** `RequestSnapshot.Curl` maps the
  snapshot onto an `httpfile.Request` and hands it to the existing
  `ExportCurl`, so quoting, `-u` for basic auth and `-F` for multipart follow
  the same rules as `http.copyAsCurl`. Header names are sorted so a request
  always exports the same command, and a binary body goes out as a base64
  heredoc piped into `curl --data-binary @-` instead of being pasted raw.
  Unlike the caret-side export, this one exports what *was* sent — the right
  answer for a re-send, an older entry, or an edited `.http` file.
- **Saving writes bytes, not the view.** The file gets `Response.Body`
  verbatim, so an image or a PDF survives — the clipboard actions cannot carry
  those at all. The path prompt (tab completion, project-relative, a directory
  takes the proposed name) is prefilled from the URL's last segment plus the
  `Content-Type`'s extension: `/things/42` + `application/json` → `42.json`.

## 2026-08-24 (Open in Find window, #2055)

- **A popup's hits can now be kept.** `cmd+enter` / `ctrl+enter` in the
  search-everywhere overlay or the transient find-usages popup tips the
  currently listed hits into the persistent Usages bottom panel and closes the
  overlay — JetBrains' "Open in Find Window". The panel keeps them until it is
  closed, so a result set can be worked through entry by entry instead of
  vanishing on the first jump.
- **One panel, two sources.** The Usages tool window is reused rather than a
  second results pane added: same "locations grouped by file, enter jumps
  there", and one singleton keeps the layout persistence, the toggle state
  machine and the mouse handling single-sourced. Only the heading differs —
  `Find: <query>` vs `Usages: Foo` (`usages.Model.SetTitled`).
- **Two small seams.** `palette.PanelMode` (opt-in per mode, names the panel)
  plus `Palette.PanelResults()`, which reports the filtered rows without
  closing the overlay; `internal/app/find_panel.go` converts rows to locations
  by their activation message and drops the ones that have none (commands).
- **A Palette-context binding.** The overlay owns the keyboard, so the keymap
  layer never sees the key: the overlay branch looks the chord up in the
  `palette` context itself and runs it only for an allowlisted command
  (`find.openInPanel`), leaving query typing untouched. It shows in the
  cheatsheet like any other binding.

## 2026-08-24 (clipboard history: pane copies, a configurable ring, #2061)

- **Pane copies reach Paste from History**: every host-side copy — the HTTP
  response viewer, the DOM tree, the data viewer and CSV column profiles,
  path/reference copies, the terminal's mouse selection, the timeline's commit
  hash, the perf-HUD snapshot, curl export, the regex pattern, a screenshot
  path — now goes through `Model.copyToClipboard`, which writes the system
  clipboard *and* pushes the text onto the app-wide history ring
  (`register.Store.PushHistory`). `cmd+shift+v` offers them next to the
  editor's yanks and deletes; the external clipboard is still never polled.
- **Duplicates collapse anywhere in the ring**: re-copying text already in the
  history moves it to the front instead of adding a second row — before, only
  an exact repeat of the *newest* entry was dropped.
- **The ring size is a setting**: `editor.clipboard_history_size` (default 20,
  clamped to 1–200) replaces the hard-coded cap, configurable in Settings →
  Editor. Shrinking trims oldest-first immediately. The ring stays in-memory
  only — a restart starts it empty, like the register set itself.

## 2026-08-24 (autocomplete follows the buffer language, #2048)

- **A file-less buffer given a language now completes like a file of it.**
  Since #2033 "Treat Buffer as …" makes an untitled buffer Go or Markdown for
  highlighting, concealing and intentions; completion did not follow, because
  every source resolved the language from a *path* the buffer does not have.
- **Two names beside the path.** Editor events and `complete.Request` carry
  `Key` (the view's `editor.ParseKey` — the buffer's identity) and `LangPath`
  (its `langPath()` — the language's synthetic name), read through
  `BufKey()` / `LangName()`. Sources index buffer text by identity, so two
  file-less buffers no longer share one entry, and gate on the language name,
  so live templates, Emmet, postfix, the symbol index's grammar extraction and
  exclusive claims all answer. Both fields are empty for a file buffer and
  fall back to `Path`.
- **The answer routes back by key.** A batch carries `CompletionMsg.Key` and
  the app delivers it through `routeToEditorKey` →
  `pane.Instance.UpdateForParseKey` (the async-parse route), so a popup can
  open in a buffer no path route could find. The LSP bridge sets no key and
  keeps routing by path.
- **Out of scope, deliberately**: LSP (needs a real `file://` document) and the
  ES console's query source (its index name comes from the file name).

## 2026-08-24 (bounded result popups with a code preview, #2047)

- **Find in Path and Find Usages got a code column**: both popups now render
  the selected hit's file around its line to the right of the result list,
  separated by a vertical rule, so one sees where enter lands before pressing
  it. The excerpt follows the list cursor and degrades to a dim
  `preview unavailable` notice for deleted or unreadable targets.
- **Stable popup height**: the result region of both is clamped to
  `ui.MinResultRows`…`ui.MaxResultRows` (**11 to 40** rows) and blank-padded to
  it. No more box that collapses onto two hits or grows without bound while a
  scan streams in — past forty rows the list scrolls inside the popup.
- **New shared pieces**: `internal/codepreview` (windowed, cached file-excerpt
  renderer — reads only the lines it shows) and `ui.JoinColumns` / `ui.PadRows`
  / `ui.ClampResultRows`. The palette side is an opt-in `PreviewMode` Mode
  extension plus `Item.Preview`, so only the references mode splits its box;
  every other mode renders exactly as before. Presses in the excerpt column are
  inert in both popups.

## 2026-08-21 (yq playground on the shared playground base, #2039)

- **A yq playground**: YAML buffers get the playground the jq one has had since
  #1936, under `yaml.yqPlayground` / `yaml.yqPlaygroundAtPath` (palette, Tools
  menu, and the caret intention over a YAML value). Live query against the
  buffer or the visual selection, YAML rendered back into the read-only result
  buffer, `ctrl+y` copy and `ctrl+o` scratch as `.yaml`, history and saved
  filters as before.
- **One implementation, two dialects**: nothing was copied. `jqplay.Dialect`
  (`internal/jqplay/dialect.go`) is the whole seam — how a buffer is decoded,
  how a value is written back, how the result folds — and the app-side mode was
  *renamed* from `jq…` to `play…` and parameterized rather than duplicated
  (`internal/app/playground.go`, `playcomplete.go`, `playfilters.go`, formerly
  `jqplayground.go` / `jqcomplete.go` / `jqfilters.go`). The query line,
  completion, geometry, key routing, debounce, result buffer and filter store
  are shared verbatim.
- **Engine**: gojq for both. YAML decodes into the value shapes gojq already
  runs over and is re-encoded on the way out (`internal/jqplay/yaml.go`), the
  trade `gojq --yaml-input --yaml-output` makes — yq's expression language is
  jq's for everything a query line holds, so a second engine would only have
  bought a second builtin list, scanner and error vocabulary. Given up: yq's
  comment/anchor preservation and `style`/`tag` operators, none of which a
  read-only query tool has a job for.
- **YAML specifics handled**: anchors and aliases resolved under a
  `MaxYAMLNodes` budget (a billion-laughs file is an error line, not an OOM),
  merge keys (`<<`) folded in with explicit keys winning, non-string keys
  stringified so `1:` is reachable as `."1"`, `0x1f`/`1_000`/20-digit ids kept
  as numbers with their digits, multi-document `---` files queried as a stream
  and rendered back as one.
- **YAML folding**: `yamlfold.go` scans the rendered result by indentation
  (JSON has delimiters, YAML does not) — mapping blocks, sequences and block
  scalars, the last counting `⋯ 7 lines`. `Fold` grew `Unit`/`Closer` in place
  of the JSON-only `Object` flag, so one `Label` serves both.
- **Filters are per dialect**, `yqfilters.json` / `yqfilters-global.json`
  beside the jq pair: a filter written for a Kubernetes manifest has no
  business in the picker over an API response. The **history stays shared** —
  unnamed scratch work in the same language. `json.jqSaveFilter` is retitled
  "Save Playground Filter…" and `json.jqQueryView` "Toggle Full Query View";
  both act on whichever playground is open.
- **Wiki**: `architecture/jq-playground.md` becomes "jq & yq Playground" and
  gains "The yq dialect" (engine trade-off, what differs, what is shared);
  folding, the filter library, the history and the keys table updated.

## 2026-08-21 (Palette: projects-column scroll window + scrolloff, #2041)

- **The Recent Projects column scrolls**: it gained its own window (`sideTop`,
  `scrollSideToSelected`, `sideVisibleRows`) beside the main list's `top`.
  Keyboard navigation, paging, rendering and the click mapping all go through
  it, so with more projects than rows everything below the fold is reachable
  again instead of invisible; `enter` and the aux actions (close workspace,
  prune from history) still hit the selected row after scrolling.
- **Scrolloff of one entry**: both palette windows follow the selection through
  the new `ui.ScrollToShowOff(top, sel, height, n, off)` with `off = 1`, so the
  window moves on already when the cursor reaches the second-to-last visible
  row — the next entry is always visible. The margin is capped at
  `(height-1)/2` and clipped against the list ends, so short lists and the
  first/last entries do not jitter or scroll blank rows into view.
  `ScrollToShow` is now the `off = 0` case of the same helper.
- **The wheel reaches the palette**: `Palette.Wheel(x, y, delta)` scrolls the
  column under the pointer (projects column on the left, results on the right),
  moving the column focus with it like a click does and clamping at both ends;
  `handleMouse` routes coalesced wheel bursts inside the box to it.

## 2026-08-21 (Layouts: the saved layout is the whole truth, #2042)

- **Apply is verbatim**: applying a saved layout (#1175) reproduces the
  snapshot exactly — tool panes, multi-tool tab hosts and singleton panels
  included — matching what the startup restore always did. The #1899
  apply-time reconciliation ("current slot config wins",
  `internal/app/layouts_slots.go`) is **removed**; the `[tools.layout]`
  template now only governs where tools open at *runtime*. A live configured
  tool the applied layout does not mention re-places at its slot/home
  position (`leftoverConfiguredTools`/`replaceToolPane`) instead of grafting
  into the flexible region.
- **Multi-tool panes are first-class**: a `tools` slot re-slots the live host
  whose tab composition best matches the saved tool list (`takeBestHost`; a
  host sharing no tool is never adopted, so tool areas cannot swap) and
  restores any saved tool missing from its tabs in place
  (`restoreMissingToolTabs`) — save/apply/restart/switch reproduce exactly
  the saved tab set.
- **Project switch groups tools**: `attachGlobalToolIn` tab-joins at every
  tier — slot resident, home-dock occupant (the "global tools never
  tab-host" rule is gone; the switch detaches tab-hosted globals tab-wise),
  and, for unplaced tools, an existing tool pane/tool host (`toolAreaPane`)
  — before falling back to the adaptive split, so arriving tools land
  grouped at their configured position, never scattered over the editor
  area. Fresh opens at an occupied home dock tab-join for global tools too
  (`openToolAtHome`; the `dockTabs` flag is gone).
- **HTTP viewer is a tool**: `KindHTTP` left `pane.KindTabbable` — the
  response viewer no longer nests as a content tab (which made every later
  save/apply treat it as anonymous editor content), keeps its edge-only drag
  zones and always persists/applies as the fixed-position `http` singleton.
  A legacy `layout.json` with a nested `http` content tab restores without
  that tab (the viewer restored empty anyway) — no crash, documented
  migration.
- The save-selector mini-map labels a multi-tool host with its tool names.
- Docs: `wiki/architecture/pane-layout.md` (new model statement, rewritten
  saved-layout bullets), `tool-panes.md`, `project-switching.md`. Tests:
  `internal/app/layouts_redesign_test.go`, rewritten
  `layouts_slots_test.go`, `internal/pane/contenttab_test.go`.

## 2026-08-21 (jq playground: editing the query across its rows, #2038)

- **The multi-line view is now an editor** (`internal/app/jqplayground.go`):
  the rows `json.jqQueryView` (`ctrl+alt+e`) lays a program out over (#2032)
  take the caret. `↑`/`↓` move it row by row keeping a **goal column**
  (`jqPlayState.qgoal`, cleared by every other key), `home`/`end` are
  row-local, `ctrl+home`/`ctrl+end` stay the whole program's ends, and a click
  on any query row puts the caret on the clicked cell. Highlighting,
  completion and the debounced live run are the one-line query line's,
  unchanged — it is one query line with more rows.
- **The history moves to `alt+↑`/`alt+↓`** there, and a plain `↑` on the first
  row (or `↓` on the last) still hands over to it, the way a multi-line shell
  prompt does. The info row's hints say which meaning the arrows have in the
  view that is up.
- **`jqplay.RowCol` / `jqplay.PosAt`** (`internal/jqplay/wrap.go`): the caret's
  coordinates in a wrapped program and back. `RowCol` differs from `LineAt` in
  the one case that matters — a caret on the end of a row the wrap broke at a
  pipe belongs to *that* row, past its last rune, because the break dropped a
  blank between the two; without it `↑` from the row below would land on a
  position that reads as being on the row below again and the motion would
  never arrive.
- **One layout for everything** (`jqQueryWindow`): the rendering, the click
  mapping and the completion popup's anchor read the same wrapped rows and the
  same window start, so the row drawn, the row clicked and the row the popup
  hangs under cannot disagree. The popup now anchors on the **caret's** row,
  and a caret standing in the blanks a stage break dropped is drawn on its
  row's first cell instead of vanishing.
- **The program stays one line.** Breaks are display and editing only, never
  runes: the history, the per-file last-valid program (#1982), the saved
  filters (#1995) and `.http` `# @capture` expressions keep speaking about
  one-line jq programs, and a multi-line paste is still flattened. The
  decision is written down in the concept doc.
- **Tests**: `internal/jqplay/wrap_test.go` (`RowCol`/`PosAt` round trip, the
  row-end rule, hard splits); `internal/app/jqplayground_test.go` (row walking
  with the goal column, row-local home/end plus an edit several rows up that
  runs live, the history keys, click-to-place, the window following the caret
  past the cap, the program staying one line through history and saved
  filters, the hints); `internal/app/jqcomplete_test.go` (the popup anchored on
  the caret's row).
- **Wiki**: `architecture/jq-playground.md`'s "The full-query view" becomes
  "The multi-line view" and gains "Editing across the rows" with the key table
  and the one-line decision; `architecture/text-input.md`'s site table notes
  the vertical motion.

## 2026-08-21 (Editor: pasted content classifies an empty buffer, #2037)

- **`lang.DetectContent`** (`internal/lang/detect.go`): the content-shaped
  counterpart to the path-shaped lookups — a pure function, text in, language
  id (or `""`) out, specified by a table in `detect_test.go`. It recognizes
  JSON, XML/HTML, `.http` request blocks, curl commands (as `shell`), YAML,
  Markdown and CSV/TSV, most-specific first. Every check is deliberately
  conservative: JSON must parse, YAML needs every line to be structure plus
  two keys or a `---`, Markdown needs a heading/fence/table/link, CSV needs
  consistent widths and a table that is wide or tall. Prose, source code and
  anything ambiguous get no verdict — the normal outcome.
- **The paste trigger** (`internal/editor/langdetect.go`): every paste funnel
  — vim `p`/`P`, clipboard paste in normal and insert mode, visual put, the
  terminal's bracketed paste — snapshots candidacy *before* the insert and
  classifies *after* it, installing the verdict through the #2033 override.
  Gates: no file, no language already set, nothing but whitespace in the
  buffer. Nested funnels re-check them, so one paste classifies once.
- **Feedback is one toast**, never a modal: `buffer language: detected json —
  alt+enter to change`, parked in `Model.detectSignal` (the vim paste paths
  return no command) and drained by `maybeReparse` or `PasteText`. An
  unrecognized paste says nothing at all, and a wrong verdict is corrected by
  the #2033 picker without touching the text.
- Tests: `internal/lang/detect_test.go` (the content → verdict table, positive
  and negative), `internal/editor/langdetect_test.go` (the acceptance cases,
  the three gates, the single toast, correctability, the vim and insert-mode
  funnels).

## 2026-08-21 (Intentions: every entry audited for real applicability, #2026)

- **The rule** (`wiki/architecture/intention-actions.md`): an intention entry
  is offered only where its command would actually do something. "Pick it and
  read the error" is a bug, and so is picking it and watching nothing happen.
  Applicability is *all* of the command's preconditions — caret situation,
  buffer state, and the external state it reads — not just the positional one.
- **The audit**: all 24 catalog entries in `internal/intention/catalog.go`
  were checked against their command implementations; the doc carries the
  full result table (tightened / already exact).
- **`editor.explainConceal`** (`internal/editor/intentions.go`): the reported
  issue. `ConcealExplainAtCaret` now requires a conceal stand-in under the
  caret instead of any value the explainer can resolve — the entry appeared on
  `getConfig` in a Python import and the popover then said nothing conceals it.
  `g?` and the palette keep the "why is this *not* masked" reading (#1930).
- **Also tightened**: the HTTP response copies and `http.resend` need a shown
  response, `http.selectEnvironment` an env file defining one;
  `http.insertCurlAsRequest` needs the gathered command to *parse* (shared
  probe `httpfile.CurlCommandAt`, so the gate and the conversion read one
  command); `json.jqPlaygroundAtPath` no longer appears over a selection;
  `debug.testAtCursor` needs a debug adapter and no running session; the
  file-scoped VCS actions need `vcs.Snapshot.Contains` (new — a file from
  outside the repo answered `StatusNone` like a clean one);
  `lsp.ignoreDiagnostic` needs a diagnostic a rule can be built from; and the
  rewriting entries need a writable buffer (a read-only `.http` buffer sends
  the curl conversion down the scratch route rather than dropping the edit).
- **Tests**: `internal/intention/catalog_test.go` (new rows per tightened
  gate), `internal/app/codeactions_test.go` (the gates needing a whole model),
  `internal/editor/intentions_test.go` (new — the caret probes),
  `internal/app/intentions_test.go`, `internal/httpfile/curl_test.go`,
  `internal/vcs/status_test.go`.

## 2026-08-21 ("Treat Buffer as …" — a language for file-less buffers, #2033)

- **Buffer language override** (`internal/editor/langoverride.go`): a buffer
  with no file can be told which language it is. The chosen id resolves to a
  synthetic name — `buffer.md`, `buffer.http`, `Dockerfile` — and `langPath()`
  hands that to every path-keyed lookup in the editor in place of the empty
  path, so highlighting, linting, concealing, comment toggling, indent rules,
  snippets, smart typing, id colors, the conceal file filter, the doc-path
  scanner, markdown rendering, the csv table layer and log rendering all treat
  the buffer as a file of that type. `Path()` stays empty — the name never
  reaches file I/O; LSP and runners are deliberately out of scope.
- **A path always wins**: the override is dropped when the buffer gets one
  (`Load`, `NewFile`, `:w name`), so a saved buffer is classified by its file
  name. A split inherits the type like the encoding (`ShareDocumentWith`).
  Switching languages invalidates the version-keyed rendering caches
  (`resetLangState`), which a language change alone would leave answering for
  the old type until the next edit.
- **Parse results route by view** (`editor.ParseKey`, `routeParse`,
  `pane.Instance.UpdateForParseKey`): a `SpansMsg` used to be delivered to the
  editor leaves *showing its path*, which skipped every path-less buffer — the
  chosen language resolved and still rendered plain. Each view now carries its
  file path or a unique tag (`\x00buffer/<n>`) and accepts only results under
  it; the Problems store keeps its path key (empty for a view-tagged result,
  as before).
- **The door** is the alt+enter intention "Treat Buffer as …"
  (`editor.setBufferLanguage`, `internal/intention/catalog.go`), offered in
  file-less buffers only — a saved file is classified by its name and the
  editor refuses the override there. `Context.Fileless` carries the fact
  explicitly, so a zero context still offers nothing. The entry names the
  current type (`Treat Buffer as… (now markdown)`).
- **The picker** (`internal/app/bufferlang.go`) is a locked palette mode
  listing "Plain Text" plus every language a path lookup could match; the
  status line's new `buflang` segment shows the choice (`as Markdown`) and a
  click on it reopens the picker.
- **HTTP by type, not by file name**: `isHTTPBuffer`/`httpSource` replace the
  `HasFile() && isHTTPPath(Path())` gates, so a pasted request block in a
  file-less HTTP buffer runs, converts to curl and shows in-flight marks; its
  dispatches are attributed to `buffer.http`.
- Tests: `internal/editor/langoverride_test.go` (resolution, refusals, path
  wins, comment/markdown/csv consumers, split, parse-key identity and
  delivery), `internal/app/bufferlang_test.go` (intention visibility, picker,
  applied type, status segment, HTTP intentions, parse routing),
  `internal/intention/catalog_test.go` (the empty-context floor).

## 2026-08-21 (jq playground: the full query expression on demand, #2032)

- **`jqplay.Wrap` / `jqplay.LineAt`** (`internal/jqplay/wrap.go`, new): pure,
  rune-indexed line breaking for a jq program — greedy packing that breaks at
  top-level `|` stages (a `|` in a string or comment and `||` are not
  boundaries), a hard cut at the width for a stage that is wider than a row,
  and the blanks after a pipe dropped at a stage's row start.
- **The full-query view** (`internal/app/jqplayground.go`): `json.jqQueryView`
  (`ctrl+alt+e`, palette / Tools menu) toggles the one-line query header into a
  wrapped multi-row one — same scanner colors, same cursor, program and result
  untouched. `jqHeaderRowsFor` now reports the query rows plus the info row, so
  the rendering, the result buffer's height and the mouse translation all move
  with the header; the view caps at 8 rows, always leaves 3 rows of result, and
  windows around the cursor's row past the cap. The info row gained a
  `· query cut` marker in Warning whenever the rows show less than the program
  holds, and the key hints name the chord bound to the command.
- **Tests**: `internal/jqplay/wrap_test.go` (pipe breaks, full coverage of the
  program at five widths, hard splits, literals, `LineAt`);
  `internal/app/jqplayground_test.go` (the whole program readable after the
  toggle, highlighting kept, the header/result geometry stays exact, the cap
  windows and flags, the hints document the chord from both focuses, the
  command form).
- **Wiki**: `architecture/jq-playground.md` gains "The full-query view" and the
  key-table row; `architecture/keybindings.md`'s ledger and the generated
  reference pages pick up `json.jqQueryView`.

## 2026-08-21 (fold objects and arrays in the jq result window, #2029)

- **`jqplay.Folds`** (`internal/jqplay/fold.go`): the foldable objects and
  arrays of a result, in pre-order, each with the number of members it holds —
  a rune walk over the pretty-printed text counting delimiters outside strings,
  never a re-decode. `Fold.Label` is the collapsed row's placeholder
  (`⋯ 3 keys }`, `⋯ 12 items ]`).
- **Host folds** (`internal/editor/hostfold.go`): `SetHostFolds` installs
  ranges a host computed for a synthetic buffer — merged over the Tree-sitter
  ranges by `foldRanges`, winning on a shared header — and `SetFoldSummary`
  overrides the collapsed header's placeholder text. Everything else about
  folding (#144, #1741) stays untouched: one fold engine, one collapsed set.
- **jq playground** (`internal/app/jqplayground.go`): the result buffer folds
  with `za`/`zc`/`zo`/`zM`/`zR`, nested included; the ranges are reinstalled
  with every result, so a changed filter leaves no fold behind. The info row
  advertises the keys while the buffer holds the keyboard.
- **Why the playground computes its own ranges**: the result window must fold
  in a cgo-free build (no grammar there), and only the structural scan knows a
  node's *member* count, which is what the placeholder says.
- See [jq Playground](/architecture/jq-playground.md#folding-the-result).

## 2026-08-21 (alt+enter in a fileless buffer froze the IDE, #2027)

- **`host.Host.Send`** (`internal/host/host.go`): messages are queued into an
  outbox that a single dispatcher goroutine drains into the program, instead
  of being handed to `Program.Send` on the caller's goroutine. bubbletea's
  Send blocks until the event loop *receives* the message, so a Send issued
  from inside Update deadlocked the program against itself — the whole IDE
  froze, no crash, no error. Delivery keeps Send order; a caller never blocks.
- **Root cause** (`plugins/lsp/bridge.go`): `codeAction` answers a buffer with
  no path on the spot (no server to ask) and did so with `h.Send` while
  running on the Update goroutine — alt+enter in an untitled tab or a pasted
  response body froze IKE. It now returns the offer as a `tea.Cmd`
  (`h.Dispatch`). The same shape sat in the local hover/definition/references
  provider paths (#922/#1629) and in `codeLensPick`: `ctrl+q` on a YAML alias
  froze the IDE identically (verified against the pre-fix binary), and the
  host fix is what covers those.
- **Tests**: `internal/host/host_test.go` (Send returns while the program is
  busy, queued messages keep Send order, no queueing before the program
  exists); `plugins/lsp/codeaction_fileless_test.go` (the fileless offer comes
  back as a command); `internal/app/codeactions_test.go` (alt+enter in a
  fileless buffer opens the anchored picker with the path-free built-ins, and
  toasts when nothing applies).
- **Wiki**: `architecture/plugins.md` gains a "Dispatch vs. Send" contract,
  `architecture/lsp.md` extends its never-block rule, `architecture/intention-actions.md`
  documents the fileless path.

## 2026-08-21 (Intentions: rename gated by position, Markdown heading rename completed, #2025)

- **Position gate** (`plugins/lsp/renamegate.go`, new): the intention popup's
  "Rename Symbol" entry now needs a `prepareRename` verdict for the caret, not
  just the server's rename capability (#2020). `Items` is synchronous and the
  check is a round trip, so the bridge validates instead: `codeAction` fires
  `prepareRename` concurrently with the code-action request and records the
  verdict for that one `(path, line, col)` before the message that makes the
  app query the providers goes out. The popup therefore still waits on one
  round trip. An unvalidated caret is not offered; a buffer edit drops the
  verdict; servers without `prepareRename` keep the #426 contract (offered,
  the rename attempt decides).
- **"cannot rename here" is unreachable from the popup**: picking the entry
  reuses (and consumes) the recorded verdict — including its placeholder —
  instead of asking a second time whose answer could differ.
- **Markdown heading rename** (`plugins/lsp/markdown_rename.go`, new): checked
  what marksman delivers before building anything, as the issue asked — it
  already rewrites every same-document `](#old-heading)` alongside the
  heading, and IKE applies those edits. The gap was the link *title*: a TOC
  entry kept reading "Old Heading" while pointing at `#new-heading`, invisible
  because IKE conceals destinations. Now a link whose destination the server
  just rewrote and whose title spells the old heading exactly is retitled too,
  in the same edit slice (one undo unit). Differently worded titles are left
  alone; no anchor slugification is reimplemented.
- **Wiki**: [intention-actions](./architecture/intention-actions.md) and
  [lsp](./architecture/lsp.md) updated.

## 2026-08-21 (Intention popup: digit shortcuts, #2023)

- **`palette.DigitPicker`** (new, `internal/palette/mode.go`): optional Mode
  extension (`DigitShortcuts() bool`) the palette consults for the **locked**
  mode on an **empty** query — `1`–`9` then activate the nth listed row
  through the ordinary activation path (`Palette.quickPick`). A digit past
  the last row is swallowed; every non-opting mode keeps plain digit typing.
- **`Item.Hint`** (new): dim leading shortcut column rendered before the row
  title (`Palette.hintView`), Spans-neutral.
- **`actionsMode`** (`internal/app/codeactions.go`): implements the seam and
  numbers the first nine rows of the *unfiltered* list; once a filter query
  is typed the hints drop and digits type into the query again (chosen over
  renumbering against the filtered list).
- **Wiki**: `architecture/intention-actions.md` gains the "Digit shortcuts"
  section and an "Adding new actions" guideline — new caret-/context-dependent
  commands should also be surfaced as intention items.

## 2026-08-20 (Intention actions: context-aware built-ins merged into alt+enter, #2020)

- **`internal/intention`** (new): the intention-action seam — `Context` (caret
  snapshot: position, language, line access, precomputed caret facts), `Item`
  (`{Title, Kind, CommandID}`, always a registered command), `Provider` (pure
  `Items(Context)`), plus the built-in catalog (`Builtins()`): doc-path
  copies + jq playground, `.http` request actions, curl→request insert, JWT
  decode, conceal explain + family toggle, ignore-diagnostic, VCS
  hunk/conflict/blame/history, run/debug test, toggle value, compare with
  clipboard.
- **Seam wiring**: `plugin.Capabilities.Intentions` +
  `registry.IntentionProviders()` (dedup, deterministic order) — providers
  are a plugin capability; the catalog ships through the `app` plugin, the
  LSP plugin contributes a capability-gated "Rename Symbol"
  (`Manager.RenameSupported`, new).
- **Flow**: `lsp.codeAction` (id kept, retitled "Show Intention Actions")
  always delivers `CodeActionsMsg{Intentions: true}` — no manager, empty or
  failed reply included; the app merges (★ preferred LSP → LSP → built-ins
  grouped by kind), opens the picker **anchored at the caret**
  (`caretPopupAnchor`), and owns the "no code actions here" toast for the
  merged list. Code-lens picker unchanged (centered).
- **`http.insertCurlAsRequest`** (new command): caret curl line (+
  backslash continuations) through the #1994 parser — replaced in place in an
  `.http` buffer, else into a fresh scratch `.http`; ignored-flags warning
  preserved.
- New editor probes (`internal/editor/intentions.go`), `concealfilter.IsFamily`.
  Added the Intention Actions concept doc; updated the LSP and Plugins docs.

## 2026-08-20 (OpenAPI import from a URL, #2009)

- **`internal/openapi/fetch.go`** (new): `openapi.Discover` resolves an
  `http(s)` URL to a parseable OpenAPI 3.x document. A URL whose path names a
  document is fetched as it is; a base URL is probed over the fixed, documented
  `ProbePaths` order (`/openapi.json`, `/openapi.yaml`, `/openapi.yml`,
  `/swagger.json`, `/swagger.yaml`, `/v3/api-docs`, `/api-docs`,
  `/api/openapi.json`, `/api/openapi.yaml`), first parseable answer wins; a
  path prefix that is not a document is probed *under*. Probing is sequential
  with a five second `ProbeTimeout` per request, and a transport failure ends
  the run at the first path — a dead host must not cost nine timeouts. Errors
  are typed so the dialog can name the actual cause (transport, HTTP status,
  parse error, "no OpenAPI document at … — probed …").
- **`internal/openapi/import.go`**: `ImportDocument` splits out of
  `ImportFile` — the import of bytes already in hand, which is what a
  confirmed URL import generates from, so the validated document and the
  imported one are the same bytes.
- **`internal/app/openapi_import.go`**: the prompt takes a path *or* a URL. A
  path still imports on the first enter; a URL is validated first (off-loop
  discovery, sequence-numbered so a stale answer for an edited input is
  dropped) and only a second enter confirms it. The resolved URL and its
  operation count are shown before confirming; a failure paints the popup in
  the theme's error colour and shows the reason. Tab completes paths only.
- **`internal/ui/floating.go`**: `SetAccent`/`Accent` let hosted content
  override the shell's border colour until it closes — the seam the red
  failure state uses.

## 2026-08-20 (Number-hint mapping: input base and skipped entries, #2008)

- **`internal/numhint`**: `EntryError` words why an `editor.number_hint_units`
  line cannot be used (no `=`, empty pattern, unknown unit — the message lists
  the vocabulary), `SetFieldUnits` keeps the entries it had to skip and
  `InvalidEntries` reports them, `UnitVocabulary` names the accepted words.
  A blank list element is nothing to complain about. The mapped unit already
  was the base the raw number is read in — `request_timeout=s` reads `1500` as
  1500 seconds and draws `25m`, not the built-in `timeout` word's `1s500ms` —
  in every position, config formats and code alike; table-driven tests lock
  that down per base and per code shape (kwarg, def default, assignment,
  constant, computed expression, Go and PHP).
- **`internal/app`**: `unitMappingDiags` turns the skipped entries into
  `editor.number_hint_units` config diagnostics, toasted like `associationDiags`
  — a silently dropped entry reads as the mapping being ignored, because the
  field simply keeps its built-in reading. The mapping and the secret key
  patterns now install *before* the reload's diagnostics are worded.
- **Settings**: the *Number hint field units* list rejects an element the
  mapping would skip, with the same message, and lists the unit words while
  the entry is typed.
- Wiki: [Editor](/architecture/editor.md) — field-unit mapping.
## 2026-08-20 (In-IDE screenshot export, #2001)

- **`internal/app/screenshot.go`** (new): `view.exportScreenshot` and
  `view.exportWindowScreenshot` paint the running IDE into a PNG through
  `internal/shotpng` — the docs renderer, until now only driven offline by
  `cmd/shotgen`. The subject is the composed frame `Model.render()` produces
  (never a re-render from file content), cropped by cell region: the focused
  pane's layout rect, or the whole window. Painting and writing run off the
  update loop behind a `tea.Cmd`; the fonts load once per process. The new
  `screenshot.directory` setting (Settings → Screenshots, default
  `~/.ike/screenshots`) names the output directory, files are
  `ike-<pane|window>-<timestamp>.png`, and the written path lands in the
  clipboard and a notification. Both commands are also View-menu entries.

## 2026-08-20 (SFTP remote file browsing, #1997)

- **`internal/remote`** (new): the SFTP browser. `Conn` is the mockable
  session interface; the real one dials `ssh -o BatchMode=yes -s <alias>
  sftp` and speaks SFTP over the subprocess's stdio (`github.com/pkg/sftp`,
  the first use of that dependency) — auth, jump hosts and known_hosts stay
  ssh's job, the #1938 stance. The pane model is an archview-shaped lazy
  tree with the ES console's async discipline (background dial + scans,
  results routed by pane key), a reveal-at-$HOME walk, the explorer's `.`
  hidden toggle, and merge-on-rescan. `CachePath` mirrors the remote tree
  under `<UserCacheDir>/ike/sftp/<alias>/…` with sanitized segments;
  `VPath`/`ParseVPath` carry the `sftp://<alias><path>` virtual-path scheme.
- **`internal/pane`**: `KindRemote`, keyed `remote:<alias>` (one browser per
  host, reopen refocuses); close ends the session and the ssh subprocess.
- **`internal/app/remote.go`**: `remote.browse` ("Browse SSH Host (SFTP)…",
  palette + Tools menu) opens the ssh host list in a browse picker mode; a
  download that lands opens via the file-handler dispatch on the cached
  local copy (archives, gz, images, databases) or as a read-only
  `ShowReadOnly` buffer titled `name (alias) [RO]` — saves refuse with E45,
  never silently landing in the cache. Persistence follows the ES console:
  `{kind: "remote", path: alias}`, restore re-dials in the background.
- **Setting** `remote.max_fetch_mb` (default 64, Settings → Remote
  Browsing): the download cap, enforced against the listing size before any
  bytes move and again during the fetch.
- Wiki: [Remote File Browsing (SFTP)](/architecture/remote-browsing.md).
## 2026-08-20 (Agent change feed, #2000)

- **`internal/changefeed`** (new): the session-scoped record of files changed
  by something other than IKE, as pure data — no I/O, no clock, no config, so
  ordering, coalescing and the caps stay testable without a filesystem. An
  `Entry` is one *file*: path, time, kind (changed/created/removed), a repeat
  count, and the pre-change content with the `Origin` that produced it
  (`FromBuffer`, `FromSnapshot`, `NoBefore`, `Dropped`). `Add` coalesces per
  path — the row moves back to the front, counts up, and keeps the content it
  was *first* recorded with, because reverting an agent's fifth rewrite means
  restoring what the file held before the first one. Kinds merge like the
  watcher's (removal wins; a created file stays created). Two caps: `Limit`
  bounds the rows, and `MaxBytes` bounds the retained pre-change content across
  all entries — past it the oldest entries release their content and go
  `Dropped`, keeping their row. Without the byte cap a batch over large files
  grew the session's memory with every write; without keeping the row the user
  would silently lose the fact that the file was touched.
- **`internal/watch`**: `Ignored(root, path)` exports the rule the recursive
  walk already prunes with, judged on the segments *below* the root (so a
  project under `~/.config` is not wholesale ignored), plus `Root()` for the
  root it is relative to. Exported rather than re-derived, so a consumer
  filtering watcher-derived lists cannot drift from what the watcher actually
  descends into.
- **`internal/app/changefeed.go`** (new): `recordChangeFeed` runs from the
  `watch.EventMsg` case *before* the event is routed onward — the auto-reload
  it triggers would otherwise overwrite the buffer the pre-change content is
  read from. Own saves are dropped twice over (the watcher's save epoch, then a
  `SavedRecently` re-check); non-file kinds never enter. The "before" side is
  the open, unmodified buffer where there is one — a dirty or stale buffer holds
  text that was never on disk, so the newest local-history snapshot (#1023) is
  the fallback. `watch.changeFeed` ("Show External Changes") raises the floating
  two-pane panel (#1969's layout): files newest-first left — kind marker,
  humanized age, name, then directory, name first because the column truncates
  at half the panel — and the selected entry's mini-diff right, its captured
  content against what the file holds *now*. The list is live: an event landing
  while the panel is open folds in, with the selection re-found by path rather
  than sliding down as rows prepend.
- **Actions:** `enter` opens, `d` sends the pair to the reusable diff pane
  (#60), `R` reloads the buffer (the conflicted case — a clean buffer has
  usually auto-reloaded already), `x` dismisses a row, `c` clears the feed, and
  `r` reverts **behind a confirmation**. An existing file is restored into its
  buffer through the local-history restore path (one undoable edit, dirty
  buffer, disk untouched until the save), so a revert is never itself
  destructive; a file deleted externally has no buffer to restore into, so its
  content is written back and opened, stamping the save epoch so the restore
  does not echo into the feed. A created file offers no revert — there is no
  previous version to invent. An action that cannot apply notifies and leaves
  the panel up rather than closing the list to toast a refusal. The command
  also sits in **View → External Changes**.
- **Config:** `files.change_feed_limit` (Settings → Files) caps the list; 0
  turns the feed off and clears it, since an off switch must not leave a stale
  list behind. Read on every recorded event, so a change takes effect at once.
- **Docs:** new concept doc `/architecture/change-feed.md`, indexed; the
  foundation doc's watcher section notes the exported ignore rule and the feed.

## 2026-08-20 (Built-in performance HUD, #1999)

- **`internal/perfhud`** (new): the metrics collector plus both renderers. The
  collector is a package-level singleton (the root model is a value type copied
  per message, so measurement state cannot live in it) gated by an atomic bool:
  `Count` buckets a message by coarse category (key/mouse/resize/tick/other,
  structural where bubbletea's interfaces allow, `Tick`-spelling for timer
  deadlines) *and* by concrete type name, `RecordFrame` takes the full-frame
  cost, `RecordPane` books render time against a pane key. `Sample` closes a
  window into rates, ranks the panes by total cost, reads
  `runtime.ReadMemStats`/`NumGoroutine`/RSS and appends to a bounded ring.
  Classification is cached per type name, so the type switch runs once per
  message *type*. `Render` draws the box (sparklines scaled to their own
  window's maximum); `SnapshotText` renders the same numbers as a plain-text
  block with the build stamp and min/avg/max over the history. RSS comes from
  `/proc/self/statm` where it exists and from `getrusage`'s peak otherwise —
  labelled as a peak rather than passed off as current.
- **Hooks** (`internal/app`): `Update` counts, `render` books the frame,
  `renderPane` (split from the new `renderPaneBox`) books the leaf. Each sits
  behind a `perfhud.Enabled()` check, so a hidden HUD costs one atomic load and
  adds no allocation, clock read or defer closure to any hot path (#1095–#1101);
  with the HUD open the box is laid out once per sample and cached in the model
  (`perfBox`/`perfBoxW`) rather than composed per frame.
- **`internal/app/perfhud.go`** (new): `perf.hud` (`ctrl+alt+p`, View menu)
  toggles collection and the demand-armed sampling tick — the HUD's only wake,
  counted in the armed-timer gauge it reports; `perf.snapshot` copies the block
  to the clipboard, using the sample on screen while the HUD is open and a
  fresh one (runtime gauges only, stated as such) while it is closed. The open
  state lives in the collector so a project switch's model rebuild cannot orphan
  the tick. The box composites top-right, above every overlay but the toasts.
- **Settings**: `[perf] hud_interval_ms` (100–10000, default 1000) and
  `hud_history_seconds` (5–600, default 60), validated, on a "Performance HUD"
  settings page; the ring length follows from the pair.
- Wiki: a HUD section in Performance & Diagnostics; reference pages and the
  keybinding ledger regenerated.

## 2026-08-20 (Rotated log sets as one merged timeline, #1996)

- **`internal/logset`** (new): rotation-set detection and merging. `Stem`
  reduces a member name to the live log's (`app.log.2.gz` → `app.log`) using
  the #1745 suffix shapes — sequence number, `YYYY-MM-DD`/`YYYYMMDD` date
  stamp, optional `.gz`, and a remainder that must keep an extension of its
  own, so `backup.1` is nobody's set; `Detect` lists the directory for every
  file reducing to that stem. Ordering is oldest first: dated by date, numbered
  by descending number, the live log always last, with a per-set fallback to
  modification times when the two spellings mix (or a rank-less `app.log.gz`
  is present), which keeps the comparison a total order. `Merge` assembles the
  timeline under both large-file thresholds (#149), reading members **newest
  first** and cutting each region from the front, so a cap costs the oldest end
  of the timeline and never the lines next to the live log; gz members go
  through `gzfile.Read` (bomb guard included), an unreadable member is skipped
  and named, and the newest member's end offset comes back as the follow
  anchor.
- **Origin separators** (`internal/logline/origin.go`): `OriginLine` /
  `OriginName` are the shared format of the `──── app.log.1 ────` line opening
  each region. `Spans` captures a recognized separator whole-line as
  `log.origin` instead of parsing it as a log entry; `logrender.go` styles it
  in the accent colour.
- **Command and buffer** (`internal/app/logsets.go`, new):
  `log.openRotatedSet` ("Open Rotated Log Set (Merged Timeline)") merges the
  set behind the focused buffer — from the live log or any rotated member, and
  on a merged buffer it re-merges — off the Update loop, and installs it as a
  read-only tab under the virtual path `<stem>!merged/<name>` (title
  `app.log (merged) [RO]`). Opening a log file with siblings offers the
  timeline once per path per session; truncation, omitted members and
  unreadable ones surface as a warning toast.
- **Follow mode on a merged buffer** (`internal/editor/mergedlog.go`, new):
  `ShowMergedLog` installs the timeline and anchors follow on the set's newest
  member (`followSrc`), which `followTarget` makes the file every #1928 path
  operates on. Enabling costs no read; appends stream into the buffer's tail as
  usual; a rotation or truncation emits `MergeLogSetMsg` and parks the view
  (`mergeWait`) until the root model's fresh merge lands, keeping follow armed
  across it. Watcher events reach such a view by follow source rather than by
  path (`routeMergedLogFollow`), and a set whose newest member is compressed
  refuses to be followed for want of an anchor.
- **Docs**: `wiki/architecture/log-timeline.md` (new) plus the log-rendering
  section of `wiki/architecture/editor.md`, `follow-mode.md`, `languages.md`;
  `userdocs/guides/file-rendering.md` § Rotated logs as one timeline;
  generated `userdocs/reference/commands.md`.

## 2026-08-20 (jq playground: named saved-filter library, #1995)

- **Store** (`internal/jqplay/library.go`, new): `Library` — a named
  saved-filter store that is path-agnostic, so one type and one set of tests
  serve both scopes. `Filter{Name, Program}`, `LoadLibrary(path)` (a missing
  or malformed file is an empty library; entries with an empty name or
  program are dropped on the way in), `Save(path)` (an error is *reported* —
  the user asked for this write), `Get`/`Has`/`All`, `Set` (add or overwrite,
  kept sorted case-insensitively, capped at `MaxFilters`), `Rename` (refusing
  a taken name — only a confirmed save may replace a filter), `Delete`, plus
  `Scope` (`ScopeProject`/`ScopeGlobal`) and `Preview` for the picker's
  one-line program chip.
- **UI** (`internal/app/jqfilters.go`, new): the two store paths —
  `.ike/jqfilters.json` and `~/.ike/jqfilters-global.json`, distinct names
  under the `IKE_CONFIG_DIR` seam (the `winsize-global.json` precedent,
  #1714) — the shell name prompt shared by save and rename (scope toggled by
  `tab`, a taken name held for a confirming second `enter`), and the locked
  palette picker `jqFiltersMode` (prefix `}`): rows fuzzy-matched over the
  name, the program as the detail chip, the scope as the accent badge,
  `shift+delete` deleting in place, both scopes listed project-first and
  re-read on every open.
- **Commands** (`internal/app/commands.go`, `internal/menu/defaults.go`):
  `json.jqSaveFilter` ("Save jq Filter…"), `json.jqFilters` ("Saved jq
  Filters…") and `json.jqRenameFilter` ("Rename Saved jq Filter…"), all three
  in the Tools menu; `ctrl+s` and `ctrl+l` are their chords on the
  playground's query line.
- **Playground** (`internal/app/jqplayground.go`): the two chords and their
  info-row hints. Inserting a filter puts it on the query line and runs it,
  opening a playground first when none is up.
- Docs: `userdocs/reference/commands.md` regenerated;
  `wiki/architecture/jq-playground.md` gained "The saved-filter library".

## 2026-08-20 (curl import/export for .http files, #1994)

- **Converter** (`internal/httpfile/curl.go`, new): `ParseCurl` turns a curl
  command line into a request block — POSIX-shell tokenizing (single/double
  quotes, backslash and `^` continuations), `-X`, the URL operand and
  `--url`, `-H` (including the `Name;` empty-header spelling), `-d` /
  `--data*` / `--data-urlencode` (a lone `-d @file` becoming a `< file` body,
  #1305), `--json`, `-F`/`--form-string` into an inline multipart body
  (#1707, fixed boundary, `;type=`/`;filename=` and file parts as per-part
  `< path` directives), `-u` into `Authorization: Basic`,
  `--oauth2-bearer`, `-A`/`-e`/`-b`, `--compressed`, `-G` and `-I`.
  Everything else lands in `CurlImport.Ignored` (`IgnoredSummary`) — named,
  never dropped silently. `ExportCurl` is the inverse: `-u` from a decodable
  basic-auth header, `-F` parts from an inline multipart body,
  `--data-binary @path` from an external body, `--data-raw` otherwise, with
  shell quoting. `FormatRequest` renders a request as block text the parser
  reads back unchanged.
- **Commands** (`internal/app/http_curl.go`, new): `http.importCurl`
  ("Import curl Command…") opens a one-line prompt prefilled from the
  clipboard when it holds a curl command (`httpfile.IsCurlCommand`, wrapped
  lines folded), appends the block named `METHOD /path` (deduplicated) to the
  focused `.http` file and lands the caret on it; `http.copyAsCurl`
  ("Copy HTTP Request as curl") resolves the block under the caret through
  the dispatch variable chain (#1867/#1993) and writes a runnable command to
  the clipboard, rebasing an external body onto the file's directory. An
  unresolved placeholder aborts with the dispatch's own message.
- **Docs**: `wiki/architecture/http-client.md` § curl import and export;
  `userdocs/guides/http-client.md` §§ Starting from a curl command / Copying
  a request out as curl; generated `userdocs/reference/commands.md`.

## 2026-08-20 (Diff two stored HTTP responses, #1992)

- **`internal/httpdiff`** (new): renders a stored response
  (`httphistory.Entry`) as the text the diff viewer compares — status line,
  sorted headers, blank line, body, with the per-run duration left out. A JSON
  body is normalized first (`NormalizeBody`): decoded with `UseNumber` and
  re-encoded with sorted keys, a fixed two-space indent and HTML escaping off,
  so a key-order-only or formatting-only difference diffs as *nothing*.
  JSON is detected by `Content-Type` (`/json`, `+json`, `ndjson`/`jsonl`/
  `json-seq` streams normalize value by value) or, without one, by a leading
  `{`/`[`; non-JSON, malformed JSON and binary bodies fall back to as-is
  (binary to a size notice).
- **Entry picker and diff** (`internal/app/http_diff.go`): `D` in the response
  pane (`httppane.DiffHistoryMsg`) or `http.diffResponses` ("Compare Stored
  HTTP Responses") opens the palette locked to `httpEntriesMode` (prefix `{`)
  listing the request's other stored responses; `DiffHTTPEntriesMsg` then puts
  the older response left and the newer right into the shared reusable diff
  slot via `openDiffTexts`, read-only. Fewer than two entries, no pane and a
  pair pruned in between notify instead.
- **Pane**: footer hint `D diff` once a second response is stored, plus the
  key in the pane-local help group.
- **Docs**: `wiki/architecture/http-client.md` § Response history;
  `userdocs/guides/http-client.md` § Comparing two responses; generated
  `userdocs/reference/commands.md`.

## 2026-08-20 (Xdebug connection doctor, #1991)

- **Doctor tool window** (`internal/debugdoctor`, `internal/app/xdebug_doctor.go`):
  new singleton pane (key `xdoctor`, command `debug.doctor`, Run menu /
  palette) showing the DBGp listener state — running/stopped, bound port,
  hostname filter, path-mapping count — and a newest-first trace of every
  incoming connection attempt with its accept/reject outcome; `c` clears.
  The app owns the 200-entry ring (`Model.doctorLog`), so the trace survives
  the panel being closed and records parked-session events too.
- **Structured bridge events** (`internal/dbgp/bridge/bridge.go`):
  `ike.listenState` `{state, port, hostname, mappings}` on listener
  start/stop and `ike.debugConn` `{outcome, reason, detail, remote, ideKey,
  fileURI, host, local, mapped}` per attempt — observability only, the
  accept/reject semantics are unchanged; `mapped=false` marks an accepted
  request whose entry file has no local path mapping.
- **Malformed init distinguished** (`internal/dbgp/conn.go`): a pre-init
  packet that fails to parse (or a well-formed non-init first packet) makes
  `WaitInit` fail fast with `ErrBadInit`; the bridge rejects with the new
  reason `init` ("malformed init packet: …") while `handshake` keeps meaning
  "no init at all".
- **Docs**: `wiki/architecture/debugger.md` § Xdebug Doctor;
  `userdocs/guides/run-and-debug.md` § PHP and Xdebug; generated
  `userdocs/reference/commands.md`.

## 2026-08-20 (Conceal/secret explain popover, #1998)

- **Span → provenance** (`internal/numhint`, `internal/secret`,
  `internal/epochtime`, `internal/consthint`): the hint sources now report
  *which* rule produced a reading. `numhint.Hint.Why` carries the rule level
  (field rule / key word / shape), the pattern or word it matched and the unit
  it applied, filled in on the branches that pick the family; `FieldRule`,
  `KeyWord`, `ValueAt`, `HintAt` and `UnitName` expose the pieces around it.
  `secret.Explain` replays the key tables in order with the pattern that
  decided, `epochtime.Unit` reports the digit-count reading, and
  `consthint.FlavorForLang` lets a caller evaluate a constant's right-hand side
  for the buffer's language.
- **`internal/concealexplain`** (new): composes those into one explanation —
  raw value, stand-in, key, family, the rule in its own words, the reading and
  its mapping word — plus the reclassification choices and the entry formats of
  the two stores.
- **The popover** (`internal/editor/explainconceal.go`, `g?` /
  `editor.explainConceal`): the caret's stand-in (with #1686's widening, and
  answering for families switched off) or the plain value under it, with
  `r` reveal, `1`–`9` reclassify, `a` pin the current reading and `m`/`u` for
  the masking rules. Rules persist through `editor.ConcealRuleMsg` and
  `app/conceal_rule.go` into `editor.number_hint_units` /
  `editor.secret_masking_keys` — the same stores the Settings UI edits — with
  an existing entry for the same pattern replaced rather than shadowed.
- **Docs**: `/architecture/editor.md` gained the popover section and the
  cross-references from the number-hint and secret-masking sections;
  `userdocs/reference/commands.md` regenerated.

## 2026-08-20 (Capture response values into variables, #1993)

- **Directive** (`internal/httpfile/capture.go`): `# @capture name = <jq-expr>`
  comment lines (`#`, `##`, `//` — not `###`, which opens a block) inside a
  request block parse into `Request.Captures`, carrying the name, the verbatim
  jq expression and the directive's position for the marker below.
- **Evaluation** (`internal/httpclient/capture.go`, `internal/jqplay/raw.go`):
  after the response is complete, each directive runs through the new
  `jqplay.EvaluateRaw` — the `jq -r`-shaped single-value form of the
  playground's gojq engine: first non-null output, strings unquoted, anything
  else in JSON spelling. Results land in `Response.Captures`; a re-send (#1832)
  has no parsed request and therefore captures nothing.
- **Precedence** (`httpfile.Vars.Captured`): a captured value is the new top of
  the `{{name}}` chain, above the file's `@name=value` definitions, the
  selected environment and the process environment — it is the freshest value
  and supersedes exactly the `@token = paste-me-here` stand-in.
- **Lifetime** (`httphistory.Entry.Captured`, `Store.Captured`): values are
  stored with the response in `.ike/http/*.json`, so they survive a restart and
  are pruned with their entry; per name, the newest response wins.
- **Failure is loud, never fatal** (`internal/app/http_capture.go`): a path
  that matched nothing, a non-JSON body or a broken expression warns in the
  response pane *and* as a warning diagnostic on the directive's own line;
  each dispatch republishes the file's set, so a fixed directive clears.
- **Highlighting** (`plugins/languages/http/capture.go`): the directive's parts
  are lifted out of the comment colour — keyword, variable, operator, and the
  expression through the jq playground's tokenizer.
- **Docs**: `/architecture/http-client.md` gains "Capturing values from a
  response"; the user guide gains "Chaining requests: `# @capture`".

## 2026-08-20 (Every text input on the shared ui helpers, #2002)

- **Audit + convention** (`wiki/architecture/text-input.md`, new): every
  single-line input site in `internal/` listed with its status, the ordering
  rules for callers (own chords first, `changed` drives side effects,
  preselect is a caller concern, paste is routed not typed), and the four
  sites deliberately left hand-rolled with the reason.
- **Migrated to `ui.EditKey`** — these had hand-rolled a subset of line
  editing, so word motions, word/line kills and the macOS `opt`/`cmd` chords
  were missing: the file rename (`internal/app/fileops.go`), symbol rename
  (`lsprename.go`), JetBrains-keymap (`jbimport_prompt.go`) and OpenAPI
  (`openapi_import.go`) import prompts, the explorer's name prompt
  (`internal/explorer/fileops.go`), and the two debug inline editors
  (`internal/debugpanel/debugpanel.go`, `internal/breakpanel/meta.go`). The
  four shell prompts also render through `ui.CursorView` now instead of each
  slicing its own cursor cell.
- **Gained a cursor** — these were append-only strings, a typo meant retyping
  from the end: the editor's find/replace panel
  (`internal/editor/replace_panel.go`, one cursor per field, kept across a
  `tab` switch), the explorer's speed search (`internal/explorer/search.go`),
  and the four settings filters (panel-wide, colour page, keymap page, enum
  editor) via new `filterKey`/`filterPaste`/`filterView` helpers in
  `internal/settings/textfield.go`. The panel's and keymap page's backspace
  had been byte-sliced and cut multi-byte filter text in half.
- **Paste routing filled in** (#1273's remaining gaps): the topmost settings
  sub-panel form (any `SubPanel` implementing `Paste` — ES, Tools, format,
  associations, debug map, LSP override, keymap import, venv wizard), the
  toolchain page's own inputs, the settings filters, and the tool-window
  panes with a text input (`handlePaste` in `internal/app/inputcoalesce.go`:
  data viewer, DOM inspector, issues, debug variables, breakpoint
  refinements). A surface whose input is closed still drops the block rather
  than letting it leak into a list or the buffer underneath.
- **Guard** (`internal/ui/inputsweep_test.go`): walks `internal/` and fails on
  `x += key.Text` / the `key.Text != ""` insertion guard outside
  `internal/ui`, against a reasoned allowlist; a second test fails when an
  allowlist entry goes stale.

## 2026-08-20 (Tool-tab panes are no longer editor slots, #1989)

- **Persistence** (`internal/app/store.go`, `layouts.go`): a tab host holding
  nothing but tool tabs saves as kind `tools` — per-project `layout.json` and
  user-layout snapshots alike — instead of `editor`+`tools`; legacy files
  keep loading and migrate on the next save.
- **Editor placement** (`internal/app/app.go` `spawnEditor`,
  `editorSlotAnchor`): a tool-tab host never attracts a fresh editor split.
  With no live file-editing pane, the designated default layout's editor slot
  anchors the split (nearest live layout sibling, saved side and share), so
  opening a file with all editors closed recreates the editor in its original
  layout slot.
- **Apply** (`resolveLeaf`, `implicitHostSlot`): `tools` slots re-slot live
  tool hosts and never consume an editor slot's content pane; leftovers never
  graft into the tools area.
## 2026-08-20 (jq playground opens on `.`, recalls the file's last program, #1982)

- **No cursor-path prefill by default** (`internal/app/jqplayground.go`):
  `json.jqPlayground` opens the query line on `.` instead of the caret's jq
  path. Most openings only check something, so the prefilled path was a
  deletion to perform before typing the program actually wanted.
- **Per-input recall**: every run that comes back without an error records its
  program under the input's key (file path, unsaved buffer's editor key,
  response pane key) in `Model.jqLastProgram`, and the next open over that
  input seeds it. Failing programs and `.` are not recorded, so a half-typed
  one never displaces the last one that worked. In memory for the session,
  like the program history — which stays one shared, buffer-agnostic list:
  it answers "what did I run recently, anywhere", the recall answers "where
  was I in this file".
- **The old behavior as its own command**: `json.jqPlaygroundAtPath` ("jq
  Playground at Cursor Path…", palette + Tools menu) opens the same mode
  seeded with the caret's jq path (#1660), falling back to `.` when the caret
  has none or the input is a selection/response body.
- **Docs**: `/architecture/jq-playground.md` gains a "What the query line
  opens on" section; docgen refreshed for the new command.

## 2026-08-20 (Markdown list continuation lines align with the item text, #1975)

- **List continuations** (`internal/editor/mdlist.go`): a list item's
  continuation lines now render flush with the item's text instead of keeping
  their bare source indent — `- My multiline bullet` / `  point` renders as
  `  • My multiline bullet` / `    point`. The pad is a display-only conceal
  range like the marker stand-ins: the continuation's leading whitespace is
  concealed behind a run of spaces as wide as the item's text column, so the
  buffer never changes and the caret in that indent reveals the raw source.
  Ordered items get their pad at run-flush time, so it follows the run's
  aligned number width across the 9 → 10 boundary. Nested items stay items,
  fenced blocks, blank lines and tab indents stay untouched. Concept doc
  `/architecture/editor.md` updated.

## 2026-08-19 (Local History as a two-pane panel with a live inline diff, #1969)

- **Local History panel** (`internal/app/localhistory.go`): `file.localHistory`
  is a two-pane panel now — the snapshot list (file name, absolute date,
  humanized age) on the left, an inline git-style unified diff of the
  *selected* snapshot against the current buffer on the right: `@@` hunk
  headers with three context lines, `+`/`-` markers, added lines green
  (`VCSAdded`), removed red (`VCSDeleted`). Moving the selection recomputes
  the diff immediately; the separate diff pane no longer gates previewing
  (though `enter` still opens it for emphasis/editing, and `r` still
  restores). The body is a width-aware `ui.Content` (the layout-select
  pattern) splitting the shell's budget between the columns. Concept doc
  `/architecture/local-history.md` updated.

## 2026-08-19 (Markdown list rendering, #1966)

- **List markers** (`internal/editor/mdlist.go`): the markdown rich-rendering
  layer now renders list markers as dynamic conceal stand-ins — `-`, `*` and
  `+` as a two-cell indent plus a `•` bullet, ordered markers as their number
  right-aligned to the widest number of their own list, so `1.` … `10.` keep
  the dots in one column. Lists are detected from the buffer text
  (grammar-free, fenced code skipped) and cached per document version; runs
  are the consecutive items at one source indent, nested lists align on their
  own width, and the caret on a marker reveals the raw source. Concept doc
  `/architecture/editor.md` updated.

## 2026-08-19 (Scratches section: wheel scrolling and a last-opened column, #1965)

- **Explorer Scratches section** (`internal/explorer/scratches.go`,
  `internal/app/app.go`, `internal/ui/reltime.go`): the section scrolls with
  the mouse wheel now — the app translates the pointer into a content-local
  row and calls the new `ScrollAt`, which routes the section's body rows to
  `ScratchScrollBy` and the tree rows (and the divider) to the tree's
  `ScrollBy`, so the wheel moves the viewport under the pointer without moving
  the cursor. Each row also gained a right-aligned **last opened** column:
  the new `ui.ShortAge` ("now", "5m", "3h", "7d", "6w") over the MRU store's
  last-opened time, pushed into the explorer by `syncExplorerOpen` via
  `SetScratchOpened`, falling back to the file's mtime for a scratch the MRU
  never saw. The name field shrinks and clips with "…" to make room; a pane
  too narrow to leave 8 columns for the name drops the age instead. Concept
  docs `/architecture/explorer.md` and `/architecture/scratch-files.md`
  updated.

## 2026-08-18 (Scratches as an explorer section, #1963)

- **Scratch files** (`internal/explorer/scratches.go`, `internal/scratch`,
  `internal/app/scratch_section.go`): the #1932 tool pane
  (`internal/scratchpanel`) is gone; the scratch store now lists as a
  **Scratches section** behind a draggable, click-collapsible divider at the
  bottom of the explorer, operated entirely with the explorer's semantics —
  one unified cursor across tree and section, enter/double-click through the
  standard open funnel, `o` into a split, `d`/`R` with the explorer's anchored
  dialogs running `scratch.Delete` and the new boundary-guarded
  `scratch.Rename` (open tabs close/re-point via the standard
  `FileDeletedMsg`/`FileMovedMsg` plumbing), `a` delegating to the
  `scratch.new` picker. Rows sort by name; `scratch.sort = modified` keeps the
  newest-first order. Collapse and dragged height persist with the explorer's
  session state; the scratch dir joins the explorer's auto-refresh poll.
  Settings `scratch.section` / `scratch.section_height` / `scratch.sort`
  replace `scratch.panel` / `scratch.panel_height`, which migrate silently;
  the `scratch.panel` command now focuses the section, persisted layouts
  prune the old pane's leaf, and the `scratch` tool-slot id is retired.
  Concept docs `/architecture/scratch-files.md` and `/architecture/explorer.md`
  updated.

## 2026-08-18 (Column profile for the data viewer and csv buffers, #1940)

- **Column profile** (`internal/datasrc/profile.go`,
  `internal/dataview/profile.go`, `internal/app/columnprofile.go`): `P` in the
  data viewer's grid — or `data.columnProfile` from the palette — profiles the
  focused column: rows, nulls, empty strings, distinct values, min/max, the
  ten most frequent values with counts, plus the mean of a numeric column or
  the length range of a text one. `Profile(ctx, table, column, clause)` joins
  the `Source` interface next to `Count`: SQLite and DuckDB answer with two
  aggregate statements through the filter's own subquery wrapper (a
  three-function dialect is the only engine-specific part — `typeof` vs
  `TRY_CAST`), Parquet with a bounded scan through its reader, capped at
  100 000 rows and marked as capped. The popup is async and cancelable (esc
  closes it and kills the query; a superseded result is dropped), owns the
  pane's input while open, and `y` copies exactly the lines it shows.
  `csv.columnProfile` runs the same scan (`datasrc.ProfileCSV`, splitting
  through `internal/sv`) over the caret's column of a table-rendered
  csv/tsv/psv buffer and shows it in the floating shell.

## 2026-08-18 (Scratch files panel with delete, #1932)

- **Scratch files** (`internal/scratchpanel`, `internal/app/scratch_panel.go`,
  `internal/scratch`): `scratch.panel` ("Scratch Files") opens a slim
  singleton pane below the editor listing the scratch store newest-first
  (name, language, mod time). Enter/double click open through the standard
  funnel; `d` arms a confirmation only `y`/`enter` answers, and the delete
  then removes the file through the new `scratch.Delete` — which refuses any
  path not lying directly in the scratch dir — closes its tabs across panes
  via the explorer's `closeEditorsForPath`, and drops its Problems findings.
  Scratches could be created and opened but never deleted from inside the IDE
  before: the scratch dir lies outside the project root, so the explorer
  cannot reach it. The panel is an ordinary split-tree leaf, so the line above
  it is the usual draggable pane edge and its height persists with the layout;
  `scratch.panel` / `scratch.panel_height` (default off, 8 rows) decide
  whether and how tall it opens on start. Concept doc
  `/architecture/scratch-files.md` extended.

## 2026-08-18 (Python secret masking covers literal spans only, #1930)

- **Secret masking in Python** (`plugins/languages/python/mask.go`) no longer
  masks the whole right-hand side of a suspect assignment, only the string
  literals inside it: `token = item["token"]`, `token = get_token()` and
  `token = other` carry no mask at all, and
  `PROXY_API_KEY = os.environ.get("PROXY_API_KEY", "8479…")` masks the fallback
  alone. Quotes stay visible and the content masks (the JSON convention); a
  literal that is not the whole value, is identifier-shaped and is itself
  secret-suspect reads as a key name and stays readable; f-strings mask their
  literal text and keep `{...}` interpolations; open triple-quoted values (PEM
  keys) still mask whole across their lines. JSON and dotenv are unchanged —
  their values are literals already.

## 2026-08-18 (DOM inspector tool pane, #1929)

- **DOM inspector** (`internal/htmldom`, `internal/domview`,
  `internal/app/dom_panel.go`): `dom.toggle` opens a singleton right-split
  pane with the focused HTML buffer's parsed DOM tree and a live CSS selector
  tester. The parser drives the `x/net/html` tokenizer through a tolerant
  stack machine — real `*html.Node`s (cascadia-matchable) with byte-offset
  source spans, no implied elements, messy fixtures welcome — and runs off
  the UI loop keyed by editor document version. Selector matches highlight in
  the tree and, via the new `editor.DOMMatchesMsg` overlay, in every editor
  showing the file (`n`/`N` step matches); `c` copies a verified
  shortest-unique selector path, `Y` the verbatim outer HTML. Enter/double
  click jumps through the open funnel; the editor cursor follow-selects the
  enclosing node. Adds `github.com/andybalholm/cascadia`. New concept doc
  `/architecture/dom-inspector.md`.

## 2026-08-18 (OpenAPI 3.x import for the HTTP client, #1939)

- **HTTP client** (`internal/openapi/`, `internal/app/openapi_import.go`):
  `http.importOpenAPI` ("Import OpenAPI Spec…") scaffolds a `.http` file from
  a local OpenAPI 3.x document (JSON or YAML) instead of hand-writing every
  request block. The new `internal/openapi` package reads the document as
  plain maps rather than through a validating model — that tolerance is the
  point: an unresolvable/external `$ref`, an exotic parameter location, a
  security scheme with no request-file spelling or a media type with no
  generator is *skipped and named*, while every other operation is still
  generated. Only "not OpenAPI 3.x at all" fails the import; a **Swagger 2.0**
  document is rejected with "convert the document to OpenAPI 3.x first".
- Output lands **next to the spec** (`petstore.yaml` → `petstore.http`) and
  opens in the editor, because the client resolves `http-client.env.json` from
  the request file's own directory — an unplaced buffer would resolve nothing.
  Blocks group by tag → path → method, named after the `operationId`, with the
  summary as a comment. Everything variable is a `{{name}}` placeholder
  (#1867): origin, parameters and credentials alike; required query parameters
  are live folded `? key = value` lines (#1269), optional ones commented below
  them. `http-client.env.json` seeds the host and parameter values,
  `http-client.private.env.json` the credentials with empty values — both only
  when absent, with the summary naming what a kept environment still lacks.
- Generation is deterministic (sorted paths, properties, media types and
  schemes; sorted JSON keys), so re-importing a spec diffs cleanly. An
  existing `.http` file is only overwritten when it carries the generated
  header marker; a hand-written one of the same name stops the import.
- Adds `gopkg.in/yaml.v3` — the only new dependency; a validating OpenAPI
  library would reject wholesale exactly the documents this command has to
  generate *something* from.

## 2026-08-18 (exited run pane: copy, resize reflow and scrollback, #1951)

- **Terminal** (`internal/terminal/model.go`, `session.go`, `scrollbar.go`):
  the exited state (#810) becomes a first-class read-only view of the finished
  run. One gate — `Model.dead()` (no session, not running, or a pipe past
  `FinishPipe`) — replaces the scattered `Running()` short-circuits: mouse
  press/drag/release and the wheel no longer consult the gone child's
  mouse-reporting/alt-screen modes, so selection and scrollback paging work;
  `Model.deadKey` handles `pgup`/`pgdn`, `up`/`down`, `home`/`end` and `r`
  (restart) and leaves every other key inert instead of snapping back to live;
  the scrollbar shows even when the exited program left the alt screen behind.
  `Session.Resize` now applies to a closed session (skipping only the closed
  PTY), so dragging the divider reflows the dead pane and re-centers the
  dialog — `release` clears the pending-resize flag and the debounced apply
  sends its own repaint, since `notify` stays silent once closed. The
  emulator's retirement moved from the child's exit to the pane's teardown
  (`Session.closeEmulator`): a closed emulator drops the writes a width reflow
  replays, so the dead grid stays writable until the pane goes away. The exit
  dialog composites over the paged view too, keeping `DeadActionHit` valid
  while scrolled (the footer fallback hit-tests at the live view only).
- **App** (`internal/app/app.go`): `focusedDeadTerminal` routes the cmd+c copy
  chord for a finished session the way `terminalFocused` routes it for a live
  one, so a selected traceback lands on the clipboard.
- **Docs**: exited-state paragraphs in `/architecture/tool-panes.md` and
  `/architecture/terminal.md` (command sessions, PTY lifecycle, reserved set).

## 2026-08-18 (SSH host profiles: picker from ~/.ssh/config, #1938)

- **New package** `internal/sshconf`: a minimal OpenSSH client-config reader —
  `Host` aliases in file order, `Include` followed (glob, `~`, paths relative
  to the including file, cycle- and depth-guarded), wildcard/negated patterns
  skipped, `HostName`/`User`/`Port` kept for the picker's detail line only.
  ssh's matching logic is deliberately not re-implemented; the alias goes to
  the user's own ssh.
- **App** (`internal/app/ssh_picker.go`): `terminal.ssh` ("SSH Host…", palette
  and the Tools menu) opens a locked fuzzy palette mode over the parsed hosts;
  picking one spawns the command session `ssh <alias>` labelled `ssh: <alias>`
  — as a tab of the active editor pane, else as a fresh split. Missing or
  unreadable config degrades to an empty picker whose placeholder is the hint.
  Exit follows command-session semantics: the pane stays on `[process exited
  with code N]`, so a refused connection stays readable.
- **Setting**: `terminal.ssh_hosts` (list, Settings UI) adds hosts no ssh
  config declares; entries carrying whitespace are rejected with a config
  diagnostic. New `## SSH host profiles` section in
  `/architecture/terminal.md`; docgen regenerated for the new command and
  setting.

## 2026-08-18 (label-jump motion: easymotion/leap-style navigation, #787)

- **Editor** (`internal/editor/labeljump.go`): `gs` in normal mode (or
  `editor.labelJump`) opens a label-jump session — the next 1–2 typed
  characters select the visible matches (sticky-header/fold/scroll aware),
  each match is overlaid with a home-row-first label, and the label key lands
  the caret via `jumpTo`, so the departure records in the navigation history.
  A unique match autojumps, esc cancels with the cursor untouched. Label
  assignment excludes every rune that could still extend a match, so a key is
  never ambiguous between narrowing and picking; past the 26-key alphabet the
  tail keys become prefix-free two-character labels. New `awaitLabel` wait
  state; `Capturing()` reports true while the session lives so the app layer
  does not steal plain keys (`q`, `tab`, `@`). Labels render in
  `renderSpanUncached` above every other decoration, the typed span
  highlights search-match-style.
- **Docs**: new `## Label jump` section in `/architecture/editor.md`, the
  per-jump-entry list in `/architecture/navigation-history.md`, a "By what
  you can see" section in the navigation user guide, docgen for the new
  command.

## 2026-08-18 (follow mode: tail -f for log buffers, #1928)

- **Editor** (`internal/editor/follow.go`): `view.toggleFollow` (palette,
  `alt+shift+f`) streams appended file content into the buffer, read-only,
  with cursor and viewport stuck to the end. Appends are incremental
  (`followOffset` anchors the consumed bytes; unterminated lines continue in
  place, split CRLF/UTF-8 tails are held back), pause re-derives after any
  user movement (keys/actions via the `Update` wrapper, wheel and scrollbar
  directly) from "cursor on the last line and visible", and the status line
  shows `FOLLOW` / `FOLLOW (paused)`. Truncation and rotation (remove +
  create, or a shrunken file) reload wholesale with a toast; the repeat-run
  cache extends incrementally over appended lines (`extendLogRuns`).
- **App** (`internal/app/follow.go`): one demand-armed tick drives the watch
  service's poll fallback while at least one view follows — armed by the
  editor's `FollowMsg`, self-stopping otherwise, so idle sessions pay
  nothing. A followed file's external delete no longer closes the pane (it
  is a rotation in progress); the poll stamp is refreshed instead.
- **Setting**: `editor.follow_poll_ms` (default 500, clamped 100–10000) with
  a Settings-UI entry; docgen regenerated for the new command, binding and
  setting. New concept doc `/architecture/follow-mode.md`.

## 2026-08-18 (jq playground: live query line over JSON buffers and HTTP responses, #1936)

- **Evaluation core** (`internal/jqplay`): `Parse` decodes a buffer as a JSON
  stream once (`UseNumber`, so large integers survive; capped at 10 000
  top-level values, malformed input reported with its line); `Run` compiles
  and runs a jq program over every value under a caller-supplied context,
  collecting pretty-printed output. An empty program is idle, a compile error
  produces no output, a runtime error keeps the values produced before it, and
  `halt` stops cleanly. Three bounds — `MaxOutputs` (500), `MaxResultBytes`
  (256 KiB) and the caller's `EvalTimeout` (5 s) — cover infinite emitters,
  enormous values and programs that loop emitting nothing.
- **Engine**: gojq (`github.com/itchyny/gojq`, MIT) — pure Go, so the build
  stays cgo-free and no `jq` binary is needed. No system-jq fallback: two
  engines would be two dialects to explain in one dialog.
- **Query-line colorizer** (`internal/jqplay/highlight.go`): a single-pass
  rune scanner (`Tokens`/`KindAt`) that never fails, because a live query line
  is usually holding a program that does not parse yet. Paths, strings,
  numbers, keywords, functions, `$vars`, `@formats`, operators and comments,
  mapped onto the chrome palette.
- **The dialog** (`internal/app/jqplayground.go`, `json.jqPlayground`): the
  input snapshot is the focused HTTP response body, else the editor's visual
  selection, else its buffer; the query line is seeded with the caret's jq
  path (#1660). Each change cancels the run in flight, bumps a generation and
  schedules a 120 ms tick — only the current generation runs, and only the
  current generation's result installs. `ctrl+y` copies the whole result,
  `ctrl+o` opens it as a `.json` scratch, `↑`/`↓` walk the per-session program
  history, and errors show inline under the query.
- **Docs**: new concept doc [jq Playground](/architecture/jq-playground.md);
  docgen refreshed for the new command.

## 2026-08-18 (Slot-assignment value hints + terminal/run/debug targets, #1946)

- **Shared id list**: `config.BuiltinAssignTools` is the single authoritative
  list of built-in `[tools.layout]` assign targets (the singleton tool
  windows plus `run`, `tests`, `issues` and the new `terminal`), shared by
  the settings form, the config validator (unknown tool ids now warn — the
  entry stays inert) and the app-side resolver.
- **Settings form value help** (`internal/settings/assign_hints.go`): List
  entries may declare `EntryHints`/`ValidateEntry` in the schema; the Slot
  assignments editor lists the effective template's slot letters (staged
  edits included) for a bare token, the assignable tool ids — custom tools
  included — after a `SLOT=` prefix, and rejects unknown slots/tools in the
  row with a message naming the valid values. Template rows get a static
  E-is-reserved reminder.
- **Assignable terminal** (`Model.openShellAtSlot`): `X=terminal` opens
  fresh integrated-terminal panes at slot X, later ones joining as tabs;
  plain shell panes and pure shell tab hosts count as slot residents (layout
  applies keep them in-slot). The popup overlay terminal and
  `terminal.newTab` are unaffected. `run` and `debug` place independently
  (both were already slot-capable; now documented and hinted).

## 2026-08-18 (Regex tester: pattern + test text with live match and group highlighting, #1937)

- **Evaluation core** (`internal/regextest`): `Evaluate` compiles a pattern
  and collects its matches with capture groups (index, name, value, plus a
  `Set` flag separating "did not participate" from "matched empty"),
  capped at 5000 matches; an empty pattern is idle, a compile error is
  reported with regexp's preamble trimmed. `LineSpans` maps matches onto
  per-line rune columns (multi-line matches split per line, zero-width
  matches color nothing), `Quote` renders the pattern as a Go raw / Go /
  TOML / JSON literal, `History` is the deduped, capped session list.
- **Tester dialog** (`internal/app/regextester.go`, command
  `tools.regexTester`, Tools menu): floating shell content with a pattern
  line and a scrolling multi-line test area prefilled from the editor's
  visual selection, live re-evaluation on every keystroke, matches
  highlighted (selected match in the selection colors, the rest muted),
  match count and the selected match's groups listed, compile errors
  inline, RE2 semantics stated on screen. `tab` switches fields,
  `ctrl+n/p` selects a match, `↑/↓` walks the pattern history, `ctrl+o`
  cycles the copy format and `ctrl+y` copies. Texts past 16 KiB evaluate
  off the event loop behind a generation stamp; pastes keep their line
  breaks here (the only prompt that does).
## 2026-08-18 (Project bookmarks: mnemonics, notes, stepping, #55)

- **Store** (`internal/bookmarks`): JetBrains-style line bookmarks keyed by
  project-relative path + 0-based line, each with an optional mnemonic digit
  (`0`-`9`, unique project-wide) and a note. Persisted in
  `.ike/bookmarks.json` (`IKE_CONFIG_DIR` override) on every change and on
  each buffer save; malformed files load empty. Edit shifts reuse the
  breakpoint delta scheme (colliding bookmarks merge, lower line wins) and
  `Rename` re-keys files and whole directories.
- **Editor**: `SetBookmarkHooks` injects the gutter-sign and adjust closures
  beside the mark hooks; the gutter draws the mnemonic digit where a bookmark
  has one, otherwise the existing `⚑`.
- **Commands** (`internal/app/bookmarks_store.go`): `bookmark.toggle` (`f11`),
  `bookmark.toggleMnemonic` (`alt+f3`, digit prompt — the same digit on the
  same line removes), `bookmark.jumpMnemonic`, `bookmark.annotate` (note
  prompt), `bookmark.next`/`bookmark.previous` (`shift+f11` /
  `ctrl+shift+f11`, wrapping in (path, line) order). All are in the Navigate
  menu and the JetBrains import map.
- **Picker**: `nav.bookmarks` now lists the project's bookmarks beside the vim
  marks — `⚑[digit]  path:line` with the note (or the line) as preview,
  shift+delete removes.

## 2026-08-18 (GitHub Issues tool window: list/filter, detail, start work, PR state, #1934)

- **Forge layer** (`internal/forge`): gh-CLI binding in the internal/vcs
  mold — `RefreshCmd` fetches open issues + PRs via `gh … --json` with
  setup/error separation (no gh, no GitHub remote → explanatory state;
  offline → transient error), `PRForIssue` joins PRs by the
  `issue/<n>-…` head-branch convention with a folded CI CheckState,
  `BranchName` derives the 50-char capped slug, `StartWorkCmd` branches
  `issue/<number>-<slug>` off a fetched default branch (dirty worktree
  refused with a clear message, fetch failure degrades to the local
  default with a warning). Every subprocess is deadline-bounded.
- **Issues pane** (`internal/ghissues`, wiring
  `internal/app/issues_panel.go`): singleton tool window (`issues.toggle`,
  Tools menu, context/slot key `issues`) listing number, title, colored
  label chips, assignees and linked-PR state; `/` fuzzy filter (live,
  score-ranked), `l` label-filter cycle, enter/double-click opens the
  glamour-rendered detail view, `s` starts work (toast + VCS refresh),
  `o` opens the issue in the browser, `r` re-fetches. Persisted as
  `{kind: "issues"}`, restored empty with refresh armed.

## 2026-08-18 (Elasticsearch console: index sidebar, Query-DSL buffers, hit grid, #1927)

- **ES console pane** (`internal/espane`, backend `internal/esq`, wiring
  `internal/app/es.go`): one read-only console per configured cluster —
  index/alias sidebar with `_cat` doc counts, paged hit grid (`_id`, `_score`,
  sorted `_source` columns; nested values as compact JSON cells, `v` for the
  full document read-only), `from`/`size` paging with `track_total_hits`
  forced so totals are exact, aggregations under `a`. The data viewer's
  layout and keys, but with **every** cluster request asynchronous — the
  #1795 background-open discipline applied to each page fetch, sequence-
  stamped so stale flights drop. Palette command `es.console.<name>` per
  endpoint; pane persisted by endpoint name and reconnected on restore; `r`
  retries a dead endpoint.
- **Query buffers as real files**: `q` opens
  `<state>/es/<endpoint>/<index>.es.json` (seeded match-all), `es.run` makes
  the buffer the index's active query. `esq.CompletionSource` claims the
  files exclusively and offers Query-DSL keys plus the mapping's flattened
  fields (dotted paths, multi-fields, type badges), accepting dotted names
  via ReplacePrefix (#1913); the pane primes the field cache when an index
  loads. The datasrc.Source reuse hint was evaluated and rejected — dataview
  pages synchronously in Update, which would stall the UI on network I/O
  (documented in the concept doc).
- **Settings:** `[[elasticsearch.endpoints]]` (name, url, basic auth or API
  key) on Settings → Elasticsearch — add/edit/delete with strict form
  validation (shared `config.ESURLError`), `s` toggling the user/project
  write scope; the config validator drops unusable entries with diagnostics.
  Docgen documents the page and `es.run`.

## 2026-08-18 (tests: PHPUnit parser for the Test Results tool window, #1926)

- **PHP joins the structured test path** (`plugins/languages/php/test.go`,
  `testoutput.go`): `*Test.php` files get the `▶` gutter markers, and a run
  fills the Test Results tree (suite/class → test → data set) with glyphs,
  durations and jump-to-failure instead of the raw Run-tool fallback.
- **`--teamcity` is the parsed surface** — one `##teamcity[…]` service message
  per line on stdout, stable across PHPUnit 9/10/11, and the only
  machine-readable PHPUnit format that needs no temp file, so it fits the
  captured-stdout model the Go and Python parsers already use. Data-provider
  cases (`testSum with data set #0`) nest as subtests (`testSum/#0`); failure
  locations prefer the frame in the test's own file over the PHPUnit-internal
  frames above it.
- **The seam grew two optional fields** (`internal/lang/test.go`):
  `TestSpec.RunAtRoot` (run with cwd = the project root, `{file}` becomes the
  root-relative path — PHPUnit needs phpunit.xml and the composer autoloader)
  and `TestSpec.Runner` (resolve `{interpreter}` to the project's test binary,
  `vendor/bin/phpunit` before a global one).
- **Re-runs** go through one anchored filter:
  `--filter '/::(testA|testB)( |$)/'` — the trailing `( |$)` keeps a test's
  data sets in and same-prefix siblings out.

## 2026-08-17 (completion: postfix templates — `expr.if`, `err.nil`, `.for`, #1913)

- **Postfix completion** (`internal/complete/postfix`): the JetBrains habit of
  writing the expression first and the construct after it. Typing `.` now
  dispatches the new source (the `complete.TriggerSource` extension — a
  punctuation trigger reaches only the sources claiming it), which offers the
  buffer language's templates alongside the server's members, marked
  `postfix <preview>` and ranked at `lsp.PriorityPostfix` = 20 so an exact LSP
  member always wins. Accepting rewrites the whole `<expr>.<template>` span:
  the item carries `CompletionItem.ReplacePrefix` and the editor widens the
  accept range leftwards over it, then runs the ordinary snippet tabstop
  session and the live-template re-indent.
- **Expression detection** is Tree-sitter first
  (`highlight.ExpressionEndingAt`): the widest node ending exactly at the dot
  whose kind the language declares in `lang.Language.PostfixExprNodes`. The
  buffer is broken mid-typing, but error recovery parks the dot in a trailing
  `ERROR` node and leaves the expression intact — so `foo(bar).if` wraps the
  whole call while `x := foo(bar).if` wraps only the call. A bracket-aware
  token scan is the fallback without cgo or a grammar.
- **Templates are the language's** (`lang.Language.Postfix`, `EXPR`
  placeholder): Go ships `if`/`nil`/`err`/`for`/`range`/`ret`/`var`/`print`
  (the `err` guard restricted to error-ish expressions), Python
  `if`/`for`/`ret`/`print`/`not`/`len`. Plugins contribute their own.
- **Settings:** `editor.postfix_completion` (default `true`) on Settings →
  Typing Assistance.

## 2026-08-17 (per-file Timeline: local history + git log on one axis, #1916)

- **`file.timeline` ("Show Timeline")** merges the focused file's local-history
  snapshots and the commits that touched it into one chronological modal list,
  each row typed by source. `enter` diffs an entry against the live buffer,
  `m`/`d` diff two entries against each other (across sources), `r` restores a
  snapshot through the existing undoable restore path, `y` copies a commit
  hash, `f` cycles the source filter, `L` loads older commits.
- **New pieces:** `internal/timeline` holds the pure merge/order/filter layer
  (snapshot ranks after a commit at an equal timestamp; stable within a
  source), `vcs.FileLogCmd` the async `git log --follow --name-only` window
  with rename-aware per-commit paths — its paging is cut in-process because
  git's `--skip` and `--follow` do not compose. The local-history diff/restore
  helpers were generalized (`openDiffTexts`, `normalizeBufferText`) instead of
  copied.
- **Settings:** `history.timeline_source` (`both` / `local` / `git`, default
  `both`) on the new Settings → Timeline page is the filter the view opens
  with; `localhistory.Entry` gained an optional persisted `label` the Timeline
  renders where present.

## 2026-08-17 (lsp: code lens, folding ranges, semantic-token refinements, selection ranges, willRenameFiles, #1912)

- **Five missing protocol features wired end to end.** Code lenses
  (`textDocument/codeLens` + resolve) render as dimmed end-of-line
  annotations and execute through the code-action picker (`lsp.codeLens`,
  "LSP: Run Code Lens") via `workspace/executeCommand`;
  `workspace/codeLens/refresh` re-requests every open document. Server
  folding ranges merge into the editor's fold engine (kinds preserved,
  Tree-sitter stays the fallback). Semantic tokens gained the
  `variable.parameter` / `type.namespace` dotted captures so themes can
  colour parameters apart from locals, plus a
  `workspace/semanticTokens/refresh` handler. `editor.selection.extend` /
  `.shrink` walk the server's `selectionRange` ladder with a Tree-sitter
  ancestor-walk fallback. Explorer renames/moves run
  `workspace/willRenameFiles` first and apply the returned WorkspaceEdit
  before the FS operation (still undoable; 2s time-box).
- **Settings:** five new `[lsp]` toggles — `code_lens`, `folding`,
  `semantic_tokens`, `selection_range`, `will_rename` (all default `true`) —
  on Settings → Language Support.

## 2026-08-17 (editor: sticky-scroll separator and large-file gate, #1910)

- **Sticky scroll grew its finishing touches** (`internal/editor/sticky.go`):
  the last pinned header row fills its right padding with a faint dashed rule,
  the subtle separator between the pinned headers and the scrolling body, and
  `stickyLines` now gates explicitly on `InsightOff` so large-file mode can
  never pin headers from stale scope data. The core feature (pinning, click
  remap, cursor unhide, `editor.sticky_scroll`/`sticky_scroll_depth` settings)
  shipped with #168; #1910 closed the remaining gaps and added a Python
  scope-fixture test (`plugins/languages/python/scopes_test.go`) beside the Go
  one.

## 2026-08-17 (tasks: task discovery + problem matchers, #1915)

- **Build-task discovery** (`internal/lang/tasks.go`, `plugins/tasks`): a
  `lang.TaskProvider` seam in the language registry, with built-in providers for
  Makefile targets, package.json scripts and justfile recipes. **`run.task`** lists
  them in a locked palette mode (prefix `)`) and runs a picked task as an ephemeral
  run configuration; **`run.taskPromote`** stores it in `.ike/runconfigs.json`
  instead — a promoted task is a completely normal configuration afterwards.
- **`run.Config` grew `Argv`/`Matchers`** — a literal command line short-circuits
  `run.Argv` past the language synthesis, and matcher names select output parsing.
  `launchRun` only moves `last_used` for stored configurations now.
- **Problem matchers** (`internal/matcher`): named single-line regex matchers
  (built-ins `go`, `generic`, `tsc`) plus a `python` traceback state machine, run by
  a streaming `Engine` (chunk → line assembly, ANSI strip, whole-run dedup). Custom
  matchers via `[[tasks.matcher]]` (name/pattern/group indexes), validated with the
  compiler's message; broken or duplicate entries dropped with a diagnostic.
- **The run terminal is teed, not re-run** (`terminal.Session.SetTap`,
  `internal/app/taskproblems.go`): the feed loop hands chunks to the collector off
  the render loop; findings land per run source in `problems.Store.SetTaskSource`
  (a third channel next to server diagnostics and lint notes), cleared at every
  launch so a re-run replaces its problems. Rows navigate like LSP diagnostics;
  unmatched output stays untouched in the terminal.

## 2026-08-17 (tests: Test Results tool window with re-run-failed, #1911)

- **Test runs get a structured result tree** (`internal/testresults`): a singleton
  bottom-split pane (`pane.KindTests`, key `tests`, `tests.toggle`) showing group →
  test → subtest with pass/fail/skip glyphs, durations and a summary line, next to a
  detail column with the selected test's buffered output (`o` flips to the raw
  stream). `enter` jumps to the parsed failure location or, for passing tests, the
  scanned source declaration; `r`/`f`/`t` re-run all / failed only / one test.
- **`lang.TestSpec` grew a structured-output seam** (`internal/lang/test.go`):
  `StructuredArgs` (machine-readable flags), `ParseOutput` (captured output →
  `lang.TestResult`s) and `FailedArgv`/`NamesJoin` (re-run a named set). Go parses
  the `go test -json` event stream (`plugins/languages/go/testoutput.go`); Python
  gained a full pytest `TestSpec` plus a `-v --tb=short` parser
  (`plugins/languages/python/testoutput.go`). Languages without a parser keep the
  raw Run tool path (#1905) untouched.
- **Captured runs bypass the PTY** (`internal/app/testrun.go`): a parser-backed
  test-scope configuration runs off-loop via `exec.Command` with combined output,
  fills the pane on completion (stale completions dropped by sequence), and still
  registers with `run.rerun`'s last-used memory. Re-runs resolve RerunIDs through
  `run.FailedArgv`.
- **Settings → Tests**: `tests.results_window` (off = raw Run tool for every test
  run) and `tests.auto_open`, both end-to-end in the Settings UI. The pane is a
  `[tools.layout]` assign target (`tests`) and a `window.hideAllTools` member.
  Docs: `architecture/test-results.md` (new), `architecture/run-configurations.md`,
  `architecture/tool-panes.md`; `userdocs/reference` regenerated.

## 2026-08-17 (run: the run terminal is its own tool, #1905)

- **Run output has a dedicated tool pane** (`internal/app/run.go`): the reserved tool
  identity `run`, placed through the shared tool machinery (`Model.placeTool`) like a
  `[[tools.custom]]` pane — slot assignment (#1897) > home position (#1889) > adaptive
  split, tool chrome, `ctrl+w` close and the #810 exited overlay with `Restart`/`Close`.
  An open Run tool takes every later run **in place** (`startInRunTool`, dedicated pane
  or hosted tab); only the first run creates it.
- **No more terminal hijacking**: `Registry.ReusableRunTerminal` — the "first terminal
  nobody typed into" scan that dropped run output into an unrelated terminal pane or an
  open tool pane's tab list — is gone. Plain terminals are never grabbed for runs, and
  the Run tool is never used for anything else.
- **`run.placement` now names the Run tool's home position**: `bottom` (the new default),
  `left`, `right`, `top`, or `in_pane` for a terminal tab in the editor pane; the old
  `new_terminal` migrates silently to `bottom` (`internal/config/validate.go`). Settings →
  Run carries the new option set.
- **The Run tool is session state**: it persists as `{kind: "runTool"}` and its leaf is
  pruned on restore (the #1370 debuggee-terminal precedent) — no program re-runs itself
  at startup. Docs: `architecture/run-configurations.md`, `architecture/tool-panes.md`,
  `userdocs/guides/run-and-debug.md`.

## 2026-08-15 (layout: slot templates cooperate with saved window layouts, #1899)

- **Applying a saved layout keeps the slot config authoritative** for slotted tools
  (`internal/app/layouts_slots.go`): snapshot leaves the active `[tools.layout]` template
  claims are pruned before the ordinary apply and re-open through the slot engine —
  template positions/proportions, tools sharing a slot restored as tabs. Live slotted
  panes are held out of the #1577 grafts and survive the apply in their slots; singleton
  panels absent from a full layout still hide (#791). Kind-only tool names join against
  the *current* `assign` map, so save-time/apply-time config mismatches resolve to
  "current slot config wins". `window.restoreLayout` behaves identically; without a
  template, apply is exactly #1175/#1568/#1577. Docs: `architecture/pane-layout.md`,
  `architecture/tool-panes.md`.

## 2026-08-14 (keymap: context-based bindings — per-pane contexts, same chord per context, #1794)

- **Every focusable pane kind has a keymap context now** (`internal/keymap/context.go`):
  `terminal`, `preview` (markdown preview + image viewer share the advertised id), `vcs`,
  `debug`, `problems`, `structure`, `usages`, `http`, `breakpoints`, `archive` and `data`
  join `editor`/`explorer`/`palette`/`diff`. All spellings qualify user overrides
  (`keymap.bindings.<context>.<chord>`, #1312); old override files load unchanged.
- **One chord, one command per context**: the shipped example is `ctrl+t` — a new terminal
  tab (`terminal.newTab`, sibling-tab semantics like the reserved `cmd+t`) with a terminal
  focused, a new empty editor tab (the new `editor.tab.new` command) with an editor focused,
  unbound elsewhere. Disjoint contexts are conflict-free by construction; pane-over-Global
  layering stays a surfaced shadow diagnostic (#1875), never an error.
- **Terminal-context bindings resolve before PTY forwarding** (`terminalContextChord`):
  only `terminal`-scoped bindings intercept, unmodified keys and the shell essentials
  (`ctrl+c/d/z`) always forward, and `ctrl+t` is deliberately carved out of the shell
  forwarding (unbind `terminal.ctrl+t` to restore readline's transpose-chars). In the popup
  terminal the same binding opens a sibling popup tab.
- **Global default audit**: `lsp.documentSymbols` (`cmd+f12`), `run.testAtCursor`
  (`ctrl+shift+f10`) and `editor.splitViewRight`/`Down` re-scoped to `editor`; true app
  commands and the `editor.tab.*` family stay Global (tab hosts advertise their active
  tab's context, #997). Settings keymap rows tag pane-scoped bindings with their context;
  the `?` cheatsheet titles all context groups.
  Updated [Keybindings & Shortcuts](/architecture/keybindings.md) (contexts, terminal
  section, regenerated status matrix).

## 2026-08-14 (layout: configurable home positions for tool windows, #1889)

- **`[[tools.custom]].placement` returned as the tool's home dock edge**
  (`left`/`right`/`top`/`bottom`; empty keeps the #1588 adaptive heuristic):
  opening docks the tool full-span against that workspace edge
  (`layout.DockNew`); an occupied slot (`Model.dockOccupant` via
  `layout.EdgeLeaf`) adds the tool as a focused tab in the pane there instead
  of splitting; a non-tabbable occupant (explorer, singleton panels) shares
  the dock via a perpendicular split. Placement is intent, not state — the
  `Move`/`Dock` drag mechanics never rewrite it, so close + reopen returns
  the tool home. Editable in Settings → Tools; invalid values degrade with a
  config diagnostic. Updated [Tool Panes](/architecture/tool-panes.md) and
  [Pane Layout & Drag](/architecture/pane-layout.md).

## 2026-08-14 (explorer: delete/rename popup opens next to the affected row, #1884)

- **The delete confirmation and the rename prompt no longer open in the pane centre**: the
  `prompt` struct carries an `anchor` path (`internal/explorer/fileops.go`) — the entry the
  operation acts on, the cursor row for a multi-select delete. `promptAnchorRow()` resolves
  that path against the current rows and the live scroll offset, so the box attaches to the
  entry's *visible* row, surviving a watcher rescan that renumbers rows while the prompt is
  open.
- **`promptBoxOrigin()` places an anchored box below the row**, flips it above when the pane's
  bottom edge is too close, and clamps the result into the pane, so it never renders partially
  off-screen. Horizontal centring is unchanged, as is the centred placement for unanchored
  prompts (new file/folder, the error notice) and for an anchor whose row is no longer visible.
  Updated [Explorer](/architecture/explorer.md).

## 2026-08-14 (dataview: the filter line prefills through WHERE, #1885)

- **`/` in the grid now asks for a condition, not a clause** (`internal/dataview/filter.go`):
  the dimmed head shown in front of the input is `SELECT * FROM <table> WHERE `, and
  `clauseOf` puts that `WHERE` in front of the typed text before it reaches
  `PageWhere`. Typing `id = 5` runs `SELECT * FROM "users" WHERE id = 5`.
- **Text that opens a clause of its own is passed through untouched**: `impliedWhere`
  matches the leading words against `WHERE`/`ORDER BY`/`GROUP BY`/`HAVING`/`LIMIT`/
  `OFFSET`/`WINDOW`/`UNION`/`EXCEPT`/`INTERSECT` word-wise (so a column named `orders`
  is still a condition), and the head then drops its `WHERE ` — the query shown stays
  the query run. `conditionOf` is the inverse, so reopening the line seeds the condition
  as it was typed. An empty line still clears the filter, and the backends are unchanged.
  Updated [Data Viewer](/architecture/data-viewer.md).

## 2026-08-14 (clone dialog: paste and the ghost-text name field, #1873)

- **Cmd+V and bracketed paste now reach the clone dialog** (and the new-project, save-as,
  save-layout and JetBrains-import prompts): `overlayCapturesKeyboard` and
  `routeOverlayPaste` (`internal/app/overlaypaste.go`, #1273) gained a case for each,
  delegating to a `paste*Prompt` helper alongside each prompt's `updateFoo`/`EditKey` path.
  A paste into the clone dialog's URL field re-derives the directory name exactly like typing
  does.
- **The derived directory name no longer looks like simultaneous typing**: while it merely
  follows the URL (`!cloneNameEdited`), `renderClonePrompt` renders it dimmed
  (`cloneGhostStyle`, faint) instead of as live text — hand-editing the field (by key or
  paste) switches it back to a normal rendering. The name-follows-URL behavior itself,
  including the edited-stop, is unchanged. Updated [Project Switching](/architecture/project-switching.md)
  and [Command Palette](/architecture/command-palette.md) (paste routing section).

## 2026-08-14 (explorer: distinct VCS status codes and a workflow palette, #1868)

- **A partially staged file is its own status now** (`internal/vcs`): `statusFromXY` no longer
  folds `AM`/`MM`/`RM` onto the staged half — both porcelain letters changed means
  `StatusPartiallyStaged`, which colours like a modification (there *is* unstaged work) and
  carries a two-letter display code. `FileEntry.Code`/`Snapshot.Code` derive that code from the
  X/Y pair the snapshot already held (`U` untracked, `C` conflicted, otherwise the non-`.`
  letters, a copy reading as `R` so `C` keeps meaning conflict), and `statusRank` places the new
  state between modified and added so directory tinting (#1053) keeps working.
- **The explorer's status cue grew a second cell** (`internal/explorer/explorer.go`): rows render
  `rowVCS.letter` — the file's porcelain code when the snapshot has one, else the one-letter
  badge — and the right-edge reservation follows the code's width instead of the hardcoded
  single column, so `U`/`A`/`M`/`AM` are all readable and a pane too narrow for the code still
  falls back to the clipping ellipsis. The VCS panel's changes list shows the same code in a
  two-cell column.
- **The VCS palette follows the git workflow** (`internal/theme/builtins.go`): every built-in's
  four status slots were re-tuned to muted role hues — blue modified, green added, **violet
  untracked**, muted red deleted — each theme keeping its own saturation/lightness feel and
  every value re-solved against that theme's backgrounds and overlays, so the contrast audit
  (baseline and the AAA high-contrast tier) still passes. Untracked no longer borrows the
  warning hue, which is what made it blur into open (underlined) files. Sparse themes derive the
  two roles without a semantic slot of their own: untracked halfway between error and info,
  deleted halfway from error toward the border tone. A new test pins that the five VCS slots and
  the plain foreground stay perceptually apart in every built-in. Updated
  [Explorer](/architecture/explorer.md) and [VCS / Git Integration](/architecture/vcs.md).

## 2026-08-14 (http client: user-defined variables, #1867)

- **`.http` files define their own variables now** (`internal/httpfile`): `@name = value` lines
  before a request line are parsed into `File.Vars` (they are no longer "invalid request lines"),
  and the new `{{name}}` placeholder resolves from them. A sibling `http-client.env.json` —
  with `http-client.private.env.json` overriding it per variable — contributes a whole named
  environment (`httpfile.LoadEnvironments`); a missing file is silent, a malformed one aborts the
  dispatch naming it. `httpfile.Vars` is the chain the three sources form: in-file definition >
  selected environment > process environment, with definitions expanding nested placeholders and
  refusing to loop. `${NAME}` and `{{$env NAME}}` are deliberately untouched — they mean the
  process environment and no user variable shadows them — and an unresolved `{{name}}` still fails
  the request naming the variable.
- **Choosing the environment**: `http.selectEnvironment` ("Select HTTP Environment") lists the
  environments of the focused file's directory in the palette, badges the active one and offers a
  clear row; the choice persists per directory in `.ike/httpenv.json`. A single environment is used
  without asking, and while a choice is pending the unresolved-placeholder error names the
  environments and the command. Completion and the reformatter learned that a definition line is
  not a request line. Updated [HTTP Client](/architecture/http-client.md).

## 2026-08-14 (highlight: interpolation scopes in Python f-strings and PHP strings, #1869)

- **`f"{kw['type']}"` now separates into four colors**: Python's query gained the standard
  bracket list (`plugins/languages/python/queries/python.scm`), so a subscript inside an
  interpolation shows its container as `variable`, its `[` `]` as `punctuation.bracket` and the
  key as `string`, with the f-string braces still `punctuation.special` — the interpolation
  pattern precedes the bracket list, and rainbow brackets keep overriding it when enabled.
  PHP already covered all three forms (`{$uid}`, `$type`, the deprecated `${dt}`) after #1466;
  both reference snippets are now pinned token by token in
  `plugins/languages/{python,php}/highlight_test.go`. Note that a Python assignment named `key`
  renders masked rather than highlighted — secret masking (#1623) claims the value first.
  Updated [Highlighting](/architecture/highlighting.md).

## 2026-08-13 (userdocs: eighteen interface screenshots and a visual pass, #1857)

- **The user docs showed the file the editor renders, never the IDE around it.** The rendering and
  conceal guides had shots since #1634/#1698, but getting-started, the concept pages and half the
  guides described panes, the palette, the modes and the tool windows in prose alone. Eighteen new
  generated shots close that (`cmd/shotgen`): the window with its four regions, a split, a tab bar,
  the palette in command / file / everything mode, the F1 cheatsheet, the menu bar, the settings
  panel, insert and visual mode, both searches, recent files, the terminal pane, a scratch file, a
  breakpoint in the gutter, and the VCS tool window — all `monokai-pro`, all embedded with an alt
  text that says what to look at. The home page hero is now one of them rather than a theme shot.
- **A scenario scripts one ordered step list** instead of `commands` then `keys`: both orders are
  needed (find-in-file types *into* a prompt, the breakpoint shot moves the caret *before* running
  the command), so steps are `cmd:<id>` / `type:<runes>` / `key:<name>` and run in the order
  written. `opens` adds background tabs, which is what the tab-bar and recent-files shots need.
  The terminal shot spawns `/bin/sh` with `VIRTUAL_ENV` cleared, so it carries no trace of whoever
  rendered it.
- **Mermaid renders now** (`mkdocs.yml` superfences custom fence): the split tree, the settings
  layers and the language-server flow are diagrams rather than paragraphs. Pages gained the
  focus/mode precondition where a chord depends on it, the mode table on the modal-editor page, and
  the getting-started index links the desktop launcher it had been leaving out. Updated
  [Documentation Screenshots](/architecture/screenshots.md).

## 2026-08-13 (archive viewer: mouse wheel, click select, double-click open, #1852)

- **The archive pane takes the mouse now** (`internal/archview/mouse.go`): the wheel scrolls the
  entry list and clamps at both ends, a left click selects the row under the pointer, a click on
  a directory's two-cell fold glyph toggles it, and a double-click activates — a file opens
  read-only through the same `activate()`/`OpenEntryMsg` path as `enter`, a directory folds. Row
  hit-testing reads the renderer's own `top` offset (content-local `y` 0 is the header, rows start
  at `y` 1) and the glyph zone shifts with the row's depth. Routing follows the existing pane
  precedent: the root model translates the cell and calls `Wheel`/`Click`, so `archview.Update`
  stays key-only. The keyboard path was verified end-to-end while in there — j/k, page keys and
  `enter`-open all work with the pane focused, as dedicated pane and as content tab; no
  focus/routing gap was found, and a regression test now pins it. Updated
  [Archive Viewer](/architecture/archive-viewer.md).

## 2026-08-13 (explorer opens every file as a tab in the last-focused editor, #1851)

- **Opening a `.duckdb`, `.png` or `.tar.gz` from the explorer no longer splits
  a pane beside the editor**: the explorer's default open (enter / `l` /
  double-click, `explorer.OpenFileMsg` without `NewPane`) now routes through
  `openPathInEditor` (`internal/app/app.go`), which records the pane
  `fileEditorKey` resolves — the focused editor, else `m.recentEditor`, else
  the first editor leaf — in `m.viewerTabHost` before the file handler runs.
  The viewer opens (`openDataPane`, `openImagePreview`, `openArchivePane`)
  consume it through the #1825 seam (`takeViewerTabHost`/`openContentTab`) and
  nest as a content tab (#1778), so *every* file the explorer opens lands in
  the same pane, whatever its kind. With no editor pane left, one is spawned
  first, matching the plain-file fallback. The explicit **open in split** (`o`,
  `OpenFileMsg{NewPane: true}`) still splits off `viewerSplitTarget` (#1779),
  as do `:e`, CLI and plugin opens; an already open viewer for the path is
  still only refocused.
  Updated [Explorer](/architecture/explorer.md),
  [Pane Layout & Drag](/architecture/pane-layout.md),
  [Data Viewer](/architecture/data-viewer.md),
  [Image Preview](/architecture/image-preview.md),
  [Archive Viewer](/architecture/archive-viewer.md).

## 2026-08-13 (csv: alignment padding clips at the right pane edge, #1847)

- **A conceal stand-in straddling the right edge of a rendered span now emits
  the prefix that fits** (`clipCells`, `internal/editor/view.go`) instead of
  being dropped whole. In a table-rendered csv (#1589) the dropped padding
  freed its cells for the following column, so a field shorter than its column
  collected the next one's text right at the pane edge — `Schweiz+49…` where
  `Schweiz` plus padding was due, `landtelefon` in the header — while the
  column's widest field simply clipped. The clip is the same yield a tab
  straddling the edge takes, holds at every horizontal offset (#1724 keeps the
  left edge), and covers decoded stand-ins (#1585) too. Updated
  [Editor](/architecture/editor.md).

## 2026-08-13 (http response pane search prompt gains a cursor and macOS editing chords, #1845)

- **The HTTP response pane's `/`/`cmd+f` search prompt is a full single-line
  editor now**, via the shared `ui.EditKey`/`ui.CursorView` widget (#763)
  instead of its previous append-only handling: left/right move a rendered
  block cursor, `alt+left/right` (`ctrl+left/right`) jump words,
  `home`/`end`/`super+left/right` jump to the line start/end,
  `alt+backspace`/`alt+delete` delete the previous/next word, and
  `super+backspace` kills to the line start — the same chords the finder,
  palette and settings text fields already support. Esc/enter/`n`/`N` keep
  their existing meaning; every edit still re-runs the search and rescrolls to
  the current match. Updated [HTTP Client](/architecture/http-client.md).

## 2026-08-13 (terminal: shift+arrow keys reach the child PTY, #1841)

- **Modifier+cursor chords are encoded by the pane itself** (`modifiedCursorSeq` in
  `internal/terminal/model.go`, written verbatim through the new `Session.SendSeq`): the vt
  emulator's key encoder matches unmodified cursor keys only and silently emits *nothing* for
  `shift+up` & co., so those presses never reached the child at all. Chords ike does not claim
  now go out xterm-style — `CSI 1 ; <mod> <final>`, `<mod>` = 1 + shift 1 + alt 2 + ctrl 4 +
  meta 8 — so `shift+up` prints `^[[1;2A` under `cat -v`. Plain arrows keep the emulator path
  (application cursor-key mode included); the reserved set, `shift+pgup`/`pgdn` paging and the
  macOS natural-editing translations (#225, #240) are unchanged, and `cmd` chords stay off the
  new path (super is not xterm-encodable). Updated
  [Terminal](/architecture/terminal.md).

## 2026-08-12 (fix docs deploy CI step failing with non-fast-forward push, #1839)

- **`mkdocs gh-deploy` now runs with `--force`** in both `.github/workflows/docs.yml`'s `deploy`
  job and `make docs-deploy` (Makefile). Without it, the CI job introduced by #1837 failed every
  time: `actions/checkout`'s shallow, single-branch checkout never fetches `gh-pages`, so `mkdocs`
  has no local `origin/gh-pages` ref to rebase its new commit onto, builds a rootless commit
  instead, and a plain `git push` is rejected as non-fast-forward. `gh-pages` is a disposable build
  artifact, not a branch anyone develops on, so force-pushing it on every deploy is the intended
  model. Updated [Documentation Site Build & Deploy](/architecture/docs-site.md).

## 2026-08-12 (make target to deploy the docs site to Pages without Actions, #1837)

- **`make docs-deploy`** builds the MkDocs site strictly and publishes it to `gh-pages` via
  `mkdocs gh-deploy --strict` (Makefile), failing with a plain error (not a stack trace) if
  `mkdocs` isn't installed. `.github/workflows/docs.yml`'s `deploy` job now uses the identical
  command instead of `actions/deploy-pages`, so CI and the local target publish through the same
  mechanism — either can update the live site, which matters when the Pages queue backs up
  (#1606) or Actions is unavailable. Required switching the repo's Pages source from "GitHub
  Actions" to "Deploy from a branch" (`gh-pages` / root); branch deploys are otherwise silently
  ignored. New [Documentation Site Build & Deploy](/architecture/docs-site.md).

## 2026-08-12 (re-send the exact request of a stored response, #1832)

- **A stored response can be sent again, verbatim**: `httpclient` now captures
  a `RequestSnapshot` on every dispatch — method, final URL, headers and body
  *as they went on the wire*, after substitution and after `.netrc`/`.curlrc`
  (a `Host:` override included, which Go keeps out of the header map) — hands
  it back on `Response.Request`, and `internal/httphistory` persists it with
  the entry under `.ike/http/` (`request`, with the readable `bodyText` /
  base64 `body` split the response body already uses). `httpclient.Resend`
  sends such a snapshot again without going through `prepare`: no re-parse, no
  re-substitution, no `.netrc`/`.curlrc` header mapping, so an edited `.http`
  file, a changed variable or a switched environment cannot alter what is
  repeated; only the client itself is still built from `.curlrc` (proxy, TLS,
  timeouts). A re-sent stream re-opens as a stream — SSE/NDJSON (#1776) are
  not excluded. In the response pane, `ctrl+r` and a clickable `⟳ re-send`
  label in the header (shown only where a snapshot exists, hit-tested from the
  same composition it is drawn from, ahead of the selection press like the
  fold's `⧉`) emit `httppane.ResendMsg`; the app dispatches through the shared
  `dispatchHTTP` path, so the duplicate guard, the in-flight indicator and the
  history append are the ones `http.run` uses, and `http.resend` ("Re-send
  Stored HTTP Request") is the palette pendant. History entries written before
  the capture load unchanged and only lose re-send, with a notice instead of a
  silent key. The substituted request lands on disk in clear text, like the
  response bodies already did — noted in the wiki's security paragraph.
  Updated [HTTP Client](/architecture/http-client.md).

## 2026-08-12 (default keybinding for showing stored HTTP responses, #1831)

- **`http.showResponse` gets a default chord**: `cmd+shift+enter` primary with
  a `ctrl+shift+f9` fallback, editor-scoped like `http.run`'s `cmd+enter` /
  `ctrl+f9` pair (`internal/keymap/defaults.go`). Looking at a stored response
  without dispatching (#1492) no longer requires the palette — the shifted
  sibling of the run chord opens the viewer for the request under the cursor.
  `ctrl+shift+f9` is CSI-parameter-encoded, so unlike plain `ctrl+f9` it is not
  eaten by macOS and reaches the program on darwin too. Command gating is
  unchanged. Regenerated `userdocs/reference` and the keymap status matrix.
  Updated [Keybindings & Shortcuts](/architecture/keybindings.md),
  [HTTP Client](/architecture/http-client.md).

## 2026-08-12 (palette-opened viewers land as a tab in the focused pane, #1825)

- **A database picked in the '@' finder no longer splits the editor**: the
  palette's file open (`openPathFocused`, `internal/app/app.go`) records the
  focused pane in `m.viewerTabHost` before the file handler runs, and the
  viewer opens — `openDataPane`, `openImagePreview`, `openArchivePane` —
  consume it through `takeViewerTabHost`/`openContentTab`
  (`internal/app/panecontent.go`), nesting the viewer as a content tab (#1778)
  instead of splitting off `viewerSplitTarget`. The pane converts into a tab
  host when needed and a lone empty scratch tab gives way to the viewer, so a
  `.duckdb` picked from an editor lands exactly where a plain file would. The
  image preview and archive viewer were pulled along: the asymmetry was the
  open path's, not the data viewer's. Everything else keeps the #1779 split —
  explorer, `:e`, CLI and plugin opens, and a palette pick made while the
  explorer or a tool window holds focus (they cannot host tabs). An already
  open viewer for the path is still only refocused.
  Updated [Data Viewer](/architecture/data-viewer.md),
  [Pane Layout & Drag](/architecture/pane-layout.md),
  [Image Preview](/architecture/image-preview.md),
  [Archive Viewer](/architecture/archive-viewer.md).

## 2026-08-12 (terminal auto-scroll while extending a mouse selection, #1821)

- **A selection drag past the pane edge scrolls the terminal**: `MouseDrag` in
  `internal/terminal/model.go` reads the pane-local row it already receives as
  a scroll trigger — above the pane pages one line into the scrollback, below
  it one line back towards the live view (stopping at offset 0) — and extends
  the selection to the clamped cell afterwards, so more than a screenful can be
  marked and copied with the mouse. The anchor logic is untouched: the
  selection is anchored virtually, only the window moves. To keep scrolling
  while the pointer rests at the edge, the drag returns a 60 ms `tea.Tick`
  (`terminal.AutoScrollMsg`, carrying session key and generation) that
  `internal/app` routes back into the terminal for the active `dragTermSelect`
  drag; the repeat retires at the ends of the history and on release. Word- and
  line-wise drags (#951) behave identically, and the mouse-reporting
  (`WantsMouse`) and scrollbar-drag (#1368) paths are unchanged.
  Updated [Terminal](/architecture/terminal.md).

## 2026-08-12 (finer undo granularity in insert mode, #1818)

- **An insert session commits several changes instead of one**:
  `internal/editor/insertundo.go` splits the open `history.Recorder` at the
  boundaries a user thinks in, so `Cmd+Z` no longer discards everything typed
  since entering insert mode. A paste mid-insert (`Cmd+V`, bracketed paste) is
  exactly one change — the running segment commits first, the block gets a
  recorder of its own, and that closes right after, so characters typed
  afterwards undo before the block and never with it. Typing splits word-wise:
  a new segment opens in front of a word run that follows a separator, making a
  change "one word plus the separators typed after it" (`foo bar baz` → three
  undos). A segment that holds no word yet never splits, so the auto-close pair,
  the smart indent after `Enter` and the line `o` opened stay welded to the
  keystroke that produced them; backspace, the kills, `Tab`/`Shift+Tab` and
  completion accepts join the running segment. `commitInsert` became "commit
  the rest"; normal-mode operations (`dd`, `ciw`, `:%s`) keep their one-change
  semantics, and `history` is untouched — branching (#59), `g-`/`g+`, the byte
  budget (#1537) and persistent undo (#148) work on the finer steps unchanged.
  Updated [Editor](/architecture/editor.md).

## 2026-08-12 (secret masking in JSON files, #1813)

- **JSON values of secret-suspect keys mask**: `plugins/languages/json/mask.go`
  is a second producer of the `secret.value` stand-in span (#1623) beside the
  dotenv one, so `"password": "hunter2"` renders as `••••` in `.json`,
  `.jsonc` and ndjson buffers. Which keys count is decided by
  `internal/secret.Suspect` alone — built-in tables plus
  `editor.secret_masking_keys` (#1712) — so nothing about the heuristic is
  duplicated, and the toggle (`view.toggleSecretMasking`), the conceal file
  rules (#1704) and the positional reveal (#1594) apply unchanged. The quotes
  stay visible and only the string's content masks; only the key directly in
  front of a value counts (nested objects mask by their own keys); non-string
  and empty values are left alone. Masks are emitted ahead of the epoch,
  escape and hint spans so first-covering-wins cannot let a decode render part
  of a credential. Updated [Editor](/architecture/editor.md) and
  [Languages](/architecture/languages.md).

## 2026-08-12 ('@' file finder: offer scratch files when the query matches "scratch", #1812)

- **Scratch files reachable without a mode switch**: `FileMode` gained
  `SetScratchList` (mirroring `ScratchMode`'s own injection over
  `internal/scratch.List`); the `@` finder now appends scratch-store rows,
  newest-first and tagged with a `"scratch"` `Detail` chip, whenever the query
  fuzzy-matches the literal word "scratch" — chosen over matching each
  scratch's own filename as the simplest rule that leaves unrelated queries
  untouched. Project matches keep their existing rank; path queries (#1433)
  and the anchored descend (#1775) are unaffected, both return before the
  scratch step runs. Updated [Command Palette](/architecture/command-palette.md)
  and [Scratch Files](/architecture/scratch-files.md).

## 2026-08-12 (project picker: relative path input resolves against the projects directory, #1808)

- **Relative path browsing/completion targets `ProjectsDir()`**: the picker's
  filesystem browsing and `tab` completion (#542) used to resolve relative
  input against the process working directory, unlike
  `newproject_prompt.go` / `clone_prompt.go`, which already default to
  `project.ProjectsDir()`. `internal/pathcomplete` gained `DirsFrom(baseDir,
  input)` (dirs-only sibling of the existing `CompleteFrom`); the picker now
  joins any non-absolute, non-`~` query against the configured projects
  directory before browsing/completing/opening it. Absolute and `~`-prefixed
  queries are unaffected. Bare queries (no `/`) now also browse — previously
  only `/…`, `~/…`, `./…`, `../…` triggered filesystem browsing at all; the
  history fuzzy-search behavior is unchanged, browse items are additive.
  Updated [Project Switching](/architecture/project-switching.md).

## 2026-08-12 (floating terminals: the popup box raises with focus too, #1806)

- **The popup box is a z-order surface**, no longer the layer's fixed base:
  `popupTerm.boxZ` records how many panels are drawn below it and
  `floatTermsSplit` cuts `floatTerms` at that slot for compositing, hit
  testing (`popupBoxAt`, now strictly top-down) and focus stepping. Focusing
  the box — a click on it or a focus-key step onto it — raises it above every
  panel (`raisePopupBox` from `setPopupFocus`), so a torn-out panel can no
  longer cover the standard popup terminal while it owns the keyboard.
  `raiseFloatTerm` and the panel removal move `boxZ` along, so the box keeps
  its relative slot when a panel below it rises or leaves. Updated
  [Integrated Terminal](/architecture/terminal.md) and
  [Floating Shell](/architecture/floating-shell.md).

## 2026-08-12 (floating terminals: focus raises the panel, #1806)

- **Focus implies raise**: `setFloatFocus` now moves the panel it focuses to
  the top of `floatTerms` itself, so every route into focus — click, focus
  chord, tear-out, tab drop — restores the #1237 invariant "the topmost panel
  owns the keyboard". The old click-only `focusFloatTermPanel` is gone; the
  raise lives in `raiseFloatTerm`.
- **Keyboard focus switch across the layer**: the spatial focus keys
  (`ctrl+left`/`ctrl+right`, #228 overrides apply) step through
  `popupSurfaces` — the box's split sides, then the panels bottom→top — and
  wrap; previously they only crossed the #1427 split. A single surface leaves
  the key to the shell. Updated
  [Integrated Terminal](/architecture/terminal.md) and
  [Floating Shell](/architecture/floating-shell.md).

## 2026-08-11 (popup terminal: move, tear-out panels, global toggle, #1793)

- **Movable popup terminal**: a title-row press outside every tab segment
  starts a titlebar move drag; the box's position persists as an offset from
  center (`popupterm:pos`) through the same project-store/user-fallback
  cascade as the size delta (#1714) and re-clamps on screen in
  `popupTermRect`.
- **Tab tear-out** (`internal/app/floatterm.go`): a tab-bar press arms a
  popup-layer `dragTab` drag — released on free space, the tab becomes its
  own floating panel with the **same live session** (`DetachTerminalTab`,
  the #708 pattern; a single-tab source re-homes its whole host and its slot
  collapses with #1427 semantics); released on another layer box, the tab
  moves there (the reverse direction). Panels follow the #1237 z-rules:
  click focuses and raises, the focused panel owns the keyboard through the
  existing funnel (`popupFocused`), toggle and outside-press hide the whole
  layer as one unit. Re-docking panels into the pane layout is deliberately
  out of scope.
- **Global toggle**: the ●/○ title-row button flips a panel between
  project-owned (#1407 parking in `wsExtras.floats`, dies with the
  workspace) and **app-owned**: a global panel rides across project switches
  with process, scrollback and CWD intact, survives workspace
  teardown/eviction (it never enters `Aux`), and ends only with an explicit
  close or app quit. Popup session keys now mint from a package-level
  counter so carried global sessions can't collide with a fresh model's.
  Updated [Integrated Terminal](/architecture/terminal.md),
  [Floating Shell](/architecture/floating-shell.md) and
  [Workspace](/architecture/workspace.md).

## 2026-08-11 (popup terminal ctrl+d freeze — teardown off the update loop, #1786)

- **Root cause 1 — reflow self-deadlock**: `softWrappedLocked` re-took `gridMu`
  for its reserve match while the width-reflow path (`logicalLinesLocked`)
  already held it. Reachable whenever the resize reserve is wider than the
  grid (a width change during an alt-screen phase narrows the grid without
  resetting the reserve); the resize path then parked forever holding `s.mu`
  and `gridMu`, and every later render touching the session (`Title`, `Cwd`,
  view) froze the whole update loop — no keys, no mouse, no redraw. The lock
  is now the caller's job; `SoftWrapped` takes it at the public boundary.
- **Root cause 2 — blocking teardown on the update loop**: `Session.Close`
  joined the read/feed/write loops synchronously (UI paths: tab close, popup
  collapse, busy-guard confirm, project switch, quit), and the exit path
  joined them **before** sending `ExitedMsg` — a wedged loop withheld the
  exit, leaving the dead tab rendering (and blocking on) the stuck session.
  Teardown is now split: bounded `release` (kill child, close PTY, close
  spool) on the caller, blocking `join` on a background goroutine; the exit
  path notifies right after the release.
- **Busy-close guard pins its target** (#1786): the confirm re-resolves the
  session key recorded at prompt time (`termCloseSess`) instead of closing
  whatever tab/pane is active/focused at confirm time — a guarded shell that
  exited on its own while the prompt was open no longer gets an unrelated
  neighbour killed.
- Data races fixed under `go test -race`: `LineText`/`HistoryLine` copied
  cells through live-buffer pointers without `gridMu`.
## 2026-08-11 (data viewer: instant open, lazy row counts, #1795)

- **Opening a large database no longer blocks the IDE** (#1795): the engine
  open, the table listing and the first page moved into a background
  `tea.Cmd` (`dataview.Model.Init`, dispatched by `openDataPane` and by the
  model's `initDataPanes` for restored panes). The pane is on screen and
  focused from the first frame, drawing `opening <file>…` until the result
  arrives as a `dataview.ResultMsg` routed by the model's own key.
- **Exact row counts became lazy and cached**: `Tables()` no longer runs a
  `COUNT(*)` per object and `Page()` no longer counts per fetch. Listings
  carry metadata estimates instead (SQLite `sqlite_stat1` / `max(rowid)`,
  DuckDB `duckdb_tables().estimated_size`, the Parquet footer), and the new
  `Source.Count(table, clause)` runs in the background for the loaded table
  only, once, with the result cached per table and filter — so paging issues
  no count queries at all.
- **Estimates are visible as estimates**: `~1204` in the sidebar and status
  line until the counted number replaces it, `?` for what cannot be counted,
  and `G` (last page) waits for an exact total rather than jumping on a guess.
  Updated [Data Viewer](/architecture/data-viewer.md).

## 2026-08-11 (floating stack finalized, #1237)

- Finalized the z-ordered floating stack (#1237): the paste-capture predicate
  now asks the stack (`floats.IsOpen()`) instead of only the base shell, so a
  transient pushed layer captures pastes too; app-level tests pin the layered
  end-to-end behavior — two panes composited topmost-last, esc and
  outside-click popping one layer at a time
  (`/architecture/floating-shell.md`, `/architecture/pane-layout.md`).

## 2026-08-11 (universal tabs: any pane as a tab in any pane, #1778)

- **Tabs stop being an editor privilege** (#1778): a `pane.Tab` slot carried
  either a document editor or a terminal, and only editor and terminal panes
  could host tabs. A slot now also carries a **nested `pane.Instance`** of a
  viewer kind, so a markdown preview, image, diff, archive, data viewer or HTTP
  response lives in a tab strip beside the files it belongs to — with the
  per-kind size/focus/palette/config/view/update/close dispatch reused verbatim
  from the pane path, and the content never reloading as it moves.
- **The host and source matrix widens**: one predicate, `pane.KindTabbable`,
  decides both who can be a tab and who can host one. A center drop converts a
  viewer pane in place (`ConvertToTabHost`) exactly as it converts a terminal
  (#836), `dragCarriesTab` replaces the old files-or-terminal pair, and a
  content tab dragged onto any edge splits back out as its own viewer pane
  (`AddContentPaneFrom`). **Singletons stay out**: explorer, VCS, Debug,
  Problems, Structure, Usages, Breakpoints and the merge view keep their
  toggle-driven roles and show edge zones only; the HTTP viewer is a tab source
  but not a host (its singleton pane key would trap the tab).
- **Everything downstream follows the content, not the pane**: dedupe-and-focus
  opens, async ticks (`RenderTickMsg`, `EditRequestMsg` — now matched by the
  viewer model's own key), key scoping and mouse routing resolve through the
  new `panecontent.go` helpers, and the layout store persists content tabs
  (`ctabs`/`activeCtab`) with the same identity a dedicated viewer pane saves.
  Tab labels gain per-kind glyphs. Updated
  [Editor Tabs](/architecture/editor-tabs.md) and
  [Pane Layout & Drag](/architecture/pane-layout.md).

## 2026-08-11 (data viewer: mouse, tab, and page keys that page, #1788)

- **The data viewer takes the mouse** (#1788): it was keyboard-only — its
  `Update` saw key presses and nothing else, and `pane.KindData` appeared in no
  branch of the app's wheel or click dispatch. It now exposes `Wheel`, `WheelX`
  and `Click`, routed like every other pane's mouse API. The wheel scrolls the
  focused region (table list or grid rows), crossing into the neighbour DB page
  when it is already parked at the loaded page's edge; horizontal wheel and
  shift+wheel pan the grid's columns. A click gives the clicked half the region
  focus, selects the sidebar object (a second click within 400 ms loads it,
  like `enter`) or moves the grid's row cursor. With the filter line open
  (#1777) clicks are inert, so a half-typed clause survives a stray click.
- **`tab` reaches the pane**: the pane had a region toggle, but the IDE's
  global `case "tab": cycleFocus()` fired first, so it never ran. A focused
  data pane is now an exception to that fallback and gets the key; pane focus
  stays on `ctrl+tab` and the focus keys.
- **`pgup`/`pgdown` page a screenful, not a fetch**: they were mapped onto the
  whole-page fetch (500 rows), which made them a plain no-op on any table
  shorter than one page. They now move by `bodyHeight` rows inside the loaded
  page and only cross into the neighbour page from its edge, while `n`/`p` and
  `ctrl+f`/`ctrl+b` keep the DB-page fetch. Updated
  [Data Viewer](/architecture/data-viewer.md).

## 2026-08-11 (viewer panes split the pane you came from, #1779)

- **Data, image and archive viewers no longer split the explorer** (#1779):
  opening a `.duckdb`, a `.png` or a `.tar` from the explorer used to split the
  *focused* leaf — the explorer itself — so the viewer landed on the far left
  instead of next to the code. The three opens now share
  `viewerSplitTarget`, which mirrors `fileEditorKey`: focused pane when it
  hosts content, else `m.recentEditor`, else the first content leaf; the
  explorer and the singleton tool windows never qualify, and the focused leaf
  remains the last-resort fallback so a tool-windows-only workspace still gets
  its pane. The refocus path for an already-open path is unchanged.

## 2026-08-11 (data viewer: filter the grid with '/', #1777)

- **`/` in the data grid filters it with SQL** (#1777): the filter line holds
  everything that follows `SELECT * FROM <table>` — a `WHERE`, an `ORDER BY`, a
  `LIMIT` — with that fixed head drawn dimmed in front of the input and the
  clause SQL-highlighted from the theme's captures. `enter` applies, `esc`
  drops the filter and brings the whole table back, and an applied filter stays
  visible in the pane header.
- **One shape across the engines**: `Source` grew `PageWhere(table, clause,
  offset, limit)` and `FilterPrefix(table)`, and every backend runs the clause
  inside a subquery with the pane's `LIMIT/OFFSET` outside it. Paging therefore
  survives a filter (the count query counts the same subquery, so `rows X–Y of
  N`, `n`/`p` and `G` keep their meaning) and a user's own `LIMIT 100` bounds
  the result instead of fighting the pane. Parquet has no query engine, so its
  filter — and only its filter — shells out to the `duckdb` CLI over
  `read_parquet('<file>')`; without the binary the filter says so and the
  unfiltered grid keeps working through the pure-Go reader.
- **Read-only under free text**: a clause carrying a `;` outside a literal,
  identifier or comment is refused before any engine sees it
  (`ErrMultiStatement`), as is an unterminated literal or block comment that
  would swallow the wrapper's closing parenthesis. The read-only opens
  (`mode=ro`, `-readonly`) remain the first line. A clause the engine rejects
  shows the engine's own message under the still-open filter line while the
  grid keeps its last good page. Updated
  [Data Viewer](/architecture/data-viewer.md).

## 2026-08-11 (editor: kwarg conceals in multi-line calls, #1773)

- **Keyword-argument conceals no longer stop at the line break** (#1773, fixes
  #1761): the call scan in `internal/consthint` was line-local — the
  parenthesis depth restarted at every line, so a formatted call put its
  arguments out of reach. It now runs as one scanner over the buffer, carrying
  depth and the argument-slot state across lines, so
  `RotatingFileHandler(\n  "app.log", maxBytes=10 * 1024 * 1024, …)` draws its
  `10 MiB` on the continuation line and PHP named arguments do the same.
- **Carried depth needs carried lexical state**: the scanner tracks Python
  triple-quoted strings and PHP `/* … */` blocks across lines (their contents
  conceal nothing) and forgets any open call on what it cannot read to the end
  of a line — an unterminated one-line string, a heredoc — so a `(` inside a
  string never fabricates an argument context. A line inside an open call is
  not a statement, so the lowercase-assignment shapes skip it and each kwarg
  reports exactly once. Updated [Editor](/architecture/editor.md).

## 2026-08-10 (editor: unit-gated conceals for variables and kwargs, #1761)

- **Constant conceals reach lowercase variables and keyword arguments**
  (#1761, extends #1701): `internal/consthint` now conceals a statement-level
  lowercase assignment (Python `duration = 5000` → `5s`, PHP
  `$timeout = 2500;` → `2s500ms`) and keyword-argument literals inside call
  parentheses (Python `process(duration=24 * 60 * 60)`, PHP named arguments
  `retry(timeout: 5000)`), several per line. Unlike CONST_CASE — which
  conceals on the case alone — both new shapes require the name to carry a
  recognised unit context (the user's `number_hint_units` mapping or a
  built-in duration/size/radix key word), so `n = 8` and `attempts=3` stay
  raw; single-letter names never qualify. Python `def` defaults conceal like
  call-site kwargs; Go stays `const`-only (a `var`/`:=` literal is an initial
  value, not a constant).
- **Same safety and channels**: the narrow evaluator is unchanged — any
  identifier, call, float or string on the value side leaves the line raw —
  and the kwarg scan is quote-aware with `==`/`::` never read as a separator,
  so comparisons and quoted `"duration=5"` never fire. The spans reuse the
  numhint/epochtime captures, so toggles and positional reveal apply as-is.
  Updated [Editor](/architecture/editor.md).

## 2026-08-10 (viewer: Parquet backend for the data pane, #1766)

- **Parquet files open in the data pane** (#1766): `.parquet`/`.pqt` by
  extension plus the `PAR1` magic at **both** ends of the file route into the
  same `KindData` pane — a `.parquet` never lands in a text buffer again. A
  Parquet file is one table, so the sidebar's list degenerates to a single
  entry named after the file; the grid, the paging and the pane are unchanged.
  The leading magic alone is not enough for a content sniff (any text file may
  begin with those four bytes), so the check reads the trailing four bytes too.
- **The library decision: `github.com/parquet-go/parquet-go`, not the DuckDB
  CLI.** #1765 had just landed a generic DuckDB path that could have served
  `SELECT * FROM 'x.parquet'` for free. It was rejected because it would make
  *viewing a file* depend on a binary the user may not have installed — a soft
  dependency is acceptable for DuckDB databases, whose owner already has
  DuckDB, but not for a format people receive from elsewhere. parquet-go is
  pure Go, so the build stays cgo-free and costs nothing at run time.
- **Never a full load**: `parquet.OpenFile` parses the footer only, so opening
  a multi-gigabyte file is instant, and one page is `SeekToRow` + `ReadRows`,
  decoding only the column pages that window touches. Row groups are crossed
  transparently; nothing resembling `ReadAll` appears in the backend.
- **Corrupt files degrade to a notice**: the library *panics* rather than
  returning an error on some malformed schemas, so both `OpenParquet` and
  `Page` recover and turn it into the pane's error notice.
- **The schema view is the point of the format**, so `s` shows more than a DDL
  line: row count, row-group count, column count, compression codecs, the
  writer that created the file, one line per leaf column (dotted path,
  physical type, logical type, nullability), and the native `message { … }`
  block — in `--` comments so the `.sql` virtual buffer still highlights.
- **Logical types render readably**, and at the *leaf*, so a timestamp buried
  in a list of structs reads like a top-level one: timestamps and INT96 as ISO
  8601 (`Z` only when the column is adjusted to UTC — an unzoned column gets no
  invented offset), decimals through `math/big` with the scale applied (no
  float round-trip, no exponent), UUIDs canonically, unsigned ints unsigned,
  untagged byte arrays as text when valid UTF-8 and `<bytes N>` otherwise, and
  list/map/struct as compact JSON with the `LIST`/`MAP` wrappers unwrapped
  (`["red","blue"]`, not `{"list":[{"element":…}]}`).
  Updated [Data Viewer](/architecture/data-viewer.md).

## 2026-08-10 (viewer: DuckDB backend for the data pane, #1765)

- **DuckDB databases open in the data pane** (#1765): `.duckdb`/`.ddb` by
  extension plus the `DUCK` magic at offset 8 (the storage header opens with
  an 8-byte checksum) route into the same `KindData` pane as SQLite —
  sidebar of tables and views with row counts, paged read-only grid, `s` for
  the `CREATE` statement. Nothing in the pane changed; the engine plugs into
  `datasrc.Open`, which now decides by magic first and extension only as the
  tie-break, so a DuckDB database named `app.db` still reaches the right
  engine.
- **The driver decision: the `duckdb` CLI, not the cgo driver.**
  `github.com/marcboeker/go-duckdb` would require **cgo** and bundle a static
  libduckdb (~100 MB of prebuilt archives), ending this repo's pure-Go
  cross-compilation, lengthening every build, and **pinning** the storage
  format to the linked libduckdb — a database written by a newer DuckDB would
  fail until IKE is rebuilt. The same reasoning already picked
  `modernc.org/sqlite` over a cgo driver. Shelling out to
  `duckdb -readonly -json <file> "SELECT …"` costs nothing at build time, is
  paid only by users who open a DuckDB file, and follows whatever format the
  installed CLI understands. One invocation measures ~20 ms.
- **A missing binary is an actionable state, not a failure**: the engine
  returns a `datasrc.MissingToolError` and the pane draws a centered dialog
  naming `duckdb` with the install hints — never a crash or a silent empty
  grid. The binary is looked up on PATH and then in the usual install
  directories, since a GUI-launched IKE inherits a minimal PATH (#1614).
- **Locked databases fail fast**: DuckDB takes an exclusive lock, so a
  database another process holds open cannot be read even read-only. The CLI
  reports the lock and the pane shows it; every invocation also runs under a
  30 s timeout with `stdin` closed, so nothing can hang the pane.
- **Rendering parity with SQLite**: BLOBs become `<blob N bytes>` in SQL
  (`octet_length`, so `NULL` stays `NULL`) rather than as an escaped byte
  string, nested `STRUCT`/`LIST`/`MAP` values keep compact JSON, columns come
  from `DESCRIBE` so an empty table still renders its header, and the JSON
  rows are decoded token by token to preserve column order. Row counts for
  the whole sidebar arrive in a single invocation.
  Updated [Data Viewer](/architecture/data-viewer.md).

## 2026-08-10 (viewer: SQLite data pane — table sidebar, paged read-only grid, #1764)

- **A SQLite database opens as a table browser, never as a binary buffer**
  (#1764): `.db`/`.sqlite`/`.sqlite3` by extension plus the `SQLite format 3`
  magic sniff land in the new `KindData` pane — a sidebar of tables and views
  with row counts next to a grid of the selected object's rows. A claimed
  `.db` *without* the magic still opens the pane, whose centered notice then
  carries the engine's error (encrypted and corrupt files degrade the same
  way).
- **The pane is the shared data grid, the engine is a plug**: the pane speaks
  only `datasrc.Source` — `Tables`, `Page`, `Schema`, `Close` — and holds no
  SQL. The DuckDB (#1765) and Parquet (#1766) viewers add engines behind
  `datasrc.Open` without touching the pane. Today's engine is
  `modernc.org/sqlite`: pure Go, cgo-free cross-compilation kept, the size
  accepted deliberately.
- **Read-only by construction**: the database opens with DSN `mode=ro` on a
  single connection, so a live application database is neither locked nor
  mutated — a concurrent writer proceeds, and its rows appear on the next
  page fetch.
- **Paged, never loaded whole**: 500 rows per fetch via
  `ORDER BY rowid LIMIT/OFFSET` (stable paging on ordinary tables; views fall
  back to the bare scan). `j`/`k` cross page edges transparently, `n`/`p`
  step pages, `g`/`G` jump to the ends, `h`/`l` scroll columns; the status
  line reads `rows X–Y of N`. `NULL` renders as a faint `∅` — distinct from
  the empty string — and BLOBs as `<blob N bytes>`.
- **`s` shows the `CREATE` statement** in a read-only buffer under the
  virtual path `<db>!<table>.sql` — the archive-entry `!` convention (#1762),
  so the tab reads `users.sql (app.db)` with SQL highlighting and no way to
  write back.
  New [Data Viewer](/architecture/data-viewer.md).

## 2026-08-10 (viewer: .gz opens transparently decompressed, read-only, #1763)

- **A plain `.gz` opens as its content, not as compressed bytes** (#1763):
  `app.log.gz` decompresses into memory and shows in a **read-only** editor
  buffer — no pane of its own, since a plain gzip holds exactly one file. The
  buffer's virtual path is `<archive>!<inner>`, so the tab title, the language
  lookup and the highlighting resolve from the *inner* name: `.log.gz` gets
  log mode including repeat folding, `.json.gz` gets json. `gzfile.InnerName`
  strips the `.gz` first and only then falls back to the gzip header's
  original-name field, which is optional and routinely wrong.
- **Routing is the exact complement of the archive viewer** (#1762): the
  `gzip` handler registers **no** extensions — the registry matches
  `filepath.Ext`, which reads `.tar.gz` as `.gz` — and claims through content
  instead. A name ending `.tar.gz`/`.tgz`/`.tar.bz2`/`.tbz`/`.tbz2`, or a
  stream whose first decompressed block holds a tar header, goes to the
  archive pane; everything else with gzip magic lands here. Exactly one of the
  two handlers ever answers.
- **The cap counts decompressed bytes**, never the compressed size, so a gzip
  bomb costs one limit-sized buffer: `gzfile.Read` reads one byte past
  `files.large_file_kb` and reports truncation. `files.large_file_lines` caps
  the line count afterwards. Either cap firing warns and still shows what was
  read; a corrupt tail keeps the bytes decompressed so far.
- **Binary content degrades to a metadata notice** — archive name, inner name,
  compressed/decompressed sizes and ratio — instead of a mojibake buffer. The
  decompressed size comes from the gzip footer's `ISIZE`, treated as advisory
  (it is modulo 2^32): nothing is ever allocated from it, and a truncated read
  shows no ratio rather than a wrong one.
- **Saving is refused and reload has its own wire**: the buffer goes in through
  `editor.ShowReadOnly`, so every mutation answers `E45: buffer is read-only`
  and the virtual path can never become a file. Because that path names content
  *inside* the archive, the editor's own watcher matching never fires — the
  root model's `refreshGzipBuffers` re-decompresses every matching tab on a
  `FileChanged`/`FileCreated` event for the outer `.gz`.
  New [Gz Viewer](/architecture/gz-viewer.md).

## 2026-08-10 (viewer: archive pane with read-only entry preview, #1762)

- **Archives open as an entry list, never as a raw text buffer** (#1762): a
  `.tar`, `.tar.gz`/`.tgz` or `.tar.bz2` opens in the new `KindArchive` pane
  (`internal/archview`) showing a collapsible tree with per-entry size, mode
  and mtime — directories grouped and synthesized where the archive never
  named them. Navigation is the shared list contract (`j`/`k` wrap, page keys
  clamp, `g`/`G`), plus `enter`/`l` to open or unfold, `space` to fold and `h`
  to collapse or jump to the parent. Reading is stdlib-only (`archive/tar`,
  `compress/gzip`, `compress/bzip2`); no dependency was added.
- **The sniff looks inside compressed streams**: the `archives` file handler
  claims `.tar`/`.tgz`/`.tbz`/`.tbz2` by extension, and everything else
  through content — gzip or bzip2 magic decompresses exactly one 512-byte
  block and tests it for a tar header, so `backup.tar.gz` lands here while
  `app.log.gz` stays with the gz viewer. A magic-less v7 tar is recognised by
  its header checksum. Corrupt, truncated, missing or empty archives degrade
  to a notice in the pane (a partial listing is kept), never a crash.
- **Enter previews one entry read-only**: the member is extracted into memory
  behind the large-file thresholds (#149) — binary members are refused rather
  than rendered as garbage — and installed via the new
  `editor.ShowReadOnly` under the virtual path `<archive>!<entry>`, whose tail
  is the member's own file name, so tab title, language lookup and syntax
  highlighting resolve with no special casing. Read-only is a real editor
  state: every mutation refuses with `E45: buffer is read-only` and `saveAs`
  — the single funnel under `:w`, `:wq`, `:w other` and the autosave — fails,
  so the virtual path can never become a file. `Load`/`NewFile` clear it. The
  pane title and tab label carry `[RO]`. Session restore reopens the archive
  pane by path; read-only tabs are session-local like scratch tabs.
  New [Archive Viewer](/architecture/archive-viewer.md).

## 2026-08-10 (log: repeat runs fold on durations, pages and counters, #1758)

- **A repeat run no longer needs the timestamp to be the only thing moving**
  (#1758, extends #1650): `logline.RepeatKey` blanks every *moving part* of a
  line, not just the stamps — duration-shaped values of `elapsed`/`duration`/
  `took`/`latency`/`dt`/`rtt`/`*_ms`, numeric values of `page`/`offset`/
  `attempt`/`retry`/`count`/`rows`/`progress`/`seq`/`index`/`batch`/`step`
  (and an opaque `cursor`), plus four message shapes: duration tokens
  (`340ms`, `2m30s`, `00:00:12`, `3 seconds`, unit included so `980ms` matches
  `1.2s`), a number behind a pagination keyword with its optional `of N` tail,
  a ratio (`17/240`) and a percentage (`42%`), and the count in front of a
  counting noun (`1500 rows`, number only). A paging fetch loop therefore folds
  into one row with a `×N` badge instead of hundreds of near-identical lines.
- **Conservative by construction**: a bare number is never blanked on its own,
  so `status=200`/`status=500`, ports, ids, `/api/v1/users/42`, `v1.2.3` and
  `Foo.java:42` keep different keys; a digit run glued to a dot or a slash on
  its left is skipped, a ratio may not run into a second slash (`01/02/2024`),
  and a token followed by a letter is none (`5mb`). The scan
  (`logline/variable.go`) is hand-written and digit-anchored, not a regex
  sweep — `RepeatKey` runs per line per document version, where a wide
  alternation measured ~34 µs/line against ~2 µs for the whole rest of the key.
  `logfold.go` is untouched: it hangs on key equality, so positional reveal,
  the `×N` badge and raw-mode gating are unchanged.
  Updated [Editor](/architecture/editor.md).

## 2026-08-10 (editor: soft wrap breaks on conceal display cells, #1756)

- **Soft wrap now breaks on the display cells of the concealed expansion**
  (#1756, follow-up to #1752): on a line carrying conceal ranges `wrapSegs`
  hands the `concealPrefix` sums to the new `viewport.WrapSegmentsDisplay`,
  which budgets a stand-in's replacement width at its range start and hidden
  columns nothing. A mask wider than its source no longer overflows the visual
  row; a narrower one no longer breaks rows early. Zero-width columns never
  start a row, so a break never lands inside a stand-in.
- **Consumers unchanged**: segments stay rune-column starts, so `ScrollWrapped`,
  gj/gk, `wrapRows`, `wrapClickAt`, `DisplayRow` and the per-segment
  `renderSpan` slicing follow without modification, and lines without conceal
  ranges wrap exactly as before. Updated [Editor](/architecture/editor.md).

## 2026-08-10 (http: in-flight progress inline at the request, #1746)

- **A running `.http` request is now marked in the file itself** (#1746): the
  request line carries `⟳ 1.2s` at its right edge, refreshed by the existing
  250 ms flight tick and cleared on response, error and `http.cancel`. Until
  now a dispatch was only visible in the statusline segment and the response
  pane, so looking at the `.http` file — the place the request was started
  from — told nothing.
- **App pushes, editor renders**: `refreshHTTPFlightMarks` builds a 0-based
  line → text map per open editor from `Model.httpFlight` and hands it to
  `editor.SetHTTPFlight`; the marker lines are resolved by re-parsing the
  buffer against the running request keys, so several requests of one file are
  marked independently and an edit above a request moves its marker with it.
  Updated [HTTP Client](/architecture/http-client.md).

## 2026-08-10 (copying a collapsed fold now copies its hidden content, #1741)

- **A closed fold is one unit for yank, delete and change** in the editor
  (#144): a linewise target covering a collapsed header grows to that fold's
  end line before the operator sees it (`expandFoldTarget`, applied at the
  `runOperator` choke point plus the ex `:y`/`:d` and visual-put paths), so
  `yy`, `V`+`y`, Cmd+C, `dd`, `cc` and Cmd+X act on what the row *stands for*.
  Vim's rule for a linewise operation on a closed fold; several covered headers
  expand to the largest end and nested folds pull in on a repeated scan.
- **Delete and change expand too**, deliberately: half-deleting a collapsed
  fold would leave hidden remnants no one asked for. The reshaping operators
  (`>`/`<`, `=`, `gu`/`gU`/`g~`, `gq`, `ys`) and every charwise target keep
  acting on the literal range — they rewrite what is on screen. The register
  entry stays linewise, so a paste reproduces the whole block and the copy
  toast (#252) reports the real line count.
- **The HTTP response viewer got the same rule** (#1330): a selection whose row
  range covers a collapsed fold header grows to the fold's last row in
  `SelectionText` (`expandFolded`), so folding a huge JSON field, selecting the
  single row it became and copying yields the whole field. Selections spanning
  past a fold are unchanged — the rows were already in range.
- **The other collapsed-row features need nothing**: repeat runs (#1650) and
  PEM blocks (#1652) hide lines inside the contiguous linewise range anyway,
  and a cursor on their header row already reveals them. See
  [/architecture/editor.md](/architecture/editor.md) and
  [/architecture/http-client.md](/architecture/http-client.md).

## 2026-08-09 (editor: the collapsed-run ×N marker became a badge, #1734)

- **The `×N` marker of a collapsed log repeat run (#1650) now draws in theme
  colours instead of `Faint`.** Dimmed and glued to the end of a normally
  styled line, it was nearly invisible — that a row stood for N occurrences was
  easy to miss entirely.
- **Emphasis scales with the run length**: `Info` below ×10, `Info` bold from
  ×10, `Warning` bold from ×100. A ×2 is a detail; a ×500 means the buffer is
  almost entirely this one line. Both colours are contrast-checked against
  every builtin theme's surfaces, so the badge holds up light and dark.
- **Placement moved to the shared annotation column** (the delta hints' of
  #1651/#1730), so the badges of a file form one scannable column instead of a
  ragged trail of line ends. No conflict with the one-annotation-per-row rule:
  a collapsed header carries no delta hint, and blame only annotates the cursor
  line, which never renders as a collapsed header. A line too long for the
  column keeps the badge appended after its text — the count is structure, not
  an optional hint, so unlike a delta it is never dropped. See
  [/architecture/editor.md](/architecture/editor.md).

## 2026-08-09 (marksman: scope wiki/ as its own workspace, #1726)

- **`wiki/.marksman.toml` added** so marksman roots the wiki at `wiki/`
  instead of the repository root. With the repo root as workspace,
  root-relative links (`/architecture/index.md`) resolved against the repo
  and every one in `wiki/index.md` flagged as "Link to non-existent
  document"; `.gitignore`d targets are likewise invisible to marksman's link
  index. Root detection already prefers the nearest marker
  (`.marksman.toml` before `.git`), now guarded by a regression test.

## 2026-08-09 (directory settings get path completion, #1720)

- **`project.directory` moved from `String` to `Path`**, so it opens the path
  editor with the live suggestion list and tab completion (#541) instead of a
  plain text input. New `Entry.Dirs` flag runs the completion engine's
  directories-only flavor (`pathcomplete.Dirs`) and switches the commit check
  to `resolvableDir`: a file is refused, and a directory that does not exist
  yet is accepted when its parent does — the setting is created on first use.
  Sweep of the remaining `String` entries found no other filesystem-valued
  ones (`project.background_lsp_timeout`, `debug.php.hostname`,
  `marketplace.catalog_url` are a duration, a host filter and a URL).

## 2026-08-09 (new-project wizard, #1718)

- **`project.new`** ("New Project…", palette + File menu) opens a three-step
  shell wizard: project type → toolchain → directory name. Types come from
  the language registry's new optional `Toolchain` extension
  `lang.ProjectScaffolder` (`ProjectOptions` / `ScaffoldProject`); python
  offers uv (`uv init` + `uv sync`) or pip/venv (`python -m venv .venv` +
  `main.py`), go scaffolds `go mod init` + `main.go`, php a plain
  `index.php`. The create runs as one async `tea.Cmd`; a failure removes the
  partial directory and stays in the dialog, success opens the project via
  the regular switch transaction. `project.CloneTarget` generalized to
  `project.Target` (shared clone/new-project target rules).

## 2026-08-08 (terminal: smaller popup default, global size fallback, #1714)

- **The popup terminal opens smaller.** Its default fractions drop from
  0.75 × 0.70 to 0.60 × 0.55; the floors (`popupTermMinW`/`popupTermMinH`) and
  the `ui.popup_max_width` cap are untouched.
- **The last resize follows you between projects.** A second, user-scoped
  `ui.WinSizes` store (`~/.ike/winsize-global.json`, `globalWinSizeFile`)
  mirrors every popup resize. `popupSize` resolves its delta as project delta →
  global last delta → plain default, so a project that was never resized opens
  at the size chosen elsewhere and a project with its own delta keeps winning.
- **Deltas, not sizes, travel** — the inherited value re-clamps against the
  live terminal exactly like a project-local one. `popupTermResize` seeds the
  project store from the inherited delta before the first step, so a resize
  never snaps the box back to the default first.
- `WinSizes` grew `Has` (an entry exists, even a zero one) and `Set` (replace +
  persist), and `ui.ResizeDelta` now also accepts the keymap-canonical
  `cmd+shift+arrow` spelling — the popup parses the chord before matching it,
  so the macOS primary resize chord never reached the box.

## 2026-08-07 (editor: custom secret key patterns, #1712)

- **Secret masking takes the user's own key patterns.** `editor.secret_masking_keys`
  (List) extends the built-in tables of `internal/secret`: each entry matches
  case-insensitively over the whole key name with `*` wildcards
  (`MY_API_KEY`, `*_LICENSE`, `db_pass*`), and a `-` (or `!`) prefix exempts
  instead, so `-PUBLIC_TOKEN` keeps a key the built-ins would mask readable.
- **The configured list decides on its own.** `Suspect` consults it first,
  earlier entries win, and only a key no entry matches reaches the built-in
  strong/marker/suffix tables. Nothing else moves: the per-family toggle, the
  conceal file filters (#1704) and the positional reveal apply to a
  custom-matched key exactly as to a built-in one.
- **Installed as a package global** (`secret.SetKeyPatterns`, the arrangement
  `numhint` uses for its field units), because the producer is a
  `lang.Language.Spans` hook with no config plumbing; `app` pushes it on every
  config load and re-parses the open editors when the list actually moved.

## 2026-08-07 (editor: per-file conceal filter, #1704)

- **Conceal now has a *where*.** `editor.conceal_include` /
  `editor.conceal_exclude` gate every conceal family by file glob, and
  `editor.conceal_file_rules` (`family=pattern`, `-` prefix for an exclude)
  overrides them for one family. Precedence is exclude > include > allow per
  level, and a family whose own rules decide a path never consults the global
  one, so masking can stay out of `**/testdata/**` while timestamps keep
  decoding there.
- **`internal/concealfilter` is new**, over a new `internal/pathglob` — the
  LSP watched-files matcher of #1144 lifted out of `lsp/manager` so both
  callers share one glob vocabulary. A separator-free pattern matches the base
  name, one with a separator the whole path, anchored at any segment boundary
  unless rooted or `**`-led; matching is case-insensitive.
- **The filter gates reads, not toggles.** `Model.concealGate` composes the
  two dimensions where a family is consumed (`decodeOn` for the thirteen
  stand-in families, `mdRenderOn`/`svRenderOn`/`logRenderOn`/`pemSummaryOn`
  for the layers), so the toggle fields keep meaning "this family is on" and a
  per-view toggle still bypasses the filter. Edited patterns reach open
  buffers on the next frame, with no reload.

## 2026-08-07 (editor: constant conceals in code, #1701)

- **Constants read like config values.** A constant assignment in a Python,
  Go or PHP buffer draws in the unit its *name* carries — the same key
  heuristics and `editor.number_hint_units` mapping the config formats use
  (#1627, #1685) — with a pure literal-arithmetic right-hand side evaluated
  first: `MAX_BYTES = 10 * 1024 * 1024` conceals as `10 MiB`,
  `SECONDS_PER_DAY = 60 * 60 * 24` as `86_400`.
- **`internal/consthint` is new**: per-language recognition (CONST_CASE in
  Python, `const` in Go, `const`/`define()` in PHP) plus a strict `math/big`
  evaluator — literals and `+ - * / % << >> & | ^` only, per-flavor
  precedence (Go binds `<<`/`&` tighter than Python/PHP), and everything the
  languages disagree on (inexact division, negative remainders) or that
  overflows is refused, so identifiers, calls, `iota` and floats stay raw.
- **No new switches.** The spans reuse the numhint/epochtime captures, so the
  existing toggles gate them and the caret reveals raw source as everywhere
  else (#1594, #1686). `numhint.LiteralHint` and `numhint.KeyUnit` are the
  new exports the producer renders through.

## 2026-08-07 (diff: no soft-wrap, synced horizontal scrolling, #1700)

- **The diff never wraps.** Every row is exactly one visual line now, clipped
  at its column edge — soft-wrapping used to push one side down while the
  other stayed put, breaking the line-by-line alignment the side-by-side view
  exists for. The `↪` continuation marker is gone with it.
- **One offset, both sides.** The model carries a single horizontal offset
  both columns render from, so they move in lockstep and corresponding columns
  stay aligned. `h`/`l`/`←`/`→` step a column, `shift+←`/`shift+→` half a
  column, `0`/`$` jump to the ends; the horizontal wheel and `shift`+wheel do
  the same. The render pass measures the widest displayed line and re-clamps
  the offset, so a resize or a layout toggle can't leave the view past the end.
- **Emphasis and syntax follow the offset.** Intra-line spans and tree-sitter
  captures are indexed in absolute display columns, so both stay on the right
  runes however far the view is scrolled.

## 2026-08-07 (docs: the conceal layer gets its own page, #1698)

- **One page for every stand-in.** `userdocs/guides/conceal.md` documents the
  whole conceal layer — the three guarantees it rests on (nothing is
  rewritten, the caret reveals, everything has an off switch), then one
  section per family: epoch timestamps, the four number-readability families
  with the `editor.number_hint_units` mapping, the three escape families,
  `.http` percent-decoding, cron, file modes, CIDR/punycode, PEM summaries and
  secret masking. Colour swatches and the JWT popup get a section saying why
  they are *not* conceals.
- **Twenty generated shots**, all `monokai-pro`, most as a raw/decoded pair a
  single view toggle apart. `cmd/shotgen` grew the fixtures each family's
  producer needs (a crontab, an `install.sh`, a `kind: Secret` manifest, a
  certificate, a `.env`, a CSS theme, escaped-JSON and ambiguous-field config
  files) and the language imports that go with them.
- **`-out` is resolved before the chdir.** A relative output directory used to
  resolve against the temp fixture project the run deletes on the way out, so
  a plain `make shots` wrote its PNGs nowhere.
- The rendering guide, the modal-editor concept page, the HTTP client guide
  and the generated settings preamble now link the new page instead of
  restating it.

## 2026-08-07 (highlight: injections resolve recursively, #1697)

- **A fragment injects again.** `overlayFragments` re-runs injection
  resolution on every fragment with the fragment's own language, so HTML
  injected into a Python f-string highlights its `<script>` body as
  JavaScript and its `<style>` body as CSS — the same result as a plain
  `.html` buffer, previously one level deep only.
- **Bounded at three levels** (`maxInjectionDepth`): deeper fragments keep
  their enclosing language's styling, so pathological nesting cannot recurse
  without limit. Nested fold ranges shift into host coordinates level by
  level, like the spans.

## 2026-08-07 (search: one result row per matching line, #1121)

- **A line is a row, not a match.** Find in Path (and every other
  `internal/locations` consumer) lists a line once however often the query
  hits it; the extra occurrences ride along as `Item.More` ranges and render
  highlighted in that one row.
- **Counts are per line.** The per-file header count and the status row
  (`N matches in M files`) both count rows now. Replacing still touches every
  occurrence — `internal/app/replace.go` expands the ranges back out first,
  so its `N replacements` summary stays per occurrence.

## 2026-08-07 (editor: field names decide the number conceal unit, #1685)

- **The field wins over the value pattern.** Where a field name names the unit,
  that unit applies even when the raw number also matches another pattern: a
  value in a `bytes` key draws as a byte size rather than a UNIX timestamp —
  a large enough byte count simply lands in the epoch range. Hints derived
  from the value's *shape* alone still give way to a timestamp, as before.
- **`editor.number_hint_units`** (new, a list, empty by default) maps field
  names to units, because the heuristics are ambiguous — `size` is not
  necessarily bytes, a `duration` is sometimes seconds:

  ```toml
  [editor]
  number_hint_units = ["*_bytes=bytes", "retention=s", "created_at=timestamp-s", "session_id=none"]
  ```

  Patterns match case-insensitively over the whole field name with `*`
  wildcards (camel case matched in its snake_case form too); units are `bytes`,
  any duration unit word, `timestamp-s`, `timestamp-ms`, `octal`, `hex`,
  `group` and `none`. Earlier entries win; malformed ones are skipped. A mapped
  field is read in that unit and no other, and `none` turns every conceal off
  for that field — the way out of a heuristic that reads a field wrong.
- Built-in defaults are unchanged where nothing is mapped: the #1627 key words
  and shape triggers decide exactly as before.
- `numhint.Hints`/`Allowed`/`SpansWith` are new — a `Hint` carries a `Claims`
  flag marking the literals whose field name decided the reading, and the
  producers resolve the collision through it. `epochtime.DecodeSeconds` /
  `DecodeMillis` decode with the unit pinned by the field rather than by the
  digit count. See [Number-readability hints](/architecture/editor.md).

## 2026-08-07 (editor: value conceals in every value position, #1684)

- The integer readability conceals — byte sizes, durations, digit grouping and
  radix (#1627) plus epoch timestamps (#1618) — now apply everywhere the
  highlighting already recognises a token as a **value**, not just in the
  config formats.
- New coverage: `.http` query parameters and folded query continuation lines,
  `.http` header values, `.http` inline request bodies (numbers as well as the
  timestamps they already decoded), YAML/TOML/ini/dotenv epoch values, and the
  payload of a log line (its logfmt pairs and JSON tail).
- **Keys are never concealed**, by construction rather than by a list:
  `numhint`'s and `epochtime`'s scanners only match a token that *follows* a
  separator, so a query key, a header name, a logfmt key or a JSON member name
  stays raw even when it is itself numeric.
- `epochtime.Value` is a new context: `JSONValue` widened by the openers and
  closers the non-JSON formats write (`=` opens a value, `&` closes one).
  `JSONValue` keeps its old strictness.
- The `.http` producer now collects its value stretches as `valueRange`s and
  scans each as its own text — so a request line's trailing ` HTTP/1.1` never
  bounds a query value — then lets the number hints step aside wherever an
  epoch, JWT, percent-escape or network stand-in already claimed the columns.
- `numhint.LineSpans` and `numhint.Except` are new, for producers that scan
  part of a buffer or a rewritten line (the log renderer's ANSI-stripped
  visible text). See [Number-readability
  hints](/architecture/editor.md) and [HTTP client](/architecture/http-client.md).

## 2026-08-07 (ui: one navigation contract for every selection list, #1666)

- Every selectable list now obeys the same two rules: **single steps wrap**
  (down on the last entry lands on the first) and **page keys clamp**, jumping
  by the list's own visible height rather than a hard-coded ten rows. `home`
  /`end` go to the extremes wherever the keys are not already the query
  cursor's.
- The semantics live in one place, `internal/ui/listnav.go`: `StepIndex`,
  `PageIndex`, `ClampIndex`, `ScrollToShow`, and a `ListNav` router whose
  `NavKeys` bitmask lets a view opt into the arrow, emacs, vim and home/end
  aliases it can spare. See [Selection-List
  Navigation](/architecture/list-navigation.md).
- Adopted across the board: palette (both columns) and Search Everywhere,
  find-in-path, the `locations.List` component (Usages, Problems, TODO index),
  the explorer tree, the undo tree, call/type hierarchy, Structure,
  Breakpoints, the debugger's frame and variable columns, the VCS changes
  list, the completion popup, every settings page (marketplace and the
  toolchain/colour/venv pickers included), and the shell-hosted pickers (pins,
  local history, VCS history, crash recovery, onboarding, theme and tool
  setup).
- The mouse wheel deliberately keeps clamped semantics everywhere — a flick
  past the end must not teleport to the other end of the list.
- `ui.Floating.ViewportRows()` is new: shell-hosted pickers take their page
  size from the box they are drawn in.

## 2026-08-07 (settings: every file-configurable key has a UI surface, #1663)

- An audit found 22 TOML leaf keys that only a text editor could reach. They
  are schema entries now: the **Explorer** page grew hidden files, git-status
  colours, icons, autoscroll-from-source, sort order and tree indent; new
  **Language Support** (the six `lsp.*` scalars), **Command Palette**
  (`max_results`, `default_mode`, `off_context`), **TODO Index**
  (`todo.patterns`) and **Marketplace Catalog** (`marketplace.catalog_url`)
  pages; **Files & Session** gained the recent-project / background-workspace
  caps and the background LSP timeout; **Terminal** the shell override; and
  **Editor** the rulers.
- `editor.rulers` needed a type: **IntList** runs the existing indexed
  multi-value editor over a `[]int` field, rejecting a non-numeric element in
  the row instead of staging a value the typed decode would refuse.
- A **Path** entry now also accepts a bare name that resolves on `PATH`
  (`terminal.shell = "fish"`) — that is exactly how the terminal spawns it;
  anything with a separator must still exist on disk.
- The gap cannot silently come back: a reflection guard walks `config.Config`
  and fails on any leaf key that is neither in the settings schema nor in one
  of three excuse maps (dedicated custom page / internal state / a named,
  reasoned gap), and a second test fails on a stale excuse. Two known gaps
  remain listed: `explorer.colors` and `theme.terminal`, colour slot maps that
  want a picker page like Syntax Colors.

## 2026-08-07 (settings: the Formatters page edits overrides, #1662)

- **Settings → Formatters** is an editor now, not a report. `e` toggles a
  language's external formatting, `b` its built-in formatter, `enter` opens the
  full `[format.<languageID>]` form — `command` (tab completes a path), `args`,
  `range_args`, `temp_file`, `install` plus the built-in's own keys — `r`
  resets the language to the plugin default and `s` picks the write layer
  (project ↔ user).
- Saving writes **only what differs**: a field left at, or cleared back to, the
  plugin default has its key removed, so an override file never freezes a
  default. Validation runs before anything reaches disk, and the whole table is
  one write-back batch with a single reload.
- Language plugins declare their built-in formatter with
  `format.RegisterBuiltin(langID, keys…)` — that marks the `builtin` switch as
  applicable and declares the built-in's own config keys (SQL's `keywords`,
  with its accepted values) so the settings form edits them generically. SQL,
  XML and `.http` now have rows of their own.
- Docs: the Formatters reference gained the key table and a table of the
  shipped per-language defaults with their install hints. See
  [/architecture/format.md](/architecture/format.md).

## 2026-08-07 (editor: json/yaml path breadcrumb, #1660)

- The status line's new `docpath` segment names the caret's position inside a
  JSON or YAML buffer — `spec.template.containers[2].env[0].name`, sequence
  indices included. It truncates **from the left** (`…env[0].name`) because the
  tail is the interesting part, and hides itself at the document root and in
  every buffer without a path scanner.
- Three commands copy the *full* path: `editor.copyDocPath` (dotted,
  `cmd+alt+shift+c`), `editor.copyDocPathJQ` (`.spec.containers[2].name`) and
  `editor.copyDocPathYQ` (yq v4's spelling for keys that need quotes).
- The derivation (`internal/docpath`) is structural, not a parse: a container
  stack for JSON (strings and JSONC comments skipped), indentation columns for
  YAML (block scalars, `#` comments and `---` boundaries honored, flow values
  handed to the JSON scanner). No second parser, no document loaded.
- A scan only ever reads up to the caret, so a buffer broken *below* it still
  yields the nearest enclosing node. Anchors and aliases are reported as
  written — `<<: *base` is the `<<` key, never the resolved target (#1629).
- Cached per document version and caret position, and off in large-file mode
  like every other whole-buffer analysis. See
  [/architecture/editor.md](/architecture/editor.md) and
  [/architecture/status-line.md](/architecture/status-line.md).

## 2026-08-07 (editor: increment/decrement and value toggling, #1658)

- `Ctrl-a` / `Ctrl-x` in normal mode raise or lower the number under the
  cursor (or the first one to its right on the line); the count is the step,
  so `5<C-a>` adds five.
- Literals keep their shape: a leading `-` is the sign, leading-zero width
  survives (`007` → `008`), and hex keeps prefix, digit width and letter case
  (`0x1f` → `0x20`, `0X00ff` → `0X0100`). Decimal arithmetic saturates at the
  int64 bounds; hex wraps in 64 bits, like vim.
- `g!` toggles the value under (or after) the cursor between the members of a
  known pair — `true`/`false`, `on`/`off`, `yes`/`no`, `enabled`/`disabled`,
  `==`/`!=`, `&&`/`||`, `<`/`>`. Whole tokens only (`<=` never toggles as
  `<`), matched case-insensitively, and the replacement copies the original's
  capitalization (`True` → `False`).
- `editor.toggle_pairs` adds `a=b` entries, matched before the built-ins so a
  member can be redefined.
- All three fan out per caret as one undo unit and record a `.`-dot. They are
  also `editor.increment` / `editor.decrement` / `editor.toggleValue`, so the
  palette reaches them and the keymap layer can rebind them where a
  multiplexer owns `Ctrl-a`. Lives in `internal/editor/increment.go`. See
  [/architecture/editor.md](/architecture/editor.md).

## 2026-08-07 (editor: ex `:sort` command family, #1657)

- `:[range]sort[!] [flags]` (short `:sor`) reorders lines without leaving the
  editor. Its default range is the **whole buffer**, not the current line —
  `:'<,'>sort` sorts a selection, `:2,10sort` a span, and `:` from visual mode
  still pre-fills the bounds.
- Flags combine: `u` drops duplicate lines, `n` sorts on the first decimal
  number in each line (a touching `-` is part of it; unnumbered lines come
  first), `i` compares case-insensitively, `!` inverts the order. An unknown
  flag letter is an error that leaves the buffer untouched.
- The sort is stable — `!` inverts the comparator instead of reversing the
  result, so equal lines keep their relative order either way. `u` compares
  whole lines (case-folded under `i`), not the sort key.
- The range is rewritten as one `buffer.Edit`, so a single `u` reverts the
  whole sort; an already-sorted range records nothing and reports *already
  sorted*. Lives in `internal/editor/sort.go`. See
  [/architecture/editor.md](/architecture/editor.md).

## 2026-08-07 (editor: chmod octal permission hints, #1656)

- An octal file mode now draws with its symbolic form appended —
  `chmod 755 build.sh` renders as `755  rwxr-xr-x`, `0644` as `rw-r--r--` —
  with the special bits decoded the way `ls` prints them: `4755` →
  `rwsr-xr-x`, `2775` → `rwxrwsr-x`, `1777` → `rwxrwxrwt`, and the capital
  `S`/`T` forms when the special bit has no execute bit under it.
- The literal carries no syntax of its own, so the *carrying context* decides:
  `chmod`/`install -m`/`mkdir -m` in shell, `COPY/ADD --chmod=` and `RUN`
  lines in Dockerfiles, the mode arguments of `os.Chmod`/`os.WriteFile`/
  `os.FileMode` in Go and `os.chmod`/`os.makedirs` in Python, and
  `mode:`/`defaultMode:` keys in YAML and Ansible. A bare port or year is
  never decoded.
- Code and YAML additionally require the octal spelling (`0644`, `0o644`): a
  YAML `mode: 644` is the decimal 644 Ansible really reads, so it keeps the
  #1627 radix warning (`= 01204`) instead — `yamlSpans` feeds the permission
  spans to `numhint.SpansExcept` so the two families never overlap.
- Rides the #1585 stand-in channel and reveals positionally (#1594). Decoding
  and context scan live in `internal/permhint`; gated by
  `editor.permission_hints` / `view.togglePermissionHints`, default on. First
  `Spans` hook for the `shell`, `dockerfile` and `ansible` languages. See
  [/architecture/editor.md](/architecture/editor.md).

## 2026-08-07 (editor: clickable URLs via OSC 8 hyperlinks, #1655)

- URLs in buffers are real terminal links: every display cell of a bare
  `http(s)://` URL — and of a Markdown link label, which carries its link
  target — wraps in its own zero-width OSC 8 open/close pair, so supporting
  terminals (Ghostty, iTerm2, kitty, WezTerm) open the browser on
  cmd/ctrl+click while others ignore the sequences by spec. Width math,
  cursor positioning and click mapping are untouched (the #1469 invariant),
  and the per-cell pairs can never be stranded open by a row splice. New
  `editor.hyperlinks` switch (default on) disables emission.

## 2026-08-07 (editor: invisible & deceptive Unicode, #1654)

- The editor never renders a character as nothing anymore: zero-width
  space/joiner/non-joiner, word joiner and an interior BOM draw as `∅`, NBSP
  as `⍽` (distinct from the `·` space glyph), the soft hyphen as `-`, and
  every bidi control (`U+202A–E`, `U+2066–69`, LRM/RLM, ALM — the
  Trojan-Source class) as `◊` — one-cell placeholders in the warning colour,
  hooked in next to the #1469 control glyphs.
- The same characters produce diagnostics-style notes (gutter tint, underline,
  hover/caret popups), plus a confusable check: an identifier mixing ASCII
  letters with Cyrillic/Greek look-alikes (`pаssword`) warns, while pure
  non-Latin text, `Δt` or `straße` never light up. The scan is
  language-agnostic — the highlight pass now runs for every buffer — and free
  on pure-ASCII lines.
- Lint notes now travel the whole diagnostics flow: next/prev-diagnostic
  walks them, the status-line counts them, and a new independent Problems
  store channel lists them in the pane. Scan and placeholder table live in
  `internal/unihint`; the look-alike table is shared with #1653. See
  [/architecture/editor.md](/architecture/editor.md),
  [/architecture/problems.md](/architecture/problems.md).

## 2026-08-07 (editor: network-literal hints, #1653)

- A CIDR prefix now draws with its address range and size appended —
  `10.0.0.0/8  10.0.0.0–10.255.255.255, 16,777,214 hosts` — and a punycode
  hostname with its decoded Unicode form (`xn--mnchen-3ya.de  münchen.de`).
- Counts follow the protocol: IPv4 subtracts network and broadcast except on
  `/31` (2 hosts, RFC 3021) and `/32`; IPv6 counts addresses and leaves the
  huge spans as a power of two (`::/0` → `2^128 addresses`).
- A decoded name carrying the homograph shape — scripts mixed inside one
  label, or a whole label of Latin look-alikes (`аррӏе.com`) — draws in the
  warning colour, since that is a phishing tell and not a readability win.
- Both ride the #1585 stand-in channel and reveal positionally (#1594).
  Whole lines are scanned in YAML/JSON/TOML/ini/dotenv/`.http`, string
  literals only in Go, JS/TS and Python. Decoding lives in `internal/nethint`;
  gated by `editor.cidr_hints` / `editor.idn_hints` and the matching
  `view.toggle*` actions, both default on. See
  [/architecture/editor.md](/architecture/editor.md).

## 2026-08-07 (editor: PEM/certificate inline summary, #1652)

- A PEM block now collapses onto its `-----BEGIN …-----` line with a decoded
  one-line summary appended — for a certificate: subject CN, validity window,
  expiry verdict, issuer CN, key type and SANs — instead of showing a wall of
  base64. Expired or not-yet-valid certificates draw in the error colour, ones
  expiring within 30 days in the warning colour.
- Private keys are never parsed: they get a type label (`private key (rsa)`)
  and nothing else. Unknown and unparseable blocks fall back to a plain label.
- The mechanic is the log repeat run's (#1650), not the conceal layer's — the
  body lines ride the fold machinery, and the block reveals positionally like
  every stand-in family (#1594): the cursor inside it renders all of it raw.
- Decoding lives in `internal/peminfo`; the editor half is
  `internal/editor/pemsummary.go`. Gated by `editor.pem_summary` /
  `view.togglePemSummary`, default on. See
  [/architecture/editor.md](/architecture/editor.md).

## 2026-08-07 (docs: generated feature screenshots, #1634)

- The user docs carry feature screenshots (`docs/screenshots/features`, linked
  from `userdocs/`): syntax highlighting in three languages, the Markdown / CSV
  / log rendering layers each against their raw counterpart plus a
  caret-reveal shot, the `.http` file, the diff viewer and the VCS gutter. All
  of them use `monokai-pro`.
- They are generated, not captured: `make shots` (`cmd/shotgen`) unpacks an
  embedded demo project into a temp tree, drives the real root model headlessly
  — window size, `window.hideAllTools`, an open, then palette commands and keys
  — and paints `Model.View().Content` into a PNG. Background messages reach the
  model through `Model.SetSender`, which is what makes the git-status snapshot
  land in the VCS shots.
- `internal/shotpng` is the painter: the frame replays through the VT emulator
  (`charmbracelet/x/vt`) into a cell grid, each cell drawn with its colours and
  attributes. Box-drawing and block characters are painted as rectangles
  instead of font glyphs, so borders have no seams; fonts resolve to Menlo /
  DejaVu Sans Mono with the embedded Go Mono as the always-available fallback.
- New user-docs page: **How files are rendered**
  (`userdocs/guides/file-rendering.md`) — highlighting, the rendering layers,
  and the positional conceal reveal, which had no narrative page before.
- Concept doc: [Documentation Screenshots](/architecture/screenshots.md).

## 2026-08-07 (editor: YAML anchor/alias pairing, preview & navigation, #1629)

- `internal/yamlanchor`: a pure-Go line scanner pairs `&name` anchors with
  their `*name` aliases — node-start positions only, comments/quoted
  scalars/block-scalar bodies skipped, `---` document boundaries respected,
  aliases resolved to the nearest preceding same-name anchor.
- Paired coloring: every mark of a name shares one hash-keyed rainbow slot
  (the #1626 content-hash trick on the #1589 palette); an alias without an
  anchor renders as an error — underlined, `theme.captures.anchor.unresolved`
  overrides.
- The local navigation seam (#922) widened into a family: providers now get
  the full document lines, and hover + references registries sit beside the
  definition one. YAML registers all three: goto-definition jumps alias →
  anchor, find-usages lists a name's marks (palette list and Usages pane),
  hover on an alias previews the resolved value as a highlighted yaml fence
  with `<<:` merge keys spliced recursively.

## 2026-08-07 (editor: rainbow brackets & depth-coloured indent guides, #1628)

- Bracket pairing moved out of the Tree-sitter walk into `internal/bracket`, a
  pure-Go leaf package: a stack-based scan returns every bracket with the
  nesting depth it sits at (both halves of a pair share one depth) and whether
  it found a partner at all. A closer matches the innermost open bracket of
  its own kind, so one typo marks one bracket instead of cascading.
- Unmatched brackets — a stray closer, an opener never closed — render in the
  theme's error colour, underlined (`bracket.unmatched`, overridable via
  `theme.captures.bracket.unmatched`).
- Strings and comments are skipped by the grammar's own `string`/`comment`
  captures where a parse exists, and by line-local quote/comment heuristics
  where it does not. Prose languages (markdown, text, log, csv/tsv) are not
  scanned at all: `(see below` in a paragraph is not a mistake.
- Indent guides are coloured by depth from the same rainbow palette, mixed
  toward the flat guide colour so the indentation stays chrome — the payoff is
  YAML and Python, where nesting has no brackets to colour. New toggle
  `editor.rainbow_indent_guides` (default on), independent of
  `editor.rainbow_brackets` and of `editor.indent_guides` itself.
- Wiki: [Highlighting](/architecture/highlighting.md) gained the pairing,
  prose-exclusion and indent-guide sections; [Editor](/architecture/editor.md)
  notes the coloured guides in its view options.

## 2026-08-07 (editor: number-readability hints, #1627)

- `internal/numhint` turns numeric literals in config files into what they
  mean: byte sizes (`10485760` → `10 MiB`, binary units), durations
  (`86400000` → `24h`, at most two components, days only past 48h), digit
  grouping (`1000000` → `1_000_000`, from five digits up) and radix readings
  (`0x1F4  = 500`, `mode: 420  = 0o644`).
- Context comes from key names — `*size*`/`*bytes*`/`*memory*` for byte
  counts, the timeout family (millis) and the TTL family (seconds) for
  durations, with `*_ms`/`*_seconds`-style last words pinning the unit, and
  `*mode*`/`*mask*` for radix — plus shape triggers (a multiple of 1024, a
  `0x` literal, a five-digit run). Weak quantifiers (`limit`, `max`) name no
  family alone: a rate limit is not a byte count.
- Four stand-in families on the #1585 channel, each with its own toggle
  (`editor.byte_size_hints`, `editor.duration_hints`, `editor.digit_grouping`,
  `editor.radix_hints` and their `view.toggle*` actions), revealed under the
  caret like every other family (#1594). Wired into JSON/ndjson, YAML, TOML,
  ini and dotenv; JSON filters through `SpansExcept` so the epoch decoding
  (#1618) keeps the digits it already claimed.

## 2026-08-06 (editor: color-hash UUIDs and hashes, #1626)

- `internal/idcolor` detects UUIDs (`8-4-4-4-12`) and standalone hex runs of
  at least `editor.id_color_min_length` characters (default 7, the
  abbreviated git SHA; all-digit runs and `#rrggbb` color literals excluded)
  and hashes each identifier (FNV-1a, case-folded) onto a slot of the shared
  rainbow palette — every occurrence of one identifier gets one color, across
  lines, panes and files.
- Active in the formats where an opaque id is the point: log buffers (#1621),
  JSON/NDJSON, `.http` files, and `.http` response bodies (`internal/httppane`
  reads the package globals `internal/app` pushes from the config). The color
  replaces the syntax foreground only, so backgrounds and the color swatch of
  #790 are untouched.
- Toggles: `editor.id_colors` (default on) plus the per-view
  `view.toggleIdentifierColors` action, sticky like the #64 view toggles.

## 2026-08-06 (editor: human-readable cron hints, #1624)

- `internal/cronhint` parses standard cron (five fields plus an optional
  leading seconds field: ranges, steps, lists, `MON`/`JAN` names, `?`, the
  `@daily`-style shorthands) and renders it compactly in English — `every
  5 min`, `Mon-Fri 04:30`, `hourly :05,:35`. Extensions needing a calendar
  (`L`, `W`, `#`) and Quartz' year field are rejected rather than guessed.
- The hint rides the #1585 stand-in mechanic on its own conceal channel
  (`cron.hint`): the span covers the expression and repeats it with the hint
  appended, so the reading sits *after* the expression and disappears under
  the caret (#1594). Gated by `editor.cron_hints` /
  `view.toggleCronHints`.
- Contexts: the new grammar-free `crontab` language (`*.cron`, `*.crontab`,
  `crontab`), CI YAML `cron:`/`schedule:` values, and quoted expressions in
  YAML, JSON and TOML — the quoted path guarded by a cron-shape test so a
  quoted number list never turns into a schedule.

## 2026-08-06 (lang: dotenv secret masking and duplicate keys, #1623)

- `internal/secret` decides from a key's name alone whether its value is a
  credential (`*_SECRET`, `PASSWORD`, `*_TOKEN`, `*_KEY`, `CREDENTIALS`, …,
  with `PUBLIC_KEY`/`API_KEY_ID`/`TOKEN_URL` cleared again). The dotenv
  producer emits such values as fixed-width `••••` stand-ins (#1585) on their
  own conceal channel, revealed positionally under the caret (#1594) and
  gated by `editor.secret_masking` / `view.toggleSecretMasking`.
- New registry seam `lang.Language.Lint func(lines []string) []lang.Note`:
  Go-computed diagnostics for languages without a server, produced by the
  highlight pass and merged into the gutter tint and inline underline through
  a channel a `DiagnosticsMsg` cannot clobber. First user: dotenv flags every
  duplicate key except the last, which is the assignment loaders keep.

## 2026-08-06 (editor: JWT detection and decoding, #1619)

- `internal/jwt` detects JSON Web Tokens structurally — three non-empty
  base64url segments whose first two decode to JSON objects — and emits one
  `jwt.signature` span per token so the meaningless signature renders faint.
  The `.http` producer scans every line, the new dotenv language
  (`plugins/languages/env`: `.env`, `.env.local`, `*.env`) scans its values.
- `editor.decodeJWT` ("Decode JWT at Caret") opens the token's header and
  payload as pretty-printed JSON in the hover popup, anchored at the token,
  with the registered time claims (`exp`/`iat`/`nbf`/`auth_time`/`updated_at`)
  annotated as UTC dates through `epochtime.Decode` (#1618) — a soft
  dependency: implausible values stay raw numbers.

## 2026-08-06 (editor: inline decoding of epoch timestamps, #1618)

- `internal/epochtime` detects Unix epoch numbers (seconds and milliseconds,
  2001–2100) and emits conceal-with-stand-in spans (#1585) rendering the UTC
  form; the raw digits reappear under the caret (#1594). Two heuristics keep
  ordinary numbers raw: the plausible range, plus a context — JSON value
  positions for the JSON languages and `.http` request bodies, a delimiter
  rule for log lines. Its own conceal channel (`stamps`) means the toggle
  (`editor.timestamp_decoding` / `view.toggleTimestampDecoding`) is
  independent of the markdown and log rendering layers.

## 2026-08-06 (deploy: Ike.app runs natively on Apple Silicon, #1614)

- `deploy/Info.plist` declares `LSArchitecturePriority` (arm64, x86_64):
  LaunchServices runs a bundle whose executable is a shell script under
  Rosetta, and the translated preference is inherited down to the login
  shell `ike-gui` re-execs through (#1579/#1581). An arch-conditional
  profile — e.g. Homebrew's `arm64` branch — then took the Intel path, so
  `/opt/homebrew/bin` never reached `PATH` and tool panes failed with
  `exec: "lazygit": executable file not found in $PATH`. With the native
  priority the profile sees the real architecture; Intel Macs fall
  through to x86_64.

## 2026-08-06 (editor: reformat keybind formats the selection when one exists, #1603)

- `lsp.format` is context-sensitive: an active visual selection reformats
  only the selected range (JetBrains Reformat Code semantics); with a
  selection but no range-capable provider it widens to the whole file with
  a notice. `lsp.formatRange` keeps its strict contract.

## 2026-08-06 (format: built-in .http reformatter folds query params, #1602)

- The HTTP plugin ships a built-in formatter (`plugins/languages/http/format.go`):
  query parameters fold onto the indented `?`/`&` continuation lines the
  parser accepts (#1269), one per line, values byte-identical (no
  re-encoding, see #1601); headers/blank-before-body normalize
  conservatively; a reparse guard aborts any semantic change.

## 2026-08-06 (deploy: desktop launch uses interactive login shell, #1581)

- `ike-gui`'s re-exec (#1579) now runs `$SHELL -i -l -c` instead of plain
  `-l`: rc files (`.zshrc`/`.bashrc`) commonly hold PATH entries like
  `~/.local/bin`, which a non-interactive login shell skips — so tools
  installed there (e.g. `claude`) were still not found in desktop
  launches while working from a terminal.

## 2026-08-06 (deploy: desktop launch loads login-shell env, #1579)

- `ike-gui` re-execs itself through the user's login shell (`$SHELL -l`,
  resolved via dscl/getent when unset, guarded by `IKE_GUI_LOGIN`) before
  launching Ghostty. A Dock/desktop launch previously carried launchd's
  minimal environment (`PATH=/usr/bin:/bin:…`), so LSP servers, formatters
  and toolchains from Homebrew or `~/.local/bin` were not found — the
  user's setup appeared ignored. `open -na Ghostty.app` forwards the
  caller's environment, so the loaded profile reaches ike on macOS too.

## 2026-08-05 (deploy: desktop launcher, #1567)

- IKE installs as a desktop application on macOS and Linux
  (`make install-desktop` → `scripts/install-desktop.sh`): a dedicated
  Ghostty config (`deploy/ghostty/ike.conf`, loaded exclusively via
  `--config-default-files=false`) plus the `ike-gui` launcher, wrapped by
  `Ike.app` (macOS) or `ike.desktop` + hicolor icons (Linux). Icon artefacts
  are generated from `deploy/icon/gen` (pure Go source render; `make icons`
  rebuilds the .icns/PNG sets on macOS). Docs: README "Desktop launcher",
  userdocs getting-started/desktop-launcher.md.

## 2026-08-05 (editor: app-wide shared register store, #1540)

- One `register.Store` for every editor across panes, tabs and workspaces
  (vim's global-register semantics): yanks/deletes, named registers, the
  numbered ring and the paste-from-history picker (cmd+shift+V) now share one
  pool — copy in workspace A, paste from history in workspace B. The
  workspace manager owns the store (a project switch rebuilds the model but
  carries the manager); it threads through `pane.NewRegistry` into every
  editor via `editor.SetRegisters`, with `editor.New` keeping a private store
  for standalone/test use.

## 2026-08-05 (terminal: configurable scrollback bound, #1545)

- New `terminal.scrollback_lines` setting (default 10000, min 100): bounds
  each terminal session's scrollback — the dominant per-pane memory cost,
  multiplied by parked workspaces. Held as a process-wide default in the
  terminal package (`SetDefaultScrollbackLines`, set at startup and on config
  reload) so every session creation path applies it; a reload also re-bounds
  the active workspace's live sessions (lowering trims forward, raising
  cannot restore trimmed history).

## 2026-08-03 (preview: images via Kitty graphics protocol, #1479)

- New `internal/imgview` pane (`KindImage`, keys `image`/`image:N`): PNG,
  JPEG, GIF (first frame) and WebP render through the Kitty graphics
  protocol's Unicode-placeholder flavour — transmit once as a virtual
  placement, draw plain placeholder cells, terminal composites; repaint-safe
  by construction. Lazy `a=q` capability probe (response via
  `uv.KittyGraphicsEvent`), metadata-card fallback (name, format,
  dimensions, size) on unsupported terminals and decode errors. The `images`
  FileHandler routes explorer/editor opens to the preview; an end-of-Update
  reconcile (`imageSyncCmd`) transmits on show/resize and deletes on
  close/workspace-switch, so no ghost graphics. Layout persistence via
  `{Kind: "image", Path}`. Added `golang.org/x/image` (WebP decode). New
  Image Preview concept doc.

## 2026-08-03 (theme: auto light/dark sync with terminal background, #1480)

- `[theme].auto` (off by default) syncs the scheme with the terminal
  background: OSC 11 via `tea.RequestBackgroundColor` in Init, the reply
  classified by `theme.IsDarkColor` (relative luminance) picks
  `[theme].light` / `[theme].dark`; no reply means `[theme].name` keeps
  applying (no timeout, no hang). `themes.syncTerminal` re-queries on
  demand; explicit theme selection also writes `theme.auto = false`.
  Appearance page gained the switch plus light/dark enums partitioned by the
  themes' `Dark` flag. Config `theme.dark` repurposed from a dead bool to
  the pair's dark theme name.

## 2026-08-03 (editor: surround operations, #1475)

- vim-surround style pair editing (`editor/surround.go`): `ys{motion}{pair}` /
  `yss` / visual `S{pair}` add a pair, `cs{old}{new}` swaps the nearest
  enclosing pair, `ds{old}` removes it — brackets, quotes and backtick, with
  the opening-member-pads / closing-member-doesn't convention on add and the
  inverse strip on delete/change. Reuses `textobject.Pair`/`Quote` for
  delimiter location; one undo unit, dot-repeat re-resolves at the cursor,
  multi-caret fan-out via `fanMutate`. New `awaitSurr*` secondary-key states.

## 2026-08-03 (editor: control bytes & SGR interpretation, #1469)

- Raw C0 controls/DEL in a buffer render as one-cell Control Pictures glyphs
  (`␛` for ESC) instead of leaking to the terminal — one rune stays one
  display cell, so click mapping, caret and selection stay aligned. SGR
  sequences are interpreted per line (`editor/ansiescape.go`): governed text
  takes the file's own colour/attributes through the normal styling pipeline,
  the sequence runes render dim, overlays still win. Copy keeps the bytes.

## 2026-08-02 (theme: inheritance analysis & navigation, 0480 #1448)

- Work stream 0480 (#1448–#1455): IntelliJ-style inheritance features on two
  new LSP request families (`textDocument/implementation`, type hierarchy
  prepare/supertypes/subtypes), wired protocol → client → manager → bridge
  after the call-hierarchy template. `lsp.goToSuper` (`cmd+u`),
  `lsp.implementations` (`cmd+alt+b`) — 1 target jumps, N open the locked
  refs picker; `lsp.typeHierarchy` (`ctrl+h`) opens the new
  `internal/typehier` overlay (callhier pattern, tab = supertypes/subtypes).
  Passive gutter marks: debounced, capped `documentSymbol` + per-symbol
  `implementation` batch in the manager yields ↑/↓ arrows in the sign column
  (below test marks, above diagnostic/git tints), toggle
  `editor.marks.inheritance` gates render + traffic. New `SymKind*`
  SymbolKind constants (distinct from CompletionItemKind). Navigate menu +
  editor context-menu entries; docgen + status-matrix ledger regenerated.

## 2026-08-02 (theme: show history for selection, #1430)

- Show History for Selection (#1430): `vcs.historyForSelection` runs
  `git log -L <start>,<end>:<file>` over the editor's visual selection
  (caret line fallback) via the new `internal/vcs/rangelog.go` async
  command (200-commit cap, truncation hinted). Commits that touched
  exactly that range show in the modal shell — enter expands one commit
  to the range patch, esc steps back. New `editor.SelectionLines()`
  accessor (1-based, normalized). Picker re-binds its shell body on
  every selection move — the root model is a value model, so a closure
  bound once at open time renders stale state (the local-history picker
  shares that latent issue). Palette + editor context menu; no chord.

## 2026-08-02 (theme: open in browser, #1429)

- "Open in Browser" (#1429): new `file.openInBrowser` command opens the
  focused file — explorer selection when the tree is focused, else the
  focused editor's file — in the platform default browser (`open` /
  `start` / `xdg-open`). Gated to browser-viewable types (markup, images,
  PDF; markdown stays with `markdown.preview`); non-viewable files toast
  instead of silently doing nothing. Palette entry plus editor and
  explorer context-menu items; no default chord (#711 budget).

## 2026-07-30 (theme: per-language formatter defaults, #1405)

- Per-language external formatter defaults (Roadmap 0470, #1405): Python
  `ruff format` → `black` (project venv copies preferred), Markdown
  `prettier` → `mdformat` (no prose reflow), Shell `shfmt` (indent flag
  from editorconfig, `-ln` dialect from the shebang, explicit hint replaces
  bash-language-server's silent path), Ansible `prettier --parser yaml` →
  `yamlfmt`. New `format.RegisterExternalDefaults` fallback chains + an
  `External.Adjust` hook for flags placeholders cannot express. Servers log
  their observed formatting capabilities on startup. Reformat coverage
  matrix added to [Language Registry](/architecture/languages.md); userdocs
  list what works out of the box.

## 2026-07-30 (theme: built-in XML formatter, #1404)

- Built-in XML formatter (Roadmap 0470, #1404): pure-Go tokenizer + tree +
  pretty-printer in the XML plugin, Tree-sitter parse as second validity
  gate (unparseable documents untouched). Indent per editorconfig;
  attributes wrap aligned under the first beyond max_line_length;
  declaration/DOCTYPE/PIs/comments/CDATA/entities/xml:space=preserve/mixed
  content verbatim; text-only elements one line; self-closing kept; range
  formatting per element subtree; idempotent (golden-tested incl. SVG and
  plist). Registered at the built-in tier; [format.xml] builtin = false
  disables it. XML is no longer highlighting-only.

## 2026-07-30 (theme: built-in SQL formatter, #1403)

- Built-in SQL formatter (Roadmap 0470, #1403): pure-Go lexer +
  clause-layout printer in the SQL plugin, Tree-sitter parse as validity
  gate (malformed SQL untouched). Clause-per-line layout, broken select/SET/
  DDL lists, AND/OR chains indented, subquery blocks, configurable keyword
  casing (`[format.sql] keywords`), comments preserved, one blank line
  between statements, idempotent (golden-tested), statement-wise range
  formatting. Registered above the LSP tier so it beats sqls by default;
  `[format.sql] builtin = false` restores sqls. New `builtin` flag wired
  through plugins/format.

## 2026-07-30 (theme: external formatter commands, #1402)

- External-command formatter provider (Roadmap 0470, #1402):
  `format.External` runs ecosystem CLIs (ruff/prettier/shfmt) with
  placeholder args, buffer on stdin / text on stdout (temp-file mode for
  stdin-less tools), project-root cwd, timeout + 10 MB guard, truncated
  stderr on failure — the buffer is never touched on error and the tool
  never writes the file. `[format.<languageID>]` config table (user <
  project, like `[lsp.servers.*]`) overrides plugin defaults; `enabled =
  false` disables external formatting per language; `range_args` opts into
  reformat-selection. Missing binaries raise a one-time install hint (#1067
  pattern). New read-only **Formatters** settings page shows the effective
  command, layer and binary state. Docs: [Formatter
  Registry](/architecture/format.md) extended; userdocs settings reference +
  code-intelligence guide.

## 2026-07-30 (theme: formatter registry, #1401)

- Formatter registry (Roadmap 0470, #1401): reformat is no longer LSP-only.
  New leaf package `internal/format` holds providers in a fixed resolution
  chain (config override → external command → LSP → built-in); the
  `lsp.format` / `lsp.formatRange` commands (ids kept for keymaps) moved to
  the new `plugins/format` plugin with neutral titles ("Reformat File" /
  "Reformat Selection"), resolved and run by `internal/app/reformat.go` with
  the buffer's effective editorconfig-layered options
  (`editor.FormatOptions`, incl. new `max_line_length`). The LSP provider
  (`plugins/lsp/provider.go`) is one chain entry; the save chain's format
  step routes through the registry (`ilsp.SaveChainRequest` now carries the
  buffer snapshot + options). Status toast names the source
  (`reformat: gopls`); reformat-selection falls back to the next
  range-capable provider or says only whole-file is available; no provider is
  a clear "no formatter" toast. New concept doc
  [Formatter Registry](/architecture/format.md); LSP/editor/editorconfig docs
  updated. Follow-ups: #1402 external commands, #1403 SQL built-in, #1404 XML
  built-in, #1405 per-language defaults.

## 2026-07-30 (theme: popup terminal, #1398)

- Popup terminal (#1398): quake-style floating terminal overlay with tabs,
  toggled by `terminal.popup` (`cmd+alt+t`; `terminal.new` moved to
  `cmd+alt+shift+t`). A detached `pane.Instance` tab host outside every
  registry and the split tree — toggling never touches the pane layout,
  sessions keep running while hidden, the resize delta persists via
  `ui.WinSizes` (`popupterm`). Deliberately not a `ui.Floating` (raw PTY key
  pass-through). New section in [Integrated Terminal](/architecture/terminal.md);
  boundary note in [Floating Shell](/architecture/floating-shell.md). Minor
  version bump to 0.2.0.

## 2026-07-30 (theme: settings-panel freeze under mouse motion, #1396)

- Settings (#1396): the panel — worst on the Keymap page — froze under mouse
  motion and drained buffered keys one by one. Three compounding costs: the
  app measured `settings.View()` (a full panel render) up to twice per mouse
  event just for geometry, now `Model.Size()`; the keymap page rebuilt
  `keymap.BuildTable` + filter + sort on every `rows()` call (2-3× per key,
  more per frame via the detail column), now memoized on config pointer /
  filter / fold generation; and the list styled all rows before windowing,
  now `pinFooterLazy` styles only the visible window. Settings-UI doc's
  "Mouse parity" section updated.

## 2026-07-29 (theme: process docs moved from CLAUDE.md)

- New `/process/` section: [Change Workflow](/process/change-workflow.md)
  (issue first, issue branch, patch-version bump, PR, merge, branch cleanup),
  [GitHub Issue Workflow](/process/issues.md) (epics, labels, conventions,
  duplicate check) and [Wiki Format (OKF)](/process/wiki-format.md) — content
  moved out of CLAUDE.md, which now keeps only a compact summary and links here.

## 2026-07-29 (theme: live cwd without OSC 7, #1383)

- Terminal (#1383): `Session.Cwd()` no longer sticks to the start directory
  when the shell lacks OSC 7 prompt integration — it queries the kernel for
  the child's actual cwd (darwin `proc_pidinfo` raw syscall, linux
  `/proc/<pid>/cwd`), so the completion popup, pane title and status line
  follow a plain `cd`. Precedence: OSC 7 > kernel query > start dir.
  Terminal doc's "Live cwd" section updated.

## 2026-07-29 (theme: TODO-index rows never wrap, #1379)

- TODO index (#1379): every overlay row hard-clips to one terminal line. Root
  cause was a 2-cell-too-wide text budget — the box style's `Width(boxW-2)`
  includes border and padding, so the text area is `boxW-6`, not the `boxW-4`
  the rows rendered at — plus lipgloss `MaxWidth` (which wraps, #971) on the
  filter/status rows, now `ansi.Truncate`-based `clipRow`. Rendering section
  added to the TODO-index doc.

## 2026-07-29 (theme: paste into editor-internal inputs, #1380)

- Paste routing (#1380): with the in-editor search (`/`, cmd+f), the `:` ex
  line or the find/replace panel open, a bracketed paste (and cmd+v) now lands
  in that input instead of the buffer — `pasteIntoPrompt`
  (`internal/editor/cmdline_paste.go`), same flattening as overlay inputs
  (#1273); the `:s///c` confirm swallows pastes. Editor doc updated.

## 2026-07-29 (theme: debug.stop freeze — non-blocking teardown, #1375)

- Fixed the whole-IDE freeze on `debug.stop` during listen-mode web-request
  debugging (#1375): `Session.FinishPipe` (new in #1370) sent its repaint
  message synchronously from the Update goroutine — `Program.Send` blocks on
  the unbuffered message channel whose only receiver was busy executing
  Update; permanent self-deadlock. The send is now asynchronous. Only listen
  mode froze because launch mode replaces the pipe terminal with a
  runInTerminal PTY (`IsPipe()` false → FinishPipe no-ops).
- The bridge's polite `detach`/`stop` DBGp calls in teardown paths (shutdown,
  `dropConn`) are now bounded by `teardownTimeout` (5s): an unresponsive
  engine gets its connection force-closed, releasing all pending calls
  instead of leaking the goroutine with the connection open.

## 2026-07-29 (theme: debug pane pair — real terminal pane for the debuggee)

- The debug panel split into two real panes (#1370): the singleton panel keeps
  frames + variables (two columns, one draggable separator), and the debuggee's
  output moved into a **real terminal pane** opened directly to its right on
  session start — independently resizable, movable and closable through the
  normal windowing system. The panel's Output column, the embedded terminal
  seam (#676) and the raw-key special case (`debugPanelTermCapturing`) are
  gone.
- `internal/terminal` gained **pipe sessions** (`NewPipeSession`/`NewPipe`):
  process-less emulator sessions fed via `FeedBytes`/`FeedText`. DAP `output`
  events render through one, inheriting the pane's reflow, scrollback and
  search; `FinishPipe` shows the `[process exited with code N]` dead view
  while staying feedable for trailing output. A `runInTerminal` debuggee's
  PTY replaces the pipe placeholder in the same pane slot
  (`Instance.ReplaceTerminal`).
- The pair survives session end for review (#689) and is reused by the next
  launch; the layout store records the terminal as `debugTerm` and restore
  prunes its leaf instead of resurrecting a shell. Runs never take the
  debuggee terminal over (`ReusableRunTerminal` exclusion).

## 2026-07-29 (theme: the 16 standard ANSI colors)

- Themes now define the integrated terminal's 16 ANSI colours plus its default
  foreground/background (#1363). `internal/terminal` rewrites the grid's
  indexed SGR colours (30-37, 90-97, `38;5;n`/`48;5;n` below 16, and 39/49) to
  those truecolor values, so shell output no longer mixes the outer terminal's
  palette with IKE's own background.
- A theme that ships no palette derives one from its semantic slots
  (error to red, success to green, ...) with a foreground/background grey ramp,
  so third-party themes are covered too. Every entry is lifted until it clears
  3.0:1 contrast against the terminal background (1.7:1 for the grey slots) -
  including a theme's own values, several upstream palettes being genuinely
  too dim to read.
- Built-in ANSI palettes ship for the themes whose upstream publishes one
  (default, tokyo-night, nord, gruvbox(+light), dracula, solarized-dark(+light),
  catppuccin-mocha(+latte), one-dark, github-dark(+light)).
- `[theme.terminal]` overrides single slots by name (`red`, `bright_blue`,
  `foreground`, `background`); unknown slot names and unparseable colours are
  reported and dropped.

## 2026-07-29 (guard prompts: enter confirms)

- Every modal guard prompt (close #259, quit #287, switch #3, workspace close
  #821, LRU eviction #780, project close #1355) now accepts `enter` as its
  primary answer (#1356); the letter shortcuts and `esc` are unchanged. The
  primary option is "save all, then …" whenever dirty buffers are involved
  and the plain confirm otherwise.
- The option lines render through the shared `guardLine`/`guardCancel`
  helpers (`internal/app/guardprompt.go`), so the primary line advertises the
  alias as `[s/enter]` and every description stays aligned.

## 2026-07-29 (keymap: default chord for Close Project)

- `project.close` now ships on `cmd+shift+w` with `ctrl+shift+w` as the
  delivered secondary (#1358), mirroring the project.switch pattern; palette
  and File menu remain the universal escape.
- The chord joined the #805 terminal allowlist (#1360): it triggers from a
  focused live terminal or tool pane like the other IDE-level entry points.
- See [Project Switching](/architecture/project-switching.md).

## 2026-07-29 (project: Close Project — close the current project, resume the MRU background workspace)

- New `project.close` command ("Close Project", palette + File menu, no
  default chord) (#1355): closes the active workspace — session/layout
  persisted, processes torn down like close-from-list (#820/#825) — and
  seamlessly resumes the most recently used background workspace. With no
  background workspace it quits the IDE through the existing quit guard.
  A busy active workspace prompts first (save all / discard / cancel, #821
  shape). History entry stays.
- See [Project Switching](/architecture/project-switching.md) — "Close
  Project (#1355)".

## 2026-07-28 (editor: unmistakable input mode — caret shape + mode-coloured pane border)

- The mode-coloured caret (#1323) was too quiet on its own. The caret now also
  changes **shape**: insert mode draws a double underline caret in the mode
  colour over a light tint of it, every other mode keeps the solid block
  (`editor.Model.cursorStyle`, `insertCaretTintFrac`).
- The **focused pane's border** takes `editor.ModeColor` while its editor is in
  a non-Normal mode (`app.paneEditorMode`, `renderPane`). Normal mode keeps
  `BorderFocus`, unfocused panes keep `Border`, and the move/tab drag colours
  still win.
- See [Editor](/architecture/editor.md) — "Mode visibility".

## 2026-07-28 (keymap: context-qualified overrides — "keep both, resolve by context")

- `keymap.bindings` keys may now carry a context qualifier — `"editor.ctrl+g"`,
  or the equivalent sub-table `[keymap.bindings.editor]` (#1312). A qualified
  key binds (or unbinds, with `""`) the chord in that pane only; the bare form
  keeps applying wherever the chord is bound. Qualifiers are `global`, `editor`,
  `explorer`, `palette`, `diff`; anything else parses as part of the chord, so
  dotted chords (`cmd+.`) are unaffected. Qualified keys are applied after bare
  ones, so the narrower statement wins deterministically.
- The config package flattens nested slot-map sub-tables into dotted keys per
  layer before merging (`flattenSlotMaps`), so both spellings are one key and
  still merge across layers.
- The keymap page's conflict dialog offers **Keep both, resolve by context**
  (`b`) when the two commands are pane-scoped in different panes, and hides it
  when either is global. The conflict check now also reports a collision in a
  non-overlapping context — a bare rebind would have taken that chord silently.
  `u` unbinds through the qualified key when a chord is shared, `r` resets
  whichever key carries the override.
- The detail column lists both sides of a shared chord with their contexts and
  reports `↔ … resolved by context` (no replacement suggestions) when they
  cannot overlap.

## 2026-07-28 (project: clone a repository into the project directory)

- `project.clone` ("Clone Repository…", File menu + palette, #1349) opens a
  two-field dialog (URL, directory name derived from the URL), clones into
  `[project] directory` via `vcs.CloneCmd` (async, own 30-minute timeout,
  no terminal prompting, partial clones removed) and opens the checkout
  through the regular switch transaction.

## 2026-07-28 (project: a project directory setting)

- `[project] directory` (default `~/IkeProjects`, #1348) names the default
  parent for projects IKE creates itself. `project.ProjectsDir` resolves it
  (`~` expansion, absolute, cleaned) and `EnsureDirectory` creates it on
  demand; the setting is editable under Settings → Files & Session.

## 2026-07-28 (docs: screenshots for the high-contrast themes)

- The README theme grid and the "twenty-six themes" counts now cover
  `high-contrast-dark` / `high-contrast-light` (#1229), with
  `docs/screenshots/high-contrast-{dark,light}.png` at the 1600x905 of the
  existing set (`userdocs/screenshots` is a symlink to the same directory).

## 2026-07-28 (themes: an accessibility tier)

- `high-contrast-dark` and `high-contrast-light` join the built-ins (#1229) as
  the strict tier above the #1226 readability floor: **WCAG AAA (7:1)** for all
  text on `Background`/`Surface`/`Panel`, syntax captures on `Surface` and file
  colors on `Surface`/`Panel`; **no dim class** — `InlayHint`, `VCSDeleted`,
  `capture:comment`, `capture:punctuation` and `file:lock` clear AAA too, the
  deliberate trade of visual hierarchy for legibility; and overlays within
  **1.2:1** of `Surface`, so the worst pair in either theme is still ~5.8:1.
  With lightness pinned at the extremes hue carries all the meaning, so the
  semantic slots take widely separated hues instead of shades of one accent.
  `TestBuiltinThemeFullContrast` grew a per-theme `contrastTier` (`tierFor`,
  defaulting to `baselineTier`), and a new test asserts both that the two are
  audited at the AAA tier and that no other built-in was tightened with them.
  Updated [Themes](/architecture/themes.md).

## 2026-07-28 (terminal: completion only at the shell prompt)

- The completion popup also fired while a foreground program was reading
  stdin — typing into `python3 -c 'input("Give me something: ")'` opened file
  suggestions over the program's own prompt, and popup-bound keys risked being
  swallowed instead of reaching the program (#1340). `Session.AtPrompt()` now
  answers "is the shell itself the foreground job" from the PTY's foreground
  process group (`TIOCGPGRP`, the same signal `Busy()` uses, so no shell
  prompt integration is required), and `completionActive` requires it. While a
  program runs, typing arms nothing, `ctrl+space` does nothing, an open popup
  is dropped, and tab/enter/arrows/esc pass through raw; completion returns
  with the prompt. An unavailable ioctl fails open.
  Updated [Integrated Terminal](/architecture/terminal.md).

## 2026-07-28 (terminal: accepting a completion ends the interaction)

- Tab-accepting a **directory** in the completion popup re-opened the popup on
  that directory's contents with the first entry preselected, so the next
  enter — the natural key to run the command — inserted that entry instead of
  submitting: `cd an` → tab → enter became `cd ansible/ansible.cfg` (#1335).
  Accepting now always closes the popup and clears the pending auto-suggest
  refresh, so the echo of the pasted remainder cannot reopen it either.
  Continued typing (or `ctrl+space`) still completes inside the accepted
  directory. Updated [Integrated Terminal](/architecture/terminal.md).

## 2026-07-28 (php debug: the listener stops losing requests)

- With "listen for connections" active most requests passed through without
  attaching, some stopped at breakpoints, and nothing said why (#1328). Three
  causes, all fixed: vetting ran **inside** the accept loop, so one slow (or
  silent) connection blocked every request behind it for up to the 30s accept
  timeout — each connection is now vetted in its own goroutine while adoption
  stays single-session under the lock; a session whose php-fpm worker died
  while paused was never noticed, so the bridge answered every later request
  with "one session at a time" — the live connection's `Closed()` is now
  checked and a dead session is reaped like a finished request; and the
  handshake-failure path returned without a word — every rejection now goes
  through `dropConn`, which logs a console line and emits `ike.debugDrop`
  {reason, detail, count} (handshake/busy/filter/ended). The client notifies on
  the first drop per reason and every tenth after that, so an asset burst does
  not spam. Updated [Debugger](/architecture/debugger.md).

## 2026-07-28 (settings: the detail column takes the mouse)

- On the master·detail grid a press in the detail column only moved the focus
  there; every control was keyboard-only (#1325). Editors now opt into two
  seams — `clickEditor` and `wheelEditor` — with editor-body-local coordinates,
  and the panel maps presses through the detail origin plus a `detailBodyTop`
  recorded at render (the documentation head has no fixed height). Bool radios
  and enum options pick in one press, the int stepper's `‹`/`›` step, list rows
  select then edit, path suggestions complete, a chord row opens the capture;
  the wheel walks enum and list rows. The stacked narrow-panel band routes the
  same way. Updated [Settings UI & Menu Bar](/architecture/settings-ui.md).

## 2026-07-28 (editor: clicks repaint again)

- In large projects a click moved the caret logically but not visibly, and a
  drag selection was never highlighted (#1327). Root cause: both render caches
  keyed on `renderEpoch`, which only `Update` bumps — the mouse entry points
  mutate the caret directly, so the line cache (#614) and the pane View cache
  (#615) both stayed valid and the previous frame was reused. Small projects
  masked it: their steadier stream of decoration messages bumped the epoch by
  accident. Both caches now also key on `caretState()` (cursor, mode + visual
  anchor, focus, secondary carets), so every mouse path — including ones added
  later — invalidates exactly when it changes what is drawn. Vertical scrolling
  still hits the cache; the warm-View benchmark is unchanged. Updated
  [Editor](/architecture/editor.md).

## 2026-07-28 (http: the response viewer folds)

- The response viewer rendered a body as flat text (#1330): a large JSON
  response could not be collapsed section by section. Fold ranges now come from
  the body's own language (`highlight.FencedFolds`, new) and are stored in row
  coordinates, with a `visible` display projection the viewport scrolls over —
  so search, selection and copy keep working on real rows. A ▾/▸ marker in the
  gutter toggles on click, `za`/`zc`/`zo` act on the fold at the top of the view
  and `zM`/`zR` on all of them, and a collapsed header carries the editor's
  `⋯ N lines` placeholder. A search hit inside a collapsed fold opens it; a
  selection spanning one copies the hidden content, never the placeholder.
  Updated [HTTP Client](/architecture/http-client.md).

## 2026-07-28 (http: request bodies and whole requests fold)

- Folding stopped at the `.http` host language (#1329): an embedded body was
  parsed for highlighting only, so a large JSON request body could not be
  collapsed. Fragments now also yield fold ranges — each is parsed with its own
  language's fold kinds and shifted into host coordinates (`offsetFolds`) — so a
  JSON body folds like a `.json` buffer's objects do, HTML/XML by their
  elements. The http language additionally declares `FoldNodes: ["section"]`, so
  a whole request folds from its `###` line. Tree-sitter end positions are
  exclusive, so a node ending at column 0 (a delimited `section`) now folds to
  the row above — otherwise a collapsed request swallowed the next request's
  header. Updated [HTTP Client](/architecture/http-client.md) and
  [Syntax Highlighting](/architecture/highlighting.md).

## 2026-07-28 (editor: typing assistance — the space after ":")

- Typing a JSON member used to leave the space after `:` to the user (#1326).
  Languages now declare their typing convention as `lang.SpaceAfter` (JSON and
  ndjson list `':'`), and the editor writes the space as the character is typed
  — per caret, inside the open insert's undo unit, suppressed inside strings
  and line comments and when a separator already follows. The rule is resolved
  per line, so a JSON body embedded in a `.http` request follows JSON's
  conventions (#1304) rather than the host's. Context detection is deliberately
  a text heuristic (quote parity + the line-comment marker): the Tree-sitter
  pass lags a keystroke behind and would be stale exactly when it matters.
  New `editor.typing.space_after_punctuation` switch on a new **Typing
  Assistance** settings page (CORE, behind Editor). Updated
  [Editor](/architecture/editor.md) and [Languages](/architecture/languages.md).

## 2026-07-28 (keymap: the "+" key can be bound)

- `ParseKey` split a step naively on `+`, so `"+"` and `"cmd++"` left an empty
  base and errored (#1331): a plus binding could neither be captured nor read
  back from `[keymap.bindings]`, and the capture dialog swallowed the failure —
  the field just stayed empty. A trailing empty part preceded by another empty
  one is now the literal plus base (a lone `"cmd+"` is still an error), so
  `String()` → `ParseKey` round-trips, and the capture dialog rejects anything
  that fails that round-trip with a visible warning instead of silently.
  Updated [Keybindings & Shortcuts](/architecture/keybindings.md).

## 2026-07-28 (editor: the current mode is now visible)

- The input mode was rendered as plain text in the status line's leftmost
  segment and nowhere else (#1323) — nothing said whether typing inserts text.
  The mode now drives two colour signals: the caret cell is painted in a
  mode colour (`editor.ModeColor`; Accent normal, Success insert, Warning
  visual, Error replace, Info command line) and the status line's mode segment
  renders as a badge in that same colour, spliced over the composed bar so no
  cells shift and click hit-testing stays valid. Foregrounds are picked by
  contrast (`theme.Readable`, new alongside `theme.Luminance` /
  `theme.ContrastRatio`), guarded by a WCAG AA test over every built-in theme.
  Updated [Editor](/architecture/editor.md) and
  [Status Line Segments](/architecture/status-line.md).

## 2026-07-28 (settings: syntax colours page)

- Single highlight colours are editable in the UI (#1238). A new **Syntax
  Colors** page, behind Appearance in the CORE section, lists every capture the
  active theme defines plus the six rainbow slots plus anything a config layer
  names — each with a swatch painted in the colour that actually resolves, the
  token in use and an override marker. `enter` picks from the named colours,
  `e` types a hex or ANSI token (validated before it is written), `r` restores
  the theme's own. Writes go straight to `theme.captures.<name>` at user scope,
  so the open editors recolour immediately. Updated
  [Settings UI & Menu Bar](/architecture/settings-ui.md) and
  [Themes](/architecture/themes.md).

## 2026-07-28 (config: theme.captures was documented but dead)

- Per-capture colour overrides were documented in two wiki pages and read by
  `highlight.NewTheme`, but nothing ever produced them (#1318): `config.Theme`
  had no `Captures` field, so `[theme.captures]` decoded into nothing and even
  warned as an unknown setting, and `Flat()` never carried the keys. The
  override path was live code with a permanently empty input — invisible to the
  tests, which stub the lookup instead of going through the config. The table
  is now a typed slot map, flattened as `theme.captures.<name>`; the write-back
  layer treats a slot-map remainder as one leaf so dotted capture names
  (`constant.builtin`, `rainbow.0`) round-trip; colour tokens are validated and
  a bad one is dropped with a diagnostic instead of silently rendering as the
  terminal default; and `NewThemeKeys` enumerates the config so an override may
  name a capture the theme does not define. Updated
  [Themes](/architecture/themes.md).

## 2026-07-28 (settings: grid rendering polish)

- Three things found by driving the finished panel (#1316): the settings column
  collapsed to its content width on a short filter result and dragged the
  detail column with it (lines are padded to the column width now, not only
  clipped); a filter jump row left the detail column describing whatever page
  the cursor sat on (it now says what enter does with the result); and
  detail-column prose clipped mid-sentence instead of wrapping.

## 2026-07-28 (settings: fold the numbered binding runs)

- Nine lines saying "alt+N goes to tab N" became one (#1300): the keymap page
  folds three or more consecutive bindings whose chord and command differ only
  in a matching trailing number into `alt+1 … alt+9 · Go to tab 1–9 ▸ 9`, with
  `z` expanding it in place and the detail column listing what it stands for.
  Runs are detected on consecutive rows only, fold state is remembered per
  session, and a filter never folds. The column rule is now drawn per line and
  each column clips to its own width, so a long row cannot push the detail
  column sideways. Updated
  [Settings UI & Menu Bar](/architecture/settings-ui.md).

## 2026-07-28 (settings: toolchain page on the grid)

- The toolchain page adopted the raster (#1299). Languages group by state —
  configured · detected-not-configured · not-installed — with the last group
  folded behind a counted caption until `z`; captions are skipped by
  navigation and clicks, and search unfolds what it matches. The candidate
  picker moved out of an inline expansion into the detail column, where it is
  always visible: real finds with provenance and probed version, the one in use
  marked, "enter a path manually…" last, and `scanning…` while discovery is in
  flight. With nothing selected the column explains the category and offers
  `a` — accept every detected interpreter in one batch. The selection now
  follows the language rather than the line number, so configuring one does not
  move the cursor. Updated
  [Settings UI & Menu Bar](/architecture/settings-ui.md).

## 2026-07-28 (settings: keymap page on the grid)

- The keymap page adopted the panel's raster (#1298). The table is `chord ·
  command`; context, layer and provenance moved into a detail column that
  explains the selected command — every binding with its context and
  `@default`/`@user` layer, its conflict state, and free chords pulled from the
  live table when something collides. The capture sub-panel now offers
  *Replace & unbind other* / *Pick a different chord* / *Cancel* instead of a
  bare enter-or-cancel; the wireframes' "keep both, resolve by context" needs
  context-qualified overrides and is tracked as #1312. Updated
  [Settings UI & Menu Bar](/architecture/settings-ui.md).

## 2026-07-28 (settings: search inside the grid)

- `/` used to flatten every match into one list next to a dead rail (#1297).
  The query now takes over the grid: the nav column lists the pages carrying
  matches with their counts, the settings column marks the matched substring,
  and the detail column keeps the editor — so `enter` sets a value straight
  from a result and `tab` leaves for its own page. Rail and match list follow
  each other, and the header reads `⌕ query · n hits · m pages`. Updated
  [Settings UI & Menu Bar](/architecture/settings-ui.md).

## 2026-07-27 (settings: staged apply with a diff)

- Settings edits stopped writing per keystroke (#1296). They collect in a
  staging buffer, the header counts them (`● n changes · ctrl+s apply`), the
  rail marks the pages carrying them and the detail column shows the selected
  row's `old → new`. `ctrl+s` opens a diff panel — `page · key · old → new`,
  target layer in the title — where `u` drops a line, `s` retargets the batch
  and enter writes it through the new `config.ApplyAndReload`, which reloads
  **once** for the whole batch. `esc` with pending edits opens the same review
  instead of discarding silently. Keys whose point is their appearance
  (`theme.name`) preview live via `settings.PreviewMsg` without ever reaching
  disk, and a discard sends the previous value back. Updated
  [Settings UI & Menu Bar](/architecture/settings-ui.md).

## 2026-07-27 (http: body from a file)

- `< ./payload.json` was sent as literal text (#1305). The parser now
  recognises a body that is nothing but a `< path` / `<@ path` directive and
  records it on `Request.BodyFile`, leaving `Body` empty so no consumer can
  send the directive by accident; `Resolve` substitutes placeholders in the
  path too. The dispatcher reads the file against a new `Options.BaseDir` —
  the `.http` file's own directory, wired in by the app — expands `~`,
  substitutes the file's own placeholders for the `<@` form, and fails with a
  message naming the file when it cannot be read. The directive line is not
  Content-Type-highlighted, since it is not payload. Updated
  [HTTP Client](/architecture/http-client.md).

## 2026-07-27 (editor: indent by the embedded region's language)

- Pressing enter after `{` in a `.http` JSON body gave plain copy-indent
  (#1304): `smartIndent` resolved block openers from the *buffer's* language.
  It now asks `indentOpeners` for the line, which consults `lang.RegionAt`
  first — so an embedded region indents by its own language (JSON after
  `{`/`[`, XML after `>`) while the host still governs everything outside it.
  A region language without indent rules falls back to copy-indent instead of
  borrowing the host's. Updated [Editor](/architecture/editor.md) and
  [HTTP Client](/architecture/http-client.md).

## 2026-07-27 (http: bodies highlighted by Content-Type)

- A JSON or XML request body rendered as flat text (#1303). The body's language
  comes from a sibling `Content-Type` header, which a Tree-sitter injection
  query cannot read, so `lang.Language` gained an optional Go-level region
  detector (`Regions`), consulted by `internal/highlight` before
  `injections.scm`; `lang.RegionAt` answers the same question per line. The
  http plugin maps media types — parameters, `x-` prefixes and `+json`-style
  suffixes included — to registered languages and reports each body's range,
  using the new `BodyStart`/`BodyEnd` fields on `httpfile.Request`. Unmapped
  types keep the host's styling. Updated
  [HTTP Client](/architecture/http-client.md) and
  [Languages](/architecture/languages.md).

## 2026-07-27 (http: completion exclusivity)

- Typing `CTy` on a `.http` header line offered `Content-Type` next to
  `contentYOff`, `Capability` and every other identifier the buffer-word and
  project-scan tiers had seen (#1302): `internal/complete` dispatched every
  source for every buffer. Sources can now claim a path through the optional
  `ExclusiveSource` extension, and the engine then dispatches only claiming
  sources; the `.http` source claims `.http`/`.rest`. Nothing claims anything by
  default, so other languages keep the full merged popup. Updated
  [Completion](/architecture/completion.md) and
  [HTTP Client](/architecture/http-client.md).

## 2026-07-27 (settings: three-column master·detail grid)

- The settings panel moved onto the 0460 wireframes' fixed raster (#1295,
  epic #1294): 24ch nav · 44ch settings + value marker · rest detail. The third
  column is never blank — it explains the category while the rail has the
  focus and otherwise carries the selected entry's documentation, its
  provenance and its **editor**. Every entry type now implements one `Editor`
  interface (toggle, stepper, filterable enum list, path candidates, indexed
  multi-value list, chord capture, free text), so a new setting needs a type
  plus documentation and never new UI. Nothing expands inline any more, the
  `@layer` column gave way to the value marker, and the footer shrank to three
  context keys with the full set behind the `?` cheatsheet. Narrow panels keep
  the detail as a band under the list rather than losing it. Updated
  [Settings UI & Menu Bar](/architecture/settings-ui.md).

## 2026-07-27 (http: fuzzy completion, wider header catalog)

- `.http` completion only matched header names by prefix (#1292), so `Cen`
  offered nothing for `Content-Encoding` — and because an empty source batch
  closes the popup, the editor's own fuzzy filter never got the items. The
  source now matches by case-insensitive subsequence (`internal/fuzzy`) for
  header names, values, methods and versions; ranking stays with the editor.
  The header catalog grew to the IANA request set plus the common `X-`/`Sec-`
  headers, with values for the closed-set ones. Updated
  [HTTP Client](/architecture/http-client.md).

## 2026-07-27 (http: pan the response viewer sideways)

- Wide response lines were unreachable (#1290): the viewer clipped every row
  at the pane width with no way to see the rest. It now keeps a horizontal
  offset — `←`/`→` pan by 8 columns, `0`/`^` and `$` jump to the edges, `g`
  resets both axes, and the horizontal wheel / shift+wheel pan like the editor
  (#230). Columns stay absolute, so highlight, search matches and mouse
  selection line up at any offset; history browsing keeps `h`/`l`. Updated
  [HTTP Client](/architecture/http-client.md).

## 2026-07-27 (tests: isolate $HOME, not just $IKE_CONFIG_DIR)

- Six `internal/app` tests failed on any machine with a saved **default
  layout** (#1288). `IKE_CONFIG_DIR` is only the first lookup, and the
  project-switch tests clear it on purpose, so the user-level stores fell back
  to the developer's real `~/.ike`: every test model materialized the personal
  default layout, its terminals made workspaces look busy, and LRU eviction
  prompted instead of evicting. `TestMain` now redirects `$HOME` too, with two
  regression guards. Updated [Configuration System](/architecture/config.md).

## 2026-07-27 (http: in-flight indicator, duplicate guard, cancel)

- A running .http dispatch is visible now (#1272): a statusline segment with
  the elapsed time, a `⟳ running` marker in the response pane header (the
  previous response stays readable below), a duplicate-dispatch guard that
  refuses to fire the same request twice in parallel, and a cancel action
  (`http.cancel` in the palette, `x` in the pane) that aborts through the
  dispatch context and reports as a confirmation rather than an error.
  Updated [HTTP Client](/architecture/http-client.md).

## 2026-07-27 (http: completion in request files)

- `.http` files complete without a language server (#1268): methods and
  `HTTP/1.x` on the request line, header names inserting `: `, and typical
  values for the headers people type by hand — nothing inside bodies,
  comments or folded query lines. Registered through the local completion
  engine's plugin seam. The editor's completion widening now takes the
  furthest matching column, so a hyphenated prefix (`Content-`) is replaced
  whole instead of being duplicated.
  Updated [HTTP Client](/architecture/http-client.md).

## 2026-07-27 (http: findable response history, readable history files)

- Response history stopped hiding (#1267): the footer hint shows from the
  first response, the palette gained `http.responseHistory`, and the help
  overlay lists the response pane's own keys (`help.SetExtra` now takes
  several groups). Text bodies are stored as plain JSON strings under
  `bodyText` instead of base64, so `.ike/http/*.json` is readable and
  diffable; binary bodies keep the base64 field and old files still load.
  Updated [HTTP Client](/architecture/http-client.md).

## 2026-07-27 (http: selecting and copying a response)

- The HTTP response viewer supports text selection and copy (#1266): mouse
  drag with word/line multi-click like the terminal pane, `y` (or
  `ctrl+c`/`cmd+c`) for the selection — falling back to the whole body — and
  `Y` for the headers block, plus the palette commands `http.copyBody` and
  `http.copyHeaders`. The pane emits `httppane.CopyMsg`; the app performs the
  clipboard write through the existing seam.
  Updated [HTTP Client](/architecture/http-client.md).

## 2026-07-27 (http: search inside the response viewer)

- The HTTP response viewer got in-pane search (#1265): `/` opens a prompt,
  `n`/`N` step through the matches with wrap-around, `Esc` clears, and the
  footer shows the position. Matching runs over the whole composed view
  (status line, headers, formatted body) through `internal/editor/search`, so
  smartcase behaves exactly like the editor's `/`.
  Updated [HTTP Client](/architecture/http-client.md).

## 2026-07-27 (http: folded query lines)

- The `.http` parser accepts the JetBrains query-folding form (#1269):
  indented continuation lines starting with `?` or `&` extend the request
  target instead of failing as invalid headers. Whitespace is stripped,
  several params may share a line, valueless flags and values containing `=`
  survive, and placeholders resolve as usual. The grammar already treats the
  folded lines as part of the URL, so highlighting needed no change.
  Updated [HTTP Client](/architecture/http-client.md).

## 2026-07-27 (http: body highlighting is build-dependent, and says so)

- Response bodies render plain whenever the fence tag's grammar is missing
  from the build — a `CGO_ENABLED=0` binary has stub grammars, and a grammar
  plugin not blank-imported in `cmd/ike/main.go` is equally absent (#1270).
  That failed silently; the viewer now shows
  `(no <tag> highlighter in this build — showing plain text)` via the new
  `highlight.FencedSupported`. `contentTag` additionally covers the
  javascript spellings and `application/xhtml` servers really send.
  Updated [HTTP Client](/architecture/http-client.md).

## 2026-07-27 (http: dispatch reopens a closed response pane)

- After closing (or hiding via window.hideAllTools) the HTTP response pane, a
  later `http.run` filled a registered-but-invisible pane: request executed,
  history grew, nothing on screen (#1271). Pane lookup now requires a visible
  layout leaf, so the dispatch re-attaches it; an impossible reopen notifies
  instead of returning silently.
  Updated [HTTP Client](/architecture/http-client.md).

## 2026-07-27 (run: shell files runnable)

- `run.file` on a shell script works (#1225): the shell plugin contributes a
  run command — `<interpreter> <file>`, explicit `[lang.shell] interpreter` >
  shebang shell (when installed, falling back instead of pointing at a
  missing binary) > extension's natural shell. Updated
  [Run Configurations](/architecture/run-configurations.md).

## 2026-07-27 (layouts: tool tabs survive the snapshot)

- Saved layouts no longer degrade tab-hosted tools to plain editors (#1277).
  A tab host carrying exactly one tool session snapshots as a dedicated
  `tool` slot; mixed hosts keep the tool names in the editor identity and
  the apply restarts them as tabs in fresh editor slots.
  Updated [Pane Layout & Drag](/architecture/pane-layout.md).

## 2026-07-27 (layouts: orphaned terminals merge as tabs)

- Applying a layout no longer strands running terminals (#1275). Surplus
  shells and TUI tool panes that don't fit a slot used to stay registered but
  leafless — process alive, pane unreachable. They now merge as live terminal
  tabs into the last terminal slot (converted to a tab host, #836) or, with
  no terminal slot, into the last editor slot; sessions never restart.
  Updated [Pane Layout & Drag](/architecture/pane-layout.md).

## 2026-07-27 (overlays: pasting into floating inputs)

- Pastes reach overlay text inputs (#1273). `handlePaste` bailed on
  `overlayCapturesKeyboard()` and discarded the block, so nothing could be
  pasted into search-everywhere, the find-in-path query, a rename prompt or
  any other floating input; `Cmd+V` was equally dead (it maps to
  `editor.paste`, which no overlay handles). New router
  `internal/app/overlaypaste.go` mirrors the `KeyPressMsg` guard chain and
  delivers to `palette.Paste`, `finder.Paste`, `settings.Paste`,
  `explorer.Paste` and the app's rename prompts; overlays with no text input
  still swallow the paste rather than leaking it into the editor below.
  `ui.PasteText` flattens a block for these single-line fields — one-line
  pastes verbatim, multi-line trimmed and joined with spaces, control
  characters stripped (`/architecture/command-palette.md`).

## 2026-07-27 (editor: clipboard failures surface, yank syncs)

- System-clipboard writes no longer swallow their error (#1255): the register
  store records it (`Store.TakeClipboardError`, destructive read), the
  Cmd+C/Cmd+X toasts drain it before reporting success, and `Update` drains it
  after every keypress, so a missing or failing clipboard utility reports
  `system clipboard unavailable: <cause>` instead of a misleading "copied 3
  lines". A failed read still falls back to the unnamed register. The reported
  `Cmd+C` regression itself did not reproduce — the app-side bridge writes
  through on every editor path; the host terminal takes the chord first
  (Ghostty binds `super+c` to `copy_to_clipboard:mixed` by default).
- Vim yanks reach the system clipboard (#1256): `editor.clipboard_sync`
  (default on, Settings → Editor) mirrors unnamed `yy` / `y{motion}` / visual
  `y`. Named registers, deletes and changes stay internal; `p`/`P` are
  unchanged (`/architecture/editor.md`, `/architecture/keybindings.md`).

## 2026-07-27 (lang: XML highlighting)

- New `xml` language plugin (#1253, `plugins/languages/xml`): Tree-sitter
  highlighting via a vendored tree-sitter-grammars/tree-sitter-xml (`xml`
  grammar only, `dtd` out of scope) for tags, attributes, entity references,
  CDATA, processing instructions, the doctype-internal subset and comments.
  Matches `.xml` plus `.xsd`/`.xsl`/`.xslt`/`.svg`/`.plist`/`.wsdl` and the
  MSBuild family; `<!-- -->` block-comment toggle, element sticky scopes and
  folding; no LSP server (`/architecture/languages.md`).

## 2026-07-27 (php: suppress intelephense by-ref P1006 false positive)

- Default `lsp.diagnostics_ignore` rules (#1260) drop intelephense's P1006
  "Expected type '...'. Found 'null'/'unset'." false positives on
  by-reference arguments (upstream bmewburn/vscode-intelephense#3504, no fix
  as of 1.18.5); other P1006 findings still surface
  (`/architecture/lsp.md`).

## 2026-07-27 (diagnostics: decoration toggles + ignore rules)

- Per-source, per-severity decoration toggles (#1259): `editor.marks.lsp_*`
  and `editor.marks.git_*` gate the scrollbar stripe, gutter colouring and
  inline underlines (`editor/marktoggles.go`); the Problems pane keeps the
  full set. Diagnostic ignore rules (`lsp.diagnostics_ignore`, project scope)
  drop matching diagnostics before every consumer via the app-level filter
  (`internal/app/diag_ignore.go`, engine in `internal/lsp/ignore.go`); the
  `lsp.ignoreDiagnostic` command appends the caret diagnostic's rule. New
  settings page "Diagnostics" (`/architecture/editor.md`,
  `/architecture/lsp.md`, `/architecture/problems.md`,
  `/architecture/settings-ui.md`).

## 2026-07-27 (http client: response history under .ike/http/)

- Response history (#1251) completes epic #1247: `internal/httphistory`
  stores the last 5 responses per request (keyed by file + request key)
  under `.ike/http/`, pruned on append, best-effort writes; the response
  viewer browses them with h/l and shows position + timestamp in the footer
  (`/architecture/http-client.md`).

## 2026-07-27 (http client: highlighting, run action, response viewer)

- HTTP client UX (#1250): new `http` language plugin with the vendored
  tree-sitter-http grammar; `http.run` (cmd+enter / ctrl+f9, Run menu,
  palette) dispatches the request block under the cursor; responses land in
  the reusable read-only `internal/httppane` viewer (pane kind `KindHTTP`)
  with JSON pretty-printing, content-type-aware highlighting and
  binary/truncation notices (`/architecture/http-client.md`).

## 2026-07-27 (http client: dispatch with .curlrc/.netrc detection)

- New `internal/httpclient` package (#1249): executes parsed request blocks
  after placeholder substitution. `.netrc` credentials apply when no explicit
  `Authorization` header exists; supported `.curlrc` options (headers, proxy,
  insecure, user-agent, user, referer, location, timeouts) map onto the
  request with explicit `.http` values always winning; unsupported options
  become warnings. Sensible defaults for redirects/TLS/timeout, 10 MiB body
  cap (`/architecture/http-client.md`).

## 2026-07-27 (http client: .http file parser)

- New `internal/httpfile` package (#1248, epic #1247) parses `.http` files
  into RFC 9112 request blocks: `###` separation with optional request names,
  `#`/`//` comments outside bodies, per-block tolerant errors with line
  numbers, and `${NAME}` / `{{$env NAME}}` placeholders resolved at dispatch
  time via `Request.Resolve` (`/architecture/http-client.md`).

## 2026-07-27 (restore last project: home is not a project marker)

- `project.restore_last` never fired from `$HOME` (#1245): `isProjectDir`
  treats a `.ike` entry as a project marker, but `~/.ike` is the user config
  directory — present for everyone who ever wrote a setting — so the cwd guard
  from #1010 suppressed every restore started in the home directory. Home is
  now exempt from the marker rule on the cwd side, matching the existing
  target-side guard (`/architecture/project-switching.md`).

## 2026-07-27 (float resize drag: edge tracks the pointer)

- Mouse resize of centered floats (#933) doubled its pointer-to-size mapping
  (#1243): a size delta grows both sides of a centered box, so 1:1 mapping
  made the grabbed edge lag the pointer by half. One pointer cell now maps to
  two size cells — the grabbed edge lands exactly under the mouse
  (`/architecture/floating-shell.md`).

## 2026-07-27 (floating shell: z-ordered stack infrastructure)

- Added `ui.Stack` (#1237, `internal/ui/stack.go`): floating shells layered in
  z-order — persistent base layers vs transient `Push`ed layers, input routed
  to the topmost open layer only, dismiss/outside-click pops one layer at a
  time, compositing bottom-to-top via `overlay.Center`. The root now hosts its
  `shell` as the stack's base layer; a stack of one behaves exactly as before,
  so all existing floating consumers are unchanged
  (`/architecture/floating-shell.md`).

## 2026-07-26 (themes: broadened palette set — 10 new built-ins)

- Added the #1230 batch: `everforest-dark`/`everforest-light` (the missing
  green family), `ayu-dark`/`ayu-mirage`/`ayu-light` (the warm/orange
  family), `github-dark`/`github-light` (Primer; the light variant passed
  the full contrast matrix with zero corrections), `oxocarbon`,
  `monokai-pro`, and `zenburn` (needed the largest lightness lifts of the
  batch, as predicted). All corrections are lightness-only with per-slot
  comments (`/architecture/themes.md`).

## 2026-07-26 (themes: JetBrains pair — darcula, intellij-light)

- Added the JetBrains theme pair (#1228): `darcula` (Surface `#2b2b2b`, the
  canonical keyword/string/number anchors) and `intellij-light` (white-first,
  `#0033b3`/`#067d17`/`#1750eb` anchors). Slots that missed the #1226 contrast
  rules moved in lightness only, each with a comment; the strong JetBrains
  list/selection backgrounds (`#4b6eaf`/`#a6d2ff`) were pulled toward
  `Surface` per rule 3, while `darcula`'s editor selection `#214283` passed
  as-is (`/architecture/themes.md`).

## 2026-07-26 (theme contrast: full-matrix audit)

- Every built-in theme was audited across the whole text/background matrix,
  not just the chrome spot-check: low-emphasis foregrounds (`InlayHint`,
  `VCSDeleted`, `file:lock`, comments) were lifted, the light variants' syntax
  accents darkened, and every overlay background (`Selection`, `Primary`,
  `SelectionMuted`, `Ruler`, the occurrence marks, the three diff tints)
  pulled back toward `Surface` so the semantic row colors panels keep under a
  selection stay readable. `TestBuiltinThemeFullContrast` now enforces the
  three rules (AA text, 3.5:1 dim text, overlay within 1.35/1.5 of `Surface`)
  (`/architecture/themes.md`).

## 2026-07-26 (scratch files: language picker, templates, running)

- `scratch.new` (`cmd+shift+n`, File menu) asks for the language instead of
  always creating `.txt` (#1223): new locked palette mode `scratchNewMode`
  (prefix `+`) listing "Plain Text" plus every registered language, `.txt`
  keeps a no-prompt command of its own (`scratch.new.text`), new scratches are
  seeded with the language's file template, and running a scratch from the
  project root with the project's interpreter is now covered by tests
  (`/architecture/scratch-files.md`).

## 2026-07-26 (vim parity: operators, g-sequences, text objects)

- The everyday vim keys missing from the modal editor landed (#1193): case
  operators `gu gU g~` (+ visual `u U ~`), `=` reindent and `gq` reflow (new
  `editor.text_width` setting), text objects `ip ap is as it at ib iB`,
  `gv gi gJ ge gE gf`, visual `J r x s =`, `zz zt zb`, `ZZ ZQ`, and the
  display-line motions `g0 g$ gj gk`
  (`/architecture/editor.md`).

## 2026-07-25 (versioning)

- IKE carries a version (#1214), starting at **0.1.0**: `internal/version`
  holds the number, the Makefile stamps commit + dirty through `-ldflags`,
  `ike --version` prints the banner and short-circuits `cli.Parse`, and the
  help overlay title shows the number
  (`/architecture/versioning.md`).

## 2026-07-25 (f3 scrolls its match into view)

- `search.nextMatch` / `search.prevMatch` (f3/shift+f3) moved the cursor to
  the next in-file match without scrolling it into view (#1198). The root
  model calls `editor.RepeatSearch` directly, bypassing `Update` — whose key
  branch ends in `scroll()`, which is what makes `n`/`N` follow the cursor.
  `RepeatSearch` now scrolls itself (`/architecture/search.md`).

## 2026-07-24 (saved window layouts)

- Saved window layouts (#1175): named, user-scoped, kind-only snapshots of
  the split tree (`~/.ike/layouts.json`) with a default marker.
  `window.saveLayout` / `window.layouts` picker / `window.setDefaultLayout` /
  `window.restoreLayout` (shift+F12); the default layout shapes projects
  without a persisted `layout.json` (`/architecture/pane-layout.md`).

## 2026-07-24 (copy path)

- Copy Path / Copy Relative Path / Copy Reference (#1173): registry
  commands + context-menu entries (editor: reference, explorer: paths),
  clipboard seam + toast (`/architecture/editor.md`).

## 2026-07-24 (hunk navigation)

- Git hunk navigation (#1170): `]c`/`[c` walk the #464 gutter hunks with
  wrap and n/m notices, registered as `vcs.nextChange`/`prevChange`
  (`/architecture/editor.md`).

## 2026-07-24

- Pinned tabs (#1172): `editor.tab.togglePin` (palette "Pin/Unpin Tab" +
  state-aware "Pin Tab"/"Unpin Tab" in the tab context menu) protects a tab
  from the `editor.tabs.limit` LRU eviction and from Close Others (manual
  closes stay allowed; an all-pinned pane exceeds the limit instead). Pinned
  segments render a single-width `•` prefix in Accent — part of the label
  string, so the bar's mirrored render/hit geometry is unchanged. Pins
  persist with the layout identity (`pinned` indexes into `tabs`) and a
  dragged-out pinned tab keeps its pin (`/architecture/editor-tabs.md`).
- Query history for search inputs (#1171): the editor's `/` `?` line and its
  `:` line recall recent committed queries with `up`/`down` (vim order, live
  line restored past the newest; typing after a recall edits normally), each
  in its own bucket; find-in-path's existing recall now persists too. One new
  `internal/histories` store (`histories.json` under the state store, named
  buckets `search`/`ex`/`findInPath`, deduped, capped at 50, marks.json
  degradation semantics), injected into editors and the finder by the app
  (`/architecture/editor.md`, `/architecture/search.md`).
- Change list (#1174): `g;` / `g,` walk a per-document ring (cap 100) of
  recent edit positions — the `CursorAfter` of every committed undo `Change`
  — with vim semantics (first `g;` = most recent edit, pointer resets on new
  edits, adjacent same-line entries collapse). Cursor motion only: no undo,
  no nav-history entry; entries shift with the local-mark delta scheme and
  jumps clamp into the buffer; notices `change list: n/m` and
  `no earlier/later edit position` (`/architecture/editor.md`).

- Terminal file:line links (#1168): output referencing `path/file.ext:12[:col]`
  (relative or absolute; Go compiler/test shapes) renders with a subtle
  always-on underline — decorated inside the version-keyed render cache and on
  scrollback rows as they window in — and cmd+click opens the file at that
  position through the standard `openPathAt` funnel (nav history records).
  Relative paths resolve against the session's live cwd; a cheap click-time
  `os.Stat` gates on existing files, so false regex positives stay inert.
  Plain-click selection is untouched (`/architecture/terminal.md`).
- Terminal scrollback search (#1169): `/` while scrolled into scrollback opens
  an inline search field on the pane's bottom row (explorer #1087 pattern) —
  case-insensitive contains, incremental jump to the nearest match above the
  anchored view, ctrl+p/up / ctrl+n/down stepping with wrap, reverse-video
  match highlights and a `3/17` counter; enter keeps the position, esc
  restores it. The live view and alt-screen/mouse-reporting children (vim,
  lazygit) always keep their own `/` (`/architecture/terminal.md`).
- Merge-conflict resolution in the editor (#1149): conflict blocks
  (`<<<<<<<` / `=======` / `>>>>>>>`, optional diff3 `|||||||` base) are
  detected per document version, tinted in the buffer (ours VCSAdded-mixed,
  theirs VCSModified-mixed, base dimmed, markers dim bold), resolvable via
  `merge.acceptOurs/acceptTheirs/acceptBoth` (one undo unit each, cursor at
  the block start) with `merge.nextConflict/prevConflict` wrap-around
  navigation; the editor context menu gains the accept entries when the caret
  sits inside a block, and blocks mark the overview ruler in VCSConflicted
  (diagnostics > conflicts > git) with click-to-jump
  (`/architecture/editor.md`, `/architecture/vcs.md`).
- Peek definition (#1154): `lsp.peekDefinition` (palette + editor context
  menu) shows the definition's surrounding lines in a cursor-anchored popup
  instead of jumping — bounded 15-line excerpt (live buffer when the target
  is open, bounded disk read otherwise), syntax-highlighted with the target's
  language, titled `path:line`. Esc closes, Enter jumps for real through the
  shared `DefinitionMsg` funnel (nav history records), up/down and
  ctrl+d/ctrl+u scroll, any other key closes and passes through; multiple
  targets route through the #279 candidates picker, then peek the chosen one
  (`/architecture/lsp.md`).

- Editor breadcrumbs bar (#1153, MVP of #31): a one-line `file ▸ symbol ▸
  child` row under an editor pane's tab/title row showing the documentSymbol
  chain enclosing the cursor, clickable per segment (jumps record nav
  history), front-eliding at narrow widths. Config `editor.breadcrumbs`
  (default on); data shared with the Structure pane via an app-side per-path
  cache filled by the settled-pass sync; the row is one extra chrome line the
  layout and all mouse translations account for
  (`/architecture/editor.md`, `/architecture/structure-view.md`).
- plugin.EventBufferSaved fires on every save (#1161): the lsp plugin's
  didSave hook (didChange flush + textDocument/didSave + the #1144 own-save
  file event) was dead in native builds; it now hangs off the same save
  funnel local history uses (`/architecture/lsp.md`).
- Live templates / snippets (#1152): `[[snippets]]` config entries (`trigger`,
  `body` in LSP snippet syntax, optional `language`) plus built-in examples
  (`internal/snippets`) expand on Tab after the trigger word in insert mode
  through the existing snippet placeholder engine — tab/shift+tab cycle
  placeholders like accepted LSP snippet completions; no match keeps Tab's
  indent behavior. Multi-line bodies re-indent to the current line, `\t`
  honours the buffer's tab settings. The same templates show as `template`
  snippet items in the completion popup via a new local completion source
  (priority 40, works with no LSP server); user entries shadow built-ins per
  trigger+language and config reloads apply live. See
  /architecture/editor.md, /architecture/completion.md,
  /architecture/config.md.
- Vim marks & bookmarks MVP (#1151, part of idea #55): `m{a-z}` sets a
  per-view local mark, `'{x}` / `` `{x} `` jump to the line's first
  non-blank / the exact position; `m{A-Z}` sets a global mark (path +
  position) persisted in the state store (`internal/marks`, marks.json) and
  jumped to through the open funnel, so cross-file jumps open the file and
  record in the navigation history. Marked lines show an accent `⚑` in the
  gutter (below breakpoint, above test marker); marks shift with edits via
  the breakpoint delta scheme and clamp on jump. `nav.bookmarks`
  (palette-only) lists all marks with path:line + preview — enter jumps,
  shift+delete removes (`/architecture/editor.md`,
  `/architecture/navigation-history.md`).

- Saved layouts with the Problems pane restore again (#1157): the
  restoreLayout pre-filter was missing the "problems" kind, silently
  falling back to the default layout (`/architecture/problems.md`).

- Test runner (#1150): languages declare test detection + command templates
  as data (`lang.TestSpec` — Go wired: `Test`/`Benchmark`/`Fuzz` in `_test.go`
  files, `go test -run '^TestX$'` with cwd = the package dir). The editor
  gutter marks test lines with a success-tone `▶` (cached per document
  version, precedence below breakpoints); `run.testAtCursor` /
  `run.testsInFile` (Run menu, palette, editor context menu) run the nearest
  test above the cursor / the file's package tests in the run-terminal
  placement, and register with `run.rerun`. Plain gutter clicks keep toggling
  breakpoints everywhere; ctrl/cmd+click on a marker runs the test. See
  /architecture/run-configurations.md.
- Usages tool window (#1155): find-references gets a persistent worklist —
  `lsp.referencesPanel` ("Find Usages (Panel)", also in the editor context
  menu) runs the same request as `lsp.references` (whose quick palette stays)
  but fills a singleton bottom-split pane (`internal/usages`,
  `pane.KindUsages`): rows grouped by file with `line:col` + preview, title
  "Usages: Foo — 12 in 4 files" from the symbol captured at request time,
  enter/double-click jumps via `DefinitionMsg`, `r` re-runs at the stored
  origin (best-effort after edits), `usages.toggle` palette command, layout
  slot persists and restores empty (`/architecture/usages.md`,
  `/architecture/lsp.md`).
- Format & organize imports on save (#1148): `editor.format_on_save` and
  `editor.organize_imports_on_save` (default off, Settings → Editor) chain a
  manual save — organize imports (`source.organizeImports` via
  `CodeActionContext.Only`, no picker), then LSP formatting, then the write —
  with per-step 2 s timeouts that fall through so a dead server never blocks
  or loses the save. Manual saves only; autosave and shutdown/switch writes
  stay raw (`write_raw`). Re-entrant saves coalesce; `:wq` still closes after
  the chained write (`/architecture/editor.md`, `/architecture/lsp.md`).

- New-file chords (#1145): cmd+n → explorer.newFile (prompt targets the
  explorer selection, works from a focused editor), cmd+shift+n →
  scratch.new (JetBrains verbatim); ledger regenerated
  (`/architecture/keybindings.md`).

- LSP watched files (#1144): external file creates/changes/deletes now reach
  language servers as `workspace/didChangeWatchedFiles` — the client
  advertises `didChangeWatchedFiles.dynamicRegistration`, stores each server's
  `client/registerCapability` globs (pragmatic `**`/`{}`/`[]` matcher), falls
  back to language matching for servers that never register, and batches the
  0140 watcher's per-file events (200 ms, per-path merge). IKE's own saves
  additionally emit a `Changed` event. Fixes Intelephense's stale
  "Undefined type" for externally created classes
  (`/architecture/lsp.md`, `/architecture/editor.md`).
- Scrollbar thumb reads as a background block (#1138): marks on the thumb
  keep their colour as foreground glyphs on the ScrollbarThumb background,
  so the thumb extent stays identifiable under dense git/diagnostic marks
  (`/architecture/editor.md`).
- Makefile language plugin (#1136): alemuller/tree-sitter-make vendored as C
  source, matched by `Makefile`/`makefile`/`GNUmakefile` base names + `.mk`;
  recipe bodies highlight as shell via the fragment injection seam; `#` line
  comments, rule folding/sticky scopes, no LSP server
  (`/architecture/languages.md`).

- Per-language indent default (#1137): `lang.Language.UseTabs` layers between
  `editor.use_spaces` and `.editorconfig` — make (recipes require a literal
  tab) and go/go.mod/go.work (gofmt) default to tabs; Tab, Enter auto-indent
  and `o` produce tabs in those buffers, an explicit `.editorconfig`
  `indent_style` still wins (`/architecture/editorconfig.md`,
  `/architecture/languages.md`).

- Dedicated go.work grammar (#1119): omertuc/tree-sitter-go-work vendored
  under the go plugin's `grammar_gowork/` (parser regenerated with
  tree-sitter-cli 0.25.10 to share the package's parser.h generation);
  `use` — single and block — now highlights; known trade-off: the grammar
  predates the Go 1.21 `toolchain` directive
  (`/architecture/languages.md`).
- Explorer exclude list (#1139): `explorer.exclude` — a TOML array of
  base-name glob patterns (default `.git`, `.idea`, `.DS_Store`) hidden from
  the tree at every depth, even with hidden files shown. Filtered in the
  single visibility gate (`childVisible`), editable live on the settings
  panel's new Explorer page via the new `List` control (comma-separated text
  persisted as a TOML array), explorer-only (go-to-file / find-in-path / LSP
  untouched) (`/architecture/explorer.md`, `/architecture/settings-ui.md`).

- Wheel scroll no longer snaps back to the selection (#1140): the
  cursor-anchored `clampScroll` split into `clampOffset` (bounds only, used by
  rebuilds, watcher/poll refreshes, config applies, resizes) and
  `followCursor` (cursor-into-view, used only where the cursor genuinely
  moved); `externalRefresh`'s cursor-stability snap explicitly does not follow
  (`/architecture/explorer.md`).

- Scrollbar thumb survives dense change marks and the viewport stops at the
  last line (#1134): thumb rows keep their glyph and only take the mark's
  colour; `SetScroll` clamps to lineCount − viewHeight (wrap/folds keep the
  looser clamp) (`/architecture/editor.md`).

- Git change marks in the editor scrollbar (#1131): hunks render as
  gutter-coloured overview-ruler marks from the existing #464 diff data,
  memoized beside the diagnostics stripe; clicking a marked track cell
  jumps centred to the hunk (`/architecture/editor.md`).

- Mouse-idle hover (#1129): resting the pointer over editor content for
  ~600ms opens the hover popup at the hovered cell (not the caret) — the
  diagnostic covering the cell immediately (works without any LSP hover
  support), the LSP hover content when the server answers. Demand-armed
  idle tick at the app layer (`internal/app/hover_idle.go`), a
  position-carrying hover seam in the bridge (`host.EditorHoverRequest` →
  `bridge.requestHover`), and an explicitly anchored hover popup; focused
  pane only for now (`/architecture/editor.md`, `/architecture/lsp.md`).
- Chrome mouse surfaces (#1128): right-clicking a tab-bar segment selects
  that tab and opens a context menu (Close / Close Others — new
  `editor.tab.closeOthers`, dirty tabs survive — / Reopen Closed); the title
  band outside segments opens a pane menu (Split Right, Split Down, Maximize,
  Close Pane — new `pane.close`, whole-pane guarded close). Every tab segment
  gains a trailing ✕ close button (dropped when a lone truncated segment has
  no room), and the status line's TODO, notifications and LSP segments become
  clickable via exposed segment spans (`/architecture/editor-tabs.md`,
  `/architecture/pane-layout.md`, `/architecture/status-line.md`).

- The VCS tool window slims to file-context features (#750): the panel is
  now a read-only changes list (enter/double-click = diff-vs-HEAD; no
  staging, no commit message, no Log tab), the commit dialog
  (`internal/commitui`) and the `vcs.commit` (cmd+k), `vcs.updateProject`
  (cmd+t) and `vcs.branches` commands are removed — the `cmd+k` pane-split
  sequences keep working, the bare prefix just times out. Git workflow is
  delegated to custom tool panes: lazygit ships preconfigured in the default
  config whenever it is on PATH (`/architecture/vcs.md`,
  `/architecture/tool-panes.md`, `/architecture/keybindings.md`).

- Large-file mode stops being silent (#1124, #1125): a persistent,
  dismissible banner over the focused flagged editor names the cause and
  both remedies (click = Force Code Insight, ✕/esc = dismiss per document),
  and the thresholds are editable in Settings → Files
  (`/architecture/editor.md`).

- Search Everywhere stops duplicating lines (#1121): workspace-symbol hits
  dedupe on (name, path, line) in SetHits — first, best-ranked hit wins;
  the shared cache also covers plain go-to-symbol
  (`/architecture/command-palette.md`).

- Undo gets the cmd+z default (#1117): dual-chord like save/redo — cmd+z
  where the terminal delivers Cmd, ctrl+z as the everywhere fallback; the
  stale "cmd is undeliverable" comment corrected
  (`/architecture/keybindings.md`).

- go.mod/go.work Tree-sitter highlighting (#1078): the
  camdencheek/tree-sitter-go-mod grammar is vendored as C source under the go
  plugin's `grammar/` (upstream's Go binding module path mismatches the repo,
  same as the Dockerfile grammar) and wired into the `go.mod`/`go.work`
  registrations with a `queries/gomod.scm` highlights query; `go.sum` stays
  plain (`/architecture/languages.md`).
- Explorer speed search (#1087): `/` with the tree focused opens a one-line
  type-to-select field on the pane's footer row (the error-banner region,
  mirroring the editor's `/` line); typing jumps the cursor to the visible
  row whose name contains the query (case-insensitive, prefix matches rank
  first, no auto-expansion), ctrl+n/p (down/up) step with wrap, enter keeps,
  esc restores the cursor, and the search captures every key so the tree's
  single-letter file-op bindings cannot fire mid-word
  (`/architecture/explorer.md`).

- Search case control (#1111): new `editor.search_ignore_case` setting
  (default off) makes in-file `/` `?` search case-insensitive without a
  `\c`; `\C` forces exact matching, ctrl+c on the open search line toggles
  the mode by rewriting the visible `\c`/`\C` marker. Precedence:
  marker > setting > smartcase (`/architecture/editor.md`).

- Search command line cursor editing (#1110): the `/` `?` (and `:`) input
  reuses the shared single-line editing helper (#763) — left/right with
  mid-query insertion, alt+backspace word delete, cmd+backspace clear —
  and the incremental preview keeps tracking mid-query edits
  (`/architecture/editor.md`).
- Recent-files MRU survives every startup path (#1112): the resumed-workspace
  path of a project switch skipped the session's `recent_files` load and the
  next save wiped the history; the MRU now reloads from `session.json`
  unconditionally in `buildModel` (`/architecture/command-palette.md`).

- Recent Files rows gained per-entry removal and a last-opened time (#1113):
  MRU entries carry timestamps (persisted as `{path, ts}`; the legacy bare
  string array still loads), rows show `ui.RelTime` and prune via
  `shift+delete` or the `✕` zone (`RemoveRecentFileMsg`), mirroring the
  project picker's #842 (`/architecture/command-palette.md`).

- Picker rows right-align the last-opened time (#1114): the new `Item.Time`
  column pins the time before the `✕` with clear separation; narrow rows
  truncate the name first and drop the time below a minimum width. Applies
  to the project picker, the Recent Projects side column and the Recent
  Files list (`/architecture/command-palette.md`,
  `/architecture/project-switching.md`).

- Stale Problems entries pruned (#1102): the LSP manager flushes a stopped/
  disabled language's unopened publishes with empty diagnostics, and
  deleting a file/directory drops its findings from the store
  (`/architecture/lsp.md`, `/architecture/problems.md`).

- Render micro-allocations trimmed (#1101): pane-box cache keyed on the
  border RGBA instead of a per-frame Sprintf, Problems header counts
  maintained on Refresh, menu bar string cached on
  (width/open/active/palette) — frame bench 1.53ms/12.2k allocs →
  1.33ms/9.0k (`/architecture/performance.md`).

- Row-style hoisting across the list panes (#1100): explorer builds a
  per-frame `rowStyleSet` (Mix for ignored rows once per frame, not per
  row); problems/structure/VCS row loops reuse loop-invariant base styles
  (`/architecture/performance.md`).

- Editor scrollbar stripe memoized + bar cells hoisted (#1097): warm View
  with an active error stripe drops from 329µs/606 allocs to 234µs/334
  (`/architecture/performance.md`).

- Explorer VCS facts resolve once per row (#1099): status/ignored/tint/letter
  share one relPath resolution, and the snapshot caches its
  EvalSymlinks-resolved root — no more per-row syscalls on symlinked roots
  (`/architecture/performance.md`).

- Explorer content width memoized + colour index precomputed (#1096, #1098):
  no more per-frame full-tree row rebuilding or per-row glob sorting; View
  reuses cached plain row widths for clipping
  (`/architecture/performance.md`).

- Frame wash stops re-wrapping the composed screen (#1095): the final
  palette background/foreground pass styles per line instead of re-running
  lipgloss Wrap/align over the exact-size frame — ~22% frame CPU and ~69%
  of per-keystroke allocations gone (bench: 2.24ms/39.7k allocs →
  1.75ms/12.4k); padded wash stays as the fallback for non-full-height
  frames (`/architecture/performance.md`).

## 2026-07-23

- Empty directories drop their expander once loaded (#1039), hidden-only
  contents included until the dot-toggle reveals them
  (`/architecture/explorer.md`).

- Explorer tree keys join the registry (#1041): navigation/open actions are
  rebindable commands with cheatsheet doc hints; the raw keys stay as the
  zero-config fallback (`/architecture/explorer.md`).

- Explorer root-path context + optional file-type markers (#1046): the root
  row carries a dimmed, home-abbreviated ` — ~/path` suffix (InlayHint
  colour, pre-truncated to the pane, suppressed under 30 cols), and
  `explorer.icons = true` (default off) adds a one-cell plain-unicode class
  glyph — dir/code/doc/config/image/other — between the expand marker and
  the name (`/architecture/explorer.md`).
- Explorer multi-select for file operations (#1044): `shift+j`/`shift+k`
  (and shifted arrows / shift+click) extend a contiguous anchor..cursor
  range — plain motions/clicks collapse it, `esc` clears it; range members
  render on the muted-selection background while the cursor keeps the full
  recipe; Delete acts on the whole selection with ONE confirm prompt and
  records the batch as ONE undo step, so a single undo restores everything
  (`/architecture/explorer.md`).

- Explorer node context menu (#1040): right-click selects the row and opens
  the shared `menu.Context` shell with the file-op commands
  (`/architecture/explorer.md`).

- Explorer tree navigation (#1032, #1043, #1035): gg/G/PageUp/PageDown/
  ctrl+u/ctrl+d motions, `C` recursive expand-all through lazy levels
  (scan-budget-bounded), ellipsis on right-clipped rows
  (`/architecture/explorer.md`).
- Explorer dims gitignored entries (#1045): `git status` gains `--ignored`
  on the same invocation, `! <path>` records feed `Snapshot.Ignored`
  (dir-prefix aware), and ignored rows render the foreground mixed toward
  the surface — below every real VCS status, no suffix tint, hidden-italic
  composes (`/architecture/explorer.md`).

- Explorer input prompts polished (#1047): new-file/new-folder/rename boxes
  render an `enter accept · esc cancel` hint line, and rename preselects the
  name stem — typing replaces it and keeps the extension, arrows/click drop
  the selection, folders and dotfiles preselect the whole name
  (`/architecture/explorer.md`).
- Explorer reveal expands collapsed ancestors (#1042): `explorer.reveal`
  (`alt+f1`) now descends to the open file through unloaded directories
  (async `pendingReveal` walk resumed by each landing scan), selects and
  scrolls to it; new `explorer.auto_reveal` config (default off) auto-reveals
  the focused editor's file on tab/focus switches — JetBrains autoscroll
  from source (`/architecture/explorer.md`).

- Explorer scrollbar thumb is draggable (#1036): thumb press grabs and a
  `dragExplScroll` gesture follows the pointer, mirroring the editor
  scrollbar (#1022); track clicks keep the click-to-jump
  (`/architecture/explorer.md`).

- Explorer trash moved into the state store (#1038): `IKE_CONFIG_DIR/trash`
  (or the project's `.ike/trash`) instead of a root-polluting `.ike-trash`;
  cross-device state dirs fall back project-local, and stale trash — the
  legacy dir included — is purged on startup since undo stacks are
  in-memory only (`/architecture/explorer.md`).

- Explorer errors stop wiping the tree (#1030): failed file ops open a
  dismissable dialog (any key/click), scan/poll errors render as a themed
  bottom banner; both leave the tree rendered and navigable
  (`/architecture/explorer.md`).

- Explorer small bug fixes: a pathy new-file name creates intermediate
  directories (#1031), the hidden-files toggle keeps the selection on the
  same entry (#1033), and `explorer.sort` actually works — `name`/`type`/
  `modified`, live re-sort on config change (#1037)
  (`/architecture/explorer.md`).

- Version-aware LSP settings (#1079): intelephense gets the project PHP
  version (`composer.json` `require.php` minimum bound, `php -v` fallback)
  as `intelephense.environment.phpVersion`; vtsls gets the vendored
  workspace TypeScript via `typescript.tsdk`. Highlighting stays
  version-agnostic by design (`/architecture/languages.md`).

- Recognized launch failures carry install advice (#1065): the Homebrew
  taplo built without the LSP feature now yields "install an LSP-capable
  build: npm install -g @taplo/cli …" in the failure toast, via a
  `knownLaunchFailures` table on top of the #1062 stderr extraction
  (`/architecture/lsp.md`, `/architecture/languages.md`).
- SQL default server is now sqls (#1066): sql-language-server crashes on
  startup under Node ≥ 26 (`ERR_PACKAGE_PATH_NOT_EXPORTED`) and upstream is
  unmaintained; sqls is a maintained Go binary speaking LSP over stdio with
  no args, installed via `go install github.com/sqls-server/sqls@latest`,
  root markers `.sqls`/`.git` (`/architecture/languages.md`).
- Companion-tool hints (#1067): a `ServerSpec` can declare optional companion
  binaries (`Companions`: shell → shellcheck, ansible → ansible/ansible-lint);
  when the server first becomes ready the LSP manager probes PATH and raises a
  one-time warn per missing tool ("shellcheck not found — shell diagnostics
  disabled (brew install shellcheck)"), deduplicated per language per session
  (`/architecture/languages.md`).
- go.mod / go.work / go.sum get gopls (#1063): the language registry gains a
  server-delegation seam (`Language.ServerLanguage` + `ServerLang()` /
  `HasServer()`), the go plugin registers the three files as filename-matched
  languages delegating to the "go" server, and the LSP manager attaches them
  to the same gopls instance/root as `.go` files while sending gopls'
  documented languageIds `go.mod`/`go.work`/`go.sum` in didOpen. No gomod
  Tree-sitter grammar is vendored, so they highlight as plain text
  (`/architecture/languages.md`).

- Startup-crash notifications name the real error (#1062): a server dying
  before the handshake surfaces its decisive stderr line (taplo's "the LSP
  is not part of this build", sql-language-server's Node error) instead of
  "jsonrpc: connection closed", and the launch-failure toast points at
  "LSP: Show Server Log" like the disable path (`/architecture/lsp.md`).

- LSP quick fixes: initialize advertises `textDocument.publishDiagnostics`
  (#1060 — vtsls gated all TypeScript push diagnostics on it), absent
  `workspace/configuration` sections answer `{}` instead of null (#1061 —
  css-language-server stopped validating on null), Problems header
  pluralizes correctly (#1064) (`/architecture/lsp.md`).

- Dirty directories take their subtree's dominant VCS status (#1053):
  `Snapshot.DirStatus` propagates the strongest child signal (conflicted >
  modified > added > untracked), so untracked-only folders stop reading as
  modified-blue and parent/child hues agree (`/architecture/explorer.md`).

- One selection recipe across the list panes (#1052, #1034): the cursor adds
  Selection background + bold and keeps the semantic foreground (explorer
  aligned to structure/problems/VCS); unfocused panes keep a muted
  `SelectionMuted` cursor row in all four lists instead of hiding it
  (`/architecture/explorer.md`).

- Explorer suffix-tint colour model (#1051, closes #1054): the colour channel
  belongs to the VCS status (whole row + right-edge `M`/`A`/`U`… letter as a
  non-colour cue); filetype colours tint only the extension of clean files;
  directories render uncoloured (`/architecture/explorer.md`).

- Explorer render polish (#1050/#1055/#1056/#1058/#1059): indent guides in
  the semantic `IndentGuide` slot (editor parity) instead of each row's
  filetype/VCS hue; hover only adds its background, preserving the
  active-file accent; open-file marker is underline-only (italics stay
  "hidden"); `(empty)` themed via `InlayHint`; guides/marker excluded from
  the selection bold (`/architecture/explorer.md`).

- Binding audit: default chords for the four unbound user-facing commands
  (#1048) — `cmd+8` problems.toggle, `cmd+3` structure.toggle (numeric
  tool-window family, palette fallback), `ctrl+f5` run.rerun and `ctrl+f2`
  debug.stop (JetBrains Windows scheme, delivered); everything else stays
  palette/vim by design (`/architecture/keybindings.md`).

- Clickable editor scrollbar with diagnostics error stripe (#1022): a
  track/thumb overlay on the pane's rightmost column when the buffer
  overflows, severity-colored markers at diagnostic lines' proportional
  positions, thumb drag (`dragEditScroll`) and click-to-jump on the track
  (`/architecture/editor.md`).

- Local history MVP (#1023, part of #35): every save records a
  content-addressed snapshot under `.ike/history/` (dedupe, 50-per-file +
  30-day pruning); `file.localHistory` lists the focused file's snapshots in
  a floating picker — enter diffs against the current buffer in the reusable
  diff pane, `r` restores through the undoable edit path
  (`/architecture/local-history.md`).
- Problems tool window (#1024, part of #33): singleton bottom-split pane
  aggregating LSP diagnostics project-wide from a new app-level store fed by
  every publish (unopened files included); grouped by file, errors first,
  enter/double-click navigates, `f` toggles current-file vs project scope;
  `problems.toggle` command, no default chord
  (`/architecture/problems.md`, new).
- Structure tool pane (#1025): singleton right-split pane with the focused
  buffer's LSP `textDocument/documentSymbol` tree — capability-gated request
  (both hierarchical and flat reply shapes), kind-glyph rows, cursor
  auto-follow, enter/double-click navigates via the open funnel (nav history
  records), refresh on open / buffer switch / save, layout persistence, new
  `structure.toggle` command (`/architecture/structure-view.md`,
  `/architecture/lsp.md`).

- Editor right-click context menu (#1020): `menu.Context` floating dropdown
  anchored at the click cell, reusing the menu bar's item/InfoFunc rendering;
  caret repositions unless the click lands inside the selection
  (`/architecture/editor.md`).

- LSP crash logs keep the actual error (#990): log markers always start on a
  fresh line, and the decisive stderr error line (`transport.ErrorLine`) is
  named in the crash/disable toasts and the crash log marker
  (`/architecture/lsp.md`).

- Stale LSP diagnostics no longer outlive their server (#994): disabling
  after repeated crashes, `StopLang` and `Shutdown` clear the dead server's
  publishes from open editors; restart attempts keep them
  (`/architecture/lsp.md`).

- Zen mode gets its chord (#934): `ctrl+alt+f` binds `view.zenMode` and sits
  on the terminal global-command allowlist, so zen toggles from a focused
  terminal/tool pane too — the pane-agnostic toggle from #957 was unreachable
  by key there (`/architecture/pane-layout.md`, `/architecture/keybindings.md`).

- ctrl+c discards the terminal's spooled output backlog (#989): aborted
  commands stop rendering immediately instead of replaying up to 16 MiB of
  pre-abort output (`/architecture/terminal.md`).

- PHP and Go ship `injections.scm` (#995): guess-gated SQL fragments in PHP
  strings/heredoc/nowdoc and Go raw/interpreted string literals — highlighting
  and fragment LSP now cover both hosts (`/architecture/highlighting.md`).

- Recursive file watch capped at 4096 directories (#1011): huge roots no
  longer exhaust kqueue fds and kill startup; truncation toasts once
  (`/architecture/performance.md`).

- restore_last guards (#1010): starting inside a project (.git/.ike) never
  redirects, and the home directory is never a restore target — fixes the
  self-sustaining $HOME hijack that exhausted fds
  (`/architecture/project-switching.md`).

- Navigation jumps frame their target 3 rows below the top edge (#996,
  `editor.JumpTo` behind `openPathAt`) — definition/usages/nav-history
  landings stop hiding at arbitrary viewport positions
  (`/architecture/navigation-history.md`).

- Idle performance (#1001): explorer auto-refresh polls off-loop (one app
  wake per minute when quiet instead of one per 2s), single-shot debounce
  timers are cancelled with their owners (terminal resize, watch flush, LSP
  bridge), and opt-in diagnostics land via IKE_PPROF / SIGUSR1
  (`/architecture/performance.md`).

- New "Open File…" (#999, `file.openPath`): palette path picker over
  absolute/`~` paths with tab completion — opens files outside the workspace
  as regular editor tabs (`/architecture/command-palette.md`).

- `project.restore_last` works now (#1000): startup re-anchors at the most
  recent project when enabled — CLI targets win, a vanished project falls
  back to cwd with a notice (`/architecture/project-switching.md`).

- File opens always land in a real editor pane (#998): terminal-only tab
  hosts and dedicated terminal/tool panes are skipped
  (`/architecture/editor-tabs.md`).

- Terminal/tools: `ctrl+cmd+left/right` switch tabs while a terminal or tool
  pane is focused (#997); the `ctrl+alt+arrow` secondaries stay with the
  shell (`/architecture/terminal.md`).

- LSP: replies to server→client requests always carry a `result` property —
  a nil payload now serializes as an explicit JSON null (#991); the omitted
  field crashed vscode-jsonrpc servers (Intelephense exited 1 right after
  `client/registerCapability`) (`/architecture/lsp.md`).

- Terminal: `cmd+t` in a dedicated terminal pane now opens a real terminal
  tab — the pane converts into a tab host in place (#983, reusing the #836
  conversion) instead of splitting a sibling pane below
  (`/architecture/terminal.md`).

- Terminal: `cmd+w` closes the focused terminal via an EOF to the shell; a
  running foreground process raises a confirmation guard first (#986,
  `Session.Busy` via TIOCGPGRP) (`/architecture/terminal.md`).

- Terminal: `cmd+d` splits the focused terminal's pane right with a fresh
  terminal (#982, iTerm-style); reserved chords now canonicalize bubbletea's
  super+/meta+ Cmd encodings (#981), and the pin-picker notice renders its
  chord platform-normalized (`/architecture/terminal.md`).

- Backspace/Delete remove a visual selection outright (#979); a selection
  made while editing returns to insert mode at the deletion point
  (`/architecture/editor.md`).

- Editor drag selection (#977): press-drag extends a charwise visual
  selection; double-click-drag word-wise (origin word always fully selected),
  triple-click-drag line-wise (`/architecture/editor.md`).

- Editor multi-click selection (#975): double-click selects the word under the
  pointer, triple-click the whole line — regular visual selections, so
  `cmd+c`/`cmd+x` and visual operators consume them; a fourth or later plain
  click collapses back to a bare cursor (`/architecture/editor.md`).

## 2026-07-22

- Focused terminals let more IDE chords through (#973): settings, go-to
  file/symbol, find/replace in path, explorer toggle, hide-all-tools, pinned
  files, TODO index, VCS panel, notification history — and double-shift now
  opens Search Everywhere from a terminal (`/architecture/terminal.md`).

- Fixed result rows wrapping onto a second line in Find in Path and the
  palette (#971): the finder box's inner width was 2 cells too wide (the
  border counts inside lipgloss Width), row "clipping" used MaxWidth (which
  WRAPS, not truncates) in the finder, locations list and palette rows, and
  multi-line match texts rendered a literal second row. All rows now hard-
  truncate and flatten embedded newlines.

- Status-line overflow shrinks priority-aware (#471): the file path gets a
  middle ellipsis first, then low-priority segments drop in a defined order;
  cursor/mode/diagnostics survive narrow widths
  (`/architecture/status-line.md`).

- Terminal completion matches case-insensitively (#968): commands, make
  targets and paths complete regardless of typed case; accepting a
  case-different candidate erases the word and pastes the canonical
  spelling (`/architecture/terminal.md`).

- Toolchain package management (#571): the python packages view installs,
  uninstalls and upgrades packages (`+`/`-`/`u`), marks available upgrades
  with `↑ <latest>`, prefers `uv add`/`uv remove` in uv projects, and keeps
  everything async with the decisive stderr line on failure
  (`/architecture/settings-ui.md`). Also fixed: package-listing results were
  never routed to the settings panel (#569 regression).

- Terminal sessions track the shell's live cwd via OSC 7 (#770): completion
  candidates, pane title and status line follow `cd`; start directory stays
  the fallback without prompt integration (`/architecture/terminal.md`).

- Rainbow brackets (#789): bracket pairs colored by Tree-sitter nesting
  depth, cycling six theme-derived colors; `editor.rainbow_brackets`
  (default on), overridable per slot via `theme.captures.rainbow.N`
  (`/architecture/highlighting.md`).

- Hide All Tool Windows (#791): cmd+shift+F12 hides every visible tool pane
  after snapshotting the layout tree; a second press restores it exactly
  (fallback re-attach when things diverged while hidden)
  (`/architecture/pane-layout.md`).

- Harpoon-style pinned file slots (#788): four per-project slots
  (`.ike/pins.json`), `ctrl+shift+1..4` jumps, `cmd+2` opens the picker
  (reorder / unpin / pin active file); a vanished pin raises the picker with
  the slot selected (`/architecture/pinned-files.md`).

- Fixed an explorer panic in projects whose root holds only hidden entries
  (#949): stepping "into" the root advanced the cursor past the visible rows
  (children existed but none rendered), and the next `current()` call — e.g.
  via refresh — indexed out of range. `current()` now also heals any stale
  out-of-range cursor.

- LSP jumps and nav-history traversal focus the pane where the target file
  is already open (#930) instead of duplicating it as a tab in the current
  pane; unopened targets open in the current pane as before
  (`/architecture/navigation-history.md`, `/architecture/lsp.md`).

- Centered popups cap their width on large terminals (#932):
  `ui.popup_max_width` (default 110 columns, 0 disables, Appearance page)
  bounds the palette box, the modal shell, and the settings panel; #774
  resize deltas apply on top and still clamp to the terminal
  (`/architecture/command-palette.md`).

- Fixed a nil-pointer crash in session snapshotting (#931): with an editor
  pane whose active tab is a terminal (#573) focused, project switch and quit
  dereferenced `Instance.Editor()` unchecked. `activeEditorKey` documents the
  editor-kind-not-editor-model invariant; all `.Editor()` call sites audited
  and guarded.

- Floating windows resize by mouse drag (#933): grab the border ring of the
  Settings panel, the centered palette (Search Everywhere / Run a Command /
  Recent Files), or the modal shell — edges resize one axis, corners both;
  sizes persist via the #774 store and re-clamp to the terminal
  (`/architecture/floating-shell.md`).

- Zen mode works for any focused pane kind (#934): terminal and tool panes
  maximize chrome-free like editors; leaving zen restores the layout
  (`/architecture/pane-layout.md`).

- Insert-mode `cmd+backspace` is now IntelliJ's Delete Line (#955): the whole
  current line is removed including the preceding line break (line 0 takes the
  following break), caret lands at the end of the previous line. `ctrl+u`
  keeps kill-to-line-start (`/architecture/editor.md`).

- Reflow corruption over resize cycles (#953): a reflow cache pins the hard
  breaks the last replay wrote (exact-width lines can no longer merge), and
  the shell's live edit line replays verbatim anchored on the last content
  row — zsh's SIGWINCH redraw no longer fights the relayout
  (`/architecture/terminal.md`).

- Terminal multi-click drag (#951): holding the button after a double/triple
  click extends the selection word-wise / logical-line-wise in both
  directions, origin unit always covered (`/architecture/terminal.md`).

- Terminal width reflow (#935): resizing rewraps the whole history (screen +
  scrollback) at the new width like iTerm2/kitty — shrink rewraps long lines,
  grow unwraps them, hard newlines never merge, round-trips reproduce the
  layout; replay-based, height-only resizes keep the #807/#826 machinery
  (`/architecture/terminal.md`).

- Terminal soft-wrap heuristic after shrink (#947): width-truncated lines no
  longer read as soft-wrapped (scrollback width / resize-reserve checks), so
  triple-click and copy no longer chain unrelated clipped lines
  (`/architecture/terminal.md`).

- Markdown table cell inline rendering (#945): box-drawn table cells now
  render their inline markdown (`code`, emphasis, strike, links) styled and
  without marker chrome; column widths size by the concealed display width
  (`/architecture/editor.md`).

- Terminal selection (#936): copying joins soft-wrapped rows into one logical
  line (only hard newlines reach the clipboard); double-click selects the
  word under the pointer (shell-friendly charset, wrap-spanning),
  triple-click the whole logical line (`/architecture/terminal.md`).

- PHP web debugging host filter (#938): the HTTP_HOST probe now uses `eval`
  (property_get missed superglobals — context 0 vs 1 + `auto_globals_jit` —
  so the filter silently detached every request); filter/busy detaches are
  announced (warning notification + console line), troubleshooting section
  added (`/architecture/debugger.md`).

- LSP handshake sequencing (#937): the client now queues all notifications and
  blocks all requests until the initialize response has arrived and
  initialized is sent — early didOpen/initialized traffic crashed Intelephense
  on every start (`/architecture/lsp.md`).

- Inline color preview (#790): `#rrggbb`/`#rgb`/`rgb()`/`hsl()` literals tint
  with their own color (contrast foreground by luminance); toggle
  `editor.color_preview` (`/architecture/editor.md`).

- Auto-import fix (#929): cursor/caret positions now adjust past
  additionalTextEdits that end on the cursor's own line — accepting an
  auto-import no longer mangles the inserted import line
  (`/architecture/lsp.md`).

- Python decorator highlighting fix (#928): only `@` + dotted name carry the
  decorator color, arguments render as normal call expressions; capture-order
  convention documented (`/architecture/highlighting.md`).

- Web language highlighting (#925): TSX grammar covers js/jsx/ts/tsx under
  the single typescript id, HTML grammar with script/style injections, CSS
  grammar; verified against real-world JS/CSS/Jinja-template files
  (`/architecture/languages.md`).

- Ansible inventory navigation (#922): IKE-side host/group index (INI + YAML
  inventories, group_vars/host_vars); goto-definition on `hosts:` /
  `delegate_to:` / `groups[...]` via new local-definition seam in the LSP
  bridge, completion source for hosts values
  (`/architecture/languages.md`).

- Markdown rich rendering (#881): bold/italic/strikethrough text attributes,
  marker concealment on non-cursor lines with exact click mapping, and
  row-preserving box-drawing pipe tables; toggle
  `editor.markdown_rendering` (`/architecture/editor.md`).

## 2026-07-21

- Ansible language support (#897): context-sniffer detection (role trees,
  playbooks, project markers) resolves `.yml` to the ansible id sharing the
  YAML grammar; @ansible/ansible-language-server for module completion; new
  generic sniff layer in the lang registry (`/architecture/languages.md`).

- Markdown language support (#880): block + inline grammar via injection,
  fenced code blocks highlighted in their fence language (new dynamic
  `@fragment.language`/`@fragment.content` seam), YAML/TOML front matter,
  marksman completion (`/architecture/languages.md`,
  `/architecture/highlighting.md`).

- Shell language support (#894): tree-sitter-bash highlighting, rc-file base
  names + shebang interpreters, bash-language-server (with automatic
  shellcheck diagnostics) (`/architecture/languages.md`).

- Shebang language fallback (#893): extensionless files resolve their language
  from the `#!` line (env and env `-S` forms, version-suffix stripping);
  editor records a per-path association the whole path-keyed pipeline follows
  (`/architecture/languages.md`).

- YAML language support (#879): tree-sitter-yaml highlighting with sticky
  mapping-key scopes, yaml-language-server (schema-store) completion; indent
  rules limited to `:` and block-scalar introducers
  (`/architecture/languages.md`).

- Dockerfile language support (#896): tree-sitter-dockerfile highlighting
  (vendored C source — upstream Go binding unusable), docker-langserver
  completion; first exact-base-name (`Filenames`) language besides templates
  (`/architecture/languages.md`).

- TOML language support (#895): tree-sitter-toml highlighting with sticky
  `[table]` scopes, taplo language server for schema-store completion
  (`/architecture/languages.md`).

- JSON / ndjson language support (#878): tree-sitter-json highlighting for
  `.json`/`.jsonc` plus ndjson/jsonl streams, vscode-json-language-server
  completion for json only (`/architecture/languages.md`).

- Settings modal-flow migrations (0420, #892): keymap capture + import, LSP
  override editor, uv-install picker and PHP mapping form are sub-panels;
  every custom page is searchable (`/architecture/settings-ui.md`).

- Settings feedback & safety (0420, #891): confirmation sub-panels for
  destructive actions, ✓ saved-to-scope flash, inline write errors, pickers
  follow the highlight (`/architecture/settings-ui.md`).

- Settings rail & chrome (0420, #890): section headers, first-letter jump,
  remembered page (persisted per project), page name in the title, ▲/▼
  scroll indicators (`/architecture/settings-ui.md`).

- Settings widget affordances (0420, #889): [x] bools, ‹enum› with ←→
  cycling, ± int steppers with visible clamp feedback, ✎/⌨ edit markers
  (`/architecture/settings-ui.md`).

- Settings shared text input (0420, #888): all nine inline inputs use
  ui.EditKey/CursorView — cursor, word ops, rune-safe backspace (umlaut
  corruption fixed) (`/architecture/settings-ui.md`).

- Settings key unification (0420, #887): r=reset / R=restart / g=refresh /
  o=options-JSON / space toggles; pgup-pgdn-home-end everywhere; shared
  chord-capture sub-panel; `?` key-help overlay
  (`/architecture/settings-ui.md`).

- Settings search over everything (0420, #886): the filter matches category
  titles (jump rows) and custom-page items via the Searchable seam
  (Toolchain/Tools/Plugins); enter navigates, the rail stays alive
  (`/architecture/settings-ui.md`).

- Settings mouse parity (0420, #885): hover highlighting, viewport wheel
  (1 category/notch on the rail), clickable scope chip / hint keys /
  completion suggestions, scrollable Plugins & Marketplace lists
  (`/architecture/settings-ui.md`).

- Venv wizard sub-panel (0420, #884): visible four-step create flow —
  tool/python/location/run — with disclosure, clickable suggestions,
  spinner+cancel, real stderr on failure, and a `+ New environment…` action
  row plus `python.newEnvironment` palette command
  (`/architecture/settings-ui.md`).

- Settings sub-panel primitive (0420, #883): pushed overlay levels with
  breadcrumb, one-level esc, clickable button rows; Tools add/edit form
  migrated as the first sub-panel (`/architecture/settings-ui.md`).

- Usages when already at the definition (#860): F4/cmd+click on the
  definition itself list the symbol's usages (count in the hint, declaration
  excluded) instead of jumping in place (`/architecture/lsp.md`).

- Cmd+click go-to-definition (#859): cmd+left-click on a symbol places the
  cursor and runs lsp.definition (F4 parity, nav history recorded).

- F4 / go-to-definition never silent (#858): an empty answer toasts whether
  nothing was found or no ready language server could be asked
  (`/architecture/lsp.md`).

- Floating-window resize chords delivered on macOS (#774): cmd+shift+arrows
  (and alt+shift) join ctrl+shift — Mission Control/Spaces own ctrl+arrows on
  macOS, so the original chord never reached the terminal.

- Shared views keep highlighting (#857): ShareDocumentWith adopts the
  source's span index (document-derived, immutable) instead of clearing it —
  tab drag/drop and pane merges no longer produce unhighlighted views
  (`/architecture/editor.md`).

- Emmet completion subset (0410, #856): CSS shorthands and HTML tag
  snippets as snippet completion items with expansion previews
  (`/architecture/completion.md`).

- Web languages + server choices (0410, #855): TypeScript/JavaScript (vtsls),
  HTML and CSS (vscode-langservers-extracted) registered as language plugins;
  per-language default-server rationale documented, overridable via
  `[lsp.servers.<id>]` (`/architecture/languages.md`).

- Unified completion ranking (0410, #854): fuzzy-dominant score with source
  priority, locality-tier and persisted per-project MRU boosts
  (`/architecture/completion.md`).

- Symbol-index completion source (0410, #853): grammar-capture symbols with
  kinds, CSS classes/IDs offered in HTML class=/id= attributes, watcher-driven
  invalidation (`/architecture/completion.md`).

- Word-index completion source (0410, #852): instant vim-keyword-level
  candidates from open buffers (event-fed, lazily extracted) + one-shot
  project scan, locality-tiered, prefix-prefiltered
  (`/architecture/completion.md`).

- Completion engine fan-in (0410, #851): completion is multi-source — the
  LSP bridge and the local engine (`internal/complete`) answer triggers as
  tagged batches, merged in the editor with priority de-dup and stable
  selection; host editor-event sinks are named and fan out
  (`/architecture/completion.md`).

- Completion trigger kinds (0410, #850): requests report
  TriggerCharacter/Invoked correctly, per-server trigger characters threaded
  through host and fragment paths (`/architecture/lsp.md`).

- Incomplete completion re-query (0410, #849): `isIncomplete` replies
  re-request on further typing, debounced in the bridge
  (`/architecture/lsp.md`).

- Lazy completion resolve (0410, #847): selection-driven, debounced
  `completionItem/resolve` fills in documentation (rendered under the popup)
  and late auto-import edits (`/architecture/lsp.md`).

- Completion auto-import (0410, #848): `additionalTextEdits` on accepted
  items apply in editor coordinates alongside the main insert, one undo step
  (`/architecture/lsp.md`).

- Snippet completions (0410, #846): the client declares `snippetSupport`;
  snippet insert texts expand via `internal/lsp/snippet` and accepted items
  with tabstops run a tab/shift+tab placeholder session in the editor
  (`/architecture/lsp.md`).

- Fuzzy completion matching (0410, #845): the completion popup filters by
  fuzzy subsequence (`internal/fuzzy`) against `filterText`, ranks by match
  score with CamelCase/boundary bonuses, and ties/unfiltered lists follow the
  server's `sortText` (`/architecture/lsp.md`).

- Project history pruning + last-opened badge (#842): picker rows (and the
  Recent Projects column) show a relative last-opened time; shift+delete /
  the ✕ zone on unloaded entries removes them from `project.history`
  (user-scope write-back, live re-list)
  (`/architecture/project-switching.md`).

- Project settings on switch + live reload (0380, #795): the switch's config
  reload applies the incoming project's `.ike/settings.toml` and drops the
  outgoing one's; the file watcher now watches `<root>/.ike` and an external
  `settings.toml` edit reloads the config without restart
  (`/architecture/project-switching.md`, `/architecture/config.md`). Epic
  0380 complete.

- Settings write-scope selector (0380, #794): `s` in the settings panel
  cycles auto/user/project as the write target for edits and resets, with a
  `[scope: …]` title chip; project writes create `.ike/settings.toml` on
  demand and clearing a project key falls back immediately
  (`/architecture/settings-ui.md`).

- Project settings loader hardening (0380, #793): unknown settings keys now
  warn per key instead of being silently ignored, and config-load
  diagnostics (parse errors, unknown keys, clamp warnings) surface as
  session-deduped warning notifications at startup, on reload and on
  project switch. The layered loader, Origin and project-scope write-back
  already existed (`/architecture/config.md`).

- Center merge on terminal/tool targets (#836): dropping tab-capable content
  (file tabs, terminal panes/tabs, tool panes) into a terminal or tool
  pane's interior converts it into a tab host — the running session becomes
  the first tab; tool tabs get `⚙` labels and persist by restarting on
  restore (`/architecture/pane-layout.md`, `/architecture/editor-tabs.md`,
  `/architecture/tool-panes.md`).

- Tool instances (#835): `multiple = true` on `[[tools.custom]]` allows
  concurrent instances via a `tool.<slug>.new` command (plain command toggles
  the most recent one); single-instance tools are now also found when hosted
  as an editor tab, so the toggle never spawns a duplicate
  (`/architecture/tool-panes.md`).

- Recent Files focus placement (#819): the dialog's column focus now follows
  the best match — empty files list or a better/only project match focuses
  the Recent Projects column; files win ties; a manual `tab`/arrow/click
  switch overrides until the query changes (`/architecture/command-palette.md`).

- The `[debug.php]` listen settings are editable in-IDE (#832): a Debug
  schema section (port, hostname filter), a PHP Debug Mappings custom page
  for `[[debug.php.path_mappings]]`, and a mapping-suggestion prompt when
  an accepted request's fileuri does not resolve locally
  (`/architecture/debugger.md`).

- PHP web/request debugging (#823): `debug.listen` toggles a persistent
  DBGp listener for php-fpm/Apache requests — sequential multi-accept,
  `[debug.php]` hostname filter (via `$_SERVER['HTTP_HOST']` probe, detach
  on mismatch) and docroot↔project path mappings
  (`/architecture/debugger.md`).

- Terminal height shrink no longer eats the newest output (#826): the top
  rows scroll into the scrollback and the cursor line stays (real-terminal
  semantics, edge-independent); a grow pulls the pushed rows back for an
  identical round trip. Also fixed the #807 reserve's stale-resurrection
  hole (height-restore guard now compares before the grow's snapshot)
  (`/architecture/terminal.md`).

- Closing an editor tab now closes its LSP document (#827):
  `EventBufferClosed` gained a producer — the root model records every
  removed editor view and fires the hook once no view of the file remains
  in any in-memory workspace, so the LSP manager drops the document
  (didClose) instead of holding its text forever (`/architecture/lsp.md`).

- Closing a background workspace now releases its memory (#825): teardown
  cuts the workspace's registry/tree references and fires the new
  `EventWorkspaceClosed` hook; the LSP bridge closes the root's documents
  and stops its servers (`manager.CloseRoot`). Weak-pointer regression
  tests pin collectability (`/architecture/workspace.md`).

- Busy workspaces confirm before teardown (#821): close-from-list prompts
  with a running/unsaved summary (save / discard / cancel), and IDE quit
  aggregates the checks across all in-memory workspaces
  (`/architecture/workspace.md`).

- Recent-projects lists mark in-memory background workspaces with ● and
  close them in place (#820): shift+delete or a click on the ✕ zone unloads
  the workspace without switching (`/architecture/workspace.md`).

- Mouse back/forward buttons drive the navigation history (#816): buttons 4/5
  resolve through the keymap as `mouse-back`/`mouse-forward` (rebindable),
  default nav.back / nav.forward (`/architecture/navigation-history.md`).

- Outer-edge pane docking (#811): dragging a pane onto the workspace's
  outermost strip docks it full-width (top/bottom) or full-height
  (left/right), with a full-span ghost preview; the docked extent is capped
  at a third of the workspace (`/architecture/pane-layout.md`).

- Tool panes survive their program's exit (#810): a centered exit dialog
  offers restart in place (r / click) and close (ctrl+w / click); the layout
  slot is kept (`/architecture/tool-panes.md`).

- Terminal shrink no longer destroys content (#807): the session snapshots
  the screen before every applied resize and restores the clipped cells on
  grow (prefix-guarded, so rewritten rows win); scrollback already keeps its
  full width (`/architecture/terminal.md`).

- Divider-drag resize smoothed (#804): terminal PTY/emulator resizes are
  debounced (leading + trailing), so a drag no longer triggers a child
  SIGWINCH redraw storm per step; motion coalescing (#602) already bounds
  relayouts to one per rendered frame (`/architecture/pane-layout.md`).

- Terminal output no longer starves the UI with many busy panes (#803):
  Session.View caches the rendered grid per mutation version, and the input
  coalescer folds cross-session OutputMsgs into one batch per adaptive flush
  (`/architecture/terminal.md`).

- Global navigation chords work from a focused terminal (#805): the chords
  bound to `palette.searchEverywhere`, `palette.recentFiles` and
  `project.switch` (plus a configured `palette.toggle_key`) dispatch in the
  IDE instead of being forwarded to the shell (`/architecture/terminal.md`).

- External git changes refresh the VCS snapshot (#738): the watch service now
  watches `.git` + `.git/logs` and emits a coalesced `GitChanged` event, so
  commits/checkouts/staging done in a lazygit pane or terminal update gutter
  marks, statusline branch and explorer coloring automatically
  (`/architecture/vcs.md`).

## 2026-07-20

- Background workspace cap & eviction (#780, 0370 M4): project.max_workspaces
  (default 3) bounds parked workspaces; idle LRU evicts silently, busy ones
  behind an e/esc guard. config.md + workspace.md updated.
- Recent Projects column in Recent Files (#778, 0370 M3): cmd+e grows a
  left column listing recent projects; tab/arrows switch columns, enter
  switches project seamlessly. Generic palette SideMode extension.
  command-palette.md updated.
- Working-directory re-anchor audit (#779): pinned the invariant that the
  process cwd equals the active workspace root and everything root-derived
  resolves at call time; audit tests added. workspace.md updated.
- Background workspaces (#777, 0370 M2): project switches park the live
  workspace (terminals, runs, dirty buffers, debug state) instead of tearing
  it down; switching back resumes it as-is. Unsaved-changes prompt removed
  from the switch path; #96 terminal adoption retired; terminal session keys
  globally unique. workspace.md updated.
- Workspace extraction (#776, Roadmap 0370 M1): internal/workspace bundles
  pane registry + split tree + terminal return-focus behind a Manager; the
  root model reaches them via m.activeWS() only. Behavior unchanged; the
  seam for background workspaces (M2). workspace.md added.
- Resizable floating windows (#774): ctrl+shift+arrows resize the settings
  panel, the centered palette (width + visible rows) and the floating shell;
  deltas persist per window kind in .ike/winsize.json and re-clamp to the
  terminal. floating-shell.md, settings-ui.md, command-palette.md updated.
- Most-used command ranking in the palette (#773): palette-window selections
  bump a persisted per-project counter (.ike/cmdusage.json); equal-score
  results rank most-used first, keybind invocations never count.
  command-palette.md updated.
- terminal.toggle ignores custom tool panes (#772): with only tool panes
  open it spawns a new regular terminal; terminal.clear follows the same
  rule. terminal.md updated.
- Keymap page lists never-bound registered commands (#771): every registry
  command without a binding — including configured tool commands — appears
  as a `(no binding)` row, filterable via `/`, enter captures its first
  chord. keybindings.md updated.
- Terminal command completion popup (#740): auto-suggest while typing at
  the shell prompt + ctrl+space on demand; PATH commands, paths and make
  targets; accept pastes the remainder; inactive on the alt screen.
  terminal.autosuggest toggles the auto trigger. terminal.md updated.
- cmd+t in a focused terminal spawns a sibling terminal (#729): sibling tab
  in an editor-hosted terminal pane, split pane below a dedicated one;
  reserved-set entry, global cmd+t binding untouched. terminal.md updated.
- Untitled buffers save as new files (#730): saving a pathless buffer opens
  a save-as prompt; accepting writes the file and binds the tab (watcher,
  MRU, highlighting, hooks) like a disk open. editor.md updated.
- Idle autosave (#731): editor.auto_save gains "idle" — a dirty titled
  buffer saves itself after editor.auto_save_idle_ms of quiet, riding the
  backup change seam; editor.md updated.
- Palette and finder inputs are full single-line editors (#763): shared
  `internal/ui.EditKey` helper gives them a movable cursor (arrows, home/end,
  word jumps, word deletion, forward delete, insert-at-cursor);
  `command-palette.md` and `search.md` updated.
- Divider gutters removed (#761): panes tile the viewport exactly, their own
  rounded borders forming the seam; the resize handle is now a two-cell hit
  band over the borders meeting at a split boundary (divider precedence over
  the title band, so a shared border resizes and the title text row moves).
  Updated `/architecture/pane-layout.md`.

- Tool-pane setup (#751–#753, #759): new curated catalog
  `internal/toolcatalog` (lazygit, lazydocker, sqlit, k9s, htop, btop —
  requirement gates, ordered install recipes with PATH re-verification). The
  post-tour setup flow gains a fourth step (checkbox list; writes
  `[[tools.custom]]`, installs missing binaries, results toasted),
  re-runnable via the new `tools.setup` palette command; Settings → Tools
  gains `s` suggestions adding a catalog tool with one keypress. Updated
  `/architecture/welcome-tour.md` and `/architecture/tool-panes.md`.

- Diagnostic details popup (#739): `lsp.diagnosticInfo` (default `ctrl+f1`)
  shows the caret line's diagnostics on the hover popup surface — severity
  header colored like the gutter, server attribution (`source · code`,
  `Diagnostic.Code` newly threaded through from the protocol), full message;
  multiple entries separate with a rule. Clean line → info toast.

- Editor tab limit (#742): `editor.tabs.limit` (default 5, 0 disables, also
  in Settings → Editor) caps document tabs per pane. Opening beyond it closes
  the least recently used eligible tab into the reopen ring; the active tab,
  dirty tabs, scratch tabs and terminal tabs are exempt — an all-exempt pane
  exceeds the limit instead of risking data.

- Tools settings page (#755): Settings → Tools edits the `[[tools.custom]]`
  list from the UI — add/edit/delete with a five-field inline form (name,
  command, args, cwd, placement), validation (required fields, duplicate
  names, placement enum), write-back at user scope and live reload of the
  `tool.<name>` commands.

- Custom TUI tool panes landed (#741): `[[tools.custom]]` config entries
  (name, command, args, cwd, placement) become `tool.<name>` palette
  commands opening a pane that runs the program directly. Toggle-focus
  semantics like terminal.toggle, tool chrome (`⚙ NAME`, not terminal
  chrome, also in the statusline), program exit closes the pane, layout
  restore restarts the tool (a de-configured tool degrades to a shell), and
  the process gets `IKE_THEME_*` env vars for theme following. New concept
  doc `/architecture/tool-panes.md`.

- Terminal session teardown is race-free (#748): `go test -race` failed on
  main because upstream vt's `Emulator.Close` races concurrent `Read`/`Write`
  (plain-bool closed flag). Teardown now joins read/feed loops, stops the
  write loop via a sentinel byte through the host-bound pipe, and closes the
  emulator only once no goroutine is inside it.

- Terminal output survives lock/sleep/resume (#734): the PTY read loop now
  drains into an in-process spool (16 MiB soft cap) and a separate feed loop
  replays chunks into the emulator — a stalled render loop can no longer
  backpressure into the kernel TTY queue where output around a suspend/resume
  window could be flushed and lost.

- Keymap page lists unbound defaults (#736): a preset default whose chord was
  unbound or rebound away stays visible as an `(unbound)` row — enter captures
  a new chord for the command, `r` is a per-binding reset restoring the shipped
  default, `u` no-ops. Unbinding can no longer strand a command permanently.

- Terminal forward word kill (#733): `option+forward-delete` in a focused
  terminal pane now translates to `ESC d` (readline `kill-word`), completing
  the #240 macOS editing-chord set.

## 2026-07-19

- cmd+v pastes the system clipboard into a focused terminal pane and the debug
  panel's embedded debuggee terminal (#727): the key is caught before raw
  forwarding and fed through the bracketed-paste path (`PasteText`), since a
  Kitty-protocol host delivers cmd+v as a key event, not a paste.

- Terminal capability check (#720): startup probe (Kitty keyboard protocol
  handshake with grace tick, tmux/screen env, color profile) opens a centered
  floating report — one headline + fix per deficiency, esc dismisses; the palette/cheatsheet "⚠
  terminal-dependent" suffix on fragile-only bindings is gone. Per-chord
  classes remain in the settings keymap page and reachability matrix.

- LSP server logs (#715): every server's stderr now tees into
  `~/.ike/logs/lsp-<lang>.log` (start header, exit footer, manager lifecycle
  markers: crashed / restarting n/3 / disabled; >1 MiB rotates to `.old`).
  New palette command `lsp.showLog` ("LSP: Show Server Log") opens the most
  recent log in a new pane; the disabled-after-repeated-crashes toast points
  at it.

- The example reference plugin no longer ships in the ike binary (#716): its
  EventFileOpened hook toasted "example saw open: <path>" on every file open.
  The package remains as the documented plugin reference with its tests.

- Post-tour setup flow (#713): finishing the Welcome Tour now chains three
  setup dialogs through the floating shell — a theme picker (j/k previews
  live, enter persists `theme.name`, esc restores), the LSP server picker
  (force-opened past the `lsp.onboarded` gate so a re-taken tour delivers
  on its closing promise), and a read-only toolchain summary (resolved
  interpreter per toolchain-capable language, pointer to Settings →
  Toolchains). Esc/q mid-tour skips the flow; first-run LSP onboarding
  still queues behind an escaped tour. New `internal/app/setup.go`; last
  tour page names the steps.

## 2026-07-18

- Leader layer retired (#711): all `space <key>` / `ctrl+k <key>` mnemonic
  bindings and the `[keymap] leader` config key are gone — every default is a
  single modifier chord, JetBrains-verbatim where a default exists. Exactly
  five multi-step sequences remain (`cmd+k down/up/left/right` pane splits,
  `cmd+k z` maximize) plus JetBrains' `shift shift` double-tap. Re-homed
  chords: `cmd+shift+t` reopen closed tab, `cmd+alt+z` revert file (JetBrains
  rollback), `cmd+9` VCS tool window, `cmd+alt+m` markdown preview,
  `cmd+alt+t` new terminal, `cmd+alt+n` notification history,
  `cmd+alt+shift+right/down` split view. Zen mode, the leader VCS family
  (undo revert, revert hunk, branches, diff, blame) and the cheatsheet's
  `cmd+k cmd+s` are palette/menu-only now. A policy test
  (`TestAllDefaultsAreModifierChords`) enforces the rule.

- Terminal tabs are draggable like file tabs (#707): another editor's center
  zone moves the live session into that tab list, any edge zone splits it off
  as its own terminal pane (`DetachTerminalTab`, `AddTerminalPaneFrom`); the
  shell never restarts, and its exit still closes a split-off pane via
  session-key routing (`terminalPaneForSession`).

- Terminal pane adopts into a tab list (#708): dragging a terminal pane's
  title bar onto an editor's center zone moves the live shell session into
  that pane's tab list as a terminal tab (`Instance.DetachTerminal` +
  `adoptTerminalPane` — no shell restart); edge drops still relocate the
  whole pane.

- PHP debugging landed (0360, epic #697): `debug.start` on a PHP file runs it
  under Xdebug with breakpoints, stepping, frames, variables and value editing
  in the existing debug UI. IKE speaks DBGp natively — `internal/dbgp`
  (protocol client, #698; latin-1 XML tolerance #705) plus an in-process
  DAP↔DBGp bridge (`internal/dbgp/bridge`, #699, variables #700) behind the
  new `lang.DebugAdapterInProcess`/`dap.Connect` seam; no Node, no external
  adapter. The PHP plugin (#701) contributes launch args and the Xdebug
  preflight: missing extension detected via `php -m`, auto-install tries
  `pecl` then Homebrew's `shivammathur/extensions/xdebug@<version>`.

- Debug panel column-resize drift fixed (#695): widths are stored in exact
  cells instead of re-encoded fractions, so the untouched column no longer
  creeps during a drag; frames|vars pushes the right separator along (output
  clamps at its minimum), vars|output leaves the left separator put.

- Debug panel keeps stale frames/variables while the debuggee runs (#693):
  input waits, sleeps and IO no longer blank the columns — the last stop's
  data renders faint behind a `running…` indicator, with frame activation,
  variable expansion and inline editing gated until the next stop.

- Debug panel columns resizable (#691): the frames/variables/output separators
  drag like pane dividers (`dragDebugDiv` gesture), with min-width clamping and
  proportions that stick across panel resizes.

## 2026-07-17

- Debug panel survives session end (#689): termination flips the panel into a
  `finished (exit code N)` state instead of closing it — output lines and the
  embedded terminal's scrollback stay reviewable, trailing adapter output
  still appends, and the next launch resets the reused panel
  (`debugpanel.SetFinished`/`ResetSession`).
- Interactive tour try-it steps (#680): selected tour pages carry checkbox
  tasks (search everywhere, file-tree toggle, terminal toggle); non-paging
  keys pass through to the app while a page's task is unfinished, the
  command-executed signal (#679) ticks the box (the header flips to "all done
  — press → to continue"), overlay-opening tasks cover or suspend the tour
  and it resumes on the same page, and paging/skipping works regardless of
  task state.

- Toolchain versioned-install discovery (#675): the interpreter picker globs
  Homebrew `opt/<formula>@*/bin` (both prefixes, unversioned formula first,
  newest version first), pyenv `~/.pyenv/versions/*` and Go `~/sdk/go*`,
  deduplicated by resolved path; opening the picker pre-selects the currently
  effective interpreter and eagerly probes every candidate's version.

- Command-executed event (#679): every command dispatch — palette, keymap
  resolution (including chord timeouts and plugin key aliases), and inline
  invocations — now emits `plugin.EventCommandExecuted` (payload: command id)
  to hooks plus an in-app `CommandExecutedMsg` through the Update loop, so
  internal consumers like the interactive tour can observe executions without
  plugin machinery. Exposed to WASM guests as `command_executed`
  (`sdk.CommandExecuted`).

- Debuggee terminal embeds in the debug panel (#676): a DAP `runInTerminal`
  debuggee now runs inside the panel's Output column (`debugpanel.SetTerminal`)
  instead of a separate bottom-split terminal pane — keys route raw to the PTY
  while the column is focused and the process runs (`shift+tab` escapes),
  mouse forwards column-local, the exited terminal stays reviewable and is
  replaced by the next session, and it closes with the panel. DAP output rows
  still render when no terminal is embedded.

- JetBrains keymap XML import (#677): `internal/keymap/jbimport` parses a
  JetBrains keymap export (keystroke grammar incl. second-keystroke chords),
  maps IntelliJ action ids onto IKE command ids and writes the result as
  `keymap.bindings.*` user-scope overrides, unbinding replaced preset
  defaults. Entry points: the `keymap.importJetBrains` palette command (shell
  path prompt with tab completion) and `i` on the settings Keymap page.

- Settings custom-page mouse (#674): optional `PageClicker`/`PageWheeler`
  interfaces on the `PageModel` seam; the panel forwards form-column clicks
  (page-local coordinates) and wheel deltas to Toolchain, Keymap, LSP,
  Plugins and Marketplace — click selects, a click on the selection runs the
  page's enter-equivalent action, picker rows are clickable, the wheel moves
  the selection.

- Tour resolver-first shortcuts (#678): tour rows resolve through the live
  keymap first (custom > default) with the curated preferred-order list kept
  when the resolved chord is among its options; curated fallbacks are
  platform-normalized for display (Meta→Ctrl off macOS), the help cheat-sheet
  row resolves like every other, and a guard test keeps help's doc-hint
  `Shortcut` fallbacks platform-neutral.

- Settings schema-page mouse (#673): the wheel scrolls the panel column under
  the pointer (categories switch pages, form rows follow); with an enum picker
  open a click chooses the option or closes the picker; with an inline edit
  active an outside click commits (or cancels when invalid) instead of being
  ignored.

- Tour first-run gate fix (#671): the tour scan keys on `ui.onboarded` alone —
  the settings file always exists at scan time because main records the
  project open into the recent-projects history before the model is built, so
  the file-existence heuristic meant the tour never auto-opened in the real
  binary (the LSP dialog appeared instead).

- Wheel batch delivery (#669): coalesced wheel bursts now reach each pane
  handler once, carrying the tick count (consumers scroll the whole distance
  in one call) instead of being replayed per event; the terminal caps what it
  forwards to the child (~one screenful) so alt-screen/mouse-reporting
  children stop lagging behind trackpad bursts.

- Theme is a user setting (#667): palette theme commands now write
  `theme.name` to user-scope `~/.ike/settings.toml` (like the Settings page)
  instead of a per-project session override; the override mechanism is
  removed and stale `session.json` theme entries are ignored, so one theme
  follows the user across projects.

- Tour known-defaults fix (#665): the tour's chord resolution now knows every
  default binding per command (leader mnemonics, delivered secondaries), so
  the resolver picking e.g. the `space space` leader default no longer
  masquerades as a remap and replace the curated preferred-order display;
  vcs.panel and menu.open show their real defaults instead of "via palette".

- Status-bar empty-editor hint (#659): a focused editor with no file shows
  `? help · shift shift find` (resolver-truth chord, dropped under ~70
  columns), and the status line now truncates instead of wrapping when the
  segments outgrow the terminal width.

- Tour first-run wiring (#658): the welcome tour auto-opens on a first start,
  sequenced crash recovery → tour → LSP onboarding dialog. New `ui.onboarded`
  config flag, written when the tour opens so a mid-tour quit neither
  re-triggers the tour nor suppresses the LSP dialog; the LSP dialog's scan
  now gates on `lsp.onboarded` alone instead of settings-file existence.

- Welcome tour (#657): new `internal/tour` package — a passive five-page
  walkthrough (entry keys & quitting, vim modes, layout & navigation, tools
  incl. the terminal escape hatch, customization) hosted in the floating
  shell with host-level paging keys; opened via the new `help.welcomeTour`
  palette command and listed in help Essentials. Shortcuts render
  resolver-truth with curated multi-chord defaults.

- Help essentials view (#656): `?` now opens a curated Essentials cheat sheet
  (~25 commands in feature groups, one screen) instead of the full registry
  dump; `tab` toggles to the full list and back, a non-empty filter always
  searches the full set, and a footer line shows counts + the toggle hint.
  Curated IDs are drift-guarded by a registry test.

- Floating shell key seam (#655): new optional `ui.KeyHandler` Content
  extension — keys that neither fed the live filter nor matched a dismiss key
  are offered to the content (`HandleKey(key) bool`) before scroll handling,
  letting content own view toggles or paging keys. Routing order is now
  filter → dismiss → key handler → scroll.

- Terminal toolchain activation (#652): the effective interpreter (explicit
  beats detected) now activates JetBrains-style in fresh IDE terminals —
  venv interpreters prepend `<venv>/bin` + set `VIRTUAL_ENV`, private
  toolchain dirs (pyenv/mise/asdf/go) prepend their own directory so `which`
  shows real paths, and shims remain only as the fallback for explicit
  choices in shared system dirs (detected shared-dir interpreters inject
  nothing). Detected project `.venv`s activate too. Running sessions keep
  their env; new terminals pick up changes on config reload.

- Version-manager shim resolution (#650): interpreter detection no longer
  surfaces pyenv/mise/asdf shims — `lang.ResolveShim` asks the owning manager
  (`<mgr> which <bin>`, run in the project root so per-project pins apply) for
  the real executable, best-effort with the shim as fallback; the python/php/go
  plugin detectors resolve their PATH hits through it and toolchain discovery
  resolves + dedupes shim candidates (the hardcoded pyenv shim entry now shows
  the resolved versioned path).

- runInTerminal robustness (#638): every bail-out path of the reverse request
  now sends an error response (gone session, empty argv, split/spawn failure —
  the adapter blocks on the answer); a failed spawn closes the just-split pane
  and re-saves the layout; the debuggee terminal stays open after the session
  for output review and the next session's runInTerminal replaces it once its
  process exited (`Model.dbgTermKey`, cleared on user close); `env` JSON nulls
  (= unset) unmarshal (`map[string]*string`, nil skipped) and malformed
  arguments are refused with a diagnostic; reverse-request refusals moved off
  the read loop (write deadlock).

- Debug variable-edit hardening (#640): a panel restored from a saved layout
  becomes editable at the first stop (`attachDebugPanel` runs on the
  panel-already-exists path too); `SetScopes`/`SetChildren` cancel an open
  inline editor; `setVariable` is refused with a notice while the debuggee
  runs and a spontaneous `continued` event blanks the panel like stepping; a
  failed refetch after a successful set surfaces an error toast; the editor
  row windows to the column width around the cursor; and the edit-cancelling
  esc no longer arms the double-esc palette.

- Debug panel mouse hardening (#639): border clicks (coordinates outside the
  pane interior, which the layout hit-test still routes to the pane) no longer
  select an off-by-one row or the wrong column; every click — output column and
  title row included — records into the double-click tracker so an intervening
  click resets a pending double-click; the wheel pulls the selection along to
  stay in the visible window (vcspanel behavior); a click while the inline
  value editor is open cancels the edit first, and a wheel while editing
  scrolls without moving the selection.

- Debug output console made live (#637): the tool window renders its columns in
  every state (placeholder in FRAMES while running / not paused, OUTPUT keeps
  streaming) and opens on the first output event if closed (once per session);
  output auto-follows the newest line unless the user scrolled up (bottom
  re-follows); ANSI escapes are stripped and `\r`/`\t` normalized before
  buffering (log too — ANSI stripped, plus a per-session delimiter line and
  trailing post-termination output); the pre-panel buffer is capped at 5000
  chunks.

- Explorer show_hidden persistence (#642): a config-driven change to
  `explorer.show_hidden` now saves the session immediately (after
  `panes.Reconfigure`, when the explorer's `ShowingHidden()` actually changed),
  so a settings edit survives a kill/crash instead of being clobbered by the
  stale session at next boot. Unrelated reloads still never write session.json.

- debug.stop cancels an in-flight launch (#636): a stop during the
  auto-install/handshake window (dbg still nil) now clears `dbgLaunching`,
  bumps a launch generation counter and drops the deferred post-install retry
  on generation mismatch, with a "launch cancelled" toast. Previously the stop
  was a silent no-op and the retry started a session anyway.

- Shared empty-editor predicate (#641, follow-up to #628): `editor.Model.IsEmpty`
  (no file, no text) is now the one emptiness definition — `Instance.IsEmptyEditor`,
  `openInTab`, and the CLI stdin/missing-path opens all use it, so a scratch tab
  with typed text gets a new tab appended instead of being clobbered. Open-in-new-pane
  (`openPath` with NewPane) reuses an empty active editor instead of splitting past
  it, mirroring the diff path. `layout.Replace`'s doc comment now matches its
  behavior (in-place mutation, no collision check).

- Interactive debug input via runInTerminal (#625): Python now debugs with
  `console: integratedTerminal`; debugpy's runInTerminal reverse request spawns
  the debuggee in an IKE command-terminal pane with a real tty, so `input()`
  works. Added the DAP reverse-request seam (`Conn.SetReverseHandler`/`Respond`,
  `Session.OnRunInTerminal`/`RespondRunInTerminal`) and terminal `Pid()`.
  Trade-off: the debuggee's output now lives in that terminal, so #624's OUTPUT
  column/log stay empty for Python.

## 2026-07-16

- Debug console output + logging (#624): the debug tool window gained an OUTPUT
  column streaming the debuggee's stdout/stderr (stderr tinted, own scroll,
  pre-open output buffered and flushed on open). Every chunk is also appended
  verbatim to a per-project transcript `.ike/debug-session.log`. Previously the
  captured output was written to a dead buffer, never shown or persisted.

- Debug variable editing (#627): `e` on a variable row in the tool window opens
  an inline editor; commit pushes the new value via a new `Session.SetVariable`
  (DAP `setVariable`) and refetches the container to show the result. Gated on
  the adapter's `supportsSetVariable` capability, now read from the initialize
  response. While editing, the app routes every key to the panel.

- Debug tool window mouse support (#626): wheel scrolls the focused column and
  left-click selects a frame/variable (double-click activates, mirroring enter),
  routed via a new `debugpanel/mouse.go` on the vcspanel pattern. The panel now
  carries per-column scroll offsets so long stacks/variable lists scroll instead
  of clipping, and keyboard nav auto-scrolls the selection into view.

- Diff-open reuses an empty editor pane (#628): every diff-open (HEAD/commit/
  diff.files) now routes through `placeDiffLeaf` — when the active editor is an
  empty scratch pane (`Instance.IsEmptyEditor`), the diff takes over its slot in
  place via the new `layout.Replace` instead of splitting a new pane; a
  file-backed or dirty editor is preserved and the diff splits beside it.

- Explorer show-hidden toggle stability (#629): `Configure` now re-applies
  `explorer.show_hidden` only when the config value actually changed (tracked in
  `hiddenCfg`), so an unrelated live reload (plugin/interpreter/project switch)
  no longer clobbers the runtime `.` toggle. Toggling also emits
  `HiddenToggledMsg`, which the app persists to the session immediately so the
  state survives a kill/crash, not only a clean quit.

- Debug adapter tty isolation (0350, #620): the DAP adapter now spawns detached
  in its own session (`transport.Spec.Detached` → `setsid`). debugpy's launcher
  was `tcsetpgrp`-ing the inherited controlling terminal to give the debuggee
  terminal foreground, stealing the tty from the TUI and stopping it with
  SIGTTIN (input leaked to the shell mid-raw-mode). Also: empty launch `args`
  are omitted (a JSON null tripped debugpy's `"args"[0] must be str` validator),
  and a `dbgLaunching` guard drops a second `debug.start` mid-launch so it can't
  tear down the in-flight session (`dap: connection closed`).

- Pane-level View cache (0400, #615): a pane whose content did not change now
  skips its inst.View() recompute. Editors expose a complete RenderVersion (the
  #614 render epoch folded with vertical scroll, viewport height, and a hash of
  the external breakpoint set); Instance.View reuses the cached string while the
  version and active-tab index are unchanged. A never-stale test compares the
  cached render against a fresh one after every mutation (scroll/cursor/resize/
  focus/edit/paused/blame/breakpoint) — proven to catch an incomplete version.
  Marginal on top of #614 (View was already cheap there) but completes the
  "render only changed parts" goal. Epic #593.

## 2026-07-16

- Editor line-body cache (0400, #614): the editor memoizes rendered line bodies
  (renderSpan output) keyed by (line, from, to, width) and guarded by a
  renderEpoch that bumps on every body-affecting mutation but NOT on vertical
  scroll (renderSpan never reads view.Top). So a scroll reuses cached lines
  instead of re-highlighting the whole window each frame; renderSpan/editor.View
  drop out of the render hot path. The gutter renders fresh (decorations never
  stale); the cache is per-view (fresh on New/ShareDocumentWith). A never-stale
  test compares every mutation's cached render against a forced-fresh one. Epic #593.

## 2026-07-15

- Incremental frame composition (0400, #612): render was recomposing every pane
  each frame (each inst.View + lipgloss paneBox/Join re-measuring every line),
  even unchanged ones. Added a per-pane box cache keyed by a hash of the freshly
  rendered content + chrome (never stale — content is always recomputed, only the
  identical-box composition is skipped), and replaced the layout-tree and
  body/status/menu lipgloss.Join* with a measurement-free compositor (joinH/joinV)
  that places exact-size boxes by direct line concatenation. Fullscreen scroll:
  Model.render cum ~69% -> ~42%, StringWidth ~32% -> ~15%. Combined with the #610
  pacing this buys higher fps at the same CPU ceiling. Epic #593.

## 2026-07-15

- Adaptive scroll-render pacing (0400, #610): sustained fullscreen scrolling
  pegged a core because the coalescer re-injected a batch every ~16ms and each
  triggered a full-frame recomposition (every pane's View + lipgloss re-measuring
  every line). No leak — fixed render cost x fixed 60fps cadence. `Model.render`
  now records its cost (`renderNanos`) and the coalescer paces the next batch at
  cost x 3 (16ms floor, 66ms ceiling), holding scroll-render CPU near 1/3 of a
  core: cheap frames stay ~60fps, expensive fullscreen frames throttle to
  ~15-22fps instead of saturating. Keys/clicks bypass pacing. Epic #593.

## 2026-07-15

- Fullscreen render lag fix (0400, #608): `os.Getwd()` — a stat syscall on macOS —
  was called every frame from the terminal title, status line, and breakpoint
  gutter (once per pane), ~49% of all CPU under a fullscreen scroll, so latency
  scaled with window size. Cached the working directory (`cachedGetwd`,
  invalidated on project switch); the per-frame syscall is gone (`rawsyscalln`
  50% -> ~4% in the re-profile), roughly halving render CPU. Residual hotspot is
  now lipgloss frame composition (grapheme-width), deferred. Epic #593.

## 2026-07-15

- Mouse coalescer backpressure fix (0400, #606): the input coalescer cleared its
  `armed` flag before the blocking re-inject `send`, so under a render-bound
  scroll every 16ms tick spawned another flush goroutine that blocked in send —
  the pending-message pile grew without bound and scrolling degraded back to the
  old lag after a while. `armed` now stays set across the whole flush and re-arms
  only after the send returns (and only if events piled up), bounding it to one
  in-flight flush. Confirmed via pprof that render (full-frame terminal write) is
  the syscall-bound bottleneck the backlog was forming behind. Epic #593.

## 2026-07-15

- Bracketed paste as one block (0400, #603): a `tea.PasteMsg` now routes to the
  focused editor's new `PasteText`, inserting the whole pasted block as a single
  edit and one undo unit (visual replaces, mid-insert splices, normal pastes
  after the cursor) without touching the yank registers or clipboard — no more
  character-by-character insertion of a large paste. Terminal panes get the block
  through their own bracketed-paste path; a modal overlay suppresses the route.
  Epic #593.

- Mouse input coalescing (0400, #602): a `tea.WithFilter` hook
  (`internal/app/inputcoalesce.go`) absorbs `MouseWheelMsg`/`MouseMotionMsg` and
  returns nil, so bubbletea skips Update + render for them — a scroll/drag burst
  no longer queues ahead of keystrokes. A ~16ms timer re-injects the folded
  events as one `coalescedInputMsg` applied in a single pass, preserving net
  scroll distance. Keys, clicks, resize and paste pass straight through. Fixes
  the "scroll a lot then cmd+k, everything drains one-by-one" unresponsiveness.
  Epic #593.

## 2026-07-14

- Coalesced diagnostics (0400, #597): a workspace-diagnostic server publishing
  for hundreds of library files no longer pushes one tea.Msg (one Update pass +
  re-render) per file. The bridge accumulates publishes (latest per path) over a
  50ms window and flushes one `DiagnosticsBatchMsg`; the app routes each set to
  its editor leaf in a single Update pass. Epic #593.

- Coalesced didChange (0400, #595): the LSP bridge no longer runs the
  O(document) diff + sync on the bubbletea Update goroutine per keystroke. Each
  edit stores the latest text and arms a 40ms `changeDebounce`; the flush (diff,
  notification, follow-up semantic/inlay/highlight requests) runs on the timer
  goroutine. Requests flush the pending change first (via `cur()`, plus
  completion/signature/save), so nothing reads stale server text; close cancels
  it. A typing burst collapses to one sync. Epic #593.

- Watcher vendored-dir pruning (0400, #596): the fsnotify watcher no longer
  registers a watch for every directory under the root. `skipWatchDir` prunes
  dot-directories (`.git`, `.venv`, caches) and a deny-list of non-dotted noise
  (`node_modules`, `__pycache__`, `site-packages`, `vendor`) at `Start` and when
  auto-watching newly-created dirs. A populated `.venv`/`node_modules` used to
  register thousands of watches — FD exhaustion + an event-loop flood — which is
  a main cause of large-project lag. Epic #593.

- Async LSP transport (0400, #594): the jsonrpc connection no longer blocks the
  caller on the server draining stdin. Callers marshal + enqueue onto an
  unbounded outbound queue; a dedicated writer goroutine owns every framed pipe
  write. This fixes the large-project freeze where per-keystroke didChange sent
  from the bubbletea Update goroutine stalled behind a busy language server, so
  keystrokes and mouse events trickled in one-by-one. Epic #593.

- Debug auto-install for non-venv interpreters: `--break-system-packages`
  fallbacks let debugpy install into an externally-managed interpreter (PEP
  668 Homebrew/system python, uv-managed standalone python) where the plain
  pip/uv install was refused outright; absent installer tools are skipped and
  the error now leads with the real cause.

- Debug adapter auto-install (#589): debug.start preflights debugpy in the
  resolved interpreter, installs it on demand (pip, then uv) with clear
  notifications and relaunches; handshake errors now carry the adapter's
  stderr tail instead of a bare "connection closed".

- Debug tool window (0350, #580): `internal/debugpanel` bottom-split panel
  (`pane.KindDebug`) with frames view and variables tree; frame selection
  re-scopes variables and navigates the editor, variable nodes expand via
  DAP `variables` requests; opens on stop, closes with the session. Epic
  #572 complete.

- Debug sessions (0350, #579): `debug.start` (shift+f9) launches the active
  file's run configuration under its DAP adapter, stops at stored
  breakpoints with an editor jump + warning-tone paused marker, and steps
  via F8/F7/shift+F8/F9; `debug.stop`, live breakpoint sync, one session at
  a time. New debugger concept doc.

- DAP client (0350, #578): `internal/dap` — LSP base-protocol framing with
  the DAP seq/type envelope, request/response correlation, event dispatch,
  and a typed Session (initialize/launch/setBreakpoints/configurationDone,
  stepping, threads/stackTrace/scopes/variables, disconnect); adapters spawn
  through the LSP transport. `internal/lang` gains `DebugAdapterProvider`;
  Python contributes debugpy (`python -m debugpy.adapter`). Go's `dlv dap`
  needs a socket transport and stays deferred.

- Breakpoints (0350, #577): per-project store (`internal/debug`,
  `.ike/breakpoints.json`), toggled via ctrl+f8 or a gutter click, rendered
  as a bold error-tone line number (wins over diagnostics/VCS marks), and
  shifted on edits like folds.

- Run current file (0350, #576): `run.file` (shift+f10, Run menu) and
  `run.rerun` launch the active file's run configuration in a terminal;
  reusable terminals are taken over, else the new `run.placement` setting
  (`in_pane` terminal tab / `new_terminal` bottom pane) decides; run panes
  survive process exit and show the exit code.

- Run-configuration model (0350, #575): `internal/run` store persisted at
  `.ike/runconfigs.json` with default synthesis on first run (`EnsureFor`)
  and rerun-last tracking; `internal/lang` gains the `RunCommandProvider` /
  `ModuleResolver` seams; Python (`-m` package detection), PHP and Go
  contribute run commands.

- Terminal command sessions (0350, #574): `StartCommandSession`/`NewCommand`
  run a program directly on the PTY with a kept exit code and a
  `[process exited with code N]` completion line; `Model.Occupied()` input
  tracking, `Model.StartCommand` in-place session take-over, and
  `Registry.ReusableRunTerminal()` for run reuse.

- Editor-pane tabs generalized to host terminals (0350, #573): `pane.Tab` sum
  type over editor/terminal, `AddTerminalTab` + `terminal.newTab` command,
  terminal-context key/mouse routing while a terminal tab is active, `⌨` tab
  labels, sessions end with their tab; terminal tabs are session-local (not
  persisted). Groundwork for run configurations (Epic #572).

## 2026-07-13

- Toolchain page streamlined for Python (#569, PyCharm-style): provenance
  badge per interpreter (`uv venv`/`venv`/`uv managed`/`pyenv`/`system`, from
  `pyvenv.cfg`'s uv stamp + path heuristics), `i` lists installed packages
  with versions (uv pip list / pip list fallback, inline + scrollable), and
  `n` became a guided create wizard: tool (uv / venv) → Python (uv version or
  base interpreter) → target directory, honored as `uv venv --python <v>` /
  `<base> -m venv`.
- Keymap defaults aligned to a user's JetBrains macOS keymap export: tab
  cycling moved to `ctrl+cmd+arrow` primaries with `ctrl+alt+arrow` secondaries
  (was `ctrl+pgup/pgdown` + `alt+home/end`), `project.switch` moved to
  `cmd+shift+p` (+ `ctrl+shift+p`; was `alt+shift+p`), and `editor.lineStart`
  gained a `home` binding. Cmd/Option chords only fire in a terminal that
  forwards the modifiers (Ghostty + Kitty protocol); the palette is the
  documented fallback for the now-fragile tab commands.

- Dependency-file edit guard (#565): a buffer under a dependency directory
  (`.venv`/`site-packages`/`node_modules`/`vendor`/…) — e.g. a go-to-definition
  jump into a library — opens read-only. The first edit is blocked, a floating
  confirmation appears, and accepting unlocks the file for the session and
  replays the blocked edit; declining leaves it untouched. Guards sit at the
  editor's mutation entry points with a locked-recorder safety net, so no
  vendored file is modified without confirmation. New `internal/editor/depedit.go`
  + `internal/app/depedit_prompt.go`; `history.Recorder.Lock`.

- LSP workspace configuration (#563): the client now advertises
  `workspace.configuration`, so a server (pyright) issues `workspace/configuration`
  and receives the toolchain-detected Python interpreter path. Previously the
  capability was unset, the server never asked, and venv imports (e.g.
  `import fastapi`) resolved against the system interpreter and showed as errors.
  The server is also registered before `initialize` so a request arriving on
  `initialized` is answered rather than dropped.

## 2026-07-12

- Revert history (#556): `vcs.revertFile` snapshots the pre-revert content
  into a persisted per-file log (state store, capped + age-pruned) before the
  checkout; `vcs.undoRevert` (`space v z`) lists the snapshots in a palette
  picker and re-applies the chosen one to the buffer as one undoable change.
  The revert prompt no longer claims "This cannot be undone."

- `vcs.revertHunk` (#555): JetBrains "Rollback Lines" — the contiguous change
  under the caret (matching the gutter diff markers, deletion anchors
  included) reverts to its HEAD content as a single undo-tree edit, so plain
  editor undo restores it. Leader `space v h`.

- Terminal OSC ghost text (#561): OSC 0/2 titles containing runes whose
  UTF-8 encoding carries the byte `0x9C` (U+2700 dingbats — Claude Code's
  `✳` spinner titles) terminated the sequence mid-rune and printed the rest
  of the title into the grid as ghost cells. The parser table now keeps raw
  `0x9C` as OSC payload (`internal/terminal/oscpatch.go`); BEL and `ESC \`
  still terminate.

- Move-drag engage threshold (#559): a plain title-bar click flashed the
  move overlay (status hint, source marker, ghost). A move or tab drag now
  stays latent until the pointer travels one row or `moveEngageCols`
  columns from the press cell; below that, release is a plain focusing
  click and nothing renders or persists.

- Custom-page footer wrap (#553): the Toolchain/Keymap/Language-Servers
  footer hints were single clipped lines (narrow windows showed "· u u" for
  "u uv install"). A shared `wrapFooter` helper word-wraps footer lines to
  the column width at a constant per-page line count (Toolchain 3, LSP 3,
  Keymap 2), keeping the #537 no-jumpiness invariant.

- Settings description footer wrap (#549): the pinned help line clipped long
  descriptions at the column edge. The footer is now a constant two lines
  with word-wrapped description + key (ellipsis beyond that); validation
  errors take the first line. Height stays constant, so the #535
  no-jumpiness invariant holds.

- uv project scaffolding (#548): create-environment on the uv path now
  generates a missing `pyproject.toml` (`uv init --bare`) and a missing
  `uv.lock` (`uv lock`) around the venv creation — best effort, existing
  files untouched, toast names the scaffolded files.

- Venv target directory (#547): the toolchain page's create-environment
  action (`n`) always built `<root>/.venv`. It now opens a path-completed
  input pre-filled with `.venv`; relative targets resolve against the project
  root, absolute and `~` targets are honored.

- Ex cmdline path completion (#543): `tab` on `:e <partial>` / `:w <partial>`
  (also `:wq`/`:x`) extends the path argument via `internal/pathcomplete`;
  ambiguous matches render as a dim hint after the cursor and typing narrows
  them. Inert on non-path commands and the search line.

- Project picker path browsing (#542): a path-shaped picker query (`/…`,
  `~/…`, `./…`) now lists matching directories as selectable `Open <dir>`
  items and tab-completes to the longest unambiguous prefix
  (`pathcomplete.Dirs`). The palette gained an optional `Completer` mode
  seam: tab asks the active mode to extend the query body.

- Path completion in settings inputs (#541): typing a filesystem path (the
  Toolchain custom-path input, schema `Path` entries) required knowing the
  exact path. A new shared engine `internal/pathcomplete` (candidates +
  longest unambiguous extension, `~` preserved, case-insensitive fallback,
  dirs-only flavor) now powers shell-style tab completion with a live
  suggestion list; the settings-local `expandHome` delegates to
  `pathcomplete.Expand`. Project picker (#542) and ex cmdline (#543) follow.

- Go interpreter detection (#538): the Toolchain page showed "(not found)"
  for Go at /opt/homebrew/bin/go when PATH lacked it — the Go plugin had no
  InterpreterDetector and the generic picker only did a PATH lookup. Go now
  ships a PHP-style detector (PATH, then well-known install locations), and
  the picker's default branch probes the same directories.

- Custom settings pages footer (#537): Toolchain, Keymap and Language Servers
  rendered hints/details inline under the selected row (same jumpiness #535
  fixed on the schema pages) and never scrolled to follow the selection. A
  shared `pinFooter` helper now pins the header top and a constant-height
  footer (hints, failure detail, env status, override input) bottom; the list
  windows with `follow` so the selection stays visible.

- Settings detail footer (#535): the selected entry's description used to
  render inline under the selected row, so ↑↓ inserted/removed a line
  mid-list and shifted every row below the selection. The description (and
  validation error) now lives in a footer pinned to the bottom of the form
  column; list rows map 1:1 to screen lines (mouse click math simplified),
  only the enum picker still expands inline.

- Settings arrow-left (#533): ← on an enum row (Appearance → Theme) used to
  prev-cycle the value, so arrow keys could not leave the form column and
  every press wrote config. ←/h now always return to the category column;
  the quick-cycle stays on →/l (wrapping) and enter still opens the picker.
- Keymap-page filter input (#531): type-to-filter shared its keyspace with the
  single-letter actions, so a search term containing `u`/`r`/`j`/`k` fired
  them instead of typing (`r` silently reset the selected binding). `/` now
  opens an explicit filter input (like the schema pages) that captures every
  printable key verbatim; enter keeps the filter, esc clears it, and the
  actions work on the filtered rows afterwards.
- Quit-key crash on tool panes (#529): `q` on a focused diff / preview / VCS
  pane nil-dereferenced the missing editor in `app.quitKey` and took the whole
  IDE down. Those panes no longer quit on `q`, and a diff pane in edit mode
  (#496) now counts as text-capturing, so `q`/`?`/`tab` typed into its
  embedded editor reach the buffer instead of the global layer.
- Completion auto-trigger (#527): completion now fires on the server's
  advertised trigger characters (PHP `->`/`::`/`$`, not just a hard-coded
  `.`; `.` stays the fallback while capabilities are unknown) and as-you-type
  on identifier characters (new `lsp.completion_auto` toggle, default on,
  settings `C` key). Auto-closed characters trigger too, and the popup
  anchors at the identifier start so the already-typed prefix filters.
- Parameter info inside string arguments (#525): an empty signatureHelp
  answer retries once at the string literal's opening delimiter — gopls
  answers null inside string literals, so `t.Error("abc")` with the cursor on
  the string now shows the popup.
- Parameter info on demand (#523): new `lsp.parameterInfo` command
  (`ctrl+p`, `cmd+p` on Cmd-forwarding terminals) opens the signature-help
  popup in insert and normal mode; the popup now lists every parameter with
  the active one marked `▶` and follows cursor motion. Inlay hints default
  off (settings `I` toggle), the automatic signature popup gets a
  `lsp.signature_auto` toggle (`S`), and `palette.toggle_key` defaults to
  empty — the palette's primary entry stays esc-esc.
- Auto-closing quote pairs (#521): `"`, `'` and `` ` `` pair like brackets
  under `editor.auto_close_pairs`; the same quote at the cursor is skipped
  over, no pair after a word rune or the same quote (apostrophes), backspace
  inside an empty quote pair deletes both.
- Enter between a bracket pair opens a block (#518): `{|}` splits into three
  lines — closer on its own line at the reference indent, caret on the
  smart-indented middle line. Gated on `editor.auto_indent`, per caret.
- Auto-closing bracket pairs (#517): typing `(`/`[`/`{` in insert mode inserts
  the matching closer with the cursor between (only when before EOL,
  whitespace, or a closer), typing a closer at the cursor skips over it, and
  backspace inside an empty pair deletes both runes. New setting
  `editor.auto_close_pairs` (default on), per-caret, all file types.
- Diff viewer v2 (Epic 0340, #493): collapsed context with expandable gaps
  (`c`/`o`, config `diff.context`), F7/shift+F7 change navigation
  (diff-scoped bindings), and an editable current side for worktree-backed
  diffs (`e` mounts a live shared-document editor as the right column,
  `ctrl+e` returns). Fixes: merge commits expand their first-parent files in
  the VCS log (#489), diff panes no longer split the bottom tool window
  (#489), and saved layouts containing diff panes survive restarts (#490).
- VCS tool window (Epic 0330, #480): persistent `vcs.panel` pane
  (`internal/vcspanel`, `space v v`) as a bottom split with terminal-style
  toggle — Changes tab (staging list, diff-on-enter, commit with the message
  draft shared with the modal dialog via `vcs.MessageDraft`) and Log tab
  (windowed `git log` with paging, commit details via `ShowCmd`, per-file
  parent-vs-commit diffs via `FileAtCmd`). New pane kind `KindVCS` with
  layout persistence; log reloads after commit/update/checkout. VCS doc
  updated.
- VCS / Git integration (Epic 0320, #461, from idea #28): new `internal/vcs`
  package — async porcelain-v2 status snapshot (debounced refresh off watcher
  events and saves) behind JetBrains-style explorer status coloring, a
  `⎇ branch ↑n ↓m` status-line segment, gutter diff markers against HEAD,
  and toggleable inline blame on the cursor line. Commands: `vcs.commit`
  (commit dialog `internal/commitui`: stage toggles + message pane),
  `vcs.updateProject` (pull merge/rebase with summary), `vcs.revertFile`
  (confirmed rollback to HEAD), `vcs.branches` (palette picker + checkout),
  `vcs.diff` (buffer vs HEAD in the diff viewer), `vcs.blameLine`. Leader
  family `space v c/u/x/b/d/a`; the blocked-bindings ledger emptied. Five new
  theme slots (`VCSModified/Added/Untracked/Deleted/Conflicted`). New
  [VCS doc](/architecture/vcs.md); editor/explorer/status-line docs updated.
- Vim macros (#58): `q{a-z}` records every keypress (mode-agnostic tap in
  `editor.Update`), `q` stops, `@{a-z}` / `@@` replay with count support
  (`5@a`). Payload is the keystroke list, kept per view beside the register
  store; replay feeds keys back through `Update` with a depth-capped recursion
  guard, and replayed keys are never re-recorded (nested `@` stays literal).
  New `recording @x` status-line segment. Editor and status-line docs updated.
- Undo tree (#59): `internal/editor/history` turned from linear past/future
  stacks into a change tree — an edit after an undo branches instead of
  discarding the redo chain; `u`/`ctrl+r` walk the active branch (unchanged
  feel), `g-`/`g+` step chronologically across branches, and the new
  `internal/undotree` overlay (palette `editor.undoTree`) renders the tree
  with timestamps/previews/current/saved marks, `enter` restoring any state.
  1000-node per-buffer cap pruning oldest branches first. Persistent-undo
  wire form now serializes the tree (`nodes` + `current`); legacy
  `past`/`future` snapshots still restore. Editor doc updated.
- Diff viewer pane (#60): new `internal/diff` package — reusable read-only
  diff component as a fifth `pane.Kind` (`KindDiff`). Line-level Myers engine
  with rune-level intra-line refinement and hunk grouping; side-by-side
  (default) or unified (`u`) rendering with dual line-number gutters and
  editor-style cell-budget wrapping; `n`/`N` hunk navigation, `enter` jumps
  the editor to the hunk; `diff.files` palette command picking two files via
  the `@` finder; three new theme ui slots (`DiffAdded`/`DiffRemoved`/
  `DiffChanged`, defaulted by `theme.Mix` tinting for sparse themes); layout
  persistence `{kind: "diff", path, path2}`. Shared infrastructure for #28,
  #35, #53. New concept doc [Diff Viewer](/architecture/diff-viewer.md).

- TODO index (#61): new `internal/todoindex` package — JetBrains' TODO tool
  window as a centered overlay over the reusable locations list. `todo.list`
  command (`cmd+6`, leader `space D`, palette); own `search.Service` scan
  (results wrapped in `ScanMsg` to stay out of the finder's generations);
  full scan at Init/project switch, single-file rescan on buffer save
  (spliced in place); in-memory tag and current-file filters (`ctrl+t`,
  `ctrl+o`); `[todo] patterns` config; "12 TODOs" status-line segment. New
  concept doc [TODO Index](/architecture/todo-index.md).

- Markdown preview pane (#62): new `internal/preview` package rendering
  markdown buffers to ANSI via glamour, split beside the editor as a fourth
  `pane.Kind`. `markdown.preview` command (`cmd+k m`, leader `space P`,
  palette) opens it; edits re-render debounced (200ms) off the shared-document
  sync seam; the preview follows the editor cursor via heading anchors;
  styling maps the active palette (dark/light + accent/info slots); layout
  persistence restores previews from disk. New concept doc
  [Markdown Preview](/architecture/markdown-preview.md); keybindings matrix
  gains the `markdown.preview` row.

- EditorConfig support (#63): new `internal/editorconfig` package (spec
  parsing, glob matching with `**`/`{a,b}`/`{n1..n2}`, upward search stopped
  by `root = true`, per-directory cache invalidated via the file watcher).
  Resolved settings are a per-buffer override layer — defaults < IKE config <
  `.editorconfig` < explicit user action — mapping `indent_style`,
  `indent_size`/`tab_width`, `trim_trailing_whitespace`,
  `insert_final_newline`, `end_of_line` and `charset` onto existing editor
  behaviour. New `indent` status segment ("Spaces: 2"), new config key
  `editor.editorconfig` (default true). New doc
  [editorconfig.md](/architecture/editorconfig.md).
- View options (#64): soft wrap (`editor.wrap` / `view.toggleWrap`; visual-row
  map in `internal/editor/viewport/wrap.go`, gj/gk-style j/k, wrapped scroll,
  click mapping, `↪` continuation gutter), visible whitespace
  (`editor.show_whitespace = none|trailing|all`, now an enum), indent guides
  (`editor.indent_guides`) and column rulers (`editor.rulers = [80]`). New
  theme slots `Whitespace`, `IndentGuide`, `Ruler`. Palette toggles override
  the config per view.
- Encoding & line endings (#66): new `internal/textenc` package (BOM/UTF-8
  detection, UTF-16 LE/BE + ISO 8859-1 + Windows-1252 transcoding via
  `golang.org/x/text`). Load detects and save re-applies the on-disk
  line-ending flavor and encoding, so CRLF/BOM/UTF-16 files round-trip
  byte-identically; mixed line endings warn on the ex line. New `eol` +
  `encoding` status segments, `file.setLineEndings.*` / `file.setEncoding.*`
  palette commands (conversions mark the buffer dirty), new config key
  `files.encoding` (fallback for BOM-less non-UTF-8 files). Updated
  [editor.md](/architecture/editor.md) and
  [status-line.md](/architecture/status-line.md).

- Status line segments (#101): the editor status line became an extensible
  left/right slot model (`internal/app/statusline.go`) replacing string
  concatenation. Two new segments: the focused buffer's effective interpreter
  (venv name or binary name via the shared `lang.Interpreter` resolution,
  cached per language, invalidated on config reload) and an unseen
  notification counter (`● N`, reset by `notifications.history`). New doc
  [status-line.md](/architecture/status-line.md).

- Plugin marketplace (Roadmap 0310, #444-#446): new `internal/market` package
  (static HTTPS `index.json` catalog with strict per-entry validation, install
  engine with SHA-256 verification and atomic .wasm+manifest writes pinning
  the reviewed capability list) and a Marketplace settings page (browse,
  capability review before install, install/update/remove, restart notice).
  New config key `marketplace.catalog_url`; new doc
  [marketplace.md](/architecture/marketplace.md).

- Code folding (#144): collapse a function body, block, import list or
  multi-line comment behind its header line. Fold ranges come from the same
  Tree-sitter parse as the highlight spans (`SpansMsg.Folds`, kinds via
  `lang.Language.FoldNodes`, `ScopeNodes` fallback); the collapsed set is
  per-view (#142). A closed fold renders as one row (header + dimmed
  `⋯ N lines` placeholder) and counts as one row for `j`/`k`, clicks and
  wheel scrolling; jumps into a fold auto-unfold, overlapping edits dissolve
  it, reparses reconcile it. Keys `za zc zo zM zR` + `editor.fold.*` palette
  commands. See [editor](/architecture/editor.md),
  [highlighting](/architecture/highlighting.md).

- Multi-caret editing (#145): a primary caret plus secondary carets fan every
  edit out — insert-mode typing/kills, `x r`, `d c y` with motions and text
  objects, `dd cc yy`, `p/P`, `o/O`, completion — as **one undo unit**.
  Created via `ctrl+g` (add next occurrence), `ctrl+shift+g` / `space G`
  (all occurrences), `alt+click` (toggle), visual block `I`/`A`; Esc
  collapses. Carets are per-view (#142) and re-clamp on reload/sync.
  See [editor](/architecture/editor.md).

- Persistent undo (#148, vim's `undofile`): undo/redo stacks survive a
  restart. New `internal/undostore` keeps one hash-keyed JSON file per
  document under `.ike/undo/`, written on save/close/quit (clean buffers
  only) and adopted on `Load` when the stored content hash matches the
  just-read file; any mismatch discards silently. Shared documents load
  once; `files.persistent_undo` (default on) toggles it; 1 MiB per-file and
  200-file LRU caps. See [editor](/architecture/editor.md),
  [session-restore](/architecture/session-restore.md).

- Large-file mode (#149): files over `files.large_file_kb` (default 1024) or
  `files.large_file_lines` (default 100000) degrade gracefully instead of
  stalling — highlighting off, LSP `didOpen` skipped, change events without
  text, watcher poll never content-hashes — with a warn toast, a
  `[large file]` status segment, and the `editor.forceCodeInsight` palette
  override per document (policy shared via new `internal/largefile`). See
  [editor](/architecture/editor.md),
  [highlighting](/architecture/highlighting.md),
  [lsp](/architecture/lsp.md).

- Sticky scroll (#168): the header lines of the declarations enclosing the
  first visible line pin as the pane's top rows (JetBrains-style), collected
  by the same Tree-sitter parse as the highlight spans via new
  `lang.Language.ScopeNodes` per language; clicking a pinned row jumps to the
  declaration, `editor.sticky_scroll` toggles it and
  `editor.sticky_scroll_depth` caps the nesting. See
  [editor](/architecture/editor.md),
  [highlighting](/architecture/highlighting.md).

- File templates (#170): newly created files start with language-aware
  content — `package ${PACKAGE}` for Go, `<?php` for PHP — rendered by
  `lang.TemplateFor` with `${FILENAME}/${NAME}/${DIR}/${PACKAGE}/${DATE}/${YEAR}`
  variables and overridable per language via `[lang.<id>] template`. Applies
  to explorer creates (written to disk), `:e` on a new path, and CLI opens of
  missing files (seeded, unmodified buffers). See
  [languages](/architecture/languages.md).

- Inlay hints (#171): inline parameter-name and inferred-type hints
  (`textDocument/inlayHint`) render as dimmed italic virtual text via the new
  `InlayHint` theme slot, refreshed document-wide on open/change and merged
  from embedded fragments; `lsp.inlay_hints` toggles them (default on), and
  the Go plugin enables gopls's parameter/type hint kinds by default. See
  [lsp](/architecture/lsp.md).

## 2026-07-11

- Document highlight (#172): occurrences of the symbol under the cursor are
  marked with a subtle background (read cool / write warm via the new
  `OccurrenceRead`/`OccurrenceWrite` theme slots), debounced 150 ms on cursor
  moves; fragment positions route to the fragment's server. See
  [lsp](/architecture/lsp.md).

- Fragment diagnostics (#415): diagnostics published on fragment documents
  now map back onto the host buffer and merge with the host server's own
  diagnostics into a single host-path publish (`manager/fragdiags.go`).
  Fragment diagnostics follow the fragment when host edits move it and clear
  immediately when the fragment closes or its language is stopped. See
  [lsp](/architecture/lsp.md).

- Fragment references/definition (#416): definition and references requests
  inside an embedded fragment now route to the fragment's server; result
  locations in fragment documents are rewritten to host-file locations
  (fragment URIs never reach the editor), real-file locations pass through,
  stale fragment locations are dropped. See [lsp](/architecture/lsp.md).

- Floating shell stale body (#409): `ui.Floating.View()` now re-renders its
  content body on every call (scroll offset preserved via `scroller.Refresh`),
  so modals that mutate state in place — the crash-recovery prompt's cursor and
  item removal — update on the next frame. The onboarding dialog's per-key
  `SetSize` workaround (#301) is removed. See
  [floating-shell](/architecture/floating-shell.md).

- Rename feedback (#426): a server that lacks the rename capability
  (intelephense free) now toasts "language server does not support rename"
  (`manager.ErrRenameUnsupported`) instead of the misleading "cannot rename
  here". See [lsp](/architecture/lsp.md).

- Comment toggling (#428): line-comment markers align with the comment on
  the line above the range (fallback: min indent, clamped to each line's
  indent), and blank lines now get a padded bare marker instead of being
  skipped — uncommenting empties them again. See
  [editor](/architecture/editor.md).

- Finder mouse support (#424): the find/replace-in-path overlay is now
  mouse-operable — click outside dismisses, clicks focus input rows, flip
  the Case/Word/Regex toggles, select result rows (press again to open),
  and the wheel scrolls the list. New `locations.List` seams: `ItemAt`,
  `SetCursor`, `Cursor`. See [search](/architecture/search.md).

- Finder ctrl chords (#422): every alt binding in the find/replace-in-path
  overlay gained a ctrl primary (`ctrl+c/w/x` toggles, `ctrl+f`/`ctrl+a`
  batch replace, `ctrl+enter` navigate, `ctrl+up/down` history) — on macOS
  Option composes characters, so the alt chords never arrived. Alt stays as
  secondary. See [search](/architecture/search.md).

- LSP call hierarchy (#173): `lsp.callHierarchy` (`ctrl+alt+h`, leader `H`)
  prepares the symbol under the cursor and opens a lazily-expanding
  callers/callees tree overlay (`internal/callhier`); `tab` flips the
  direction, `enter` jumps to the call site / declaration. See
  [LSP](/architecture/lsp.md).

- Tree-sitter language injections (#299): embedded fragments (SQL in Python
  strings) now highlight with the fragment language's own grammar. The SQL
  plugin gained a Tree-sitter grammar (DerekStride/tree-sitter-sql), so .sql
  files highlight too. See [highlighting](/architecture/highlighting.md).

- Embedded-language LSP via virtual documents (0300, #412–#414): Tree-sitter
  injection queries detect fragments (SQL in Python strings, capture
  convention `fragment.<lang>[.guess]`), the manager mirrors each into an
  `ike-fragment:` document on the fragment language's server and routes
  completion/hover inside the fragment there, mapping positions both ways.
  New `sql` language plugin (sql-language-server). Diagnostics (#415) and
  references (#416) follow. See [LSP](/architecture/lsp.md).

- First-start LSP onboarding (#301): a one-time dialog on the very first
  launch (no user config yet) lists the servers with install recipes as
  checkboxes; enter batch-installs the checked ones via `lsp.installMissing`,
  unchecked ones persist disabled, esc skips, `lsp.onboarded = true` keeps it
  from returning; `lsp.auto_install = false` suppresses it entirely. See
  [LSP](/architecture/lsp.md).

- Diagnostic navigation: `lsp.nextDiagnostic` / `lsp.prevDiagnostic` (f2 /
  shift+f2, JetBrains parity) step the cursor through the focused document's
  diagnostics in document order with wrap-around, toasting the message (#369).
  See [LSP](/architecture/lsp.md) and
  [Keybindings](/architecture/keybindings.md).

- New built-in theme `dracula` (official Dracula spec), AA-contrast checked
  (#385). See [Themes](/architecture/themes.md).

- New built-in themes `solarized-dark` / `solarized-light` (Ethan
  Schoonover's Solarized), AA-contrast checked (#386). See
  [Themes](/architecture/themes.md).

- New built-in theme `one-dark` (Atom's One Dark), AA-contrast checked
  (#387). See [Themes](/architecture/themes.md).

- New built-in theme `kanagawa` (wave variant of rebelot/kanagawa.nvim),
  AA-contrast checked (#388). See [Themes](/architecture/themes.md).

- f3/shift+f3 repeat a committed in-file search (`/`, `?`, cmd+f) like `n`/`N`
  while it is the most recent search; a new find-in-path scan reclaims them
  (#376). See [Project Search](/architecture/search.md).

- Go-to-symbol / search everywhere ranks project symbols above
  dependency/stdlib results, exact name match on top (#377). See
  [LSP](/architecture/lsp.md).

- `lsp.hover` (quick documentation) gets a delivered default chord (#378):
  `ctrl+q` (JetBrains Windows/Linux quick doc; XON is disabled in raw mode)
  plus the `space k` / `ctrl+k k` leader path (vim's K keyword lookup). See
  [Keybindings](/architecture/keybindings.md).

- Hover popup renders LSP markdown instead of showing it raw (#379): ```` ```go ````
  fence markers are stripped, the fenced signature block is syntax-highlighted
  via the language registry (accent tint when the fence tag has no grammar),
  and `---` draws as a horizontal rule. See [LSP](/architecture/lsp.md).

- Status line's LSP server segment is scoped to the focused buffer's language
  (#380): `ServerState` messages are tracked per language and the segment shows
  only the focused buffer's entry — non-LSP buffers show no server text, and
  stale event text no longer sticks globally. `host.SetStatus` stays as the
  plugin-facing global segment. See
  [Notifications](/architecture/notifications.md).

- Status line names the focused pane kind (#381): a focused terminal shows
  `TERMINAL │ shell · dir` (`[exited]` when dead), the explorer shows
  `EXPLORER`; editor mode/file/cursor render only while an editor holds
  focus. See [Integrated Terminal](/architecture/terminal.md).

- Settings window QoL pass (#383): ←→/h/l switch between the category column
  and the form (arrow-left only on custom pages, `h` stays page filter text);
  both columns scroll with the selection instead of truncating; enum entries
  open a picker list on enter (←/→ still quick-cycle on the row); the
  unfocused column dims its selection so focus is unambiguous; filtered
  results name the custom pages the filter cannot search. See
  [Settings UI](/architecture/settings-ui.md).

- Auto-installed language servers start without PATH surgery (#370):
  `transport.Resolve` probes `go env GOBIN` / `GOPATH/bin` and npm's global
  prefix after `exec.LookPath` fails and launches the server via absolute
  path, so a fresh `go install`ed gopls works immediately; the install
  success toast now fires only after the binary actually resolves, otherwise
  an error toast names the probed directories. See
  [LSP](/architecture/lsp.md).

- LSP actions no longer use a stale cursor after programmatic jumps (#371):
  `editor.SetCursor` now emits a cursor-move event, so go-to-definition /
  usages-pick / nav back-forward landings update the LSP bridge's tracked
  position and rename/references immediately act on the landed symbol. See
  [LSP](/architecture/lsp.md).

- LSP request errors surface as toasts (#372): a failing hover / definition /
  references / formatting / code-action request now raises an error toast with
  the server's message ("find usages failed: …") instead of silently doing
  nothing, via the shared `requestFailed` seam in `plugins/lsp/bridge.go`. See
  [LSP](/architecture/lsp.md).

- Explorer prompts never render invisibly (#373): a rename/delete prompt box
  wider than the pane used to be silently dropped while still capturing keys
  (blind renames/deletes). `promptBox` now truncates the title and windows the
  input row to the pane width, and `View` overlays via `overlay.Place` (clips)
  instead of `overlay.Center` (drops). See
  [explorer — file operations](/architecture/explorer.md).

- Palette-invoked explorer file ops focus the explorer (#374): dispatching
  `NewFileMsg`/`NewDirMsg`/`DeleteMsg`/`RenameMsg` from the command palette now
  moves focus to the explorer pane first (re-showing a hidden tree), so the
  modal prompt captures every typed key instead of leaking vim commands into
  the focused editor buffer. See
  [explorer — file operations](/architecture/explorer.md).

- Theme contrast audit (#384): all built-in themes now pass WCAG AA (≥ 4.5:1)
  on the rendered fg/bg slot pairs, enforced by the new table-driven
  `TestBuiltinThemeContrast`. Light themes (gruvbox-light, rose-pine-dawn,
  catppuccin-latte) had their accent/diagnostic slots darkened; the default
  theme's near-invisible `Error`/`Info`/`Hint`/`Warning` were lifted.
  Selected-row renderers (settings pages, pickers) now always set
  `Foreground(SelectionText)` with a `Selection` background instead of
  inheriting the terminal default. See
  [themes — contrast rule](/architecture/themes.md).

- 0082 sheet 11+13 verdicts (#18): `f4` is the delivered primary for
  `lsp.definition` (JetBrains jump-to-source; `cmd+b` stays secondary), and
  `shift+f6` is context-aware refactor-rename — `lsp.rename` in an editor,
  `file.rename` in the explorer. Label/matrix primary selection now prefers
  fewer keystrokes before shorter strings, so single-step chords beat leader
  sequences. Matrix regenerated. See
  [keybindings](/architecture/keybindings.md).

- Workspace edits (rename/format/code actions) apply once per document, not
  once per view (#366): `FormatEditsMsg` now goes through exactly one view of
  a shared document; per-view routing hit the aliased buffer N times when the
  file was open in a second tab/split. See [lsp](/architecture/lsp.md).

- Rename no longer applies edits twice (#364): `WorkspaceEdit.AllChanges`
  prefers `documentChanges` over `changes` per spec instead of merging both —
  pylsp sends the same edits in both fields, corrupting the buffer
  (`z` → `match1` became `match1atch1`). See [lsp](/architecture/lsp.md).

- `readStdin` folded back into `cmd/ike/main.go` (#362) so the single-file
  invocation `go run cmd/ike/main.go` compiles again; `cmd/ike/stdin.go`
  deleted. See [foundation](/architecture/foundation.md).

- Zen mode (#359, Roadmap 0290 — epic complete): `view.zenMode`
  (`cmd+k shift+z`, View menu — the dormant entry is live now) maximizes the
  active editor and hides the tab bar + status line; leaving zen restores
  both, tree mutations drop it like the zoom. See
  [Pane Layout](/architecture/pane-layout.md).

- Pane maximize (#358, Roadmap 0290): `pane.maximize` (`cmd+k z`, View menu,
  palette) zooms the focused pane tmux-style — it renders alone over the body
  while the split tree survives untouched; any leaf-set change (split, close,
  relocation) auto-unzooms via a signature check in `layout()`. Not
  persisted. Documented in [Pane Layout](/architecture/pane-layout.md);
  zen mode follows as #359.

- Paste from History (#57): every yank/delete feeds a bounded 20-entry
  register history; `editor.pasteFromHistory` (`cmd+shift+v`, Edit menu,
  palette) picks an entry from a palette list (first line + size, fuzzy
  filter) — it becomes the current clipboard and pastes with Cmd+V
  semantics. See [Editor](/architecture/editor.md); matrix regenerated.

- Scratch picker (#352, Roadmap 0280 — epic complete): `scratch.list`
  ("Open Scratch File…", palette + File menu) locks the palette to a new
  scratch mode — newest-first, fuzzy filter, enter opens; empty store shows
  an inert hint row. See [Scratch Files](/architecture/scratch-files.md).

- Scratch files land (#350/#351, Roadmap 0280): `scratch.new` and per-language
  "New Scratch File: <Lang>" palette commands (File menu too) create
  `scratch-N.<ext>` under `$IKE_CONFIG_DIR/scratches` / `~/.ike/scratches`
  (new `internal/scratch` store) and open it focused through the standard
  funnel — highlighting/LSP/session flow from the extension. Documented in
  [Scratch Files](/architecture/scratch-files.md); picker follows as #352.

- Split view (#147): `editor.splitViewRight`/`Down` (`cmd+k shift+right`/
  `shift+down`, View menu, palette) split the focused editor into a second
  live shared view of the same document (#142), cursor/scroll copied from the
  source, new view focused; file-less editors no-op with a toast. Documented
  in [Editor](/architecture/editor.md); keybinding matrix regenerated.

- `command | ike -` pipes stdin into a scratch buffer (#344, Roadmap 0270):
  read to EOF before the UI starts, opened focused after any file targets,
  dirty + never-saved so the quit guard prompts and `:w <path>` names it; the
  keyboard re-points at /dev/tty. `ike -` on a TTY fails fast. Roadmap 0270
  is complete.

- CLI open targets (#343, Roadmap 0270): `ike file.go:42`, `file.go:42:7` and
  vim-style `+42 file.go` open files as tabs at startup — first target focused
  with the cursor placed, explorer revealing it; a nonexistent path opens as
  an unsaved buffer. Session restore still runs first; CLI files win focus.
  Documented in [Foundation](/architecture/foundation.md); README usage updated.

- Shift+Tab in insert mode dedents the whole current line one indent unit
  (#337, Roadmap 0260) — the same unit `<<` removes — wherever the cursor
  sits, inside the open insert's undo unit; plain Tab keeps inserting one
  unit at the cursor (and still accepts an open completion popup).

- Enter and `o` gain language-aware smart indent (#336, Roadmap 0260): with
  `editor.auto_indent` on, a line whose trimmed text ends with a block opener
  (`lang.IndentAfter` — Python `:`, Go/PHP braces) opens the next line one
  `tabText()` unit deeper; Enter keys off the text left of the cursor, `O` and
  unknown languages keep plain copy-indent. Documented in
  [Editor](/architecture/editor.md).

- The language registry gains smart-indent metadata (#335, Roadmap 0260):
  `Language.IndentAfter` lists trimmed-line suffixes that open a block, resolved
  per buffer path via `lang.IndentAfter`. Python registers `":"` + open
  brackets, Go and PHP register `{ ( [`. Documented in
  [Language Registry](/architecture/languages.md).

- Files already open at startup now receive an LSP `didOpen` (#332): the session
  restore paths load editors directly (bypassing the interactive open), so
  `Model.Init` fires the file-open hook for each restored file — deduped per path
  for buffers shared across tabs (#142) — instead of leaving them without a
  server until reopened.

- Accepting an LSP completion no longer duplicates the already-typed identifier
  prefix (#330): the insert now replaces the identifier run before the cursor
  (`identifierStart`) rather than the request anchor, which is empty for a
  manual `ctrl+space` trigger (`xyz.__` + `__dict__` had produced `xyz.____dict__`).

- Tab cycling gains an `alt+home`/`alt+end` default pair, `alt+shift+home/end`
  to move the active tab (#328): on Macs without physical PgUp/PgDn keys,
  `fn+ctrl+arrows` is claimed by macOS globals, while `fn+option+left/right`
  arrives as exactly these chords. `ctrl+pgup/pgdown` stay bound.

- Shift+arrow selections stop on unshifted navigation (#326): a selection
  started with `Shift+arrows` is now GUI-style — releasing Shift and pressing
  a plain navigation key (arrows, `Home`/`End`, word/paragraph/page keys)
  drops the selection and moves the caret (vim's `keymodel=stopsel`), instead
  of extending it. Vim motions and `v`/`V`/`Ctrl+V` selections keep extending.
  Documented in [Editor](/architecture/editor.md).

- Center drop zone merges as tab (#318): during a move or tab drag an editor
  target now shows five zones — the four edges split/relocate as before, the
  interior center merges JetBrains-style: a whole-pane drop moves all source
  files into the target's tab list (deduped) and closes the source pane, a
  tab drop joins the list with that file; edge drops of a tab on an editor
  now split next to it. Feedback distinguishes the center (`⧉ merge as tab`
  marker, full-pane ghost). Documented in
  [Pane Layout & Drag](/architecture/pane-layout.md).

- LSP popups framed and freed from the pane clamp (#316): completion,
  signature and hover render inside a rounded themed border (like the
  floating shell) and may now overflow the owning pane — clamped to the
  terminal edges instead, still shifting left / flipping above the anchor;
  the width/height caps and ellipsis row stay as safety nets. Documented in
  [LSP](/architecture/lsp.md).

- Word-wise alt+arrow cursor motion (#303): `alt/opt+←/→` (and the delivered
  `ctrl+←/→` fallback) now move word-wise within the current line with `.` as
  a stop point; `shift+alt/ctrl+←/→` extend the selection the same way. The
  alt+arrow tab-cycling secondaries were freed for this — tab cycling keeps
  `ctrl+pgup/pgdown`. Documented in [Editor](/architecture/editor.md),
  [Editor Tabs](/architecture/editor-tabs.md) and
  [Keybindings](/architecture/keybindings.md).

- Tab drop next to terminals (#317): a dragged tab released on a non-editor
  pane's edge zone now splits that pane and opens the file in the fresh
  editor leaf; interior drops stay a no-op and the drag feedback (zone
  arrow, ghost) reflects it. Documented in
  [Pane Layout & Drag](/architecture/pane-layout.md).

- Terminal duplication on project switch fixed (#320): when layout restore
  already recreated a terminal under the carried session's key, the live
  session now takes over that pane instead of splitting a duplicate leaf
  (which mirrored one instance in two panels). Documented in
  [Terminal](/architecture/terminal.md).

- Signature popup lifecycle (#315): leaving insert mode and insert-mode
  arrow motion dismiss the popup, stale replies after esc are dropped — it
  no longer trails normal-mode cursor motion. Documented in
  [LSP](/architecture/lsp.md).

- Code-action clarity (#309): readable kind chips in the palette list, an
  explainer in the wiki, and feedback for every apply outcome — edited-N,
  changed-nothing, unresolved-action warning, command errors. Documented in
  [LSP](/architecture/lsp.md).

- ctrl+space triggers completion manually (#302): both the Kitty and the
  legacy NUL spelling emit the same trigger event the "." auto-path uses.
  Documented in [LSP](/architecture/lsp.md).

- LSP popup fixes from live use (#306, #307, #308): signature/hover popups
  clamp to the owning pane (width wrap, ellipsis row, shift/flip placement),
  mouse clicks dismiss cursor-anchored popups, the completion list shows an
  accept-keys hint and the signature popup a dim `ƒ` marker. Documented in
  [LSP](/architecture/lsp.md).

- Live workspace-symbol palette mode (#295, Epic 0250 phase 2): cmd+o now
  opens the palette locked to a live symbol mode — 150 ms debounced
  `workspace/symbol` re-query per keystroke (`palette.LiveMode` plumbing),
  symbol-name rows, stale replies dropped — and the same mode fills the
  search-everywhere seat (#236) via silent hook-priming. Replaces the
  phase-1 prompt. Documented in [LSP](/architecture/lsp.md) and
  [Command Palette](/architecture/command-palette.md).

- Workspace-symbol search (#294, Epic 0250 promoted from idea #146):
  `project.goToClass` (cmd+o / leader `S`) prompts for a query and lists
  `workspace/symbol` hits in the references palette (Enter navigates like
  go-to-definition); capability-gated with honest no-provider/zero-hit
  toasts. The last non-VCS blocked-ledger entry is gone. Documented in
  [LSP](/architecture/lsp.md); status matrix regenerated in
  [Keybindings](/architecture/keybindings.md).

- Find/replace panel (#283, Epic 0240 phase 2): `editor.replace` (cmd+r /
  leader `R`) now opens a two-field panel — Find with live incremental
  preview and match tally, Replace, tab to switch — finishing through the
  one substitute engine: ctrl+a replaces all ("N substitutions" report),
  enter enters the y/n/a/q/l confirm flow, esc cancels and restores the
  origin. The panel counts as capturing so global keys keep out of its
  inputs. Replaces the phase-1 ex-line prefill (#282). Documented in
  [Editor](/architecture/editor.md).

- 0082-review fixes documented (#289): blocked-chord toast (#267) and
  bracket-glyph canonicalisation (#284) in
  [Keybindings](/architecture/keybindings.md); canonical open paths / tab
  dedupe (#272) and the app-quit unsaved-changes guard (#287) in
  [Editor Tabs](/architecture/editor-tabs.md); visual-mode counts (#265) in
  [Editor](/architecture/editor.md); finder query preselect (#277) in
  [Project Search](/architecture/search.md); save-all no-op hint (#275) in
  [Notifications](/architecture/notifications.md).

- In-file replace, phase 1 (#282, Epic 0240 promoted from idea #49):
  `editor.replace` (cmd+r, leader `R`) opens the ex line prefilled with
  `%s/<pattern>/` (seeded from the committed search when literal and
  slash-free) and runs through the existing `:substitute` engine — flags,
  per-match confirm and single-undo included. The chord left the blocked
  ledger. Documented in [Editor](/architecture/editor.md); status matrix
  regenerated in [Keybindings](/architecture/keybindings.md).

- Multi-target go-to-definition picker (#279): several definition sites open
  the references-style palette list ("Definitions — pick a target…") instead
  of silently jumping to the first; one site still jumps directly. The
  location→reference conversion is now shared with find-references. From the
  0082 sheet 11 protocol (#18). Documented in [LSP](/architecture/lsp.md).

- Cheatsheet live filter (#271): typing in the help overlay narrows the
  bindings (titles + shortcuts, empty groups drop, title echoes the filter);
  `q`/`?` stay dismiss keys only while the filter is empty, esc clears then
  closes. Implements the last open item on 0082 sheet 27 (#21). Documented in
  [Help Overlay](/architecture/help-overlay.md).

- Explorer hide/show (#268): `explorer.toggle` (`space e` / cmd+1) now runs
  the JetBrains cmd+1 state machine — focused tree hides and editors reclaim
  the width, a hidden tree comes back at its remembered ratio and takes
  focus; the hidden state persists across restarts. Found running the 0082
  sheet 25 protocol (#21). Documented in
  [Explorer](/architecture/explorer.md).

- Search-everywhere follow-ups (#263): `space space` opens the mode (the
  terminal stand-in for JetBrains' double-shift, via the leader engine), and
  an empty query now lists the recent files first (active file excluded)
  before the command listing. From 0082 sheet 17 (#20). Documented in
  [Command Palette](/architecture/command-palette.md).

- Save feedback on the ex line (#261): `:w` / `cmd+s` report `"file" written`
  on success and a vim-style `E: <error>` on failure (previously silent);
  a failed write keeps the buffer dirty, aborts `:wq`, and a nameless
  scratch `:w` reports "E: no file name". Found running the 0082 sheet 14
  protocol (#19). Documented in [Editor](/architecture/editor.md).

- Unsaved-changes guard on close (#259): `cmd+w` / `ctrl+w` / `:q` on a dirty
  buffer now prompt save/discard/cancel instead of silently dropping the
  edits; `:q!` forces, shared documents skip the prompt, a failed save keeps
  the tab open. Found running the 0082 sheet 16 protocol (#19). Documented in
  [Editor Tabs](/architecture/editor-tabs.md).

- Smartcase search (#257): `/` `?` (and the incremental preview, `n`/`N`,
  ex `/pat/` addresses) fold case for all-lowercase patterns and stay exact
  once the pattern contains an uppercase rune; `*`/`#` remain exact-word,
  `:s` keeps its explicit `i`/`I` flags. Closes the last behavior item on
  0082 sheet 09 (#18). Documented in [Editor](/architecture/editor.md).

- Incremental in-file search (#255): the `/` line now previews live — per-
  keystroke jump to the nearest match, match-count tally on the input line,
  full-buffer match highlighting (current match underlined), exact
  cursor/viewport restore on Esc, and "no matches" / "search wrapped"
  ex-line hints. Normal-mode Esc clears highlights (`:noh`-style). Found
  running the 0082 sheet 09 protocol (#18). Documented in
  [Editor](/architecture/editor.md).

- Copy/cut feedback toasts (#252): `editor.copy` / `editor.cut` report what
  landed in the clipboard ("copied 1 line", "cut 5 chars") through the
  existing `NoticeMsg` toast path; vim-native `y`/`d` stay silent. Found
  running the 0082 sheet 03/04 protocols (#17). Documented in
  [Editor](/architecture/editor.md).

- Undo tracks the save point (#251): the history pins a checkpoint on save,
  and undo/redo clear the modified flag when they land exactly on it — `[+]`
  no longer sticks after undoing back to the saved content. Crash-restored
  buffers never read as clean. Found running the 0082 sheet 01/02 protocols
  (#17). Documented in [Editor](/architecture/editor.md).

## 2026-07-10

- Search-everywhere palette mode (#236): `cmd+shift+a` / double-shift (leader
  `A`) rank one query across commands and files by composing the existing
  command and file modes — per-kind cap, score interleave, kind glyph per row.
  `palette.searchEverywhere` left the blocked ledger. Documented in
  [Command Palette](/architecture/command-palette.md).

- Delivered tab chords (#248): `ctrl+pgup`/`ctrl+pgdown` cycle tabs and
  `ctrl+shift+pgup/pgdown` reorder them — delivered primaries for the
  alt-arrow chords that never arrive on macOS (Option composes characters).
  The reachability rules now exempt CSI-parameter keys from the ctrl+shift
  collapse. Documented in [Keybindings](/architecture/keybindings.md) and
  [Editor Tabs](/architecture/editor-tabs.md).

- Insert-mode backward kills (#246): `option+backspace` / `ctrl+w` delete the
  previous word, `cmd+backspace` / `ctrl+u` delete to the line start, all
  through the open insert session's recorder (one undo unit). Documented in
  [Editor](/architecture/editor.md).

- Defaults for palette-only commands (#242): `f3`/`shift+f3` step retained
  search matches, `alt+f1` reveals the open file in the explorer (fragile,
  palette fallback), leader `T` opens a new terminal and leader `h` the
  notification history. Status matrix regenerated in
  [Keybindings](/architecture/keybindings.md).

- Theme override survives config reloads (#241): `reloadConfig` no longer
  unconditionally re-resolves `[theme].name` — a palette-selected runtime
  theme now survives unrelated settings edits; an explicit `[theme].name`
  change still wins and clears the override. Documented in
  [Themes](/architecture/themes.md).

- Terminal word/line kill chords (#240): `motionKey` extends the #225 natural
  text editing set — `option+backspace` sends `ESC DEL` (backward-kill-word),
  `cmd+backspace` sends `ctrl+u` (kill to line start). Documented in
  [Integrated Terminal](/architecture/terminal.md).

- Wheel-event coalescing (#238): queued mouse-wheel events accumulate in the
  root model and apply in a single update pass via a scheduled `wheelFlushMsg`,
  so fast scroll bursts cost one render instead of replaying every stale event;
  any non-wheel message flushes the batch first to preserve input ordering.
  Documented in [Pane Layout & Drag](/architecture/pane-layout.md).

- Recent-files palette mode (#235, Roadmap 0230): `palette.recentFiles`
  (cmd+e / leader m / Navigate menu) opens the palette locked to an MRU file
  list — touched on every open and tab activation, persisted as
  `recent_files` in .ike/session.json, active file excluded so enter jumps
  to the previous file. The binding left the keymap blocked ledger.

- Editor horizontal scrolling (#230): horizontal wheel and shift+wheel scroll
  the editor viewport sideways (`editor.ScrollXBy`, wired in `app.handleMouse`
  like the explorer's), clamped so the longest visible line keeps its last
  character on screen; the cursor stays put.
- Counted undo/redo (#231): `{count}u` / `{count}ctrl+r` undo/redo count
  changes at once, stopping early when the history runs out.

- Terminal mouse selection + copy (#227): left-drag selects text on the grid
  (virtual-anchored, survives scrollback paging), cmd+c copies it to the
  system clipboard; clicks forward to mouse-reporting children instead.
- Terminal focus escape (#228): the spatial focus moves (default
  ctrl+arrows, keymap.bindings.focus_* overrides) now work from a focused
  terminal; the reserved-set table grew accordingly.

- Terminal wheel routing (#226): the mouse wheel now reaches the running
  application — forwarded as encoded mouse events when the child enabled
  mouse reporting, as arrow keys on the alt screen (less/man/vim), and it
  keeps paging ike's scrollback at the plain prompt.

- Terminal macOS editing chords (#225): option+left/right word-jump
  (ESC b / ESC f), cmd+left/right line start/end (ctrl+a / ctrl+e) — the
  iTerm "natural text editing" convention, translated in
  internal/terminal/model.go (motionKey).

- Terminal shifted-input fix (#224): the vt encoder drops non-special keys
  that still carry a modifier, so uppercase/caps-lock characters never
  reached the shell; the pane now replays shift/caps-lock/num-lock-only
  text presses as their produced text (`toVTKeys` in
  internal/terminal/model.go).

- Navigation history cross-pane polish (#220, Roadmap 0220, closes the
  epic): stale-entry skipping via BackWhere/ForwardWhere validity filter
  (deleted/renamed files are dropped silently, traversal continues, no
  duplicate departures on the opposite stack); back/forward acts in the
  active editor pane with split layouts. TUI usability pass over
  finder/definition jump chains incl. deleting a mid-chain file.
- In-editor jump sources (#219, Roadmap 0220): the editor emits EventJump
  (departure position) for large motions (gg, G, {count}G via
  motion.Result.Jump) and search landings (initial //? jump, n/N, */#
  via jumpTo); the app's editorEmitter records it into the shared
  history and swallows the event. Small motions and operator-composed
  motions (dG) never record. navigation-history.md refreshed.
- Navigation history core (#218, Roadmap 0220 — promoted from idea #51):
  internal/nav History (per-jump entries, forward-truncation on fresh
  jumps, same-line dedup, 100-entry cap) recorded at the open funnel
  (openPath file switches, openPathAt same-file line jumps — covers
  definition/references/search/file opens at two choke points); nav.back /
  nav.forward appCommands unblock the cmd+bracket defaults (removed from
  the 0081 blocked ledger) with new leader mnemonics space b / space i;
  status matrix regenerated. New concept doc
  architecture/navigation-history.md.
- Sandbox limits + plugin manifest (#27, Roadmap 9900, closes the epic):
  per-module memory cap (64 MiB default) and call deadlines (5 s default,
  wazero CloseOnContextDone) on every guest call incl. _initialize; a
  runaway callback closes the module and the bridge unloads it with an
  error toast. Optional sidecar <plugin>.manifest.json (name/version/
  capabilities) validated strictly at load — invalid manifests reject the
  module; a present manifest gates registration kinds (bridge drops
  undeclared ones with diagnostics) and host calls (gated "ike" module →
  no-ops). Docs in plugins.md, plugin-authoring.md, sdk/README.md; example
  plugin ships a least-privilege manifest.
- Go guest SDK + example plugin (#26, Roadmap 9900): sdk/ (nested module
  ike/sdk) wraps the raw ABI in a typed guest API — Command/Keymap/Hook
  declarations plus Notify/SetStatus/OpenFile/Dispatch/ConfigGet host
  calls; sdk/example is a buildable reference plugin; new authoring guide
  wiki/architecture/plugin-authoring.md (SDK, build via GOOS=wasip1
  -buildmode=c-shared, ABI reference for other languages). Full-pipeline
  test builds the example and drives it through scan → register → invoke.
- WASM capability bridge (#25, Roadmap 9900): internal/wasm/bridge adapts
  loaded modules into plugin.Plugin — register() descriptors become
  registry commands/keymaps/hooks (guest callbacks run inside tea.Cmds,
  faults toast as warnings), HostAdapter binds abi.Host onto the live
  host.API late (main.go instantiates the "ike" module before guests load,
  SetAPI after app.New). A WASM plugin is now palette-reachable and
  indistinguishable from a compile-in plugin; parity is pinned by tests
  against a real Go wasip1 guest.
- WASM ABI (#24, Roadmap 9900): internal/wasm/abi fixes the host↔guest
  contract — JSON payloads over (ptr,len) regions, packed-u64 returns,
  guest ike_alloc for host→guest buffers; guest entry points register/
  on_command/on_key/on_hook; host imports open_file/dispatch/notify/
  set_status/config_get as thin shims over the narrow abi.Host interface
  (malformed payloads dropped). Verified end to end against a real Go
  wasip1 c-shared guest exercising every shim.
- WASM plugin runtime (#23, Roadmap 9900): internal/wasm embeds wazero —
  plugins-dir scan (diagnostic-and-skip on faults), load/instantiate/unload
  lifecycle supporting both WASI conventions (command _start incl. clean
  proc_exit, reactor _initialize with callable exports), no ambient FS/net,
  guest stdio sunk. Tests build real Go wasip1 fixtures (including a
  c-shared reactor whose add export is called through the sandbox).
  main.go scans at startup; the capability bridge is #25.
- Per-binding status matrix (#16, Roadmap 0081): generated acceptance
  ledger (keymap.StatusMatrix/MatrixMarkdown) — one row per default-bound
  command with primary chord, reachability class, reachable fallback and
  resolution status; the final-gate test in cmd/ike runs against the
  shipped plugin set and fails on any unresolved row. All 60 rows resolve:
  live, live-via-fallback, or honestly blocked. Table persisted in
  architecture/keybindings.md. This closes Roadmap 0081 — epic #39 and its
  milestone.
- Keybinding discoverability (#15, Roadmap 0081): which-key panel for held
  chord prefixes (live continuations, letters first); keymap.LiveBindings
  gives the cheatsheet and the palette shortcut column honest labels from
  the effective table across reloads (delivered chord plain, fragile with
  warning + escape route, blocked with dependency); the cheatsheet gains a
  never-hidden blocked section.
- Leader key & terminal-safe defaults (#14, Roadmap 0081): space-leader
  (outside the editor, [keymap] leader tunable) plus universal ctrl+k
  mnemonics through the existing chord resolver — go-to-file, grep,
  project switch, terminal, LSP actions, tabs and more get delivered
  two-keystroke paths. Fragile flags now derive from the reachability
  table instead of hand-maintained booleans; a completeness test enforces
  an escape route (leader / delivered chord / documented exception) for
  every fragile default.
- Terminal reality probe & reachability table (#10, Roadmap 0081):
  cmd/keyprobe (interactive chord probe with machine-parseable PROBE lines
  and shift-collapse evidence) plus internal/keymap/reachability.go — every
  default chord classified delivered/fragile/undetectable with an honest
  note; ground truth recorded against tmux/macOS (ctrl+tab eaten,
  ctrl+shift+z collapses to ctrl+z, alt+* and Kitty-encoded cmd+* arrive).
  Downstream 0081 work (#14–#16) keys off these classes. Table persisted in
  architecture/keybindings.md.
- Toolchain environment injection (#98, Roadmap 0170): per-project shim dir
  (.ike/shims) with exec scripts for php/python/python3 targeting the
  settings-page interpreters (silent detection never injects); terminal
  spawns prepend it to PATH, venv choices set VIRTUAL_ENV + venv bin; the
  pane title indicates the mapping; shims regenerate on config reload and
  retarget running sessions (exec by absolute path). No setting → untouched
  env. Windows .cmd note documented. This closes Roadmap 0170 — epic #88
  and its milestone.
- Terminal commands & UX (#97, Roadmap 0170): terminal.toggle (alt+f12,
  JetBrains state machine: create → focus → return-focus, also reserved
  inside a focused terminal), terminal.clear (canonical 2J+3J wipe — 2J
  alone pushes lines into the scrollback — plus ctrl+l prompt repaint),
  Tools-menu entries, and OSC 0/2 titles appended to the pane title.
  Chord and commands land together, so no blocked-ledger entry.
- Terminal workspace integration (#96, Roadmap 0170): pane titles show
  shell + origin dir; the reserved key set is documented and minimal
  (ctrl+tab only — everything else reaches the shell); scrollback paging
  via shift+pgup/pgdn and the mouse wheel (styled history lines, marker
  row, snap-back on typing); layout persistence restores terminals as
  fresh shells in their saved position with the origin cwd; live sessions
  survive a project switch (adopted below the new active editor, titled
  with their origin root). Spawn dirs are pinned absolute.
- Integrated terminal core (#95, Roadmap 0170): new internal/terminal —
  creack/pty spawns the shell (terminal.shell → $SHELL → /bin/sh) in the
  project root, charmbracelet/x/vt emulates the screen, output notifications
  are coalesced. pane.KindTerminal + terminal.new (splits below the active
  editor); focused terminals take every key raw with ctrl+tab as the escape
  hatch; shell exit closes the pane; terminal leaves prune from layout
  restore until #96. Quality bar verified: vim, less, resize, colors. New
  doc architecture/terminal.md.
- Python environment management (#132, Roadmap 0180): the toolchain page
  creates a project venv (uv, python -m venv fallback) and installs managed
  Pythons picked from `uv python list`; results register the absolute
  interpreter via write-back ([lang.python] interpreter) and restart the
  servers. Async cmds with fake-runner tests; uv-absent fallback covered.
  This closes Roadmap 0180 — epic #129 and its milestone.
- Plugin manager page (#133, Roadmap 0180): settings panel gains a Plugins
  page — every registered plugin with live enabled state, capability
  summary and expandable inspection; `e` toggles plugins.<id>.enabled via
  write-back (new real [plugins] config section; applyPluginConfig is now
  symmetric and runs on reload). Language packages register a `lang-<id>`
  plugin shim (plugins/languages/register), so a disabled language plugin
  takes its LSP server with it and enabling one kicks the missing-server
  install (new lsp.installMissing command). Registry.Describe lists
  disabled plugins' capabilities for inspection.
- LSP semantic-token highlighting (#9, Roadmap 0100): new
  internal/highlight/semantic decodes legend-based 5-tuples into highlight
  spans (modifier-refined capture mapping, UTF-16 via convert.go); manager
  requests full/delta with per-document result state; bridge refreshes on
  open + change (coalesced); editor layers the overlay over the
  Tree-sitter base (base < semantic < diagnostics). Verified against gopls
  in a CGO-free build (no Tree-sitter): the whole file renders from the
  overlay alone. **This closes Roadmap 0100 — epic #38 and its milestone.**
- LSP incremental didChange sync (#13, Roadmap 0100): the manager now
  respects the negotiated TextDocumentSyncKind — incremental servers get
  the minimal change region (common-prefix/suffix diff against the
  previously synced lines, manager/incremental.go) instead of the whole
  document on every keystroke; full-sync servers keep the old behaviour,
  SyncNone servers get nothing. UTF-16/UTF-32 offsets via convert.go,
  monotonic versions that only advance on a real send. Verified against
  gopls (negotiates incremental): diagnostics track correctly through
  inserts, newline splits and line deletes.
- LSP signature help (#4, Roadmap 0100): typing a server-advertised trigger
  character opens a cursor-anchored popup with the active signature, the
  active parameter emphasised (substring and UTF-16 offset-pair labels both
  resolve), first doc line and overload counter. While showing, every change
  retriggers so the parameter follows the cursor; a null answer (past ")")
  or esc dismisses. Capability-gated; completion popup takes precedence.
  Verified against gopls: popup on "(", highlight moves on ",", gone on ")".
- LSP code actions (#8, Roadmap 0100): `lsp.codeAction` (alt+enter) lists
  quick-fixes/refactors for the cursor or visual selection in a locked
  palette picker (preferred first), passing cached diagnostics as context.
  Chosen actions apply their WorkspaceEdit via workspace_edit.go and/or run
  workspace/executeCommand; the manager now answers server-initiated
  workspace/applyEdit (off the read loop — inline responding can deadlock
  against a flushing server) through the new Callbacks.ApplyEdit seam.
  Verified against gopls: Organize Imports removes an unused import through
  the full executeCommand → applyEdit round trip.
- LSP rename symbol (#6, Roadmap 0100): `lsp.rename` — prepareRename
  validation (reject toast), name prompt prefilled with the symbol
  (bridge-built Apply continuation keeps the manager out of the app), and
  WorkspaceEdit application through new shared infrastructure
  (`plugins/lsp/workspace_edit.go`): open buffers in-editor as one undo
  unit, closed files rewritten on disk; both WorkspaceEdit shapes decode.
  Manager splits edits by open/closed and converts positions (UTF-16 in
  convert.go). Verified against gopls across an open and a closed file.
- LSP document & range formatting (#7, Roadmap 0100): `lsp.format`
  (`cmd+alt+l`) and `lsp.formatRange` apply server `TextEdit`s to the buffer
  as one undo unit via the new `editor.ApplyTextEdits` (bottom-up, clamped,
  multi-line). Editor events now carry the visual anchor so the bridge knows
  the selection for range requests; `FormattingOptions` honour
  `editor.tab_width`/`use_spaces`; UTF-16 conversion stays in
  protocol/convert.go (manager converts, owning the synced lines).
  Capability-gated both ways; file-open now primes the bridge's current file
  so formatting works before the first cursor move.
- LSP find references (#5, Roadmap 0100): `textDocument/references` through
  client/manager (capability-gated on `referencesProvider`, UTF-16 conversion
  via protocol/convert.go), new `lsp.references` command ("LSP: Find
  Usages"), `alt+f7` reconciled in the chord table (blocked-ledger entry
  removed). Results route by count: toast / direct navigation / palette
  locked to a new references mode with `path:line` + preview and fuzzy
  filter; activation reuses the DefinitionMsg navigation path. Tests across
  client, manager (fake server echoes includeDeclaration), and app routing.
- Project switching complete (#3, Roadmap 0090): msg-driven switch
  transaction — `SwitchTo` validation, unsaved-changes guard (save-all /
  discard / cancel in the floating shell), root-model re-root via chdir +
  model rebuild with the live host carried over (LSP bridge and program
  sender survive), session/layout persisted per project, history recorded on
  success with a config reload. `alt+shift+p` added to the JetBrains chord
  table so the picker opens from a capturing editor. Fixed: the floating
  shell drops boxes wider than the terminal — prompt paths render through
  `CompactPath`. Epic #37 closes with this.
- project.switch command + picker (#12, Roadmap 0090): `internal/project`
  registers the `project.switch` command (default slot `alt+shift+p`) and a
  palette picker mode listing recent projects newest-first with fuzzy match
  on name/path, compacted path details and an `Open "<query>"…` affordance.
  The root model opens the picker locked and routes `PickedMsg` (stub toast
  until the switch orchestration #3 lands). File-menu "Switch Project" now
  resolves. Doc `architecture/project-switching.md` updated.
- Project-switching data layer (#2, Roadmap 0090): new `internal/project`
  package — `Entry` (path/name/last_opened), `Validate` (expand `~`, absolute,
  exists/is-dir/readable with actionable errors) and history content rules
  (upsert-by-path, move-to-front dedupe, cap at `project.max_history`),
  persisted to the user layer via config's typed setter. `project.history`
  becomes a `[[project.history]]` table array (`config.ProjectHistoryEntry`;
  `config.PushHistory` removed). Startup records the initial open before the
  model loads config. New doc `architecture/project-switching.md`; config doc
  updated.
- Missing-server install helper (#131, Roadmap 0180): `lang.ServerSpec` grows
  an `Install` recipe (argv; gopls via `go install`, pyright/intelephense via
  `npm -g`). A server launch failing with ErrNotFound on file open triggers
  the recipe automatically in the background (`plugins/lsp/install.go`) —
  progress/result toasts, on success the triggering document re-opens so the
  server starts immediately. New config `lsp.auto_install` (default true);
  the Language Servers page toggles it (`A`) and runs the install manually
  (`i`) — the retry path after a failure. Guards: one install per language,
  auto path backs off after a failure, failures log the output tail to
  debug.log (root model, every ServerEventError). Tests in `plugins/lsp`
  (fake runner: recipe, opt-out, backoff, concurrency, no-recipe warn) and
  `internal/settings`; wiki (lsp.md, settings-ui.md) updated.

- Language-server settings page (#130, Roadmap 0180): new custom settings
  page "Language Servers", contributed by the LSP plugin via SettingsPages
  (`internal/settings/lsp_page.go`). One row per language with a server:
  live status (ready/idle/crashed/missing/disabled/off-master — from
  language-tagged `ServerStatusMsg`s, which now carry `Lang`, plus the new
  `Manager.RunningLangs`), effective command line and source layer. Controls:
  `E` master switch, `e` per-server enable (new `[lsp.servers.<id>] enabled`
  key, honored by `resolveSpec`), inline `c`/`a`/`s` command/args/settings
  overrides (project-scope write-back, empty = reset), `x` reset all, `r`
  per-server restart (new `Manager.StopLang`, async per #123), `R` restart
  all. Missing binaries render the launch-failure reason with an install-
  helper hint (#131). Tests across `internal/settings`, `internal/lsp/manager`
  and `plugins/lsp`; wiki (settings-ui.md, lsp.md) updated.

- Editor tabs — session persistence (#160, Roadmap 0190, closes the epic):
  `layout.json`'s per-leaf identity grows `tabs` (ordered file-backed tab
  paths) and `active` (index within that list); `path` stays the active tab's
  file for older builds. Restore rebuilds tab lists tolerantly: pre-tabs
  identities become single-tab panes, missing files are skipped (active index
  maps to survivors), all-gone panes restore as a scratch tab, and one file in
  several tabs/panes restores as one shared document. Scratch-tab text remains
  crash recovery's job. Tests in `internal/app/tabpersist_test.go`; wiki
  updated. **Epic 0190 complete** — tab model (#156), bar (#157), commands
  (#158), mouse (#159), persistence (#160).

- Editor tabs — mouse on the bar (#159, Roadmap 0190): `tabAt`/`tabBarHit`
  (in `internal/app/tabbar.go`) hit-test the rendered bar geometry exactly.
  Left-click focuses/activates the clicked tab (the active segment still
  starts a pane move, preserving the title-row drag handle), middle-click
  closes it with the editor.closeTab guard (reopen ring fed; a single-tab
  pane closes entirely), and the wheel over the bar row cycles tabs instead
  of scrolling. Tests in `internal/app/tabmouse_test.go`; wiki updated.

- Editor tabs — commands & keybindings (#158, Roadmap 0190): new registry
  commands `editor.tab.next`/`prev` (alt+right/left, wrapping),
  `editor.tab.select1…9` (alt+1…9), `editor.tab.moveLeft`/`moveRight`
  (alt+shift+arrows) and `editor.tab.reopenClosed` (alt+shift+t) — handlers in
  `internal/app/tabs.go`, acting on the focused (else most recent) editor
  pane. A 10-entry reopen ring records path + caret of closed tabs (tab and
  pane closes both feed it); reopen skips files deleted since and restores the
  caret. Chords are QWERTZ-safe and distinct from the ctrl+tab pane switcher;
  alt+arrow rows are marked fragile (option-as-meta). "Reopen Closed Tab"
  joins the File menu; palette/cheatsheet entries come via the registry.
  Tests in `internal/app/tabcommands_test.go`; wiki concept doc updated.

## 2026-07-09

- Editor tabs — tab bar rendering (#157, Roadmap 0190): editor panes with ≥ 2
  tabs render a tab bar on the pane's top row, replacing the single-document
  title (zero extra vertical cost; `internal/app/tabbar.go`). Labels show the
  basename with directory disambiguation for duplicates, a dirty ● and stale
  `!` marker; the active tab is highlighted via theme slots (Accent/bold,
  separators in Border). Overflow elides around the active tab with `…` at the
  hidden end — never wraps. New config key `editor.tabs.always_show` (default
  false, `[editor.tabs]`, settings-page toggle) forces the bar for single-tab
  panes. Tests in `internal/app/tabbar_test.go`; wiki concept doc updated.

- Editor tabs — tab model (#156, Roadmap 0190): each editor pane
  (`pane.Instance`) now hosts an **ordered tab list** (`[]*editor.Model`) with
  one active index; `Editor()` stays the active tab so the pane surface is
  unchanged. New ops `AddTab`/`ActivateTab`/`MoveTab`/`CloseTab` (+
  `TabForPath`, `Editors`, `UpdateForPath`, `UpdateTab`). `openPath` routes all
  open seams into the focused pane's tab list (`openInTab`): activate an
  existing tab, fill a scratch tab in place, else append a tab (autosaving the
  document being left, #174); open-in-new-pane keeps splitting. Shared
  documents (#142) span tabs: `loadOrShare`/sync/highlight/LSP routing reach
  background tabs. `editor.closeTab` (cmd+w, `:q`) closes the active tab and
  the pane only on its last tab; backup snapshots (#165), save-all, external
  delete/move, conflicts and replace-in-buffer are all tab-aware. New concept
  doc [Editor Tabs](/architecture/editor-tabs.md); tests in
  `internal/pane/tabs_test.go` and `internal/app/tabs_test.go`.

- Backup config & GC (#167, Roadmap 0210): new `[backup]` config section —
  `enable` (default true; `false` fully disables the subsystem and **purges**
  existing snapshots, at startup and on live reload), `debounce_ms` (default
  2000, clamped ≥ 100), `max_age_days` (default 7, clamped ≥ 1) — plus the
  write-side wiring (`internal/app/backup.go`): the `editor.SyncMsg` change
  seam marks dirty buffers on the `Debouncer`, one armed `tea.Tick` snapshots
  the quiet ones off the Update loop, and save / close-with-discard / clean
  quit remove their snapshots. Age-based GC (`Service.Prune`) runs at startup
  only after the restore prompt closes. New settings **Backup** page
  (`backup.enable` / `backup.debounce_ms` / `backup.max_age_days`). Tests
  across `internal/backup`, `internal/config`, `internal/settings`,
  `internal/app`. Wiki updated (crash-recovery config table + privacy note,
  config schema/clamps, settings pages).

- Restore flow (#166, Roadmap 0210): crash recovery reads leftover snapshots at
  launch (`internal/app/recovery.go`). `scanRecovery` runs in the constructor;
  once the window is sized, `maybeOpenRecovery` shows a floating prompt (reusing
  the save-conflict UX) listing every recoverable file with a per-file
  base-changed warning. `r` restores the recovered text as a dirty buffer (new
  `editor.RestoreText`: onto the base file for titled buffers, a fresh untitled
  editor otherwise), `d` discards, `s` skips, `esc` skips all. Base-change
  detection compares the on-disk hash/mtime against the snapshot header. Crash-
  simulated tests (`recovery_test.go`). Wiki updated.

- Backup service (#165, Roadmap 0210): new `internal/backup` subsystem for
  crash recovery — `Service` writes/reads/removes one full-text snapshot per
  dirty buffer (`<sha256(key)>.ikebak`: magic + header with key/path/base
  mtime+hash/timestamp, blank line, verbatim text) with **atomic** temp→fsync→
  rename writes to `<state dir>/backups`; untitled buffers are marked "no base
  file". `Debouncer` (injectable clock) collapses edit bursts into one pending
  snapshot ~2s after the last edit, so clean buffers cause zero writes.
  `BaseInfo` stats+hashes the on-disk base for change detection. Fully unit-
  tested (fake clock + temp dirs). App event-loop wiring + restore UI land with
  #166/#167. New concept doc `architecture/crash-recovery.md`.

- Substitute confirm mode (#163, Roadmap 0200): the `c` flag
  (`:s/pat/repl/gc`) drives an interactive match-by-match walk in a mode-machine
  sub-state (`internal/editor/substitute_confirm.go`) — `y`/`n`/`a`/`q`/`l`
  (+ `Esc`), the current match highlighted, the prompt on the command-line row.
  Accepted replacements share one open recorder (a single undo unit; cancel keeps
  what was applied), and a per-line rune-delta keeps multiple matches on a line
  aligned as lengths change. Completes epic 0200.
- Range companions (#164, Roadmap 0200): `internal/editor/excmd_ops.go` adds
  `:[range]d [reg]` (delete into register), `:[range]y [reg]` (yank; cursor
  stays), and `:[range]>` / `:[range]<` (indent/outdent, `:>>` repeats) over the
  shared #161 resolver, reusing the operator/register/indent logic. Each is one
  undo unit with vim-matching cursor behavior (verified against vim).
- `:substitute` core (#162, Roadmap 0200): `internal/editor/substitute.go`
  implements `:[range]s/pat/repl/[flags]` on top of the #161 parser/resolver —
  flags `g`/`i`/`I`/`n`, any delimiter (`:s#a#b#`), pattern via the search-regex
  convention (`\v`, empty pattern reuses the last search), vim-style capture-group
  replacements (`&`, `\0`-`\9`). All replacements form a single undo unit, the
  cursor lands on the last changed line, and the result is reported as *N
  substitutions on M lines* (or a clear error for unknown flags / pattern not
  found).
- Ex parser & range resolver (#161, Roadmap 0200): `internal/editor/excmd` now
  parses the `:` line into a typed `Command{Range, Name, Bang, Args}` AST with a
  full address grammar — `%`, line numbers, `.`, `$`, `'<` / `'>`, `/pat/` /
  `?pat?`, and signed offsets — and a single `Range.Resolve` shared by all
  range-taking commands. Existing `:w :q :wq :e` and bare line jumps keep
  working; `:g` / `:v` / `:s` are reserved with a *not implemented* message.
  Entering `:` from Visual pre-fills `'<,'>`. Ex-command errors/reports now show
  on a transient command-line message row.
- F6 move / Shift+F6 rename (#175): `file.move` (f6) moves the explorer
  selection or the focused editor's file into a folder picked from a new
  palette directory mode; `file.rename` (shift+f6) renames it (explorer inline
  prompt, or a shell prompt for the focused editor). Renames/moves now emit
  `FileMovedMsg` and open editors **follow the new path** (buffer, cursor,
  undo history intact) instead of being closed; undo/redo of the operation
  re-points them back. shift+f6 was reclaimed from the blocked LSP
  rename-symbol placeholder (#6 needs a new chord when it lands).

- Auto-save on focus switch (#174): `editor.auto_save = focus` (default; `off`
  disables) saves a dirty buffer when focus leaves its pane or its document is
  replaced by opening another file. Saves ride the normal path (watcher epoch,
  LSP didSave, shared-view sync); undo history is untouched, and undo/redo now
  re-dirty the buffer so post-save undos persist on the next blur. Stale
  buffers are skipped (conflict guard unchanged). Settings entry under Editor.

## 2026-07-08

- Replace in path (#86, Roadmap 0150): `project.replaceInPath` (cmd+shift+r)
  adds a replacement input + before/after preview to the finder; enter/alt+f/
  alt+a apply per match/file/all. Dirty buffers are edited in place (one undo
  unit per file), other files rewritten on disk (clean open buffers reload
  via the 0140 watcher); stale lines are skipped and counted; `$1` capture
  groups expand. Epic #73 (Find in Path) is complete.

- Find-in-path UI (#85, Roadmap 0150): `project.findInPath` (cmd+shift+f) is
  live — modal overlay with query, case/word/regex toggles, include/exclude
  globs, query history, and live-streamed results grouped by file (the new
  reusable `internal/locations` list). Enter jumps to the match;
  `search.nextMatch`/`prevMatch` step retained results with the overlay
  closed. Blocked-ledger entry for the binding removed.

- Project-search scanner engine (#84, Roadmap 0150): `internal/search` streams
  matches in batches with generation-based cancellation and a truncation
  bound; `rg --json` backend (with `--no-require-git`) and a pure-Go
  walker+matcher fallback with a small gitignore matcher, guarded by a
  backend-parity test. UI lands with #85.

- Shared documents (#142): the same file open in several panes is one document
  with multiple views — shared buffer and undo stack, per-pane cursor/scroll;
  unsaved edits, dirty/stale flags, saves and reloads mirror live across the
  views via an emitter-driven SyncMsg broadcast. Async per-path messages
  (highlight, LSP, watch) now route to all owning panes.

- Explorer auto-refresh on watcher events (#83, Roadmap 0140): directory
  events re-scan just the affected subtree, preserving expansion state and
  cursor; externally deleted files close their editor pane (dirty buffers
  survive, marked stale); manual `r` and the mtime poll stay as fallbacks.
  This completes epic #72.

- Stale marking + save conflict guard (#82, Roadmap 0140): an external change
  to a dirty buffer marks it stale (tab `!`, status `[disk changed]`) instead
  of reloading; saving a stale buffer opens a floating prompt — keep mine /
  reload (discard edits) / cancel. Keep-mine stamps the watcher's save epoch;
  reload reuses the clean-reload path.

- Clean-buffer auto-reload (#81, Roadmap 0140): a non-dirty editor buffer whose
  file changed on disk reloads in place, preserving cursor and scroll (clamped
  like session restore); undo history restarts; highlighting and LSP re-sync
  via the ordinary change event. Config `files.auto_reload = clean|never`
  (default `clean`). Dirty buffers stay untouched until #82.

- Menu bar polish (#137): the open dropdown gets a rounded border, mouse
  hover selects entries (disabled ones skipped), and a title's first letter
  jumps to that menu while open — the bar underlines each first letter as
  the hint.

- Help overlay polish: the cheat sheet is now scoped to the focused pane
  (global commands plus the focused context's group; `Snapshot` takes a
  context id), shortcuts are right-aligned to the column edge, columns carry a
  fixed slack beyond their widest cell so the pane is wider than its text, and
  the floating shell's title row is underlined with a blank spacer row beneath
  it (budget reserves `titleRows = 2`).

- Settings mouse control (#127): category clicks switch pages, form-entry
  clicks select, a second click activates (enter semantics — bool toggles,
  enum cycles, text opens the inline edit).

- Slow-update diagnostics (#125): Update passes over 200ms log message type +
  duration to `.ike/debug.log`, so UI stalls (like the #123 restart deadlock)
  are attributable after the fact. Fixed #123 itself: `lsp.restart` now runs
  Shutdown asynchronously and returns its status message instead of calling
  `host.Send` from the Update goroutine (which deadlocks bubbletea's
  unbuffered message channel).

- Click-outside dismiss (#116): a mouse press outside an open floating
  overlay — settings panel, floating shell (help/modals/history), command
  palette (centered and anchored) — closes it; clicks inside never leak to
  the panes below. The menu dropdown already behaved this way.

- Settings panel floats (#115): the settings window renders as a centered
  rounded-border box (capped ~110×32) above the workspace instead of covering
  the whole terminal; overlong form rows clip instead of wrapping the frame.

- Split keybindings (#114, #121): `pane.splitDown` / `pane.splitUp` /
  `pane.splitRight` / `pane.splitLeft` (cmd+k + arrow) split the focused leaf
  with a fresh empty editor and move focus onto it — no drag or file open
  needed (wires the existing `Model.SplitFocused`).

- Toolchain settings page (#94, closes epic #87 / roadmap 0160): per-language
  interpreter rows with source badge and async version probe; discovery
  pickers (Python: venv/.venv/uv/pyenv/PATH; PHP: PATH + install locations) +
  custom path; choices land in the project config (`[lang.<id>] interpreter`)
  and trigger an LSP restart. New `lang.Interpreter` resolution (explicit
  beats detection) with `InterpreterDetector`/`ExplicitSettings` toolchain
  extensions — the single source of truth 0170's terminal shims will reuse.

- Keymap editor page (#93, roadmap 0160): settings pages can now be custom
  models (`settings.PageModel`); the Keymap page lists the effective binding
  table (layer badges, blocked-with-reason, fragile ⚠, type-to-filter) and
  rebinds by chord capture — conflict confirmation, cmd-chord honesty warning,
  `u` unbind, `r` reset-to-preset — all as `keymap.bindings.*` overrides via
  write-back; the app rebuilds its resolver on config reload so edits apply
  live.

- Core settings pages (#92, roadmap 0160): Editor (all live `applyConfig`
  keys), Appearance (theme enum from the registry with immediate preview,
  menu bar, palette chord), Files & Session (restore-last, files.watch) and
  Notifications. No-dead-keys test guards every entry against the typed
  schema.

- Settings panel framework (#91, roadmap 0160): new `internal/settings` —
  full-window panel (cmd+, / `settings.open`), left category list, right
  schema-driven form (bool/int/string/enum/path/chord). Apply-on-change
  through the write-back layer + live reload; per-entry layer badge
  (`config.Origin`) and `r` reset-to-default; `/` filter across all pages;
  plugin-contributable pages via `Capabilities.SettingsPages`.

- Menu bar (#90, roadmap 0160): new `internal/menu` — File · Edit · View ·
  Navigate · Tools · Settings · Help above the panes (`ui.menu_bar`, default
  true). Menus are data over the command registry: entries show cheatsheet
  shortcuts, unregistered ids render disabled with their dependency hint;
  selection dispatches `menu.RunMsg` → `RunCommand`. f10 toggles; arrows/
  enter/esc navigate; mouse clicks open/run. New concept doc
  `architecture/settings-ui.md`.

## 2026-07-07

- Config write-back layer (#89, roadmap 0160): `config.WriteKey`/`RemoveKey`
  persist one dotted key to the user or project settings file via a TOML
  round-trip (unknown keys survive; broken files are refused, never
  destroyed); `DefaultScope` routes keys to their conventional layer;
  `WriteAndReload`/`RemoveAndReload` chain into the existing reload path so
  changes apply live. Foundation for the 0160 settings UI.

- File-watcher service (#80, roadmap 0140): new `internal/watch` — fsnotify on
  the project root (recursive, `.git` skipped), ~100ms debounce with per-path
  coalescing, `watch.EventMsg` routed to the owning editor / explorer.
  Self-event suppression via a save epoch (new editor `EventSave` → emitter
  adapter → `MarkSaved`); mtime+size poll fallback with hash-on-suspicion for
  tracked (open) files; config `files.watch` (default true).

- Block-comment toggling (#76, closes epic #70 / roadmap 0120):
  `editor.commentBlock` (cmd+shift+7) wraps a charwise selection inline
  (`/* sel */`), a linewise selection or the current line on its own marker
  lines; exactly-wrapped targets unwrap; python falls back to line comments.
  One undo unit, dot-repeatable. Blocked-ledger entry removed.

- Line-comment toggling (#75, roadmap 0120): `editor.commentLine` (cmd+7,
  cmd+k cmd+c) toggles the registry language's line marker on the current line
  or visual selection at the minimal indent — mixed ranges comment the
  uncommented lines, fully commented ranges uncomment, blank lines skipped.
  Single-line toggle advances the cursor (JetBrains); selections survive the
  toggle. One undo unit, dot-repeatable; buffers without comment syntax raise
  an info toast (`editor.NoticeMsg`). Blocked-ledger entry removed.

- Notification history (#78, closes epic #71 / roadmap 0130): ring of the
  newest 100 notifications with timestamp + severity; `notifications.history`
  palette command lists them in the floating shell. New typed config section
  `[notifications]` (`timeout_seconds`, `min_severity` — below the floor is
  history-only, no toast), live-reloaded; the host's config view now refreshes
  on config reload (`Host.SetConfig`).

- Event-like `SetStatus` call sites migrated to `Notify` (#79, closes roadmap
  0130's migration milestone): example plugin messages, save-all ("saved N
  files"), theme select/warnings and transient LSP events (crashed → warn,
  restarted → info, launch failure / disabled after repeated crashes → error)
  are toasts now; `lsp.ServerStatusMsg` carries a `ServerStatusKind` assigned in
  the manager. Persistent LSP server state stays on the status line, which
  renders `SetStatus` as an extra segment instead of replacing the whole line.

- Command line moved into the editor pane (#99): the ":" / "/" / "?" input
  renders as the pane's bottom row (vim-style) instead of replacing the app
  status line. Status line shows the focused file's project-relative path
  (absolute outside the project) instead of just the base name (#100).

- Notification toasts (#77, roadmap 0130): `host.Notify(severity, text)` queues
  event messages the root model drains each Update pass and renders as
  severity-colored toasts bottom-right above the status line — info/warn expire
  (`notifications.timeout_seconds`, default 4s), errors persist until Esc
  (pass-through). New concept doc `architecture/notifications.md`.

- Language registry comment metadata (#74, roadmap 0120): `lang.Language` grows
  `LineComment`/`BlockComment`, `lang.Comments(path)` resolves the syntax per
  buffer path; go/php declare `//` + `/* */`, python `#`. Consumed by the
  upcoming comment-toggle actions (#75/#76).

- Command coverage & id reconciliation, no inert bindings (#11): new blocked
  ledger (`keymap/blocked.go`) documents every intentionally-unregistered
  default binding with its unblocking dependency; thin commands registered for
  `editor.find`, `editor.duplicateLine`, `editor.saveAll` and
  `explorer.toggle`; `cmd+b` reconciled onto the registered `lsp.definition`
  id; coverage test `TestNoSilentlyDeadDefaultBindings` guards the invariant.

- Conventional selection & clipboard (#47): word navigation moved from
  `shift+←/→` to `alt/option+←/→` (paragraph jumps to `alt+↑/↓`; ctrl variants
  stay); `shift+arrows` now start/extend a charwise visual selection;
  `editor.copy/cut/paste` registered (cmd+c/x/v live, acting on the selection
  or current line through the system clipboard — new `internal/clipboard`
  package wired into `register.SetClipboard`); `cmd+left`/`cmd+right` bound to
  new `editor.lineStart`/`editor.lineEnd` commands.

- Cheatsheet, pane switcher, go-to-file as registered commands (#44, #45, #46):
  `palette.keymapHelp` (f1 / cmd+k cmd+s) opens the help overlay via the new
  `openHelp` helper (hardcoded `?`/`f1` kept as fallback), `pane.switcher`
  (ctrl+tab, fragile) cycles focus like the hardcoded tab, and
  `project.goToFile` (cmd+shift+o) opens the centered palette locked to the `@`
  file mode via the new `palette.OpenLocked`.
- Cmd+W closes the focused tab (#43): new compile-in `app` plugin
  (`internal/app/commands.go`) exposes root-model actions as registry commands;
  `editor.closeTab` dispatches `CloseTabMsg` → `CloseFocused`, so the default
  `cmd+w` binding is live and the action is palette-invokable.

- Ctrl/Cmd+S saves the file (#42): the default `cmd+s` binding now targets
  `editor.write` (the registered `:w` command) instead of the never-registered
  `editor.save`, and a `ctrl+s` fallback chord was added because macOS terminals
  never forward `Cmd` — mirroring the undo/redo pattern. Works from insert mode
  (modified chords stay keymap-eligible). `cmd+shift+s`/`editor.saveAll` stays
  inert until a save-all command exists (#11/#19).

- Planning moved from the `roadmaps/` directory to GitHub issues on
  `TrueDaerk/ike`: specs live verbatim in epic issues #37–#41 (0090 Project
  Switching, 0100 LSP deferred, 0081 Keybinding Audit, 0082 Usability Review,
  9900 WASM Plugins), work items are sub-issues tracked via one milestone per
  epic. `roadmaps/` was deleted (history remains in git); wiki links to roadmap
  files were rewritten as "former Roadmap NNNN" notes.
- Theme switching from the command palette: the `themes` plugin now registers
  one global command per built-in scheme (`themes.select.<name>`, "Theme:
  <name>"), dispatching `app.SelectThemeMsg` → `selectTheme` (resolve over
  built-ins + plugin themes, re-thread via `applyTheme`, status confirmation).
  The runtime pick persists in the session store (`session.json` `theme` field,
  `Model.themeOverride`) and is re-applied on restore, overriding the
  config-derived theme; only explicit picks are recorded, so `settings.toml`
  stays untouched and config edits keep working until a runtime pick overrides.
  `settings.toml` write-back remains with 0040/0090.

- Roadmap 0110 (Themes / Color Schemes) implemented: new leaf package
  `internal/theme` (semantic `UI` slots + `Captures` + `Files` per Textual/
  sqlit model, `Palette` with resolved colors, single `theme.Resolve` token
  resolver, `theme.Select` lookup with fallback-to-default). Built-ins:
  `default` (pixel-identical to the old hard-coded colors), `tokyo-night`,
  `nord`, `gruvbox`(+light), `rose-pine`(+dawn), `catppuccin-mocha`/`-latte`,
  shipped via the compile-in `themes` plugin (`Capabilities.Themes`, merged by
  `registry.Themes()`). `[theme].name` now selects the palette;
  `highlight.NewTheme` and the explorer color table take their defaults from
  it (per-key `theme.captures.*` / `[explorer.colors]` still win). Every hex
  chrome literal in `app`, `editor`, `explorer`, `ui`, `palette`, and `help`
  was replaced by a palette slot; the screen background/foreground are set at
  the renderer level from the palette. Live re-theme: the app now consumes
  `config.ConfigReloadedMsg` (re-resolve palette, re-thread, reconfigure
  panes). Updated the Themes concept doc from planned to implemented.

## 2026-07-06

- Explorer UX pass (four features). **Open-file marking:** every file open in
  any editor pane renders its name underlined + italic (`Model.SetOpen`, kept
  current by `app.syncExplorerOpen` on open/close/restore; a stale `active`
  mark is cleared; `rowParts` keeps guides/padding undecorated). The active
  accent is muted (no bold) and tracks the **focused** editor's file
  (`app.setFocus` → `SetActive`), so switching panes moves the highlight. **Double-click to open:** a single mouse press now only selects a
  row; opening a file / toggling a directory takes a double-click (400ms
  window, injectable clock) — except a single click on a directory's expand
  caret, which still toggles. **Auto-refresh:** directory mtimes are polled
  every 2s off-thread (`schedulePoll`/`pollMsg`); only changed directories are
  re-scanned, and `setChildren` now merges by path so any re-scan (auto or
  manual `r`, which also became a deep re-scan of expanded children) preserves
  expansion state. Disable with `explorer.auto_refresh = "false"`.
  **Undo/redo:** file operations gained a redo stack (`explorer.redo`,
  `Ctrl+Shift+Z`/`Cmd+Shift+Z`) and rename is now undoable; undo of a create
  moves the entry to `.ike-trash/` instead of removing it, so undo/redo apply
  instantly without confirmation prompts. `editor.redo` additionally binds
  `ctrl+shift+z`, which — unlike `cmd+shift+z` — macOS terminals can deliver.

- Editor: mouse wheel now scrolls the viewport (`editor.ScrollBy`, wired in
  `app.handleMouse`'s `mouseWheel` case for `pane.KindEditor`), independent of
  vim mode — it moves `view.Top` directly instead of the cursor, so it works
  the same in Normal/Insert/Visual/etc. Previously only the explorer pane
  handled the wheel.
- Roadmap 0050 (File Explorer) file-operations milestone completed: added
  `explorer.rename` (prompt prefilled with the current name, `R` default key) to
  the existing create/delete/undo set. Rename is not on the undo stack (rename
  back to undo); it reuses `FileDeletedMsg` for the old path so the app closes any
  editor open on it, since the editor can't follow a path change in place. Along
  the way fixed a latent test-helper bug: `pumpScans` didn't unwrap `tea.BatchMsg`
  (used by delete/rename's `tea.Batch(refreshDir, deletedCmd)`), so it silently
  skipped the rescan — invisible before because no test asserted post-delete
  row/cursor state. Roadmap 0050 is now fully checked off.
- Roadmap 0110 (Themes) reworked to match landed reality. Syntax highlight (0100)
  and explorer file colors (0050) already ship config-driven color models with
  duplicated resolvers, and `[theme].name` is inert. 0110 now: activate
  `[theme].name` so a **named palette** recolors syntax + explorer + chrome at
  once; new leaf `internal/theme` holds built-in palettes + one shared color
  resolver (collapsing the `highlight`/`explorer` copies) and feeds the
  **defaults** of the existing `highlight.Theme` / explorer `colorTable` (per-key
  config still overrides); chrome hex literals move onto ui slots. Naming caution
  recorded: color pkg is `internal/theme`, not `internal/palette` (that's 0070's
  command palette). Also captured the **background-bleed bug**: `app.render`
  paints `appBackground` once around the whole screen (`app.go:1512`), so pane
  interiors, the floating shell, the palette, and LSP popups still show the raw
  terminal background (lipgloss won't repaint occupied cells). 0110 mandates
  painting backgrounds **per surface** (pane bodies fill `surface` + pad to full
  size; overlays paint an opaque surface before compositing). Updated
  `roadmaps/0110-themes.md` + `architecture/themes.md`.

## 2026-07-02

- **Extensible language system (Roadmap 0105).** The hardcoded three-language set
  became a plugin registry. New leaf package `internal/lang` is the single source
  of truth for a language: `Language{ID, Extensions, Filenames, Grammar, Server,
  Toolchain}`, registered from a plugin's `init()` like `registry.Register`. The
  highlight engine (`internal/highlight`) no longer knows any language — it exposes
  `NewGrammar(tsLang, query)` (cgo) and resolves grammars via `lang.ByPath`; the
  Go/PHP/Python grammars + `highlights.scm` queries moved into
  `plugins/languages/{go,php,python}` (grammar behind a cgo build tag, nil stub for
  `CGO_ENABLED=0`). LSP server baselines now come from each language's
  `Language.Server`; `[lsp.servers.<id>]` config only *overlays* them
  (`resolveSpec` merge; `applyDefaults` no longer hardcodes servers). New
  `lang.Toolchain` seam: `manager.ensureServer` runs the language's detector against
  the workspace root and merges the result into server settings, and the manager now
  answers `workspace/configuration` from those settings — so the Python detector's
  resolved interpreter (`$VIRTUAL_ENV` → `.venv` → `.python-version` → PATH) reaches
  pyright as `python.defaultInterpreterPath`, giving version-aware diagnostics
  without IKE reimplementing any version logic. Tree-sitter highlighting stays
  version-agnostic. Adding a language = new `plugins/languages/<lang>/` package + a
  blank import in `cmd/ike/main.go`. See [Language Registry](/architecture/languages.md).

## 2026-06-28

- **LSP + syntax highlighting (Roadmap 0100, MVP slice).** IKE gained language
  intelligence for **Go / PHP / Python**. A pure-Go JSON-RPC 2.0 client
  (`internal/lsp/{jsonrpc,transport,protocol,client,manager}`) speaks LSP over a
  server's stdio; a `manager` maps each `(language, workspace root)` to one server,
  spawns lazily, routes ops, and recovers from crashes (backoff respawn + re-open
  tracked docs). Editor edits flow out through the existing `Emitter` seam — now
  forwarded via a new `host.EditorEmitter` + `host.Send` (async injection wired
  from `main.go`'s `program.Send`) — and the `plugins/lsp` compile-in plugin drives
  `didOpen`/`didChange`/`didSave`/`didClose`. Results return as `tea.Msg`s
  (`DiagnosticsMsg`/`CompletionMsg`/`HoverMsg`/`DefinitionMsg`/`ServerStatusMsg`)
  routed by path to the owning editor leaf: diagnostics colour the gutter + underline
  inline + count in the status line; completion shows a cursor-anchored, prefix-
  filtered popup; hover shows a popup; go-to-definition navigates. `lsp.hover` /
  `lsp.definition` / `lsp.restart` are registry commands. Server defaults
  (gopls/intelephense/pyright) ship via `config/extend.go`; a missing binary is a
  graceful no-op. Separately, **Tree-sitter syntax highlighting** (`internal/highlight`)
  parses Go/PHP/Python off the event loop into theme-coloured spans applied per cell
  in `renderLine`; it is CGo, isolated behind a build tag with a no-op stub so
  `CGO_ENABLED=0` still builds. Deferred to a later increment: references, rename,
  formatting, code actions, signature help, and the LSP semantic-token overlay. See
  [LSP](/architecture/lsp.md) and [Syntax Highlighting](/architecture/highlighting.md).

## 2026-06-25

- **Upgraded to Bubble Tea v2 (Roadmap 0085).** The whole charm stack moved to
  `charm.land/bubbletea/v2 v2.0.7`, `charm.land/lipgloss/v2 v2.0.4`, and
  `charm.land/bubbles/v2 v2.1.0`. The driver is the **kitty keyboard protocol**:
  keyboard enhancements are now requested on the root model's `tea.View`
  (`ReportEventTypes`), unlocking disambiguated chords (ctrl+i vs tab, shift+enter).
  Key handling moved from `key.Type`/`key.Runes` to `key.Code`/`key.Text`/`key.Mod`
  (the in-house keymap still funnels everything through `fromkeymsg.go`'s `String()`);
  the single `tea.MouseMsg` split into four messages normalised into one `mouseEvent`;
  `Model.View()` now returns a `tea.View` (alt-screen/mouse declared there, not via
  program options); and lipgloss v2 is "pure" so rendered-output tests `ansi.Strip`
  first. See [Foundation](/architecture/foundation.md) and
  [Keybindings](/architecture/keybindings.md).

## 2026-06-24

- **Editor undo fixed for insert mode.** `editor.undo`/`redo` now flush an open
  insert session before walking history, so `Ctrl+Z` while typing reverts the
  whole typed run as one unit and behaves the same from insert and normal mode
  (previously it ran against history with the in-progress insert still
  uncommitted, so it no-opped or desynced). See
  [Editor](/architecture/editor.md).

- **Explorer file operations (create / delete / undo).** New `fileops.go` adds
  `explorer.newFile` (`a`), `explorer.newFolder` (`A`), `explorer.delete` (`d`),
  and `explorer.undo` (`Ctrl+Z`). Every destructive step is gated behind a
  modal prompt; deletes move entries to a same-filesystem `.ike-trash/` so undo
  can restore them, and a linear op stack reverses the last create (delete it) or
  delete (restore it). The root model routes keys straight to a prompting
  explorer so typed names/answers are not stolen by other bindings. Removing a
  file (delete or undo-create) emits `FileDeletedMsg`, which the app handles by
  closing any editor still open on that path. See
  [File Explorer](/architecture/explorer.md).

- **Keybindings layer (Roadmap 0080).** New `internal/keymap` package: a
  chord/key model (`Key` + `Mod` bitset, multi-step `Chord`), canonical
  parse/format, the JetBrains-flavoured default set as data, context-scoped
  resolution (pane-scoped shadows Global), build-time conflict detection,
  platform normalisation (Cmd→Ctrl off macOS), a `tea.KeyMsg` adapter, a
  partial-chord resolver with 600ms timeout, and a cheatsheet view. Wired into
  `internal/app` dispatch: IDE-level chords resolve to registered command ids
  before pane routing (only modified chords in a capturing editor); inert/unbound
  chords fall through. Bindings reference command ids owned elsewhere and define
  no commands; `vcs.*` ids stay inert pending a future VCS roadmap. See
  [Keybindings & Shortcuts](/architecture/keybindings.md).

- **Pane focus: directional, geometry-aware.** `FocusDir` (Ctrl+arrow) now routes
  through a pure `focusTarget` scorer over the computed leaf rectangles. It ranks
  candidates in the travel direction by perpendicular-span overlap, then travel-
  axis distance, then perpendicular alignment — so focus-right lands on the pane
  beside you, not a wide full-width pane below whose centre happened to be closer
  by raw Manhattan distance.

## 2026-06-21

- **Editor: expand tabs when rendering.** `renderLine` now budgets by display
  cells and expands each tab to `tab_width` spaces. Previously it emitted raw
  tabs counted as one rune each; the terminal expanded them past the line's width
  budget, wrapping the line and pushing a split editor pane's bottom border off
  screen. Fixes the "split pane has no bottom border" bug on tab-indented files.

- **Command palette refinements (Roadmap 0070).** Box is now compact (half-width
  centered / pane-width anchored, each with a floor). Key bindings render as a
  highlighted chip pinned right of each row (title truncates first). Two new entry
  points: **esc-esc** opens the centered palette from a non-capturing context, and
  **`@` in an editor's normal mode** opens a slimmed, file-only palette *anchored*
  over the editor pane (`OpenAnchored` + `overlay.Place`, locked to `@` so no mode
  switching).

- **Command palette (Roadmap 0070).** New `internal/palette` overlay fronts every
  action: a leading prefix rune selects a `Mode` — `:` runs registry commands
  (snapshot per open, ranked context-first/global/off-context), `@` fuzzy-finds
  files by relative path (directory segments included). New `internal/fuzzy`
  matcher returns an optimal-alignment score + matched rune spans shared by
  ranking and highlighting. The palette is its own modal tea-model (the read-only
  floating shell can't take typed input); it dispatches `RunCommandMsg` /
  `OpenFileMsg` and executes nothing. Root model hosts it, toggles on `ctrl+p`
  (config `palette.toggle_key`), forwards keys, composites it centered. New
  `[palette]` config section (`max_results`, `default_mode`, `off_context`,
  `toggle_key`). The `plugin.Command.Scope` field it ranks by was already present.
  New concept doc [Command Palette](/architecture/command-palette.md).

- **Pane splitting & multiple editors (Roadmap 0037).** The fixed two-component
  root becomes a dynamic pane set. New `internal/pane` registry maps each layout
  leaf to a live instance (explorer singleton + N editors); focus is now the
  focused leaf. `internal/layout` gains `SplitLeaf`/`Close` (the create/close
  half, reusing `insert`/`remove`) and `DecodeTree`/`Leaves`. Binding-agnostic
  ops `SplitFocused`/`CloseFocused`/`FocusDir` + tab focus-cycle; mouse self-edge
  drag spawns a split. Open-in-new-pane rides an additive `NewPane` flag on
  `explorer.OpenFileMsg` / `host.OpenFileRequest` (+ `host.API.OpenFileIn`),
  defaulting to Replace. The layout store grows a per-leaf identity table
  (`{kind, path}`) so editors restore their files best-effort (missing file →
  empty editor); old bare-tree files still load. New concept doc
  [Pane Registry & Multiple Editors](/architecture/pane-registry.md); Pane Layout
  doc updated.

- **Pane-split rendering fixes.** `paneBox` now hard-clamps to its rect
  (`MaxWidth`/`MaxHeight` + title truncation) so a narrow split column can no
  longer wrap its title and overflow by a row — the overflow had pushed the whole
  tiling up (cut-off pane titles) and desynced mouse hit-testing from `m.lay`.
  Open-in-new-pane now splits the **active editor's** leaf rather than the focused
  explorer, so a file opened from the explorer lands in the editor area instead of
  shrinking the explorer.

- **Pane focus/close keybinds.** `Ctrl+W` closes the focused editor pane
  (`CloseFocused`; no-op on the explorer / last leaf). Spatial focus moves
  (`FocusDir`) get default **Ctrl+arrow** bindings, overridable via
  `keymap.bindings.focus_{left,right,up,down}`. Cmd is intentionally avoided —
  terminals don't deliver it to a TUI. Both are core keys; Roadmap 0080 owns the
  final keymap.

## 2026-06-20

- `F1` now opens the help overlay as an alias for `?`, and dismisses it as well
  (added to the floating shell's dismiss key set).

- Help overlay is now a **full reference**: it snapshots every registered command
  (`registry.Commands()`) regardless of focus, so the Editor section shows
  alongside Global and Explorer. Added a documentation-only `plugin.Command.Shortcut`
  hint — help falls back to it when no keymap resolves — so the editor's vim
  ex-commands (`:w`/`:q`/`:wq`) and modal keys (`u`/`ctrl+r`) display their
  shortcuts. Scope groups are now separated by a blank line for readability.

- Fixed explorer mouse-click desync after restoring a session with expanded
  directories: `clampScroll` now also clamps `offset` to `len(rows)-textH`.
  Restore runs at height 0 and parked an offset past the last page; `View`
  clamped it for display but `MouseClick`/hover read the raw offset, so clicks
  landed on rows far below the ones shown until the user scrolled.

- Session restore now also persists the editor's **viewport framing** (scroll
  `top`/`left`), not just the cursor. `Top` is sticky during editing, so cursor-
  only restore reframed the file and made mouse clicks land on the wrong lines.
  Saved offset is applied after the editor is first sized (`Model.pendingScroll`
  → `editor.SetScroll`). New `editor.ScrollOffset`/`SetScroll`.

- Added **session restore**: a per-project `session.json` (beside `layout.json`,
  same `IKE_CONFIG_DIR`/`.ike` discovery) saves the open file + cursor and the
  explorer's expanded dirs + show-hidden + cursor on quit, reapplied on launch.
  Explorer restore loads directories synchronously and `Init` skips its async
  scan once the root is restored. New `internal/app/session.go`,
  `internal/explorer/state.go`, `editor.SetCursor`/`CursorPos`,
  `app.quit()`/`restoreSession`. See `/architecture/session-restore.md`.

- `q` now quits the app from the editor too, when it is focused in normal mode
  (previously only from the explorer). Insert/command mode still routes `q` to
  the buffer. See `app.quitKey`/`app.isCoreKey`.

- Editor follow-ups: the visual selection is now rendered (per-cell highlight in
  `view.go`, cursor wins on overlap) and visual mode gained `i`/`a` text-object
  selection, `>`/`<` indent, and register-replace `p`. Added word navigation on
  `Shift+←/→` (+ `Ctrl+←/→`), paragraph jumps on `Shift+↑/↓`, page scrolling via
  `PgUp`/`PgDn` + `Ctrl-f/b/d/u`, screen jumps `H M L`, plus `~` toggle-case and
  `*`/`#` word search. Arrow/Home/End and the new motions also work mid-insert.
  Mouse click focuses the editor and positions the cursor.

- Editor (Roadmap 0060): the foundation's minimal modal editor is rebuilt into a
  full vim-like editor across focused sub-packages under `internal/editor/`:
  `buffer` (line slice + rune/byte `Position` + reversible `Apply(Edit)`),
  `mode`, `motion` (`h j k l w b e W B E 0 ^ $ gg G { } f t F T ; , %`),
  `textobject` (`iw aw`, bracket/quote pairs), `operator` (`d c y p gp`, doubled
  `dd cc yy`, `Compose`), `register` (`" a-z 0 - 1-9 +`), `history` (undo/redo +
  `.` repeat), `viewport` (scroll/scrolloff/gutter), `search` (`/ ? n N`, `\v`
  regex), and `excmd` (`:w :q :wq :q! :e`, `:<n>`). The `editor.Model` keeps its
  pane API (so the root is unchanged but for routing `ActionMsg` and using
  `Capturing()`); `commands.go` registers actions/ex-commands as plugin
  `Command`s dispatched via a single `ActionMsg` path; `events.go` is the LSP
  hook seam. `[editor]` config (tab width, expandtab, line numbers, scrolloff…)
  is read live via `Configure`.

- Help: command shortcuts now render. `plugin.Keymap` gained a `CommandID`
  field; `*registry.Registry` implements `help.BindingResolver` via a new
  `Binding(cmdID)` reverse-lookup, and the root wires it (was `nil`). Explorer
  default keymaps link to their command ids, so the cheat sheet shows e.g.
  `Explorer: Toggle Hidden Files  .`. Full keymap layer still owned by 0080.

- Explorer (Roadmap 0050): config-driven per-filetype colours (`colors.go`,
  glob→ext→`dir`/`default` resolution from `[explorer.colors]`), italic hidden
  entries with a `explorer.toggleHidden` runtime toggle (default off via
  `explorer.show_hidden`), indent guides sized by `explorer.tree_indent`, and
  async directory scans (`scanCmd`/`ScanDoneMsg`, no blocking IO in `Update`).
  Added registry commands + default keymaps (`toggleHidden` `.`, `refresh` `r`,
  `collapseAll` `c`, `reveal`) that dispatch explorer `Msg`s the root routes
  back. `host.Config` gained `Keys()` so the explorer can enumerate the dynamic
  `[explorer.colors]` section. Only the optional file-ops milestone remains.

- Explorer: hover highlight (mouse motion), an "open file" highlight distinct
  from cursor/hover (`SetActive`, set on open and cleared on editor close), and
  shift-wheel / horizontal-wheel sideways scrolling (`ScrollXBy`). Row styling is
  now resolved through a testable `rowKind` precedence: cursor > hover > active >
  dir > plain.

- Explorer (Roadmap 0050, partial): mouse navigation and scrollbars. The root
  model forwards in-pane mouse events to the explorer — left-press selects/
  activates a row, wheel scrolls without moving the cursor, scrollbar-track press
  jumps an axis. The explorer gained a horizontal scroll offset and renders
  conditional right/bottom scrollbars (dim track + heavier thumb, sized by
  `scrollThumb`) whenever content overflows the pane; rows are clipped with
  `ansi.Cut` so long names scroll sideways instead of wrapping.

- Roadmap 0040 (Settings / Configuration) implemented: new leaf-level
  `internal/config` package — typed `Config` sections (`schema.go`), in-code
  defaults (`defaults.go`), `~/.ike` + `{root}/.ike` discovery with
  `IKE_CONFIG_DIR` override (`discovery.go`), TOML decode isolated behind the
  package (`load.go`), deep map merge with scalar-replace / table-merge /
  list-replace semantics (`merge.go`), clamp-and-warn validation with non-fatal
  `Diagnostic`s and parse-error layer isolation (`validate.go`), an idempotent
  `Extension` registration hook (`extend.go`), `Load`/`Get`/`Set` accessors plus
  `Config.Flat` (`config.go`), a `ConfigReloadedMsg` reload seam (`watch.go`),
  and a typed setter seam `PushHistory` (`write.go`). `internal/host` now depends
  on `internal/config` via `host.FromConfig` (flat read-only view backing the
  plugin API); `internal/app.New` loads the merged config at startup. Backed by
  `BurntSushi/toml`. Tests cover precedence, table/list merge, clamp-and-warn,
  parse-error isolation, and extend round-trip (config 87% coverage).


- Roadmap 0036 (Pane Drag) implemented: new pure `internal/layout` split-tree
  (`tree.go` types + `Compute`/`Rects` exact tiling, `rect.go` hit-testing +
  drop zones, `resize.go` clamped divider drag, `move.go` drop-zone re-parent,
  `state.go` tolerant encode/decode). `internal/app` replaces hard-coded
  `explorerWidth`/`JoinHorizontal` with tree-driven `Rects`, adds a `tea.MouseMsg`
  drag state machine (press hit-test → resize/move, release commit), and a
  per-project layout store (`store.go`, `IKE_CONFIG_DIR`/`.ike/layout.json`,
  save-on-release, default fallback on stale state). `cmd/ike` enables
  `tea.WithMouseCellMotion`. New concept doc `architecture/pane-layout.md`.

- Roadmap 0110 (Themes) planned: added `roadmaps/0110-themes.md` and a stub
  concept doc `architecture/themes.md`. Semantic-slot theme model mirroring
  sqlit/Textual; built-in palettes (tokyo-night, nord, gruvbox, rose-pine,
  catppuccin); selector behind 0040's `[theme]`, registration via 0020. Stub is
  marked planned — not implemented yet.
- Roadmap 0035 (Floating Shell) implemented: extracted the one-off help overlay
  chrome into a reusable component. New `internal/overlay` (pure ANSI-aware
  `Center` compositing, moved out of `internal/app`) and `internal/ui`
  (`Floating` shell hosting any `ui.Content`; `sizing.go` content budget;
  `scroll.go` generalised scroller wrapping `bubbles/viewport`; `ModelContent`
  adapter to float a view-only model). `internal/help` refactored to a
  `ui.Content` provider (snapshot + column layout only); its local chrome,
  sizing, and scroll deleted. Root model now hosts one active `*ui.Floating`,
  forwards size + keys, and composites via `overlay.Center`. Added an additive
  in-process plugin seam, `host.OpenModalRequest{Title, View}`, so a plugin can
  present its pane as a floating modal; optional `overlay.*` config tuning
  (margin, max width/height fraction). Added the Floating Shell concept doc and
  updated Help Overlay.

## 2026-06-19

- Roadmap 0030 (Help Overlay) implemented: `internal/help` (`source.go` snapshot
  + binding join + scope grouping, `layout.go` responsive column-major packing,
  `viewport.go` vertical scroll wrapping `bubbles/viewport` with a position
  indicator, `help.go` overlay `tea.Model`). Root model hosts the overlay, opens
  it on `?`, forwards size + keys, and renders it on top. Binding resolver
  (roadmap 0080) consumed through a `BindingResolver` interface; not wired yet,
  so commands render title-only. The overlay renders as a content-sized floating
  pane centered over the layout (max two columns), composited via an ANSI-aware
  splice (`x/ansi`) so the base stays visible around it. Added the Help Overlay
  concept doc and the `bubbles` dependency.

- Roadmap 0020 (Plugins: Compile-in Registry) implemented: `internal/plugin`
  (Plugin interface + Command/Keymap/Pane/FileHandler/Hook capability types,
  Scope, ContextProvider), `internal/registry` (Register, conflict detection,
  deterministic ordering, enable/disable, lookups), `internal/host` (host.API +
  in-process impl). Root model now routes file opens through handlers, fires
  lifecycle hooks, resolves layered plugin keymaps, and exposes `RunCommand`.
  Added `plugins/example` reference plugin and the Plugin Extension Contract
  concept doc.

- Explorer reworked into an expandable tree rooted at a fixed project base:
  folders expand/collapse in place (`▾`/`▸`) instead of replacing the listing,
  and the explorer can no longer ascend above the root.
- Roadmap 0010 (Foundation) implemented: file explorer pane, modal vim editor
  pane, root model routing/focus/status line. Added concept docs for the
  foundation slice, explorer, and editor.
