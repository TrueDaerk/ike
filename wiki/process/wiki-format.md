---
type: process
title: Wiki Format (OKF)
description: Rules for reading and writing this wiki — an OKF v0.1 bundle with frontmatter concept docs, reserved index/log files, and bundle-relative links.
tags: [process, wiki, okf]
timestamp: 2026-07-29T00:00:00Z
---

# Wiki Format (OKF)

The `wiki/` directory is an **OKF (Open Knowledge Format) v0.1** bundle — hierarchical markdown
organized for progressive disclosure by humans and agents. The format is specified at
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

**Keep the wiki current.** When you change behavior the wiki documents (a feature, a subsystem, the
architecture), update the matching concept document in the same change, refresh its `timestamp`, and
add a `log.md` entry. Treat the wiki as part of the deliverable, not an afterthought.
