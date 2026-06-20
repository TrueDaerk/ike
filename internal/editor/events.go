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
)

// Event is one emitted signal. Line/Col are 0-based; Path is the buffer's file.
type Event struct {
	Kind EventKind
	Path string
	Line int
	Col  int
	Mode mode.Mode
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

// emit sends an event when an emitter is installed.
func (m *Model) emit(kind EventKind) {
	if m.emitter == nil {
		return
	}
	m.emitter.Emit(Event{
		Kind: kind,
		Path: m.path,
		Line: m.cursor.Line,
		Col:  m.cursor.Col,
		Mode: m.mode,
	})
}
