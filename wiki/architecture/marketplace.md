---
type: concept
title: Plugin Marketplace
description: Roadmap 0310 — discover, install, update and remove WASM plugins from inside the IDE; static HTTPS catalog, checksum-verified installs, capability review before install.
resource: internal/market
tags: [architecture, plugins, wasm, marketplace, settings]
timestamp: 2026-07-12T00:00:00Z
---

# Plugin Marketplace

Roadmap 0310 (epic #443, promoted from idea #134). The marketplace lets a user
browse a plugin catalog, review what a plugin may do, and install, update or
remove it — all without leaving the IDE. It builds directly on the
[WASM runtime](./plugin-authoring.md) (plugins as `.wasm` + manifest sidecar in
the plugins directory) and surfaces as a page in the
[settings panel](./settings-ui.md).

## Catalog (`internal/market/catalog.go`, `fetch.go`)

The catalog is a **static JSON document** (`index.json`) served over HTTPS —
a raw file in a git repository is enough. The location comes from
`marketplace.catalog_url` (user scope) falling back to the built-in
`market.DefaultCatalogURL` (currently empty: no catalog configured → the page
says so and does nothing).

Format version 1:

```json
{
  "version": 1,
  "plugins": [
    {
      "name": "example",
      "version": "1.2.0",
      "description": "one-line summary",
      "homepage": "https://…",
      "capabilities": ["commands", "notify"],
      "artifact": {
        "url": "https://…/example-1.2.0.wasm",
        "sha256": "<hex digest of the .wasm>"
      }
    }
  ]
}
```

Parsing is strict per entry and tolerant per document (mirroring
`wasm.ScanDir`): an unsupported top-level `version` rejects the document; an
entry with a bad name (must be a safe file base — it becomes `<name>.wasm`),
unparsable `MAJOR.MINOR.PATCH` version, unknown capability (validated against
`wasm.KnownCapabilities`), non-HTTPS artifact URL or malformed sha256 is
skipped with a diagnostic while the rest loads. Fetches run through
`market.Client` — injectable transport, 15 s timeout, 4 MiB index cap.

## Install engine (`internal/market/engine.go`)

`market.Engine` operates on the plugins directory (`wasm.DefaultDir()`):

- **Install** downloads the artifact (64 MiB cap), verifies its SHA-256
  against the catalog digest — a mismatch or failed download writes nothing —
  and atomically (temp file + rename) places `<name>.wasm` plus the
  `<name>.manifest.json` sidecar.
- **Trust model:** the sidecar written at install time pins the *catalog's*
  capability list, so the runtime's capability gate enforces exactly what the
  user reviewed. Ordering guards the pin: the manifest lands **before** the
  module (a module without a sidecar runs unrestricted), and `Remove` deletes
  the module **first** (a leftover sidecar is inert). Sandboxing itself is the
  9900 runtime: no FS/net/env, memory cap, call timeouts.
- **Update** is the same operation, offered when the catalog version compares
  greater than the installed manifest version. Hand-installed plugins without
  a parsable manifest version are never offered updates.
- **Installed state** is scanned from the sidecars in the plugins directory;
  there is no separate state file.

Signature verification beyond the checksum is deliberately out of scope for
v1: the sha256 pins artifact↔catalog, catalog authenticity rests on HTTPS plus
the configured URL.

## Marketplace page (`internal/settings/marketplace_page.go`)

A custom `settings.PageModel` ("Marketplace", registered in
`internal/app/app.go` next to the Plugins page). The list shows each catalog
entry with its status (available / installed / `update 1.0.0 → 1.2.0` /
working…); `enter` expands the detail — versions, homepage and the **full
capability list**. `i` installs or updates, and only works from the expanded
detail: the review is structurally in front of the action. `x` removes, `r`
re-fetches the catalog.

All network and disk work happens inside `tea.Cmd`s; results return as
`settings.MarketCatalogMsg` / `settings.MarketActionMsg`, routed through the
panel's `Deliver` (the app also toasts the headline). Failures render inline
under the row. Because the runtime scans the plugins directory at startup,
every successful install/update shows a "restart IKE to load" notice.

Enable/disable and capability inspection of *installed* plugins stay on the
[Plugins page](./settings-ui.md); the marketplace only manages presence and
versions.
