package app

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/scratch"
	"ike/internal/testdata"
	"ike/internal/ui"
)

// scratch_generate.go is the UI half of the test-data generator (#2134):
// `scratch.generate` opens a four-step shell wizard — format, row
// count/seed/table, field list, field editor — and writes the rendered
// document into a fresh scratch. The per-format `scratch.generate.<format>`
// commands skip the wizard entirely and generate straight from the stored
// preset, mirroring how `scratch.new.<lang>` shortcuts the language picker.
//
// The wizard follows the new-project wizard (#1718) rather than the settings
// SubPanel form: it is a modal shell dialog with steps walked by enter/esc, so
// it needs no page host and works from the palette anywhere. Generation itself
// runs as a tea.Cmd — a million rows must not be rendered on the update loop.

// Wizard step indices; esc walks them backwards.
const (
	tdStepFormat = iota
	tdStepOptions
	tdStepFields
	tdStepField
)

// Option-step field indices.
const (
	tdOptRows = iota
	tdOptSeed
	tdOptTable
	tdOptCount
)

// Field-editor field indices.
const (
	tdEditName = iota
	tdEditKind
	tdEditParam
	tdEditCount
)

// tdOptNames / tdEditNames label the text rows of the two form steps.
var (
	tdOptNames  = [tdOptCount]string{"Rows", "Seed", "Table"}
	tdEditNames = [tdEditCount]string{"Name", "Kind", "Param"}
)

// GenerateScratchMsg asks the root model to open the test-data wizard, or —
// when Format is set — to generate that format straight from its preset with
// no prompt at all.
type GenerateScratchMsg struct {
	Format testdata.Format
}

// scratchGenDoneMsg carries a finished generation back into Update.
type scratchGenDoneMsg struct {
	path string
	rows int
	err  error
}

// tdGenState is the open wizard; nil when it is closed.
type tdGenState struct {
	step    int
	formats []testdata.Format
	fmtPick int

	// spec holds everything the steps edit that is not currently in a text
	// field: the format and the field list.
	spec testdata.Spec

	opt      [tdOptCount]string
	optPos   [tdOptCount]int
	optField int

	fieldPick int

	// editIdx is the field being edited, -1 for a new one.
	editIdx   int
	edit      [tdEditCount]string
	editPos   [tdEditCount]int
	editField int

	running bool
	err     string
}

// generateCommands builds the scratch.generate family. Like scratchCommands
// it is rebuilt per registry query, so the format list has one definition
// (testdata.Formats) and no ordering constraints.
func generateCommands() []plugin.Command {
	cmds := []plugin.Command{
		appCommand("scratch.generate", "Generate Test Data…", GenerateScratchMsg{}),
	}
	for _, f := range testdata.Formats() {
		cmds = append(cmds, appCommand(
			"scratch.generate."+string(f),
			"Generate Test Data: "+f.Title(),
			GenerateScratchMsg{Format: f},
		))
	}
	return cmds
}

// startGenerateScratch opens the wizard on the format step with the first
// format selected and its preset loaded; moving the selection loads the next
// format's preset, since each format remembers its own field list.
func (m *Model) startGenerateScratch() {
	s := &tdGenState{formats: testdata.Formats(), editIdx: -1}
	s.spec = testdata.Preset(s.formats[0])
	s.loadSpec(s.spec)
	m.tdGen = s
	m.renderGenerateScratch()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// generateScratchOpen reports whether the shell currently shows the wizard.
func (m Model) generateScratchOpen() bool { return m.tdGen != nil && m.shell.IsOpen() }

// closeGenerateScratch clears the wizard state and the shell.
func (m *Model) closeGenerateScratch() {
	m.tdGen = nil
	m.shell.Close()
}

// loadSpec fills the option text fields from spec — called when the wizard
// opens and whenever the format step picks a different format, because each
// format remembers its own preset.
func (s *tdGenState) loadSpec(spec testdata.Spec) {
	s.spec = spec.Normalized()
	s.opt[tdOptRows] = strconv.Itoa(spec.Rows)
	s.opt[tdOptSeed] = strconv.FormatUint(spec.Seed, 10)
	s.opt[tdOptTable] = s.spec.Table
	for i := range s.opt {
		s.optPos[i] = len([]rune(s.opt[i]))
	}
	s.fieldPick = 0
}

// compose folds the option text fields back into a spec and validates it.
// Every accept path runs it, so "row count ≤ 0", an empty field list and an
// unknown kind are refused in one place with one message.
func (s *tdGenState) compose() (testdata.Spec, error) {
	spec := s.spec
	spec.Format = s.formats[s.fmtPick]
	rows, err := strconv.Atoi(strings.TrimSpace(s.opt[tdOptRows]))
	if err != nil {
		return spec, fmt.Errorf("row count %q is not a number", strings.TrimSpace(s.opt[tdOptRows]))
	}
	spec.Rows = rows
	seedText := strings.TrimSpace(s.opt[tdOptSeed])
	if seedText == "" {
		seedText = "0"
	}
	seed, err := strconv.ParseUint(seedText, 10, 64)
	if err != nil {
		return spec, fmt.Errorf("seed %q is not a non-negative number", seedText)
	}
	spec.Seed = seed
	spec.Table = strings.TrimSpace(s.opt[tdOptTable])
	if err := spec.Validate(); err != nil {
		return spec, err
	}
	return spec.Normalized(), nil
}

// renderGenerateScratch (re)fills the shell for the current step.
func (m *Model) renderGenerateScratch() {
	s := m.tdGen
	avail := m.width - 30
	if avail < 20 {
		avail = 20
	}
	var lines []string
	add := func(l string) { lines = append(lines, l) }
	switch s.step {
	case tdStepFormat:
		add("Format")
		add("")
		for i, f := range s.formats {
			marker := "  ○ "
			if i == s.fmtPick {
				marker = "> ● "
			}
			add(marker + f.Title() + "  ." + f.Ext())
		}
		add("")
		add("↑↓ select · enter next · esc cancel")
	case tdStepOptions:
		add(s.formats[s.fmtPick].Title() + " — rows, seed, table")
		add("")
		for i, name := range tdOptNames {
			marker := "  "
			if i == s.optField {
				marker = "▸ "
			}
			value := windowedPlain(s.opt[i], avail)
			if i == s.optField {
				value = windowedInput(s.opt[i], s.optPos[i], avail)
			}
			add(marker + tdPad(name, 8) + value)
		}
		add("")
		add("Seed 0 draws a fresh random seed; any other seed repeats byte for byte.")
		add("Table names the SQL table and the XML root element.")
		add("")
		add("tab next field · enter next · esc back")
	case tdStepFields:
		add(s.formats[s.fmtPick].Title() + " — fields")
		add("")
		if len(s.spec.Fields) == 0 {
			add("  (no fields — press a to add one)")
		}
		for i, f := range s.spec.Fields {
			marker := "  "
			if i == s.fieldPick {
				marker = "> "
			}
			row := marker + tdPad(f.Name, 16) + tdPad(string(f.Kind), 12)
			if f.Param != "" {
				row += f.Param
			}
			add(row)
		}
		add("")
		add("↑↓ select · a add · e edit · d delete · enter generate · esc back")
	case tdStepField:
		title := "Edit field"
		if s.editIdx < 0 {
			title = "New field"
		}
		add(title)
		add("")
		for i, name := range tdEditNames {
			marker := "  "
			if i == s.editField {
				marker = "▸ "
			}
			value := windowedPlain(s.edit[i], avail)
			if i == s.editField {
				value = windowedInput(s.edit[i], s.editPos[i], avail)
			}
			add(marker + tdPad(name, 8) + value)
		}
		add("")
		if info, ok := testdata.Info(testdata.Kind(strings.TrimSpace(s.edit[tdEditKind]))); ok {
			hint := info.Desc
			if info.Param != "" {
				hint += " · param: " + info.Param
			} else {
				hint += " · takes no param"
			}
			add("  " + hint)
		} else {
			add("  kinds: " + strings.Join(testdata.KindNames(), " "))
		}
		add("")
		add("tab next field · ↑↓ on Kind cycles kinds · enter accept · esc cancel")
	}
	if s.running {
		lines = append(lines, "", "Generating…")
	} else if s.err != "" {
		lines = append(lines, "", "E: "+s.err)
	}
	body := strings.Join(lines, "\n")
	m.shell.SetContent(ui.ModelContent{
		Heading: "Generate Test Data",
		Body:    func() string { return body },
	})
}

// updateGenerateScratch consumes every key while the wizard is open.
func (m Model) updateGenerateScratch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.tdGen
	if msg.Code == tea.KeyEscape {
		switch {
		case s.running, s.step == tdStepFormat:
			m.closeGenerateScratch()
			return m, nil
		default:
			s.step--
			s.err = ""
			m.renderGenerateScratch()
		}
		return m, nil
	}
	if s.running {
		return m, nil
	}
	switch s.step {
	case tdStepFormat:
		switch msg.Code {
		case tea.KeyUp:
			s.fmtPick = (s.fmtPick + len(s.formats) - 1) % len(s.formats)
			s.loadSpec(testdata.Preset(s.formats[s.fmtPick]))
		case tea.KeyDown, tea.KeyTab:
			s.fmtPick = (s.fmtPick + 1) % len(s.formats)
			s.loadSpec(testdata.Preset(s.formats[s.fmtPick]))
		case tea.KeyEnter:
			s.step = tdStepOptions
			s.optField = 0
		}
		s.err = ""
		m.renderGenerateScratch()
		return m, nil
	case tdStepOptions:
		switch {
		case msg.Code == tea.KeyEnter:
			if _, err := s.compose(); err != nil {
				s.err = err.Error()
				m.renderGenerateScratch()
				return m, nil
			}
			s.err = ""
			s.step = tdStepFields
		case msg.Code == tea.KeyTab && msg.Mod&tea.ModShift != 0, msg.Code == tea.KeyUp:
			s.optField = (s.optField + tdOptCount - 1) % tdOptCount
			s.optPos[s.optField] = len([]rune(s.opt[s.optField]))
		case msg.Code == tea.KeyTab, msg.Code == tea.KeyDown:
			s.optField = (s.optField + 1) % tdOptCount
			s.optPos[s.optField] = len([]rune(s.opt[s.optField]))
		default:
			if out, pos, handled, _ := ui.EditKey(msg, s.opt[s.optField], s.optPos[s.optField]); handled {
				s.opt[s.optField], s.optPos[s.optField] = out, pos
			}
		}
		m.renderGenerateScratch()
		return m, nil
	case tdStepFields:
		return m.updateGenerateFields(msg)
	case tdStepField:
		return m.updateGenerateFieldEdit(msg)
	}
	return m, nil
}

// updateGenerateFields drives the field-list step: single-key actions, since
// no text field has the focus here.
func (m Model) updateGenerateFields(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.tdGen
	n := len(s.spec.Fields)
	switch {
	case msg.Code == tea.KeyUp && n > 0:
		s.fieldPick = (s.fieldPick + n - 1) % n
	case (msg.Code == tea.KeyDown || msg.Code == tea.KeyTab) && n > 0:
		s.fieldPick = (s.fieldPick + 1) % n
	case msg.Code == 'a' && msg.Mod == 0:
		s.openFieldEditor(-1)
	case msg.Code == 'e' && msg.Mod == 0 && n > 0:
		s.openFieldEditor(s.fieldPick)
	case msg.Code == 'd' && msg.Mod == 0 && n > 0:
		s.spec.Fields = append(s.spec.Fields[:s.fieldPick:s.fieldPick], s.spec.Fields[s.fieldPick+1:]...)
		if s.fieldPick >= len(s.spec.Fields) && s.fieldPick > 0 {
			s.fieldPick--
		}
		s.err = ""
	case msg.Code == tea.KeyEnter:
		spec, err := s.compose()
		if err != nil {
			s.err = err.Error()
			m.renderGenerateScratch()
			return m, nil
		}
		s.err = ""
		s.running = true
		m.renderGenerateScratch()
		return m, generateScratchCmd(spec)
	}
	m.renderGenerateScratch()
	return m, nil
}

// openFieldEditor moves to the field editor for idx (-1 = new field).
func (s *tdGenState) openFieldEditor(idx int) {
	s.editIdx = idx
	f := testdata.Field{Kind: testdata.Kinds()[0]}
	if idx >= 0 && idx < len(s.spec.Fields) {
		f = s.spec.Fields[idx]
	}
	s.edit = [tdEditCount]string{f.Name, string(f.Kind), f.Param}
	for i := range s.edit {
		s.editPos[i] = len([]rune(s.edit[i]))
	}
	s.editField = 0
	s.step = tdStepField
	s.err = ""
}

// updateGenerateFieldEdit drives the field editor. Up/down cycle the catalog
// while the Kind row has the focus — 26 kinds are more than a hint line can
// list usefully — and move between rows everywhere else.
func (m Model) updateGenerateFieldEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.tdGen
	switch {
	case msg.Code == tea.KeyEnter:
		if err := s.acceptField(); err != nil {
			s.err = err.Error()
			m.renderGenerateScratch()
			return m, nil
		}
		s.err = ""
		s.step = tdStepFields
	case s.editField == tdEditKind && (msg.Code == tea.KeyUp || msg.Code == tea.KeyDown):
		delta := 1
		if msg.Code == tea.KeyUp {
			delta = -1
		}
		s.cycleKind(delta)
	case msg.Code == tea.KeyTab && msg.Mod&tea.ModShift != 0, msg.Code == tea.KeyUp:
		s.editField = (s.editField + tdEditCount - 1) % tdEditCount
		s.editPos[s.editField] = len([]rune(s.edit[s.editField]))
	case msg.Code == tea.KeyTab, msg.Code == tea.KeyDown:
		s.editField = (s.editField + 1) % tdEditCount
		s.editPos[s.editField] = len([]rune(s.edit[s.editField]))
	default:
		if out, pos, handled, _ := ui.EditKey(msg, s.edit[s.editField], s.editPos[s.editField]); handled {
			s.edit[s.editField], s.editPos[s.editField] = out, pos
		}
	}
	m.renderGenerateScratch()
	return m, nil
}

// cycleKind steps the Kind row through the catalog, starting from the current
// value; an unknown value restarts at the first kind.
func (s *tdGenState) cycleKind(delta int) {
	kinds := testdata.Kinds()
	idx := 0
	for i, k := range kinds {
		if string(k) == strings.TrimSpace(s.edit[tdEditKind]) {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(kinds)) % len(kinds)
	s.edit[tdEditKind] = string(kinds[idx])
	s.editPos[tdEditKind] = len([]rune(s.edit[tdEditKind]))
	// A parameter belongs to the kind it was typed for; carrying it to a kind
	// that takes none would only be rejected on accept.
	if info, ok := testdata.Info(kinds[idx]); ok && info.Param == "" {
		s.edit[tdEditParam] = ""
		s.editPos[tdEditParam] = 0
	}
}

// acceptField validates the editor's field and folds it into the spec. The
// field is checked on its own — name, known kind, parameter grammar — so a
// typo is caught here rather than at generate time.
func (s *tdGenState) acceptField() error {
	f := testdata.Field{
		Name:  strings.TrimSpace(s.edit[tdEditName]),
		Kind:  testdata.Kind(strings.TrimSpace(s.edit[tdEditKind])),
		Param: strings.TrimSpace(s.edit[tdEditParam]),
	}
	if f.Name == "" {
		return fmt.Errorf("field name is required")
	}
	if _, ok := testdata.Info(f.Kind); !ok {
		return fmt.Errorf("unknown kind %q — one of: %s", string(f.Kind), strings.Join(testdata.KindNames(), ", "))
	}
	for i, e := range s.spec.Fields {
		if i != s.editIdx && e.Name == f.Name {
			return fmt.Errorf("a field named %q already exists", f.Name)
		}
	}
	// A one-field probe spec runs the parameter through the very same checks
	// the generator will, so the editor and the generator can never disagree.
	probe := testdata.Spec{Format: s.formats[s.fmtPick], Rows: 1, Fields: []testdata.Field{f}}
	if err := probe.Validate(); err != nil {
		return err
	}
	if s.editIdx >= 0 && s.editIdx < len(s.spec.Fields) {
		s.spec.Fields[s.editIdx] = f
		s.fieldPick = s.editIdx
	} else {
		s.spec.Fields = append(s.spec.Fields, f)
		s.fieldPick = len(s.spec.Fields) - 1
	}
	s.editIdx = -1
	return nil
}

// pasteGenerateScratch inserts a paste into the focused text field (#1873);
// the two list steps have no input to paste into.
func (m *Model) pasteGenerateScratch(text string) bool {
	s := m.tdGen
	if s == nil || s.running {
		return false
	}
	var field *string
	var pos *int
	switch s.step {
	case tdStepOptions:
		field, pos = &s.opt[s.optField], &s.optPos[s.optField]
	case tdStepField:
		field, pos = &s.edit[s.editField], &s.editPos[s.editField]
	default:
		return false
	}
	out, np, changed := ui.PasteText(*field, *pos, text)
	if !changed {
		return false
	}
	*field, *pos = out, np
	m.renderGenerateScratch()
	return true
}

// generateScratchCmd renders the spec and writes it into a fresh scratch off
// the update loop — MaxRows worth of faker calls has no business blocking the
// UI. The spec is remembered as the format's preset only once it produced a
// file, so a failed run never overwrites a working preset.
func generateScratchCmd(spec testdata.Spec) tea.Cmd {
	return func() tea.Msg {
		data, err := testdata.Render(spec)
		if err != nil {
			return scratchGenDoneMsg{err: err}
		}
		path, err := scratch.CreateWithContent(spec.Format.Ext(), data)
		if err != nil {
			return scratchGenDoneMsg{err: err}
		}
		testdata.SavePreset(spec)
		return scratchGenDoneMsg{path: path, rows: spec.Rows}
	}
}

// startPresetGenerate is the no-prompt path of scratch.generate.<format>: the
// format's stored preset (or the stock default) generated straight away.
func (m Model) startPresetGenerate(format testdata.Format) (tea.Model, tea.Cmd) {
	spec := testdata.Preset(format)
	if err := spec.Validate(); err != nil {
		m.host.Notify(host.Warn, "test data: "+err.Error())
		return m, nil
	}
	return m, generateScratchCmd(spec)
}

// finishGenerateScratch answers a completed generation: a failure keeps the
// wizard open with the reason (or just toasts when it was a quick command), a
// success closes it, refreshes the explorer's Scratches section and opens the
// file through the standard funnel.
func (m Model) finishGenerateScratch(msg scratchGenDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if m.generateScratchOpen() {
			m.tdGen.running = false
			m.tdGen.err = msg.err.Error()
			m.renderGenerateScratch()
			return m, nil
		}
		m.host.Notify(host.Error, "test data: "+msg.err.Error())
		return m, nil
	}
	if m.generateScratchOpen() {
		m.closeGenerateScratch()
	}
	m.host.Notify(host.Info, fmt.Sprintf("test data: %d rows → %s", msg.rows, filepath.Base(msg.path)))
	// The explorer's Scratches section (#1963) shows the new file right away.
	m.explorer().RefreshScratches()
	return m.openPath(msg.path, false)
}

// tdPad left-aligns a label in n columns — the wizard's two form steps line
// their values up the way the settings forms do.
func tdPad(s string, n int) string {
	if len(s) >= n {
		return s + " "
	}
	return s + strings.Repeat(" ", n-len(s))
}
