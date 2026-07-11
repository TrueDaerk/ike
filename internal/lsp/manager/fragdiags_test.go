package manager

import (
	"path/filepath"
	"testing"
	"time"

	"ike/internal/lsp/protocol"
)

// diagCollector wires a Diagnostics callback into a channel keyed by path.
type diagPublish struct {
	path  string
	diags []protocol.Diagnostic
}

func diagCollector() (Callbacks, chan diagPublish) {
	ch := make(chan diagPublish, 16)
	cb := Callbacks{Diagnostics: func(path string, p protocol.PublishDiagnosticsParams, lines []string, enc string) {
		ch <- diagPublish{path: path, diags: p.Diagnostics}
	}}
	return cb, ch
}

// waitDiags drains publishes until one for path satisfies match.
func waitDiags(t *testing.T, ch chan diagPublish, path string, match func([]protocol.Diagnostic) bool) []protocol.Diagnostic {
	t.Helper()
	deadline := time.After(3 * time.Second)
	var last []protocol.Diagnostic
	for {
		select {
		case p := <-ch:
			if p.path != path {
				continue
			}
			last = p.diags
			if match(p.diags) {
				return p.diags
			}
		case <-deadline:
			t.Fatalf("expected diagnostics publish never arrived; last for %s: %+v", path, last)
		}
	}
}

// The fake server pushes one "boom" diagnostic (range 0:0-0:3) on every
// didOpen, for host and fragment documents alike. The fragment's diagnostic
// must come back on the host path, mapped into host coordinates and merged
// with the host server's own diagnostic.
func TestFragmentDiagnosticsMapAndMergeIntoHost(t *testing.T) {
	cb, diags := diagCollector()
	m := New(multiResolver(fragmentSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull}), cb)
	defer m.Shutdown()
	m.SetFragmentDetector(lineDetector)

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "print(1)\nsql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	got := waitDiags(t, diags, path, func(ds []protocol.Diagnostic) bool { return len(ds) == 2 })

	// Host diagnostic first (0:0-0:3, untouched), fragment diagnostic second:
	// fragment 0:0-0:3 shifts to host line 1, columns 4-7 (the fragment starts
	// at host column 4).
	if r := got[0].Range; r.Start.Line != 0 || r.Start.Character != 0 || r.End.Character != 3 {
		t.Errorf("host diagnostic range = %+v, want 0:0-0:3", r)
	}
	fr := got[1].Range
	if fr.Start.Line != 1 || fr.Start.Character != 4 || fr.End.Line != 1 || fr.End.Character != 7 {
		t.Errorf("mapped fragment diagnostic range = %+v, want 1:4-1:7", fr)
	}
	if got[1].Message != "boom" {
		t.Errorf("fragment diagnostic message = %q, want boom", got[1].Message)
	}
}

// A host edit that moves the fragment (without changing its content) must
// re-emit the merged diagnostics with the fragment diagnostic at its new
// host position — no fresh server publish needed.
func TestFragmentDiagnosticsFollowFragmentMove(t *testing.T) {
	cb, diags := diagCollector()
	m := New(multiResolver(fragmentSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull}), cb)
	defer m.Shutdown()
	m.SetFragmentDetector(lineDetector)

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "print(1)\nsql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	waitDiags(t, diags, path, func(ds []protocol.Diagnostic) bool { return len(ds) == 2 })

	// Insert a line above the fragment: same content, new position.
	if err := m.Change(path, "x = 1\nprint(1)\nsql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	got := waitDiags(t, diags, path, func(ds []protocol.Diagnostic) bool {
		return len(ds) == 2 && ds[1].Range.Start.Line == 2
	})
	if r := got[1].Range; r.Start.Character != 4 || r.End.Character != 7 {
		t.Errorf("moved fragment diagnostic range = %+v, want 2:4-2:7", r)
	}
}

// When the fragment disappears from the host text, its diagnostics must be
// cleared from the merged view even though the fragment server never
// publishes an empty set for the closed document.
func TestFragmentDiagnosticsClearOnFragmentClose(t *testing.T) {
	cb, diags := diagCollector()
	m := New(multiResolver(fragmentSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull}), cb)
	defer m.Shutdown()
	m.SetFragmentDetector(lineDetector)

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "print(1)\nsql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	waitDiags(t, diags, path, func(ds []protocol.Diagnostic) bool { return len(ds) == 2 })

	if err := m.Change(path, "print(1)"); err != nil {
		t.Fatal(err)
	}
	got := waitDiags(t, diags, path, func(ds []protocol.Diagnostic) bool { return len(ds) == 1 })
	if got[0].Range.Start.Line != 0 || got[0].Range.Start.Character != 0 {
		t.Errorf("remaining diagnostic = %+v, want the host server's own", got[0])
	}
}

// StopLang on the fragment language clears its diagnostics from every host's
// merged view; the host document itself stays open and keeps its own.
func TestFragmentDiagnosticsClearOnStopLang(t *testing.T) {
	cb, diags := diagCollector()
	m := New(multiResolver(fragmentSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull}), cb)
	defer m.Shutdown()
	m.SetFragmentDetector(lineDetector)

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "print(1)\nsql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	waitDiags(t, diags, path, func(ds []protocol.Diagnostic) bool { return len(ds) == 2 })

	m.StopLang("sql")
	got := waitDiags(t, diags, path, func(ds []protocol.Diagnostic) bool { return len(ds) == 1 })
	if got[0].Message != "boom" || got[0].Range.Start.Line != 0 {
		t.Errorf("remaining diagnostic = %+v, want the host server's own", got[0])
	}
}

// A publish for a fragment URI no tracked document backs (stale, host already
// closed) must be dropped without reaching the callback.
func TestFragmentDiagnosticsStaleURIDropped(t *testing.T) {
	cb, diags := diagCollector()
	m := New(multiResolver(fragmentSpecs()...), fakeConnector(), cb)
	defer m.Shutdown()

	m.onFragmentDiagnostics(protocol.PublishDiagnosticsParams{
		URI:         fragmentURI("/tmp/gone.py", 0),
		Diagnostics: []protocol.Diagnostic{{Message: "stale"}},
	})
	select {
	case p := <-diags:
		t.Fatalf("unexpected publish: %+v", p)
	case <-time.After(200 * time.Millisecond):
	}
}
