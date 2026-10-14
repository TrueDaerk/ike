package app

// complexity_intention.go is the caret-side doorway into the Complexity
// window (#2965): alt+enter on a function header — or anywhere inside a
// function, the innermost measured unit containing the caret — offers the
// breakdown, the window, the baseline toggle, the pin toggle and the
// simplify agent. Each entry is a registered command that opens the File
// tab, selects the caret's function and delegates to the pane's own action.

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/intention"
)

// The caret entries' messages, one per command.
type (
	// ComplexityBreakdownAtCaretMsg runs complexity.breakdownAtCaret.
	ComplexityBreakdownAtCaretMsg struct{}
	// ComplexityOpenAtCaretMsg runs complexity.openAtCaret.
	ComplexityOpenAtCaretMsg struct{}
	// ComplexityBaselineAtCaretMsg runs complexity.baselineAtCaret.
	ComplexityBaselineAtCaretMsg struct{}
	// ComplexityPinAtCaretMsg runs complexity.pinAtCaret.
	ComplexityPinAtCaretMsg struct{}
	// ComplexityAskAtCaretMsg runs complexity.askAtCaret.
	ComplexityAskAtCaretMsg struct{}
)

// complexityAtCaret finds the innermost measured unit of path containing
// the 0-based line, with its ordinal among same-named units; false outside
// any function, for an unmeasured file or with the index off.
func (m Model) complexityAtCaret(path string, line int) (complexity.Function, int, bool) {
	if m.complexityIndex == nil || path == "" {
		return complexity.Function{}, 0, false
	}
	fns := m.complexityIndex.Snapshot().Files[canonicalPath(path)]
	best := -1
	for i, fn := range fns {
		if line < fn.Line || line > fn.EndLine {
			continue
		}
		if best < 0 || fn.Line >= fns[best].Line && fn.EndLine <= fns[best].EndLine {
			best = i
		}
	}
	if best < 0 {
		return complexity.Function{}, 0, false
	}
	return fns[best], complexity.Ordinals(fns)[best], true
}

// complexityIntention is the provider: the entries for the caret's
// function, each omitted when its feature is off.
func (m Model) complexityIntention() intention.Provider {
	return intention.Provider{
		ID: "complexity",
		Items: func(cx intention.Context) []intention.Item {
			if cx.Fileless {
				return nil
			}
			fn, ord, ok := m.complexityAtCaret(cx.Path, cx.Line)
			if !ok {
				return nil
			}
			item := func(title, id string) intention.Item {
				return intention.Item{Title: title, Kind: "complexity", CommandID: id}
			}
			items := []intention.Item{}
			if len(fn.Sites) > 0 {
				items = append(items, item("Show complexity breakdown", "complexity.breakdownAtCaret"))
			}
			items = append(items, item("Open in Complexity window", "complexity.openAtCaret"))
			rel := baselineRel(m.complexityRoot(), canonicalPath(cx.Path))
			key := complexity.EntryKey{File: rel, Function: fn.Name, Ordinal: ord}
			if m.cxBase != nil && complexityBaselineOn() {
				if _, accepted := m.complexityBaseline().Lookup(key); accepted {
					items = append(items, item("Remove from complexity baseline", "complexity.baselineAtCaret"))
				} else if c := config.Get(); c == nil || m.complexityThresholdsFor(c.Complexity, cx.Path).Over(fn) {
					items = append(items, item("Accept into complexity baseline", "complexity.baselineAtCaret"))
				}
			}
			if m.cxPins != nil {
				if _, pinned := m.complexityPins().Lookup(key); pinned {
					items = append(items, item("Unpin in Complexity window", "complexity.pinAtCaret"))
				} else {
					items = append(items, item("Pin in Complexity window", "complexity.pinAtCaret"))
				}
			}
			return append(items, item("Ask agent to simplify", "complexity.askAtCaret"))
		},
	}
}

// complexityPanelAtCaret opens the Complexity window on the File tab with
// the caret's function selected; nil (and a toast) when the caret is in no
// measured function.
func (m *Model) complexityPanelAtCaret() *complexitypanel.Model {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		return nil
	}
	line, _ := ed.CursorPos()
	if _, _, ok := m.complexityAtCaret(ed.Path(), line); !ok {
		m.host.Notify(host.Info, "complexity: the caret is not inside a measured function")
		return nil
	}
	if m.complexityPanel() == nil {
		m.openComplexityPanel()
	}
	p := m.complexityPanel()
	if p == nil {
		return nil
	}
	p.SetTab(complexitypanel.TabFile)
	p.Follow(canonicalPath(ed.Path()), line)
	p.Refresh()
	if !p.SelectCaret() {
		return nil
	}
	return p
}

// handleComplexityAtCaret runs one caret entry.
func (m *Model) handleComplexityAtCaret(msg tea.Msg) tea.Cmd {
	p := m.complexityPanelAtCaret()
	if p == nil {
		return nil
	}
	switch msg.(type) {
	case ComplexityBreakdownAtCaretMsg:
		if r, ok := p.Expanded(); !ok || r.Fn.Line != p.Rows()[p.CaretRow()].Fn.Line {
			p.ToggleSites()
		}
	case ComplexityBaselineAtCaretMsg:
		if !m.baselineEditable() {
			return nil
		}
		return m.toggleComplexityBaselineRow()
	case ComplexityPinAtCaretMsg:
		return m.toggleComplexityPinRow()
	case ComplexityAskAtCaretMsg:
		return m.complexityAskAgent()
	}
	return nil
}
