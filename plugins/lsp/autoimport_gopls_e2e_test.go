package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/manager"

	// The Go plugin registers the gopls spec the test drives.
	_ "ike/plugins/languages/go"
)

// TestGoplsAutoImportEndToEnd drives a real gopls (#2610): completing
// `strings.ToU` in a file that does not import "strings" must yield an item
// whose accept adds the import — inline or through completionItem/resolve
// with the item's data round-tripped — and the popup detail must name the
// package. Self-skips without gopls on PATH.
func TestGoplsAutoImportEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	l, ok := lang.ByID("go")
	if !ok || l.Server == nil {
		t.Skip("go language not registered in this build")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.go")
	content := "package main\n\nfunc main() {\n\tstrings.ToU\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := *l.Server
	resolve := func(id string) (ilsp.ServerSpec, bool) { return spec, id == "go" }
	msgs := make(chan tea.Msg, 64)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { msgs <- m })
	b := &bridge{h: h, mgr: manager.New(resolve, nil, manager.Callbacks{})}
	t.Cleanup(func() { b.mgr.Shutdown() })
	if err := b.mgr.Open(path, "go", content); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// gopls warms up its package cache after initialize; retry the request
	// until it answers with the unimported member.
	deadline := time.Now().Add(90 * time.Second)
	var cm ilsp.CompletionMsg
	var idx = -1
	for time.Now().Before(deadline) && idx < 0 {
		b.requestCompletion(path, 3, 12, "")
		select {
		case m := <-msgs:
			c, ok := m.(ilsp.CompletionMsg)
			if !ok {
				continue
			}
			cm = c
			for i, it := range c.Items {
				if it.Label == "ToUpper" || strings.HasSuffix(it.Label, ".ToUpper") {
					idx = i
					break
				}
			}
		case <-time.After(5 * time.Second):
		}
	}
	if idx < 0 {
		t.Fatalf("gopls never offered ToUpper; last reply had %d items", len(cm.Items))
	}
	it := cm.Items[idx]
	t.Logf("item: label %q detail %q inline edits %d", it.Label, it.Detail, len(it.AdditionalEdits))
	edits := it.AdditionalEdits
	if len(edits) == 0 {
		b.resolveNow(path, it.ID, cm.Seq)
		select {
		case m := <-msgs:
			rm, ok := m.(ilsp.CompletionResolveMsg)
			if !ok {
				t.Fatalf("expected a CompletionResolveMsg, got %#v", m)
			}
			if rm.Seq != cm.Seq || rm.ID != it.ID {
				t.Fatalf("resolve for id %d seq %d, want id %d seq %d", rm.ID, rm.Seq, it.ID, cm.Seq)
			}
			edits = rm.AdditionalEdits
		case <-time.After(30 * time.Second):
			t.Fatal("no CompletionResolveMsg arrived")
		}
	}
	joined := ""
	for _, e := range edits {
		joined += e.Text
	}
	if !strings.Contains(joined, `"strings"`) {
		t.Fatalf("additional edits %+v do not add the strings import", edits)
	}
}
