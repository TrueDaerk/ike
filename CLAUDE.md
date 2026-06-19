# CLAUDE.md

Guidance for Claude Code (and humans) working in this repository.

## Project

**IKE** is a terminal IDE built with [bubbletea](https://github.com/charmbracelet/bubbletea).
It is a Jetbrains inspired TUI IDE, but with vim like controls in the editor.
It supports windowing, tabs, panes and resizing / moving any pane to another location.

## Where to look first

- **Roadmaps** live in `roadmaps/` and define the plan and build order.
- **Progress tracking:** `roadmaps/PROGRESS.md` has a checklist with one box per roadmap;
  each roadmap document has a `## Milestones` checklist for its sub-tasks. As you complete work, tick
  the milestone boxes, and tick a roadmap in the Progress list once all its milestones are done.

## Working agreements

- **Language:** write all code, comments, doc strings, descriptions, and commit messages in **English**,
  unless explicitly asked otherwise. (Conversational replies may be in the user's language.)
- **Testing & coverage:** new code should ship with tests.

## Wiki

The `wiki/` directory is an **OKF (Open Knowledge Format) v0.1** bundle — hierarchical markdown organized
for progressive disclosure by humans and agents. The format is specified at
<https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md>.

Rules that matter when reading or writing the wiki:
√
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
