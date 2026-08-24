package palette

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/codepreview"
)

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
	// Badge is an optional dim marker rendered after the title (#820): the
	// recent-projects lists mark workspaces that are open in memory with "●".
	Badge string
	// Time is an optional right-aligned column (#1114) — the last-opened
	// time of recent-projects (#842) and recent-files (#1113) rows. It is
	// pinned to the right with clear separation from the title and the "✕"
	// zone, and drops first when the row gets too narrow.
	Time string
	// Aux is an optional secondary action (#820): shift+delete or
	// cmd+backspace (#1418, the forward-delete-free chord) on the selected
	// row — or a click on the row's aux zone — emits it without closing the
	// palette, e.g. closing a background workspace from the recent-projects
	// list. Nil hides the affordance.
	Aux tea.Msg
	// Hint is an optional leading shortcut label (#2023): the intention
	// popup numbers its first nine rows "1"…"9" so a digit runs that action
	// directly. It is rendered dim in front of the title and does not shift
	// the Spans, which stay title-relative.
	Hint string
	// AuxGlyph overrides the aux zone's default "✕" glyph (#1418), so rows
	// whose aux action is not a removal — closing an in-memory workspace
	// keeps the history entry — are visually distinct. Single-cell glyphs
	// only; "" renders the default.
	AuxGlyph string
	// Preview points the row at a source location (#2047). In a PreviewMode
	// open the palette renders a file excerpt around it beside the list, so
	// one sees where activating the row leads before jumping. The zero value
	// (empty Path) renders an empty preview column, so a mode may mix
	// positioned rows with positionless ones (#2053) — the file picker's
	// directory candidates, a scratch buffer's local mark.
	Preview PreviewTarget
}

// PreviewTarget is a row's source location for the palette's code-preview
// column (#2047): Line is 1-based, matching what the excerpt renderer expects.
// It is an alias of the shared component's Target (#2053), so a mode can pass
// a location straight through without a conversion hop.
type PreviewTarget = codepreview.Target

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

// Refresher is an optional Mode extension (#1372): a mode holding a cached
// snapshot of external state drops it here. The palette calls Refresh on every
// mode each time it opens, so a fresh open never serves stale results — the
// file finder re-walks the project and picks up files created or deleted since
// the last open.
type Refresher interface {
	Refresh()
}

// Completer is an optional Mode extension (#542): a mode that can extend the
// query body on tab — the project picker completes filesystem paths this way.
// Complete returns the extended body; returning the input unchanged means
// "nothing to complete" and the tab press is inert.
type Completer interface {
	Complete(query string) string
}

// ItemCompleter is a second, optional completion seam (#1775): a mode that can
// extend the query from the *selected row* when Complete has nothing textual
// to add — the '@' finder adopts the highlighted candidate on tab, the way a
// shell or the JetBrains file search does. It is only consulted after
// Completer declined, so path-style completion keeps precedence, and a mode
// returning false leaves the tab press inert as before.
type ItemCompleter interface {
	CompleteItem(query string, sel Item) (string, bool)
}

// PreviewMode is an optional Mode extension (#2047): a locked mode whose rows
// carry a source location (Item.Preview). The palette then splits its box —
// result list on the left, a code excerpt of the selected row's target on the
// right, separated by a vertical rule — and bounds the result window to
// [ui.MinResultRows, ui.MaxResultRows], so the popup neither collapses on two
// hits nor grows past forty. Every picker whose rows are file positions
// enables it (#2053): find-usages, the symbol and class pickers, the file
// picker and the bookmarks list. Modes whose rows are not positions —
// commands, recent projects, the mixed Search Everywhere list — keep the
// single-column layout, as do anchored opens.
type PreviewMode interface {
	// CodePreview reports whether the preview column is active for this open.
	CodePreview() bool
}

// DigitPicker is an optional Mode extension (#2023): a locked mode whose
// listed rows can be run by their number. While the palette's query is empty,
// digits 1–9 activate the first nine rows directly (the same path as selecting
// a row and pressing enter); once a filter query is typed, digits are ordinary
// query text again. The intention popup enables it; every other mode leaves
// digit keys alone.
type DigitPicker interface {
	// DigitShortcuts reports whether the digit fast path is active.
	DigitShortcuts() bool
}

// PanelMode is an optional Mode extension (#2055): a mode whose listed rows
// can be tipped out of the transient overlay into the persistent Find panel
// ("Open in Find Window" in JetBrains). Implementing it enables the
// find.openInPanel binding while the mode is active; PanelTitle names the
// resulting panel for the current query body.
type PanelMode interface {
	PanelTitle(query string) string
}

// RunCommandMsg is emitted when a command-mode item is activated. The root model
// resolves the id against the registry and runs it, keeping the palette free of
// any command store of its own.
type RunCommandMsg struct{ ID string }

// OpenFileMsg is emitted when a file-mode item is activated. The root model
// opens it through its normal open-file path.
type OpenFileMsg struct {
	Path string
	// CountUsage marks a selection confirmed from one of the two ranked
	// palette windows — Run a Command's file source or Search Everywhere
	// (#1419): the root model bumps the persisted file-usage counter only for
	// these. The palette sets it during activation; every other OpenFileMsg
	// producer (explorer, anchored "@" finder, go-to-file, recent-files mode)
	// leaves the zero value.
	CountUsage bool
}
