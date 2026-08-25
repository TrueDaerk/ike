package settings

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/concealfilter"
	"ike/internal/config"
)

// concealPage builds the control center over throwaway config layers.
func concealPage(t *testing.T) (*ConcealPage, config.Options) {
	t.Helper()
	restoreConfig(t)
	opts := config.Options{
		UserPath:    filepath.Join(t.TempDir(), "settings.toml"),
		ProjectRoot: t.TempDir(),
	}
	p := NewConcealPage(opts)
	p.SetSubPanelHost(&stubHost{})
	return p, opts
}

// selectKey positions the page on the row editing key.
func selectKey(t *testing.T, p *ConcealPage, key string) {
	t.Helper()
	for i, ri := range p.nav {
		if p.rows[ri].key == key {
			p.sel = i
			return
		}
	}
	t.Fatalf("no row for %s", key)
}

func typeEntry(u *concealEntryForm, s string) {
	for _, r := range s {
		u.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
}

// press routes one key into a sub-panel the way the host does (#883): a
// button's own key is claimed before the panel's Update ever sees it.
func press(sp SubPanel, k tea.KeyPressMsg) tea.Cmd {
	for _, b := range sp.Buttons() {
		if !b.Disabled && b.Key != "" && b.Key == k.String() && b.Do != nil {
			return b.Do()
		}
	}
	return sp.Update(k)
}

// TestConcealPageCoversEveryFamily guards the control-center promise (#2133):
// every registered conceal family has a row, and so do the three coloring
// layers that are not gateable.
func TestConcealPageCoversEveryFamily(t *testing.T) {
	p, _ := concealPage(t)
	seen := map[string]bool{}
	for _, r := range p.rows {
		if r.family != "" {
			seen[r.family] = true
		}
	}
	for _, fam := range concealfilter.Families() {
		if !seen[fam] {
			t.Errorf("no row for conceal family %s", fam)
		}
	}
	keys := map[string]bool{}
	for _, r := range p.rows {
		keys[r.key] = true
	}
	for _, k := range []string{"editor.rainbow_brackets", "editor.color_preview", "editor.id_colors",
		"editor.id_color_min_length", "editor.conceal_include", "editor.conceal_exclude",
		"editor.conceal_file_rules", "editor.number_hint_units", "editor.secret_masking_keys"} {
		if !keys[k] {
			t.Errorf("no row for %s", k)
		}
	}
	// Headers are never selectable; every other row is.
	for _, ri := range p.nav {
		if p.rows[ri].kind == concealHeaderRow {
			t.Fatal("a group header must not be selectable")
		}
	}
}

// TestConcealPageTogglePersists guards the first acceptance criterion: a
// toggle writes its existing config key at user scope and reloads.
func TestConcealPageTogglePersists(t *testing.T) {
	p, _ := concealPage(t)
	selectKey(t, p, "editor.timestamp_decoding")
	before := config.Get().Editor.TimestampDecoding
	apply(t, p.Update(key("enter")))
	if got := config.Get().Editor.TimestampDecoding; got == before {
		t.Fatalf("timestamp_decoding unchanged (%v)", got)
	}
	// And back, through the space alias.
	apply(t, p.Update(key(" ")))
	if got := config.Get().Editor.TimestampDecoding; got != before {
		t.Fatalf("timestamp_decoding = %v, want the original %v", got, before)
	}
}

// TestConcealPageStepperBounds guards the identifier-length stepper: ←/→ move
// it and the entry's bounds hold.
func TestConcealPageStepperBounds(t *testing.T) {
	p, _ := concealPage(t)
	selectKey(t, p, "editor.id_color_min_length")
	before := config.Get().Editor.IDColorMinLength
	apply(t, p.Update(key("right")))
	if got := config.Get().Editor.IDColorMinLength; got != before+1 {
		t.Fatalf("min length = %d, want %d", got, before+1)
	}
	// The row's own bounds come from the schema entry; stepping past the floor
	// writes nothing at all.
	row := p.selected()
	for i := 0; i < row.max+5; i++ {
		if cmd := p.Update(key("left")); cmd != nil {
			apply(t, cmd)
		}
	}
	if got := config.Get().Editor.IDColorMinLength; got != row.min {
		t.Fatalf("min length = %d, want the floor %d", got, row.min)
	}
}

// TestConcealPageResetRemovesKey guards "r": the user-layer override goes away
// and the built-in default is back in force.
func TestConcealPageResetRemovesKey(t *testing.T) {
	p, opts := concealPage(t)
	selectKey(t, p, "editor.digit_grouping")
	apply(t, p.Update(key("enter")))
	apply(t, p.Update(key("r")))
	if got := config.Origin(opts, "editor.digit_grouping"); got != "default" {
		t.Fatalf("origin after reset = %q, want default", got)
	}
}

// openList opens the structured list editor of key.
func openList(t *testing.T, p *ConcealPage, key string) *concealListPanel {
	t.Helper()
	selectKey(t, p, key)
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	panel, ok := p.host.(*stubHost).top().(*concealListPanel)
	if !ok {
		t.Fatalf("enter on %s did not open a list editor", key)
	}
	return panel
}

// TestConcealRuleEditorValidates guards the structured rule editing: the
// family/mode/glob fields compose the `family=-pattern` element the config
// expects, and a family the filter does not know is refused in the form.
func TestConcealRuleEditorValidates(t *testing.T) {
	p, _ := concealPage(t)
	panel := openList(t, p, "editor.conceal_file_rules")
	press(panel, key("a"))
	f, ok := p.host.(*stubHost).top().(*concealEntryForm)
	if !ok {
		t.Fatal("a did not open the entry form")
	}

	// A family nothing registers is rejected, and the form stays open.
	typeEntry(f, "not_a_family")
	f.Update(key("enter"))
	if f.note == "" {
		t.Fatal("an unknown family must be rejected with a message")
	}
	if p.host.(*stubHost).top() != f {
		t.Fatal("a rejected entry must keep the form open")
	}

	// Correct it, switch the mode to exclude, and save.
	for range "not_a_family" {
		f.Update(key("backspace"))
	}
	typeEntry(f, concealfilter.SecretMasking)
	f.Update(key("tab")) // mode
	f.Update(key("right"))
	f.Update(key("tab")) // glob
	typeEntry(f, "**/testdata/**")
	apply(t, f.Update(key("enter")))

	want := concealfilter.SecretMasking + "=-**/testdata/**"
	got := config.Get().Editor.ConcealFileRules
	if len(got) != 1 || got[0] != want {
		t.Fatalf("rules = %q, want [%s]", got, want)
	}
	// And the rule does what it says: masking is off under testdata.
	rules := concealfilter.Compile(nil, nil, got)
	if rules.Allows(concealfilter.SecretMasking, "testdata/fixture.env") {
		t.Fatal("the written rule does not block testdata")
	}
}

// TestConcealUnitEditorValidates guards the field→unit editor: it reuses the
// loader's own element check, so an unknown unit never reaches the config.
func TestConcealUnitEditorValidates(t *testing.T) {
	p, _ := concealPage(t)
	panel := openList(t, p, "editor.number_hint_units")
	press(panel, key("a"))
	f := p.host.(*stubHost).top().(*concealEntryForm)

	typeEntry(f, "retention")
	f.Update(key("tab"))
	typeEntry(f, "parsecs")
	f.Update(key("enter"))
	if f.note == "" {
		t.Fatal("an unknown unit must be rejected")
	}
	for range "parsecs" {
		f.Update(key("backspace"))
	}
	typeEntry(f, "s")
	apply(t, f.Update(key("enter")))
	if got := config.Get().Editor.NumberHintUnits; len(got) != 1 || got[0] != "retention=s" {
		t.Fatalf("units = %q, want [retention=s]", got)
	}
}

// TestConcealSecretEditorKeepsExemptSemantics guards the `-` prefix: the mode
// field is the prefix, so an exemption round-trips through parse and compose.
func TestConcealSecretEditorKeepsExemptSemantics(t *testing.T) {
	p, _ := concealPage(t)
	panel := openList(t, p, "editor.secret_masking_keys")
	press(panel, key("a"))
	f := p.host.(*stubHost).top().(*concealEntryForm)
	f.Update(key("right")) // mask → exempt
	f.Update(key("tab"))
	typeEntry(f, "PUBLIC_TOKEN")
	apply(t, f.Update(key("enter")))
	if got := config.Get().Editor.SecretMaskingKeys; len(got) != 1 || got[0] != "-PUBLIC_TOKEN" {
		t.Fatalf("secret keys = %q, want [-PUBLIC_TOKEN]", got)
	}
	// Re-opening the element must show the exemption as such, not as a
	// pattern that happens to start with a dash.
	panel.sel = 0
	panel.Update(key("enter"))
	edit := p.host.(*stubHost).top().(*concealEntryForm)
	if edit.vals[0] != "exempt" || edit.vals[1] != "PUBLIC_TOKEN" {
		t.Fatalf("parsed element = %q", edit.vals)
	}
}

// TestConcealListDeleteAndReorder guards the remaining list operations: order
// is meaning in both pattern maps (first match wins), so it is editable.
func TestConcealListDeleteAndReorder(t *testing.T) {
	p, _ := concealPage(t)
	panel := openList(t, p, "editor.conceal_exclude")
	apply(t, panel.commit([]string{"*.log", "vendor/**"}))
	panel.sel = 1
	apply(t, panel.move(-1))
	if got := config.Get().Editor.ConcealExclude; got[0] != "vendor/**" {
		t.Fatalf("after reorder = %q", got)
	}
	apply(t, press(panel, key("d")))
	if got := config.Get().Editor.ConcealExclude; len(got) != 1 || got[0] != "*.log" {
		t.Fatalf("after delete = %q", got)
	}
}

// TestConcealPreviewFollowsToggle guards the live preview: the sample line is
// drawn both ways, and the arrow marks the state the toggle currently picks.
func TestConcealPreviewFollowsToggle(t *testing.T) {
	p, _ := concealPage(t)
	for _, tc := range []struct{ key, raw, drawn string }{
		{"editor.timestamp_decoding", "1735689600", "2025-01-01"},
		{"editor.byte_size_hints", "10485760", "10 MiB"},
		{"editor.digit_grouping", "1000000", "1_000_000"},
		{"editor.secret_masking", "s3cr3t-value", "••••"},
	} {
		selectKey(t, p, tc.key)
		on := concealBool(tc.key)
		out := p.View(120, 30)
		if !strings.Contains(out, tc.raw) {
			t.Errorf("%s preview: raw sample %q missing", tc.key, tc.raw)
		}
		if !strings.Contains(out, tc.drawn) {
			t.Errorf("%s preview: drawn form %q missing", tc.key, tc.drawn)
		}
		// Flip it and confirm the marked line moves.
		markedDrawn := previewMarksDrawn(out)
		if markedDrawn != on {
			t.Errorf("%s: preview marks drawn=%v with the toggle %v", tc.key, markedDrawn, on)
		}
		apply(t, p.Update(key("enter")))
		if markedDrawn == previewMarksDrawn(p.View(120, 30)) {
			t.Errorf("%s: preview did not follow the toggle", tc.key)
		}
		apply(t, p.Update(key("enter")))
	}
}

// previewMarksDrawn reports whether the preview's ❯ sits on the "drawn" line.
func previewMarksDrawn(view string) bool {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "❯") && strings.Contains(line, "drawn") {
			return true
		}
	}
	return false
}

// TestConcealFilterPreviewShowsVerdicts guards the file-rule preview: a glob
// list previews as the verdict on sample paths, which is what it decides.
func TestConcealFilterPreviewShowsVerdicts(t *testing.T) {
	p, _ := concealPage(t)
	panel := openList(t, p, "editor.conceal_exclude")
	apply(t, panel.commit([]string{"*.log"}))
	selectKey(t, p, "editor.conceal_exclude")
	out := p.View(120, 30)
	if !strings.Contains(out, "build/run.log") || !strings.Contains(out, "blocked") {
		t.Fatalf("exclude preview does not report the blocked sample path:\n%s", out)
	}
}

// TestConcealPageSearchable guards #886 coverage: the panel-wide filter can
// reach every row of a custom page only through SearchItems.
func TestConcealPageSearchable(t *testing.T) {
	p, _ := concealPage(t)
	items := p.SearchItems()
	if len(items) != len(p.nav) {
		t.Fatalf("%d search items for %d rows", len(items), len(p.nav))
	}
	for _, it := range items {
		if it.Label == "Secret masking" {
			it.Activate()
			if p.selected().key != "editor.secret_masking" {
				t.Fatalf("activate selected %q", p.selected().key)
			}
			return
		}
	}
	t.Fatal("no search item for secret masking")
}

// TestConcealEntriesLeftTheEditorPage guards the migration: the keys moved to
// the control center are gone from the generic Editor page and present exactly
// once on the new one.
func TestConcealEntriesLeftTheEditorPage(t *testing.T) {
	var editor, conceal []Entry
	for _, p := range BasePages(nil, nil, nil) {
		switch p.Title {
		case "Editor":
			editor = p.Entries
		case ConcealPageTitle:
			conceal = p.Entries
		}
	}
	if len(conceal) == 0 {
		t.Fatalf("no %q page in BasePages", ConcealPageTitle)
	}
	moved := map[string]bool{}
	for _, e := range conceal {
		if moved[e.Key] {
			t.Errorf("%s listed twice on the conceal page", e.Key)
		}
		moved[e.Key] = true
	}
	for _, e := range editor {
		if moved[e.Key] {
			t.Errorf("%s still on the Editor page", e.Key)
		}
	}
	for _, fam := range concealfilter.Families() {
		if !moved["editor."+fam] {
			t.Errorf("family %s has no entry on the conceal page", fam)
		}
	}
}

// TestAttachCustomKeepsEntries guards the seam the app registers through: the
// page renders as a model while its entries stay the documented key list.
func TestAttachCustomKeepsEntries(t *testing.T) {
	restoreConfig(t)
	pages := AttachCustom(BasePages(nil, nil, nil), ConcealPageTitle, NewConcealPage(config.Options{}))
	for _, p := range pages {
		if p.Title != ConcealPageTitle {
			continue
		}
		if p.Custom == nil {
			t.Fatal("AttachCustom did not install the model")
		}
		if len(p.Entries) == 0 {
			t.Fatal("AttachCustom dropped the schema entries docgen renders")
		}
		return
	}
	t.Fatalf("no %q page", ConcealPageTitle)
}
