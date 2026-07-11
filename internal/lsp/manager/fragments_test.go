package manager

import (
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
