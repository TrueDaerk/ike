package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/httpfile"
	"ike/internal/jqplay"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/ui"
)

// playexport.go is the playground's export picker (#2788, playground.export,
// ctrl+shift+o): the next steps after ctrl+y (copy) and ctrl+o (scratch) —
// save the result to a file of one's choosing, copy it as CSV or TSV for a
// spreadsheet, or make it the body of an HTTP request. Every target is always
// listed; one the current result cannot serve is shown unavailable with the
// reason, and picking it repeats the reason instead of doing nothing — so the
// list reads the same every time and says what would unlock a row.

// playExportPrefix selects the export picker inside the palette. The root
// model only ever opens it locked, so the rune merely has to be unique among
// the registered modes — and not one anybody types.
const playExportPrefix = '¤'

// ShowPlayExportMsg opens the export picker over the open playground's result.
type ShowPlayExportMsg struct{}

// PlayExportMsg runs one export target. Reason is set on a row the result
// cannot serve: the pick explains instead of acting.
type PlayExportMsg struct {
	Target string
	Reason string
}

// The export targets, the picker's rows in order.
const (
	playExportSave = "save"
	playExportCSV  = "csv"
	playExportTSV  = "tsv"
	playExportHTTP = "http"
)

// playExportRow is one target with its availability for the current result.
type playExportRow struct {
	title, detail, target, reason string
}

// playExportMode is the palette Mode listing the targets. rows are computed
// by the root model when it opens the picker: availability depends on the
// result and the open buffers, which the mode cannot see.
type playExportMode struct{ rows []playExportRow }

// Prefix implements palette.Mode.
func (*playExportMode) Prefix() rune { return playExportPrefix }

// Placeholder implements palette.Mode.
func (*playExportMode) Placeholder() string { return "Export playground result…" }

// Results implements palette.Mode: the fixed target rows, fuzzy-matched on
// their titles. An unavailable row keeps its place, badged, with the reason as
// its detail.
func (j *playExportMode) Results(query string, _ palette.Context) []palette.Item {
	var items []palette.Item
	for _, r := range j.rows {
		match, ok := fuzzy.Match(query, r.title)
		if !ok {
			continue
		}
		it := palette.Item{
			Title:  r.title,
			Detail: r.detail,
			Spans:  match.Positions,
			Score:  match.Score,
			Msg:    PlayExportMsg{Target: r.target, Reason: r.reason},
		}
		if r.reason != "" {
			it.Badge, it.Detail = "unavailable", r.reason
		}
		items = append(items, it)
	}
	return items
}

// playExportRows lists the targets with their availability for the open
// playground's result.
func (m Model) playExportRows() []playExportRow {
	s := m.play
	res := s.result
	empty := ""
	if !s.haveResult || res.Text() == "" {
		empty = "the result is empty"
	}
	table := empty
	if table == "" {
		table = res.TableReason()
	}
	httpReason := empty
	if httpReason == "" {
		if n := len(res.Outputs); n > 1 {
			httpReason = "the result has " + strconv.Itoa(n) + " values — a request body takes one"
		} else if ed := m.playHTTPTarget(); ed == nil {
			httpReason = "no .http buffer is open"
		}
	}
	httpDetail := "into the request under the .http buffer's cursor, or its last one"
	if ed := m.playHTTPTarget(); ed != nil {
		httpDetail = "into " + playEditorLabel(ed)
	}
	return []playExportRow{
		{"Save as file…", "writes the result as shown, ." + res.Ext() + " by default", playExportSave, empty},
		{"Copy as CSV", "a list of objects or scalars, comma-separated", playExportCSV, table},
		{"Copy as TSV", "a list of objects or scalars, tab-separated", playExportTSV, table},
		{"Use as HTTP request body", httpDetail, playExportHTTP, httpReason},
	}
}

// openPlayExportPicker fills and opens the picker locked to the export mode.
func (m *Model) openPlayExportPicker() {
	if !m.playOpen() {
		m.host.Notify(host.Info, "export: open a playground first")
		return
	}
	m.playExport.rows = m.playExportRows()
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(m.paletteContext(), playExportPrefix)
}

// runPlayExport carries out one picked target.
func (m Model) runPlayExport(msg PlayExportMsg) (tea.Model, tea.Cmd) {
	s := m.play
	if s == nil {
		return m, nil
	}
	if msg.Reason != "" {
		s.status, s.statusWarn = "export unavailable: "+msg.Reason, true
		return m, nil
	}
	switch msg.Target {
	case playExportSave:
		m.startPlaySaveFilePrompt()
	case playExportCSV, playExportTSV:
		m.copyPlayResultTable(msg.Target)
	case playExportHTTP:
		return m, m.usePlayResultAsHTTPBody()
	}
	return m, nil
}

// copyPlayResultTable copies the result as CSV or TSV.
func (m *Model) copyPlayResultTable(target string) {
	s := m.play
	sep, name := rune(jqplay.CSV), "CSV"
	if target == playExportTSV {
		sep, name = jqplay.TSV, "TSV"
	}
	text, err := s.result.Delimited(sep)
	if err != nil {
		s.status, s.statusWarn = "export unavailable: "+err.Error(), true
		return
	}
	m.copyToClipboard(text)
	rows := strings.Count(text, "\n")
	s.status, s.statusWarn = "copied the result as "+name+" ("+strconv.Itoa(rows)+" line(s))", false
	m.host.Notify(host.Info, "copied the "+s.dialect.Name()+" result as "+name)
}

// playHTTPTarget finds the .http buffer the result goes into: the most
// recently focused editor when it is one, otherwise the first open .http
// buffer in layout order (a pane's active tab before its other tabs). Nil
// when none is open or every candidate is read-only.
func (m Model) playHTTPTarget() *editor.Model {
	keys := m.leafOrder()
	if m.recentEditor != "" {
		keys = append([]string{m.recentEditor}, keys...)
	}
	for _, key := range keys {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		eds := append([]*editor.Model{inst.Editor()}, inst.Editors()...)
		for _, ed := range eds {
			if isHTTPBuffer(ed) && !ed.ReadOnly() {
				return ed
			}
		}
	}
	return nil
}

// usePlayResultAsHTTPBody writes the result into a request of the target
// .http buffer as one undoable edit: the request under that buffer's cursor,
// or its last request when the cursor is outside every block. An existing
// body — inline or a `< file` reference — is replaced; a request without one
// gets a blank line and the body after its head.
func (m *Model) usePlayResultAsHTTPBody() tea.Cmd {
	s := m.play
	ed := m.playHTTPTarget()
	if ed == nil {
		s.status, s.statusWarn = "export unavailable: no .http buffer is open", true
		return nil
	}
	f := httpfile.Parse(ed.Text())
	line, _ := ed.CursorPos()
	req, ok := f.RequestAt(line + 1)
	if !ok && len(f.Requests) > 0 {
		req, ok = f.Requests[len(f.Requests)-1], true
	}
	if !ok {
		s.status, s.statusWarn = "export: "+playEditorLabel(ed)+" holds no request", true
		return nil
	}
	if req.GraphQL != nil || req.WebSocket != nil {
		s.status, s.statusWarn = "export: "+requestLabel(req)+" has a body of its own shape — not replaced", true
		return nil
	}
	body := s.result.Text()
	var edit editor.TextEdit
	if req.BodyStart > 0 {
		end := req.BodyEnd - 1
		edit = editor.TextEdit{StartLine: req.BodyStart - 1, EndLine: end, EndCol: len([]rune(ed.LineText(end))), Text: body}
	} else {
		at := req.HeadEnd - 1
		col := len([]rune(ed.LineText(at)))
		edit = editor.TextEdit{StartLine: at, StartCol: col, EndLine: at, EndCol: col, Text: "\n\n" + body}
	}
	ed.ApplyTextEdits([]editor.TextEdit{edit})
	s.status, s.statusWarn = "inserted the result as the body of "+requestLabel(req), false
	m.host.Notify(host.Info, "inserted the "+s.dialect.Name()+" result into "+requestLabel(req)+" in "+playEditorLabel(ed))
	return ed.ReparseEdits()
}

// playSaveFilePrompt is the shell prompt naming the file the result is saved
// to. confirm holds the path an overwrite was asked for: a second enter on the
// same path writes over it.
type playSaveFilePrompt struct {
	open    bool
	input   ui.Field
	err     string
	confirm string
	dialect jqplay.Dialect // names the result in the heading
}

// playSaveFileOpen reports whether the shell shows the save-to-file prompt.
func (m Model) playSaveFileOpen() bool { return m.playSaveFile.open && m.shell.IsOpen() }

// startPlaySaveFilePrompt opens the prompt prefilled with a path next to the
// source file — `<source>-result.<ext>`, the extension the result is shown in —
// or in the project root for a source without a file.
func (m *Model) startPlaySaveFilePrompt() {
	m.playSaveFile = playSaveFilePrompt{open: true, dialect: m.play.dialect, input: ui.NewField(m.playSaveFileDefault())}
	m.renderPlaySaveFilePrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// playSaveFileDefault is the prefilled path, relative to the project root when
// it lies under it.
func (m Model) playSaveFileDefault() string {
	s := m.play
	root := m.explorer().Root()
	dir, stem := root, s.dialect.Name()
	if s.srcEd != nil && s.srcEd.HasFile() {
		p := s.srcEd.Path()
		dir, stem = filepath.Dir(p), strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	}
	path := filepath.Join(dir, stem+"-result."+s.result.Ext())
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

// renderPlaySaveFilePrompt (re)fills the shell for the current input.
func (m *Model) renderPlaySaveFilePrompt() {
	p := m.playSaveFile
	body := "> " + p.input.View()
	if p.err != "" {
		body += "\n\n" + p.err
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: "Save " + p.dialect.Name() + " result — path relative to " + displayPath(m.explorer().Root()),
		Body:    func() string { return body + "\n\nenter save · esc cancel" },
	})
}

// updatePlaySaveFilePrompt consumes every key while the prompt is open.
func (m Model) updatePlaySaveFilePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.Code {
	case tea.KeyEscape:
		m.closePlaySaveFilePrompt()
		return m, nil
	case tea.KeyEnter:
		path, ok := m.commitPlaySaveFile()
		if !ok {
			m.renderPlaySaveFilePrompt()
			return m, nil
		}
		m.closePlaySaveFilePrompt()
		m.closePlayground()
		return m.openPath(path, false)
	}
	if handled, changed := m.playSaveFile.input.Key(msg); handled {
		if changed {
			m.playSaveFile.err, m.playSaveFile.confirm = "", ""
		}
		m.renderPlaySaveFilePrompt()
	}
	return m, nil
}

// commitPlaySaveFile writes the result to the prompt's path, reporting the
// written path — false leaves the prompt open holding the message it set (no
// path, a directory, an overwrite to confirm, a write that failed).
func (m *Model) commitPlaySaveFile() (string, bool) {
	p := &m.playSaveFile
	s := m.play
	if s == nil {
		return "", false
	}
	name := strings.TrimSpace(p.input.Text)
	if name == "" {
		p.err = "the result needs a path to be saved to"
		return "", false
	}
	path := filepath.FromSlash(name)
	if !filepath.IsAbs(path) {
		path = filepath.Join(m.explorer().Root(), path)
	}
	path = filepath.Clean(path)
	if st, err := os.Stat(path); err == nil {
		if st.IsDir() {
			p.err = displayPath(path) + " is a directory"
			return "", false
		}
		if p.confirm != path {
			// Never silently clobber a file: the second enter confirms.
			p.err, p.confirm = "file exists: "+displayPath(path)+" — enter again overwrites", path
			return "", false
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		p.err = err.Error()
		return "", false
	}
	if err := os.WriteFile(path, []byte(s.result.Text()+"\n"), 0o644); err != nil {
		p.err = err.Error()
		return "", false
	}
	m.host.Notify(host.Info, "saved the "+s.dialect.Name()+" result to "+displayPath(path))
	return path, true
}

// closePlaySaveFilePrompt drops the prompt and its shell.
func (m *Model) closePlaySaveFilePrompt() {
	m.playSaveFile = playSaveFilePrompt{}
	m.shell.Close()
}

// pastePlaySaveFilePrompt inserts a bracketed paste into the path input.
func (m *Model) pastePlaySaveFilePrompt(text string) bool {
	if !m.playSaveFile.input.Paste(text) {
		return false
	}
	m.playSaveFile.err, m.playSaveFile.confirm = "", ""
	m.renderPlaySaveFilePrompt()
	return true
}
