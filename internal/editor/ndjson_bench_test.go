package editor

import (
	"os"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/textenc"
	_ "ike/plugins/languages/json"
)

// runParseMsg runs a parse command and returns its SpansMsg.
func runParseMsg(t *testing.T, cmd tea.Cmd) highlight.SpansMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a parse command")
	}
	msg, ok := cmd().(highlight.SpansMsg)
	if !ok {
		t.Fatalf("expected a SpansMsg, got %T", msg)
	}
	return msg
}

// TestNDJSONOpenPhases prints the phase split of a synchronous open (#2996):
// read, decode, buffer build, full Load, first View. Run with -v; it only
// asserts that each step completes.
func TestNDJSONOpenPhases(t *testing.T) {
	if testing.Short() {
		t.Skip("phase measurement")
	}
	for _, fx := range []struct {
		name  string
		n, wd int
	}{{"small-900KB", 2000, 450}, {"big-100MB", 200_000, 500}} {
		t.Run(fx.name, func(t *testing.T) {
			// The 100 MB case is a measurement, not a check: it alone took the
			// editor package past a minute in a full parallel run (#2997), so it
			// runs on request — IKE_SLOW_TESTS=1 go test ./internal/editor -run NDJSON -v.
			if fx.n > 10_000 && os.Getenv("IKE_SLOW_TESTS") == "" {
				t.Skip("100 MB phase measurement; set IKE_SLOW_TESTS=1 to run it")
			}
			path := ndjsonFixture(t, t.TempDir(), fx.n, fx.wd)
			t0 := time.Now()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			tRead := time.Since(t0)
			t1 := time.Now()
			text, _, err := textenc.Decode(data, textenc.UTF8)
			if err != nil {
				t.Fatal(err)
			}
			tDecode := time.Since(t1)
			t2 := time.Now()
			buf := buffer.FromString(text)
			tBuf := time.Since(t2)
			m := New()
			m.SetSize(200, 50)
			t3 := time.Now()
			if err := m.LoadSync(path); err != nil {
				t.Fatal(err)
			}
			tLoad := time.Since(t3)
			t4 := time.Now()
			_ = m.View()
			tView := time.Since(t4)
			t5 := time.Now()
			spans := highlight.Highlight(path, buf.Lines())
			tParseAll := time.Since(t5)
			t6 := time.Now()
			lines := buf.Lines()
			spansWin := highlight.Highlight(path, lines[:60])
			tParseWin := time.Since(t6)
			t.Logf("%s: read %v decode %v buffer %v | LoadSync %v | first View %v | parse all %v (%d spans) | parse 60 lines %v (%d spans)",
				fx.name, tRead, tDecode, tBuf, tLoad, tView, tParseAll, len(spans), tParseWin, len(spansWin))
			// On-loop costs: the whole-document SpansMsg landing (the
			// before state, index of every span) versus the line-scoped one,
			// and — for the async path — the LoadMsg landing.
			whole := highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: spans}
			t7 := time.Now()
			m, _ = m.Update(whole)
			tWhole := time.Since(t7)
			win := runParseMsg(t, m.parseCmd())
			t8 := time.Now()
			m, _ = m.Update(win)
			tWin := time.Since(t8)
			a := New()
			a.SetSize(200, 50)
			tLand := time.Duration(0)
			if err := a.Load(path); err == nil && a.Loading() {
				lm := a.TakeLoadCmd()()
				t9 := time.Now()
				a, _ = a.Update(lm)
				tLand = time.Since(t9)
			}
			t.Logf("%s: on-loop SpansMsg landing whole-doc %v | windowed %v (%d spans) | async LoadMsg landing %v",
				fx.name, tWhole, tWin, len(win.Spans), tLand)
		})
	}
}
