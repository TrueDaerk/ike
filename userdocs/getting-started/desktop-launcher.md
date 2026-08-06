# Desktop launcher

IKE can install as a first-class desktop application on macOS and Linux:
an **Ike** icon in the Dock / app grid that opens a dedicated
[Ghostty](https://ghostty.org/) window running `ike` as the sole process.
When IKE exits, the window closes.

Ghostty stays a user-installed prerequisite — install it from
[ghostty.org](https://ghostty.org/) first.

## Install

From the repository root:

```sh
make install          # installs the ike binary (~/.local/bin)
make install-desktop  # installs the launcher for your platform
```

`make install-desktop` installs three pieces:

1. **`~/.config/ghostty/ike.conf`** — a dedicated Ghostty configuration.
   The launcher loads it *exclusively*
   (`--config-default-files=false --config-file=…`), so your normal Ghostty
   config never interferes with the IKE window and vice versa. It ships
   with `keybind = clear` plus minimal re-adds, so chords reach IKE without
   the [manual terminal setup](terminal-setup.md). The installer points its
   `command =` line at the resolved `ike` binary, because a Dock launch
   does not inherit your shell's `PATH`. Edit it freely; reinstalling asks
   before overwriting.
2. **`ike-gui`** (in `~/.local/bin`) — the launcher script. Run it from a
   shell to open the IKE window without the desktop icon. Before launching
   the terminal it re-execs through your interactive login shell
   (`$SHELL -i -l`), so a Dock/desktop launch gets the same environment
   (`PATH` from `.zprofile` *and* `.zshrc` — LSP servers, formatters,
   toolchains) as a terminal launch.
3. **The platform shell:**
    - macOS: `/Applications/Ike.app` — appears in Launchpad, Spotlight and
      the Dock with the Ike icon.
    - Linux: `~/.local/share/applications/ike.desktop` plus hicolor icons.
      The config sets `class = ike`, matching the entry's `StartupWMClass`,
      so the dock shows the Ike icon at runtime too.

## What the terminal keeps

`ike.conf` wipes all Ghostty keybindings, then re-adds only what the
terminal itself must own (`super+` on macOS, `ctrl+` mirrors on Linux):

| Binding | Action |
|---|---|
| `super+q` / `ctrl+shift+q` | quit the window |
| `super+plus` / `minus` / `zero` (and `ctrl+` mirrors) | font zoom in / out / reset |
| `super+,` / `super+shift+,` (and `ctrl+` mirrors) | open / reload the Ghostty config |
| `alt+backspace` | macOS Option+Backspace escape sequence (delete word back) |

Option+Arrows and Option+Forward-Delete need no re-binding: Ghostty
delivers them with a real alt modifier, so IKE's own word navigation and
word deletion match directly.

Copy and paste are deliberately **not** bound — IKE handles the clipboard
itself.

## Known limitations

- **macOS Dock icon at runtime:** the running window shows the Ghostty
  icon; the Ike icon applies to the launcher tile (Launchpad, Spotlight,
  Dock). Fixing that would require a re-signed Ghostty clone — out of
  scope. Linux does better: the `StartupWMClass` match keeps the Ike icon
  at runtime.
- **Linux without Ghostty:** `ike-gui` falls back to `$TERMINAL`, kitty,
  WezTerm or foot running `ike` directly — without the dedicated config,
  so the terminal's own chords may shadow IKE's
  (see [terminal setup](terminal-setup.md)).
- **Windows** is not covered; use the [manual setup](terminal-setup.md).
