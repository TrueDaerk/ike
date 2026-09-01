package app

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/scratch"
	"ike/internal/testdata"
	"ike/internal/ui"
)

// scratch_generate.go is the UI half of the test-data generator (#2134,
// wizard #2228, DSL rework #2392): `scratch.generate` opens a single-screen
// modal shell dialog — template/format/rows/seed/table header, a multi-line
// DSL spec editor with autocomplete, and a debounced live preview — and
// writes the rendered document into a fresh scratch.
//
// The dialog follows the shell-dialog family (new-project wizard #1718,
// scratch manager #2256) rather than the settings SubPanel form: modal, no
// page host, fully mouse-operable — every rendered line records what it
// targets (a header field, an editor line, a suggestion, a button), so a
// click acts on what it visibly hits. Generation itself runs as a tea.Cmd —
// a million rows must not be rendered on the update loop — and so does the
// preview, which re-renders (debounced) whenever the spec or a header knob
// changes. A spec that does not parse shows its error, with the offending
// line, where the preview would be, and generation is refused until it is
// fixed.

// Focus zones, cycled by tab/shift-tab.
const (
	tdFocTemplate = iota
	tdFocFormat
	tdFocRows
	tdFocSeed
	tdFocTable
	tdFocEditor
	tdFocCount
)

// Header text-field indices (focus - tdFocRows).
const (
	tdHdrRows = iota
	tdHdrSeed
	tdHdrTable
	tdHdrCount
)

// tdHdrNames labels the header rows; the picker rows carry their own labels.
var tdHdrNames = [tdHdrCount]string{"Rows", "Seed", "Table"}

// tdHitKind tags what a rendered line (or a span of it) targets, so the click
// handler acts on what the user visibly hit instead of re-deriving the layout.
type tdHitKind int

const (
	tdHitHeader tdHitKind = iota // arg: focus index (template..table)
	tdHitLine                    // arg: editor line index
	tdHitAC                      // arg: index into the current suggestions
	tdHitButton                  // arg: a tdBtn* id
)

// Button ids for tdHitButton.
const (
	tdBtnCancel = iota
	tdBtnGenerate
	tdBtnSaveTpl
	tdBtnDeleteTpl
	tdBtnPromptOK
	tdBtnPromptCancel
)

// tdHit is one clickable region: the body line it lies on, its column span
// ([x0, x1)), what it targets and which one. Whole-line targets span the line.
type tdHit struct {
	y, x0, x1 int
	kind      tdHitKind
	arg       int
}

// GenerateScratchMsg asks the root model to open the generator dialog.
type GenerateScratchMsg struct{}

// scratchGenDoneMsg carries a finished generation back into Update.
type scratchGenDoneMsg struct {
	path string
	rows int
	err  error
}

// tdPreviewTickMsg fires the debounced preview; a stale generation means the
// user kept typing.
type tdPreviewTickMsg struct{ gen int }

// tdPreviewDoneMsg carries an off-loop preview render back into Update.
type tdPreviewDoneMsg struct {
	gen  int
	text string
	err  string
}

// tdPreviewDebounce is how long the spec must sit quiet before the preview
// re-renders. A var so tests can shorten it.
var tdPreviewDebounce = 250 * time.Millisecond

// tdACItem is one autocomplete suggestion.
type tdACItem struct {
	insert string // text inserted at the cursor
	back   int    // cursor steps back after inserting (into the parens)
	label  string // list rendering: name + parameter grammar
	desc   string // one-line catalog description
}

// tdGenState is the open dialog; nil when it is closed.
type tdGenState struct {
	formats []testdata.Format
	fmtPick int

	// tpls is the template list (built-ins + user); tplPick indexes the
	// picker where 0 is "(custom)" and i+1 is tpls[i].
	tpls    []testdata.Template
	tplPick int

	hdr    [tdHdrCount]string
	hdrPos [tdHdrCount]int

	focus int

	// The DSL editor: lines of text with a rune cursor at (curL, curC).
	lines []string
	curL  int
	curC  int

	// Autocomplete over the editor: suggestions for the token at the cursor.
	acOpen  bool
	acItems []tdACItem
	acPick  int

	// The debounced preview: prevGen stamps the run in flight, prevText is
	// the last rendered preview, specErr the parse/validation error shown in
	// its place ("" when the spec is generatable).
	prevGen  int
	prevText string
	specErr  string
	prevRun  bool // a preview render is in flight

	// The save-template name prompt, opened by ctrl+s / [save template].
	savePrompt bool
	saveName   string
	savePos    int

	// hits are the clickable regions of the last render, in body-line
	// coordinates — rebuilt by every renderGenerateScratch call.
	hits []tdHit

	running bool
	err     string
	// note is a non-error status line ("saved template …"); the next action
	// clears it.
	note string
}

// generateCommands registers the single generator entry point (#2392 removed
// the per-format quick commands — the palette keeps one row, the dialog picks
// the format).
func generateCommands() []plugin.Command {
	return []plugin.Command{
		appCommand("scratch.generate", "Generate Test Data…", GenerateScratchMsg{}),
	}
}

// startGenerateScratch opens the dialog on the last used spec (or the stock
// default) and schedules the first preview render.
func (m *Model) startGenerateScratch() tea.Cmd {
	s := &tdGenState{formats: testdata.Formats(), tpls: testdata.Templates()}
	spec := testdata.LastSpec()
	for i, f := range s.formats {
		if f == spec.Format {
			s.fmtPick = i
		}
	}
	s.hdr[tdHdrRows] = strconv.Itoa(spec.Rows)
	s.hdr[tdHdrSeed] = strconv.FormatUint(spec.Seed, 10)
	s.hdr[tdHdrTable] = spec.Table
	for i := range s.hdr {
		s.hdrPos[i] = len([]rune(s.hdr[i]))
	}
	s.setText(spec.DSL)
	s.focus = tdFocEditor
	m.tdGen = s
	cmd := s.dirtyPreview()
	m.renderGenerateScratch()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	return cmd
}

// generateScratchOpen reports whether the shell currently shows the dialog.
func (m Model) generateScratchOpen() bool { return m.tdGen != nil && m.shell.IsOpen() }

// closeGenerateScratch clears the dialog state and the shell.
func (m *Model) closeGenerateScratch() {
	m.tdGen = nil
	m.shell.Close()
}

// text joins the editor lines back into the DSL body.
func (s *tdGenState) text() string { return strings.Join(s.lines, "\n") }

// setText loads a DSL body into the editor, cursor at the start.
func (s *tdGenState) setText(dsl string) {
	s.lines = strings.Split(strings.TrimSuffix(dsl, "\n"), "\n")
	if len(s.lines) == 0 {
		s.lines = []string{""}
	}
	s.curL, s.curC = 0, 0
	s.acOpen = false
}

// dirtyPreview stamps a new preview generation and returns the debounce tick.
// Every spec-affecting change runs through it, so a stale tick is dropped by
// its generation.
func (s *tdGenState) dirtyPreview() tea.Cmd {
	s.prevGen++
	gen := s.prevGen
	return tea.Tick(tdPreviewDebounce, func(time.Time) tea.Msg {
		return tdPreviewTickMsg{gen: gen}
	})
}

// compose folds the header fields and the editor text into a spec and
// validates it. Every generate and preview runs it, so a bad row count, a bad
// seed and a DSL error are refused in one place with one message.
func (s *tdGenState) compose() (testdata.Spec, error) {
	spec := testdata.Spec{Format: s.formats[s.fmtPick], DSL: s.text()}
	rowsText := strings.TrimSpace(s.hdr[tdHdrRows])
	rows, err := strconv.Atoi(rowsText)
	if err != nil {
		return spec, fmt.Errorf("row count %q is not a number", rowsText)
	}
	spec.Rows = rows
	seedText := strings.TrimSpace(s.hdr[tdHdrSeed])
	if seedText == "" {
		seedText = "0"
	}
	seed, err := strconv.ParseUint(seedText, 10, 64)
	if err != nil {
		return spec, fmt.Errorf("seed %q is not a non-negative number", seedText)
	}
	spec.Seed = seed
	spec.Table = strings.TrimSpace(s.hdr[tdHdrTable])
	if err := spec.Validate(); err != nil {
		return spec, err
	}
	return spec.Normalized(), nil
}

// fireGeneratePreview answers the debounce tick: a stale generation is
// dropped, an invalid spec shows its error in the preview area, a valid one
// renders off the update loop.
func (m *Model) fireGeneratePreview(msg tdPreviewTickMsg) tea.Cmd {
	s := m.tdGen
	if s == nil || msg.gen != s.prevGen {
		return nil
	}
	spec, err := s.compose()
	if err != nil {
		s.specErr = err.Error()
		s.prevText = ""
		m.renderGenerateScratch()
		return nil
	}
	s.specErr = ""
	s.prevRun = true
	gen := s.prevGen
	m.renderGenerateScratch()
	return func() tea.Msg {
		data, err := testdata.Preview(spec)
		if err != nil {
			return tdPreviewDoneMsg{gen: gen, err: err.Error()}
		}
		return tdPreviewDoneMsg{gen: gen, text: string(data)}
	}
}

// finishGeneratePreview folds a finished preview render back into the dialog.
func (m *Model) finishGeneratePreview(msg tdPreviewDoneMsg) {
	s := m.tdGen
	if s == nil || msg.gen != s.prevGen {
		return
	}
	s.prevRun = false
	if msg.err != "" {
		s.specErr = msg.err
		s.prevText = ""
	} else {
		s.prevText = msg.text
	}
	m.renderGenerateScratch()
}

// tdValueWidth is the width budget of a text-field value or an editor line:
// the window minus the dialog chrome and the label column, so a narrow
// terminal clips values instead of overflowing the box.
func (m *Model) tdValueWidth() int {
	avail := m.width - 30
	if avail < 20 {
		avail = 20
	}
	return avail
}

// tplName renders the template picker's current value.
func (s *tdGenState) tplName() string {
	if s.tplPick == 0 {
		return "(custom)"
	}
	return s.tpls[s.tplPick-1].Name
}

// curTemplate returns the selected template, nil for "(custom)".
func (s *tdGenState) curTemplate() *testdata.Template {
	if s.tplPick == 0 {
		return nil
	}
	return &s.tpls[s.tplPick-1]
}

// cycleTemplate moves the template picker by delta (wrapping) and loads the
// picked template's body into the editor. "(custom)" keeps the current text —
// edits never write back into a template unless saved again.
func (s *tdGenState) cycleTemplate(delta int) tea.Cmd {
	n := len(s.tpls) + 1
	s.tplPick = (s.tplPick + delta%n + n) % n
	if t := s.curTemplate(); t != nil {
		s.setText(t.DSL)
		return s.dirtyPreview()
	}
	return nil
}

// cycleFormat moves the format picker by delta (wrapping).
func (s *tdGenState) cycleFormat(delta int) tea.Cmd {
	n := len(s.formats)
	s.fmtPick = (s.fmtPick + delta%n + n) % n
	return s.dirtyPreview()
}

// renderGenerateScratch (re)fills the shell and rebuilds the click map: every
// clickable line registers a hit while it is added, so the hit-test can never
// drift from the rendering.
func (m *Model) renderGenerateScratch() {
	s := m.tdGen
	avail := m.tdValueWidth()
	var lines []string
	s.hits = s.hits[:0]
	add := func(l string) { lines = append(lines, l) }
	addHit := func(l string, kind tdHitKind, arg int) {
		s.hits = append(s.hits, tdHit{y: len(lines), x0: 0, x1: len([]rune(l)), kind: kind, arg: arg})
		add(l)
	}
	// addButtons renders one line of "[label]" buttons, each its own span.
	addButtons := func(btns ...[2]any) { // {label string, id int}
		var b []rune
		y := len(lines)
		for _, btn := range btns {
			if len(b) > 0 {
				b = append(b, ' ', ' ')
			}
			x0 := len(b)
			b = append(b, []rune("["+btn[0].(string)+"]")...)
			s.hits = append(s.hits, tdHit{y: y, x0: x0, x1: len(b), kind: tdHitButton, arg: btn[1].(int)})
		}
		add(string(b))
	}
	marker := func(f int) string {
		if s.focus == f && !s.savePrompt {
			return "▸ "
		}
		return "  "
	}
	picker := func(v string, focused bool) string {
		if focused {
			return "◂ " + v + " ▸"
		}
		return v
	}

	// Header: template and format pickers, then the three text fields.
	addHit(marker(tdFocTemplate)+tdPad("Template", 9)+picker(s.tplName(), s.focus == tdFocTemplate), tdHitHeader, tdFocTemplate)
	f := s.formats[s.fmtPick]
	addHit(marker(tdFocFormat)+tdPad("Format", 9)+picker(f.Title(), s.focus == tdFocFormat)+"  ."+f.Ext(), tdHitHeader, tdFocFormat)
	for i, name := range tdHdrNames {
		foc := tdFocRows + i
		value := windowedPlain(s.hdr[i], avail)
		if s.focus == foc && !s.savePrompt {
			value = windowedInput(s.hdr[i], s.hdrPos[i], avail)
		}
		addHit(marker(foc)+tdPad(name, 9)+value, tdHitHeader, foc)
	}
	add("")

	// The DSL editor: gutter line numbers match the error messages.
	add(marker(tdFocEditor) + "Spec — name = expression, e.g. host = hostname({domain})")
	for i, l := range s.lines {
		gutter := fmt.Sprintf("%2d │ ", i+1)
		text := windowedPlain(l, avail)
		if i == s.curL && s.focus == tdFocEditor && !s.savePrompt && !s.running {
			text = windowedInput(l, s.curC, avail)
		}
		addHit("  "+gutter+text, tdHitLine, i)
	}
	if s.acOpen && len(s.acItems) > 0 {
		for i, it := range s.acItems {
			mark := "    "
			if i == s.acPick {
				mark = "  ↳ "
			}
			row := mark + tdPad(it.label, 28) + it.desc
			addHit(tdTrunc(row, avail+24), tdHitAC, i)
		}
	}
	add("")

	// The live preview, or the spec error in its place.
	switch {
	case s.specErr != "":
		add("Preview — the spec cannot generate:")
		add("  E: " + tdTrunc(s.specErr, avail+20))
	case s.prevText == "" && s.prevRun:
		add("Preview (" + f.Title() + ") — rendering…")
	default:
		title := fmt.Sprintf("Preview (%s, first %d rows)", f.Title(), testdata.PreviewRows)
		add(title)
		prev := strings.Split(strings.TrimRight(s.prevText, "\n"), "\n")
		const maxPrevLines = 12
		for i, l := range prev {
			if i == maxPrevLines {
				add(fmt.Sprintf("  … %d more lines", len(prev)-maxPrevLines))
				break
			}
			add("  " + tdTrunc(strings.ReplaceAll(l, "\t", "    "), avail+20))
		}
	}
	add("")

	// The button row, or the save-template name prompt while it is open.
	if s.savePrompt {
		add("Template name: " + windowedInput(s.saveName, s.savePos, avail))
		addButtons([2]any{"save", tdBtnPromptOK}, [2]any{"cancel", tdBtnPromptCancel})
		add("")
		add("enter save · esc cancel")
	} else {
		btns := [][2]any{{"cancel", tdBtnCancel}, {"save template", tdBtnSaveTpl}}
		if t := s.curTemplate(); t != nil && !t.BuiltIn {
			btns = append(btns, [2]any{"delete template", tdBtnDeleteTpl})
		}
		btns = append(btns, [2]any{"generate", tdBtnGenerate})
		addButtons(btns...)
		add("")
		add("tab field · ←→ pick · ^space suggest · ^g generate · ^s save template · esc close")
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

// updateGenerateScratch consumes every key while the dialog is open.
func (m Model) updateGenerateScratch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.tdGen
	if s.running {
		if msg.Code == tea.KeyEscape {
			m.closeGenerateScratch()
		}
		return m, nil
	}
	if s.savePrompt {
		return m.updateGenerateSavePrompt(msg)
	}
	s.note = ""
	switch {
	case msg.Code == tea.KeyEscape:
		if s.acOpen {
			s.acOpen = false
			m.renderGenerateScratch()
			return m, nil
		}
		m.closeGenerateScratch()
		return m, nil
	case msg.String() == "ctrl+g":
		return m.generateNow()
	case msg.String() == "ctrl+s":
		s.openSavePrompt()
		m.renderGenerateScratch()
		return m, nil
	case msg.String() == "ctrl+d":
		m.deleteCurrentTemplate()
		m.renderGenerateScratch()
		return m, nil
	case msg.Code == tea.KeyTab && s.acOpen && s.focus == tdFocEditor:
		cmd := s.acceptAC()
		m.renderGenerateScratch()
		return m, cmd
	case msg.Code == tea.KeyTab && msg.Mod&tea.ModShift != 0:
		s.setFocus((s.focus + tdFocCount - 1) % tdFocCount)
		m.renderGenerateScratch()
		return m, nil
	case msg.Code == tea.KeyTab:
		s.setFocus((s.focus + 1) % tdFocCount)
		m.renderGenerateScratch()
		return m, nil
	}
	var cmd tea.Cmd
	switch s.focus {
	case tdFocTemplate:
		switch msg.Code {
		case tea.KeyLeft:
			cmd = s.cycleTemplate(-1)
		case tea.KeyRight, tea.KeyEnter:
			cmd = s.cycleTemplate(1)
		case tea.KeyUp:
			s.setFocus(tdFocCount - 1)
		case tea.KeyDown:
			s.setFocus(tdFocFormat)
		}
	case tdFocFormat:
		switch msg.Code {
		case tea.KeyLeft:
			cmd = s.cycleFormat(-1)
		case tea.KeyRight, tea.KeyEnter:
			cmd = s.cycleFormat(1)
		case tea.KeyUp:
			s.setFocus(tdFocTemplate)
		case tea.KeyDown:
			s.setFocus(tdFocRows)
		}
	case tdFocRows, tdFocSeed, tdFocTable:
		i := s.focus - tdFocRows
		switch msg.Code {
		case tea.KeyUp:
			s.setFocus(s.focus - 1)
		case tea.KeyDown, tea.KeyEnter:
			s.setFocus(s.focus + 1)
		default:
			if out, pos, handled, changed := ui.EditKey(msg, s.hdr[i], s.hdrPos[i]); handled {
				s.hdr[i], s.hdrPos[i] = out, pos
				if changed {
					cmd = s.dirtyPreview()
				}
			}
		}
	case tdFocEditor:
		return m.updateGenerateEditor(msg)
	}
	s.err = ""
	m.renderGenerateScratch()
	return m, cmd
}

// setFocus moves the focus zone, closing the autocomplete and putting text
// cursors at their value's end.
func (s *tdGenState) setFocus(f int) {
	s.focus = f
	s.acOpen = false
	for i := range s.hdr {
		s.hdrPos[i] = len([]rune(s.hdr[i]))
	}
}

// updateGenerateEditor drives the DSL editor: multi-line cursor movement,
// per-line editing through ui.EditKey, and the autocomplete popup, whose
// up/down/enter/tab outrank the cursor keys while it is open.
func (m Model) updateGenerateEditor(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.tdGen
	s.err = ""
	line := s.lines[s.curL]
	r := []rune(line)
	if s.curC > len(r) {
		s.curC = len(r)
	}
	var cmd tea.Cmd
	switch {
	case s.acOpen && msg.Code == tea.KeyUp:
		s.acPick = (s.acPick + len(s.acItems) - 1) % len(s.acItems)
	case s.acOpen && msg.Code == tea.KeyDown:
		s.acPick = (s.acPick + 1) % len(s.acItems)
	case s.acOpen && msg.Code == tea.KeyEnter:
		cmd = s.acceptAC()
	case msg.String() == "ctrl+space":
		s.computeAC(true)
	case msg.Code == tea.KeyUp:
		if s.curL > 0 {
			s.curL--
			s.clampCol()
		} else {
			s.setFocus(tdFocTable)
		}
	case msg.Code == tea.KeyDown:
		if s.curL < len(s.lines)-1 {
			s.curL++
			s.clampCol()
		}
	case msg.Code == tea.KeyLeft && s.curC == 0 && s.curL > 0:
		s.curL--
		s.curC = len([]rune(s.lines[s.curL]))
		s.acOpen = false
	case msg.Code == tea.KeyRight && s.curC == len(r) && s.curL < len(s.lines)-1:
		s.curL++
		s.curC = 0
		s.acOpen = false
	case msg.Code == tea.KeyEnter:
		s.lines = append(s.lines[:s.curL+1], append([]string{string(r[s.curC:])}, s.lines[s.curL+1:]...)...)
		s.lines[s.curL] = string(r[:s.curC])
		s.curL++
		s.curC = 0
		s.acOpen = false
		cmd = s.dirtyPreview()
	case msg.Code == tea.KeyBackspace && s.curC == 0 && s.curL > 0:
		prev := []rune(s.lines[s.curL-1])
		s.lines[s.curL-1] = string(prev) + line
		s.lines = append(s.lines[:s.curL], s.lines[s.curL+1:]...)
		s.curL--
		s.curC = len(prev)
		s.acOpen = false
		cmd = s.dirtyPreview()
	case msg.Code == tea.KeyDelete && s.curC == len(r) && s.curL < len(s.lines)-1:
		s.lines[s.curL] = line + s.lines[s.curL+1]
		s.lines = append(s.lines[:s.curL+1], s.lines[s.curL+2:]...)
		s.acOpen = false
		cmd = s.dirtyPreview()
	default:
		out, pos, handled, changed := ui.EditKey(msg, line, s.curC)
		if handled {
			s.lines[s.curL] = out
			s.curC = pos
			if changed {
				cmd = s.dirtyPreview()
				s.computeAC(false)
			} else {
				s.acOpen = false // a plain cursor move dismisses the popup
			}
		}
	}
	m.renderGenerateScratch()
	return m, cmd
}

// clampCol keeps the cursor inside the current line after a vertical move,
// and closes the autocomplete (its context line changed).
func (s *tdGenState) clampCol() {
	if n := len([]rune(s.lines[s.curL])); s.curC > n {
		s.curC = n
	}
	s.acOpen = false
}

// fieldsAbove lists the field names defined on the lines above the cursor —
// what a {reference} suggestion may offer.
func (s *tdGenState) fieldsAbove() []string {
	var out []string
	for i := 0; i < s.curL; i++ {
		name, _, ok := strings.Cut(s.lines[i], "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if testdata.ValidFieldName(name) {
			out = append(out, name)
		}
	}
	return out
}

// computeAC rebuilds the suggestion list for the token at the cursor: inside
// an open "{" it offers the fields defined above; after the line's "=" it
// offers the generator catalog (with descriptions and parameter grammars)
// plus weighted. force opens the popup even on an empty token (ctrl+space).
func (s *tdGenState) computeAC(force bool) {
	s.acItems = s.acItems[:0]
	s.acPick = 0
	s.acOpen = false
	r := []rune(s.lines[s.curL])
	if s.curC > len(r) {
		s.curC = len(r)
	}
	before := string(r[:s.curC])
	if i := strings.LastIndex(before, "{"); i >= 0 && !strings.Contains(before[i:], "}") {
		prefix := strings.ToLower(before[i+1:])
		for _, name := range s.fieldsAbove() {
			if strings.HasPrefix(strings.ToLower(name), prefix) {
				s.acItems = append(s.acItems, tdACItem{
					insert: name[len(prefix):] + "}",
					label:  "{" + name + "}",
					desc:   "field reference",
				})
			}
		}
		s.acOpen = len(s.acItems) > 0 && (force || prefix != "")
		return
	}
	eq := strings.Index(before, "=")
	if eq < 0 {
		return // still typing the field name — nothing to offer
	}
	// The trailing identifier is the token being completed.
	start := len(before)
	for start > 0 {
		c := before[start-1]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
			start--
			continue
		}
		break
	}
	if start <= eq {
		return // the cursor sits before the '='
	}
	tok := strings.ToLower(before[start:])
	if tok == "" && !force {
		return
	}
	entries := append(testdata.Catalog(), testdata.WeightedInfo())
	for _, e := range entries {
		name := string(e.Kind)
		if !strings.HasPrefix(name, tok) {
			continue
		}
		it := tdACItem{insert: name[len(tok):] + "()", label: name + "()", desc: e.Desc}
		if e.Param != "" {
			it.back = 1
			it.label = name + "(" + e.Param + ")"
		}
		s.acItems = append(s.acItems, it)
	}
	s.acOpen = len(s.acItems) > 0
}

// acceptAC inserts the picked suggestion at the cursor.
func (s *tdGenState) acceptAC() tea.Cmd {
	if !s.acOpen || s.acPick >= len(s.acItems) {
		s.acOpen = false
		return nil
	}
	it := s.acItems[s.acPick]
	r := []rune(s.lines[s.curL])
	if s.curC > len(r) {
		s.curC = len(r)
	}
	ins := []rune(it.insert)
	s.lines[s.curL] = string(r[:s.curC]) + it.insert + string(r[s.curC:])
	s.curC += len(ins) - it.back
	s.acOpen = false
	return s.dirtyPreview()
}

// generateNow validates and starts the generation; an invalid spec keeps the
// dialog open with the reason.
func (m Model) generateNow() (tea.Model, tea.Cmd) {
	s := m.tdGen
	spec, err := s.compose()
	if err != nil {
		s.err = err.Error()
		s.specErr = err.Error()
		s.prevText = ""
		m.renderGenerateScratch()
		return m, nil
	}
	s.err = ""
	s.running = true
	m.renderGenerateScratch()
	return m, generateScratchCmd(spec)
}

// openSavePrompt opens the template name prompt, prefilled with the selected
// template's name so re-saving an edited user template is one enter.
func (s *tdGenState) openSavePrompt() {
	s.savePrompt = true
	s.saveName = ""
	if t := s.curTemplate(); t != nil && !t.BuiltIn {
		s.saveName = t.Name
	}
	s.savePos = len([]rune(s.saveName))
	s.acOpen = false
}

// updateGenerateSavePrompt drives the template name prompt.
func (m Model) updateGenerateSavePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.tdGen
	switch msg.Code {
	case tea.KeyEscape:
		s.savePrompt = false
	case tea.KeyEnter:
		if err := testdata.SaveTemplate(s.saveName, s.text()+"\n"); err != nil {
			s.err = err.Error()
			break
		}
		name := strings.TrimSpace(s.saveName)
		s.savePrompt = false
		s.err = ""
		s.note = fmt.Sprintf("saved template %q", name)
		s.tpls = testdata.Templates()
		s.tplPick = 0
		for i, t := range s.tpls {
			if t.Name == name {
				s.tplPick = i + 1
			}
		}
	default:
		if out, pos, handled, _ := ui.EditKey(msg, s.saveName, s.savePos); handled {
			s.saveName, s.savePos = out, pos
		}
	}
	m.renderGenerateScratch()
	return m, nil
}

// deleteCurrentTemplate removes the selected user template; built-ins and
// "(custom)" refuse with a note.
func (m *Model) deleteCurrentTemplate() {
	s := m.tdGen
	t := s.curTemplate()
	switch {
	case t == nil:
		s.err = "no template selected — pick one in the Template field first"
	case t.BuiltIn:
		s.err = fmt.Sprintf("%q is a built-in template and cannot be deleted", t.Name)
	default:
		name := t.Name
		testdata.DeleteTemplate(name)
		s.tpls = testdata.Templates()
		s.tplPick = 0 // the editor keeps the text as "(custom)"
		s.err = ""
		s.note = fmt.Sprintf("deleted template %q", name)
	}
}

// mouseGenerateScratch answers a left press landing on the open dialog:
// cx/cy are content-local, already scroll-adjusted. A press acts on the hit
// region under the pointer, recorded per line by the last render. The wheel
// stays with the shell's viewport — the dialog is one scrollable page, not a
// windowed list.
func (m Model) mouseGenerateScratch(msg mouseEvent, cx, cy int) (Model, tea.Cmd, bool) {
	s := m.tdGen
	if s.running {
		return m, nil, true
	}
	if msg.action != mousePress || msg.Button != tea.MouseLeft {
		return m, nil, false
	}
	for _, h := range s.hits {
		if h.y != cy || cx < h.x0 || cx >= h.x1 {
			continue
		}
		return m.clickGenerateHit(h, cx)
	}
	return m, nil, true
}

// clickGenerateHit acts on one clicked region: header fields focus (a second
// click on a picker cycles it), editor lines place the cursor, suggestion
// rows accept, buttons run their action.
func (m Model) clickGenerateHit(h tdHit, cx int) (Model, tea.Cmd, bool) {
	s := m.tdGen
	s.note = ""
	var cmd tea.Cmd
	switch h.kind {
	case tdHitButton:
		if s.savePrompt {
			switch h.arg {
			case tdBtnPromptOK:
				out, c := m.updateGenerateSavePrompt(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
				return out.(Model), c, true
			case tdBtnPromptCancel:
				s.savePrompt = false
			}
			break
		}
		switch h.arg {
		case tdBtnCancel:
			m.closeGenerateScratch()
			return m, nil, true
		case tdBtnGenerate:
			out, c := m.generateNow()
			return out.(Model), c, true
		case tdBtnSaveTpl:
			s.openSavePrompt()
		case tdBtnDeleteTpl:
			m.deleteCurrentTemplate()
		}
	case tdHitHeader:
		if s.savePrompt {
			break
		}
		if s.focus == h.arg {
			switch h.arg {
			case tdFocTemplate:
				cmd = s.cycleTemplate(1)
			case tdFocFormat:
				cmd = s.cycleFormat(1)
			}
		} else {
			s.setFocus(h.arg)
		}
	case tdHitLine:
		if s.savePrompt {
			break
		}
		s.setFocus(tdFocEditor)
		s.curL = h.arg
		// The editor gutter is "  NN │ " — 7 cells before the text.
		col := cx - 7
		if col < 0 {
			col = 0
		}
		if n := len([]rune(s.lines[s.curL])); col > n {
			col = n
		}
		s.curC = col
	case tdHitAC:
		if h.arg >= 0 && h.arg < len(s.acItems) {
			s.acPick = h.arg
			cmd = s.acceptAC()
		}
	}
	m.renderGenerateScratch()
	return m, cmd, true
}

// pasteGenerateScratch inserts a paste into the focused input (#1873). The
// editor takes multi-line pastes verbatim — pasting a whole spec is the
// fastest way in; the header fields and the name prompt flatten like every
// single-line field.
func (m *Model) pasteGenerateScratch(text string) bool {
	s := m.tdGen
	if s == nil || s.running {
		return false
	}
	if s.savePrompt {
		out, np, changed := ui.PasteText(s.saveName, s.savePos, text)
		if !changed {
			return false
		}
		s.saveName, s.savePos = out, np
		m.renderGenerateScratch()
		return true
	}
	switch {
	case s.focus >= tdFocRows && s.focus <= tdFocTable:
		i := s.focus - tdFocRows
		out, np, changed := ui.PasteText(s.hdr[i], s.hdrPos[i], text)
		if !changed {
			return false
		}
		s.hdr[i], s.hdrPos[i] = out, np
	case s.focus == tdFocEditor:
		ins := strings.ReplaceAll(text, "\r\n", "\n")
		ins = strings.ReplaceAll(ins, "\r", "\n")
		parts := strings.Split(ins, "\n")
		r := []rune(s.lines[s.curL])
		if s.curC > len(r) {
			s.curC = len(r)
		}
		tail := string(r[s.curC:])
		s.lines[s.curL] = string(r[:s.curC]) + parts[0]
		for i := 1; i < len(parts); i++ {
			s.curL++
			s.lines = append(s.lines[:s.curL], append([]string{parts[i]}, s.lines[s.curL:]...)...)
		}
		s.curC = len([]rune(s.lines[s.curL]))
		s.lines[s.curL] += tail
		s.acOpen = false
	default:
		return false
	}
	m.renderGenerateScratch()
	return true
}

// generateScratchCmd renders the spec and writes it into a fresh scratch off
// the update loop — MaxRows worth of faker calls has no business blocking the
// UI. The spec is remembered as the next dialog's starting point only once it
// produced a file, so a failed run never overwrites a working one.
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
		testdata.SaveLast(spec)
		return scratchGenDoneMsg{path: path, rows: spec.Rows}
	}
}

// finishGenerateScratch answers a completed generation: a failure keeps the
// dialog open with the reason, a success closes it, refreshes the explorer's
// Scratches section and opens the file through the standard funnel.
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

// tdWindow slides a list window of vis rows over n entries so that pick stays
// visible, and returns the window's start (also used by the scratch manager).
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

// tdPad left-aligns a label in n columns, rune-counted so a non-ASCII column
// name does not break the alignment.
func tdPad(s string, n int) string {
	if r := len([]rune(s)); r < n {
		return s + strings.Repeat(" ", n-r)
	}
	return s + " "
}

// tdTrunc clips a line to n columns with an ellipsis, so a long description
// or preview line never overflows the dialog on a narrow terminal.
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
