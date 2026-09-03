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

import "strings"

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
	// ReadOnly reports a buffer that refuses edits (#1762): every intention
	// that rewrites text would apply nothing there, so none is offered
	// (#2026).
	ReadOnly bool
	// HasClipboard reports that the system clipboard holds text — the
	// precondition of diff.compareWithClipboard, which otherwise answers
	// "clipboard is empty".
	HasClipboard bool
	// Fileless reports that the buffer has no file at all — the state the
	// buffer-level language pick applies to (#2033). Path is empty in that
	// case too, but a zero Context must offer nothing, so the fact is
	// carried explicitly rather than inferred from the empty path.
	Fileless bool
	// LangExt is the extension, without the dot, the buffer's language
	// resolves under ("json" for a JSON buffer) — "" with no language, and
	// for one recognized by base name only (Dockerfile), which has no
	// extension to write a file under. It gates "Materialize to File"
	// (#2056), whose whole job is producing a file the path-keyed
	// subsystems classify the same way.
	LangExt string

	// DocPath reports that the caret sits on a JSON/YAML value with a
	// non-root document path (the status-line breadcrumb, #1660).
	DocPath bool
	// HTTPRequest reports that the buffer is an .http file and the caret
	// sits inside a request block.
	HTTPRequest bool
	// The response-side facts of the HTTP client (#2026): the copy and
	// re-send actions read the *visible* response pane, not the caret, so
	// without one they can only answer "no response pane open".
	// HTTPResponseBody / HTTPResponseHeaders report that the shown response
	// has a body / a headers block to copy, HTTPResendable that it carries
	// the request snapshot http.resend repeats.
	HTTPResponseBody    bool
	HTTPResponseHeaders bool
	HTTPResendable      bool
	// HTTPResponseSaveable reports that the shown response has raw body bytes
	// to write to a file (#2059) — true for a binary body too, which has no
	// text to copy but is the very case the file export exists for.
	HTTPResponseSaveable bool
	// HTTPEnvironments reports that an http-client.env.json (or its private
	// twin) next to the buffer defines at least one environment, which is
	// all http.selectEnvironment has to offer.
	HTTPEnvironments bool
	// DiagnosticAtCaret reports an LSP diagnostic on the caret line.
	DiagnosticAtCaret bool
	// HunkAtCaret reports a VCS gutter mark (added/changed/deleted line)
	// on the caret line.
	HunkAtCaret bool
	// ConflictAtCaret reports that the caret sits inside a merge-conflict
	// block (#1149).
	ConflictAtCaret bool
	// InRepo reports that the buffer is a tracked file *inside* the open
	// repository — a file from elsewhere on disk has no blame and no
	// history to show (#2026).
	InRepo bool
	// TestAtCaret reports a runnable test at or above the caret (the
	// run.testAtCursor gate, lang.TestsInFile).
	TestAtCaret bool
	// CanDebug reports that the test at the caret could actually start under
	// the debugger: the language has a debug adapter (lang.SupportsDebug)
	// and no session is running or launching (#2026).
	CanDebug bool
	// BreakpointAtCaret reports a breakpoint on the caret line (#2405), so
	// the intention popup can offer its refinements — the condition form is
	// otherwise only reachable through a chord nobody finds by accident.
	// BreakpointConditional reports that this breakpoint already carries a
	// condition, which turns the entry from "Add" into "Edit".
	BreakpointAtCaret     bool
	BreakpointConditional bool
	// CanToggleValue reports that the caret word is a known toggle pair
	// (true/false, on/off, …; #1658).
	CanToggleValue bool
	// ConcealValue reports that the caret sits on a conceal stand-in the
	// explainer speaks for (#1998) — a decoded timestamp, a size/duration
	// hint, a mask. ConcealFamily names the concealfilter family gating it.
	// A plain identifier is *not* one (#2026): explaining it only ever said
	// "nothing conceals this", which is no intention.
	ConcealValue  bool
	ConcealFamily string
	// The Ansible Vault facts (#2293). VaultBuffer reports a buffer already
	// vault-backed (decrypted in memory, re-encrypted on save) — nothing left
	// to offer. VaultReady reports that a password source is configured (an
	// ANSIBLE_VAULT_* variable or ansible.vault_password_file), the
	// precondition of vault.treatAsFile — without one the action could only
	// answer "no password source".
	VaultBuffer bool
	VaultReady  bool

	// Preview computes what the entry for one command id would change, for
	// the popup's diff preview of the highlighted action (#2252). The app
	// fills it with a pure function over copies of the buffer — it applies
	// nothing and mutates nothing — and answers false for every command it
	// cannot preview. A nil Preview (a provider test, a context built before
	// the preview seam existed) previews nothing at all, which is the same
	// honest "no preview" the popup shows for a command-style action.
	Preview func(commandID string) (Edit, bool)
}

// Edit is what an intention would do to the buffer, as text: the affected
// region before and after the action, from which the popup renders a small
// inline diff. Line is the 0-based line Before starts at, so a preview of a
// region can be labelled with where it sits. Nothing here is applied — the
// apply path stays the command dispatch it always was.
type Edit struct {
	Before string
	After  string
	Line   int
}

// PreviewFor wires an item to the lazy preview of its command: the returned
// closure is called only when the row is actually highlighted, and only after
// the popup's debounce, so scrolling the list computes nothing. It is nil when
// this context previews nothing, which is what makes "no preview" the default
// for every entry that does not opt in.
func (c Context) PreviewFor(commandID string) func() (Edit, bool) {
	if c.Preview == nil {
		return nil
	}
	return func() (Edit, bool) { return c.Preview(commandID) }
}

// lineAt reads line i of the buffer through the snapshot's accessor, falling
// back to the caret line so a provider works in a single-line test context
// (LineAt may be nil there). Out-of-range reads answer "".
func (c Context) lineAt(i int) string {
	if c.LineAt != nil {
		return c.LineAt(i)
	}
	if i == c.Line {
		return c.LineText
	}
	return ""
}

// textProbeLines bounds hasText: an intention gate must stay a cheap probe,
// and a buffer whose first hundred lines are all blank is empty enough that
// treating it as such costs nothing real.
const textProbeLines = 100

// hasText reports whether the buffer holds non-whitespace text within the
// first textProbeLines lines — the precondition of every entry that hands the
// buffer's own content to a tool (#2056: the playgrounds refuse an empty
// input).
func (c Context) hasText() bool {
	n := c.lineCount()
	if n > textProbeLines {
		n = textProbeLines
	}
	for i := 0; i < n; i++ {
		if strings.TrimSpace(c.lineAt(i)) != "" {
			return true
		}
	}
	return false
}

// lineCount is LineCount with the single-line fallback lineAt implies.
func (c Context) lineCount() int {
	if c.LineAt != nil {
		return c.LineCount
	}
	return c.Line + 1
}

// Item is one applicable intention action: a title, the kind chip the picker
// groups it under ("copy", "http", "vcs", "test", …), and the registered
// command activation dispatches.
type Item struct {
	Title string
	Kind  string
	// CommandID names the registered command to run on activation.
	CommandID string
	// Preview computes the edit this entry would produce, for the popup's
	// inline diff of the highlighted row (#2252). Nil — the default — means
	// "no preview": an entry that runs a command, opens a picker or has any
	// other side effect has no edit to show, and the popup says so rather
	// than pretending. Providers get one from Context.PreviewFor; it must
	// stay pure, since highlighting a row must never change the buffer.
	Preview func() (Edit, bool)
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
