package app

// previewdiagrams_test.go guards the root model's half of the diagram
// pipeline (#2421): the missing-renderer hint is one notification per session,
// and preview.rerenderDiagrams reaches every open preview.

import (
	"strings"
	"testing"

	"ike/internal/preview"
)

const diagramDoc = "# Diagram\n\n```mermaid\ngraph TD\n  A-->B\n```\n"

// TestMissingRendererHintOncePerSession: however many fences, panes and
// renders report an uninstalled renderer, the user is told exactly once.
func TestMissingRendererHintOncePerSession(t *testing.T) {
	m, _ := openMarkdownFile(t, diagramDoc)
	tm, _ := m.Update(preview.DiagramMsg{Missing: true, Tool: "mermaid-ascii", Lang: "mermaid"})
	m = tm.(Model)
	if n := lastNotification(t, m); !strings.Contains(n, "install mermaid-ascii") {
		t.Fatalf("notification = %q, want the install hint", n)
	}
	before := len(m.history)
	tm, _ = m.Update(preview.DiagramMsg{Missing: true, Tool: "mermaid-ascii", Lang: "mermaid"})
	m = tm.(Model)
	if len(m.history) != before {
		t.Fatalf("the hint repeated: %d entries, want %d", len(m.history), before)
	}
}

// TestRerenderDiagramsClearsOpenPreviews: the command reaches the open
// preview, and says so when there is none.
func TestRerenderDiagramsClearsOpenPreviews(t *testing.T) {
	m, path := openMarkdownFile(t, diagramDoc)
	tm, _ := m.Update(RerenderDiagramsMsg{})
	m = tm.(Model)
	if n := lastNotification(t, m); !strings.Contains(n, "no markdown preview is open") {
		t.Fatalf("notification = %q, want the no-preview notice", n)
	}

	tm, _ = m.Update(MarkdownPreviewMsg{})
	m = tm.(Model)
	if previewKeyFor(m, path) == "" {
		t.Fatal("the preview pane should be open")
	}
	tm, _ = m.Update(RerenderDiagramsMsg{})
	m = tm.(Model)
	if n := lastNotification(t, m); !strings.Contains(n, "re-rendering diagrams") {
		t.Fatalf("notification = %q, want the re-render notice", n)
	}
}
