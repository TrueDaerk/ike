package app

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/scratch"
	"ike/internal/telemetry"
	"ike/internal/timepanel"
)

// time_panel.go wires the Time tool window (#2426): a singleton bottom-split
// pane reporting per-project active time from the local usage log, plus the
// opt-in status-line segment showing today's total in the current project.
//
// The read is a background tea.Cmd over the whole telemetry directory
// (telemetry.Reader, which caches per file mtime), so a directory of
// multi-megabyte session files never blocks the render loop. Project tokens
// are hashes of the project root (#2235 forbids the clear-text path in the
// log), so the join runs the other way: every path in the recent-projects
// history is hashed and matched against the tokens the log carries.
//
// Everything here reads. Nothing writes to the log, and nothing uploads.

// TimeToggleMsg runs time.toggle.
type TimeToggleMsg struct{}

// TimeRefreshMsg runs time.refresh: re-read the log past the mtime cache's
// unchanged files (they are re-stat'ed either way, so this simply reloads).
type TimeRefreshMsg struct{}

// timeReportMsg carries a finished background read back to the root model.
// Token is the current project's token, taken in the background command so
// the status-line segment never has to hash the working directory per frame.
type timeReportMsg struct {
	Report *telemetry.Report
	Token  string
}

// timeTickMsg re-reads the log for the status-line segment. gen drops the
// ticks of a superseded arming, mirroring the autosave/backup debouncers.
type timeTickMsg struct{ gen int64 }

// timeSegmentInterval paces the status-line segment's refresh. A minute is as
// often as the number can meaningfully change — the recorder's own heartbeat
// runs at the same cadence — and it keeps a directory scan off the hot path.
const timeSegmentInterval = time.Minute

// toggleTimePanel is the time.toggle state machine, mirroring
// toggleDepsPanel: no panel → open at the bottom; unfocused → focus it;
// focused → return focus to the remembered pane.
func (m *Model) toggleTimePanel() tea.Cmd {
	if !m.activeWS().Panes.Has(pane.TimeKey) {
		m.timeReturnFocus = m.activeWS().Panes.Focused()
		return m.openTimePanel()
	}
	if m.activeWS().Panes.Focused() != pane.TimeKey {
		m.timeReturnFocus = m.activeWS().Panes.Focused()
		m.setFocus(pane.TimeKey)
		return nil
	}
	target := m.timeReturnFocus
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = pane.ExplorerKey
	}
	m.setFocus(target)
	return nil
}

// timePanel returns the singleton panel model, or nil when it is not open.
func (m Model) timePanel() *timepanel.Model {
	if !m.activeWS().Panes.Has(pane.TimeKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.TimeKey).Time()
}

// openTimePanel splits the active editor (fallback: focused leaf) at the
// bottom with the singleton panel, seeded from the last report, and starts a
// fresh read.
func (m *Model) openTimePanel() tea.Cmd {
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return nil
	}
	key := m.activeWS().Panes.AddTime()
	if !m.insertToolPane(key, target, layout.ZoneBottom) {
		m.activeWS().Panes.Close(key)
		return nil
	}
	p := m.activeWS().Panes.Get(key).Time()
	if m.timeReport != nil {
		p.Set(m.timeReport)
	} else {
		p.SetLoading(true)
	}
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	return m.timeReadCmd()
}

// newTimeReader builds the reader over the recorder's own directory, so the
// report always covers exactly the files this session writes. Built with the
// model (like the deps scanner) rather than lazily: the model is copied by
// value on every Update pass, so a lazy field would keep re-creating the
// mtime cache on a discarded copy.
func newTimeReader() *telemetry.Reader { return telemetry.NewReader(telemetryDir()) }

// timeReadCmd reads and aggregates the usage log in the background. The
// project-name join happens here too: it needs the config, which the command
// can read off the loaded snapshot without touching the model.
func (m Model) timeReadCmd() tea.Cmd {
	reader := m.timeReader
	if reader == nil {
		reader = newTimeReader()
	}
	token := telemetryProjectToken()
	paths := projectNamesByPath()
	if p := m.timePanel(); p != nil {
		p.SetLoading(true)
	}
	return func() tea.Msg {
		rep := reader.Read()
		rep.Resolve(paths)
		return timeReportMsg{Report: rep, Token: token}
	}
}

// projectNamesByPath maps every known project root to the name to show for
// it: the recent-projects history (#2394) plus the project open right now,
// which is not in the history until it is recorded.
func projectNamesByPath() map[string]string {
	out := map[string]string{}
	if cfg := config.Get(); cfg != nil {
		for _, e := range project.History(cfg) {
			out[e.Path] = e.Name
		}
	}
	if wd, err := os.Getwd(); err == nil && wd != "" {
		if _, ok := out[wd]; !ok {
			out[wd] = filepath.Base(wd)
		}
	}
	return out
}

// handleTimeReport lands a finished background read on the model, the panel
// and the status-line segment.
func (m Model) handleTimeReport(msg timeReportMsg) (tea.Model, tea.Cmd) {
	m.timeReport = msg.Report
	m.timeToken = msg.Token
	if p := m.timePanel(); p != nil {
		p.Set(msg.Report)
	}
	return m, nil
}

// timeSegmentCmd arms the status-line segment's periodic re-read (#2426):
// nothing at all while the setting is off, so an opted-out session never
// scans the telemetry directory. Called from Init, whose receiver copy is
// discarded — hence no generation bump here; the chain re-arms itself with
// the model's live generation from handleTimeTick.
func (m Model) timeSegmentCmd() tea.Cmd {
	if !projectTimeSegmentOn() {
		return nil
	}
	gen := m.timeTickGen
	return tea.Batch(
		m.timeReadCmd(),
		tea.Tick(timeSegmentInterval, func(time.Time) tea.Msg { return timeTickMsg{gen: gen} }),
	)
}

// handleTimeTick re-reads the log for the segment and re-arms the ticker. A
// tick from a superseded arming, or one that arrives after the setting was
// switched off, ends the chain.
func (m Model) handleTimeTick(msg timeTickMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.timeTickGen || !projectTimeSegmentOn() {
		return m, nil
	}
	gen := m.timeTickGen
	return m, tea.Batch(
		m.timeReadCmd(),
		tea.Tick(timeSegmentInterval, func(time.Time) tea.Msg { return timeTickMsg{gen: gen} }),
	)
}

// projectTimeSegmentOn reads the opt-in switch live from the config, so a
// settings flip applies without a restart.
func projectTimeSegmentOn() bool {
	c := config.Get()
	return c != nil && c.StatusLine.ProjectTime == "on"
}

// projectTimeSegment is the status-line segment: today's active time in the
// current project (#2426). Hidden while the setting is off, before the first
// read has landed, and on a day with nothing recorded yet — a segment reading
// "0m" all morning is noise, not information.
func (m Model) projectTimeSegment() string {
	if !projectTimeSegmentOn() || m.timeReport == nil || m.timeToken == "" {
		return ""
	}
	ps := m.timeReport.Projects[m.timeToken]
	if ps == nil {
		return ""
	}
	day := ps.Days[time.Now().Format(telemetry.DayFormat)]
	if day == nil || day.Active <= 0 {
		return ""
	}
	return "⏱ " + telemetry.FormatDuration(day.Active)
}

// handleTimeExport writes the pane's CSV view to a scratch file ('e') and
// opens it, so the export lands somewhere the session already manages rather
// than in an arbitrary directory.
func (m Model) handleTimeExport(msg timepanel.ExportMsg) (tea.Model, tea.Cmd) {
	path, err := scratch.CreateWithContent("csv", []byte(msg.CSV))
	if err != nil {
		m.host.Notify(host.Error, "time: export failed: "+err.Error())
		return m, nil
	}
	m.host.Notify(host.Info, "time: "+strings.ToLower(msg.Label)+" exported to "+displayPath(path))
	return m.openPathAt(path, 0, 0)
}
