---
type: process
title: Change Workflow
description: The lifecycle of every change — issue first, issue branch, version bump, PR, merge, branch cleanup, plus the settings-UI, default-keybind and span-family-ledger obligations.
tags: [process, git, release]
resource: internal/version/version.go
timestamp: 2026-08-31T00:00:00Z
---

# Change Workflow

Every change — feature, fix, docs — goes through the same loop. No direct commits to `main`.

1. **Issue first.** Before touching code, make sure a GitHub issue exists for the change
   (after the duplicate check in [GitHub Issue Workflow](/process/issues.md)); create one otherwise.
2. **Branch per issue.** Work on `issue/<number>-<slug>` (e.g. `issue/12-project-picker`),
   branched from an up-to-date `main`. Reference the issue in commits where useful.
3. **When done, bump the patch version.** Raise the last component of `Version` in
   `internal/version/version.go` (e.g. `0.1.6` → `0.1.7`) as part of the branch, in its own
   `chore(version)` commit.
4. **Open a PR on GitHub.** Body says `Closes #<number>` so the issue auto-closes on merge.
5. **Merge into `main`.** Merging closes the issue and finishes the task — don't leave PRs
   open or issues dangling.
6. **Clean up.** Delete the issue branch (local and remote), switch back to `main`, and pull.

Definition of done and epic bookkeeping (ticking the task list, closing milestones) are in
[GitHub Issue Workflow](/process/issues.md).

## New settings ship with UI support

Every new configuration setting must also be **configurable in the Settings UI, in the same
change** that introduces it — a setting reachable only by hand-editing the config file is
considered incomplete. Concretely: extend the matching settings page/form
(`internal/settings/`), validate the input there, persist it through the write-back layer,
surface it in any list rendering, and cover it with tests.

Where the config validator has to be lenient (downgrading a bad combination to a diagnostic,
because a config file on disk must still load), the form is free to be **strict** and reject
the input outright with a clear message — that is the preferred behavior, since the user is
editing interactively and can fix it immediately.

## New commands ship with a default keybind

Commands are driven by keybind far more often than through the palette, so a command that
ships **palette-only is effectively invisible** to everyday use. Every new command therefore
ships with a **default keybind in the same change** — a row in `jetbrainsRows`
(`internal/keymap/defaults.go`), following the existing conventions: JetBrains' own chord
where one exists and is free on both platforms, otherwise the closest family in the table
(`ctrl+alt+*` for view/tool toggles, the numeric `cmd+<n>` tool windows, the F-key run/debug
scheme), single modifier chords only, and conflict-free after the Cmd→Ctrl fold off macOS.
A fragile (Cmd/Alt-modified) primary also needs its escape route recorded in
`reachableAlternatives` (`internal/keymap/matrix.go`).

Shipping **without** a keybind stays allowed, but only **with a recorded reason** — the
command already has a vim-native key or a pane-local single key, it is one entry of a picker,
it is a second flavour of a command that already has a chord, `alt+enter`'s intention popup
offers it where it applies, or it is a genuine one-off. The reason goes into the audit ledger
in `cmd/ike/keybind_audit_test.go`, whose test fails the build for any registered command that
is neither bound nor justified — and for any ledger entry that has gone stale.

After changing the default table, refresh the generated documentation in the same change:
`go run ./cmd/docgen` (the `userdocs/reference` pages) and
`IKE_GEN_MATRIX=<file> go test ./cmd/ike -run TestGenerateMatrixMarkdown` for the status
matrix embedded in [Keybindings & Shortcuts](/architecture/keybindings.md). New bindings show
up in the cheatsheet (`f1`) and the palette's shortcut column automatically — both read the
live binding table.

## Languages account for every span family

Language plugins wire their stand-in and hint families — unicode-escape decoding, entity
decoding, base64 decoding, secret masking, network/permission/cron/number hints — by hand in
their `lang.Language.Spans` hook, so a family a language *could* offer but does not is
invisible until someone notices (that is how the unicode decoding of #1620 missed Python,
PHP, YAML and TOML until #2334). The audit ledger in `cmd/ike/spanfamily_audit_test.go`
(#2337) closes that hole: every registered language must, for every audited family, either
**offer it** (verified behaviourally — the test runs the hook over a per-family probe buffer
and looks for the family's capture) or carry a **ledger entry with a reason** from the
ledger's small named set (no syntax for such literals, no marking convention in the format,
foreign data rendered verbatim, injection helper, or a genuine gap linked to its tracking
issue).

Concretely: a **new language** fails the build until its row is filled in; **wiring a
family** means moving its cell to `offeredSpanFamilies` (and extending the family's probe if
the new producer needs a shape the probe lacks); a reason entry for a family the language
has since started offering is **stale and fails**. Real gaps are never recorded as a bare
reason — they are closed or linked as a follow-up issue (`reasonGap`, currently #2345).
