---
type: concept
title: Run Configurations
description: Work stream 0350 — named, persisted run/debug configurations synthesized into command lines through the language registry; per-project store in .ike/runconfigs.json.
resource: internal/run
tags: [architecture, run, debug, toolchain, languages]
timestamp: 2026-07-14T00:00:00Z
---

# Run Configurations (0350)

Epic #572. `internal/run` holds JetBrains-style run configurations: named,
persisted descriptions of how to run (or debug) a file. A configuration is
**data, not a shell string** — the command line is synthesized at launch, so
interpreter changes (venv switch, explicit `[lang.<id>] interpreter`) apply
to every later run automatically.

## The model (`internal/run`)

```go
type Config struct {
    Name   string            // unique; base name, or relative path on collision
    Kind   Kind              // "run" | "debug" — a debug launch reuses the run's data
    Lang   string            // language id in the registry
    File   string            // project-relative target file
    Module string            // language module spelling (Python -m), optional
    Args   []string          // program arguments
    Env    map[string]string // extra environment
    Cwd    string            // project-relative working dir; "" = root
}
```

- **Store**: `run.Load()` / `run.Save(store)` persist `.ike/runconfigs.json`
  (`IKE_CONFIG_DIR` override like session/layout). `Store` keeps the ordered
  config list plus `LastUsed` (the rerun-last target, `Touch`/`Last`).
  Missing or malformed files load as empty — run configs are convenience
  state, never a startup error; a failed save must not abort the run.
- **Default synthesis**: `Store.EnsureFor(root, file)` returns the config for
  a file, creating and remembering the default on first run: kind `run`, no
  env, cwd = project root, the language's module form when the file lies in
  a package, name = base name (relative path on collision).
- **Launch**: `run.Argv(root, cfg, explicitInterpreter)` resolves the argv
  through the language seam below; `Config.Dir(root)` and
  `Config.EnvSlice()` feed the terminal spawn.

## The language seam (`internal/lang/run.go`)

Language plugins contribute run behavior via optional `Toolchain` extensions:

- `RunCommandProvider.RunCommand(root, RunSpec{File, Module, Args},
  interpreter) (argv, ok)` — the interpreter arrives pre-resolved via
  `lang.Interpreter` (explicit config beats detection, one source of truth
  with the LSP/terminal shims).
- `ModuleResolver.Module(root, file) (module, ok)` — the file's module
  spelling for default configs.

Registered providers:

| Language | Command | Module form |
|---|---|---|
| Python | `<interpreter> file.py` / `<interpreter> -m pkg.mod` | dotted path when every directory from root to the file is a package (`__init__.py` chain); `__main__.py` maps to its package |
| PHP | `<php> file.php` | — |
| Go | `<go> run file.go` | — |

## Running (#576)

`internal/app/run.go` wires the commands end to end:

- **`run.file`** (shift+f10 — JetBrains' Windows-keymap Run; macOS ctrl+r
  would shadow vim redo — Run menu, palette) ensures a configuration for the
  active file (`EnsureFor`; the first run persists the default and says so in
  the toast) and launches it. **`run.rerun`** repeats the last-used config.
- The command runs as a **terminal command session** (#574) — interactive
  stdin, exit code shown on completion — with the toolchain shim env plus the
  config's env overlay, in the config's cwd; the terminal is labelled with
  the config name.
- **Placement**: a reusable terminal (never typed into, or finished) is taken
  over in place first (`ReusableRunTerminal` + `StartCommand`). Otherwise the
  `run.placement` setting (settings page "Run", default `in_pane`) decides:
  `in_pane` opens a terminal tab in the focused editor pane (#573),
  `new_terminal` a bottom-split terminal pane. A command session's pane stays
  open on exit — the output is the point of the run.

## Consumers

- The DAP debugger (#578/#579) launches from the same configuration with
  kind `debug`.
