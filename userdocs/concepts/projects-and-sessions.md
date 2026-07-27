# Projects and sessions

## What a project is

A project is a directory. There is no project file, no import step, no
metadata IKE needs you to create: the directory you start in becomes the root,
and everything scopes to it — the file tree, project-wide search, per-project
settings, and the session state that gets restored next time.

What IKE writes into your project is a `.ike/` directory for state and
optional per-project settings, plus a `.ike-trash/` directory holding
explorer deletions so they can be undone. Both belong in `.gitignore` (this
repository ignores them).

## Starting in the right place

```sh
cd ~/src/my-project && ike
```

With `project.restore_last` turned on (it is off by default), running `ike`
from a directory that is *not* a project — no `.git`, no `.ike` — reopens your most recent project
instead. Inside a project directory that never happens: an explicit checkout
always wins over the history. Passing a file or a pipe on the command line
also counts as explicit. Your home directory never counts as a project, even
though `~/.ike` (IKE's config directory) and a dotfiles `~/.git` live there —
`ike` in `~` restores your most recent project.

**Switch project** (++cmd+shift+p++) moves to another workspace without
restarting, offering your recent-projects history. Unsaved changes are guarded
— you get asked before anything is dropped.

## The three settings layers

```
built-in defaults  <  ~/.ike/settings.toml  <  <project>/.ike/settings.toml
```

Later layers win, key by key. Your personal preferences live in the user
layer; anything genuinely specific to one codebase — a tab width the project
insists on, a language server override, a tool pane — goes in the project
layer and travels with the repository if you commit it.

Changes apply live at every layer. Nothing needs a restart.

!!! note "Lists replace, they do not merge"
    A TOML array in a later layer replaces the earlier one wholesale rather
    than appending to it. A project-level `[[tools.custom]]` block hides your
    user-level tools rather than adding to them.

`$IKE_CONFIG_DIR` relocates the user layer if you keep dotfiles somewhere
unusual.

## What comes back when you reopen

Session state is per project and saved on quit:

- **The layout** — panes, splits and their ratios.
- **The open file** — its path, the cursor line and column, and the scroll
  position. The scroll position is stored separately on purpose: restoring only
  the cursor would reframe the file, and every on-screen row would land
  somewhere else than you left it.
- **The explorer** — which directories are expanded, whether hidden files are
  shown, and where the cursor was.

Undo history persists too (`files.persistent_undo`), so an undo after a
restart still works — as long as the file has not changed on disk in the
meantime. If it has, that file reopens with an empty history rather than an
undo stack that no longer matches its content.

## Crash recovery

Session restore covers a clean exit. For an unclean one there is a second,
independent mechanism, modelled on vim's swapfiles.

While a buffer is dirty, IKE writes a full-text snapshot of it to the project
state directory — debounced, so it happens a couple of seconds after you stop
typing rather than on every keystroke. On a clean save or close the snapshot
goes away. If IKE dies without one, the next start finds the leftovers and
offers them back.

Leftover snapshots are always offered before anything is cleaned up; the
age-based pruning runs only after you have answered the prompt.

| Setting | Default | Meaning |
|---|---|---|
| `backup.enable` | `true` | Turning it off also **purges every existing snapshot**, immediately |
| `backup.debounce_ms` | `2000` | Quiet time after your last edit before a snapshot is written |
| `backup.max_age_days` | `7` | Snapshots older than this are pruned at startup |

!!! warning "Snapshots contain your file contents"
    They live in the project state directory, outside the project tree, in
    plain text. If that is not acceptable for a particular codebase, set
    `backup.enable = false` in that project's `.ike/settings.toml` — it purges
    what is already on disk as well.

## State that is not per project

A few things follow you instead of the workspace, and live under `~/.ike`:

- Your settings, unless a project overrides them.
- The recent-projects history behind **Switch project**.
- Named window layouts.
- Scratch files — deliberately outside any project tree, so a throwaway buffer
  never ends up in a commit.

## Files IKE writes

| Path | What it is |
|---|---|
| `~/.ike/settings.toml` | Your settings (or `$IKE_CONFIG_DIR/settings.toml`) |
| `<project>/.ike/` | Per-project settings, layout, session and crash snapshots |
| `<project>/.ike-trash/` | Explorer deletions, kept so they can be undone |

Both project directories belong in `.gitignore` unless you deliberately want
to share the project settings file.
