package app

// dataquery.go is the root model's half of the data viewer's sort and export
// (#2248). Both live in the pane — the ORDER BY travels to the backend, the
// export line owns its own keyboard — so this file does the two things only
// the root model can do: route the palette command to the focused viewer, and
// turn a finished export into a toast that outlives the line it came from.

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"ike/internal/dataview"
	"ike/internal/host"
	"ike/internal/pane"
)

// DataSortColumnMsg runs data.sortColumn against the focused data pane.
type DataSortColumnMsg struct{}

// DataExportMsg runs data.export against the focused data pane.
type DataExportMsg struct{}

// dataPaneUpdate forwards a palette command to the focused data viewer, which
// may live in a content tab (#1778). A focused pane that is not a data viewer
// — the commands are pane-scoped, but a stale palette entry can still fire —
// is simply not touched.
func (m *Model) dataPaneUpdate(msg tea.Msg) tea.Cmd {
	inst := m.focusedContent()
	if inst == nil || inst.Kind() != pane.KindData {
		return nil
	}
	return inst.Update(msg)
}

// exported reports a finished export. The row count is the point — an export
// that silently wrote 0 rows, or that the cap cut short, must say so — and
// the cap turns the toast into a warning, since the file is then a prefix of
// the result rather than the result.
func (m *Model) exported(msg dataview.ExportedMsg) tea.Cmd {
	what := fmt.Sprintf("exported %d row%s as %s to %s",
		msg.Rows, pluralS(msg.Rows), msg.Format, filepath.Base(msg.Path))
	if msg.Capped {
		m.host.Notify(host.Warn, fmt.Sprintf("%s — capped, more rows matched", what))
		return nil
	}
	m.host.Notify(host.Info, what)
	return nil
}

// pluralS is the count suffix for the toast.
func pluralS(n int64) string {
	if n == 1 {
		return ""
	}
	return "s"
}
