# 0081/20 — Command Coverage & ID Reconciliation

Make every default binding point at a **real** command id. A binding to an
unregistered id is inert; the user presses the key and nothing happens, with no
feedback. For each binding, resolve into exactly one of three states:

1. **Live** — the id is registered and does the right thing.
2. **Aliased / app-command** — the id is reconciled to an existing command, or a
   thin app-level command is registered (within app/keymap scope) to back it.
3. **Blocked** — the real command belongs to a not-yet-built roadmap; record the
   precise dependency and surface it in the cheatsheet as blocked (doc 40).

## Known gaps (from the 0080 audit)

- **Registered today:** `editor.undo`, `editor.redo`, `editor.write`,
  `editor.quit`, `editor.write_quit`, `explorer.toggleHidden`,
  `explorer.refresh`, `explorer.collapseAll`, `explorer.reveal`.
- **ID mismatch:** the table binds `editor.save` but the registered id is
  `editor.write`; `cmd+w` → `editor.closeTab` while close is `editor.quit` /
  app `CloseFocused`. Reconcile names so one canonical id is used by palette,
  cheatsheet, and keymap.
- **App-ownable (cheap):** `explorer.toggle` (show/hide tree), `pane.switcher`
  (focus cycle — app already has `cycleFocus`/`FocusDir`), `nav.back`/`nav.forward`
  (if a navigation stack is in app scope) — register as app-level commands so the
  binding is live now instead of waiting.
- **Blocked-by-roadmap:** `editor.duplicateLine`, `editor.comment*`,
  `editor.copy/cut/paste`, `editor.find/replace`, `editor.findUsages`,
  `editor.rename`, `editor.gotoDeclaration` → **06/10**;
  `project.goToFile/goToClass/findInPath/replaceInPath` → **09**;
  `palette.searchEverywhere/recentFiles/keymapHelp` → **07**;
  `vcs.commit/updateProject/revertFile` → **future VCS**.

## Approach

- **Reconcile ids at the source.** Prefer renaming/aliasing the registered
  command (e.g. expose `editor.save` as the canonical id, keep `:w` ex-command)
  over editing the keymap table to a non-JetBrains id. Update the 0080 default
  table only where the JetBrains id is genuinely wrong.
- **Thin app commands where it's in app scope.** Register `explorer.toggle`,
  `pane.switcher`, etc. as `plugin.Command`s in `internal/app` (or the owning core
  plugin) so their bindings go live without waiting on a feature roadmap. Keep
  them minimal — they dispatch existing app behaviour, no new feature.
- **Record blocked dependencies as data.** A binding to an unowned id is tagged
  `blocked: <roadmap>` so the matrix (doc 50) and cheatsheet (doc 40) can show it
  truthfully. Add the dependency line to `roadmaps/PROGRESS.md` "Known gaps".
- **Feedback on inert press.** While blocked, pressing the chord should not feel
  dead: emit a transient status hint ("Comment line — needs editor command
  (Roadmap 0060)") rather than swallowing silently. (Coordinated with doc 40.)

## Milestones

- [ ] Reconcile `editor.save`↔`editor.write` and `editor.closeTab`↔close into canonical ids; update palette/cheatsheet/keymap to one vocabulary.
- [ ] Register thin app-level commands for the app-ownable bindings (`explorer.toggle`, `pane.switcher`, `nav.back`/`nav.forward` if in scope) so they go live.
- [ ] Tag every remaining binding `blocked: <roadmap>` with the dependency recorded in PROGRESS.md.
- [ ] Inert-press feedback: a transient status hint instead of a silent no-op for blocked bindings.
- [ ] Tests: each non-blocked binding resolves to a registered command; blocked bindings report the expected dependency tag; alias reconciliation keeps `:w` working.
