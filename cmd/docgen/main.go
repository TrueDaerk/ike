// docgen generates the reference section of the user documentation site
// (Epic #1182) from the source of truth in the code: the default keymap, the
// settings schema, and the command registry. The generated pages are checked
// in and CI re-runs this generator to fail on drift, so a renamed command or
// a new setting cannot silently leave the documentation stale.
//
//	go run ./cmd/docgen                 # writes userdocs/reference
//	go run ./cmd/docgen -out some/dir   # or elsewhere
//
// It follows the precedent of cmd/ike/genmatrix_test.go, which emits the
// keymap audit ledger for the wiki.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ike/internal/config"
	"ike/internal/keymap"
	"ike/internal/registry"
	"ike/internal/settings"

	// The compiled-in plugin set has to match cmd/ike: command IDs only
	// resolve against the registry the shipped binary builds, so a plugin
	// missing here would silently drop its commands from the reference.
	// internal/app pulls in the core command and theme providers; the
	// explorer, project and editor packages self-register their own.
	// plugins/example stays out, exactly as it does in the shipped binary.
	_ "ike/internal/app"
	_ "ike/internal/editor"
	_ "ike/internal/explorer"
	_ "ike/internal/project"

	_ "ike/plugins/format"
	_ "ike/plugins/lsp"

	_ "ike/plugins/languages/ansible"
	_ "ike/plugins/languages/csv"
	_ "ike/plugins/languages/dockerfile"
	_ "ike/plugins/languages/go"
	_ "ike/plugins/languages/http"
	_ "ike/plugins/languages/json"
	_ "ike/plugins/languages/make"
	_ "ike/plugins/languages/markdown"
	_ "ike/plugins/languages/php"
	_ "ike/plugins/languages/python"
	_ "ike/plugins/languages/shell"
	_ "ike/plugins/languages/sql"
	_ "ike/plugins/languages/toml"
	_ "ike/plugins/languages/web"
	_ "ike/plugins/languages/xml"
	_ "ike/plugins/languages/yaml"
)

func main() {
	out := flag.String("out", filepath.Join("userdocs", "reference"), "directory to write the reference pages into")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}

	// Install a deterministic configuration before querying the registry.
	// Command capabilities are lazy and read the live config, and one default
	// — the preconfigured lazygit tool pane — depends on what happens to be
	// on PATH. Documenting the developer's PATH would make the checked-in
	// output differ from CI's, so user-configured tool panes are dropped and
	// mentioned in prose on the commands page instead.
	cfg, _ := config.Load(config.Options{})
	cfg.Tools.Custom = nil
	config.Set(cfg)

	reg := registry.Global()
	for name, body := range map[string]string{
		"keybindings.md": keybindingsPage(),
		"settings.md":    settingsPage(reg),
		"commands.md":    commandsPage(reg),
	} {
		if err := os.WriteFile(filepath.Join(*out, name), []byte(body), 0o644); err != nil {
			fail(err)
		}
	}
}

// customPageProse documents entry-less settings pages whose backing config
// is a free-form slot table the schema cannot describe (Roadmap 0470, #1402:
// the [format.<languageID>] overrides behind the Formatters page). Keyed by
// the page title; pages not listed keep the generic interactive-editor note.
var customPageProse = map[string]string{
	"Formatters": "The **Formatters** page lists each language's reformat command, the config\n" +
		"layer supplying it and whether the binary is installed — and it edits them:\n" +
		"\n" +
		"| Key | Action |\n" +
		"| --- | --- |\n" +
		"| `enter` | edit the language's override: `command` (tab completes a path), `args`, `range_args`, `temp_file`, `install`, plus the built-in's own keys |\n" +
		"| `e` | external formatting on/off for the language (`enabled`) |\n" +
		"| `b` | built-in formatter on/off (`builtin`), where the language ships one |\n" +
		"| `r` | reset: remove the whole override, back to the plugin default |\n" +
		"| `s` | the layer writes land in: project ↔ user |\n" +
		"\n" +
		"A field left at (or cleared back to) the plugin default is **not** written, so\n" +
		"the config file only ever holds what actually differs. The same overrides can\n" +
		"be written by hand as a `[format.<languageID>]` table, mirroring\n" +
		"`[lsp.servers.*]`:\n" +
		"\n" +
		"```toml\n" +
		"[format.python]\n" +
		"command = \"ruff\"\n" +
		"args = [\"format\", \"--stdin-filename\", \"${FILE}\", \"-\"]\n" +
		"range_args = [\"format\", \"--range\", \"${START_LINE}-${END_LINE}\", \"-\"]\n" +
		"enabled = true      # false switches external formatting off for the language\n" +
		"temp_file = false   # true for tools that cannot read stdin\n" +
		"install = \"pip install ruff\"\n" +
		"```\n" +
		"\n" +
		"The command reads the buffer on stdin and prints the formatted text on\n" +
		"stdout (set `temp_file = true` for in-place-only tools; IKE then formats a\n" +
		"temp copy and reads it back — your file is never written behind the\n" +
		"editor's back). Available placeholders: `${FILE}`, `${TAB_WIDTH}`,\n" +
		"`${INDENT_STYLE}`, `${MAX_LINE_LENGTH}`, and in `range_args`\n" +
		"`${START_LINE}` / `${END_LINE}` (1-based). `range_args` opts into\n" +
		"**Reformat Selection**; without it the selection command falls back to\n" +
		"another range-capable source or tells you only whole-file reformat is\n" +
		"available.\n" +
		"\n" +
		"Languages with a built-in formatter (SQL, XML, `.http`) accept two extra\n" +
		"keys: `builtin = false` disables the built-in (falling back to the language\n" +
		"server), and for SQL `keywords = \"upper\" | \"lower\" | \"preserve\"` controls\n" +
		"keyword casing.\n" +
		"\n" +
		"#### Shipped defaults\n" +
		"\n" +
		"Without any override these are the formatters IKE reaches for. Chains try\n" +
		"each tool in order and use the first one installed; nothing is installed for\n" +
		"you — a missing binary raises the install hint once and the reformat falls\n" +
		"through to the next source.\n" +
		"\n" +
		"| Language | Default formatter | Install hint |\n" +
		"| --- | --- | --- |\n" +
		"| Python | `ruff format`, else `black` (a project `.venv/` or `venv/` install wins over PATH) | `pip install ruff` / `pip install black` |\n" +
		"| Markdown | `prettier`, else `mdformat` | `npm install -g prettier` / `pip install mdformat` |\n" +
		"| Shell | `shfmt` (indent flag and dialect derived per buffer) | `brew install shfmt` |\n" +
		"| Ansible | `prettier` (YAML parser), else `yamlfmt` | `npm install -g prettier` / `go install github.com/google/yamlfmt/cmd/yamlfmt@latest` |\n" +
		"| SQL | built-in (`keywords` casing), above the `sqls` server | — |\n" +
		"| XML | built-in pretty-printer | — |\n" +
		"| `.http` | built-in request formatter | — |\n" +
		"\n" +
		"Every other language reformats through its language server when the server\n" +
		"advertises formatting.",
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "docgen:", err)
	os.Exit(1)
}

// header is the banner every generated page carries, so a reader who lands on
// one from a search engine knows where to send a correction.
func header(title, description string) string {
	return fmt.Sprintf(`<!--
Generated by cmd/docgen. Do not edit this file by hand — your changes will be
overwritten and CI will fail on the drift. Change the source instead and run:

    go run ./cmd/docgen
-->

# %s

%s

`, title, description)
}

// ---------------------------------------------------------------- keybindings

// keybindingsPage renders the default binding table once per context, with a
// column per platform: the same logical chord is Cmd on macOS and Ctrl
// everywhere else (keymap.NormalizeChord).
func keybindingsPage() string {
	var b strings.Builder
	b.WriteString(header("Keybindings",
		`Every modifier chord IKE ships with. These are defaults — rebind anything
in the settings panel (**Settings → Keymap**) or in `+"`settings.toml`"+`.

The editor's vim keys are a separate layer and are not listed here: they are
not rebindable chords but the modal grammar itself. See
[The modal editor](../concepts/modal-editor.md) for those.

The same logical chord is written with **Cmd** on macOS and **Ctrl** on Linux
and Windows; both columns are listed.

All of these assume a terminal that speaks the Kitty keyboard protocol and
does not claim the chord for itself — that is what IKE needs to see modified
keys at all. A chord marked ⚠️ needs more than that: it relies on key-release
reporting, which fewer terminals implement. See
[Troubleshooting](../troubleshooting.md) when a chord does nothing.`))

	byContext := map[keymap.Context][]keymap.Binding{}
	for _, bd := range keymap.Defaults(keymap.PresetJetBrains) {
		byContext[bd.Context] = append(byContext[bd.Context], bd)
	}

	for _, ctx := range []struct {
		key   keymap.Context
		title string
		intro string
	}{
		{keymap.Global, "Global", "Active everywhere, unless a focused pane binds the same chord more specifically."},
		{keymap.Editor, "Editor", "Active when an editor pane has focus."},
		{keymap.Explorer, "Explorer", "Active when the file explorer has focus."},
		{keymap.Diff, "Diff viewer", "Active when a diff pane has focus."},
		{keymap.Palette, "Palette", "Active while the command palette is open."},
	} {
		rows := byContext[ctx.key]
		if len(rows) == 0 {
			continue
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Title != rows[j].Title {
				return rows[i].Title < rows[j].Title
			}
			return rows[i].Chord.String() < rows[j].Chord.String()
		})

		fmt.Fprintf(&b, "## %s\n\n%s\n\n", ctx.title, ctx.intro)
		b.WriteString("| Action | macOS | Linux / Windows | Command |\n")
		b.WriteString("|---|---|---|---|\n")
		for _, bd := range rows {
			// Fragile covers every Cmd/Alt chord, which the page-level note
			// already explains; only the undetectable ones (bare-modifier
			// taps needing key-up events) earn a per-row marker.
			warn := keymap.Classify(bd.Chord) == keymap.Undetectable
			mac := chordCell(keymap.NormalizeChord(bd.Chord, "darwin"), warn)
			other := chordCell(keymap.NormalizeChord(bd.Chord, "linux"), warn)
			fmt.Fprintf(&b, "| %s | %s | %s | `%s` |\n", bd.Title, mac, other, bd.Command)
		}
		b.WriteString("\n")
	}

	b.WriteString(`## Rebinding

Bindings live under ` + "`[keymap.bindings]`" + ` in ` + "`settings.toml`" + `, keyed by chord;
the value is the command ID, and an empty value unbinds the chord:

` + "```toml" + `
[keymap.bindings]
"cmd+7" = "editor.commentLine"
"cmd+j" = ""                      # unbind
` + "```" + `

A chord may be qualified with the pane it applies in — ` + "`global`" + `, ` + "`editor`" + `,
` + "`explorer`" + `, ` + "`palette`" + ` or ` + "`diff`" + ` — so one chord can run different
commands depending on what has focus. The qualified form only touches its own
context; the bare form applies wherever the chord is bound:

` + "```toml" + `
[keymap.bindings.editor]
"ctrl+g" = "editor.nextOccurrence"

# The same thing written as a dotted key:
[keymap.bindings]
"editor.ctrl+g" = "editor.nextOccurrence"
` + "```" + `

The keymap settings page offers this as **keep both, resolve by context** when a
captured chord collides with a command in another pane.

The [Commands reference](commands.md) lists every command ID, including the
ones with no default chord.
`)
	return b.String()
}

// chordCell renders one chord, flagging the ones that need key-release
// reporting on top of the Kitty protocol.
func chordCell(c keymap.Chord, warn bool) string {
	cell := "`" + c.String() + "`"
	if warn {
		cell += " ⚠️"
	}
	return cell
}

// ------------------------------------------------------------------- settings

// settingsPage renders the schema-driven settings pages — the same descriptors
// the in-app settings panel renders from — joined with the default values.
func settingsPage(reg *registry.Registry) string {
	var b strings.Builder
	b.WriteString(header("Settings",
		`Every setting IKE understands, in the order the settings panel shows them.

Settings are TOML and merge across three layers — built-in defaults, then
`+"`~/.ike/settings.toml`"+` (or `+"`$IKE_CONFIG_DIR/settings.toml`"+`), then
`+"`<project>/.ike/settings.toml`"+`. Later layers win, and changes are picked up
live. The **Scope** column is the layer the settings panel writes a change to.

Everything here can also be edited interactively: open the menu bar and pick
Settings, or run `+"`settings.open`"+` from the palette.`))

	flat := config.Get().Flat()

	themes := reg.Themes()
	names := make([]string, len(themes))
	for i, t := range themes {
		names[i] = t.Name
	}
	light, dark := []string{}, []string{}
	for _, t := range themes {
		if t.Dark {
			dark = append(dark, t.Name)
		} else {
			light = append(light, t.Name)
		}
	}
	pages := append(settings.BasePages(names, light, dark), reg.SettingsPages()...)

	section := ""
	for _, p := range pages {
		if p.Section != "" && p.Section != section {
			section = p.Section
			fmt.Fprintf(&b, "## %s\n\n", strings.ToUpper(section[:1])+strings.ToLower(section[1:]))
		}
		fmt.Fprintf(&b, "### %s\n\n", p.Title)
		if len(p.Entries) == 0 {
			// Custom pages (the keymap editor, the plugin manager) render
			// their own UI and have no schema entries to document; pages
			// whose backing config is a free-form table carry hand-written
			// prose from customPageProse instead.
			if prose, ok := customPageProse[p.Title]; ok {
				b.WriteString(prose + "\n\n")
			} else {
				b.WriteString("This page is an interactive editor in the settings panel rather than a\nlist of keys.\n\n")
			}
			continue
		}
		b.WriteString("| Setting | Key | Type | Default | Scope | Description |\n")
		b.WriteString("|---|---|---|---|---|---|\n")
		for _, e := range p.Entries {
			fmt.Fprintf(&b, "| %s | `%s` | %s | %s | %s | %s |\n",
				e.Title, e.Key, entryType(e), defaultValue(flat, e.Key), scopeName(e.Scope), e.Description)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// entryType names the control type, folding the enum options and the integer
// bounds into the cell — they are part of what the value may be.
func entryType(e settings.Entry) string {
	switch e.Type {
	case settings.Bool:
		return "boolean"
	case settings.Int:
		if e.Min != 0 || e.Max != 0 {
			return fmt.Sprintf("integer (%d–%d)", e.Min, e.Max)
		}
		return "integer"
	case settings.String:
		return "string"
	case settings.Enum:
		if len(e.Options) == 0 {
			return "enum"
		}
		quoted := make([]string, len(e.Options))
		for i, o := range e.Options {
			quoted[i] = "`" + o + "`"
		}
		return "enum: " + strings.Join(quoted, ", ")
	case settings.Path:
		return "path"
	case settings.Chord:
		return "key chord"
	case settings.List:
		return "list"
	case settings.IntList:
		return "list of integers"
	}
	return "unknown"
}

// defaultValue looks the key up in the flattened default config. Free-form
// slot keys (per-language, per-plugin tables) have no default row.
func defaultValue(flat map[string]string, key string) string {
	v, ok := flat[key]
	if !ok {
		return "—"
	}
	if v == "" {
		return "*(empty)*"
	}
	return "`" + v + "`"
}

func scopeName(s config.Scope) string {
	if s == config.ProjectScope {
		return "project"
	}
	return "user"
}

// ------------------------------------------------------------------- commands

// commandsPage renders the registry — everything the palette can run — joined
// with the default chord for the commands that have one.
func commandsPage(reg *registry.Registry) string {
	var b strings.Builder
	b.WriteString(header("Commands",
		`Every command registered in this build, as the command palette sees them.

Open the palette with **Search everywhere** and type `+"`:`"+` to filter commands.
The **Chord** column is the default keybinding, where one exists; see
[Keybindings](keybindings.md) for the full table and how to rebind.

**Available in** is the pane a command is scoped to — a scoped command only
shows up, and only runs, while that pane has focus.

**In the editor** lists the vim key or ex-command that does the same thing.
Those are handled by the editor directly rather than through the keymap layer,
so they work regardless of what the chord table says.

Your own configuration adds to this list: every tool pane you define under
`+"`[[tools.custom]]`"+` gets a `+"`tool.<name>`"+` command, and plugins you install
bring their own.`))

	chords := map[string]string{}
	for _, bd := range keymap.Defaults(keymap.PresetJetBrains) {
		// Several chords can share a command (a JetBrains primary plus a
		// deliverable fallback); the shortest is the one worth advertising.
		if cur, ok := chords[bd.Command]; !ok || len(bd.Chord.String()) < len(cur) {
			chords[bd.Command] = bd.Chord.String()
		}
	}

	cmds := reg.Commands()
	byOwner := map[string][]registry.OwnedCommand{}
	owners := []string{}
	for _, c := range cmds {
		if _, seen := byOwner[c.Owner]; !seen {
			owners = append(owners, c.Owner)
		}
		byOwner[c.Owner] = append(byOwner[c.Owner], c)
	}
	sort.Strings(owners)

	for _, owner := range owners {
		fmt.Fprintf(&b, "## %s\n\n", owner)
		b.WriteString("| Command | ID | Chord | In the editor | Available in |\n")
		b.WriteString("|---|---|---|---|---|\n")
		for _, c := range byOwner[owner] {
			chord := "—"
			if ch, ok := chords[c.ID]; ok {
				chord = "`" + ch + "`"
			}
			vim := "—"
			if c.Shortcut != "" {
				vim = "`" + c.Shortcut + "`"
			}
			scope := "everywhere"
			if !c.Scope.Global && c.Scope.ContextID != "" {
				scope = "`" + c.Scope.ContextID + "` pane"
			}
			fmt.Fprintf(&b, "| %s | `%s` | %s | %s | %s |\n", c.Title, c.ID, chord, vim, scope)
		}
		b.WriteString("\n")
	}
	return b.String()
}
