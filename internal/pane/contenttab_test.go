package pane

import (
	"os"
	"path/filepath"
	"testing"
)

// contenttab_test.go covers #1778: tabs as a universal pane facility — any
// viewer pane's content can live in a tab slot next to documents and
// terminals, viewer panes convert into tab hosts, and detached tabs become
// dedicated panes again.

// tmpFile writes a throwaway file and returns its path.
func tmpFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// viewerPanes builds one registered pane per tabbable viewer kind and
// returns their keys, for matrix-style tests.
func viewerPanes(t *testing.T, r *Registry) map[Kind]string {
	t.Helper()
	md := tmpFile(t, "doc.md", "# hi\n")
	img := tmpFile(t, "pic.png", "png")
	l, rr := tmpFile(t, "l.txt", "a\n"), tmpFile(t, "r.txt", "b\n")
	arch := tmpFile(t, "a.zip", "zip")
	db := tmpFile(t, "d.sqlite", "db")
	bin := tmpFile(t, "b.bin", "\x00\x01\x02")
	return map[Kind]string{
		KindMarkdown: r.AddMarkdownPreview(md),
		KindImage:    r.AddImagePreview(img),
		KindDiff:     r.AddDiff(l, rr),
		KindArchive:  r.AddArchiveView(arch),
		KindData:     r.AddDataView(db),
		KindHex:      r.AddHexView(bin),
	}
}

// TestKindTabbable pins the tabbable set: editors, terminals and the viewer
// kinds are in; the explorer, the singleton tool windows — the HTTP response
// viewer included (#2042) — and the merge view stay out.
func TestKindTabbable(t *testing.T) {
	in := []Kind{KindEditor, KindTerminal, KindMarkdown, KindImage, KindDiff, KindArchive, KindData, KindHex}
	out := []Kind{KindExplorer, KindVCS, KindDebug, KindProblems, KindStructure, KindUsages, KindBreakpoints, KindMerge, KindHTTP}
	for _, k := range in {
		if !KindTabbable(k) {
			t.Errorf("kind %d must be tabbable", k)
		}
	}
	for _, k := range out {
		if KindTabbable(k) {
			t.Errorf("kind %d must not be tabbable", k)
		}
	}
}

// TestConvertViewerPaneToTabHost (#1778): converting a markdown pane keeps
// the live preview as the first tab — kind, path, title and context id all
// follow the nested content.
func TestConvertViewerPaneToTabHost(t *testing.T) {
	r := newReg()
	path := tmpFile(t, "doc.md", "# hi\n")
	i := r.Get(r.AddMarkdownPreview(path))
	if !i.ConvertToTabHost() {
		t.Fatal("a markdown pane must convert into a tab host")
	}
	if i.Kind() != KindEditor || i.TabCount() != 1 {
		t.Fatalf("after convert: kind=%d tabs=%d, want editor/1", i.Kind(), i.TabCount())
	}
	c := i.TabContent(0)
	if c == nil || c.Kind() != KindMarkdown || c.Preview().Path() != path {
		t.Fatal("the first tab must carry the live preview")
	}
	if i.Tab(0).Kind() != KindMarkdown {
		t.Fatal("Tab.Kind must report the nested kind")
	}
	if got := i.Tab(0).Title(); got != "doc.md" {
		t.Fatalf("content tab title = %q, want doc.md", got)
	}
	if got := i.ContextID(); got != "preview" {
		t.Fatalf("host context = %q, want preview (active content tab)", got)
	}
}

// TestConvertRefusedOutsideTabbableSet: the singleton tool windows and the
// explorer never convert.
func TestConvertRefusedOutsideTabbableSet(t *testing.T) {
	r := newReg()
	if r.Get(r.AddExplorer()).ConvertToTabHost() {
		t.Fatal("the explorer must not convert")
	}
	if r.Get(r.AddVCS()).ConvertToTabHost() {
		t.Fatal("the VCS tool window must not convert")
	}
	if r.Get(r.AddMerge(tmpFile(t, "c.txt", "x\n"))).ConvertToTabHost() {
		t.Fatal("the merge view must not convert")
	}
}

// TestMergeMatrixViewerIntoEditorHost (#1778): every tabbable viewer kind's
// content detaches and joins an editor pane's tab list; the host context
// follows the active tab in every combination.
func TestMergeMatrixViewerIntoEditorHost(t *testing.T) {
	r := newReg()
	host := r.Get(r.AddEditor())
	keys := viewerPanes(t, r)
	want := len(keys)
	for kind, key := range keys {
		src := r.Get(key)
		nested, ok := src.DetachContent()
		if !ok {
			t.Fatalf("kind %d: DetachContent failed", kind)
		}
		if !host.AddContentTab(nested) {
			t.Fatalf("kind %d: AddContentTab failed", kind)
		}
		if host.TabContent(host.ActiveTab()) != nested {
			t.Fatalf("kind %d: new content tab must be active", kind)
		}
		if host.ContextID() != nested.ContextID() {
			t.Fatalf("kind %d: host context %q, want %q", kind, host.ContextID(), nested.ContextID())
		}
	}
	if host.TabCount() != 1+want {
		t.Fatalf("host tabs = %d, want %d", host.TabCount(), 1+want)
	}
	// The scratch document tab still resolves the editor context when active.
	host.ActivateTab(0)
	if host.ContextID() != "editor" {
		t.Fatalf("editor tab active: context %q, want editor", host.ContextID())
	}
	// Content tabs are exempt from the file-tab limit accounting (#742).
	if host.FileTabCount() != 1 {
		t.Fatalf("FileTabCount = %d, want 1 (content tabs exempt)", host.FileTabCount())
	}
	if _, ok := host.EvictableLRUTab(); ok {
		t.Fatal("no content tab may be LRU-evictable")
	}
}

// TestDetachContentTabRoundtrip: a content tab detaches (never the last tab),
// re-adopts, and splits back into a dedicated pane under a fresh key.
func TestDetachContentTabRoundtrip(t *testing.T) {
	r := newReg()
	path := tmpFile(t, "doc.md", "# hi\n")
	i := r.Get(r.AddMarkdownPreview(path))
	if !i.ConvertToTabHost() {
		t.Fatal("convert failed")
	}
	if _, ok := i.DetachContentTab(0); ok {
		t.Fatal("the sole tab must not detach")
	}
	i.AddTab() // a scratch document beside the preview
	nested, ok := i.DetachContentTab(0)
	if !ok || nested.Kind() != KindMarkdown {
		t.Fatal("content tab must detach once another tab exists")
	}
	if i.TabCount() != 1 {
		t.Fatalf("host tabs after detach = %d, want 1", i.TabCount())
	}
	key, ok := r.AddContentPaneFrom(nested)
	if !ok {
		t.Fatal("AddContentPaneFrom must register the detached content")
	}
	if key == i.Key() || !r.Has(key) {
		t.Fatalf("re-registered under %q — must be a fresh registered key", key)
	}
	got := r.Get(key)
	if got.Kind() != KindMarkdown || got.Preview().Path() != path {
		t.Fatal("the dedicated pane must carry the same live preview")
	}
}

// TestHTTPViewerNeverNests (#2042): the HTTP response viewer is a tool
// window with a fixed position in the layout model, not editor content — it
// refuses to detach into a tab, to convert into a tab host, and to restore
// as a content tab (the legacy layout.json migration path).
func TestHTTPViewerNeverNests(t *testing.T) {
	r := newReg()
	src := r.Get(r.AddHTTP())
	if _, ok := src.DetachContent(); ok {
		t.Fatal("the HTTP viewer must not detach into a tab")
	}
	if src.ConvertToTabHost() {
		t.Fatal("the HTTP viewer must not convert into a tab host")
	}
	if nested := r.NewContentPane(KindHTTP, "", "", "", ""); nested != nil {
		t.Fatal("a legacy nested-http tab must restore as nothing")
	}
}

// TestAdoptTabsFromMovesMixedContent (#1778): a whole-host merge moves
// documents, content tabs and pins, deduping files the target already shows.
func TestAdoptTabsFromMovesMixedContent(t *testing.T) {
	r := newReg()
	src := r.Get(r.AddEditor())
	shared := loadTab(t, src, "shared.txt")
	src.AddTab()
	loadTab(t, src, "only.txt")
	md := r.Get(r.AddMarkdownPreview(tmpFile(t, "doc.md", "# x\n")))
	nested, _ := md.DetachContent()
	src.AddContentTab(nested)
	src.SetTabPinned(src.ActiveTab(), true)

	dst := r.Get(r.AddEditor())
	if err := dst.Editor().Load(shared); err != nil {
		t.Fatal(err)
	}
	if !dst.AdoptTabsFrom(src) {
		t.Fatal("AdoptTabsFrom failed")
	}
	// shared.txt stays behind (dedupe); only.txt and the preview move.
	if src.TabCount() != 1 || src.TabForPath(shared) != 0 {
		t.Fatalf("source keeps only the duplicate: tabs=%d", src.TabCount())
	}
	if dst.TabCount() != 3 {
		t.Fatalf("target tabs = %d, want 3 (shared, only, preview)", dst.TabCount())
	}
	if dst.TabForPath(shared) < 0 {
		t.Fatal("target must keep its own shared.txt tab")
	}
	last := dst.TabCount() - 1
	if c := dst.TabContent(last); c == nil || c.Kind() != KindMarkdown {
		t.Fatal("the content tab must arrive last")
	}
	if !dst.TabPinned(last) {
		t.Fatal("the pin must survive the move (#1172)")
	}
}
