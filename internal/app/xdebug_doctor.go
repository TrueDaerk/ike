package app

import (
	"encoding/json"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/dap"
	"ike/internal/debugdoctor"
	"ike/internal/pane"
)

// xdebug_doctor.go wires the Xdebug Doctor tool window (#1991): a singleton
// pane that makes the DBGp listener observable — its running state, port and
// filter configuration plus a live accept/reject trace of every incoming
// connection attempt. The app owns the log (doctorLog) so the trace survives
// the panel being closed; the bridge's ike.listenState / ike.debugConn events
// feed it through handleDebugEvent, and the panel renders it live. Purely
// observational: accept/reject decisions stay in the bridge.

// DebugDoctorMsg runs debug.doctor.
type DebugDoctorMsg struct{}

// toggleDoctorPanel is the debug.doctor state machine, mirroring
// toggleBreakpointsPanel: no panel → open at the adaptive placement;
// unfocused → focus it; focused → return focus to the remembered pane.
func (m *Model) toggleDoctorPanel() {
	m.togglePanel(pane.DoctorKey, func() tea.Cmd { m.openDoctorPanel(); return nil })
}

// doctorPanel returns the singleton panel model, or nil when it is not open.
func (m Model) doctorPanel() *debugdoctor.Model {
	if !m.activeWS().Panes.Has(pane.DoctorKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.DoctorKey).Doctor()
}

// openDoctorPanel splits the active editor (fallback: focused leaf) at the
// adaptive placement (auxZone, #1588) with the singleton panel, sharing the
// app-owned log.
func (m *Model) openDoctorPanel() {
	m.openToolPane(m.activeWS().Panes.AddDoctor, m.auxZone, func(key string) {
		m.wireDoctorPanel(m.activeWS().Panes.Get(key).Doctor())
	})
}

// wireDoctorPanel shares the app-owned log with a panel instance; layout
// restore uses it too.
func (m *Model) wireDoctorPanel(p *debugdoctor.Model) {
	p.SetLog(m.doctorLog)
}

// applyDoctorListenState records one ike.listenState event (#1991): the
// bridge announced the listener came up (with the bound port and filter
// configuration) or went down.
func (m *Model) applyDoctorListenState(ev dap.Event) {
	var body struct {
		State    string `json:"state"`
		Port     int    `json:"port"`
		Hostname string `json:"hostname"`
		Mappings int    `json:"mappings"`
	}
	if json.Unmarshal(ev.Body, &body) != nil {
		return
	}
	m.doctorLog.SetListener(debugdoctor.Listener{
		Running:  body.State == "listening",
		Port:     body.Port,
		Hostname: body.Hostname,
		Mappings: body.Mappings,
	})
}

// applyDoctorConn records one ike.debugConn event (#1991): a traced
// connection attempt with its accept/reject outcome and identity.
func (m *Model) applyDoctorConn(ev dap.Event) {
	var body struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
		Detail  string `json:"detail"`
		Remote  string `json:"remote"`
		IDEKey  string `json:"ideKey"`
		FileURI string `json:"fileURI"`
		Host    string `json:"host"`
		Local   string `json:"local"`
		Mapped  bool   `json:"mapped"`
	}
	if json.Unmarshal(ev.Body, &body) != nil {
		return
	}
	m.doctorLog.Add(debugdoctor.Entry{
		Time:     time.Now(),
		Accepted: body.Outcome == "accepted",
		Reason:   body.Reason,
		Detail:   body.Detail,
		Remote:   body.Remote,
		IDEKey:   body.IDEKey,
		FileURI:  body.FileURI,
		Host:     body.Host,
		Local:    body.Local,
		Mapped:   body.Mapped,
	})
}

// doctorSessionEnded marks the listener stopped when the listen-mode debug
// session goes away (#1991) — the belt to the bridge's best-effort stopped
// event (a client-initiated disconnect may close the pipe before reading it).
func (m *Model) doctorSessionEnded(cfgName string) {
	if cfgName != listenCfgName {
		return
	}
	ls := m.doctorLog.Listener()
	ls.Running = false
	m.doctorLog.SetListener(ls)
}
