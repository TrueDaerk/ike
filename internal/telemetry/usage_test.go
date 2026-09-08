package telemetry

import (
	"testing"
	"time"
)

// usage_test.go pins the usage aggregates (#2552) against synthetic logs:
// commands split by source (with the v1 internal filter), unbound chords per
// context, palette dismissals per mode with the query/results split, op
// lifecycles and slow dispatches — and the per-file cache, which must hand
// the usage half back unchanged on an untouched file.

// usageToday aggregates dir over the reference day.
func usageToday(t *testing.T, dir string) UsageSummary {
	t.Helper()
	return read(t, dir).UsageRange(day, day)
}

func srcCmd(t time.Time, id, source string) Event {
	return ev(t, TypeCommand, map[string]string{"id": id, "source": source})
}

func TestUsageCommandsSplitBySource(t *testing.T) {
	dir := t.TempDir()
	v1 := srcCmd(at(time.Minute), "lsp.documentSymbols", SourceInternal)
	v1.V = 1
	writeLog(t, dir, "a.jsonl",
		session(at(0), "aaa"),
		srcCmd(at(time.Minute), "editor.save", SourceKeybind),
		srcCmd(at(2*time.Minute), "editor.save", SourceKeybind),
		srcCmd(at(3*time.Minute), "editor.save", SourcePalette),
		srcCmd(at(4*time.Minute), "file.open", SourceMenu),
		srcCmd(at(5*time.Minute), "file.open", SourceMouse),
		v1,
		ev(at(6*time.Minute), TypeInternal, map[string]string{"id": "lsp.documentSymbols", "source": SourceInternal}),
	)
	s := usageToday(t, dir)
	if len(s.Commands) != 2 {
		t.Fatalf("commands = %+v, want editor.save and file.open only (internal filtered)", s.Commands)
	}
	save := s.Commands[0]
	if save.ID != "editor.save" || save.N != 3 || save.Keybind != 2 || save.Palette != 1 {
		t.Errorf("editor.save = %+v, want 3 total, 2 keybind, 1 palette", save)
	}
	open := s.Commands[1]
	if open.ID != "file.open" || open.N != 2 || open.Menu != 1 || open.Mouse != 1 {
		t.Errorf("file.open = %+v, want 2 total, 1 menu, 1 mouse", open)
	}
}

func TestUsageUnboundChordsByContext(t *testing.T) {
	dir := t.TempDir()
	unbound := func(t time.Time, chord, ctx, removed string) Event {
		d := map[string]string{"chord": chord, "status": KeyStatusUnbound, "context": ctx}
		if removed != "" {
			d["command"] = removed
		}
		return ev(t, TypeKey, d)
	}
	writeLog(t, dir, "a.jsonl",
		session(at(0), "aaa"),
		key(at(time.Minute), "ctrl+s"), // resolved: not a miss
		unbound(at(2*time.Minute), "cmd+shift+z", "editor[go]", ""),
		unbound(at(3*time.Minute), "cmd+shift+z", "editor[go]", ""),
		unbound(at(4*time.Minute), "cmd+shift+z", "explorer", ""),
		unbound(at(5*time.Minute), "cmd+alt+0", "editor[go]", "time.toggle"),
	)
	s := usageToday(t, dir)
	if len(s.Unbound) != 3 {
		t.Fatalf("unbound = %+v, want 3 rows", s.Unbound)
	}
	if u := s.Unbound[0]; u.Chord != "cmd+shift+z" || u.Context != "editor[go]" || u.N != 2 {
		t.Errorf("top row = %+v, want cmd+shift+z in editor[go] ×2", u)
	}
	var removed *UnboundUsage
	for i := range s.Unbound {
		if s.Unbound[i].Chord == "cmd+alt+0" {
			removed = &s.Unbound[i]
		}
	}
	if removed == nil || removed.Removed != "time.toggle" {
		t.Errorf("removed-by-config row = %+v, want Removed = time.toggle", removed)
	}
}

func TestUsagePaletteDismissalsPerMode(t *testing.T) {
	dir := t.TempDir()
	dismiss := func(t time.Time, mode, qlen, results, ms string) Event {
		d := map[string]string{"mode": mode, "query_len": qlen, "ms": ms}
		if results != "" {
			d["results"] = results
		}
		return ev(t, TypePaletteDismiss, d)
	}
	pick := func(t time.Time, mode string) Event {
		return ev(t, TypePalettePick, map[string]string{"mode": mode, "query_len": "3", "rank": "0", "results": "5"})
	}
	writeLog(t, dir, "a.jsonl",
		session(at(0), "aaa"),
		pick(at(time.Minute), ":"),
		pick(at(2*time.Minute), ":"),
		dismiss(at(3*time.Minute), ":", "0", "12", "1000"), // nothing typed
		dismiss(at(4*time.Minute), ":", "4", "0", "3000"),  // fruitless
		dismiss(at(5*time.Minute), ":", "4", "", "2000"),   // v5: results unknown
		dismiss(at(6*time.Minute), "@", "2", "7", "500"),   // found it, changed mind
	)
	s := usageToday(t, dir)
	if len(s.Palette) != 2 || s.Palette[0].Mode != ":" {
		t.Fatalf("palette = %+v, want ':' first (most dismissed)", s.Palette)
	}
	c := s.Palette[0]
	if c.Opens != 5 || c.Picks != 2 || c.Dismissed != 3 {
		t.Errorf("':' counts = %+v, want 5 opens, 2 picks, 3 dismissals", c)
	}
	if c.WithQuery != 2 || c.NoQuery != 1 || c.NoResults != 1 {
		t.Errorf("':' split = %+v, want 2 with query, 1 without, 1 fruitless", c)
	}
	if c.Rate != 0.6 {
		t.Errorf("':' rate = %v, want 0.6", c.Rate)
	}
	if c.AvgOpen != 2*time.Second {
		t.Errorf("':' avg open = %v, want 2s", c.AvgOpen)
	}
	if a := s.Palette[1]; a.Mode != "@" || a.Rate != 1 || a.NoResults != 0 {
		t.Errorf("'@' = %+v, want rate 1 and no fruitless search", a)
	}
}

func TestUsageOpsAndSlowDispatches(t *testing.T) {
	dir := t.TempDir()
	op := func(t time.Time, id, phase, ms string) Event {
		d := map[string]string{"id": id, "phase": phase}
		if ms != "" {
			d["ms"] = ms
		}
		return ev(t, TypeOp, d)
	}
	writeLog(t, dir, "a.jsonl",
		session(at(0), "aaa"),
		op(at(time.Minute), OpHTTPFlight, OpPhaseStart, ""),
		op(at(time.Minute+time.Second), OpHTTPFlight, OpPhaseOK, "400"),
		op(at(2*time.Minute), OpHTTPFlight, OpPhaseStart, ""),
		op(at(2*time.Minute+time.Second), OpHTTPFlight, OpPhaseError, "1200"),
		op(at(3*time.Minute), OpHTTPFlight, OpPhaseStart, ""), // never came back
		op(at(4*time.Minute), OpProjectSwitch, OpPhaseStart, ""),
		op(at(4*time.Minute+time.Second), OpProjectSwitch, OpPhaseOK, "80"),
		op(at(4*time.Minute+2*time.Second), OpProjectSwitch, "lsp", "9000"), // warm-up: not an end
		ev(at(5*time.Minute), TypeCommand, map[string]string{"id": "vcs.commit", "source": SourceKeybind, "ok": "true", "ms": "120"}),
		ev(at(6*time.Minute), TypeCommand, map[string]string{"id": "vcs.commit", "source": SourceKeybind, "ok": "true", "ms": "80"}),
		ev(at(7*time.Minute), TypeCommand, map[string]string{"id": "nope", "source": SourcePalette, "ok": "false", "ms": "0"}),
		srcCmd(at(8*time.Minute), "editor.save", SourceKeybind), // fast: not slow
	)
	s := usageToday(t, dir)
	if len(s.Ops) != 2 || s.Ops[0].ID != OpHTTPFlight {
		t.Fatalf("ops = %+v, want http.flight first (slowest max)", s.Ops)
	}
	h := s.Ops[0]
	if h.Started != 3 || h.OK != 1 || h.Errors != 1 || h.Canceled != 0 {
		t.Errorf("http.flight = %+v, want 3 started, 1 ok, 1 error", h)
	}
	if h.Avg != 800*time.Millisecond || h.Max != 1200*time.Millisecond {
		t.Errorf("http.flight timing = avg %v max %v, want 800ms / 1.2s", h.Avg, h.Max)
	}
	if p := s.Ops[1]; p.Avg != 80*time.Millisecond || p.Max != 80*time.Millisecond {
		t.Errorf("project.switch = %+v, want the lsp phase excluded (80ms)", p)
	}
	if len(s.Slow) != 2 || s.Slow[0].ID != "vcs.commit" {
		t.Fatalf("slow = %+v, want vcs.commit and nope", s.Slow)
	}
	if c := s.Slow[0]; c.N != 2 || c.Avg != 100*time.Millisecond || c.Max != 120*time.Millisecond || c.Failed != 0 {
		t.Errorf("vcs.commit = %+v, want 2 dispatches avg 100ms max 120ms", c)
	}
	if c := s.Slow[1]; c.N != 1 || c.Failed != 1 {
		t.Errorf("nope = %+v, want 1 failed dispatch", c)
	}
}

func TestUsageBucketsByEventDayAndRange(t *testing.T) {
	dir := t.TempDir()
	// A pre-v3 file has no session marker at all; usage counts regardless.
	writeLog(t, dir, "old.jsonl",
		srcCmd(at(0), "editor.save", SourceKeybind),
		srcCmd(at(-24*time.Hour), "editor.save", SourceKeybind),
		srcCmd(at(-9*24*time.Hour), "editor.save", SourceKeybind),
	)
	rep := read(t, dir)
	if n := rep.UsageRange(day, day).Commands[0].N; n != 1 {
		t.Errorf("today = %d, want 1", n)
	}
	if n := rep.UsageRange(day.AddDate(0, 0, -6), day).Commands[0].N; n != 2 {
		t.Errorf("week = %d, want 2", n)
	}
	if n := rep.UsageRange(day.AddDate(0, 0, -29), day).Commands[0].N; n != 3 {
		t.Errorf("month = %d, want 3", n)
	}
	if !rep.UsageRange(day.AddDate(0, 0, 1), day.AddDate(0, 0, 1)).Empty() {
		t.Error("tomorrow should be empty")
	}
}

func TestUsageSurvivesTheFileCache(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "a.jsonl",
		session(at(0), "aaa"),
		srcCmd(at(time.Minute), "editor.save", SourceKeybind),
	)
	r := NewReader(dir)
	first := r.Read().UsageRange(day, day)
	second := r.Read().UsageRange(day, day)
	if len(first.Commands) != 1 || len(second.Commands) != 1 || second.Commands[0].N != 1 {
		t.Errorf("cached read = %+v, want the same single dispatch as the first %+v", second, first)
	}
}

func TestFormatMs(t *testing.T) {
	cases := map[time.Duration]string{
		0:                             "0ms",
		12 * time.Millisecond:         "12ms",
		1400 * time.Millisecond:       "1.4s",
		2*time.Minute + 3*time.Second: "2m",
	}
	for d, want := range cases {
		if got := FormatMs(d); got != want {
			t.Errorf("FormatMs(%v) = %q, want %q", d, got, want)
		}
	}
}
