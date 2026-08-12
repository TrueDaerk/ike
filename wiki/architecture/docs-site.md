---
type: architecture
title: Documentation Site Build & Deploy
description: MkDocs user-docs pipeline — strict PR builds, gh-pages branch deploy from both CI (docs.yml) and the local `make docs-deploy` escape hatch, and the one-time Pages source switch that makes it work (#1837).
resource: Makefile
tags: [docs, ci, pages, mkdocs]
timestamp: 2026-08-12T00:00:00Z
---

# Documentation Site Build & Deploy

The user documentation site (Epic #1182, `userdocs/` built by `mkdocs`) publishes to GitHub Pages
by pushing a built `site/` directory to the repo's `gh-pages` branch — `mkdocs gh-deploy`. Both
`.github/workflows/docs.yml` and the local `make docs-deploy` target use this exact command, so
either can update the live site (#1837).

## Two publish paths, one mechanism

* **CI (`docs.yml`).** A `build` job runs `mkdocs build --strict` on every push and pull request
  touching `userdocs/**`, `docs/screenshots/**`, or `mkdocs.yml` — broken links and warnings fail
  the job. A `deploy` job, gated to pushes on `main`, runs `mkdocs gh-deploy --strict` with
  `contents: write` permission, which builds the site again and force-pushes it to `gh-pages`.
* **Local (`make docs-deploy`).** Requires `mkdocs` on `PATH` (`pip install -r
  userdocs/requirements.txt`); the target fails with a plain error, not a stack trace, if it is
  missing. Runs the same `mkdocs gh-deploy --strict` — useful when Actions is slow or unavailable
  and someone wants to publish a docs change straight from a checkout (#1606 is what motivated
  this: the Pages queue has backed up before).

Two concurrent `gh-deploy` pushes race on `gh-pages`. CI's `docs.yml` keeps a `concurrency: pages`
group to serialize itself; a local `make docs-deploy` run has no such guard, so avoid deploying at
the same moment CI is mid-publish.

## One-time repo setup: Pages source

Branch deploys only take effect if the repo's Pages settings are configured for them. Under
**Settings → Pages → Build and deployment → Source**, the source must be **"Deploy from a
branch"** pointed at `gh-pages` / `/ (root)` — not `"GitHub Actions"`. With the Actions source
selected, `gh-pages` commits sit in the branch but nothing serves them; the workflow's old
`actions/deploy-pages` step (removed by #1837) was what used to do the serving, and there is no
Actions-based equivalent once branch deploys are in play. This switch was performed once as part
of #1837; it does not need repeating.

Related: [Documentation Screenshots](/architecture/screenshots.md) (the `make shots` half of the
docs pipeline), [Versioning](/architecture/versioning.md).
