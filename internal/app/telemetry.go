package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/diag"
	"ike/internal/host"
	"ike/internal/keymap"
	"ike/internal/layout"
	"ike/internal/telemetry"
	"ike/internal/version"
)

// telemetryDir returns the directory the usage recorder (#2235) writes its
// per-session JSONL files into. It follows the IKE_CONFIG_DIR redirection
// seam like every other state file, and falls back to ~/.ike/telemetry — NOT
// the project's .ike directory, because usage spans projects (the recorder
// rides across project switches) and the files must never end up in a repo.
func telemetryDir() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "telemetry")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", "telemetry")
}

// telemetryEnabled reads the switch live from the config, so a settings flip
// applies to the very next event — no restart, no recorder rebuild.
func telemetryEnabled() bool {
	c := config.Get()
	return c == nil || c.Telemetry.Enabled
}

// telemetryHeartbeatInterval paces the liveness stamp (#2348): frequent
// enough that the last heartbeat brackets a freeze to within a minute, sparse
// enough that a day-long session costs well under a megabyte.
//
// Widened from 10s to 60s in #2408: at 10s the beats were 61% of all events in
// a two-day export — the diagnostic that was supposed to sit beside the usage
// data was burying it. A minute still brackets a freeze closely enough to tell
// "the loop is stuck" from "the process ended", and the `top` payload (#2402)
// now names an interval's loudest wake sources, which a coarser beat reports
// just as well.
const telemetryHeartbeatInterval = 60 * time.Second

// newUsageRecorder builds the session's usage recorder. It is inert until
// the first event, so a model discarded on project switch never opens a file.
// The heartbeat (#2348) carries the update-loop pass count, so a log that
// ends can be read three ways: heartbeats continuing with a frozen pass count
// mean the loop is stuck (or starved of messages), heartbeats continuing with
// an advancing count mean the freeze sits outside the loop (input reader,
// renderer, terminal), and heartbeats stopping dead mean the process itself
// ended — the distinction the #2348 freeze log could not make.
func newUsageRecorder() *telemetry.Recorder {
	r := telemetry.New(telemetryDir(), telemetryEnabled)
	// prev is the counter snapshot the previous heartbeat took; the diff names
	// what woke the loop *during* the interval (#2402) — the cumulative totals
	// would only ever name the session's loudest type. Safe without a lock:
	// telemetry calls the payload func from its single heartbeat goroutine.
	var prev map[string]uint64
	freeze := newFreezeWatch(diag.LoopPasses)
	r.SetHeartbeat(telemetryHeartbeatInterval, func() map[string]string {
		cur := diag.MessageCounts()
		p := map[string]string{"passes": strconv.FormatUint(diag.LoopPasses(), 10)}
		if top := topMessageDelta(prev, cur, 3); top != "" {
			p["top"] = top
		}
		prev = cur
		recordFreeze(r, freeze, p)
		return p
	})
	return r
}

// newFreezeWatch builds the session's starvation watch (#2627): its goroutine
// dumps land next to debug.log — the same state-dir discovery the stall
// watchdog uses, resolved at dump time so a project switch is followed — and
// the one-line pointers go through logDiagnostic, a plain file append that
// never depends on the loop being diagnosed. passes is the loop's pass
// counter (diag.LoopPasses in the session, a stub in tests).
func newFreezeWatch(passes func() uint64) *diag.FreezeWatch {
	return diag.NewFreezeWatch(passes,
		func() string { return filepath.Dir(debugLogFile()) }, logDiagnostic)
}

// recordFreeze runs one beat of the starvation watch and records the `freeze`
// event for a frozen interval (#2627). Called from the heartbeat goroutine
// with the beat's payload, so the dump header and the telemetry log carry the
// same numbers; the dump itself is written off this goroutine by the watch.
func recordFreeze(r *telemetry.Recorder, w *diag.FreezeWatch, snapshot map[string]string) {
	if rep := w.Beat(snapshot); rep.Frozen {
		r.Freeze(rep.Passes, rep.Since, rep.Dumped)
	}
}

// topMessageDelta formats the n loudest pass sources of an interval as
// "type:count,type:count", counting cur minus prev. Most frequent first,
// name-ordered on ties so equal intervals compare stably; empty when nothing
// moved (the truly idle interval the #2402 target is about).
func topMessageDelta(prev, cur map[string]uint64, n int) string {
	type d struct {
		name string
		n    uint64
	}
	var ds []d
	for name, c := range cur {
		if delta := c - prev[name]; delta > 0 {
			ds = append(ds, d{name, delta})
		}
	}
	sort.Slice(ds, func(i, j int) bool {
		if ds[i].n != ds[j].n {
			return ds[i].n > ds[j].n
		}
		return ds[i].name < ds[j].name
	})
	if len(ds) > n {
		ds = ds[:n]
	}
	var b strings.Builder
	for i, e := range ds {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(e.name)
		b.WriteByte(':')
		b.WriteString(strconv.FormatUint(e.n, 10))
	}
	return b.String()
}

// recordTelemetrySession emits the session marker (#2348): the Ike version,
// the OS and the structural project token — enough to attribute a frozen
// session's log to a build, a platform and a project state directory. Called
// at model build (deferred until the first meaningful event, #2318) and again
// on every project switch, so the token in effect is always the last one
// recorded.
func recordTelemetrySession(r *telemetry.Recorder) {
	r.Session(version.Short(), runtime.GOOS, telemetryProjectToken())
}

// projectClock measures how long the current project was actually worked in
// (#2408): the foreground time between the session marker that opened it and
// the project.leave event that closes it. Terminals that report focus let it
// pause while the window is in the background, so a project left open in
// another tab overnight does not read as a night of work; terminals that never
// report focus simply never pause, which is the same wall-clock answer the
// session markers already gave.
//
// It is a pointer on the Model because the model is copied by value on every
// Update pass — the clock must be the one object all copies share. A project
// switch builds a fresh model and therefore a fresh clock, which is exactly
// the reset the new project needs.
type projectClock struct {
	now    func() time.Time
	since  time.Time     // start of the running foreground span; zero while blurred
	active time.Duration // foreground time banked before the running span
}

func newProjectClock() *projectClock {
	c := &projectClock{now: time.Now}
	c.since = c.now()
	return c
}

// blur banks the running span and stops the clock (terminal window lost focus).
func (c *projectClock) blur() {
	if c == nil || c.since.IsZero() {
		return
	}
	c.active += c.now().Sub(c.since)
	c.since = time.Time{}
}

// focus restarts the clock (terminal window regained focus). Idempotent: a
// terminal that reports focus twice must not restart a running span.
func (c *projectClock) focus() {
	if c == nil || !c.since.IsZero() {
		return
	}
	c.since = c.now()
}

// elapsed is the foreground time so far, running span included.
func (c *projectClock) elapsed() time.Duration {
	if c == nil {
		return 0
	}
	d := c.active
	if !c.since.IsZero() {
		d += c.now().Sub(c.since)
	}
	return d
}

// recordProjectLeave closes the current project's time budget (#2408): project
// is the departing project's token (the caller takes it before any chdir, as
// telemetryProjectToken hashes the working directory), reason is "switch",
// "close" or "quit". Called once per departure — the switch transaction, which
// the project-close path also runs through, and the quit teardown — always
// before the next model's clock starts, so two projects' spans never overlap.
func (m Model) recordProjectLeave(project, reason string) {
	m.usage.ProjectLeave(project, reason, m.projClock.elapsed())
}

// switchLSPWait times the language-server warm-up of a project the session
// just switched into (#2403). The switch op closes when the model is ready,
// which is long before the incoming root's servers publish anything; this
// holds the switch's start stamp until the first publishDiagnostics arrives,
// so the export can tell "the switch was slow" from "the switch was instant
// and the editor stayed diagnostic-blind for eight seconds".
type switchLSPWait struct {
	start time.Time
	// lang names the language of the first server-backed document the switch
	// opened (#2629): the silent-server notice says whose server went quiet,
	// and "gopls said nothing" is a far more actionable sentence than "a
	// server said nothing".
	lang string
	// notified records that the silent-server notice already went out, so the
	// quiet fallback can say whether the user was told (#2629).
	notified bool
}

// noteSwitchLSPReady records the first LSP publish after a project switch as
// the op's "lsp" phase (#2403), carrying the ms from the switch's start. It
// fires at most once per switch — the wait disarms on the first publish — and
// no-ops when no switch is pending, which is every launch that never switched.
// A deliberately separate phase, not a second "ok": the start/ok pairing that
// measures the transaction itself must stay unambiguous.
func (m *Model) noteSwitchLSPReady() {
	if m.switchLSPWait == nil {
		return
	}
	ms := time.Since(m.switchLSPWait.start)
	m.switchLSPWait = nil
	if ms < 0 {
		ms = 0
	}
	m.usage.Op(telemetry.OpProjectSwitch, "lsp", map[string]string{
		"ms": strconv.FormatInt(ms.Milliseconds(), 10),
	})
}

// noteSwitchLSPSkipped closes an armed warm-up wait without a publish (#2492):
// the phase still lands — every `project.switch ok` is followed by an "lsp"
// phase, so the export never has to guess — but carries `skipped` naming why
// no publish measurement exists: "no_server_docs" (the model came up with no
// open document of a server language), "quiet" (armed, but no publish within
// switchLSPQuietTimeout — no server configured/running, or nothing to say),
// "superseded" (the next switch started first) or "quit" (the session ended
// first). ms still counts from the switch's start, so even a skipped phase
// prices how long the model sat publish-less.
func (m *Model) noteSwitchLSPSkipped(reason string) {
	if m.switchLSPWait == nil {
		return
	}
	ms := time.Since(m.switchLSPWait.start)
	notified := m.switchLSPWait.notified
	m.switchLSPWait = nil
	if ms < 0 {
		ms = 0
	}
	fields := map[string]string{
		"ms":      strconv.FormatInt(ms.Milliseconds(), 10),
		"skipped": reason,
	}
	// A quiet end the user was warned about reads differently from one they
	// never noticed (#2629), so the phase says which it was.
	if notified {
		fields["notified"] = "true"
	}
	m.usage.Op(telemetry.OpProjectSwitch, "lsp", fields)
}

// switchLSPQuietTimeout bounds the post-switch warm-up wait (#2492): a switch
// whose servers never publish (none configured, binary missing, or simply
// nothing to say) would otherwise leave the op without its "lsp" phase and the
// export unable to tell a lost event from a warm-up still in flight. Generous
// on purpose — a real cold start on a large project publishes only after full
// indexing (a 67 s outlier is on record), and a quiet marker must not preempt
// that measurement.
const switchLSPQuietTimeout = 2 * time.Minute

// switchLSPQuietMsg fires when the quiet timeout for one armed wait elapses.
// It carries the wait's identity, not a generation counter: a newer switch
// armed a different pointer, so a stale timer compares unequal and is ignored.
type switchLSPQuietMsg struct{ wait *switchLSPWait }

// armSwitchLSPQuiet schedules the quiet fallback for the wait just armed.
func armSwitchLSPQuiet(wait *switchLSPWait) tea.Cmd {
	return tea.Tick(switchLSPQuietTimeout, func(time.Time) tea.Msg { return switchLSPQuietMsg{wait: wait} })
}

// switchLSPNoticeMsg fires when the warm-up notice threshold for one armed
// wait elapses. Like switchLSPQuietMsg it carries the wait's identity, so a
// timer outliving its switch is recognised and dropped.
type switchLSPNoticeMsg struct{ wait *switchLSPWait }

// switchLSPNoticeDelay reads lsp.warmup_notice_ms — how long a switch waits
// for the first publish before it says the server has gone quiet (#2629).
// 0 (or an out-of-range value validation already reset) turns the notice off,
// and so does lsp.enabled = false: with the subsystem switched off no server
// is meant to answer, and reporting that on every switch would be pure noise.
func switchLSPNoticeDelay() time.Duration {
	cfg := config.Get()
	if !cfg.LSP.Enabled || cfg.LSP.WarmupNoticeMs <= 0 {
		return 0
	}
	return time.Duration(cfg.LSP.WarmupNoticeMs) * time.Millisecond
}

// armSwitchLSPNotice schedules the silent-server notice for the wait just
// armed, or nothing when the notice is switched off.
func armSwitchLSPNotice(wait *switchLSPWait) tea.Cmd {
	d := switchLSPNoticeDelay()
	if d <= 0 {
		return nil
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return switchLSPNoticeMsg{wait: wait} })
}

// noteSwitchLSPSilent raises the silent-server notice (#2629): the switch
// armed a warm-up wait — documents of a server language are open — and the
// threshold passed without a single diagnostics publish. Telemetry had this
// case covered for a while (the quiet fallback, #2492) but the *user* saw
// nothing at all for two minutes: no diagnostics, no breadcrumbs, no hint that
// the server was missing or dead. The notice names the language and carries
// the two follow-ups that actually help — restart the servers, or open the
// doctor that explains why none is running. It fires at most once per wait;
// the wait itself stays armed, so a late publish is still measured.
func (m *Model) noteSwitchLSPSilent() {
	if m.switchLSPWait == nil || m.switchLSPWait.notified {
		return
	}
	m.switchLSPWait.notified = true
	name := m.switchLSPWait.lang
	if name == "" {
		name = "this project"
	}
	m.host.NotifyActions(host.Warn,
		fmt.Sprintf("Language server for %s has not responded since the switch", name),
		host.NotifyAction{Command: "lsp.restart", Label: "Restart Language Servers"},
		host.NotifyAction{Command: "lsp.doctor", Label: "Open LSP Doctor"},
	)
}

// telemetryProjectToken names the current project structurally: a short hash
// of the working directory (the project root — main.go and performSwitch
// chdir there). The privacy line (#2235) forbids the clear-text path; the
// hash still lets an analyst equate "this session ran in the same project as
// that one" and rehash a candidate root to match a log to a known project —
// which is exactly what the Time report does (#2426), hashing every entry of
// the recent-projects history to put names back on the tokens.
func telemetryProjectToken() string {
	wd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return telemetry.ProjectToken(wd)
}

// recordableUnbound reports whether an unresolved key press may be recorded
// as an "unbound" event. The privacy line (#2235): plain typed characters —
// including shifted ones — must never reach the log, so only chords carrying
// a command modifier (ctrl/alt/cmd) or a function key qualify. Those are the
// presses that look like an expected-but-missing keybind rather than typing —
// the same class keymap.Key.NonTyping names for insert-mode dispatch (#2622).
func recordableUnbound(k keymap.Key) bool { return k.NonTyping() }

// telemetryZone names a layout zone for the usage log.
func telemetryZone(z layout.Zone) string {
	switch z {
	case layout.ZoneLeft:
		return "left"
	case layout.ZoneRight:
		return "right"
	case layout.ZoneTop:
		return "top"
	case layout.ZoneBottom:
		return "bottom"
	case layout.ZoneCenter:
		return "center"
	}
	return "unknown"
}
