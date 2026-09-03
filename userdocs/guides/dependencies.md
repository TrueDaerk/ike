# Dependencies

The **Dependencies** tool window (`cmd+0`, or *Tools ▸ Dependencies*) lists
your project's declared dependencies per manifest file — the current version,
the latest available one, whether the dependency is direct or transitive, and
how many known vulnerabilities affect it.

IKE reads the manifests in the project root — `go.mod`, `package.json`,
`composer.json`, `Cargo.toml`, `requirements.txt`, `pyproject.toml` — and
asks *your* toolchain for the rest: `go list -m -u` and `govulncheck` for Go,
`npm`/`pnpm`/`yarn outdated` and `audit` for JavaScript (the package manager
is detected from the lockfile), `composer outdated`/`audit` for PHP, `cargo
outdated`/`cargo audit` for Rust (when those cargo subcommands are
installed), and `pip`/`uv` with `pip-audit` for Python. IKE itself never
talks to the network — only the tools do. If a tool is missing, a dialog
tells you how to install it; nothing crashes.

## Scanning

A scan runs in the background — the status line shows `⟳ deps: scanning`
while it works. By default one scan starts when you open a project that has
manifests (*Settings ▸ Dependencies ▸ Scan dependencies on project open*).
Results are cached until a manifest changes; `deps.refresh` (or `r` in the
window) forces a fresh scan, and `deps.audit` does the same from the
security angle.

## In the window

| key | action |
|---|---|
| `enter` | open the manifest at the dependency's line |
| `u` | update the dependency: rewrite the version in the manifest, then optionally run the install step (`go mod tidy`, `npm install`, …) — always behind a confirmation |
| `v` | show the vulnerability details |
| `r` | rescan |
| `/`, `cmd+f` | filter — `manifest:`, `state:outdated`, `state:vulnerable`, or free text over names |

## In the editor

Hovering a dependency line in a manifest shows `current → latest` and any
advisories. `alt+enter` on such a line offers **Update … to latest** — the
same rewrite-then-confirm flow as `u`. Vulnerable dependencies also appear
in the Problems window (`cmd+8`) as warnings, so they surface even with the
Dependencies window closed.
