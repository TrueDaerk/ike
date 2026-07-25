# Installation

IKE is a single Go binary with no runtime dependencies. There are no
prebuilt releases yet — you build it from source.

## Requirements

- [Go 1.26+](https://go.dev/dl/)
- A terminal with truecolor, mouse reporting, and Kitty keyboard protocol
  support. This one is not optional — see [Terminal setup](terminal-setup.md).

## Build and install

```sh
git clone https://github.com/TrueDaerk/ike.git
cd ike
make install                        # installs to ~/.local/bin/ike
make install BINDIR=/usr/local/bin  # or pick another directory
```

`make` on its own produces `./ike` without installing it, and plain
`go build -o ike ./cmd/ike` works too if you would rather skip the Makefile.

Make sure the install directory is on your `PATH`:

```sh
echo $PATH | tr ':' '\n' | grep -q "$HOME/.local/bin" || \
  echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
```

## Per-platform notes

=== "Linux"

    Works in any terminal with Kitty keyboard protocol support — Ghostty,
    kitty, WezTerm, foot, Alacritty. Put the binary anywhere on your `PATH`:

    ```sh
    install ike ~/.local/bin/
    ```

=== "macOS"

    Works in Ghostty, kitty, WezTerm, iTerm2 3.5+, Alacritty. Note the
    Option-key caveat in [Terminal setup](terminal-setup.md) — on
    international layouts Option is a composition key, which changes how
    Alt-chords arrive.

    ```sh
    go build -o /usr/local/bin/ike ./cmd/ike
    ```

=== "Windows"

    ```sh
    go build -o ike.exe ./cmd/ike
    ```

    Run it in a terminal that speaks the Kitty keyboard protocol — WezTerm is
    the reliable choice. WSL2 also works well; follow the Linux notes inside
    it.

## Optional tools

IKE degrades gracefully without these — no errors, the feature simply goes
quiet — but each one unlocks something.

| Tool | What you get | Without it |
|---|---|---|
| [`ripgrep`](https://github.com/BurntSushi/ripgrep) (`rg`) | Fast project-wide search | A pure-Go fallback: correct, slower on large trees |
| `gopls` | Go code intelligence | No diagnostics, completion, hover or go-to-definition for Go |
| `pyright-langserver` | Python code intelligence | Same, for Python |
| `intelephense` | PHP code intelligence | Same, for PHP |
| `shellcheck` | Shell script diagnostics | No shell linting |
| [`lazygit`](https://github.com/jesseduffield/lazygit) | A preconfigured Git tool pane | Git status, gutter markers and diffs still work; the full Git workflow does not |

IKE detects language servers on your `PATH` at startup and disables LSP per
language when one is missing. Install the server, restart IKE, and the feature
appears — the palette command **LSP: Install Missing Servers** can do the
install for you.

## Verifying

```sh
cd ~/src/my-project
ike
```

You should get a file tree on the left and an editor area on the right. If
keys do nothing, go to [Terminal setup](terminal-setup.md) — that is the
usual cause, not a broken build.
