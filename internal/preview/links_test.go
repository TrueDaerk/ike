package preview

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/theme"
)

const linkDoc = `# Alpha

See [the notes](notes.md) and [upstream](https://example.com/x) for more.

## Beta

Back to [Alpha](#alpha).
`

// newLinked returns a sized preview of linkDoc bound to a path inside dir.
func newLinked(dir string) Model {
	m := New("preview", filepath.Join(dir, "doc.md"), theme.DefaultPalette())
	m.SetSize(60, 12)
	m.SetSourceImmediate(linkDoc)
	return m
}

// TestScanLinksIndexesRenderedLinks guards the link index: every markdown
// link appears once, in reading order, with its raw destination — glamour
// renders each link twice (label plus printed URL) and the two halves must
// collapse into one selectable entry.
func TestScanLinksIndexesRenderedLinks(t *testing.T) {
	m := newLinked(t.TempDir())
	var got []string
	for _, l := range m.links {
		got = append(got, l.target)
	}
	want := []string{"notes.md", "https://example.com/x", "#alpha"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("links = %v, want %v", got, want)
	}
	for i := 1; i < len(m.links); i++ {
		if m.links[i].row < m.links[i-1].row {
			t.Fatalf("links must be in reading order, got rows %d then %d", m.links[i-1].row, m.links[i].row)
		}
	}
}

// TestTabSelectsLinksAndWraps guards the selection mechanism: tab walks the
// links forward and wraps, shift+tab walks back, and the selected label is
// marked in the rendered view.
func TestTabSelectsLinksAndWraps(t *testing.T) {
	m := newLinked(t.TempDir())
	if _, ok := m.SelectedTarget(); ok {
		t.Fatal("nothing should be selected before the first tab")
	}
	for _, want := range []string{"notes.md", "https://example.com/x", "#alpha", "notes.md"} {
		m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		if got, ok := m.SelectedTarget(); !ok || got != want {
			t.Fatalf("selected = %q (%v), want %q", got, ok, want)
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if got, _ := m.SelectedTarget(); got != "#alpha" {
		t.Fatalf("shift+tab should step back, got %q", got)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if got, _ := m.SelectedTarget(); got != "https://example.com/x" {
		t.Fatalf("shift+tab should step back again, got %q", got)
	}
}

// TestSelectedLinkIsHighlighted guards the visual feedback: the selected
// label is wrapped in reverse video, and only while it is selected.
func TestSelectedLinkIsHighlighted(t *testing.T) {
	m := newLinked(t.TempDir())
	if strings.Contains(m.View(), "\x1b[7m") {
		t.Fatal("no link is selected, nothing may be highlighted")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	v := m.View()
	if !strings.Contains(v, "\x1b[7m") || !strings.Contains(v, "\x1b[27m") {
		t.Fatalf("the selected link should be marked, got:\n%q", v)
	}
	// The marker must not destroy the document: the label is still readable.
	if !strings.Contains(v, "the notes") {
		t.Fatalf("highlighting must keep the label intact, got:\n%q", v)
	}
}

// TestEnterEmitsLinkMsg guards the follow seam: enter on a selection yields
// the LinkMsg carrying the pane key, the previewed file and the raw target;
// y yields the same message flagged as a copy.
func TestEnterEmitsLinkMsg(t *testing.T) {
	dir := t.TempDir()
	m := newLinked(dir)
	if cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("enter with nothing selected must be inert")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a selected link must emit a command")
	}
	msg, ok := cmd().(LinkMsg)
	if !ok {
		t.Fatalf("enter should emit a LinkMsg, got %T", cmd())
	}
	if msg.Key != "preview" || msg.Path != filepath.Join(dir, "doc.md") || msg.Target != "notes.md" || msg.Copy {
		t.Fatalf("LinkMsg = %+v", msg)
	}
	cmd = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("y on a selected link must emit a command")
	}
	if msg, _ := cmd().(LinkMsg); !msg.Copy || msg.Target != "notes.md" {
		t.Fatalf("y should emit a copy LinkMsg, got %+v", msg)
	}
}

// TestScrollToAnchorFindsHeading guards in-document anchors: the slug of a
// heading scrolls the view to it, an unknown slug is refused.
func TestScrollToAnchorFindsHeading(t *testing.T) {
	m := New("preview", "doc.md", theme.DefaultPalette())
	m.SetSize(60, 8)
	var b strings.Builder
	b.WriteString("# Top\n\n")
	for i := 0; i < 40; i++ {
		b.WriteString("filler line\n")
	}
	b.WriteString("\n## Deep Section\n\nend\n")
	m.SetSourceImmediate(b.String())
	m.scrollTo(0)
	if !m.ScrollToAnchor("deep-section") {
		t.Fatal("the heading's slug should resolve")
	}
	if m.top == 0 {
		t.Fatal("an anchor near the end should scroll the view")
	}
	if m.ScrollToAnchor("no-such-heading") {
		t.Fatal("an unknown slug must be refused so the caller can toast")
	}
}

// TestSlugMatchesGitHubForm guards the anchor slugs links are written
// against: lower-cased, punctuation dropped, spaces hyphenated.
func TestSlugMatchesGitHubForm(t *testing.T) {
	cases := map[string]string{
		"Hello World":        "hello-world",
		"Live updates (v2)":  "live-updates-v2",
		"`code` & symbols":   "code--symbols",
		"Markdown Preview":   "markdown-preview",
		"  Padded Heading  ": "padded-heading",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestHeadingLineSkipsFencedCode guards the anchor lookup used when a link
// carries a fragment: a "# heading" inside a code fence is not a heading.
func TestHeadingLineSkipsFencedCode(t *testing.T) {
	src := "# Real\n\n```\n# Fake\n```\n\n## Second\n"
	if line, ok := HeadingLine(src, "real"); !ok || line != 0 {
		t.Fatalf("HeadingLine(real) = %d,%v want 0,true", line, ok)
	}
	if _, ok := HeadingLine(src, "fake"); ok {
		t.Fatal("a fenced heading must not be an anchor")
	}
	if line, ok := HeadingLine(src, "second"); !ok || line != 6 {
		t.Fatalf("HeadingLine(second) = %d,%v want 6,true", line, ok)
	}
}

// TestRemoteClassifiesDestinations guards the network boundary: anything with
// a scheme is remote and never opened for its bytes.
func TestRemoteClassifiesDestinations(t *testing.T) {
	for _, s := range []string{"https://example.com", "http://x/y.png", "mailto:a@b.c", "ftp://h/f"} {
		if !Remote(s) {
			t.Errorf("Remote(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"notes.md", "./a/b.png", "../up.md", "/abs/path.md", "#anchor"} {
		if Remote(s) {
			t.Errorf("Remote(%q) = true, want false", s)
		}
	}
}
