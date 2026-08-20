---
type: concept
title: Remote File Browsing (SFTP)
description: "#1997 — an SSH host profile browsed as an explorer-like pane over the sftp subsystem of the user's own ssh; remote files download into a local cache and open read-only (viewers get the local copy), saves are blocked, never silently redirected."
resource: internal/remote
tags: [architecture, remote, sftp, ssh, pane, read-only]
timestamp: 2026-08-20T00:00:00Z
---

# Remote File Browsing (SFTP) (#1997)

Working with a remote host used to mean terminal round trips — `scp` the
file, open it locally. The remote browser folds that into the IDE: the same
`~/.ssh/config` host list the [SSH terminal picker](./terminal.md#ssh-host-profiles-1938)
offers (plus the `terminal.ssh_hosts` extras) can be **browsed** — an
explorer-like pane listing the host's directories and files, remote files
opening through a local download cache.

Three pieces carry it:

- `internal/remote` — the connection seam (`Conn`, dialed over the user's own
  ssh), the cache mapping, the virtual-path helpers, and the pane model.
- `internal/pane` — `KindRemote`, keyed `remote:<alias>` (the alias is the
  identity, like the ES console's endpoint): one browser per host, a second
  open refocuses.
- `internal/app/remote.go` — the `remote.browse` command ("Browse SSH Host
  (SFTP)…", palette and Tools menu), the locked host-picker mode, the pane
  wiring, and the download-and-open funnel.

## The connection is the user's ssh

`remote.Dial` spawns `ssh -o BatchMode=yes -s <alias> sftp` and speaks the
SFTP protocol over the subprocess's stdio (`github.com/pkg/sftp`,
`NewClientPipe`). This is the #1938 stance applied to file transfer: keys,
agents, `ProxyJump`, `known_hosts`, per-host options — all of it stays ssh
resolving its own config for the alias verbatim, IKE re-implements none of
it. A `ControlMaster` setup accelerates the dial for free, exactly as it
would in a shell.

**BatchMode is the error surface.** A host that would need an interactive
prompt (password, passphrase, unverified host key) cannot hang a background
dial — ssh fails fast and its own stderr tail becomes the pane's notice
("Permission denied (publickey)…"), suffixed with the one hint that matters:
interactive auth is not available here, set up a key or agent (or connect
once via a terminal for the host-key prompt). `ConnectTimeout=15` bounds a
dead host; a bounded stderr buffer keeps a garbage-streaming host from
growing memory.

The pane model only ever sees the `Conn` interface (`Home`, `ReadDir`,
`Fetch`, `Close`) — tests drive it with an in-memory fake, so the SFTP layer
stays mockable. `Fetch` reads through `io.LimitReader(max+1)`, so the cap
refuses without buffering what it refuses.

## The pane

`remote.Model` follows the archive viewer's tree shape and the ES console's
async discipline: **no network round trip ever runs inside Update**. The
dial is the pane's background `Init`; every directory listing is a
background `scanCmd`; all results funnel into one `ResultMsg` the root model
routes back by pane key (`remoteResult`). A result whose pane is gone is
discarded — and a *connect* landing after its pane closed closes the
just-dialed session, or the ssh subprocess would linger.

- **Reveal at home.** After the connect the tree (rooted at `/`) descends
  toward the remote `$HOME` (`pendingReveal`), expanding loaded ancestors
  and scanning the first unloaded one — the explorer's reveal walk — so the
  pane opens on the home directory, not a bare `/`.
- **Navigation** mirrors the explorer/archview feel: `j`/`k` and the shared
  list-nav keys, `enter`/`l` expand-or-open, `h` collapse-or-parent (never
  above the root), `r` re-scan the selected directory. Scans **merge** by
  path, so a refresh keeps expansion state and loaded subtrees.
- **Hidden entries**: `.` toggles dot-entries, the explorer's semantics —
  children are cached on their nodes, so the toggle is a pure rebuild. A
  fresh pane seeds the filter from `explorer.show_hidden`, so both trees
  agree; the runtime toggle stays authoritative.
- **Errors**: a failed dial replaces the pane body (message + fix hint); a
  failed scan shows on the footer line in the Error colour until the next
  successful scan — the tree is never replaced by raw error text.
- **Close** (`ctrl+w` / `pane.close`) ends the SFTP session and kills the
  ssh subprocess (`releaseContent`).

## Opening a remote file: the cache

Activating a file emits `remote.OpenFileMsg`; the app downloads it in the
background into the cache at
`<UserCacheDir>/ike/sftp/<alias>/<remote path mirrored>` — one directory per
alias, every segment sanitized (`..` and separators neutralized), the base
name preserved so language lookup and every viewer's format sniff resolve
from the name the host uses. `remote.max_fetch_mb` (Settings → Remote
Browsing, default 64 MiB) caps the download twice: the listing size refuses
before any bytes move, the fetch cap catches a file that grew since.

The landed download forks:

- **A viewer claims it** (`ResolveHandler` on the local copy — archives, a
  plain `.gz`, images, databases): it opens through the normal handler
  dispatch with the *local path*, which is all those viewers ever wanted.
  Their panes are read-only by construction.
- **Text** lands in a **read-only editor buffer** (`ShowReadOnly`, the
  [gz-viewer seam](./gz-viewer.md#read-only-and-reload)) under the virtual
  path `sftp://<alias><path>`. The tail is the remote file's own name, so
  the tab title and highlighting resolve unchanged; the pane title reads
  `app.log (web01) [RO]` (`remoteEntryTitle`, next to the archive and
  merged-log decoders) and the status line shows the full `sftp://` path —
  the remote origin is always visible.
- **Binary** nothing claims is refused with a notice, like a binary archive
  member.

**Saves are blocked, never silently lost.** Write-back over SFTP is out of
scope: the buffer is permanently read-only, every mutation refuses with
`E45: buffer is read-only`, `:w` cannot fire, and the `[RO]` title says why.
Nothing ever writes to the cache copy in the file's stead — a remote
preview can never masquerade as a successful remote save.

## Persistence

A browser pane persists as `{kind: "remote", path: <alias>}` — the ES-console
convention — and restores by re-dialing the host in the background
(`AddRemoteKey` + the `initRemotePanes` sweep); an unreachable host restores
as the pane's own error notice. The read-only buffers themselves are session
state, like every `ShowReadOnly` preview: skipped when persisting tabs.

## Boundaries

- **Read-only, always.** No write-back, no remote file operations (create,
  rename, delete), no remote search.
- **Symlinked directories are not followed** — a symlink lists as a plain
  entry.
- **No auto-refresh.** Remote listings refresh on `r` (or a re-expand), not
  by polling — a poll per open directory over the wire is the wrong default
  for slow links.
- **The cache is not pruned** — it lives under the user cache directory,
  where the OS's usual cache hygiene applies.
