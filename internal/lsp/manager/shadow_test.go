package manager

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	langreg "ike/internal/lang"
	"ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/protocol"
)

// Shadow-document tests (#2330): a host language with EmbeddedShadow merges
// all regions of one embedded language into a single whole-buffer virtual
// document with non-region text blanked, so positions map 1:1.

func init() {
	langreg.Register(langreg.Language{ID: "shadowhost", EmbeddedShadow: true})
	langreg.Register(langreg.Language{ID: "shadowjs", Extensions: []string{"sjs"}})
	langreg.Register(langreg.Language{ID: "shadowcss", Extensions: []string{"scss3"}})
}

// shadowHostDetector marks every "js>"/"cs>" line's remainder as an embedded
// region — a deterministic stand-in for the HTML injection query.
func shadowHostDetector(lang string, lines []string) []highlight.Fragment {
	var out []highlight.Fragment
	for i, l := range lines {
		var id string
		switch {
		case strings.HasPrefix(l, "js>"):
			id = "shadowjs"
		case strings.HasPrefix(l, "cs>"):
			id = "shadowcss"
		default:
			continue
		}
		out = append(out, highlight.Fragment{
			Lang:      id,
			StartLine: i, StartCol: 3,
			EndLine: i, EndCol: len(l),
			Lines: []string{l[3:]},
		})
	}
	return out
}

func shadowSpecs() []lsp.ServerSpec {
	return []lsp.ServerSpec{
		{Language: "shadowhost", Command: "fake-host"},
		{Language: "shadowjs", Command: "fake-js", FragmentScheme: "untitled"},
		{Language: "shadowcss", Command: "fake-css"},
	}
}

func TestShadowLinesBlanking(t *testing.T) {
	lines := []string{
		"<h>",
		"js>let x = 1",
		"tail",
	}
	got := shadowLines(lines, []highlight.Fragment{
		{StartLine: 1, StartCol: 3, EndLine: 1, EndCol: 12},
	})
	want := []string{
		"   ",
		"   let x = 1",
		"    ",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}

	// A multi-line region keeps its middle lines whole, clamps past-the-end
	// coordinates, and preserves rune counts on non-ASCII text.
	lines = []string{"aä<", "böd y", "x>é"}
	got = shadowLines(lines, []highlight.Fragment{
		{StartLine: 0, StartCol: 2, EndLine: 2, EndCol: 1},
		{StartLine: 5, StartCol: 0, EndLine: 9, EndCol: 9}, // outside: ignored
	})
	want = []string{"  <", "böd y", "x  "}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("multi-line %d = %q, want %q", i, got[i], want[i])
		}
		if len([]rune(got[i])) != len([]rune(lines[i])) {
			t.Errorf("line %d rune count changed: %q vs %q", i, got[i], lines[i])
		}
	}
}

func TestShadowDetectedMergesPerLanguage(t *testing.T) {
	lines := []string{
		"<h>",
		"js>let x = 1",
		"cs>b { }",
		"js>x.f()",
	}
	dets := shadowDetected(lines, plainDetected(shadowHostDetector("shadowhost", lines)))
	if len(dets) != 2 {
		t.Fatalf("dets = %d, want 2 (one per language): %+v", len(dets), dets)
	}
	// Slots follow sorted language ids: shadowcss before shadowjs.
	if dets[0].frag.Lang != "shadowcss" || dets[1].frag.Lang != "shadowjs" {
		t.Fatalf("langs = %q/%q, want shadowcss/shadowjs", dets[0].frag.Lang, dets[1].frag.Lang)
	}
	js := dets[1]
	if js.frag.StartLine != 0 || js.frag.StartCol != 0 || js.frag.EndLine != 3 || js.frag.EndCol != len("js>x.f()") {
		t.Errorf("shadow frag range = %+v, want whole buffer", js.frag)
	}
	if len(js.regions) != 2 {
		t.Fatalf("js regions = %d, want 2", len(js.regions))
	}
	wantJS := []string{"   ", "   let x = 1", "        ", "   x.f()"}
	for i, w := range wantJS {
		if js.frag.Lines[i] != w {
			t.Errorf("js shadow line %d = %q, want %q", i, js.frag.Lines[i], w)
		}
	}
}

func TestSchemeFragmentURI(t *testing.T) {
	uri := schemeFragmentURI("untitled", "/tmp/my dir/page.html", 1, "ts")
	if !strings.HasPrefix(uri, "untitled:/ike-embedded/") {
		t.Errorf("uri = %q, want untitled:/ike-embedded/ prefix", uri)
	}
	if !strings.HasSuffix(uri, "/1/page.ts") {
		t.Errorf("uri = %q, want /1/page.ts suffix", uri)
	}
	if strings.ContainsAny(uri, " %") {
		t.Errorf("uri = %q must not need percent-encoding", uri)
	}
	if !isFragmentURI(uri) {
		t.Errorf("isFragmentURI(%q) = false", uri)
	}
	if !isFragmentURI(fragmentURI("/tmp/a.py", 0)) {
		t.Error("default-scheme fragment URI not recognized")
	}
	if isFragmentURI("file:///tmp/a.py") || isFragmentURI("untitled:Untitled-1") {
		t.Error("non-fragment URIs must not be recognized")
	}
	// Distinct hosts must never collide, even when their base sanitizes alike.
	other := schemeFragmentURI("untitled", "/tmp/my_dir/page.html", 1, "ts")
	if other == uri {
		t.Errorf("URIs for distinct hosts collide: %q", uri)
	}
	if got := fragmentExt("shadowjs"); got != "sjs" {
		t.Errorf("fragmentExt(shadowjs) = %q, want sjs", got)
	}
	if got := fragmentExt("no-such-lang"); got != "no-such-lang" {
		t.Errorf("fragmentExt fallback = %q, want the id", got)
	}
}

func TestShadowFragmentOpensBlankedDocs(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	m := New(multiResolver(shadowSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens}), Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(shadowHostDetector)

	path := filepath.Join(t.TempDir(), "page.shost")
	text := "<h>\njs>let x = 1\ncs>b { }\njs>x.f()"
	if err := m.Open(path, "shadowhost", text); err != nil {
		t.Fatal(err)
	}
	var js, css protocol.DidOpenTextDocumentParams
	for js.TextDocument.URI == "" || css.TextDocument.URI == "" {
		p := waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
			return isFragmentURI(p.TextDocument.URI)
		})
		switch p.TextDocument.LanguageID {
		case "shadowjs":
			js = p
		case "shadowcss":
			css = p
		}
	}
	// vtsls-style spec: untitled scheme, the fragment language's extension.
	if !strings.HasPrefix(js.TextDocument.URI, "untitled:/ike-embedded/") || !strings.HasSuffix(js.TextDocument.URI, "/1/page.sjs") {
		t.Errorf("js URI = %q, want untitled scheme with slot 1 and .sjs", js.TextDocument.URI)
	}
	// Scheme-less spec keeps the classic ike-fragment URI.
	if css.TextDocument.URI != fragmentURI(path, 0) {
		t.Errorf("css URI = %q, want %q", css.TextDocument.URI, fragmentURI(path, 0))
	}
	if want := "   \n   let x = 1\n        \n   x.f()"; js.TextDocument.Text != want {
		t.Errorf("js shadow text = %q, want %q", js.TextDocument.Text, want)
	}
	if want := "   \n            \n   b { }\n        "; css.TextDocument.Text != want {
		t.Errorf("css shadow text = %q, want %q", css.TextDocument.Text, want)
	}
}

// TestShadowRoutingIdentityMapping: requests inside a region hit the shadow
// server with untranslated (identity) positions; the blanked filler and the
// host's own text stay with the host server.
func TestShadowRoutingIdentityMapping(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	m := New(multiResolver(shadowSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens}), Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(shadowHostDetector)

	path := filepath.Join(t.TempDir(), "page.shost")
	if err := m.Open(path, "shadowhost", "<h>\njs>let x = 1\njs>x.f()"); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return isFragmentURI(p.TextDocument.URI)
	})

	// Outside every region: host markup and the js> marker itself.
	if _, _, ok := m.fragmentAt(path, buffer.Position{Line: 0, Col: 1}); ok {
		t.Error("host markup must not route into the shadow doc")
	}
	if _, _, ok := m.fragmentAt(path, buffer.Position{Line: 1, Col: 2}); ok {
		t.Error("the region marker must not route into the shadow doc")
	}
	// Inside the second region — and on the second script line, so the
	// identity mapping (not a per-fragment offset shift) is observable.
	srv, fd, ok := m.fragmentAt(path, buffer.Position{Line: 2, Col: 5})
	if !ok || srv == nil || fd.lang != "shadowjs" {
		t.Fatalf("fragmentAt = (%v, %+v, %v), want the shadowjs doc", srv, fd, ok)
	}
	h, err := m.Hover(context.Background(), path, buffer.Position{Line: 2, Col: 5})
	if err != nil || h == nil {
		t.Fatalf("hover = %+v err = %v", h, err)
	}
	if got := string(h.Contents); !strings.Contains(got, "hover@2:5") {
		t.Errorf("contents = %s, want identity position 2:5 at the shadow server", got)
	}
	// The fake's range 0:0-0:6 maps back without an offset shift; its end
	// clamps to the 3-rune first line on the way through editor coordinates.
	if h.Range == nil || h.Range.Start.Line != 0 || h.Range.Start.Character != 0 || h.Range.End.Character != 3 {
		t.Errorf("mapped hover range = %+v, want identity 0:0 start, clamped 0:3 end", h.Range)
	}
}

// TestShadowEditKeepsSync: a host edit above the regions re-blanks the shadow
// document and shifts the routable regions with it — no stale positions.
func TestShadowEditKeepsSync(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	changes := make(chan protocol.DidChangeTextDocumentParams, 8)
	m := New(multiResolver(shadowSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens, didChanges: changes}), Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(shadowHostDetector)

	path := filepath.Join(t.TempDir(), "page.shost")
	if err := m.Open(path, "shadowhost", "<h>\njs>let x = 1"); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return isFragmentURI(p.TextDocument.URI)
	})

	if err := m.Change(path, "<h>\n<p>\njs>let x = 1"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case p := <-changes:
			if !isFragmentURI(p.TextDocument.URI) {
				continue // the host document's own didChange
			}
			if want := "   \n   \n   let x = 1"; p.ContentChanges[0].Text != want {
				t.Fatalf("shadow change text = %q, want %q", p.ContentChanges[0].Text, want)
			}
			if p.TextDocument.Version != 2 {
				t.Fatalf("shadow version = %d, want 2", p.TextDocument.Version)
			}
		case <-deadline:
			t.Fatal("shadow didChange never arrived")
		}
		break
	}
	// The region moved from line 1 to line 2; routing follows.
	waitFor(t, func() bool {
		_, _, ok := m.fragmentAt(path, buffer.Position{Line: 2, Col: 5})
		return ok
	})
	if _, _, ok := m.fragmentAt(path, buffer.Position{Line: 1, Col: 5}); ok {
		t.Error("the old region line must no longer route")
	}
}

func TestShadowSignatureHelpRoutes(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	m := New(multiResolver(shadowSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens}), Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(shadowHostDetector)

	path := filepath.Join(t.TempDir(), "page.shost")
	if err := m.Open(path, "shadowhost", "<h>\njs>f(1"); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return isFragmentURI(p.TextDocument.URI)
	})

	sh, handled, err := m.fragmentSignatureHelp(context.Background(), path, buffer.Position{Line: 1, Col: 5})
	if err != nil || !handled {
		t.Fatalf("fragmentSignatureHelp handled = %v err = %v", handled, err)
	}
	if sh == nil || len(sh.Signatures) != 1 || sh.Signatures[0].Label != "Greet(name string)" {
		t.Errorf("signature = %+v, want the fake's answer", sh)
	}
	if _, handled, _ := m.fragmentSignatureHelp(context.Background(), path, buffer.Position{Line: 0, Col: 0}); handled {
		t.Error("a host position must not be handled by the shadow doc")
	}
	// The public entry routes the same way.
	sh, err = m.SignatureHelp(context.Background(), path, buffer.Position{Line: 1, Col: 5})
	if err != nil || sh == nil || len(sh.Signatures) != 1 {
		t.Errorf("SignatureHelp via manager = %+v err = %v", sh, err)
	}
}

// TestCompletionTriggersAtPerRegion: inside a region the embedded server's
// trigger characters apply; outside, the host server's.
func TestCompletionTriggersAtPerRegion(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	connect := func(spec lsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), func() string, error) {
		opts := fakeOpts{syncKind: protocol.SyncFull, didOpens: opens, completionTriggers: []string{"<", ":"}}
		if spec.Language == "shadowjs" {
			opts.completionTriggers = []string{".", "\""}
		}
		return fakeConnectorOpts(opts)(spec, root, handler)
	}
	m := New(multiResolver(shadowSpecs()...), connect, Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(shadowHostDetector)

	path := filepath.Join(t.TempDir(), "page.shost")
	if err := m.Open(path, "shadowhost", "<h>\njs>x."); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return isFragmentURI(p.TextDocument.URI)
	})

	got := m.CompletionTriggersAt(path, buffer.Position{Line: 1, Col: 5})
	if len(got) != 2 || got[0] != "." {
		t.Errorf("in-region triggers = %v, want the embedded server's [. \"]", got)
	}
	got = m.CompletionTriggersAt(path, buffer.Position{Line: 0, Col: 1})
	if len(got) != 2 || got[0] != "<" {
		t.Errorf("host triggers = %v, want the host server's [< :]", got)
	}
}

// TestShadowDiagnosticsMergeIdentity: a diagnostic published on the shadow
// document reaches the host merged publish at its identity position.
func TestShadowDiagnosticsMergeIdentity(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	diags := make(chan protocol.PublishDiagnosticsParams, 16)
	cb := Callbacks{Diagnostics: func(path string, p protocol.PublishDiagnosticsParams, lines []string, enc string) {
		diags <- p
	}}
	m := New(multiResolver(shadowSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens}), cb)
	defer m.Shutdown()
	m.SetFragmentDetector(shadowHostDetector)

	path := filepath.Join(t.TempDir(), "page.shost")
	if err := m.Open(path, "shadowhost", "<h>\njs>let x = 1"); err != nil {
		t.Fatal(err)
	}

	// The fake pushes one diagnostic (0:0-0:3, "boom") per didOpen: the host
	// doc's and the shadow doc's merge into one publish for the host path.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case p := <-diags:
			if len(p.Diagnostics) < 2 {
				continue // only one source has published so far
			}
			for _, d := range p.Diagnostics {
				if d.Range.Start.Line != 0 || d.Range.Start.Character != 0 || d.Range.End.Character != 3 {
					t.Fatalf("merged diagnostic range = %+v, want identity 0:0-0:3", d.Range)
				}
			}
			return
		case <-deadline:
			t.Fatal("merged host+shadow diagnostics never arrived")
		}
	}
}
