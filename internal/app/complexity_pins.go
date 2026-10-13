package app

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/host"
)

// complexity_pins.go wires the optimisation-session pins and the session
// Δ (Roadmap 0551, #2959) into the app. The pin list
// (complexity.Pins, complexity-pins.json in the state store — IKE_CONFIG_DIR,
// else the project's .ike/) is read lazily the first time the Complexity
// window or 'p' needs it, toggled by the pane's PinToggleMsg and saved
// atomically on every change. The session base is the first settled
// snapshot of the complexity index per project root, kept for the process
// lifetime so a reopened window (or a rebuilt model) still measures the
// whole session.

// ComplexityPinMsg runs complexity.pin ('p' in the pane): pin or unpin the
// selected function.
type ComplexityPinMsg struct{}

// complexityPinsState is the lazily loaded pin list, shared by pointer
// across value-model copies like cxBase.
type complexityPinsState struct {
	path   string
	pins   *complexity.Pins
	loaded bool
}

// newComplexityPinsState names the file; nothing is read yet.
func newComplexityPinsState(root string) *complexityPinsState {
	return &complexityPinsState{path: complexity.PinsPath(root)}
}

// list loads the file on first use; a malformed file is an empty list
// (pins are convenience state, like bookmarks) and is overwritten by the
// next pin.
func (st *complexityPinsState) list() *complexity.Pins {
	if !st.loaded {
		st.pins, _ = complexity.LoadPins(st.path)
		st.loaded = true
	}
	return st.pins
}

// complexityPins is the pin list the window shows, nil without state.
func (m Model) complexityPins() *complexity.Pins {
	if m.cxPins == nil {
		return nil
	}
	return m.cxPins.list()
}

// toggleComplexityPinRow is the complexity.pin handler: an unopened panel
// is opened first; a row that cannot be pinned says why.
func (m *Model) toggleComplexityPinRow() tea.Cmd {
	p := m.complexityPanel()
	if p == nil {
		m.openComplexityPanel()
		if p = m.complexityPanel(); p == nil {
			return nil
		}
	}
	cmd, why := p.TogglePin()
	if cmd == nil {
		m.host.Notify(host.Info, "complexity: "+why)
	}
	return cmd
}

// handleComplexityPinToggle applies the pane's PinToggleMsg: pin the
// function at its current metrics or unpin it, save, re-apply.
func (m *Model) handleComplexityPinToggle(msg complexitypanel.PinToggleMsg) {
	if m.cxPins == nil {
		return
	}
	list := m.cxPins.list()
	pinned := list.Toggle(msg.Rel, msg.Fn, msg.Ordinal, time.Now())
	if err := list.Save(m.cxPins.path); err != nil {
		m.host.Notify(host.Error, "complexity: pins not saved: "+err.Error())
	} else if pinned {
		m.host.Notify(host.Info, "complexity: pinned "+msg.Fn.Name)
	} else {
		m.host.Notify(host.Info, "complexity: unpinned "+msg.Fn.Name)
	}
	if p := m.complexityPanel(); p != nil {
		p.SetPins(list)
	}
}

// cxSessionBases holds the first settled snapshot per project root for
// the process lifetime (the session Δ's reference).
var cxSessionBases = struct {
	sync.Mutex
	views map[string]complexity.View
}{views: map[string]complexity.View{}}

// complexitySessionBase records the index's snapshot as the session base
// of its root when none is held and the index has settled (walk finished,
// enabled, measurable), and returns the root's base.
func (m Model) complexitySessionBase() (complexity.View, bool) {
	ix := m.complexityIndex
	if ix == nil {
		return complexity.View{}, false
	}
	root := ix.Root()
	cxSessionBases.Lock()
	defer cxSessionBases.Unlock()
	if v, ok := cxSessionBases.views[root]; ok {
		return v, true
	}
	st := ix.Stats()
	if st.Scanning || !st.Enabled || st.Unavailable {
		return complexity.View{}, false
	}
	v := ix.Snapshot()
	cxSessionBases.views[root] = v
	return v, true
}

// applyComplexitySession records the session base (once) and hands it to
// an open window — on seed and on every ComplexityChangedMsg.
func (m *Model) applyComplexitySession() {
	v, ok := m.complexitySessionBase()
	if !ok {
		return
	}
	if p := m.complexityPanel(); p != nil {
		p.SetSessionBase(v)
	}
}
