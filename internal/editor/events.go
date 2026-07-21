package editor

import "ike/internal/editor/mode"

// events.go is the LSP seam (Roadmap 0100). The editor emits on-change,
// cursor-move and completion-trigger signals through an injectable Emitter; no
// language intelligence lives here. A nil emitter (the default) drops events, so
// the editor runs standalone in tests. The LSP roadmap wires an Emitter that
// fans these out to registry Hooks.

// EventKind classifies an editor lifecycle signal.
type EventKind int

const (
	// EventChange fires after the buffer is mutated.
	EventChange EventKind = iota
	// EventCursorMove fires after the cursor moves without a buffer change.
	EventCursorMove
	// EventCompletionTrigger fires when a key likely warrants completion (e.g.
	// a "." typed in insert mode). LSP decides what to do with it.
	EventCompletionTrigger
	// EventSave fires after the buffer was written to disk (Roadmap 0140: the
	// watcher records a save epoch so IKE's own writes are not reported back as
	// external changes; LSP didSave hangs off the same signal).
	EventSave
	// EventJump fires immediately before the cursor departs on an in-file
	// jump — large motions (gg, G, {count}G) and search landings (/, ?, n,
	// N, *, #). The event carries the departure position so the navigation
	// history (Roadmap 0220) can record where the caret came from; the
	// landing follows as an ordinary EventCursorMove. Small motions (hjkl,
	// w/b, paragraphs) never emit it.
	EventJump
	// EventCompletionSelect fires when the completion popup's selection lands
	// on an item without documentation (#847). CompletionID carries the item's
	// reply index; the LSP bridge answers with completionItem/resolve.
	EventCompletionSelect
)

// SelKind classifies the visual selection carried on an event: none, a
// character-wise range (visual / visual-block), or whole lines (visual-line).
type SelKind int

const (
	SelNone SelKind = iota
	SelChar
	SelLine
)

// Event is one emitted signal. Line/Col are 0-based; Path is the buffer's file.
// Text carries the full buffer content, populated only on EventChange so the LSP
// bridge can drive full-document sync without a separate read-back seam.
// AnchorLine/AnchorCol carry the visual anchor while a selection is active
// (Sel != SelNone) so range-scoped LSP features (range formatting) know the
// selection without a read-back seam; the cursor is the other end.
type Event struct {
	Kind       EventKind
	Path       string
	Line       int
	Col        int
	Mode       mode.Mode
	Text       string
	Sel        SelKind
	AnchorLine int
	AnchorCol  int
	// Char carries the just-typed character on EventCompletionTrigger, so the
	// LSP bridge can match it against the server's completion trigger
	// characters (#527). Empty means a manual request (ctrl+space), which the
	// bridge honours unconditionally.
	Char string
	// Large marks a change on a document in large-file mode (#149): Text is
	// intentionally absent (not "the file became empty"), so the LSP bridge
	// must stop syncing instead of shipping an empty didChange — a reload can
	// flip an already-didOpened document into this state when it grows past
	// the threshold on disk.
	Large bool
	// CompletionID carries the selected item's reply index on
	// EventCompletionSelect (#847).
	CompletionID int
}

// Emitter receives editor events. Implementations must not block.
type Emitter interface {
	Emit(Event)
}

// EmitterFunc adapts a function to the Emitter interface.
type EmitterFunc func(Event)

// Emit implements Emitter.
func (f EmitterFunc) Emit(e Event) { f(e) }

// SetEmitter installs the LSP event sink. Passing nil disables emission.
func (m *Model) SetEmitter(e Emitter) { m.emitter = e }

// emit sends an event when an emitter is installed. A buffer change also bumps
// the document version (independent of any emitter) so the syntax highlighter can
// tag and order async parse results.
func (m *Model) emit(kind EventKind) { m.emitChar(kind, "") }

// emitCompletionSelect announces the selected completion item for lazy
// resolve (#847); the bridge debounces and answers with CompletionResolveMsg.
func (m *Model) emitCompletionSelect(id int) {
	if m.emitter == nil {
		return
	}
	m.emitter.Emit(Event{
		Kind:         EventCompletionSelect,
		Path:         m.path,
		Line:         m.cursor.Line,
		Col:          m.cursor.Col,
		Mode:         m.mode,
		CompletionID: id,
	})
}

// emitChar is emit with the typed character attached (EventCompletionTrigger).
func (m *Model) emitChar(kind EventKind, ch string) {
	if kind == EventChange {
		m.docVersion++
		// Keep collapsed folds consistent with the mutation (#144): dissolve
		// the fold the edit landed in, shift the ones below it (fold.go).
		m.dissolveFoldsAtEdit()
		// Breakpoints shift the same way (0350, #577), through the app's store.
		m.notifyBreakpointEdit()
	}
	if m.emitter == nil {
		return
	}
	ev := Event{
		Kind: kind,
		Path: m.path,
		Line: m.cursor.Line,
		Col:  m.cursor.Col,
		Mode: m.mode,
		Char: ch,
	}
	if m.mode.IsVisual() {
		ev.Sel = SelChar
		if m.mode == VisualLine {
			ev.Sel = SelLine
		}
		ev.AnchorLine, ev.AnchorCol = m.anchor.Line, m.anchor.Col
	}
	if kind == EventChange {
		if m.InsightOff() {
			// A flagged large document ships no text (#149): re-joining
			// megabytes per keystroke is exactly the cost this mode avoids.
			ev.Large = true
		} else {
			ev.Text = m.buf.String()
		}
	}
	m.emitter.Emit(ev)
}
