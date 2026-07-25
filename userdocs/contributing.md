# About this project & contributing

## What IKE is

IKE is a personal project. It is built by one person, to that person's taste,
with heavy AI assistance — "vibe-coded" is a fair description. The defaults
follow a specific JetBrains muscle memory, on a German keyboard, in a specific
terminal, because that is the setup it was built for.

None of that is a warning label. It is just useful to know before you decide
whether it fits how *you* work.

- **Use it if you like it.** It is public on purpose. The
  [licence](https://github.com/TrueDaerk/ike/blob/main/LICENSE) is MIT with
  the Commons Clause: do what you want with it, including building commercial
  software with it — just do not sell IKE itself.
- **There is no support promise.** No SLA, no roadmap commitment, no
  guarantee a feature survives a refactor. Issues get worked on when they are
  interesting or in the way.
- **Improvements are very welcome.** Bug fixes, tests, documentation, and
  features that sit cleanly behind a setting all get read, and merged when
  they fit.

The one rule worth knowing in advance: a change that *adds* something has an
easier time than one that reshapes an existing default to a different taste.
If you are unsure which yours is, open an issue and ask before writing the
patch.

## How to contribute

The full guide lives in the repository:

- **[CONTRIBUTING.md](https://github.com/TrueDaerk/ike/blob/main/CONTRIBUTING.md)**
  — the issue-first workflow, branch naming, commit style, tests, and what to
  update in the docs.
- **[SECURITY.md](https://github.com/TrueDaerk/ike/blob/main/SECURITY.md)** —
  reporting a vulnerability privately instead of in a public issue.

The short version:

1. Open an issue, or comment on an existing one, before writing code.
2. Branch as `issue/<number>-<slug>`.
3. Ship tests with the change; `go test ./...` has to pass.
4. Update `wiki/` (architecture) and `userdocs/` (this site) when your change
   invalidates them.
5. Open a PR whose body says `Closes #<number>`.

Everything is written in English — code, comments, commits, issues, PRs.

## Filing a good bug report

Include your **terminal emulator and version**. A large share of "IKE ignores
my keys" reports turn out to be the terminal claiming the chord before IKE
ever sees it — [Troubleshooting](troubleshooting.md) covers how to tell the
difference, and `go run ./cmd/keyprobe` answers it definitively.
