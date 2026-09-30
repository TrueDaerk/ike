package app

import (
	"fmt"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"ike/internal/crashlog"
	"ike/internal/host"
	"ike/internal/safego"
	"ike/internal/telemetry"
)

// crash.go is the root model's side of crash reporting (#2836). bubbletea's
// own panic catcher restores the terminal, prints the stack to a stderr the
// alternate screen has just swallowed and ends the program — IKE simply
// vanished, twice, with nothing left to read. The wrappers here run *inside*
// the loop, ahead of that catcher:
//
//   - Update recovers a panic raised while handling one message, writes the
//     crash report (internal/crashlog) and keeps the session: the model from
//     before the message is returned, an error notification names the report,
//     and the next message is handled normally. Dirty buffers stay protected —
//     the backup service (wiki/architecture/crash-recovery.md) rides on the
//     same loop, which is exactly what did not end.
//   - View recovers a panic raised while composing a frame and draws a plain
//     fallback frame naming the report instead; the loop stays up, so a key
//     retries the render and ctrl+q still quits.
//   - Every Cmd the model hands to the program is wrapped (guardCmd): a
//     panicking Cmd goroutine reports and delivers a crashRecoveredMsg, not a
//     dead program. Batches are re-wrapped member by member.
//   - Goroutines IKE starts itself go through safego.Go; the reporter installed
//     here (installCrashHooks) turns their failure into the same notification.
//
// The report carries the key context and focused pane (publishCrashFocus, a
// snapshot refreshed after every settled pass so a background goroutine's
// report has it too) and the last usage-telemetry events of the session.
// main.go covers whatever slips past all of this: a program that ends with
// bubbletea's ErrProgramPanic still gets a report and a stderr pointer.

// OpenCrashLogMsg opens the newest crash report (crash.openLastLog).
type OpenCrashLogMsg struct{}

// crashRecoveredMsg is delivered after a recovered panic: from the Cmd guard
// (the panicking Cmd's replacement message) and from Update's own recovery
// (to run one settled pass, so the notification drains right away). notify
// asks the Update wrapper to raise the toast; Update's own recovery already
// did, so its message carries false.
type crashRecoveredMsg struct {
	where  string
	msg    string
	value  any
	path   string
	notify bool
}

// crashPanicMsg is the test hook the acceptance test injects: Update panics
// with the value, inside the guard.
type crashPanicMsg struct{ value string }

// liveHost is the host the goroutine reporter notifies through — the running
// model's, refreshed by every Update pass, so a project switch's fresh model
// is followed.
var liveHost atomic.Pointer[host.Host]

// crashAction is the follow-up every crash notification offers.
func crashAction() host.NotifyAction {
	return host.NotifyAction{Command: "crash.openLastLog", Label: "Open crash log"}
}

// installCrashHooks wires the crash writer to this session: the report file
// names the telemetry session, the report's trailing context is the
// recorder's in-memory ring, and a guarded goroutine's failure reaches the UI
// as an error notification.
func installCrashHooks(usage *telemetry.Recorder, h *host.Host) {
	if usage != nil {
		crashlog.SetSession(usage.SessionID())
		crashlog.SetRecent(usage.Recent)
	}
	if h != nil {
		liveHost.Store(h)
	}
	safego.SetReporter(func(name string, value any, path string) {
		if lh := liveHost.Load(); lh != nil {
			lh.NotifyActions(host.Error, safego.Describe(name, value, path), crashAction())
		}
		logDiagnostic(fmt.Sprintf("panic recovered on goroutine %s: %s", name, crashlog.Summary(value)))
	})
}

// Update is the program's entry point: updateUnguarded under the panic guard.
func (m Model) Update(msg tea.Msg) (out tea.Model, cmd tea.Cmd) {
	if m.host != nil && liveHost.Load() != m.host {
		liveHost.Store(m.host)
	}
	defer func() {
		if r := recover(); r != nil {
			out, cmd = m.recoverUpdate(msg, r, crashlog.StackHere())
		}
	}()
	switch p := msg.(type) {
	case crashRecoveredMsg:
		if p.notify {
			m.notifyCrash(p)
		}
	case crashPanicMsg:
		panic(p.value)
	}
	out, cmd = m.updateUnguarded(msg)
	if mm, ok := out.(Model); ok {
		mm.publishCrashFocus()
		return mm, guardCmd(cmd)
	}
	return out, guardCmd(cmd)
}

// Init is the program's start-up command under the same Cmd guard.
func (m Model) Init() tea.Cmd { return guardCmd(m.initUnguarded()) }

// View composes the frame under the panic guard.
func (m Model) View() (v tea.View) {
	defer func() {
		if r := recover(); r != nil {
			v = m.recoverView(r, crashlog.StackHere())
		}
	}()
	if m.viewPanic != "" {
		panic(m.viewPanic) // the test hook
	}
	return m.viewUnguarded()
}

// recoverUpdate is the Update guard's handler: the report, the toast and the
// pre-message model. The returned command runs one more pass so the toast
// drains without waiting for the next key.
func (m Model) recoverUpdate(msg tea.Msg, r any, stack []byte) (tea.Model, tea.Cmd) {
	typ := fmt.Sprintf("%T", msg)
	path, _ := crashlog.Write(crashlog.Report{Where: "update", Value: r, Stack: stack, Msg: typ})
	rec := crashRecoveredMsg{where: "update", msg: typ, value: r, path: path}
	m.notifyCrash(rec)
	return m, func() tea.Msg { return rec }
}

// recoverView is the View guard's handler: the report, the toast (raised
// through the host, drained by the next pass) and a fallback frame.
func (m Model) recoverView(r any, stack []byte) tea.View {
	path, _ := crashlog.Write(crashlog.Report{Where: "view", Value: r, Stack: stack})
	m.notifyCrash(crashRecoveredMsg{where: "view", value: r, path: path})
	if path == "" {
		path = crashlog.Last()
	}
	text := "IKE could not render this frame.\n\n" +
		"  panic: " + crashlog.Summary(r) + "\n"
	if path != "" {
		text += "  crash log: " + path + "\n"
	}
	text += "\nThe session is still running: any key retries the render, ctrl+q quits.\n"
	v := tea.NewView(text)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// notifyCrash raises the error toast for a recovered panic. A repeat the
// crash writer throttled (no path) stays quiet: the first reports named the
// file, and a panic recurring per frame must not flood the notification
// center. The debug log gets a line either way.
func (m Model) notifyCrash(p crashRecoveredMsg) {
	where := p.where
	if p.msg != "" {
		where += " (" + p.msg + ")"
	}
	logDiagnostic(fmt.Sprintf("panic recovered in %s: %s", where, crashlog.Summary(p.value)))
	if p.path == "" || m.host == nil {
		return
	}
	m.host.NotifyActions(host.Error,
		"IKE recovered from a crash in "+where+": "+crashlog.Summary(p.value)+" — crash log: "+p.path,
		crashAction())
}

// guardCmd wraps a Cmd so a panic inside it becomes a crash report plus a
// crashRecoveredMsg instead of the program's end. A batch's members are
// wrapped as the batch resolves, since the program runs each on a goroutine
// of its own.
func guardCmd(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				path, _ := crashlog.Write(crashlog.Report{Where: "cmd", Value: r, Stack: crashlog.StackHere()})
				msg = crashRecoveredMsg{where: "cmd", value: r, path: path, notify: true}
			}
		}()
		msg = cmd()
		if b, ok := msg.(tea.BatchMsg); ok {
			out := make(tea.BatchMsg, len(b))
			for i, c := range b {
				out[i] = guardCmd(c)
			}
			return out
		}
		return msg
	}
}

// publishCrashFocus refreshes the report's UI snapshot — key context, focused
// pane kind and file — after a settled pass. A few string compares when
// nothing moved; never lets its own failure escape into the guard.
func (m Model) publishCrashFocus() {
	defer func() { _ = recover() }()
	if m.ws == nil {
		return
	}
	ws := m.activeWS()
	if ws == nil || ws.Panes == nil {
		return
	}
	kind, path := "", ""
	if inst := ws.Panes.FocusedInstance(); inst != nil {
		kind = inst.ContextID()
		if ed := inst.Editor(); ed != nil {
			path = ed.Path()
		}
	}
	crashlog.SetFocusIfChanged(string(m.keyContext()), kind, path)
}

// openLastCrashLog is crash.openLastLog: the newest report in a split, the
// way lsp.showLog opens a server log; without one the notification says
// where the reports would be.
func (m Model) openLastCrashLog() (tea.Model, tea.Cmd) {
	path := crashlog.Latest()
	if path == "" {
		m.host.Notify(host.Info, "no crash logs ("+crashlog.Dir()+")")
		return m, nil
	}
	return m.openPath(path, true)
}

// NoticeLastCrash raises the next-launch notice (#2836): a crash report
// newer than the last acknowledged one is announced once, with the open
// command as its action, and acknowledged so the next start stays quiet.
// main calls it after construction, like the restore notice.
func (m Model) NoticeLastCrash() Model {
	path := crashlog.Unacknowledged()
	if path == "" || m.host == nil {
		return m
	}
	m.host.NotifyActions(host.Warn, "IKE crashed last time — see "+path, crashAction())
	crashlog.Acknowledge(path)
	return m
}
