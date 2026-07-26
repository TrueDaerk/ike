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
			Selection:      "#1f3065",
			SelectionText:  "#d0d0d0",
			SelectionMuted: "#2c2c2c", // editor visual selection
			// Occurrence marks (#172) sit below the visual selection in
			// emphasis: read cool, write warm.
			OccurrenceRead:  "#242d36",
			OccurrenceWrite: "#332b23",
			InlayHint:       "gray",
			Whitespace:      "#585858",
			IndentGuide:     "#585858",
			Ruler:           "#2c2c2c",
			Accent:          "#d7af87", // explorer active entry
			Primary:         "#09384b", // completion selected row
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
			DiffAdded:       "#1e311e",
			DiffRemoved:     "#432323",
			DiffChanged:     "#2e2e0e",
			VCSModified:     "#8d8dff", // vcs status foregrounds (Roadmap 0320)
			VCSAdded:        "#5fd75f",
			VCSUntracked:    "#9f9f00",
			VCSDeleted:      "#868686",
			VCSConflicted:   "#ff6464",
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
			Selection:       "#2f3954",
			SelectionText:   "#a9b1d6",
			SelectionMuted:  "#273252",
			OccurrenceRead:  "#23334e",
			OccurrenceWrite: "#3a312e",
			InlayHint:       "#717aa6",
			Whitespace:      "#414868",
			IndentGuide:     "#414868",
			Ruler:           "#24283b",
			Accent:          "#7fa1de",
			Primary:         "#2f3954",
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
			DiffAdded:       "#2d352f",
			DiffRemoved:     "#432c39",
			DiffChanged:     "#383130",
			VCSModified:     "#7aa2f7",
			VCSAdded:        "#9ece6a",
			VCSUntracked:    "#e0af68",
			VCSDeleted:      "#727ca7",
			VCSConflicted:   "#f7768e",
		},
		Captures: map[string]string{
			"keyword":          "#bb9af7",
			"operator":         "#89ddff",
			"string":           "#9ece6a",
			"number":           "#ff9e64",
			"comment":          "#656f9e",
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
			"lock":    "#717aa6",
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
			Selection:       "#3f4f5b",
			SelectionText:   "#d8dee9",
			SelectionMuted:  "#3f4758",
			OccurrenceRead:  "#3b4657",
			OccurrenceWrite: "#4c4642",
			InlayHint:       "#909bb0",
			Whitespace:      "#4c566a",
			IndentGuide:     "#4c566a",
			Ruler:           "#3b4252",
			Accent:          "#8fbcbb",
			Primary:         "#3f4f5b",
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
			DiffAdded:       "#40494b",
			DiffRemoved:     "#4a4551",
			DiffChanged:     "#464749",
			VCSModified:     "#97b2cc",
			VCSAdded:        "#a3be8c",
			VCSUntracked:    "#ebcb8b",
			VCSDeleted:      "#909bb0",
			VCSConflicted:   "#d9a1a6",
		},
		Captures: map[string]string{
			"keyword":          "#81a1c1",
			"operator":         "#81a1c1",
			"string":           "#a3be8c",
			"number":           "#b691af",
			"comment":          "#7e8aa3",
			"function":         "#88c0d0",
			"type":             "#8fbcbb",
			"constant":         "#d18a74",
			"constant.builtin": "#d18a74",
			"variable":         "#d8dee9",
			"variable.builtin": "#cf8990",
			"property":         "#d8dee9",
			"label":            "#81a1c1",
			"attribute":        "#ebcb8b",
			"punctuation":      "#eceff4",
			"escape":           "#ebcb8b",
			"boolean":          "#81a1c1",
			"tag":              "#cf8990",
			"embedded":         "#d8dee9",
		},
		Files: map[string]string{
			"dir":     "#99b3cd",
			"default": "#d8dee9",
			"go":      "#88c0d0",
			"md":      "#a3be8c",
			"toml":    "#ebcb8b",
			"json":    "#ebcb8b",
			"yaml":    "#ebcb8b",
			"lock":    "#909bb0",
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
			Selection:       "#164953",
			SelectionText:   "#ebdbb2",
			SelectionMuted:  "#403c39",
			OccurrenceRead:  "#303f41",
			OccurrenceWrite: "#4b392c",
			InlayHint:       "#9b8d7f",
			Whitespace:      "#504945",
			IndentGuide:     "#504945",
			Ruler:           "#3c3836",
			Accent:          "#fe8019",
			Primary:         "#164953",
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
			DiffAdded:       "#3d3e27",
			DiffRemoved:     "#4e3734",
			DiffChanged:     "#443c29",
			VCSModified:     "#8aaa9e",
			VCSAdded:        "#b8bb26",
			VCSUntracked:    "#fabd2f",
			VCSDeleted:      "#988d87",
			VCSConflicted:   "#fc7d6e",
		},
		Captures: gruvboxCaptures(false),
		Files: map[string]string{
			"dir":     "#89a99d",
			"default": "#ebdbb2",
			"go":      "#8ec07c",
			"md":      "#b8bb26",
			"toml":    "#fabd2f",
			"json":    "#fabd2f",
			"yaml":    "#fabd2f",
			"lock":    "#9b8d7f",
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
			Selection:       "#c0cbaf",
			SelectionText:   "#3c3836",
			SelectionMuted:  "#dfd0ab",
			OccurrenceRead:  "#d0dce2",
			OccurrenceWrite: "#ecd9b5",
			InlayHint:       "#7c6f64",
			Whitespace:      "#d5c4a1",
			IndentGuide:     "#d5c4a1",
			Ruler:           "#ebdbb2",
			Accent:          "#9f450a",
			Primary:         "#c0cbaf",
			Secondary:       "#9f450a",
			Success:         "#646311",
			Warning:         "#7c5813",
			Error:           "#ba211a",
			Info:            "#36676a",
			Hint:            "#446845",
			MoveSource:      "#cc241d",
			DropTarget:      "#d79921",
			Ghost:           "#d65d0e",
			ScrollbarTrack:  "#ebdbb2",
			ScrollbarThumb:  "#bdae93",
			DiffAdded:       "#dcd49f",
			DiffRemoved:     "#efcba7",
			DiffChanged:     "#e0d1a1",
			VCSModified:     "#36676a",
			VCSAdded:        "#646311",
			VCSUntracked:    "#7c5813",
			VCSDeleted:      "#876f3c",
			VCSConflicted:   "#ba211a",
		},
		Captures: gruvboxCaptures(true),
		Files: map[string]string{
			"dir":     "#36676a",
			"default": "#3c3836",
			"go":      "#456a46",
			"md":      "#646311",
			"toml":    "#7f5a13",
			"json":    "#7f5a13",
			"yaml":    "#7f5a13",
			"lock":    "#7c6f64",
		},
	}
}

// gruvboxCaptures builds the capture table for gruvbox; the light variant
// swaps in the darker accent shades so contrast holds on the light background.
// Both sets are tuned so every capture clears WCAG AA against its variant's
// Surface (see TestBuiltinThemeFullContrast).
func gruvboxCaptures(light bool) map[string]string {
	if light {
		return map[string]string{
			"keyword":          "#cc241d",
			"operator":         "#3c3836",
			"string":           "#717013",
			"number":           "#a35177",
			"comment":          "#7c6f64",
			"function":         "#717013",
			"type":             "#8c6415",
			"constant":         "#a35177",
			"constant.builtin": "#a35177",
			"variable":         "#3c3836",
			"variable.builtin": "#b44e0c",
			"property":         "#3c7477",
			"label":            "#cc241d",
			"attribute":        "#8c6415",
			"punctuation":      "#665c54",
			"escape":           "#b44e0c",
			"boolean":          "#a35177",
			"tag":              "#cc241d",
			"embedded":         "#3c3836",
		}
	}
	return map[string]string{
		"keyword":          "#fb5643",
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
		"label":            "#fb5643",
		"attribute":        "#fabd2f",
		"punctuation":      "#a89984",
		"escape":           "#fe8019",
		"boolean":          "#d3869b",
		"tag":              "#fb5643",
		"embedded":         "#ebdbb2",
	}
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
			Selection:       "#3b344b",
			SelectionText:   "#e0def4",
			SelectionMuted:  "#312e40",
			OccurrenceRead:  "#273143",
			OccurrenceWrite: "#392c39",
			InlayHint:       "#7b7693",
			Whitespace:      "#403d52",
			IndentGuide:     "#403d52",
			Ruler:           "#26233a",
			Accent:          "#ebbcba",
			Primary:         "#313844",
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
			DiffAdded:       "#2b313d",
			DiffRemoved:     "#42283a",
			DiffChanged:     "#372e2f",
			VCSModified:     "#4097bb",
			VCSAdded:        "#9ccfd8",
			VCSUntracked:    "#f6c177",
			VCSDeleted:      "#7b7699",
			VCSConflicted:   "#eb6f92",
		},
		Captures: map[string]string{
			"keyword":          "#3a8aaa",
			"operator":         "#908caa",
			"string":           "#f6c177",
			"number":           "#ebbcba",
			"comment":          "#706c89",
			"function":         "#ebbcba",
			"type":             "#9ccfd8",
			"constant":         "#ebbcba",
			"constant.builtin": "#ebbcba",
			"variable":         "#e0def4",
			"variable.builtin": "#eb6f92",
			"property":         "#c4a7e7",
			"label":            "#3a8aaa",
			"attribute":        "#c4a7e7",
			"punctuation":      "#908caa",
			"escape":           "#eb6f92",
			"boolean":          "#ebbcba",
			"tag":              "#eb6f92",
			"embedded":         "#e0def4",
		},
		Files: map[string]string{
			"dir":     "#3f96b9",
			"default": "#e0def4",
			"go":      "#9ccfd8",
			"md":      "#f6c177",
			"toml":    "#c4a7e7",
			"json":    "#c4a7e7",
			"yaml":    "#c4a7e7",
			"lock":    "#7b7693",
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
			Selection:       "#d4c7d4",
			SelectionText:   "#555076",
			SelectionMuted:  "#dfdad9",
			OccurrenceRead:  "#dee7ea",
			OccurrenceWrite: "#f3ddd0",
			InlayHint:       "#7d778e",
			Whitespace:      "#dfdad9",
			IndentGuide:     "#dfdad9",
			Ruler:           "#f2e9e1",
			Accent:          "#b83f39",
			Primary:         "#bfcdcf",
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
			DiffAdded:       "#d1d6d3",
			DiffRemoved:     "#e6d0d0",
			DiffChanged:     "#e4d3bc",
			VCSModified:     "#286983",
			VCSAdded:        "#416f77",
			VCSUntracked:    "#945c0f",
			VCSDeleted:      "#887673",
			VCSConflicted:   "#a34e66",
		},
		Captures: map[string]string{
			"keyword":          "#286983",
			"operator":         "#6f6b89",
			"string":           "#9e6210",
			"number":           "#c2423c",
			"comment":          "#837d92",
			"function":         "#c2423c",
			"type":             "#44757e",
			"constant":         "#c2423c",
			"constant.builtin": "#c2423c",
			"variable":         "#575279",
			"variable.builtin": "#ab526c",
			"property":         "#7e649b",
			"label":            "#286983",
			"attribute":        "#7e649b",
			"punctuation":      "#797593",
			"escape":           "#ab526c",
			"boolean":          "#c2423c",
			"tag":              "#ab526c",
			"embedded":         "#575279",
		},
		Files: map[string]string{
			"dir":     "#286983",
			"default": "#575279",
			"go":      "#416f77",
			"md":      "#945c0f",
			"toml":    "#765e92",
			"json":    "#765e92",
			"yaml":    "#765e92",
			"lock":    "#7d778e",
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
			Selection:       "#383951",
			SelectionText:   "#cdd6f4",
			SelectionMuted:  "#333446",
			OccurrenceRead:  "#2a3045",
			OccurrenceWrite: "#3e3330",
			InlayHint:       "#84889c",
			Whitespace:      "#45475a",
			IndentGuide:     "#45475a",
			Ruler:           "#313244",
			Accent:          "#f5c2e7",
			Primary:         "#323b55",
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
			DiffAdded:       "#2f373c",
			DiffRemoved:     "#413042",
			DiffChanged:     "#37343d",
			VCSModified:     "#89dceb",
			VCSAdded:        "#a6e3a1",
			VCSUntracked:    "#f9e2af",
			VCSDeleted:      "#8386a0",
			VCSConflicted:   "#f38ba8",
		},
		Captures: map[string]string{
			"keyword":          "#cba6f7",
			"operator":         "#89dceb",
			"string":           "#a6e3a1",
			"number":           "#fab387",
			"comment":          "#71758c",
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
			"lock":    "#84889c",
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
			Selection:       "#283f52", // waveBlue2
			SelectionText:   "#dcd7ba",
			SelectionMuted:  "#223249", // waveBlue1
			OccurrenceRead:  "#25354d",
			OccurrenceWrite: "#383534",
			InlayHint:       "#828178",
			Whitespace:      "#54546d",
			IndentGuide:     "#54546d",
			Ruler:           "#2a2a37",
			Accent:          "#e6c384", // carpYellow
			Primary:         "#283f52", // waveBlue2 (pmenu selection)
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
			DiffAdded:       "#313732",
			DiffRemoved:     "#4f2c34",
			DiffChanged:     "#42332b",
			VCSModified:     "#7fb4ca",
			VCSAdded:        "#98bb6c",
			VCSUntracked:    "#ff9e3b",
			VCSDeleted:      "#7d7d9b",
			VCSConflicted:   "#ff5d62",
		},
		Captures: map[string]string{
			"keyword":          "#957fb8", // oniViolet
			"operator":         "#c0a36e", // boatYellow2
			"string":           "#98bb6c", // springGreen
			"number":           "#d27e99", // sakuraPink
			"comment":          "#77766e", // fujiGray
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
			"lock":    "#828178",
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
			OccurrenceWrite: "#493e33",
			InlayHint:       "#798191",
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
			DiffAdded:       "#38423e",
			DiffRemoved:     "#4b3c43",
			DiffChanged:     "#41403d",
			VCSModified:     "#61afef",
			VCSAdded:        "#98c379",
			VCSUntracked:    "#e5c07b",
			VCSDeleted:      "#778197",
			VCSConflicted:   "#e88388",
		},
		Captures: map[string]string{
			"keyword":          "#c678dd", // purple
			"operator":         "#abb2bf", // mono1
			"string":           "#98c379", // green
			"number":           "#d19a66", // orange 1
			"comment":          "#798191", // mono3
			"function":         "#61afef", // blue
			"type":             "#e5c07b", // orange 2 (classes/types)
			"constant":         "#d19a66",
			"constant.builtin": "#d19a66",
			"variable":         "#abb2bf",
			"variable.builtin": "#e17079", // red 1
			"property":         "#e17079",
			"label":            "#c678dd",
			"attribute":        "#d19a66",
			"punctuation":      "#abb2bf",
			"escape":           "#56b6c2", // cyan
			"boolean":          "#d19a66",
			"tag":              "#e17079",
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
			"lock":    "#798191",
		},
	}
}

// solarizedDark ports Ethan Schoonover's Solarized (dark). The scheme's
// low-contrast accents sit below AA on the base03/base02 backgrounds, so the
// slots the contrast test checks against Panel (Secondary, Warning, Error,
// Info, Hint) carry lightened accent shades; Accent and Success only render
// on Surface and are nudged just far enough to clear it.
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
			Selection:       "#244650", // base01
			SelectionText:   "#a3afaf", // base3
			SelectionMuted:  "#073642", // base02 (editor visual selection)
			OccurrenceRead:  "#0a4152",
			OccurrenceWrite: "#3d3a28",
			InlayHint:       "#6e8992",
			Whitespace:      "#586e75",
			IndentGuide:     "#586e75",
			Ruler:           "#073642",
			Accent:          "#c49500", // yellow
			Primary:         "#244650", // base01 (pmenu selection)
			Secondary:       "#db815c", // orange lightened for AA on Panel
			Success:         "#8ea300", // green
			Warning:         "#bb9316", // yellow lightened for AA on Panel
			Error:           "#e87674", // red lightened for AA on Panel
			Info:            "#4b9fda", // blue lightened for AA on Panel
			Hint:            "#39a89f", // cyan lightened for AA on Panel
			MoveSource:      "#dc322f", // red
			DropTarget:      "#b58900", // yellow
			Ghost:           "#cb4b16", // orange
			ScrollbarTrack:  "#073642",
			ScrollbarThumb:  "#586e75",
			DiffAdded:       "#1d432a",
			DiffRemoved:     "#333b43",
			DiffChanged:     "#28412f",
			VCSModified:     "#4b9fda",
			VCSAdded:        "#8ea300",
			VCSUntracked:    "#bb9316",
			VCSDeleted:      "#6e8992",
			VCSConflicted:   "#e87674",
		},
		Captures: solarizedCaptures(false),
		Files: map[string]string{
			"dir":     "#48a0de",
			"default": "#93a1a1",
			"go":      "#2ca9a0",
			"md":      "#8ea300",
			"toml":    "#c49500",
			"json":    "#c49500",
			"yaml":    "#c49500",
			"lock":    "#6e8992",
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
			Selection:       "#cccdc2", // base01
			SelectionText:   "#49595f", // base3
			SelectionMuted:  "#eee8d5", // base2 (editor visual selection)
			OccurrenceRead:  "#e0ecec",
			OccurrenceWrite: "#f2e4c4",
			InlayHint:       "#6c7c7c",
			Whitespace:      "#93a1a1",
			IndentGuide:     "#93a1a1",
			Ruler:           "#eee8d5",
			Accent:          "#b64314", // orange darkened for AA on Surface
			Primary:         "#cccdc2", // base01 (pmenu selection)
			Secondary:       "#b64314", // orange darkened for AA on Panel
			Success:         "#5f6e00", // green darkened for AA on Surface
			Warning:         "#846400", // yellow darkened for AA on Panel
			Error:           "#c52d2a", // red darkened for AA on Panel
			Info:            "#1e6da5", // blue darkened for AA on Panel
			Hint:            "#1e746d", // cyan darkened for AA on Panel
			MoveSource:      "#dc322f", // red
			DropTarget:      "#b58900", // yellow
			Ghost:           "#cb4b16", // orange
			ScrollbarTrack:  "#eee8d5",
			ScrollbarThumb:  "#93a1a1",
			DiffAdded:       "#dcdab1",
			DiffRemoved:     "#f2cfbf",
			DiffChanged:     "#e2d6b1",
			VCSModified:     "#1e6da5",
			VCSAdded:        "#5f6e00",
			VCSUntracked:    "#846400",
			VCSDeleted:      "#6c7c7c",
			VCSConflicted:   "#c52d2a",
		},
		Captures: solarizedCaptures(true),
		Files: map[string]string{
			"dir":     "#1d6ca2",
			"default": "#586b72",
			"go":      "#1e756e",
			"md":      "#5d6b00",
			"toml":    "#826200",
			"json":    "#826200",
			"yaml":    "#826200",
			"lock":    "#6c7c7c",
		},
	}
}

// solarizedCaptures builds the capture table for solarized, following the
// canonical vim mapping (Statement=green, Identifier=blue, Type=yellow,
// Constant=cyan/magenta, Special=red, Comment=base01). Accents are shared
// between variants by design; only the monotone slots (operator, variable,
// comment, punctuation, embedded) flip between the base0x and base0x-inverse
// halves of the palette. Stock solarized accents sit below WCAG AA on both
// backgrounds, so each hue is lightened (dark) or darkened (light) until it
// clears 4.5:1 against its variant's Surface.
func solarizedCaptures(light bool) map[string]string {
	if light {
		return map[string]string{
			"keyword":          "#667500",
			"operator":         "#5e737a",
			"string":           "#217d76",
			"number":           "#cd2d7a",
			"comment":          "#758787",
			"function":         "#2074af",
			"type":             "#8c6a00",
			"constant":         "#cd2d7a",
			"constant.builtin": "#cd2d7a",
			"variable":         "#5e737a",
			"variable.builtin": "#c24815",
			"property":         "#2074af",
			"label":            "#667500",
			"attribute":        "#6166c0",
			"punctuation":      "#738588",
			"escape":           "#d72724",
			"boolean":          "#cd2d7a",
			"tag":              "#d72724",
			"embedded":         "#5e737a",
		}
	}
	return map[string]string{
		"keyword":          "#859900",
		"operator":         "#839496",
		"string":           "#2aa198",
		"number":           "#dd649f",
		"comment":          "#657e86",
		"function":         "#3295da",
		"type":             "#b58900",
		"constant":         "#dd649f",
		"constant.builtin": "#dd649f",
		"variable":         "#839496",
		"variable.builtin": "#e96630",
		"property":         "#3295da",
		"label":            "#859900",
		"attribute":        "#858ace",
		"punctuation":      "#698089",
		"escape":           "#e56663",
		"boolean":          "#dd649f",
		"tag":              "#e56663",
		"embedded":         "#839496",
	}
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
			Selection:       "#424557", // selection
			SelectionText:   "#f8f8f2",
			SelectionMuted:  "#3b3e4f", // editor visual selection
			OccurrenceRead:  "#333e59",
			OccurrenceWrite: "#463d3d",
			InlayHint:       "#6f7eab",
			Whitespace:      "#44475a",
			IndentGuide:     "#44475a",
			Ruler:           "#21222c",
			Accent:          "#ff79c6", // pink
			Primary:         "#424557", // pmenu selection
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
			DiffAdded:       "#2c433e",
			DiffRemoved:     "#57333c",
			DiffChanged:     "#3c3f3f",
			VCSModified:     "#bd93f9",
			VCSAdded:        "#50fa7b",
			VCSUntracked:    "#f1fa8c",
			VCSDeleted:      "#797e9a",
			VCSConflicted:   "#ff5555",
		},
		Captures: map[string]string{
			"keyword":          "#ff79c6", // pink
			"operator":         "#ff79c6",
			"string":           "#f1fa8c", // yellow
			"number":           "#bd93f9", // purple
			"comment":          "#6f7eab", // comment
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
			"lock":    "#6f7eab",
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
			Selection:       "#bbc5f7",
			SelectionText:   "#4c4f69",
			SelectionMuted:  "#ced1da",
			OccurrenceRead:  "#ccd8e8",
			OccurrenceWrite: "#e8d8c4",
			InlayHint:       "#64697d",
			Whitespace:      "#bcc0cc",
			IndentGuide:     "#bcc0cc",
			Ruler:           "#cdd1db",
			Accent:          "#a1197d",
			Primary:         "#b3c9f5",
			Secondary:       "#9b3901",
			Success:         "#28641b",
			Warning:         "#7c4f10",
			Error:           "#b10d30",
			Info:            "#025f83",
			Hint:            "#0f6166",
			MoveSource:      "#d20f39",
			DropTarget:      "#df8e1d",
			Ghost:           "#fe640b",
			ScrollbarTrack:  "#ccd0da",
			ScrollbarThumb:  "#9ca0b0",
			DiffAdded:       "#c5d7c6",
			DiffRemoved:     "#e4cbd4",
			DiffChanged:     "#d8d1c7",
			VCSModified:     "#025f83",
			VCSAdded:        "#28641b",
			VCSUntracked:    "#7c4f10",
			VCSDeleted:      "#61687f",
			VCSConflicted:   "#b10d30",
		},
		Captures: map[string]string{
			"keyword":          "#8839ef",
			"operator":         "#03729f",
			"string":           "#327c21",
			"number":           "#bc4501",
			"comment":          "#777d93",
			"function":         "#145ff5",
			"type":             "#976014",
			"constant":         "#bc4501",
			"constant.builtin": "#bc4501",
			"variable":         "#4c4f69",
			"variable.builtin": "#d20f39",
			"property":         "#3b58fc",
			"label":            "#8839ef",
			"attribute":        "#976014",
			"punctuation":      "#797c91",
			"escape":           "#c71f9a",
			"boolean":          "#bc4501",
			"tag":              "#d20f39",
			"embedded":         "#4c4f69",
		},
		Files: map[string]string{
			"dir":     "#094cd2",
			"default": "#4c4f69",
			"go":      "#0f5f64",
			"md":      "#28641b",
			"toml":    "#7c4f10",
			"json":    "#7c4f10",
			"yaml":    "#7c4f10",
			"lock":    "#64697d",
		},
	}
}
