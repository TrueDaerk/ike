package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

func hintsMsg(path string, hints ...ilsp.InlayHint) ilsp.InlayHintsMsg {
	return ilsp.InlayHintsMsg{Path: path, Hints: hints}
}

func firstLine(m Model) string {
	return strings.TrimRight(strings.SplitN(ansi.Strip(m.View()), "\n", 2)[0], " ")
}

// TestInlayHintsRenderInline injects the hint text before its anchor cell,
// with the server-requested padding, without touching the buffer (#171).
func TestInlayHintsRenderInline(t *testing.T) {
	m, path := loaded(t, "add(1, 2)\n")
	m.Configure(host.MapConfig{"lsp.inlay_hints": "true"}) // default off (#523)
	m, _ = m.Update(hintsMsg(path,
		ilsp.InlayHint{Line: 0, Col: 4, Label: "x:", Kind: protocol.InlayHintParameter, PadRight: true},
		ilsp.InlayHint{Line: 0, Col: 7, Label: "y:", Kind: protocol.InlayHintParameter, PadRight: true},
	))
	if got, want := firstLine(m), "add(x: 1, y: 2)"; got != want {
		t.Errorf("rendered = %q, want %q", got, want)
	}
	if m.buf.Line(0) != "add(1, 2)" {
		t.Error("hints are virtual text and must not change the buffer")
	}

	// An empty reply clears the hints.
	m, _ = m.Update(hintsMsg(path))
	if got := firstLine(m); got != "add(1, 2)" {
		t.Errorf("after clear rendered = %q", got)
	}
}

// TestInlayHintsAtLineEnd flushes hints anchored at (or past) the end of the
// line's text, where the render loop otherwise stops.
func TestInlayHintsAtLineEnd(t *testing.T) {
	m, path := loaded(t, "x := foo()\n")
	m.Configure(host.MapConfig{"lsp.inlay_hints": "true"}) // default off (#523)
	m, _ = m.Update(hintsMsg(path, ilsp.InlayHint{Line: 0, Col: 10, Label: "int", Kind: protocol.InlayHintType, PadLeft: true}))
	if got, want := firstLine(m), "x := foo() int"; got != want {
		t.Errorf("rendered = %q, want %q", got, want)
	}
}

// TestInlayHintsOtherPathIgnored keeps another document's hints out.
func TestInlayHintsOtherPathIgnored(t *testing.T) {
	m, _ := loaded(t, "add(1)\n")
	m, _ = m.Update(hintsMsg("/other.go", ilsp.InlayHint{Line: 0, Col: 4, Label: "x:"}))
	if got := firstLine(m); got != "add(1)" {
		t.Errorf("rendered = %q, other-path hints must be ignored", got)
	}
}

// TestInlayHintsConfigToggle stops rendering (without dropping the cached
// hints) while lsp.inlay_hints is false, and resumes when it flips back.
func TestInlayHintsConfigToggle(t *testing.T) {
	m, path := loaded(t, "add(1)\n")
	m, _ = m.Update(hintsMsg(path, ilsp.InlayHint{Line: 0, Col: 4, Label: "x:", PadRight: true}))

	m.Configure(host.MapConfig{"lsp.inlay_hints": "false"})
	if got := firstLine(m); got != "add(1)" {
		t.Errorf("toggle off: rendered = %q, want plain line", got)
	}
	m.Configure(host.MapConfig{"lsp.inlay_hints": "true"})
	if got := firstLine(m); got != "add(x: 1)" {
		t.Errorf("toggle on: rendered = %q, cached hints must resume", got)
	}
}

// TestDisplayOffsetCountsTabsAndHints anchors overlays where renderLine
// actually drew the cell: tabs expand and hint text shifts everything after
// its anchor (#171).
func TestDisplayOffsetCountsTabsAndHints(t *testing.T) {
	m, path := loaded(t, "\tab\n")
	m.Configure(host.MapConfig{"lsp.inlay_hints": "true"}) // default off (#523)
	if got := m.DisplayOffset(0, 2); got != 5 {
		t.Errorf("tab-only DisplayOffset = %d, want 5", got)
	}
	m, _ = m.Update(hintsMsg(path, ilsp.InlayHint{Line: 0, Col: 1, Label: "T:"}))
	if got := m.DisplayOffset(0, 2); got != 7 {
		t.Errorf("with hint DisplayOffset = %d, want 7", got)
	}
	// A hint anchored exactly at the queried column shifts that cell too.
	if got := m.DisplayOffset(0, 1); got != 6 {
		t.Errorf("hint at anchor DisplayOffset = %d, want 6", got)
	}
}
