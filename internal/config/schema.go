package config

// schema.go defines the typed configuration sections. Every section is a plain
// struct with TOML field tags; the slot maps (Explorer.Colors, Keymap.Bindings,
// LSP.Servers) are intentionally empty here — downstream roadmaps fill their
// concrete entries through extend.go, never by editing these structs.

// Config is the root configuration document. It is the only type the rest of
// IKE reads; no TOML types leak past this package.
type Config struct {
	Editor   Editor   `toml:"editor"`
	Explorer Explorer `toml:"explorer"`
	Keymap   Keymap   `toml:"keymap"`
	LSP      LSP      `toml:"lsp"`
	Theme    Theme    `toml:"theme"`
	Project  Project  `toml:"project"`
}

// Editor holds text-editing behaviour (Roadmap 0060 consumes most of it).
type Editor struct {
	TabWidth               int  `toml:"tab_width"`
	UseSpaces              bool `toml:"use_spaces"`
	LineNumbers            bool `toml:"line_numbers"`
	RelativeLineNumbers    bool `toml:"relative_line_numbers"`
	Wrap                   bool `toml:"wrap"`
	ScrollOff              int  `toml:"scroll_off"`
	AutoIndent             bool `toml:"auto_indent"`
	TrimTrailingWhitespace bool `toml:"trim_trailing_whitespace"`
	InsertFinalNewline     bool `toml:"insert_final_newline"`
	ShowWhitespace         bool `toml:"show_whitespace"`
}

// Explorer holds file-tree behaviour. Colors is a per-filetype color-name slot
// filled by Roadmap 0050.
type Explorer struct {
	ShowHidden bool              `toml:"show_hidden"`
	GitStatus  bool              `toml:"git_status"`
	TreeIndent int               `toml:"tree_indent"`
	Sort       string            `toml:"sort"`
	Colors     map[string]string `toml:"colors"`
}

// Keymap selects a binding preset and carries per-action overrides. Bindings is
// a slot filled by Roadmap 0080.
type Keymap struct {
	Preset   string            `toml:"preset"`
	Bindings map[string]string `toml:"bindings"`
}

// LSP holds language-server settings. Servers is a per-language slot filled by
// Roadmap 0100; its value type stays a free-form table so a server entry can
// carry arbitrary keys without a schema change here.
type LSP struct {
	Enabled  bool                      `toml:"enabled"`
	LogLevel string                    `toml:"log_level"`
	Servers  map[string]map[string]any `toml:"servers"`
}

// Theme selects the active palette; its contents are owned by Roadmap 0110.
type Theme struct {
	Name string `toml:"name"`
	Dark bool   `toml:"dark"`
}

// Project holds recent-project history. History is a replace-by-default list
// (see merge.go); its presentation/editing UX lives in Roadmap 0090.
type Project struct {
	History     []string `toml:"history"`
	MaxHistory  int      `toml:"max_history"`
	RestoreLast bool     `toml:"restore_last"`
}
