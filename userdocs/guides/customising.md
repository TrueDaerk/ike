# Customising IKE

Three things you will want to change: settings, keybindings, and the theme.
All three are TOML underneath, and all three have a UI so you rarely have to
write it by hand.

## Settings

++cmd+comma++ opens the settings panel. It is a **three-column grid**:
categories on the left, the settings of the selected page in the middle with a
marker showing how each one edits, and a detail column on the right that
explains the selected setting *and* holds its editor.

```
categories │ settings           ◉ ‹› ▸ ⌨ ≡ ✎ │ description + editor
```

++tab++ walks the three columns, ++up++/++down++ (or `j`/`k`) move inside one,
++enter++ activates. On a narrow terminal the detail column becomes a band
under the list rather than disappearing. `?` opens the full key list.

The value marker tells you what kind of editor you get before you press
anything: `◉` a toggle, `‹›` a stepper, `▸` a list of options, `⌨` a chord
capture, `≡` a multi-value list, `✎` free text. Enums and integers can also be
changed straight on the row with ++left++/++right++.

The detail column is never empty: with the categories focused it describes the
page, with a setting selected it shows the description, `key · type · default`,
the editor, and where the value currently comes from (`set in user · writes to
user`).

The panel and the file are the same thing seen two ways — nothing is
panel-only, and nothing you write by hand is invisible to the panel. The
[settings reference](../reference/settings.md) lists every key, its type,
default and allowed values.

### Changes are staged, then applied

Editing a setting does **not** write it immediately. Changes collect in a
batch: the header counts them (`● 3 changes · ctrl+s apply`), each affected
page is marked in the category rail, and the detail column shows the selected
row's `● old → new`. Editing a value back to where it started removes it from
the batch again.

++ctrl+s++ opens the diff panel — one line per change as
`page · key · old → new`:

| Key | What it does |
|---|---|
| ++enter++ | Write the batch (one write, one reload) |
| `u` | Drop the selected line |
| `s` | Retarget the whole batch at another layer |
| `d` | Discard everything |

++esc++ with pending changes opens the same diff rather than throwing your
edits away silently. `r` (reset to the default) stages a removal like any
other change.

Settings whose whole point is how they look — the theme — preview live while
staged, and the preview is undone if you discard.

Custom pages are the exception: installing a plugin, choosing an interpreter
or creating a virtualenv writes straight through, because those are actions,
not values in a file.

### Searching

`/` searches the whole panel and keeps the grid working: the left column
becomes the pages that have hits, the middle lists every match as
`Page › Title`, and the right column is still the editor — so ++enter++ sets
the value right there instead of navigating you somewhere. ++tab++ opens the
match's own page on that row, ++esc++ clears the query. The header reads
`⌕ query · 7 hits · 5 pages`.

The search reaches custom pages too: `/python` finds `Toolchain › python` and
`Toolchain › New Python environment`.

### Which file gets written

```
built-in defaults  <  ~/.ike/settings.toml  <  <project>/.ike/settings.toml
```

The **Scope** column in the settings reference tells you which layer a given
setting is written to when you change it in the panel. Most are user-scoped;
a few that are inherently project-specific are not.

Editing by hand works exactly as well:

```toml
[editor]
relative_line_numbers = true
tab_width = 2
format_on_save = true

[theme]
name = "gruvbox"
```

`$IKE_CONFIG_DIR` relocates the user layer if you keep your dotfiles
elsewhere.

!!! warning "Arrays replace, they do not merge"
    A TOML array in a later layer replaces the earlier one wholesale. A
    project-level `[[tools.custom]]` or `[[snippets]]` block hides your
    user-level ones rather than adding to them.

## Keybindings

The **Keymap** settings page lists the **effective** bindings as
`chord · command`, on the same grid as every other page.

| Key | What it does |
|---|---|
| `/` | Filter the list |
| ++enter++ | Capture a new chord for the selected row |
| `u` | Unbind |
| `r` | Reset to the shipped default |
| `i` | Import a JetBrains keymap XML |
| `z` | Unfold a folded run of numbered bindings |

The detail column carries what used to clutter the table: the command's title
and id, **every** chord bound to it with its context and layer (`@default` /
`@user`), and its conflict state — `✓ no conflicts`, or the other commands
sharing the chord plus two free chords you could take instead.

Runs of numbered bindings fold into one row — `alt+1 … alt+9 · Go to tab 1–9
▸ 9` — so nine near-identical lines do not bury the rest. `z` (or ++enter++ on
the folded row) expands it, and the detail column meanwhile lists every
binding the row stands for. Filtering never folds: you see exactly what
matched.

Capturing is literal: press the chord you want and each key press appends a
step, so multi-step chords like ++cmd+k++ ++cmd+s++ work. ++enter++ confirms,
++esc++ cancels.

If the chord is already taken, the capture dialog makes it a decision rather
than a yes/no: **Replace & unbind the other** (++enter++), **Pick a different
chord** (`p`, which clears what you pressed and keeps capturing), or cancel.
Capturing a chord that not every terminal delivers gets you a warning rather
than a silent disappointment.

The list is honest about its edges: commands whose default chord you rebound
stay visible as `(unbound)` rows so you can give them a new one, and every
command with no binding at all — palette-only commands, your tool panes — is
listed at the end so it can be bound.

### By hand

```toml
[keymap.bindings]
"cmd+7" = "editor.commentLine"
"ctrl+shift+k" = "editor.duplicateLine"
"cmd+d" = ""                          # unbind
```

The key is the chord, the value the command ID; an empty value unbinds. The
[commands reference](../reference/commands.md) lists every ID, including the
ones with no default chord.

### Importing a JetBrains keymap

Export your keymap from IntelliJ (Settings → Keymap → the gear → Export) and
run **Import JetBrains Keymap XML…** from the palette, or `i` on the Keymap
page.

IKE translates the Swing keystroke syntax into its own, maps IntelliJ action
IDs onto IKE commands, and reports what it did: how many bindings landed,
which action IDs it has no counterpart for, and which keystrokes it could not
translate. Nothing is fatal — an unmappable entry is skipped and named.

An imported chord **replaces** the default rather than joining it: defaults
for the imported commands that your export did not keep are unbound, so you
end up with your keymap, not a merge of two.

!!! note "Chords the terminal has to deliver"
    Rebinding cannot fix a chord your terminal swallows. If a new binding does
    nothing, check [Terminal setup](../getting-started/terminal-setup.md)
    before assuming the rebind failed.

### What is not in the keymap

The vim keys inside the editor — motions, operators, text objects,
ex-commands — are the editor's own and are not part of the keymap layer.
`d`, `w`, `ciw` and `:w` are not rebindable there.

## Themes

One setting recolours everything — syntax, file tree, chrome:

```toml
[theme]
name = "tokyo-night"
```

Twenty-eight themes ship with IKE: `default`, `tokyo-night`, `nord`, `gruvbox`,
`gruvbox-light`, `rose-pine`, `rose-pine-dawn`, `catppuccin-mocha`,
`catppuccin-latte`, `kanagawa`, `one-dark`, `solarized-dark`,
`solarized-light`, `dracula`, `darcula`, `intellij-light`,
`everforest-dark`, `everforest-light`, `ayu-dark`, `ayu-mirage`,
`ayu-light`, `github-dark`, `github-light`, `oxocarbon`, `monokai-pro`,
`zenburn`, `high-contrast-dark` and `high-contrast-light`. The last two are
the accessibility tier: they target WCAG AAA (7:1) everywhere and give up the
dimmed comment/hint layer entirely, for low-vision use, bright rooms and
washed-out projectors. Plugins can register more. The README shows a
[screenshot of every theme](https://github.com/TrueDaerk/ike#themes).

Switching live is easiest from the palette — type `:` and then `Theme`. The
**Appearance** settings page lists them too, and previews as you move through
the list.

Tool panes get the theme's colours in their environment, so a TUI program that
reads them follows along.

### Recolouring single syntax captures

A theme you like except for one colour does not have to be forked. The
**Syntax Colors** page (right behind Appearance) lists every syntax capture
the active theme defines — plus the six `rainbow.N` bracket slots — with a
swatch in the colour that actually resolves, the capture name and the token in
use. `(derived)` marks a slot with no token of its own.

| Key | What it does |
|---|---|
| ++enter++ | Pick from the named colours, or the capture's theme default |
| `e` | Type a colour token directly |
| `r` | Remove the override |

The detail column says what the capture paints, names the config key, and
tells you whether the colour comes from the theme or from your config.

Overrides live under `[theme.captures]` and are written at user scope:

```toml
[theme.captures]
comment = "#7f8c98"
"keyword.control" = "magenta"
"rainbow.0" = "42"
```

A token is a hex colour (`#rrggbb` or `#rgb`), a named colour, or an ANSI
palette index.

These write through immediately rather than being staged — choosing a colour
you cannot see would be pointless. An unparseable token is rejected instead of
silently rendering as your terminal default. Captures your config names that
the theme does not define stay listed, so an override can never become
unreachable.

## Plugins

WASM plugins can add commands, themes and languages. The **Plugins** settings
page lists what is installed and toggles each one; the marketplace page
installs new ones.

Disabling a plugin is written as:

```toml
[plugins.some-plugin]
enabled = false
```

A plugin's own settings, where it has any, appear as their own settings page.

## Where it all ends up

| Path | What |
|---|---|
| `~/.ike/settings.toml` | Your settings, keybindings, theme |
| `<project>/.ike/settings.toml` | Project overrides |
| `$IKE_CONFIG_DIR` | Relocates the user layer |

## Related

- [Settings reference](../reference/settings.md) — every key
- [Keybindings reference](../reference/keybindings.md) — every default binding
- [Commands reference](../reference/commands.md) — every command ID
