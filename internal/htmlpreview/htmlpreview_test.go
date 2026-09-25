package htmlpreview

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
)

// longDoc is an HTML document of n paragraphs, one per source line, so the
// source line of paragraph i is i+2 (after <html> and <body>).
func longDoc(n int) string {
	var b strings.Builder
	b.WriteString("<html>\n<body>\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "<p>para%03d</p>\n", i)
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

func sized(t *testing.T, src string) Model {
	t.Helper()
	m := New("htmlpreview", "/tmp/page.html", theme.DefaultPalette())
	m.SetSize(60, 10)
	m.SetSourceImmediate(src)
	m.Flush()
	return m
}

func plain(m Model) string { return ansi.Strip(m.View()) }

func TestIsHTMLPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/a/index.html":               true,
		"/a/INDEX.HTM":                true,
		"/a/page.xhtml":               true,
		"/a/page.html.gz":             true,
		"/a/page.html.gz!page.html":   true,
		"/a/dump.gz!index.htm":        true,
		"/a/readme.md":                false,
		"/a/app.log.gz":               false,
		"/a/archive.tar.gz!notes.txt": false,
		"":                            false,
	} {
		if got := IsHTMLPath(path); got != want {
			t.Errorf("IsHTMLPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestRendersOnOpen(t *testing.T) {
	m := sized(t, "<html><head><title>Hi</title></head><body><h1>Welcome</h1><p>Hello <b>world</b></p></body></html>")
	v := plain(m)
	if !strings.Contains(v, "Welcome") || !strings.Contains(v, "Hello world") {
		t.Fatalf("rendered view missing content:\n%s", v)
	}
	if strings.Contains(v, "<p>") || strings.Contains(v, "<h1>") {
		t.Fatalf("markup leaked into the rendering:\n%s", v)
	}
	if m.Title() != "Hi" {
		t.Fatalf("title = %q, want Hi", m.Title())
	}
	if n := len(strings.Split(m.View(), "\n")); n != 10 {
		t.Fatalf("view is %d rows, want the pane height 10", n)
	}
}

func TestUnsizedPaneRendersOnFirstSize(t *testing.T) {
	m := New("htmlpreview", "/tmp/page.html", nil)
	m.SetSourceImmediate("<p>late</p>")
	if m.View() != "" {
		t.Fatal("an unsized pane renders nothing")
	}
	m.SetSize(40, 5)
	m.Flush()
	if !strings.Contains(plain(m), "late") {
		t.Fatalf("first SetSize must render the pending source:\n%s", plain(m))
	}
}

// TestDebouncedRenderDropsStaleTicks: only the tick armed by the newest
// SetSource renders; an older one is a no-op.
func TestDebouncedRenderDropsStaleTicks(t *testing.T) {
	m := sized(t, "<p>first</p>")
	if m.SetSource("<p>second</p>") == nil {
		t.Fatal("SetSource must arm the debounce tick")
	}
	m.SetSource("<p>third</p>")
	if !strings.Contains(plain(m), "first") {
		t.Fatal("nothing re-renders before the tick fires")
	}
	settle(&m, m.Update(RenderTickMsg{Key: "htmlpreview", Seq: m.seq - 1}))
	if !strings.Contains(plain(m), "first") {
		t.Fatal("a stale tick must not render")
	}
	settle(&m, m.Update(RenderTickMsg{Key: "other", Seq: m.seq}))
	if !strings.Contains(plain(m), "first") {
		t.Fatal("another pane's tick must not render")
	}
	settle(&m, m.Update(RenderTickMsg{Key: "htmlpreview", Seq: m.seq}))
	if v := plain(m); !strings.Contains(v, "third") || strings.Contains(v, "second") {
		t.Fatalf("the newest tick renders the newest source:\n%s", v)
	}
}

// TestCursorSyncIsLineAccurate: the caret's source line lands its own
// paragraph a third down the viewport, through the source map.
func TestCursorSyncIsLineAccurate(t *testing.T) {
	m := sized(t, longDoc(80))
	for _, para := range []int{0, 20, 55, 79} {
		m.SetCursorLine(para + 2)
		want := fmt.Sprintf("para%03d", para)
		line, ok := m.doc.LineForSourceLine(para + 2)
		if !ok || !strings.Contains(ansi.Strip(m.doc.Lines[line]), want) {
			t.Fatalf("source map puts line %d on %q", para+2, ansi.Strip(m.doc.Lines[min(line, len(m.doc.Lines)-1)]))
		}
		if !strings.Contains(plain(m), want) {
			t.Fatalf("cursor on %s: view does not show it:\n%s", want, plain(m))
		}
		if para == 55 && m.Top() != line-m.h/3 {
			t.Fatalf("mid-document target sits at top %d, want %d (a third down)", m.Top(), line-m.h/3)
		}
	}
}

// TestCursorSyncSurvivesRerender: an edit that re-renders keeps the view on
// the caret's line.
func TestCursorSyncSurvivesRerender(t *testing.T) {
	m := sized(t, longDoc(80))
	m.SetCursorLine(52)
	top := m.Top()
	m.SetSource(longDoc(80) + "<p>tail</p>")
	settle(&m, m.Update(RenderTickMsg{Key: "htmlpreview", Seq: m.seq}))
	if m.Top() != top {
		t.Fatalf("re-render moved the view from %d to %d", top, m.Top())
	}
}

func TestScrollKeysAndWheel(t *testing.T) {
	m := sized(t, longDoc(80))
	m.SetFocused(true)
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.Top() != 1 {
		t.Fatalf("j scrolls one line, top = %d", m.Top())
	}
	m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	if m.Top() != m.maxTop() || m.maxTop() == 0 {
		t.Fatalf("G goes to the end, top = %d max = %d", m.Top(), m.maxTop())
	}
	m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	if m.Top() != 0 {
		t.Fatalf("g goes to the start, top = %d", m.Top())
	}
	m.ScrollBy(5)
	if m.Top() != 5 {
		t.Fatalf("wheel scrolls, top = %d", m.Top())
	}
	m.ScrollBy(-50)
	if m.Top() != 0 {
		t.Fatalf("scroll clamps at the start, top = %d", m.Top())
	}
}

func TestResizeRewraps(t *testing.T) {
	m := sized(t, "<p>"+strings.Repeat("word ", 40)+"</p>")
	wide := len(m.Lines())
	m.SetSize(20, 10)
	m.Flush()
	if len(m.Lines()) <= wide {
		t.Fatalf("narrowing the pane must re-wrap: %d lines at 60, %d at 20", wide, len(m.Lines()))
	}
	for _, l := range m.Lines() {
		if w := ansi.StringWidth(l); w > 20 {
			t.Fatalf("line %q is %d wide, pane is 20", ansi.Strip(l), w)
		}
	}
}

func TestPaletteSwitchRerenders(t *testing.T) {
	m := sized(t, "<h1>Head</h1>")
	before := strings.Join(m.Lines(), "\n")
	light := *theme.DefaultPalette()
	light.Accent = nil
	light.Dark = !light.Dark
	m.SetPalette(&light)
	m.Flush()
	if strings.Join(m.Lines(), "\n") == before {
		t.Fatal("a palette switch must re-render in the new colours")
	}
}
