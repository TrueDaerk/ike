package editor

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/largefile"
	_ "ike/plugins/languages/json"
)

// ndjsonModel builds a view over n in-memory ndjson lines under an .ndjson
// path (no file on disk: the parse resolves its grammar by path).
func ndjsonModel(t *testing.T, n, height int) Model {
	t.Helper()
	m := New()
	m.path = "/virtual/stream.ndjson"
	m.buf = buffer.New(ndjsonLines(n, 120))
	m.SetSize(100, height)
	if !m.lineScoped() {
		t.Fatal("setup: ndjson must be registered line-scoped")
	}
	return m
}

// runParse runs a parse command and returns its SpansMsg.
func runParse(t *testing.T, cmd tea.Cmd) highlight.SpansMsg {
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

// TestLineScopedParsesViewportWindowOnly guards #2996: a 10 000-line ndjson
// buffer's parseCmd parses the visible rows plus the margin, nothing more,
// and a scroll past the window re-parses the new one.
func TestLineScopedParsesViewportWindowOnly(t *testing.T) {
	m := ndjsonModel(t, 10_000, 40)
	msg := runParse(t, m.parseCmd())
	if !msg.Windowed || msg.WinFrom != 0 || msg.WinTo != 40+lineScopeMinMargin {
		t.Fatalf("the first parse must cover the viewport plus the margin: %+v", [3]any{msg.Windowed, msg.WinFrom, msg.WinTo})
	}
	if n := m.lineSpans.size(); n != msg.WinTo {
		t.Fatalf("only the window's %d lines may have been parsed, cache holds %d", msg.WinTo, n)
	}
	for _, s := range msg.Spans {
		if s.Line >= msg.WinTo {
			t.Fatalf("span outside the window: %+v", s)
		}
	}
	m, _ = m.Update(msg)
	if !m.windowCovered() {
		t.Fatal("the landed window must cover the viewport")
	}
	if m.windowParseCmd() != nil {
		t.Fatal("no re-parse while the viewport stays inside the window")
	}
	// Scroll far past the window (wheel / scrollbar path): ScrollCmd hands
	// back the re-parse.
	m.SetScroll(5000, 0)
	msg2 := runParse(t, m.ScrollCmd())
	if msg2.WinFrom != 5000-lineScopeMinMargin || msg2.WinTo != 5000+40+lineScopeMinMargin {
		t.Fatalf("the scrolled parse must re-center on the viewport: [%d, %d)", msg2.WinFrom, msg2.WinTo)
	}
	m, _ = m.Update(msg2)
	if m.SyntaxCapture(5000, 1) == "" {
		t.Fatal("the new window's lines must be highlighted")
	}
	if m.SyntaxCapture(0, 1) != "" {
		t.Fatal("lines outside the window carry no spans")
	}
	if m.windowParseCmd() != nil {
		t.Fatal("the new window covers the viewport")
	}
	// The cache keeps both windows: a scroll back costs no parse of its own
	// lines, only the cache lookups.
	if n, want := m.lineSpans.size(), msg.WinTo+(msg2.WinTo-msg2.WinFrom); n != want {
		t.Fatalf("both windows' lines must be cached: want %d, got %d", want, n)
	}
}

// TestLineScopedUpdateReparsesOnKeyScroll: a key that moves the viewport
// out of the parsed window makes Update batch the re-parse.
func TestLineScopedUpdateReparsesOnKeyScroll(t *testing.T) {
	m := ndjsonModel(t, 10_000, 40)
	m, _ = m.Update(runParse(t, m.parseCmd()))
	m, cmd := m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	if cmd == nil {
		t.Fatal("G must scroll out of the window and schedule a parse")
	}
	// The batch may wrap the parse command; drain it through the gate's
	// message by running the batch's commands until a SpansMsg shows.
	msg := findSpans(t, cmd)
	if msg.WinTo != 10_000 || msg.WinFrom >= 10_000-40 {
		t.Fatalf("the parse must cover the buffer's end: [%d, %d)", msg.WinFrom, msg.WinTo)
	}
}

// findSpans runs cmd (or the commands of a batch) until one yields a
// SpansMsg.
func findSpans(t *testing.T, cmd tea.Cmd) highlight.SpansMsg {
	t.Helper()
	switch msg := cmd().(type) {
	case highlight.SpansMsg:
		return msg
	case tea.BatchMsg:
		for _, c := range msg {
			if c == nil {
				continue
			}
			if s, ok := c().(highlight.SpansMsg); ok {
				return s
			}
		}
	}
	t.Fatal("no SpansMsg among the commands")
	return highlight.SpansMsg{}
}

// TestLineScopedSpansEqualSingleLineJSON: a visible line's spans are exactly
// those of parsing that line alone as a JSON document. The one-line document
// goes through the ndjson id: it shares the JSON grammar and span hooks, and
// other tests in this package re-register "json" grammar-less (the registry
// is process-wide), which would make a ".json" comparison vacuous.
func TestLineScopedSpansEqualSingleLineJSON(t *testing.T) {
	m := ndjsonModel(t, 200, 10)
	msg := runParse(t, m.parseCmd())
	for _, line := range []int{0, 7, 42} {
		var got []highlight.Span
		for _, s := range msg.Spans {
			if s.Line == line {
				s.Line = 0
				got = append(got, s)
			}
		}
		want := highlight.Highlight("/virtual/one.ndjson", []string{m.buf.Line(line)})
		if len(want) == 0 {
			t.Fatal("setup: the JSON grammar must produce spans")
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("line %d: windowed spans differ from a single-line JSON parse:\n got %v\nwant %v", line, got, want)
		}
	}
}

// TestLineScopedHighlightStaysOnPastCliff: an ndjson buffer flagged large
// keeps its parse — only the parse — while the other services stay gated
// and the badge still lists them.
func TestLineScopedHighlightStaysOnPastCliff(t *testing.T) {
	m := ndjsonModel(t, 500, 20)
	m.largeFile = true
	if m.FeatureOff(largefile.FeatureHighlight) {
		t.Fatal("highlighting must stay on for a line-scoped language past the cliff")
	}
	if !m.FeatureOff(largefile.FeatureLSP) || !m.FeatureOff(largefile.FeatureVCS) {
		t.Fatal("the other services keep their gate")
	}
	for _, f := range m.DegradedFeatures() {
		if f == largefile.FeatureHighlight {
			t.Fatal("the badge must not list highlighting")
		}
	}
	if len(m.DegradedFeatures()) != int(largefile.FeatureCount)-1 {
		t.Fatalf("every other feature is degraded, got %v", m.DegradedFeatures())
	}
	msg := runParse(t, m.parseCmd())
	m, _ = m.Update(msg)
	if m.SyntaxCapture(0, 1) == "" {
		t.Fatal("the visible lines must be highlighted")
	}
	// Plain JSON past the cliff stays unparsed, as before.
	j := New()
	j.path = "/virtual/doc.json"
	j.buf = buffer.New([]string{`{"a": 1}`})
	j.largeFile = true
	if j.parseCmd() != nil {
		t.Fatal("a whole-document language past the cliff schedules no parse")
	}
}

// TestLineScopedEditReparsesOnlyTheChangedLine: the cache is keyed by line
// text, so an edit costs one line's parse.
func TestLineScopedEditReparsesOnlyTheChangedLine(t *testing.T) {
	m := ndjsonModel(t, 300, 20)
	m, _ = m.Update(runParse(t, m.parseCmd()))
	before := m.lineSpans.size()
	lines := m.buf.Lines()
	lines[3] = `{"edited": true}`
	m.buf = buffer.New(lines)
	m.docVersion++
	msg := runParse(t, m.parseCmd())
	if msg.Version != m.docVersion {
		t.Fatal("the parse must carry the new version")
	}
	if n := m.lineSpans.size(); n != before+1 {
		t.Fatalf("one new line text must cost one cache entry: %d -> %d", before, n)
	}
}

// TestLineSpanCacheRotates: the cache stays bounded at two generations.
func TestLineSpanCacheRotates(t *testing.T) {
	c := newLineSpanCache()
	for i := 0; i < lineSpanCacheCap*3; i++ {
		c.put("x.ndjson", string(rune('a'+i%26))+string(rune(i)), lineParse{})
	}
	if n := c.size(); n > lineSpanCacheCap*2 {
		t.Fatalf("the cache must hold at most two generations, got %d", n)
	}
	c.put("y.ndjson", "z", lineParse{})
	if n := c.size(); n != 1 {
		t.Fatalf("a different grammar path empties the cache, got %d", n)
	}
}
