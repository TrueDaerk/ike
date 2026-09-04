// Package telemetry records local-only usage events (#2235): which commands
// run and how they were invoked, which key chords resolve (or don't), and how
// panes, tabs and projects are arranged. Events append as JSONL to one file
// per session under the user's IKE directory, so later analysis is a jq
// one-liner away — nothing ever leaves the machine.
//
// The hard privacy line: events carry structure only. No typed text, no file
// contents, no clear-text paths — the hook points in internal/app are
// responsible for never passing content, and the package-level tests pin the
// schema to structural fields.
//
// Writes are asynchronous: Record hands the event to a buffered channel and a
// single writer goroutine does the disk I/O, so the render loop never blocks
// on telemetry. A full channel drops the event silently — a usage log must
// never disrupt the session. A nil *Recorder is inert, like the frecency
// store's zero value.
package telemetry

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SchemaVersion is the event-schema version stamped into every line as "v".
// Readers filter on it; additions bump it only when a field changes meaning.
//
// v2 (#2304): internally triggered command dispatches (source "internal" —
// polling/background funnels like the structure-panel's document-symbols
// refresh) now land as their own type ("internal") instead of "command", so
// "command" is exclusively user-triggered dispatches (keybind/palette/menu/
// mouse) and a v1 reader that still expects "internal" under "command" must
// check "v" first.
//
// v3 (#2348): three diagnostic event types join — "session" (version, OS,
// hashed project token), "heartbeat" (periodic liveness stamp carrying the
// update-loop pass count) and "op" (lifecycle of a long-running operation:
// start/ok/error/canceled with duration, status class, streaming flag). No
// existing field changed meaning; the bump tells a reader that a log ending
// without heartbeats predates them rather than evidencing a freeze.
//
// v4 (#2408): the heartbeat interval widens from 10s to 60s — a v4 log holds
// roughly a sixth of the beats per hour, so any rate computed across versions
// must branch on "v"; "command"/"internal" events gain "ok" and "ms" whenever
// the dispatch failed or took longer than CommandSlowThreshold (their absence
// means "fast and fine", not "unknown"); and two types join —
// "palette.dismiss" (a palette mode closed without a pick) and "project.leave"
// (foreground time spent in a project). The recent-files dismissal that v3
// recorded as the pseudo-command "palette.recentFiles.dismiss" is gone: every
// mode now reports its dismissals under "palette.dismiss".
//
// v5 (#2492): the "project.switch" op's warm-up phase becomes total — every
// "ok" is now followed by exactly one "lsp" phase. When no publishDiagnostics
// measurement exists the phase carries "skipped" naming why: "no_server_docs"
// (the switched-to model opened no server-language document), "quiet" (armed
// but nothing published within the fallback window), "superseded" (the next
// switch started first) or "quit" (the session ended first). A v4 log's
// missing "lsp" phase is ambiguous — server silence or lost event — a v5
// reader can treat absence as a bug.
// v6 (#2490): "palette.dismiss" events gain "results" — how many rows the
// palette was listing when esc was pressed. It separates "typed a name that
// does not exist" (query_len > 0, results 0) from "found it, changed my mind",
// which query_len alone cannot. The field is additive: every v5 field keeps
// its meaning, and its absence on a v5 log means "not recorded", not zero.
const SchemaVersion = 6

// defaultFlushInterval is how often the writer goroutine flushes the
// bufio.Writer on its own, independent of buffer fill or explicit Flush
// calls — so a frozen UI loop still leaves recent events on disk within a
// few seconds.
const defaultFlushInterval = 3 * time.Second

// Event types.
const (
	TypeCommand   = "command"   // a user-triggered command was dispatched (keybind/palette/menu/mouse)
	TypeInternal  = "internal"  // a command was dispatched internally (polling/background funnels), not by the user
	TypeKey       = "key"       // a chord resolved, was blocked, or found no binding
	TypeLayout    = "layout"    // a structural layout operation
	TypeSession   = "session"   // session start / project switch: version, OS, hashed project token (#2348)
	TypeHeartbeat = "heartbeat" // periodic liveness stamp with the update-loop pass count (#2348)
	TypeOp        = "op"        // lifecycle of a long-running operation (#2348)

	TypePaletteDismiss = "palette.dismiss" // a palette mode closed without a pick (#2408)
	TypeProjectLeave   = "project.leave"   // foreground time spent in the project being left (#2408)
)

// Operation ids for the op lifecycle events (#2348, #2403). Callers outside
// this package use them so the export's vocabulary stays in one place.
const (
	OpHTTPFlight     = "http.flight"     // one .http request dispatch (#2348)
	OpProjectSwitch  = "project.switch"  // the seamless project switch transaction (#2403)
	OpProjectClose   = "project.close"   // closing a project and resuming the MRU one (#2403)
	OpSessionRestore = "session.restore" // the startup layout/session restore (#2403)
)

// CommandSlowThreshold is the dispatch duration from which a command event
// carries its outcome fields (#2408). Below it a successful dispatch keeps the
// v3 shape — id and source only — so the common case costs no extra bytes; at
// or above it the event is worth a latency look.
const CommandSlowThreshold = 50 * time.Millisecond

// Command sources.
const (
	SourcePalette  = "palette"
	SourceMenu     = "menu"
	SourceKeybind  = "keybind"
	SourceMouse    = "mouse"
	SourceInternal = "internal"
)

// Event is one JSONL line. Data holds the type-specific payload; every value
// is a short structural token (command id, chord string, context id, op
// name), never content.
type Event struct {
	V    int               `json:"v"`
	TS   string            `json:"ts"`
	SID  string            `json:"sid"`
	Type string            `json:"type"`
	Data map[string]string `json:"data,omitempty"`
}

// maxPending caps the deferred low-signal events held before the session
// file exists (see startsSession). A handful is all a real session produces
// before its first meaningful event; beyond that the oldest are dropped.
const maxPending = 32

// Recorder appends events for one session. Create it with New; the session
// file opens lazily on the first *meaningful* event (see startsSession), so a
// recorder that never records — telemetry off, a model discarded on project
// switch, or a launch that only ever emitted the synthetic startup
// pane.focus — costs nothing and leaves no file (#2318).
type Recorder struct {
	dir       string
	enabled   func() bool
	now       func() time.Time
	newTicker func(d time.Duration) (<-chan time.Time, func())

	// MaxBytes caps the session file; once written bytes exceed it the
	// recorder goes dead and drops everything (bounded growth per session).
	// KeepFiles caps the telemetry directory: opening a new session file
	// prunes the oldest session files beyond KeepFiles-1. FlushInterval sets
	// how often the writer goroutine flushes on its own (see
	// defaultFlushInterval). All three are set before the first event only.
	MaxBytes      int64
	KeepFiles     int
	FlushInterval time.Duration

	mu      sync.Mutex
	started bool
	closed  bool
	sid     string
	pending []*Event // low-signal events held until the file is opened
	ch      chan envelope
	done    chan struct{}

	// Heartbeat (#2348): installed via SetHeartbeat before the first event,
	// the goroutine spawns with the writer when the session file opens — an
	// inert recorder never ticks. hbQuit/hbDone coordinate the shutdown with
	// Close, which must not close ch while a heartbeat record is in flight.
	hbInterval time.Duration
	hbSnapshot func() map[string]string
	hbQuit     chan struct{}
	hbDone     chan struct{}
}

// envelope carries either an event or a flush request through the channel.
type envelope struct {
	ev  *Event
	ack chan struct{} // non-nil: flush request — writer flushes then closes it
}

// New builds a recorder writing under dir. enabled is consulted on every
// Record call, so a live config flip applies to the very next event; nil
// means always on. An empty dir yields an inert recorder (the test seam and
// the "no home directory" fallback).
func New(dir string, enabled func() bool) *Recorder {
	if dir == "" {
		return nil
	}
	return &Recorder{
		dir:           dir,
		enabled:       enabled,
		now:           time.Now,
		newTicker:     realTicker,
		MaxBytes:      5 << 20,
		KeepFiles:     20,
		FlushInterval: defaultFlushInterval,
	}
}

// realTicker wraps a time.Ticker behind the newTicker seam.
func realTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

// SessionID returns the session id, minting it on first use.
func (r *Recorder) SessionID() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sidLocked()
}

func (r *Recorder) sidLocked() string {
	if r.sid == "" {
		var b [6]byte
		if _, err := rand.Read(b[:]); err == nil {
			r.sid = hex.EncodeToString(b[:])
		} else {
			r.sid = "000000000000"
		}
	}
	return r.sid
}

// Command records a command dispatch: the command id and how it was invoked
// (one of the Source* constants). Internally triggered dispatches (source
// SourceInternal — polling/background funnels, e.g. the structure panel's
// document-symbols refresh) land as TypeInternal instead of TypeCommand, so
// TypeCommand stays exclusively user-triggered actions (#2304) — otherwise
// high-frequency polling commands like lsp.documentSymbols dominate any
// analysis of actual command usage.
func (r *Recorder) Command(id, source string) {
	r.CommandDetail(id, source, nil)
}

// CommandDetail is Command with extra structural payload (#2399): the
// recent-files palette records its dismissals as "palette.recentFiles.dismiss"
// carrying the typed query *length*, so the next export shows whether the
// re-open streaks shrink. Detail values must stay structural — a count, a mode
// token — never typed text; the "id" and "source" keys are reserved and any
// entry using them is ignored.
func (r *Recorder) CommandDetail(id, source string, detail map[string]string) {
	typ := TypeCommand
	if source == SourceInternal {
		typ = TypeInternal
	}
	d := map[string]string{"id": id, "source": source}
	for k, v := range detail {
		if k == "id" || k == "source" {
			continue
		}
		d[k] = v
	}
	r.record(typ, d)
}

// CommandOutcome is Command with the dispatch's outcome (#2408): ok is false
// when the command could not run at all (an unknown id — the app's dispatch
// funnel has no other failure mode), d is how long the synchronous dispatch
// took. Both land in the event as "ok"/"ms", but only when the dispatch failed
// or was slow (CommandSlowThreshold); a fast success keeps the plain v3 shape,
// so the fields read as "worth a look" markers instead of per-event noise.
func (r *Recorder) CommandOutcome(id, source string, ok bool, d time.Duration) {
	if ok && d < CommandSlowThreshold {
		r.CommandDetail(id, source, nil)
		return
	}
	if d < 0 {
		d = 0
	}
	r.CommandDetail(id, source, map[string]string{
		"ok": strconv.FormatBool(ok),
		"ms": strconv.FormatInt(d.Milliseconds(), 10),
	})
}

// PaletteDismiss records a palette mode closed without a pick (#2408): mode is
// the mode's prefix rune as a string (":", "%", "@"), queryLen the number of
// runes typed — never the query itself — results how many rows the list was
// showing at that moment, and d how long the box was open. A dismissal is the
// one palette outcome that otherwise leaves no trace at all, so re-open
// streaks ("wrong entry, esc, try again") stay invisible without it; since
// #2490 the row count tells a fruitless search (results 0 on a typed query)
// from a deliberate change of mind. A negative count is clamped to zero.
func (r *Recorder) PaletteDismiss(mode string, queryLen, results int, d time.Duration) {
	if d < 0 {
		d = 0
	}
	if results < 0 {
		results = 0
	}
	r.record(TypePaletteDismiss, map[string]string{
		"mode":      mode,
		"query_len": strconv.Itoa(queryLen),
		"results":   strconv.Itoa(results),
		"ms":        strconv.FormatInt(d.Milliseconds(), 10),
	})
}

// ProjectLeave records the foreground time spent in a project as it is left
// (#2408): project is the same hashed token the session marker carries, reason
// is "switch", "close" or "quit", and d is the time the project was active
// with the terminal window focused. It does not start a session file (see
// startsSession) — a launch that only ever leaves again must stay a ghost.
func (r *Recorder) ProjectLeave(project, reason string, d time.Duration) {
	if d < 0 {
		d = 0
	}
	r.record(TypeProjectLeave, map[string]string{
		"project": project,
		"reason":  reason,
		"ms":      strconv.FormatInt(d.Milliseconds(), 10),
	})
}

// Key records a keymap resolution: the canonical chord string, the focus
// context it resolved in, the command it resolved to (empty when none) and
// the outcome ("resolved", "blocked" or "unbound"). Callers must pre-filter
// unbound events so plain typed characters never reach here.
func (r *Recorder) Key(chord, context, command, status string) {
	d := map[string]string{"chord": chord, "status": status}
	if context != "" {
		d["context"] = context
	}
	if command != "" {
		d["command"] = command
	}
	r.record(TypeKey, d)
}

// Layout records a structural layout operation ("split", "tab.switch",
// "project.switch", …) with optional structural detail (a zone or direction —
// never a path or title).
func (r *Recorder) Layout(op string, detail map[string]string) {
	d := map[string]string{"op": op}
	for k, v := range detail {
		d[k] = v
	}
	r.record(TypeLayout, d)
}

// Session records the session-start (or project-switch) marker (#2348): the
// app version, the OS and a structural project token — a short hash, never a
// clear-text path — so a later freeze analysis can tell which Ike, which
// platform and which project state directory a log belongs to. Deferred like
// pane.focus, so a launch that never records anything meaningful still leaves
// no file (#2318).
func (r *Recorder) Session(version, osName, project string) {
	r.record(TypeSession, map[string]string{"app": version, "os": osName, "project": project})
}

// Op records one phase of a long-running operation's lifecycle (#2348): id
// names the operation ("http.flight"), phase is "start", "ok", "error" or
// "canceled", detail carries structural extras (duration in ms, status class,
// streaming flag — never a URL, header or body).
func (r *Recorder) Op(id, phase string, detail map[string]string) {
	d := map[string]string{"id": id, "phase": phase}
	for k, v := range detail {
		d[k] = v
	}
	r.record(TypeOp, d)
}

// OpTimer starts a timed operation (#2403): it records the "start" phase of
// id right away and returns the closer for the end phase — "ok", "error" or
// "canceled" — which stamps the elapsed time into the detail map as "ms".
// Callers keep the closer for as long as the operation flies (an HTTP request
// stores it on its flight entry, a project switch calls it in the same pass)
// and must call it exactly once; a start without an end stays visible in the
// export as exactly that.
func (r *Recorder) OpTimer(id string) func(phase string, detail map[string]string) {
	start := time.Now()
	if r != nil && r.now != nil {
		start = r.now()
	}
	r.Op(id, "start", nil)
	return func(phase string, detail map[string]string) {
		since := time.Since(start)
		if r != nil && r.now != nil {
			since = r.now().Sub(start)
		}
		if since < 0 {
			since = 0
		}
		d := map[string]string{"ms": strconv.FormatInt(since.Milliseconds(), 10)}
		for k, v := range detail {
			d[k] = v
		}
		r.Op(id, phase, d)
	}
}

// SetHeartbeat installs the periodic liveness stamp (#2348): every interval,
// snapshot is asked for the heartbeat payload and a non-nil result lands as a
// TypeHeartbeat event. The goroutine spawns only once the session file opens
// (an inert recorder never ticks) and stops with Close. Must be called before
// the first event; later calls are ignored.
func (r *Recorder) SetHeartbeat(interval time.Duration, snapshot func() map[string]string) {
	if r == nil || snapshot == nil || interval <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.closed {
		return
	}
	r.hbInterval, r.hbSnapshot = interval, snapshot
}

// heartbeat is the liveness goroutine: one event per tick, for as long as the
// writer lives. Its record calls race Close by design, which is why Close
// waits for hbDone before closing the event channel.
func (r *Recorder) heartbeat() {
	defer close(r.hbDone)
	tick, stop := r.newTicker(r.hbInterval)
	defer stop()
	for {
		select {
		case <-r.hbQuit:
			return
		case <-tick:
			if data := r.hbSnapshot(); data != nil {
				r.record(TypeHeartbeat, data)
			}
		}
	}
}

// startsSession reports whether an event is meaningful enough to create the
// session file. Everything is, except a bare pane focus change — IKE emits one
// on startup when the session restore moves focus from the explorer to the
// restored editor, so a launch that is immediately quit would otherwise leave
// a ghost file holding that single event (#2318) — and the session marker
// (#2348), which every launch emits and which must not resurrect exactly that
// ghost file. Deferred events are not lost — they are held in memory and
// written once a meaningful event arrives.
func startsSession(typ string, data map[string]string) bool {
	if typ == TypeSession {
		return false
	}
	if typ == TypeProjectLeave {
		// Leaving is not using (#2408): a launch that opens a project and
		// quits again without doing anything would otherwise resurrect
		// exactly the ghost file the deferred pane.focus rule avoids.
		return false
	}
	if typ == TypeOp && data["id"] == OpSessionRestore {
		// The startup restore (#2403) is not usage either: it runs on every
		// launch, so letting it open the file would resurrect the very ghost
		// the deferred pane.focus and session rules avoid. Held like them, it
		// still lands in every file a real session writes.
		return false
	}
	return !(typ == TypeLayout && data["op"] == "pane.focus")
}

// record enqueues one event. It never blocks: a full buffer drops the event.
func (r *Recorder) record(typ string, data map[string]string) {
	if r == nil {
		return
	}
	if r.enabled != nil && !r.enabled() {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	if !r.started && typ == TypeHeartbeat {
		// A heartbeat neither starts a session nor is worth holding: pending
		// beats would only evict the deferred session marker (#2348).
		r.mu.Unlock()
		return
	}
	if !r.started && !startsSession(typ, data) {
		// Hold it: this event alone must not create a file.
		r.pending = append(r.pending, r.newEvent(typ, data))
		if len(r.pending) > maxPending {
			// The session marker at index 0 survives the trim (#2348): it is
			// the attribution anchor the whole file depends on.
			if r.pending[0].Type == TypeSession {
				r.pending = append(r.pending[:1], r.pending[len(r.pending)-maxPending+1:]...)
			} else {
				r.pending = r.pending[len(r.pending)-maxPending:]
			}
		}
		r.mu.Unlock()
		return
	}
	if !r.started {
		r.started = true
		r.sidLocked()
		// The directory and file are created synchronously here — a one-time
		// cost per session — so nothing touches the filesystem after the
		// last Record (tests remove their state dir on cleanup); only the
		// per-event writes are asynchronous.
		f := r.open()
		if f == nil {
			r.closed = true
			r.pending = nil
			r.mu.Unlock()
			return
		}
		r.ch = make(chan envelope, 256)
		r.done = make(chan struct{})
		go r.run(f)
		if r.hbSnapshot != nil {
			// The liveness stamp starts with the session (#2348) — a recorder
			// that never opens a file never ticks.
			r.hbQuit = make(chan struct{})
			r.hbDone = make(chan struct{})
			go r.heartbeat()
		}
		// The deferred low-signal events precede this one in time, so they
		// go in first — the log keeps its chronological order.
		for _, p := range r.pending {
			r.ch <- envelope{ev: p}
		}
		r.pending = nil
	}
	ev := r.newEvent(typ, data)
	ch := r.ch
	r.mu.Unlock()
	select {
	case ch <- envelope{ev: ev}:
	default: // full buffer: drop — never block the render loop
	}
}

// newEvent stamps one event. Called under the mutex.
func (r *Recorder) newEvent(typ string, data map[string]string) *Event {
	return &Event{
		V:    SchemaVersion,
		TS:   r.now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		SID:  r.sidLocked(),
		Type: typ,
		Data: data,
	}
}

// Flush blocks until every event enqueued before the call is on disk. Mainly
// a test seam; Close flushes too.
func (r *Recorder) Flush() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if !r.started || r.closed {
		r.mu.Unlock()
		return
	}
	ch := r.ch
	r.mu.Unlock()
	ack := make(chan struct{})
	ch <- envelope{ack: ack}
	<-ack
}

// FlushSoon asks the writer to put everything enqueued so far on disk without
// waiting for it (#2348) — the call before a long-running operation starts, so
// the events leading up to a potential hang are on the platter while the
// answer is still out. Never blocks: a full channel drops the request, the
// periodic flush covers it a few seconds later.
func (r *Recorder) FlushSoon() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if !r.started || r.closed {
		r.mu.Unlock()
		return
	}
	ch := r.ch
	r.mu.Unlock()
	select {
	case ch <- envelope{ack: make(chan struct{})}:
	default: // full buffer: the ticker flush is close enough
	}
}

// Close flushes and ends the writer. Further Record calls are dropped.
func (r *Recorder) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	started, ch, done := r.started, r.ch, r.done
	hbQuit, hbDone := r.hbQuit, r.hbDone
	r.mu.Unlock()
	// The heartbeat goroutine ends first (#2348): its in-flight record call —
	// if any — completes before the channel closes, so the send can never hit
	// a closed channel.
	if hbQuit != nil {
		close(hbQuit)
		<-hbDone
	}
	if !started {
		return
	}
	close(ch)
	<-done
}

// open creates the telemetry directory, prunes old session files and opens
// this session's file. Called once, synchronously, under the mutex; nil on
// any failure (the recorder then goes closed and drops everything).
func (r *Recorder) open() *os.File {
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return nil
	}
	r.prune()
	name := r.now().UTC().Format("20060102T150405") + "-" + r.sid + ".jsonl"
	f, err := os.OpenFile(filepath.Join(r.dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	return f
}

// run is the writer goroutine: it drains the channel until Close, doing all
// per-event disk I/O. All errors are swallowed — once anything fails (or the
// size cap is hit) it keeps draining so senders never notice, it just stops
// writing.
//
// A ticker flushes the buffer on its own cadence (FlushInterval), so events
// already enqueued reach disk within a few seconds even if the render/update
// loop that would otherwise drive Record/Flush calls is frozen — the writer
// goroutine never depends on that loop running.
func (r *Recorder) run(f *os.File) {
	defer close(r.done)
	var (
		written int64
		dead    bool
	)
	w := bufio.NewWriter(f)
	defer func() {
		w.Flush()
		f.Close()
	}()

	tick, stop := r.newTicker(r.FlushInterval)
	defer stop()

	for {
		select {
		case env, ok := <-r.ch:
			if !ok {
				return
			}
			if env.ack != nil {
				w.Flush()
				close(env.ack)
				continue
			}
			if dead {
				continue
			}
			line, err := json.Marshal(env.ev)
			if err != nil {
				continue
			}
			n, err := w.Write(append(line, '\n'))
			written += int64(n)
			if err != nil || (r.MaxBytes > 0 && written > r.MaxBytes) {
				dead = true // cap reached or disk trouble: stop writing, keep draining
				w.Flush()
			}
		case <-tick:
			if !dead {
				w.Flush()
			}
		}
	}
}

// prune deletes empty session files and then the oldest ones beyond
// KeepFiles-1, so the directory holds at most KeepFiles files once this
// session's file is created. The timestamped names sort chronologically.
//
// Zero-byte files are litter from earlier launches that were killed before
// the writer flushed anything; dropping them here keeps the directory (and
// the retention budget) free of sessions that hold no events (#2318).
func (r *Recorder) prune() {
	keep := r.KeepFiles - 1
	if keep < 0 {
		keep = 0
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if info, err := e.Info(); err == nil && info.Size() == 0 {
			os.Remove(filepath.Join(r.dir, e.Name()))
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for i := 0; i < len(names)-keep; i++ {
		os.Remove(filepath.Join(r.dir, names[i]))
	}
}
