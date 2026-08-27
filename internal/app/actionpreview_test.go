package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/palette"
)

// actionpreview_test.go covers the intention popup's diff preview (#2252):
// what the highlighted row shows, that resolving is lazy and debounced, that
// rows without an edit say "no preview", and that previewing never touches the
// buffer.

// selectedRow returns the popup's currently highlighted row.
func selectedRow(t *testing.T, m Model) palette.Item {
	t.Helper()
	items := m.actions.Results("", palette.Context{})
	if len(items) == 0 {
		t.Fatal("the popup lists no rows")
	}
	return items[0]
}

// rowTitled returns the popup row with the given title.
func rowTitled(t *testing.T, m Model, title string) palette.Item {
	t.Helper()
	for _, it := range m.actions.Results("", palette.Context{}) {
		if it.Title == title {
			return it
		}
	}
	t.Fatalf("no row titled %q in %v", title, m.actions.Results("", palette.Context{}))
	return palette.Item{}
}

// settle drives the popup's preview for one row the way the palette does:
// the debounce fires, the mode resolves, and any resulting command runs.
func settlePreview(t *testing.T, m Model, row palette.Item) tea.Cmd {
	t.Helper()
	return m.actions.SelectionChanged(row, palette.Context{})
}

// footerOf renders the preview footer of one row.
func footerOf(t *testing.T, m Model, row palette.Item) string {
	t.Helper()
	return strings.Join(m.actions.Footer(row, 60), "\n")
}

// TestIntentionPreviewShowsBuiltinEdit: highlighting a built-in intention that
// rewrites the buffer shows its diff — the "+" side is what applying writes.
func TestIntentionPreviewShowsBuiltinEdit(t *testing.T) {
	m := intentionModel(t, "x.json", `{"debug": true}`, 0, 10)
	out, _ := m.Update(ilsp.CodeActionsMsg{Intentions: true})
	m = out.(Model)

	row := rowTitled(t, m, "Toggle Value Under Caret")
	settlePreview(t, m, row)
	foot := footerOf(t, m, row)
	if !strings.Contains(foot, `{"debug": true}`) || !strings.Contains(foot, `{"debug": false}`) {
		t.Fatalf("preview must diff the toggle, got:\n%s", foot)
	}
	if !strings.Contains(foot, "-") || !strings.Contains(foot, "+") {
		t.Fatalf("preview must render as a diff, got:\n%s", foot)
	}
}

// TestIntentionPreviewDoesNotTouchTheBuffer is the "preview applies nothing"
// contract: the buffer text and its dirty flag are what they were.
func TestIntentionPreviewDoesNotTouchTheBuffer(t *testing.T) {
	const content = `{"debug": true}`
	m := intentionModel(t, "x.json", content, 0, 10)
	out, _ := m.Update(ilsp.CodeActionsMsg{Intentions: true})
	m = out.(Model)

	row := rowTitled(t, m, "Toggle Value Under Caret")
	settlePreview(t, m, row)
	footerOf(t, m, row)

	ed := m.activeEditor()
	if got := ed.Text(); got != content {
		t.Fatalf("buffer = %q, want it untouched by the preview", got)
	}
	if ed.Dirty() {
		t.Fatal("previewing must not make the buffer dirty")
	}
}

// TestIntentionPreviewCommandRowSaysNoPreview: an entry that dispatches a
// command has no edit to show and says so instead of an empty diff.
func TestIntentionPreviewCommandRowSaysNoPreview(t *testing.T) {
	m := intentionModel(t, "x.json", `{"name": "value"}`, 0, 11)
	out, _ := m.Update(ilsp.CodeActionsMsg{Intentions: true})
	m = out.(Model)

	row := rowTitled(t, m, "Copy Path as jq Expression")
	if cmd := settlePreview(t, m, row); cmd != nil {
		t.Fatal("a command row must resolve locally, without a request")
	}
	if foot := footerOf(t, m, row); !strings.Contains(foot, noPreviewNote) {
		t.Fatalf("footer = %q, want the no-preview note", foot)
	}
}

// TestIntentionPreviewResolvesLSPRowLazily: an LSP row asks the bridge for its
// preview exactly once — highlighting it again reuses the answer.
func TestIntentionPreviewResolvesLSPRowLazily(t *testing.T) {
	m := intentionModel(t, "x.json", `{"name": "value"}`, 0, 11)
	asked := 0
	out, _ := m.Update(ilsp.CodeActionsMsg{
		Intentions: true,
		Path:       m.activeEditor().Path(),
		Actions:    []ilsp.CodeActionChoice{{Title: "Sort keys", Kind: "refactor"}},
		Apply:      func(int) tea.Cmd { return nil },
		Preview: func(i int) tea.Cmd {
			asked++
			return nil
		},
	})
	m = out.(Model)

	row := rowTitled(t, m, "Sort keys")
	settlePreview(t, m, row)
	if asked != 1 {
		t.Fatalf("preview requests = %d, want one for the settled row", asked)
	}
	if foot := footerOf(t, m, row); !strings.Contains(foot, "resolving") {
		t.Fatalf("footer = %q, want the pending note while the reply is out", foot)
	}

	// The reply lands and is rendered for that row.
	m.actions.SetActionPreview(ilsp.ActionPreviewMsg{
		Path:  m.activeEditor().Path(),
		Index: 0,
		Files: []ilsp.PreviewFile{{
			Path:   m.activeEditor().Path(),
			Edits:  1,
			Before: "{\"b\": 1,\n\"a\": 2}",
			After:  "{\"a\": 2,\n\"b\": 1}",
		}},
	})
	foot := footerOf(t, m, row)
	if !strings.Contains(foot, "\"a\": 2") {
		t.Fatalf("footer must render the previewed WorkspaceEdit, got:\n%s", foot)
	}

	// Highlighting the same row again resolves nothing further.
	settlePreview(t, m, row)
	if asked != 1 {
		t.Fatalf("preview requests = %d, want the resolved row reused", asked)
	}
}

// TestIntentionPreviewIgnoresRepliesForAnotherOffer: a late reply naming a
// different file is dropped instead of shown against the current rows.
func TestIntentionPreviewIgnoresRepliesForAnotherOffer(t *testing.T) {
	m := intentionModel(t, "x.json", `{"name": "value"}`, 0, 11)
	out, _ := m.Update(ilsp.CodeActionsMsg{
		Intentions: true,
		Path:       m.activeEditor().Path(),
		Actions:    []ilsp.CodeActionChoice{{Title: "Sort keys", Kind: "refactor"}},
		Apply:      func(int) tea.Cmd { return nil },
		Preview:    func(int) tea.Cmd { return nil },
	})
	m = out.(Model)

	row := rowTitled(t, m, "Sort keys")
	settlePreview(t, m, row)
	m.actions.SetActionPreview(ilsp.ActionPreviewMsg{
		Path:  "/somewhere/else.json",
		Index: 0,
		Files: []ilsp.PreviewFile{{Path: "/somewhere/else.json", Before: "a", After: "b"}},
	})
	if foot := footerOf(t, m, row); !strings.Contains(foot, "resolving") {
		t.Fatalf("footer = %q, want the stale reply ignored", foot)
	}
}

// TestIntentionPreviewFallsBackWithoutABridge: an offer that reached no server
// cannot preview its rows, and says so rather than waiting forever.
func TestIntentionPreviewFallsBackWithoutABridge(t *testing.T) {
	m := intentionModel(t, "x.json", `{"name": "value"}`, 0, 11)
	out, _ := m.Update(ilsp.CodeActionsMsg{
		Intentions: true,
		Path:       m.activeEditor().Path(),
		Actions:    []ilsp.CodeActionChoice{{Title: "Sort keys", Kind: "refactor"}},
	})
	m = out.(Model)

	row := selectedRow(t, m)
	if cmd := settlePreview(t, m, row); cmd != nil {
		t.Fatal("without a preview continuation nothing may be requested")
	}
	if foot := footerOf(t, m, row); !strings.Contains(foot, noPreviewNote) {
		t.Fatalf("footer = %q, want the no-preview note", foot)
	}
}

// TestIntentionPopupSchedulesPreviewOnOpen: opening highlights a row, which is
// a selection change — so the debounce is scheduled right away.
func TestIntentionPopupSchedulesPreviewOnOpen(t *testing.T) {
	m := intentionModel(t, "x.json", `{"debug": true}`, 0, 10)
	out, cmd := m.Update(ilsp.CodeActionsMsg{Intentions: true})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("the popup must open")
	}
	if cmd == nil {
		t.Fatal("opening must schedule the preview debounce for the first row")
	}
	if !hasSelectionTick(cmd()) {
		t.Fatalf("scheduled %#v, want a selection tick among the commands", cmd())
	}
}

// hasSelectionTick reports whether a (possibly batched) message carries the
// palette's selection debounce.
func hasSelectionTick(msg tea.Msg) bool {
	if _, ok := msg.(palette.SelectionTickMsg); ok {
		return true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return false
	}
	for _, c := range batch {
		if c != nil && hasSelectionTick(c()) {
			return true
		}
	}
	return false
}
