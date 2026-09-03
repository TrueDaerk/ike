package dataview

// export.go is the grid's export (#2248): `E` — or `data.export` from the
// palette — writes **what the grid currently shows**, filter and sort
// included, to a CSV or JSON file.
//
// The line is the filter line's twin, and deliberately so: one editable field
// that owns the keyboard, enter applies, esc drops it, and an error stays
// inline instead of becoming a toast the user has to remember. What it holds
// is a path, and the **extension picks the format** — `orders.csv` or
// `orders.json` — so exporting is one question, not two.
//
// Three rules the rest of the pane already follows:
//
//   - **Nothing runs on the UI thread.** The export is a tea.Cmd like the row
//     count and the profile; a million rows stream out while the grid keeps
//     rendering, and the line says "exporting…" meanwhile.
//   - **It is cancelable.** esc cancels the running export through its
//     context, and the partial file is left where it is rather than silently
//     removed — a file the user can see is a file the user can delete.
//   - **A late result is dropped.** Each export carries a sequence number, so
//     one landing after its line closed cannot overwrite what replaced it.
//
// Nothing is overwritten by accident: an existing path is reported once and
// only a second enter writes over it.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/datasrc"
	"ike/internal/gridview"
	"ike/internal/pathcomplete"
	"ike/internal/theme"
	"ike/internal/ui"
)

// ExportedMsg reports one finished export to the root model, which shows it
// as a toast — the pane's own line is already closed by then, and an export
// is worth a confirmation that outlives it.
type ExportedMsg struct {
	Path   string
	Rows   int64
	Capped bool
	Format string
}

// ExportMsg asks a focused data pane to open its export line — the palette's
// `data.export`, the same action the pane's `E` runs.
type ExportMsg struct{}

// exportState is the open export line: the path being typed, the running
// job, and the error of the last attempt.
type exportState struct {
	input string
	cur   int
	err   error
	seq   int
	// cancel stops a running export; nil while the line is only being typed.
	cancel context.CancelFunc
	// overwrite records that the user was told the file exists, so the next
	// enter is a confirmation rather than a second warning.
	overwrite bool
}

// exportResult is one finished export on its way back to the pane.
type exportResult struct {
	seq int
	res datasrc.ExportResult
	err error
}

// Exporting reports whether the export line is open (tests).
func (m *Model) Exporting() bool { return m.exp != nil }

// ExportInput returns the path in the open export line (tests).
func (m *Model) ExportInput() string {
	if m.exp == nil {
		return ""
	}
	return m.exp.input
}

// ExportErr returns the error of the last rejected or failed export (tests).
func (m *Model) ExportErr() error {
	if m.exp == nil {
		return nil
	}
	return m.exp.err
}

// ExportRunning reports whether an export is in flight (tests).
func (m *Model) ExportRunning() bool { return m.exp != nil && m.exp.cancel != nil }

// startExport opens the line, prefilled with a path next to the database so
// enter alone already writes a sensible file.
func (m *Model) startExport() {
	if m.src == nil || m.sel < 0 {
		return
	}
	m.exp = &exportState{input: m.defaultExportPath()}
	m.exp.cur = len([]rune(m.exp.input))
	m.clampScroll()
}

// defaultExportPath is `<database dir>/<table>.csv`, the table's name
// stripped of an extension it may carry (a Parquet table is named after its
// file) and of separators, so the prefill is always one file in one directory.
func (m *Model) defaultExportPath() string {
	name := m.tables[m.sel].Name
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.NewReplacer("/", "_", `\`, "_", ":", "_").Replace(strings.TrimSpace(name))
	if name == "" {
		name = "export"
	}
	return filepath.Join(filepath.Dir(m.path), name+".csv")
}

// closeExport dismisses the line, cancelling a running export with it.
func (m *Model) closeExport() {
	if m.exp != nil && m.exp.cancel != nil {
		m.exp.cancel()
	}
	m.exp = nil
}

// exportKey feeds one key to the open line. Like the filter line it owns the
// keyboard: a path is text, so the grid's single-letter keys are plain
// characters here.
func (m *Model) exportKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.closeExport()
	case "enter":
		return m.runExport()
	default:
		if m.exp.cancel != nil {
			return nil // the path is settled while its export runs
		}
		if out, ncur, handled, changed := ui.EditKey(msg, m.exp.input, m.exp.cur); handled {
			m.exp.input, m.exp.cur = out, ncur
			if changed {
				// The warnings described the path that is now gone.
				m.exp.err, m.exp.overwrite = nil, false
			}
		}
	}
	return nil
}

// runExport validates the path and starts the background write. Everything
// that can be decided without touching the result set is decided here, so a
// typo never costs a scan: an empty path, an extension neither format claims,
// a missing directory, and the first sighting of an existing file.
func (m *Model) runExport() tea.Cmd {
	if m.src == nil || m.sel < 0 || m.exp == nil || m.exp.cancel != nil {
		return nil
	}
	path := strings.TrimSpace(m.exp.input)
	if path == "" {
		m.exp.err = fmt.Errorf("type a path ending in .csv or .json")
		return nil
	}
	path = pathcomplete.Expand(path)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if _, err := datasrc.FormatForPath(path); err != nil {
		m.exp.err = err
		return nil
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		m.exp.err = fmt.Errorf("no such directory: %s", filepath.Dir(path))
		return nil
	}
	if _, err := os.Stat(path); err == nil && !m.exp.overwrite {
		m.exp.err = fmt.Errorf("%s exists — enter again to overwrite", filepath.Base(path))
		m.exp.overwrite = true
		return nil
	}
	m.expSeq++
	ctx, cancel := context.WithCancel(context.Background())
	m.exp.cancel, m.exp.err, m.exp.seq = cancel, nil, m.expSeq
	table, clause, sort := m.tables[m.sel].Name, m.filter, m.sort
	src, key, seq := m.src, m.key, m.expSeq
	return func() tea.Msg {
		res, err := datasrc.ExportFile(ctx, src, table, clause, sort, path, datasrc.ExportLimit)
		cancel() // release the context with the job it guarded
		return ResultMsg{Key: key, export: &exportResult{seq: seq, res: res, err: err}}
	}
}

// applyExport folds one finished export in. A failure keeps the line open
// with its message — the path is probably one edit away from working — while
// a success closes it and hands the confirmation to the root model.
func (m *Model) applyExport(r *exportResult) tea.Cmd {
	if m.exp == nil || m.exp.seq != r.seq {
		return nil // the line is gone, or another export replaced this one
	}
	m.exp.cancel = nil
	if r.err != nil {
		m.exp.err = r.err
		return nil
	}
	m.exp = nil
	m.clampScroll()
	msg := ExportedMsg{Path: r.res.Path, Rows: r.res.Rows, Capped: r.res.Capped, Format: r.res.Format.String()}
	return func() tea.Msg { return msg }
}

// exportLine renders the open field: the dimmed label, then the path with a
// block cursor in it — the same shape the filter line draws.
func (m *Model) exportLine(pal *theme.Palette) string {
	label := "export to "
	if m.exp.cancel != nil {
		label = "exporting to "
	}
	shown := lipgloss.NewStyle().Faint(true).Render(label)
	avail := m.w - lipgloss.Width(label)
	if avail < 4 {
		avail = 4
	}
	if m.exp.cancel != nil {
		return shown + lipgloss.NewStyle().Foreground(pal.Foreground).Render(gridview.ClipTo(m.exp.input+" …", avail))
	}
	return shown + renderInput(pal, m.exp.input, m.exp.cur, avail)
}

// exportFooter is the status line while the export line is open: the error of
// the last attempt, or the keys and what the export will contain.
func (m *Model) exportFooter(pal *theme.Palette) string {
	if m.exp.err != nil {
		return lipgloss.NewStyle().Foreground(pal.Error).Render(gridview.ClipTo(" "+m.exp.err.Error(), m.w))
	}
	if m.exp.cancel != nil {
		return lipgloss.NewStyle().Faint(true).Render(gridview.ClipTo(" exporting… · esc cancels", m.w))
	}
	what := "the whole table"
	if m.filter != "" {
		what = "the filtered rows"
	}
	if m.sort.Active() {
		what += ", sorted by " + m.sort.String()
	}
	hint := fmt.Sprintf(" enter write · esc cancel · .csv or .json · %s, up to %d rows", what, datasrc.ExportLimit)
	return lipgloss.NewStyle().Faint(true).Render(gridview.ClipTo(hint, m.w))
}

// renderInput draws a one-line text field with a block cursor, scrolled so
// the cursor stays visible. The filter line highlights its text as SQL; a
// path has no grammar, so this is the plain version of the same drawing.
func renderInput(pal *theme.Palette, text string, cur, avail int) string {
	r := []rune(text)
	if cur < 0 {
		cur = 0
	}
	if cur > len(r) {
		cur = len(r)
	}
	start := 0
	if cur >= avail {
		start = cur - avail + 1
	}
	base := lipgloss.NewStyle().Foreground(pal.Foreground)
	var b strings.Builder
	drawn := 0
	for i := start; i < len(r) && drawn < avail; i++ {
		st := base
		if i == cur {
			st = st.Reverse(true)
		}
		b.WriteString(st.Render(string(r[i])))
		drawn++
	}
	if cur >= len(r) && drawn < avail {
		b.WriteString(lipgloss.NewStyle().Reverse(true).Render(" "))
	}
	return b.String()
}
