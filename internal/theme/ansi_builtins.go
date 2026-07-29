package theme

// ansi_builtins.go carries the terminal palettes (#1363) of the built-in
// themes whose upstream project publishes one. They are applied by Builtins()
// so the theme definitions in builtins.go stay focused on the IDE's own
// chrome. A theme with no entry here — and every third-party theme — derives
// its palette from its own semantic colors (deriveANSI), which is why this
// table is allowed to be partial.

// ansiSet spells a palette in the standard order: black, red, green, yellow,
// blue, magenta, cyan, white, then the eight bright variants.
func ansiSet(c ...string) Terminal {
	var t Terminal
	copy(t.ANSI[:], c)
	return t
}

// builtinTerminals maps a built-in theme's name to its terminal palette.
var builtinTerminals = map[string]Terminal{
	// IKE's own colors: the bright half is the named-color table from
	// resolve.go, the normal half the same hues one step darker.
	DefaultName: ansiSet(
		"#303030", "#d75f5f", "#5faf5f", "#d7af5f", "#5f87d7", "#af87d7", "#5fafaf", "#b2b2b2",
		"#585858", "#ff5555", "#5fd75f", "#ffd75f", "#5fafff", "#d787ff", "#5fd7d7", "#e4e4e4",
	),
	"tokyo-night": ansiSet(
		"#15161e", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#a9b1d6",
		"#414868", "#ff899d", "#9fe044", "#faba4a", "#8db0ff", "#c7a9ff", "#a4daff", "#c0caf5",
	),
	"nord": ansiSet(
		"#3b4252", "#bf616a", "#a3be8c", "#ebcb8b", "#81a1c1", "#b48ead", "#88c0d0", "#e5e9f0",
		"#4c566a", "#d08770", "#a3be8c", "#ebcb8b", "#81a1c1", "#b48ead", "#8fbcbb", "#eceff4",
	),
	"gruvbox": ansiSet(
		"#282828", "#cc241d", "#98971a", "#d79921", "#458588", "#b16286", "#689d6a", "#a89984",
		"#928374", "#fb4934", "#b8bb26", "#fabd2f", "#83a598", "#d3869b", "#8ec07c", "#ebdbb2",
	),
	"gruvbox-light": ansiSet(
		"#3c3836", "#cc241d", "#98971a", "#d79921", "#458588", "#b16286", "#689d6a", "#7c6f64",
		"#928374", "#9d0006", "#79740e", "#b57614", "#076678", "#8f3f71", "#427b58", "#3c3836",
	),
	"dracula": ansiSet(
		"#21222c", "#ff5555", "#50fa7b", "#f1fa8c", "#bd93f9", "#ff79c6", "#8be9fd", "#f8f8f2",
		"#6272a4", "#ff6e6e", "#69ff94", "#ffffa5", "#d6acff", "#ff92df", "#a4ffff", "#ffffff",
	),
	"solarized-dark": ansiSet(
		"#073642", "#dc322f", "#859900", "#b58900", "#268bd2", "#d33682", "#2aa198", "#eee8d5",
		"#586e75", "#f5534f", "#a5bf00", "#d9a800", "#4aa5e8", "#e85a9c", "#35c4b8", "#fdf6e3",
	),
	"solarized-light": ansiSet(
		"#073642", "#dc322f", "#859900", "#b58900", "#268bd2", "#d33682", "#2aa198", "#eee8d5",
		"#586e75", "#a52a28", "#5f6d00", "#7f6000", "#1c6699", "#992760", "#1d7069", "#93a1a1",
	),
	"catppuccin-mocha": ansiSet(
		"#45475a", "#f38ba8", "#a6e3a1", "#f9e2af", "#89b4fa", "#f5c2e7", "#94e2d5", "#bac2de",
		"#585b70", "#f5a0b7", "#b6e8b1", "#fae9c1", "#a0c2fb", "#f7cfeb", "#a7e8dd", "#a6adc8",
	),
	"catppuccin-latte": ansiSet(
		"#5c5f77", "#d20f39", "#40a02b", "#df8e1d", "#1e66f5", "#ea76cb", "#179299", "#acb0be",
		"#6c6f85", "#b00d30", "#358623", "#b47718", "#1957cf", "#c1629f", "#137a80", "#bcc0cc",
	),
	"one-dark": ansiSet(
		"#3f4451", "#e06c75", "#98c379", "#e5c07b", "#61afef", "#c678dd", "#56b6c2", "#abb2bf",
		"#5c6370", "#ef8b93", "#b0d495", "#eed09b", "#85c3f3", "#d597e7", "#7ecad3", "#ffffff",
	),
	"github-dark": ansiSet(
		"#484f58", "#ff7b72", "#3fb950", "#d29922", "#58a6ff", "#bc8cff", "#39c5cf", "#b1bac4",
		"#6e7681", "#ffa198", "#56d364", "#e3b341", "#79c0ff", "#d2a8ff", "#56d4dd", "#f0f6fc",
	),
	"github-light": ansiSet(
		"#24292f", "#cf222e", "#116329", "#4d2d00", "#0969da", "#8250df", "#1b7c83", "#6e7781",
		"#57606a", "#a40e26", "#1a7f37", "#633c01", "#0757ba", "#6639ba", "#166b72", "#8c959f",
	),
}
