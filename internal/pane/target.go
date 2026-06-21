// Package pane is the registry of live pane instances behind IKE's dynamic
// tiled workspace. Where internal/layout is the pure geometry/structure of the
// split tree (leaves are opaque string keys), this package owns the *instances*
// those keys map to: each leaf key resolves to one live component (the explorer
// singleton or one of N editors). It is almost pure — it holds components but no
// I/O — so its lifecycle (allocate key, create, focus, close, dispatch by kind)
// is unit-tested independently of the bubbletea wiring in internal/app.
package pane

import "ike/internal/layout"

// OpenTarget is the "where to open" intent threaded through the open-file path.
// It is additive and defaults to Replace, so the explorer, host API, and plugin
// FileHandler contract stay backward-compatible: a zero value means "today's
// behaviour".
type OpenTarget int

const (
	// Replace loads the file into the focused editor (or the most-recent editor
	// when the explorer is focused) — the historical behaviour.
	Replace OpenTarget = iota
	// NewPane splits off a fresh editor beside the active leaf and loads the file
	// there, leaving existing buffers untouched.
	NewPane
)

// Open bundles an open-file intent: the target mode and, for NewPane, the side
// the new editor should land on. The zero value is a plain Replace.
type Open struct {
	Target OpenTarget
	// Zone hints which side a NewPane split lands on. It is ignored for Replace.
	Zone layout.Zone
}
