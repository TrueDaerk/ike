package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/httpclient"
	"ike/internal/layout"
	"ike/internal/menu"
	"ike/internal/palette"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/telemetry"
	"ike/internal/version"
)

// usageEvents flushes m's recorder and returns every event written for this
// model's isolated config dir.
func usageEvents(t *testing.T, m Model) []telemetry.Event {
	t.Helper()
	m.usage.Flush()
	dir := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "telemetry")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []telemetry.Event
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if line == "" {
				continue
			}
			var ev telemetry.Event
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatalf("bad JSONL line %q: %v", line, err)
			}
			out = append(out, ev)
		}
	}
	return out
}

// eventsOf filters by type.
func eventsOf(evs []telemetry.Event, typ string) []telemetry.Event {
	var out []telemetry.Event
	for _, ev := range evs {
		if ev.Type == typ {
			out = append(out, ev)
		}
	}
	return out
}

// telemetryModel builds an isolated model with one bindable test command.
func telemetryModel(t *testing.T, cfg host.MapConfig) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	reg := registry.New()
	reg.Add(fakePlugin{id: "tm", caps: plugin.Capabilities{Commands: []plugin.Command{{
		ID: "tm.fire", Title: "Fire", Scope: plugin.GlobalScope(),
		Run: func(host.API) tea.Cmd { return nil },
	}, {
		// A dispatch that blocks the update loop past the outcome threshold
		// (#2408), so the "ms" field has something to report.
		ID: "tm.slow", Title: "Slow", Scope: plugin.GlobalScope(),
		Run: func(host.API) tea.Cmd {
			time.Sleep(telemetry.CommandSlowThreshold + 10*time.Millisecond)
			return nil
		},
	}}}})
	m := NewWith(reg, cfg)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return tm.(Model)
}

// TestTelemetryKeybindRecordsCommandAndKey drives a bound chord and expects
// both a key "resolved" event and a command event sourced "keybind".
func TestTelemetryKeybindRecordsCommandAndKey(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{"keymap.bindings.ctrl+y": "tm.fire"})
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	m = tm.(Model)

	evs := usageEvents(t, m)
	cmds := eventsOf(evs, telemetry.TypeCommand)
	if len(cmds) != 1 || cmds[0].Data["id"] != "tm.fire" || cmds[0].Data["source"] != telemetry.SourceKeybind {
		t.Fatalf("want one keybind-sourced command event for tm.fire, got %v", cmds)
	}
	keys := eventsOf(evs, telemetry.TypeKey)
	if len(keys) != 1 || keys[0].Data["chord"] != "ctrl+y" || keys[0].Data["status"] != "resolved" ||
		keys[0].Data["command"] != "tm.fire" {
		t.Fatalf("want one resolved key event for ctrl+y, got %v", keys)
	}
}

// TestTelemetryPaletteAndMenuSources checks the invocation source survives the
// palette and menu dispatch paths.
func TestTelemetryPaletteAndMenuSources(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	tm, _ := m.Update(palette.RunCommandMsg{ID: "tm.fire"})
	m = tm.(Model)
	tm, _ = m.Update(menu.RunMsg{Command: "tm.fire"})
	m = tm.(Model)

	cmds := eventsOf(usageEvents(t, m), telemetry.TypeCommand)
	var sources []string
	for _, ev := range cmds {
		if ev.Data["id"] == "tm.fire" {
			sources = append(sources, ev.Data["source"])
		}
	}
	if len(sources) != 2 || sources[0] != telemetry.SourcePalette || sources[1] != telemetry.SourceMenu {
		t.Fatalf("want palette then menu sources, got %v", sources)
	}
}

// TestTelemetryUnboundChordRecorded expects a modified chord without a binding
// to land as an "unbound" key event — the missing-keybind signal.
func TestTelemetryUnboundChordRecorded(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	// ctrl+alt+0 is bound to nothing in any preset.
	tm, _ := m.Update(tea.KeyPressMsg{Code: '0', Mod: tea.ModCtrl | tea.ModAlt})
	m = tm.(Model)

	keys := eventsOf(usageEvents(t, m), telemetry.TypeKey)
	found := false
	for _, ev := range keys {
		if ev.Data["status"] == "unbound" && strings.Contains(ev.Data["chord"], "0") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unbound modified chord not recorded; key events: %v", keys)
	}
}

// TestTelemetryPlainTypingNeverRecorded is the privacy line: unmodified (and
// shift-only) presses must never appear in the log, bound or not.
func TestTelemetryPlainTypingNeverRecorded(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	m.cycleFocus() // focus the editor: plain keys are typing
	for _, r := range "secret" {
		tm, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(Model)
	}
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'S', Text: "S", Mod: tea.ModShift})
	m = tm.(Model)

	for _, ev := range eventsOf(usageEvents(t, m), telemetry.TypeKey) {
		if ev.Data["status"] != "unbound" {
			continue
		}
		c := ev.Data["chord"]
		if !strings.Contains(c, "ctrl") && !strings.Contains(c, "alt") && !strings.Contains(c, "cmd") {
			t.Fatalf("plain press %q leaked into the log", c)
		}
	}
}

// TestTelemetryStartAndQuitLeavesNoFile pins the ghost-session rule (#2318):
// a launch whose only telemetry is a pane focus change — what the session
// restore emits on startup — must leave no file behind at all.
func TestTelemetryStartAndQuitLeavesNoFile(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	m.cycleFocus()
	m.quit()

	dir := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "telemetry")
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("start-and-quit left telemetry files: %v", entries)
	}
}

// TestTelemetryLayoutEvents checks the structural layout hooks: split, pane
// focus and resize land as layout events with structural payloads only.
func TestTelemetryLayoutEvents(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	m.cycleFocus()
	m.SplitFocused(layout.ZoneRight)
	m.cycleFocus()
	m.resizeFocusedPane(layout.ZoneRight)

	evs := eventsOf(usageEvents(t, m), telemetry.TypeLayout)
	ops := map[string]int{}
	for _, ev := range evs {
		ops[ev.Data["op"]]++
	}
	if ops["split"] != 1 {
		t.Fatalf("want one split event, got ops %v", ops)
	}
	if ops["pane.focus"] == 0 {
		t.Fatalf("want pane.focus events, got ops %v", ops)
	}
	if ops["resize"] != 1 {
		t.Fatalf("want one resize event, got ops %v", ops)
	}
	for _, ev := range evs {
		if ev.Data["op"] == "split" && ev.Data["zone"] != "right" {
			t.Fatalf("split zone = %q, want right", ev.Data["zone"])
		}
	}
}

// TestTelemetryDisabledWritesNothing flips the setting off and expects no
// telemetry file at all.
func TestTelemetryDisabledWritesNothing(t *testing.T) {
	prev := config.Get()
	off := *prev
	off.Telemetry.Enabled = false
	config.Set(&off)
	t.Cleanup(func() { config.Set(prev) })

	m := telemetryModel(t, host.MapConfig{"keymap.bindings.ctrl+y": "tm.fire"})
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	m = tm.(Model)
	m.cycleFocus()
	m.SplitFocused(layout.ZoneRight)
	m.usage.Flush()

	dir := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "telemetry")
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		t.Fatalf("telemetry disabled but files were written: %v", entries)
	}
}

// TestTelemetryNoClearTextPaths opens a real file, exercises hooks, and scans
// every written line for the file's path segments — none may appear.
func TestTelemetryNoClearTextPaths(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	dir := t.TempDir()
	path := filepath.Join(dir, "very-secret-name.txt")
	if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	m.cycleFocus()
	m.SplitFocused(layout.ZoneRight)
	m.usage.Flush()

	tdir := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "telemetry")
	entries, err := os.ReadDir(tdir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(tdir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "very-secret-name") || strings.Contains(string(raw), "content") {
			t.Fatalf("telemetry leaked file path or content:\n%s", raw)
		}
	}
}

// TestTelemetrySessionMarker: the first meaningful event carries the deferred
// session marker (#2348) ahead of it — version, OS and a hashed project token,
// never the clear-text working directory.
func TestTelemetrySessionMarker(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{"keymap.bindings.ctrl+y": "tm.fire"})
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	m = tm.(Model)

	evs := usageEvents(t, m)
	sessions := eventsOf(evs, telemetry.TypeSession)
	if len(sessions) != 1 {
		t.Fatalf("want one session marker, got %v", sessions)
	}
	s := sessions[0]
	if s.Data["app"] != version.Short() || s.Data["os"] != runtime.GOOS {
		t.Fatalf("session marker version/os wrong: %v", s.Data)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	token := s.Data["project"]
	if len(token) != 12 {
		t.Fatalf("project token %q is not a 12-char hash", token)
	}
	if strings.Contains(wd, token) || strings.Contains(token, filepath.Base(wd)) {
		t.Fatalf("project token %q leaks the working directory %q", token, wd)
	}
	if evs[0].Type != telemetry.TypeSession {
		t.Fatalf("session marker must land first, got %v", evs[0])
	}
}

// TestTelemetryHTTPFlightLifecycle: a dispatch leaves a start op (flushed
// before the exchange departs, #2348) and its answer leaves the matching end
// op with duration, status class and streaming flag — no URL, key or label.
func TestTelemetryHTTPFlightLifecycle(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	send := func(ctx context.Context, source, key string, cb httpclient.StreamCallbacks) (*httpclient.Response, error) {
		return nil, nil // never executed: the returned tea.Cmd is not run
	}
	if cmd := m.dispatchHTTP("a.http", "GET /x", "GET /x", send); cmd == nil {
		t.Fatal("dispatch refused")
	}

	// The start op — and the events before it — must reach disk without any
	// explicit Flush: dispatchHTTP's FlushSoon is the only flusher here.
	deadline := time.Now().Add(5 * time.Second)
	for {
		found := false
		for _, ev := range eventsOfNoFlush(t) {
			if ev.Type == telemetry.TypeOp && ev.Data["phase"] == "start" {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("start op not flushed to disk before the exchange")
		}
		time.Sleep(5 * time.Millisecond)
	}

	tm, _ := m.Update(HTTPResponseMsg{Source: "a.http", Request: "GET /x",
		Resp: &httpclient.Response{Status: "200 OK", StatusCode: 200}})
	m = tm.(Model)

	ops := eventsOf(usageEvents(t, m), telemetry.TypeOp)
	if len(ops) != 2 {
		t.Fatalf("want start + end ops, got %v", ops)
	}
	end := ops[1]
	if end.Data["id"] != "http.flight" || end.Data["phase"] != "ok" ||
		end.Data["class"] != "2xx" || end.Data["stream"] != "false" {
		t.Fatalf("end op wrong: %v", end)
	}
	if _, err := strconv.Atoi(end.Data["ms"]); err != nil {
		t.Fatalf("end op ms %q is not a number", end.Data["ms"])
	}
	for _, ev := range ops {
		for k, v := range ev.Data {
			if strings.Contains(v, "a.http") || strings.Contains(v, "/x") {
				t.Fatalf("flight op leaks request detail: %s=%q", k, v)
			}
		}
	}
}

// TestTelemetryHTTPFlightCancelAndError: a user abort records "canceled", a
// transport failure "error" — and a stream marks its flag.
func TestTelemetryHTTPFlightCancelAndError(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	send := func(ctx context.Context, source, key string, cb httpclient.StreamCallbacks) (*httpclient.Response, error) {
		return nil, nil
	}
	m.dispatchHTTP("a.http", "k1", "GET /1", send)
	m.httpFlight[httpFlightKey("a.http", "k1")].canceled = true
	tm, _ := m.Update(HTTPResponseMsg{Source: "a.http", Request: "k1", Err: context.Canceled})
	m = tm.(Model)

	m.dispatchHTTP("a.http", "k2", "GET /2", send)
	tm, _ = m.Update(HTTPResponseMsg{Source: "a.http", Request: "k2", Err: errors.New("boom")})
	m = tm.(Model)

	m.dispatchHTTP("a.http", "k3", "GET /3", send)
	m.httpFlight[httpFlightKey("a.http", "k3")].streamed = true
	tm, _ = m.Update(HTTPResponseMsg{Source: "a.http", Request: "k3",
		Resp: &httpclient.Response{Status: "200 OK", StatusCode: 200}})
	m = tm.(Model)

	var phases []string
	var streams []string
	for _, ev := range eventsOf(usageEvents(t, m), telemetry.TypeOp) {
		if ev.Data["phase"] == "start" {
			continue
		}
		phases = append(phases, ev.Data["phase"])
		streams = append(streams, ev.Data["stream"])
	}
	if len(phases) != 3 || phases[0] != "canceled" || phases[1] != "error" || phases[2] != "ok" {
		t.Fatalf("want canceled/error/ok end phases, got %v", phases)
	}
	if streams[2] != "true" {
		t.Fatalf("streamed flight not flagged: %v", streams)
	}
}

// eventsOfNoFlush reads the session file without flushing the recorder — the
// seam for asserting FlushSoon put events on disk by itself.
func eventsOfNoFlush(t *testing.T) []telemetry.Event {
	t.Helper()
	dir := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "telemetry")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []telemetry.Event
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if line == "" {
				continue
			}
			var ev telemetry.Event
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue // a partially flushed last line is fine here
			}
			out = append(out, ev)
		}
	}
	return out
}

// TestTraceLogFollowsSetting: off by default nothing is written; on, every
// processed message leaves one structural line in trace.log (#2348).
func TestTraceLogFollowsSetting(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m = tm.(Model)
	trace := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "trace.log")
	if _, err := os.Stat(trace); !os.IsNotExist(err) {
		t.Fatalf("trace.log written with the setting off: %v", err)
	}

	prev := config.Get()
	on := *prev
	on.Perf.TraceLog = true
	config.Set(&on)
	t.Cleanup(func() { config.Set(prev) })
	tm, _ = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	_ = tm

	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatalf("trace.log missing with the setting on: %v", err)
	}
	line := string(raw)
	if !strings.Contains(line, "trace: tea.KeyPressMsg") || !strings.Contains(line, "flights=0") {
		t.Fatalf("trace line wrong: %q", line)
	}
	if strings.Contains(line, `"y"`) || strings.Contains(line, "text") {
		t.Fatalf("trace line leaks key content: %q", line)
	}
}

// TestTelemetryRecorderRidesProjectSwitch: the recorder is session state; the
// rebuilt model must carry the same one (same session id, one file per run).
func TestTelemetryRecorderRidesProjectSwitch(t *testing.T) {
	t.Chdir(t.TempDir()) // performSwitch chdirs into the target; restore after
	m := telemetryModel(t, host.MapConfig{})
	m.usage.Command("before.switch", telemetry.SourceInternal)
	sid := m.usage.SessionID()

	other := t.TempDir()
	tm, _ := m.performSwitch(other)
	fresh := tm.(Model)
	if fresh.usage != m.usage {
		t.Fatal("project switch built a new recorder; session state must ride across")
	}
	if fresh.usage.SessionID() != sid {
		t.Fatalf("session id changed across switch: %q -> %q", sid, fresh.usage.SessionID())
	}
	evs := eventsOf(usageEvents(t, fresh), telemetry.TypeLayout)
	found := false
	for _, ev := range evs {
		if ev.Data["op"] == "project.switch" {
			found = true
		}
	}
	if !found {
		t.Fatalf("project switch not recorded; layout events: %v", evs)
	}
}

// TestTopMessageDelta: the heartbeat's per-interval breakdown (#2402) counts
// cur minus prev, keeps the n loudest, orders ties by name, and reports a
// dead-quiet interval as empty rather than as "everything:0".
func TestTopMessageDelta(t *testing.T) {
	prev := map[string]uint64{"a.tick": 10, "b.key": 5}
	cur := map[string]uint64{"a.tick": 14, "b.key": 5, "c.out": 9, "d.msg": 4}
	if got, want := topMessageDelta(prev, cur, 3), "c.out:9,a.tick:4,d.msg:4"; got != want {
		t.Errorf("topMessageDelta = %q, want %q", got, want)
	}
	if got := topMessageDelta(cur, cur, 3); got != "" {
		t.Errorf("idle interval must be empty, got %q", got)
	}
	if got, want := topMessageDelta(nil, map[string]uint64{"x": 2}, 3), "x:2"; got != want {
		t.Errorf("nil prev: got %q, want %q", got, want)
	}
}

// TestTelemetryHeartbeatIntervalIsAMinute pins the #2408 acceptance criterion:
// at the old 10s cadence the beats were 61% of a two-day export, drowning the
// usage data they were supposed to accompany; a minute puts them near 20%.
func TestTelemetryHeartbeatIntervalIsAMinute(t *testing.T) {
	if telemetryHeartbeatInterval != 60*time.Second {
		t.Fatalf("telemetryHeartbeatInterval = %v, want 60s", telemetryHeartbeatInterval)
	}
}

// TestTelemetryCommandOutcome is the #2408 command-event criterion, in both
// shapes: a fast dispatch keeps id/source only, a slow one carries ok/ms, and
// an invocation of an unregistered id — the dispatch funnel's one failure
// mode — is recorded with ok=false instead of vanishing.
func TestTelemetryCommandOutcome(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	tm, _ := m.Update(palette.RunCommandMsg{ID: "tm.fire"})
	m = tm.(Model)
	tm, _ = m.Update(palette.RunCommandMsg{ID: "tm.slow"})
	m = tm.(Model)
	m.RunCommandFrom("tm.gone", telemetry.SourceMenu)

	byID := map[string]telemetry.Event{}
	for _, ev := range eventsOf(usageEvents(t, m), telemetry.TypeCommand) {
		byID[ev.Data["id"]] = ev
	}
	fast, ok := byID["tm.fire"]
	if !ok {
		t.Fatalf("no command event for tm.fire: %v", byID)
	}
	if _, has := fast.Data["ok"]; has {
		t.Errorf("a fast dispatch must keep the plain shape: %v", fast)
	}
	slow, ok := byID["tm.slow"]
	if !ok {
		t.Fatalf("no command event for tm.slow: %v", byID)
	}
	if slow.Data["ok"] != "true" {
		t.Errorf("slow dispatch: ok = %q, want true", slow.Data["ok"])
	}
	if ms, err := strconv.Atoi(slow.Data["ms"]); err != nil || ms < 50 {
		t.Errorf("slow dispatch: ms = %q, want at least 50", slow.Data["ms"])
	}
	gone, ok := byID["tm.gone"]
	if !ok {
		t.Fatalf("an unregistered command id must still be recorded: %v", byID)
	}
	if gone.Data["ok"] != "false" || gone.Data["source"] != telemetry.SourceMenu {
		t.Errorf("failed dispatch = %v, want ok=false from the menu", gone)
	}
}

// TestProjectClockPausesWhileBlurred: foreground time only. A terminal that
// reports focus lets a project sitting in a background window stop counting;
// a repeated focus report must not restart a running span (#2408).
func TestProjectClockPausesWhileBlurred(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := &projectClock{now: func() time.Time { return now }}
	c.focus()

	now = now.Add(10 * time.Second)
	c.blur()
	now = now.Add(time.Hour) // the window sat in the background
	c.focus()
	c.focus() // a second focus report changes nothing
	now = now.Add(5 * time.Second)

	if got, want := c.elapsed(), 15*time.Second; got != want {
		t.Fatalf("elapsed = %v, want %v (background time excluded)", got, want)
	}
	c.blur()
	if got, want := c.elapsed(), 15*time.Second; got != want {
		t.Fatalf("elapsed after blur = %v, want %v", got, want)
	}
}

// TestTelemetryProjectLeaveEvents is the #2408 per-project time criterion: a
// switch closes the departing project's budget, the quit closes the last one's,
// and both carry the hashed token — never a path.
func TestTelemetryProjectLeaveEvents(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	m.usage.Command("before.switch", telemetry.SourceInternal) // open the session file
	leaving := telemetryProjectToken()

	other := t.TempDir()
	tm, _ := m.performSwitch(other)
	fresh := tm.(Model)
	fresh.quit()

	var leaves []telemetry.Event
	for _, ev := range usageEvents(t, fresh) {
		if ev.Type == telemetry.TypeProjectLeave {
			leaves = append(leaves, ev)
		}
	}
	if len(leaves) != 2 {
		t.Fatalf("want a leave event per project, got %v", leaves)
	}
	if leaves[0].Data["reason"] != "switch" || leaves[0].Data["project"] != leaving {
		t.Errorf("switch leave = %v, want reason switch for the departing token %q", leaves[0], leaving)
	}
	if leaves[1].Data["reason"] != "quit" {
		t.Errorf("quit leave = %v, want reason quit", leaves[1])
	}
	for _, ev := range leaves {
		if _, ok := ev.Data["ms"]; !ok {
			t.Errorf("leave event without an active time: %v", ev)
		}
		if strings.Contains(ev.Data["project"], string(os.PathSeparator)) {
			t.Errorf("leave event carries a path: %v", ev)
		}
	}
}
