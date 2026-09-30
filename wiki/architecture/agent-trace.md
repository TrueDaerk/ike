---
type: architecture
title: Agent Trace
description: Epic 0540 — the coding-agent session as a trace graph. This page covers the harness-neutral transcript model, the Claude Code JSONL parser with incremental tailing and edit-to-line resolution, and session discovery by working directory with fork exclusion (internal/agenttrace, #2842); the tool window, hooks, change-feed links and "ask" follow in their own sub-issues.
resource: internal/agenttrace
tags: [architecture, agents, claude, transcript, trace, discovery]
timestamp: 2026-09-30T18:00:00Z
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
  the change-feed link (#2838) joins against.

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

## Tests

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
  side that file nodes will link to (#2838).
- [Hierarchy Tree](/architecture/hiertree.md) — the tree the trace tool
  window is built on (#2840).
- [Deep Links](/architecture/deep-links.md) — the socket the hook push
  extends with an `event` message (#2843).
