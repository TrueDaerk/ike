package manager

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// lineDetector is a deterministic fake detector: every line starting with
// "sql>" is an sql fragment covering the rest of that line.
func lineDetector(lang string, lines []string) []highlight.Fragment {
	var out []highlight.Fragment
	for i, l := range lines {
		if strings.HasPrefix(l, "sql>") {
			out = append(out, highlight.Fragment{
				Lang:      "sql",
				StartLine: i, StartCol: 4,
				EndLine: i, EndCol: len(l),
				Lines: []string{l[4:]},
			})
		}
	}
	return out
}

func multiResolver(specs ...lsp.ServerSpec) func(string) (lsp.ServerSpec, bool) {
	return func(lang string) (lsp.ServerSpec, bool) {
		for _, s := range specs {
			if s.Language == lang {
				return s, true
			}
		}
		return lsp.ServerSpec{}, false
	}
}

func fragmentSpecs() []lsp.ServerSpec {
	return []lsp.ServerSpec{
		{Language: "python", Command: "fake-py"},
		{Language: "sql", Command: "fake-sql"},
	}
}

// waitOpen drains didOpens until a URI matching want arrives.
func waitOpen(t *testing.T, ch chan protocol.DidOpenTextDocumentParams, match func(protocol.DidOpenTextDocumentParams) bool) protocol.DidOpenTextDocumentParams {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case p := <-ch:
			if match(p) {
				return p
			}
		case <-deadline:
			t.Fatal("expected didOpen never arrived")
		}
	}
}

func TestFragmentOpensVirtualDocument(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	m := New(multiResolver(fragmentSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens}), Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(lineDetector)

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "print(1)\nsql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	frag := waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return isFragmentURI(p.TextDocument.URI)
	})
	if frag.TextDocument.LanguageID != "sql" {
		t.Errorf("LanguageID = %q, want sql", frag.TextDocument.LanguageID)
	}
	if frag.TextDocument.Text != "SELECT 1" {
		t.Errorf("Text = %q, want SELECT 1", frag.TextDocument.Text)
	}
	if want := fragmentURI(path, 0); frag.TextDocument.URI != want {
		t.Errorf("URI = %q, want %q", frag.TextDocument.URI, want)
	}
}

func TestFragmentChangeMirrorsHostEdit(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	changes := make(chan protocol.DidChangeTextDocumentParams, 8)
	m := New(multiResolver(fragmentSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens, didChanges: changes}), Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(lineDetector)

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "sql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return isFragmentURI(p.TextDocument.URI)
	})

	if err := m.Change(path, "sql>SELECT 22"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case p := <-changes:
			if !isFragmentURI(p.TextDocument.URI) {
				continue // the host document's own didChange
			}
			if got := p.ContentChanges[0].Text; got != "SELECT 22" {
				t.Fatalf("fragment change text = %q, want SELECT 22", got)
			}
			if p.TextDocument.Version != 2 {
				t.Fatalf("fragment version = %d, want 2", p.TextDocument.Version)
			}
			return
		case <-deadline:
			t.Fatal("fragment didChange never arrived")
		}
	}
}

func TestFragmentRemovedSendsDidClose(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	closes := make(chan protocol.DidCloseTextDocumentParams, 8)
	m := New(multiResolver(fragmentSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens, didCloses: closes}), Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(lineDetector)

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "sql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return isFragmentURI(p.TextDocument.URI)
	})

	if err := m.Change(path, "print(1)"); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-closes:
		if want := fragmentURI(path, 0); p.TextDocument.URI != want {
			t.Fatalf("didClose URI = %q, want %q", p.TextDocument.URI, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fragment didClose never arrived")
	}
}

func TestFragmentAtRoutesByPosition(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	m := New(multiResolver(fragmentSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens}), Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(lineDetector)

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "print(1)\nsql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return isFragmentURI(p.TextDocument.URI)
	})

	if _, _, ok := m.fragmentAt(path, buffer.Position{Line: 0, Col: 0}); ok {
		t.Error("position outside fragment must not route")
	}
	if _, _, ok := m.fragmentAt(path, buffer.Position{Line: 1, Col: 3}); ok {
		t.Error("position before fragment start must not route")
	}
	srv, fd, ok := m.fragmentAt(path, buffer.Position{Line: 1, Col: 8})
	if !ok || srv == nil || fd.lang != "sql" {
		t.Fatalf("fragmentAt = (%v, %+v, %v), want sql fragment", srv, fd, ok)
	}
	// End boundary is inclusive so completion at the closing quote routes in.
	if _, _, ok := m.fragmentAt(path, buffer.Position{Line: 1, Col: len("sql>SELECT 1")}); !ok {
		t.Error("end boundary should be inclusive")
	}
}

func TestFragmentSkippedWithoutServer(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	// Only python resolves; sql fragments must degrade silently.
	m := New(multiResolver(lsp.ServerSpec{Language: "python", Command: "fake-py"}), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens}), Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(lineDetector)

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "sql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return !isFragmentURI(p.TextDocument.URI)
	})
	select {
	case p := <-opens:
		t.Fatalf("unexpected extra didOpen: %+v", p.TextDocument)
	case <-time.After(300 * time.Millisecond):
	}
	if _, _, ok := m.fragmentAt(path, buffer.Position{Line: 0, Col: 6}); ok {
		t.Error("fragment without a server must not route")
	}
}

func TestFragmentPositionMapping(t *testing.T) {
	fr := highlight.Fragment{Lang: "sql", StartLine: 2, StartCol: 5, EndLine: 4, EndCol: 3}

	// First line: column shifts by StartCol.
	if got := hostToFrag(fr, buffer.Position{Line: 2, Col: 9}); got != (buffer.Position{Line: 0, Col: 4}) {
		t.Errorf("hostToFrag first line = %+v", got)
	}
	// Later lines: only the line shifts.
	if got := hostToFrag(fr, buffer.Position{Line: 4, Col: 2}); got != (buffer.Position{Line: 2, Col: 2}) {
		t.Errorf("hostToFrag later line = %+v", got)
	}
	// Round-trip.
	for _, p := range []buffer.Position{{Line: 2, Col: 5}, {Line: 2, Col: 12}, {Line: 3, Col: 0}, {Line: 4, Col: 3}} {
		if got := fragToHost(fr, hostToFrag(fr, p)); got != p {
			t.Errorf("round-trip %+v = %+v", p, got)
		}
	}

	// Containment boundaries.
	cases := []struct {
		pos buffer.Position
		in  bool
	}{
		{buffer.Position{Line: 1, Col: 9}, false},
		{buffer.Position{Line: 2, Col: 4}, false},
		{buffer.Position{Line: 2, Col: 5}, true},
		{buffer.Position{Line: 3, Col: 99}, true},
		{buffer.Position{Line: 4, Col: 3}, true},
		{buffer.Position{Line: 4, Col: 4}, false},
		{buffer.Position{Line: 5, Col: 0}, false},
	}
	for _, c := range cases {
		if got := fragContains(fr, c.pos); got != c.in {
			t.Errorf("fragContains(%+v) = %v, want %v", c.pos, got, c.in)
		}
	}
}

func TestFragmentCompletionRoutesAndMapsRanges(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	m := New(multiResolver(fragmentSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens}), Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(lineDetector)

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "sql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return isFragmentURI(p.TextDocument.URI)
	})

	items, err := m.Completion(context.Background(), path, buffer.Position{Line: 0, Col: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].TextEdit == nil {
		t.Fatalf("items = %+v", items)
	}
	// The fake's fragment-relative edit range 0:0-0:3 maps to host 0:4-0:7
	// (the fragment starts at host column 4).
	r := items[0].TextEdit.Range
	if r.Start.Character != 4 || r.End.Character != 7 || r.Start.Line != 0 {
		t.Errorf("mapped edit range = %+v, want 0:4-0:7", r)
	}
	// Outside the fragment the host server answers; its range stays put.
	items, err = m.Completion(context.Background(), path, buffer.Position{Line: 0, Col: 0})
	if err != nil {
		t.Fatal(err)
	}
	if r := items[0].TextEdit.Range; r.Start.Character != 0 || r.End.Character != 3 {
		t.Errorf("host edit range = %+v, want 0:0-0:3", r)
	}
}

func TestFragmentDefinitionRoutesAndMapsLocations(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	m := New(multiResolver(fragmentSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens}), Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(lineDetector)

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "sql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return isFragmentURI(p.TextDocument.URI)
	})

	locs, err := m.Definition(context.Background(), path, buffer.Position{Line: 0, Col: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(locs) != 1 {
		t.Fatalf("locs = %+v, want 1", locs)
	}
	// The fake answers with a location in the requested (fragment) doc; it
	// must come back as the host file, range 0:0-0:6 shifted to 0:4-0:10.
	if want := protocol.PathToURI(path); locs[0].URI != want {
		t.Errorf("URI = %q, want host %q", locs[0].URI, want)
	}
	if r := locs[0].Range; r.Start.Character != 4 || r.End.Character != 10 || r.Start.Line != 0 {
		t.Errorf("mapped range = %+v, want 0:4-0:10", r)
	}

	// Outside the fragment the host server answers; its location stays put.
	locs, err = m.Definition(context.Background(), path, buffer.Position{Line: 0, Col: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(locs) != 1 || locs[0].URI != protocol.PathToURI(path) || locs[0].Range.Start.Character != 0 {
		t.Errorf("host locs = %+v, want unmapped host location", locs)
	}
}

func TestFragmentReferencesRoutesAndMapsLocations(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	m := New(multiResolver(fragmentSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens}), Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(lineDetector)

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "sql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return isFragmentURI(p.TextDocument.URI)
	})

	locs, err := m.References(context.Background(), path, buffer.Position{Line: 0, Col: 10}, true)
	if err != nil {
		t.Fatal(err)
	}
	// The fake answers with a real-file location plus (includeDeclaration)
	// one in the requested fragment doc.
	if len(locs) != 2 {
		t.Fatalf("locs = %+v, want 2", locs)
	}
	if locs[0].URI != "file:///tmp/other.go" || locs[0].Range.Start.Line != 2 {
		t.Errorf("real-file location must pass through, got %+v", locs[0])
	}
	if want := protocol.PathToURI(path); locs[1].URI != want {
		t.Errorf("fragment location URI = %q, want host %q", locs[1].URI, want)
	}
	// Fragment 0:0 maps to host 0:4.
	if locs[1].Range.Start.Character != 4 || locs[1].Range.Start.Line != 0 {
		t.Errorf("fragment location range = %+v, want start 0:4", locs[1].Range)
	}
}

func TestFragLocationsToHostDropsStale(t *testing.T) {
	m := New(multiResolver(fragmentSpecs()...), fakeConnector(), Callbacks{})
	defer m.Shutdown()

	// A fragment URI that no tracked fragment document backs must be dropped,
	// never surfaced as an unopenable synthetic URI.
	locs := m.fragLocationsToHost([]protocol.Location{
		{URI: fragmentURI("/tmp/gone.py", 0)},
		{URI: "file:///tmp/keep.go"},
	}, protocol.EncodingUTF16)
	if len(locs) != 1 || locs[0].URI != "file:///tmp/keep.go" {
		t.Errorf("locs = %+v, want only the real-file location", locs)
	}
}

func TestFragmentHoverRoutesAndMapsRange(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	m := New(multiResolver(fragmentSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens}), Callbacks{})
	defer m.Shutdown()
	m.SetFragmentDetector(lineDetector)

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "sql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return isFragmentURI(p.TextDocument.URI)
	})

	h, err := m.Hover(context.Background(), path, buffer.Position{Line: 0, Col: 10})
	if err != nil || h == nil {
		t.Fatalf("hover = %+v err = %v", h, err)
	}
	// Host col 10 is fragment col 6: the request position must arrive
	// fragment-relative at the fragment server.
	if got := string(h.Contents); !strings.Contains(got, "hover@0:6") {
		t.Errorf("contents = %s, want request at fragment position 0:6", got)
	}
	// The fake's fragment range 0:0-0:6 maps back to host 0:4-0:10.
	if h.Range == nil || h.Range.Start.Character != 4 || h.Range.End.Character != 10 {
		t.Errorf("mapped hover range = %+v, want 0:4-0:10", h.Range)
	}
}
