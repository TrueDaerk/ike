package editor

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/lang"
	"ike/internal/logline"
	"ike/internal/theme"
)

// logLoaded loads content under a .log path with the log language registered
// (the plugin package only glues logline.Spans to the registry, so the test
// registers the same shape directly), spans parsed and delivered.
func logLoaded(t *testing.T, content string) Model {
	t.Helper()
	lang.Register(lang.Language{ID: "log", Extensions: []string{"log"}, Spans: logline.Spans})
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(60, 10)
	m.SetFocused(true)
	cmd := m.Reparse()
	if cmd == nil {
		t.Fatal("log language must support span parsing")
	}
	mm, _ := m.Update(cmd())
	return mm
}

const logDoc = "2024-01-02 10:11:12 [main] ERROR com.example.Foo - boom\n"

// TestLogSeverityAndTimestampStyles: the severity token styles with the
// palette's error color, the timestamp renders faint, the thread name gets a
// rainbow slot.
func TestLogSeverityAndTimestampStyles(t *testing.T) {
	m := logLoaded(t, logDoc)

	if st, ok := m.styleAt(0, 28); !ok || st.GetForeground() != theme.DefaultPalette().Error {
		t.Errorf("severity style = %v (%v), want palette error color", st.GetForeground(), ok)
	}
	if st, ok := m.styleAt(0, 3); !ok || !st.GetFaint() {
		t.Errorf("timestamp must render faint (ok=%v)", ok)
	}
	if got := m.hlIndex.CaptureAt(0, 22); got != rainbowLogCapture("main") {
		t.Errorf("thread capture = %q, want %q", got, rainbowLogCapture("main"))
	}
}

// rainbowLogCapture mirrors the plugin's stable name→slot keying for
// assertions without hard-coding the hash.
func rainbowLogCapture(name string) string {
	spans := logline.Spans([]string{"INFO [" + name + "] x"})
	for _, s := range spans {
		if len(s.Capture) > len("log.rainbow.") && s.Capture[:len("log.rainbow.")] == "log.rainbow." {
			return s.Capture
		}
	}
	return ""
}

// TestLogToggleOffRendersRaw: with logRender off, log captures stop styling
// and the escape-byte conceal ranges disappear.
func TestLogToggleOffRendersRaw(t *testing.T) {
	m := logLoaded(t, "\x1b[31mERROR\x1b[0m boom\n")
	m.cursor = buffer.Position{Line: 0, Col: 7} // inside ERROR, outside escapes

	if got := len(m.lineConcealRanges(0)); got != 2 {
		t.Fatalf("conceal ranges = %d, want 2 escapes", got)
	}
	if _, ok := m.styleAt(0, 7); !ok {
		t.Fatal("severity must style while rendering is on")
	}

	m.toggleLogRendering()
	if got := len(m.lineConcealRanges(0)); got != 0 {
		t.Errorf("toggle off must reveal escape bytes, got %d ranges", got)
	}
	if _, ok := m.styleAt(0, 7); ok {
		t.Error("toggle off must render unstyled")
	}
	if !m.logRenderSet {
		t.Error("toggle must stick as a per-view override")
	}
}

// TestLogConcealRevealsUnderCaret (#1594 granularity): only the escape range
// the caret sits inside reveals.
func TestLogConcealRevealsUnderCaret(t *testing.T) {
	m := logLoaded(t, "\x1b[31mERROR\x1b[0m boom\n")
	m.cursor = buffer.Position{Line: 0, Col: 1} // inside the first escape

	ranges := m.lineConcealRanges(0)
	if len(ranges) != 1 {
		t.Fatalf("ranges = %+v, want only the second escape concealed", ranges)
	}
	if ranges[0].start != 10 {
		t.Errorf("remaining conceal = %+v, want the [10,14) escape", ranges[0])
	}
}

// TestLogANSIStyleResolvesPalette: an ansi.* capture resolves indexed colors
// against the theme's terminal palette and carries the attributes.
func TestLogANSIStyleResolvesPalette(t *testing.T) {
	m := logLoaded(t, logDoc)
	st, ok := m.logStyle("ansi.fg1.bold")
	if !ok || !st.GetBold() {
		t.Fatalf("ansi style = ok=%v bold=%v", ok, st.GetBold())
	}
	if st.GetForeground() != theme.DefaultPalette().ANSI[1] {
		t.Errorf("fg = %v, want terminal palette red", st.GetForeground())
	}
}
