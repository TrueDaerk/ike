// Package intention defines the seam for intention actions (#2020): the
// caret-dependent entries alt+enter merges with the LSP code-action offer.
// A Provider receives a Context snapshot of the caret — position, language,
// line access, plus the caret facts the owning subsystem already caches —
// and returns the Items applicable right there. Providers are registered as
// a plugin capability (plugin.Capabilities.Intentions), so any plugin can
// contribute; the built-in catalog (catalog.go) ships through the app plugin
// and the LSP plugin adds its capability-gated entries.
//
// Every Item delegates to a registered command, never to new logic: the
// intention popup is a caret-aware doorway into commands that already exist,
// and activation funnels through the same dispatch path the palette and the
// menus use (EventCommandExecuted fires, #679). Providers run synchronously
// on every popup open, so applicability checks must stay cheap line/caret
// probes.
package intention

// Context describes the caret the intention popup opens on. Position and
// line access mirror what the LSP bridge tracks for its requests; the
// remaining fields are caret facts the app precomputes from state the
// subsystems already hold (VCS gutter marks, conceal state, diagnostics),
// so providers stay pure functions over this struct — which is what keeps
// them table-testable.
type Context struct {
	// Path is the buffer's file path ("" for an unsaved buffer).
	Path string
	// LangID is the buffer's registered language id (e.g. "json", "go").
	LangID string
	// Line and Col are the 0-based caret position.
	Line, Col int
	// LineText is the caret line's text.
	LineText string
	// LineCount and LineAt give read access to the whole buffer for probes
	// that need more than the caret line. LineAt may be nil in tests that
	// exercise single-line probes.
	LineCount int
	LineAt    func(i int) string
	// HasSelection reports an active visual selection.
	HasSelection bool

	// DocPath reports that the caret sits on a JSON/YAML value with a
	// non-root document path (the status-line breadcrumb, #1660).
	DocPath bool
	// HTTPRequest reports that the buffer is an .http file and the caret
	// sits inside a request block.
	HTTPRequest bool
	// DiagnosticAtCaret reports an LSP diagnostic on the caret line.
	DiagnosticAtCaret bool
	// HunkAtCaret reports a VCS gutter mark (added/changed/deleted line)
	// on the caret line.
	HunkAtCaret bool
	// ConflictAtCaret reports that the caret sits inside a merge-conflict
	// block (#1149).
	ConflictAtCaret bool
	// InRepo reports that the buffer is a tracked file in a git repository.
	InRepo bool
	// TestAtCaret reports a runnable test at or above the caret (the
	// run.testAtCursor gate, lang.TestsInFile).
	TestAtCaret bool
	// CanToggleValue reports that the caret word is a known toggle pair
	// (true/false, on/off, …; #1658).
	CanToggleValue bool
	// ConcealValue reports that the conceal explainer resolves a value at
	// the caret (#1998). ConcealFamily names the concealfilter family
	// gating the stand-in ("" when the value has none).
	ConcealValue  bool
	ConcealFamily string
}

// Item is one applicable intention action: a title, the kind chip the picker
// groups it under ("copy", "http", "vcs", "test", …), and the registered
// command activation dispatches.
type Item struct {
	Title string
	Kind  string
	// CommandID names the registered command to run on activation.
	CommandID string
}

// Provider yields the items applicable at a caret. Items runs synchronously
// on every popup open — implementations must be cheap.
type Provider struct {
	// ID identifies the provider for diagnostics and duplicate detection,
	// namespaced by the owning plugin (e.g. "app.docpath").
	ID string
	// Items returns the applicable actions, in the order they should list
	// within their kind group; nil/empty when nothing applies here.
	Items func(Context) []Item
}
