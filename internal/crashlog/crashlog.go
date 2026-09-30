// Package crashlog writes IKE's crash reports (#2836): one file per caught
// panic under the user log directory, holding everything a post-mortem
// needs — the version, the panic value, the panicking goroutine's stack and
// a dump of every goroutine, the key context and focused pane at the time,
// and the last usage-telemetry events of the session. It is a leaf package:
// the root model, the goroutine guard (internal/safego) and main all write
// through it, and nothing here depends on the UI.
//
// A crash without a trace must not be possible in an IDE. bubbletea catches
// panics in Update/View/Cmd goroutines, restores the terminal and prints the
// stack to stderr — which the alternate screen has just swallowed. IKE's own
// handlers therefore run *inside* that catcher (the model's Update/View
// wrappers, the Cmd guard, safego.Go) and write the report before the
// program even notices, and main's fallback covers whatever slipped past.
//
// The report is content-free in the telemetry sense: chords, command ids,
// context ids, pane kinds and file paths, never buffer text.
package crashlog

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ike/internal/version"
)

// KeepFiles caps the crash directory: writing a new report prunes the oldest
// crash logs beyond KeepFiles-1, so a crash loop never fills the disk.
const KeepFiles = 20

// filePrefix and fileSuffix name the reports: crash-<timestamp>-<session>.log.
const (
	filePrefix = "crash-"
	fileSuffix = ".log"
	ackFile    = "crash-ack"
)

// maxPerReason bounds how many reports one process writes for the same
// panic message: a panic recurring on every frame (a View that keeps
// failing) would otherwise cost a full goroutine dump per render. Beyond the
// bound the crash is counted and the count lands in the next report.
const maxPerReason = 3

// Report is one caught panic.
type Report struct {
	// Where names the catcher: "update", "view", "cmd", "goroutine:<name>",
	// "program" (main's fallback).
	Where string
	// Value is the recovered panic value.
	Value any
	// Stack is the panicking goroutine's stack (runtime/debug.Stack, taken
	// inside the recover); empty means "not available" (main's fallback).
	Stack []byte
	// Msg is the message type Update was processing, when known.
	Msg string
	// Extra is free-form key/value context the catcher adds (the stderr tail
	// main captured, a goroutine name, a session id).
	Extra map[string]string
}

// Focus is the UI state snapshot the root model publishes after every settled
// Update pass (SetFocus): the crash writer reads it from any goroutine.
type Focus struct {
	KeyContext string
	PaneKind   string
	PanePath   string
}

var (
	focus  atomic.Pointer[Focus]
	recent atomic.Pointer[func() []string]

	mu      sync.Mutex
	counts  = map[string]int{}
	dropped int
	session string
	// dirOverride is the test seam for the log directory.
	dirOverride string
	// last is the path of the last report written by this process.
	last string
)

// SetFocus publishes the current key context and focused pane. Cheap and
// lock-free; the model calls it whenever the values change.
func SetFocus(keyContext, paneKind, panePath string) {
	focus.Store(&Focus{KeyContext: keyContext, PaneKind: paneKind, PanePath: panePath})
}

// SetFocusIfChanged is SetFocus without the allocation when nothing moved —
// the root model calls it after every settled Update pass.
func SetFocusIfChanged(keyContext, paneKind, panePath string) {
	if f := focus.Load(); f != nil && f.KeyContext == keyContext && f.PaneKind == paneKind && f.PanePath == panePath {
		return
	}
	SetFocus(keyContext, paneKind, panePath)
}

// CurrentFocus returns the published snapshot (nil before the first SetFocus).
func CurrentFocus() *Focus { return focus.Load() }

// SetRecent installs the provider of the last telemetry events (the usage
// recorder's in-memory ring), called at write time.
func SetRecent(fn func() []string) {
	if fn == nil {
		recent.Store(nil)
		return
	}
	recent.Store(&fn)
}

// SetSession names the session in the report file name (the usage recorder's
// session id, so the crash log and the telemetry file line up). Unset, a
// random per-process tag is used.
func SetSession(id string) {
	mu.Lock()
	defer mu.Unlock()
	if id != "" {
		session = id
	}
}

// SetDir overrides the log directory (tests). Empty restores discovery.
func SetDir(dir string) {
	mu.Lock()
	defer mu.Unlock()
	dirOverride = dir
}

// Dir is the crash-log directory: $IKE_CONFIG_DIR/logs when the override is
// set, ~/.ike/logs otherwise — the language-server logs' directory, so every
// diagnostic file IKE writes for the user lives in one place.
func Dir() string {
	mu.Lock()
	o := dirOverride
	mu.Unlock()
	if o != "" {
		return o
	}
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "logs")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", "logs")
}

// Last returns the path of the last report this process wrote ("" when none).
func Last() string {
	mu.Lock()
	defer mu.Unlock()
	return last
}

// sessionTag returns the session id for file names, minting one on first use.
// Called under mu.
func sessionTag() string {
	if session == "" {
		var b [6]byte
		if _, err := rand.Read(b[:]); err == nil {
			session = hex.EncodeToString(b[:])
		} else {
			session = "000000000000"
		}
	}
	return session
}

// Write renders rep into a new crash file and returns its path. A report
// beyond the per-reason bound is counted and not written ("" path, nil
// error); a directory that cannot be created is the only error. Write never
// panics — it is called from inside recover handlers.
func Write(rep Report) (path string, err error) {
	defer func() {
		if r := recover(); r != nil {
			path, err = "", fmt.Errorf("crashlog: writer panicked: %v", r)
		}
	}()
	reason := fmt.Sprint(rep.Value)
	mu.Lock()
	counts[reason]++
	n := counts[reason]
	if n > maxPerReason {
		dropped++
		mu.Unlock()
		return "", nil
	}
	skipped := dropped
	dropped = 0
	tag := sessionTag()
	mu.Unlock()

	dir := Dir()
	if dir == "" {
		return "", fmt.Errorf("crashlog: no log directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	now := time.Now()
	stamp := now.UTC().Format("20060102T150405.000Z")
	path = filepath.Join(dir, fmt.Sprintf("%s%s-%s%s", filePrefix, stamp, tag, fileSuffix))
	for i := 1; ; i++ {
		// Two reports in one millisecond (a panic per frame) keep both files.
		if _, statErr := os.Stat(path); statErr != nil {
			break
		}
		path = filepath.Join(dir, fmt.Sprintf("%s%s-%s-%d%s", filePrefix, stamp, tag, i, fileSuffix))
	}
	body := render(rep, now, n, skipped)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	mu.Lock()
	last = path
	mu.Unlock()
	prune(dir)
	return path, nil
}

// render composes the report text.
func render(rep Report, now time.Time, nth, skipped int) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "IKE crash report\n")
	fmt.Fprintf(&b, "time:     %s\n", now.Format(time.RFC3339Nano))
	fmt.Fprintf(&b, "version:  %s\n", version.Full())
	fmt.Fprintf(&b, "go:       %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "where:    %s\n", rep.Where)
	if rep.Msg != "" {
		fmt.Fprintf(&b, "message:  %s\n", rep.Msg)
	}
	fmt.Fprintf(&b, "panic:    %v\n", rep.Value)
	if nth > 1 {
		fmt.Fprintf(&b, "repeat:   %d of at most %d reports for this panic in this process\n", nth, maxPerReason)
	}
	if skipped > 0 {
		fmt.Fprintf(&b, "skipped:  %d earlier repeats were not written\n", skipped)
	}
	if f := focus.Load(); f != nil {
		fmt.Fprintf(&b, "context:  %s\n", f.KeyContext)
		fmt.Fprintf(&b, "pane:     %s", f.PaneKind)
		if f.PanePath != "" {
			fmt.Fprintf(&b, " %s", f.PanePath)
		}
		b.WriteString("\n")
	}
	keys := make([]string, 0, len(rep.Extra))
	for k := range rep.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := rep.Extra[k]
		if strings.Contains(v, "\n") {
			fmt.Fprintf(&b, "\n--- %s ---\n%s\n", k, strings.TrimRight(v, "\n"))
		} else {
			fmt.Fprintf(&b, "%-9s %s\n", k+":", v)
		}
	}
	if fn := recent.Load(); fn != nil {
		if events := (*fn)(); len(events) > 0 {
			fmt.Fprintf(&b, "\n--- last %d telemetry events (oldest first) ---\n", len(events))
			for _, e := range events {
				b.WriteString(e)
				b.WriteString("\n")
			}
		}
	}
	if len(rep.Stack) > 0 {
		b.WriteString("\n--- panicking goroutine ---\n")
		b.Write(bytes.TrimRight(rep.Stack, "\n"))
		b.WriteString("\n")
	}
	b.WriteString("\n--- all goroutines ---\n")
	b.Write(allStacks())
	return b.Bytes()
}

// allStacks dumps every goroutine, growing the buffer until it fits (capped
// at 16 MiB — a runaway goroutine count must not stall the crash path).
func allStacks() []byte {
	buf := make([]byte, 256<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return buf[:n]
		}
		if len(buf) >= 16<<20 {
			return buf
		}
		buf = make([]byte, len(buf)*2)
	}
}

// prune keeps the newest KeepFiles crash logs in dir.
func prune(dir string) {
	logs := List(dir)
	for len(logs) > KeepFiles {
		_ = os.Remove(logs[0])
		logs = logs[1:]
	}
}

// List returns the crash logs in dir, oldest first (the timestamp in the
// name sorts them; "" dir means Dir()).
func List(dir string) []string {
	if dir == "" {
		dir = Dir()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, filePrefix) || !strings.HasSuffix(n, fileSuffix) {
			continue
		}
		out = append(out, filepath.Join(dir, n))
	}
	sort.Strings(out)
	return out
}

// Latest returns the newest crash log in Dir() ("" when none).
func Latest() string {
	logs := List("")
	if len(logs) == 0 {
		return ""
	}
	return logs[len(logs)-1]
}

// Unacknowledged returns the newest crash log written since the last
// Acknowledge, "" when there is none — the next-launch notice's question.
func Unacknowledged() string {
	latest := Latest()
	if latest == "" {
		return ""
	}
	ack, _ := os.ReadFile(filepath.Join(Dir(), ackFile))
	if strings.TrimSpace(string(ack)) >= filepath.Base(latest) {
		return ""
	}
	return latest
}

// Acknowledge records path as seen: Unacknowledged stays empty until a newer
// crash log appears.
func Acknowledge(path string) {
	if path == "" {
		return
	}
	dir := Dir()
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, ackFile), []byte(filepath.Base(path)+"\n"), 0o644)
}

// Summary is the one-line description of a panic for notifications: the
// panic value, cut to a readable length.
func Summary(v any) string {
	s := strings.ReplaceAll(fmt.Sprint(v), "\n", " ")
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}

// StackHere is debug.Stack for callers that build a Report themselves.
func StackHere() []byte { return debug.Stack() }

// ResetForTest clears the per-process throttle and session (tests).
func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	counts = map[string]int{}
	dropped = 0
	session = ""
	last = ""
}
