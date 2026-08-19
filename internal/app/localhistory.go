package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/diff"
	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/localhistory"
	"ike/internal/project"
	"ike/internal/textenc"
)

// localhistory.go wires Local History (#35, MVP #1023) into the app: every
// successful editor save records the saved file into the per-project snapshot
// store, and file.localHistory raises a floating two-pane panel (#1969) over
// the focused file's snapshots — the list (file name + date) on the left, an
// inline git-style diff of the selected snapshot against the current buffer
// on the right, following the selection live. enter still opens the full
// reusable diff pane (#60), r restores the snapshot into the buffer through
// the normal edit path (undoable, marks dirty; the file on disk is untouched
// until the next save).

// LocalHistoryMsg runs file.localHistory: show the focused file's snapshots.
type LocalHistoryMsg struct{}

// localHistorySnapshotMsg reports one buffer save (any flow: manual write,
// Save All, autosave — they all funnel through editor.saveAs, whose EventSave
// the emitter forwards). The handler snapshots the just-written file.
type localHistorySnapshotMsg struct{ path string }

// localHistoryDir returns the snapshot store directory, mirroring
// layoutFile(): IKE_CONFIG_DIR when set, else the project's ".ike" directory.
func localHistoryDir() string {
	base := ".ike"
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		base = d
	}
	return filepath.Join(base, "history")
}

// recordLocalHistory snapshots path's on-disk content (the bytes the save
// just wrote). Store errors and unreadable files are swallowed — snapshotting
// must never disrupt the save that triggered it.
func (m *Model) recordLocalHistory(path string) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	m.lhStore.Record(path, data)
}

// openLocalHistoryPicker shows the focused file's snapshots in the modal
// shell, newest-first: the two-pane panel (#1969) with the list on the left
// and the selected snapshot's inline diff on the right.
func (m *Model) openLocalHistoryPicker() {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "no file for local history")
		return
	}
	path := ed.Path()
	entries := m.lhStore.List(path)
	if len(entries) == 0 {
		m.host.Notify(host.Info, "no local history for "+baseName(path)+" yet — snapshots record on save")
		return
	}
	m.lhPath = path
	m.lhEntries = entries
	m.lhSel = 0
	m.lhPicker = true
	m.lhCur = ed.Text()
	m.refreshLocalHistoryDiff()
	m.setLocalHistoryContent()
	m.shell.Open()
}

// localHistoryContent implements ui.Content (not ModelContent) so the body
// learns the shell's content width budget and can split it into the snapshot
// list and the inline diff columns (#1969).
type localHistoryContent struct{ m Model }

// Title implements ui.Content.
func (c localHistoryContent) Title() string { return "LOCAL HISTORY — " + baseName(c.m.lhPath) }

// Render implements ui.Content.
func (c localHistoryContent) Render(width int) string { return c.m.localHistoryBody(width) }

// setLocalHistoryContent (re-)binds the shell body to THIS model copy (#1440).
// The root model is a value model, so content bound once at open time would
// keep rendering the open-time selection; every selection change re-binds.
func (m *Model) setLocalHistoryContent() {
	m.shell.SetContent(localHistoryContent{m: *m})
}

// localHistoryOpen reports whether the shell currently shows the snapshot
// panel — the content check guards against another overlay having taken the
// shell over without the panel's own close path running (the pins pattern).
func (m Model) localHistoryOpen() bool {
	if !m.lhPicker || !m.shell.IsOpen() {
		return false
	}
	_, ok := m.shell.Content().(localHistoryContent)
	return ok
}

// refreshLocalHistoryDiff recomputes the inline diff of the selected snapshot
// against the buffer text captured at open time. A load/decode failure lands
// in lhErr and renders in place of the diff instead of notifying — the
// selection may sweep across a pruned entry and back without side effects.
func (m *Model) refreshLocalHistoryDiff() {
	m.lhDiff, m.lhErr = diff.Result{}, ""
	if m.lhSel < 0 || m.lhSel >= len(m.lhEntries) {
		return
	}
	data, err := m.lhStore.Read(m.lhEntries[m.lhSel].Hash)
	if err != nil {
		m.lhErr = "snapshot unreadable (pruned?)"
		return
	}
	text, err := normalizeBufferText(data)
	if err != nil {
		m.lhErr = "snapshot undecodable: " + err.Error()
		return
	}
	m.lhDiff = diff.Compute(text, m.lhCur)
}

// localHistoryBody renders the two-pane panel: the snapshot list (file name +
// date) left, the selected snapshot's inline diff right, plus the key hints.
func (m Model) localHistoryBody(width int) string {
	pal := m.pal()
	sel := lipgloss.NewStyle().Foreground(pal.Foreground).Bold(true)
	dim := lipgloss.NewStyle().Foreground(pal.Hint)

	name := baseName(m.lhPath)
	now := time.Now()
	left := make([]string, 0, len(m.lhEntries))
	leftW := 0
	for i, e := range m.lhEntries {
		line := fmt.Sprintf("  %s  %s  %s", name,
			e.Time.Local().Format("2006-01-02 15:04:05"), project.RelTime(e.Time, now))
		if i == m.lhSel {
			line = "▍" + line[1:]
		}
		leftW = max(leftW, lipgloss.Width(line))
		left = append(left, line)
	}
	if capW := max(20, width/2); leftW > capW {
		leftW = capW
	}
	rightW := max(10, width-leftW-3)
	right := m.renderLocalHistoryDiff(rightW)

	var b strings.Builder
	sep := dim.Render(" │ ")
	for i := 0; i < max(len(left), len(right)); i++ {
		l := ""
		if i < len(left) {
			l = ansi.Truncate(left[i], leftW, "…")
			if i == m.lhSel {
				l = sel.Render(l)
			}
		}
		b.WriteString(l + strings.Repeat(" ", max(0, leftW-lipgloss.Width(l))))
		b.WriteString(sep)
		if i < len(right) {
			b.WriteString(right[i])
		}
		b.WriteByte('\n')
	}
	b.WriteString("\nenter open in diff pane · r restore into buffer · j/k move · esc close")
	return b.String()
}

// lhContext is how many unchanged lines frame each hunk of the inline diff,
// matching git's default.
const lhContext = 3

// renderLocalHistoryDiff renders the selected snapshot's unified diff against
// the current buffer as styled lines, git style: @@ hunk headers, a +/-
// gutter marker per line, added lines green, removed lines red, context
// plain. Long lines clip at the column budget.
func (m Model) renderLocalHistoryDiff(width int) []string {
	pal := m.pal()
	dim := lipgloss.NewStyle().Foreground(pal.Hint)
	if m.lhErr != "" {
		return []string{lipgloss.NewStyle().Foreground(pal.Error).Render(ansi.Truncate(m.lhErr, width, "…"))}
	}
	if len(m.lhDiff.Hunks) == 0 {
		return []string{dim.Render(ansi.Truncate("no changes — snapshot matches the current buffer", width, "…"))}
	}
	added := lipgloss.NewStyle().Foreground(pal.VCSAdded)
	removed := lipgloss.NewStyle().Foreground(pal.VCSDeleted)
	clip := func(marker, text string) string {
		text = strings.ReplaceAll(text, "\t", "    ")
		return ansi.Truncate(marker+text, width, "…")
	}
	var out []string
	for _, h := range mergedLocalHistoryHunks(m.lhDiff) {
		out = append(out, dim.Render(ansi.Truncate(hunkHeader(m.lhDiff.Rows[h.Start:h.End]), width, "…")))
		for _, row := range m.lhDiff.Rows[h.Start:h.End] {
			switch row.Kind {
			case diff.RowSame:
				out = append(out, clip("  ", row.Left))
			case diff.RowChanged:
				out = append(out, removed.Render(clip("- ", row.Left)), added.Render(clip("+ ", row.Right)))
			case diff.RowRemoved:
				out = append(out, removed.Render(clip("- ", row.Left)))
			case diff.RowAdded:
				out = append(out, added.Render(clip("+ ", row.Right)))
			}
		}
	}
	return out
}

// mergedLocalHistoryHunks widens each computed hunk by lhContext unchanged
// rows on both sides and merges ranges that touch, so the inline diff shows
// git-style framed hunks instead of the whole file.
func mergedLocalHistoryHunks(res diff.Result) []diff.Hunk {
	var out []diff.Hunk
	for _, h := range res.Hunks {
		start := max(0, h.Start-lhContext)
		end := min(len(res.Rows), h.End+lhContext)
		if n := len(out); n > 0 && start <= out[n-1].End {
			out[n-1].End = end
			continue
		}
		out = append(out, diff.Hunk{Start: start, End: end})
	}
	return out
}

// hunkHeader formats one widened hunk's "@@ -start,count +start,count @@"
// line from the row's per-side line numbers (0 marks the gap side).
func hunkHeader(rows []diff.Row) string {
	ls, lc, rs, rc := 0, 0, 0, 0
	for _, r := range rows {
		if r.LeftNo > 0 {
			if ls == 0 {
				ls = r.LeftNo
			}
			lc++
		}
		if r.RightNo > 0 {
			if rs == 0 {
				rs = r.RightNo
			}
			rc++
		}
	}
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@", ls, lc, rs, rc)
}

// updateLocalHistoryPicker consumes every key while the panel is open:
// navigation (the inline diff follows the selection live), diff pane,
// restore. Everything else is swallowed (the panel is modal).
func (m Model) updateLocalHistoryPicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePicker := func() {
		m.lhPicker = false
		m.lhCur, m.lhDiff, m.lhErr = "", diff.Result{}, ""
		m.shell.Close()
	}
	// Shared list semantics (#1666): steps wrap, page jumps clamp. Every move
	// recomputes the inline diff before re-binding the shell body.
	if m.pickerNav(msg.String(), &m.lhSel, len(m.lhEntries), func() {
		m.refreshLocalHistoryDiff()
		m.setLocalHistoryContent()
	}) {
		return m, nil
	}
	switch msg.String() {
	case "esc", "q":
		closePicker()
		return m, nil
	case "enter":
		path, entry := m.lhPath, m.lhEntries[m.lhSel]
		closePicker()
		text, ok := m.localHistoryText(entry)
		if !ok {
			return m, nil
		}
		m.openLocalHistoryDiffPane(path, entry.Time, text)
		return m, nil
	case "r":
		path, entry := m.lhPath, m.lhEntries[m.lhSel]
		closePicker()
		text, ok := m.localHistoryText(entry)
		if !ok {
			return m, nil
		}
		return m, m.restoreLocalHistory(path, entry.Time, text)
	}
	return m, nil
}

// localHistoryText reads a snapshot blob and normalizes it to the buffer's
// native form (UTF-8, LF line endings, no final newline — the save path
// re-adds it) for diffing and restoring.
func (m *Model) localHistoryText(entry localhistory.Entry) (string, bool) {
	data, err := m.lhStore.Read(entry.Hash)
	if err != nil {
		m.host.Notify(host.Warn, "local history snapshot unreadable (pruned?)")
		return "", false
	}
	text, err := normalizeBufferText(data)
	if err != nil {
		m.host.Notify(host.Warn, "local history snapshot undecodable: "+err.Error())
		return "", false
	}
	return text, true
}

// normalizeBufferText decodes raw file bytes (a snapshot blob, a git blob) and
// folds them into the buffer's native form: UTF-8, LF line endings, no final
// newline — the save path re-applies the file's stored EOL/encoding flavor.
func normalizeBufferText(data []byte) (string, error) {
	text, _, err := textenc.Decode(data, textenc.UTF8)
	if err != nil {
		return "", err
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSuffix(text, "\n"), nil
}

// openLocalHistoryDiffPane shows the snapshot taken at (left) against the live
// buffer (right) in the reusable diff pane.
func (m *Model) openLocalHistoryDiffPane(path string, at time.Time, snapshot string) {
	right := readFileOrEmpty(path)
	if ed := m.editorForPath(path); ed != nil {
		right = ed.Text()
	}
	name := baseName(path)
	m.openDiffTexts(path, name+" @ "+project.RelTime(at, time.Now()), name, snapshot, right, true)
}

// openDiffTexts shows two already-resolved texts in the reusable diff pane,
// following the openDiffHeadPane shape: reuse the single diff slot when one
// exists, otherwise split a titled pane beside the editor. path backs the
// right column (language for highlighting, and the write target while the
// right side is the working tree — editable).
func (m *Model) openDiffTexts(path, leftTitle, rightTitle, left, right string, editable bool) {
	if inst, hostKey, tabIdx, ok := m.diffSlot(); ok {
		inst.StopDiffEdit()
		inst.Diff().Retarget(leftTitle, rightTitle, "", path, "", "", editable)
		inst.Diff().SetContents(left, right)
		m.focusContentAt(hostKey, tabIdx)
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
		return
	}
	key := m.activeWS().Panes.AddDiffTitled(leftTitle, rightTitle, path)
	if !m.placeDiffLeaf(key) {
		return
	}
	inst := m.activeWS().Panes.Get(key)
	inst.Diff().SetEditable(editable)
	inst.Diff().SetContents(left, right)
	m.setFocus(key)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// restoreLocalHistory replaces the buffer of path with the snapshot text
// through the normal edit path — one history change, so a single undo brings
// the pre-restore content back and the buffer marks dirty. The file on disk
// is untouched until the user saves.
func (m *Model) restoreLocalHistory(path string, at time.Time, snapshot string) tea.Cmd {
	ed := m.editorForPath(path)
	if ed == nil {
		m.host.Notify(host.Warn, "no open buffer for "+baseName(path))
		return nil
	}
	if ed.Text() == snapshot {
		m.host.Notify(host.Info, "buffer already matches this snapshot")
		return nil
	}
	lines := strings.Split(ed.Text(), "\n")
	last := lines[len(lines)-1]
	ed.ApplyTextEdits([]editor.TextEdit{{
		StartLine: 0, StartCol: 0,
		EndLine: len(lines) - 1, EndCol: len([]rune(last)),
		Text: snapshot,
	}})
	m.host.Notify(host.Info, fmt.Sprintf("restored %s from %s — undo reverts, save writes it",
		baseName(path), project.RelTime(at, time.Now())))
	// The restore bypassed the editor's Update loop; drop its stale
	// highlight/conceal caches and reparse the new content (#1683).
	return ed.ReparseEdits()
}
