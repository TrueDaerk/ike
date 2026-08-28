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
const SchemaVersion = 2

// defaultFlushInterval is how often the writer goroutine flushes the
// bufio.Writer on its own, independent of buffer fill or explicit Flush
// calls — so a frozen UI loop still leaves recent events on disk within a
// few seconds.
const defaultFlushInterval = 3 * time.Second

// Event types.
const (
	TypeCommand  = "command"  // a user-triggered command was dispatched (keybind/palette/menu/mouse)
	TypeInternal = "internal" // a command was dispatched internally (polling/background funnels), not by the user
	TypeKey      = "key"      // a chord resolved, was blocked, or found no binding
	TypeLayout   = "layout"   // a structural layout operation
)

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
	typ := TypeCommand
	if source == SourceInternal {
		typ = TypeInternal
	}
	r.record(typ, map[string]string{"id": id, "source": source})
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

// startsSession reports whether an event is meaningful enough to create the
// session file. Everything is, except a bare pane focus change: IKE emits one
// on startup when the session restore moves focus from the explorer to the
// restored editor, so a launch that is immediately quit would otherwise leave
// a ghost file holding that single event (#2318). Deferred events are not
// lost — they are held in memory and written once a meaningful event arrives.
func startsSession(typ string, data map[string]string) bool {
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
	if !r.started && !startsSession(typ, data) {
		// Hold it: this event alone must not create a file.
		r.pending = append(r.pending, r.newEvent(typ, data))
		if len(r.pending) > maxPending {
			r.pending = r.pending[len(r.pending)-maxPending:]
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
	r.mu.Unlock()
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
