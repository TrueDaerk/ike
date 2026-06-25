# Roadmap 0085 — Bubble Tea v2 Upgrade (Kitty Keyboard Protocol)

Move the whole charm stack from v1 to v2: `bubbletea v1.3.4 → charm.land/bubbletea/v2
v2.0.7`, `lipgloss v1.0.0 → charm.land/lipgloss/v2 v2.0.4`, `bubbles v0.20.0 →
charm.land/bubbles/v2 v2.1.0`.

The driver is the **kitty keyboard protocol**: v2's progressive keyboard enhancements
unlock disambiguated chords that are impossible on v1 (ctrl+i vs tab, ctrl+m vs enter,
shift+enter, plus key repeat/release reporting). The upgrade also brings IKE back to
current on a stack that had already moved its canonical module path to `charm.land/*`.

## What v2 changed (and how IKE absorbs it)

- **Module path.** v2 modules declare themselves under `charm.land/*`, not
  `github.com/charmbracelet/*`. The github path no longer resolves for current v2 tags,
  so all imports use `charm.land/...`.
- **Keys.** `tea.KeyMsg` (a struct with `Type`/`Runes`/`Alt`) became the `tea.KeyMsg`
  *interface* plus concrete `tea.KeyPressMsg`/`tea.KeyReleaseMsg` (both aliases of
  `tea.Key{Text, Mod, Code, ShiftedCode, BaseCode, IsRepeat}`). `key.Type`/`key.Runes`
  are gone; named keys switch on `key.Code` (`tea.KeyEscape`, `tea.KeyEnter`, …) and
  printable input reads `key.Text` (a bare space arrives as `Text == " "`). Modifiers
  live in `key.Mod` (`tea.ModCtrl`/`ModAlt`/`ModShift`); there are no `tea.KeyCtrlH`
  style constants. `String()` still yields `ctrl+/alt+/shift+` tokens and names specials
  (`esc`, `space`, `f7`, …), which is what the in-house keymap relies on.
- **Mouse.** `tea.MouseMsg` split into `tea.MouseClickMsg`/`MouseReleaseMsg`/
  `MouseWheelMsg`/`MouseMotionMsg`, each carrying `tea.Mouse{X, Y, Button, Mod}` via
  `.Mouse()`. Buttons renamed (`MouseButtonLeft → MouseLeft`, `MouseButtonWheelUp →
  MouseWheelUp`, …); shift is now `Mod & ModShift`, not a `.Shift` field.
- **View / program options.** `Model.View()` returns a `tea.View` struct, not a string.
  Alt-screen, mouse mode and keyboard enhancements are **declared on that View**, not
  passed as `tea.NewProgram` options — `tea.WithAltScreen`/`tea.WithMouseCellMotion` are
  gone.
- **lipgloss is pure.** v2 `Style.Render` always emits ANSI escapes; terminal
  downgrading happens in bubbletea's renderer at output time. Tests that string-match
  rendered output must `ansi.Strip` first. `lipgloss.Color` is now a function returning
  `color.Color` (no longer a string type).
- **bubbles/viewport.** `viewport.New(opts...)` with `WithWidth/WithHeight`;
  `Width`/`Height`/`YOffset` are now methods.

## Design

- **One atomic change.** The import-path bump breaks every importer at once and
  key/mouse/View break in lockstep, so there is no green intermediate state — the
  migration lands as a single sequenced pass, validated package by package.
- **Key model is funnelled.** The global keymap is in-house and reads keys only through
  `internal/keymap/fromkeymsg.go` via `String()`, so that one bridge insulates ~30 files.
  Only the editor's insert/command/normal/visual handlers read key fields directly and
  were rewritten to `Code`/`Text`/`Mod`.
- **Mouse stays one handler.** The four v2 mouse messages are normalised at the `Update`
  boundary into an internal `mouseEvent{tea.Mouse, action}` so the drag state machine
  (`internal/app/app.go handleMouse`) keeps its press→motion→release shape unchanged.
- **View wraps render.** Only the top-level `app.Model.View()` is the bubbletea
  entrypoint; it now returns `tea.View` wrapping the existing string renderer (renamed
  `render()`), and declares `AltScreen`, `MouseMode = MouseModeCellMotion`, and
  `KeyboardEnhancements.ReportEventTypes = true`. The seven internal `View() string`
  composition methods are untouched. `Update` dispatches only `KeyPressMsg`, so
  `KeyReleaseMsg` events from the kitty protocol are ignored (no double processing).

## Milestones

- [x] Bump deps to `charm.land/*` v2 and rewrite all import paths.
- [x] Migrate the keymap bridge (`fromkeymsg.go`) and keep `internal/keymap` tests green.
- [x] Rewrite the editor key readers (insert/command/normal/visual) to `Code`/`Text`/`Mod`.
- [x] Split mouse handling into the four v2 messages via a normalised `mouseEvent`.
- [x] Convert `app.Model.View()` to `tea.View`; move alt-screen/mouse off program options.
- [x] Enable the kitty keyboard protocol (keyboard enhancements on the View) and ignore key releases.
- [x] Migrate `lipgloss/v2` (incl. `color.Color`) and `bubbles/v2` viewport.
- [x] Rewrite test message constructors; `ansi.Strip` rendered-output assertions.
- [x] `go build ./... && go test ./...` green; wiki + roadmap updated.

## Verification

- `go build ./... && go vet ./... && go test ./...` all pass.
- Manual smoke (`go run ./cmd/ike`): alt-screen enters, pane drag/resize/move and the
  scroll viewport work, the editor and palette take input. In a kitty/ghostty terminal
  the app receives a `KeyboardEnhancementsMsg` and can distinguish previously-ambiguous
  chords (e.g. ctrl+i from tab) with no double-typed characters.
