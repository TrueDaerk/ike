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
			Accent:         "#d7af87", // explorer active entry
			Primary:        "#005f87", // completion selected row
			Secondary:      "#ffaf5f",
			Success:        "#5fd75f",
			Warning:        "#9f9f00",
			Error:          "#ff6464",
			Info:           "#8d8dff",
			Hint:           "#00a9a9",
			MoveSource:     "#ff5f5f",
			DropTarget:     "#ffd700",
			Ghost:          "#af8700",
			ScrollbarTrack: "#585858",
			ScrollbarThumb: "#8a8a8a",
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
			Background:     "#16161e",
			Foreground:     "#a9b1d6",
			Surface:        "#1a1b26",
			Panel:          "#24283b",
			Border:         "#414868",
			BorderFocus:    "#7aa2f7",
			Selection:      "#7aa2f7",
			SelectionText:  "#1a1b26",
			SelectionMuted: "#283457",
			Accent:         "#7fa1de",
			Primary:        "#7aa2f7",
			Secondary:      "#ff9e64",
			Success:        "#9ece6a",
			Warning:        "#e0af68",
			Error:          "#f7768e",
			Info:           "#7aa2f7",
			Hint:           "#1abc9c",
			MoveSource:     "#f7768e",
			DropTarget:     "#e0af68",
			Ghost:          "#ff9e64",
			ScrollbarTrack: "#24283b",
			ScrollbarThumb: "#414868",
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
			Background:     "#2e3440",
			Foreground:     "#d8dee9",
			Surface:        "#2e3440",
			Panel:          "#3b4252",
			Border:         "#4c566a",
			BorderFocus:    "#88c0d0",
			Selection:      "#88c0d0",
			SelectionText:  "#2e3440",
			SelectionMuted: "#434c5e",
			Accent:         "#8fbcbb",
			Primary:        "#88c0d0",
			Secondary:      "#dba291",
			Success:        "#a3be8c",
			Warning:        "#ebcb8b",
			Error:          "#d9a1a6",
			Info:           "#97b2cc",
			Hint:           "#8fbcbb",
			MoveSource:     "#bf616a",
			DropTarget:     "#ebcb8b",
			Ghost:          "#d08770",
			ScrollbarTrack: "#3b4252",
			ScrollbarThumb: "#4c566a",
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
			Background:     "#1d2021",
			Foreground:     "#ebdbb2",
			Surface:        "#282828",
			Panel:          "#3c3836",
			Border:         "#504945",
			BorderFocus:    "#fabd2f",
			Selection:      "#076678",
			SelectionText:  "#ebdbb2",
			SelectionMuted: "#504945",
			Accent:         "#fe8019",
			Primary:        "#076678",
			Secondary:      "#fe8019",
			Success:        "#b8bb26",
			Warning:        "#fabd2f",
			Error:          "#fc7d6e",
			Info:           "#8aaa9e",
			Hint:           "#8ec07c",
			MoveSource:     "#fb4934",
			DropTarget:     "#fabd2f",
			Ghost:          "#fe8019",
			ScrollbarTrack: "#3c3836",
			ScrollbarThumb: "#665c54",
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
			Background:     "#f9f5d7",
			Foreground:     "#3c3836",
			Surface:        "#fbf1c7",
			Panel:          "#ebdbb2",
			Border:         "#d5c4a1",
			BorderFocus:    "#d79921",
			Selection:      "#3d7679",
			SelectionText:  "#fbf1c7",
			SelectionMuted: "#d5c4a1",
			Accent:         "#9f450a",
			Primary:        "#3d7679",
			Secondary:      "#9f450a",
			Success:        "#717013",
			Warning:        "#7c5813",
			Error:          "#ba211a",
			Info:           "#36676a",
			Hint:           "#446845",
			MoveSource:     "#cc241d",
			DropTarget:     "#d79921",
			Ghost:          "#d65d0e",
			ScrollbarTrack: "#ebdbb2",
			ScrollbarThumb: "#bdae93",
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
			Background:     "#191724",
			Foreground:     "#e0def4",
			Surface:        "#191724",
			Panel:          "#26233a",
			Border:         "#403d52",
			BorderFocus:    "#c4a7e7",
			Selection:      "#c4a7e7",
			SelectionText:  "#191724",
			SelectionMuted: "#403d52",
			Accent:         "#ebbcba",
			Primary:        "#9ccfd8",
			Secondary:      "#f6c177",
			Success:        "#9ccfd8",
			Warning:        "#f6c177",
			Error:          "#eb6f92",
			Info:           "#4097bb",
			Hint:           "#9ccfd8",
			MoveSource:     "#eb6f92",
			DropTarget:     "#f6c177",
			Ghost:          "#ebbcba",
			ScrollbarTrack: "#26233a",
			ScrollbarThumb: "#524f67",
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
			Background:     "#faf4ed",
			Foreground:     "#575279",
			Surface:        "#faf4ed",
			Panel:          "#f2e9e1",
			Border:         "#dfdad9",
			BorderFocus:    "#907aa9",
			Selection:      "#7e649b",
			SelectionText:  "#faf4ed",
			SelectionMuted: "#dfdad9",
			Accent:         "#b83f39",
			Primary:        "#286983",
			Secondary:      "#945c0f",
			Success:        "#416f77",
			Warning:        "#945c0f",
			Error:          "#a34e66",
			Info:           "#286983",
			Hint:           "#416f77",
			MoveSource:     "#b4637a",
			DropTarget:     "#ea9d34",
			Ghost:          "#d7827e",
			ScrollbarTrack: "#f2e9e1",
			ScrollbarThumb: "#9893a5",
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
			Background:     "#181825",
			Foreground:     "#cdd6f4",
			Surface:        "#1e1e2e",
			Panel:          "#313244",
			Border:         "#45475a",
			BorderFocus:    "#b4befe",
			Selection:      "#b4befe",
			SelectionText:  "#1e1e2e",
			SelectionMuted: "#45475a",
			Accent:         "#f5c2e7",
			Primary:        "#89b4fa",
			Secondary:      "#fab387",
			Success:        "#a6e3a1",
			Warning:        "#f9e2af",
			Error:          "#f38ba8",
			Info:           "#89dceb",
			Hint:           "#94e2d5",
			MoveSource:     "#f38ba8",
			DropTarget:     "#f9e2af",
			Ghost:          "#fab387",
			ScrollbarTrack: "#313244",
			ScrollbarThumb: "#585b70",
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
			Background:     "#1f1f28", // sumiInk3
			Foreground:     "#dcd7ba", // fujiWhite
			Surface:        "#1f1f28",
			Panel:          "#2a2a37", // sumiInk4
			Border:         "#54546d", // sumiInk6
			BorderFocus:    "#7e9cd8", // crystalBlue
			Selection:      "#2d4f67", // waveBlue2
			SelectionText:  "#dcd7ba",
			SelectionMuted: "#223249", // waveBlue1
			Accent:         "#e6c384", // carpYellow
			Primary:        "#2d4f67", // waveBlue2 (pmenu selection)
			Secondary:      "#ffa066", // surimiOrange
			Success:        "#98bb6c", // springGreen
			Warning:        "#ff9e3b", // roninYellow
			Error:          "#ff5d62", // peachRed
			Info:           "#7fb4ca", // springBlue
			Hint:           "#7aa89f", // waveAqua2
			MoveSource:     "#e46876", // waveRed
			DropTarget:     "#ff9e3b",
			Ghost:          "#ffa066",
			ScrollbarTrack: "#2a2a37",
			ScrollbarThumb: "#54546d",
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

func catppuccinLatte() Theme {
	return Theme{
		Name: "catppuccin-latte",
		Dark: false,
		UI: UI{
			Background:     "#e6e9ef",
			Foreground:     "#4c4f69",
			Surface:        "#eff1f5",
			Panel:          "#ccd0da",
			Border:         "#bcc0cc",
			BorderFocus:    "#7287fd",
			Selection:      "#3d5afc",
			SelectionText:  "#eff1f5",
			SelectionMuted: "#bcc0cc",
			Accent:         "#a1197d",
			Primary:        "#1761f5",
			Secondary:      "#9b3901",
			Success:        "#327c21",
			Warning:        "#7c4f10",
			Error:          "#b10d30",
			Info:           "#025f83",
			Hint:           "#0f6166",
			MoveSource:     "#d20f39",
			DropTarget:     "#df8e1d",
			Ghost:          "#fe640b",
			ScrollbarTrack: "#ccd0da",
			ScrollbarThumb: "#9ca0b0",
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
