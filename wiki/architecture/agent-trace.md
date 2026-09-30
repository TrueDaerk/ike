---
type: architecture
title: Agent Trace
description: Epic 0540 — the coding-agent session as a trace graph. This page covers the harness-neutral transcript model, the Claude Code JSONL parser with incremental tailing and edit-to-line resolution, and session discovery by working directory with fork exclusion (internal/agenttrace, #2842), the Claude Code hook push — settings.json installer, `ike agent-hook` CLI, binding a session to its terminal (#2843) — and the Agent Trace tool window on hiertree with click-to-code, live updates and the install-hooks dialog (internal/tracepanel, #2840), and the links from writing file nodes to change-feed entries with diff/revert and the feed's jump back (#2838); "ask" follows in its own sub-issue.
resource: internal/agenttrace
tags: [architecture, agents, claude, transcript, trace, discovery, hooks, tool-window, hiertree, change-feed]
timestamp: 2026-09-30T23:00:00Z
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
Tool    { ID, Name, Title, Input (raw JSON), Output, IsError, Truncated, Done, Paths []FileRef }
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
- **Session.Files()** flattens every `FileRef` in timeline order — the input
  the change-feed link (#2838) joins against. `Tool.DoneAt` records when the
  result arrived; with the call's own time it bounds the link's window.

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

- `isSidechain` lines (subagent traffic in older transcripts) and `isMeta`
  user lines (harness caveats, memory injections).
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
| `Write` | `file_path` | write, upgraded to **create** when the result's `toolUseResult.type` is `create` | — |
| `NotebookEdit` | `notebook_path` | edit, or **delete** for `edit_mode: delete` | — |

An edit's line is resolved best effort, in this order:

1. `toolUseResult.structuredPatch[0].newStart` once the result has arrived —
   the harness recorded exactly where the hunk landed.
2. Until then, `new_string` searched in the file *as it is now*
   (`Parser.ReadFile`, default `os.ReadFile`): after the edit the file
   contains the new text.
3. `old_string` as the fallback for an edit that was reverted or never
   applied.
4. `0` when the file cannot be read or neither string is in it.

The file cache lives per batch: `Parse` reads each file once, the `Reader`
clears it on every `Update`, so an edit from a later turn is resolved against
the file after the earlier turns' changes. Tests inject `ReadFile`.

## Incremental reader (`reader.go`)

`NewReader(path)` tails one transcript. `Update()` opens the file, seeks to
the kept byte offset, and feeds every line that ends in `\n`; a trailing
partial line — Claude Code is mid-write — is held back and re-read on the next
call, so a half-written JSON object never counts as malformed. A file that
shrank below the offset (rotated, truncated) restarts from byte 0 with a
fresh `Session`. A missing file is not an error: the first line may not exist
yet when the pane opens. `Update` returns how many events were added or
completed, which is what a tool window uses to decide whether to re-render;
`Session()` is stable across calls and grows in place.

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
- a **separator** row for a compaction (`— context compacted —`).

Node keys come from event indices (`t3`, `e17`, `e17/f0`, `e17/x`), which the
append-only `Events` slice keeps stable, so the tree host can carry state
across rebuilds by key.

### Pane

The panel is a [hiertree](./hiertree.md) host over `agenttrace.Node` with
the synchronous `hiertree.Static` fetch — the children are already in
memory. Every `Set` goes through `Tree.Refresh`, which keeps expanded rows
expanded and the cursor on the same key; the newest turn on the first
`Set` and every turn that appears later are expanded whole (`ExpandDeep`),
so the latest activity is visible without a keystroke. A header line names
the session (`11111111 · hook · 3 turns · ~/.claude/projects/…`, `· ended`
after SessionEnd; `scan` when discovery found it), a hint row closes the
pane.

Keys: the tree's own (`j/k`, page keys, `space`/`l` expand, `h` fold or
walk to the parent), `enter` opens the row's file — or folds/unfolds a row
that has none — `r` looks the session up again, `i` installs the Claude
hooks, `D` / `V` show the mini-diff / revert of a linked row (below). Mouse: a click selects, a click on the marker cell folds, a second
click within `ui.DoubleClickWindow` opens (the shared list-mouse gesture),
the wheel scrolls through `Tree.Wheel`.

**Click to code.** A row with a `FileRef` — every file row, and a tool row
whose call touched exactly one file — yields `tracepanel.OpenLocationMsg`
with the 0-based line (−1 when unknown), which the root model routes into
`openPathAt`: the same pipeline as the terminal's `file:line` links and the
`ike://open` deep link, so navigation history, the Source-view switch and
the focused-pane rules all apply.

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
tickers) runs an incremental `Update()` and regroups the tree **off the
loop** — at most one read in flight, and the on-loop side only ever sees the
finished `[]Node`, so the reader's session is never shared. A read that
added nothing leaves the pane untouched. The tick re-locates when the
followed terminal changed, every fifth tick while nothing is found or the
session has ended, and immediately on a hook push (`AgentEventMsg`), on the
pane's `r`, when the pane is re-focused through its toggle, and after a
hook install finished. Closing the pane ends the chain; a pane restored
with the layout starts it from `Init`.

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
answer two keys that route into the feed's **own** handlers — no copies:
`D` (`ChangeDiffMsg`) opens the feed panel on that entry, i.e. its
mini-diff, and `V` (`ChangeRevertMsg`) raises the feed's revert
confirmation. The feed's `t` jumps the other way (`jumpToTraceNode`): an
open pane is focused and `Select` unfolds the node's ancestors and puts the
cursor on it; a closed pane opens and the pending `traceJump` is selected
by the first read.

### Empty state

No transcript for the followed directory is actionable — start the agent
or install the hooks — so the pane draws the **centered dialog** the
missing-tool states use, not a one-line notice: heading, the directory
looked at, and an action strip built from `ui.Segmented`
(`[Install Claude hooks] [Rescan]`, the primary in accent) that takes
clicks at the drawn position; `i` / `enter` install, `r` rescans. A read
error is shown in the box; a pane too small for it falls back to a
one-line notice. While the lookup runs the pane says so and offers nothing.

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

`internal/agenttrace/link_test.go` covers change-feed matching (#2838):
path, window edges and slack, reads never linking, unattributed and
foreign-terminal changes staying plain, a relative pending write, and the
back-link picking the newest cause. `tracepanel_test.go` checks the `Δ`
mark, `D`/`V` on linked and unlinked rows and `Select` unfolding a folded
turn; `agenttrace_panel_test.go` runs both directions end to end — `D`
opening the feed on the entry with its mini-diff, `V` its revert prompt, the
feed's `t` focusing the pane on the node, and the pending jump.

`internal/agenttrace/hooks_test.go` round-trips install → install →
uninstall against a settings file with foreign hooks and unrelated keys
(document equal after the round trip, key order and file mode kept), a moved
binary with a quoted path, file creation, a group shared with foreign hooks,
malformed files left untouched, and the marker matcher's compound/quoting
cases. `internal/deeplink/event_test.go` covers the wire form, every
validation refusal, the length cap, an open-only endpoint refusing events and
owner-first routing; `cmd/ike/agenthook_test.go` the stdin → event mapping;
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
