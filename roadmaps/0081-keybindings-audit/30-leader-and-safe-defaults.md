# 0081/30 — Leader Key & Terminal-Safe Defaults

Give every fragile or undetectable action a chord that **actually arrives**, and
re-pick the primary default for each binding from the reachability table (doc 10).
This is where user-friendliness becomes concrete: the shortcut printed in the
cheatsheet is one the user can press in their terminal.

## Leader model

- **Leader prefix.** A reserved, always-delivered key starts a leader sequence,
  building on 0080's multi-step chord + timeout machinery (no new engine):
  - `space` as leader **outside the editor** (explorer / global, where `space`
    isn't text entry), and in the editor's **normal** mode (vim-style leader).
  - `Ctrl+K …` as a **universal** prefix that also works while typing (insert
    mode), for actions needed mid-edit. Mirrors the existing `cmd+k cmd+…` chord
    family, but with a delivered prefix.
- **Leader bindings are data.** They live in `defaults.go` as ordinary multi-step
  `Chord`s (`space g f`, `ctrl+k s`); the resolver already handles prefix + 600ms
  timeout. The leader is configurable via `[keymap].leader` (doc references 04).
- **Which-key on hold.** When a leader prefix is held and the timeout is pending,
  show the available continuations (doc 40). The leader is both a fallback and a
  discoverability surface.

## Primary-default selection rule

For each action pick the **primary** default by reachability, keeping the
JetBrains chord as a secondary alias where it might be delivered:

| Reach of JetBrains chord | Primary default            | Secondary (alias) |
|--------------------------|----------------------------|-------------------|
| delivered                | keep JetBrains chord        | leader optional   |
| fragile                  | leader sequence (primary)   | JetBrains chord   |
| intercepted              | leader sequence (primary)   | — (documented)    |
| undetectable             | leader sequence (primary)   | — (documented)    |

## Proposed leader map (draft — finalise against doc 10/20)

| Action               | Leader (primary)   | JetBrains alias  |
|----------------------|--------------------|------------------|
| Search everywhere    | `space space`      | `cmd+shift+a`    |
| Go to file           | `space f f`        | `cmd+shift+o`    |
| Recent files         | `space f r`        | `cmd+e`          |
| Find in path         | `space s p`        | `cmd+shift+f`    |
| Save                 | `ctrl+k s`         | `cmd+s`          |
| Save all             | `ctrl+k a`         | `cmd+shift+s`    |
| Comment line         | `ctrl+k c`         | `cmd+/`          |
| Duplicate line       | `space d`          | `cmd+d`          |
| Toggle project tree  | `space e`          | `cmd+1`          |
| Switch pane focus    | `space w` / `ctrl+w`| `ctrl+tab`      |
| Commit (VCS)         | `space g c`        | `cmd+k` (blocked)|
| Keymap cheatsheet    | `space ?` / `f1`   | `cmd+k cmd+s`    |

`space space` replaces the undetectable `shift shift`; `ctrl+w`/`space w` give a
delivered pane-switch instead of fragile `ctrl+tab`.

## Approach

- Add leader bindings to `defaults.go`, normalised and conflict-checked like any
  other (0080 conflict pass must stay green after the additions).
- Re-tag each binding with its `Reach` so the cheatsheet renders primary vs alias
  and flags intercepted/undetectable JetBrains chords as "macOS only / use X".
- Make the leader configurable; default `space` (non-editor/normal) + `ctrl+k`
  (universal). Editor insert mode never steals `space`.

## Milestones

- [ ] Implement leader prefixes (`space` non-editor/normal, `ctrl+k` universal) on 0080's multi-step resolver; `[keymap].leader` config tunable.
- [ ] Add the leader map (finalised vs docs 10/20) to `defaults.go`; conflict pass stays clean.
- [ ] Re-pick primary defaults from the reachability table; keep JetBrains chords as documented secondaries.
- [ ] Ensure editor insert mode never captures the leader as a command (plain `space` types a space).
- [ ] Tests: leader sequence resolves; leader timeout falls through; `space` types in insert mode; no conflicts after additions.
