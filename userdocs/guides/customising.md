# Customising IKE

Three things you will want to change: settings, keybindings, and the theme.
All three are TOML underneath, and all three have a UI so you rarely have to
write it by hand.

## Settings

++cmd+comma++ opens the settings panel: categories on the left, a form on the
right, a one-line description for whatever is selected. Every control writes
straight through to a file and the change applies live.

The panel and the file are the same thing seen two ways — nothing is
panel-only, and nothing you write by hand is invisible to the panel. The
[settings reference](../reference/settings.md) lists every key, its type,
default and allowed values.

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

The **Keymap** settings page lists the **effective** bindings — chord,
command, context, and whether it comes from the defaults (`@default`) or from
you (`@user`).

| Key | What it does |
|---|---|
| `/` | Filter the list |
| ++enter++ | Capture a new chord for the selected row |
| `u` | Unbind |
| `r` | Reset to the shipped default |
| `i` | Import a JetBrains keymap XML |

Capturing is literal: press the chord you want and each key press appends a
step, so multi-step chords like ++cmd+k++ ++cmd+s++ work. ++enter++ confirms,
++esc++ cancels.

If the chord is already taken, IKE names the command holding it and waits —
++enter++ overrides it, anything else cancels. Capturing a chord that not
every terminal delivers gets you a warning rather than a silent
disappointment.

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

Twenty-six themes ship with IKE: `default`, `tokyo-night`, `nord`, `gruvbox`,
`gruvbox-light`, `rose-pine`, `rose-pine-dawn`, `catppuccin-mocha`,
`catppuccin-latte`, `kanagawa`, `one-dark`, `solarized-dark`,
`solarized-light`, `dracula`, `darcula`, `intellij-light`,
`everforest-dark`, `everforest-light`, `ayu-dark`, `ayu-mirage`,
`ayu-light`, `github-dark`, `github-light`, `oxocarbon`, `monokai-pro`
and `zenburn`. Plugins can register more. The README shows a
[screenshot of every theme](https://github.com/TrueDaerk/ike#themes).

Switching live is easiest from the palette — type `:` and then `Theme`. The
**Appearance** settings page lists them too, and previews as you move through
the list.

Tool panes get the theme's colours in their environment, so a TUI program that
reads them follows along.

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
