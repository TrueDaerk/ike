package theme

// Builtins returns the built-in themes, ported from the proven sqlit / Textual
// palette set. Each supplies all three color groups (ui + captures + files).
// "default" reproduces IKE's historical colors exactly.
func Builtins() []Theme {
	return []Theme{
		defaultTheme(),
		tokyoNight(),
		nord(),
		gruvbox(),
		gruvboxLight(),
		rosePine(),
		rosePineDawn(),
		catppuccinMocha(),
		catppuccinLatte(),
		kanagawa(),
		oneDark(),
		solarizedDark(),
		solarizedLight(),
		dracula(),
	}
}

// Default is the fallback theme: today's colors (the former defaultCaptures,
// defaultColors, and chrome literals) re-expressed as one palette. Visually
// identical to pre-theme IKE.
func Default() Theme { return defaultTheme() }

// DefaultName is the theme selected when [theme].name is empty or unknown.
const DefaultName = "default"

func defaultTheme() Theme {
	return Theme{
		Name: DefaultName,
		Dark: true,
		UI: UI{
			Background:     "#121212", // former app.appBackground
			Foreground:     "#d0d0d0", // former app.appForeground
			Surface:        "#121212",
			Panel:          "#303030", // status bar / popups / hover rows
			Border:         "#585858", // blurred borders, dividers
			BorderFocus:    "#5f87ff", // former colorPaneFocus
			Selection:      "#3668ff",
			SelectionText:  "#ffffff",
			SelectionMuted: "#444444", // editor visual selection
			// Occurrence marks (#172) sit below the visual selection in
			// emphasis: read cool, write warm.
			OccurrenceRead:  "#31404f",
			OccurrenceWrite: "#4f4031",
			InlayHint:       "gray",
			Whitespace:      "#585858",
			IndentGuide:     "#585858",
			Ruler:           "#303030",
			Accent:          "#d7af87", // explorer active entry
			Primary:         "#005f87", // completion selected row
			Secondary:       "#ffaf5f",
			Success:         "#5fd75f",
			Warning:         "#9f9f00",
			Error:           "#ff6464",
			Info:            "#8d8dff",
			Hint:            "#00a9a9",
			MoveSource:      "#ff5f5f",
			DropTarget:      "#ffd700",
			Ghost:           "#af8700",
			ScrollbarTrack:  "#585858",
			ScrollbarThumb:  "#8a8a8a",
		},
		Captures: map[string]string{
			"keyword":          "magenta",
			"operator":         "white",
			"string":           "green",
			"number":           "orange",
			"comment":          "gray",
			"function":         "blue",
			"type":             "cyan",
			"constant":         "orange",
			"constant.builtin": "orange",
			"variable":         "white",
			"variable.builtin": "red",
			"property":         "white",
			"label":            "magenta",
			"attribute":        "yellow",
			"punctuation":      "gray",
			"escape":           "orange",
			"boolean":          "orange",
			"tag":              "red",
			"embedded":         "white",
		},
		Files: map[string]string{
			"dir":     "blue",
			"default": "white",
			"go":      "cyan",
			"md":      "green",
			"toml":    "yellow",
			"json":    "yellow",
			"yaml":    "yellow",
			"lock":    "gray",
		},
	}
}

func tokyoNight() Theme {
	return Theme{
		Name: "tokyo-night",
		Dark: true,
		UI: UI{
			Background:      "#16161e",
			Foreground:      "#a9b1d6",
			Surface:         "#1a1b26",
			Panel:           "#24283b",
			Border:          "#414868",
			BorderFocus:     "#7aa2f7",
			Selection:       "#7aa2f7",
			SelectionText:   "#1a1b26",
			SelectionMuted:  "#283457",
			OccurrenceRead:  "#233450",
			OccurrenceWrite: "#413630",
			InlayHint:       "#565f89",
			Whitespace:      "#414868",
			IndentGuide:     "#414868",
			Ruler:           "#24283b",
			Accent:          "#7fa1de",
			Primary:         "#7aa2f7",
			Secondary:       "#ff9e64",
			Success:         "#9ece6a",
			Warning:         "#e0af68",
			Error:           "#f7768e",
			Info:            "#7aa2f7",
			Hint:            "#1abc9c",
			MoveSource:      "#f7768e",
			DropTarget:      "#e0af68",
			Ghost:           "#ff9e64",
			ScrollbarTrack:  "#24283b",
			ScrollbarThumb:  "#414868",
		},
		Captures: map[string]string{
			"keyword":          "#bb9af7",
			"operator":         "#89ddff",
			"string":           "#9ece6a",
			"number":           "#ff9e64",
			"comment":          "#565f89",
			"function":         "#7aa2f7",
			"type":             "#2ac3de",
			"constant":         "#ff9e64",
			"constant.builtin": "#ff9e64",
			"variable":         "#c0caf5",
			"variable.builtin": "#f7768e",
			"property":         "#73daca",
			"label":            "#bb9af7",
			"attribute":        "#e0af68",
			"punctuation":      "#89ddff",
			"escape":           "#ff9e64",
			"boolean":          "#ff9e64",
			"tag":              "#f7768e",
			"embedded":         "#a9b1d6",
		},
		Files: map[string]string{
			"dir":     "#7aa2f7",
			"default": "#a9b1d6",
			"go":      "#7dcfff",
			"md":      "#9ece6a",
			"toml":    "#e0af68",
			"json":    "#e0af68",
			"yaml":    "#e0af68",
			"lock":    "#565f89",
		},
	}
}

func nord() Theme {
	return Theme{
		Name: "nord",
		Dark: true,
		UI: UI{
			Background:      "#2e3440",
			Foreground:      "#d8dee9",
			Surface:         "#2e3440",
			Panel:           "#3b4252",
			Border:          "#4c566a",
			BorderFocus:     "#88c0d0",
			Selection:       "#88c0d0",
			SelectionText:   "#2e3440",
			SelectionMuted:  "#434c5e",
			OccurrenceRead:  "#3b4657",
			OccurrenceWrite: "#524a43",
			InlayHint:       "#616e88",
			Whitespace:      "#4c566a",
			IndentGuide:     "#4c566a",
			Ruler:           "#3b4252",
			Accent:          "#8fbcbb",
			Primary:         "#88c0d0",
			Secondary:       "#dba291",
			Success:         "#a3be8c",
			Warning:         "#ebcb8b",
			Error:           "#d9a1a6",
			Info:            "#97b2cc",
			Hint:            "#8fbcbb",
			MoveSource:      "#bf616a",
			DropTarget:      "#ebcb8b",
			Ghost:           "#d08770",
			ScrollbarTrack:  "#3b4252",
			ScrollbarThumb:  "#4c566a",
		},
		Captures: map[string]string{
			"keyword":          "#81a1c1",
			"operator":         "#81a1c1",
			"string":           "#a3be8c",
			"number":           "#b48ead",
			"comment":          "#616e88",
			"function":         "#88c0d0",
			"type":             "#8fbcbb",
			"constant":         "#d08770",
			"constant.builtin": "#d08770",
			"variable":         "#d8dee9",
			"variable.builtin": "#bf616a",
			"property":         "#d8dee9",
			"label":            "#81a1c1",
			"attribute":        "#ebcb8b",
			"punctuation":      "#eceff4",
			"escape":           "#ebcb8b",
			"boolean":          "#81a1c1",
			"tag":              "#bf616a",
			"embedded":         "#d8dee9",
		},
		Files: map[string]string{
			"dir":     "#81a1c1",
			"default": "#d8dee9",
			"go":      "#88c0d0",
			"md":      "#a3be8c",
			"toml":    "#ebcb8b",
			"json":    "#ebcb8b",
			"yaml":    "#ebcb8b",
			"lock":    "#616e88",
		},
	}
}

func gruvbox() Theme {
	return Theme{
		Name: "gruvbox",
		Dark: true,
		UI: UI{
			Background:      "#1d2021",
			Foreground:      "#ebdbb2",
			Surface:         "#282828",
			Panel:           "#3c3836",
			Border:          "#504945",
			BorderFocus:     "#fabd2f",
			Selection:       "#076678",
			SelectionText:   "#ebdbb2",
			SelectionMuted:  "#504945",
			OccurrenceRead:  "#324547",
			OccurrenceWrite: "#503b2c",
			InlayHint:       "#928374",
			Whitespace:      "#504945",
			IndentGuide:     "#504945",
			Ruler:           "#3c3836",
			Accent:          "#fe8019",
			Primary:         "#076678",
			Secondary:       "#fe8019",
			Success:         "#b8bb26",
			Warning:         "#fabd2f",
			Error:           "#fc7d6e",
			Info:            "#8aaa9e",
			Hint:            "#8ec07c",
			MoveSource:      "#fb4934",
			DropTarget:      "#fabd2f",
			Ghost:           "#fe8019",
			ScrollbarTrack:  "#3c3836",
			ScrollbarThumb:  "#665c54",
		},
		Captures: gruvboxCaptures(false),
		Files: map[string]string{
			"dir":     "#83a598",
			"default": "#ebdbb2",
			"go":      "#8ec07c",
			"md":      "#b8bb26",
			"toml":    "#fabd2f",
			"json":    "#fabd2f",
			"yaml":    "#fabd2f",
			"lock":    "#928374",
		},
	}
}

func gruvboxLight() Theme {
	return Theme{
		Name: "gruvbox-light",
		Dark: false,
		UI: UI{
			Background:      "#f9f5d7",
			Foreground:      "#3c3836",
			Surface:         "#fbf1c7",
			Panel:           "#ebdbb2",
			Border:          "#d5c4a1",
			BorderFocus:     "#d79921",
			Selection:       "#3d7679",
			SelectionText:   "#fbf1c7",
			SelectionMuted:  "#d5c4a1",
			OccurrenceRead:  "#d0dce2",
			OccurrenceWrite: "#ecd9b5",
			InlayHint:       "#7c6f64",
			Whitespace:      "#d5c4a1",
			IndentGuide:     "#d5c4a1",
			Ruler:           "#ebdbb2",
			Accent:          "#9f450a",
			Primary:         "#3d7679",
			Secondary:       "#9f450a",
			Success:         "#717013",
			Warning:         "#7c5813",
			Error:           "#ba211a",
			Info:            "#36676a",
			Hint:            "#446845",
			MoveSource:      "#cc241d",
			DropTarget:      "#d79921",
			Ghost:           "#d65d0e",
			ScrollbarTrack:  "#ebdbb2",
			ScrollbarThumb:  "#bdae93",
		},
		Captures: gruvboxCaptures(true),
		Files: map[string]string{
			"dir":     "#458588",
			"default": "#3c3836",
			"go":      "#689d6a",
			"md":      "#98971a",
			"toml":    "#d79921",
			"json":    "#d79921",
			"yaml":    "#d79921",
			"lock":    "#7c6f64",
		},
	}
}

// gruvboxCaptures builds the capture table for gruvbox; the light variant
// swaps in the darker accent shades so contrast holds on the light background.
func gruvboxCaptures(light bool) map[string]string {
	c := map[string]string{
		"keyword":          "#fb4934",
		"operator":         "#ebdbb2",
		"string":           "#b8bb26",
		"number":           "#d3869b",
		"comment":          "#928374",
		"function":         "#b8bb26",
		"type":             "#fabd2f",
		"constant":         "#d3869b",
		"constant.builtin": "#d3869b",
		"variable":         "#ebdbb2",
		"variable.builtin": "#fe8019",
		"property":         "#83a598",
		"label":            "#fb4934",
		"attribute":        "#fabd2f",
		"punctuation":      "#a89984",
		"escape":           "#fe8019",
		"boolean":          "#d3869b",
		"tag":              "#fb4934",
		"embedded":         "#ebdbb2",
	}
	if light {
		for k, v := range map[string]string{
			"keyword": "#cc241d", "operator": "#3c3836", "string": "#98971a",
			"number": "#b16286", "comment": "#7c6f64", "function": "#98971a",
			"type": "#d79921", "constant": "#b16286", "constant.builtin": "#b16286",
			"variable": "#3c3836", "variable.builtin": "#d65d0e", "property": "#458588",
			"label": "#cc241d", "attribute": "#d79921", "punctuation": "#665c54",
			"escape": "#d65d0e", "boolean": "#b16286", "tag": "#cc241d",
			"embedded": "#3c3836",
		} {
			c[k] = v
		}
	}
	return c
}

func rosePine() Theme {
	return Theme{
		Name: "rose-pine",
		Dark: true,
		UI: UI{
			Background:      "#191724",
			Foreground:      "#e0def4",
			Surface:         "#191724",
			Panel:           "#26233a",
			Border:          "#403d52",
			BorderFocus:     "#c4a7e7",
			Selection:       "#c4a7e7",
			SelectionText:   "#191724",
			SelectionMuted:  "#403d52",
			OccurrenceRead:  "#2a3549",
			OccurrenceWrite: "#4a3844",
			InlayHint:       "#6e6a86",
			Whitespace:      "#403d52",
			IndentGuide:     "#403d52",
			Ruler:           "#26233a",
			Accent:          "#ebbcba",
			Primary:         "#9ccfd8",
			Secondary:       "#f6c177",
			Success:         "#9ccfd8",
			Warning:         "#f6c177",
			Error:           "#eb6f92",
			Info:            "#4097bb",
			Hint:            "#9ccfd8",
			MoveSource:      "#eb6f92",
			DropTarget:      "#f6c177",
			Ghost:           "#ebbcba",
			ScrollbarTrack:  "#26233a",
			ScrollbarThumb:  "#524f67",
		},
		Captures: map[string]string{
			"keyword":          "#31748f",
			"operator":         "#908caa",
			"string":           "#f6c177",
			"number":           "#ebbcba",
			"comment":          "#6e6a86",
			"function":         "#ebbcba",
			"type":             "#9ccfd8",
			"constant":         "#ebbcba",
			"constant.builtin": "#ebbcba",
			"variable":         "#e0def4",
			"variable.builtin": "#eb6f92",
			"property":         "#c4a7e7",
			"label":            "#31748f",
			"attribute":        "#c4a7e7",
			"punctuation":      "#908caa",
			"escape":           "#eb6f92",
			"boolean":          "#ebbcba",
			"tag":              "#eb6f92",
			"embedded":         "#e0def4",
		},
		Files: map[string]string{
			"dir":     "#31748f",
			"default": "#e0def4",
			"go":      "#9ccfd8",
			"md":      "#f6c177",
			"toml":    "#c4a7e7",
			"json":    "#c4a7e7",
			"yaml":    "#c4a7e7",
			"lock":    "#6e6a86",
		},
	}
}

func rosePineDawn() Theme {
	return Theme{
		Name: "rose-pine-dawn",
		Dark: false,
		UI: UI{
			Background:      "#faf4ed",
			Foreground:      "#575279",
			Surface:         "#faf4ed",
			Panel:           "#f2e9e1",
			Border:          "#dfdad9",
			BorderFocus:     "#907aa9",
			Selection:       "#7e649b",
			SelectionText:   "#faf4ed",
			SelectionMuted:  "#dfdad9",
			OccurrenceRead:  "#dee7ea",
			OccurrenceWrite: "#f3ddd0",
			InlayHint:       "#9893a5",
			Whitespace:      "#dfdad9",
			IndentGuide:     "#dfdad9",
			Ruler:           "#f2e9e1",
			Accent:          "#b83f39",
			Primary:         "#286983",
			Secondary:       "#945c0f",
			Success:         "#416f77",
			Warning:         "#945c0f",
			Error:           "#a34e66",
			Info:            "#286983",
			Hint:            "#416f77",
			MoveSource:      "#b4637a",
			DropTarget:      "#ea9d34",
			Ghost:           "#d7827e",
			ScrollbarTrack:  "#f2e9e1",
			ScrollbarThumb:  "#9893a5",
		},
		Captures: map[string]string{
			"keyword":          "#286983",
			"operator":         "#797593",
			"string":           "#ea9d34",
			"number":           "#d7827e",
			"comment":          "#9893a5",
			"function":         "#d7827e",
			"type":             "#56949f",
			"constant":         "#d7827e",
			"constant.builtin": "#d7827e",
			"variable":         "#575279",
			"variable.builtin": "#b4637a",
			"property":         "#907aa9",
			"label":            "#286983",
			"attribute":        "#907aa9",
			"punctuation":      "#797593",
			"escape":           "#b4637a",
			"boolean":          "#d7827e",
			"tag":              "#b4637a",
			"embedded":         "#575279",
		},
		Files: map[string]string{
			"dir":     "#286983",
			"default": "#575279",
			"go":      "#56949f",
			"md":      "#ea9d34",
			"toml":    "#907aa9",
			"json":    "#907aa9",
			"yaml":    "#907aa9",
			"lock":    "#9893a5",
		},
	}
}

func catppuccinMocha() Theme {
	return Theme{
		Name: "catppuccin-mocha",
		Dark: true,
		UI: UI{
			Background:      "#181825",
			Foreground:      "#cdd6f4",
			Surface:         "#1e1e2e",
			Panel:           "#313244",
			Border:          "#45475a",
			BorderFocus:     "#b4befe",
			Selection:       "#b4befe",
			SelectionText:   "#1e1e2e",
			SelectionMuted:  "#45475a",
			OccurrenceRead:  "#2a3045",
			OccurrenceWrite: "#463830",
			InlayHint:       "#6c7086",
			Whitespace:      "#45475a",
			IndentGuide:     "#45475a",
			Ruler:           "#313244",
			Accent:          "#f5c2e7",
			Primary:         "#89b4fa",
			Secondary:       "#fab387",
			Success:         "#a6e3a1",
			Warning:         "#f9e2af",
			Error:           "#f38ba8",
			Info:            "#89dceb",
			Hint:            "#94e2d5",
			MoveSource:      "#f38ba8",
			DropTarget:      "#f9e2af",
			Ghost:           "#fab387",
			ScrollbarTrack:  "#313244",
			ScrollbarThumb:  "#585b70",
		},
		Captures: map[string]string{
			"keyword":          "#cba6f7",
			"operator":         "#89dceb",
			"string":           "#a6e3a1",
			"number":           "#fab387",
			"comment":          "#6c7086",
			"function":         "#89b4fa",
			"type":             "#f9e2af",
			"constant":         "#fab387",
			"constant.builtin": "#fab387",
			"variable":         "#cdd6f4",
			"variable.builtin": "#f38ba8",
			"property":         "#b4befe",
			"label":            "#cba6f7",
			"attribute":        "#f9e2af",
			"punctuation":      "#9399b2",
			"escape":           "#f5c2e7",
			"boolean":          "#fab387",
			"tag":              "#f38ba8",
			"embedded":         "#cdd6f4",
		},
		Files: map[string]string{
			"dir":     "#89b4fa",
			"default": "#cdd6f4",
			"go":      "#94e2d5",
			"md":      "#a6e3a1",
			"toml":    "#f9e2af",
			"json":    "#f9e2af",
			"yaml":    "#f9e2af",
			"lock":    "#6c7086",
		},
	}
}

// kanagawa ports the "wave" variant of rebelot/kanagawa.nvim. Diagnostic
// slots swap the scheme's darkest reds/blues (samuraiRed, dragonBlue,
// waveAqua1) for their lighter siblings so every pair clears AA contrast
// on Surface and Panel.
func kanagawa() Theme {
	return Theme{
		Name: "kanagawa",
		Dark: true,
		UI: UI{
			Background:      "#1f1f28", // sumiInk3
			Foreground:      "#dcd7ba", // fujiWhite
			Surface:         "#1f1f28",
			Panel:           "#2a2a37", // sumiInk4
			Border:          "#54546d", // sumiInk6
			BorderFocus:     "#7e9cd8", // crystalBlue
			Selection:       "#2d4f67", // waveBlue2
			SelectionText:   "#dcd7ba",
			SelectionMuted:  "#223249", // waveBlue1
			OccurrenceRead:  "#25354d",
			OccurrenceWrite: "#49443c",
			InlayHint:       "#727169",
			Whitespace:      "#54546d",
			IndentGuide:     "#54546d",
			Ruler:           "#2a2a37",
			Accent:          "#e6c384", // carpYellow
			Primary:         "#2d4f67", // waveBlue2 (pmenu selection)
			Secondary:       "#ffa066", // surimiOrange
			Success:         "#98bb6c", // springGreen
			Warning:         "#ff9e3b", // roninYellow
			Error:           "#ff5d62", // peachRed
			Info:            "#7fb4ca", // springBlue
			Hint:            "#7aa89f", // waveAqua2
			MoveSource:      "#e46876", // waveRed
			DropTarget:      "#ff9e3b",
			Ghost:           "#ffa066",
			ScrollbarTrack:  "#2a2a37",
			ScrollbarThumb:  "#54546d",
		},
		Captures: map[string]string{
			"keyword":          "#957fb8", // oniViolet
			"operator":         "#c0a36e", // boatYellow2
			"string":           "#98bb6c", // springGreen
			"number":           "#d27e99", // sakuraPink
			"comment":          "#727169", // fujiGray
			"function":         "#7e9cd8", // crystalBlue
			"type":             "#7aa89f", // waveAqua2
			"constant":         "#ffa066", // surimiOrange
			"constant.builtin": "#ffa066",
			"variable":         "#dcd7ba", // fujiWhite
			"variable.builtin": "#e46876", // waveRed
			"property":         "#e6c384", // carpYellow
			"label":            "#957fb8",
			"attribute":        "#e6c384",
			"punctuation":      "#9cabca", // springViolet2
			"escape":           "#7fb4ca", // springBlue
			"boolean":          "#ffa066",
			"tag":              "#e46876",
			"embedded":         "#dcd7ba",
		},
		Files: map[string]string{
			"dir":     "#7e9cd8",
			"default": "#dcd7ba",
			"go":      "#7aa89f",
			"md":      "#98bb6c",
			"toml":    "#e6c384",
			"json":    "#e6c384",
			"yaml":    "#e6c384",
			"lock":    "#727169",
		},
	}
}

// oneDark ports Atom's One Dark. The Error slot lightens the scheme's red
// (#e06c75, 4.38:1 on Surface) to #e88388 so every checked pair clears the
// AA contrast test; all other slots keep the official palette values.
func oneDark() Theme {
	return Theme{
		Name: "one-dark",
		Dark: true,
		UI: UI{
			Background:      "#282c34", // black
			Foreground:      "#abb2bf", // mono1
			Surface:         "#282c34",
			Panel:           "#21252b", // sidebar/panel background
			Border:          "#3e4451", // gutter/selection gray
			BorderFocus:     "#61afef", // blue
			Selection:       "#3e4451",
			SelectionText:   "#abb2bf",
			SelectionMuted:  "#2c313c", // cursor line
			OccurrenceRead:  "#323b4d",
			OccurrenceWrite: "#4a3f33",
			InlayHint:       "#5c6370",
			Whitespace:      "#3e4451",
			IndentGuide:     "#3e4451",
			Ruler:           "#21252b",
			Accent:          "#61afef", // blue
			Primary:         "#3e4451", // pmenu selection
			Secondary:       "#d19a66", // orange 1
			Success:         "#98c379", // green
			Warning:         "#e5c07b", // orange 2 (yellow)
			Error:           "#e88388", // red 1 lightened for AA
			Info:            "#61afef", // blue
			Hint:            "#56b6c2", // cyan
			MoveSource:      "#c678dd", // purple
			DropTarget:      "#d19a66",
			Ghost:           "#5c6370", // mono3 / comment gray
			ScrollbarTrack:  "#21252b",
			ScrollbarThumb:  "#4b5263", // gutter gray
		},
		Captures: map[string]string{
			"keyword":          "#c678dd", // purple
			"operator":         "#abb2bf", // mono1
			"string":           "#98c379", // green
			"number":           "#d19a66", // orange 1
			"comment":          "#5c6370", // mono3
			"function":         "#61afef", // blue
			"type":             "#e5c07b", // orange 2 (classes/types)
			"constant":         "#d19a66",
			"constant.builtin": "#d19a66",
			"variable":         "#abb2bf",
			"variable.builtin": "#e06c75", // red 1
			"property":         "#e06c75",
			"label":            "#c678dd",
			"attribute":        "#d19a66",
			"punctuation":      "#abb2bf",
			"escape":           "#56b6c2", // cyan
			"boolean":          "#d19a66",
			"tag":              "#e06c75",
			"embedded":         "#abb2bf",
		},
		Files: map[string]string{
			"dir":     "#61afef",
			"default": "#abb2bf",
			"go":      "#56b6c2",
			"md":      "#98c379",
			"toml":    "#e5c07b",
			"json":    "#e5c07b",
			"yaml":    "#e5c07b",
			"lock":    "#5c6370",
		},
	}
}

// solarizedDark ports Ethan Schoonover's Solarized (dark). The scheme's
// low-contrast accents sit below AA on the base03/base02 backgrounds, so the
// slots the contrast test checks against Panel (Secondary, Warning, Error,
// Info, Hint) carry lightened accent shades; Accent and Success only render
// on Surface and keep (or barely nudge) the canonical values.
func solarizedDark() Theme {
	return Theme{
		Name: "solarized-dark",
		Dark: true,
		UI: UI{
			Background:      "#002b36", // base03
			Foreground:      "#93a1a1", // base1 (base0 misses AA on Panel)
			Surface:         "#002b36",
			Panel:           "#073642", // base02
			Border:          "#586e75", // base01
			BorderFocus:     "#268bd2", // blue
			Selection:       "#586e75", // base01
			SelectionText:   "#fdf6e3", // base3
			SelectionMuted:  "#073642", // base02 (editor visual selection)
			OccurrenceRead:  "#0a4152",
			OccurrenceWrite: "#3d3a28",
			InlayHint:       "#586e75",
			Whitespace:      "#586e75",
			IndentGuide:     "#586e75",
			Ruler:           "#073642",
			Accent:          "#b58900", // yellow
			Primary:         "#586e75", // base01 (pmenu selection)
			Secondary:       "#db815c", // orange lightened for AA on Panel
			Success:         "#859900", // green
			Warning:         "#bb9316", // yellow lightened for AA on Panel
			Error:           "#e87674", // red lightened for AA on Panel
			Info:            "#4b9fda", // blue lightened for AA on Panel
			Hint:            "#39a89f", // cyan lightened for AA on Panel
			MoveSource:      "#dc322f", // red
			DropTarget:      "#b58900", // yellow
			Ghost:           "#cb4b16", // orange
			ScrollbarTrack:  "#073642",
			ScrollbarThumb:  "#586e75",
		},
		Captures: solarizedCaptures(false),
		Files: map[string]string{
			"dir":     "#268bd2",
			"default": "#93a1a1",
			"go":      "#2aa198",
			"md":      "#859900",
			"toml":    "#b58900",
			"json":    "#b58900",
			"yaml":    "#b58900",
			"lock":    "#586e75",
		},
	}
}

// solarizedLight mirrors solarizedDark on the base3/base2 backgrounds.
// Foreground darkens base00 slightly (#657b83 is 3.64:1 on base2) and the
// accent slots use darkened shades where the contrast test checks them.
func solarizedLight() Theme {
	return Theme{
		Name: "solarized-light",
		Dark: false,
		UI: UI{
			Background:      "#fdf6e3", // base3
			Foreground:      "#586c73", // base00 darkened for AA on Panel
			Surface:         "#fdf6e3",
			Panel:           "#eee8d5", // base2
			Border:          "#93a1a1", // base1
			BorderFocus:     "#268bd2", // blue
			Selection:       "#586e75", // base01
			SelectionText:   "#fdf6e3", // base3
			SelectionMuted:  "#eee8d5", // base2 (editor visual selection)
			OccurrenceRead:  "#e0ecec",
			OccurrenceWrite: "#f2e4c4",
			InlayHint:       "#93a1a1",
			Whitespace:      "#93a1a1",
			IndentGuide:     "#93a1a1",
			Ruler:           "#eee8d5",
			Accent:          "#c44815", // orange darkened for AA on Surface
			Primary:         "#586e75", // base01 (pmenu selection)
			Secondary:       "#b64314", // orange darkened for AA on Panel
			Success:         "#687800", // green darkened for AA on Surface
			Warning:         "#846400", // yellow darkened for AA on Panel
			Error:           "#c52d2a", // red darkened for AA on Panel
			Info:            "#1e6da5", // blue darkened for AA on Panel
			Hint:            "#1e746d", // cyan darkened for AA on Panel
			MoveSource:      "#dc322f", // red
			DropTarget:      "#b58900", // yellow
			Ghost:           "#cb4b16", // orange
			ScrollbarTrack:  "#eee8d5",
			ScrollbarThumb:  "#93a1a1",
		},
		Captures: solarizedCaptures(true),
		Files: map[string]string{
			"dir":     "#268bd2",
			"default": "#657b83",
			"go":      "#2aa198",
			"md":      "#859900",
			"toml":    "#b58900",
			"json":    "#b58900",
			"yaml":    "#b58900",
			"lock":    "#93a1a1",
		},
	}
}

// solarizedCaptures builds the capture table for solarized, following the
// canonical vim mapping (Statement=green, Identifier=blue, Type=yellow,
// Constant=cyan/magenta, Special=red, Comment=base01). Accents are shared
// between variants by design; only the monotone slots (operator, variable,
// comment, punctuation, embedded) flip between the base0x and base0x-inverse
// halves of the palette.
func solarizedCaptures(light bool) map[string]string {
	c := map[string]string{
		"keyword":          "#859900", // green
		"operator":         "#839496", // base0
		"string":           "#2aa198", // cyan
		"number":           "#d33682", // magenta
		"comment":          "#586e75", // base01
		"function":         "#268bd2", // blue
		"type":             "#b58900", // yellow
		"constant":         "#d33682",
		"constant.builtin": "#d33682",
		"variable":         "#839496", // base0
		"variable.builtin": "#cb4b16", // orange
		"property":         "#268bd2",
		"label":            "#859900",
		"attribute":        "#6c71c4", // violet
		"punctuation":      "#657b83", // base00
		"escape":           "#dc322f", // red
		"boolean":          "#d33682",
		"tag":              "#dc322f",
		"embedded":         "#839496",
	}
	if light {
		for k, v := range map[string]string{
			"operator": "#657b83", "comment": "#93a1a1", "variable": "#657b83",
			"punctuation": "#839496", "embedded": "#657b83",
		} {
			c[k] = v
		}
	}
	return c
}

// dracula ports the official Dracula spec (draculatheme.com/contribute).
// Every slot uses a canonical palette value; the darker panel/scrollbar
// shades follow the Dracula VSCode port (#21222c sidebar). Red (#ff5555)
// clears AA on both Surface (4.52:1) and the darker Panel, so no accent
// needed lightening.
func dracula() Theme {
	return Theme{
		Name: "dracula",
		Dark: true,
		UI: UI{
			Background:      "#282a36", // background
			Foreground:      "#f8f8f2", // foreground
			Surface:         "#282a36",
			Panel:           "#21222c", // sidebar/panel (VSCode port)
			Border:          "#44475a", // current line / selection
			BorderFocus:     "#bd93f9", // purple
			Selection:       "#44475a", // selection
			SelectionText:   "#f8f8f2",
			SelectionMuted:  "#44475a", // editor visual selection
			OccurrenceRead:  "#34405c",
			OccurrenceWrite: "#514440",
			InlayHint:       "#6272a4",
			Whitespace:      "#44475a",
			IndentGuide:     "#44475a",
			Ruler:           "#21222c",
			Accent:          "#ff79c6", // pink
			Primary:         "#44475a", // pmenu selection
			Secondary:       "#ffb86c", // orange
			Success:         "#50fa7b", // green
			Warning:         "#f1fa8c", // yellow
			Error:           "#ff5555", // red
			Info:            "#bd93f9", // purple
			Hint:            "#8be9fd", // cyan
			MoveSource:      "#ff5555",
			DropTarget:      "#ffb86c",
			Ghost:           "#6272a4", // comment
			ScrollbarTrack:  "#21222c",
			ScrollbarThumb:  "#44475a",
		},
		Captures: map[string]string{
			"keyword":          "#ff79c6", // pink
			"operator":         "#ff79c6",
			"string":           "#f1fa8c", // yellow
			"number":           "#bd93f9", // purple
			"comment":          "#6272a4", // comment
			"function":         "#50fa7b", // green
			"type":             "#8be9fd", // cyan
			"constant":         "#bd93f9",
			"constant.builtin": "#bd93f9",
			"variable":         "#f8f8f2",
			"variable.builtin": "#bd93f9", // this/self purple
			"property":         "#f8f8f2",
			"label":            "#ff79c6",
			"attribute":        "#50fa7b", // HTML attributes green
			"punctuation":      "#f8f8f2",
			"escape":           "#ff79c6",
			"boolean":          "#bd93f9",
			"tag":              "#ff79c6", // HTML tags pink
			"embedded":         "#f8f8f2",
		},
		Files: map[string]string{
			"dir":     "#bd93f9",
			"default": "#f8f8f2",
			"go":      "#8be9fd",
			"md":      "#50fa7b",
			"toml":    "#f1fa8c",
			"json":    "#f1fa8c",
			"yaml":    "#f1fa8c",
			"lock":    "#6272a4",
		},
	}
}

func catppuccinLatte() Theme {
	return Theme{
		Name: "catppuccin-latte",
		Dark: false,
		UI: UI{
			Background:      "#e6e9ef",
			Foreground:      "#4c4f69",
			Surface:         "#eff1f5",
			Panel:           "#ccd0da",
			Border:          "#bcc0cc",
			BorderFocus:     "#7287fd",
			Selection:       "#3d5afc",
			SelectionText:   "#eff1f5",
			SelectionMuted:  "#bcc0cc",
			OccurrenceRead:  "#ccd8e8",
			OccurrenceWrite: "#e8d8c4",
			InlayHint:       "#9ca0b0",
			Whitespace:      "#bcc0cc",
			IndentGuide:     "#bcc0cc",
			Ruler:           "#ccd0da",
			Accent:          "#a1197d",
			Primary:         "#1761f5",
			Secondary:       "#9b3901",
			Success:         "#327c21",
			Warning:         "#7c4f10",
			Error:           "#b10d30",
			Info:            "#025f83",
			Hint:            "#0f6166",
			MoveSource:      "#d20f39",
			DropTarget:      "#df8e1d",
			Ghost:           "#fe640b",
			ScrollbarTrack:  "#ccd0da",
			ScrollbarThumb:  "#9ca0b0",
		},
		Captures: map[string]string{
			"keyword":          "#8839ef",
			"operator":         "#04a5e5",
			"string":           "#40a02b",
			"number":           "#fe640b",
			"comment":          "#9ca0b0",
			"function":         "#1e66f5",
			"type":             "#df8e1d",
			"constant":         "#fe640b",
			"constant.builtin": "#fe640b",
			"variable":         "#4c4f69",
			"variable.builtin": "#d20f39",
			"property":         "#7287fd",
			"label":            "#8839ef",
			"attribute":        "#df8e1d",
			"punctuation":      "#7c7f93",
			"escape":           "#ea76cb",
			"boolean":          "#fe640b",
			"tag":              "#d20f39",
			"embedded":         "#4c4f69",
		},
		Files: map[string]string{
			"dir":     "#1e66f5",
			"default": "#4c4f69",
			"go":      "#179299",
			"md":      "#40a02b",
			"toml":    "#df8e1d",
			"json":    "#df8e1d",
			"yaml":    "#df8e1d",
			"lock":    "#9ca0b0",
		},
	}
}
