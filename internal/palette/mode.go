package palette

import tea "github.com/charmbracelet/bubbletea"

// Item is one ranked result row produced by a Mode. Title is the primary label
// the palette renders and highlights; Spans are the rune indices within Title
// that matched the query (from internal/fuzzy), so the rendered highlight lines
// up exactly with what the scorer rewarded. Detail is an optional dim suffix
// (shortcut, owner, …). Msg is the tea.Msg the palette emits when this item is
// activated — the palette executes nothing itself, it only dispatches.
type Item struct {
	Title  string
	Detail string
	Spans  []int
	Score  int
	Msg    tea.Msg
}

// Mode is a palette sub-mode selected by a single leading prefix rune. It turns
// a query (already stripped of the prefix) into ranked Items for the current
// Context. The palette core is prefix-agnostic: adding a mode is registering one
// more Mode. A Mode produces a fully ranked list (best first); the palette caps
// and renders it.
type Mode interface {
	// Prefix is the leading rune that selects this mode (e.g. ':' or '@').
	Prefix() rune
	// Placeholder is the hint shown while the query body is empty.
	Placeholder() string
	// Results returns ranked Items for query in cx, best first.
	Results(query string, cx Context) []Item
}

// RunCommandMsg is emitted when a command-mode item is activated. The root model
// resolves the id against the registry and runs it, keeping the palette free of
// any command store of its own.
type RunCommandMsg struct{ ID string }

// OpenFileMsg is emitted when a file-mode item is activated. The root model
// opens it through its normal open-file path.
type OpenFileMsg struct{ Path string }
