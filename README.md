# IKE

**IKE** is a terminal IDE — a JetBrains-inspired TUI built with
[bubbletea](https://github.com/charmbracelet/bubbletea), with vim-like controls
in the editor. It brings the pieces you expect from a desktop IDE into the
terminal: a file explorer, multiple editor panes with tabs, a command palette,
LSP-powered language intelligence, Tree-sitter syntax highlighting, an
integrated terminal, themes, and a WASM plugin system.

![IKE with the tokyo-night theme](docs/screenshots/tokyo-night.png)

📖 **[Documentation](https://truedaerk.github.io/ike/)** — installation,
terminal setup, feature guides, and the keybinding/settings reference.

**Why it exists:** built for working across a lot of microservice repositories
at once. A full IDE has indexing to do before a project is usable, so closing
one you will need again shortly is a bad trade — you keep them all open, they
all stay resident, and a surprising share of the day goes into finding the
right window. Neovim was the obvious lighter alternative, but assembling an
IDE out of plugins was a bigger project than the one worth solving. So: a
JetBrains-shaped IDE in a terminal, with **project switching** in place of
window hunting.

> [!NOTE]
> IKE is a personal project: built by one person, to that person's taste, with
> heavy AI assistance. The defaults follow a specific JetBrains muscle memory,
> on a German keyboard, in a specific terminal, and there is no support
> promise. It is public on purpose, though — use it if it suits you, and
> [pull requests](CONTRIBUTING.md) that improve it are genuinely welcome.

## Installation & usage

IKE is a single Go binary. You need [Go 1.26+](https://go.dev/dl/) and a
terminal with truecolor, mouse, and Kitty keyboard protocol support
(see [Platform notes](#platform-notes)).

Build from source (all platforms):


cd ike
make install                        # installs to ~/.local/bin/ike
make install BINDIR=/usr/local/bin  # or pick another directory
```

(Or build without installing: `make` produces `./ike`; plain
`go build -o ike ./cmd/ike` works too.)

Check what you built with `ike --version` — a Makefile build stamps the commit
and whether the tree was clean:

```
ike 0.1.0 (a1b2c3d, dirty) go1.26.5 darwin/arm64
```

Then run `ike` from the directory you want to open as a project — the current
working directory becomes the project root:

```sh
cd ~/src/my-project
ike
```

Files can be opened directly from the command line — as tabs, optionally at a
line (and column); the vim-style `+N` form works too, and a path that does not
exist yet opens as a new unsaved buffer:

```sh
ike internal/app/app.go:725       # open at line 725
ike main.go:10:4 README.md        # two tabs, first one focused at 10:4
ike +42 main.go                   # vim-style line prefix
git log | ike -                   # pipe stdin into a scratch buffer
```

### Desktop launcher (macOS & Linux)

IKE can install as a first-class desktop application wrapping
[Ghostty](https://ghostty.org/) (user-installed prerequisite):

```sh
make install          # the ike binary first
make install-desktop  # Ike.app (macOS) or ike.desktop + icons (Linux)
```

`make install-desktop` also registers the **`ike://` URL scheme** (a small
link-handler applet on macOS, `x-scheme-handler/ike` on Linux) — re-run it
after upgrading, or links clicked in a browser go nowhere. Other devices can
drive the same actions over TCP once `[network].enabled` is on: see
[Network Links](wiki/architecture/network-links.md) for the protocol and the
pairing flow.

Clicking the **Ike** icon opens a dedicated Ghostty window with IKE-specific
settings (`~/.config/ghostty/ike.conf`, loaded exclusively — your normal
Ghostty config is untouched and vice versa), running `ike` as the sole
process; quitting IKE closes the window. The config ships with
`keybind = clear` plus the minimal re-adds (font zoom, quit, the macOS
Option-key fixes below), so chords reach IKE without any manual terminal
setup. Known macOS limitation: the *running* window shows the Ghostty Dock
icon — the Ike icon applies to the launcher tile. If you don't want the
desktop integration, the manual setup below works as before.

### Platform notes

> [!IMPORTANT]
> IKE only works properly in a terminal that supports the
> [Kitty keyboard protocol](https://sw.kovidgoyal.net/kitty/keyboard-protocol/)
> **and** has its own keybindings (mostly) disabled — otherwise the terminal
> swallows chords before they ever reach IKE. For example, with
> [Ghostty](https://ghostty.org/) use a config like:
>
> ```
> keybind = clear
> keybind = super+,=open_config
> keybind = super+shift+,=reload_config
> # macOS only: Option is a composition key (needed for { [ @ ~ on
> # international layouts), so option+backspace arrives without the alt
> # modifier. This sends ESC DEL directly (backward-kill-word in IKE and
> # in shells). Must come *after* `keybind = clear`.
> keybind = alt+backspace=text:\x1b\x7f
> ```
>
> (on Windows/Linux use `ctrl` instead of `super`.) Ghostty merges every
> config file it finds (e.g. `~/.config/ghostty/config` **and**
> `~/Library/Application Support/com.mitchellh.ghostty/config.ghostty`);
> a `keybind = clear` in a later file wipes keybinds from earlier ones —
> check the effective result with `ghostty +show-config`.

| Platform | Notes |
|---|---|
| **Linux** | Works in terminals with Kitty keyboard protocol support (Ghostty, kitty, wezterm, foot, Alacritty, …). Put the binary on your `PATH`, e.g. `install ike ~/.local/bin/`. |
| **macOS** | Works in Ghostty, kitty, wezterm, iTerm2 (3.5+), Alacritty, …. Install with `go build -o /usr/local/bin/ike ./cmd/ike` (or any `PATH` directory). |
| **Windows** | Build with `go build -o ike.exe ./cmd/ike` and run in a terminal with Kitty keyboard protocol support (e.g. wezterm). WSL2 also works well — follow the Linux notes there. |

### Optional tools

IKE degrades gracefully without these, but they unlock extra features:

- [`ripgrep`](https://github.com/BurntSushi/ripgrep) (`rg`) — fast backend for
  project-wide search (a pure-Go fallback is built in).
- **Language servers** for code intelligence, per language: `gopls` (Go),
  `pyright-langserver` (Python), `intelephense` (PHP). IKE detects them on
  your `PATH` and disables LSP per language when missing.

### Configuration

Settings live in TOML, merged as *defaults < user < project*:

- User: `~/.ike/settings.toml` (or `$IKE_CONFIG_DIR/settings.toml`)
- Project: `<project>/.ike/settings.toml`

```toml
[theme]
name = "tokyo-night"
```

Most settings can also be edited interactively in the settings panel
(menu bar → Settings), and config changes are picked up live — no restart.

## Features

- **Pane layout** — split the workspace into any arrangement of panes; resize
  by dragging dividers and move panes by dragging their title bars (full mouse
  support). The layout persists per project.
- **Editor tabs** — each editor pane holds an ordered set of tabs; buffers are
  shared across panes.
- **Vim-like modal editor** — normal/insert/visual modes, motions, operators,
  text objects, registers, undo/redo, and in-buffer search.
- **Command palette** — `cmd+shift+a` (or double-shift) opens a centered
  overlay: type `:` to run any registered command (context-ranked), or `@` to
  fuzzy-find files. A dedicated toggle chord can be set via
  `palette.toggle_key`.
- **JetBrains-like keybindings** — context-scoped shortcuts with multi-step
  chords, conflict detection, platform normalisation, and a built-in
  cheatsheet (help overlay).
- **File explorer** — directory tree pane with per-filetype colors.
- **Syntax highlighting** — Tree-sitter grammars, parsed off the UI loop and
  colored by the active theme.
- **LSP integration** — diagnostics, completion, hover, and go-to-definition
  over a language server's stdio, managed per (language, project root).
- **Project search** — streaming find-in-path with an `rg --json` backend and
  a pure-Go fallback.
- **Integrated terminal** — a real PTY-spawned shell in a pane, with raw key
  routing (`ctrl+arrows` escape back to the IDE), mouse passthrough, and text
  selection.
- **Themes** — one `[theme].name` recolors the whole IDE; twenty-eight built-ins
  plus plugin-registered themes, switchable live from the palette
  (see [screenshots](#themes) below).
- **Session restore & crash recovery** — open files, cursors, and explorer
  state are restored per project; dirty buffers are snapshotted vim-swapfile
  style and offered back after a crash.
- **Project switching** — recent-projects history for jumping between
  workspaces.
- **Scratch files** — language-aware throwaway buffers ("New Scratch File:
  Python" in the palette), stored outside the project tree and surviving
  restarts.
- **Navigation history** — JetBrains-style Back/Forward across cursor jumps.
- **Settings UI & menu bar** — schema-driven settings forms with config
  write-back, and a menu bar fronting the command registry.
- **Plugins** — compile-in Go plugins and sandboxed **WASM plugins** (with a
  Go guest SDK) can add commands, themes, and languages. Adding a language is
  one new package: extensions + grammar + LSP server + toolchain detection.

The `wiki/` directory contains the full architecture documentation, one
concept document per subsystem.

## Themes

Select a theme in `settings.toml` (`[theme] name = "..."`) or at runtime via
the command palette (`:` → "Theme: …"). All twenty-eight built-ins are shown
below.

| | |
|---|---|
| **default** ![default](docs/screenshots/default.png) | **tokyo-night** ![tokyo-night](docs/screenshots/tokyo-night.png) |
| **nord** ![nord](docs/screenshots/nord.png) | **gruvbox** ![gruvbox](docs/screenshots/gruvbox.png) |
| **gruvbox-light** ![gruvbox-light](docs/screenshots/gruvbox-light.png) | **rose-pine** ![rose-pine](docs/screenshots/rose-pine.png) |
| **rose-pine-dawn** ![rose-pine-dawn](docs/screenshots/rose-pine-dawn.png) | **catppuccin-mocha** ![catppuccin-mocha](docs/screenshots/catppuccin-mocha.png) |
| **catppuccin-latte** ![catppuccin-latte](docs/screenshots/catppuccin-latte.png) | **kanagawa** ![kanagawa](docs/screenshots/kanagawa.png) |
| **one-dark** ![one-dark](docs/screenshots/one-dark.png) | **solarized-dark** ![solarized-dark](docs/screenshots/solarized-dark.png) |
| **solarized-light** ![solarized-light](docs/screenshots/solarized-light.png) | **dracula** ![dracula](docs/screenshots/dracula.png) |
| **darcula** ![darcula](docs/screenshots/darcula.png) | **intellij-light** ![intellij-light](docs/screenshots/intellij-light.png) |
| **everforest-dark** ![everforest-dark](docs/screenshots/everforest-dark.png) | **everforest-light** ![everforest-light](docs/screenshots/everforest-light.png) |
| **ayu-dark** ![ayu-dark](docs/screenshots/ayu-dark.png) | **ayu-mirage** ![ayu-mirage](docs/screenshots/ayu-mirage.png) |
| **ayu-light** ![ayu-light](docs/screenshots/ayu-light.png) | **github-dark** ![github-dark](docs/screenshots/github-dark.png) |
| **github-light** ![github-light](docs/screenshots/github-light.png) | **oxocarbon** ![oxocarbon](docs/screenshots/oxocarbon.png) |
| **monokai-pro** ![monokai-pro](docs/screenshots/monokai-pro.png) | **zenburn** ![zenburn](docs/screenshots/zenburn.png) |
| **high-contrast-dark** ![high-contrast-dark](docs/screenshots/high-contrast-dark.png) | **high-contrast-light** ![high-contrast-light](docs/screenshots/high-contrast-light.png) |

## Development

- Planning lives in [GitHub issues](https://github.com/TrueDaerk/ike/issues)
  (epics + sub-issues, one milestone per epic) — see `CLAUDE.md` for the
  workflow.
- Run the tests with `go test ./...`.
- Architecture docs live in [`wiki/`](wiki/index.md).
- Contributor guide: [`CONTRIBUTING.md`](CONTRIBUTING.md). Security reports:
  [`SECURITY.md`](SECURITY.md).
- The user documentation site is built from [`userdocs/`](userdocs/index.md)
  with MkDocs Material and deployed to GitHub Pages on every push to `main`.
  Preview it locally with
  `pip install -r userdocs/requirements.txt && mkdocs serve`.

## License

[MIT with the Commons Clause](LICENSE). Use, modify and redistribute IKE
freely — including building commercial software with it. The one restriction
is selling IKE itself, modified or not.
