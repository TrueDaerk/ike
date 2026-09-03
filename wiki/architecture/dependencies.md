---
type: concept
title: Dependencies tool window
description: The singleton Dependencies pane (#2419) lists the project's declared dependencies per manifest — current/latest version, direct/indirect, vulnerability count — by shelling out to the ecosystem toolchain (go, npm/pnpm/yarn, composer, cargo, pip), with manifest hovers, an "Update to latest" code action, vulnerable entries in the Problems store, and confirmed-only install steps.
resource: internal/deps/deps.go
tags:
  - tool-window
  - dependencies
  - security
timestamp: 2026-09-03T00:00:00Z
---

# Dependencies tool window (#2419)

`deps.toggle` (default `cmd+0`, Tools menu) opens the singleton **Dependencies**
pane at the bottom zone, following the Problems pane's toggle state machine
(`internal/app/deps_panel.go`). Rows group by manifest file; each dependency
row shows the name, the current version, `current → latest` when a newer
version is known, direct/indirect, and its vulnerability count.

## Providers

`internal/deps` holds one provider per ecosystem behind a small seam
(`Provider`: `Manifest(path)`, `Outdated(ctx, dir)`, `Audit(ctx, dir)`,
`Bump(path, dep, version)`, `InstallCmd(dir)`, `Tools()`):

| provider | manifest | outdated | audit |
|---|---|---|---|
| go | `go.mod` | `go list -m -u -json all` | `govulncheck -json ./...` |
| npm | `package.json` | `npm/pnpm/yarn outdated --json` (package manager by lockfile) | `npm/pnpm/yarn audit --json` |
| composer | `composer.json` | `composer outdated --format=json` | `composer audit --format=json` |
| cargo | `Cargo.toml` | `cargo outdated --format json` (if cargo-outdated installed) | `cargo audit --json` (if cargo-audit installed) |
| pip | `requirements.txt`, `pyproject.toml` | `pip list --outdated` / `uv pip list --outdated` | `pip-audit --format=json` (if installed) |

All network traffic happens inside those tools — the IDE itself never dials
out. A missing binary is never a crash: each provider names its tools with an
install hint, a manual scan (`deps.refresh` / `deps.audit` / `r`) reports
them in the centered dialog, an automatic scan as a quiet notification.

## Scanning

Scans run in a background `tea.Cmd` (3-minute budget) with a `⟳ deps:
scanning` status-line segment while in flight. `deps.Scanner` caches per
manifest mtime, so re-scans without a manifest edit skip the toolchain;
`deps.refresh` forces past the cache. With `deps.auto_scan = on` (the
default, Settings ▸ Dependencies) one scan starts on project open when the
root holds any manifest (`Init`, re-run on project switch). Only root-level
manifests are scanned, and only those some registered language claims via
`lang.Language.DepManifests` — disabling a language plugin silences its
ecosystem.

A finished scan lands in three places: the pane, the global snapshot
(`deps.SetSnapshot` — the synchronous read for hover and intentions), and
the Problems store (`SetTaskSource("deps", …)`): one severity-warning entry
per vulnerability, anchored at the dependency's manifest line.

## Keys in the pane

`enter` opens the manifest at the dependency's line; `u` bumps the manifest
to the latest version (provider `Bump`, constraint prefixes preserved) and
then offers the install step (`go mod tidy`, `npm install`, …) behind an
explicit confirmation — updates never install unasked; `v` shows the
vulnerability details dialog; `r` forces a rescan; `/` and the shared find
chord open the filter row (`manifest:`, `state:outdated|vulnerable|fresh`,
free text over names). A dirty open editor on the manifest blocks `u` — the
on-disk rewrite would race the unsaved buffer.

## Editor integration

In scanned manifest files a local hover (`ilsp.RegisterLocalHover`) shows
`current → latest` plus any advisories, and the alt+enter intention popup
offers "Update <name> to <latest>" (`deps.updateLatest`), which runs the same
bump-then-confirm flow as `u`. Vulnerable entries appear in the Problems
pane under source `deps` with the advisory id as the code.

## Commands and settings

`deps.toggle` (`cmd+0`, palette fallback), `deps.refresh` (pane `r` /
palette), `deps.audit` (palette), `deps.updateLatest` (intention popup) —
the latter three are ledgered in `cmd/ike/keybind_audit_test.go`. The one
setting is `deps.auto_scan` (bool, default on), configurable in Settings ▸
Dependencies.
