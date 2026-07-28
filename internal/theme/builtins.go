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
		darcula(),
		intellijLight(),
		everforestDark(),
		everforestLight(),
		ayuDark(),
		ayuMirage(),
		ayuLight(),
		githubDark(),
		githubLight(),
		oxocarbon(),
		monokaiPro(),
		zenburn(),
		highContrastDark(),
		highContrastLight(),
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

// darcula ports JetBrains' IntelliJ Darcula scheme. Anchors are the canonical
// editor values (Surface #2b2b2b, Panel #3c3f41, Foreground #a9b7c6, keyword
// #cc7832, string #6a8759, number #6897bb, comment #808080). Slots that
// deviate from a canonical value carry a comment saying why.
func darcula() Theme {
	return Theme{
		Name: "darcula",
		Dark: true,
		UI: UI{
			Background:      "#2b2b2b", // editor background
			Foreground:      "#a9b7c6", // default text
			Surface:         "#2b2b2b",
			Panel:           "#3c3f41", // tool window / status bar background
			Border:          "#555555", // panel separator
			BorderFocus:     "#4a88c7", // focus ring blue
			Selection:       "#214283", // editor selection
			SelectionText:   "#a9b7c6",
			SelectionMuted:  "#323232", // caret row
			OccurrenceRead:  "#344134", // identifier under caret
			OccurrenceWrite: "#40332b", // identifier under caret (write)
			InlayHint:       "#959595", // comment gray #808080 lightened for 3.5:1 over overlays
			Whitespace:      "#505050",
			IndentGuide:     "#505050",
			Ruler:           "#323232",
			Accent:          "#ffc66d", // function yellow
			Primary:         "#2e446c", // list selection #4b6eaf pulled toward Surface (selection cap)
			Secondary:       "#da9d69", // keyword orange #cc7832 lightened for AA on Panel
			Success:         "#6cba76", // IDE green #499c54 lightened for AA on Panel
			Warning:         "#bbb529", // annotation yellow
			Error:           "#dd9795", // error red #bc3f3c lightened for AA on Panel
			Info:            "#8aaeca", // number blue #6897bb lightened for AA on Panel
			Hint:            "#86b57b", // doc-comment green #629755 lightened for AA on Panel
			MoveSource:      "#cf5b56",
			DropTarget:      "#bbb529",
			Ghost:           "#cc7832",
			ScrollbarTrack:  "#3c3f41",
			ScrollbarThumb:  "#595b5d",
			DiffAdded:       "#294436", // diff added line
			DiffRemoved:     "#3d3f3f", // diff deleted #484a4a pulled toward Surface (overlay cap)
			DiffChanged:     "#2b4155", // diff changed #385570 pulled toward Surface (overlay cap)
			VCSModified:     "#8aaeca", // VCS modified blue #6897bb lightened for AA on Panel
			VCSAdded:        "#86b57b", // VCS added green #629755 lightened for AA on Panel
			VCSUntracked:    "#bbb529",
			VCSDeleted:      "#959595", // comment gray #808080 lightened for 3.5:1 over overlays
			VCSConflicted:   "#dd9795", // error red #bc3f3c lightened for AA on Panel
		},
		Captures: map[string]string{
			"keyword":          "#cf7e3b", // #cc7832 lightened for AA on Surface
			"operator":         "#a9b7c6",
			"string":           "#7b9b68", // #6a8759 lightened for AA on Surface
			"number":           "#6897bb",
			"comment":          "#808080",
			"function":         "#ffc66d",
			"type":             "#a9b7c6", // Darcula leaves class names default
			"constant":         "#a588b5", // static field purple #9876aa lightened for AA
			"constant.builtin": "#a588b5",
			"variable":         "#a9b7c6",
			"variable.builtin": "#cf7e3b", // this/self keyword orange, lightened for AA
			"property":         "#a588b5", // instance field purple #9876aa lightened for AA
			"label":            "#cf7e3b",
			"attribute":        "#bbb529", // annotation yellow
			"punctuation":      "#a9b7c6",
			"escape":           "#cf7e3b", // valid escape orange, lightened for AA
			"boolean":          "#cf7e3b",
			"tag":              "#e8bf6a", // HTML tag yellow
			"embedded":         "#a9b7c6",
		},
		Files: map[string]string{
			"dir":     "#8aaeca", // number blue lightened for AA on Panel
			"default": "#a9b7c6",
			"go":      "#86b57b", // doc green lightened for AA on Panel
			"md":      "#98b189", // string green lightened for AA on Panel
			"toml":    "#bbb529",
			"json":    "#bbb529",
			"yaml":    "#bbb529",
			"lock":    "#959595", // lightened for 3.5:1 over overlays
		},
	}
}

// intellijLight ports JetBrains' IntelliJ Light scheme. Anchors are the
// canonical editor values (Surface #ffffff, Foreground #000000, keyword
// #0033b3, string #067d17, number #1750eb, comment #8c8c8c). Slots that
// deviate from a canonical value carry a comment saying why.
func intellijLight() Theme {
	return Theme{
		Name: "intellij-light",
		Dark: false,
		UI: UI{
			Background:      "#f2f2f2", // tool window background
			Foreground:      "#000000",
			Surface:         "#ffffff", // editor background
			Panel:           "#f2f2f2",
			Border:          "#d1d1d1", // panel separator
			BorderFocus:     "#3574f0", // focus ring blue
			Selection:       "#b3d8ff", // editor selection #a6d2ff lightened toward Surface (selection cap)
			SelectionText:   "#000000",
			SelectionMuted:  "#fcfaed", // caret row
			OccurrenceRead:  "#edebfc", // identifier under caret
			OccurrenceWrite: "#fce8f4", // identifier under caret (write)
			InlayHint:       "#808080", // comment gray #8c8c8c darkened for 3.5:1 on Panel
			Whitespace:      "#adadad",
			IndentGuide:     "#d3d3d3",
			Ruler:           "#f5f5f5",
			Accent:          "#0033b3", // keyword blue
			Primary:         "#d4e2ff", // completion selected row
			Secondary:       "#871094", // field purple
			Success:         "#067d17", // string green
			Warning:         "#806e0b", // metadata gold #9e880d darkened for AA on Panel
			Error:           "#df0000", // error red #f50000 darkened for AA on Panel
			Info:            "#1750eb", // number blue
			Hint:            "#00627a", // function teal
			MoveSource:      "#db3b4b",
			DropTarget:      "#9e880d",
			Ghost:           "#c77800",
			ScrollbarTrack:  "#f2f2f2",
			ScrollbarThumb:  "#a6a6a6",
			DiffAdded:       "#d4f0d4",
			DiffRemoved:     "#f5d8d8",
			DiffChanged:     "#f0ecd7",
			VCSModified:     "#0032a0", // VCS modified blue
			VCSAdded:        "#067d17",
			VCSUntracked:    "#806e0b", // metadata gold darkened for AA on Panel
			VCSDeleted:      "#616161",
			VCSConflicted:   "#a90f21",
		},
		Captures: map[string]string{
			"keyword":          "#0033b3",
			"operator":         "#000000",
			"string":           "#067d17",
			"number":           "#1750eb",
			"comment":          "#888888", // #8c8c8c darkened for 3.5:1 on Surface
			"function":         "#00627a",
			"type":             "#000000", // IntelliJ Light leaves class names default
			"constant":         "#871094", // static field purple
			"constant.builtin": "#871094",
			"variable":         "#000000",
			"variable.builtin": "#0033b3", // this/self keyword blue
			"property":         "#871094", // instance field purple
			"label":            "#0033b3",
			"attribute":        "#174ad4", // HTML attribute blue
			"punctuation":      "#000000",
			"escape":           "#0037a6", // valid escape blue
			"boolean":          "#0033b3",
			"tag":              "#0033b3",
			"embedded":         "#000000",
		},
		Files: map[string]string{
			"dir":     "#1750eb",
			"default": "#000000",
			"go":      "#00627a",
			"md":      "#067d17",
			"toml":    "#806e0b", // metadata gold darkened for AA on Panel
			"json":    "#806e0b",
			"yaml":    "#806e0b",
			"lock":    "#808080", // darkened for 3.5:1 on Panel
		},
	}
}

// everforestDark ports sainnhe/everforest (dark, medium). Anchors are the
// canonical palette (bg0 #2d353b, fg #d3c6aa, red #e67e80, orange #e69875,
// yellow #dbbc7f, green #a7c080, aqua #83c092, blue #7fbbb3, purple #d699b6).
// Slots deviating from a canonical value carry a comment saying why.
func everforestDark() Theme {
	return Theme{
		Name: "everforest-dark",
		Dark: true,
		UI: UI{
			Background:      "#232a2e", // bg_dim
			Foreground:      "#d3c6aa", // fg
			Surface:         "#2d353b", // bg0
			Panel:           "#343f44", // bg1
			Border:          "#475258", // bg3
			BorderFocus:     "#a7c080", // green
			Selection:       "#543a48", // bg_visual
			SelectionText:   "#d3c6aa",
			SelectionMuted:  "#3d484d", // bg2
			OccurrenceRead:  "#354a55", // bg_blue, pulled toward Surface from #3a515d (overlay cap)
			OccurrenceWrite: "#48473f", // bg_yellow, pulled toward Surface from #4d4c43 (overlay cap)
			InlayHint:       "#89968d", // grey1, lightened for contrast from #859289
			Whitespace:      "#475258",
			IndentGuide:     "#475258",
			Ruler:           "#343f44",
			Accent:          "#dbbc7f", // yellow
			Primary:         "#425047", // bg_green
			Secondary:       "#e69875", // orange
			Success:         "#a7c080", // green
			Warning:         "#dbbc7f", // yellow
			Error:           "#e98f91", // red, lightened for contrast from #e67e80
			Info:            "#7fbbb3", // blue
			Hint:            "#83c092", // aqua
			MoveSource:      "#e67e80",
			DropTarget:      "#dbbc7f",
			Ghost:           "#e69875",
			ScrollbarTrack:  "#343f44",
			ScrollbarThumb:  "#4f585e", // bg4
			DiffAdded:       "#3d4a42", // bg_green, pulled toward Surface from #425047 (overlay cap)
			DiffRemoved:     "#514045", // bg_red
			DiffChanged:     "#354a55", // bg_blue, pulled toward Surface from #3a515d (overlay cap)
			VCSModified:     "#7fbbb3",
			VCSAdded:        "#a7c080",
			VCSUntracked:    "#dbbc7f",
			VCSDeleted:      "#89968d", // grey1, lightened for contrast from #859289
			VCSConflicted:   "#e98f91", // lightened for contrast from #e67e80
		},
		Captures: map[string]string{
			"keyword":          "#e67e80", // red
			"operator":         "#e69875", // orange
			"string":           "#a7c080", // green
			"number":           "#d699b6", // purple
			"comment":          "#859289", // grey1
			"function":         "#83c092", // aqua
			"type":             "#dbbc7f", // yellow
			"constant":         "#83c092", // aqua
			"constant.builtin": "#83c092",
			"variable":         "#d3c6aa",
			"variable.builtin": "#d699b6", // purple
			"property":         "#7fbbb3", // blue
			"label":            "#e69875",
			"attribute":        "#dbbc7f",
			"punctuation":      "#9da9a0", // grey2
			"escape":           "#e69875",
			"boolean":          "#d699b6",
			"tag":              "#e67e80",
			"embedded":         "#d3c6aa",
		},
		Files: map[string]string{
			"dir":     "#7fbbb3",
			"default": "#d3c6aa",
			"go":      "#83c092",
			"md":      "#a7c080",
			"toml":    "#dbbc7f",
			"json":    "#dbbc7f",
			"yaml":    "#dbbc7f",
			"lock":    "#89968d", // lightened for contrast from #859289
		},
	}
}

// everforestLight ports sainnhe/everforest (light, medium). Anchors are the
// canonical palette (bg0 #fdf6e3, fg #5c6a72, red #f85552, orange #f57d26,
// yellow #dfa000, green #8da101, aqua #35a77c, blue #3a94c5, purple #df69ba).
// Slots deviating from a canonical value carry a comment saying why.
func everforestLight() Theme {
	return Theme{
		Name: "everforest-light",
		Dark: false,
		UI: UI{
			Background:      "#f2efdf", // bg_dim
			Foreground:      "#5c6a72", // fg
			Surface:         "#fdf6e3", // bg0
			Panel:           "#efebd4", // bg2
			Border:          "#e0dcc7", // bg4
			BorderFocus:     "#8da101", // green
			Selection:       "#eaedc8", // bg_visual
			SelectionText:   "#5a6870", // darkened for contrast from #5c6a72
			SelectionMuted:  "#f4f0d9", // bg1
			OccurrenceRead:  "#ecf5ed", // bg_blue
			OccurrenceWrite: "#faedcd", // bg_yellow
			InlayHint:       "#727f6f", // grey1, darkened for contrast from #939f91
			Whitespace:      "#e0dcc7",
			IndentGuide:     "#e0dcc7",
			Ruler:           "#efebd4",
			Accent:          "#896300", // yellow, darkened for contrast from #dfa000
			Primary:         "#dde8cb", // deepened bg_green so the row reads selected
			Secondary:       "#ad4d08", // orange, darkened for contrast from #f57d26
			Success:         "#627001", // green, darkened for contrast from #8da101
			Warning:         "#896300", // yellow, darkened for contrast from #dfa000
			Error:           "#d50c09", // red, darkened for contrast from #f85552
			Info:            "#2c7096", // blue, darkened for contrast from #3a94c5
			Hint:            "#267758", // aqua, darkened for contrast from #35a77c
			MoveSource:      "#f85552",
			DropTarget:      "#dfa000",
			Ghost:           "#f57d26",
			ScrollbarTrack:  "#efebd4",
			ScrollbarThumb:  "#a6b0a0", // grey0
			DiffAdded:       "#e5efda", // bg_green tint
			DiffRemoved:     "#fbe3da", // bg_red
			DiffChanged:     "#faedcd", // bg_yellow
			VCSModified:     "#2c7096", // darkened for contrast from #3a94c5
			VCSAdded:        "#627001", // darkened for contrast from #8da101
			VCSUntracked:    "#896300", // darkened for contrast from #dfa000
			VCSDeleted:      "#727f6f", // grey1, darkened for contrast from #939f91
			VCSConflicted:   "#d50c09", // darkened for contrast from #f85552
		},
		Captures: map[string]string{
			"keyword":          "#e10d09", // red, darkened for contrast from #f85552
			"operator":         "#b95309", // orange, darkened for contrast from #f57d26
			"string":           "#697801", // green, darkened for contrast from #8da101
			"number":           "#c92b98", // purple, darkened for contrast from #df69ba
			"comment":          "#788776", // grey1, darkened for contrast from #939f91
			"function":         "#287f5e", // aqua, darkened for contrast from #35a77c
			"type":             "#946a00", // yellow, darkened for contrast from #dfa000
			"constant":         "#287f5e", // aqua, darkened for contrast from #35a77c
			"constant.builtin": "#287f5e", // darkened for contrast from #35a77c
			"variable":         "#5c6a72",
			"variable.builtin": "#c92b98", // purple, darkened for contrast from #df69ba
			"property":         "#2f789f", // blue, darkened for contrast from #3a94c5
			"label":            "#b95309", // darkened for contrast from #f57d26
			"attribute":        "#946a00", // darkened for contrast from #dfa000
			"punctuation":      "#778776", // grey2, darkened for contrast from #829181
			"escape":           "#b95309", // darkened for contrast from #f57d26
			"boolean":          "#c92b98", // darkened for contrast from #df69ba
			"tag":              "#e10d09", // darkened for contrast from #f85552
			"embedded":         "#5c6a72",
		},
		Files: map[string]string{
			"dir":     "#2c7096", // darkened for contrast from #3a94c5
			"default": "#5c6a72",
			"go":      "#267758", // darkened for contrast from #35a77c
			"md":      "#627001", // darkened for contrast from #8da101
			"toml":    "#896300", // darkened for contrast from #dfa000
			"json":    "#896300", // darkened for contrast from #dfa000
			"yaml":    "#896300", // darkened for contrast from #dfa000
			"lock":    "#727f6f", // darkened for contrast from #939f91
		},
	}
}

// ayuDark ports ayu-theme/ayu-colors (dark). Anchors are the canonical
// editor values (bg #0b0e14, fg #bfbdb6, accent #e6b450, keyword #ff8f40,
// string #aad94c, function #ffb454, entity #59c2ff, constant #d2a6ff).
// Slots deviating from a canonical value carry a comment saying why.
func ayuDark() Theme {
	return Theme{
		Name: "ayu-dark",
		Dark: true,
		UI: UI{
			Background:      "#0b0e14",
			Foreground:      "#bfbdb6",
			Surface:         "#0b0e14",
			Panel:           "#0f131a", // panel background
			Border:          "#2d3440", // guide/line
			BorderFocus:     "#e6b450", // accent
			Selection:       "#1f3244", // selection (solid form of #409fff alpha), pulled toward Surface from #253c52 (overlay cap)
			SelectionText:   "#bfbdb6",
			SelectionMuted:  "#131721", // line highlight
			OccurrenceRead:  "#132639",
			OccurrenceWrite: "#2b2620",
			InlayHint:       "#7a828e",
			Whitespace:      "#2d3440",
			IndentGuide:     "#2d3440",
			Ruler:           "#0f131a",
			Accent:          "#e6b450", // accent
			Primary:         "#1f3244", // pulled toward Surface (overlay cap) from #253c52
			Secondary:       "#ff8f40", // keyword orange
			Success:         "#7fd962", // vcs added
			Warning:         "#e6b450",
			Error:           "#d95757", // error
			Info:            "#73b8ff", // vcs modified
			Hint:            "#95e6cb", // regexp teal
			MoveSource:      "#f26d78", // vcs removed
			DropTarget:      "#e6b450",
			Ghost:           "#ff8f40",
			ScrollbarTrack:  "#0f131a",
			ScrollbarThumb:  "#2d3440",
			DiffAdded:       "#142117",
			DiffRemoved:     "#241317",
			DiffChanged:     "#1f1c10",
			VCSModified:     "#73b8ff",
			VCSAdded:        "#7fd962",
			VCSUntracked:    "#e6b450",
			VCSDeleted:      "#646c77", // lightened for contrast from #636b76
			VCSConflicted:   "#f26d78",
		},
		Captures: map[string]string{
			"keyword":          "#ff8f40",
			"operator":         "#f29668",
			"string":           "#aad94c",
			"number":           "#d2a6ff",
			"comment":          "#636b76", // solid form of the alpha comment gray
			"function":         "#ffb454",
			"type":             "#59c2ff", // entity blue
			"constant":         "#d2a6ff",
			"constant.builtin": "#d2a6ff",
			"variable":         "#bfbdb6",
			"variable.builtin": "#f07178", // markup red
			"property":         "#39bae6", // tag blue
			"label":            "#ff8f40",
			"attribute":        "#ffb454",
			"punctuation":      "#8a9199", // ui fg
			"escape":           "#95e6cb",
			"boolean":          "#d2a6ff",
			"tag":              "#39bae6",
			"embedded":         "#bfbdb6",
		},
		Files: map[string]string{
			"dir":     "#59c2ff",
			"default": "#bfbdb6",
			"go":      "#95e6cb",
			"md":      "#aad94c",
			"toml":    "#e6b450",
			"json":    "#e6b450",
			"yaml":    "#e6b450",
			"lock":    "#8a9199",
		},
	}
}

// ayuMirage ports ayu-theme/ayu-colors (mirage) — the mid-dark ground that
// sits between the dark and light tiers. Anchors: bg #1f2430, fg #cccac2,
// accent #ffcc66, keyword #ffad66, string #d5ff80, entity #73d0ff.
// Slots deviating from a canonical value carry a comment saying why.
func ayuMirage() Theme {
	return Theme{
		Name: "ayu-mirage",
		Dark: true,
		UI: UI{
			Background:      "#171b24", // panel background
			Foreground:      "#cccac2",
			Surface:         "#1f2430",
			Panel:           "#171b24",
			Border:          "#3a4151",
			BorderFocus:     "#ffcc66", // accent
			Selection:       "#32405c", // selection, pulled toward Surface from #33415e (overlay cap)
			SelectionText:   "#cccac2",
			SelectionMuted:  "#242936", // line highlight
			OccurrenceRead:  "#223142",
			OccurrenceWrite: "#35302a",
			InlayHint:       "#707a8c", // ui fg
			Whitespace:      "#3a4151",
			IndentGuide:     "#3a4151",
			Ruler:           "#242936",
			Accent:          "#ffcc66", // accent
			Primary:         "#32405c", // pulled toward Surface (overlay cap) from #33415e
			Secondary:       "#ffad66", // keyword orange
			Success:         "#87d96c", // vcs added
			Warning:         "#ffcc66",
			Error:           "#ff6666", // error
			Info:            "#80bfff", // vcs modified
			Hint:            "#95e6cb", // regexp teal
			MoveSource:      "#f27983", // vcs removed
			DropTarget:      "#ffcc66",
			Ghost:           "#ffad66",
			ScrollbarTrack:  "#171b24",
			ScrollbarThumb:  "#3a4151",
			DiffAdded:       "#26362a",
			DiffRemoved:     "#3b2a30",
			DiffChanged:     "#35322a",
			VCSModified:     "#80bfff",
			VCSAdded:        "#87d96c",
			VCSUntracked:    "#ffcc66",
			VCSDeleted:      "#707a8c",
			VCSConflicted:   "#f27983",
		},
		Captures: map[string]string{
			"keyword":          "#ffad66",
			"operator":         "#f29e74",
			"string":           "#d5ff80",
			"number":           "#dfbfff",
			"comment":          "#6d7a89", // solid form of the alpha comment gray, lightened for contrast from #5c6773
			"function":         "#ffd173",
			"type":             "#73d0ff", // entity blue
			"constant":         "#dfbfff",
			"constant.builtin": "#dfbfff",
			"variable":         "#cccac2",
			"variable.builtin": "#f28779", // markup red
			"property":         "#5ccfe6", // tag blue
			"label":            "#ffad66",
			"attribute":        "#ffd173",
			"punctuation":      "#8a919e",
			"escape":           "#95e6cb",
			"boolean":          "#dfbfff",
			"tag":              "#5ccfe6",
			"embedded":         "#cccac2",
		},
		Files: map[string]string{
			"dir":     "#73d0ff",
			"default": "#cccac2",
			"go":      "#5ccfe6",
			"md":      "#d5ff80",
			"toml":    "#ffcc66",
			"json":    "#ffcc66",
			"yaml":    "#ffcc66",
			"lock":    "#707a8c",
		},
	}
}

// ayuLight ports ayu-theme/ayu-colors (light). Anchors: bg #fcfcfc,
// fg #5c6166, accent #ffaa33, keyword #fa8d3e, string #86b300,
// entity #399ee6, constant #a37acc.
// Slots deviating from a canonical value carry a comment saying why.
func ayuLight() Theme {
	return Theme{
		Name: "ayu-light",
		Dark: false,
		UI: UI{
			Background:      "#f8f9fa", // panel background
			Foreground:      "#5c6166",
			Surface:         "#fcfcfc",
			Panel:           "#eceef0",
			Border:          "#d0d3d6",
			BorderFocus:     "#ffaa33", // accent
			Selection:       "#d9e9fb", // selection (solid form of #035bd6 alpha)
			SelectionText:   "#5c6166",
			SelectionMuted:  "#f3f4f5", // line highlight
			OccurrenceRead:  "#e6f2fb",
			OccurrenceWrite: "#fdf2e2",
			InlayHint:       "#757e87", // ui fg, darkened for contrast from #8a9199
			Whitespace:      "#d0d3d6",
			IndentGuide:     "#d0d3d6",
			Ruler:           "#f3f4f5",
			Accent:          "#9d5c00", // accent, darkened for contrast from #ffaa33
			Primary:         "#cfe3fa",
			Secondary:       "#b34e05", // keyword orange, darkened for contrast from #fa8d3e
			Success:         "#437729", // vcs added, darkened for contrast from #6cbf43
			Warning:         "#975f0b", // function gold, darkened for contrast from #f2ae49
			Error:           "#d41e1e", // error, darkened for contrast from #e65050
			Info:            "#306fae", // vcs modified, darkened for contrast from #478acc
			Hint:            "#2b785f", // regexp teal, darkened for contrast from #4cbf99
			MoveSource:      "#ff7383", // vcs removed
			DropTarget:      "#ffaa33",
			Ghost:           "#fa8d3e",
			ScrollbarTrack:  "#f3f4f5",
			ScrollbarThumb:  "#aab0b6",
			DiffAdded:       "#e0f0d4",
			DiffRemoved:     "#fbe0e0",
			DiffChanged:     "#faeed6",
			VCSModified:     "#306fae", // darkened for contrast from #478acc
			VCSAdded:        "#437729", // darkened for contrast from #6cbf43
			VCSUntracked:    "#975f0b", // darkened for contrast from #f2ae49
			VCSDeleted:      "#757e87", // darkened for contrast from #8a9199
			VCSConflicted:   "#d90019", // darkened for contrast from #ff7383
		},
		Captures: map[string]string{
			"keyword":          "#c15405", // darkened for contrast from #fa8d3e
			"operator":         "#c45117", // darkened for contrast from #ed9366
			"string":           "#5e7e00", // darkened for contrast from #86b300
			"number":           "#8f5dc1", // darkened for contrast from #a37acc
			"comment":          "#787b80",
			"function":         "#a5670c", // darkened for contrast from #f2ae49
			"type":             "#1879be", // entity blue, darkened for contrast from #399ee6
			"constant":         "#8f5dc1", // darkened for contrast from #a37acc
			"constant.builtin": "#8f5dc1", // darkened for contrast from #a37acc
			"variable":         "#5c6166",
			"variable.builtin": "#e71818", // markup red, darkened for contrast from #f07171
			"property":         "#277d9a", // tag blue, darkened for contrast from #55b4d4
			"label":            "#c15405", // darkened for contrast from #fa8d3e
			"attribute":        "#a5670c", // darkened for contrast from #f2ae49
			"punctuation":      "#808890", // darkened for contrast from #8a9199
			"escape":           "#2e8166", // darkened for contrast from #4cbf99
			"boolean":          "#8f5dc1", // darkened for contrast from #a37acc
			"tag":              "#277d9a", // darkened for contrast from #55b4d4
			"embedded":         "#5c6166",
		},
		Files: map[string]string{
			"dir":     "#166faf", // darkened for contrast from #399ee6
			"default": "#5c6166",
			"go":      "#2b785f", // darkened for contrast from #4cbf99
			"md":      "#587600", // darkened for contrast from #86b300
			"toml":    "#975f0b", // darkened for contrast from #f2ae49
			"json":    "#975f0b", // darkened for contrast from #f2ae49
			"yaml":    "#975f0b", // darkened for contrast from #f2ae49
			"lock":    "#757e87", // darkened for contrast from #8a9199
		},
	}
}

// githubDark ports GitHub's Primer dark scheme, whose upstream values are
// themselves contrast-audited. Anchors: canvas #0d1117, fg #e6edf3,
// keyword #ff7b72, string #a5d6ff, constant #79c0ff, function #d2a8ff.
// Slots deviating from a canonical value carry a comment saying why.
func githubDark() Theme {
	return Theme{
		Name: "github-dark",
		Dark: true,
		UI: UI{
			Background:      "#010409", // canvas inset
			Foreground:      "#e6edf3",
			Surface:         "#0d1117", // canvas default
			Panel:           "#161b22", // canvas subtle
			Border:          "#30363d",
			BorderFocus:     "#58a6ff", // accent
			Selection:       "#1b3252", // solid form of the #388bfd alpha selection, pulled toward Surface from #1f3a5f (overlay cap)
			SelectionText:   "#e6edf3",
			SelectionMuted:  "#161b22",
			OccurrenceRead:  "#142a45",
			OccurrenceWrite: "#342a18",
			InlayHint:       "#8b949e", // fg muted
			Whitespace:      "#30363d",
			IndentGuide:     "#30363d",
			Ruler:           "#161b22",
			Accent:          "#58a6ff", // accent
			Primary:         "#1b3252", // pulled toward Surface (overlay cap) from #1f3a5f
			Secondary:       "#ffa657", // variable orange
			Success:         "#3fb950",
			Warning:         "#d29922", // attention
			Error:           "#f85149", // danger
			Info:            "#58a6ff",
			Hint:            "#39c5cf", // scale cyan
			MoveSource:      "#f85149",
			DropTarget:      "#d29922",
			Ghost:           "#ffa657",
			ScrollbarTrack:  "#161b22",
			ScrollbarThumb:  "#30363d",
			DiffAdded:       "#122117", // solid form of the diff green alpha
			DiffRemoved:     "#25171c",
			DiffChanged:     "#221d10",
			VCSModified:     "#58a6ff",
			VCSAdded:        "#3fb950",
			VCSUntracked:    "#d29922",
			VCSDeleted:      "#8b949e",
			VCSConflicted:   "#f85149",
		},
		Captures: map[string]string{
			"keyword":          "#ff7b72",
			"operator":         "#e6edf3",
			"string":           "#a5d6ff",
			"number":           "#79c0ff",
			"comment":          "#8b949e",
			"function":         "#d2a8ff",
			"type":             "#7ee787", // entity green
			"constant":         "#79c0ff",
			"constant.builtin": "#79c0ff",
			"variable":         "#ffa657",
			"variable.builtin": "#ff7b72",
			"property":         "#79c0ff",
			"label":            "#ff7b72",
			"attribute":        "#79c0ff",
			"punctuation":      "#8b949e",
			"escape":           "#79c0ff",
			"boolean":          "#79c0ff",
			"tag":              "#7ee787",
			"embedded":         "#e6edf3",
		},
		Files: map[string]string{
			"dir":     "#58a6ff",
			"default": "#e6edf3",
			"go":      "#39c5cf",
			"md":      "#7ee787",
			"toml":    "#d29922",
			"json":    "#d29922",
			"yaml":    "#d29922",
			"lock":    "#8b949e",
		},
	}
}

// githubLight ports GitHub's Primer light scheme. Anchors: canvas #ffffff,
// fg #1f2328, keyword #cf222e, string #0a3069, constant #0550ae,
// function #8250df, tag #116329.
// Slots deviating from a canonical value carry a comment saying why.
func githubLight() Theme {
	return Theme{
		Name: "github-light",
		Dark: false,
		UI: UI{
			Background:      "#f6f8fa", // canvas subtle
			Foreground:      "#1f2328",
			Surface:         "#ffffff", // canvas default
			Panel:           "#f6f8fa",
			Border:          "#d0d7de",
			BorderFocus:     "#0969da", // accent
			Selection:       "#b6e3ff", // scale blue 1
			SelectionText:   "#1f2328",
			SelectionMuted:  "#f6f8fa",
			OccurrenceRead:  "#ddf4ff", // scale blue 0
			OccurrenceWrite: "#fff8c5", // scale yellow 0
			InlayHint:       "#6e7781", // fg muted
			Whitespace:      "#afb8c1",
			IndentGuide:     "#d8dee4",
			Ruler:           "#f6f8fa",
			Accent:          "#0969da", // accent
			Primary:         "#b6e3ff",
			Secondary:       "#953800", // variable rust
			Success:         "#1a7f37",
			Warning:         "#9a6700", // attention
			Error:           "#cf222e", // danger
			Info:            "#0969da",
			Hint:            "#1b7c83", // scale cyan
			MoveSource:      "#cf222e",
			DropTarget:      "#9a6700",
			Ghost:           "#bc4c00", // severe
			ScrollbarTrack:  "#f6f8fa",
			ScrollbarThumb:  "#afb8c1",
			DiffAdded:       "#d8f3dc",
			DiffRemoved:     "#ffd7d5",
			DiffChanged:     "#fff3c2",
			VCSModified:     "#0969da",
			VCSAdded:        "#1a7f37",
			VCSUntracked:    "#9a6700",
			VCSDeleted:      "#6e7781",
			VCSConflicted:   "#cf222e",
		},
		Captures: map[string]string{
			"keyword":          "#cf222e",
			"operator":         "#1f2328",
			"string":           "#0a3069",
			"number":           "#0550ae",
			"comment":          "#6e7781",
			"function":         "#8250df",
			"type":             "#6639ba", // entity purple
			"constant":         "#0550ae",
			"constant.builtin": "#0550ae",
			"variable":         "#953800",
			"variable.builtin": "#cf222e",
			"property":         "#0550ae",
			"label":            "#cf222e",
			"attribute":        "#0550ae",
			"punctuation":      "#6e7781",
			"escape":           "#0550ae",
			"boolean":          "#0550ae",
			"tag":              "#116329",
			"embedded":         "#1f2328",
		},
		Files: map[string]string{
			"dir":     "#0969da",
			"default": "#1f2328",
			"go":      "#1b7c83",
			"md":      "#1a7f37",
			"toml":    "#9a6700",
			"json":    "#9a6700",
			"yaml":    "#9a6700",
			"lock":    "#6e7781",
		},
	}
}

// oxocarbon ports nyoom-engineering/oxocarbon.nvim (IBM Carbon): cold
// violet-cyan on true black, following the base16 slot conventions
// (base00 #161616 ground, base05 #f2f4f8 text, base0E #be95ff keywords,
// base0B #33b1ff strings, base0D #42be65 functions, base0A #ee5396 types).
// The Carbon palette has no yellow, so the warm slots reuse its pinks —
// noted per slot. Slots deviating from a canonical value carry a comment.
func oxocarbon() Theme {
	return Theme{
		Name: "oxocarbon",
		Dark: true,
		UI: UI{
			Background:      "#161616", // base00
			Foreground:      "#f2f4f8", // base05
			Surface:         "#161616",
			Panel:           "#262626", // base01
			Border:          "#393939", // base02
			BorderFocus:     "#33b1ff", // base0B
			Selection:       "#363636", // base02, pulled toward Surface from #393939 (overlay cap)
			SelectionText:   "#f2f4f8",
			SelectionMuted:  "#262626", // base01
			OccurrenceRead:  "#1c2733",
			OccurrenceWrite: "#33212b",
			InlayHint:       "#7a7a7a", // lightened for contrast from #6f6f6f
			Whitespace:      "#393939",
			IndentGuide:     "#393939",
			Ruler:           "#262626",
			Accent:          "#3ddbd9", // base08
			Primary:         "#363636", // pulled toward Surface (overlay cap) from #393939
			Secondary:       "#ff7eb6", // base0C
			Success:         "#42be65", // base0D
			Warning:         "#ff7eb6", // base0C — Carbon has no yellow
			Error:           "#ee5396", // base0A
			Info:            "#78a9ff", // base09
			Hint:            "#08bdba", // base07
			MoveSource:      "#ee5396",
			DropTarget:      "#82cfff", // base0F — no yellow in the palette
			Ghost:           "#be95ff", // base0E
			ScrollbarTrack:  "#262626",
			ScrollbarThumb:  "#525252", // base03
			DiffAdded:       "#16281c",
			DiffRemoved:     "#2d1a24",
			DiffChanged:     "#252031",
			VCSModified:     "#78a9ff",
			VCSAdded:        "#42be65",
			VCSUntracked:    "#ff7eb6", // no yellow in the palette
			VCSDeleted:      "#8d8d8d",
			VCSConflicted:   "#ee5396",
		},
		Captures: map[string]string{
			"keyword":          "#be95ff", // base0E
			"operator":         "#dde1e6", // base04
			"string":           "#33b1ff", // base0B
			"number":           "#78a9ff", // base09
			"comment":          "#6e6e6e", // base03, lightened for contrast from #525252
			"function":         "#42be65", // base0D
			"type":             "#ee5396", // base0A
			"constant":         "#ff7eb6", // base0C
			"constant.builtin": "#ff7eb6",
			"variable":         "#3ddbd9", // base08
			"variable.builtin": "#ee5396",
			"property":         "#08bdba", // base07
			"label":            "#be95ff",
			"attribute":        "#78a9ff",
			"punctuation":      "#dde1e6",
			"escape":           "#ff7eb6",
			"boolean":          "#78a9ff",
			"tag":              "#ee5396",
			"embedded":         "#f2f4f8",
		},
		Files: map[string]string{
			"dir":     "#78a9ff",
			"default": "#f2f4f8",
			"go":      "#3ddbd9",
			"md":      "#42be65",
			"toml":    "#82cfff",
			"json":    "#82cfff",
			"yaml":    "#82cfff",
			"lock":    "#7a7a7a", // lightened for contrast from #6f6f6f
		},
	}
}

// monokaiPro ports the Monokai Pro default filter. Anchors: bg #2d2a2e,
// fg #fcfcfa, red #ff6188, orange #fc9867, yellow #ffd866, green #a9dc76,
// cyan #78dce8, purple #ab9df2, dim #727072.
// Slots deviating from a canonical value carry a comment saying why.
func monokaiPro() Theme {
	return Theme{
		Name: "monokai-pro",
		Dark: true,
		UI: UI{
			Background:      "#221f22", // darker chrome shade
			Foreground:      "#fcfcfa",
			Surface:         "#2d2a2e",
			Panel:           "#221f22",
			Border:          "#5b595c",
			BorderFocus:     "#ffd866", // yellow
			Selection:       "#403e41", // selection
			SelectionText:   "#fcfcfa",
			SelectionMuted:  "#363337",
			OccurrenceRead:  "#2c363b",
			OccurrenceWrite: "#3d332c",
			InlayHint:       "#939293",
			Whitespace:      "#5b595c",
			IndentGuide:     "#5b595c",
			Ruler:           "#221f22",
			Accent:          "#ffd866", // yellow
			Primary:         "#403e41",
			Secondary:       "#fc9867", // orange
			Success:         "#a9dc76", // green
			Warning:         "#ffd866", // yellow
			Error:           "#ff6188", // red
			Info:            "#78dce8", // cyan
			Hint:            "#ab9df2", // purple
			MoveSource:      "#ff6188",
			DropTarget:      "#ffd866",
			Ghost:           "#fc9867",
			ScrollbarTrack:  "#221f22",
			ScrollbarThumb:  "#5b595c",
			DiffAdded:       "#37402f",
			DiffRemoved:     "#452d36",
			DiffChanged:     "#403a2b",
			VCSModified:     "#78dce8",
			VCSAdded:        "#a9dc76",
			VCSUntracked:    "#ffd866",
			VCSDeleted:      "#939293",
			VCSConflicted:   "#ff6188",
		},
		Captures: map[string]string{
			"keyword":          "#ff6188",
			"operator":         "#ff6188",
			"string":           "#ffd866",
			"number":           "#ab9df2",
			"comment":          "#807e80", // lightened for contrast from #727072
			"function":         "#a9dc76",
			"type":             "#78dce8",
			"constant":         "#ab9df2",
			"constant.builtin": "#ab9df2",
			"variable":         "#fcfcfa",
			"variable.builtin": "#fc9867", // self/this orange
			"property":         "#fcfcfa",
			"label":            "#ff6188",
			"attribute":        "#78dce8",
			"punctuation":      "#939293",
			"escape":           "#ab9df2",
			"boolean":          "#ab9df2",
			"tag":              "#ff6188",
			"embedded":         "#fcfcfa",
		},
		Files: map[string]string{
			"dir":     "#78dce8",
			"default": "#fcfcfa",
			"go":      "#ab9df2",
			"md":      "#a9dc76",
			"toml":    "#ffd866",
			"json":    "#ffd866",
			"yaml":    "#ffd866",
			"lock":    "#939293",
		},
	}
}

// zenburn ports the classic zenburn.el palette — deliberately low-contrast
// by design, so it is the sharpest test of the #1226 rules. Anchors:
// bg #3f3f3f, fg #dcdccc, keyword #f0dfaf, string #cc9393, comment #7f9f7f,
// function #93e0e3, orange #dfaf8f, magenta #dc8cc3.
// Slots deviating from a canonical value carry a comment saying why.
func zenburn() Theme {
	return Theme{
		Name: "zenburn",
		Dark: true,
		UI: UI{
			Background:      "#2b2b2b", // bg-1 (modeline)
			Foreground:      "#dcdccc",
			Surface:         "#3f3f3f", // bg
			Panel:           "#4f4f4f", // bg+1
			Border:          "#5f5f5f", // bg+2
			BorderFocus:     "#f0dfaf", // yellow
			Selection:       "#2f2f2f", // region
			SelectionText:   "#dcdccc",
			SelectionMuted:  "#383838", // hl-line
			OccurrenceRead:  "#3a4442",
			OccurrenceWrite: "#4a4038",
			InlayHint:       "#abab9e", // lightened for contrast from #989888
			Whitespace:      "#5f5f5f",
			IndentGuide:     "#5f5f5f",
			Ruler:           "#464646",
			Accent:          "#e3b89c", // orange, lightened for contrast from #dfaf8f
			Primary:         "#3e5e5e", // dim teal selection, pulled toward Surface from #3f5f5f (overlay cap)
			Secondary:       "#f0dfaf", // yellow
			Success:         "#b3c6b3", // green, lightened for contrast from #7f9f7f
			Warning:         "#f0dfaf", // yellow
			Error:           "#deb8b8", // red, lightened for contrast from #cc9393
			Info:            "#9dc4f4", // blue+1, lightened for contrast from #94bff3
			Hint:            "#93e0e3", // cyan
			MoveSource:      "#cc9393",
			DropTarget:      "#f0dfaf",
			Ghost:           "#dfaf8f",
			ScrollbarTrack:  "#4f4f4f",
			ScrollbarThumb:  "#6f6f6f",
			DiffAdded:       "#3f4f3f",
			DiffRemoved:     "#4f3f3f",
			DiffChanged:     "#4f4f3f",
			VCSModified:     "#9dc4f4", // lightened for contrast from #94bff3
			VCSAdded:        "#a7caa7", // green+2, lightened for contrast from #9fc59f
			VCSUntracked:    "#f0dfaf",
			VCSDeleted:      "#abab9e", // lightened for contrast from #989888
			VCSConflicted:   "#deb8b8", // lightened for contrast from #cc9393
		},
		Captures: map[string]string{
			"keyword":          "#f0dfaf",
			"operator":         "#dcdccc",
			"string":           "#d19d9d", // lightened for contrast from #cc9393
			"number":           "#8cd0d3", // blue
			"comment":          "#7f9f7f", // zenburn's famous green comments
			"function":         "#93e0e3",
			"type":             "#dfdfbf",
			"constant":         "#dca3a3",
			"constant.builtin": "#dca3a3",
			"variable":         "#dfaf8f",
			"variable.builtin": "#de92c6", // lightened for contrast from #dc8cc3
			"property":         "#dcdccc",
			"label":            "#f0dfaf",
			"attribute":        "#94bff3",
			"punctuation":      "#9f9f8f",
			"escape":           "#dfaf8f",
			"boolean":          "#dca3a3",
			"tag":              "#de92c6", // lightened for contrast from #dc8cc3
			"embedded":         "#dcdccc",
		},
		Files: map[string]string{
			"dir":     "#9dc4f4", // lightened for contrast from #94bff3
			"default": "#dcdccc",
			"go":      "#93e0e3",
			"md":      "#a7caa7", // lightened for contrast from #9fc59f
			"toml":    "#f0dfaf",
			"json":    "#f0dfaf",
			"yaml":    "#f0dfaf",
			"lock":    "#abab9e", // lightened for contrast from #989888
		},
	}
}

// highContrastDark is the strict tier for dark environments (#1229): near-black
// surfaces, near-white text, and every foreground held to WCAG **AAA** (7:1) on
// every base it can land on — including the slots every other built-in ships as
// a deliberately dim class (comments, punctuation, inlay hints, deleted files,
// lock files). Trading that low-emphasis layer away is the point: for low-vision
// users, bright ambient light or a washed-out projector, legibility beats visual
// hierarchy. Overlays (selection, ruler, occurrence marks, diff lines) stay
// within 1.2:1 of Surface instead of the usual 1.35/1.5, so painting one under
// text costs almost nothing — 7:1 text over a 1.2:1 overlay is still ~5.8:1.
//
// With lightness pinned near the extremes, hue is the only channel left to tell
// the semantic slots apart, so they take widely separated hues (red / amber /
// green / cyan / blue / magenta) rather than shades of one accent.
func highContrastDark() Theme {
	return Theme{
		Name: "high-contrast-dark",
		Dark: true,
		UI: UI{
			Background:      "#000000",
			Foreground:      "#ffffff",
			Surface:         "#000000",
			Panel:           "#0d0d0d",
			Border:          "#b0b0b0", // borders are structure: bright, not hinted at
			BorderFocus:     "#ffd54f",
			Selection:       "#001a2b", // every overlay within 1.2:1 of Surface
			SelectionText:   "#ffffff",
			SelectionMuted:  "#181818",
			OccurrenceRead:  "#00141f",
			OccurrenceWrite: "#1a1200",
			InlayHint:       "#c0c0c0", // no dim class: AAA like body text
			Whitespace:      "#8a8a8a",
			IndentGuide:     "#8a8a8a",
			Ruler:           "#141414",
			Accent:          "#ffab40", // orange
			Primary:         "#001a2b",
			Secondary:       "#ff79c6", // magenta
			Success:         "#69f0ae", // green
			Warning:         "#ffd54f", // amber
			Error:           "#ff8a80", // red
			Info:            "#8ab4ff", // blue
			Hint:            "#00e5ff", // cyan
			MoveSource:      "#ff8a80",
			DropTarget:      "#ffd54f",
			Ghost:           "#ffab40",
			ScrollbarTrack:  "#3d3d3d",
			ScrollbarThumb:  "#b0b0b0",
			DiffAdded:       "#001a00",
			DiffRemoved:     "#1a0000",
			DiffChanged:     "#1a1a00",
			VCSModified:     "#8ab4ff",
			VCSAdded:        "#69f0ae",
			VCSUntracked:    "#ffd54f",
			VCSDeleted:      "#c0c0c0", // no dim class
			VCSConflicted:   "#ff8a80",
		},
		Captures: map[string]string{
			"keyword":          "#ff79c6",
			"operator":         "#ffffff",
			"string":           "#69f0ae",
			"number":           "#ffab40",
			"comment":          "#c0c0c0", // no dim class
			"function":         "#8ab4ff",
			"type":             "#00e5ff",
			"constant":         "#ffab40",
			"constant.builtin": "#ffab40",
			"variable":         "#ffffff",
			"variable.builtin": "#ff8a80",
			"property":         "#ffffff",
			"label":            "#ff79c6",
			"attribute":        "#ffd54f",
			"punctuation":      "#e0e0e0", // no dim class
			"escape":           "#ffab40",
			"boolean":          "#ffab40",
			"tag":              "#ff8a80",
			"embedded":         "#ffffff",
		},
		Files: map[string]string{
			"dir":     "#8ab4ff",
			"default": "#ffffff",
			"go":      "#00e5ff",
			"md":      "#69f0ae",
			"toml":    "#ffd54f",
			"json":    "#ffd54f",
			"yaml":    "#ffd54f",
			"lock":    "#c0c0c0", // no dim class
		},
	}
}

// highContrastLight is the inverse of highContrastDark: near-white surfaces,
// near-black text, the same AAA / no-dim-class / 1.2:1-overlay rules. The hues
// are the dark tier's, rotated to the dark end of each so they clear 7:1 on
// white.
func highContrastLight() Theme {
	return Theme{
		Name: "high-contrast-light",
		Dark: false,
		UI: UI{
			Background:      "#f2f2f2",
			Foreground:      "#000000",
			Surface:         "#ffffff",
			Panel:           "#f2f2f2",
			Border:          "#4d4d4d",
			BorderFocus:     "#7a3600",
			Selection:       "#dfefff", // every overlay within 1.2:1 of Surface
			SelectionText:   "#000000",
			SelectionMuted:  "#ebebeb",
			OccurrenceRead:  "#e0f0ff",
			OccurrenceWrite: "#fff0d0",
			InlayHint:       "#4d4d4d", // no dim class: AAA like body text
			Whitespace:      "#767676",
			IndentGuide:     "#767676",
			Ruler:           "#ebebeb",
			Accent:          "#7a3600", // orange
			Primary:         "#dfefff",
			Secondary:       "#8b1a6b", // magenta
			Success:         "#00591c", // green
			Warning:         "#6b4a00", // amber
			Error:           "#96000e", // red
			Info:            "#123a8a", // blue
			Hint:            "#005252", // cyan
			MoveSource:      "#96000e",
			DropTarget:      "#6b4a00",
			Ghost:           "#7a3600",
			ScrollbarTrack:  "#d0d0d0",
			ScrollbarThumb:  "#4d4d4d",
			DiffAdded:       "#e0ffe0",
			DiffRemoved:     "#ffe8e8",
			DiffChanged:     "#fffbd6",
			VCSModified:     "#123a8a",
			VCSAdded:        "#00591c",
			VCSUntracked:    "#6b4a00",
			VCSDeleted:      "#4d4d4d", // no dim class
			VCSConflicted:   "#96000e",
		},
		Captures: map[string]string{
			"keyword":          "#8b1a6b",
			"operator":         "#000000",
			"string":           "#00591c",
			"number":           "#7a3600",
			"comment":          "#4d4d4d", // no dim class
			"function":         "#123a8a",
			"type":             "#005252",
			"constant":         "#7a3600",
			"constant.builtin": "#7a3600",
			"variable":         "#000000",
			"variable.builtin": "#96000e",
			"property":         "#000000",
			"label":            "#8b1a6b",
			"attribute":        "#6b4a00",
			"punctuation":      "#333333", // no dim class
			"escape":           "#7a3600",
			"boolean":          "#7a3600",
			"tag":              "#96000e",
			"embedded":         "#000000",
		},
		Files: map[string]string{
			"dir":     "#123a8a",
			"default": "#000000",
			"go":      "#005252",
			"md":      "#00591c",
			"toml":    "#6b4a00",
			"json":    "#6b4a00",
			"yaml":    "#6b4a00",
			"lock":    "#4d4d4d", // no dim class
		},
	}
}
