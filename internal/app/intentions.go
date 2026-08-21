package app

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/httpfile"
	"ike/internal/intention"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
	"ike/internal/scratch"
	"ike/internal/vcs"
)

// intentions.go is the app half of the intention-action seam (#2020):
// alt+enter (lsp.codeAction) makes the bridge deliver its offer with
// Intentions set, the root model snapshots the focused editor's caret into
// an intention.Context, queries every registered provider, merges the
// applicable items behind the LSP actions and opens the code-action picker
// anchored at the caret. The catalog itself lives in internal/intention;
// item activation dispatches registered commands, so this file adds no
// behavior of its own — except http.insertCurlAsRequest, the one intention
// without a pre-existing command.

// InsertCurlAsRequestMsg converts the curl command on the caret line into an
// .http request block (#2020, over the #1994 parser): replaced in place in
// an .http buffer, appended to a fresh scratch .http file from anywhere
// else. Dispatched by http.insertCurlAsRequest.
type InsertCurlAsRequestMsg struct{}

// intentionContext snapshots the focused editor's caret for the providers:
// position, language and line access, plus the caret facts the editor and
// the app already cache. ok is false without an editor to anchor on.
func (m Model) intentionContext() (intention.Context, bool) {
	ed := m.activeEditor()
	if ed == nil {
		return intention.Context{}, false
	}
	line, col := ed.CursorPos()
	_, _, hasSel := ed.SelectionLines()
	cx := intention.Context{
		Path:              ed.Path(),
		LangID:            ed.LangID(),
		Line:              line,
		Col:               col,
		LineText:          ed.LineText(line),
		LineCount:         ed.LineCount(),
		LineAt:            ed.LineText,
		HasSelection:      hasSel,
		Fileless:          !ed.HasFile(),
		DiagnosticAtCaret: ed.DiagnosticOnCaretLine(),
		HunkAtCaret:       ed.HunkAtCursor(),
		ConflictAtCaret:   ed.ConflictAtCursor(),
		CanToggleValue:    ed.CanToggleValueAtCaret(),
	}
	if _, ok := ed.DocPath(editor.DocPathJQ); ok {
		cx.DocPath = true
	}
	cx.ConcealFamily, cx.ConcealValue = ed.ConcealExplainAtCaret()
	// The HTTP entries follow the buffer's *type*, so a file-less buffer
	// treated as HTTP (#2033) offers them too.
	if isHTTPBuffer(ed) {
		// RequestAt speaks 1-based cursor lines (the block ranges the parser
		// records), like every other caller.
		if _, ok := httpfile.Parse(ed.Text()).RequestAt(line + 1); ok {
			cx.HTTPRequest = true
		}
	}
	if snap := m.vcs.snap; snap != nil && ed.HasFile() && snap.Status(ed.Path()) != vcs.StatusUntracked {
		cx.InRepo = true
	}
	if ed.HasFile() && lang.HasTests(ed.Path()) {
		if _, ok := ed.NearestTestAt(line); ok {
			cx.TestAtCaret = true
		}
	}
	return cx, true
}

// openIntentions merges the LSP offer with the built-in providers and opens
// the picker anchored at the caret. The "no code actions here" verdict moved
// here from the bridge: it is only honest for the merged list.
func (m *Model) openIntentions(msg ilsp.CodeActionsMsg) {
	var items []intention.Item
	if cx, ok := m.intentionContext(); ok {
		for _, p := range m.reg.IntentionProviders() {
			items = append(items, p.Items(cx)...)
		}
	}
	m.actions.SetMerged(msg, items)
	if m.actions.Len() == 0 {
		m.host.Notify(host.Info, "no code actions here")
		return
	}
	m.palette.SetSize(m.width, m.height)
	cx := palette.Context{ContextID: m.focusContext(), Root: "."}
	if x, y, w, ok := m.caretPopupAnchor(m.actions.Len()); ok {
		m.palette.OpenAnchoredWith(cx, actionsPrefix, "", x, y, w)
		return
	}
	m.palette.OpenLocked(cx, actionsPrefix)
}

// intentionPopupWidth is the anchored intention box's outer width: wide
// enough for the titles plus kind chips without shadowing the whole pane
// (the palette clamps it to the space right of the anchor).
const intentionPopupWidth = 46

// caretPopupAnchor places the intention popup one row below the caret — the
// compositeLSPPopups math for the focused editor, shifted left at the right
// screen edge and flipped above the caret when the box would cross the
// bottom. ok is false without a laid-out editor pane to anchor on.
func (m Model) caretPopupAnchor(rows int) (x, y, w int, ok bool) {
	key := m.activeEditorKey()
	if key == "" {
		return 0, 0, 0, false
	}
	inst := m.activeWS().Panes.Get(key)
	r, found := m.lay.Panes[key]
	if inst == nil || !found {
		return 0, 0, 0, false
	}
	ed := inst.Editor()
	if ed == nil {
		return 0, 0, 0, false
	}
	line, col := ed.CursorPos()
	x = r.X + paneContentX + ed.GutterWidth() + ed.DisplayOffset(line, col)
	y = r.Y + m.contentYOff(key) + ed.DisplayRow(line, col) + 1
	w = intentionPopupWidth
	if x+w > m.width {
		x = m.width - w
	}
	if x < 0 {
		x = 0
	}
	// The box is the framed query row plus the result rows (capped at the
	// palette's default window).
	h := min(rows, 12) + 4
	if y+h > m.height {
		if above := y - 1 - h; above >= 0 {
			y = above
		} else if y = m.height - h; y < 0 {
			y = 0
		}
	}
	return x, y, w, true
}

// insertCurlAsRequest lands the caret line's curl command as an .http block
// (#2020): converted through the #1994 parser, named like an import, with
// the ignored-flags warning preserved. In an .http buffer the command lines
// are replaced by the block (one undo restores them); elsewhere the block
// opens in a fresh scratch .http file, immediately runnable.
func (m Model) insertCurlAsRequest() (tea.Model, tea.Cmd) {
	ed := m.activeEditor()
	if ed == nil {
		m.host.Notify(host.Info, "curl: focus a file tab first")
		return m, nil
	}
	line, _ := ed.CursorPos()
	cmdText, endLine := curlCommandAt(ed, line)
	if cmdText == "" {
		m.host.Notify(host.Info, "curl: no curl command on this line")
		return m, nil
	}
	imp, err := httpfile.ParseCurl(cmdText)
	if err != nil {
		m.host.Notify(host.Error, "curl: "+err.Error())
		return m, nil
	}
	if isHTTPBuffer(ed) {
		name := uniqueRequestName(httpfile.Parse(ed.Text()), curlRequestName(imp.Request))
		block := strings.TrimSuffix(httpfile.FormatRequest(imp.Request, name), "\n")
		endCol := len([]rune(ed.LineText(endLine)))
		ed.ApplyTextEdits([]editor.TextEdit{{StartLine: line, EndLine: endLine, EndCol: endCol, Text: block}})
		m.notifyCurlInsert("replaced curl with ### "+name, imp)
		return m, nil
	}
	name := curlRequestName(imp.Request)
	block := httpfile.FormatRequest(imp.Request, name)
	path, err := scratch.Create("http")
	if err != nil {
		m.host.Notify(host.Warn, "scratch: "+err.Error())
		return m, nil
	}
	if err := os.WriteFile(path, []byte(block), 0o644); err != nil {
		m.host.Notify(host.Warn, "scratch: "+err.Error())
		return m, nil
	}
	// The explorer's Scratches section (#1963) shows the new file right away.
	m.explorer().RefreshScratches()
	m.notifyCurlInsert("added ### "+name+" to "+baseName(path), imp)
	return m.openPath(path, false)
}

// notifyCurlInsert reports the conversion, warning about the flags the
// parser had to drop — the same honesty the curl import shows (#1994).
func (m Model) notifyCurlInsert(what string, imp *httpfile.CurlImport) {
	notice := "curl: " + what
	level := host.Info
	if s := imp.IgnoredSummary(); s != "" {
		notice += " — ignored flags: " + s
		level = host.Warn
	}
	m.host.Notify(level, notice)
}

// curlCommandAt gathers the curl command starting on the caret line plus its
// backslash-continued follow-up lines, flattened to the single-line form the
// parser reads. It returns "" when the caret line is no curl command.
func curlCommandAt(ed *editor.Model, line int) (cmd string, endLine int) {
	if !httpfile.IsCurlCommand(ed.LineText(line)) {
		return "", line
	}
	end := line
	for end < ed.LineCount()-1 && strings.HasSuffix(strings.TrimSpace(ed.LineText(end)), "\\") {
		end++
	}
	parts := make([]string, 0, end-line+1)
	for i := line; i <= end; i++ {
		parts = append(parts, ed.LineText(i))
	}
	return flattenCurlCommand(strings.Join(parts, "\n")), end
}
