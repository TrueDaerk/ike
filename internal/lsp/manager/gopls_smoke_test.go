//go:build goplssmoke

// This is a real end-to-end smoke test against an installed gopls. It is gated
// behind the `goplssmoke` build tag so the normal suite stays binary-free; run it
// with: go test -tags goplssmoke ./internal/lsp/manager/ -run Smoke -v
package manager

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/editor/buffer"
	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

func TestSmokeGoplsDiagnosticsAndCompletion(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not on PATH")
	}
	diagCh := make(chan []protocol.Diagnostic, 8)
	spec := lsp.ServerSpec{Language: "go", Command: "gopls", RootMarkers: []string{"go.mod"}}
	m := New(func(string) (lsp.ServerSpec, bool) { return spec, true }, nil, Callbacks{
		Diagnostics: func(path string, p protocol.PublishDiagnosticsParams, lines []string, enc string) {
			diagCh <- p.Diagnostics
		},
		Status: func(lang, text string, kind lsp.ServerStatusKind) { t.Logf("status: %s", text) },
	})
	defer m.Shutdown()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module smoke\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.go")
	// A deliberate error: undefined symbol + unused import.
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tx := undefinedThing\n\t_ = x\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := m.Open(path, "go", src); err != nil {
		t.Fatalf("open: %v", err)
	}

	// gopls publishes diagnostics for the undefined symbol / unused import.
	select {
	case ds := <-diagCh:
		t.Logf("diagnostics: %d", len(ds))
		for _, d := range ds {
			t.Logf("  [%d] %s", d.Severity, d.Message)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no diagnostics from gopls")
	}

	// Completion after "fmt." — request at a position with a real prefix context.
	src2 := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.\n}\n"
	if err := os.WriteFile(path, []byte(src2), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = m.Change(path, src2)
	time.Sleep(500 * time.Millisecond) // let gopls index the change
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	items, _, err := m.Completion(ctx, path, buffer.Position{Line: 5, Col: 5}, ".") // just after "fmt."
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	t.Logf("completion items: %d", len(items))
	if len(items) == 0 {
		t.Fatal("expected completion items after fmt.")
	}
	// Sanity: Println should be among them.
	found := false
	for _, it := range items {
		if it.Label == "Println" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Println in completion items")
	}
}

// TestSmokeGoplsInlayHints drives a real gopls with the Go plugin's baseline
// hint settings (#171) and expects parameter-name and inferred-type hints for
// a plain call + assignment.
func TestSmokeGoplsInlayHints(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not on PATH")
	}
	spec := lsp.ServerSpec{
		Language: "go", Command: "gopls", RootMarkers: []string{"go.mod"},
		Settings: map[string]any{"hints": map[string]any{
			"parameterNames":         true,
			"assignVariableTypes":    true,
			"rangeVariableTypes":     true,
			"compositeLiteralFields": true,
		}},
	}
	m := New(func(string) (lsp.ServerSpec, bool) { return spec, true }, nil, Callbacks{
		Status: func(lang, text string, kind lsp.ServerStatusKind) { t.Logf("status: %s", text) },
	})
	defer m.Shutdown()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module smoke\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.go")
	src := "package main\n\nfunc add(a, b int) int { return a + b }\n\nfunc main() {\n\tx := add(1, 2)\n\t_ = x\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(path, "go", src); err != nil {
		t.Fatalf("open: %v", err)
	}

	// gopls answers empty until the package is type-checked; poll briefly.
	deadline := time.Now().Add(20 * time.Second)
	var hints []lsp.InlayHint
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var err error
		hints, err = m.InlayHints(ctx, path)
		cancel()
		if err != nil {
			t.Fatalf("inlay hints: %v", err)
		}
		if len(hints) > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Logf("hints: %+v", hints)
	var haveParam, haveType bool
	for _, h := range hints {
		if h.Kind == protocol.InlayHintParameter {
			haveParam = true
		}
		if h.Kind == protocol.InlayHintType {
			haveType = true
		}
	}
	if !haveParam || !haveType {
		t.Errorf("expected parameter and type hints, got %+v", hints)
	}
	// The call's first parameter hint anchors on line 5 before the "1".
	sorted := false
	for i := 1; i < len(hints); i++ {
		sorted = hints[i-1].Line < hints[i].Line || (hints[i-1].Line == hints[i].Line && hints[i-1].Col <= hints[i].Col)
		if !sorted {
			break
		}
	}
	if len(hints) > 1 && !sorted {
		t.Errorf("hints must arrive sorted by position: %+v", hints)
	}
}
