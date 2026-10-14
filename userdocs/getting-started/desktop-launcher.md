# Desktop launcher

IKE can install as a first-class desktop application: an **Ike** icon in
the Dock / app grid / Start menu that opens a dedicated terminal window
running `ike` as the sole process. When IKE exits, the window closes.

- **macOS and Linux** wrap [Ghostty](https://ghostty.org/), a user-installed
  prerequisite — install it from [ghostty.org](https://ghostty.org/) first.
- **Windows** wraps [WezTerm](https://wezterm.org/); the package managers
  install it for you — see [Windows](#windows).

## Install (macOS, Linux)

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
      the Dock with the Ike icon. Its `Info.plist` declares
      `LSArchitecturePriority` (arm64 first): without it, macOS runs a
      script-based bundle under Rosetta, and an architecture-conditional
      shell profile (like Homebrew's arm64 branch) would build the wrong
      `PATH` for everything IKE spawns.
    - Linux: `~/.local/share/applications/ike.desktop` plus hicolor icons.
      The config sets `class = ike`, matching the entry's `StartupWMClass`,
      so the dock shows the Ike icon at runtime too.

## What the terminal keeps

`ike.conf` wipes all Ghostty keybindings, then re-adds only what the
terminal itself must own (`super+` on macOS, `ctrl+` mirrors on Linux):

| Binding | Action |
|---|---|
| `super+q` / `ctrl+shift+q` | quit the window |
| `ctrl+plus` / `ctrl+-` / `ctrl+zero` | font zoom in / out / reset |
| `ctrl+,` / `ctrl+shift+,` | open / reload the Ghostty config |
| `cmd+c` (performable) / `cmd+v` | copy terminal selection / paste from clipboard |
| `super+z` | sent as the `ctrl+z` byte so IKE sees undo |
| `alt+backspace` | macOS Option+Backspace escape sequence (delete word back) |

The `super+` variants of font zoom and config open/reload are deliberately
absent: Cmd chords stay with the IDE (IKE owns `super+,` internally, and
`cmd+-`/`cmd++` remain free for IDE bindings).

Option+Arrows and Option+Forward-Delete need no re-binding: Ghostty
delivers them with a real alt modifier, so IKE's own word navigation and
word deletion match directly.

`cmd+c` uses Ghostty's `performable:` prefix: with no terminal selection
the chord falls through to IKE, so IDE-side copy keeps working.

The runtime font zoom is per-instance and not persisted — each launch
starts from `ike.conf`, which ships with `font-size = 12`. Edit that value
to change the default size.

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
- **Windows:** see [Windows](#windows) below.

## Windows

On Windows the launcher wraps [WezTerm](https://wezterm.org/) — Ghostty has
no Windows build, and Windows Terminal does not speak the Kitty keyboard
protocol. One package-manager command installs IKE, WezTerm and the launcher.

### Install

With [Scoop](https://scoop.sh/):

```powershell
scoop bucket add extras        # WezTerm lives in the extras bucket
scoop install https://raw.githubusercontent.com/TrueDaerk/ike/main/deploy/scoop/ike.json
```

This installs WezTerm as a dependency, puts `ike`, `ike-gui` and `keyprobe`
on your `PATH`, and creates the Start-menu entry **Ike** (under *Scoop
Apps*). `scoop update ike` follows new releases: the release workflow keeps
the manifest at that URL current.

With winget, from a checkout of the repository (the package is not in the
public winget catalogue yet):

```powershell
winget install --manifest deploy\winget\<version>
& (Get-ChildItem "$env:LOCALAPPDATA\Microsoft\WinGet\Packages\TrueDaerk.Ike_*\install-desktop.ps1").FullName
```

winget installs WezTerm (`wez.wezterm`) as a dependency and puts `ike` on
your `PATH`; it cannot create Start-menu entries for a zip package, so the
second line runs the bundled `install-desktop.ps1` for that.

Without a package manager, download `ike_<version>_windows_<arch>.zip` from
the [releases page](https://github.com/TrueDaerk/ike/releases), unpack it
anywhere, install WezTerm, and run
`powershell -ExecutionPolicy Bypass -File install-desktop.ps1` in the
unpacked folder (`-Uninstall` removes the entry again).

### What gets installed

The Windows release zip carries, side by side:

| File | Role |
|---|---|
| `ike.exe` | IKE itself |
| `keyprobe.exe` | the [key probe](terminal-setup.md#verifying-with-keyprobe) |
| `wezterm-ike.lua` | the dedicated WezTerm config, loaded *exclusively* (`--config-file`), so your own WezTerm config never interferes |
| `ike-gui.cmd` | the launcher for a shell: opens IKE in its own WezTerm window on the current directory |
| `install-desktop.ps1` | creates / removes the Start-menu entry for zip and winget installs |
| `ike.ico` | the Start-menu icon |

`wezterm-ike.lua` is the counterpart of Ghostty's `ike.conf`: Kitty keyboard
protocol on, all WezTerm key bindings off (`disable_default_key_bindings`)
plus the minimal re-adds below, truecolor, no tab bar, the bundled
JetBrains Mono with Windows font fallbacks, the `ike.exe` from its own
folder as the program, and the window closes when IKE exits. A package update
replaces it — keep personal changes in a copy.

| Binding | Action |
|---|---|
| `ctrl+shift+q` | quit the window |
| `ctrl+plus` / `ctrl+-` / `ctrl+0` | font zoom in / out / reset |
| `ctrl+,` / `ctrl+shift+,` | open the config in Notepad / reload it |

### Launching

- **Start menu → Ike** opens IKE on your home directory, which IKE never
  treats as a project: it reopens your last project instead.
- **`ike-gui`** in a terminal opens IKE on the current directory; extra
  arguments go to `ike` (`ike-gui main.go`). It finds `wezterm-gui.exe` via
  `%IKE_WEZTERM%`, the Scoop install, the WezTerm installer / winget location
  (`%ProgramFiles%\WezTerm`, `%LOCALAPPDATA%\Programs\WezTerm`), then
  `PATH`. Without WezTerm it prints a one-line hint and exits 1.
- **`ike`** alone runs IKE in the terminal you are in — fine in a WezTerm
  you configured yourself ([terminal setup](terminal-setup.md)).

The launcher always starts a separate WezTerm process
(`start --always-new-process`): a running WezTerm would otherwise open the
window with your own config instead of IKE's.

### Known limitations on Windows

- The **integrated terminal** and the tools that run on a pseudo-terminal
  (Run configurations, tasks) do not start on Windows yet — the PTY library
  IKE uses has no Windows backend. Editing, LSP, search and everything else
  work.
- `ike://` deep links are not registered by the Windows install.

### Publishing to winget (maintainers)

The release workflow regenerates `deploy/winget/<version>/` on every tag
but does not submit it. To publish a release to the public catalogue:

1. Fork [`microsoft/winget-pkgs`](https://github.com/microsoft/winget-pkgs).
2. Copy the three files of `deploy/winget/<version>/` to
   `manifests/t/TrueDaerk/Ike/<version>/` in the fork.
3. Run `winget validate --manifest manifests\t\TrueDaerk\Ike\<version>`
   and `winget install --manifest …` on a Windows machine.
4. Open a pull request against `microsoft/winget-pkgs`; once merged,
   `winget install TrueDaerk.Ike` works for everyone.
