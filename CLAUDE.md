# CLAUDE.md

Guidance for Claude Code (and humans) working in this repository.

## Project

**IKE** is a terminal IDE built with [bubbletea](https://github.com/charmbracelet/bubbletea).
It is a Jetbrains inspired TUI IDE, but with vim like controls in the editor.
It supports windowing, tabs, panes and resizing / moving any pane to another location.

## Where to look first

- **All planning lives in GitHub issues** on `TrueDaerk/ike` (see "GitHub issues" below).
  Specs (architecture, design rules) are held verbatim in **epic** issues; work items are
  sub-issues linked from the epic's task list.
- **Progress tracking:** GitHub **milestones** (one per epic) show completion; an epic's
  task list mirrors its sub-issues. The former `roadmaps/` directory was migrated into
  epics #37–#41 and deleted — its history remains in git.

## Working agreements

- **Language:** write all code, comments, doc strings, descriptions, and commit messages in **English**,
  unless explicitly asked otherwise. (Conversational replies may be in the user's language.)
- **Testing & coverage:** new code should ship with tests.

## GitHub issues

All planning and work tracking happens as GitHub issues on `TrueDaerk/ike` (use the `gh` CLI).
There is no roadmap directory anymore — the structure is:

- **Epic issue** (label `epic` + `roadmap:NNNN`): holds the full spec verbatim (architecture,
  design rules, milestones) and a `- [ ] #N` task list of its sub-issues. One epic per work stream.
  Current epics: #37 (0090 Project Switching), #38 (0100 LSP deferred), #39 (0081 Keybinding Audit),
  #40 (0082 Usability Review), #41 (9900 WASM Plugins).
- **Sub-issue**: one independently completable, reviewable task, linked from its epic's task list.
- **GitHub milestone** (one per epic): assigned to the epic and all its sub-issues; its progress
  bar is the progress tracking. Close the milestone when the epic is done.

### Labels

| Label | Meaning |
|---|---|
| `epic` | Umbrella issue holding a full spec + sub-issue task list. |
| `roadmap:NNNN` | Work-stream tag (e.g. `roadmap:0090`); shared by an epic and all its sub-issues. New stream → new label + new milestone. |
| `idea` | Gap-analysis proposal, not yet planned. Promoting an idea = write the spec into a new **epic** issue, create its `roadmap:NNNN` label + milestone, split into sub-issues, close the idea with a link to the epic. |
| `bug` | Defect in shipped behavior. |
| `enhancement` | New feature or improvement (usually combined with `roadmap:*` or `idea`). |
| `documentation` | Wiki / README work only. |

### Issue conventions

- **Title:** sub-issues are prefixed with their stream number — `0090: <what>` (sub-doc form
  `0081/30: <what>`); epics use `Epic NNNN: <name> (spec)`; ideas use `idea: <what>`.
  Titles and bodies in English.
- **Body:** link the spec (`spec: #<epic>`), list concrete acceptance criteria as a `- [ ]`
  checklist, name dependencies by issue number (`Depends on #12`), and include tests + wiki updates
  in the checklist when they apply.
- **Scope:** one issue = one independently completable, reviewable task. Split rather than batch.

### Before creating an issue: check for duplicates

1. Search open **and** closed issues: `gh issue list --state all --search "<keywords>"` (try both
   feature terms and the stream number).
2. Check the stream's label: `gh issue list --state all --label "roadmap:NNNN"`.
3. Skim the matching epic's task list.
4. If a matching issue exists, extend/comment on it instead of opening a new one; if it exists but
   is closed and the problem is back, reopen it.

### Working an issue

1. Pick an issue whose dependencies (`Depends on #N`) are closed; comment briefly that work starts
   (or assign yourself).
2. Branch per issue: `issue/<number>-<slug>` (e.g. `issue/12-project-picker`).
3. Reference the issue in commits where useful; the closing PR/commit uses
   `Closes #<number>` so the issue auto-closes on merge.
4. Definition of done: acceptance checklist ticked, tests pass, wiki updated where behavior
   changed, the epic's task-list box for this issue ticked. When the last sub-issue closes,
   close the epic and its milestone.
5. Discoveries out of scope while working an issue become **new** issues (after the duplicate
   check), not scope creep on the current one. New sub-issues get added to their epic's task
   list and milestone.

## Wiki

The `wiki/` directory is an **OKF (Open Knowledge Format) v0.1** bundle — hierarchical markdown organized
for progressive disclosure by humans and agents. The format is specified at
<https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md>.

Rules that matter when reading or writing the wiki:

- **Concept documents** (every `.md` that is not a reserved file) MUST have parseable YAML frontmatter
  with a non-empty `type` field. Also include the recommended fields: `title`,
  `description` (one-line summary), and where the concept is backed by source, `resource`
  (a repo-relative path to the code it documents). `tags` and `timestamp` (ISO 8601) are optional but encouraged.
- **Reserved files** are `index.md` and `log.md`:
  - `index.md` provides directory listings for progressive disclosure and contains **no frontmatter**
    (the sole exception: the root `index.md` may carry `okf_version: "0.1"`). Entries use `* [Title](url) - description`.
  - `log.md` (optional) records changes newest-first under `## YYYY-MM-DD` headings.
- **Cross-links** are bundle-relative (`/core/config.md`) or relative (`./other.md`);
  broken links are tolerated (may point at future docs).
- Consumers must tolerate unknown `type` values, unknown keys, and missing optional fields gracefully.

**Keep the wiki current.** When you change behavior the wiki documents (a feature, a subsystem, the architecture),
update the matching concept document in the same change, refresh its `timestamp`, and add a `log.md` entry. Treat the
wiki as part of the deliverable, not an afterthought.
