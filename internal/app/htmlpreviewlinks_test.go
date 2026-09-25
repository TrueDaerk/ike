package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/htmlpreview"
)

// htmlpreviewlinks_test.go covers following HTML preview links and the
// reverse cursor sync (#2741), mirroring previewlinks_test.go.

// openHTMLPreviewIn writes page.html (plus extra files beside it) into a
// fresh temp dir, opens it with its preview and returns the model, the
// preview's pane key and the page path.
func openHTMLPreviewIn(t *testing.T, doc string, extra map[string]string) (Model, string, string) {
	t.Helper()
	m, path := openHTMLFile(t, doc)
	for name, body := range extra {
		p := filepath.Join(filepath.Dir(path), name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m = stepHTML(m, HTMLPreviewMsg{})
	key := htmlPreviewKeyFor(m, path)
	if key == "" {
		t.Fatal("the HTML preview should have opened")
	}
	return m, key, path
}

// TestHTMLLinkPathClassifies guards the resolver's split between local and
// remote destinations.
func TestHTMLLinkPathClassifies(t *testing.T) {
	for _, tc := range []struct {
		target, path, frag string
		local              bool
	}{
		{"notes.html", "notes.html", "", true},
		{"sub/page.html#install", "sub/page.html", "install", true},
		{"my%20notes.html?x=1#a", "my notes.html", "a", true},
		{"file:///tmp/x.html#top", "/tmp/x.html", "top", true},
		{"https://example.com/x", "", "", false},
		{"mailto:someone@example.com", "", "", false},
	} {
		path, frag, local := htmlLinkPath(tc.target)
		if path != tc.path || frag != tc.frag || local != tc.local {
			t.Errorf("htmlLinkPath(%q) = %q, %q, %v; want %q, %q, %v",
				tc.target, path, frag, local, tc.path, tc.frag, tc.local)
		}
	}
	if got := htmlPreviewBase("/d/page.html.gz!page.html"); got != "/d/page.html.gz" {
		t.Errorf("a gz buffer resolves beside its archive, got %q", got)
	}
}

// TestHTMLPreviewRelativeHTMLLinkOpensWithPreview: a relative .html link opens
// the page in an editor with a preview of its own, landing on the fragment —
// its tab's Preview view (#2766), since HTML files open rendered.
func TestHTMLPreviewRelativeHTMLLinkOpensWithPreview(t *testing.T) {
	var other strings.Builder
	other.WriteString("<p>top</p>\n")
	for range 60 {
		other.WriteString("<p>filler</p>\n")
	}
	other.WriteString("<h2 id=\"install\">Install Here</h2>\n")
	m, key, path := openHTMLPreviewIn(t, `<p><a href="sub/other.html#install">other</a></p>`,
		map[string]string{"sub/other.html": other.String()})
	target := filepath.Join(filepath.Dir(path), "sub", "other.html")

	m = stepHTML(m, htmlpreview.LinkMsg{Key: key, Path: path, Target: "sub/other.html#install"})
	if m.editorForPath(target) == nil {
		t.Fatal("following a relative HTML link must open the page in an editor")
	}
	pv := m.tabViewForPath(target)
	if pv == nil {
		t.Fatal("the followed page should open in its tab's Preview view")
	}
	if pk := htmlPreviewKeyFor(m, target); pk != "" {
		t.Fatalf("the tab's own preview is the rendering: no split pane (%s)", pk)
	}
	if v := ansi.Strip(pv.View()); !strings.Contains(v, "## Install") {
		t.Fatalf("the new preview should land on the #install anchor:\n%s", v)
	}
}

// TestHTMLPreviewRelativeHTMLLinkSourceModeSplits: with
// preview.html_open_mode = source the followed page opens in Source view and
// gets a split preview beside it, as before #2766.
func TestHTMLPreviewRelativeHTMLLinkSourceModeSplits(t *testing.T) {
	m, key, path := openHTMLPreviewIn(t, `<p><a href="other.html">other</a></p>`,
		map[string]string{"other.html": "<p>other page</p>\n"})
	withHTMLOpenMode(t, "source")
	target := filepath.Join(filepath.Dir(path), "other.html")
	m = stepHTML(m, htmlpreview.LinkMsg{Key: key, Path: path, Target: "other.html"})
	if m.tabViewForPath(target) != nil {
		t.Fatal("source mode must open the page in Source view")
	}
	if htmlPreviewKeyFor(m, target) == "" {
		t.Fatal("the followed page should get a split preview")
	}
}

// TestHTMLPreviewRelativeOtherLinkOpensEditor: a non-HTML target opens by its
// normal opener, without a preview.
func TestHTMLPreviewRelativeOtherLinkOpensEditor(t *testing.T) {
	m, key, path := openHTMLPreviewIn(t, `<p><a href="notes.txt">notes</a></p>`,
		map[string]string{"notes.txt": "hello\n"})
	target := filepath.Join(filepath.Dir(path), "notes.txt")
	m = step(m, htmlpreview.LinkMsg{Key: key, Path: path, Target: "notes.txt"})
	if m.editorForPath(target) == nil {
		t.Fatal("a relative text link must open the file in an editor")
	}
	if htmlPreviewKeyFor(m, target) != "" {
		t.Fatal("a non-HTML target must not get an HTML preview")
	}
}

// TestHTMLPreviewLinkKeysFollowSelection guards the keyboard path: with the
// preview focused, tab (claimed from the focus cycle) selects the first link
// and enter follows it.
func TestHTMLPreviewLinkKeysFollowSelection(t *testing.T) {
	m, key, path := openHTMLPreviewIn(t, `<p>See <a href="notes.txt">the notes</a>.</p>`,
		map[string]string{"notes.txt": "hello\n"})
	m.setFocus(key)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.activeWS().Panes.Focused() != key {
		t.Fatal("tab on a preview with links must not cycle the focus away")
	}
	if got, ok := m.activeWS().Panes.Get(key).HTMLPreview().SelectedTarget(); !ok || got != "notes.txt" {
		t.Fatalf("tab should select the first link, got %q (%v)", got, ok)
	}
	if !strings.Contains(ansi.Strip(m.render()), "→ notes.txt") {
		t.Fatal("the status line should name the selected link")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.editorForPath(filepath.Join(filepath.Dir(path), "notes.txt")) == nil {
		t.Fatal("enter on the selected link must open it")
	}
}

// TestHTMLPreviewTabWithoutLinksCyclesFocus: a link-free page keeps tab's
// global focus-cycling meaning.
func TestHTMLPreviewTabWithoutLinksCyclesFocus(t *testing.T) {
	m, key, _ := openHTMLPreviewIn(t, `<p>no links here</p>`, nil)
	m.setFocus(key)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.activeWS().Panes.Focused() == key {
		t.Fatal("tab on a link-free preview must cycle the focus")
	}
}

// TestHTMLPreviewAnchorLinkScrolls: "#anchor" scrolls the preview itself; an
// unknown one toasts.
func TestHTMLPreviewAnchorLinkScrolls(t *testing.T) {
	var b strings.Builder
	b.WriteString("<p><a href=\"#end\">down</a></p>\n")
	for range 80 {
		b.WriteString("<p>filler</p>\n")
	}
	b.WriteString("<h2 id=\"end\">The End</h2>\n")
	m, key, path := openHTMLPreviewIn(t, b.String(), nil)

	m = step(m, htmlpreview.LinkMsg{Key: key, Path: path, Target: "#end"})
	if v := ansi.Strip(m.activeWS().Panes.Get(key).View()); !strings.Contains(v, "The End") {
		t.Fatalf("the anchor should scroll the preview to its element:\n%s", v)
	}
	if len(m.toasts) != 0 {
		t.Fatalf("a resolvable anchor must not toast, got %+v", m.toasts)
	}
	m = step(m, htmlpreview.LinkMsg{Key: key, Path: path, Target: "#nowhere"})
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "no anchor for") {
		t.Fatalf("an unknown anchor must toast, got %+v", m.toasts)
	}
}

// TestHTMLPreviewExternalLinkUsesBrowser: http(s) and mailto reach the
// open-in-browser opener, only on the explicit action.
func TestHTMLPreviewExternalLinkUsesBrowser(t *testing.T) {
	var opened []string
	orig := browserOpen
	browserOpen = func(u string) error { opened = append(opened, u); return nil }
	defer func() { browserOpen = orig }()

	m, key, path := openHTMLPreviewIn(t, `<p><a href="https://example.com/x">up</a></p>`, nil)
	if len(opened) != 0 {
		t.Fatalf("rendering must not open anything, got %q", opened)
	}
	for _, u := range []string{"https://example.com/x", "mailto:a@example.com"} {
		m = step(m, htmlpreview.LinkMsg{Key: key, Path: path, Target: u})
	}
	if strings.Join(opened, " ") != "https://example.com/x mailto:a@example.com" {
		t.Fatalf("opened = %q, want both external links", opened)
	}
}

// TestHTMLPreviewMissingTargetToasts: a link to an absent file says so.
func TestHTMLPreviewMissingTargetToasts(t *testing.T) {
	m, key, path := openHTMLPreviewIn(t, `<p><a href="gone.html">gone</a></p>`, nil)
	m = step(m, htmlpreview.LinkMsg{Key: key, Path: path, Target: "gone.html"})
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "link target not found") {
		t.Fatalf("a missing target must toast, got %+v", m.toasts)
	}
}

// TestHTMLPreviewLinkCopy: y's copy puts the raw destination on the clipboard.
func TestHTMLPreviewLinkCopy(t *testing.T) {
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(text string) { copied = text }
	defer func() { clipboardWrite = orig }()
	m, key, path := openHTMLPreviewIn(t, `<p><a href="x.html">x</a></p>`, nil)
	step(m, htmlpreview.LinkMsg{Key: key, Path: path, Target: "x.html", Copy: true})
	if copied != "x.html" {
		t.Fatalf("copied = %q", copied)
	}
}

// TestHTMLPreviewClickFollowsLink: a left click on a link label follows it; a
// click on plain text only focuses the pane.
func TestHTMLPreviewClickFollowsLink(t *testing.T) {
	var opened string
	orig := browserOpen
	browserOpen = func(u string) error { opened = u; return nil }
	defer func() { browserOpen = orig }()

	m, key, path := openHTMLPreviewIn(t, `<p>plain words then <a href="https://example.com/c">click me</a></p>`, nil)
	r := m.lay.Panes[key]
	x0, y0 := r.X+paneContentX, r.Y+m.contentYOff(key)
	line := ansi.Strip(m.activeWS().Panes.Get(key).HTMLPreview().Lines()[0])
	col := strings.Index(line, "click me")
	if col < 0 {
		t.Fatalf("no link on the first line: %q", line)
	}

	m = step(m, tea.MouseClickMsg{X: x0 + 1, Y: y0, Button: tea.MouseLeft})
	if opened != "" {
		t.Fatalf("a click on plain text must not follow anything, got %q", opened)
	}
	if m.activeWS().Panes.Focused() != key {
		t.Fatal("the click should focus the preview")
	}
	if ed := m.editorForPath(path); ed != nil {
		if line, _ := ed.CursorPos(); line != 0 {
			t.Fatalf("a plain click must not move the editor caret, got line %d", line)
		}
	}

	tm, cmd := m.Update(tea.MouseClickMsg{X: x0 + col + 2, Y: y0, Button: tea.MouseLeft})
	m = drainCmd(tm.(Model), cmd)
	if opened != "https://example.com/c" {
		t.Fatalf("a click on the link must follow it, opened %q", opened)
	}
}

// TestHTMLPreviewReverseSync: enter with no selected link moves the editor
// caret to the source line of the rendered line at the sync row, and focuses
// the editor.
func TestHTMLPreviewReverseSync(t *testing.T) {
	m, key, path := openHTMLPreviewIn(t, paragraphs(120), nil)
	editorKey := m.editorKeyForPath(path)
	pv := m.activeWS().Panes.Get(key).HTMLPreview()
	pv.ScrollBy(50)
	m.setFocus(key)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	ed := m.editorForPath(path)
	line, _ := ed.CursorPos()
	// The sync row is a third down the viewport; each paragraph's source
	// line holds the text it renders.
	rows := strings.Split(ansi.Strip(m.activeWS().Panes.Get(key).View()), "\n")
	syncText := strings.TrimSpace(rows[len(rows)/3])
	if got := readLine(t, path, line); syncText == "" || !strings.Contains(got, syncText) {
		t.Fatalf("caret on source line %d (%q), want the line of the sync row %q", line, got, syncText)
	}
	if m.activeWS().Panes.Focused() != editorKey {
		t.Fatalf("the reverse sync should focus the editor, focus is %q", m.activeWS().Panes.Focused())
	}
}

// readLine returns 0-based source line n of path.
func readLine(t *testing.T, path string, n int) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	if n < 0 || n >= len(lines) {
		t.Fatalf("line %d out of range", n)
	}
	return lines[n]
}
