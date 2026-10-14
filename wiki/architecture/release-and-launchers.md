---
type: architecture
title: Releases & Desktop Launchers
description: How IKE is shipped outside a source build — the tag-triggered release workflow (per-OS CGo matrix, full + complexity-only archives, checksums), the Scoop / winget manifests it regenerates and commits back, and the deploy/ layout of the desktop launchers (Ghostty on macOS/Linux, WezTerm on Windows) (#1567, #3002)
resource: deploy
tags: [release, packaging, scoop, winget, launcher, ghostty, wezterm, windows, ci]
timestamp: 2026-10-14T15:00:00Z
---

# Releases & Desktop Launchers

IKE is one Go binary, but a usable install is three things: the binary, a
terminal that delivers its chords (Kitty keyboard protocol with the
terminal's own shortcuts cleared), and a way to start the two together. The
release workflow ships the first; the launchers in `deploy/` ship the other
two.

## Release workflow (#3002)

`.github/workflows/release.yml`, triggered by a `v*` tag (publishes),
`workflow_dispatch` (publishes only on a tag ref) and pull requests touching
the release machinery (builds, never publishes):

1. **check** — `go run ./cmd/release version` reads `version.Version`;
   `check-tag` fails a tag that is not `v` + that number, naming
   `internal/version` in the message. The version is not parsed from the
   file text: the helper imports the package.
2. **build** — a plain Actions matrix, one native runner per target, because
   the Tree-sitter grammars need CGo and cross-compiling CGo (MinGW from
   Linux in particular) is out of scope. GoReleaser was not used: per-runner
   outputs merged into one release is its paid split/merge feature.

   | Target | Runner | C toolchain |
   |---|---|---|
   | linux/amd64, linux/arm64 | `ubuntu-latest`, `ubuntu-24.04-arm` | gcc |
   | darwin/arm64, darwin/amd64 | `macos-latest` | Apple clang; amd64 via `CC="clang -arch x86_64"` |
   | windows/amd64 | `windows-latest` | MSYS2 UCRT64 gcc |
   | windows/arm64 | `windows-11-arm` | MSYS2 CLANGARM64 clang |

   Each job runs `scripts/release/package.sh <goos> <goarch>` — the same
   script `make release-archive` runs locally. It builds through the
   Makefile (`make build`, `make build-complexity`), so the commit / dirty
   stamp comes from the Makefile's `LDFLAGS`; on the host platform it smoke
   tests `ike --version` (must carry `(<commit>`) and
   `ike complexity --json .`. Windows checkouts set `core.autocrlf false`:
   embedded query files must stay byte-identical, and a CRLF conversion
   would mark the tree dirty. The Windows jobs also run `ike-gui.cmd` without
   WezTerm (must exit 1 with the hint) and `winget validate` where the image
   has winget.
3. **release** — `cmd/release checksums` writes `checksums.txt` (sha256sum
   format), `cmd/release manifests` regenerates the Scoop and winget
   manifests from it, and `gh release create` attaches everything. The
   manifests are then **committed back to `main`** (never backwards: a
   re-run for an older tag leaves a newer manifest alone), so the raw
   manifest URL always installs the newest release and `scoop update`
   follows it.

Assets per release: `ike_<v>_<os>_<arch>.{zip,tar.gz}` (full IDE plus
`keyprobe`), `ike-complexity_<v>_<os>_<arch>.*` (the #2939 build, binary
still named `ike`), `checksums.txt`, `ike.json` and the three winget YAMLs.
Files sit at the archive root; `.zip` on Windows only.

## Package manifests

Generated, never hand-edited: `cmd/release` owns the templates and
`TestCheckedInManifestsAreGenerated` re-renders the checked-in files from
their own version, date and hashes (so Scoop and winget cannot drift apart).

- **`deploy/scoop/ike.json`** — `depends: extras/wezterm`; `bin` shims
  `ike.exe`, `ike-gui.cmd`, `keyprobe.exe`; `shortcuts` creates **Ike** in
  *Scoop Apps*. The shortcut targets the dependency's
  `..\..\wezterm\current\wezterm-gui.exe` (Scoop resolves it against the
  app's `current` dir) with `--config-file "$dir\wezterm-ike.lua" start
  --always-new-process` and `ike.ico` — a `.cmd` target would flash a
  console window (or a Windows Terminal tab) on every launch. `checkver` /
  `autoupdate` point at the GitHub releases and `checksums.txt`, ready for a
  bucket.
- **`deploy/winget/<version>/`** — exactly one version set (older ones are
  removed on regeneration): `zip` installer with a `portable` nested
  `ike.exe` (alias `ike`), `PackageDependencies: wez.wezterm`,
  `MinimumOSVersion` 10.0.17763 (ConPTY). winget cannot create Start-menu
  entries for a portable zip, so users run the bundled
  `install-desktop.ps1`. Submission to `microsoft/winget-pkgs` is a manual
  maintainer step (userdocs, desktop launcher page).

Until the first tag is pushed the checked-in manifests carry zero hashes;
the first release overwrites them.

## Launcher layout in deploy/

| Path | Platform | Role |
|---|---|---|
| `deploy/ike-gui` | macOS, Linux | launcher script (#1567): login-shell re-exec for `PATH`, deep-link hand-off, Ghostty with the exclusive config; fallbacks kitty / WezTerm / foot on Linux |
| `deploy/ghostty/ike.conf` | macOS, Linux | dedicated Ghostty config: `keybind = clear` + minimal re-adds |
| `deploy/Info.plist`, `deploy/ike.desktop` | macOS / Linux | the platform shells; installed by `scripts/install-desktop.sh` |
| `deploy/ike-gui.cmd` | Windows | launcher: finds `wezterm-gui.exe` (`%IKE_WEZTERM%`, Scoop, installer / winget dirs, then `PATH` — real dirs first because a Scoop shim is a console program), `ike.exe` (own dir, `PATH`) and `wezterm-ike.lua` (own dir, beside `ike.exe`, winget package dir); starts `--always-new-process` on `%CD%`; exit 1 with a one-line hint without WezTerm |
| `deploy/wezterm/ike.lua` | Windows | dedicated WezTerm config, shipped as `wezterm-ike.lua`: `enable_kitty_keyboard`, `disable_default_key_bindings` + the Ghostty config's Linux re-adds, `default_prog` = the `ike.exe` beside it (`wezterm.config_dir`), `default_cwd` = home, `exit_behavior = "Close"`, no tab bar, JetBrains Mono + Windows fallbacks, `COLORTERM=truecolor`; `allow_win32_input_mode` stays on, the path that carries full key events through ConPTY |
| `deploy/install-desktop.ps1` | Windows | Start-menu entry for zip / winget installs (`-Uninstall` removes it) |
| `deploy/icon/` | all | `gen` renders `ike-1024.png`; `make icons` derives hicolor PNGs, `ike.icns`, and `ike.ico` (`deploy/icon/ico` packs the hicolor PNGs as PNG entries; a test fails when it is stale) |

`--always-new-process` matters on Windows: `wezterm start` otherwise hands
the window to an already running WezTerm GUI, which would apply the user's
config instead of IKE's.

The Windows launcher does not register the `ike://` scheme; the registry
recipe stays manual ([Deep Links](./deep-links.md)).

## Windows build portability

`GOOS=windows` must keep compiling (the release matrix fails otherwise).
Unix-only calls sit behind build-tagged files with a Windows stand-in:
`internal/terminal/fgpgrp_*.go` (`TIOCGPGRP`; ConPTY has no process groups,
so `AtPrompt` assumes the prompt and `ForegroundPid` reports nothing),
`internal/perfhud/rusage_*.go` (`getrusage` peak RSS; omitted on Windows),
`internal/diag/dumpsig_*.go` (`SIGUSR1` dump trigger; none on Windows), next
to the existing `spawn_*.go` / `cwd_*.go`. A quick local check:
`GOOS=windows CGO_ENABLED=0 go build ./...` type-checks everything outside
the cgo grammar bindings.

Known gap: `creack/pty` v1.1.24 has no ConPTY backend (`StartWithSize`
returns `ErrUnsupported` on Windows), so the integrated terminal and every
PTY-backed tool (Run configurations, tasks) cannot spawn on Windows yet; the
binary, editor, LSP and complexity report are unaffected.
