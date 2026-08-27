package app

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/breakpanel"
	"ike/internal/dap"
	"ike/internal/debug"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/ui"
)

// breakpoint_form.go is the breakpoint-properties form (#2245): condition,
// hit count and log message of one breakpoint, edited together in the shell
// dialog the run-configuration form uses (#2173). The Breakpoints window's
// `c`/`n`/`l` still edit a single field inline; this is the JetBrains "Edit
// breakpoint" surface — reachable from the list (`p`) and from the editor
// gutter chord (debug.breakpointProperties, cmd/ctrl+alt+f8) — where all
// three are visible at once, and where a field the live adapter cannot honour
// is visibly disabled rather than silently dropped on the wire.

// bpField indexes the three form fields, in render order.
type bpField int

const (
	bpFieldCondition bpField = iota
	bpFieldHit
	bpFieldLog
	bpFieldCount
)

// label is the field's prompt, padded to one column.
func (f bpField) label() string {
	switch f {
	case bpFieldCondition:
		return "condition: "
	case bpFieldHit:
		return "hit count: "
	default:
		return "log:       "
	}
}

// bpFormState is the open form; nil when it is closed.
type bpFormState struct {
	path string // store key (project-relative), the store's own key
	line int    // 0-based buffer line
	vals [bpFieldCount]string
	curs [bpFieldCount]int
	// field is the focused row; it never rests on a field the adapter cannot
	// honour.
	field bpField
	// caps is the capability snapshot taken when the form opened; a session
	// that starts or ends while it is open does not move the goalposts
	// mid-edit — the store keeps the value either way.
	caps breakpanel.Caps
	// err is the validation complaint under the fields; the next accepted
	// keystroke clears it.
	err string
}

// breakpointCaps reports which refinements the live adapter advertised
// (#2245). Without a session every field is editable: breakpoints are edited
// offline all the time, and Session.SetBreakpoints strips what its adapter
// cannot honour when they are finally pushed.
func (m Model) breakpointCaps() breakpanel.Caps {
	if m.dbg == nil || m.dbg.sess == nil {
		return breakpanel.FullCaps()
	}
	sess := m.dbg.sess
	return breakpanel.Caps{
		Condition:    sess.SupportsConditionalBreakpoints(),
		HitCondition: sess.SupportsHitConditionalBreakpoints(),
		LogMessage:   sess.SupportsLogPoints(),
	}
}

// allows reports whether f is editable under c.
func bpCapAllows(c breakpanel.Caps, f bpField) bool {
	switch f {
	case bpFieldCondition:
		return c.Condition
	case bpFieldHit:
		return c.HitCondition
	default:
		return c.LogMessage
	}
}

// bpCapNotice names the missing capability of f, for the disabled marker and
// the toast.
func bpCapNotice(f bpField) string {
	switch f {
	case bpFieldCondition:
		return "conditional breakpoints"
	case bpFieldHit:
		return "hit counts"
	default:
		return "logpoints"
	}
}

// openBreakpointForm opens the form for the breakpoint on the store key path
// and the 0-based line. A line without a breakpoint is a no-op with a notice:
// the properties belong to a breakpoint, not to a line.
func (m *Model) openBreakpointForm(path string, line int) {
	if !m.bpts.Has(path, line) {
		m.host.Notify(host.Info, "no breakpoint on "+displayPath(m.bpAbsPath(path))+":"+strconv.Itoa(line+1))
		return
	}
	meta := m.bpts.MetaAt(path, line)
	s := &bpFormState{path: path, line: line, caps: m.breakpointCaps()}
	s.vals[bpFieldCondition] = meta.Condition
	s.vals[bpFieldHit] = meta.HitCondition
	s.vals[bpFieldLog] = meta.LogMessage
	for f := bpField(0); f < bpFieldCount; f++ {
		s.curs[f] = len([]rune(s.vals[f]))
	}
	// Focus the first field the adapter can honour; all-disabled leaves the
	// form open read-only, so the stored values are still visible.
	s.field = bpFieldCount
	for f := bpField(0); f < bpFieldCount; f++ {
		if bpCapAllows(s.caps, f) {
			s.field = f
			break
		}
	}
	m.bpForm = s
	if notice := bpDisabledNotice(s.caps); notice != "" {
		m.host.Notify(host.Warn, notice)
	}
	m.renderBreakpointForm()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// bpDisabledNotice lists the refinements the live adapter does not support,
// as one toast shown when the form opens ("" when it supports everything).
func bpDisabledNotice(c breakpanel.Caps) string {
	var missing []string
	for f := bpField(0); f < bpFieldCount; f++ {
		if !bpCapAllows(c, f) {
			missing = append(missing, bpCapNotice(f))
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return "debug: adapter does not support " + strings.Join(missing, ", ") + " — field disabled"
}

// strippedRefinementNotice words the one warning a session start earns when
// the store holds refinements this adapter did not advertise (#2245). The
// breakpoints themselves are still sent — Session.SetBreakpoints only strips
// the unsupported fields — so the session keeps working; the user just learns
// why a condition is not being honoured instead of debugging a breakpoint
// that "ignores" it. "" when everything travels intact.
func (m Model) strippedRefinementNotice(sess *dap.Session, files map[string][]dap.SourceBreakpoint) string {
	if sess == nil {
		return ""
	}
	var cond, hit, log bool
	for _, bps := range files {
		for _, bp := range bps {
			cond = cond || bp.Condition != ""
			hit = hit || bp.HitCondition != ""
			log = log || bp.LogMessage != ""
		}
	}
	var missing []string
	if cond && !sess.SupportsConditionalBreakpoints() {
		missing = append(missing, "conditions")
	}
	if hit && !sess.SupportsHitConditionalBreakpoints() {
		missing = append(missing, "hit counts")
	}
	if log && !sess.SupportsLogPoints() {
		missing = append(missing, "log messages")
	}
	if len(missing) == 0 {
		return ""
	}
	return "debug: adapter does not support " + strings.Join(missing, ", ") +
		" — those breakpoints stop unconditionally"
}

// breakpointFormOpen reports whether the shell currently shows the form.
func (m Model) breakpointFormOpen() bool { return m.bpForm != nil && m.shell.IsOpen() }

// closeBreakpointForm drops the form; unsaved edits are discarded (the store
// was never touched).
func (m *Model) closeBreakpointForm() {
	m.bpForm = nil
	m.shell.Close()
}

// renderBreakpointForm (re)fills the shell for the current state.
func (m *Model) renderBreakpointForm() {
	s := m.bpForm
	avail := m.width - 30
	if avail < 20 {
		avail = 20
	}
	var lines []string
	add := func(l string) { lines = append(lines, l) }
	add("")
	for f := bpField(0); f < bpFieldCount; f++ {
		marker := "  "
		if f == s.field {
			marker = "> "
		}
		switch {
		case !bpCapAllows(s.caps, f):
			// Disabled: the stored value stays visible (it survives in the
			// store and reaches an adapter that does support it), marked as
			// ignored by this session.
			val := s.vals[f]
			if val == "" {
				val = "—"
			}
			add("  " + f.label() + windowedPlain(val, avail) + "   (unsupported by adapter)")
		case f == s.field:
			add(marker + f.label() + windowedInput(s.vals[f], s.curs[f], avail))
		default:
			add(marker + f.label() + windowedPlain(s.vals[f], avail))
		}
	}
	add("")
	add("A logpoint (log message set) prints instead of stopping.")
	add("Hit count: \"5\", \">3\", \"%2\" — an empty field drops the refinement.")
	add("")
	add("tab/↑↓ field · enter apply · esc cancel")
	if s.err != "" {
		add("")
		add("E: " + s.err)
	}
	body := strings.Join(lines, "\n")
	m.shell.SetContent(ui.ModelContent{
		Heading: "Breakpoint — " + displayPath(m.bpAbsPath(s.path)) + ":" + strconv.Itoa(s.line+1),
		Body:    func() string { return body },
	})
}

// updateBreakpointForm consumes every key while the form is open.
func (m Model) updateBreakpointForm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.bpForm
	switch msg.Code {
	case tea.KeyEscape:
		m.closeBreakpointForm()
		return m, nil
	case tea.KeyTab:
		m.moveBreakpointFormField(1)
		m.renderBreakpointForm()
		return m, nil
	case tea.KeyEnter:
		return m.saveBreakpointForm()
	}
	switch msg.String() {
	case "shift+tab", "up":
		m.moveBreakpointFormField(-1)
		m.renderBreakpointForm()
		return m, nil
	case "down":
		m.moveBreakpointFormField(1)
		m.renderBreakpointForm()
		return m, nil
	}
	if s.field >= bpFieldCount {
		return m, nil // every field disabled: read-only
	}
	if out, ncur, handled, _ := ui.EditKey(msg, s.vals[s.field], s.curs[s.field]); handled {
		s.vals[s.field], s.curs[s.field] = out, ncur
		s.err = ""
		m.renderBreakpointForm()
	}
	return m, nil
}

// moveBreakpointFormField steps the focus by delta, skipping the fields the
// adapter cannot honour and wrapping like every other list in the app. A form
// with no editable field keeps its out-of-range focus.
func (m *Model) moveBreakpointFormField(delta int) {
	s := m.bpForm
	n := int(bpFieldCount)
	for i := 0; i < n; i++ {
		next := (int(s.field) + delta*(i+1)) % n
		if next < 0 {
			next += n
		}
		if bpCapAllows(s.caps, bpField(next)) {
			s.field = bpField(next)
			return
		}
	}
}

// saveBreakpointForm validates the three fields and writes them into the
// store, then persists, refreshes the list and pushes to a live session —
// the same tail as every other breakpoint mutation. A rejected field keeps
// the form open with the reason and the focus on the offending row.
func (m Model) saveBreakpointForm() (tea.Model, tea.Cmd) {
	s := m.bpForm
	meta := debug.Meta{
		Condition:    s.vals[bpFieldCondition],
		HitCondition: s.vals[bpFieldHit],
		LogMessage:   s.vals[bpFieldLog],
	}
	// The fields are checked in render order, so the first complaint is the
	// topmost one — the focus lands where the eye already is.
	for _, check := range []struct {
		field bpField
		err   error
	}{
		{bpFieldCondition, debug.ValidateCondition(meta.Condition)},
		{bpFieldHit, debug.ValidateHitCondition(meta.HitCondition)},
		{bpFieldLog, debug.ValidateLogMessage(meta.LogMessage)},
	} {
		if check.err != nil {
			s.err = check.err.Error()
			if bpCapAllows(s.caps, check.field) {
				s.field = check.field
			}
			m.renderBreakpointForm()
			return m, nil
		}
	}
	if !m.bpts.Has(s.path, s.line) {
		// The breakpoint went away while the form was open (a gutter toggle,
		// a delete in the list): nothing to attach the properties to.
		m.host.Notify(host.Info, "breakpoint is gone — properties discarded")
		m.closeBreakpointForm()
		return m, nil
	}
	path, line := s.path, s.line
	m.bpts.SetMeta(path, line, meta)
	m.saveBreakpoints()
	m.refreshBreakpointsPanel()
	m.closeBreakpointForm()
	m.host.Notify(host.Info, bpFormSavedNotice(meta, displayPath(m.bpAbsPath(path))+":"+strconv.Itoa(line+1)))
	return m, m.syncSessionBreakpoints(m.bpAbsPath(path))
}

// bpFormSavedNotice words the confirmation by what the breakpoint became.
func bpFormSavedNotice(meta debug.Meta, where string) string {
	switch meta.Kind() {
	case debug.KindLogpoint:
		return "logpoint set — " + where
	case debug.KindConditional:
		return "conditional breakpoint set — " + where
	default:
		return "breakpoint properties cleared — " + where
	}
}

// pasteBreakpointForm inserts a paste into the focused field (#1873).
func (m *Model) pasteBreakpointForm(text string) bool {
	s := m.bpForm
	if s == nil || s.field >= bpFieldCount {
		return false
	}
	out, pos, changed := ui.PasteText(s.vals[s.field], s.curs[s.field], text)
	if !changed {
		return false
	}
	s.vals[s.field], s.curs[s.field] = out, pos
	s.err = ""
	m.renderBreakpointForm()
	return true
}

// breakpointPropertiesAtCursor is the debug.breakpointProperties handler: the
// focused editor's file at the cursor line, the gutter-side counterpart of
// the list's `p` (#2245).
func (m *Model) breakpointPropertiesAtCursor() {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindEditor {
		m.host.Notify(host.Info, "breakpoint properties need a focused editor")
		return
	}
	ed := inst.Editor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "breakpoint properties need an open file")
		return
	}
	line, _ := ed.CursorPos()
	m.openBreakpointForm(bpKey(ed.Path()), line)
}
