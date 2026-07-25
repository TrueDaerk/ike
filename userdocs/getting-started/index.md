# Getting started

Four steps from nothing to a working editor. The second one is the one people
skip and regret.

1. **[Installation](installation.md)** — build the binary, put it on your
   `PATH`, and decide which optional tools you want.
2. **[Terminal setup](terminal-setup.md)** — make your terminal forward keys
   to IKE instead of eating them. IKE is unusable without this, and the
   failure mode looks like "the shortcut does nothing" rather than like a
   configuration problem.
3. **[Your first project](first-project.md)** — open a directory, find your
   way around the panes, edit and save a file.
4. **[Command line](command-line.md)** — open files at a line, in tabs, or
   from a pipe.

## The five-minute version

If you would rather try it before reading:

```sh
git clone https://github.com/TrueDaerk/ike.git
cd ike
make install          # ~/.local/bin/ike
cd ~/src/my-project
ike
```

On the first start IKE opens a short welcome tour — five pages with the keys
that open everything. It is worth the two minutes; you can reopen it any time
by running **Welcome Tour** from the palette.

Three keys carry the rest:

| Keys | What it does |
|---|---|
| ++shift++ twice, or ++cmd+shift+a++ | Search everywhere — commands, files, symbols |
| ++f1++ | The cheatsheet, with the live bindings for your build |
| ++q++ or ++ctrl+c++ | Quit (unsaved changes always prompt first) |

!!! note "Cmd or Ctrl?"
    Chords are written with **Cmd** throughout this documentation. Off macOS,
    IKE maps `Cmd` to `Ctrl` at build time, so ++cmd+s++ on macOS is
    ++ctrl+s++ on Linux and Windows. The
    [keybinding reference](../reference/keybindings.md) lists both columns.

!!! warning "If typing does nothing"
    You are in normal mode. The editor is modal, like vim: press ++i++ to
    start inserting text, ++esc++ to go back. The current mode is always shown
    at the left of the status bar.
