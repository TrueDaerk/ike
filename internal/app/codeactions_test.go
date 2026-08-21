package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/intention"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
	"ike/internal/registry"
)

// noticed reports whether the model recorded a notification containing text
// (the Update loop drains the host queue into the history ring each pass).
func noticed(m Model, text string) bool {
	for _, h := range m.history {
		if strings.Contains(h.text, text) {
			return true
		}
	}
	return false
}

func actionsOffer() (ilsp.CodeActionsMsg, *int) {
	var picked int = -1
	return ilsp.CodeActionsMsg{
		Path: "/proj/a.go",
		Actions: []ilsp.CodeActionChoice{
			{Title: "Organize imports", Kind: "source.organizeImports"},
			{Title: "Fix undeclared name", Kind: "quickfix", Preferred: true},
		},
		Apply: func(i int) tea.Cmd {
			picked = i
			return nil
		},
	}, &picked
}

func TestActionsModePreferredFirstAndIndices(t *testing.T) {
	a := &actionsMode{}
	msg, picked := actionsOffer()
	a.Set(msg)
	items := a.Results("", palette.Context{})
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Title != "★ Fix undeclared name" || items[0].Detail != "quick fix" {
		t.Fatalf("preferred action should list first, got %+v", items[0])
	}
	// The picked index must reference the ORIGINAL offer order despite the sort.
	pm, ok := items[0].Msg.(actionPickedMsg)
	if !ok {
		t.Fatalf("msg = %#v", items[0].Msg)
	}
	a.Run(pm)
	if *picked != 1 {
		t.Fatalf("picked = %d, want original index 1", *picked)
	}
}

func TestCodeActionsMsgOpensLockedPicker(t *testing.T) {
	m := sized(t, 100, 40)
	msg, picked := actionsOffer()
	out, _ := m.Update(msg)
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("offer should open the palette")
	}
	// Activating routes through actionPickedMsg to the continuation; the
	// first row is the preferred action, original offer index 1.
	items := m.actions.Results("", palette.Context{})
	out, cmd := m.Update(items[0].Msg)
	m = out.(Model)
	_ = cmd
	if *picked != 1 {
		t.Fatalf("activation should run the continuation, picked = %d", *picked)
	}
}

// TestSetMergedOrder guards the #2020 merge: preferred LSP actions first,
// then the remaining LSP actions in server order, then the built-ins grouped
// by kind — kinds in first-appearance order, stable within one — even when
// the provider list interleaves kinds.
func TestSetMergedOrder(t *testing.T) {
	a := &actionsMode{}
	msg, _ := actionsOffer()
	a.SetMerged(msg, []intention.Item{
		{Title: "Copy Path as jq Expression", Kind: "copy", CommandID: "editor.copyDocPathJQ"},
		{Title: "Run Request", Kind: "http", CommandID: "http.run"},
		{Title: "Copy Path", Kind: "copy", CommandID: "editor.copyDocPath"},
	})
	items := a.Results("", palette.Context{})
	titles := make([]string, len(items))
	for i, it := range items {
		titles[i] = it.Title
	}
	want := []string{
		"★ Fix undeclared name",      // preferred LSP first
		"Organize imports",           // remaining LSP
		"Copy Path as jq Expression", // built-ins: kind "copy" appeared first…
		"Copy Path",                  // …and groups despite the interleave
		"Run Request",
	}
	for i, w := range want {
		if i >= len(titles) || titles[i] != w {
			t.Fatalf("order = %v, want %v", titles, want)
		}
	}
}

// TestMergedActivation guards the two activation paths: an LSP row runs the
// bridge continuation, a built-in row resolves to its command id.
func TestMergedActivation(t *testing.T) {
	a := &actionsMode{}
	msg, picked := actionsOffer()
	a.SetMerged(msg, []intention.Item{{Title: "Decode JWT at Caret", Kind: "decode", CommandID: "editor.decodeJWT"}})
	items := a.Results("", palette.Context{})
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	// Row 0 is the preferred LSP action (original offer index 1).
	pm := items[0].Msg.(actionPickedMsg)
	if id := a.CommandFor(pm); id != "" {
		t.Fatalf("LSP row resolved to command %q", id)
	}
	a.Run(pm)
	if *picked != 1 {
		t.Fatalf("picked = %d, want original index 1", *picked)
	}
	// The built-in row names its command and never touches Apply.
	pm = items[2].Msg.(actionPickedMsg)
	if id := a.CommandFor(pm); id != "editor.decodeJWT" {
		t.Fatalf("builtin row resolved to %q", id)
	}
	*picked = -1
	if cmd := a.Run(pm); cmd != nil || *picked != -1 {
		t.Fatal("builtin row must not run the LSP continuation")
	}
}

// intentionModel opens path with content and puts the caret at (line, col).
// It registers a minimal .json language association (id only — deliberately
// not the json plugin: its ServerSpec would open the missing-server prompt in
// every test model) so the doc-path probes resolve like in the compiled app.
func intentionModel(t *testing.T, name, content string, line, col int) Model {
	t.Helper()
	lang.Register(lang.Language{ID: "json", Extensions: []string{"json"}})
	m := sizedWith(t, registry.Global(), 100, 40)
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	m = out.(Model)
	m.activeEditor().SetCursor(line, col)
	return m
}

// TestIntentionsOfferMergesBuiltinsAnchored guards the #2020 acceptance
// core: an empty LSP offer over a JSON buffer still opens the picker —
// anchored at the caret — with the doc-path built-ins listed.
func TestIntentionsOfferMergesBuiltinsAnchored(t *testing.T) {
	m := intentionModel(t, "x.json", `{"name": "value"}`, 0, 11)
	out, _ := m.Update(ilsp.CodeActionsMsg{Intentions: true})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("merged offer must open the palette")
	}
	if !m.palette.Anchored() {
		t.Fatal("the intention picker must anchor at the caret, not center")
	}
	items := m.actions.Results("", palette.Context{})
	found := false
	for _, it := range items {
		if it.Title == "Copy Path as jq Expression" {
			found = true
		}
	}
	if !found {
		t.Fatalf("doc-path builtin missing from %+v", items)
	}
}

// TestIntentionsEmptyMergeToasts guards the toast move: with no LSP actions
// and no applicable built-ins the picker stays closed and the app reports
// "no code actions here".
func TestIntentionsEmptyMergeToasts(t *testing.T) {
	m := intentionModel(t, "main.go", "", 0, 0)
	out, _ := m.Update(ilsp.CodeActionsMsg{Intentions: true})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("an empty merged offer must not open the picker")
	}
	if !noticed(m, "no code actions here") {
		t.Fatalf("missing empty-offer toast, history = %+v", m.history)
	}
}

// TestActionsModeDigitHints guards #2023: the unfiltered list numbers its
// first nine rows and nothing beyond, and a filter query drops the hints
// (digits type into the query then, so a visible number would lie).
func TestActionsModeDigitHints(t *testing.T) {
	a := &actionsMode{}
	var builtins []intention.Item
	for i := 0; i < 11; i++ {
		builtins = append(builtins, intention.Item{
			Title:     "Action " + strconv.Itoa(i),
			Kind:      "test",
			CommandID: "cmd." + strconv.Itoa(i),
		})
	}
	a.SetMerged(ilsp.CodeActionsMsg{}, builtins)

	items := a.Results("", palette.Context{})
	if len(items) != 11 {
		t.Fatalf("items = %d", len(items))
	}
	for i, it := range items {
		want := ""
		if i < 9 {
			want = strconv.Itoa(i + 1)
		}
		if it.Hint != want {
			t.Fatalf("row %d hint = %q, want %q", i, it.Hint, want)
		}
	}

	for _, it := range a.Results("Action 1", palette.Context{}) {
		if it.Hint != "" {
			t.Fatalf("a filtered list must not show digit hints: %+v", it)
		}
	}
}

// TestIntentionPopupDigitRunsAction drives the #2023 fast path end to end: the
// caret popup is open with an empty query, "2" runs the second listed entry —
// the same dispatch enter on that row would do.
func TestIntentionPopupDigitRunsAction(t *testing.T) {
	m := intentionModel(t, "x.json", `{"name": "value"}`, 0, 11)
	out, _ := m.Update(ilsp.CodeActionsMsg{Intentions: true})
	m = out.(Model)
	items := m.actions.Results("", palette.Context{})
	if len(items) < 2 {
		t.Fatalf("need at least two intention rows, got %+v", items)
	}
	want := items[1].Msg.(actionPickedMsg)

	out, cmd := m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("a digit shortcut must close the popup")
	}
	if cmd == nil {
		t.Fatal("a digit shortcut must dispatch the row")
	}
	got, ok := cmd().(actionPickedMsg)
	if !ok || got != want {
		t.Fatalf("digit emitted %#v, want %#v", cmd(), want)
	}
}

// TestIntentionPopupDigitFiltersWithQuery: once a filter query is typed the
// digits are ordinary query text again and the popup stays open.
func TestIntentionPopupDigitFiltersWithQuery(t *testing.T) {
	m := intentionModel(t, "x.json", `{"name": "value"}`, 0, 11)
	out, _ := m.Update(ilsp.CodeActionsMsg{Intentions: true})
	m = out.(Model)

	out, _ = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m = out.(Model)
	out, cmd := m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m = out.(Model)
	if cmd != nil {
		t.Fatal("with a query typed, the digit must filter instead of running")
	}
	if !m.palette.IsOpen() {
		t.Fatal("filtering must keep the popup open")
	}
	if got := m.palette.Query(); got != "c2" {
		t.Fatalf("query = %q, want the typed digit appended", got)
	}
}

// filelessModel puts content into the untitled startup buffer — no path, the
// shape a cmd+n tab and the split pane a pasted response body lands in share
// (#2027) — and parks the caret at (line, col).
func filelessModel(t *testing.T, content string, line, col int) Model {
	t.Helper()
	m := sizedWith(t, registry.Global(), 100, 40)
	ed := m.activeEditor()
	if ed == nil {
		t.Fatal("the startup model must have an editor to anchor on")
	}
	if ed.HasFile() {
		t.Fatalf("the startup buffer must have no file, got %q", ed.Path())
	}
	if content != "" {
		ed.ApplyTextEdits([]editor.TextEdit{{Text: content}})
	}
	ed.SetCursor(line, col)
	return m
}

// TestIntentionsFilelessBufferOpensPicker guards the #2027 freeze at the app
// surface: alt+enter in a buffer with no file must answer like any other —
// here the caret line is a curl command, an intention the catalog offers in
// any buffer — instead of hanging the Update loop on the bridge's reply.
func TestIntentionsFilelessBufferOpensPicker(t *testing.T) {
	m := filelessModel(t, "curl https://example.com/things", 0, 0)
	out, _ := m.Update(ilsp.CodeActionsMsg{Intentions: true})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("a fileless buffer with applicable built-ins must open the picker")
	}
	if !m.palette.Anchored() {
		t.Fatal("the intention picker must anchor at the caret here too")
	}
	items := m.actions.Results("", palette.Context{})
	found := false
	for _, it := range items {
		if it.Title == "Insert as HTTP Request" {
			found = true
		}
	}
	if !found {
		t.Fatalf("curl built-in missing from %+v", items)
	}
}

// TestIntentionsFilelessBufferEmptyToasts: with nothing applicable at the
// caret the fileless buffer gets the honest verdict — never silence, never a
// hang.
func TestIntentionsFilelessBufferEmptyToasts(t *testing.T) {
	m := filelessModel(t, "", 0, 0)
	out, _ := m.Update(ilsp.CodeActionsMsg{Intentions: true})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("an empty merged offer must not open the picker")
	}
	if !noticed(m, "no code actions here") {
		t.Fatalf("missing empty-offer toast, history = %+v", m.history)
	}
}

// TestActionKindLabel guards #309: LSP kinds render readably and an omitted
// kind still yields a chip.
func TestActionKindLabel(t *testing.T) {
	cases := map[string]string{
		"":                       "action",
		"quickfix":               "quick fix",
		"source.organizeImports": "source · organize imports",
		"refactor.extract":       "refactor · extract",
		"refactor":               "refactor",
	}
	for in, want := range cases {
		if got := actionKindLabel(in); got != want {
			t.Errorf("actionKindLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
