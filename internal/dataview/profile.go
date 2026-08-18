package dataview

// profile.go is the pane's half of the column profile (#1940): `P` on the
// grid's focused column asks the backend for the cheap aggregates — rows,
// nulls, empties, distinct, min/max, the top values, plus the mean of a
// numeric column and the length range of a text one — and shows them in a
// small popup over the grid.
//
// Three rules shape it, and they are the same three the open and the row
// count already follow (#1795):
//
//   - **Nothing runs on the UI thread.** The profile is a tea.Cmd like the
//     count; the popup appears immediately with "profiling …" and the grid
//     keeps rendering while a ten-million-row table is aggregated.
//   - **It is cancelable.** esc closes the popup *and* cancels the query
//     through its context — SQLite's statement is interrupted, the duckdb CLI
//     dies with its process, the Parquet scan stops between row batches.
//   - **A late result is dropped.** Each profile carries a sequence number;
//     one that lands after its popup closed (or after another column was
//     profiled) is ignored rather than replacing what the user is looking at.
//
// The popup owns the keyboard while it is open, exactly like the filter line:
// esc/q close, y copies the profile as text, j/k scroll it when it is taller
// than the pane.

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/datasrc"
	"ike/internal/overlay"
	"ike/internal/theme"
)

// CopyMsg asks the root model to put Text on the system clipboard; What names
// it for the confirmation toast.
type CopyMsg struct {
	Text string
	What string
}

// ProfileMsg asks a focused data pane to profile its focused column — the
// palette's `data.columnProfile`, the same action the pane's `P` runs.
type ProfileMsg struct{}

// profileState is the open profile popup: what it is about, whether its query
// is still running, and the result (or the engine's error) once it lands.
type profileState struct {
	table  string
	column string
	seq    int
	// cancel stops the running query; nil once the result has landed.
	cancel context.CancelFunc
	prof   datasrc.Profile
	err    error
	top    int // first shown line, for a profile taller than the pane
}

// profileResult is one finished profile on its way back to the pane.
type profileResult struct {
	seq  int
	prof datasrc.Profile
	err  error
}

// ProfileOpen reports whether the profile popup is showing (tests).
func (m *Model) ProfileOpen() bool { return m.prof != nil }

// ProfileRunning reports whether the popup's query is still in flight (tests).
func (m *Model) ProfileRunning() bool { return m.prof != nil && m.prof.cancel != nil }

// ProfileColumn returns the profiled column, "" with no popup open (tests).
func (m *Model) ProfileColumn() string {
	if m.prof == nil {
		return ""
	}
	return m.prof.column
}

// focusedColumn is the grid column the profile acts on: the leftmost visible
// one, which h/l move — the grid scrolls columns rather than carrying a
// column cursor of its own.
func (m *Model) focusedColumn() (string, bool) {
	if m.sel < 0 || m.colOff < 0 || m.colOff >= len(m.page.Columns) {
		return "", false
	}
	return m.page.Columns[m.colOff], true
}

// startProfile opens the popup and returns the command computing the profile
// of the focused column. A profile already running is cancelled first: only
// the column the user last asked for is worth the scan.
func (m *Model) startProfile() tea.Cmd {
	column, ok := m.focusedColumn()
	if !ok || m.src == nil {
		return nil
	}
	m.cancelProfile()
	m.profSeq++
	ctx, cancel := context.WithCancel(context.Background())
	table, filter, key, src, seq := m.tables[m.sel].Name, m.filter, m.key, m.src, m.profSeq
	m.prof = &profileState{table: table, column: column, seq: seq, cancel: cancel}
	return func() tea.Msg {
		prof, err := src.Profile(ctx, table, column, filter)
		cancel() // release the context with the query it guarded
		return ResultMsg{Key: key, profile: &profileResult{seq: seq, prof: prof, err: err}}
	}
}

// cancelProfile stops a running query without touching the popup state.
func (m *Model) cancelProfile() {
	if m.prof != nil && m.prof.cancel != nil {
		m.prof.cancel()
		m.prof.cancel = nil
	}
}

// closeProfile dismisses the popup, cancelling the query behind it.
func (m *Model) closeProfile() {
	m.cancelProfile()
	m.prof = nil
}

// applyProfile folds one finished profile in, dropping a result whose popup
// is gone or which another column's profile has superseded.
func (m *Model) applyProfile(r *profileResult) tea.Cmd {
	if m.prof == nil || m.prof.seq != r.seq {
		return nil
	}
	m.prof.cancel = nil
	m.prof.prof, m.prof.err = r.prof, r.err
	m.prof.top = 0
	return nil
}

// profileKey runs one key while the popup is open. It owns the keyboard: the
// grid's single-letter keys would otherwise page a grid the user cannot see.
func (m *Model) profileKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q", "P":
		m.closeProfile()
	case "y", "c":
		return m.copyProfile()
	case "j", "down":
		m.prof.top++
	case "k", "up":
		if m.prof.top > 0 {
			m.prof.top--
		}
	case "g", "home":
		m.prof.top = 0
	}
	return nil
}

// copyProfile puts the profile on the clipboard — the rendered text, exactly
// the lines the popup shows.
func (m *Model) copyProfile() tea.Cmd {
	if m.prof == nil || m.prof.cancel != nil || m.prof.err != nil {
		return nil
	}
	msg := CopyMsg{Text: m.prof.prof.Text(), What: "column profile"}
	return func() tea.Msg { return msg }
}

// profileLines is what the popup body shows: the pending notice, the engine's
// error, or the profile itself.
func (m *Model) profileLines() []string {
	p := m.prof
	switch {
	case p.cancel != nil:
		return []string{"profiling " + p.column + " in " + p.table + "…"}
	case p.err != nil:
		return []string{"cannot profile " + p.column + ":", p.err.Error()}
	default:
		return p.prof.Lines()
	}
}

// profileBox renders the popup: a bordered box of the profile lines with the
// key hints under them, sized to the pane and scrolled by top.
func (m *Model) profileBox(pal *theme.Palette) string {
	lines := m.profileLines()
	width := m.w - 6
	if width > profileMaxWidth {
		width = profileMaxWidth
	}
	if width < 12 {
		return ""
	}
	hint := "esc close"
	if m.prof.cancel != nil {
		hint = "esc cancel"
	} else if m.prof.err == nil {
		hint += " · y copy"
	}
	// The body is bounded by the pane: the border, the hint line and one row
	// of margin on each side have to fit too.
	maxBody := m.h - 5
	if maxBody < 1 {
		maxBody = 1
	}
	if m.prof.top > len(lines)-1 {
		m.prof.top = len(lines) - 1
	}
	if m.prof.top < 0 {
		m.prof.top = 0
	}
	shown := lines[m.prof.top:]
	if len(shown) > maxBody {
		shown = shown[:maxBody]
		hint += " · j/k scroll"
	}
	body := make([]string, 0, len(shown)+2)
	for _, l := range shown {
		body = append(body, clipTo(l, width))
	}
	body = append(body, "", lipgloss.NewStyle().Faint(true).Render(clipTo(hint, width)))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Accent).
		Foreground(pal.Foreground).
		Background(pal.Surface).
		Padding(0, 1).
		Render(strings.Join(body, "\n"))
	return box
}

// profileMaxWidth caps the popup: a profile is a column of short label/value
// lines, and a box spanning a wide pane only makes them harder to read.
const profileMaxWidth = 56

// compositeProfile draws the popup centered over the pane's own body.
func (m *Model) compositeProfile(base string, pal *theme.Palette) string {
	box := m.profileBox(pal)
	if box == "" {
		return base
	}
	return overlay.Center(base, box, m.w, m.h)
}
