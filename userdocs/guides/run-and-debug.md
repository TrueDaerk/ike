# Running and debugging

## Running

++shift+f10++ runs the file you are looking at. There is no configuration step
first: IKE derives a run configuration from the file's language, saves it as
the project's default for that file, and tells you it did. ++ctrl+f5++ repeats
whatever ran last.

Configurations are named, persisted per project in `.ike/runconfigs.json`, and
carry a command line, a working directory and environment overrides. The
language registry is what knows how to turn "this Go file" into an actual
command.

### Where the output goes

A run is a **terminal command session**, not a captured log: real stdin, real
exit code, in a terminal pane. Programs that prompt for input work.

IKE reuses a terminal you have not typed into (or one whose command finished)
rather than opening a new one each time. When there is nothing reusable,
`run.placement` decides:

| Value | Where the run opens |
|---|---|
| `in_pane` (default) | A terminal tab in the focused editor pane |
| `new_terminal` | A new terminal pane, split off the editor (below, or to the right of a wide landscape pane) |

The pane stays open when the command exits — the output is the point.

The terminal gets the project's toolchain environment plus the configuration's
own overrides, so the interpreter that runs your code is the one IKE shows
everywhere else.

### Shell scripts

`.sh`, `.bash` and `.zsh` files run too, including scratch buffers. The
interpreter is resolved in this order:

1. an explicit `[lang.shell] interpreter`,
2. the file's shebang shell, when that binary is actually on `PATH`,
3. the shell the extension implies (`.bash` → `bash`, `.zsh` → `zsh`,
   `.sh` → `sh`).

The command is `<interpreter> <file>` from the project root. IKE never
executes the file directly through its shebang, so your scripts do not need to
be executable and nothing gets `chmod`ed behind your back. A shebang naming a
shell you have not installed falls back to step 3 instead of failing.

### Tests

**Run Test at Cursor** and **Run Tests in File** run tests specifically. Test
detection is regex-based per language, deliberately: it works with no language
server running and in builds without CGo.

The command is executed directly — no shell parses it — so a test name with
spaces or quotes in it needs no escaping.

## Debugging

++shift+f9++ starts a debug session for the current file. One session at a
time.

| Keys | What it does |
|---|---|
| ++ctrl+f8++ | Toggle a breakpoint on the cursor line |
| ++shift+f9++ | Start debugging |
| ++f9++ | Continue |
| ++f8++ | Step over |
| ++f7++ | Step into |
| ++shift+f8++ | Step out |
| ++ctrl+f2++ | Stop the session |

Breakpoints can also be set by **clicking in the gutter**. They are stored per
project in `.ike/breakpoints.json` and survive restarts.

### The debug window

It opens on the first stop, without stealing focus from the paused line — you
look at your code, not at a panel. Three resizable columns: frames, variables,
and output.

When the session ends the panel stays open in a finished state: frames and
variables clear to `finished (exit code N)`, but the output stays readable.
Close it like any pane; the next launch reuses it.

### Adapters install themselves

Debugging Python needs `debugpy` in the interpreter you are debugging with.
IKE checks before it spawns anything and, if it is missing, installs it —
trying `pip`, then `uv pip`, then both again with `--break-system-packages`
for externally managed interpreters like a Homebrew Python. You get a
notification, not a failure.

### PHP and Xdebug

PHP needs no external adapter and no Node: IKE speaks DBGp, Xdebug's own
protocol, directly.

++shift+f9++ on a PHP file launches it with Xdebug enabled and connects. For
debugging a **web request** rather than a script, run **Listen for PHP Debug
Connections** — IKE waits for Xdebug to call in, so you trigger the request
from your browser and the session starts when it hits your breakpoint.

One request is debugged at a time. A page load usually opens several
connections (subrequests, assets), and any that arrive while a request is
paused are let through undebugged — IKE says so instead of ignoring them: the
debug console gets a line per dropped connection with the reason, and a warning
notification for the first of each kind. So if a request never stops at a
breakpoint, the reason is on screen: `busy` (finish or stop the paused session
first), `filter` (the hostname filter in **Settings › Debug** rejected it) or
`handshake` (something dialed the port without speaking DBGp).

## When something does not run

**"No configuration"** — the language has no run template. Running is
per-language, and a language that IKE only highlights has nothing to run.

**Wrong interpreter** — the Toolchain settings page shows which one was
resolved, and lets you pin it. Run, debug and the language server all read the
same resolution, so fixing it there fixes all three.

**The run reuses a terminal you wanted to keep** — type something into that
terminal; a terminal you have typed into is never taken over.

## Related

- [The integrated terminal](terminal.md) — where runs land
- [Code intelligence](code-intelligence.md) — the same toolchain resolution
- [Keybindings reference](../reference/keybindings.md) — the full F-key set
