---
type: architecture
title: Agent Trace
description: Epic 0540 — the coding-agent session as a trace graph. This page covers the harness-neutral transcript model, the Claude Code JSONL parser with incremental tailing and edit-to-line resolution, subagent transcripts joined under their Agent call and `Write` on an existing file as an edit (#2861), and session discovery by working directory with fork exclusion (internal/agenttrace, #2842), the Claude Code hook push — settings.json installer, `ike agent-hook` CLI, binding a session to its terminal (#2843) — and the Agent Trace tool window on hiertree with click-to-code, live updates and the install-hooks dialog (internal/tracepanel, #2840), its graph view — the session as a snake path of prompt, change and answer boxes with expand-in-place, the `agent.trace.view` setting and the tree toggle (#2858), the links from writing file nodes to change-feed entries with diff/revert and the feed's jump back (#2838), per-change diffs reconstructed from the transcript with their provenance and a session-before / git HEAD / working-file base switch (#2859), and `agent.ask` — a question about a node answered by a fork of the same session on a cheaper model, with its node context, settings and answer overlay (internal/agentask, #2845), and follow-up questions on the same fork, fork tagging and the trimmed context (#2844), the per-project session history — records written at turn boundaries, /clear boundaries, rewinds as abandoned branches behind a marker, the one-time import of Claude transcripts and the session picker with a read-only stored view (#2860) — plus keybinds and limitations (other harnesses) (#2839).
resource: internal/agenttrace
tags: [architecture, agents, claude, transcript, trace, discovery, hooks, tool-window, hiertree, change-feed, diff, ask, settings, history, rewind]
timestamp: 2026-10-01T22:00:00Z
---

# Agent Trace

Epic 0540 (#2837) ports three ideas from
[devdotfast/whiteboard](https://github.com/devdotfast/whiteboard) into a TUI:
a trace of what the coding agent decided and changed, click-to-code from that
trace, and asking a *fork* of the same session for rationale without touching
its history. The foundation is `internal/agenttrace` (#2842): a pure package
that turns a Claude Code transcript into a harness-neutral event model and
finds the transcript that belongs to a tool pane. Nothing in it draws, reads
config or knows about the app; later sub-issues (hook push #2843, tool window
#2840, change-feed links #2838, `agent.ask` #2845) build on it.

## Transcript model

```
Session { ID, ParentID, Harness, CWD, StartedAt, EndedAt, Events []Event, Malformed }
Event   { Kind (user|assistant|tool|separator), Turn, At, Text, Reasoning, UUID, Tool *Tool }
Tool    { ID, Name, Title, Input (raw JSON), Output, IsError, Truncated, Done, DoneAt, Paths []FileRef,
          Result (raw toolUseResult of a file-changing call), AgentID, Subagent *Subagent }
Subagent { ID, Type, Description, ToolUseID, Session *Session }
FileRef { Path, Line (1-based, 0 = unknown), Op (read|edit|write|create|delete) }
```

- **Turn** counts user prompts; every event carries the turn it belongs to,
  so a tree of turn → decision → tool call → file is a grouping of the flat
  slice, no second pass.
- **Assistant** events are text blocks; `Reasoning` marks a thinking block.
  Redacted thinking (empty text with a signature, the common case) yields no
  event.
- **Tool** events appear when the `tool_use` block arrives and are completed
  in place when the matching `tool_result` shows up in a later user line:
  `Done`, `IsError`, and the joined text output capped at `MaxOutput`
  (32 KiB, rune-safe, `Truncated` set). `Title` is the one-line label a tree
  row shows: the path for file tools, the description (else the first command
  line) for Bash, the pattern for Grep/Glob, the description for Agent.
- **Separator** events mark boundaries that are not prompts: a context
  compaction (`system/compact_boundary` or a user line flagged
  `isCompactSummary`) and the old-format `summary` line.
- **Session.Files()** flattens every `FileRef` in timeline order — the
  subagents' refs included, interleaved with the session's own calls by
  timestamp (#2861) — the input the change-feed link (#2838) joins against.
  `Tool.DoneAt` records when the result arrived; with the call's own time it
  bounds the link's window.
- **Tool.Result** keeps the structured result object (`toolUseResult`) of
  `Edit`, `MultiEdit`, `Write` and `NotebookEdit` — `originalFile`,
  `structuredPatch` — for the per-change diffs (#2859); `nil` for a
  plain-string result (a rejected or failed call).
- **Session.Timeline(node)** resolves a tree node to its event — following
  `Node.Agent` into subagent sessions — and returns the timeline holding it,
  which `agent.ask` reads its context from.

## Parser (`parse.go`)

`Parse(io.Reader)` reads a whole transcript; `Parser.Line([]byte)` is the
per-line state machine both `Parse` and the incremental `Reader` feed. One
Claude Code line is one JSON object with `type`, `uuid`, `timestamp`, `cwd`,
`sessionId`, and for `user`/`assistant` a `message` whose `content` is either
a string (a typed prompt) or an array of blocks (`text`, `thinking`,
`tool_use`, `tool_result`). Everything else — `attachment`,
`queue-operation`, `worktree-state`, `cost-state`, `last-prompt`,
`atis-latch`, `mode`, `pr-link`, `file-history-snapshot` — contributes at
most the session id, cwd and the started/ended timestamps.

What is dropped on purpose:

- `isSidechain` lines inside the main transcript (subagent traffic in older
  transcripts) and `isMeta` user lines (harness caveats, memory injections).
  Current subagent transcripts live in their own files and are read by a
  sidechain parser (see [Subagent transcripts](#subagent-transcripts-2861)).
- `<system-reminder>…</system-reminder>` blocks inside a prompt; the prompt
  is what the user typed.
- A user line that is only `<local-command-stdout>` (the echo of a local
  slash command). A slash command itself (`<command-name>/verify</command-name>`
  + `<command-args>`) becomes the prompt `/verify main.go`.
- A `tool_result` whose call was never seen (orphan after truncation).
- Blank lines; a line that is not a JSON object counts in
  `Session.Malformed` and is skipped.

### File references and line resolution

| Tool | Path field | Op | Line |
|---|---|---|---|
| `Read` | `file_path` | read | `offset` when given |
| `Edit` | `file_path` | edit | see below |
| `MultiEdit` | `file_path`, one ref per `edits[]` entry | edit | see below |
| `Write` | `file_path` | write; **create** when the result's `toolUseResult.type` is `create`, **edit** when it is `update` (an existing file overwritten, #2861) | `structuredPatch[0].newStart` for `update` |
| `NotebookEdit` | `notebook_path` | edit, or **delete** for `edit_mode: delete` | — |

An edit's line is resolved best effort, in this order:

1. `toolUseResult.structuredPatch[0].newStart` once the result has arrived —
   the harness recorded exactly where the hunk landed. A `MultiEdit`'s hunks
   come in file order, its edits in input order, so only an edit the search
   below could not place takes the hunk of its index.
2. Until then, `new_string` searched in the file *as it is now*
   (`Parser.ReadFile`, default `os.ReadFile`): after the edit the file
   contains the new text.
3. `old_string` as the fallback for an edit that was reverted or never
   applied.
4. `0` when the file cannot be read or neither string is in it.

`toolUseResult` is an object for a successful call and a plain **string**
for a rejected or failed one (`"Error: The user doesn't want to proceed…"`);
the string is ignored — the row stays, with its searched line and `✗ error`.
`replace_all` edits need no special case: the first hunk is the line.

`Write` on an existing file is shown as **edit**, not as a separate
`overwrite` op (#2861): for the trace an overwrite is a change to a file
that existed, which is what `edit` says; the extent is the diff's business
(#2859), and `create` stays reserved for a file the call brought into being.

The file cache lives per batch: `Parse` reads each file once, the `Reader`
clears it on every `Update`, so an edit from a later turn is resolved against
the file after the earlier turns' changes. Tests inject `ReadFile`.

## Incremental reader (`reader.go`)

`NewReader(path)` tails one transcript. `Update()` opens the file, seeks to
the kept byte offset, and feeds every line that ends in `\n`; a trailing
partial line — Claude Code is mid-write — is held back and re-read on the next
call, so a half-written JSON object never counts as malformed — however many
`write(2)` calls the line takes. A file that was **replaced** restarts from
byte 0 with a fresh `Session` (#2857): another file under the path
(`os.SameFile` fails — renamed over, deleted and recreated), one that shrank
below the offset (truncated), or one whose byte before the offset is no
longer the `\n` the last read stopped behind (truncated and rewritten past
the old size). A missing file is not an error: the first line may not exist
yet when the pane opens. `Update` returns how many events were added or
completed; `Revision()` changes with every update that changed the session
and every restart, which is what the tool window compares against the
revision it shows. `Read(view)` is `Update` plus a callback on the session
under the reader's own mutex — the host builds its tree there — so two
reads never race even when the host gave up waiting for one. `Session()` is
stable across calls and grows in place. `Load(path)` is one `Update` of a
fresh reader — the whole session, subagents included — for a one-off parse
(`agent.ask`'s context).

### Subagent transcripts (#2861)

Claude Code writes what a subagent (an `Agent`/`Task` call — builders,
explorers, general-purpose agents, where many edits happen) does to its own
file next to the main transcript:

```
<projects>/<encoded-cwd>/<session-id>.jsonl
<projects>/<encoded-cwd>/<session-id>/subagents/agent-<id>.jsonl      every line isSidechain
<projects>/<encoded-cwd>/<session-id>/subagents/agent-<id>.meta.json  {agentType, description, toolUseId, spawnDepth}
```

The main transcript carries only the `Agent` call and its summary result
(`toolUseResult.agentId`), so before #2861 every file a subagent touched was
invisible. The `Reader` is directory-aware: every `Update` reads the main
file, then lists `<session-id>/subagents/` (a subagent starts mid-turn) and
tails each `agent-*.jsonl` with its own offset, replacement check and
sidechain `Parser`. The meta file is read until it exists; its `toolUseId`
is the join key to the spawning call, the agent id the call's result
reports is the fallback when there is no meta file. The call is looked up in
the main timeline and in the other subagents' (a nested spawn), and the
subagent is attached as `Tool.Subagent` once found — re-attached after a
restart of the main file. A subagent read, reset or attachment counts as a
change, so `Revision()` moves and the tree is rebuilt within one tick.
Discovery and the hook binding are unaffected: the session id is the main
file's name, and the subagent directory is never listed as a session.

## Session discovery (`discover.go`)

Claude Code writes `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`
(`$CLAUDE_CONFIG_DIR/projects` when that variable is set; `ProjectsDir()`).
`EncodeCWD` reproduces the directory name: **every byte outside
`[A-Za-z0-9]` becomes `-`**, so `/Users/me/src/ike` is `-Users-me-src-ike`,
`/home/me/src/ike` is `-home-me-src-ike`, the worktree
`/Users/me/src/ike/.claude/worktrees/issue+1-x` is
`-Users-me-src-ike--claude-worktrees-issue-1-x`, and `C:\src\ike` is
`C--src-ike`. The mapping is lossy, so it only picks the directory to scan;
the match itself is the `cwd` field of the transcript's first lines.

`Discover(cwd, exclude...)` (and `DiscoverIn(projectsDir, …)` for tests)
returns the **newest non-fork** transcript whose recorded cwd is the
directory, `ErrNotFound` otherwise. `ListIn` returns all matches newest
first with forks marked. Details that matter:

- **Symlinks.** Claude Code records the resolved directory: a session started
  in `/tmp/x` on macOS says `/private/tmp/x`. Discovery scans both the
  encoded name of the directory as given and of its `EvalSymlinks` form, and
  compares cwds with symlinks resolved on both sides.
- **Header scan only.** `locate` reads at most the first 256 lines of each
  candidate for the session id, cwd, first timestamp and the uuid of the
  first `user`/`assistant` line; large transcripts are not parsed to be
  listed.
- **Exclusion list.** The caller can pass session ids to skip — the `agent.ask`
  command (#2845) knows the id of the fork it just spawned.
- **Ask tag.** A transcript whose preamble `queue-operation` line carries
  `agenttrace.AskMarker` (`<!-- ike:agent.ask -->`) at the start of its
  `content` is a fork `agent.ask` created (#2844): `locate` sets
  `Located.Asked` and `Discover` skips it. Claude Code records the print-mode
  prompt in that line, ahead of the copied history, so the tag sits inside
  the header scan and survives an IKE restart — unlike the in-memory
  exclusion list — and it still holds when the fork cannot be grouped with
  its original (original deleted).

### Fork detection

`claude --resume <id> --fork-session` was verified on Claude Code 2.1.280
(2026-09-30):

- **It works while the original session is still running**, including while
  the original is mid-turn (a `-p` fork answered while the interactive
  original was streaming a reply). The original keeps running and its
  transcript is not touched; the fork's answer goes into its own file.
- **The fork carries no parent marker.** The new file is the parent's
  conversation lines copied verbatim — same `uuid`s, same `timestamp`s, even
  the same `cost-state.startTime` — with only `sessionId` rewritten, preceded
  by the fork's own preamble (`mode`, `atis-latch`, a `queue-operation`
  stamped at fork time) and followed by the new turn. Nothing in either file
  names the other.
- **The fork lands under the cwd of the `claude -p` process**, not under the
  parent's. A fork started from another directory ends up in another project
  directory and cannot be correlated; `agent.ask` must run in the tool pane's
  cwd.

Because there is no marker, `Session.ParentID` is filled by discovery, not by
the parser: transcripts in one project directory that share the uuid of their
first message form a group; the oldest is the original and every other member
gets `ParentID` = the original's id. "Oldest" is decided by the file's birth
time (macOS `Birthtimespec`, Linux `statx` `STATX_BTIME`), then by the
timestamp of the first timestamped line (a `-p` fork opens with its own
queue line at fork time, the original at session start), then by
modification time. The upshot for the trace: right after an "ask" the fork is
the newest file in the directory, and `Discover` still returns the pane's
session.

## Claude Code hooks (#2843)

Discovery guesses; the hook push knows. Claude Code runs a command hook on
`SessionStart`, `SessionEnd` and `UserPromptSubmit` with the hook input JSON
(`session_id`, `transcript_path`, `cwd`, `hook_event_name`, …) on stdin.
IKE's hooks run `ike agent-hook <event>`, which forwards the session to a
running IKE, and IKE binds it to the terminal the agent runs in.

### Installer (`hooks.go`)

`agent.hooks.install` (palette, no default chord — a one-off setup step,
recorded in the keybind audit ledger) writes one matcher group per event into
Claude Code's user settings (`$CLAUDE_CONFIG_DIR/settings.json`, else
`~/.claude/settings.json`; `agenttrace.SettingsPath`):

```json
"hooks": {
  "SessionStart": [
    { "matcher": "", "hooks": [
      { "type": "command", "command": "/usr/local/bin/ike agent-hook SessionStart", "timeout": 5 } ] } ],
  …
}
```

The executable is the running binary (`os.Executable`, symlinks resolved),
single-quoted when it is not shell-safe. As in Whiteboard's
`agent-trace-hooks.ts`, **the command shape is the marker**: a hook is IKE's
exactly when its command is `<exe> agent-hook <SessionStart|SessionEnd|UserPromptSubmit>`
with an `exe` whose base name starts with `ike`, plain or quoted exactly as
IKE quotes (`agenttrace.IsIKEHook`). A shell compound (`ike agent-hook X; rm …`)
never matches, so uninstall can only remove what IKE wrote; install refuses a
binary not named `ike*` because its entries could not be recognised again.

- **Install** (`InstallHooks`) strips every IKE entry, then appends a fresh
  group per event — idempotent, and a moved binary is picked up by running it
  again. The result is compared to the file as JSON; an unchanged document is
  not rewritten ("already up to date").
- **Uninstall** (`agent.hooks.uninstall`, `UninstallHooks`) removes IKE
  commands only: a group keeps its foreign hooks, a group or event list left
  empty is dropped, and a `hooks` object left empty is removed. Install then
  uninstall returns the original document.
- The rest of the file survives: key order at every level (the settings
  object is edited as an ordered list of raw members), unknown keys, foreign
  hooks, strings unescaped. Output is two-space indented, Claude Code's own
  format. The write is atomic (temp file + rename) and keeps the file's
  mode (0600 for a new file). A file that is not a JSON object — or whose
  `hooks` is not one — is refused and left untouched.

### `ike agent-hook <event>` (`cmd/ike/agenthook.go`)

The first-argument subcommand (`cli.Invocation.AgentHook`) reads at most
1 MiB of hook JSON from stdin, keeps `session_id`, `cwd`, `transcript_path`,
takes the event name from the command line, adds `$IKE_SESSION` /
`$IKE_PID`, and sends a [deeplink `event` message](./deep-links.md#the-event-message-2843).
It **always exits 0** — a non-zero exit (2 in particular) from a
`UserPromptSubmit` hook would block the user's prompt — and prints a failure
to stderr only; no running IKE is not a failure.

### Binding in the app (`internal/app/agenthooks.go`)

Every terminal IKE spawns carries `IKE_SESSION=<terminal session key>` and
`IKE_PID=<IDE pid>` in its environment (`terminal.startSession`), so the hook
knows which terminal it ran in. An `AgentEventMsg` binds the session to:

1. the terminal whose session key is `ike_session`, when `ike_pid` is this
   process — pane and tab terminals of the active and parked workspaces, and
   parked global tools;
2. else a terminal whose live cwd is the event's cwd (symlinks resolved),
   preferring the one already bound to that session, then a tool pane, then
   a plain terminal without a live binding;
3. else nothing — the event is dropped.

The binding (`Model.agentSessions`, keyed by terminal session key) holds the
session id, transcript path, cwd and last event; `SessionEnd` keeps it but
marks it ended, and the next `SessionStart` (e.g. after `/clear`) replaces
it. `agentSessionLocator(t)` is what the trace window (#2840) calls off the
Update loop: a live hook binding wins, otherwise `agenttrace.Discover` on the
terminal's cwd.

## Trace tool window (#2840)

`agent.trace.toggle` (`cmd+alt+shift+a`, Tools menu, palette) opens the
singleton **Agent Trace** tool window (`pane.KindAgentTrace`, key
`agenttrace`, context `agenttrace`) through the shared
[tool-window wiring](./tool-panes.md) — `togglePanelWith` / `openToolPane`
at the adaptive `auxZone`, restored with the layout like every other
singleton, tabbable, numbered by the pane slots. The pane component is
`internal/tracepanel`; the app half is `internal/app/agenttrace_panel.go`.

### Tree

`agenttrace.BuildTree(session)` groups the flat timeline into
**turn → assistant decision → tool call → file**, a pure function the app
runs after every read:

- a **turn** row per user prompt (`#3 <first line of the prompt>`, the time
  as detail); events before the first prompt form a leading `session start`
  turn;
- a **decision** row per assistant text or thinking block (`thinking` as
  detail); tool calls issued before any assistant text of the turn nest
  under an implicit `tool calls` decision;
- a **tool** row per call: the tool name, then either its single file as the
  location (`Edit  main.go:3`), `N files`, or the call's title (`Bash Build
  and vet the module`); `✗ error` marks a failed result, `…` one that has
  not arrived;
- a **file** row per `FileRef` below the call (`edit  main.go:3`, `create
  hello.go`); a line the parser could not resolve shows the path alone;
- below an **Agent** row whose subagent was found (#2861), the subagent's
  tool calls with their file rows, nested the same way (a subagent's own
  `Agent` calls included); the row's detail is `<agent type> ·
  <description>`. The subagent's prompt and texts are not shown — the
  `Agent` row is the decision;
- a **separator** row for a compaction (`— context compacted —`).

Node keys come from event indices (`t3`, `e17`, `e17/f0`, `e17/x`), which the
append-only `Events` slice keeps stable, so the tree host can carry state
across rebuilds by key. A subagent's call extends the spawning call's key by
its index in the subagent's events (`e17/a4`, its files `e17/a4/f0`); the
node keeps the spawning call's `Turn` and `Event` and records the path into
the subagent sessions in `Node.Agent`. Change-feed links, `D`/`V` and `a`
work on these rows like on any other: links walk the whole tree, and
`agent.ask` resolves the node with `Session.Timeline`, so the context is the
subagent's call and the decision before it in the subagent's timeline.

### Pane

The panel is a [hiertree](./hiertree.md) host over `agenttrace.Node` with
the synchronous `hiertree.Static` fetch — the children are already in
memory. Every `Set` goes through `Tree.Refresh`, which keeps expanded rows
expanded and the cursor on the same key; the newest turn on the first
`Set`, and every node that appears later — a new turn, but also a decision,
call or file landing in the running turn — are expanded whole
(`ExpandDeep`), while a row the user folded stays folded.

**Following the tail (#2857).** The pane opens with the cursor on the
newest row and keeps it there: while the cursor sits on the last row, every
`Set` moves it onto the new last row, which scrolls the newest activity into
view. Moving the cursor off the last row (keys, click, wheel, `Select`)
stops following and the selection then stays put across updates; moving
back onto the last row resumes it.

A header line names the session and tells "no new lines" from "not
reading": `11111111 · hook · 3 turns · read 14:05:09 +2 · ⇢ claude
(focused)  ~/.claude/projects/…` — `scan` instead of `hook` when discovery
found it, `· ended` after SessionEnd, the time of the last read with the
number of events it added or completed (`+0` on an idle tick, `not read
yet` before the first), and the followed terminal with why it was picked
(`focused`, `last focused`, `hook-bound`, `tool pane`, `only terminal`, or
`project root`). A hint row closes the pane.

Keys: the tree's own (`j/k`, page keys, `space`/`l` expand, `h` fold or
walk to the parent), `enter` opens the row's file — or folds/unfolds a row
that has none — `r` looks the session up again, `i` installs the Claude
hooks, `D` shows what a change row did ([Diffs](#diffs-2859)), `V` the revert of a linked row (below), `a` asks the agent about the row (#2845, below). Mouse: a click selects, a click on the marker cell folds, a second
click within `ui.DoubleClickWindow` opens (the shared list-mouse gesture),
the wheel scrolls through `Tree.Wheel`.

**Click to code.** A row with a `FileRef` — every file row, and a tool row
whose call touched exactly one file — yields `tracepanel.OpenLocationMsg`
with the 0-based line (−1 when unknown), which the root model routes into
`openPathAt`: the same pipeline as the terminal's `file:line` links and the
`ike://open` deep link, so navigation history, the Source-view switch and
the focused-pane rules all apply.

### Graph view (#2858)

The tree answers "what happened"; the **graph** answers "what changed",
and it is the view the pane opens in (setting `agent.trace.view`, Settings
UI page *Agent Trace*, `graph` by default; `tree` opens the tree). It draws
the session as a path of boxes: for every turn the prompt, then one box
per file a tool call created, wrote, edited or deleted, in order, then the
agent's answer — and the next turn continues the path. Reasoning and the
tool calls are secondary: they live *inside* a change box and show on
expand.

```
┌?─────────────────────┐   ┌✎─────────────────────┐   ┌+─────────────────────┐
│#1 Add a greeting to …│──▶│main.go               │──▶│hello.go              │
└─ 14:00 ──────────────┘   └─ edit :3 +1 −0 ──────┘   └─ create ─────────────┘
                                                                  │
                                                                  ▼
┌✓─────────────────────┐   ┌✕─────────────────────┐   ┌✎─────────────────────┐
│I'll read main.go, th…│◀──│notes.ipynb           │◀──│main.go               │
└─ ended on a tool c… ─┘   └─ delete ─────────────┘   └─ edit :2 ×2 ─────────┘
            │
── #2 ──────┼───────────────────────────────────────────────────────────────────
            ▼
┌?─────────────────────┐         ┌✓─────────────────────┐
│#2 /verify main.go    │──▶─◇───▶│Done: main.go greets,…│
└─ 14:01 ──────────────┘         └─ 14:02 ──────────────┘
```

**Path model.** `agenttrace.BuildPath(session) []Stop` is the pure
reduction (`path.go`), rebuilt from the same `Session` on every read like
the tree. A `Stop` is a prompt (`t<turn>`, `#<turn> <first line>`, the
whole text in `Text`), a change (the file node's key `e<i>/f<n>` — a
subagent's `e<i>/a<j>/f<n>` — so the change-feed links of #2838 apply
unchanged; label the file name, detail `edit :12`, `create`, `delete`,
`×2` for several edits of one file in one call, `+12 −3` when the result
recorded a patch, `✗` on a failed call, `…` while pending; `Calls` and
`Context` carry the call(s) and the assistant text/reasoning that preceded
it in the turn), an answer (`t<turn>/end`, stable whether the turn's final
text exists yet: `Implicit` when the turn ended on a tool call, `Pending`
for the last turn until the next prompt lands or the host's `Settle`
marks the session ended) or a separator (`e<i>`, a compaction, drawn as a
`◇` marker on the path, not selectable). Consecutive calls to the same
file are separate boxes; several edits inside one call stay one. Prompt
and answer labels are the first non-empty line with whitespace collapsed
(`Collapse`), system reminders already stripped by the parser.

**Layout.** `tracepanel/snake.go` is pure over widths: `BoxWidth(paneW)`
picks the box width — as many boxes per row as keep each at least 22
cells, else at least 18, capped at 28; a pane narrower than two 18-cell
boxes is a single column of full-width boxes — and `Snake(widths, paneW,
expanded, detailH)` places the slots: left-to-right from the top-left, a
turn down when the next box would not fit, then right-to-left, and so on
(boustrophedon). The first box of a row sits directly under the last box
of the row before (sharing the edge the path came from), so a turn is a
`│` over a `▼` in the two rows between — the rows breathe instead of
stacking — and boxes on a row join with `──▶` / `◀──`. **A new question
breaks the row (#2866):** every prompt stop but the first starts a new row
even when it would still fit (`Snake` takes the break indices; `graphLayout`
passes every prompt after the first), the path turning down as at a full
row and continuing its boustrophedon direction; a faint full-width turn
rule `── #<turn> ──…` sits between the rows (three gap rows: `│`, the rule
crossed by `┼`, `▼`), so a session of many turns reads as separate
questions. Compaction separators are unaffected. In a 12-row bottom pane
two box rows plus their gap still fit. Boxes are
three rows — top border with the kind glyph (`?` prompt, `✎` edit/write,
`+` create, `✕` delete, `✓` answer, `✗` failed call, `…` pending; the `Δ`
of a linked change sits before the right corner), the label, the detail
set into the bottom border as `└─ detail ─┘`, cut so a space and at least
one `─` stay on both sides and the frame stays closed at every box width;
the canvas gives every rune exactly the cells its width claims (a wide
rune never straddles the edge or leaves half a glyph behind) — so kinds
read without colour; the border
colour (accent, warning, success, error, info) adds the second cue. The
expanded box's detail block is inserted beneath its row and pushes the
rows below down. The renderer draws onto a cell canvas (`graph.go`) and
the mouse hit test (`Layout.At`) reads the same slots.

**Selection and keys.** One box is selected; `h`/`l` (`left`/`right`)
walk the path, skipping separators, `j`/`k` (`down`/`up`) jump to the
nearest box on the row below/above on screen (`Layout.Below`/`Above`),
`g`/`G` the first/last box, page keys scroll. `enter` on a change box
opens the file at the line (`OpenLocationMsg`, the click-to-code pipeline
above); a second `enter` on the same box, or `space`, expands it in place
into the detail block — the call(s) with name, title and status, the
preceding assistant text (`›` lines), the location with the patch summary
and the Δ mark, and the keys — `space` again collapses, and only one box
is expanded at a time (`esc` collapses too). `enter` on a prompt or answer
box shows the whole text markdown-rendered in the floating shell
(`ShowTextMsg` → `openTraceText`, the ask overlay's renderer). `D` / `V` /
`a` act on the box like on a tree row: the box's key is the file node's
key, so a change box opens its diff, a linked one answers the feed's
revert, and
`Current()` presents the box as a node (a change as its file node) for
`agent.ask` and the links. `t` toggles graph ↔ tree in either view
(`agent.trace.view` from the palette does the same; it stays palette-only
because `t` is the pane's own key, recorded in the keybind ledger); the
pane remembers the pick for the running IKE session, so a reopened pane
comes back in the view it was closed in, and the setting is the default
for a fresh session. Mouse: a click selects, a second click within the
double-click window opens, the wheel scrolls.

**Live updates.** `SetPath` installs every read's path next to the tree:
the selected and the expanded box survive by key; while *following* — the
newest box selected and the window at the end — the selection moves onto
the newest box and the newest turn scrolls into view; a wheel up, or
moving the selection off the newest box, parks the window until the user
returns to the end (`G`). The feed's back-link (`Select`) lands on the
change box, also when it names the tool node's key.

### Following the agent pane and reading live

The trace follows one terminal, picked on every lookup (`traceTargetNow`):
the focused pane's tool terminal; else the tool terminal the keyboard last
sat in (`setFocus` records it); else the best live terminal — one with a
live hook binding, then any tool pane, then a plain terminal; else no
terminal and the project root. The session is then the terminal's hook
binding when live, otherwise `agenttrace.Discover` on its cwd
(`locateAgentSession`, off the Update loop).

One `agenttrace.Reader` tails the located transcript. While the pane is open
a one-second tick (`traceTickMsg`, generation-guarded like the other
tickers) runs an incremental read (`Reader.Read`) and regroups the tree
**off the loop**; the on-loop side only ever sees the finished `[]Node`.
The tick re-locates when the followed terminal (its key and cwd — never the
reason, so focus moving from the agent pane to the editor costs nothing)
changed, every fifth tick while nothing is found or the session has ended,
and immediately on a hook push (`AgentEventMsg`), on the pane's `r`, when
the pane is re-focused through its toggle, and after a hook install
finished. Closing the pane ends the chain; a pane restored with the layout
starts it from `Init`.

The read side is built so that no lost message can freeze the pane (#2857):

- **One read in flight**, and a read asked for meanwhile (a hook push or a
  relocation landing mid-read) is remembered and runs the moment the
  current one reports — a push's new lines never wait for the next tick.
- **A read is applied by reader, not by generation.** A read of the current
  `Reader` is shown even when a relocation started while it ran — its
  events are consumed either way; only a read of a replaced reader is
  dropped.
- **The pane re-renders on revision, not on a count.** It rebuilds the tree
  whenever the reader's `Revision()` differs from the one shown (or the
  session facts changed), so a dropped result, or a completion that only
  changed a row, still reaches the screen.
- **Lost work is revived.** A read that never reported (its command
  panicked; the crash guard swallows the message) stops blocking after
  ten seconds — the reader serializes the two. A poll chain whose tick was
  lost (the pane was in another workspace, a recovered panic) is restarted
  by the next relocation, read or toggle once no tick was armed for three
  intervals (`ensureTraceTick`).

**Root cause of #2857** ("the pane does not update live"): the reading
worked — the transcript was tailed every second — but the tree grew *below
the fold*. The cursor stayed on the oldest turn, the view never scrolled,
decisions arriving in the running turn came up collapsed, and during one
long agent turn even the header's turn count stood still, so a growing
session looked exactly like a frozen one. Following the tail, expanding new
nodes and the header's read/`+N`/follow segments fix what the user sees;
the chain defects above (dropped in-flight reads, the `added > 0` gate, a
chain or busy flag that never recovered) were fixed with it. Hook delivery
skips dead sockets without dialling (see
[deep links](./deep-links.md#the-event-message-2843)).

### Change-feed links (#2838)

A file node whose tool call wrote the file (every op but `read`) links to
the [change-feed](./change-feed.md) entry the write produced.
`agenttrace.Link` (`link.go`, pure) matches on all three of:

- **path** — the node's file is the entry's (relative paths resolve against
  the session cwd);
- **time** — the entry's span (`First`…`Time`, the oldest and newest
  coalesced event) overlaps the tool call's window: from the `tool_use`
  line (`Node.At`) to the `tool_result` line (`Node.Until`, from
  `Tool.DoneAt`), widened by `LinkSlack` (5 s) for the watcher's debounce
  and the two clocks; a pending call stays open for `LinkPending` (10 min —
  a permission prompt can hold the write back);
- **source process** — the entry's `SourceKey` is the session key of the
  terminal the trace follows. The feed sets the key only when exactly one
  terminal was busy at the write, so an ambiguous moment (two processes, or
  the same program in two panes) stays unattributed and never links, and a
  trace that follows no terminal links nothing.

Unmatched nodes stay plain. A tool row that touched a single file shares its
file node's link. The back-link of an entry names the file node that best
explains its newest write: the latest call issued by then, else the earliest
one issued after it (it matched only inside the slack).

The app relinks on every read (`syncTraceLinks`, once per poll tick) and
keeps the last result on the model (`traceLinks`), so the feed can jump back
even while the pane is closed. Linked rows carry a `Δ` in their detail and
route into the feed's **own** handlers — no copies: `D` shows the feed's
exact diff of the entry in the [diff view](#diffs-2859) (whose `f` opens the
feed panel on the entry, i.e. its mini-diff), and `V` (`ChangeRevertMsg`)
raises the feed's revert confirmation. The feed's `t` jumps the other way (`jumpToTraceNode`): an
open pane is focused and `Select` unfolds the node's ancestors and puts the
cursor on it; a closed pane opens and the pending `traceJump` is selected
by the first read.

### Diffs (#2859)

Every change box and writing file node shows what the agent changed, also
for writes the feed never saw (a session opened later, an ambiguous write).
`agenttrace.Diffs(session)` (`diffs.go`, pure) reconstructs one
`ChangeDiff` per writing call and file — `Key` the file node / change stop
key, `Keys` every node showing it (all file nodes of the path in the call,
the tool node when the call touched one file) — with `Before` / `After`
(whole contents, when known), unified `Hunks`, `+N −M` counts and its
**provenance** (`DiffSource`), in order of preference:

| Source | Label | When |
|---|---|---|
| `DiffFeed` | exact (change feed) | the node links to a feed entry with its pre-change content (set by the app; the feed's before against the file now) |
| `DiffPatch` | from structuredPatch | the result recorded hunks; `originalFile` / `originalFileContents` is the whole before, the patch applied to it the after |
| `DiffStrings` | from old/new string | `Edit` / `MultiEdit` without a patch: the replacements placed in the content the session last saw — a `Read`'s output (whole when the result's `file` span says so, else the window with its line numbers) or an earlier change's after — else in the file on disk when it still holds every new text (`Note` says so), else a bare hunk at an unknown position |
| `DiffSession` | from earlier session content | a `Write` without a result, against the content the session saw before |
| `DiffCreate` | new file | a `Write` whose result says `create` |
| `DiffAfterOnly` | after only | a `Write` whose previous content is unknown — no counts |

The parser keeps a `Read`'s span (`Tool.Span`, from `toolUseResult.file`)
for this. Rejected and failed calls have no diff and leave the session's
view of the file alone; `NotebookEdit` has none. The few line diffs the
reconstruction needs (placed strings, a `Write` against earlier content) run
through a small bounded Myers in `linediff.go` — `internal/diff` carries a
UI half the pure package does not import. `BuildTree` and `BuildPath` take
the counts on every read through a cheap pass (no disk, whole contents
assembled only where a later count needs them): the file row's detail and
the change box's second line show `+N −M`.

**The view.** `D` on a change box or writing file node (`DiffMsg`, reads
and other rows stay silent) parses the transcript again off the loop
(`traceDiffCmd`; subagent calls included), picks the node's diff with
`agenttrace.DiffFor` and reads both comparison bases: the working file and
its git HEAD blob (`vcs.DetectRoot` + `vcs.HeadContent`). A linked node
takes the feed's exact diff instead. The floating shell shows the file, the
op and tool, the turn, `+N −M` and the provenance (plus its note), a
`ui.Segmented` strip of the bases and the diff: whole contents through the
engine and the change feed's renderer (`miniDiffLines`), hunks only
through the ask overlay's markdown renderer in a `diff` fence, an after-only
write as all-added. `1` / `2` / `3` switch the left side between *session
before* (default), *git HEAD* and *working file*; the right side stays the
change's after, so HEAD and the working file answer "what of this change is
still there" ("no differences — … holds exactly this change's result").
A base that cannot be used is marked `✗` with its reason under the strip
and refuses the key with a notice: HEAD outside a repository ("not a git
repository") or for a file HEAD does not track, the working file once it
is gone, and both while only hunks of the change are known. `f` opens the
feed panel on a linked entry, `esc` closes, the rest scrolls.

**Ask context.** `agent.ask` (#2845) still prefers the linked feed entry's
hunk; without one the fork's context carries the reconstructed one
(`agentask.SessionHunk`, computed off the loop with the node context, under
the same `MaxHunkLines` / `MaxHunkLine` / `MaxHunkBytes` caps).

### Empty state

No transcript for the followed directory is actionable — start the agent
or install the hooks — so the pane draws the **centered dialog** the
missing-tool states use, not a one-line notice: heading, the directory
looked at, and an action strip built from `ui.Segmented`
(`[Install Claude hooks] [Rescan]`, the primary in accent) that takes
clicks at the drawn position; `i` / `enter` install, `r` rescans. A read
error is shown in the box; a pane too small for it falls back to a
one-line notice. While the lookup runs the pane says so and offers nothing.

## Ask the agent (#2845)

`agent.ask` (`cmd+alt+shift+q`, `a` in the trace pane, Tools menu → *Ask the
Agent…*, palette) asks the traced session *why* — about the selected node,
or about the session as a whole when no row is selected. The answer comes
from a **fork** of that session (`--resume <id> --fork-session`, see the
fork notes above): the fork has the whole conversation as context, runs on
a cheaper model without tools, and writes its answer into its own
transcript. **The original session's JSONL is never written to** — the only
process IKE starts is the fork, and `internal/app/agentask_test.go` asserts
the transcript is byte-for-byte unchanged after an ask through a fake
`claude`. The pure half is `internal/agentask`; the app half
`internal/app/agentask.go`.

### Prompt and context

The command needs an open Agent Trace with a session (otherwise a notice
says what to open). It opens a `ui.Field` prompt in the floating shell —
the rename prompt's twin: typing edits, paste and `ctrl+u` work, `enter`
asks, `esc` cancels — with a faint `about:` line naming the node. On enter
the run goes off the Update loop: the transcript is parsed once more
(`agenttrace.Parse`, independent of the pane's reader, which is never
shared across goroutines) and `agentask.NodeContext` derives the node's
context, which is prefixed to the question:

| line | source |
|---|---|
| `turn: #3 (2026-09-30 14:03)` | the turn and its prompt line's timestamp |
| `file: main.go:12 (edit)` | the node's `FileRef` — file rows and single-file tool rows |
| `tool: Edit main.go` | the tool call's name and title, whitespace collapsed to one line, capped at `MaxTool` = 300 runes |
| `tool output:` + a fenced block | the call's result text when it is textual and at most `MaxOutput` = 1500 runes; a binary (NUL or invalid UTF-8), harness-truncated or larger output becomes `tool output: omitted (binary)` / `omitted (N bytes, too large)` instead |
| `assistant said: …` | the decision's own text, or the assistant text that preceded the tool call in that turn (never a thinking block; capped at `MaxAssistant` = 2000 runes) |
| `diff:` + a ```` ```diff ```` block | the linked change-feed entry's diff (#2838) through `agentask.Hunk`: unified by `UnifiedHunks`, capped at `MaxHunkLines` = 60 lines of at most `MaxHunkLine` = 200 runes and `MaxHunkBytes` = 4000 bytes in all (cut at a line with a trailing `…`); left out entirely when either side is binary or larger than `MaxDiffInput` = 256 KiB |

The prompt is then `AskMarker` + `Context from the session trace …` +
`Question: …` (`agentask.Prompt`); an empty context (a turn row, no row, or a
follow-up) sends the marker and the bare question. The marker is an HTML
comment the model reads past; it is what tags the fork (see *Ask tag*
above and *Fork tagging* below).

### The command

`agentask.Command` assembles

```
claude -p --resume <session-id> --fork-session --model <agent.ask.model> \
  --tools "" --max-turns <agent.ask.max_turns> --output-format json \
  --append-system-prompt "Explain only; do not modify files." "<prompt>"
```

and `agentask.Run` executes it **in the session's cwd** (a fork started
elsewhere lands in another project directory and the resume is refused),
capturing both streams, cancellable through a context. The JSON result
(`type: result`, `result`, `session_id`, `subtype`, `is_error`,
`duration_ms`, `num_turns`, `total_cost_usd`) is `agentask.ParseResult`;
a stream of objects is tolerated by taking the `result` line. Failures are
typed: `ErrNotInstalled` when `claude` is not on PATH, `*ResumeError`
when stderr says the session is unknown (`No conversation found …`), a
harness-reported error (`error_max_turns`, `is_error`) with the fork id
kept.

The fork's session id is remembered on the model (`askForks`) and passed
to discovery as an exclusion (`locateAgentSession` → `DiscoverIn(…,
exclude…)`), belt and braces over the fork grouping: right after an ask the
fork is the newest file in the project directory, and the trace must keep
following the original.

### Overlay

The same shell shows every phase (`askContent`, a `ui.Content` rendering at
the shell's width budget):

- **running** — the app-wide braille spinner (`askSpinMsg`, 200 ms,
  generation-guarded), the model, the elapsed time and the question; `esc`
  cancels the process.
- **answered** — the question in bold, the injected context in faint text
  (only while `agent.ask.show_context` is on; paths shortened through
  `displayPath`), the answer as glamour-rendered markdown
  (`agentask.RenderMarkdown`: the issues pane's theme-bound style, hanging
  list indents, wrapped at the budget), and a footer with duration, cost
  and the fork's short id. The render is cached per width. `esc` / `q`
  close; other keys scroll the shell.
- **failed** — the shell in the error accent titled *Ask failed*: the
  message, plus what to do (install Claude Code / put `claude` on PATH; a
  refused resume names the directory the fork ran in).

A result that lands after `esc` (a cancelled run, or a superseded ask) is
dropped by generation; a late spinner tick never reopens the shell.

### Settings

All three live on the **Agent Trace** page of the Settings UI (validated
in the form, persisted at user scope, shown in the list):

| Setting | Default | Values |
|---|---|---|
| `agent.ask.model` | `sonnet` | `sonnet`, `opus` or a full model id — one word (`config.AgentAskModelError`, shared by the config validator, which falls back to `sonnet`, and the form, which refuses) |
| `agent.ask.max_turns` | `1` | 1–5 (`--max-turns`; the config validator falls back to 1, the form clamps) |
| `agent.ask.show_context` | `true` | show the injected context in the answer overlay |

### Follow-up questions

`f` on an answer (the footer says `f follow up`) turns the overlay back
into the prompt, the earlier exchange staying above it (question in bold,
answer rendered; cached per width and length because the prompt re-renders
on every key). The next question resumes **the fork**, never the original:
`agentask.FollowUp` is `Command` on the fork id without `--fork-session`, so
the fork's conversation continues and the original session id appears
nowhere in the argv; the prompt carries no context (the fork has it). The
running view says `asking fork <id>`.

The fork id is kept per traced session and node for the IKE session
(`askNodeForks`, keyed by `askForkKey` — session id plus node key, the
session id alone for a session-wide ask). Asking about the same node again
opens the prompt in follow-up mode (`follow-up on fork …`); `ctrl+n` there
drops the fork and the history, so the next question forks the original
afresh (and that fork becomes the node's). A follow-up refused with a
`*ResumeError` (the fork file is gone) forgets the node's fork.

### Fork tagging

Every fork IKE creates is kept out of the trace three ways:

- **Discovery** skips the fork ids of this IKE session (`askForks`) and —
  from the transcript itself, across restarts — every transcript tagged with
  `AskMarker` (`Located.Asked`).
- **Hooks.** `agentask.Run` starts `claude` with `IKE_AGENT_ASK=1`
  (`agentask.EnvAsk`); `ike agent-hook` exits silently when it is set, so the
  fork's `SessionStart` / `UserPromptSubmit` / `SessionEnd` never reach IKE.
  Without that, the fork's events (same cwd) would bind the fork to the
  traced tool pane and the trace would switch to it.
- **The tree.** `BuildTree` leaves out every turn whose prompt starts with
  the marker (question and answer), should a tagged fork ever be traced: its
  copied history shows, the asks do not.

## History (#2860)

The trace follows the live session; the **history** keeps the sessions it
followed, the states a session went through when the user reset or rewound
it, and — once imported — the sessions that ran before IKE watched them.
Everything lives in `internal/agenttrace/history.go` (the record and the
store), `internal/tracepanel/history.go` (the picker and the stored view) and
`internal/app/agenttrace_history.go` (when records are written, the import,
the commands).

### Record format

A session is stored as `.ike/agent-trace/<session-id>.json` under the
project's state directory (`$IKE_CONFIG_DIR/agent-trace` when the variable
is set, like the local history). The record is the harness-neutral model,
versioned (`"v": 1`; a newer version is refused with `ErrRecordVersion`, an
older one read with zero defaults) and independent of Claude's transcript
schema — never a copy of the transcript:

- header: `id`, `harness`, `cwd`, `transcript` (the file it was read from,
  kept for `agent.ask`), `started_at` / `ended_at`, `ended`, `source`
  (`live` or `import`), the transcript's `transcript_size` /
  `transcript_mod` at write time (import idempotence), and the picker's
  columns `turns`, `files_changed`, `first_prompt`;
- `events`: every `Event` as kind, turn, time, text, `thinking`,
  `abandoned` and — for a tool call — name, title, error/done and the
  `FileRef`s (`path`, `line`, `op`); a subagent's events nest under its
  call (`agent`). Tool inputs and outputs are dropped; a prompt or answer
  keeps 4 KiB, an intermediate text or thinking block 512 bytes;
- `rewinds`: the `Rewind` list (`from`, `to`, `at`);
- `diffs`: the reconstructed changes (#2859) as `RecordDiff` — key(s),
  path, op, tool, turn, provenance, hunks and line counts, never the whole
  before/after contents.

`agenttrace.NewRecord(session, transcript, ended)` builds it;
`Record.Session()` rebuilds a `Session` whose `KnownDiffs` are the stored
diffs, so `BuildTree`, `BuildPath` and `Diffs` work on a stored session
like on a live one (the counts on the boxes and `D` included).

**Caps.** One record is at most `MaxRecordBytes` (512 KiB): `Encode` drops
the hunks of the largest diffs first (their counts stay), then halves the
text caps, and marks the record `truncated`. The `Store` keeps at most
`agent.trace.history_max_sessions` records (Settings UI page *Agent Trace*,
1–500, default 50): every `Save` prunes the oldest by session end (then
start), so the directory is bounded at cap × 512 KiB. Writes are atomic
(temp file, rename).

### When records are written

The live read (`traceReadCmd`) builds the record under the reader's lock
and writes it whenever the session's turn count, rewind count or ended
state moved since the last write (`traceRecordDue`) — i.e. at every turn
boundary, on a rewind and on `SessionEnd` — so a crash loses at most the
running turn. A write failure is reported once per distinct error.

**Session boundaries.** A `/clear` starts a new Claude session: with hooks
the new `SessionStart` rebinds the terminal and the trace relocates; without
them the project directory's modification time is polled every few ticks
(`agenttrace.DirStamp`) and a new transcript triggers a relocation. Either
way the session the trace leaves is filed as ended (`traceCloseCmd`) and the
new one gets its own record; both are listed in the picker.

### Rewinds

Claude Code's `esc esc` rewinds to an earlier message: the next prompt is
written with a `parentUuid` naming the line *before* the picked message
instead of the tail. The parser keeps every line's `parentUuid`
(`Event.ParentUUID`) and the event count after every line id; a prompt or
assistant line whose parent is a known id other than the tail is a rewind
(`Session.Rewinds`): the events after the parent are marked `Abandoned` in
place and the line reports a change, so the tailing host re-reads. A
`tool_result` whose parent is its own `tool_use` is *not* a rewind — that is
how parallel tool calls chain (the real transcripts in `~/.claude/projects`
show exactly this shape, and nothing else, as a parent-chain break).

`BuildTree` and `BuildPath` follow the live branch. Where the transcript
went back they insert a marker: a `NodeRewind` root (`↶ rewound`, detail
`N turns abandoned`, key `rw<index of the rewinding event>`) whose children
are the abandoned events grouped into turns of their own (turn keys
prefixed `rw<i>/`, event keys unchanged), kept folded until expanded; and a
`StopRewind` box (faint, glyph `↶`) whose `Branch` is the abandoned path —
`space` or `enter` expands it into a block listing the branch's prompt,
changes and answer. Nested rewinds (going back past an earlier rewind)
nest their markers: every event belongs to the innermost rewind that
abandoned it (`branchOwners`). An abandoned turn's answer is never
"working …".

The rule was verified against a real `esc esc` → *Restore conversation*
rewind on Claude Code 2.1.285: the rewind stays in the same transcript
file (no new session id), the restored prompt text comes back into the
input, and the next prompt line carries the `parentUuid` of the line before
the picked message — parsed into one rewind with the picked turn abandoned.
The `rewind.jsonl` fixture holds the same shape (plus a parallel tool pair
as the negative case).

### Import (`agent.trace.import`)

`agenttrace.Import(store, projectsDir, cwd, progress)` scans the project's
transcript directories (as given and symlink-resolved) and writes a record
for every original session not yet stored — or stored from an older file
(size or mtime differ). Forks and ask-tagged forks are skipped
(`ImportResult.Forks`), a file without a session header or without events
(a malformed transcript) is reported in `Failed`, never fatal. The app runs
it off the loop; progress goes through the host's coalescing outbox into
the status bar segment `⇣ agent sessions 3/12` (`agentimport`, a click
opens the picker), and the result is one notice: `N imported, M unchanged,
K forks skipped · unreadable: …`. Re-running is idempotent. The command
is palette-only (ledger: occasional).

### Picker and stored view

`s` in the pane (any state, the empty one included) or
`agent.trace.history` lists the store (`HistoryMsg` → the app's `List` →
`OpenPicker`): newest first, one row per session — `●` for the live one,
the start date, the duration (`1h05m`, `live`), turns, files changed and
the first prompt. The list is built from the shared blocks: `ui.LineSearch`
(`/` filters by date, prompt and id with smartcase; `enter` keeps the
filter, `esc` drops it, a second `esc` closes), `ui.ListNav` with
`ui.ClampWindow`, `ui.RenderWindow` / `ui.ListPaneView`, and the list-mouse
`ClickTracker.ClickRow` / `WheelWindow`. `enter` (or a double click) on a
stored session asks for it (`ShowHistoryMsg`); the app loads the record
off the loop and hands its tree and path to `SetStored`.

A stored session is shown read-only: the header reads `history ·
<date> · <id> · N turns · esc live`, the tree opens on the first turn and
the graph on the first box (history is read from the start), no read or
follow status is shown and the change-feed links are empty. The ticks keep
reading and filing the *live* session meanwhile — they just leave the pane
alone (`traceShowingHistory`) — and `esc` or `r` (`LiveMsg`) return to it,
located and read afresh. `agent.ask` on a stored session forks its id
when the transcript still exists; otherwise `a` explains that Claude Code
pruned it and only the record remains.

## Keybinds

| Chord | Command | Where |
|---|---|---|
| `cmd+alt+shift+a` | `agent.trace.toggle` | global; also in the Tools menu |
| `cmd+alt+shift+q` | `agent.ask` | global; `a` inside the trace pane |

`agent.trace.view` (graph ↔ tree, #2858) and `agent.trace.history` (the
session picker, #2860) are palette-only: the pane's own `t` and `s` are the
bindings, as the keybind ledger records; `agent.trace.import` (#2860) is an
occasional one-off, palette-only too.

In-pane keys (`enter`, `r`, `i`, `D`, `V`, `a`, `t`, `s`, `esc` on a stored
session) are listed under
[Pane](#pane) and [Graph view](#graph-view-2858); the diff view's `1` / `2`
/ `3` / `f` under [Diffs](#diffs-2859); the answer overlay's `f` / `ctrl+n` under
[Follow-up questions](#follow-up-questions); the feed's `t` under
[Change-feed links](#change-feed-links-2838).
The history picker's keys (`/`, `enter`, `esc`, `q`) are under
[History](#history-2860).

## Limitations

- **Claude Code only.** The model is harness-neutral (`Session.Harness`),
  but the only parser, discovery rule, hook installer and ask runner are for
  Claude Code (`HarnessClaude`). Other harnesses (Codex, Aider, Gemini CLI,
  …) show the empty state; supporting one means a parser into the same
  `Session`/`Event` model, a discovery rule for its transcript location and,
  optionally, a hook push.
- Without hooks a session is found by working directory only; two sessions
  in one cwd bind to the newest — install the hooks for an exact binding.
- `agent.ask` needs the `claude` CLI on `PATH` and spends tokens on the
  chosen model; it explains only (`--tools ""`) and never edits files.
- Change-feed links exist only for writes the feed saw during this ike
  session. Diffs without one are reconstructed from the transcript and are
  only as exact as their provenance says: a string placed in the file on
  disk or a bare hunk may sit at the wrong spot, a `Write` of a file the
  session never saw has no before.

## Tests

`internal/agenttrace/tree_test.go` builds the tree from `basic.jsonl` (the
full outline, details and refs, the implicit decision and the pending mark)
and checks that keys and rows survive an incremental append;
`internal/hiertree/static_test.go` covers `Static`, `Refresh` keeping
expansion and selection (and clamping when the selected row is gone),
`ExpandDeep`, `Toggle`, `Wheel` and path-less rows;
`internal/tracepanel/tracepanel_test.go` the newest-turn expansion, enter
on tool and file rows (line −1 for a create), the live append keeping a
collapsed row collapsed and the cursor in place, click/double-click/marker
clicks, and the dialog's keys and button hit test against the drawn
columns; `internal/app/agenttrace_panel_test.go` the toggle lifecycle, the
lookup → read → tree pipeline against a transcript under a temporary
`CLAUDE_CONFIG_DIR`, enter opening the editor at the hunk line, the tick
reading an appended turn without losing the selection (and dying when
stale or after close), the empty state's actions, and the followed
terminal (focused tool pane, last tool pane, hook binding, project root).
`internal/app/agenttrace_live_test.go` (#2857) drives the real tick → read
→ re-arm chain through a small goroutine runtime while a fake agent appends
a turn every 100 ms: the tree grows within two ticks without a key press and
follows the newest row with the editor focused, keeps a moved selection and
a fold with the tool terminal focused, a `UserPromptSubmit` push with a
stale `ike_pid` binds by cwd and reads before the next tick, an in-flight
read survives a relocation, and a lost chain and a lost read recover.
`reader_test.go` covers a transcript renamed over (same size) and rewritten
in place, and a line arriving in many writes; `tracepanel_test.go` the
tail-follow, new nodes in the running turn arriving expanded, and the
header diagnostics.

The graph view (#2858): `internal/agenttrace/path_test.go` reduces
`basic.jsonl` to the expected stop sequence (prompt, changes in order with
ops and reconstructed counts, the implicit answer of a turn that ended on a tool
call, the separator, the explicit answer), the stops' refs, calls and
context, the pending answer of a running turn and key stability across
an append, ask turns and read-only calls left out; `internal/tracepanel/graph_test.go`
the snake at widths 40/80/120 (row 1 left→right, row 2 right→left, …,
rows joined under the last box, no box cut at the border, `j`/`k`
neighbours), the single column below two boxes and the expanded row
pushing the rows below down, the rendered boxes, connectors and glyphs,
the keys (`h/l/j/k/g/G`, `enter` open / second `enter` and `space`
expand, `ShowTextMsg` on prompt and answer, separators skipped), linked
boxes answering `D`/`V`/`a` and `Select` by file or tool key, selection and
expansion surviving a live append with a pending answer settling, the
newest-turn auto-scroll parked by a wheel up, click/double-click/wheel
against the drawn geometry, and the kinds telling apart by glyph;
`internal/app/agenttrace_graph_test.go` the pane opening in the configured
view, `agent.trace.view` and `t` toggling with the pick remembered for a
reopened pane, a live append in graph mode keeping the selected and
expanded box, enter opening the editor at the hunk line and the prompt
text in the shell; `internal/config/agent_trace_validate_test.go` and
`internal/settings/agent_trace_test.go` the setting's validation, Settings
UI entry and persistence.

`internal/agenttrace/subagent_test.go` (#2861) parses `edits.jsonl` —
`Edit` with a string (rejected) result in a line that also has a thinking
block, `Edit` with `replace_all`, `MultiEdit` with one edit placed by the
search and one by its hunk, `Write` `update` and `create` — into `edit` file
rows with lines (and the kept structured results), and reads the
`subagent/` session directory: the subagent's calls below the `Agent` row,
`Session.Files()` in timeline order, `Timeline` on a subagent node, the
main file's own sidechain line still dropped, `Load`; a subagent file
appearing while the reader tails (attached once its meta file names the
call), a join by agent id without a meta file plus a nested spawn, and
re-attachment after the main file was replaced.
`internal/agentask/agentask_test.go` checks a subagent node's context;
`internal/app/agenttrace_subagent_test.go` the app end to end — the row
appearing on the next read, the feed link, `D`, `V` and `a`.

`internal/agenttrace/link_test.go` covers change-feed matching (#2838):
path, window edges and slack, reads never linking, unattributed and
foreign-terminal changes staying plain, a relative pending write, and the
back-link picking the newest cause. `tracepanel_test.go` checks the `Δ`
mark, `D`/`V` on linked and unlinked rows and `Select` unfolding a folded
turn; `agenttrace_panel_test.go` runs both directions end to end — `D`
opening the feed on the entry with its mini-diff, `V` its revert prompt, the
feed's `t` focusing the pane on the node, and the pending jump.

`internal/agenttrace/diffs_test.go` (#2859) reconstructs `basic.jsonl`
(`Edit` from its patch with whole before/after, `Write` create, the
patch-less `MultiEdit` placed in the content the `Edit` left), `edits.jsonl`
(the rejected edit without a diff, `replace_all`, `MultiEdit` and `Write`
update from their patches) and `readedit.jsonl` — a whole `Read` → patch-less
`Edit` (context and whole contents from the read), a partial `Read` → `Edit`
at the read's line numbers, a `Write` against the edited content, an
after-only `Write`, an `Edit` placed on disk, a bare one, a rejected one —
plus the cheap counting pass skipping the disk, the labels, hunk merging
and patch application; `linediff_test.go` the line script and subagent
keys. `internal/tracepanel/diffdetail_test.go` checks `+N −M` in the tree
row and the change box and `D` on change and read rows;
`internal/agentask/sessionhunk_test.go` the ask fallback hunk and its cap;
`internal/app/agenttrace_diff_test.go` the view end to end in a temporary
git repository — provenance header, `esc`, the HEAD and working-file bases,
HEAD disabled for an untracked file and outside a repository, the
working file refused while only hunks are known — and the ask fallback.

`internal/agenttrace/hooks_test.go` round-trips install → install →
uninstall against a settings file with foreign hooks and unrelated keys
(document equal after the round trip, key order and file mode kept), a moved
binary with a quoted path, file creation, a group shared with foreign hooks,
malformed files left untouched, and the marker matcher's compound/quoting
cases. `internal/deeplink/event_test.go` covers the wire form, every
validation refusal, the length cap, an open-only endpoint refusing events,
owner-first routing, and delivery past six crashed instances' leftovers and
a pid-alive-but-unbound socket (removed) and a refusing live instance (kept)
in well under the hook timeout (#2857); `cmd/ike/agenthook_test.go` the stdin → event mapping;
`internal/app/agenthooks_test.go` binding by pane key, by cwd, unmatched
events, the discovery fallback and both commands against a temporary
`CLAUDE_CONFIG_DIR`.

`internal/agenttrace/testdata/basic.jsonl` is a current-format session with
a typed prompt carrying a system reminder, thinking + text blocks, `Read`,
`Edit` (with `structuredPatch`), `Write` (result `create`), a failing
`Bash` with array content, `MultiEdit` and `NotebookEdit` in one message, a
slash command, local-command output, an `isMeta` caveat, a sidechain line, a
`compact_boundary`, a malformed line and a `cost-state` trailer.
`fork.jsonl` is what `claude -p --resume --fork-session` writes: preamble,
copied root, new question. The discovery tests copy these into a temporary
projects root (session ids and cwd rewritten) to cover fork marking, the
exclusion list, encoded-name collisions, trailing slashes and the symlinked
cwd; `TestParseRealTranscript` parses a real file when `AGENTTRACE_SAMPLE`
points at one.

`internal/agentask/agentask_test.go` covers #2845's pure half: the node
context of file, decision and turn rows (the preceding decision, never the
thinking block; clipping), prompt composition, the exact argv, unified
hunks with their cap, result parsing (single object, stream, harness
errors, garbage), and `Run` through a fake `claude` script on PATH (argv
and cwd recorded; missing binary → `ErrNotInstalled`; a refusal on stderr
→ `*ResumeError`; cancellation; `IKE_AGENT_ASK` in the fork's
environment), and #2844's context trimming — the one-line clipped tool
title, tool output kept or replaced by its binary / too-large note, `Hunk`
dropping binary and oversized sides and capping line width and bytes — plus
the marker on every prompt and the `FollowUp` argv.
`internal/agenttrace/discover_test.go` covers the ask tag (a tagged fork
with no original to group with is skipped; untagged, it is discovered) and
`tree_test.go` the dropped ask turns; `cmd/ike/agenthook_test.go` the
silent hook under `IKE_AGENT_ASK`. `internal/config/agent_ask_validate_test.go`
and `internal/settings/agent_ask_test.go` the defaults, the validator
fallbacks, `Flat`, and the form (refused multi-word model, clamped turns,
the toggle, list rendering). `internal/app/agentask_test.go` the overlay
lifecycle end to end against the same fake: prompt → run → answer with
context and rendered markdown, the transcript byte-identical afterwards,
the fork recorded, `show_context` off, the two error dialogs, `esc` while
running with the late result ignored, the pane's `a`, the empty-question
note, paste and `ctrl+u`, and the session-wide ask; the follow-up chain
(`f` → prompt on the fork with the history shown → argv resuming only the
fork, tagged, context-free → both exchanges in the overlay → the original
transcript untouched → reopening the node continues the kept fork →
`ctrl+n` forks afresh) and a vanished fork being forgotten.

`internal/agenttrace/rewind_test.go` (#2860) parses `rewind.jsonl`: the one
rewind with its abandoned range, `ParentUUID` on every event, the parallel
tool pair that is no rewind, the tree and path markers with the abandoned
branch behind them (keys, detail, no pending answer) and a rewind arriving
incrementally; `history_test.go` round-trips `basic.jsonl` and
`rewind.jsonl` through the store (tree, path, rewinds and diffs identical),
the summary columns and the dropped inputs/outputs, the size cap trimming
hunks, pruning oldest first and the version refusal, the import over a
projects root (two originals stored, a plain and an ask-tagged fork
skipped, a malformed file reported, idempotent, a grown transcript
re-imported) and `DirStamp`. `internal/tracepanel/history_test.go` covers
`s` in both states, the picker (rows, live mark, `/` filter, miss, enter,
esc, mouse), the stored header with `esc`/`r`, and the rewind marker folded
in the tree and expandable in the graph. `internal/app/agenttrace_history_test.go`
covers the record written on the first read, not on an idle one, on a new
turn and on the end; a hook `/clear` closing the first record and starting
the second, the picker switching to the stored session while live reads
keep filing, `esc` back; `agent.ask` on a stored session with and without
its transcript; and `agent.trace.import` with its status segment and
notice. `internal/config/agent_trace_validate_test.go` and
`internal/settings/agent_trace_history_test.go` cover
`agent.trace.history_max_sessions`.

## Related

- [External-Change Feed](/architecture/change-feed.md) — the diff/revert
  side writing file nodes link to, and the `t` back-link (#2838).
- [Hierarchy Tree](/architecture/hiertree.md) — the tree the trace tool
  window is built on: `Static`, `Refresh`, `ExpandDeep` were added for it
  (#2840).
- [Deep Links](/architecture/deep-links.md) — the socket the hook push
  extends with an `event` message (#2843).
- [Tool Panes](/architecture/tool-panes.md) — the tool panes a hook binds a
  session to and the shared wiring the trace window is opened through.
- [Shared Building Blocks](/architecture/shared-building-blocks.md) — the
  catalog the pane is assembled from (`hiertree`, `ui.Segmented`, the
  list-mouse helpers, `togglePanel` / `openToolPane`).
