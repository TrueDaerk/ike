# Roadmap 0081 — Keybinding Audit & User-Friendly Activation

Roadmap 0080 built the keybinding **machinery**: the chord/key model, the
JetBrains default table, context-scoped resolution, conflict detection, platform
normalisation, the resolver, and a cheatsheet. What it did **not** deliver is a
working, discoverable, terminal-honest set of bindings the user can actually
press. Today most default bindings are **inert** (their command id has no owner
registered) or **terminal-fragile** (the chord never reaches a TUI: `Cmd+*`
intercepted by the OS, `Ctrl+Tab` eaten by the terminal, `shift shift`
undetectable without key-up events).

**This roadmap is a controlled audit:** take every binding one at a time, prove
whether it reaches the program and whether its command exists, then make it
either truly live, reachably aliased (leader/palette), or honestly surfaced as
blocked — with an automated + manual verification trail per binding. The goal is
*user-friendliness*: a person opening IKE can find a shortcut, press it, and have
it do the right thing or learn the working alternative.

It does **not** invent a new binding engine — it consumes 0080's `internal/keymap`
— and it does not re-own commands that belong to other roadmaps; where a command
is missing it either registers a thin app-level command, aliases an existing id,
or records a precise blocked-by dependency.

## Sub-documents (build order)

Work the docs in order; each carries its own `## Milestones`.

1. [Terminal Reality & Capability Probe](./10-terminal-reality.md) — establish,
   per chord, whether the terminal actually delivers it; build a small probe and
   a ground-truth reachability table. Everything downstream keys off this.
2. [Command Coverage & ID Reconciliation](./20-command-coverage.md) — for every
   binding, make the target command id real: register a thin command, alias an
   existing id (e.g. `editor.save` → `editor.write`), or mark blocked-by-roadmap.
3. [Leader Key & Terminal-Safe Defaults](./30-leader-and-safe-defaults.md) — a
   leader prefix (`space` outside the editor, `Ctrl+K …` universal) so every
   fragile action has a chord that *does* arrive; pick the primary default per
   binding from the reachability table.
4. [Discoverability](./40-discoverability.md) — which-key hints on a held leader,
   the `palette.keymapHelp` cheatsheet wired live, palette shortcut column,
   honest "fragile → use X" labelling.
5. [Per-Binding Status Matrix](./50-binding-matrix.md) — the controlled checklist:
   one row per binding with reachable?/live?/primary chord/fallback/verified
   columns. This is the acceptance ledger; the roadmap is done when every row is
   resolved.

## Design rules

- **One binding at a time, gated.** A binding is not "done" until it passes the
  per-binding Definition of Done (see the matrix doc): command exists → chord
  reaches the program (or a reachable alias does) → conflict-free in context →
  discoverable → verified (automated test + one manual terminal check).
- **Terminal truth beats aspiration.** A pretty `Cmd+T` that never arrives is
  worse than an honest `Space u p`. The reachability table (doc 10) is the source
  of truth; defaults are chosen from it, not from JetBrains nostalgia.
- **Never silently dead.** A binding whose command can't exist yet (e.g. `vcs.*`)
  is surfaced in the cheatsheet as *blocked: needs Roadmap NNNN*, not hidden and
  not silently inert.
- **Every action reachable without a mouse and without a fragile chord.** The
  command palette (07) and the leader key (doc 30) are the two universal paths;
  the audit guarantees each action sits on at least one of them.
- **Reconcile, don't fork, command ids.** Where 0080's table names an id that
  differs from the registered one, fix it at the source (rename/alias) so the id
  is canonical across palette, cheatsheet, and keymap — no parallel vocab.
- **Defaults stay data, overrides stay config.** All changes land in
  `internal/keymap/defaults.go` (data) and the config override path (04); no
  binding logic is special-cased in panes.

## Prerequisites / Dependencies

- **0080 Keybindings** — owns `internal/keymap` (model, table, resolver,
  cheatsheet) and the `internal/app` dispatch hook. 0081 consumes and tunes it;
  it may extend `defaults.go`, add a leader resolver mode, and add a probe, but
  the core resolution stays 0080's.
- **0040 Settings** — the `[keymap]` override path and precedence; 0081 may add a
  `[keymap].leader` tunable and `fragile`/probe results read-only.
- **0070 Command Palette** — universal fallback and the host of
  `palette.keymapHelp`; 0081 wires the cheatsheet and shortcut column live.
- **0020 Plugins registry** — where thin app-level commands register (doc 20).
- **Downstream owners (blocked-by, do not block this roadmap):** 05 explorer, 06
  editor, 09 project, and a future VCS roadmap own the real semantic commands.
  Where a binding targets an unowned id, 0081 records the dependency in the matrix
  and (where cheap and within app scope) registers a temporary app-level command.

## Milestones (roadmap-level)

- [ ] **10** Terminal reachability table established from a real probe; every
  default chord classified delivered / fragile / undetectable.
- [ ] **20** Every default binding's command id is real: registered, aliased, or
  explicitly blocked-by-roadmap with the dependency recorded.
- [ ] **30** Leader key implemented; every fragile/undetectable binding has a
  reachable leader (and/or palette) alternative; primary defaults re-picked from
  the reachability table.
- [ ] **40** Discoverability shipped: live cheatsheet (`palette.keymapHelp`),
  which-key leader hints, palette shortcut column, honest fragile labelling.
- [ ] **50** Per-binding status matrix fully resolved — every row reachable, live
  or honestly blocked, and verified (automated test + manual terminal check).
- [ ] Tests: probe parsing, leader resolution + timeout, alias/registration
  coverage, cheatsheet reflects live vs blocked, no same-context conflicts after
  re-pick.
- [ ] Wiki: update `wiki/architecture/keybindings.md` (leader, reachability,
  discoverability), bump timestamp, add a `log.md` entry.

## Out of scope

- **A new binding engine** — 0081 tunes 0080's `internal/keymap`, it does not
  replace it.
- **The real semantic commands owned elsewhere** — explorer/editor/project/VCS
  commands belong to their roadmaps; 0081 only guarantees the *binding experience*
  (reachable, discoverable, verified) and records blocked-by dependencies.
- **An interactive in-app rebinding UI** — overrides remain config-driven
  (Roadmap 0080 Out-of-scope item still stands); a keymap editor is later.
- **Mouse/gesture bindings** — keyboard only.
