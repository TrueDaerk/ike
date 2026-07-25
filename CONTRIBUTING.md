# Contributing to IKE

Thanks for looking. Pull requests are genuinely welcome — this page describes
how the repository is organised so a patch has an easy time getting merged.

Please read [What this project is](#what-this-project-is) first. It is not
boilerplate; it explains the one thing that decides whether a change lands.

## What this project is

IKE is a personal project. It is built by one person, to that person's taste,
with heavy AI assistance — "vibe-coded" is a fair description, and the
architecture notes in [`wiki/`](wiki/index.md) exist largely so the agents and
the human stay on the same page.

That has consequences worth stating openly:

- **Design decisions are personal preferences, not committee output.** The
  keymap follows a specific JetBrains muscle memory, on a German keyboard, in
  a specific terminal. Defaults are chosen for that setup first.
- **There is no support promise.** No SLA, no roadmap commitment, no
  guarantee that a feature survives a refactor. Issues are worked on when they
  are interesting or in the way.
- **You are very welcome to use it anyway.** If it is useful to you, take it.
  See [`LICENSE`](LICENSE) for the terms — short version: do what you like,
  including building commercial software with it, just do not sell IKE itself.
- **Improvements are welcome.** A PR that fixes a bug, adds a test, sharpens
  the docs, or adds a feature cleanly behind a setting will get read and, if
  it fits, merged.

The corollary: a change that reshapes existing behaviour to a different taste
is a harder sell than one that adds an option. If you are unsure which one
yours is, open an issue first and ask — that costs you nothing and saves you
from writing a patch that gets declined on grounds you could not have guessed.

## Before you write code

**Open an issue first**, or comment on the one you are picking up. Every piece
of work in this repository starts as an issue, and planning lives entirely in
[GitHub issues](https://github.com/TrueDaerk/ike/issues) — there is no roadmap
directory.

Check for an existing issue before opening a new one:

```sh
gh issue list --state all --search "<keywords>"
```

The structure is:

- **Epic** (label `epic` + `roadmap:NNNN`) — holds a full spec verbatim and a
  task list of its sub-issues. One epic per work stream, one GitHub milestone
  per epic.
- **Sub-issue** — one independently completable, reviewable task, linked from
  its epic's task list.
- **`idea`** — a proposal that is not planned yet.

Issue titles are prefixed with their stream number (`0090: <what>`); epics use
`Epic NNNN: <name> (spec)`. Bodies list concrete acceptance criteria as a
checklist and name dependencies by issue number.

## Working on a change

1. Branch per issue: `issue/<number>-<slug>` — e.g. `issue/12-project-picker`.
2. Write the code **and its tests**. New code ships with tests; `go test ./...`
   has to pass.
3. Update the docs you invalidated, in the same change:
   - [`wiki/`](wiki/index.md) is the architecture bundle (OKF v0.1) — update
     the matching concept document and refresh its `timestamp`, and add a
     `log.md` entry.
   - [`userdocs/`](userdocs/index.md) is the user-facing documentation site.
     If you changed something a user can see, say so there.
4. Open a PR whose body contains `Closes #<number>` so the issue closes on
   merge.

Everything — code, comments, doc strings, commit messages, issues, PRs — is
written in **English**.

### Commit messages

Conventional-commit style, with the subsystem as the scope:

```
feat(terminal): clickable file:line references + scrollback search
fix(editor): keep the cursor column across a wrapped line
docs(site): MkDocs Material scaffold, home page, and Pages workflow
```

Keep the subject under ~72 characters and explain the *why* in the body when
it is not obvious.

## Building and testing

```sh
make                 # build ./ike
make install         # install to ~/.local/bin/ike
go test ./...        # the full test suite
```

IKE needs a terminal with Kitty keyboard protocol support to run properly —
see the [documentation](https://truedaerk.github.io/ike/) for terminal setup.
`go run ./cmd/keyprobe` tells you what your terminal actually delivers, which
is the first thing to check when a keybinding "does nothing".

The documentation site builds with:

```sh
pip install -r userdocs/requirements.txt
mkdocs serve                 # preview on http://127.0.0.1:8000
mkdocs build --strict        # what CI runs
```

## Licensing of contributions

By submitting a pull request you agree that your contribution is licensed
under the same terms as the project — see [`LICENSE`](LICENSE) (MIT with the
Commons Clause). There is no separate CLA.

## Reporting bugs and vulnerabilities

Bugs: open an issue with the `bug` label. Include your terminal emulator and
version — a surprising share of "IKE ignores my keys" reports are the terminal
claiming the chord before IKE sees it.

Security issues: do **not** open a public issue. See
[`SECURITY.md`](SECURITY.md).
