# Code intelligence

Diagnostics, completion, hover, jump-to-definition, find-usages and rename all
come from a **language server** — a separate program that understands your
language. IKE speaks LSP to it; it does not implement any of this itself.

That has one practical consequence: everything on this page depends on a
server being available for the language you are editing. When there is none,
the features go quiet rather than breaking.

## Which server for which language

| Language | Server | Installed with |
|---|---|---|
| Go | `gopls` | `go install golang.org/x/tools/gopls@latest` |
| Python | `pyright-langserver` | `npm install -g pyright` |
| PHP | `intelephense` | `npm install -g intelephense` |
| TypeScript / JavaScript / web | `vtsls` | `npm install -g @vtsls/language-server` |
| JSON | `vscode-json-language-server` | `npm install -g vscode-langservers-extracted` |
| YAML | `yaml-language-server` | `npm install -g yaml-language-server` |
| TOML | `taplo` | `npm install -g @taplo/cli` |
| Shell | `bash-language-server` | `npm install -g bash-language-server` |
| SQL | `sqls` | `go install github.com/sqls-server/sqls@latest` |
| Markdown | `marksman` | `brew install marksman` |
| Dockerfile | `docker-langserver` | `npm install -g dockerfile-language-server-nodejs` |
| Ansible | `ansible-language-server` | `npm install -g @ansible/ansible-language-server` |

Shell diagnostics additionally need `shellcheck` — `bash-language-server`
delegates linting to it.

## Installation happens on its own

You normally do not run those commands yourself.

On the **first launch** IKE offers a dialog listing every language whose
server it knows how to install, all pre-checked. Enter installs them; ++esc++
skips. Anything you uncheck is remembered as disabled and left alone
afterwards.

After that, **activation implies installation**: open a file in a language
whose server is missing and IKE runs the install recipe in the background,
tells you it is doing so, and re-opens the file when the server is ready.

Guard rails, so this never becomes a nuisance:

- One install per language at a time.
- A failed automatic install backs off **permanently** — it will not retry on
  every file open. The manual path is the retry.
- Failures show the tail of the install output and write a line to
  `debug.log`.
- Success is only claimed once the binary actually resolves. A recipe that
  exits cleanly but installs somewhere off your `PATH` is reported as a
  failure, naming the directories that were probed.

Set `lsp.auto_install = false` to turn all of this off. Then
**LSP: Install Missing Servers** from the palette, or `i` on the
**Language Servers** settings page, is how you install one deliberately.

## What you get

| Keys | What it does |
|---|---|
| ++f4++ | Go to definition (++cmd+b++ also) |
| ++alt+f7++ | Find usages |
| ++ctrl+q++ | Quick documentation for the symbol at the cursor |
| ++cmd+p++ | Parameter info while writing a call |
| ++alt+enter++ | Intention actions / quick fixes |
| ++shift+f6++ | Rename the symbol everywhere |
| ++cmd+alt+l++ | Reformat the file |
| ++ctrl+alt+h++ | Call hierarchy |
| ++f2++ / ++shift+f2++ | Next / previous diagnostic |
| ++ctrl+f1++ | Show the diagnostic under the cursor in full |
| ++cmd+o++ | Go to symbol across the project |
| ++cmd+8++ | The Problems window — every diagnostic in the project |

Completion appears as you type; ++enter++ accepts, ++esc++ dismisses.
**LSP: Peek Definition** shows a definition inline without leaving the file,
and **LSP: Find Usages (Panel)** puts the results in a tool window rather than
a popup.

Diagnostics are shown three ways at once: underlined in the text, as marks in
the scrollbar, and collected in the Problems window.

## On save

Two settings, both off by default, both applying to **manual** saves only —
auto-save deliberately never reformats under you:

| Setting | What it does |
|---|---|
| `editor.format_on_save` | Runs the server's formatter before writing |
| `editor.organize_imports_on_save` | Applies the organize-imports action before writing |

## Toolchains

The **Toolchain** settings page is where the interpreter or SDK per language
is decided. Run, debug and the language server all read that one answer.

Rows group by state, so the page shows the situation rather than a flat list:
`configured`, `detected · not configured`, and a folded `not installed · n`
caption you open with `z`. Selecting a language fills the detail column with
the candidates actually discovered for it — an active venv, a project
`.venv`, `uv python list`, pyenv shims, Homebrew and other versioned install
directories, PATH — each with its provenance and probed version, the one in
use marked `●`, and *enter a path manually…* as the last entry. While the scan
runs it says `scanning…`.

On a fresh machine, the column starts with the point of the page and
`a · accept all n recommendations`: one key writes every detected interpreter
in a single batch instead of walking you through a picker per language.

A choice is written to the **project** config and restarts the language
servers against the new interpreter; `r` goes back to detection. Python rows
additionally offer `p` (probe the version), `i` (installed packages, with
install/uninstall/upgrade) and `n` (the guided new-environment wizard).

## When something does not work

**No diagnostics, no completion, nothing.** The server for that language is
missing or failed to start. Open the **Language Servers** settings page — it
shows the state of each server (ready, disabled, missing) — or run
**LSP: Show Server Log** to see what the server said. **LSP: Restart Servers**
restarts them without restarting IKE.

**Some features work, others do nothing.** Servers advertise which
capabilities they support, and IKE only offers what the server actually
implements. A server that does not do call hierarchy simply has none.

**It works in one project but not another.** Servers run per (language,
project root). A project-level `[lsp.servers.<id>]` block, or a toolchain that
resolves differently in that directory, changes the picture — the
**Toolchain** settings page shows which interpreter or SDK was picked.

**Python picks the wrong interpreter.** Set it explicitly:

```toml
[lang.python]
interpreter = "/path/to/.venv/bin/python"
```

The Toolchain settings page writes this for you, and can create a virtual
environment from scratch.

## Related

- [Search](search.md) — find usages is semantic; find-in-path is textual, and
  sometimes that is what you want.
- [Settings reference](../reference/settings.md) — the full `[lsp]` section.
