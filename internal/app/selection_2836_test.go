package app

import (
	"testing"

	"ike/internal/pane"
)

// selection_2836_test.go covers the find-in-path crash (#2836): with a
// viewer content tab active in an editor pane — an image, a data or hex
// viewer, an archive, a notebook (#1778) — the pane has no editor behind its
// active tab, and activeSelectionText dereferenced a nil one. cmd+shift+f and
// cmd+shift+r both open their overlay now, with an empty prefill.

func TestFindInPathFromViewerTabDoesNotPanic(t *testing.T) {
	for _, tc := range viewerFiles {
		t.Run(tc.name, func(t *testing.T) {
			m := newSized()
			editorKey := m.fileEditorKey()
			m.setFocus(editorKey)
			m.setFocus(pane.ExplorerKey)
			m = explorerOpen(t, m, tc.file(t), false)
			m.setFocus(editorKey)
			inst := m.activeWS().Panes.FocusedInstance()
			if inst == nil || inst.Kind() != pane.KindEditor || inst.Editor() != nil || inst.ActiveContent() == nil {
				t.Fatalf("precondition: an editor pane with a viewer content tab active, got %v", inst)
			}
			if got := m.activeSelectionText(); got != "" {
				t.Fatalf("activeSelectionText = %q, want empty", got)
			}
			tm, _ := m.Update(OpenFindInPathMsg{})
			m = tm.(Model)
			if !m.finder.IsOpen() {
				t.Fatal("find in path must open from a viewer tab")
			}
			m.finder.Close()
			tm, _ = m.Update(OpenReplaceInPathMsg{})
			m = tm.(Model)
			if !m.finder.IsOpen() {
				t.Fatal("replace in path must open from a viewer tab")
			}
		})
	}
}
