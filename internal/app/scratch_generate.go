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

// scratch_generate.go is the UI half of the test-data generator (#2134,
// reworked in #2228): `scratch.generate` opens a five-step shell wizard —
// format, row count/seed/table, column list, column editor, kind picker — and
// writes the rendered document into a fresh scratch. The per-format
// `scratch.generate.<format>` commands skip the wizard entirely and generate
// straight from the stored preset, mirroring how `scratch.new.<lang>`
// shortcuts the language picker.
//
// The wizard follows the new-project wizard (#1718) rather than the settings
// SubPanel form: it is a modal shell dialog with steps walked by enter/esc, so
// it needs no page host and works from the palette anywhere. Since #2228 it is
// also fully mouse-operable: every rendered line records what it targets (a
// format, an option field, a column, a catalog kind, a button), so a click
// acts on what it visibly hits, every step carries a clickable button row, and
// the wheel moves the list selections. The kind is never typed as free text
// any more — the column editor's Kind row opens a filterable catalog picker.
// Generation itself runs as a tea.Cmd — a million rows must not be rendered on
// the update loop.

// Wizard step indices; esc walks them backwards.
const (
	tdStepFormat = iota
	tdStepOptions
	tdStepFields
	tdStepField
	tdStepKind
)

// Option-step field indices.
const (
	tdOptRows = iota
	tdOptSeed
	tdOptTable
	tdOptCount
)

// Column-editor row indices.
const (
	tdEditName = iota
	tdEditKind
	tdEditParam
	tdEditCount
)

// tdOptNames / tdEditNames label the rows of the two form steps.
var (
	tdOptNames  = [tdOptCount]string{"Rows", "Seed", "Table"}
	tdEditNames = [tdEditCount]string{"Name", "Kind", "Param"}
)

// tdHitKind tags what a rendered line (or a span of it) targets, so the click
// handler acts on what the user visibly hit instead of re-deriving the layout.
type tdHitKind int

const (
	tdHitFormat  tdHitKind = iota // arg: format index
	tdHitOpt                      // arg: option field index
	tdHitField                    // arg: column index
	tdHitEdit                     // arg: editor row index
	tdHitCatalog                  // arg: index into the filtered catalog
	tdHitButton                   // arg: the key rune the button replays
)

// tdHit is one clickable region: the body line it lies on, its column span
// ([x0, x1)), what it targets and which one. Whole-line targets span the line.
type tdHit struct {
	y, x0, x1 int
	kind      tdHitKind
	arg       int
}

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
	// field: the format and the column list.
	spec testdata.Spec

	opt      [tdOptCount]string
	optPos   [tdOptCount]int
	optField int

	fieldPick int
	fieldTop  int

	// editIdx is the column being edited, -1 for a new one. The name and the
	// parameter are text fields; the kind is an index into testdata.Kinds() —
	// it is chosen from the catalog, never typed (#2228).
	editIdx   int
	editName  string
	editNPos  int
	editParam string
	editPPos  int
	editKind  int
	editField int

	// The kind-picker step: kindPick indexes the *filtered* catalog, kindTop
	// is its window's first visible row, kindSearch the live type-to-filter.
	kindPick   int
	kindTop    int
	kindSearch ui.SpeedSearch

	// hits are the clickable regions of the last render, in body-line
	// coordinates — rebuilt by every renderGenerateScratch call.
	hits []tdHit

	running bool
	err     string
	// note is a non-error status line ("removed column …"); the next action
	// clears it.
	note string
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
// format's preset, since each format remembers its own column list.
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
	s.fieldPick, s.fieldTop = 0, 0
}

// compose folds the option text fields back into a spec and validates it.
// Every accept path runs it, so "row count ≤ 0", an empty column list and an
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

// tdValueWidth is the width budget of a text-field value or a catalog
// description: the window minus the dialog chrome and the label column, so a
// narrow terminal clips values instead of overflowing the box.
func (m *Model) tdValueWidth() int {
	avail := m.width - 30
	if avail < 20 {
		avail = 20
	}
	return avail
}

// tdListRows is the height budget of a list window: the shell's laid-out
// viewport minus the step's fixed rows (heading, filter, indicators, hints,
// buttons), so a long list scrolls inside the dialog — never past its bottom,
// where the hint lines would become invisible.
func (m *Model) tdListRows(n int) int {
	rows := m.shell.ViewportRows() - 12
	if rows <= 0 {
		rows = m.height - 16 // before the shell's first layout
	}
	if rows < 4 {
		rows = 4
	}
	if rows > n {
		rows = n
	}
	return rows
}

// tdWindow slides a list window of vis rows over n entries so that pick stays
// visible, and returns the window's start.
func tdWindow(top *int, pick, n, vis int) int {
	if pick < *top {
		*top = pick
	}
	if pick >= *top+vis {
		*top = pick - vis + 1
	}
	if *top > n-vis {
		*top = n - vis
	}
	if *top < 0 {
		*top = 0
	}
	return *top
}

// filteredCatalog is the kind-picker's row set: the catalog narrowed by the
// live filter, in catalog order. Matching runs over "name description" so
// typing either finds the kind.
func (s *tdGenState) filteredCatalog() []testdata.KindInfo {
	all := testdata.Catalog()
	if !s.kindSearch.Active() {
		return all
	}
	out := make([]testdata.KindInfo, 0, len(all))
	for _, e := range all {
		if s.kindSearch.Matches(string(e.Kind) + " " + e.Desc) {
			out = append(out, e)
		}
	}
	return out
}

// renderGenerateScratch (re)fills the shell for the current step and rebuilds
// the click map: every clickable line registers a hit while it is added, so
// the hit-test can never drift from the rendering.
func (m *Model) renderGenerateScratch() {
	s := m.tdGen
	avail := m.tdValueWidth()
	var lines []string
	s.hits = s.hits[:0]
	add := func(l string) { lines = append(lines, l) }
	// addHit renders a whole clickable line.
	addHit := func(l string, kind tdHitKind, arg int) {
		s.hits = append(s.hits, tdHit{y: len(lines), x0: 0, x1: len([]rune(l)), kind: kind, arg: arg})
		add(l)
	}
	// addButtons renders one line of "[label]" buttons, each its own span,
	// each replaying the key it stands for when clicked.
	addButtons := func(btns ...[2]string) { // {label, key}
		var b []rune
		y := len(lines)
		for _, btn := range btns {
			if len(b) > 0 {
				b = append(b, ' ', ' ')
			}
			x0 := len(b)
			b = append(b, []rune("["+btn[0]+"]")...)
			s.hits = append(s.hits, tdHit{y: y, x0: x0, x1: len(b), kind: tdHitButton, arg: int([]rune(btn[1])[0])})
		}
		add(string(b))
	}
	const esc, enter = "\x1b", "\r"
	switch s.step {
	case tdStepFormat:
		add("Format")
		add("")
		for i, f := range s.formats {
			marker := "  ○ "
			if i == s.fmtPick {
				marker = "> ● "
			}
			addHit(marker+f.Title()+"  ."+f.Ext(), tdHitFormat, i)
		}
		add("")
		addButtons([2]string{"cancel", esc}, [2]string{"next", enter})
		add("")
		add("↑↓/click select · enter next · esc cancel")
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
			addHit(marker+tdPad(name, 8)+value, tdHitOpt, i)
		}
		add("")
		add("Rows is how many rows the file gets (1…" + strconv.Itoa(testdata.MaxRows) + ").")
		add("Seed 0 draws a fresh random seed; any other seed repeats byte for byte.")
		add("Table names the SQL table and the XML root element.")
		add("")
		addButtons([2]string{"back", esc}, [2]string{"next", enter})
		add("")
		add("tab/click next field · enter next · esc back")
	case tdStepFields:
		add(s.formats[s.fmtPick].Title() + " — columns")
		add("")
		n := len(s.spec.Fields)
		if n == 0 {
			add("  (no columns — press a or click [add])")
		}
		vis := m.tdListRows(n)
		top := tdWindow(&s.fieldTop, s.fieldPick, n, vis)
		if top > 0 {
			add(fmt.Sprintf("  ↑ %d more", top))
		}
		for i := top; i < top+vis && i < n; i++ {
			f := s.spec.Fields[i]
			marker := "  "
			if i == s.fieldPick {
				marker = "> "
			}
			row := marker + tdPad(f.Name, 16) + tdPad(string(f.Kind), 12)
			if f.Param != "" {
				row += f.Param
			}
			addHit(tdTrunc(row, avail+26), tdHitField, i)
		}
		if rest := n - top - vis; rest > 0 {
			add(fmt.Sprintf("  ↓ %d more", rest))
		}
		add("")
		addButtons([2]string{"back", esc}, [2]string{"add", "a"}, [2]string{"edit", "e"},
			[2]string{"delete", "d"}, [2]string{"generate", "g"})
		add("")
		add("↑↓/click select · enter/click again edit · a add · d delete · g generate · esc back")
	case tdStepField:
		title := "Edit column"
		if s.editIdx < 0 {
			title = "New column"
		}
		add(title)
		add("")
		info := testdata.Catalog()[s.editKind]
		for i, name := range tdEditNames {
			marker := "  "
			if i == s.editField {
				marker = "▸ "
			}
			var value string
			switch i {
			case tdEditName:
				value = windowedPlain(s.editName, avail)
				if i == s.editField {
					value = windowedInput(s.editName, s.editNPos, avail)
				}
			case tdEditKind:
				value = tdTrunc(string(info.Kind)+" — "+info.Desc, avail)
			case tdEditParam:
				if info.Param == "" {
					value = "(the kind takes none)"
				} else {
					value = windowedPlain(s.editParam, avail)
					if i == s.editField {
						value = windowedInput(s.editParam, s.editPPos, avail)
					}
				}
			}
			addHit(marker+tdPad(name, 8)+value, tdHitEdit, i)
		}
		add("")
		if info.Param != "" {
			add(tdTrunc("  param: "+info.Param, avail+8))
		}
		add("")
		addButtons([2]string{"cancel", esc}, [2]string{"choose kind", "\x00"}, [2]string{"ok", enter})
		add("")
		add("tab/↑↓ move · enter on Kind opens the catalog · enter accept · esc cancel")
	case tdStepKind:
		add("Kind — type to filter")
		add("")
		filtered := s.filteredCatalog()
		if s.kindSearch.Active() {
			add("  filter: " + s.kindSearch.Query())
			add("")
		}
		if len(filtered) == 0 {
			add("  (no kind matches — backspace edits the filter, esc clears it)")
		}
		if s.kindPick >= len(filtered) {
			s.kindPick = len(filtered) - 1
		}
		if s.kindPick < 0 {
			s.kindPick = 0
		}
		vis := m.tdListRows(len(filtered))
		top := tdWindow(&s.kindTop, s.kindPick, len(filtered), vis)
		if top > 0 {
			add(fmt.Sprintf("  ↑ %d more", top))
		}
		for i := top; i < top+vis && i < len(filtered); i++ {
			e := filtered[i]
			marker := "  "
			if i == s.kindPick {
				marker = "> "
			}
			addHit(tdTrunc(marker+tdPad(string(e.Kind), 12)+e.Desc, avail+14), tdHitCatalog, i)
		}
		if rest := len(filtered) - top - vis; rest > 0 {
			add(fmt.Sprintf("  ↓ %d more", rest))
		}
		add("")
		if len(filtered) > 0 {
			if p := filtered[s.kindPick].Param; p != "" {
				add(tdTrunc("  param: "+p, avail+8))
			} else {
				add("  takes no param")
			}
		}
		add("")
		addButtons([2]string{"back", esc}, [2]string{"choose", enter})
		add("")
		add("type filters · ↑↓ select · enter/click choose · esc back")
	}
	if s.running {
		lines = append(lines, "", "Generating…")
	} else if s.err != "" {
		lines = append(lines, "", "E: "+s.err)
	} else if s.note != "" {
		lines = append(lines, "", s.note)
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
		case s.step == tdStepKind && s.kindSearch.EscClears():
			s.kindSearch.Reset()
			s.kindPick, s.kindTop = 0, 0
			m.renderGenerateScratch()
		default:
			s.step--
			s.err, s.note = "", ""
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
			s.pickFormat((s.fmtPick + len(s.formats) - 1) % len(s.formats))
		case tea.KeyDown, tea.KeyTab:
			s.pickFormat((s.fmtPick + 1) % len(s.formats))
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
			s.focusOpt((s.optField + tdOptCount - 1) % tdOptCount)
		case msg.Code == tea.KeyTab, msg.Code == tea.KeyDown:
			s.focusOpt((s.optField + 1) % tdOptCount)
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
	case tdStepKind:
		return m.updateGenerateKindPick(msg)
	}
	return m, nil
}

// pickFormat moves the format selection and loads that format's preset.
func (s *tdGenState) pickFormat(i int) {
	s.fmtPick = i
	s.loadSpec(testdata.Preset(s.formats[i]))
}

// focusOpt moves the option-step focus, putting the cursor at the value's end.
func (s *tdGenState) focusOpt(i int) {
	s.optField = i
	s.optPos[i] = len([]rune(s.opt[i]))
}

// updateGenerateFields drives the column-list step: single-key actions, since
// no text field has the focus here. Enter edits the selected column like every
// other list here; generating is the deliberate `g` (or the [generate]
// button), so the file-writing action is never the reflex key (#2228 review).
func (m Model) updateGenerateFields(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.tdGen
	s.note = ""
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
	case msg.Code == tea.KeyEnter:
		// Enter on an empty list starts the first column instead of erroring.
		if n == 0 {
			s.openFieldEditor(-1)
		} else {
			s.openFieldEditor(s.fieldPick)
		}
	case msg.Code == 'd' && msg.Mod == 0 && n > 0:
		removed := s.spec.Fields[s.fieldPick].Name
		s.spec.Fields = append(s.spec.Fields[:s.fieldPick:s.fieldPick], s.spec.Fields[s.fieldPick+1:]...)
		if s.fieldPick >= len(s.spec.Fields) && s.fieldPick > 0 {
			s.fieldPick--
		}
		s.err = ""
		s.note = fmt.Sprintf("removed column %q — a re-adds one", removed)
	case msg.Code == 'g' && msg.Mod == 0:
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

// openFieldEditor moves to the column editor for idx (-1 = new column).
func (s *tdGenState) openFieldEditor(idx int) {
	s.editIdx = idx
	f := testdata.Field{Kind: testdata.Kinds()[0]}
	if idx >= 0 && idx < len(s.spec.Fields) {
		f = s.spec.Fields[idx]
	}
	s.editName, s.editParam = f.Name, f.Param
	s.editNPos = len([]rune(s.editName))
	s.editPPos = len([]rune(s.editParam))
	s.editKind = kindIndex(f.Kind)
	s.editField = 0
	s.step = tdStepField
	s.err = ""
}

// kindIndex locates a kind in the catalog; an unknown one lands on the first
// entry, which cannot happen for a validated spec.
func kindIndex(k testdata.Kind) int {
	for i, e := range testdata.Kinds() {
		if e == k {
			return i
		}
	}
	return 0
}

// updateGenerateFieldEdit drives the column editor. The Kind row is not a
// text field: enter on it (or a click) opens the catalog picker; enter
// elsewhere accepts the column. Arrows always move between rows — nothing
// cycles in place (#2228 review).
func (m Model) updateGenerateFieldEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.tdGen
	switch {
	case msg.Code == tea.KeyEnter && s.editField == tdEditKind:
		s.openKindPicker()
	case msg.Code == tea.KeyEnter:
		if err := s.acceptField(); err != nil {
			s.err = err.Error()
			m.renderGenerateScratch()
			return m, nil
		}
		s.err = ""
		s.step = tdStepFields
	case msg.Code == tea.KeyTab && msg.Mod&tea.ModShift != 0, msg.Code == tea.KeyUp:
		s.focusEdit((s.editField + tdEditCount - 1) % tdEditCount)
	case msg.Code == tea.KeyTab, msg.Code == tea.KeyDown:
		s.focusEdit((s.editField + 1) % tdEditCount)
	default:
		switch s.editField {
		case tdEditName:
			if out, pos, handled, _ := ui.EditKey(msg, s.editName, s.editNPos); handled {
				s.editName, s.editNPos = out, pos
			}
		case tdEditParam:
			if testdata.Catalog()[s.editKind].Param == "" {
				break // the kind takes none; nothing to type into
			}
			if out, pos, handled, _ := ui.EditKey(msg, s.editParam, s.editPPos); handled {
				s.editParam, s.editPPos = out, pos
			}
		}
	}
	m.renderGenerateScratch()
	return m, nil
}

// focusEdit moves the editor focus, putting the cursor at the value's end.
func (s *tdGenState) focusEdit(i int) {
	s.editField = i
	s.editNPos = len([]rune(s.editName))
	s.editPPos = len([]rune(s.editParam))
}

// openKindPicker moves to the catalog picker with the current kind selected
// and the filter cleared.
func (s *tdGenState) openKindPicker() {
	s.kindSearch.Reset()
	s.kindPick = s.editKind
	s.kindTop = 0
	s.step = tdStepKind
	s.err = ""
}

// updateGenerateKindPick drives the catalog picker: printable keys narrow the
// live filter, the navigation keys move the selection, enter chooses. Enter on
// an empty filtered list is a no-op — it must not accept a stale kind.
func (m Model) updateGenerateKindPick(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.tdGen
	filtered := s.filteredCatalog()
	switch {
	case msg.Code == tea.KeyEnter:
		if len(filtered) > 0 {
			s.chooseKind(filtered[min(s.kindPick, len(filtered)-1)].Kind)
		}
	default:
		if handled, changed := s.kindSearch.Key(msg); handled {
			if changed {
				s.kindPick, s.kindTop = 0, 0
			}
			break
		}
		nav := s.kindPick
		if ui.ListNav(msg.String(), &nav, len(filtered), m.tdListRows(len(filtered)), ui.NavDefault) {
			s.kindPick = nav
		}
	}
	m.renderGenerateScratch()
	return m, nil
}

// chooseKind folds a picked catalog kind back into the editor and returns to
// it, moving the focus to the parameter when the kind takes one (that is what
// the user fills in next) and to the name otherwise — never back onto the Kind
// row, where enter would reopen the picker. A parameter belongs to the kind it
// was typed for; carrying it to a kind that takes none would only be rejected
// on accept.
func (s *tdGenState) chooseKind(k testdata.Kind) {
	s.editKind = kindIndex(k)
	s.editField = tdEditName
	if info, ok := testdata.Info(k); ok {
		if info.Param == "" {
			s.editParam, s.editPPos = "", 0
		} else {
			s.editField = tdEditParam
		}
	}
	s.step = tdStepField
	s.err = ""
}

// acceptField validates the editor's column and folds it into the spec. The
// column is checked on its own — name, parameter grammar — so a typo is caught
// here rather than at generate time.
func (s *tdGenState) acceptField() error {
	f := testdata.Field{
		Name:  strings.TrimSpace(s.editName),
		Kind:  testdata.Kinds()[s.editKind],
		Param: strings.TrimSpace(s.editParam),
	}
	if f.Name == "" {
		return fmt.Errorf("field name is required")
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

// mouseGenerateScratch answers a mouse event landing on the open wizard
// (#2228): cx/cy are content-local, already scroll-adjusted. A left press acts
// on the hit region under the pointer — recorded per line by the last render —
// and the wheel moves the current step's list selection. It reports whether it
// consumed the event; the caller keeps the border-resize and click-outside
// behavior.
func (m Model) mouseGenerateScratch(msg mouseEvent, cx, cy int) (Model, tea.Cmd, bool) {
	s := m.tdGen
	if s.running {
		return m, nil, true
	}
	if msg.action == mouseWheel {
		delta := wheelLines * msg.ticks()
		switch msg.Button {
		case tea.MouseWheelUp:
			delta = -delta
		case tea.MouseWheelDown:
		default:
			return m, nil, false
		}
		m.wheelGenerateScratch(delta)
		return m, nil, true
	}
	if msg.action != mousePress || msg.Button != tea.MouseLeft {
		return m, nil, false
	}
	for _, h := range s.hits {
		if h.y != cy || cx < h.x0 || cx >= h.x1 {
			continue
		}
		return m.clickGenerateHit(h)
	}
	return m, nil, true
}

// clickGenerateHit acts on one clicked region. List rows select on the first
// click and activate on a click at the already-selected row (advance for a
// format, edit for a column); a kind row chooses directly, dropdown-style; the
// Kind editor row opens the picker; buttons replay the key they are labelled
// with.
func (m Model) clickGenerateHit(h tdHit) (Model, tea.Cmd, bool) {
	s := m.tdGen
	key := func(code rune) (Model, tea.Cmd, bool) {
		k := tea.Key{Code: code, Text: string(code)}
		switch code {
		case '\r':
			k = tea.Key{Code: tea.KeyEnter}
		case '\x1b':
			k = tea.Key{Code: tea.KeyEscape}
		}
		out, cmd := m.updateGenerateScratch(tea.KeyPressMsg(k))
		return out.(Model), cmd, true
	}
	switch h.kind {
	case tdHitButton:
		// The editor's [choose kind] button opens the picker regardless of
		// which row has the focus; it has no key equivalent to replay.
		if h.arg == 0 && s.step == tdStepField {
			s.openKindPicker()
			break
		}
		return key(rune(h.arg))
	case tdHitFormat:
		if s.fmtPick == h.arg {
			return key('\r')
		}
		s.pickFormat(h.arg)
		s.err = ""
	case tdHitOpt:
		s.focusOpt(h.arg)
	case tdHitField:
		if s.fieldPick == h.arg {
			s.openFieldEditor(h.arg)
		} else {
			s.fieldPick = h.arg
		}
		s.note = ""
	case tdHitEdit:
		if h.arg == tdEditKind {
			s.openKindPicker()
		} else {
			s.focusEdit(h.arg)
		}
	case tdHitCatalog:
		filtered := s.filteredCatalog()
		if h.arg >= 0 && h.arg < len(filtered) {
			s.chooseKind(filtered[h.arg].Kind)
		}
	}
	m.renderGenerateScratch()
	return m, nil, true
}

// wheelGenerateScratch moves the current step's list selection by delta rows —
// the wheel scrolls what the arrows walk, so the list window follows and the
// selection can never scroll out of reach.
func (m *Model) wheelGenerateScratch(delta int) {
	s := m.tdGen
	move := func(pick *int, n int) {
		if n == 0 {
			return
		}
		p := *pick + delta
		if p < 0 {
			p = 0
		}
		if p >= n {
			p = n - 1
		}
		*pick = p
	}
	switch s.step {
	case tdStepFormat:
		old := s.fmtPick
		move(&s.fmtPick, len(s.formats))
		if s.fmtPick != old {
			s.pickFormat(s.fmtPick)
		}
	case tdStepFields:
		move(&s.fieldPick, len(s.spec.Fields))
	case tdStepKind:
		move(&s.kindPick, len(s.filteredCatalog()))
	default:
		return
	}
	m.renderGenerateScratch()
}

// pasteGenerateScratch inserts a paste into the focused text field (#1873);
// the list steps have no input to paste into.
func (m *Model) pasteGenerateScratch(text string) bool {
	s := m.tdGen
	if s == nil || s.running {
		return false
	}
	var field *string
	var pos *int
	switch {
	case s.step == tdStepOptions:
		field, pos = &s.opt[s.optField], &s.optPos[s.optField]
	case s.step == tdStepField && s.editField == tdEditName:
		field, pos = &s.editName, &s.editNPos
	case s.step == tdStepField && s.editField == tdEditParam:
		if testdata.Catalog()[s.editKind].Param == "" {
			return false
		}
		field, pos = &s.editParam, &s.editPPos
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

// tdPad left-aligns a label in n columns, rune-counted so a non-ASCII column
// name does not break the alignment.
func tdPad(s string, n int) string {
	if r := len([]rune(s)); r < n {
		return s + strings.Repeat(" ", n-r)
	}
	return s + " "
}

// tdTrunc clips a line to n columns with an ellipsis, so a long description
// or parameter never overflows the dialog on a narrow terminal.
func tdTrunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}
