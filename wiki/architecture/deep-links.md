---
type: architecture
title: Deep Links (ike:// URL scheme)
description: The ike:// URL scheme — parse/normalise/resolve in internal/deeplink, per-instance socket hand-off, history→projects-dir→clone resolution, file/tool payload after the switch, OS registration per platform (#2396)
resource: internal/deeplink
tags: [deeplink, url-scheme, ipc, project-switching]
timestamp: 2026-09-02T00:00:00Z
---

# Deep Links (ike:// URL scheme)

E-mail has `mailto:`; IKE has `ike://` (#2396): a click in a browser, a chat
message, an Obsidian note or an OSC 8 terminal hyperlink switches the running
IKE to the project that belongs to a git remote (or a project name),
optionally opens a file at a line and shows a tool window.

## URL format

```
ike://open?remote=<git remote url>[&file=<path>[:<line>]][&tool=<tool name>]
ike://open?project=<directory name>[&file=<path>[:<line>]][&tool=<tool name>]
```

- `remote` — any spelling of a git remote (`git@github.com:a/b.git`,
  `https://github.com/a/b`, `ssh://git@github.com/a/b`, with or without
  `.git`). Both the link and the local remotes normalise to a canonical key —
  `host/owner/repo`, lower-case, no `.git` (`deeplink.NormalizeRemote`, the
  one `ParseRemote` implementation `internal/forge` now delegates to).
- `project` — the project's directory name (basename of its root); plain name
  only, separators are rejected.
- `file` — path relative to the project root, optional 1-based `:<line>`
  suffix, percent-encoded. Absolute paths and any `..` traversal are refused —
  a link must not address files outside the project.
- `tool` — a tool-window name (`terminal`, `vcs`, `problems`, `structure`,
  `usages`, `http`, `debug`, `breakpoints`, `explorer`, or a `[[tools.custom]]`
  name).

Exactly one of `remote` / `project` is required. Unknown parameters are
ignored; a malformed URL produces one notification and nothing else. Parser,
matcher and resolution pipeline live in **`internal/deeplink`** — a pure leaf
package (no bubbletea) with full unit tests.

## Hand-off to the running instance

Every instance listens on a **unix domain socket** under the user state
directory (`$IKE_CONFIG_DIR/deeplink` else `~/.ike/deeplink`, dir 0700,
socket 0600, one per pid — `deeplink.Serve` in `internal/deeplink/ipc.go`).
The socket accepts exactly one message form, `open ike://…\n` (8 KiB cap);
anything else is answered with an error and dropped, and the receiver
re-parses the URL before acting. A sidecar `.focus` stamp file (touched on
`tea.FocusMsg`) marks the most recently focused instance; `deeplink.Send`
tries sockets newest-stamp-first and removes dead ones as it goes.

`ike ike://…` on the command line (`internal/cli`) delivers to a running
instance and exits, or starts the IDE and resolves the link after startup.
`ike --url-send-only ike://…` is the OS handler's probe: deliver-or-exit-1,
never start the IDE.

## Resolving the target project

`deeplink.Resolve` runs a fixed pipeline (all hits skip stale paths):

1. **Recent-projects history** — entries now carry a `remotes` field
   (canonical keys, filled by `project.NewEntry` on every recorded open;
   pre-#2396 entries are read live from the checkout). Worktrees resolve like
   git does: a `.git` *file* (`gitdir:`) leads to the linked gitdir, whose
   `commondir` holds the shared config. Exactly one hit → switch. Several
   (clones/worktrees) → a chooser dialog, most recently opened first (digit
   picks, enter takes the default, esc cancels).
2. **Projects directory scan** — every direct child (one level, no recursion)
   of `project.directory`; remotes read for `remote=`, the directory name
   compared for `project=`. A hit switches and is recorded into the history.
3. **Clone** — `remote=` only: the existing Clone Repository dialog opens,
   pre-filled with the linked URL **verbatim** — nothing is ever cloned
   without the user confirming there. A successful clone continues with the
   switch and the file/tool payload. `project=` links with no hit just notify
   "project not found".

Only switching to an already known local project runs without a prompt.

## What the switch does

The seamless switch (`performSwitch`) runs as usual; the link's payload parks
in `dlPending` (riding the model rebuild like #2394's pending open) and the
`SwitchedMsg` handler finishes: the tool window opens first (only when not
already open — never a second instance), then `file` opens through the normal
file-open funnel (`openPathAt`, landing in the file-editor slot the layout's
flex area resolves to) and jumps to `line` — so focus ends on the file when
one was given, on the tool otherwise. A missing file is a notification; the
switch stands.

`project.open_link` (palette; audit-ledger entry, no default chord) opens a
one-line paste prompt for entering an `ike://` URL by hand.

## OS registration

- **macOS** — `scripts/install-desktop.sh` compiles an `Ike Link Handler.app`
  applet (osacompile, `on open location`) declaring `CFBundleURLTypes` for
  `ike` and forwarding the URL to `ike-gui`. The scheme is deliberately *not*
  declared in `deploy/Info.plist`: Ike.app's executable is a shell script and
  cannot receive the GetURL Apple Event.
- **Linux** — `deploy/ike.desktop` declares `MimeType=x-scheme-handler/ike;`
  and `Exec=ike-gui %u`; the install script runs
  `xdg-mime default ike.desktop x-scheme-handler/ike`.
- **Windows** (documented; no build exercised yet) — register under
  `HKCU\Software\Classes\ike`: default value `URL:IKE deep link`, string
  value `URL Protocol` (empty), and `shell\open\command` =
  `"C:\path\to\ike.exe" "%1"`.

`ike-gui` with a URL argument first probes a running instance
(`ike --url-send-only`); only when nobody answers does it open a terminal
window running `ike <url>`. Raising the terminal window on a click is
best-effort — the switch itself always happens.

## Security

Links are untrusted input: the parser rejects file paths that escape the
project root, the socket is user-only and single-message, incoming URLs are
re-parsed before anything acts, cloning always goes through the confirmed
dialog showing the URL verbatim, and `ike-gui` refuses quoting-hostile URL
strings before they reach a shell.
